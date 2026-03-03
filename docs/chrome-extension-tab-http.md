# Chrome Extension: Attached Tab Credentialed HTTP Client

## 1  Current-State Assessment

### 1.1  Extension layout (`assets/chrome-extension/`)

```
chrome-extension/
├── manifest.json          # Manifest v3 — permissions: debugger, tabs, activeTab, storage
├── background.js          # 463-line plain-JS service worker — CDP relay logic
├── options.html / .js     # Config page: relay URL + bearer token
└── icons/                 # 16/32/48/128 px PNGs
```

The extension is a **Chrome DevTools Protocol (CDP) relay**.
It attaches to tabs via `chrome.debugger`, maintains a WebSocket to
`/api/v1/browser`, and forwards CDP commands/events between TeaNode and
Chrome.  There is no build step — source files are loaded directly.

### 1.2  Backend relay (`internal/integrations/browsers/relaybrowser/`)

| Struct | Purpose |
|--------|---------|
| `Relay` | Manages `map[string]*relayConnection`; exposes `SendCDPCommand()` which blocks via `pending.Requests` |
| `relayConnection` | Per-extension state: userId, gorilla WS conn, targets map, pending map |

`Relay` is registered on the gateway at `/api/v1/browser` and wired into
`CompositeBrowser` alongside the optional headless backend.

### 1.3  Main WebSocket RPC (`/api/v1/websocket`)

Frame types: `req` (client→server), `res` (server→client), `event` (push).
Auth via `?token=` query param or `Authorization: Bearer` header.
~40 RPC methods dispatched in `websocket.go:dispatch()`.

### 1.4  Tool system (`internal/tools/`, `internal/runners/runner.go`)

```go
type Tool interface {
    Definition() providers.ToolDefinition
    Execute(ctx context.Context, arguments string) (string, error)
}
```

Tools register via `init()` → `tools.RegisterBuiltinTool()`.
Runner dispatches tool calls in parallel goroutines (Phase 2), collects
results in order (Phase 3), persists tool-result messages.
Context carries: `Runner`, `User`, `Store`, `PubSub`, `QuestionBroker`, etc.

### 1.5  Blocking tool precedent — `ask_user_question`

The `QuestionBroker` in `internal/tools/askuser/` implements the exact
"block tool goroutine → wait for client response via RPC" pattern we need:

1. Tool creates `PendingQuestion` with buffered `chan AnswerPayload`.
2. Registers in broker, broadcasts `conversation_questions` event (pubsub).
3. Blocks on `select { case <-answerChan; case <-ctx.Done() }`.
4. Client receives event, presents UI, calls `questions.answer` RPC.
5. RPC handler calls `broker.Answer()` which sends to channel.

### 1.6  Build system

* **Bundler**: Webpack 5 (`web/webpack.config.js`), single entry `./src/index.tsx`.
* **Output**: `internal/frontend/static/` (embedded by Go via `//go:embed`).
* **Build command**: `cd web && npm run build` (invoked by `make web`).
* **Stack**: React 19, MUI 7, TanStack Router, TypeScript 5.8, i18next.

---

## 2  Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     TeaNode Backend                         │
│                                                             │
│  Runner goroutine         TabToolBroker        WS conn pool │
│  ┌──────────────┐     ┌────────────────┐    ┌────────────┐ │
│  │ tab.http_req │────▶│ pending map    │    │ extension  │ │
│  │ Execute()    │     │ attachment map │    │ WS conn    │ │
│  │ blocks on ch │◀────│ resultChan    │◀───│ tab.tool   │ │
│  └──────────────┘     └────────────────┘    │ _result()  │ │
│                              │               └─────▲──────┘ │
│                              │ pubsub event        │        │
│                              ▼ tab_tool_call       │        │
│                        ┌───────────┐               │        │
│                        │ /api/v1/  │               │        │
│                        │ websocket │───────────────┘        │
│                        └─────┬─────┘                        │
└──────────────────────────────┼──────────────────────────────┘
                               │  single WS (existing endpoint)
                    ┌──────────┴──────────┐
                    │   Chrome Extension   │
                    │                      │
                    │  Side Panel (React)   │
                    │  ┌────────────────┐  │
                    │  │ Chat UI        │  │  ← agent/conversation picker, messages
                    │  │ rpc.ts client  │──┘  ← reuses web/src/rpc.ts
                    │  └───────┬────────┘
                    │          │ chrome.runtime.sendMessage
                    │  ┌───────▼────────┐
                    │  │ Background SW  │     ← holds API token, chrome.cookies
                    │  └───────┬────────┘
                    │          │ chrome.tabs.sendMessage
                    │  ┌───────▼────────────────────────────┐
                    │  │ Content Script (ISOLATED world)    │
                    │  │ nonce-gated postMessage             │
                    │  └───────┬────────────────────────────┘
                    │          │ window.postMessage({__tn:<nonce>, …})
                    │  ┌───────▼────────────────────────────┐
                    │  │ Page Bridge    (MAIN world)        │
                    │  │ fetch() with page credentials      │
                    │  └────────────────────────────────────┘
                    └──────────────────────────────────────────
