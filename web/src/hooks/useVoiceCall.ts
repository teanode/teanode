import { useState, useRef, useCallback, useEffect } from "react";
import { useChimePlayer, type ChimeConfig } from "./useChimePlayer";
import { useVoiceSession } from "./useVoiceSession";

const AGENT_WAITING_CHIME_MS = 3200;
const AGENT_WAITING_CHIME_MIN_GAP_MS = 1400;

export interface UseVoiceCallOptions {
  sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>;
  sendBinary: (data: ArrayBuffer | Uint8Array) => void;
  onBinaryMessage: (handler: (data: ArrayBuffer) => void) => () => void;
  onVoiceMessage: (
    handler: (message: Record<string, unknown>) => void,
  ) => () => void;
  sendVoiceMessage: (
    text: string,
    model?: string,
    systemPromptSuffix?: string,
  ) => void;
  abortRun: () => void;
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  connected: boolean;
  ttsVoice: string;
  conversationId: string | null;
  agentId: string;
  audioCapability: boolean;
  chimeConfig: ChimeConfig;
}

export interface UseVoiceCallReturn {
  isCallActive: boolean;
  isConnecting: boolean;
  callDuration: number;
  isMuted: boolean;
  isUserSpeaking: boolean;
  isPlaying: boolean;
  isSynthesizing: boolean;
  callError: string | null;
  startCall: () => Promise<void>;
  endCall: () => void;
  toggleMute: () => void;
}

