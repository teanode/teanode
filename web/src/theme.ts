import { createTheme, type Theme } from "@mui/material/styles";

// Extend MUI palette with custom colors.
declare module "@mui/material/styles" {
  interface Palette {
    surface2: string;
    userBg: string;
    toolBg: string;
    codeBg: string;
    accentDim: string;
    dangerDim: string;
  }
  interface PaletteOptions {
    surface2?: string;
    userBg?: string;
    toolBg?: string;
    codeBg?: string;
    accentDim?: string;
    dangerDim?: string;
  }
}

const sharedSettings = {
  breakpoints: {
    values: { xs: 0, sm: 600, md: 768, lg: 1200, xl: 1536 },
  },
  typography: {
    fontFamily: [
      "-apple-system",
      "BlinkMacSystemFont",
      '"Segoe UI"',
      "Roboto",
      "sans-serif",
    ].join(","),
    fontSize: 14,
  },
  shape: {
    borderRadius: 8,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        "*::-webkit-scrollbar": { width: 6 },
        "*::-webkit-scrollbar-track": { background: "transparent" },
        "*::-webkit-scrollbar-thumb": { background: "#555", borderRadius: 3 },
        // Prevent iOS Safari from zooming in when focusing inputs.
        "@media screen and (max-width: 768px)": {
          "input, textarea, select": { fontSize: "16px !important" },
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: { textTransform: "none" as const },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: { backgroundImage: "none" },
      },
    },
  },
} as const;

export function getTheme(mode: "dark" | "light"): Theme {
  if (mode === "light") {
    return createTheme({
      ...sharedSettings,
      palette: {
        mode: "light",
        primary: { main: "#729d39" },
        error: { main: "#c66" },
        background: { default: "#fafafa", paper: "#ffffff" },
        text: { primary: "#1a1a1a", secondary: "#666" },
        divider: "#e0e0e0",
        surface2: "#f0f0f0",
        userBg: "#e8f0dc",
        toolBg: "#f5f5e0",
        codeBg: "#f5f5f5",
        accentDim: "#9bc46a",
        dangerDim: "#fde8e8",
      },
    });
  }

  return createTheme({
    ...sharedSettings,
    palette: {
      mode: "dark",
      primary: { main: "#729d39" },
      error: { main: "#c66" },
      background: { default: "#0f0f0f", paper: "#1a1a1a" },
      text: { primary: "#ffffff", secondary: "#888" },
      divider: "#333",
      surface2: "#252525",
      userBg: "#1e2a14",
      toolBg: "#1e1e14",
      codeBg: "#111",
      accentDim: "#5a6a35",
      dangerDim: "#5a2a2a",
    },
  });
}
