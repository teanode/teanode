import { useRef, useCallback } from "react";

export type ChimeType = "inputCaptured" | "agentDone" | "agentWaiting";

export interface ChimeConfig {
  enabled: boolean;
  volume: number;
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
const TONE_PARAMS: Record<
  ChimeType,
  { frequency: number; durationSeconds: number; rampMs: number }
> = {
  inputCaptured: { frequency: 880, durationSeconds: 0.08, rampMs: 10 },
  agentDone: { frequency: 660, durationSeconds: 0.12, rampMs: 15 },
  agentWaiting: { frequency: 520, durationSeconds: 0.1, rampMs: 12 },
};

/**
 * Hook that plays short indicator chimes via Web Audio API.
 *
 * - Chimes are synthesized OscillatorNode sine tones (no network).
 * - Reuses the voice-call AudioContext (passed via init()) so we don't
 *   create a second context or interfere with iOS audio session.
 */
export function useChimePlayer(config: ChimeConfig): UseChimePlayerReturn {
  const audioContextRef = useRef<AudioContext | null>(null);
  const configRef = useRef(config);
  configRef.current = config;

  const init = useCallback((audioContext: AudioContext) => {
    audioContextRef.current = audioContext;
  }, []);

  const close = useCallback(() => {
    audioContextRef.current = null;
  }, []);

  const play = useCallback((type: ChimeType) => {
    const { enabled, volume } = configRef.current;
    if (!enabled || volume <= 0) return;

    const context = audioContextRef.current;
    if (!context || context.state === "closed") return;
    playTone(context, type, volume);
  }, []);

  return { init, play, close };
}

/** Synthesize a short sine tone directly on the AudioContext. */
function playTone(
  context: AudioContext,
  type: ChimeType,
  volume: number,
): void {
  const { frequency, durationSeconds, rampMs } = TONE_PARAMS[type];
  const now = context.currentTime;

  const oscillator = context.createOscillator();
  oscillator.type = "sine";
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
