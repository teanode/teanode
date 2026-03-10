import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useRef,
  useMemo,
} from "react";
import type { useBackend } from "./hooks/useBackend";
import {
  LANGUAGE_PREFERENCE_STORAGE_KEY,
  type LanguagePreference,
} from "./i18n/config";

export type ThemeMode = "dark" | "light" | "system";
export type VoiceCallSTTMode = "server" | "client";
export type VoicePipelineMode = "classic" | "realtime";

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
  voicePipeline: VoicePipelineMode;
  setVoicePipeline: (value: VoicePipelineMode) => void;
  languagePreference: LanguagePreference;
  setLanguagePreference: (value: LanguagePreference) => void;
  todosPanelCollapsed: boolean;
  setTodosPanelCollapsed: (value: boolean) => void;
}

const AppContext = createContext<AppContextValue | null>(null);

/**
 * Streaming context — a separate context whose value changes whenever
 * streaming-related backend properties change. Conversation pages subscribe
 * to this so they re-render during streaming; settings pages do not.
 */
const StreamingContext = createContext<object | null>(null);

export function useAppContext(): AppContextValue {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error("useAppContext must be used within AppProvider");
  }
  return context;
}

/**
 * Subscribe to streaming updates. Call this in any component that needs to
 * re-render when streaming-related backend properties change (messages,
 * streamText, isStreaming, isRunning, status, toolActivity, todos, etc.).
 */
export function useStreamingContext(): void {
  useContext(StreamingContext);
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
    if (stored === "light" || stored === "dark") return stored;
    return "system";
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
  const [voicePipeline, setVoicePipelineState] =
    useState<VoicePipelineMode>(() => {
      const stored = localStorage.getItem("teanode-voice-pipeline");
      return stored === "realtime" ? "realtime" : "classic";
    });
  const [languagePreference, setLanguagePreferenceState] =
    useState<LanguagePreference>(() => {
      const stored = localStorage.getItem(LANGUAGE_PREFERENCE_STORAGE_KEY);
      if (stored === "en") return "en";
      if (stored === "zh") return "zh";
      if (stored === "ja") return "ja";
      return "auto";
    });
  const [todosPanelCollapsed, setTodosPanelCollapsedState] = useState(() => {
    return localStorage.getItem("teanode-todos-collapsed") === "true";
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

  const setVoicePipeline = useCallback((value: VoicePipelineMode) => {
    setVoicePipelineState(value);
    localStorage.setItem("teanode-voice-pipeline", value);
  }, []);

  const setLanguagePreference = useCallback((value: LanguagePreference) => {
    setLanguagePreferenceState(value);
    localStorage.setItem(LANGUAGE_PREFERENCE_STORAGE_KEY, value);
  }, []);

  const setTodosPanelCollapsed = useCallback((value: boolean) => {
    setTodosPanelCollapsedState(value);
    localStorage.setItem("teanode-todos-collapsed", String(value));
  }, []);

  // Keep a ref to the latest backend object, updated every render.
  const backendRef = useRef(backend);
  backendRef.current = backend;

  // Stable proxy with a fixed identity that delegates all property reads
  // to backendRef.current, so consumers always get the latest values
  // without the proxy reference itself changing.
  const stableBackendProxy = useMemo(
    () =>
      new Proxy({} as ReturnType<typeof useBackend>, {
        get(_target, prop, receiver) {
          return Reflect.get(backendRef.current, prop, receiver);
        },
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // Memoize context value on stable backend deps + UI pref deps.
  // During streaming, none of these change, so settings pages won't re-render.
  const contextValue = useMemo<AppContextValue>(
    () => ({
      backend: stableBackendProxy,
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
      voicePipeline,
      setVoicePipeline,
      languagePreference,
      setLanguagePreference,
      todosPanelCollapsed,
      setTodosPanelCollapsed,
    }),
    [
      stableBackendProxy,
      // Stable backend deps (change infrequently, NOT during streaming)
      backend.connected,
      backend.connecting,
      backend.hasConnectedOnce,
      backend.isAdmin,
      backend.currentUserId,
      backend.agents,
      backend.models,
      backend.conversations,
      backend.currentAgentId,
      backend.serverDefaultAgentId,
      backend.conversationId,
      backend.defaultProviderModelName,
      backend.conversationModel,
      backend.audioCapability,
      backend.jobs,
      backend.jobsLoading,
      backend.hasMoreHistory,
      backend.loadingOlderMessages,
      // UI prefs
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
      voicePipeline,
      setVoicePipeline,
      languagePreference,
      setLanguagePreference,
      todosPanelCollapsed,
      setTodosPanelCollapsed,
    ],
  );

  // Streaming token — a new object ref whenever streaming props change.
  // Conversation pages subscribe to this context to get re-renders during
  // streaming, then read fresh data through the proxy.
  const streamingToken = useMemo(
    () => ({}),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [
      backend.messages,
      backend.streamText,
      backend.isStreaming,
      backend.isRunning,
      backend.status,
      backend.toolActivity,
      backend.todos,
      backend.lastActiveRunState,
      backend.pendingQuestions,
    ],
  );

  return (
    <StreamingContext.Provider value={streamingToken}>
      <AppContext.Provider value={contextValue}>{children}</AppContext.Provider>
    </StreamingContext.Provider>
  );
}
