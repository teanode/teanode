# `ask_user_question` Tool — Design & Implementation Plan

## Overview

A new builtin tool that allows an LLM agent to present a question with multiple choices to the human user, wait for an answer, and receive the selection as a tool result. The tool blocks the runner's tool-call loop (inside `Execute()`) until the user responds or the run is aborted.

**Design constraints:**
- No changes to the runner's execution model — tools block in `Execute()` as they do today.
- No new persistence layer or store records — pending questions exist only in the in-memory broker and are not persisted beyond what the runner already writes (assistant messages with tool calls, tool result messages).

---

## 1. Tool Definition

**Package:** `internal/tools/askuser/askuser.go`

```go
type askUserQuestionTool struct{}

func init() {
    tools.RegisterBuiltinTool(func() []tools.Tool {
        return []tools.Tool{&askUserQuestionTool{}}
    })
}
```

The tool implements the `tools.Tool` interface (defined in `internal/tools/tools.go:12-15`):

```go
type Tool interface {
    Definition() providers.ToolDefinition
    Execute(ctx context.Context, arguments string) (string, error)
}
```

**LLM-facing definition:**

```go
providers.ToolDefinition{
    Type: "function",
    Function: providers.FunctionSpec{
        Name:        "ask_user_question",
        Description: "Present a question with choices to the user and wait for their answer. Only works on the web UI channel.",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "question": map[string]interface{}{
                    "type":        "string",
                    "description": "The question to present to the user.",
                },
                "choices": map[string]interface{}{
                    "type": "array",
                    "items": map[string]interface{}{
                        "type": "string",
                    },
                    "minItems":    2,
                    "description": "List of choices the user can pick from.",
                },
            },
            "required": []string{"question", "choices"},
        },
        Returns: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "answer": map[string]interface{}{"type": "string"},
            },
        },
    },
}
```

**Input JSON (from LLM):**

```json
{
  "question": "Which database should we use?",
  "choices": ["PostgreSQL", "SQLite", "MySQL"]
}
```

**Output JSON (tool result to LLM):**

```json
{ "answer": "PostgreSQL" }
```

**Error result (non-web channel):**

```json
{ "error": "ask_user_question is not supported on the telegram channel" }
```

---

## 2. Channel Gating

### Problem

The `Origin` field (e.g. `"webui"`, `"telegram"`, `"discord"`, `""`) lives on `coordinators.RunParameters` (`internal/coordinators/handle.go:16`) but is not propagated to the runner context. The coordinator broadcasts it as part of the `user_message` event payload (`coordinator.go:151-153`) but does not set it on the run context. Tools receive a `context.Context` but have no way to determine the channel today.

### Solution: Add Origin to Runner Context

Add a new context key in `internal/runners/runctx.go` (extending the existing iota block at line 7):

```go
const (
    contextKeySpawnDepth contextKey = iota
    contextKeyRunner
    contextKeyOrigin  // NEW
)

func ContextWithOrigin(ctx context.Context, origin string) context.Context {
    return context.WithValue(ctx, contextKeyOrigin, origin)
}

func OriginFromContext(ctx context.Context) string {
    value, _ := ctx.Value(contextKeyOrigin).(string)
    return value
}
```

Set it in `coordinator.go` where the run context is built inside `processQueue` (the non-compact branch at line 485). The current context chain is:

```go
ctx, cancel = context.WithCancel(ContextWithCoordinator(pubsub.ContextWithPubSub(
    models.ContextWithUserSessionToken(
        self.ctx,
        models.UserFromContext(message.ctx),
        models.SessionFromContext(message.ctx),
        models.TokenFromContext(message.ctx),
    ), self.pubsub), self))
```

Add the origin to this chain:

```go
ctx, cancel = context.WithCancel(
    runners.ContextWithOrigin(
        ContextWithCoordinator(pubsub.ContextWithPubSub(
            models.ContextWithUserSessionToken(
                self.ctx,
                models.UserFromContext(message.ctx),
                models.SessionFromContext(message.ctx),
                models.TokenFromContext(message.ctx),
            ), self.pubsub), self),
        message.parameters.Origin,
    ))
```

The origin already lives on `message.parameters` (of type `runners.RunParameters`) — no change to the `queuedMessage` struct is needed.

### Gating Logic in Execute

```go
func (self *askUserQuestionTool) Execute(ctx context.Context, rawArguments string) (string, error) {
    origin := runners.OriginFromContext(ctx)
    if origin != "webui" {
        channel := origin
        if channel == "" {
            channel = "automated"
        }
        result, _ := json.Marshal(map[string]string{
            "error": fmt.Sprintf("ask_user_question is not supported on the %s channel", channel),
        })
        return string(result), nil
    }
    // ... proceed with question
}
```

---

## 3. Blocking & Answer Delivery Lifecycle

### Core Design

The tool `Execute()` method blocks until the user clicks a choice. The runner executes tools in parallel goroutines (`waitGroup.Wait()`), so blocking one tool goroutine is fine — other parallel tool calls continue independently.

### Mechanism: Per-Question Channel

When `ask_user_question` executes:

1. Generate a unique question ID (ULID).
2. Register a pending question in the **QuestionBroker** (in-memory only).
3. Broadcast a question event via pubsub so the frontend can display it.
4. Block on a channel waiting for the answer or context cancellation.
5. On answer receipt, return the answer as the tool result.
6. On context cancellation (abort), clean up and return an error.

### QuestionBroker

**Package:** `internal/tools/askuser/broker.go`

The broker is an in-memory registry that holds a channel per pending question. It exists solely to route answers from the WebSocket RPC layer to the blocked `Execute()` goroutine.

```go
type PendingQuestion struct {
    ID             string   `json:"id"`
    ConversationID string   `json:"conversationId"`
    AgentID        string   `json:"agentId"`
    UserID         string   `json:"userId"`
    RunID          string   `json:"runId"`
    ToolCallID     string   `json:"toolCallId"`
    Question       string   `json:"question"`
    Choices        []string `json:"choices"`
    answerChan     chan string  // unexported, in-memory only
}

type QuestionBroker struct {
    mu       sync.Mutex
    pending  map[string]*PendingQuestion  // questionId -> question
}

func (b *QuestionBroker) Register(q *PendingQuestion) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.pending[q.ID] = q
}

func (b *QuestionBroker) Answer(questionId, answer string, other ...string) error {
    b.mu.Lock()
    q, ok := b.pending[questionId]
    if ok {
        delete(b.pending, questionId)
    }
    b.mu.Unlock()
    if !ok {
        return fmt.Errorf("question not found or already answered: %s", questionId)
    }
    // Encode the answer + optional freeform text as JSON for the tool result.
    result := map[string]string{"answer": answer}
    if len(other) > 0 && other[0] != "" {
        result["other"] = other[0]
    }
    encoded, _ := json.Marshal(result)
    q.answerChan <- string(encoded)
    return nil
}

func (b *QuestionBroker) Cancel(questionId string) {
    b.mu.Lock()
    q, ok := b.pending[questionId]
    if ok {
        delete(b.pending, questionId)
    }
    b.mu.Unlock()
    if ok {
        close(q.answerChan)
    }
}

func (b *QuestionBroker) PendingForConversation(conversationId string) []*PendingQuestion {
    b.mu.Lock()
    defer b.mu.Unlock()
    var result []*PendingQuestion
    for _, q := range b.pending {
        if q.ConversationID == conversationId {
            result = append(result, q)
        }
    }
    return result
}
```

