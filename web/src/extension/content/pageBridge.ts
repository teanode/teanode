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
