/**
 * Page bridge injected into MAIN world via chrome.scripting.executeScript.
 * Receives fetch requests from the content script (ISOLATED world) via postMessage,
 * executes fetch() with the page's cookies/credentials, and returns the result.
 *
 * This file is compiled as an IIFE by webpack. The nonce is injected at execution
 * time via chrome.scripting.executeScript({ args: [nonce], func: ... }).
 * Since we use webpack bundling, we pass the nonce via a global set before injection.
 */

const MAX_BODY_SIZE = 512 * 1024; // 512 KB
const MAX_HEADERS_SIZE = 64 * 1024; // 64 KB

// The nonce is set as a global by the background SW before injecting this script.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const NONCE: string = (globalThis as any).__tn_nonce || "";

if (NONCE) {
  window.addEventListener("message", async (event: MessageEvent) => {
    if (event.source !== window) return;
    if (event.data?.__tn !== NONCE || event.data?.type !== "req") return;

    const { id, payload } = event.data;
    const { method, url, headers, body, timeoutMs } = payload;

    const start = performance.now();

    try {
      const controller = new AbortController();
      const timer = setTimeout(
        () => controller.abort(),
        timeoutMs || 30000,
      );

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
  });
}
