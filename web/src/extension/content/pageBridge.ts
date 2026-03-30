/**
 * Page bridge injected into MAIN world via chrome.scripting.executeScript.
 * Receives fetch requests from the content script (ISOLATED world) via postMessage,
 * executes fetch() with the page's cookies/credentials, and returns the result.
 *
 * Also handles generic page actions: localStorage, DOM, and eval.
 *
 * This file is compiled as an IIFE by webpack. The nonce is injected at execution
 * time via chrome.scripting.executeScript({ args: [nonce], func: ... }).
 * Since we use webpack bundling, we pass the nonce via a global set before injection.
 */

const MAX_BODY_SIZE = 128 * 1024; // 128 KB (matches server-side cap)
const MAX_HEADERS_SIZE = 64 * 1024; // 64 KB
const MAX_SNAPSHOT_SIZE = 128 * 1024; // 128 KB (matches server-side cap)
const MAX_LOCALSTORAGE_TOTAL = 512 * 1024; // 512 KB
const MAX_EVAL_RESULT_SIZE = 512 * 1024; // 512 KB
const MAX_QUERY_RESULTS = 25;
const MAX_ELEMENT_CONTENT = 16 * 1024; // 16 KB per element in querySelector
const MAX_ATTR_VALUE = 200; // truncate long attribute values

// The nonce is set as a global by the background SW before injecting this script.
const NONCE: string = (globalThis as any).__tn_nonce || "";

/**
 * Safely serialize a value to JSON, handling cycles, functions, and size caps.
 */
function safeStringify(
  value: unknown,
  maxSize: number,
): { json: string; truncated: boolean } {
  const seen = new WeakSet();

  function replacer(_key: string, value: unknown): unknown {
    if (value === undefined) return null;
    if (typeof value === "function") return "[Function]";
    if (typeof value === "symbol") return value.toString();
    if (typeof value === "bigint") return value.toString();
    if (value instanceof Error) {
      return { message: value.message, name: value.name, stack: value.stack };
    }
    if (value instanceof RegExp) return value.toString();
    if (value !== null && typeof value === "object") {
      if (seen.has(value)) return "[Circular]";
      seen.add(value);
    }
    return value;
  }

  let json: string;
  try {
    json = JSON.stringify(value, replacer);
  } catch {
    return { json: '"[unserializable]"', truncated: false };
  }

  if (json.length > maxSize) {
    return { json: json.slice(0, maxSize), truncated: true };
  }

  return { json, truncated: false };
}

if (NONCE) {
  window.addEventListener("message", async (event: MessageEvent) => {
    if (event.source !== window) return;
    if (event.data?.__tn !== NONCE) return;

    if (event.data?.type === "req") {
      await handleFetchRequest(event.data);
    } else if (event.data?.type === "action_req") {
      await handleActionRequest(event.data);
    } else if (event.data?.type === "steps_req") {
      await handleStepsRequest(event.data);
    }
  });
}

async function handleFetchRequest(data: any): Promise<void> {
  const { id, payload } = data;
  const { method, url, headers, body, timeoutMs } = payload;

  const start = performance.now();

  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs || 30000);

    const response = await fetch(url, {
      method: method || "GET",
      headers: headers || {},
      body: body || undefined,
      credentials: "include",
      signal: controller.signal,
    });
    clearTimeout(timer);

    const respHeaders: Record<string, string> = {};
    let headersSize = 0;
    response.headers.forEach((v, k) => {
      // Exclude Set-Cookie (security) and cap total headers size.
      if (k.toLowerCase() === "set-cookie") return;
      const entrySize = k.length + v.length;
      if (headersSize + entrySize > MAX_HEADERS_SIZE) return;
      headersSize += entrySize;
      respHeaders[k] = v;
    });

    const contentType = (
      response.headers.get("content-type") || ""
    ).toLowerCase();
    const isBinary =
      contentType.startsWith("image/") ||
      contentType.startsWith("audio/") ||
      contentType.startsWith("video/") ||
      contentType === "application/octet-stream" ||
      contentType.startsWith("application/pdf");

    let respBody: string;
    let truncated = false;

    if (isBinary) {
      const blob = await response.blob();
      respBody = `[binary, ${blob.size} bytes]`;
      truncated = true;
    } else {
      respBody = await response.text();
      if (respBody.length > MAX_BODY_SIZE) {
        respBody = respBody.slice(0, MAX_BODY_SIZE);
        truncated = true;
      }
    }

    const durationMs = Math.round(performance.now() - start);

    window.postMessage({
      __tn: NONCE,
      type: "res",
      id,
      result: {
        status: response.status,
        statusText: response.statusText,
        headers: respHeaders,
        body: respBody,
        url: response.url,
        truncated,
        durationMs,
      },
    });
  } catch (err) {
    window.postMessage({
      __tn: NONCE,
      type: "res",
      id,
      error: String(err),
    });
  }
}

