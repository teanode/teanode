/**
 * Overlay entry point — renders the same SidePanel UI inside a floating iframe.
 * Listens for postMessage from the content script for close/minimize commands.
 */

import React, { useState, useMemo, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { ThemeProvider } from "@mui/material/styles";
import CssBaseline from "@mui/material/CssBaseline";
import GlobalStyles from "@mui/material/GlobalStyles";
import type { Theme } from "@mui/material/styles";
import { getTheme } from "../../theme";
import {
  useResolvedTheme,
  loadThemePreference,
  type ThemePreference,
} from "../../themePreference";
import { SidePanel } from "../sidepanel/SidePanel";
import "../../i18n/config";
import "highlight.js/styles/github-dark.css";

function markdownStyles(theme: Theme) {
  const palette = theme.palette;
  return {
    ".markdown-content h1, .markdown-content h2, .markdown-content h3, .markdown-content h4, .markdown-content h5, .markdown-content h6":
      { fontWeight: 600, color: palette.text.primary, margin: "8px 0 4px" },
    ".markdown-content h1": { fontSize: "1.3em" },
    ".markdown-content h2": { fontSize: "1.15em" },
    ".markdown-content h3": { fontSize: "1.05em" },
    ".markdown-content p": { marginBottom: "6px" },
    ".markdown-content p:last-child": { marginBottom: 0 },
    ".markdown-content strong": { fontWeight: 600 },
    ".markdown-content em": { fontStyle: "italic" },
    ".markdown-content a": {
      color: palette.primary.main,
      textDecoration: "none",
      "&:hover": { textDecoration: "underline" },
    },
    ".markdown-content code": {
      backgroundColor: palette.codeBg,
      fontFamily: '"SF Mono","Fira Code",Consolas,monospace',
      fontSize: "12px",
      padding: "1px 4px",
      borderRadius: "3px",
    },
    ".markdown-content .code-block": {
      position: "relative" as const,
      margin: "6px 0",
    },
    ".markdown-content .code-block .code-header": {
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      backgroundColor: palette.background.paper,
      border: `1px solid ${palette.divider}`,
      borderBottom: "none",
      borderRadius: "6px 6px 0 0",
      fontSize: "10px",
      color: palette.text.secondary,
      padding: "3px 6px 3px 10px",
    },
    ".markdown-content .code-block .code-lang": {
      fontFamily: '"SF Mono","Fira Code",Consolas,monospace',
    },
    ".markdown-content .code-block .copy-btn": {
      backgroundColor: "transparent",
      border: "none",
      color: palette.text.secondary,
      cursor: "pointer",
      padding: "2px",
      lineHeight: 0,
      display: "inline-flex",
      alignItems: "center",
      justifyContent: "center",
      borderRadius: "4px",
      "&:hover": { color: palette.text.primary },
      "&.copied": { color: palette.primary.main },
    },
    ".markdown-content .code-block pre": {
      backgroundColor: palette.codeBg,
      borderRadius: "0 0 6px 6px",
      border: `1px solid ${palette.divider}`,
      borderTop: "none",
      overflowX: "auto" as const,
      margin: 0,
      padding: "8px",
    },
    ".markdown-content pre": {
      backgroundColor: palette.codeBg,
      borderRadius: "6px",
      overflowX: "auto" as const,
      margin: "6px 0",
      padding: "8px",
    },
    ".markdown-content pre code": {
      backgroundColor: "transparent",
      padding: 0,
      fontFamily: '"SF Mono","Fira Code",Consolas,monospace',
      fontSize: "12px",
    },
    ".markdown-content pre code.hljs": {
      backgroundColor: "transparent",
      padding: 0,
    },
    ".markdown-content ul, .markdown-content ol": {
      paddingLeft: "18px",
      margin: "6px 0",
    },
    ".markdown-content li": { marginBottom: "3px" },
    ".markdown-content li > p": { marginBottom: "3px" },
    ".markdown-content blockquote": {
      borderLeft: `3px solid ${palette.accentDim}`,
      paddingLeft: "10px",
      color: palette.text.secondary,
      margin: "6px 0",
    },
    ".markdown-content table": {
      borderCollapse: "collapse" as const,
      margin: "6px 0",
      width: "100%",
    },
    ".markdown-content th, .markdown-content td": {
      border: `1px solid ${palette.divider}`,
      textAlign: "left" as const,
      padding: "4px 8px",
    },
    ".markdown-content th": {
      backgroundColor: palette.surface2,
      fontWeight: 600,
    },
    ".markdown-content hr": {
      border: "none",
      borderTop: `1px solid ${palette.divider}`,
      margin: "8px 0",
    },
    ".markdown-content img": { maxWidth: "100%", borderRadius: "6px" },
    ...(palette.mode === "light"
      ? {
          ".hljs": { color: "#24292e" },
          ".hljs-comment, .hljs-quote": { color: "#6a737d" },
          ".hljs-keyword, .hljs-selector-tag, .hljs-type": {
            color: "#d73a49",
          },
          ".hljs-string, .hljs-addition, .hljs-attr": { color: "#032f62" },
          ".hljs-number, .hljs-literal, .hljs-section": { color: "#005cc5" },
          ".hljs-built_in, .hljs-title, .hljs-title\\.class_, .hljs-title\\.function_":
            { color: "#6f42c1" },
          ".hljs-variable, .hljs-template-variable": { color: "#e36209" },
          ".hljs-name, .hljs-tag, .hljs-selector-class": { color: "#22863a" },
          ".hljs-deletion": { color: "#b31d28" },
          ".hljs-symbol, .hljs-bullet, .hljs-link": { color: "#0366d6" },
          ".hljs-meta, .hljs-selector-id": { color: "#005cc5" },
          ".hljs-subst": { color: "#24292e" },
        }
      : {}),
  };
}

function Root() {
  const [themePreference] = useState<ThemePreference>(loadThemePreference);
  const resolvedTheme = useResolvedTheme(themePreference);
  const theme = useMemo(() => getTheme(resolvedTheme), [resolvedTheme]);

  // Listen for close request from content script via postMessage.
  useEffect(() => {
    const extOrigin = `chrome-extension://${chrome.runtime.id}`;
    const handler = (e: MessageEvent) => {
      if (e.origin !== extOrigin) return;
      if (e.data?.type === "tn:close") {
        // Tell content script to remove the overlay.
        window.parent.postMessage({ type: "tn:close-ack" }, "*");
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, []);

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <GlobalStyles
        styles={(currentTheme: Theme) => markdownStyles(currentTheme)}
      />
      <SidePanel />
    </ThemeProvider>
  );
}

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(<Root />);
}