**Singleton:** Attach to the `Coordinator`, since it already owns the pubsub and runner lifecycle.

```go
// internal/coordinators/coordinator.go
type Coordinator struct {
    // ... existing fields ...
    questionBroker *askuser.QuestionBroker
}

func (c *Coordinator) QuestionBroker() *askuser.QuestionBroker {
    return c.questionBroker
}
```

Expose via context so tools can access it. Use a struct-based context key, matching the pattern in `internal/pubsub/context.go`:

```go
// internal/tools/askuser/context.go
type contextKeyQuestionBroker struct{}

func ContextWithQuestionBroker(ctx context.Context, broker *QuestionBroker) context.Context {
    return context.WithValue(ctx, contextKeyQuestionBroker{}, broker)
}

func QuestionBrokerFromContext(ctx context.Context) *QuestionBroker {
    value, _ := ctx.Value(contextKeyQuestionBroker{}).(*QuestionBroker)
    return value
}
```

Set on the run context in `coordinator.go` (inside the `processQueue` context chain at line 485):

```go
ctx, cancel = context.WithCancel(
    askuser.ContextWithQuestionBroker(
        runners.ContextWithOrigin(
            ContextWithCoordinator(pubsub.ContextWithPubSub(
                models.ContextWithUserSessionToken(
                    self.ctx,
                    models.UserFromContext(message.ctx),
                    models.SessionFromContext(message.ctx),
                    models.TokenFromContext(message.ctx),
                ), self.pubsub), self),
            message.parameters.Origin,
        ),
        self.questionBroker,
    ))
```

### Execute Flow

The tool resolves metadata from context following the same patterns as the conversation todo tool (`internal/tools/conversations/todo.go:115-128`):

```go
func (self *askUserQuestionTool) Execute(ctx context.Context, rawArguments string) (string, error) {
    // 1. Channel gate
    origin := runners.OriginFromContext(ctx)
    if origin != "webui" {
        // return error result (see §2)
    }

    // 2. Parse arguments
    var args struct {
        Question string   `json:"question"`
        Choices  []string `json:"choices"`
    }
    if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
        return "", fmt.Errorf("parsing arguments: %w", err)
    }
    if args.Question == "" || len(args.Choices) < 2 {
        return "", fmt.Errorf("question and at least 2 choices are required")
    }

    // 3. Get broker and runner metadata from context
    //    (same pattern as conversation todo tool)
    broker := QuestionBrokerFromContext(ctx)
    if broker == nil {
        return "", fmt.Errorf("question broker not available")
    }
    runner := runners.RunnerFromContext(ctx)
    if runner == nil {
        return "", fmt.Errorf("runner context not available")
    }
    user := models.UserFromContext(ctx)
    if user == nil {
        return "", fmt.Errorf("authentication required")
    }

    // 4. Register pending question (in-memory only)
    pending := &PendingQuestion{
        ID:             security.NewULID(),
        ConversationID: runner.ConversationID,
        AgentID:        runner.AgentID,
        UserID:         user.ID,
        RunID:          runner.ID,
        ToolCallID:     /* from tool call context */,
        Question:       args.Question,
        Choices:        args.Choices,
        answerChan:     make(chan string, 1),
    }
    broker.Register(pending)

    // 5. Broadcast question event via pubsub from context
    //    (same pattern as emitTodoEvent in conversations/todo.go:410-424)
    ps := pubsub.PubSubFromContext(ctx)
    if ps != nil {
        ps.Broadcast(pubsub.EventTypeConversationQuestions, map[string]interface{}{
            "action":         "asked",
            "conversationId": pending.ConversationID,
            "agentId":        pending.AgentID,
            "userId":         pending.UserID,
            "runId":          pending.RunID,
            "questionId":     pending.ID,
            "question":       pending.Question,
            "choices":        pending.Choices,
        })
    }

    // 6. Block until answer or cancellation.
    //    The answer channel receives a pre-encoded JSON string from broker.Answer()
    //    (e.g. `{"answer":"PostgreSQL"}` or `{"answer":"Other","other":"CockroachDB"}`).
    select {
    case encodedResult, ok := <-pending.answerChan:
        if !ok {
            return "", fmt.Errorf("question cancelled")
        }
        return encodedResult, nil
    case <-ctx.Done():
        broker.Cancel(pending.ID)
        return "", ctx.Err()
    }
}
```

---

## 4. RPC Endpoints

### New File: `internal/api/v1api/rpc_questions.go`

Following the established pattern in `rpc_todos.go`, question RPC handlers live in a dedicated file. All handlers receive a `requestFrame` (defined in `frames.go:10-15`), unmarshal `frame.Params`, and respond via `self.sendResponse(frame.ID, ...)` or `self.sendError(frame.ID, ...)`.

### Dispatch Registration

Add cases to the `dispatch` method in `internal/api/v1api/websocket.go` (after the `conversations.todos.*` block at line 348):

```go
case "questions.list":
    self.handleQuestionsList(frame)
case "questions.answer":
    self.handleQuestionsAnswer(frame)
case "questions.answer_batch":
    self.handleQuestionsAnswerBatch(frame)
```

### `questions.answer` Handler

Answers a single question. Retained for programmatic use and cross-tab "answered" broadcasts, but the primary frontend submit path uses `questions.answer_batch` (see below).