async function handleActionRequest(data: any): Promise<void> {
  const { id, action, params } = data;

  try {
    let result: unknown;

    switch (action) {
      case "getLocalStorage":
        result = handleGetLocalStorage(params);
        break;
      case "setLocalStorage":
        result = handleSetLocalStorage(params);
        break;
      case "removeLocalStorage":
        result = handleRemoveLocalStorage(params);
        break;
      case "snapshot":
        result = handleSnapshot(params);
        break;
      case "querySelector":
        result = handleQuerySelector(params);
        break;
      case "eval":
        result = await handleEval(params);
        break;
      case "clickRef":
        result = handleClickRef(params);
        break;
      case "typeRef":
        result = await handleTypeRef(params);
        break;
      case "hoverRef":
        result = handleHoverRef(params);
        break;
      case "selectOption":
        result = handleSelectOption(params);
        break;
      case "wait":
        result = await handleWait(params);
        break;
      default:
        throw new Error(`unknown page action: ${action}`);
    }

    window.postMessage({
      __tn: NONCE,
      type: "action_res",
      id,
      result,
    });
  } catch (err) {
    window.postMessage({
      __tn: NONCE,
      type: "action_res",
      id,
      error: String(err),
    });
  }
}

// ---- localStorage handlers ----

function handleGetLocalStorage(params: Record<string, unknown>): unknown {
  const key = params.key as string | undefined;

  if (key != null && key !== "") {
    const value = localStorage.getItem(key);
    return { value };
  }

  // Return all entries, capped by total size.
  const entries: Record<string, string> = {};
  let totalSize = 0;
  let truncated = false;

  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (k == null) continue;
    const v = localStorage.getItem(k) ?? "";
    const entrySize = k.length + v.length;
    if (totalSize + entrySize > MAX_LOCALSTORAGE_TOTAL) {
      truncated = true;
      break;
    }
    totalSize += entrySize;
    entries[k] = v;
  }

  return { entries, truncated };
}

function handleSetLocalStorage(params: Record<string, unknown>): unknown {
  const key = params.key as string;
  const value = params.value as string;
  localStorage.setItem(key, value);
  return { ok: true };
}

function handleRemoveLocalStorage(params: Record<string, unknown>): unknown {
  const key = params.key as string;
  localStorage.removeItem(key);
  return { ok: true };
}

// ---- DOM handlers ----

function handleSnapshot(params: Record<string, unknown>): unknown {
  const mode = (params.mode as string) || "html";

  switch (mode) {
    case "text":
      return snapshotText();
    case "accessibility":
      return snapshotAccessibility();
    case "interactive":
      return snapshotInteractive();
    default:
      return snapshotHtml();
  }
}

function snapshotHtml(): { html: string; truncated: boolean } {
  const clone = document.documentElement.cloneNode(true) as HTMLElement;

  // Strip elements that are noise for understanding page structure.
  for (const tag of ["script", "style", "noscript", "link[rel=stylesheet]"]) {
    clone.querySelectorAll(tag).forEach((element) => element.remove());
  }

  // Strip noisy attributes and truncate long values.
  const walker = document.createTreeWalker(clone, NodeFilter.SHOW_ELEMENT);
  let node: Element | null = walker.currentNode as Element;
  while (node) {
    const attrs = Array.from(node.attributes || []);
    for (const attr of attrs) {
      if (attr.name === "style") {
        node.removeAttribute("style");
        continue;
      }
      if (attr.name === "d" && node.tagName === "path") {
        node.setAttribute("d", "[path]");
        continue;
      }
      if (attr.value.length > MAX_ATTR_VALUE) {
        node.setAttribute(attr.name, attr.value.slice(0, MAX_ATTR_VALUE) + "…");
      }
    }
    node = walker.nextNode() as Element | null;
  }

  let html = clone.outerHTML;
  html = html.replace(/\s{2,}/g, " ");

  let truncated = false;
  if (html.length > MAX_SNAPSHOT_SIZE) {
    html = html.slice(0, MAX_SNAPSHOT_SIZE);
    truncated = true;
  }

  return { html, truncated };
}

function snapshotText(): { text: string; truncated: boolean } {
  const lines: string[] = [];

  // Page title
  if (document.title) {
    lines.push(`# ${document.title}`);
    lines.push("");
  }

  // Walk visible text nodes, using semantic elements for structure hints.
  const walker = document.createTreeWalker(
    document.body || document.documentElement,
    NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
    {
      acceptNode(node: Node) {
        if (node.nodeType === Node.ELEMENT_NODE) {
          const element = node as HTMLElement;
          const tag = element.tagName.toLowerCase();
          // Skip invisible and non-content elements.
          if (
            tag === "script" ||
            tag === "style" ||
            tag === "noscript" ||
            tag === "svg" ||
            tag === "template"
          )
            return NodeFilter.FILTER_REJECT;
          if (element.hidden || element.getAttribute("aria-hidden") === "true")
            return NodeFilter.FILTER_REJECT;
          const style = getComputedStyle(element);
          if (style.display === "none" || style.visibility === "hidden")
            return NodeFilter.FILTER_REJECT;
        }
        return NodeFilter.FILTER_ACCEPT;
      },
    },
  );

  let current: Node | null = walker.currentNode;
  while (current) {
    if (current.nodeType === Node.ELEMENT_NODE) {
      const element = current as HTMLElement;
      const tag = element.tagName.toLowerCase();
      // Add structural markers for headings and list items.
      if (/^h[1-6]$/.test(tag)) {
        const level = parseInt(tag[1]);
        const text = (element.textContent || "").trim();
        if (text) {
          lines.push("");
          lines.push(`${"#".repeat(level)} ${text}`);
        }
        // Skip children since we grabbed textContent.
        let next: Node | null = walker.nextSibling();
        while (!next) {
          if (!walker.parentNode()) break;
          next = walker.nextSibling();
        }
        current = next;
        continue;
      }
    } else if (current.nodeType === Node.TEXT_NODE) {
      const text = (current.textContent || "").trim();
      if (text) {
        lines.push(text);
      }
    }
    current = walker.nextNode();
  }

  let text = lines.join("\n").replace(/\n{3,}/g, "\n\n");
  let truncated = false;
  if (text.length > MAX_SNAPSHOT_SIZE) {
    text = text.slice(0, MAX_SNAPSHOT_SIZE);
    truncated = true;
  }

  return { text, truncated };
}

