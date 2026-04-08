import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
  useMemo,
} from "react";
import { useAppContext } from "../context";

interface ArtifactPanelState {
  openMessageId: string | null;
  openArtifactIndex: number | null;
  /** ID that was explicitly dismissed by the user — auto-open skips it. */
  dismissedId: string | null;
  openArtifactPanel: (messageId: string, artifactIndex: number) => void;
  closeArtifactPanel: () => void;
}

const ArtifactPanelContext = createContext<ArtifactPanelState | null>(null);

export function useArtifactPanel(): ArtifactPanelState {
  const context = useContext(ArtifactPanelContext);
  if (!context) {
    throw new Error(
      "useArtifactPanel must be used within ArtifactPanelProvider",
    );
  }
  return context;
}

export default function ArtifactPanelProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const { backend } = useAppContext();
  const [openMessageId, setOpenMessageId] = useState<string | null>(null);
  const [openArtifactIndex, setOpenArtifactIndex] = useState<number | null>(
    null,
  );
  const dismissedRef = useRef<string | null>(null);
  const [dismissedId, setDismissedId] = useState<string | null>(null);

  // Close panel when conversation changes.
  const conversationId = backend.conversationId;
  useEffect(() => {
    setOpenMessageId(null);
    setOpenArtifactIndex(null);
    dismissedRef.current = null;
    setDismissedId(null);
  }, [conversationId]);

  const openArtifactPanel = useCallback(
    (messageId: string, artifactIndex: number) => {
      setOpenMessageId(messageId);
      setOpenArtifactIndex(artifactIndex);
      // Opening a different artifact resets the dismissed tracker.
      const newId = `${messageId}-${artifactIndex}`;
      if (dismissedRef.current !== newId) {
        dismissedRef.current = null;
        setDismissedId(null);
      }
    },
    [],
  );

  const closeArtifactPanel = useCallback(() => {
    // Record which artifact was explicitly closed so auto-open won't re-open it.
    if (openMessageId !== null && openArtifactIndex !== null) {
      const id = `${openMessageId}-${openArtifactIndex}`;
      dismissedRef.current = id;
      setDismissedId(id);
    }
    setOpenMessageId(null);
    setOpenArtifactIndex(null);
  }, [openMessageId, openArtifactIndex]);

  const value = useMemo(
    () => ({
      openMessageId,
      openArtifactIndex,
      dismissedId,
      openArtifactPanel,
      closeArtifactPanel,
    }),
    [
      openMessageId,
      openArtifactIndex,
      dismissedId,
      openArtifactPanel,
      closeArtifactPanel,
    ],
  );

  return (
    <ArtifactPanelContext.Provider value={value}>
      {children}
    </ArtifactPanelContext.Provider>
  );
}