```go
func (self *webSocketConnection) handleQuestionsAnswer(frame requestFrame) {
    var parameters struct {
        QuestionID string `json:"questionId"`
        Answer     string `json:"answer"`
        Other      string `json:"other,omitempty"`
    }
    if frame.Params != nil {
        json.Unmarshal(frame.Params, &parameters)
    }
    if parameters.QuestionID == "" || parameters.Answer == "" {
        self.sendError(frame.ID, 400, "questionId and answer are required")
        return
    }

    // Security: verify the caller owns this question (see §7)
    broker := self.api.coordinator.QuestionBroker()
    if err := broker.VerifyOwnership(parameters.QuestionID, self.userId()); err != nil {
        self.sendError(frame.ID, 403, err.Error())
        return
    }

    if err := broker.Answer(parameters.QuestionID, parameters.Answer, parameters.Other); err != nil {
        self.sendError(frame.ID, 404, err.Error())
        return
    }

    // Broadcast "answered" event so other tabs dismiss the question
    self.api.pubsub.Broadcast(pubsub.EventTypeConversationQuestions, map[string]interface{}{
        "action":     "answered",
        "userId":     self.userId(),
        "questionId": parameters.QuestionID,
        "answer":     parameters.Answer,
    })

    self.sendResponse(frame.ID, map[string]interface{}{"ok": true})
}
```

### `questions.answer_batch` Handler

Accepts an array of answers and delivers them atomically. The frontend collects per-question selections locally and submits them all in a single RPC call when the user clicks "Submit All". This avoids partial-answer states where some tool goroutines unblock before the user has finished answering all questions.

```go
func (self *webSocketConnection) handleQuestionsAnswerBatch(frame requestFrame) {
    var parameters struct {
        Answers []struct {
            QuestionID string `json:"questionId"`
            Answer     string `json:"answer"`
            Other      string `json:"other,omitempty"`
        } `json:"answers"`
    }
    if frame.Params != nil {
        json.Unmarshal(frame.Params, &parameters)
    }
    if len(parameters.Answers) == 0 {
        self.sendError(frame.ID, 400, "answers array is required and must be non-empty")
        return
    }

    broker := self.api.coordinator.QuestionBroker()

    // Phase 1: Validate all answers before delivering any.
    // This ensures we don't partially answer if one fails ownership/existence check.
    for _, a := range parameters.Answers {
        if a.QuestionID == "" || a.Answer == "" {
            self.sendError(frame.ID, 400, "each answer must have questionId and answer")
            return
        }
        if err := broker.VerifyOwnership(a.QuestionID, self.userId()); err != nil {
            self.sendError(frame.ID, 403, err.Error())
            return
        }
    }

    // Phase 2: Deliver all answers.
    var delivered []string
    for _, a := range parameters.Answers {
        if err := broker.Answer(a.QuestionID, a.Answer, a.Other); err != nil {
            // Question already answered (race with another tab) — skip, not fatal.
            continue
        }
        delivered = append(delivered, a.QuestionID)
    }

    // Phase 3: Broadcast "answered" for each delivered answer so other tabs update.
    for _, a := range parameters.Answers {
        self.api.pubsub.Broadcast(pubsub.EventTypeConversationQuestions, map[string]interface{}{
            "action":     "answered",
            "userId":     self.userId(),
            "questionId": a.QuestionID,
            "answer":     a.Answer,
        })
    }

    self.sendResponse(frame.ID, map[string]interface{}{
        "ok":        true,
        "delivered": delivered,
    })
}
```

**Why batch instead of sequential single calls?** If the frontend sent N sequential `questions.answer` RPCs, the first answer would unblock its runner goroutine immediately. If that goroutine's tool result causes the LLM to emit new streaming content while other questions are still being submitted, the UX becomes confusing (new assistant text appearing mid-answer-flow). The batch endpoint delivers all answers within a single broker lock cycle, so all blocked goroutines unblock together after the RPC returns.

**Broker change for batch:** `broker.Answer()` signature adds an optional `other` parameter (see §3 for the existing `Other` field on `PendingQuestion`). The answer string delivered to the tool's channel is the selected choice label; the `other` freeform text (if any) is appended as a secondary field in the tool result JSON.

### `questions.list` Handler

Returns pending questions for a conversation. Used by the frontend on reconnect to recover pending question UI.

```go
func (self *webSocketConnection) handleQuestionsList(frame requestFrame) {
    var parameters struct {
        ConversationID string `json:"conversationId"`
    }
    if frame.Params != nil {
        json.Unmarshal(frame.Params, &parameters)
    }
    if parameters.ConversationID == "" {
        self.sendError(frame.ID, 400, "conversationId is required")
        return
    }

    if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
        self.sendError(frame.ID, 403, err.Error())
        return
    }

    broker := self.api.coordinator.QuestionBroker()
    pending := broker.PendingForConversation(parameters.ConversationID)

    // Filter to only questions belonging to this user
    var result []*askuser.PendingQuestion
    for _, q := range pending {
        if q.UserID == self.userId() {
            result = append(result, q)
        }
    }
    if result == nil {
        result = make([]*askuser.PendingQuestion, 0)
    }

    self.sendResponse(frame.ID, map[string]interface{}{"questions": result})
}
```

---

## 5. PubSub Events

### New Event Type: `EventTypeConversationQuestions`

Add a new constant in `internal/pubsub/pubsub.go` (alongside the existing types like `EventTypeConversationTodos`):

```go
EventTypeConversationQuestions EventType = "conversation_questions"
```

This follows the same pattern as `EventTypeConversationTodos` — a dedicated event type for question lifecycle, separate from the core conversation run states (`EventTypeConversation`).

### Event: Question Asked

Broadcast from the tool's `Execute()` via `pubsub.PubSubFromContext(ctx).Broadcast(...)`:

```json
{
  "action": "asked",
  "conversationId": "...",
  "agentId": "...",
  "userId": "...",
  "runId": "...",
  "questionId": "01JXYZ...",
  "question": "Which database should we use?",
  "choices": ["PostgreSQL", "SQLite", "MySQL"]
}
```

### Event: Question Answered

Broadcast from the `questions.answer` RPC handler via `self.api.pubsub.Broadcast(...)`:

```json
{
  "action": "answered",
  "userId": "...",
  "questionId": "01JXYZ...",
  "answer": "PostgreSQL"
}
```

Both events include `userId` in the payload, so the existing `shouldDeliverEvent()` filter on `webSocketConnection` (`websocket.go:125-139`) ensures only the owning user's connections receive them.

---

## 6. Frontend Changes

### 6.1 Types

**File:** `web/src/types.ts`

