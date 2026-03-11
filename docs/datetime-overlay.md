# Design: Datetime Tool Overlay Injection

**Status:** Draft
**Author:** ziyan
**Date:** 2026-03-11

## Summary

Inject the current date/time into every LLM prompt as a late system overlay, removing the need for models to explicitly call the `datetime` tool to learn the current time. The overlay is always injected on every turn with no configuration. The existing `datetime` tool remains available for explicit use (e.g., timezone conversion) but is rarely needed.

---

## 1. Injection Point in the Prompt Pipeline

**Location:** `internal/runners/runner.go`, inside `buildMessages()`, at the existing overlay injection site (lines 717-722).

The datetime overlay will implement the `tools.OverlayBuilder` interface, the same mechanism used by `user_memory` (STM cache) and `todo`. Overlays are appended as late system messages after conversation history but before the voice overlay.

**Current injection order in `buildMessages()`:**

```
1. System prompt (systemprompt.txt template)
2. Previous conversation summary (if compacted)
3. Conversation messages (user/assistant/tool history)
4. Tool overlays (sorted by tool name)        <-- datetime overlay goes here
5. Voice overlay (if voice mode active)
```

**Why here and not in the system prompt template:**

- The system prompt is built once via `buildSystemPrompt()` and is designed to be cacheable across turns. Embedding a changing timestamp would bust the cache on every turn.
- The overlay mechanism already supports per-turn dynamic content and is the canonical place for "fresh context that changes between turns."
- Overlays are sorted by tool name, giving stable ordering: `datetime` < `todo` < `user_memory` (alphabetical).

**Alternative considered — provider translation layer:** Injecting at the provider level (e.g., in `anthropic.go`) would couple datetime logic to each provider adapter. Rejected: the overlay interface is provider-agnostic.

---

## 2. Overlay Block Format

The overlay will be a short system message using a descriptive XML-style tag, consistent with existing overlays (`<short_term_memory_cache>`, `<voice_call_context>`).

### Format

```xml
<current_datetime>
2026-03-11 14:32:07 PDT (UTC-07:00) (Tuesday)
</current_datetime>
```

The overlay always includes:

- **Date and time** in `YYYY-MM-DD HH:MM:SS` format.
- **Local timezone abbreviation** (e.g., `PDT`, `EST`).
- **Numeric UTC offset** when available (e.g., `UTC-07:00`), for unambiguous timezone identification since abbreviations can be ambiguous.
- **Day of week**, because models frequently need it for scheduling tasks and it costs only one token.

### Design notes

- The tag name `<current_datetime>` is self-explanatory — no additional prose wrapping needed. Models already understand XML-tagged context blocks.
- Total token cost: ~25 tokens per turn (see section 5).

---

## 3. Behavior

The datetime overlay is unconditional:

- **Always enabled.** There is no toggle to disable it.
- **Always injected on every turn.** There is no per-conversation cadence option.
- **Always uses server local time** with timezone info included for clarity.
- **No configuration options.** No per-agent overrides, no workspace-level settings.

This keeps the implementation simple and ensures every agent always has time awareness. The token cost (~25 tokens/turn) is negligible, and there is no realistic scenario where injecting the current time is harmful.

---

## 4. Interaction with the Existing `datetime` Tool

### Keep the tool, reduce its usage

The `datetime` tool remains registered and callable. This handles edge cases:

- **Explicit timezone conversion:** A user asks "what time is it in Tokyo?" — the overlay shows local time, but the model can call `datetime` with a timezone argument (future enhancement) or reason from the overlay.
- **Sub-second precision:** Unlikely need, but available.

### Updated tool description

Since the overlay is always present, update the `datetime` tool description to reflect this:

```
"Returns the current date and time. Note: the current date/time is already provided in the prompt context on every turn. Use this tool only if you need a fresh timestamp mid-turn or a specific timezone conversion."
```

This nudges the model away from redundant calls without removing capability.

### System prompt hint (optional, low priority)

No explicit "the current time is X" sentence is needed in the system prompt. The `<current_datetime>` tag is self-explanatory. If models under-utilize it, a one-line hint can be added to `systemprompt.txt`:

```
The current date and time is provided in the <current_datetime> context block.
```

This should be measured before adding — it may not be necessary.

---

## 5. Token/Cost Impact and Caching Strategy

### Token cost

| Component | Tokens (approx) |
|-----------|-----------------|
| `<current_datetime>\n` | 4 |
| `2026-03-11 14:32:07 PDT (UTC-07:00) (Tuesday)` | 18 |
| `\n</current_datetime>` | 4 |
| **Total** | **~26 tokens/turn** |

At $3/M input tokens (Sonnet-class pricing), 26 tokens/turn = **$0.000078/turn**. For a 50-turn conversation: ~$0.004 additional cost. Negligible.

### Caching strategy

**Problem:** The datetime overlay changes every turn (or at least every minute), which could bust prompt caching if placed in a cacheable prefix.

