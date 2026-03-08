# STM Actions Plan: retrieve / summary / filter

## Overview

Add three new top-level actions to the memory tools (`agent_memory`, `user_memory`, `project_memory`): **retrieve**, **summary**, and **filter**. These extend the existing action set (`get`, `list`, `search`, `batch`) without changing any existing behaviour.

| Action     | Operates on          | Core purpose                                       |
|------------|----------------------|---------------------------------------------------|
| `retrieve` | Memory items         | Keyword-ranked snippet retrieval with relevance scoring |
| `summary`  | Conversation messages | Generate a structured summary of the current conversation |
| `filter`   | Conversation messages | Extract messages matching criteria (role, keyword, time range) |

---

## 1. Action: `retrieve`

### Purpose

Return keyword-ranked snippets from memory items, ordered by relevance. Unlike `search` (which returns the first matching lines per item), `retrieve` scores every matched line, deduplicates, and returns the top-N snippets across all items.

### Input Schema

```jsonschema
{
  "action": "retrieve",           // required, literal "retrieve"
  "query": "<string>",            // required, keyword(s) to rank against
  "tags": ["<string>", ...],      // optional, pre-filter by tags
  "maxResults": <integer>,        // optional, default 10, max snippets returned
  "contextLines": <integer>       // optional, default 1, lines of surrounding context per snippet
}
```

For `project_memory`, `projectId` is required as with all other actions.

### Output Schema

```json
{
  "action": "retrieve",
  "snippets": [
    {
      "itemId": "<string>",
      "title": "<string>",
      "snippet": "<string>",
      "score": <float>,
      "tags": ["<string>"]
    }
  ],
  "totalMatches": <integer>
}
```

### Implementation Details

#### Ranking algorithm

1. **Pre-filter** — If `tags` provided, list items filtered by tags (reuse existing `ListMemoryItems` with tag filter). Otherwise list all non-archived items for scope.
2. **Tokenise query** — Split query on whitespace, lowercase. Discard stop-words (<3 chars).
3. **Score each line** in each item's content (and title as a single "line"):
   - For each query token, if the line contains the token (case-insensitive), add `1.0 / totalLinesInItem` (so shorter items are boosted).
   - Title matches get a 2× multiplier.
   - Score is the sum across all tokens.
4. **Collect scored lines** into a flat list of `(itemId, lineIndex, score)`.
5. **Merge context** — For each scored line, expand by `contextLines` above and below. Merge overlapping ranges within the same item.
6. **Sort descending by score**, truncate to `maxResults`.
7. **Build snippets** — For each entry, extract the merged line range as the snippet string.

#### Key difference from `search`

| Aspect         | `search`                     | `retrieve`                       |
|----------------|------------------------------|----------------------------------|
| Granularity    | Per-item                     | Per-snippet (line-level)         |
| Ranking        | None (insertion order)       | Token-frequency score            |
| Context lines  | None                         | Configurable                     |
| Output field   | `matches[].snippet`          | `snippets[].snippet` + `.score`  |

#### Store dependency

Reuses `SearchMemoryItems` only for the initial item fetch (with `IncludeContent: true`). All ranking logic lives in the tool layer, not the store.

---

## 2. Action: `summary`

### Purpose

Produce a structured summary of the current conversation's messages. Optionally persist the summary as a memory item.

### Input Schema

```jsonschema
{
  "action": "summary",             // required, literal "summary"
  "maxMessages": <integer>,        // optional, default 0 (all), limit to last N messages
  "roles": ["user", "assistant"],  // optional, filter by role before summarising
  "persist": {                     // optional, if present the summary is saved as a memory item
    "title": "<string>",           // optional, default "Conversation summary"
    "tags": ["<string>"]           // optional
  }
}
```

For `project_memory`, `projectId` is required.

### Output Schema

```json
{
  "action": "summary",
  "conversationId": "<string>",
  "messageCount": <integer>,
  "summary": {
    "totalMessages": <integer>,
    "byRole": {
      "user": <integer>,
      "assistant": <integer>,
      "system": <integer>,
      "tool": <integer>
    },
    "firstMessageAt": "<ISO8601>",
    "lastMessageAt": "<ISO8601>",
    "topicSegments": [
      {
        "startIndex": <integer>,
        "endIndex": <integer>,
        "topic": "<string>",
        "messageCount": <integer>
      }
    ],
    "keyPoints": ["<string>", ...]
  },
  "persisted": {                  // only present if persist was requested
    "itemId": "<string>"
  }
}
```