```typescript
export interface PendingQuestion {
  id: string;
  conversationId: string;
  agentId: string;
  runId: string;
  question: string;
  choices: string[];
  allowOther?: boolean;
  otherLabel?: string;
  otherPlaceholder?: string;
}

/** Local draft answer for a single question, held in client state until batch submit. */
export interface QuestionDraft {
  /** The selected choice label, or the otherLabel if "Other" is selected. */
  selectedChoice: string | null;
  /** If the user selected "Other", their freeform text. Preserved even if they switch
   *  back to a predefined choice, so switching to Other again restores it. */
  otherText: string;
  /** True when the "Other" text input is the active selection (vs a predefined choice). */
  isOther: boolean;
}
```

### 6.2 State Management

**File:** `web/src/hooks/useBackend.ts`

#### State

```typescript
const [pendingQuestions, setPendingQuestions] = useState<PendingQuestion[]>([]);
const [questionDrafts, setQuestionDrafts] = useState<Record<string, QuestionDraft>>({});
```

`pendingQuestions` holds the questions received from the backend. `questionDrafts` is a `questionId → QuestionDraft` map holding the user's local selections before submission. Drafts are client-only state — never sent to the backend until the user clicks Submit All.

#### Derived state

```typescript
/** True when every pending question has a valid local answer. */
const allQuestionsAnswered = useMemo(() => {
  if (pendingQuestions.length === 0) return false;
  return pendingQuestions.every((q) => {
    const d = questionDrafts[q.id];
    if (!d) return false;
    if (d.isOther) return d.otherText.trim().length > 0;
    return d.selectedChoice !== null;
  });
}, [pendingQuestions, questionDrafts]);
```

#### Event handler

Handle the `conversation_questions` event type in the WebSocket event handler (alongside the existing `conversation_todos` handler):

```typescript
} else if (event === "conversation_questions") {
  const payload = parsed.payload as ConversationQuestionsEvent;
  if (payload.action === "asked") {
    setPendingQuestions((prev) => {
      if (prev.some((q) => q.id === payload.questionId)) return prev; // dedup
      return [
        ...prev,
        {
          id: payload.questionId,
          conversationId: payload.conversationId!,
          agentId: payload.agentId!,
          runId: payload.runId!,
          question: payload.question!,
          choices: payload.choices!,
          allowOther: payload.allowOther,
          otherLabel: payload.otherLabel,
          otherPlaceholder: payload.otherPlaceholder,
        },
      ];
    });
    setStatus("waiting for your answer...");
  } else if (payload.action === "answered") {
    setPendingQuestions((prev) => prev.filter((q) => q.id !== payload.questionId));
    setQuestionDrafts((prev) => {
      const next = { ...prev };
      delete next[payload.questionId];
      return next;
    });
  }
}
```

#### Draft manipulation helpers

```typescript
const setQuestionDraft = useCallback(
  (questionId: string, update: Partial<QuestionDraft>) => {
    setQuestionDrafts((prev) => ({
      ...prev,
      [questionId]: {
        selectedChoice: null,
        otherText: "",
        isOther: false,
        ...prev[questionId],
        ...update,
      },
    }));
  },
  [],
);
```

#### Batch submit

```typescript
const submitAllAnswers = useCallback(async () => {
  const answers = pendingQuestions.map((q) => {
    const d = questionDrafts[q.id];
    return {
      questionId: q.id,
      answer: d.isOther ? (q.otherLabel ?? "Other") : d.selectedChoice!,
      ...(d.isOther && d.otherText.trim() ? { other: d.otherText.trim() } : {}),
    };
  });

  await sendRpc("questions.answer_batch", { answers });

  // Optimistic clear — the "answered" broadcast also removes them, but clearing
  // immediately avoids a flash of stale UI.
  setPendingQuestions([]);
  setQuestionDrafts({});
}, [pendingQuestions, questionDrafts, sendRpc]);
```

#### Exposed from hook

Add to the returned object:

```typescript
return {
  // ... existing fields ...
  pendingQuestions,
  questionDrafts,
  allQuestionsAnswered,
  setQuestionDraft,
  submitAllAnswers,
};
```

### 6.3 Recovery on Page Refresh / Reconnect

When the WebSocket reconnects (after a page refresh or network interruption), the frontend must recover any pending questions that were displayed before the disconnect. The recovery flow runs as a standalone effect:

```typescript
useEffect(() => {
  if (!connected || !conversationId) return;
  sendRpc<{ questions: PendingQuestion[] }>("questions.list", {
    conversationId,
  }).then((result) => {
    setPendingQuestions(result?.questions ?? []);
    // Clear stale drafts — user must re-select after refresh.
    setQuestionDrafts({});
  });
}, [connected, conversationId]);
```

**Why this works:** The `questions.list` RPC reads from the in-memory `QuestionBroker`. As long as the server hasn't restarted, the broker still holds the pending questions and the runner goroutine is still blocked waiting for answers. The WebSocket disconnect does NOT cancel the run — the run context is derived from `Coordinator.ctx`, not the WebSocket's context (see `coordinator.go:485` where `self.ctx` is used as the base, not `message.ctx`). The question remains pending across frontend refreshes.

**What the user sees:** After refreshing, the page loads, the WebSocket reconnects, conversation history loads (showing the assistant message with tool calls but no results for `ask_user_question`), and the `questions.list` response re-populates the `pendingQuestions` state. The InputArea is replaced by the QuestionPanel. From the user's perspective the question reappears within milliseconds of the page loading.

**Draft state is lost on refresh.** `questionDrafts` is not persisted to localStorage — this is intentional. After a page refresh the user sees the questions again with no pre-selected answers and must re-select. This avoids stale draft state and keeps the implementation simple.

**Edge case — switching conversations:** When the user navigates to a different conversation and back, the same `questions.list` call runs on conversation load. Questions for other conversations are not shown.

**Edge case — server restarted:** If the server restarted while the question was pending, the broker is empty and `questions.list` returns `[]`. The question does not reappear. See §9.1 (Limitations) for full discussion and expected UX.

### 6.4 Question UI: InputArea Replacement

When `pendingQuestions.length > 0`, the conversation route **replaces** the InputArea component with a `QuestionPanel` component in the same position (bottom of the conversation layout). This ensures the user answers where they would normally type — no modals, no inline bubbles in the message list.

**File:** `web/src/components/QuestionPanel.tsx`

#### Visual layout

