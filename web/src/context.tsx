import React, { createContext, useContext, useState, useCallback } from 'react';
import type { useBackend } from './hooks/useBackend';

export type ThemeMode = 'dark' | 'light';

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
}

const AppContext = createContext<AppContextValue | null>(null);

export function useAppContext(): AppContextValue {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error('useAppContext must be used within AppProvider');
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
    return localStorage.getItem('teanode-show-tools') !== 'false';
  });
  const [showTokenUsage, setShowTokenUsageState] = useState(() => {
    return localStorage.getItem('teanode-show-usage') !== 'false';
  });
  const [themeMode, setThemeModeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem('teanode-theme-mode');
    return stored === 'light' ? 'light' : 'dark';
  });

  const setShowToolCalls = useCallback((value: boolean) => {
    setShowToolCallsState(value);
    localStorage.setItem('teanode-show-tools', String(value));
  }, []);

  const setShowTokenUsage = useCallback((value: boolean) => {
    setShowTokenUsageState(value);
    localStorage.setItem('teanode-show-usage', String(value));
  }, []);

  const setThemeMode = useCallback((mode: ThemeMode) => {
    setThemeModeState(mode);
    localStorage.setItem('teanode-theme-mode', mode);
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
      }}
    >
      {children}
    </AppContext.Provider>
  );
}
