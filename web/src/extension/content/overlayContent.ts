/**
 * Content script for the floating overlay mode.
 * Injected into the active tab; creates a closed shadow DOM host containing
 * a drag handle, resize handle, and an iframe pointing to the extension's
 * overlay.html page.
 *
 * All drag/resize logic lives here (not in the iframe) to avoid cross-origin
 * pointer-capture issues.
 */

const MIN_WIDTH = 320;
const MIN_HEIGHT = 400;
const DEFAULT_WIDTH = 400;
const DEFAULT_HEIGHT = 600;
const MARGIN = 16;
const SAVE_DEBOUNCE_MS = 500;

interface OverlayGeometry {
  xRatio: number;
  yRatio: number;
  width: number;
  height: number;
  minimized: boolean;
}

// ---- Geometry persistence ----

function getOrigin(): string {
  try {
    return new URL(window.location.href).origin;
  } catch {
    return "unknown";
  }
}

function storageKey(): string {
  return `overlay:geometry:${getOrigin()}`;
}

async function loadGeometry(): Promise<OverlayGeometry> {
  try {
    const stored = await chrome.storage.local.get([storageKey()]);
    const geo = stored[storageKey()] as OverlayGeometry | undefined;
    if (geo && typeof geo.xRatio === "number") return geo;
  } catch { /* ignore */ }

  // Default: bottom-right corner
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  return {
    xRatio: Math.max(0, (vw - DEFAULT_WIDTH - MARGIN) / vw),
    yRatio: Math.max(0, (vh - DEFAULT_HEIGHT - MARGIN) / vh),
    width: DEFAULT_WIDTH,
    height: DEFAULT_HEIGHT,
    minimized: false,
  };
}

let saveTimer: ReturnType<typeof setTimeout> | null = null;

function saveGeometry(geo: OverlayGeometry): void {
  if (saveTimer) clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    chrome.storage.local.set({ [storageKey()]: geo }).catch(() => {});
  }, SAVE_DEBOUNCE_MS);
}

function saveGeometryImmediate(geo: OverlayGeometry): void {
  if (saveTimer) clearTimeout(saveTimer);
  chrome.storage.local.set({ [storageKey()]: geo }).catch(() => {});
}

// ---- Clamp helpers ----

function clamp(val: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, val));
}

function clampRect(
  x: number, y: number, w: number, h: number,
): { x: number; y: number; w: number; h: number } {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  w = clamp(w, MIN_WIDTH, vw - MARGIN);
  h = clamp(h, MIN_HEIGHT, vh - MARGIN);
  x = clamp(x, 0, vw - w);
  y = clamp(y, 0, vh - h);
  return { x, y, w, h };
}

// ---- Build the overlay ----