export function useVoiceCall(options: UseVoiceCallOptions): UseVoiceCallReturn {
  const {
    sendRpc,
    sendBinary,
    onBinaryMessage,
    onVoiceMessage,
    isRunning,
    connected,
    conversationId,
    agentId,
    chimeConfig,
  } = options;

  const [isCallActive, setIsCallActive] = useState(false);
  const [isConnecting, setIsConnecting] = useState(false);
  const [callDuration, setCallDuration] = useState(0);
  const [isMuted, setIsMuted] = useState(false);
  const [callError, setCallError] = useState<string | null>(null);
  const [isAgentBusy, setIsAgentBusy] = useState(false);

  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const durationIntervalRef = useRef<ReturnType<typeof setInterval> | null>(
    null,
  );
  const waitingToneIntervalRef = useRef<ReturnType<typeof setInterval> | null>(
    null,
  );
  const lastWaitingChimeAtRef = useRef(0);
  const interruptTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const wakeLockRef = useRef<WakeLockSentinel | null>(null);
  const endCallRef = useRef<() => void>(() => {});
  const pendingAgentDoneChimeRef = useRef(false);

  const chimePlayer = useChimePlayer(chimeConfig);
  const playWaitingChime = useCallback(() => {
    const now = Date.now();
    if (now - lastWaitingChimeAtRef.current < AGENT_WAITING_CHIME_MIN_GAP_MS) {
      return;
    }
    lastWaitingChimeAtRef.current = now;
    chimePlayer.play("agentWaiting");
  }, [chimePlayer]);
  const {
    start: startVoiceSession,
    stop: stopVoiceSession,
    interruptPlayback,
    resumePlayback,
    isUserSpeaking,
    isPlaying,
    isSynthesizing,
  } = useVoiceSession({
    sendRpc,
    sendBinary,
    onBinaryMessage,
    conversationId,
    agentId,
  });

  const startCall = useCallback(async () => {
    if (isCallActive) return;
    setCallError(null);
    setIsConnecting(true);

    try {
      const audioContext = new AudioContext();
      if (audioContext.state === "suspended") {
        await audioContext.resume();
      }
      audioContextRef.current = audioContext;
      chimePlayer.init(audioContext);

      const mediaStream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });
      streamRef.current = mediaStream;

      await startVoiceSession(audioContext, mediaStream);

      try {
        if ("wakeLock" in navigator) {
          const lock = await navigator.wakeLock.request("screen");
          wakeLockRef.current = lock;
        }
      } catch {
        // ignore wake lock failures
      }

      chimePlayer.play("agentDone");
      setCallDuration(0);
      durationIntervalRef.current = setInterval(() => {
        setCallDuration((prev) => prev + 1);
      }, 1000);

      setIsCallActive(true);
      setIsConnecting(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setCallError(message);
      setIsConnecting(false);

      if (streamRef.current) {
        streamRef.current.getTracks().forEach((track) => track.stop());
        streamRef.current = null;
      }
      if (audioContextRef.current) {
        audioContextRef.current.close();
        audioContextRef.current = null;
      }
    }
  }, [chimePlayer, isCallActive, startVoiceSession]);

  useEffect(() => {
    if (!isCallActive) return;
    return onVoiceMessage((message) => {
      const type = message.type;
      const payload = message.payload as Record<string, unknown> | undefined;
      if (typeof type !== "string") return;
      if (type === "response.completed") {
        setIsAgentBusy(false);
        pendingAgentDoneChimeRef.current = true;
        return;
      }
      if (type === "response.started") {
        setIsAgentBusy(false);
        pendingAgentDoneChimeRef.current = false;
        resumePlayback();
        return;
      }
      if (type !== "turn.event" || !payload) return;
      const event = payload.event;
      if (event === "turn_committed") {
        setIsAgentBusy(true);
        chimePlayer.play("inputCaptured");
        playWaitingChime();
      } else if (event === "turn_queued") {
        setIsAgentBusy(true);
        playWaitingChime();
      } else if (event === "barge_in_triggered") {
        // Stop current playback immediately and ignore stale in-flight audio
        // until the next response starts.
        interruptPlayback();
      }
    });
  }, [
    chimePlayer,
    interruptPlayback,
    isCallActive,
    onVoiceMessage,
    playWaitingChime,
    resumePlayback,
  ]);

  useEffect(() => {
    if (!isCallActive) return;
    if (!pendingAgentDoneChimeRef.current) return;
    if (isPlaying || isSynthesizing) return;
    pendingAgentDoneChimeRef.current = false;
    chimePlayer.play("agentDone");
  }, [chimePlayer, isCallActive, isPlaying, isSynthesizing]);

  useEffect(() => {
    if (!isCallActive || isConnecting || connected) return;
    setCallError("Connection lost. Call ended.");
    endCallRef.current();
  }, [connected, isCallActive, isConnecting]);

  useEffect(() => {
    const shouldPlayWaiting = isCallActive && (isAgentBusy || isRunning);
    if (!shouldPlayWaiting) {
      if (waitingToneIntervalRef.current) {
        clearInterval(waitingToneIntervalRef.current);
        waitingToneIntervalRef.current = null;
      }
      return;
    }
    if (!waitingToneIntervalRef.current) {
      waitingToneIntervalRef.current = setInterval(() => {
        playWaitingChime();
      }, AGENT_WAITING_CHIME_MS);
    }
    return () => {
      if (waitingToneIntervalRef.current) {
        clearInterval(waitingToneIntervalRef.current);
        waitingToneIntervalRef.current = null;
      }
    };
  }, [isAgentBusy, isCallActive, isRunning, playWaitingChime]);

  const endCall = useCallback(() => {
    setIsCallActive(false);
    setIsMuted(false);
    setCallDuration(0);
    setIsAgentBusy(false);
    pendingAgentDoneChimeRef.current = false;

    stopVoiceSession();

    if (durationIntervalRef.current) {
      clearInterval(durationIntervalRef.current);
      durationIntervalRef.current = null;
    }
    if (waitingToneIntervalRef.current) {
      clearInterval(waitingToneIntervalRef.current);
      waitingToneIntervalRef.current = null;
    }
    if (interruptTimeoutRef.current) {
      clearTimeout(interruptTimeoutRef.current);
      interruptTimeoutRef.current = null;
    }

    if (streamRef.current) {
      streamRef.current.getTracks().forEach((track) => track.stop());
      streamRef.current = null;
    }

    if (wakeLockRef.current) {
      wakeLockRef.current.release().catch(() => {});
      wakeLockRef.current = null;
    }

    if (audioContextRef.current) {
      audioContextRef.current.close();
      audioContextRef.current = null;
    }

    chimePlayer.close();
  }, [chimePlayer, stopVoiceSession]);

  endCallRef.current = endCall;

  const toggleMute = useCallback(() => {
    const nextMuted = !isMuted;
    setIsMuted(nextMuted);
    if (streamRef.current) {
      streamRef.current.getAudioTracks().forEach((track) => {
        track.enabled = !nextMuted;
      });
    }
  }, [isMuted]);

  useEffect(() => {
    return () => {
      endCallRef.current();
    };
  }, []);

  return {
    isCallActive,
    isConnecting,
    callDuration,
    isMuted,
    isUserSpeaking,
    isPlaying,
    isSynthesizing,
    callError,
    startCall,
    endCall,
    toggleMute,
  };
}