/** ARIA role mapping for common HTML elements. */
const IMPLICIT_ROLES: Record<string, string> = {
  a: "link",
  button: "button",
  input: "textbox",
  select: "combobox",
  textarea: "textbox",
  img: "img",
  nav: "navigation",
  main: "main",
  header: "banner",
  footer: "contentinfo",
  aside: "complementary",
  form: "form",
  table: "table",
  ul: "list",
  ol: "list",
  li: "listitem",
  h1: "heading",
  h2: "heading",
  h3: "heading",
  h4: "heading",
  h5: "heading",
  h6: "heading",
};

function snapshotAccessibility(): { text: string; truncated: boolean } {
  const lines: string[] = [];
  if (document.title) {
    lines.push(`page: ${document.title}`);
  }

  function walk(element: Element, depth: number): void {
    const tag = element.tagName.toLowerCase();

    // Skip non-content elements.
    if (
      tag === "script" ||
      tag === "style" ||
      tag === "noscript" ||
      tag === "template"
    )
      return;
    if (
      (element as HTMLElement).hidden ||
      element.getAttribute("aria-hidden") === "true"
    )
      return;

    const role = element.getAttribute("role") || IMPLICIT_ROLES[tag] || "";
    const label =
      element.getAttribute("aria-label") ||
      element.getAttribute("alt") ||
      element.getAttribute("title") ||
      element.getAttribute("placeholder") ||
      "";

    // Determine accessible name: label or direct text content (not children's).
    let name = label;
    if (!name) {
      // Use direct text nodes only (not nested element text).
      const directText = Array.from(element.childNodes)
        .filter((n) => n.nodeType === Node.TEXT_NODE)
        .map((n) => (n.textContent || "").trim())
        .join(" ")
        .trim();
      if (directText) name = directText;
    }

    // Build the node representation.
    const parts: string[] = [];
    if (role) parts.push(role);
    else if (tag !== "div" && tag !== "span") parts.push(tag);

    // Add heading level.
    if (/^h[1-6]$/.test(tag)) {
      parts.push(`level=${tag[1]}`);
    }

    // Add relevant state attributes.
    if (element.getAttribute("disabled") !== null) parts.push("disabled");
    if (element.getAttribute("aria-expanded") !== null)
      parts.push(`expanded=${element.getAttribute("aria-expanded")}`);
    if (element.getAttribute("aria-checked") !== null)
      parts.push(`checked=${element.getAttribute("aria-checked")}`);
    if (element.getAttribute("aria-selected") !== null)
      parts.push(`selected=${element.getAttribute("aria-selected")}`);
    if ((element as HTMLInputElement).type && tag === "input")
      parts.push(`type=${(element as HTMLInputElement).type}`);
    if (
      (element as HTMLInputElement).value &&
      (tag === "input" || tag === "textarea")
    )
      parts.push(
        `value="${(element as HTMLInputElement).value.slice(0, 100)}"`,
      );
    if (element.getAttribute("href"))
      parts.push(`href="${element.getAttribute("href")!.slice(0, 150)}"`);

    if (name) parts.push(`"${name.slice(0, 200)}"`);

    // Only emit nodes that have a role, label, or are interactive/semantic.
    if (parts.length > 0 && (role || label || tag !== "div")) {
      const indent = "  ".repeat(depth);
      lines.push(`${indent}${parts.join(" ")}`);
    }

    // Recurse into children.
    for (const child of element.children) {
      walk(child, role || label ? depth + 1 : depth);
    }
  }

  walk(document.body || document.documentElement, 0);

  let text = lines.join("\n");
  let truncated = false;
  if (text.length > MAX_SNAPSHOT_SIZE) {
    text = text.slice(0, MAX_SNAPSHOT_SIZE);
    truncated = true;
  }

  return { text, truncated };
}