function createOverlay(): void {
  // Prevent double injection
  if (document.querySelector("teanode-overlay")) return;

  const host = document.createElement("teanode-overlay");
  const shadow = host.attachShadow({ mode: "closed" });

  // Styles inside shadow DOM
  const style = document.createElement("style");
  style.textContent = `
    :host {
      position: fixed !important;
      z-index: 2147483647;
      pointer-events: none;
      top: 0; left: 0;
      width: 100vw; height: 100vh;
      margin: 0 !important; padding: 0 !important;
      border: none !important;
    }
    .overlay-container {
      pointer-events: auto;
      position: absolute;
      border-radius: 12px;
      box-shadow: 0 8px 32px rgba(0,0,0,0.24);
      overflow: hidden;
      display: flex;
      flex-direction: column;
      background: #1e1e2e;
    }
    .overlay-container.minimized {
      display: none;
    }
    .drag-handle {
      cursor: grab;
      height: 32px;
      background: #181825;
      display: flex;
      align-items: center;
      padding: 0 8px;
      user-select: none;
      flex-shrink: 0;
      gap: 8px;
    }
    .drag-handle:active { cursor: grabbing; }
    .drag-handle .title {
      flex: 1;
      color: #cdd6f4;
      font-size: 12px;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      font-weight: 600;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .drag-handle button {
      background: none;
      border: none;
      color: #a6adc8;
      cursor: pointer;
      width: 24px;
      height: 24px;
      border-radius: 6px;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 0;
      font-size: 14px;
      line-height: 1;
    }
    .drag-handle button:hover {
      background: rgba(205, 214, 244, 0.1);
      color: #cdd6f4;
    }
    iframe {
      border: none;
      flex: 1;
      width: 100%;
      min-height: 0;
    }
    .resize-handle {
      position: absolute;
      bottom: 0; right: 0;
      width: 16px; height: 16px;
      cursor: nwse-resize;
      pointer-events: auto;
    }
    .resize-handle::after {
      content: "";
      position: absolute;
      bottom: 3px; right: 3px;
      width: 8px; height: 8px;
      border-right: 2px solid rgba(205, 214, 244, 0.3);
      border-bottom: 2px solid rgba(205, 214, 244, 0.3);
    }
    .minimized-btn {
      pointer-events: auto;
      position: absolute;
      width: 44px;
      height: 44px;
      border-radius: 12px;
      background: linear-gradient(135deg, #2d8c5a, #1e6b43);
      box-shadow: 0 4px 16px rgba(0,0,0,0.3);
      cursor: pointer;
      border: none;
      display: none;
      align-items: center;
      justify-content: center;
      padding: 0;
      color: white;
      font-size: 20px;
      font-weight: 700;
    }
    .minimized-btn:hover {
      transform: scale(1.08);
    }
    .minimized-btn.visible {
      display: flex;
    }
  `;
  shadow.appendChild(style);

  // Container
  const container = document.createElement("div");
  container.className = "overlay-container";

  // Drag handle
  const handle = document.createElement("div");
  handle.className = "drag-handle";

  const title = document.createElement("span");
  title.className = "title";
  title.textContent = "TeaNode";
  handle.appendChild(title);

  const minimizeBtn = document.createElement("button");
  minimizeBtn.textContent = "−";
  minimizeBtn.title = "Minimize";
  handle.appendChild(minimizeBtn);

  const closeBtn = document.createElement("button");
  closeBtn.textContent = "✕";
  closeBtn.title = "Close";
  handle.appendChild(closeBtn);

  container.appendChild(handle);

  // Iframe
  const iframe = document.createElement("iframe");
  iframe.src = `chrome-extension://${chrome.runtime.id}/dist/overlay.html`;
  iframe.allow = "";
  container.appendChild(iframe);

  // Resize handle
  const resizeHandle = document.createElement("div");
  resizeHandle.className = "resize-handle";
  container.appendChild(resizeHandle);

  // Minimized button (shown when minimized)
  const minBtn = document.createElement("button");
  minBtn.className = "minimized-btn";
  minBtn.textContent = "T";
  minBtn.title = "Restore TeaNode";

  shadow.appendChild(container);
  shadow.appendChild(minBtn);
  document.documentElement.appendChild(host);

  // ---- State ----
  let geo: OverlayGeometry;
  let dragging = false;
  let resizing = false;
  let dragOffsetX = 0;
  let dragOffsetY = 0;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartW = 0;
  let resizeStartH = 0;

  function applyGeo(): void {
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const { x, y, w, h } = clampRect(
      geo.xRatio * vw, geo.yRatio * vh, geo.width, geo.height,
    );
    container.style.left = `${x}px`;
    container.style.top = `${y}px`;
    container.style.width = `${w}px`;
    container.style.height = `${h}px`;

    if (geo.minimized) {
      container.classList.add("minimized");
      minBtn.classList.add("visible");
      minBtn.style.left = `${x}px`;
      minBtn.style.top = `${y}px`;
    } else {
      container.classList.remove("minimized");
      minBtn.classList.remove("visible");
    }
  }

  function updateGeoFromPosition(): void {
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    geo.xRatio = parseFloat(container.style.left) / vw;
    geo.yRatio = parseFloat(container.style.top) / vh;
    geo.width = container.offsetWidth;
    geo.height = container.offsetHeight;
  }

  // Init geometry from storage then render
  loadGeometry().then((stored) => {
    geo = stored;
    applyGeo();
  });

  // ---- Drag ----
  handle.addEventListener("pointerdown", (e: PointerEvent) => {
    if ((e.target as HTMLElement).tagName === "BUTTON") return;
    dragging = true;
    dragOffsetX = e.clientX - container.offsetLeft;
    dragOffsetY = e.clientY - container.offsetTop;
    handle.setPointerCapture(e.pointerId);
    e.preventDefault();
  });

  handle.addEventListener("pointermove", (e: PointerEvent) => {
    if (!dragging) return;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const w = container.offsetWidth;
    const h = container.offsetHeight;
    const x = clamp(e.clientX - dragOffsetX, 0, vw - w);
    const y = clamp(e.clientY - dragOffsetY, 0, vh - h);
    container.style.left = `${x}px`;
    container.style.top = `${y}px`;
  });

  handle.addEventListener("pointerup", () => {
    if (!dragging) return;
    dragging = false;
    updateGeoFromPosition();
    saveGeometry(geo);
  });

  // ---- Resize ----
  resizeHandle.addEventListener("pointerdown", (e: PointerEvent) => {
    resizing = true;
    resizeStartX = e.clientX;
    resizeStartY = e.clientY;
    resizeStartW = container.offsetWidth;
    resizeStartH = container.offsetHeight;
    resizeHandle.setPointerCapture(e.pointerId);
    e.preventDefault();
  });

  resizeHandle.addEventListener("pointermove", (e: PointerEvent) => {
    if (!resizing) return;
    const dx = e.clientX - resizeStartX;
    const dy = e.clientY - resizeStartY;
    const { w, h } = clampRect(
      container.offsetLeft,
      container.offsetTop,
      resizeStartW + dx,
      resizeStartH + dy,
    );
    container.style.width = `${w}px`;
    container.style.height = `${h}px`;
  });

  resizeHandle.addEventListener("pointerup", () => {
    if (!resizing) return;
    resizing = false;
    updateGeoFromPosition();
    saveGeometry(geo);
  });

  // ---- Minimize / Restore / Close ----
  minimizeBtn.addEventListener("click", () => {
    geo.minimized = true;
    applyGeo();
    saveGeometryImmediate(geo);
  });

  minBtn.addEventListener("click", () => {
    geo.minimized = false;
    applyGeo();
    saveGeometryImmediate(geo);
  });

  closeBtn.addEventListener("click", () => {
    saveGeometryImmediate(geo);
    host.remove();
  });

  // ---- Window resize: clamp overlay ----
  window.addEventListener("resize", () => {
    if (geo) applyGeo();
  });

  // ---- MutationObserver: re-inject if page removes the host ----
  const observer = new MutationObserver(() => {
    if (!document.documentElement.contains(host) && document.documentElement) {
      document.documentElement.appendChild(host);
    }
  });
  observer.observe(document.documentElement, { childList: true });

  // ---- Listen for toggle message from background SW ----
  chrome.runtime.onMessage.addListener((message: { type: string }) => {
    if (message.type === "tn:toggle-overlay") {
      if (geo.minimized) {
        geo.minimized = false;
        applyGeo();
        saveGeometryImmediate(geo);
      } else {
        geo.minimized = true;
        applyGeo();
        saveGeometryImmediate(geo);
      }
    }
    if (message.type === "tn:close-overlay") {
      saveGeometryImmediate(geo);
      host.remove();
      observer.disconnect();
    }
    return undefined;
  });

  // ---- beforeunload: save geometry ----
  window.addEventListener("beforeunload", () => {
    if (geo) saveGeometryImmediate(geo);
  });
}

// ---- Entry point ----
// If the overlay host already exists, toggle it. Otherwise create it.
if (document.querySelector("teanode-overlay")) {
  // Already exists — the background SW should send toggle messages.
  // This handles re-injection on navigation.
} else {
  createOverlay();
}
