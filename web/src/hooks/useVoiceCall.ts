import { useState, useRef, useCallback, useEffect } from "react";
import { useChimePlayer, type ChimeConfig } from "./useChimePlayer";
import { useVoiceSession } from "./useVoiceSession";
import type { VoiceCallSTTMode } from "../context";

/** Interval between periodic "agent is busy" chime tones. */
const AGENT_WAITING_CHIME_MS = 3200;
/** Minimum gap between consecutive waiting chimes to avoid overlapping. */
const AGENT_WAITING_CHIME_MIN_GAP_MS = 1400;
/** How long the user must speak before we interrupt queued/in-flight TTS (normal path). */
const MIN_INTERRUPT_MS = 1500;
/** After TTS stops, ignore VAD speech events for this long to suppress echo artifacts. */
const TTS_ECHO_COOLDOWN_MS = 800;
/** During active TTS, the user must speak continuously for this long to trigger a barge-in. */
const TTS_BARGE_IN_MS = 2000;

/** VAD parameters when TTS is idle — sensitive, quick to detect speech. */
const VAD_PARAMS_SENSITIVE = {
  /** Probability threshold (0–1) above which a frame is classified as speech. */
  positiveSpeechThreshold: 0.8,
  /** Probability threshold (0–1) below which a frame is classified as silence. */
  negativeSpeechThreshold: 0.4,
  /** Minimum consecutive speech frames required before triggering onSpeechStart. */
  minSpeechFrames: 5,
  /** Silence frames tolerated within speech before triggering onSpeechEnd. */
  redemptionFrames: 12,
};
/** VAD parameters during TTS playback — less sensitive to reduce echo false positives. */
const VAD_PARAMS_DURING_TTS = {
  positiveSpeechThreshold: 0.92,
  negativeSpeechThreshold: 0.65,
  minSpeechFrames: 8,
  redemptionFrames: 6,
};


