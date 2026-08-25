package runners

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
)

func summaryHistoryMessage(id string, role string, stopReason string, text string) *models.ConversationMessage {
	message := NewTextMessage(role, text)
	message.ID = id
	if stopReason != "" {
		reason := models.StopReason(stopReason)
		message.StopReason = &reason
	}
	return &message
}

func toolCallHistoryMessage(id string, toolCallId string) *models.ConversationMessage {
	message := summaryHistoryMessage(id, "assistant", "tool_calls", "")
	toolCalls, err := json.Marshal([]providers.ToolCall{{
		ID:       toolCallId,
		Type:     "function",
		Function: providers.FunctionCall{Name: "datetime", Arguments: "{}"},
	}})
	if err != nil {
		panic(err)
	}
	message.ToolCalls = toolCalls
	return message
}

func toolResultHistoryMessage(id string, toolCallId string) *models.ConversationMessage {
	message := summaryHistoryMessage(id, "tool", "", "{}")
	message.ToolCallID = &toolCallId
	toolName := "datetime"
	message.ToolName = &toolName
	return message
}

// withResumeFrom stamps the metadata summarizeAndPersist writes when
// compaction knew its coverage.
func withResumeFrom(message *models.ConversationMessage, resumeFromMessageId string) *models.ConversationMessage {
	metadata, err := json.Marshal(map[string]interface{}{
		summaryMetadataKey:           true,
		resumeFromMessageMetadataKey: resumeFromMessageId,
		coverageRecordedMetadataKey:  true,
	})
	if err != nil {
		panic(err)
	}
	message.Metadata = metadata
	return message
}

// withoutCoverage stamps the metadata a legacy summary carries: the
// summary flag and nothing about what it covers.
func withoutCoverage(message *models.ConversationMessage) *models.ConversationMessage {
	metadata, err := json.Marshal(map[string]bool{summaryMetadataKey: true})
	if err != nil {
		panic(err)
	}
	message.Metadata = metadata
	return message
}

func promptShape(t *testing.T, history []*models.ConversationMessage) (transcript int, userTurns int) {
	t.Helper()
	ctx, _ := newSystemPromptTestContext(t, "user-1", "default")
	runner := &Runner{AgentID: "default"}
	for _, message := range runner.buildMessages(ctx, history, SystemPromptModeFull, "") {
		if message.Role != "system" {
			transcript++
		}
		if message.Role == "user" {
			userTurns++
		}
	}
	return transcript, userTurns
}

// The shape that burned 6.6M tokens over 503 assistant turns without
// answering: compaction summarized a prefix, kept a verbatim tail, and
// appended the summary after the user's question. Cutting at the
// summary dropped the question and everything since, and the skip loop
// then walked off the end because a tool loop has no terminal stop
// reason.
func TestBuildMessagesLegacySummaryAfterUserQuestion(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "user", "", "what is the pallet height"),
		withoutCoverage(summaryHistoryMessage("m2", "system", "context_summary", "summary")),
		summaryHistoryMessage("m3", "assistant", "tool_calls", ""),
		summaryHistoryMessage("m4", "tool", "", "{}"),
		summaryHistoryMessage("m5", "user", "", "hello"),
		summaryHistoryMessage("m6", "assistant", "tool_calls", ""),
		summaryHistoryMessage("m7", "tool", "", "{}"),
	}
	transcript, userTurns := promptShape(t, history)
	if transcript == 0 {
		t.Fatal("prompt carries no transcript: the model has nothing to answer")
	}
	if userTurns == 0 {
		t.Fatal("the user's question is not in the prompt")
	}
}

// A legacy summary appended as the very last message is the exact
// production row: guessing from its position drops everything.
func TestBuildMessagesLegacySummaryAtEndKeepsTheQuestion(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "user", "", "what is the pallet height"),
		withoutCoverage(summaryHistoryMessage("m2", "system", "context_summary", "summary")),
	}
	startIndex, _ := summaryStartIndex(history)
	if startIndex != 0 {
		t.Fatalf("startIndex = %d, want 0 (the user question must survive)", startIndex)
	}
	if _, userTurns := promptShape(t, history); userTurns != 1 {
		t.Fatalf("prompt carries %d user turns, want 1", userTurns)
	}
}

