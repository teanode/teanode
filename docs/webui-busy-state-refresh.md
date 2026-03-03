# Web UI Busy-State Lost on Refresh

## Problem

When a tool is running (agent is busy / a run is ongoing) and the user
refreshes the Web UI, the UI no longer shows that the agent is busy.  The
"thinking..." spinner, the stop button, and the tool-activity indicator all
disappear.  The run continues on the backend but the user has no way to see
it or abort it from the UI.

## Repro Steps

1. Open TeaNode web UI.  Send a message that triggers a long-running tool
   (e.g. a web search or code execution).
2. While the tool is executing (spinner visible, "calling \<tool\>..." status),
   press F5 / Cmd-R to hard-refresh the page.
3. Observe: the UI loads the conversation history but shows no busy indicator.
   The input area shows a Send button (not Stop).  Status reads "connected".
4. Meanwhile the run finishes on the backend and a `final` event is broadcast,
   but the UI ignores it (no `currentRunIdRef` is set, so `handleEvent`
   discards the event or it was received before `historyLoadedRef` flipped).

## Current Behaviour: How Busy State Works

### Backend

Active runs are tracked **entirely in-memory** inside the `Coordinator`
(`internal/coordinators/coordinator.go`):

```
activeRunners              sync.Map   // conversationId -> *conversationRunner
activeRunIdConversationIds sync.Map   // runId -> conversationId
activeConversationIdRunIds sync.Map   // conversationId -> runId
```

The run's context is derived from the coordinator's long-lived context (not
the WebSocket request context), so runs survive client disconnects by design.

When `conversations.history` is called, the handler checks for an active run:

```go
// internal/api/v1api/rpc.go:328-332
if self.api.coordinator.GetActiveConversationRunner(parameters.ConversationID) != nil {
    response["running"] = true
    if activeRunId := self.api.coordinator.GetActiveConversationRunID(parameters.ConversationID); activeRunId != "" {
        response["running"] = activeRunId   // JSON key: "running"
    }
}
```

The backend **does** send the active run ID — but under the JSON key
`"running"`, not `"activeRunId"`.

### Frontend

On WebSocket connect (including reconnect after refresh), `handleConnect`
(`web/src/hooks/useBackend.ts:852`) calls `conversations.history` and reads
the response through the `ConversationHistoryResult` TypeScript interface:

```typescript
// web/src/types.ts:87-96
export interface ConversationHistoryResult {
  conversationId: string;
  messages: Message[];
  activeRunId?: string;   // <-- expects JSON key "activeRunId"
  hasMore?: boolean;
  ...
}
```

The reconciliation logic then checks `res.activeRunId`
(`useBackend.ts:939-954`), which drives `isRunning`, `currentRunIdRef`,
`runQueueRef`, the placeholder assistant bubble, and the event replay buffer.

## Root Cause

**JSON field name mismatch.** The backend sends the run ID under the key
`"running"` (`rpc.go:331`), but the frontend TypeScript type declares
`activeRunId` (`types.ts:90`).  At runtime `res.activeRunId` is always
`undefined` because the actual JSON payload contains `"running"`, not
`"activeRunId"`.

As a result:

1. `reconcileRunStateFromHistory` receives `activeRunId = undefined` and
   returns `{ isRunning: false }`.
2. No placeholder assistant bubble is appended.
3. `currentRunIdRef` stays `null`.
4. Subsequent `delta` / `tool_call` / `final` events that arrive after
   history loads are processed but have no matching assistant bubble or run
   context, causing them to be silently dropped or misattributed.

The entire reconnect-rehydration path exists and is correctly implemented on
both sides — the **only** problem is the field name mismatch at the JSON
serialisation boundary.

### Secondary Observation

The backend first sets `response["running"] = true` (a boolean) and then
conditionally overwrites it with the string run ID.  If
`GetActiveConversationRunID` ever returned `""` while `GetActiveConversationRunner`
returned non-nil (a possible transient race during run teardown), the client
would receive `running: true` (boolean) instead of a string run ID.  This
would be truthy but would fail to match any `runId` on event frames.

