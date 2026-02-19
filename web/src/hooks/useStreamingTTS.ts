import { useState, useRef, useCallback, useEffect } from 'react';

interface QueueItem {
  text: string;
  audioBuffer?: AudioBuffer;
  status: 'pending' | 'fetching' | 'ready' | 'playing' | 'done';
}

const MAX_CONCURRENT_FETCHES = 4;

export interface UseStreamingTTSOptions {
  voice: string;
  /** AudioContext to play through (created during user gesture in startCall). */
  audioContext: AudioContext | null;
  /** Called once when the entire queue drains naturally (all items played to
   *  completion, not interrupted by stopAndClear). Only fires if at least one
   *  audio segment was actually played during the turn. */
  onTurnComplete?: () => void;
}

export interface UseStreamingTTSReturn {
  enqueueSentence: (text: string) => void;
  stopAndClear: () => void;
  isPlaying: boolean;
  isSynthesizing: boolean;
}

export function useStreamingTTS(options: UseStreamingTTSOptions): UseStreamingTTSReturn {
  const { voice, audioContext, onTurnComplete } = options;
  const [isPlaying, setIsPlaying] = useState(false);
  const [isSynthesizing, setIsSynthesizing] = useState(false);

  const queueRef = useRef<QueueItem[]>([]);
  const playIndexRef = useRef(0);
  const abortControllersRef = useRef<AbortController[]>([]);
  const voiceRef = useRef(voice);
  const audioContextRef = useRef(audioContext);
  const activeRef = useRef(false);
  const playedAnyRef = useRef(false);
  const onTurnCompleteRef = useRef(onTurnComplete);
  const currentSourceRef = useRef<AudioBufferSourceNode | null>(null);
  voiceRef.current = voice;
  audioContextRef.current = audioContext;
  onTurnCompleteRef.current = onTurnComplete;

  const updateSynthesizingState = useCallback(() => {
    const hasPendingOrFetching = queueRef.current.some(
      (item) => item.status === 'pending' || item.status === 'fetching'
    );
    setIsSynthesizing(hasPendingOrFetching);
  }, []);

  const advanceQueue = useCallback(() => {
    const queue = queueRef.current;
    const index = playIndexRef.current;
    if (index < queue.length) {
      queue[index].status = 'done';
    }
    currentSourceRef.current = null;
    playIndexRef.current++;
  }, []);

  const playNext = useCallback(() => {
    if (!activeRef.current) return;
    const context = audioContextRef.current;
    if (!context || context.state === 'closed') return;

    const queue = queueRef.current;

    // Skip any items that failed during fetch.
    while (playIndexRef.current < queue.length) {
      if (queue[playIndexRef.current].status === 'done') {
        playIndexRef.current++;
        continue;
      }
      break;
    }

    const currentIndex = playIndexRef.current;
    if (currentIndex >= queue.length) {
      setIsPlaying(false);
      if (playedAnyRef.current && onTurnCompleteRef.current) {
        onTurnCompleteRef.current();
      }
      return;
    }

    const item = queue[currentIndex];
    if (item.status === 'ready' && item.audioBuffer) {
      item.status = 'playing';
      playedAnyRef.current = true;
      setIsPlaying(true);

      // Play via AudioBufferSourceNode — bypasses all HTMLAudioElement
      // quirks on iOS Safari (silent first play, src-change issues).
      const source = context.createBufferSource();
      source.buffer = item.audioBuffer;
      source.connect(context.destination);
      currentSourceRef.current = source;

      source.onended = () => {
        if (!activeRef.current) return;
        advanceQueue();
        playNext();
      };

      source.start();
    }
    // If not ready yet, playNext will be called again when the fetch completes
  }, [advanceQueue]);

  const fetchTTS = useCallback(async (item: QueueItem, index: number) => {
    if (!activeRef.current) return;
    const context = audioContextRef.current;
    if (!context || context.state === 'closed') return;

    item.status = 'fetching';
    updateSynthesizingState();

    const controller = new AbortController();
    abortControllersRef.current.push(controller);

    try {
      const synthesizeResponse = await fetch('/api/v1/audio/synthesize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: item.text, voice: voiceRef.current }),
        signal: controller.signal,
      });
      if (!synthesizeResponse.ok) throw new Error(`TTS synthesize failed: ${synthesizeResponse.status}`);
      const { token } = await synthesizeResponse.json();

      // Download and decode audio into an AudioBuffer for instant playback.
      const audioResponse = await fetch(`/api/v1/audio/stream?token=${token}`, {
        signal: controller.signal,
      });
      if (!audioResponse.ok) throw new Error(`TTS stream failed: ${audioResponse.status}`);
      const arrayBuffer = await audioResponse.arrayBuffer();
      item.audioBuffer = await context.decodeAudioData(arrayBuffer);
      item.status = 'ready';
    } catch {
      item.status = 'done';
    }

    updateSynthesizingState();

    // If this item is at playIndex and ready, start playing.
    if (activeRef.current && item.status === 'ready') {
      const currentItem = queueRef.current[playIndexRef.current];
      if (currentItem === item || (currentItem && currentItem.status === 'done')) {
        playNext();
      }
    }

    startFetches();
  }, [playNext, updateSynthesizingState]);

  const startFetches = useCallback(() => {
    const queue = queueRef.current;
    const activeFetches = queue.filter((item) => item.status === 'fetching').length;
    let slotsAvailable = MAX_CONCURRENT_FETCHES - activeFetches;

    for (let index = 0; index < queue.length && slotsAvailable > 0; index++) {
      if (queue[index].status === 'pending') {
        slotsAvailable--;
        fetchTTS(queue[index], index);
      }
    }
  }, [fetchTTS]);

  const enqueueSentence = useCallback((text: string) => {
    if (!text.trim()) return;
    if (!activeRef.current) {
      playedAnyRef.current = false;
    }
    activeRef.current = true;
    const item: QueueItem = { text, status: 'pending' };
    queueRef.current.push(item);
    setIsSynthesizing(true);
    startFetches();
  }, [startFetches]);

  const stopAndClear = useCallback(() => {
    activeRef.current = false;
    playedAnyRef.current = false;
    // Stop the currently playing source node.
    if (currentSourceRef.current) {
      try { currentSourceRef.current.stop(); } catch { /* may already be stopped */ }
      currentSourceRef.current = null;
    }
    // Abort all in-flight fetches.
    for (const controller of abortControllersRef.current) {
      controller.abort();
    }
    abortControllersRef.current = [];
    queueRef.current = [];
    playIndexRef.current = 0;
    setIsPlaying(false);
    setIsSynthesizing(false);
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      activeRef.current = false;
      if (currentSourceRef.current) {
        try { currentSourceRef.current.stop(); } catch { /* ignore */ }
      }
      for (const controller of abortControllersRef.current) {
        controller.abort();
      }
    };
  }, []);

  return { enqueueSentence, stopAndClear, isPlaying, isSynthesizing };
}