// compressContext keeps a verbatim tail and appends the summary after
// it. The transcript resumes at the start of that tail, not at the
// summary message.
func TestSummaryStartIndexResumesAtTheKeptTail(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "user", "", "first question"),
		summaryHistoryMessage("m2", "assistant", "stop", "first answer"),
		summaryHistoryMessage("m3", "user", "", "second question"),
		withResumeFrom(summaryHistoryMessage("m4", "system", "context_summary", "summary"), "m3"),
	}
	startIndex, summaryText := summaryStartIndex(history)
	if summaryText != "summary" {
		t.Fatalf("summaryText = %q, want %q", summaryText, "summary")
	}
	if startIndex != 2 {
		t.Fatalf("startIndex = %d, want 2 (the kept tail)", startIndex)
	}
}

// A long tool loop has its only user turn near the start. Rewinding to
// it would replay every summarized message, so compaction would never
// shrink the prompt and would bill a summarizer pass per round.
func TestSummaryStartIndexRecordedResumeIsNotRewound(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "user", "", "the only question"),
	}
	for index := 2; index <= 40; index++ {
		history = append(history, summaryHistoryMessage("m"+strconv.Itoa(index), "assistant", "tool_calls", "step"))
	}
	history = append(history, withResumeFrom(summaryHistoryMessage("mS", "system", "context_summary", "summary"), "m30"))

	startIndex, _ := summaryStartIndex(history)
	if startIndex != 29 {
		t.Fatalf("startIndex = %d, want 29 (the recorded resume point, not the user turn at 0)", startIndex)
	}
}

// A recorded resume point is already a well-formed boundary. Treating
// the assistant message it lands on as a crash fragment would skip to
// the next user message and discard the verbatim tail, including the
// assistant's own final answer, which the summary does not hold.
func TestSummaryStartIndexRecordedResumeKeepsTheTail(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("u1", "user", "", "question"),
		toolCallHistoryMessage("a1", "call-a"),
		toolResultHistoryMessage("t1", "call-a"),
		toolCallHistoryMessage("a2", "call-b"),
		toolResultHistoryMessage("t2", "call-b"),
		withResumeFrom(summaryHistoryMessage("S", "system", "context_summary", "summary"), "a2"),
		summaryHistoryMessage("a3", "assistant", "stop", "final answer"),
		summaryHistoryMessage("u2", "user", "", "follow-up"),
	}
	startIndex, _ := summaryStartIndex(history)
	if startIndex != 3 {
		t.Fatalf("startIndex = %d, want 3 (the recorded resume point)", startIndex)
	}
}

// A mid-loop prompt legitimately carries no user message. The guard
// must not fire there, or it discards the recorded cut and replays.
func TestBuildMessagesCompactedToolLoopKeepsItsCut(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("u1", "user", "", "the only question"),
	}
	for index := 1; index <= 12; index++ {
		suffix := strconv.Itoa(index)
		history = append(history,
			toolCallHistoryMessage("a"+suffix, "call-"+suffix),
			toolResultHistoryMessage("t"+suffix, "call-"+suffix))
	}
	history = append(history, withResumeFrom(summaryHistoryMessage("S", "system", "context_summary", "summary"), "a10"))

	transcript, userTurns := promptShape(t, history)
	if userTurns != 0 {
		t.Fatalf("fixture is wrong: the kept tail should hold no user message, got %d", userTurns)
	}
	if transcript > 8 {
		t.Fatalf("prompt carries %d transcript rows: the cut was discarded and the loop replayed", transcript)
	}
}

