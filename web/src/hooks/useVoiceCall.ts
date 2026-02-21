import { useState, useRef, useCallback, useEffect } from "react";
import { useStreamingTTS } from "./useStreamingTTS";
import { useChimePlayer, type ChimeConfig } from "./useChimePlayer";

const VOICE_CALL_PROMPT =
  "The user is in a live voice call with you. Their messages are transcribed speech and your responses will be spoken aloud in real time. Keep responses brief and conversational — 1–3 sentences unless the user asks for more detail. Avoid markdown formatting, code blocks, and bullet lists.";

/** Split accumulated text into complete sentences and a remainder. */
function extractCompleteSentences(text: string): {
  sentences: string[];
  remainder: string;
} {
  // Match sentence-ending punctuation: ASCII (.!?) followed by whitespace/end,
  // or CJK punctuation (。！？) which doesn't require trailing whitespace.
  const abbreviations =
    /(?:Mr|Mrs|Ms|Dr|Prof|Sr|Jr|St|vs|etc|approx|Inc|Ltd|Corp|U\.S|U\.K|e\.g|i\.e)\.\s*$/;
  const sentenceBoundary = /[.!?](?:\s|$)|[。！？]/;
  const sentences: string[] = [];
  let remainder = text;

  while (true) {
    const match = remainder.match(sentenceBoundary);
    if (!match || match.index === undefined) break;

    const punctuation = match[0];
    const isCJK = /[。！？]/.test(punctuation);
    const candidateEnd = match.index + (isCJK ? 1 : 1); // include the punctuation character
    const candidate = remainder.slice(0, candidateEnd).trim();

    // Skip if it looks like an abbreviation (only applies to ASCII period)
    if (!isCJK && abbreviations.test(candidate)) {
      const afterMatch = match.index + match[0].length;
      if (afterMatch >= remainder.length) break;
      const nextPart = remainder.slice(afterMatch);
      const nextMatch = nextPart.match(sentenceBoundary);
      if (!nextMatch || nextMatch.index === undefined) break;
      const nextIsCJK = /[。！？]/.test(nextMatch[0]);
      const fullEnd = afterMatch + nextMatch.index + (nextIsCJK ? 1 : 1);
      sentences.push(remainder.slice(0, fullEnd).trim());
      remainder = remainder.slice(fullEnd).trimStart();
      continue;
    }

    sentences.push(candidate);
    remainder = remainder.slice(candidateEnd).trimStart();
  }

  return { sentences, remainder };
}

/** Convert Float32Array PCM (16kHz, mono) to a WAV blob. */
function pcmToWav(samples: Float32Array, sampleRate: number): Blob {
  const numChannels = 1;
  const bitsPerSample = 16;
  const byteRate = sampleRate * numChannels * (bitsPerSample / 8);
  const blockAlign = numChannels * (bitsPerSample / 8);
  const dataSize = samples.length * (bitsPerSample / 8);
  const buffer = new ArrayBuffer(44 + dataSize);
  const view = new DataView(buffer);

  // RIFF header
  writeString(view, 0, "RIFF");
  view.setUint32(4, 36 + dataSize, true);
  writeString(view, 8, "WAVE");

  // fmt chunk
  writeString(view, 12, "fmt ");
  view.setUint32(16, 16, true); // chunk size
  view.setUint16(20, 1, true); // PCM format
  view.setUint16(22, numChannels, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, byteRate, true);
  view.setUint16(32, blockAlign, true);
  view.setUint16(34, bitsPerSample, true);

  // data chunk
  writeString(view, 36, "data");
  view.setUint32(40, dataSize, true);

  // Write PCM samples (clamp to int16 range)
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
  sendVoiceMessage: (
    text: string,
    model?: string,
    systemPromptSuffix?: string,
  ) => void;
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
  /** Set when startCall fails. Cleared on next attempt. */
  callError: string | null;
  startCall: () => Promise<void>;
  endCall: () => void;
  toggleMute: () => void;
}