```

**Key decisions:**

| Decision | Rationale |
|----------|-----------|
| Reuse `/api/v1/websocket` | No new endpoint; extension is just another WS client with extra capabilities |
| Broker pattern (à la `ask_user_question`) | Proven blocking pattern; no bidirectional RPC changes needed |
| Side panel (not popup) | Persistent UI that survives clicks outside the extension |
| Background SW holds token | Webpage never sees TeaNode credentials |
| Content script + page bridge with nonce | Page world `fetch()` inherits cookies/CORS; nonce prevents page from spoofing results |
| `chrome.cookies` in background SW | No page bridge needed for cookies; background SW has direct API access |

---

## 3  Backend Changes

### 3.1  Tab Tool Broker (`internal/tools/tab/broker.go`)

Combines attachment registry + pending tool-call tracking.
Modeled after `askuser.QuestionBroker`.

```go
package tab

type TabToolBroker struct {
    mu          sync.Mutex
    attachments map[string]*TabAttachment   // key: "userId:agentId:conversationId"
    pending     map[string]*PendingToolCall  // key: requestId (ULID)
}

type TabAttachment struct {
    UserID         string
    AgentID        string
    ConversationID string
    TabURL         string
    TabTitle       string
    TabID          int       // chrome tab id (informational)
    AttachedAt     time.Time
}

type PendingToolCall struct {
    ID             string
    UserID         string
    AgentID        string
    ConversationID string
    ToolName       string
    Arguments      json.RawMessage
    resultChan     chan ToolCallResult   // buffered, cap 1
}

