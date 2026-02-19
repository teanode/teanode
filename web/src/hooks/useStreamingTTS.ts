import { useState, useRef, useCallback, useEffect } from 'react';

interface QueueItem {
  text: string;
  audioToken?: string;
  status: 'pending' | 'fetching' | 'ready' | 'playing' | 'done';
}

const MAX_CONCURRENT_FETCHES = 2;

export interface UseStreamingTTSReturn {
  enqueueSentence: (text: string) => void;
  stopAndClear: () => void;
  isPlaying: boolean;
  isSynthesizing: boolean;
}

export function useStreamingTTS(voice: string, audioElement: HTMLAudioElement | null): UseStreamingTTSReturn {
  const [isPlaying, setIsPlaying] = useState(false);
  const [isSynthesizing, setIsSynthesizing] = useState(false);

  const queueRef = useRef<QueueItem[]>([]);
  const playIndexRef = useRef(0);
  const abortControllersRef = useRef<AbortController[]>([]);
  const voiceRef = useRef(voice);
  const audioElementRef = useRef(audioElement);
  const activeRef = useRef(false);
  voiceRef.current = voice;
  audioElementRef.current = audioElement;

  const updateSynthesizingState = useCallback(() => {
    const hasPendingOrFetching = queueRef.current.some(
      (item) => item.status === 'pending' || item.status === 'fetching'
    );
    setIsSynthesizing(hasPendingOrFetching);
  }, []);

  const playNext = useCallback(() => {
    if (!activeRef.current) return;
    const audio = audioElementRef.current;
    if (!audio) return;

    const queue = queueRef.current;
    const index = playIndexRef.current;

    if (index >= queue.length) {
      setIsPlaying(false);
      return;
    }

    const item = queue[index];
    if (item.status === 'ready' && item.audioToken) {
      item.status = 'playing';
      setIsPlaying(true);
      audio.src = `/api/v1/audio/stream?token=${item.audioToken}`;
      audio.play().catch(() => {
        // Playback failed — advance to next
        item.status = 'done';
        playIndexRef.current++;
        playNext();
      });
    }
    // If not ready yet, playNext will be called again when the fetch completes
  }, []);

  const fetchTTS = useCallback(async (item: QueueItem, index: number) => {
    if (!activeRef.current) return;
    item.status = 'fetching';
    updateSynthesizingState();

    const controller = new AbortController();
    abortControllersRef.current.push(controller);

    try {
      const response = await fetch('/api/v1/audio/synthesize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: item.text, voice: voiceRef.current }),
        signal: controller.signal,
      });
      if (!response.ok) throw new Error(`TTS request failed: ${response.status}`);
      const { token } = await response.json();
      item.audioToken = token;
      item.status = 'ready';
    } catch {
      item.status = 'done'; // Skip on error
    }

    updateSynthesizingState();

    // If this is the item at playIndex and it's ready, start playing
    if (activeRef.current && playIndexRef.current === index && item.status === 'ready') {
      playNext();
    }

    // Kick off next pending fetch
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
    activeRef.current = true;
    const item: QueueItem = { text, status: 'pending' };
    queueRef.current.push(item);
    setIsSynthesizing(true);
    startFetches();
  }, [startFetches]);

  const stopAndClear = useCallback(() => {
    activeRef.current = false;
    const audio = audioElementRef.current;
    if (audio) {
      audio.pause();
      // Reset to a silent WAV instead of removing src — removeAttribute('src')
      // re-locks the audio element on iOS Safari, breaking future playback.
      audio.src = 'data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEARKwAAIhYAQACABAAZGF0YQAAAAA=';
    }
    // Abort all in-flight fetches
    for (const controller of abortControllersRef.current) {
      controller.abort();
    }
    abortControllersRef.current = [];
    queueRef.current = [];
    playIndexRef.current = 0;
    setIsPlaying(false);
    setIsSynthesizing(false);
  }, []);

  // Handle audio ended event — advance to next item
  useEffect(() => {
    const audio = audioElement;
    if (!audio) return;

    const handleEnded = () => {
      const queue = queueRef.current;
      const index = playIndexRef.current;
      if (index < queue.length) {
        queue[index].status = 'done';
      }
      playIndexRef.current++;
      playNext();
    };

    const handleError = () => {
      const queue = queueRef.current;
      const index = playIndexRef.current;
      if (index < queue.length) {
        queue[index].status = 'done';
      }
      playIndexRef.current++;
      playNext();
    };

    audio.addEventListener('ended', handleEnded);
    audio.addEventListener('error', handleError);
    return () => {
      audio.removeEventListener('ended', handleEnded);
      audio.removeEventListener('error', handleError);
    };
  }, [audioElement, playNext]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      activeRef.current = false;
      for (const controller of abortControllersRef.current) {
        controller.abort();
      }
    };
  }, []);

  return { enqueueSentence, stopAndClear, isPlaying, isSynthesizing };
}
