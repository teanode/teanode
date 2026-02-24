import React, { createContext, useContext, useEffect, useMemo } from "react";
import { Outlet, useParams } from "@tanstack/react-router";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { useAppContext } from "../../../context";
import {
  useVoiceCall,
  type UseVoiceCallReturn,
} from "../../../hooks/useVoiceCall";
import type { ChimeConfig } from "../../../hooks/useChimePlayer";

const VoiceCallContext = createContext<UseVoiceCallReturn | null>(null);

export function useAgentVoiceCall(): UseVoiceCallReturn {
  const context = useContext(VoiceCallContext);
  if (!context)
    throw new Error(
      "useAgentVoiceCall must be used within ConversationsAgentLayout",
    );
  return context;
}

/** /conversations/$agentId — layout that syncs the current agent and renders child routes. */
export default function ConversationsAgentLayout() {
  const { agentId } = useParams({ strict: false }) as { agentId: string };
  const {
    backend,
    ttsVoice,
    voiceChimesEnabled,
    voiceChimesVolume,
    voiceCallSttMode,
  } = useAppContext();

  useEffect(() => {
    if (agentId && agentId !== backend.currentAgentId) {
      backend.setCurrentAgentId(agentId);
    }
  }, [agentId, backend.currentAgentId, backend.setCurrentAgentId]);

  const chimeConfig: ChimeConfig = useMemo(
    () => ({
      enabled: voiceChimesEnabled,
      volume: voiceChimesVolume,
    }),
    [voiceChimesEnabled, voiceChimesVolume],
  );

  const voiceCall = useVoiceCall({
    sendRpc: backend.sendRpc,
    sendBinary: backend.sendBinary,
    onBinaryMessage: backend.onBinaryMessage,
    onVoiceMessage: backend.onVoiceMessage,
    sendVoiceMessage: backend.sendVoiceMessage,
    abortRun: backend.abortRun,
    isRunning: backend.isRunning,
    isStreaming: backend.isStreaming,
    streamText: backend.streamText,
    connected: backend.connected,
    ttsVoice,
    voiceCallSttMode,
    conversationId: backend.conversationId,
    agentId,
    audioCapability: backend.audioCapability,
    chimeConfig,
  });

  return (
    <VoiceCallContext.Provider value={voiceCall}>
      <Outlet />
      {voiceCall.callError && (
        <Box
          sx={{
            position: "fixed",
            bottom: 16,
            left: 16,
            right: 16,
            p: 2,
            bgcolor: "error.dark",
            color: "error.contrastText",
            borderRadius: 1,
            zIndex: 9999,
            maxHeight: "30vh",
            overflow: "auto",
          }}
        >
          <Typography
            variant="caption"
            sx={{
              fontFamily: "monospace",
              whiteSpace: "pre-wrap",
              wordBreak: "break-all",
            }}
          >
            Voice call failed: {voiceCall.callError}
          </Typography>
        </Box>
      )}
    </VoiceCallContext.Provider>
  );
}