```
┌─ QuestionPanel ──────────────────────────────────────────────────────┐
│                                                                      │
│  ┌─ QuestionCard (currently visible) ──────────────────────────────┐ │
│  │                                                                  │ │
│  │  Which database should we use?                  (1 / 3)         │ │
│  │                                                                  │ │
│  │  ┌──────────────┐  ┌──────────┐  ┌──────────┐                  │ │
│  │  │ PostgreSQL ● │  │  SQLite  │  │  MySQL   │                  │ │
│  │  └──────────────┘  └──────────┘  └──────────┘                  │ │
│  │  ┌──────────────────────────────────────────┐                  │ │
│  │  │  Other: [________________________]       │                  │ │
│  │  └──────────────────────────────────────────┘                  │ │
│  │                                                                  │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│  ┌─ Navigation + Submit ───────────────────────────────────────────┐ │
│  │  [← Prev]    ● ○ ○    [Next →]           [Submit All ✓]        │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

#### Component hierarchy

```
QuestionPanel
├── QuestionCard (one visible at a time, swipeable)
│   ├── Question text + page indicator ("1 / 3")
│   ├── Choice buttons (radio-style: outlined, filled on select)
│   └── Other input row (if allowOther)
└── Navigation bar
    ├── Prev / Next buttons
    ├── Dot indicators (one per question, filled = answered)
    └── Submit All button (disabled until allQuestionsAnswered)
```

#### Props

```typescript
interface QuestionPanelProps {
  questions: PendingQuestion[];
  drafts: Record<string, QuestionDraft>;
  allAnswered: boolean;
  onDraftChange: (questionId: string, update: Partial<QuestionDraft>) => void;
  onSubmitAll: () => void;
}
```

#### Swipe / navigation

- **Touch:** Horizontal swipe on the `QuestionCard` area navigates between questions. Use CSS `scroll-snap-type: x mandatory` on a horizontal scroll container, or a lightweight swipe hook (e.g. pointer events with velocity threshold). No heavy carousel library.
- **Non-touch:** `← Prev` and `→ Next` buttons flanking the dot indicators. Keyboard left/right arrow keys also navigate when the panel has focus.
- **Current index:** Local state `const [currentIndex, setCurrentIndex] = useState(0)`. Clamped to `[0, questions.length - 1]`.
- **Single question:** When `questions.length === 1`, hide the navigation bar entirely (no prev/next, no dots). The Submit All button moves into the card itself.

#### QuestionCard sub-component

```typescript
interface QuestionCardProps {
  question: PendingQuestion;
  draft: QuestionDraft;
  pageLabel: string;  // e.g. "2 / 3", hidden when single question
  onDraftChange: (update: Partial<QuestionDraft>) => void;
}
```

Renders:

1. **Question text** — styled as a heading, with the page label right-aligned.
2. **Choice buttons** — flex-wrap row of outlined `Button` components. The currently selected choice is visually filled (primary color). Clicking a choice sets `draft.selectedChoice = choiceLabel` and `draft.isOther = false`. Selecting a predefined choice does **not** clear `draft.otherText` — it remains preserved in case the user switches back.
3. **"Other" row** (if `question.allowOther`) — rendered as an additional choice in the same button row, but when selected, expands inline to show a text input:

```
┌────────────────────────────────────────────────────────────────┐
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│  │  Choice1  │  │  Choice2  │  │  Choice3  │                    │
│  └──────────┘  └──────────┘  └──────────┘                    │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │ Other: [previously typed text still here_______________] │ │
│  └──────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

**Other choice behavior:**
- The "Other" button sits in the choices row like any other choice.
- Clicking "Other" sets `draft.isOther = true` and focuses the text input.
- The text input shows `draft.otherText` — if the user previously typed something and then switched to a predefined choice, the text is still there when they switch back.
- Typing in the Other field updates `draft.otherText` via `onDraftChange`.
- Clicking a predefined choice after typing in Other sets `draft.isOther = false` and `draft.selectedChoice = choiceLabel` but **does not clear `draft.otherText`**. This preserves the user's freeform text.
- `draft.otherText` is only cleared if the user manually deletes the text in the input field.
- The Other input auto-focuses when the Other choice is selected.
- `Enter` key in the Other input navigates to the next unanswered question (or focuses Submit if all answered), rather than submitting — the single Submit All button is the only submission path.

#### Dot indicators

Each dot corresponds to a question. Dots are styled:
- **Hollow circle (○):** No answer selected yet for this question.
- **Filled circle (●):** Answer selected (either a predefined choice or Other with non-empty text).
- **Current dot** has a subtle ring/border to indicate which card is visible.

Clicking a dot navigates to that question.

#### Submit All button

- Labeled "Submit" (single question) or "Submit All" (multiple questions).
- Disabled (greyed out) until `allQuestionsAnswered` is true.
- On click, calls `onSubmitAll()` which triggers `submitAllAnswers()` from useBackend.
- Shows a brief loading spinner during the RPC call. On completion, the entire QuestionPanel unmounts (pendingQuestions becomes empty) and the InputArea reappears.
- Keyboard shortcut: `Ctrl+Enter` / `Cmd+Enter` submits when enabled, matching the send shortcut in InputArea.

### 6.5 Placement: InputArea ↔ QuestionPanel Swap

**File:** `web/src/routes/conversations/$agentId/$conversationId.tsx`

The conversation route conditionally renders either InputArea or QuestionPanel in the same layout slot:

```tsx
{backend.pendingQuestions.length > 0 ? (
  <QuestionPanel
    questions={backend.pendingQuestions}
    drafts={backend.questionDrafts}
    allAnswered={backend.allQuestionsAnswered}
    onDraftChange={backend.setQuestionDraft}
    onSubmitAll={backend.submitAllAnswers}
  />
) : voiceCall.isCallActive ? (
  <VoiceCallBar ... />
) : (
  <InputArea
    isRunning={backend.isRunning}
    connected={backend.connected && !backend.connecting}
    ...
  />
)}
```

**Layout contract:** QuestionPanel wraps itself in the same `<Container maxWidth="md">` that InputArea uses, so it occupies the identical footprint at the bottom of the conversation view. The message list above it remains scrollable and unaffected.

**Transition:** No animation needed — the swap is instant. When `pendingQuestions` goes from `[]` to non-empty, QuestionPanel mounts. When the user submits and `pendingQuestions` returns to `[]`, InputArea remounts. The InputArea's draft text is preserved in localStorage (`draft:${conversationId}`), so any in-progress user text reappears when InputArea returns.

**Scroll behavior:** When QuestionPanel appears, the message list should scroll to the bottom so the user sees the most recent assistant message (the one that triggered the questions). This is already handled by the existing scroll-to-bottom-on-new-content logic.

### 6.6 Answered Questions in History

When the conversation history is loaded (`conversations.history`), past `ask_user_question` tool calls appear as normal tool-invoke + tool-result messages. The existing `ToolInvoke` and `ToolResult` components already render these.

