import { useState, useRef, useCallback, useEffect } from 'react';

export interface UseTTSReturn {
  speak: (text: string) => Promise<void>;
  stop: () => void;
  isSpeaking: boolean;
  isLoading: boolean;
}

export function useTTS(voice: string): UseTTSReturn {
  const [isSpeaking, setIsSpeaking] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const blobUrlRef = useRef<string | null>(null);
  const voiceRef = useRef(voice);
  voiceRef.current = voice;

  // Reuse a single audio element (important for iOS Safari audio unlock).
  const getAudioElement = useCallback(() => {
    if (!audioRef.current) {
      audioRef.current = new Audio();
    }
    return audioRef.current;
  }, []);

  const revokeBlobUrl = useCallback(() => {
    if (blobUrlRef.current) {
      URL.revokeObjectURL(blobUrlRef.current);
      blobUrlRef.current = null;
    }
  }, []);

  const stop = useCallback(() => {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.currentTime = 0;
    }
    setIsSpeaking(false);
    setIsLoading(false);
    revokeBlobUrl();
  }, [revokeBlobUrl]);

  const speak = useCallback(async (text: string) => {
    if (!text.trim()) return;
    stop();
    setIsLoading(true);

    try {
      const response = await fetch('/api/v1/audio/synthesize', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text, voice: voiceRef.current }),
      });
      if (!response.ok) {
        throw new Error(`TTS request failed: ${response.status}`);
      }

      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      revokeBlobUrl();
      blobUrlRef.current = url;

      const audio = getAudioElement();
      audio.src = url;

      audio.onended = () => {
        setIsSpeaking(false);
        revokeBlobUrl();
      };
      audio.onerror = () => {
        setIsSpeaking(false);
        revokeBlobUrl();
      };

      setIsLoading(false);
      setIsSpeaking(true);
      await audio.play();
    } catch (error) {
      console.error('TTS error:', error);
      setIsLoading(false);
      setIsSpeaking(false);
    }
  }, [stop, revokeBlobUrl, getAudioElement]);

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current = null;
      }
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current);
        blobUrlRef.current = null;
      }
    };
  }, []);

  return { speak, stop, isSpeaking, isLoading };
}