// The compact tool runs mid-turn, so its own tool result is the only
// message after the summary and its assistant is cut away. Every later
// pass can delete messages, so the guard has to hold on the final list.
func TestBuildMessagesCompactToolMidTurnLeavesATurnToAnswer(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("u1", "user", "", "the question"),
		toolCallHistoryMessage("a1", "call-a"),
		toolResultHistoryMessage("t1", "call-a"),
		toolCallHistoryMessage("aC", "call-compact"),
		withResumeFrom(summaryHistoryMessage("S", "system", "context_summary", "summary"), ""),
		toolResultHistoryMessage("tC", "call-compact"),
	}
	if transcript, _ := promptShape(t, history); transcript == 0 {
		t.Fatal("prompt carries no transcript: this is the incident, reachable again")
	}
}

// A cut between an assistant and its results strands them, and every
// provider rejects a tool result it cannot pair.
func TestBuildMessagesDropsToolResultsWhoseCallWasCutAway(t *testing.T) {
	ctx, _ := newSystemPromptTestContext(t, "user-1", "default")
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "user", "", "first question"),
		toolCallHistoryMessage("m2", "call-a"),
		withResumeFrom(summaryHistoryMessage("m3", "system", "context_summary", "summary"), "m4"),
		summaryHistoryMessage("m4", "user", "", "mid-run question"),
		toolResultHistoryMessage("m5", "call-a"),
	}
	runner := &Runner{AgentID: "default"}
	messages := runner.buildMessages(ctx, history, SystemPromptModeFull, "")

	announced := make(map[string]bool)
	sawUser := false
	for _, message := range messages {
		for _, toolCall := range message.ToolCalls {
			announced[toolCall.ID] = true
		}
		if message.Role == "user" {
			sawUser = true
		}
		if message.Role == "tool" && message.ToolCallID != "" && !announced[message.ToolCallID] {
			t.Fatalf("orphan tool result %q: the provider rejects a result it cannot pair", message.ToolCallID)
		}
	}
	if !sawUser {
		t.Fatal("the user question was dropped along with the orphan")
	}
}

func TestFindResumeFromMessageIdNamesTheOldestKeptMessage(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("u0", "user", "", "first"),
		toolCallHistoryMessage("a1", "call-a"),
		summaryHistoryMessage("u1", "user", "", "mid-run"),
		toolResultHistoryMessage("t1", "call-a"),
	}
	kept := []providers.ChatMessage{
		{Role: "tool", SourceMessageID: "t1"},
		{Role: "user", SourceMessageID: "u1"},
	}
	if got := findResumeFromMessageId(history, kept); got != "u1" {
		t.Fatalf("findResumeFromMessageId = %q, want u1 (the oldest kept message in history order)", got)
	}
	if got := findResumeFromMessageId(history, nil); got != "" {
		t.Fatalf("empty tail = %q, want \"\"", got)
	}
}

func TestSummaryResumeFromRoundTripsThroughTheMetadataKeys(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		resumeFrom       string
		coverageRecorded bool
	}{
		{"recorded", "m7", true},
		{"kept nothing", "", true},
		{"degraded", "", false},
	} {
		metadata, err := json.Marshal(map[string]interface{}{
			summaryMetadataKey:           true,
			resumeFromMessageMetadataKey: testCase.resumeFrom,
			coverageRecordedMetadataKey:  testCase.coverageRecorded,
		})
		if err != nil {
			t.Fatal(err)
		}
		gotResumeFrom, gotRecorded := summaryResumeFrom(&models.ConversationMessage{Metadata: metadata})
		if gotResumeFrom != testCase.resumeFrom || gotRecorded != testCase.coverageRecorded {
			t.Fatalf("%s: got (%q, %t), want (%q, %t)",
				testCase.name, gotResumeFrom, gotRecorded, testCase.resumeFrom, testCase.coverageRecorded)
		}
	}
}

func TestSkipOrphanedTurnFragmentsStopsAtTheFirstUserMessage(t *testing.T) {
	history := []*models.ConversationMessage{
		summaryHistoryMessage("m1", "assistant", "tool_calls", ""),
		summaryHistoryMessage("m2", "user", "", "first"),
		summaryHistoryMessage("m3", "user", "", "second"),
	}
	if got := skipOrphanedTurnFragments(history, 0); got != 1 {
		t.Fatalf("skipOrphanedTurnFragments = %d, want 1 (stop at the first user message)", got)
	}
}