type ToolCallResult struct {
    Result string
    Error  string
}
```

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `Attach` | `(a TabAttachment) error` | Register attachment; error if duplicate key exists for different connection |
| `Detach` | `(userId, agentId, conversationId string)` | Remove attachment |
| `HasAttachment` | `(userId, agentId, conversationId string) bool` | Check existence |
| `GetAttachment` | `(userId, agentId, conversationId string) *TabAttachment` | Lookup |
| `ListForUser` | `(userId string) []TabAttachment` | List all user attachments |
| `RegisterPending` | `(p *PendingToolCall)` | Add pending call to map |
| `Resolve` | `(requestId string, result ToolCallResult) error` | Deliver result to channel, remove from map |
| `CancelPending` | `(requestId string)` | Close channel, remove from map |
| `DetachAll` | `(userId string)` | Remove all attachments for user (on WS disconnect) |

The broker is instantiated once in `Coordinator` (like `QuestionBroker`)
and injected into tool context via `ContextWithTabToolBroker()`.

### 3.2  New RPC methods (`internal/api/v1api/rpc_tab.go`)

Registered in `websocket.go:dispatch()` alongside existing methods.

#### `tab.attach`

```jsonc
// Request
{
  "type": "req", "id": "c1", "method": "tab.attach",
  "params": {
    "agentId": "default-agent",
    "conversationId": "conv-abc",
    "tabUrl": "https://app.example.com/dashboard",
    "tabTitle": "Dashboard — Example App",
    "tabId": 42
  }
}
// Response
{ "type": "res", "id": "c1", "ok": true, "payload": null }
```

Handler: validates ownership of the conversation, calls `broker.Attach()`.
Broadcasts `tab_attachment` event (`action: "attached"`) via pubsub.

#### `tab.detach`

```jsonc
// Request
{
  "type": "req", "id": "c2", "method": "tab.detach",
  "params": {
    "agentId": "default-agent",
    "conversationId": "conv-abc"
  }
}
// Response
{ "type": "res", "id": "c2", "ok": true, "payload": null }
```

Handler: calls `broker.Detach()`, cancels any pending tool calls for that
attachment, broadcasts `tab_attachment` event (`action: "detached"`).

#### `tab.tool_result`

```jsonc
// Request
{
  "type": "req", "id": "c3", "method": "tab.tool_result",
  "params": {
    "requestId": "01JXYZ...",
    "result": "{\"status\":200,\"body\":\"...\"}",  // JSON-stringified
    "error": ""                                      // or error message
  }
}
// Response
{ "type": "res", "id": "c3", "ok": true, "payload": null }
```

Handler: calls `broker.Resolve(requestId, result)`.  Returns error if
`requestId` not found (already resolved/cancelled).

#### `tab.attachments.list`

```jsonc
// Request
{ "type": "req", "id": "c4", "method": "tab.attachments.list", "params": {} }
// Response
{
  "type": "res", "id": "c4", "ok": true,
  "payload": {
    "attachments": [
      {
        "agentId": "default-agent",
        "conversationId": "conv-abc",
        "tabUrl": "https://app.example.com/dashboard",
        "tabTitle": "Dashboard — Example App",
        "attachedAt": "2026-03-02T10:00:00Z"
      }
    ]
  }
}
```

Handler: calls `broker.ListForUser(userId)`.

### 3.3  Server → Client event: `tab_tool_call`

Broadcast via pubsub when a tool's `Execute()` registers a pending call.
Filtered to matching `userId` by the existing pubsub subscriber filter.

```jsonc
{
  "type": "event",
  "event": "tab_tool_call",
  "payload": {
    "requestId": "01JXYZ...",
    "agentId": "default-agent",
    "conversationId": "conv-abc",
    "toolName": "tab.http_request",
    "arguments": {
      "method": "GET",
      "url": "/api/data",
      "headers": { "Accept": "application/json" }
    }
  }
}
```

The extension side panel receives this event.  Only the extension instance
that holds the matching (agentId, conversationId) attachment should act on
it; other WS clients (e.g., the main web UI) ignore it.

### 3.4  Server → Client event: `tab_attachment`

Broadcast on attach/detach so all connected clients can update UI.

```jsonc
{
  "type": "event",
  "event": "tab_attachment",
  "payload": {
    "action": "attached",          // or "detached"
    "userId": "user-123",
    "agentId": "default-agent",
    "conversationId": "conv-abc",
    "tabUrl": "https://app.example.com",
    "tabTitle": "Dashboard"
  }
}
```

### 3.5  WS disconnect cleanup

In `webSocketConnection.serve()` defer block, call
`broker.DetachAllForConnection(conn)` to remove attachments owned by the
disconnecting WS client and reject any pending tool calls.

To enable this, each `TabAttachment` must store a reference to its owning
WS connection (or a connection identifier).  On disconnect the broker
iterates attachments and removes those matching the connection.

### 3.6  Tool definitions (`internal/tools/tab/`)

Three new tools registered via `init()` → `tools.RegisterBuiltinTool()`.
All gated: return error if no `TabToolBroker` in context (i.e., not a
WebUI-originated run) or if no attachment exists for the conversation.

#### 3.6.1  `tab.http_request`

```jsonc
{
  "type": "function",
  "function": {
    "name": "tab.http_request",
    "description": "Execute an HTTP request in the context of the attached browser tab. The request runs with the tab's cookies, session, and CORS policy. Supports relative URLs (resolved against the tab's current URL).",
    "parameters": {
      "type": "object",
      "properties": {
        "method":     { "type": "string", "enum": ["GET","POST","PUT","PATCH","DELETE","HEAD","OPTIONS"], "default": "GET" },
        "url":        { "type": "string", "description": "Absolute or relative URL." },
        "headers":    { "type": "object", "additionalProperties": { "type": "string" }, "description": "Request headers (key-value)." },
        "body":       { "type": "string", "description": "Request body (for POST/PUT/PATCH)." },
        "timeout_ms": { "type": "integer", "default": 30000, "description": "Request timeout in milliseconds." }
      },
      "required": ["url"]
    }
  }
}
```

**Execute() flow:**

1. Extract `userId`, `agentId`, `conversationId` from context.
2. Verify attachment exists via `broker.HasAttachment()`.
3. Validate & cap arguments (see §7 size caps).
4. Create `PendingToolCall` with buffered result channel.
5. `broker.RegisterPending(pending)`.
6. Broadcast `tab_tool_call` event via pubsub.
7. `select { case result := <-pending.resultChan; case <-ctx.Done() }`.
8. On result: validate size, return JSON string.
9. On context cancel: `broker.CancelPending(pending.ID)`, return error.

**Result schema (returned by extension):**

```jsonc
{
  "status": 200,
  "statusText": "OK",
  "headers": { "content-type": "application/json", ... },
  "body": "...",
  "url": "https://app.example.com/api/data",   // final URL after redirects
  "truncated": false,
  "duration_ms": 142
}
```

#### 3.6.2  `tab.cookies.list`

```jsonc
{
  "type": "function",
  "function": {
    "name": "tab.cookies.list",
    "description": "List cookies accessible to the attached browser tab, optionally filtered by URL, domain, or name.",
    "parameters": {
      "type": "object",
      "properties": {
        "url":    { "type": "string", "description": "Filter by URL. If omitted, uses attached tab URL." },
        "domain": { "type": "string", "description": "Filter by domain." },
        "name":   { "type": "string", "description": "Filter by cookie name." }
      }
    }
  }
}
```

**Result schema:**

```jsonc
{
  "cookies": [
    {
      "name": "session_id",
      "value": "abc123",
      "domain": ".example.com",
      "path": "/",
      "secure": true,
      "httpOnly": true,
      "sameSite": "lax",
      "expirationDate": 1740000000
    }
  ]
}
```

#### 3.6.3  `tab.cookies.get`

```jsonc
{
  "type": "function",
  "function": {
    "name": "tab.cookies.get",
    "description": "Get a specific cookie by name for the attached tab's URL.",
    "parameters": {
      "type": "object",
      "properties": {
        "url":  { "type": "string", "description": "Cookie URL scope. If omitted, uses attached tab URL." },
        "name": { "type": "string", "description": "Cookie name." }
      },
      "required": ["name"]
    }
  }
}
```

**Result schema:**

```jsonc
{
  "cookie": {
    "name": "session_id",
    "value": "abc123",
    "domain": ".example.com",
    "path": "/",
    "secure": true,
    "httpOnly": true,
    "sameSite": "lax",
    "expirationDate": 1740000000
  }
}
// or { "cookie": null } if not found
```

### 3.7  Context propagation

In `coordinator.go`, enrich the runner context:

```go
ctx = tab.ContextWithTabToolBroker(ctx, self.tabToolBroker)
```

Alongside the existing `askuser.ContextWithQuestionBroker(ctx, ...)`.

### 3.8  Agent tool allow-listing

The three `tab.*` tools are registered as builtins but are **not enabled
by default** in agents.  They must be explicitly added to an agent's
`tools` allow-list (e.g., `["tab.http_request", "tab.cookies.list",
"tab.cookies.get"]`) via agent configuration.  This follows the existing
pattern where tools like `shell` require explicit opt-in.

---

## 4  Extension Changes

### 4.1  Manifest updates (`manifest.json`)

```jsonc
{
  "manifest_version": 3,
  "name": "TeaNode Browser Relay",
  "version": "0.2.0",
  "description": "Attach TeaNode to your browser — CDP relay + credentialed tab tools.",

  "permissions": [
    "debugger",        // existing — CDP relay
    "tabs",            // existing
    "activeTab",       // existing
    "storage",         // existing
    "cookies",         // NEW — tab.cookies.* tools
    "scripting",       // NEW — inject content script / page bridge
    "sidePanel"        // NEW — persistent chat UI
  ],

  "host_permissions": [
    "<all_urls>"       // CHANGED from localhost-only — needed for cookies
                       // + scripting on arbitrary attached tabs
  ],

  "background": {
    "service_worker": "dist/background.js",   // CHANGED — now built by webpack
    "type": "module"
  },

  "side_panel": {
    "default_path": "dist/sidepanel.html"     // NEW
  },

  "action": {
    "default_title": "TeaNode (click to open side panel)",
    "default_icon": { "16": "icons/icon16.png", "32": "icons/icon32.png",
                      "48": "icons/icon48.png", "128": "icons/icon128.png" }
  },

  "options_page": "options.html",             // keep existing plain-JS options page

  "icons": {
    "16": "icons/icon16.png", "32": "icons/icon32.png",
    "48": "icons/icon48.png", "128": "icons/icon128.png"
  }
}
```

**Notes on `<all_urls>` host_permissions:** Required for `chrome.cookies.getAll()`
with arbitrary URLs and `chrome.scripting.executeScript()` on any attached tab.
This is a broad permission; the Chrome Web Store listing should explain the
legitimate use case.  In the future, optional permissions + `permissions.request()`
could narrow this to user-approved domains.

### 4.2  New source layout under `web/src/extension/`

```
web/src/extension/
├── background/
│   ├── index.ts               # SW entry — merges legacy CDP relay + new tool handler
│   ├── cdpRelay.ts            # Ported from current background.js (typed)
│   ├── toolHandler.ts         # Receives tool requests, dispatches to content script
│   └── cookieHandler.ts       # chrome.cookies wrapper
│
├── sidepanel/
│   ├── index.tsx              # React entry point
│   ├── SidePanel.tsx          # Root component: agent/conversation picker + chat
│   ├── AgentSelector.tsx      # Dropdown — fetches via agents.list RPC
│   ├── ConversationPicker.tsx # List + "New Conversation" — conversations.list RPC
│   ├── TabAttachment.tsx      # Attach/detach button + status badge
│   ├── ChatView.tsx           # Reuses MessageList pattern from web app (simplified)
│   └── sidepanel.html         # Shell HTML (loaded by side_panel.default_path)
│
├── content/
│   ├── contentScript.ts       # Injected into ISOLATED world — nonce relay
│   └── pageBridge.ts          # Injected into MAIN world — runs fetch()
│
└── shared/
    ├── types.ts               # Extension-specific types (tool request/result, nonce msg)
    └── messages.ts            # chrome.runtime message type constants
