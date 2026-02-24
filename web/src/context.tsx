import React, { createContext, useContext, useState, useCallback } from "react";
import type { useBackend } from "./hooks/useBackend";
import {
  LANGUAGE_PREFERENCE_STORAGE_KEY,
  type LanguagePreference,
} from "./i18n/config";

export type ThemeMode = "dark" | "light";
export type VoiceCallSTTMode = "server" | "client";

export interface AppContextValue {
  backend: ReturnType<typeof useBackend>;
  showToolCalls: boolean;
  setShowToolCalls: (value: boolean) => void;
  showTokenUsage: boolean;
  setShowTokenUsage: (value: boolean) => void;
  mobileSidebarOpen: boolean;
  setMobileSidebarOpen: (open: boolean) => void;
  themeMode: ThemeMode;
  setThemeMode: (mode: ThemeMode) => void;
  voiceAutoSend: boolean;
  setVoiceAutoSend: (value: boolean) => void;
  ttsVoice: string;
  setTtsVoice: (voice: string) => void;
  voiceChimesEnabled: boolean;
  setVoiceChimesEnabled: (value: boolean) => void;
  voiceChimesVolume: number;
  setVoiceChimesVolume: (value: number) => void;
  voiceCallSttMode: VoiceCallSTTMode;
  setVoiceCallSttMode: (value: VoiceCallSTTMode) => void;
  languagePreference: LanguagePreference;
  setLanguagePreference: (value: LanguagePreference) => void;
}

const AppContext = createContext<AppContextValue | null>(null);

export function useAppContext(): AppContextValue {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error("useAppContext must be used within AppProvider");
  }
  return context;
}

export function AppProvider({
  backend,
  children,
}: {
  backend: ReturnType<typeof useBackend>;
  children: React.ReactNode;
}) {
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [showToolCalls, setShowToolCallsState] = useState(() => {
    return localStorage.getItem("teanode-show-tools") === "true";
  });
  const [showTokenUsage, setShowTokenUsageState] = useState(() => {
    return localStorage.getItem("teanode-show-usage") === "true";
  });
  const [themeMode, setThemeModeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem("teanode-theme-mode");
    return stored === "light" ? "light" : "dark";
  });
  const [voiceAutoSend, setVoiceAutoSendState] = useState(() => {
    return localStorage.getItem("teanode-voice-auto-send") !== "false";
  });
  const [ttsVoice, setTtsVoiceState] = useState(() => {
    return localStorage.getItem("teanode-voice-tts-voice") || "alloy";
  });
  const [voiceChimesEnabled, setVoiceChimesEnabledState] = useState(() => {
    return localStorage.getItem("teanode-voice-chimes-enabled") !== "false";
  });
  const [voiceChimesVolume, setVoiceChimesVolumeState] = useState(() => {
    const stored = localStorage.getItem("teanode-voice-chimes-volume");
    return stored !== null ? Number(stored) : 0.3;
  });
  const [voiceCallSttMode, setVoiceCallSttModeState] =
    useState<VoiceCallSTTMode>(() => {
      const stored = localStorage.getItem("teanode-voice-call-stt-mode");
      return stored === "client" ? "client" : "server";
    });
  const [languagePreference, setLanguagePreferenceState] =
    useState<LanguagePreference>(() => {
      const stored = localStorage.getItem(LANGUAGE_PREFERENCE_STORAGE_KEY);
      if (stored === "en") return "en";
      if (stored === "zh") return "zh";
      if (stored === "ja") return "ja";
      return "auto";
    });

  const setShowToolCalls = useCallback((value: boolean) => {
    setShowToolCallsState(value);
    localStorage.setItem("teanode-show-tools", String(value));
  }, []);

  const setShowTokenUsage = useCallback((value: boolean) => {
    setShowTokenUsageState(value);
    localStorage.setItem("teanode-show-usage", String(value));
  }, []);

  const setThemeMode = useCallback((mode: ThemeMode) => {
    setThemeModeState(mode);
    localStorage.setItem("teanode-theme-mode", mode);
  }, []);

  const setVoiceAutoSend = useCallback((value: boolean) => {
    setVoiceAutoSendState(value);
    localStorage.setItem("teanode-voice-auto-send", String(value));
  }, []);

  const setTtsVoice = useCallback((voice: string) => {
    setTtsVoiceState(voice);
    localStorage.setItem("teanode-voice-tts-voice", voice);
  }, []);

  const setVoiceChimesEnabled = useCallback((value: boolean) => {
    setVoiceChimesEnabledState(value);
    localStorage.setItem("teanode-voice-chimes-enabled", String(value));
  }, []);

  const setVoiceChimesVolume = useCallback((value: number) => {
    setVoiceChimesVolumeState(value);
    localStorage.setItem("teanode-voice-chimes-volume", String(value));
  }, []);

  const setVoiceCallSttMode = useCallback((value: VoiceCallSTTMode) => {
    setVoiceCallSttModeState(value);
    localStorage.setItem("teanode-voice-call-stt-mode", value);
  }, []);

  const setLanguagePreference = useCallback((value: LanguagePreference) => {
    setLanguagePreferenceState(value);
    localStorage.setItem(LANGUAGE_PREFERENCE_STORAGE_KEY, value);
  }, []);

  return (
    <AppContext.Provider
      value={{
        backend,
        showToolCalls,
        setShowToolCalls,
        showTokenUsage,
        setShowTokenUsage,
        mobileSidebarOpen,
        setMobileSidebarOpen,
        themeMode,
        setThemeMode,
        voiceAutoSend,
        setVoiceAutoSend,
        ttsVoice,
        setTtsVoice,
        voiceChimesEnabled,
        setVoiceChimesEnabled,
        voiceChimesVolume,
        setVoiceChimesVolume,
        voiceCallSttMode,
        setVoiceCallSttMode,
        languagePreference,
        setLanguagePreference,
      }}
    >
      {children}
    </AppContext.Provider>
  );
}