## Proposed Fix

### Option A: Rename the backend field (minimal, recommended)

Change one line in `internal/api/v1api/rpc.go`:

```go
// Before
response["running"] = true
// ...
response["running"] = activeRunId

// After
response["activeRunId"] = activeRunId
```

Remove the boolean fallback — if a runner is active, `GetActiveConversationRunID`
should always return a non-empty string (it is set atomically with the runner
in `Coordinator.Run`).  If it is ever empty, returning nothing is safer than
returning `true`.

No frontend changes needed — `ConversationHistoryResult.activeRunId` already
expects this key.

### Option B: Rename the frontend field (alternative)

Change the TypeScript interface and all call-sites to read `res.running`
instead of `res.activeRunId`.  This is more invasive (touches `types.ts`,
two paths in `useBackend.ts`, and the test file) for no benefit.

**Recommendation: Option A.**

### Additional Hardening (optional)

1. **Always send `activeRunId` as a string or omit it.** Remove the
   `response["running"] = true` boolean fallback to avoid type ambiguity.

2. **Add a `toolActivity` field to the history response.**  When a run is
   active and the coordinator knows which tool is executing (available from
   the runner's state), include it so the UI can show "calling \<tool\>..."
   instead of just "thinking..." on reconnect.  This is a UX nicety, not
   required for correctness.

3. **Guard against stale run IDs.**  The current `reconcileRunStateFromHistory`
   + event replay buffer already handles the race where a `final` event
   arrives between the history fetch and the replay.  No additional guard is
   needed — the event sets `isRunning = false` via `finishCurrentRun()`.

## Compatibility Considerations

- **Multiple conversations:** `activeRunsRef` is a `Map<conversationId, runId>`.
  Each conversation independently tracks its run state via
  `reconcileRunStateFromHistory`.  The fix is per-conversation and does not
  interfere with other conversations.

- **Multiple tabs:** Each tab has its own WebSocket connection and its own
  `useBackend` instance.  Each tab independently calls `conversations.history`
  on connect.  PubSub events are broadcast to all connections for the same
  user.  The fix does not change this — each tab will independently rehydrate
  the correct busy state.

- **False positives if the run already finished:** If the run finishes between
  the history RPC response and the event replay, the `final` event will be in
  `pendingEventsRef` and will be replayed immediately after history loads,
  calling `finishCurrentRun()` and clearing the busy state.  No false positive.

- **Cross-channel runs (Telegram/Discord):** Runs from other channels also
  set coordinator state.  The fix correctly shows busy state regardless of
  which channel originated the run.

## Test Plan

1. **Unit test:** Add a case to `useBackend.test.ts` that simulates a
   `conversations.history` response with `activeRunId: "run-123"` and
   verifies that `reconcileRunStateFromHistory` returns `isRunning: true`
   (this test already exists and passes — confirming the frontend logic is
   correct if the field is present).

2. **Integration test (manual):**
   - Start a run with a slow tool (e.g. a 10-second sleep tool).
   - Refresh the page mid-run.
   - Verify: "thinking..." or "calling \<tool\>..." spinner appears, Stop
     button is shown, streaming text resumes when the next `delta` arrives.
   - Wait for the run to complete — verify the `final` event clears the
     busy state normally.

3. **Edge case — run finishes during refresh:**
   - Start a run, then refresh immediately as the run is about to finish.
   - Verify: if the `final` event arrives before history loads, it is
     buffered and replayed, clearing the busy state.  No stuck spinner.

4. **Multi-tab test:**
   - Open two tabs on the same conversation.
   - Start a run in tab A, refresh tab B.
   - Verify tab B shows the busy state.
   - Abort from tab B — verify tab A also clears the busy state (via the
     `aborted` event).

5. **Type safety:** Run `npx tsc --noEmit` in `web/` to verify no type errors.