```

### 4.3  Background service worker (`background/index.ts`)

**Responsibilities:**

1. **Legacy CDP relay** — ported from `background.js` to TypeScript as `cdpRelay.ts`.
   Connects to `/api/v1/browser` for CDP forwarding.  No behavior change.

2. **Tool execution dispatch** — listens for `chrome.runtime.onMessage` from
   the side panel.  Routes `tab.http_request` to content script,
   `tab.cookies.*` to `cookieHandler.ts`.

3. **Content script lifecycle** — injects `contentScript.ts` + `pageBridge.ts`
   into the attached tab on first tool request (lazy injection).
   Tracks injected tabs to avoid double-injection.  Re-injects on
   `chrome.tabs.onUpdated` (status === "complete") for navigation.

4. **Side panel management** — opens side panel on extension icon click
   via `chrome.sidePanel.open()`.

**Message protocol (chrome.runtime):**

```typescript
// Side panel → Background SW
interface ToolExecuteRequest {
  type: "tool_execute";
  toolName: "tab.http_request" | "tab.cookies.list" | "tab.cookies.get";
  requestId: string;
  tabId: number;
  arguments: Record<string, unknown>;
}

// Background SW → Side panel
interface ToolExecuteResponse {
  type: "tool_execute_response";
  requestId: string;
  result?: string;   // JSON-stringified result
  error?: string;
}

// Background SW → Content script (chrome.tabs.sendMessage)
interface PageFetchRequest {
  type: "page_fetch_request";
  requestId: string;
  method: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
  timeout_ms: number;
}

