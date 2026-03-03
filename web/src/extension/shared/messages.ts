/** Message type constants used across extension components. */
export const MSG = {
  TOOL_EXECUTE: "tool_execute",
  TOOL_EXECUTE_RESPONSE: "tool_execute_response",
  PAGE_FETCH_REQUEST: "page_fetch_request",
  PAGE_FETCH_RESPONSE: "page_fetch_response",
  PAGE_ACTION_REQUEST: "page_action_request",
  PAGE_ACTION_RESPONSE: "page_action_response",
  TAB_URL_CHANGED: "tab_url_changed",
  TAB_CLOSED: "tab_closed",
  OVERLAY_TOGGLE: "tn:toggle-overlay",
  OVERLAY_CLOSE: "tn:close-overlay",
  CDP_TOGGLE: "tn:toggle-cdp",
  CDP_STATE_QUERY: "tn:cdp-state-query",
  CDP_STATE: "tn:cdp-state",
} as const;