### Implementation Details

#### Obtaining conversationId

The `Runner` struct already carries `ConversationID` and is available via `RunnerFromContext(ctx)`. The memory tool's `Execute` method can read it:

```go
runner := runners.RunnerFromContext(ctx)
conversationId := runner.ConversationID
```

**No plumbing changes are needed.** The runner context is already propagated to all tool executions.

#### Message retrieval

Use the store's `ListConversationMessages(ctx, conversationId, opts)` to fetch messages. Apply `roles` filter and `maxMessages` truncation in the tool layer after fetching.

#### Summary generation (deterministic, no LLM call)

1. **Count by role** — Iterate messages, bucket by `Role`.
2. **Time range** — `CreatedAt` of first and last message.
3. **Topic segmentation** — Slide a window of 5 messages. When consecutive user messages share no significant tokens (>4 chars) with the previous window, start a new segment. The segment's `topic` is the most frequent significant token in that segment's user messages.
4. **Key points** — Extract the first sentence of each assistant message that immediately follows a user message (i.e. direct replies). Deduplicate and cap at 10.

#### Persist option

If `persist` is set:
1. Serialise `summary` to a formatted text block.
2. Call the existing `batchAdd` path with scope/scopeId from the current tool instance, using the provided title/tags.
3. Include the resulting `itemId` in the response.
4. Trigger the tool's `afterMutate` callback.

---

## 3. Action: `filter`

### Purpose

Extract and return conversation messages matching caller-specified criteria. Optionally persist the filtered set as a memory item.

### Input Schema

```jsonschema
{
  "action": "filter",               // required, literal "filter"
  "roles": ["user", "assistant"],   // optional, include only these roles
  "keyword": "<string>",            // optional, case-insensitive substring match on content
  "after": "<ISO8601>",             // optional, messages created after this time
  "before": "<ISO8601>",            // optional, messages created before this time
  "maxResults": <integer>,          // optional, default 50
  "persist": {                      // optional, save filtered messages as a memory item
    "title": "<string>",            // optional, default "Filtered messages"
    "tags": ["<string>"]            // optional
  }
}
```

For `project_memory`, `projectId` is required.

### Output Schema

```json
{
  "action": "filter",
  "conversationId": "<string>",
  "messages": [
    {
      "id": "<string>",
      "role": "<string>",
      "content": "<string>",
      "createdAt": "<ISO8601>"
    }
  ],
  "totalMatched": <integer>,
  "persisted": {
    "itemId": "<string>"
  }
}
```

### Implementation Details

#### Obtaining conversationId

Same as `summary` — via `RunnerFromContext(ctx).ConversationID`.

#### Filtering pipeline

1. Fetch all messages via `ListConversationMessages`.
2. Apply filters in order:
   - `roles` — include only messages with matching `Role`.
   - `after` / `before` — compare `CreatedAt`.
   - `keyword` — case-insensitive substring search on the text content of the message. For multimodal content (JSON array), extract text parts and search those.
3. Record `totalMatched` before truncation.
4. Truncate to `maxResults` (take the most recent).

#### Content extraction

`ConversationMessage.Content` is `json.RawMessage`. It may be:
- A plain string (JSON string literal).
- An array of content blocks (`[{"type":"text","text":"..."},{"type":"image",...}]`).

The filter implementation must handle both: attempt JSON string unmarshal first; if that fails, unmarshal as `[]contentBlock` and concatenate text parts.

#### Persist option

Same mechanism as `summary`: serialise the filtered messages into a text block, call `batchAdd`, return `itemId`, trigger `afterMutate`.

---

## 4. File-by-File Change List

### Modified files

| File | Changes |
|------|---------|
| `internal/tools/memory/memory.go` | 1. Add `"retrieve"`, `"summary"`, `"filter"` to the action enum in the tool parameter schema. 2. Add cases in the `Execute` switch for the three new actions. 3. Implement `executeRetrieve()`, `executeSummary()`, `executeFilter()` methods on `memoryTool`. |
| `internal/tools/memory/memory.go` (schema) | Update the `parameters.properties.action.enum` array from `["get","list","search","batch"]` to `["get","list","search","batch","retrieve","summary","filter"]`. Add new optional parameters: `contextLines` (integer), `roles` (array of strings), `keyword` (string), `after` (string), `before` (string), `persist` (object with `title` and `tags`). |