function pcmToWav(samples: Float32Array, sampleRate: number): Blob {
  const numChannels = 1;
  const bitsPerSample = 16;
  const byteRate = sampleRate * numChannels * (bitsPerSample / 8);
  const blockAlign = numChannels * (bitsPerSample / 8);
  const dataSize = samples.length * (bitsPerSample / 8);
  const buffer = new ArrayBuffer(44 + dataSize);
  const view = new DataView(buffer);

  writeString(view, 0, "RIFF");
  view.setUint32(4, 36 + dataSize, true);
  writeString(view, 8, "WAVE");
  writeString(view, 12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, numChannels, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, byteRate, true);
  view.setUint16(32, blockAlign, true);
  view.setUint16(34, bitsPerSample, true);
  writeString(view, 36, "data");
  view.setUint32(40, dataSize, true);

  let offset = 44;
  for (let index = 0; index < samples.length; index++) {
    const sample = Math.max(-1, Math.min(1, samples[index]));
    view.setInt16(offset, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
    offset += 2;
  }
  return new Blob([buffer], { type: "audio/wav" });
}

function writeString(view: DataView, offset: number, value: string): void {
  for (let index = 0; index < value.length; index++) {
    view.setUint8(offset + index, value.charCodeAt(index));
  }
}

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
    voiceMode?: "call" | "input",
  ) => void;
  abortRun: () => void;
  isRunning: boolean;
  isStreaming: boolean;
  streamText: string;
  connected: boolean;
  ttsVoice: string;
  voiceCallSttMode: VoiceCallSTTMode;
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
  interruptAgent: () => void;
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
    abortRun,
    sendVoiceMessage,
    isRunning,
    connected,
    voiceCallSttMode,
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
  const [isClientUserSpeaking, setIsClientUserSpeaking] = useState(false);
  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const vadRef = useRef<{
    destroy: () => void;
    pause: () => void;
    start: () => void;
    receive: (node: AudioNode) => void;
  } | null>(null);
  const sourceNodeRef = useRef<MediaStreamAudioSourceNode | null>(null);
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
  const localSpeechInterruptIssuedRef = useRef(false);
  const isCallActiveRef = useRef(false);
  const speechStartTimeRef = useRef<number | null>(null);
  const pendingInterruptRef = useRef(false);
  const ttsActiveRef = useRef(false);
  const ttsEndedAtRef = useRef(0);
  const bargeInTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const bargedInRef = useRef(false);

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
    cancelResponse,
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

  useEffect(() => {
    const active = isPlaying || isSynthesizing;
    const wasActive = ttsActiveRef.current;
    ttsActiveRef.current = active;
    if (wasActive && !active) {
      ttsEndedAtRef.current = Date.now();
    }
    // Swap VAD sensitivity: less sensitive during TTS to reduce echo triggers,
    // more sensitive when idle so user speech is caught quickly.
    // frameProcessor is private in TS but accessible at runtime.
    const vad = vadRef.current as Record<string, unknown> | null;
    const frameProcessor = vad?.frameProcessor as
      | { options: Record<string, number> }
      | undefined;
    if (frameProcessor?.options) {
      const params = active ? VAD_PARAMS_DURING_TTS : VAD_PARAMS_SENSITIVE;
      Object.assign(frameProcessor.options, params);
    }
  }, [isPlaying, isSynthesizing]);

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

      await startVoiceSession(audioContext, mediaStream, {
        enableServerStt: voiceCallSttMode === "server",
      });
      isCallActiveRef.current = true;

      if (voiceCallSttMode === "client") {
        const sourceNode = new MediaStreamAudioSourceNode(audioContext, {
          mediaStream,
        });
        sourceNodeRef.current = sourceNode;

        const { AudioNodeVAD } = await import("@ricky0123/vad-web");
        const vad = await AudioNodeVAD.new(audioContext, {
          ortConfig: (ort) => {
            ort.env.wasm.wasmPaths = "/";
            if (
              typeof crossOriginIsolated !== "undefined" &&
              !crossOriginIsolated
            ) {
              ort.env.wasm.numThreads = 1;
            }
          },
          onSpeechStart: () => {
            if (!isCallActiveRef.current) return;

            // Echo cooldown after TTS ends — always reject.
            if (
              !ttsActiveRef.current &&
              Date.now() - ttsEndedAtRef.current < TTS_ECHO_COOLDOWN_MS
            ) {
              return;
            }

            // During active TTS — start sustained-speech barge-in timer.
            if (ttsActiveRef.current) {
              if (bargeInTimeoutRef.current) {
                clearTimeout(bargeInTimeoutRef.current);
              }
              bargeInTimeoutRef.current = setTimeout(() => {
                if (!isCallActiveRef.current) return;
                bargedInRef.current = true;
                interruptPlayback();
                cancelResponse("client_barge_in").catch(() => {});
                setIsClientUserSpeaking(true);
                speechStartTimeRef.current = Date.now();
              }, TTS_BARGE_IN_MS);
              return;
            }

            // Normal (non-TTS) speech start.
            setIsClientUserSpeaking(true);
            speechStartTimeRef.current = Date.now();
            pendingInterruptRef.current = true;
            if (interruptTimeoutRef.current) {
              clearTimeout(interruptTimeoutRef.current);
            }
            interruptTimeoutRef.current = setTimeout(() => {
              if (!isCallActiveRef.current || !pendingInterruptRef.current) {
                return;
              }
              const speechStartTime = speechStartTimeRef.current;
              if (
                speechStartTime === null ||
                Date.now() - speechStartTime < MIN_INTERRUPT_MS
              ) {
                return;
              }
              interruptPlayback();
              cancelResponse("client_barge_in").catch(() => {});
            }, MIN_INTERRUPT_MS);
          },
          onSpeechEnd: async (audioData: Float32Array) => {
            if (!isCallActiveRef.current) return;
            setIsClientUserSpeaking(false);
            pendingInterruptRef.current = false;
            speechStartTimeRef.current = null;
            if (interruptTimeoutRef.current) {
              clearTimeout(interruptTimeoutRef.current);
              interruptTimeoutRef.current = null;
            }
            // Clear barge-in timer if speech ended before threshold.
            if (bargeInTimeoutRef.current) {
              clearTimeout(bargeInTimeoutRef.current);
              bargeInTimeoutRef.current = null;
            }
            // If user successfully barged in, skip the echo gate.
            if (bargedInRef.current) {
              bargedInRef.current = false;
            } else if (
              ttsActiveRef.current ||
              Date.now() - ttsEndedAtRef.current < TTS_ECHO_COOLDOWN_MS
            ) {
              // Echo gate — discard audio.
              return;
            }

            vadRef.current?.pause();
            chimePlayer.play("inputCaptured");
            setTimeout(() => {
              if (isCallActiveRef.current) vadRef.current?.start();
            }, 300);

            const wavBlob = pcmToWav(audioData, 16000);
            const formData = new FormData();
            formData.append("file", wavBlob, "audio.wav");

            try {
              const response = await fetch("/api/v1/audio/transcribe", {
                method: "POST",
                body: formData,
              });
              if (!response.ok) return;
              const result = await response.json();
              const text = result.text?.trim();
              if (!text) return;
              sendVoiceMessage(text, undefined, "call");
            } catch {
              // ignore transcription failures
            }
          },
          ...VAD_PARAMS_SENSITIVE,
        });
        vad.receive(sourceNode);
        vad.start();
        vadRef.current = vad;
      }

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
      isCallActiveRef.current = false;
      const message = error instanceof Error ? error.message : String(error);
      setCallError(message);
      setIsConnecting(false);

      if (streamRef.current) {
        streamRef.current.getTracks().forEach((track) => track.stop());
        streamRef.current = null;
      }
      if (sourceNodeRef.current) {
        sourceNodeRef.current.disconnect();
        sourceNodeRef.current = null;
      }
      if (vadRef.current) {
        vadRef.current.destroy();
        vadRef.current = null;
      }
      if (audioContextRef.current) {
        audioContextRef.current.close();
        audioContextRef.current = null;
      }
    }
  }, [
    cancelResponse,
    chimePlayer,
    interruptPlayback,
    isCallActive,
    sendVoiceMessage,
    startVoiceSession,
    voiceCallSttMode,
  ]);

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
      if (event === "turnCommitted") {
        setIsAgentBusy(true);
        chimePlayer.play("inputCaptured");
        playWaitingChime();
      } else if (event === "turnQueued") {
        setIsAgentBusy(true);
        playWaitingChime();
      } else if (event === "bargeInTriggered") {
        // Stop current playback immediately and ignore stale in-flight audio
        // until the next response starts.
        interruptPlayback();
        localSpeechInterruptIssuedRef.current = true;
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
    if (!isUserSpeaking) {
      localSpeechInterruptIssuedRef.current = false;
      return;
    }
    // Client-side hard interrupt to eliminate audible overlap from buffered
    // chunks before server-side barge_in/flush events are observed.
    if (
      (isPlaying || isSynthesizing) &&
      !localSpeechInterruptIssuedRef.current
    ) {
      interruptPlayback();
      abortRun();
      localSpeechInterruptIssuedRef.current = true;
    }
  }, [
    abortRun,
    interruptPlayback,
    isCallActive,
    isPlaying,
    isSynthesizing,
    isUserSpeaking,
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
    isCallActiveRef.current = false;
    setIsCallActive(false);
    setIsClientUserSpeaking(false);
    setIsMuted(false);
    setCallDuration(0);
    setIsAgentBusy(false);
    pendingAgentDoneChimeRef.current = false;
    pendingInterruptRef.current = false;
    speechStartTimeRef.current = null;

    stopVoiceSession();

    if (sourceNodeRef.current) {
      sourceNodeRef.current.disconnect();
      sourceNodeRef.current = null;
    }
    if (vadRef.current) {
      vadRef.current.destroy();
      vadRef.current = null;
    }

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
    if (bargeInTimeoutRef.current) {
      clearTimeout(bargeInTimeoutRef.current);
      bargeInTimeoutRef.current = null;
    }
    bargedInRef.current = false;

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

  const interruptAgent = useCallback(() => {
    interruptPlayback();
    cancelResponse("client_manual_interrupt").catch(() => {});
  }, [interruptPlayback, cancelResponse]);

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
    isUserSpeaking:
      voiceCallSttMode === "client" ? isClientUserSpeaking : isUserSpeaking,
    isPlaying,
    isSynthesizing,
    callError,
    interruptAgent,
    startCall,
    endCall,
    toggleMute,
  };
}
