import { useRef, useCallback } from 'react';

export type ChimeType = 'inputCaptured' | 'agentDone';

export interface ChimeConfig {
  enabled: boolean;
  volume: number;
  inputUrl?: string;
  agentUrl?: string;
}

export interface UseChimePlayerReturn {
  /** Attach to an existing AudioContext (call during startCall). */
  init: (audioContext: AudioContext) => void;
  /** Play a chime. No-op if disabled or not initialized. */
  play: (type: ChimeType) => void;
  /** Tear down. Safe to call multiple times. */
  close: () => void;
}

/** Default tone parameters for each chime type. */
const TONE_PARAMS: Record<ChimeType, { frequency: number; durationSeconds: number; rampMs: number }> = {
  inputCaptured: { frequency: 880, durationSeconds: 0.08, rampMs: 10 },
  agentDone: { frequency: 660, durationSeconds: 0.12, rampMs: 15 },
};

/**
 * Hook that plays short indicator chimes via Web Audio API.
 *
 * - Default chimes are synthesized OscillatorNode sine tones (no network).
 * - If a custom URL is provided for a chime type, it is fetched once on
 *   first play and decoded into an AudioBuffer for low-latency replay.
 * - Reuses the voice-call AudioContext (passed via init()) so we don't
 *   create a second context or interfere with iOS audio session.
 */
export function useChimePlayer(config: ChimeConfig): UseChimePlayerReturn {
  const audioContextRef = useRef<AudioContext | null>(null);
  const configRef = useRef(config);
  configRef.current = config;

  // Cache decoded AudioBuffers keyed by URL to avoid re-fetching.
  const bufferCacheRef = useRef<Map<string, AudioBuffer>>(new Map());

  const init = useCallback((audioContext: AudioContext) => {
    audioContextRef.current = audioContext;
  }, []);

  const close = useCallback(() => {
    audioContextRef.current = null;
    bufferCacheRef.current.clear();
  }, []);

  const play = useCallback((type: ChimeType) => {
    const { enabled, volume, inputUrl, agentUrl } = configRef.current;
    if (!enabled || volume <= 0) return;

    const context = audioContextRef.current;
    if (!context || context.state === 'closed') return;

    const customUrl = type === 'inputCaptured' ? inputUrl : agentUrl;

    if (customUrl) {
      playCustom(context, customUrl, volume, bufferCacheRef.current);
    } else {
      playTone(context, type, volume);
    }
  }, []);

  return { init, play, close };
}

/** Synthesize a short sine tone directly on the AudioContext. */
function playTone(context: AudioContext, type: ChimeType, volume: number): void {
  const { frequency, durationSeconds, rampMs } = TONE_PARAMS[type];
  const now = context.currentTime;

  const oscillator = context.createOscillator();
  oscillator.type = 'sine';
  oscillator.frequency.value = frequency;

  const gainNode = context.createGain();
  // Quick ramp up then ramp down to avoid click artifacts.
  gainNode.gain.setValueAtTime(0, now);
  gainNode.gain.linearRampToValueAtTime(volume, now + rampMs / 1000);
  gainNode.gain.linearRampToValueAtTime(0, now + durationSeconds);

  oscillator.connect(gainNode);
  gainNode.connect(context.destination);

  oscillator.start(now);
  oscillator.stop(now + durationSeconds + 0.01);

  // Self-cleanup after stop.
  oscillator.onended = () => {
    oscillator.disconnect();
    gainNode.disconnect();
  };
}

/** Fetch (once) and play a custom audio file via AudioBufferSourceNode. */
async function playCustom(
  context: AudioContext,
  url: string,
  volume: number,
  cache: Map<string, AudioBuffer>,
): Promise<void> {
  try {
    let buffer = cache.get(url);
    if (!buffer) {
      const response = await fetch(url);
      if (!response.ok) return;
      const arrayBuffer = await response.arrayBuffer();
      buffer = await context.decodeAudioData(arrayBuffer);
      cache.set(url, buffer);
    }

    const source = context.createBufferSource();
    source.buffer = buffer;

    const gainNode = context.createGain();
    gainNode.gain.value = volume;

    source.connect(gainNode);
    gainNode.connect(context.destination);
    source.start();

    source.onended = () => {
      source.disconnect();
      gainNode.disconnect();
    };
  } catch {
    // Silently ignore fetch/decode errors for custom chimes.
  }
}