function handleQuerySelector(params: Record<string, unknown>): unknown {
  const selector = params.selector as string;
  const mode = (params.mode as string) || "text";
  const all = params.all as boolean;

  if (all) {
    const elements = document.querySelectorAll(selector);
    const results: Array<{
      tagName: string;
      content: string;
      attributes: Record<string, string>;
    }> = [];

    const limit = Math.min(elements.length, MAX_QUERY_RESULTS);
    for (let i = 0; i < limit; i++) {
      results.push(extractElement(elements[i] as HTMLElement, mode));
    }

    return {
      results,
      totalMatches: elements.length,
      truncated: elements.length > MAX_QUERY_RESULTS,
    };
  }

  const element = document.querySelector(selector);
  if (!element) {
    return { results: [], totalMatches: 0, truncated: false };
  }

  return {
    results: [extractElement(element as HTMLElement, mode)],
    totalMatches: 1,
    truncated: false,
  };
}

function extractElement(
  element: HTMLElement,
  mode: string,
): { tagName: string; content: string; attributes: Record<string, string> } {
  const attributes: Record<string, string> = {};
  for (let i = 0; i < element.attributes.length; i++) {
    const attr = element.attributes[i];
    if (attr.name === "style") continue; // skip inline styles
    let value = attr.value;
    if (value.length > MAX_ATTR_VALUE) {
      value = value.slice(0, MAX_ATTR_VALUE) + "…";
    }
    attributes[attr.name] = value;
  }

  let content: string;
  if (mode === "html") {
    content = element.outerHTML;
    if (content.length > MAX_ELEMENT_CONTENT) {
      content = content.slice(0, MAX_ELEMENT_CONTENT);
    }
  } else {
    content = element.textContent || "";
    if (content.length > MAX_ELEMENT_CONTENT) {
      content = content.slice(0, MAX_ELEMENT_CONTENT);
    }
  }

  return { tagName: element.tagName.toLowerCase(), content, attributes };
}

// ---- eval handler ----

async function handleEval(params: Record<string, unknown>): Promise<unknown> {
  const code = params.code as string;

  try {
    // Execute in page context. Use indirect eval to run in global scope.
    const evalFn = eval;
    const raw = await evalFn(code);

    // Safely serialize the result.
    const { json, truncated } = safeStringify(raw, MAX_EVAL_RESULT_SIZE);
    const value = JSON.parse(json);

    return { value, truncated };
  } catch (err) {
    return {
      error: {
        message: err instanceof Error ? err.message : String(err),
        name: err instanceof Error ? err.name : "Error",
        stack: err instanceof Error ? err.stack : undefined,
      },
    };
  }
}

// ---- Interactive snapshot with stable refs ----

/**
 * Ref store: maps ref number → DOM element. Populated by snapshotInteractive(),
 * consumed by ref-based actions (clickRef, typeRef, hoverRef, selectOption).
 * Cleared and repopulated on each interactive snapshot.
 */
const refStore = new Map<number, Element>();

/** Roles that receive a [ref=N] marker in the interactive snapshot. */
const INTERACTIVE_ROLES = new Set([
  "button",
  "link",
  "textbox",
  "searchbox",
  "combobox",
  "listbox",
  "option",
  "checkbox",
  "radio",
  "switch",
  "slider",
  "spinbutton",
  "tab",
  "menuitem",
  "menuitemcheckbox",
  "menuitemradio",
  "treeitem",
]);

/** HTML tags that are inherently interactive even without an explicit role. */
const INTERACTIVE_TAGS = new Set([
  "a",
  "button",
  "input",
  "select",
  "textarea",
]);

/**
 * Produce an AI-friendly accessibility tree with [ref=N] markers on interactive
 * elements. The ref store is repopulated so subsequent ref-based actions can
 * resolve ref numbers to live DOM elements.
 */
