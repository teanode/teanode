import { useCallback, useEffect, useRef, useState } from "react";

const FRAME_MAGIC = 0xa1;
const FRAME_TYPE_AUDIO_IN = 0x01;
const FRAME_TYPE_AUDIO_OUT = 0x02;
const FRAME_TYPE_FLUSH = 0x03;
const FRAME_HEADER_BYTES = 18;
const INPUT_FRAME_SAMPLES = 320; // 20ms @ 16kHz
const PROCESSOR_BUFFER_SIZE = 1024; // Must be 0 or power-of-two in [256..16384]
const INPUT_SAMPLE_RATE_HZ = 16000;
const CLIENT_SPEECH_RMS_THRESHOLD = 0.003;
const CLIENT_SPEECH_HANGOVER_MS = 100;

type BinarySender = (data: ArrayBuffer | Uint8Array) => void;
type BinarySubscriber = (handler: (data: ArrayBuffer) => void) => () => void;

interface VoiceStartResult {
  sessionId: string;
  conversationId?: string;
  pipeline?: "classic" | "realtime";
}

export interface UseVoiceSessionOptions {
  sendRpc: <T = unknown>(method: string, params: unknown) => Promise<T>;
  sendBinary: BinarySender;
  onBinaryMessage: BinarySubscriber;
  conversationId: string | null;
  agentId: string;
}

export interface VoiceSessionRuntime {
  start: (
    audioContext: AudioContext,
    mediaStream: MediaStream,
    options?: VoiceSessionStartOptions,
  ) => Promise<void>;
  stop: () => void;
  interruptPlayback: () => void;
  resumePlayback: () => void;
  cancelResponse: (reason?: string) => Promise<void>;
  isUserSpeaking: boolean;
  isPlaying: boolean;
  isSynthesizing: boolean;
  /** The active pipeline mode, set after start() completes. */
  activePipeline: "classic" | "realtime" | null;
}

export interface VoiceSessionStartOptions {
  enableServerStt?: boolean;
  /** Voice pipeline mode: "classic" (STT→LLM→TTS) or "realtime" (OpenAI Realtime API). */
  pipeline?: "classic" | "realtime";
}