// Content script → Background SW (chrome.runtime.sendMessage response)
interface PageFetchResponse {
  type: "page_fetch_response";
  requestId: string;
  result?: FetchResult;
  error?: string;
}
```

### 4.4  Side panel chat UI (`sidepanel/`)

A lightweight React app built with the same MUI theme (`web/src/theme.ts`),
i18n setup, and TypeScript types as the main web app.

**Reused from `web/src/`:**

| Module | Usage in side panel |
|--------|---------------------|
| `rpc.ts` | WebSocket RPC client — `openSocket()`, `sendRpc()` |
| `theme.ts` | MUI theme provider |
| `types.ts` | Frame types, conversation types, agent types |
| `i18n/` | Translations |
| `components/MessageBubble.tsx` | Render individual messages (if suitable) |
| `markdown.ts` | Markdown rendering utilities |

**New components:**

* **`AgentSelector`** — calls `agents.list`, renders MUI `Select`.
  Defaults to `"default-agent"`.
* **`ConversationPicker`** — calls `conversations.list` filtered by agent,
  renders list with "New Conversation" button.  Selecting a conversation
  loads history via `conversations.history`.
* **`TabAttachment`** — shows current tab info, "Attach" / "Detach" toggle.
  On attach: gets active tab via `chrome.tabs.query()`, calls `tab.attach` RPC.
  On detach: calls `tab.detach` RPC.
  Shows green/red badge for attachment status.
* **`ChatView`** — simplified message list.  Subscribes to `conversation`
  events for streaming.  Input box at bottom for `conversations.send`.
  Shows tool-call / tool-result bubbles inline (including `tab.*` tools).

**Event handling in side panel:**

The side panel listens for `tab_tool_call` events.  When received:

1. Check if `(agentId, conversationId)` matches the currently attached tab.
2. If yes, forward to background SW via `chrome.runtime.sendMessage()`.
3. Wait for `ToolExecuteResponse` from background SW.
4. Call `tab.tool_result` RPC with the result.

This keeps the WS connection and RPC calls in the side panel (which has
direct network access), while tool execution logic runs in the background
SW (which has chrome API access).

### 4.5  Content script + page bridge — nonce scheme

#### 4.5.1  Nonce lifecycle

```
┌──────────────────┐     inject with nonce      ┌──────────────────┐
│ Content Script   │ ─────────────────────────▶  │ Page Bridge      │
│ (ISOLATED world) │                             │ (MAIN world)     │
│                  │   window.postMessage         │                  │
│ nonce = crypto   │ ◀─────────────────────────▶ │ const NONCE =    │
│  .randomUUID()   │   {__tn: nonce, ...}        │   "<injected>"   │
└──────────────────┘                             └──────────────────┘
```

1. **Injection** — background SW calls `chrome.scripting.executeScript()`
   twice on the attached tab:
   - First: `contentScript.ts` in `world: "ISOLATED"` — generates nonce
     via `crypto.randomUUID()`, stores in module scope.
   - Second: `pageBridge.ts` in `world: "MAIN"` — nonce is passed as an
     argument to the injected function (via the `args` parameter of
     `executeScript`), captured in closure.

2. **Nonce relay** — content script passes nonce to page bridge through
   a one-time `CustomEvent` on `document` with a random event name
   (shared via a DOM element attribute or `chrome.scripting.executeScript`
   args).

   *Preferred approach:* background SW generates nonce, passes it to
   **both** scripts via `chrome.scripting.executeScript({ args: [nonce] })`.
   Both scripts receive the same nonce without any DOM-observable exchange.

3. **Request flow:**
   - Content script receives `PageFetchRequest` from background SW.
   - Posts `window.postMessage({ __tn: nonce, type: "req", id, payload })`.
   - Page bridge's `message` listener validates `event.data.__tn === NONCE`.
   - Page bridge executes `fetch(url, options)` in page world.
   - Page bridge posts `window.postMessage({ __tn: nonce, type: "res", id, result })`.
   - Content script's `message` listener validates `event.data.__tn === NONCE`
     and `event.data.id === expectedId`.
   - Content script sends result to background SW via `chrome.runtime.sendMessage()`.

4. **Per-request ID** — each request carries a unique `id` (UUID).
   Content script only accepts the first response matching that `id`.
   This prevents replay attacks even if the page somehow observes the nonce.

#### 4.5.2  Security properties

| Threat | Mitigation |
|--------|------------|
| Page sends fake response | Must know nonce (injected via `executeScript args`, not DOM-observable) |
| Page replays observed nonce | Per-request `id` ensures content script only accepts one response per request |
| Page intercepts request data | `postMessage` is observable, but the page already has its own cookies/data — no escalation |
| Page calls TeaNode API | Background SW holds token; page has no channel to the extension (no `externally_connectable`) |
| Content script persists across navigations | Background SW tracks injection state; re-injects on `tabs.onUpdated` with fresh nonce |
| Nonce reuse across navigations | New nonce generated on each injection |

#### 4.5.3  Page bridge `fetch()` implementation

```typescript
// Simplified — actual implementation in pageBridge.ts
window.addEventListener("message", async (event) => {
  if (event.source !== window) return;
  if (event.data?.__tn !== NONCE || event.data?.type !== "req") return;

  const { id, payload } = event.data;
  const { method, url, headers, body, timeout_ms } = payload;

  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeout_ms || 30000);

    const response = await fetch(url, {
      method: method || "GET",
      headers: headers || {},
      body: body || undefined,
      credentials: "include",          // send cookies
      signal: controller.signal,
    });
    clearTimeout(timer);

    const respHeaders: Record<string, string> = {};
    response.headers.forEach((v, k) => { respHeaders[k] = v; });

    let respBody = await response.text();
    let truncated = false;
    if (respBody.length > MAX_BODY_SIZE) {
      respBody = respBody.slice(0, MAX_BODY_SIZE);
      truncated = true;
    }

    window.postMessage({
      __tn: NONCE, type: "res", id,
      result: {
        status: response.status,
        statusText: response.statusText,
        headers: respHeaders,
        body: respBody,
        url: response.url,
        truncated,
      },
    });
  } catch (err) {
    window.postMessage({
      __tn: NONCE, type: "res", id,
      error: String(err),
    });
  }
});
```

### 4.6  Cookie handler (`background/cookieHandler.ts`)

Runs in the background service worker using `chrome.cookies` API.
No content script or page bridge needed.

```typescript
export async function listCookies(args: {
  url?: string; domain?: string; name?: string;
}): Promise<chrome.cookies.Cookie[]> {
  const query: chrome.cookies.GetAllDetails = {};
  if (args.url) query.url = args.url;
  if (args.domain) query.domain = args.domain;
  if (args.name) query.name = args.name;
  return chrome.cookies.getAll(query);
}