function snapshotInteractive(): {
  tree: string;
  refCount: number;
  pageUrl: string;
  title: string;
  truncated: boolean;
} {
  refStore.clear();
  let nextRef = 1;
  const lines: string[] = [];

  if (document.title) {
    lines.push(`page: ${document.title}`);
  }

  function walk(element: Element, depth: number): void {
    const tag = element.tagName.toLowerCase();

    // Skip non-content elements.
    if (
      tag === "script" ||
      tag === "style" ||
      tag === "noscript" ||
      tag === "template" ||
      tag === "svg"
    )
      return;
    const htmlElement = element as HTMLElement;
    if (htmlElement.hidden || element.getAttribute("aria-hidden") === "true")
      return;

    // Check computed visibility (only for HTMLElements).
    if (htmlElement.offsetParent === null && tag !== "body" && tag !== "html") {
      // Element is likely hidden (display:none or similar). Skip unless it
      // has position:fixed/sticky which also reports null offsetParent.
      const style = getComputedStyle(htmlElement);
      if (style.position !== "fixed" && style.position !== "sticky") return;
    }

    const role = element.getAttribute("role") || IMPLICIT_ROLES[tag] || "";
    const label =
      element.getAttribute("aria-label") ||
      element.getAttribute("alt") ||
      element.getAttribute("title") ||
      element.getAttribute("placeholder") ||
      "";

    let name = label;
    if (!name) {
      const directText = Array.from(element.childNodes)
        .filter((node) => node.nodeType === Node.TEXT_NODE)
        .map((node) => (node.textContent || "").trim())
        .join(" ")
        .trim();
      if (directText) name = directText;
    }

    // Skip generic wrappers without meaningful names.
    if (
      (role === "" || role === "none" || role === "generic") &&
      !name &&
      (tag === "div" || tag === "span")
    ) {
      for (const child of element.children) walk(child, depth);
      return;
    }

    const isInteractive =
      INTERACTIVE_ROLES.has(role) || INTERACTIVE_TAGS.has(tag);
    let refMarker = "";
    if (isInteractive) {
      const ref = nextRef++;
      refStore.set(ref, element);
      refMarker = `[ref=${ref}] `;
    }

    const parts: string[] = [];
    if (role) parts.push(role);
    else if (tag !== "div" && tag !== "span") parts.push(tag);

    if (/^h[1-6]$/.test(tag)) parts.push(`level=${tag[1]}`);

    // State attributes.
    if (element.getAttribute("disabled") !== null) parts.push("disabled");
    if (element.getAttribute("aria-expanded") !== null)
      parts.push(`expanded=${element.getAttribute("aria-expanded")}`);
    if (element.getAttribute("aria-checked") !== null)
      parts.push(`checked=${element.getAttribute("aria-checked")}`);
    if (element.getAttribute("aria-selected") !== null)
      parts.push(`selected=${element.getAttribute("aria-selected")}`);
    if ((htmlElement as HTMLInputElement).type && tag === "input")
      parts.push(`type=${(htmlElement as HTMLInputElement).type}`);
    if (
      (htmlElement as HTMLInputElement).value &&
      (tag === "input" || tag === "textarea")
    )
      parts.push(
        `value="${(htmlElement as HTMLInputElement).value.slice(0, 100)}"`,
      );
    if (element.getAttribute("href"))
      parts.push(`href="${element.getAttribute("href")!.slice(0, 150)}"`);

    if (name) parts.push(`"${name.slice(0, 200)}"`);

    if (parts.length > 0 && (role || label || tag !== "div" || isInteractive)) {
      const indent = "  ".repeat(depth);
      lines.push(`${indent}${refMarker}${parts.join(" ")}`);
    }

    for (const child of element.children) {
      walk(child, role || label || isInteractive ? depth + 1 : depth);
    }
  }

  walk(document.body || document.documentElement, 0);

  let tree = lines.join("\n");
  let truncated = false;
  if (tree.length > MAX_SNAPSHOT_SIZE) {
    tree = tree.slice(0, MAX_SNAPSHOT_SIZE);
    truncated = true;
  }

  return {
    tree,
    refCount: refStore.size,
    pageUrl: location.href,
    title: document.title,
    truncated,
  };
}

// ---- Ref-based action handlers ----

function resolveRef(ref: number): Element {
  const element = refStore.get(ref);
  if (!element) {
    throw new Error(
      `ref ${ref} not found. Run snapshot with mode "interactive" first to populate refs.`,
    );
  }
  if (!element.isConnected) {
    throw new Error(
      `ref ${ref} points to a detached element. Run a new interactive snapshot to refresh refs.`,
    );
  }
  return element;
}

function scrollIntoViewIfNeeded(element: Element): void {
  const rect = element.getBoundingClientRect();
  const inViewport =
    rect.top >= 0 &&
    rect.left >= 0 &&
    rect.bottom <= window.innerHeight &&
    rect.right <= window.innerWidth;
  if (!inViewport) {
    element.scrollIntoView({ block: "center", inline: "center" });
  }
}

function handleClickRef(params: Record<string, unknown>): unknown {
  const ref = params.ref as number;
  const element = resolveRef(ref);
  scrollIntoViewIfNeeded(element);

  // Dispatch a real click with proper event sequence.
  const htmlElement = element as HTMLElement;
  htmlElement.focus?.();
  htmlElement.click();

  const role =
    element.getAttribute("role") ||
    IMPLICIT_ROLES[element.tagName.toLowerCase()] ||
    "";
  const name =
    element.getAttribute("aria-label") ||
    element.getAttribute("alt") ||
    element.getAttribute("title") ||
    (element.textContent || "").trim().slice(0, 100) ||
    "";

  return { ref, role, name, clicked: true };
}

async function handleTypeRef(
  params: Record<string, unknown>,
): Promise<unknown> {
  const ref = params.ref as number;
  const text = params.text as string;
  const clearFirst = params.clearFirst as boolean;
  const element = resolveRef(ref);

  scrollIntoViewIfNeeded(element);
  const htmlElement = element as HTMLElement;
  htmlElement.focus?.();

  if (
    element instanceof HTMLInputElement ||
    element instanceof HTMLTextAreaElement
  ) {
    if (clearFirst) {
      htmlElement.focus();
      (element as HTMLInputElement).value = "";
      element.dispatchEvent(new Event("input", { bubbles: true }));
    }
    // Use input event simulation for proper framework reactivity.
    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
      Object.getPrototypeOf(element),
      "value",
    )?.set;
    if (nativeInputValueSetter) {
      nativeInputValueSetter.call(
        element,
        clearFirst ? text : (element as HTMLInputElement).value + text,
      );
    } else {
      (element as HTMLInputElement).value = clearFirst
        ? text
        : (element as HTMLInputElement).value + text;
    }
    element.dispatchEvent(new Event("input", { bubbles: true }));
    element.dispatchEvent(new Event("change", { bubbles: true }));
  } else if (element.getAttribute("contenteditable") !== null) {
    if (clearFirst) htmlElement.textContent = "";
    htmlElement.textContent = (htmlElement.textContent || "") + text;
    element.dispatchEvent(new Event("input", { bubbles: true }));
  } else {
    throw new Error(
      `ref ${ref} is not a text input element (tag: ${element.tagName.toLowerCase()})`,
    );
  }

  const role =
    element.getAttribute("role") ||
    IMPLICIT_ROLES[element.tagName.toLowerCase()] ||
    "";
  return { ref, role, text, clearFirst: !!clearFirst };
}