export function useVoiceSession(
  options: UseVoiceSessionOptions,
): VoiceSessionRuntime {
  const { sendRpc, sendBinary, onBinaryMessage, conversationId, agentId } =
    options;

  const [isUserSpeaking, setIsUserSpeaking] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);
  const [isSynthesizing, setIsSynthesizing] = useState(false);
  const [activePipeline, setActivePipeline] = useState<
    "classic" | "realtime" | null
  >(null);

  const sessionIdRef = useRef<string | null>(null);
  const mediaSourceRef = useRef<MediaStreamAudioSourceNode | null>(null);
  const processorRef = useRef<ScriptProcessorNode | null>(null);
  const inputSeqRef = useRef<bigint>(BigInt(0));
  const outputQueueRef = useRef<AudioBuffer[]>([]);
  const currentSourceRef = useRef<AudioBufferSourceNode | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);
  const unsubscribeBinaryRef = useRef<(() => void) | null>(null);
  const pendingInputSamplesRef = useRef<Float32Array>(new Float32Array(0));
  const inputFramesSentRef = useRef(0);
  const outputFramesRecvRef = useRef(0);
  const lastVoiceDetectedAtRef = useRef(0);
  const dropIncomingAudioRef = useRef(false);

  const resampleTo16k = useCallback(
    (input: Float32Array, inputRate: number): Float32Array => {
      if (inputRate === INPUT_SAMPLE_RATE_HZ) {
        return input;
      }
      if (inputRate <= 0 || input.length === 0) {
        return new Float32Array(0);
      }
      const ratio = inputRate / INPUT_SAMPLE_RATE_HZ;
      const outputLength = Math.max(0, Math.floor(input.length / ratio));
      const output = new Float32Array(outputLength);
      for (let i = 0; i < outputLength; i++) {
        const sourcePos = i * ratio;
        const left = Math.floor(sourcePos);
        const right = Math.min(left + 1, input.length - 1);
        const frac = sourcePos - left;
        output[i] = input[left] + (input[right] - input[left]) * frac;
      }
      return output;
    },
    [],
  );

  const playNext = useCallback(() => {
    const context = audioContextRef.current;
    if (!context) {
      return;
    }
    if (currentSourceRef.current) {
      return;
    }
    const next = outputQueueRef.current.shift();
    if (!next) {
      setIsPlaying(false);
      // Normal end-of-response path: queue drained without an explicit flush.
      setIsSynthesizing(false);
      return;
    }
    const source = context.createBufferSource();
    source.buffer = next;
    source.connect(context.destination);
    source.onended = () => {
      currentSourceRef.current = null;
      playNext();
    };
    currentSourceRef.current = source;
    setIsPlaying(true);
    source.start();
  }, []);

  const handleFlush = useCallback(() => {
    console.debug("[voice][session] flush received", {
      queued: outputQueueRef.current.length,
      hasCurrentSource: Boolean(currentSourceRef.current),
    });
    outputQueueRef.current = [];
    if (currentSourceRef.current) {
      try {
        currentSourceRef.current.stop();
      } catch {
        // ignore stop errors
      }
      currentSourceRef.current = null;
    }
    setIsPlaying(false);
    setIsSynthesizing(false);
  }, []);

  const handleAudioOutFrame = useCallback(
    (pcmBytes: Uint8Array) => {
      if (dropIncomingAudioRef.current) {
        return;
      }
      const context = audioContextRef.current;
      if (!context || pcmBytes.byteLength < 2) {
        return;
      }
      const sampleCount = Math.floor(pcmBytes.byteLength / 2);
      const buffer = context.createBuffer(1, sampleCount, 24000);
      const channel = buffer.getChannelData(0);
      const view = new DataView(
        pcmBytes.buffer,
        pcmBytes.byteOffset,
        sampleCount * 2,
      );
      for (let i = 0; i < sampleCount; i++) {
        channel[i] = view.getInt16(i * 2, true) / 32768;
      }
      outputQueueRef.current.push(buffer);
      setIsSynthesizing(true);
      outputFramesRecvRef.current += 1;
      if (outputFramesRecvRef.current % 20 === 0) {
        console.debug("[voice][session] output frames", {
          count: outputFramesRecvRef.current,
          queueDepth: outputQueueRef.current.length,
          bytes: pcmBytes.byteLength,
        });
      }
      playNext();
    },
    [playNext],
  );

  const handleBinary = useCallback(
    (data: ArrayBuffer) => {
      if (data.byteLength < FRAME_HEADER_BYTES) {
        return;
      }
      const view = new DataView(data);
      const magic = view.getUint8(0);
      if (magic !== FRAME_MAGIC) {
        return;
      }
      const frameType = view.getUint8(1);
      if (frameType === FRAME_TYPE_FLUSH) {
        handleFlush();
        return;
      }
      if (frameType !== FRAME_TYPE_AUDIO_OUT) {
        return;
      }
      const payload = new Uint8Array(data.slice(FRAME_HEADER_BYTES));
      handleAudioOutFrame(payload);
    },
    [handleAudioOutFrame, handleFlush],
  );

  const encodeInputFrame = useCallback((pcmData: Int16Array): Uint8Array => {
    inputSeqRef.current += BigInt(1);
    const seq = inputSeqRef.current;
    const now = BigInt(Date.now());
    const payloadLength = pcmData.length * 2;
    const buffer = new ArrayBuffer(FRAME_HEADER_BYTES + payloadLength);
    const view = new DataView(buffer);
    view.setUint8(0, FRAME_MAGIC);
    view.setUint8(1, FRAME_TYPE_AUDIO_IN);

    const seqHi = Number(seq >> BigInt(16));
    const seqLo = Number(seq & BigInt(0xffff));
    view.setUint32(2, seqHi, false);
    view.setUint16(6, seqLo, false);

    const tsHi = Number(now >> BigInt(32)) >>> 0;
    const tsLo = Number(now & BigInt(0xffffffff));
    view.setUint32(8, tsHi, false);
    view.setUint32(12, tsLo, false);
    view.setUint16(16, 20, false);

    let offset = FRAME_HEADER_BYTES;
    for (let i = 0; i < pcmData.length; i++) {
      view.setInt16(offset, pcmData[i], true);
      offset += 2;
    }
    return new Uint8Array(buffer);
  }, []);

  const start = useCallback(
    async (
      audioContext: AudioContext,
      mediaStream: MediaStream,
      options?: VoiceSessionStartOptions,
    ) => {
      const enableServerStt = options?.enableServerStt !== false;
      const pipeline = options?.pipeline ?? "classic";
      audioContextRef.current = audioContext;
      dropIncomingAudioRef.current = false;
      inputFramesSentRef.current = 0;
      outputFramesRecvRef.current = 0;
      setIsUserSpeaking(false);
      setActivePipeline(null);

      console.debug("[voice][session] start request", {
        conversationId,
        agentId,
        pipeline,
      });
      const result = await sendRpc<VoiceStartResult>("voice.start", {
        conversationId: conversationId,
        agentId: agentId,
        pipeline,
        audioIn: {
          codec: "pcm_s16le",
          sampleRateHz: 16000,
          channels: 1,
          frameMs: 20,
        },
        audioOut: { codec: "pcm_s16le", sampleRateHz: 24000, channels: 1 },
        features: {
          serverVad: pipeline === "realtime" || enableServerStt,
          serverTurn: pipeline === "realtime" || enableServerStt,
          serverDenoise: enableServerStt,
          bargeIn: true,
        },
        client: { platform: "web", appVersion: "1.0.0" },
      });
      sessionIdRef.current = result.sessionId;
      setActivePipeline(result.pipeline ?? "classic");
      console.debug("[voice][session] start ready", {
        sessionId: result.sessionId,
        conversationId: result.conversationId,
        pipeline: result.pipeline,
      });

      unsubscribeBinaryRef.current = onBinaryMessage(handleBinary);
      if (!enableServerStt) {
        return;
      }

      const source = new MediaStreamAudioSourceNode(audioContext, {
        mediaStream,
      });
      const processor = audioContext.createScriptProcessor(
        PROCESSOR_BUFFER_SIZE,
        1,
        1,
      );
      source.connect(processor);
      processor.connect(audioContext.destination);

      processor.onaudioprocess = (event) => {
        const samples = event.inputBuffer.getChannelData(0);
        const resampled = resampleTo16k(samples, event.inputBuffer.sampleRate);
        const prior = pendingInputSamplesRef.current;
        const combined = new Float32Array(prior.length + resampled.length);
        combined.set(prior);
        combined.set(resampled, prior.length);

        let offset = 0;
        let sawVoice = false;
        while (offset + INPUT_FRAME_SAMPLES <= combined.length) {
          const chunk = combined.subarray(offset, offset + INPUT_FRAME_SAMPLES);
          const pcm = new Int16Array(INPUT_FRAME_SAMPLES);
          let sumSquares = 0;
          for (let i = 0; i < INPUT_FRAME_SAMPLES; i++) {
            const sample = Math.max(-1, Math.min(1, chunk[i]));
            sumSquares += sample * sample;
            pcm[i] = sample < 0 ? sample * 0x8000 : sample * 0x7fff;
          }
          if (!sawVoice) {
            const rms = Math.sqrt(sumSquares / INPUT_FRAME_SAMPLES);
            // This gate is a bandwidth guard only (prevents sending true silence).
            // Voice activity detection is performed server-side.
            // Do not raise these thresholds — it will clip soft speech onset.
            if (rms >= CLIENT_SPEECH_RMS_THRESHOLD) {
              sawVoice = true;
            }
          }
          sendBinary(encodeInputFrame(pcm));
          inputFramesSentRef.current += 1;
          if (inputFramesSentRef.current % 50 === 0) {
            console.debug("[voice][session] input frames", {
              count: inputFramesSentRef.current,
              pendingSamples: pendingInputSamplesRef.current.length,
              sawVoice,
            });
          }
          offset += INPUT_FRAME_SAMPLES;
        }

        pendingInputSamplesRef.current = combined.subarray(offset);
        const now = Date.now();
        if (sawVoice) {
          lastVoiceDetectedAtRef.current = now;
          setIsUserSpeaking(true);
        } else if (
          now - lastVoiceDetectedAtRef.current >
          CLIENT_SPEECH_HANGOVER_MS
        ) {
          setIsUserSpeaking(false);
        }
      };

      mediaSourceRef.current = source;
      processorRef.current = processor;
    },
    [
      agentId,
      conversationId,
      encodeInputFrame,
      handleBinary,
      onBinaryMessage,
      resampleTo16k,
      sendBinary,
      sendRpc,
    ],
  );

  const stop = useCallback(() => {
    console.debug("[voice][session] stop", {
      sessionId: sessionIdRef.current,
    });
    if (sessionIdRef.current) {
      sendRpc("voice.end", { sessionId: sessionIdRef.current }).catch(() => {});
      sessionIdRef.current = null;
    }
    unsubscribeBinaryRef.current?.();
    unsubscribeBinaryRef.current = null;

    if (processorRef.current) {
      processorRef.current.disconnect();
      processorRef.current.onaudioprocess = null;
      processorRef.current = null;
    }
    if (mediaSourceRef.current) {
      mediaSourceRef.current.disconnect();
      mediaSourceRef.current = null;
    }

    handleFlush();
    dropIncomingAudioRef.current = false;
    pendingInputSamplesRef.current = new Float32Array(0);
    setIsUserSpeaking(false);
    setActivePipeline(null);
  }, [handleFlush, sendRpc]);

  const interruptPlayback = useCallback(() => {
    dropIncomingAudioRef.current = true;
    handleFlush();
  }, [handleFlush]);

  const resumePlayback = useCallback(() => {
    dropIncomingAudioRef.current = false;
  }, []);

  const cancelResponse = useCallback(
    async (reason?: string) => {
      if (!sessionIdRef.current) {
        return;
      }
      await sendRpc("voice.response.cancel", {
        responseId: "",
        reason: reason || "client_interrupt",
      });
    },
    [sendRpc],
  );

  useEffect(() => {
    return () => stop();
  }, [stop]);

  return {
    start,
    stop,
    interruptPlayback,
    resumePlayback,
    cancelResponse,
    isUserSpeaking,
    isPlaying,
    isSynthesizing,
    activePipeline,
  };
}