**Solution:** The overlay is already placed in the "late system messages" section, which is *after* conversation history. This section is inherently uncacheable in current provider caching schemes (Anthropic's prompt caching caches prefixes, not suffixes). Therefore, the datetime overlay has **zero impact on prompt cache hit rates**.

The cacheable prefix (system prompt + early conversation turns) remains stable.

---

## 6. Overlay Ordering and Interaction with Other Overlays

### Current overlay producers (sorted alphabetically by tool name)

| Tool Name | Overlay Tag | Purpose |
|-----------|------------|---------|
| `datetime` (new) | `<current_datetime>` | Current date/time |
| `todo` | `<active_todos>` | Active task list |
| `user_memory` | `<short_term_memory_cache>` | Recently retrieved memory snippets |

After adding `datetime`, the overlay order becomes: `datetime` → `todo` → `user_memory` (alphabetical by tool name, per `BuildOverlays()` in `tools.go`).

### Voice overlay

The voice overlay is injected *after* all tool overlays (line 725-730 in `runner.go`). The datetime overlay does not conflict — voice mode benefits from having time context available.

### Interaction notes

- **Memory overlay:** No conflict. Memory snippets and datetime serve orthogonal purposes.
- **Todo overlay:** No conflict. Todos may reference deadlines — having datetime context nearby helps the model reason about urgency.
- **Voice overlay:** Complementary. Voice interactions often involve scheduling ("set a reminder for tomorrow") and benefit from time awareness.
- **Future overlays:** The alphabetical ordering is stable. New overlays should choose tool names aware of this ordering, but in practice ordering between overlays doesn't matter much since they're independent context blocks.

---

## 7. Testing Strategy

### 7.1 Unit Tests: Overlay Builder

**File:** `internal/tools/datetime/overlay_test.go`

```go
// Test cases:
// 1. BuildOverlay returns correctly formatted <current_datetime> block
// 2. Output includes timezone abbreviation and numeric UTC offset
// 3. Output includes day of week
// 4. Output includes date and time components
```

### 7.2 Integration Tests: Prompt Composition

**File:** `internal/runners/runner_test.go` (extend existing)

```go
// Test cases:
// 1. buildMessages() includes <current_datetime> block in output
// 2. Overlay appears after conversation messages, before voice overlay
// 3. Overlay ordering is alphabetical (datetime < todo < user_memory)
```

### 7.3 Golden Prompt Tests

**Directory:** `internal/runners/testdata/`

Add golden prompt snapshots that include the datetime overlay. Since the timestamp changes, use a regex or placeholder matcher:

```go
// Replace actual timestamp with placeholder before comparison:
// "<current_datetime>\n__DATETIME__\n</current_datetime>"
```

Alternatively, inject a fixed `time.Now` via a clock interface for deterministic tests.

### 7.4 Recommended: Clock Interface

To make all datetime tests deterministic, introduce a minimal clock interface:

```go
type Clock interface {
    Now() time.Time
}

type realClock struct{}
func (realClock) Now() time.Time { return time.Now() }
```

Pass `Clock` to the overlay builder. Tests inject a fixed clock. This is a small, well-justified abstraction.

---

## 8. Migration and Rollout

### Phase 1: Ship overlay, always on

- Implement `BuildOverlay` on the datetime tool.
- The overlay is always injected on every turn for every agent — no config needed.
- Update `datetime` tool description to mention overlay context.
- **No breaking changes.** All agents gain time awareness automatically.

### Phase 2: Observe

- Monitor `datetime` tool call frequency. Expect significant drop.
- Monitor token usage — should be negligible increase.

### Phase 3: Optional — deprecate explicit tool calls (not recommended short-term)

- If `datetime` tool call frequency drops to near-zero, consider removing it from the default tool set.
- Keep it available for explicit opt-in via agent `Tools` allowlist.
- **Not recommended initially** — the tool is cheap and provides a useful escape hatch.

### Backward Compatibility

- **Existing agents:** Get the overlay automatically. This is the desired behavior — all agents benefit.
- **Agents with explicit `Tools` allowlist that excludes `datetime`:** The overlay builder is attached to the `datetime` tool. If `datetime` is filtered out via `ApplyFilter()`, its overlay won't fire. This is correct — if an agent explicitly excludes datetime, it shouldn't get the overlay either.

---

## 9. Implementation Checklist

1. [ ] Implement `BuildOverlay` on `datetimeTool` in `internal/tools/datetime/datetime.go`
2. [ ] Add `Clock` interface for testability
3. [ ] Include local timezone abbreviation and numeric UTC offset in overlay output
4. [ ] Update `datetime` tool description to reference overlay
5. [ ] Write unit tests (overlay builder)
6. [ ] Write integration tests (prompt composition)
7. [ ] Add golden prompt test fixtures

---

## 10. Open Questions

1. **Is day-of-week worth the extra tokens?** Strongly yes for scheduling use cases. ~1 extra token.
2. **Should the numeric UTC offset always be included, or only when the abbreviation is ambiguous?** Current proposal: always include for consistency and clarity. Abbreviations like `CST` are ambiguous (US Central vs China Standard).
