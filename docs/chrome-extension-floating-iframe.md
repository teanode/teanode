# Chrome Extension: Floating Iframe Overlay

Design proposal for an alternative to the Chrome side panel — a draggable, resizable
iframe overlay injected into the active webpage.

## Motivation

The Chrome side panel has limitations:

- It reduces the webpage's viewport width (the browser literally resizes the page).
- Only one side panel can be active per window (conflicts with other extensions).
- Position is fixed (right edge); cannot be moved or resized.
- Some users prefer a floating widget that can be repositioned freely.

A floating iframe overlay avoids these issues while keeping the same chat UI.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│  Web Page (MAIN world)                                      │
│                                                             │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  Shadow DOM Host (content script, ISOLATED world)      │ │
│  │                                                        │ │
│  │  ┌──────────────────────────────────────────────────┐  │ │
│  │  │  Drag Handle  ──  Minimize / Close buttons       │  │ │
│  │  ├──────────────────────────────────────────────────┤  │ │
│  │  │                                                  │  │ │
│  │  │   <iframe src="chrome-extension://EXT_ID/        │  │ │
│  │  │            dist/overlay.html">                   │  │ │
│  │  │                                                  │  │ │
│  │  │   (Same React chat UI as side panel)             │  │ │
│  │  │                                                  │  │ │
│  │  ├──────────────────────────────────────────────────┤  │ │
│  │  │  Resize handle (bottom-right corner)             │  │ │
│  │  └──────────────────────────────────────────────────┘  │ │
│  │                                                        │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Layers

| Layer | World | Role |
|---|---|---|
| **Web page** | MAIN | Unmodified; cannot see or interact with the overlay |
| **Content script** | ISOLATED | Injects shadow DOM host; manages drag/resize; owns the iframe element |
| **Iframe** | Extension origin | `chrome-extension://<id>/dist/overlay.html` — full React app with WS connection to TeaNode |
| **Background SW** | Extension | Coordinates lifecycle, stores position/size, relays tool calls |

---

## Rendering the Iframe Content

### Extension page URL

The iframe `src` is an extension-bundled HTML page:

```
chrome-extension://<extension-id>/dist/overlay.html
```

This is a new webpack entry point (`overlay.js`) that renders the same `SidePanel` /
`ChatView` React components. The key difference from the side panel entry point is
layout: the overlay variant uses `100vw × 100vh` to fill the iframe, and hides any
side-panel-specific chrome (e.g. "Open in new tab" link).

Using an extension page URL means:
- The iframe runs in the **extension origin**, fully isolated from the page.
- It can use `chrome.storage`, `chrome.runtime`, and all extension APIs.
- The page's JavaScript cannot access `iframe.contentWindow` or
  `iframe.contentDocument` (cross-origin enforcement).
- The token never leaves the extension origin.

### Manifest declaration

```jsonc
// manifest.json
"web_accessible_resources": [
  {
    "resources": ["dist/overlay.html", "dist/overlay.js"],
    "matches": ["<all_urls>"]
  }
]
```

This is required for the content script to set the iframe `src` to an extension URL
from within an arbitrary page.

### postMessage bridge

The content script (ISOLATED world) and the iframe (extension origin) communicate via
`window.postMessage` on the iframe's `contentWindow`:

```
Content script                          Iframe (overlay.html)
─────────────                           ─────────────────────
iframe.contentWindow.postMessage(       window.addEventListener("message",
  { type: "tn:toggle-minimize" },         (e) => {
  "chrome-extension://<id>"               if (e.origin !== `chrome-extension://${chrome.runtime.id}`) return;
)                                         // handle
                                        })
```

Messages needed:

| Direction | Type | Purpose |
|---|---|---|
| Content → Iframe | `tn:set-visible` | Show/hide the chat UI (minimize) |
| Iframe → Content | `tn:resize-request` | Chat UI requests a different default size |
| Iframe → Content | `tn:close` | User clicked close inside the chat |
| Content → Iframe | `tn:theme-hint` | Pass page background color for theme matching |

Most communication bypasses the content script entirely — the iframe talks directly to
the background SW via `chrome.runtime` APIs, the same way the side panel does today.

---

## Position & Size Persistence

### Storage schema

```typescript
interface OverlayGeometry {
  // Stored as fractions of viewport (0–1) so they adapt to different screen sizes
  xRatio: number;   // left edge as fraction of viewport width
  yRatio: number;   // top edge as fraction of viewport height
  width: number;    // px
  height: number;   // px
  minimized: boolean;
}