func TestDropUnpairedToolResults(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "tool", ToolCallID: "orphan", Content: "{}"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-a", Function: providers.FunctionCall{Name: "datetime"}}}},
		{Role: "tool", ToolCallID: "call-a", Content: "{}"},
		{Role: "tool", Content: "no call id"},
	}
	kept := dropUnpairedToolResults(messages)
	if len(kept) != 3 {
		t.Fatalf("kept %d messages, want 3 (the id-less tool message is not an orphan)", len(kept))
	}
	if kept[0].Role != "assistant" {
		t.Fatalf("orphan survived: %+v", kept[0])
	}
}

// computeKeepBoundary must leave the verbatim tail starting on a turn
// boundary. expandKeepBoundaryForRecentTokens widens the tail by token
// count alone, so on its own it can land the boundary on a tool result
// whose assistant is about to be summarized away — a prompt every
// provider rejects.
func TestComputeKeepBoundaryTailNeverStartsOnAToolResult(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "question"},
	}
	for index := range 6 {
		callId := "call-" + strconv.Itoa(index)
		messages = append(messages,
			providers.ChatMessage{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: callId, Function: providers.FunctionCall{Name: "datetime"}},
			}},
			providers.ChatMessage{Role: "tool", ToolCallID: callId, Content: strings.Repeat("x", 4000)})
	}
	// 3000 and 5000 are the values that actually land the raw expansion
	// on a tool result for this fixture; a sweep that misses them
	// passes with the pairing correction removed.
	for _, minKeepRecentTokens := range []int{0, 100, 2000, 3000, 5000, 8000} {
		keepIndex := computeKeepBoundary(messages, contextCompressionLimits{
			MinKeepMessages:     4,
			MinKeepRecentTokens: minKeepRecentTokens,
		})
		if keepIndex < 1 || keepIndex > len(messages) {
			t.Fatalf("minKeepRecentTokens=%d: keepIndex %d out of range", minKeepRecentTokens, keepIndex)
		}
		if keepIndex < len(messages) && messages[keepIndex].Role == "tool" {
			t.Fatalf("minKeepRecentTokens=%d: tail starts on a tool result at %d, its assistant is summarized away",
				minKeepRecentTokens, keepIndex)
		}
	}
}

// The compact tool must always leave a verbatim tail: its own result is
// appended right after the summary, and with nothing kept that result
// has no assistant to pair against.
func TestSplitForCompactionAlwaysKeepsATail(t *testing.T) {
	base := []providers.ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "question"},
	}
	shapes := map[string][]providers.ChatMessage{
		"assistant landed": append(append([]providers.ChatMessage{}, base...),
			providers.ChatMessage{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "call-compact", Function: providers.FunctionCall{Name: "conversation"}},
			}}),
		"assistant not yet persisted": append(append([]providers.ChatMessage{}, base...),
			providers.ChatMessage{Role: "assistant", Content: "earlier answer"},
			providers.ChatMessage{Role: "user", Content: "follow-up"}),
	}
	for name, messages := range shapes {
		prefix, kept := splitForCompaction(messages, compactToolMinKeepMessages)
		if len(kept) == 0 {
			t.Fatalf("%s: kept no tail, so the compact tool's own result would be orphaned", name)
		}
		if len(prefix)+len(kept) != len(messages)-1 {
			t.Fatalf("%s: split lost messages: %d + %d != %d", name, len(prefix), len(kept), len(messages)-1)
		}
	}
	// Nothing worth summarizing must produce an empty prefix so the
	// caller skips persisting a summary for a compaction that did not
	// happen.
	for name, messages := range map[string][]providers.ChatMessage{
		"empty":       {},
		"system only": {{Role: "system", Content: "system prompt"}},
		"system plus one": {
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "question"},
		},
	} {
		if prefix, _ := splitForCompaction(messages, compactToolMinKeepMessages); len(prefix) != 0 {
			t.Fatalf("%s: prefix has %d messages, want 0", name, len(prefix))
		}
	}
}
