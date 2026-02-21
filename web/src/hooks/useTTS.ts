import { useState, useRef, useCallback, useEffect } from "react";

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
  const voiceRef = useRef(voice);
  voiceRef.current = voice;

  // Reuse a single audio element (important for iOS Safari audio unlock).
  const getAudioElement = useCallback(() => {
    if (!audioRef.current) {
      audioRef.current = new Audio();
    }
    return audioRef.current;
  }, []);

  const stop = useCallback(() => {
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.currentTime = 0;
      audio.removeAttribute("src");
    }
    setIsSpeaking(false);
    setIsLoading(false);
  }, []);

  const speak = useCallback(
    async (text: string) => {
      if (!text.trim()) return;
      stop();
      setIsLoading(true);

      try {
        // Step 1: Get a streaming token.
        const response = await fetch("/api/v1/audio/synthesize", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ text, voice: voiceRef.current }),
        });
        if (!response.ok) {
          throw new Error(`TTS request failed: ${response.status}`);
        }
        const { token } = await response.json();

        // Step 2: Point audio element at the streaming endpoint.
        const audio = getAudioElement();
        audio.src = `/api/v1/audio/stream?token=${token}`;

        audio.oncanplay = () => {
          setIsLoading(false);
          setIsSpeaking(true);
        };
        audio.onended = () => setIsSpeaking(false);
        audio.onerror = () => {
          setIsLoading(false);
          setIsSpeaking(false);
        };

        await audio.play();
      } catch (error) {
        console.error("TTS error:", error);
        setIsLoading(false);
        setIsSpeaking(false);
      }
    },
    [stop, getAudioElement],
  );

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current = null;
      }
    };
  }, []);

  return { speak, stop, isSpeaking, isLoading };
}