export function useVoiceCall(options: UseVoiceCallOptions): UseVoiceCallReturn {
  const MIN_INTERRUPT_MS = 500;

  const speechStartTimeRef = useRef<number | null>(null);
  const pendingInterruptRef = useRef(false);

  const {
    sendVoiceMessage,
    abortRun,
    isRunning,
    isStreaming,
    streamText,
    ttsVoice,
    conversationId,
    agentId,
    chimeConfig,
  } = options;

  const [isCallActive, setIsCallActive] = useState(false);
  const [isConnecting, setIsConnecting] = useState(false);
  const [callDuration, setCallDuration] = useState(0);
  const [isMuted, setIsMuted] = useState(false);
  const [isUserSpeaking, setIsUserSpeaking] = useState(false);
  const [ttsAudioContext, setTtsAudioContext] = useState<AudioContext | null>(
    null,
  );
  const [callError, setCallError] = useState<string | null>(null);

  // Refs for mutable state across callbacks
  const vadRef = useRef<{
    destroy: () => void;
    pause: () => void;
    start: () => void;
    receive: (node: AudioNode) => void;
  } | null>(null);
  const sourceNodeRef = useRef<MediaStreamAudioSourceNode | null>(null);
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
  const interruptedRef = useRef(false);
  const isCallActiveRef = useRef(false);
  const conversationIdRef = useRef(conversationId);
  const agentIdRef = useRef(agentId);
  const isRunningRef = useRef(isRunning);
  const prevStreamTextLengthRef = useRef(0);
  const sentencesEnqueuedRef = useRef(0);
  const wasRunningRef = useRef(false);
  const wasStreamingRef = useRef(false);

  conversationIdRef.current = conversationId;
  agentIdRef.current = agentId;
  isRunningRef.current = isRunning;

  const chimePlayer = useChimePlayer(chimeConfig);

  const handleTurnComplete = useCallback(() => {
    // Pause VAD while the chime plays to prevent echo feedback on iOS
    // (speaker output leaks back through the mic, VAD re-detects as speech).
    vadRef.current?.pause();
    chimePlayer.play("agentDone");
    setTimeout(() => {
      if (isCallActiveRef.current) vadRef.current?.start();
    }, 300);
  }, [chimePlayer]);

  const streamingTTS = useStreamingTTS({
    voice: ttsVoice,
    audioContext: ttsAudioContext,
    onTurnComplete: handleTurnComplete,
  });

  const startCall = useCallback(async () => {
    if (isCallActiveRef.current) return;
    setCallError(null);
    setIsConnecting(true);

    try {
      // Create AudioContext immediately during user gesture — iOS Safari
      // suspends AudioContexts created outside gesture handlers.
      const audioContext = new AudioContext();
      audioContextRef.current = audioContext;
      setTtsAudioContext(audioContext);
      if (audioContext.state === "suspended") {
        await audioContext.resume();
      }

      // Attach chime player to the shared AudioContext.
      chimePlayer.init(audioContext);

      // Request mic with echo cancellation
      const mediaStream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });
      streamRef.current = mediaStream;

      // Connect mic stream to our AudioContext
      const sourceNode = new MediaStreamAudioSourceNode(audioContext, {
        mediaStream: mediaStream,
      });
      sourceNodeRef.current = sourceNode;

      // Use AudioNodeVAD (not MicVAD) so we control the AudioContext lifecycle.
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
          setIsUserSpeaking(true);

          // Delay interruption slightly so brief noise doesn't barge in.
          speechStartTimeRef.current = Date.now();
          pendingInterruptRef.current = true;
          if (interruptTimeoutRef.current) {
            clearTimeout(interruptTimeoutRef.current);
          }
          interruptTimeoutRef.current = setTimeout(() => {
            if (!isCallActiveRef.current || !pendingInterruptRef.current)
              return;
            const startedAt = speechStartTimeRef.current;
            if (!startedAt || Date.now() - startedAt < MIN_INTERRUPT_MS) return;

            // Interrupt: stop TTS and abort running LLM after sustained speech.
            streamingTTS.stopAndClear();
            if (isRunningRef.current) {
              abortRun();
              interruptedRef.current = true;
            }
          }, MIN_INTERRUPT_MS);
        },
        onSpeechEnd: async (audioData: Float32Array) => {
          if (!isCallActiveRef.current) return;
          setIsUserSpeaking(false);
          pendingInterruptRef.current = false;
          speechStartTimeRef.current = null;
          if (interruptTimeoutRef.current) {
            clearTimeout(interruptTimeoutRef.current);
            interruptTimeoutRef.current = null;
          }

          // Pause VAD while the chime plays to prevent echo feedback on iOS
          // (speaker output leaks back through the mic, VAD detects it as speech).
          vadRef.current?.pause();
          chimePlayer.play("inputCaptured");
          setTimeout(() => {
            if (isCallActiveRef.current) vadRef.current?.start();
          }, 300);

          // Convert PCM to WAV and transcribe
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

            // Reset stream tracking for new response
            interruptedRef.current = false;
            prevStreamTextLengthRef.current = 0;
            sentencesEnqueuedRef.current = 0;

            // Send as voice message with voice mode system prompt
            sendVoiceMessage(text, undefined, VOICE_CALL_PROMPT);
          } catch (error) {
            console.error("Voice call transcription error:", error);
          }
        },
        positiveSpeechThreshold: 0.8,
        negativeSpeechThreshold: 0.4,
        minSpeechFrames: 5,
        redemptionFrames: 12,
      });

      vad.receive(sourceNode);
      vad.start();
      vadRef.current = vad;

      // Signal that the call is ready to accept speech.
      chimePlayer.play("agentDone");

      // Request WakeLock (progressive enhancement)
      try {
        if ("wakeLock" in navigator) {
          const lock = await navigator.wakeLock.request("screen");
          wakeLockRef.current = lock;
        }
      } catch {
        /* WakeLock not available */
      }

      // Start duration timer
      setCallDuration(0);
      durationIntervalRef.current = setInterval(() => {
        setCallDuration((previous) => previous + 1);
      }, 1000);

      isCallActiveRef.current = true;
      setIsConnecting(false);
      setIsCallActive(true);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error("Failed to start voice call:", message, error);
      setCallError(message);
      setIsConnecting(false);
      // Cleanup on failure
      if (streamRef.current) {
        streamRef.current.getTracks().forEach((track) => track.stop());
        streamRef.current = null;
      }
      if (audioContextRef.current) {
        audioContextRef.current.close();
        audioContextRef.current = null;
      }
    }
  }, [abortRun, sendVoiceMessage, streamingTTS, chimePlayer]);

  const endCall = useCallback(() => {
    isCallActiveRef.current = false;
    setIsCallActive(false);
    setIsUserSpeaking(false);
    setIsMuted(false);
    setCallDuration(0);

    // Stop VAD and disconnect source
    if (sourceNodeRef.current) {
      sourceNodeRef.current.disconnect();
      sourceNodeRef.current = null;
    }
    if (vadRef.current) {
      vadRef.current.destroy();
      vadRef.current = null;
    }

    // Stop mic stream
    if (streamRef.current) {
      streamRef.current.getTracks().forEach((track) => track.stop());
      streamRef.current = null;
    }

    // Close AudioContext
    if (audioContextRef.current) {
      audioContextRef.current.close();
      audioContextRef.current = null;
    }

    // Stop TTS
    streamingTTS.stopAndClear();
    setTtsAudioContext(null);

    // Close chime player
    chimePlayer.close();

    // Release WakeLock
    if (wakeLockRef.current) {
      wakeLockRef.current.release().catch(() => {});
      wakeLockRef.current = null;
    }

    // Clear duration timer
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

    // Reset stream tracking
    interruptedRef.current = false;
    pendingInterruptRef.current = false;
    speechStartTimeRef.current = null;
    prevStreamTextLengthRef.current = 0;
    sentencesEnqueuedRef.current = 0;
  }, [streamingTTS, chimePlayer]);

  const toggleMute = useCallback(() => {
    if (!streamRef.current) return;
    const tracks = streamRef.current.getAudioTracks();
    setIsMuted((previous) => {
      const newMuted = !previous;
      tracks.forEach((track) => {
        track.enabled = !newMuted;
      });
      return newMuted;
    });
  }, []);

  // Monitor stream text for sentence extraction and TTS enqueueing
  useEffect(() => {
    if (!isCallActiveRef.current || !isStreaming || interruptedRef.current)
      return;

    // Detect stream text reset (new message in a multi-message response):
    // if streamText is shorter than what we previously tracked, the backend
    // started a new streaming segment — reset our sentence counter.
    if (streamText.length < prevStreamTextLengthRef.current) {
      sentencesEnqueuedRef.current = 0;
    }
    prevStreamTextLengthRef.current = streamText.length;

    const { sentences } = extractCompleteSentences(streamText);
    // Only enqueue new sentences we haven't sent yet
    const newSentences = sentences.slice(sentencesEnqueuedRef.current);
    for (const sentence of newSentences) {
      streamingTTS.enqueueSentence(sentence);
    }
    sentencesEnqueuedRef.current = sentences.length;
  }, [streamText, isStreaming, streamingTTS]);

  // Flush remainder when a streaming segment ends (isStreaming transitions false)
  // so TTS can synthesize the tail while tool calls execute.
  useEffect(() => {
    if (wasStreamingRef.current && !isStreaming && isCallActiveRef.current) {
      if (!interruptedRef.current && streamText) {
        const { sentences, remainder } = extractCompleteSentences(streamText);
        const newSentences = sentences.slice(sentencesEnqueuedRef.current);
        for (const sentence of newSentences) {
          streamingTTS.enqueueSentence(sentence);
        }
        if (remainder.trim()) {
          streamingTTS.enqueueSentence(remainder.trim());
        }
        sentencesEnqueuedRef.current =
          sentences.length + (remainder.trim() ? 1 : 0);
      }
    }
    wasStreamingRef.current = isStreaming;
  }, [isStreaming, streamText, streamingTTS]);

  // Reset tracking when run finishes (isRunning transitions false)
  useEffect(() => {
    if (wasRunningRef.current && !isRunning) {
      prevStreamTextLengthRef.current = 0;
      sentencesEnqueuedRef.current = 0;
    }
    wasRunningRef.current = isRunning;
  }, [isRunning]);

  useEffect(() => {
    const shouldPlayWaitingTone =
      isCallActive && isRunning && !isUserSpeaking && !streamingTTS.isPlaying;

    if (shouldPlayWaitingTone) {
      if (!waitingToneIntervalRef.current) {
        chimePlayer.play("agentWaiting");
        waitingToneIntervalRef.current = setInterval(() => {
          if (isCallActiveRef.current) chimePlayer.play("agentWaiting");
        }, 2200);
      }
      return;
    }

    if (waitingToneIntervalRef.current) {
      clearInterval(waitingToneIntervalRef.current);
      waitingToneIntervalRef.current = null;
    }
  }, [
    isCallActive,
    isRunning,
    isUserSpeaking,
    streamingTTS.isPlaying,
    chimePlayer,
  ]);

  // Re-acquire WakeLock when returning from background
  useEffect(() => {
    if (!isCallActive) return;

    const handleVisibilityChange = async () => {
      if (document.visibilityState === "visible") {
        // Resume AudioContext (iOS suspends it when backgrounded)
        if (audioContextRef.current?.state === "suspended") {
          audioContextRef.current.resume().catch(() => {});
        }
        // Re-acquire WakeLock
        if (!wakeLockRef.current && "wakeLock" in navigator) {
          try {
            wakeLockRef.current = await navigator.wakeLock.request("screen");
          } catch {
            /* ignore */
          }
        }
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () =>
      document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, [isCallActive]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (isCallActiveRef.current) {
        // Force cleanup without state updates
        if (sourceNodeRef.current) sourceNodeRef.current.disconnect();
        if (vadRef.current) vadRef.current.destroy();
        if (streamRef.current)
          streamRef.current.getTracks().forEach((track) => track.stop());
        if (audioContextRef.current) audioContextRef.current.close();
        if (wakeLockRef.current) wakeLockRef.current.release().catch(() => {});
        if (durationIntervalRef.current)
          clearInterval(durationIntervalRef.current);
        if (waitingToneIntervalRef.current)
          clearInterval(waitingToneIntervalRef.current);
        if (interruptTimeoutRef.current)
          clearTimeout(interruptTimeoutRef.current);
      }
    };
  }, []);

  return {
    isCallActive,
    isConnecting,
    callDuration,
    isMuted,
    isUserSpeaking,
    isPlaying: streamingTTS.isPlaying,
    isSynthesizing: streamingTTS.isSynthesizing,
    callError,
    startCall,
    endCall,
    toggleMute,
  };
}
