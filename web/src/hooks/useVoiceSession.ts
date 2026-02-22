import { useCallback, useEffect, useRef, useState } from "react";

const FRAME_MAGIC = 0xa1;
const FRAME_TYPE_AUDIO_IN = 0x01;
const FRAME_TYPE_AUDIO_OUT = 0x02;
const FRAME_TYPE_FLUSH = 0x03;
const FRAME_HEADER_BYTES = 18;
const INPUT_FRAME_SAMPLES = 320; // 20ms @ 16kHz
const PROCESSOR_BUFFER_SIZE = 1024; // Must be 0 or power-of-two in [256..16384]
const INPUT_SAMPLE_RATE_HZ = 16000;

type BinarySender = (data: ArrayBuffer | Uint8Array) => void;
type BinarySubscriber = (handler: (data: ArrayBuffer) => void) => () => void;

interface VoiceStartResult {
  session_id: string;
  conversation_id?: string;
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
  ) => Promise<void>;
  stop: () => void;
  isUserSpeaking: boolean;
  isPlaying: boolean;
  isSynthesizing: boolean;
}

export function useVoiceSession(
  options: UseVoiceSessionOptions,
): VoiceSessionRuntime {
  const { sendRpc, sendBinary, onBinaryMessage, conversationId, agentId } =
    options;

  const [isUserSpeaking, setIsUserSpeaking] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);
  const [isSynthesizing, setIsSynthesizing] = useState(false);

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
    const payloadLen = pcmData.length * 2;
    const buf = new ArrayBuffer(FRAME_HEADER_BYTES + payloadLen);
    const view = new DataView(buf);
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
    return new Uint8Array(buf);
  }, []);

  const start = useCallback(
    async (audioContext: AudioContext, mediaStream: MediaStream) => {
      audioContextRef.current = audioContext;
      inputFramesSentRef.current = 0;
      outputFramesRecvRef.current = 0;

      console.debug("[voice][session] start request", {
        conversationId,
        agentId,
      });
      const result = await sendRpc<VoiceStartResult>("voice.start", {
        conversation_id: conversationId,
        agent_id: agentId,
        audio_in: {
          codec: "pcm_s16le",
          sample_rate_hz: 16000,
          channels: 1,
          frame_ms: 20,
        },
        audio_out: { codec: "pcm_s16le", sample_rate_hz: 24000, channels: 1 },
        features: {
          server_vad: true,
          server_turn: true,
          server_denoise: false,
          barge_in: true,
        },
        client: { platform: "web", app_version: "1.0.0" },
      });
      sessionIdRef.current = result.session_id;
      console.debug("[voice][session] start ready", {
        session_id: result.session_id,
        conversation_id: result.conversation_id,
      });

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
          for (let i = 0; i < INPUT_FRAME_SAMPLES; i++) {
            const sample = Math.max(-1, Math.min(1, chunk[i]));
            if (!sawVoice && Math.abs(sample) > 0.015) {
              sawVoice = true;
            }
            pcm[i] = sample < 0 ? sample * 0x8000 : sample * 0x7fff;
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
        } else if (now - lastVoiceDetectedAtRef.current > 250) {
          setIsUserSpeaking(false);
        }
      };

      mediaSourceRef.current = source;
      processorRef.current = processor;

      unsubscribeBinaryRef.current = onBinaryMessage(handleBinary);
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
      session_id: sessionIdRef.current,
    });
    if (sessionIdRef.current) {
      sendRpc("voice.end", { session_id: sessionIdRef.current }).catch(
        () => {},
      );
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
    pendingInputSamplesRef.current = new Float32Array(0);
    setIsUserSpeaking(false);
  }, [handleFlush, sendRpc]);

  useEffect(() => {
    return () => stop();
  }, [stop]);

  return { start, stop, isUserSpeaking, isPlaying, isSynthesizing };
}