To make past decisions more user-friendly, add special rendering in `ToolResult.tsx` when `toolName === "ask_user_question"`:

```typescript
if (toolName === "ask_user_question") {
  const parsed = JSON.parse(content);
  // Render: "Answered: {parsed.answer}" with a checkmark icon
  // If parsed.other exists, show it as secondary text below
}
```

And in `ToolInvoke.tsx`, parse the arguments to show the original question + choices in a readable format instead of raw JSON.

---

## 7. Security: Authorization Model

### Who Can Answer a Question

Only the user who owns the conversation can answer questions in that conversation. This is enforced at multiple levels:

**Level 1 — PubSub event delivery:** The question event includes `userId` in the payload. The existing `shouldDeliverEvent()` filter on `webSocketConnection` (`websocket.go:125-139`) only delivers events to connections authenticated as that user. Other users never see the question.

**Level 2 — RPC authorization:** The `questions.answer` handler verifies ownership before delivering the answer:

```go
func (b *QuestionBroker) VerifyOwnership(questionId, callerUserId string) error {
    b.mu.Lock()
    q, ok := b.pending[questionId]
    b.mu.Unlock()
    if !ok {
        return fmt.Errorf("question not found: %s", questionId)
    }
    if q.UserID != callerUserId {
        return fmt.Errorf("not authorized to answer this question")
    }
    return nil
}
```

**Level 3 — `questions.list` access control:** The handler calls `self.verifyConversationAccess(conversationId)` (reusing the helper from `rpc_todos.go:291-308`) and then filters results to `q.UserID == self.userId()`. A user cannot enumerate another user's pending questions.

### Idempotency

- **Broker `Answer()`** atomically removes the question from the map and sends on the channel. A second call with the same question ID returns `"question not found or already answered"`.
- **Frontend** disables choice buttons immediately after click (optimistic) to prevent double-submission.
- **WebSocket RPC deduplication** — the existing 15-minute `method+id`-based deduplication on `webSocketConnection` (`websocket.go:190-196`) prevents replayed frames from being processed twice.
- **Choice validation** — the `Answer()` handler accepts any string, not just choices from the original list. This is intentional: the LLM receives whatever the user selected, and the frontend is the enforcement point for valid choices. A tampered answer is no different from a tool result with unexpected content — the LLM handles it.

### API Token Access

API tokens (used for the OpenAI-compatible `/chat/completions` endpoint) do not have WebSocket access, so they cannot call `questions.answer` or `questions.list`. This is enforced by the existing auth middleware which separates REST and WebSocket auth paths. If a run is triggered via API token and the LLM calls `ask_user_question`, the channel gate (§2) rejects it since the origin would not be `"webui"`.

### Multi-Tab Behavior

Multiple browser tabs for the same user all receive the `"asked"` event. If the user answers in one tab, the `"answered"` broadcast causes the other tabs to dismiss the question card. The `questions.answer` RPC is idempotent-on-failure — the second tab's answer attempt returns an error which the frontend handles gracefully (the question was already removed by the broadcast).

---

## 8. Concurrency & Ordering Semantics

### Multiple Parallel Questions

If the LLM calls `ask_user_question` multiple times in the same tool-call batch (e.g. alongside other tools), each call:
- Gets its own question ID and blocking channel
- Registers independently in the broker
- Broadcasts independently to the frontend
- Can be answered in any order

The runner's `waitGroup.Wait()` unblocks only when ALL tool calls complete, so the last question answered determines when the round finishes.

### Ordering Guarantees

- Questions appear on the frontend in broadcast order (arrival order).
- Answers can arrive in any order — each resolves its own goroutine independently.
- The tool result persist phase (Phase 3 in runner) processes results in the original tool-call order regardless of answer order.

### Abort Behavior

When `conversations.abort` is called:
- The run context is cancelled (`ctx.Done()` fires).
- Each blocked `Execute()` hits the `case <-ctx.Done()` branch.
- The broker's `Cancel()` is called to clean up the in-memory entry.
- The question disappears from the frontend via the existing `"aborted"` event handling.

### Race: Answer vs. Abort

The `select` in `Execute` handles this cleanly. If both the answer channel and `ctx.Done()` are ready, Go picks one non-deterministically, but either outcome is acceptable:
- If answer wins: tool returns the answer, but the run may be cancelled on the next iteration.
- If abort wins: tool returns error, answer is discarded.

---

## 9. Limitations

This design deliberately avoids extra persistence and runner changes. This keeps the implementation simple but introduces known limitations.

### 9.1 Gateway Restart Loses Pending Questions

**What happens:** When the server process restarts, all runner goroutines are killed (the `Coordinator.ctx` is cancelled, propagating to all run contexts). The in-memory `QuestionBroker` is destroyed. There is no persistent record of the pending question.

**Conversation state after restart:**
- The assistant message with `toolCalls` (including `ask_user_question`) was already written to the store in Phase 0 of the runner.
- The tool result message was **never written** — the goroutine was killed before Phase 3.
- This leaves a "dangling" tool call in the conversation history.

**What the user sees:**
1. All WebSocket connections drop. The frontend reconnects automatically.
2. `conversations.history` returns the conversation with the dangling assistant tool call but no tool result.
3. `questions.list` returns `{"questions": []}` (broker is empty, fresh process).
4. The pending question UI does **not** reappear. The run is no longer active (`activeRunId` is absent in the history response), so the UI does not show a "thinking" state.
5. The user sees the conversation end abruptly at the assistant's tool call.

**Recovery path:** When the user sends a new message, the runner's `fixInterruptedToolCalls()` (`runner.go:673-719`) scans the conversation history, finds the dangling `ask_user_question` tool call with no result, and injects a synthetic tool result: `"Tool call was interrupted and did not complete."` The LLM sees this and can re-ask the question or make a decision on its own.

**Mitigation — not implemented, noted for future:** If restart-resilience is needed, a dedicated store record could be introduced to persist pending questions and enable post-restart recovery (synthetic tool result injection + continuation run). This is explicitly out of scope for the current design to avoid new persistence contracts.

### 9.2 Long-Pending Questions Block the Conversation

While a question is pending, the runner goroutine is parked on a channel select. The `conversationRunner` stays in `processing = true` state:
- New user messages for this conversation are queued by the coordinator.
- The user cannot interact with the conversation until the question is answered or the run is aborted.

**Mitigation:** The user sees "waiting for your answer..." status. If they want to proceed without answering, they can abort the run via the existing abort button. On abort, `fixInterruptedToolCalls()` handles the dangling tool call on the next message.