### New files

| File | Purpose |
|------|---------|
| `internal/tools/memory/retrieve.go` | `executeRetrieve()` implementation: tokeniser, line scorer, context merger, snippet builder. |
| `internal/tools/memory/summary.go` | `executeSummary()` implementation: message fetching, role counting, topic segmentation, key point extraction, optional persist. |
| `internal/tools/memory/filter.go` | `executeFilter()` implementation: message fetching, filter pipeline, content extraction, optional persist. |
| `internal/tools/memory/content.go` | Shared helper: `extractTextContent(raw json.RawMessage) string` for parsing conversation message content (handles both string and multimodal array formats). Used by both `summary` and `filter`. |

### No changes required

| File | Reason |
|------|--------|
| `internal/runners/runner.go` | `ConversationID` is already on the `Runner` struct and available via context. |
| `internal/runners/runctx.go` | `RunnerFromContext` already exists. |
| `internal/store/interfaces.go` | `ListConversationMessages` already exists. |
| `internal/store/dbstore/` | No new store queries needed; all new logic is in the tool layer. |
| `internal/store/fsstore/` | Same — no new store operations. |

---

## 5. Test Plan

### Unit tests

| File | Tests |
|------|-------|
| `internal/tools/memory/retrieve_test.go` | **TestRetrieveBasic** — single item, single keyword, verify snippet and score. **TestRetrieveMultiToken** — multi-word query, verify token-frequency scoring. **TestRetrieveTagFilter** — verify tags pre-filter excludes non-matching items. **TestRetrieveContextLines** — verify contextLines=2 expands snippet correctly. **TestRetrieveMaxResults** — verify truncation. **TestRetrieveEmptyQuery** — verify error response. **TestRetrieveTitleBoost** — verify title matches score higher. |
| `internal/tools/memory/summary_test.go` | **TestSummaryBasic** — simple conversation, verify role counts and time range. **TestSummaryRoleFilter** — filter to user-only, verify counts. **TestSummaryMaxMessages** — verify truncation to last N. **TestSummaryTopicSegmentation** — conversation with distinct topics, verify segments. **TestSummaryKeyPoints** — verify key point extraction from assistant replies. **TestSummaryPersist** — verify memory item is created with correct content. **TestSummaryNoConversation** — verify graceful error when conversationId is empty. |
| `internal/tools/memory/filter_test.go` | **TestFilterByRole** — filter user messages only. **TestFilterByKeyword** — case-insensitive keyword match. **TestFilterByTimeRange** — after/before filters. **TestFilterCombined** — role + keyword + time. **TestFilterMaxResults** — verify truncation takes most recent. **TestFilterPersist** — verify persisted memory item. **TestFilterMultimodalContent** — verify text extraction from content block arrays. **TestFilterNoMatches** — empty result set. |
| `internal/tools/memory/content_test.go` | **TestExtractTextString** — plain JSON string content. **TestExtractTextBlocks** — array of content blocks with text + image. **TestExtractTextEmpty** — null/empty content. |

### Integration tests

| Test | Description |
|------|-------------|
| **TestRetrieveEndToEnd** | Create items via batch add, then retrieve with a query. Verify ranked snippets come back correctly. Run against both dbstore and fsstore. |
| **TestSummaryEndToEnd** | Create a conversation with messages via `CreateConversationMessage`, then call summary action. Verify output matches expectations. Test persist flow creates a retrievable memory item. |
| **TestFilterEndToEnd** | Create a conversation with mixed roles and timestamps, call filter with various criteria combinations. Verify correct messages returned. Test persist flow. |

### Edge cases to cover

- `retrieve` with query matching zero items → `{"snippets": [], "totalMatches": 0}`
- `summary` / `filter` on a conversation with zero messages → valid empty response
- `persist` with content exceeding 64KB → error, no partial persist
- `project_memory` actions without `projectId` → existing validation catches this
- `roles` filter with invalid role value → ignore unknown roles, filter on valid ones