export async function getCookie(args: {
  url: string; name: string;
}): Promise<chrome.cookies.Cookie | null> {
  return chrome.cookies.get({ url: args.url, name: args.name });
}
```

### 4.7  Tab navigation handling

When the attached tab navigates (detected via `chrome.tabs.onUpdated`):

1. Background SW re-injects content script + page bridge with a **new nonce**.
2. Background SW sends `tabUrlChanged` message to side panel.
3. Side panel calls `tab.attach` RPC again with the updated `tabUrl`/`tabTitle`
   to keep the backend's attachment registry current.

When the attached tab is closed (detected via `chrome.tabs.onRemoved`):

1. Background SW notifies side panel.
2. Side panel calls `tab.detach` RPC.
3. Side panel updates UI to show detached state.

---

## 5  Build Integration

### 5.1  Webpack configuration

Extend `web/webpack.config.js` to export an **array** of configurations:

```javascript
module.exports = [
  // ---- Config 1: Existing web app (unchanged) ----
  webAppConfig,

  // ---- Config 2: Chrome extension ----
  {
    name: "extension",
    entry: {
      "background":     "./src/extension/background/index.ts",
      "sidepanel":      "./src/extension/sidepanel/index.tsx",
      "content-script": "./src/extension/content/contentScript.ts",
      "page-bridge":    "./src/extension/content/pageBridge.ts",
    },
    output: {
      path: path.resolve(__dirname, "../assets/chrome-extension/dist"),
      filename: "[name].js",
      clean: true,
    },
    resolve: {
      extensions: [".ts", ".tsx", ".js"],
    },
    module: {
      rules: [
        { test: /\.tsx?$/, use: "ts-loader", exclude: /node_modules/ },
        // CSS rules for sidepanel (MUI needs emotion runtime, not extracted CSS)
        { test: /\.css$/, use: ["style-loader", "css-loader"] },
      ],
    },
    plugins: [
      new HtmlWebpackPlugin({
        template: "./src/extension/sidepanel/sidepanel.html",
        filename: "sidepanel.html",
        chunks: ["sidepanel"],
      }),
      // Copy manifest.json is NOT needed — it lives in the extension root,
      // not in dist/.  The manifest references dist/*.js paths.
    ],
    optimization: {
      // content-script and page-bridge must be single files (no chunks)
      splitChunks: false,
    },
    // Background SW cannot use eval-based source maps
    devtool: isProd ? false : "cheap-module-source-map",
  },
];
```

### 5.2  TypeScript configuration

Add `web/src/extension/` to the existing `tsconfig.json` `include` (it's
already covered by `"include": ["src"]`).  No change needed.

**Chrome types dependency:**

```bash
cd web && npm install --save-dev @anthropic-ai/chrome-types
```

Alternatively, use the well-known community package `chrome-types` or
`@anthropic-ai/chrome-types`.

To avoid polluting the main web build, create a separate
`tsconfig.extension.json` that extends the base config:

```jsonc
// web/tsconfig.extension.json
{
  "extends": "./tsconfig.json",
  "compilerOptions": {
    "types": ["chrome-types"]
  },
  "include": ["src/extension"]
}
```

The extension webpack config references this via `ts-loader` options:
`configFile: "tsconfig.extension.json"`.  The main web build is unaffected.

### 5.3  npm scripts

Update `web/package.json`:

```jsonc
{
  "scripts": {
    "build": "webpack --mode production",                       // builds BOTH configs
    "dev": "webpack serve --mode development",                  // serves web app only
    "build:extension": "webpack --mode production --config-name extension",
    "watch:extension": "webpack --mode development --watch --config-name extension"
  }
}
```

`npm run build` already builds all configs when `module.exports` is an
array.  `--config-name extension` isolates extension-only builds during
development.

### 5.4  Makefile

No change needed — `make web` runs `npm run build` which now produces both
the web app and extension bundles.

### 5.5  Extension loading

The `assets/chrome-extension/` directory is the extension root:

```
assets/chrome-extension/
├── manifest.json          # references dist/*.js
├── options.html           # existing (keep as plain HTML)
├── options.js             # existing (keep as plain JS)
├── icons/                 # existing
└── dist/                  # NEW — webpack output
    ├── background.js
    ├── sidepanel.js
    ├── sidepanel.html
    ├── content-script.js
    └── page-bridge.js