**Resource cost:** A parked goroutine is ~8KB of stack. Even 1000 concurrent pending questions across all users would consume ~8MB — negligible.

### 9.3 No Persistent Audit Trail of Questions

Pending questions are not stored anywhere beyond the broker's in-memory map. Once answered, the question content exists only as:
- The tool call arguments in the assistant message (question text + choices).
- The tool result message (the selected answer).

There is no queryable "question history" outside the conversation transcript. This is sufficient for the current use case — past questions can be seen by loading conversation history.

---

## 10. Channel Gating Behavior Summary

| Origin       | Behavior                                                  |
|--------------|-----------------------------------------------------------|
| `"webui"`    | Full lifecycle: broadcast question, wait for answer.      |
| `"telegram"` | Immediate tool error result: not supported on telegram.   |
| `"discord"`  | Immediate tool error result: not supported on discord.    |
| `""`         | Immediate tool error result: not supported on automated.  |

The error is returned as a **successful tool result** (not a Go error), so the LLM sees it as a tool response and can react (e.g. make a decision on its own, explain to the user, etc.).

---

## 11. Files to Create or Modify

### New Files

| File | Purpose |
|------|---------|
| `internal/tools/askuser/askuser.go` | Tool definition, `Execute()`, `init()` registration |
| `internal/tools/askuser/broker.go` | `QuestionBroker` struct and methods |
| `internal/tools/askuser/context.go` | Context helpers for broker (struct-key pattern, cf. `pubsub/context.go`) |
| `internal/api/v1api/rpc_questions.go` | `handleQuestionsList`, `handleQuestionsAnswer`, and `handleQuestionsAnswerBatch` RPC handlers (cf. `rpc_todos.go`) |
| `web/src/components/QuestionPanel.tsx` | React component that replaces InputArea when questions are pending; contains swipeable QuestionCard sub-components, navigation dots, and Submit All button |

### Modified Files

| File | Change |
|------|--------|
| `internal/runners/runctx.go` | Add `contextKeyOrigin` to iota, add `ContextWithOrigin` / `OriginFromContext` |
| `internal/coordinators/coordinator.go` | Add `questionBroker` field + `QuestionBroker()` accessor; set origin + broker on run context in `processQueue` (line 485) |
| `internal/pubsub/pubsub.go` | Add `EventTypeConversationQuestions` constant |
| `internal/api/v1api/websocket.go` | Add `questions.list`, `questions.answer`, and `questions.answer_batch` cases to `dispatch()` (after todos block) |
| `web/src/types.ts` | Add `PendingQuestion` and `QuestionDraft` interfaces |
| `web/src/hooks/useBackend.ts` | Add `pendingQuestions` + `questionDrafts` state, `conversation_questions` event handler, `setQuestionDraft` + `submitAllAnswers` actions, `allQuestionsAnswered` derived flag, reconnect recovery via `questions.list` |
| `web/src/routes/conversations/$agentId/$conversationId.tsx` | Swap InputArea ↔ QuestionPanel based on `pendingQuestions.length > 0` |
| `web/src/components/ToolResult.tsx` | Special rendering for past answered questions |
| `web/src/components/ToolInvoke.tsx` | Special rendering for past question invocations |

### Import Registration

Add blank import in `cmd/gateway.go` (or wherever tools are imported):

```go
_ "github.com/teanode/teanode/internal/tools/askuser"
```

---

## 12. Testing Strategy

### Unit Tests

**`internal/tools/askuser/askuser_test.go`:**

1. **Channel gating:** Call `Execute()` with origin `"telegram"`, `"discord"`, `""` — assert error result JSON, no blocking.
2. **Happy path:** Call `Execute()` in a goroutine with origin `"webui"`. From main goroutine, call `broker.Answer()`. Assert returned result contains correct answer.
3. **Context cancellation:** Call `Execute()` in a goroutine with origin `"webui"`. Cancel the context. Assert `Execute()` returns `ctx.Err()`.
4. **Invalid arguments:** Missing question, empty choices, single choice — assert Go error returned.
5. **Multiple concurrent questions:** Register 3 questions, answer them in reverse order. Assert all goroutines unblock with correct answers.

**`internal/tools/askuser/broker_test.go`:**

1. **Register + Answer:** Register question, answer it, assert channel receives answer.
2. **Answer unknown ID:** Assert error returned.
3. **Double answer:** Answer same ID twice, second returns error.
4. **Cancel:** Register + cancel, assert channel is closed.
5. **PendingForConversation:** Register questions for 2 conversations, assert correct filtering.
6. **VerifyOwnership:** Assert correct user can answer; wrong user is rejected.

### Integration Tests

**`internal/api/v1api/rpc_questions_test.go`:**

1. **RPC `questions.answer`:** Register a question in the broker, send RPC, assert the pending question's channel receives the answer.
2. **RPC `questions.answer` with wrong user:** Assert 403 error response.
3. **RPC `questions.answer_batch` — happy path:** Register 3 questions, send batch answer, assert all 3 channels receive answers.
4. **RPC `questions.answer_batch` — partial ownership failure:** Register 2 questions for user A and 1 for user B. Call batch as user A with all 3. Assert 403 and NO answers delivered (atomic validation).
5. **RPC `questions.answer_batch` — race with single answer:** Register 2 questions. Answer one via `questions.answer`. Then send a batch including both. Assert the already-answered question is skipped (not an error), and the other is delivered.
6. **RPC `questions.list`:** Register questions, call list via RPC, assert correct results filtered by user and conversation.
7. **RPC `questions.list` with conversation access control:** Assert 403 when listing questions for another user's conversation.
8. **RPC answer with invalid ID:** Assert 404 error response.

### Frontend Tests

**`web/src/components/QuestionPanel.test.tsx`:**

1. **Single question:** Renders question text and choices. No navigation dots or prev/next buttons. Submit button labeled "Submit".
2. **Multiple questions:** Renders first question. Shows dot indicators and prev/next buttons. Page label shows "1 / N".
3. **Navigation:** Clicking Next advances to next question. Clicking Prev goes back. Clicking a dot navigates to that question.
4. **Choice selection:** Clicking a choice fills it visually and updates the draft. Clicking a different choice switches the selection.
5. **Other choice:** Clicking Other shows text input. Typing updates `otherText`. Switching to a predefined choice hides the input but preserves `otherText`. Switching back to Other restores the text.
6. **Submit gating:** Submit button disabled when not all questions have answers. Enabled when all have selections. Clicking it calls `onSubmitAll`.
7. **Keyboard:** Ctrl+Enter triggers submit when enabled. Left/Right arrows navigate between questions.