function handleHoverRef(params: Record<string, unknown>): unknown {
  const ref = params.ref as number;
  const element = resolveRef(ref);
  scrollIntoViewIfNeeded(element);

  const rect = element.getBoundingClientRect();
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;

  element.dispatchEvent(
    new MouseEvent("mouseenter", {
      bubbles: true,
      clientX: centerX,
      clientY: centerY,
    }),
  );
  element.dispatchEvent(
    new MouseEvent("mouseover", {
      bubbles: true,
      clientX: centerX,
      clientY: centerY,
    }),
  );
  element.dispatchEvent(
    new MouseEvent("mousemove", {
      bubbles: true,
      clientX: centerX,
      clientY: centerY,
    }),
  );

  const role =
    element.getAttribute("role") ||
    IMPLICIT_ROLES[element.tagName.toLowerCase()] ||
    "";
  const name =
    element.getAttribute("aria-label") ||
    (element.textContent || "").trim().slice(0, 100) ||
    "";

  return { ref, role, name, x: Math.round(centerX), y: Math.round(centerY) };
}

function handleSelectOption(params: Record<string, unknown>): unknown {
  const ref = params.ref as number;
  const element = resolveRef(ref);

  if (!(element instanceof HTMLSelectElement)) {
    throw new Error(
      `ref ${ref} is not a <select> element (tag: ${element.tagName.toLowerCase()})`,
    );
  }

  const optionValue = params.optionValue as string | undefined;
  const optionIndex = params.optionIndex as number | undefined;

  if (optionValue != null) {
    element.value = optionValue;
  } else if (optionIndex != null) {
    element.selectedIndex = optionIndex;
  } else {
    throw new Error("either optionValue or optionIndex is required");
  }

  element.dispatchEvent(new Event("change", { bubbles: true }));
  element.dispatchEvent(new Event("input", { bubbles: true }));

  return {
    ref,
    selectedValue: element.value,
    selectedIndex: element.selectedIndex,
    selectedText: element.options[element.selectedIndex]?.text || "",
  };
}

// ---- Wait primitives ----

const WAIT_POLL_INTERVAL = 200; // ms
const WAIT_DEFAULT_TIMEOUT = 30000; // ms
const NETWORK_IDLE_THRESHOLD_MS = 500;

type NavigationTracker = {
  sequence: number;
  lastChangeAt: number;
};

type NavigationTrackerWindow = Window & {
  __teanodeNavigationWaitTracker?: NavigationTracker;
  __teanodeNavigationHistoryWrapped?: boolean;
};

type NavigationWaitState = {
  sequence: number;
  url: string;
  readyState: DocumentReadyState;
};

type NetworkIdleTracker = {
  activeRequests: number;
  lastActivityAt: number;
  idleThresholdMs: number;
};

type NetworkIdleTrackerWindow = Window & {
  __teanodeNetworkIdleTracker?: NetworkIdleTracker;
  __teanodeNetworkIdleFetchWrapped?: boolean;
};

type TrackedXMLHttpRequest = XMLHttpRequest & {
  __teanodeNetworkIdleTracked?: boolean;
};

type NetworkIdleXMLHttpRequestPrototype = XMLHttpRequest & {
  __teanodeNetworkIdleWrapped?: boolean;
};

type NetworkIdleWaitState = {
  activeRequests: number;
  lastActivityAt: number;
  currentTime: number;
  readyState: DocumentReadyState;
  idleThresholdMs: number;
};

async function handleWait(params: Record<string, unknown>): Promise<unknown> {
  const mode = params.waitMode as string;
  const timeoutMs = (params.timeoutMs as number) || WAIT_DEFAULT_TIMEOUT;
  const selector = params.selector as string | undefined;

  switch (mode) {
    case "selector":
      return waitForSelector(selector!, timeoutMs);
    case "navigation":
      return waitForNavigation(timeoutMs);
    case "network_idle":
      return waitForNetworkIdle(timeoutMs);
    case "timeout":
      return waitForTimeout(timeoutMs);
    default:
      throw new Error(
        `unknown wait mode: ${mode} (supported: selector, navigation, network_idle, timeout)`,
      );
  }
}

function waitForSelector(
  selector: string,
  timeoutMs: number,
): Promise<unknown> {
  if (!selector) {
    throw new Error("selector is required for wait mode 'selector'");
  }
  const startTime = performance.now();

  return new Promise((resolve, reject) => {
    function poll(): void {
      if (document.querySelector(selector) !== null) {
        resolve({
          mode: "selector",
          selector,
          found: true,
          elapsed: Math.round(performance.now() - startTime),
        });
        return;
      }
      if (performance.now() - startTime >= timeoutMs) {
        reject(
          new Error(
            `wait for selector "${selector}" timed out after ${timeoutMs}ms`,
          ),
        );
        return;
      }
      setTimeout(poll, WAIT_POLL_INTERVAL);
    }
    poll();
  });
}