// Keyed by origin: "overlay:geometry:<origin>"
// e.g. "overlay:geometry:https://github.com"
```

Using `chrome.storage.local` (same as token storage). Keyed per origin so the overlay
remembers its position on each site independently.

### Save strategy

- **Debounced writes**: during drag/resize, buffer position changes and write to
  storage at most once per 500 ms.
- **On close/minimize**: immediate write.
- **On page unload**: write via `beforeunload` event from content script (best-effort;
  the debounced write covers most cases).

### Viewport adaptation

On `window.resize`:

1. Clamp the overlay so it stays fully within the viewport.
2. If the overlay was anchored to a corner (right edge within 20 px of viewport right),
   maintain that anchor after resize.
3. Store updated ratios.

Default geometry (first open, no stored value):

```
width: 400px, height: 600px
position: bottom-right corner, 16px margin
```

---

## Z-Index, Pointer Events, and Page Isolation

### Shadow DOM encapsulation

The content script creates a shadow DOM host to prevent page CSS from affecting the
overlay:

```typescript
const host = document.createElement("teanode-overlay");
const shadow = host.attachShadow({ mode: "closed" });
// "closed" prevents page JS from accessing shadow.host.shadowRoot
document.documentElement.appendChild(host);
```

Using `closed` mode means `host.shadowRoot` returns `null` to page scripts.

### Styling

All overlay styles live inside the shadow DOM:

```css
:host {
  position: fixed !important;
  z-index: 2147483647; /* max 32-bit int — above all page content */
  pointer-events: none; /* pass through everywhere except the overlay itself */
  top: 0; left: 0;
  width: 100vw; height: 100vh;
}

.overlay-container {
  pointer-events: auto; /* re-enable on the actual widget */
  position: absolute;
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.24);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.drag-handle {
  cursor: grab;
  height: 32px;
  background: #1a1a2e;
  display: flex;
  align-items: center;
  padding: 0 8px;
  user-select: none;
}

iframe {
  border: none;
  flex: 1;
  width: 100%;
}

.resize-handle {
  position: absolute;
  bottom: 0; right: 0;
  width: 16px; height: 16px;
  cursor: nwse-resize;
}
```

Key points:

- **`pointer-events: none`** on the host means clicks pass through to the page
  everywhere except the overlay container itself (`pointer-events: auto`).
- **`z-index: 2147483647`** ensures the overlay sits above page content, modals, and
  most other overlays. If a page uses the same value (rare), the overlay still wins
  because it is appended to `documentElement` (later in DOM order).
- **`position: fixed`** keeps the overlay viewport-relative regardless of page scroll.

### Drag implementation

Drag is handled entirely in the content script (on the shadow DOM elements), not inside
the iframe. This avoids cross-origin mouse-capture issues:

```typescript
let dragging = false;
let offsetX: number, offsetY: number;

handle.addEventListener("pointerdown", (e) => {
  dragging = true;
  offsetX = e.clientX - container.offsetLeft;
  offsetY = e.clientY - container.offsetTop;
  handle.setPointerCapture(e.pointerId);
});

document.addEventListener("pointermove", (e) => {
  if (!dragging) return;
  const x = clamp(e.clientX - offsetX, 0, window.innerWidth - container.offsetWidth);
  const y = clamp(e.clientY - offsetY, 0, window.innerHeight - container.offsetHeight);
  container.style.left = `${x}px`;
  container.style.top = `${y}px`;
});

document.addEventListener("pointerup", () => {
  dragging = false;
  saveGeometry(); // debounced
});
```

Resize follows the same pattern on the resize handle, adjusting `width`/`height`.

### Preventing page interference

| Threat | Mitigation |
|---|---|
| Page CSS leaking in | Closed shadow DOM; all styles scoped inside shadow |
| Page JS accessing iframe | Cross-origin (extension URL); `contentWindow` access blocked by browser |
| Page JS removing the host element | MutationObserver on `document.documentElement` re-injects if removed |
| Page capturing pointer events | `setPointerCapture` on drag handle; overlay z-index above page |
| Page `window.postMessage` spoofing | Origin check on all messages; only accept `chrome-extension://<id>` |

---

## CSP Restrictions

### The problem

Some pages set strict `Content-Security-Policy` headers, e.g.:

```
frame-src 'self' https://trusted.com;
```

This would block the `chrome-extension://` iframe from loading.

### Why this is a non-issue for Manifest V3

**Chrome exempts extension content scripts and their injected elements from page CSP.**