```

Developers load `assets/chrome-extension/` as an unpacked extension.
The `dist/` directory is gitignored (built artifact).

---

## 6  RPC Method Schemas (Summary)

| Method | Direction | Params | Response Payload |
|--------|-----------|--------|------------------|
| `tab.attach` | client→server | `{agentId, conversationId, tabUrl, tabTitle, tabId}` | `null` |
| `tab.detach` | client→server | `{agentId, conversationId}` | `null` |
| `tab.tool_result` | client→server | `{requestId, result?, error?}` | `null` |
| `tab.attachments.list` | client→server | `{}` | `{attachments: TabAttachment[]}` |

| Event | Direction | Payload |
|-------|-----------|---------|
| `tab_tool_call` | server→client | `{requestId, agentId, conversationId, toolName, arguments}` |
| `tab_attachment` | server→client | `{action, userId, agentId, conversationId, tabUrl, tabTitle}` |

---

## 7  Size Caps & Redaction

| Limit | Value | Enforcement point |
|-------|-------|--------------------|
| HTTP response body | 512 KB | Page bridge truncates before `postMessage`; backend validates |
| HTTP request body (from LLM) | 1 MB | Backend tool `Execute()` rejects before broadcast |
| Response headers total | 64 KB | Page bridge caps; excess headers dropped |
| Cookie value (per cookie) | 4 KB | Chrome enforces natively |
| Cookies list max entries | 200 | Backend tool caps in result |
| Total tool result string | 768 KB | Backend tool truncates with `"truncated": true` |
| `postMessage` payload | 768 KB | Content script rejects oversized bridge responses |

**Redaction rules:**

* `Set-Cookie` response headers are **excluded** from the headers map
  returned by `tab.http_request` (the LLM doesn't need them; cookie
  access is via `tab.cookies.*`).
* Binary response bodies (detected via `Content-Type`) are **not** returned
  as base64; instead the body field contains `"[binary, <N> bytes]"` and
  `truncated: true`.  The LLM should use the `browser` tool's screenshot
  action if it needs to see images.

---

## 8  Attachment Registry Design

### 8.1  Scoping

Attachments are keyed by `(userId, agentId, conversationId)`.
This means:

* A user can have at most **one tab attached per agent+conversation pair**.
* Different conversations for the same agent can attach different tabs.
* Different users can independently attach tabs (multi-tenant).

### 8.2  Invariants

1. **One writer:** Only the WS connection that called `tab.attach` can
   modify or use that attachment.  The broker stores a connection identifier
   and validates it on `tab.detach` and `tab.tool_result`.

2. **Cleanup on disconnect:** When a WS connection drops, all its
   attachments are removed and any pending tool calls are rejected with
   `"extension disconnected"`.

3. **Idempotent attach:** Re-attaching the same (userId, agentId,
   conversationId) from the same connection updates `tabUrl`/`tabTitle`
   in place (e.g., after navigation).  Attaching from a different
   connection replaces the old attachment (the old extension is assumed
   stale).

4. **No cross-user access:** `tab.tool_result` validates that the caller's
   userId matches the pending tool call's userId.

### 8.3  Reconnect recovery

When the side panel reconnects (WS drop + reconnect):

1. Side panel calls `tab.attachments.list` to check if a previous
   attachment is still registered (it won't be — disconnect cleanup
   removes them).
2. If no attachment found, side panel re-attaches the current tab by
   calling `tab.attach` again.
3. Any tool calls that were pending during the disconnect will have been
   rejected.  The agent's runner will see the error and may retry.

---

## 9  Milestones

### M1 — Build infrastructure & extension scaffold
*Estimated scope: webpack config, manifest, empty entry points, `make` builds it*

- [ ] Add `web/src/extension/` directory structure.
- [ ] Extend `webpack.config.js` with extension config (array export).
- [ ] Update `manifest.json` with new permissions, side_panel, dist paths.
- [ ] Create shell `sidepanel.html` + empty React root.
- [ ] Create empty `background/index.ts` that imports legacy `cdpRelay.ts`.
- [ ] Port `background.js` → `background/cdpRelay.ts` (TypeScript, no behavior change).
- [ ] Verify `npm run build` produces both web app and extension dist.
- [ ] Verify extension loads in Chrome and CDP relay still works.
- [ ] Install `chrome-types` dev dependency.

### M2 — Side panel chat UI
*Estimated scope: agent/conversation selection, basic chat, WS connection*

- [ ] `SidePanel.tsx` — root component with MUI ThemeProvider + i18n.
- [ ] `AgentSelector.tsx` — dropdown, `agents.list` RPC.
- [ ] `ConversationPicker.tsx` — list, create new, `conversations.list` RPC.
- [ ] `ChatView.tsx` — message list, text input, `conversations.send` RPC.
- [ ] Wire up `rpc.ts` for WS connection from side panel (read token from
      `chrome.storage.local`).
- [ ] Handle `conversation` events for streaming deltas, tool calls, finals.
- [ ] Test: open side panel, select agent, start conversation, send messages.

### M3 — Tab attachment registry (backend)
*Estimated scope: broker, RPC methods, events, cleanup*

- [ ] `internal/tools/tab/broker.go` — `TabToolBroker` struct + methods.
- [ ] `internal/tools/tab/context.go` — context helpers.
- [ ] `internal/api/v1api/rpc_tab.go` — `tab.attach`, `tab.detach`,
      `tab.attachments.list`, `tab.tool_result` handlers.
- [ ] Register methods in `websocket.go:dispatch()`.
- [ ] Instantiate broker in `Coordinator`, inject into runner context.
- [ ] WS disconnect cleanup hook.
- [ ] Pubsub events: `tab_tool_call`, `tab_attachment`.
- [ ] Unit tests for broker (attach, detach, resolve, cleanup, concurrent access).

### M4 — Tab attachment UI (extension)
*Estimated scope: attach/detach button, status indicator*

- [ ] `TabAttachment.tsx` — UI component.
- [ ] `tab.attach` / `tab.detach` RPC calls from side panel.
- [ ] Listen for `tab_attachment` events.
- [ ] Handle `chrome.tabs.onRemoved` and `chrome.tabs.onUpdated` for
      attached tab lifecycle.
- [ ] Re-attach on navigation (update tabUrl).
- [ ] Test: attach tab, navigate, detach, close tab — all states correct.

### M5 — Content script + page bridge
*Estimated scope: nonce scheme, fetch execution, message relay*

- [ ] `content/contentScript.ts` — nonce validation, postMessage relay.
- [ ] `content/pageBridge.ts` — nonce validation, `fetch()` execution.
- [ ] `background/toolHandler.ts` — inject scripts, route requests.
- [ ] Nonce generation in background SW, passed via `executeScript args`.
- [ ] Re-injection on tab navigation with fresh nonce.
- [ ] Test: inject into page, execute fetch, verify nonce prevents spoofing.

### M6 — Tool implementation (backend + frontend integration)
*Estimated scope: 3 tools, end-to-end flow*

- [ ] `internal/tools/tab/http_request.go` — tool definition + `Execute()`.
- [ ] `internal/tools/tab/cookies.go` — `tab.cookies.list` + `tab.cookies.get`.
- [ ] `background/cookieHandler.ts` — `chrome.cookies` wrapper.
- [ ] Side panel: listen for `tab_tool_call`, forward to background SW,
      return result via `tab.tool_result`.
- [ ] Size cap enforcement (page bridge + backend).
- [ ] Redaction rules (Set-Cookie, binary bodies).
- [ ] End-to-end test: agent calls `tab.http_request` → extension executes
      fetch in page world → result returns to agent.

### M7 — Polish & hardening
*Estimated scope: error handling, reconnect, edge cases*

- [ ] Graceful handling of extension disconnect mid-tool-call.
- [ ] Timeout handling (tool context deadline, fetch timeout, overall cap).
- [ ] Error messages: user-friendly strings for common failures
      (tab closed, page navigated, CORS error, network error).
- [ ] Side panel reconnect: re-attach tab, recover pending state.
- [ ] Multiple agent/conversation sessions (verify isolation).
- [ ] Verify existing CDP relay still works alongside new features.

---

## 10  Test Plan

### 10.1  Unit tests (Go)

| Test | Location | Coverage |
|------|----------|----------|
| Broker attach/detach | `internal/tools/tab/broker_test.go` | Register, lookup, duplicate key, cleanup |
| Broker pending resolve | same | Register pending, resolve, cancel, timeout |
| Broker disconnect cleanup | same | DetachAll removes attachments + rejects pending |
| Concurrent access | same | Parallel attach/detach/resolve with goroutines |
| Tool execute — no attachment | `internal/tools/tab/http_request_test.go` | Returns error when no tab attached |
| Tool execute — happy path | same | Mock broker, verify channel send/receive |
| Tool execute — context cancel | same | Verify pending is cleaned up |
| Size validation | same | Reject oversized request body, truncate result |
| RPC tab.attach validation | `internal/api/v1api/rpc_tab_test.go` | Missing params, invalid conversation, duplicate |
| RPC tab.tool_result | same | Unknown requestId, userId mismatch |

### 10.2  Unit tests (TypeScript)

| Test | Location | Coverage |
|------|----------|----------|
| Nonce validation | `web/src/extension/content/__tests__/` | Content script rejects wrong nonce |
| Page bridge fetch | same | Mock fetch, verify result serialization |
| Size truncation | same | Body > 512KB truncated |
| Cookie handler | `web/src/extension/background/__tests__/` | chrome.cookies mock |
| Tool request routing | same | Side panel → background → content script path |

### 10.3  Integration tests (manual)

| Scenario | Steps | Expected |
|----------|-------|----------|
| Basic HTTP GET | Attach to example.com, agent calls `tab.http_request {url: "/"}` | Returns HTML, status 200 |
| Relative URL | Attach to app.example.com, request `url: "/api/data"` | Resolves to `https://app.example.com/api/data` |
| Credentialed request | Attach to site with active session cookie, request API endpoint | Request includes session cookie, returns authenticated data |
| CORS request | Request cross-origin URL that the page itself can access | Succeeds (inherits page's CORS policy) |
| POST with body | `method: "POST", url: "/api/items", body: "{...}"` | Server receives body, returns result |
| Large response | Request endpoint returning >512KB | Body truncated, `truncated: true` |
| Binary response | Request an image URL | Body contains `"[binary, N bytes]"` |
| Cookie list | `tab.cookies.list {}` on attached tab | Returns cookies for tab URL |
| Cookie get | `tab.cookies.get {name: "session_id"}` | Returns specific cookie or null |
| Tab navigation | Navigate attached tab to new URL | Attachment updated, tools still work |
| Tab close | Close attached tab | Attachment removed, pending calls rejected |
| Extension disconnect | Kill extension process during tool call | Backend tool returns error, agent sees failure |
| Multiple conversations | Attach tab to conv A, different tab to conv B | Each conversation accesses its own tab |
| CDP relay coexistence | Attach CDP debugger AND tab HTTP to same tab | Both work independently |
| No attachment error | Agent calls `tab.http_request` without attachment | Error: "no browser tab attached to this conversation" |

### 10.4  Security tests

| Test | Steps | Expected |
|------|-------|----------|
| Page spoofing | Inject JS into page that sends fake `postMessage` with guessed nonce | Content script ignores (wrong nonce) |
| Page observing | Page listens for `postMessage` events | Page can see messages but cannot craft valid responses |
| No external communication | Page tries to connect to TeaNode WS | Blocked — no `externally_connectable`, page doesn't know URL/token |
| Cross-user isolation | Two users attach tabs concurrently | Each user only sees their own attachments and tool results |
| Token not leaked | Inspect page world variables / network requests | TeaNode API token never appears in page context |
