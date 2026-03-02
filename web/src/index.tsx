import React, { useMemo, useState, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";
import { ThemeProvider } from "@mui/material/styles";
import { useTranslation } from "react-i18next";
import dayjs from "dayjs";
import "dayjs/locale/en";
import "dayjs/locale/ja";
import "dayjs/locale/zh-cn";
import CssBaseline from "@mui/material/CssBaseline";
import GlobalStyles from "@mui/material/GlobalStyles";
import Box from "@mui/material/Box";
import CircularProgress from "@mui/material/CircularProgress";
import type { Theme } from "@mui/material/styles";
import { router } from "./router";
import { getTheme } from "./theme";
import { useResolvedTheme } from "./themePreference";
import { useAppContext, AppProvider } from "./context";
import { useBackend } from "./hooks/useBackend";
import { authStatus } from "./rpc";
import "./i18n/config";
import { resolveLanguageFromPreference } from "./i18n/config";
import "./index.css";

function markdownStyles(theme: Theme) {
  const palette = theme.palette;
  return {
    ".markdown-content h1, .markdown-content h2, .markdown-content h3, .markdown-content h4, .markdown-content h5, .markdown-content h6":
      {
        fontWeight: 600,
        color: palette.text.primary,
        margin: "12px 0 6px",
      },
    ".markdown-content h1": { fontSize: "1.4em" },
    ".markdown-content h2": { fontSize: "1.25em" },
    ".markdown-content h3": { fontSize: "1.1em" },
    ".markdown-content p": { marginBottom: "8px" },
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
      fontFamily: '"SF Mono","Fira Code","Cascadia Code",Consolas,monospace',
      fontSize: "13px",
      padding: "2px 5px",
      borderRadius: "3px",
    },
    ".markdown-content .code-block": {
      position: "relative" as const,
      margin: "8px 0",
    },
    ".markdown-content .code-block .code-header": {
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      backgroundColor: palette.background.paper,
      border: `1px solid ${palette.divider}`,
      borderBottom: "none",
      borderRadius: "8px 8px 0 0",
      fontSize: "11px",
      color: palette.text.secondary,
      padding: "4px 8px 4px 12px",
    },
    ".markdown-content .code-block .code-lang": {
      fontFamily: '"SF Mono","Fira Code","Cascadia Code",Consolas,monospace',
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
      borderRadius: "0 0 8px 8px",
      border: `1px solid ${palette.divider}`,
      borderTop: "none",
      overflowX: "auto" as const,
      margin: 0,
      padding: "12px",
    },
    ".markdown-content pre": {
      backgroundColor: palette.codeBg,
      borderRadius: "8px",
      overflowX: "auto" as const,
      margin: "8px 0",
      padding: "12px",
    },
    ".markdown-content pre code": {
      backgroundColor: "transparent",
      padding: 0,
      fontFamily: '"SF Mono","Fira Code","Cascadia Code",Consolas,monospace',
      fontSize: "13px",
    },
    ".markdown-content pre code.hljs": {
      backgroundColor: "transparent",
      padding: 0,
    },
    ".markdown-content ul, .markdown-content ol": {
      paddingLeft: "20px",
      margin: "8px 0",
    },
    ".markdown-content li": { marginBottom: "4px" },
    ".markdown-content li > p": { marginBottom: "4px" },
    ".markdown-content blockquote": {
      borderLeft: `3px solid ${palette.accentDim}`,
      paddingLeft: "12px",
      color: palette.text.secondary,
      margin: "8px 0",
    },
    ".markdown-content table": {
      borderCollapse: "collapse" as const,
      margin: "8px 0",
      width: "100%",
    },
    ".markdown-content th, .markdown-content td": {
      border: `1px solid ${palette.divider}`,
      textAlign: "left" as const,
      padding: "6px 10px",
    },
    ".markdown-content th": {
      backgroundColor: palette.surface2,
      fontWeight: 600,
    },
    ".markdown-content hr": {
      border: "none",
      borderTop: `1px solid ${palette.divider}`,
      margin: "12px 0",
    },
    ".markdown-content img": {
      maxWidth: "100%",
      borderRadius: "8px",
    },
    // Light-mode overrides for highlight.js (base import is github-dark).
    ...(palette.mode === "light"
      ? {
          ".hljs": { color: "#24292e" },
          ".hljs-comment, .hljs-quote": { color: "#6a737d" },
          ".hljs-keyword, .hljs-selector-tag, .hljs-type": { color: "#d73a49" },
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

function ThemedApp({ children }: { children: React.ReactNode }) {
  const { themeMode, languagePreference } = useAppContext();
  const { i18n } = useTranslation();
  const resolvedTheme = useResolvedTheme(themeMode);
  const theme = useMemo(() => getTheme(resolvedTheme), [resolvedTheme]);

  useEffect(() => {
    const targetLanguage = resolveLanguageFromPreference(languagePreference);
    if (i18n.language !== targetLanguage) {
      i18n.changeLanguage(targetLanguage).catch(() => {});
    }

    const dayjsLocale = targetLanguage === "zh" ? "zh-cn" : targetLanguage;
    dayjs.locale(dayjsLocale);
  }, [languagePreference, i18n]);

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <GlobalStyles
        styles={(currentTheme: Theme) => markdownStyles(currentTheme)}
      />
      {children}
    </ThemeProvider>
  );
}

function Root() {
  const [ready, setReady] = useState(false);
  const backend = useBackend();

  useEffect(() => {
    const pathname = window.location.pathname;

    authStatus()
      .then((result) => {
        if (!result.passwordSet) {
          // Redirect to /setup if not already there.
          if (pathname !== "/setup") {
            const next =
              pathname !== "/login" && pathname !== "/" ? pathname : "";
            const url = next
              ? `/setup?next=${encodeURIComponent(next)}`
              : "/setup";
            window.history.replaceState(null, "", url);
          }
        } else if (!result.authenticated) {
          // Redirect to /login with ?next= if not already there.
          if (pathname !== "/login") {
            const next =
              pathname !== "/setup" && pathname !== "/" ? pathname : "";
            const url = next
              ? `/login?next=${encodeURIComponent(next)}`
              : "/login";
            window.history.replaceState(null, "", url);
          }
        } else {
          // Authenticated — if on /login or /setup, redirect to ?next= or /.
          if (pathname === "/login" || pathname === "/setup") {
            const params = new URLSearchParams(window.location.search);
            const next = params.get("next");
            window.history.replaceState(
              null,
              "",
              next && next.startsWith("/") && !next.startsWith("//")
                ? next
                : "/",
            );
          }
        }
      })
      .catch(() => {
        // If auth status fails (e.g. no server), leave URL as-is
        // so the app can show its normal connection error state.
      })
      .finally(() => setReady(true));
  }, []);

  if (!ready) {
    return (
      <AppProvider backend={backend}>
        <ThemedApp>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              minHeight: "100vh",
            }}
          >
            <CircularProgress />
          </Box>
        </ThemedApp>
      </AppProvider>
    );
  }

  return (
    <AppProvider backend={backend}>
      <ThemedApp>
        <RouterProvider router={router} />
      </ThemedApp>
    </AppProvider>
  );
}

const root = createRoot(document.getElementById("root")!);
root.render(<Root />);