Specifically:
- Content scripts run in an isolated world with their own CSP (the extension's CSP).
- Iframes with `chrome-extension://` URLs injected by content scripts are **not**
  subject to the page's `frame-src` directive — the browser recognizes them as
  extension-controlled.
- This has been the behavior since Manifest V2 and is explicitly maintained in V3.

Reference: [Chrome Extensions CSP documentation](https://developer.chrome.com/docs/extensions/develop/migrate/improve-security#content-scripts)

### Edge case: `sandbox` attribute on page iframes

If the page itself is inside a sandboxed iframe (e.g. `sandbox="allow-scripts"`), the
content script may not run at all. This is rare and also affects the existing side panel
approach (content scripts for tab tools wouldn't work either). No mitigation needed
beyond the existing behavior.

---

## Lifecycle & Toggle

### Opening the overlay

When the user clicks the extension icon:

1. Background SW receives `chrome.action.onClicked`.
2. Background sends a message to the content script in the active tab:
   `{ type: "tn:toggle-overlay" }`.
3. If the content script is not yet injected, background injects it first via
   `chrome.scripting.executeScript`.
4. Content script checks if the shadow DOM host exists:
   - **No**: create it, create the iframe, load stored geometry, show.
   - **Yes, visible**: minimize or close (based on user preference).
   - **Yes, minimized**: restore.

### Minimized state

When minimized, the overlay collapses to a small floating button (the TeaNode icon,
~48×48 px) in the same corner. Clicking it restores the full overlay. The iframe
remains loaded but hidden (`display: none` on the container, `visibility: visible` on
the icon button) so the WebSocket stays connected and messages keep arriving.

### Page navigation (SPA)

For single-page apps that change URL without full navigation:

- The content script and shadow DOM survive SPA navigations (they're in the DOM).
- The iframe WebSocket connection persists.
- Tab attach/detach may need to update the URL — the background SW listens to
  `chrome.tabs.onUpdated` and notifies the iframe.

For full page navigations, the content script is re-injected by the background SW's
`chrome.tabs.onUpdated` listener (same as existing behavior for tab tools). Stored
geometry is restored from `chrome.storage.local`.

---

## Webpack Build Changes

Add a new entry point:

```javascript
// webpack.config.js (extensionConfig)
entry: {
  background: "./src/extension/background/index.ts",
  sidepanel: "./src/extension/sidepanel/index.tsx",
  overlay:   "./src/extension/overlay/index.tsx",      // NEW
  "content-script": "./src/extension/content/contentScript.ts",
  "overlay-content": "./src/extension/content/overlayContent.ts", // NEW
  "page-bridge": "./src/extension/content/pageBridge.ts",
},
```

New files:

| File | Purpose |
|---|---|
| `web/src/extension/overlay/index.tsx` | React entry point; renders same ChatView in full-viewport layout |
| `web/src/extension/overlay/overlay.html` | Minimal HTML shell (like `sidepanel.html`) |
| `web/src/extension/content/overlayContent.ts` | Content script that creates shadow DOM, drag/resize, iframe |
| `assets/chrome-extension/manifest.json` | Add `web_accessible_resources` for overlay files |

The overlay entry point can share nearly all code with the side panel entry point.
The main difference is the absence of side-panel-specific UI and the addition of
`postMessage` listeners for content-script communication.

---

## Tradeoffs vs Side Panel

| Aspect | Side Panel | Floating Iframe |
|---|---|---|
| **Viewport impact** | Shrinks page width | No impact (overlays on top) |
| **Position** | Fixed right edge | User-chosen, draggable |
| **Size** | Fixed width, full height | User-chosen, resizable |
| **Persistence** | Browser manages | We manage (chrome.storage) |
| **Multi-extension conflict** | Only one side panel per window | No conflict |
| **Discoverability** | Native Chrome UI; obvious location | Must remember icon click |
| **Page interference risk** | None (separate browser pane) | Minimal but nonzero (z-index wars, page removing DOM) |
| **Implementation complexity** | Low (browser handles rendering) | Medium (drag, resize, shadow DOM, geometry persistence) |
| **Mobile / tablet** | Side panel may not be available | Overlay may be awkward on small screens |
| **Accessibility** | Browser provides focus management | Must implement keyboard focus trap and escape-to-close |
| **Performance** | Separate process | Shares renderer with page (iframe is out-of-process in Chrome, but compositing overhead exists) |
| **Security** | Extension page in browser-managed pane | Extension page in iframe; same origin isolation, but MutationObserver needed to prevent removal |

### Recommendation

Offer both modes. Default to side panel (simpler, more robust), with a toggle in the
extension options page to switch to floating overlay. The two modes share the same
React chat UI — only the hosting shell differs.

---

## Open Questions

1. **Keyboard shortcut**: Should the overlay have a global shortcut
   (`chrome.commands`) to toggle visibility? (e.g. `Ctrl+Shift+T`)
2. **Multiple monitors**: Should geometry be stored per-monitor or per-origin only?
   (Per-origin is simpler and sufficient for most users.)
3. **Dark/light theme**: Should the overlay detect the page's color scheme and match?
   Or always follow the user's TeaNode theme preference?
4. **Resize minimum**: What are sensible minimum dimensions? Suggest 320×400 px.