function waitForNavigation(timeoutMs: number): Promise<unknown> {
  const startTime = performance.now();
  const initialState = readNavigationWaitState();
  let sawNavigation = initialState.readyState !== "complete";

  return new Promise((resolve, reject) => {
    function poll(): void {
      const currentState = readNavigationWaitState();
      if (!sawNavigation) {
        sawNavigation =
          currentState.sequence !== initialState.sequence ||
          currentState.url !== initialState.url ||
          currentState.readyState !== initialState.readyState;
      }

      if (sawNavigation && currentState.readyState === "complete") {
        resolve({
          mode: "navigation",
          readyState: currentState.readyState,
          url: currentState.url,
          navigationDetected: true,
          elapsed: Math.round(performance.now() - startTime),
        });
        return;
      }
      if (performance.now() - startTime >= timeoutMs) {
        reject(new Error("wait for navigation timed out"));
        return;
      }
      setTimeout(poll, WAIT_POLL_INTERVAL);
    }
    poll();
  });
}

function waitForNetworkIdle(timeoutMs: number): Promise<unknown> {
  const startTime = performance.now();
  ensureNetworkIdleTracker();

  return new Promise((resolve, reject) => {
    function poll(): void {
      const state = readNetworkIdleWaitState();
      const idleForMs = state.currentTime - state.lastActivityAt;

      if (state.activeRequests === 0 && idleForMs >= state.idleThresholdMs) {
        resolve({
          mode: "network_idle",
          activeRequests: state.activeRequests,
          idleForMs: Math.round(idleForMs),
          idleThresholdMs: state.idleThresholdMs,
          tracker: "fetch_xhr",
          readyState: state.readyState,
          elapsed: Math.round(performance.now() - startTime),
        });
        return;
      }
      if (performance.now() - startTime >= timeoutMs) {
        reject(new Error("wait for network idle timed out"));
        return;
      }
      setTimeout(poll, WAIT_POLL_INTERVAL);
    }
    poll();
  });
}

function readNavigationWaitState(): NavigationWaitState {
  const tracker = ensureNavigationTracker();
  return {
    sequence: tracker.sequence,
    url: location.href,
    readyState: document.readyState,
  };
}

function ensureNavigationTracker(): NavigationTracker {
  const trackerWindow = window as NavigationTrackerWindow;
  if (trackerWindow.__teanodeNavigationWaitTracker) {
    return trackerWindow.__teanodeNavigationWaitTracker;
  }

  const tracker: NavigationTracker = {
    sequence: 0,
    lastChangeAt: performance.now(),
  };
  const markNavigation = (): void => {
    tracker.sequence += 1;
    tracker.lastChangeAt = performance.now();
  };

  if (!trackerWindow.__teanodeNavigationHistoryWrapped) {
    const originalPushState = history.pushState.bind(history);
    const originalReplaceState = history.replaceState.bind(history);
    history.pushState = (...args) => {
      const result = originalPushState(...args);
      markNavigation();
      return result;
    };
    history.replaceState = (...args) => {
      const result = originalReplaceState(...args);
      markNavigation();
      return result;
    };
    trackerWindow.__teanodeNavigationHistoryWrapped = true;
  }

  window.addEventListener("popstate", markNavigation);
  window.addEventListener("hashchange", markNavigation);
  window.addEventListener("beforeunload", markNavigation);
  window.addEventListener("pageshow", markNavigation);
  window.addEventListener("load", markNavigation);

  trackerWindow.__teanodeNavigationWaitTracker = tracker;
  return tracker;
}

