import { useState, useRef, useCallback, useEffect } from 'react';
import { useChimePlayer, type ChimeConfig } from './useChimePlayer';
import { useVoiceSession } from './useVoiceSession';

export interface UseVoiceCallOptions {
  sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>;
  sendBinary: (data: ArrayBuffer | Uint8Array) => void;
  onBinaryMessage: (handler: (data: ArrayBuffer) => void) => () => void;
  sendVoiceMessage: (text: string, model?: string, systemPromptSuffix?: string) => void;
  abortRun: () => void;
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
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
    conversationId,
    agentId,
    chimeConfig,
  } = options;

  const [isCallActive, setIsCallActive] = useState(false);
  const [isConnecting, setIsConnecting] = useState(false);
  const [callDuration, setCallDuration] = useState(0);
  const [isMuted, setIsMuted] = useState(false);
  const [callError, setCallError] = useState<string | null>(null);

  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const durationIntervalRef = useRef<ReturnType<typeof setInterval> | null>(
    null,
  );
  const waitingToneIntervalRef = useRef<ReturnType<typeof setInterval> | null>(
    null,
  );
  const interruptTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const wakeLockRef = useRef<WakeLockSentinel | null>(null);
  const endCallRef = useRef<() => void>(() => {});

  const chimePlayer = useChimePlayer(chimeConfig);
  const voiceSession = useVoiceSession({
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
      if (audioContext.state === 'suspended') {
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

      await voiceSession.start(audioContext, mediaStream);

      try {
        if ('wakeLock' in navigator) {
          const lock = await navigator.wakeLock.request('screen');
          wakeLockRef.current = lock;
        }
      } catch {
        // ignore wake lock failures
      }

      chimePlayer.play('agentDone');
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
  }, [chimePlayer, isCallActive, voiceSession]);

  const endCall = useCallback(() => {
    setIsCallActive(false);
    setIsMuted(false);
    setCallDuration(0);

    voiceSession.stop();

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
  }, [chimePlayer, voiceSession]);

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
    isUserSpeaking: voiceSession.isUserSpeaking,
    isPlaying: voiceSession.isPlaying,
    isSynthesizing: voiceSession.isSynthesizing,
    callError,
    startCall,
    endCall,
    toggleMute,
  };
}
