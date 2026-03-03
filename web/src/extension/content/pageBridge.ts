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

const MAX_BODY_SIZE = 512 * 1024; // 512 KB
const MAX_HEADERS_SIZE = 64 * 1024; // 64 KB
const MAX_DOM_SIZE = 512 * 1024; // 512 KB
const MAX_LOCALSTORAGE_TOTAL = 512 * 1024; // 512 KB
const MAX_EVAL_RESULT_SIZE = 512 * 1024; // 512 KB
const MAX_QUERY_RESULTS = 50;

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

  function replacer(_key: string, val: unknown): unknown {
    if (val === undefined) return null;
    if (typeof val === "function") return "[Function]";
    if (typeof val === "symbol") return val.toString();
    if (typeof val === "bigint") return val.toString();
    if (val instanceof Error) {
      return { message: val.message, name: val.name, stack: val.stack };
    }
    if (val instanceof RegExp) return val.toString();
    if (val !== null && typeof val === "object") {
      if (seen.has(val)) return "[Circular]";
      seen.add(val);
    }
    return val;
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
      case "snapshotDom":
        result = handleSnapshotDom();
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

function handleSnapshotDom(): unknown {
  let html = document.documentElement.outerHTML;
  let truncated = false;

  if (html.length > MAX_DOM_SIZE) {
    html = html.slice(0, MAX_DOM_SIZE);
    truncated = true;
  }

  return { html, truncated };
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
  el: HTMLElement,
  mode: string,
): { tagName: string; content: string; attributes: Record<string, string> } {
  const attributes: Record<string, string> = {};
  for (let i = 0; i < el.attributes.length; i++) {
    const attr = el.attributes[i];
    attributes[attr.name] = attr.value;
  }

  let content: string;
  if (mode === "html") {
    content = el.outerHTML;
    if (content.length > MAX_BODY_SIZE) {
      content = content.slice(0, MAX_BODY_SIZE);
    }
  } else {
    content = el.textContent || "";
    if (content.length > MAX_BODY_SIZE) {
      content = content.slice(0, MAX_BODY_SIZE);
    }
  }

  return { tagName: el.tagName.toLowerCase(), content, attributes };
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