function ensureNetworkIdleTracker(): NetworkIdleTracker {
  const trackerWindow = window as NetworkIdleTrackerWindow;
  if (trackerWindow.__teanodeNetworkIdleTracker) {
    return trackerWindow.__teanodeNetworkIdleTracker;
  }

  const tracker: NetworkIdleTracker = {
    activeRequests: 0,
    lastActivityAt: performance.now(),
    idleThresholdMs: NETWORK_IDLE_THRESHOLD_MS,
  };
  const markActivity = (): void => {
    tracker.lastActivityAt = performance.now();
  };
  const beginRequest = (): void => {
    tracker.activeRequests += 1;
    markActivity();
  };
  const endRequest = (): void => {
    tracker.activeRequests = Math.max(0, tracker.activeRequests - 1);
    markActivity();
  };

  if (
    typeof window.fetch === "function" &&
    !trackerWindow.__teanodeNetworkIdleFetchWrapped
  ) {
    const originalFetch = window.fetch.bind(window);
    window.fetch = (...args) => {
      beginRequest();
      return originalFetch(...args).finally(() => {
        endRequest();
      });
    };
    trackerWindow.__teanodeNetworkIdleFetchWrapped = true;
  }

  const xhrPrototype = window.XMLHttpRequest?.prototype as
    | NetworkIdleXMLHttpRequestPrototype
    | undefined;
  if (xhrPrototype && !xhrPrototype.__teanodeNetworkIdleWrapped) {
    const originalOpen = xhrPrototype.open as {
      (this: XMLHttpRequest, method: string, url: string | URL): void;
      (
        this: XMLHttpRequest,
        method: string,
        url: string | URL,
        async: boolean,
        username?: string | null,
        password?: string | null,
      ): void;
    };
    const originalSend = xhrPrototype.send;
    xhrPrototype.open = function (
      this: XMLHttpRequest,
      method: string,
      url: string | URL,
      async?: boolean,
      username?: string | null,
      password?: string | null,
    ) {
      (this as TrackedXMLHttpRequest).__teanodeNetworkIdleTracked = false;
      if (async === undefined) {
        return originalOpen.call(this, method, url, true);
      }
      return originalOpen.call(this, method, url, async, username, password);
    };
    xhrPrototype.send = function (...args) {
      const request = this as TrackedXMLHttpRequest;
      if (!request.__teanodeNetworkIdleTracked) {
        request.__teanodeNetworkIdleTracked = true;
        beginRequest();
        request.addEventListener(
          "loadend",
          () => {
            if (request.__teanodeNetworkIdleTracked) {
              request.__teanodeNetworkIdleTracked = false;
              endRequest();
            }
          },
          { once: true },
        );
      }
      return originalSend.apply(this, args);
    };
    xhrPrototype.__teanodeNetworkIdleWrapped = true;
  }

  trackerWindow.__teanodeNetworkIdleTracker = tracker;
  return tracker;
}

function readNetworkIdleWaitState(): NetworkIdleWaitState {
  const tracker = ensureNetworkIdleTracker();
  return {
    activeRequests: tracker.activeRequests,
    lastActivityAt: tracker.lastActivityAt,
    currentTime: performance.now(),
    readyState: document.readyState,
    idleThresholdMs: tracker.idleThresholdMs,
  };
}

function waitForTimeout(timeoutMs: number): Promise<unknown> {
  const startTime = performance.now();
  return new Promise((resolve) => {
    setTimeout(() => {
      resolve({
        mode: "timeout",
        elapsed: Math.round(performance.now() - startTime),
      });
    }, timeoutMs);
  });
}

// ---- Multi-step execution (executeSteps) ----

interface StepDefinition {
  action: string;
  // Snapshot
  mode?: string;
  // Ref actions
  ref?: number;
  text?: string;
  clearFirst?: boolean;
  optionValue?: string;
  optionIndex?: number;
  // querySelector
  selector?: string;
  all?: boolean;
  // eval
  code?: string;
  // wait
  waitMode?: string;
  timeoutMs?: number;
  // fetch params
  method?: string;
  url?: string;
  headers?: Record<string, string>;
  body?: string;
  // localStorage
  key?: string;
  value?: string;
}

async function handleStepsRequest(data: any): Promise<void> {
  const { id, steps } = data;

  try {
    const results: Array<{
      step: number;
      action: string;
      result?: unknown;
      error?: string;
    }> = [];

    for (let index = 0; index < steps.length; index++) {
      const step = steps[index] as StepDefinition;
      const stepResult: {
        step: number;
        action: string;
        result?: unknown;
        error?: string;
      } = {
        step: index + 1,
        action: step.action,
      };

      try {
        stepResult.result = await executeSingleStep(step);
      } catch (err) {
        stepResult.error = err instanceof Error ? err.message : String(err);
        results.push(stepResult);
        break; // Stop on first error.
      }

      results.push(stepResult);
    }

    window.postMessage({
      __tn: NONCE,
      type: "steps_res",
      id,
      result: {
        stepsExecuted: results.length,
        totalSteps: steps.length,
        results,
      },
    });
  } catch (err) {
    window.postMessage({
      __tn: NONCE,
      type: "steps_res",
      id,
      error: String(err),
    });
  }
}

async function executeSingleStep(step: StepDefinition): Promise<unknown> {
  switch (step.action) {
    case "snapshot":
      return handleSnapshot({ mode: step.mode || "interactive" });
    case "querySelector":
      return handleQuerySelector({
        selector: step.selector,
        mode: step.mode || "text",
        all: step.all,
      });
    case "eval":
      return handleEval({ code: step.code });
    case "clickRef":
      return handleClickRef({ ref: step.ref });
    case "typeRef":
      return handleTypeRef({
        ref: step.ref,
        text: step.text,
        clearFirst: step.clearFirst,
      });
    case "hoverRef":
      return handleHoverRef({ ref: step.ref });
    case "selectOption":
      return handleSelectOption({
        ref: step.ref,
        optionValue: step.optionValue,
        optionIndex: step.optionIndex,
      });
    case "wait":
      return handleWait({
        waitMode: step.waitMode || "navigation",
        selector: step.selector,
        timeoutMs: step.timeoutMs,
      });
    case "getLocalStorage":
      return handleGetLocalStorage({ key: step.key });
    case "setLocalStorage":
      return handleSetLocalStorage({ key: step.key, value: step.value });
    case "removeLocalStorage":
      return handleRemoveLocalStorage({ key: step.key });
    default:
      throw new Error(`unknown step action: ${step.action}`);
  }
}
