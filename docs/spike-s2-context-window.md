# Spike S2: Context window implementation location
File: internal/agents/runner.go

Function: `Run()` and `buildMessages()`

Line range:
- `Run()` message assembly + context handling: `internal/agents/runner.go:259-277`
- `buildMessages()` actual `messages []` construction: `internal/agents/runner.go:543-623`

Existing token counting: yes (context-window aware compression exists)
- `Run()` resolves `contextWindow` (`lookupContextWindow` then config fallback) and calls `compressContext(...)` before `ChatCompletionStream`.
- This is the existing context-size enforcement path, though it is model-context based rather than a voice-specific max-token budget argument.

Recommended insertion point for MaxContextTokens check:
- Primary insertion: in `Run()`, immediately after `llmMessages := self.buildMessages(...)` and before/alongside `compressContext(...)` at `internal/agents/runner.go:259-277`.
- Voice path should pass `MaxContextTokens` via `voice -> gw -> SendMessageParameters`, then `Run()` should apply an additional budget trim for voice-origin requests.
- Alternative (less ideal): inside `buildMessages()` after the `messages` slice is assembled and before `fixInterruptedToolCalls`, but this mixes transport-specific policy into generic message construction.

Additional trace evidence:
- Voice entry point commits at `internal/voice/pipeline.go:350-362` via `deps.SendMessage(...)`.
- Gateway adapter forwards in `internal/gw/gateway.go:852-861` into `gw.SendMessage(...)`.
- `gw.SendMessage(...)` invokes runner `Run(...)` where LLM request messages are created and compressed.
