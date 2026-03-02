/** Message type constants used across extension components. */
export const MSG = {
  TOOL_EXECUTE: "tool_execute",
  TOOL_EXECUTE_RESPONSE: "tool_execute_response",
  PAGE_FETCH_REQUEST: "page_fetch_request",
  PAGE_FETCH_RESPONSE: "page_fetch_response",
  TAB_URL_CHANGED: "tab_url_changed",
  TAB_CLOSED: "tab_closed",
  OVERLAY_TOGGLE: "tn:toggle-overlay",
  OVERLAY_CLOSE: "tn:close-overlay",
} as const;