### Manual / E2E Testing

1. Create an agent with `ask_user_question` in its tool list.
2. Prompt the LLM to use the tool. Verify InputArea is replaced by QuestionPanel.
3. Select a choice and click Submit. Verify InputArea reappears and the LLM receives the answer and continues.
4. **Multiple questions:** Trigger 2+ parallel `ask_user_question` calls. Verify swipe/prev/next navigation works. Answer all, submit. Verify all tool goroutines unblock together.
5. **Other choice:** Select Other, type text, switch to a predefined choice, switch back to Other. Verify text is preserved. Submit with Other selected. Verify the tool result includes both the label and freeform text.
6. **Partial answers:** With 3 questions pending, answer 2 of 3. Verify Submit is disabled. Answer the 3rd. Verify Submit is enabled.
7. Test abort while questions are pending. Verify QuestionPanel disappears and InputArea returns.
8. Test on Telegram channel. Verify immediate error tool result.
9. **Reload the page while questions are pending.** Verify QuestionPanel reappears after reconnect via `questions.list`. Verify draft selections are cleared (user must re-select).
10. Open two browser tabs. Answer in one, verify the other tab's QuestionPanel disappears and InputArea returns.
11. **Restart the server while questions are pending.** Reconnect. Verify the QuestionPanel does NOT reappear (InputArea shows instead). Send a new message. Verify `fixInterruptedToolCalls()` fires and the LLM handles it gracefully.
12. **Draft preservation:** Type text in InputArea, then a question arrives (InputArea swaps to QuestionPanel). Answer and submit. Verify InputArea returns with the draft text still present.

---

## 13. State Diagram

```
              ┌──────────────────────────────────────────────────────────┐
              │                    Normal Flow                           │
              │                                                          │
 LLM calls    │  ┌──────────┐   ┌──────────┐   ┌───────┐               │
 tool (×N)    │  │ Register │──►│Broadcast │──►│ Block │  (per tool)   │
 ────────────►│  │ (broker) │   │ (pubsub) │   │(chan)  │               │
              │  └──────────┘   └──────────┘   └───┬───┘               │
              │                                     │                    │
              │  Frontend: InputArea swaps to QuestionPanel.            │
              │  User selects answers locally (draft state).            │
              │  Submit All enabled when all N questions answered.       │
              │                                     │                    │
 User clicks  │  ┌───────────────────────────────┐  │                    │
 Submit All   │  │ questions.answer_batch RPC    │  │                    │
 ────────────►│  │ validates all, then delivers  │◄─┘                    │
              │  │ all answers to broker         │                       │
              │  └──────────────┬────────────────┘                       │
              │                 │                                        │
              │  ┌──────────────▼──────────────┐                        │
              │  │ All N channels receive       │                        │
              │  │ answers simultaneously       │                        │
              │  └──────────────┬──────────────┘                        │
              │                 │                                        │
              │  ┌──────────────▼──────────────┐                        │
              │  │ All tool goroutines unblock  │                        │
              │  │ → runner waitGroup completes │                        │
              │  └──────────────┬──────────────┘                        │
              │                 │                                        │
              │  Frontend: QuestionPanel unmounts, InputArea returns.    │
              └──────────────────────────────────────────────────────────┘

              ┌──────────────────────────────────────────────────────────┐
              │           Page Refresh (server alive)                    │
              │                                                          │
 Client       │  ┌──────────────┐   ┌───────────────────┐              │
 reconnects   │  │questions.list│──►│ Broker returns    │              │
 ────────────►│  │   (RPC)      │   │ pending questions  │              │
              │  └──────────────┘   └────────┬──────────┘              │
              │                              │                          │
              │               ┌──────────────▼──────────────┐          │
              │               │ QuestionPanel mounts,       │          │
              │               │ InputArea stays hidden.     │          │
              │               │ Drafts are cleared (user    │          │
              │               │ must re-select answers).    │          │
              │               └──────────────┬──────────────┘          │
              │                              │                          │
 User answers │               ┌──────────────▼──────────────┐          │
 & submits    │               │ Normal batch answer path    │          │
 ────────────►│               └─────────────────────────────┘          │
              └──────────────────────────────────────────────────────────┘

              ┌──────────────────────────────────────────────────────────┐
              │           Server Restart (lossy)                        │
              │                                                          │
 Server       │  Runner goroutines killed, broker lost.                 │
 restarts     │  Conversation has dangling tool call(s).                │
 ────────────►│                                                          │
              │                                                          │
 Client       │  ┌──────────────┐   ┌───────────────────┐              │
 reconnects   │  │questions.list│──►│ Broker empty: []  │              │
 ────────────►│  └──────────────┘   └───────────────────┘              │
              │                                                          │
              │  QuestionPanel does NOT mount. InputArea shown.         │
              │                                                          │
 User sends   │  ┌─────────────────────────────────────────┐           │
 message      │  │ fixInterruptedToolCalls() injects       │           │
 ────────────►│  │ synthetic error for dangling call(s).   │           │
              │  │ LLM sees error and can re-ask.          │           │
              │  └─────────────────────────────────────────┘           │
              └──────────────────────────────────────────────────────────┘
```

---

## 14. Future Considerations (Out of Scope)

- **Timeout:** Auto-reject after N minutes of no response. Could be a parameter on the tool. Would return a timeout error as the tool result, letting the LLM decide how to proceed.
- **Telegram/Discord support:** Present questions as inline buttons. Would require channel-specific rendering in the bot integration code. The channel gating (§2) is the only thing that needs to change.
- **Persistent pending questions:** Store pending questions in the store (fsstore/dbstore) so they survive server restarts. On restart, `questions.list` falls back to the store, and `questions.answer` writes a synthetic tool result + triggers a continuation run. This would eliminate the limitation in §9.1 but requires new store interfaces and a `ContinueConversation` coordinator method.
- **Pausable runner:** Convert the runner from a synchronous for-loop to a state machine that can yield on `pending_input` and resume. This eliminates the parked goroutine and unblocks the conversation queue while a question is pending. Significant architectural change — only warranted if questions routinely stay pending for long periods.
- **Question metadata storage:** A dedicated query API over past questions, beyond the conversation transcript. Not needed since answered questions are already stored as tool messages.
- **Draft persistence across refreshes:** Currently `questionDrafts` are lost on page refresh. Could persist to localStorage keyed by conversation ID, but the cost of re-selecting answers is low and avoids stale draft bugs.
- **Animated transitions:** The InputArea ↔ QuestionPanel swap is currently instant. A slide-up/fade transition could smooth the UX but adds complexity.
