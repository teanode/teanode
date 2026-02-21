import React, { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import Container from "@mui/material/Container";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import AttachFileRounded from "@mui/icons-material/AttachFileRounded";
import MicRounded from "@mui/icons-material/MicRounded";
import PhoneRounded from "@mui/icons-material/PhoneRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import StopRounded from "@mui/icons-material/StopRounded";
import type { Attachment } from "../types";
import { useAudioRecorder } from "../hooks/useAudioRecorder";

interface PendingFile {
  file: File;
  previewUrl?: string;
}

async function uploadMedia(file: File): Promise<Attachment> {
  const formData = new FormData();
  formData.append("file", file);
  const response = await fetch("/api/v1/media/upload", {
    method: "POST",
    body: formData,
  });
  if (!response.ok) {
    throw new Error(`Upload failed: ${response.status}`);
  }
  return response.json();
}

function isImageFile(file: File): boolean {
  return file.type.startsWith("image/");
}

type VoiceState = "idle" | "recording" | "transcribing";

interface InputAreaProps {
  agentName: string;
  draftKey?: string;
  placeholder?: string;
  autoFocus?: boolean;
  isRunning?: boolean;
  model?: string | null;
  /** Slot for a model picker widget rendered in the toolbar. */
  modelPicker?: React.ReactNode;
  /** When true, renders the input box without a Container wrapper. */
  bare?: boolean;
  /** When true, the toolbar is always visible (not gated by focus). */
  alwaysExpanded?: boolean;
  /** Whether voice input is available (audio-capable provider configured). */
  voiceEnabled?: boolean;
  /** Whether to auto-send after transcription. */
  voiceAutoSend?: boolean;
  /** Whether a voice call is currently active (hides mic/call buttons, VAD handles audio). */
  voiceCallActive?: boolean;
  /** Whether a voice call is currently connecting. */
  voiceCallConnecting?: boolean;
  /** Called to start a voice call. */
  onStartVoiceCall?: () => void;
  onSend: (text: string, attachments?: Attachment[]) => void;
  onAbort?: () => void;
  /** Called when voice-transcribed text should be auto-sent. */
  onVoiceMessage?: (text: string) => void;
}

export default function InputArea({
  agentName,
  draftKey,
  placeholder,
  autoFocus,
  isRunning = false,
  model,
  modelPicker,
  bare,
  alwaysExpanded,
  voiceEnabled,
  voiceAutoSend,
  voiceCallActive,
  voiceCallConnecting,
  onStartVoiceCall,
  onSend,
  onAbort,
  onVoiceMessage,
}: InputAreaProps) {
  const { t } = useTranslation();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [hasText, setHasText] = useState(false);
  const [focused, setFocused] = useState(false);
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const uploadingRef = useRef(false);
  const [dragOver, setDragOver] = useState(false);
  const draftKeyRef = useRef(draftKey);
  draftKeyRef.current = draftKey;
  const [voiceState, setVoiceState] = useState<VoiceState>("idle");

  const handleRecordingComplete = useCallback(
    async (blob: Blob, format: string) => {
      setVoiceState("transcribing");
      try {
        const formData = new FormData();
        formData.append("file", blob, `audio.${format}`);
        const response = await fetch("/api/v1/audio/transcribe", {
          method: "POST",
          body: formData,
        });
        if (!response.ok)
          throw new Error(`Transcription failed: ${response.status}`);
        const result = await response.json();
        const text = result.text?.trim();
        if (text) {
          if (voiceAutoSend && onVoiceMessage) {
            onVoiceMessage(text);
          } else {
            const element = textareaRef.current;
            if (element) {
              element.value = text;
              element.style.height = "auto";
              element.style.height = Math.min(element.scrollHeight, 150) + "px";
              setHasText(true);
              element.focus();
            }
          }
        }
      } catch (error) {
        console.error("Transcription error:", error);
      }
      setVoiceState("idle");
    },
    [voiceAutoSend, onVoiceMessage],
  );

  const {
    isRecording,
    isSupported: micSupported,
    duration,
    startRecording,
    stopRecording,
    cancelRecording,
  } = useAudioRecorder({
    onRecordingComplete: handleRecordingComplete,
  });

  // Sync recording state.
  useEffect(() => {
    if (isRecording) setVoiceState("recording");
  }, [isRecording]);

  const showMic =
    voiceEnabled &&
    micSupported &&
    !hasText &&
    voiceState === "idle" &&
    !voiceCallActive;
  const showCallButton =
    voiceEnabled &&
    !voiceCallActive &&
    voiceState === "idle" &&
    onStartVoiceCall &&
    !voiceCallConnecting;
  const showCallConnecting = voiceCallConnecting;

  // Restore draft when draftKey changes (conversation switch).
  useEffect(() => {
    const element = textareaRef.current;
    if (!element) return;
    const saved = draftKey ? localStorage.getItem(`draft:${draftKey}`) : null;
    element.value = saved || "";
    element.style.height = "auto";
    if (saved) {
      element.style.height = Math.min(element.scrollHeight, 150) + "px";
    }
    setHasText(!!element.value.trim());
    setPendingFiles([]);
  }, [draftKey]);

  // Clean up preview URLs on unmount.
  useEffect(() => {
    return () => {
      pendingFiles.forEach((pf) => {
        if (pf.previewUrl) URL.revokeObjectURL(pf.previewUrl);
      });
    };
  }, [pendingFiles]);

  const addFiles = useCallback((files: FileList | File[]) => {
    const newFiles: PendingFile[] = Array.from(files).map((file) => ({
      file,
      previewUrl: isImageFile(file) ? URL.createObjectURL(file) : undefined,
    }));
    setPendingFiles((prev) => [...prev, ...newFiles]);
  }, []);

  const removeFile = useCallback((index: number) => {
    setPendingFiles((prev) => {
      const removed = prev[index];
      if (removed?.previewUrl) URL.revokeObjectURL(removed.previewUrl);
      return prev.filter((_, i) => i !== index);
    });
  }, []);

  const handleSend = useCallback(async () => {
    if (uploadingRef.current) return;
    const element = textareaRef.current;
    if (!element) return;
    const text = element.value.trim();
    if (!text && pendingFiles.length === 0) return;

    if (pendingFiles.length > 0) {
      uploadingRef.current = true;
      setUploading(true);
      try {
        const attachments = await Promise.all(
          pendingFiles.map((pf) => uploadMedia(pf.file)),
        );
        onSend(text, attachments);
      } catch (err) {
        console.error("File upload failed:", err);
        uploadingRef.current = false;
        setUploading(false);
        return;
      }
      uploadingRef.current = false;
      setUploading(false);
      pendingFiles.forEach((pf) => {
        if (pf.previewUrl) URL.revokeObjectURL(pf.previewUrl);
      });
      setPendingFiles([]);
    } else {
      onSend(text);
    }

    element.value = "";
    element.style.height = "auto";
    setHasText(false);
    if (draftKeyRef.current) {
      localStorage.removeItem(`draft:${draftKeyRef.current}`);
    }
  }, [onSend, pendingFiles]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleInput = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = "auto";
    element.style.height = Math.min(element.scrollHeight, 150) + "px";
    setHasText(!!element.value.trim());
    if (draftKeyRef.current) {
      if (element.value) {
        localStorage.setItem(`draft:${draftKeyRef.current}`, element.value);
      } else {
        localStorage.removeItem(`draft:${draftKeyRef.current}`);
      }
    }
  }, []);

  const handleDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    setDragOver(true);
  }, []);

  const handleDragLeave = useCallback(() => {
    setDragOver(false);
  }, []);

  const handleDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();
      setDragOver(false);
      if (event.dataTransfer.files.length > 0) {
        addFiles(event.dataTransfer.files);
      }
    },
    [addFiles],
  );

  const handleFileChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      if (event.target.files && event.target.files.length > 0) {
        addFiles(event.target.files);
        event.target.value = "";
      }
    },
    [addFiles],
  );

  const hasContent = hasText || pendingFiles.length > 0;
  const showStop = isRunning && !hasContent && !!onAbort;
  const expanded = alwaysExpanded || focused;

  // Extract the short model name (after the colon) for display.
  const displayModel = model
    ? model.includes(":")
      ? model.split(":").slice(1).join(":")
      : model
    : null;

  const resolvedPlaceholder =
    voiceState === "recording"
      ? t("settings.listening")
      : voiceState === "transcribing"
        ? t("settings.transcribing")
        : placeholder || t("conversations.reply", { agentName });

  const inputBox = (
    <Box
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      sx={{
        display: "flex",
        flexDirection: "column",
        bgcolor: "surface2",
        borderRadius: 1.5,
        border: 1,
        borderColor: dragOver ? "primary.main" : "divider",
        px: 1.5,
        py: 1,
        gap: 0.5,
        "&:focus-within": {
          borderColor: "primary.main",
        },
      }}
    >
      {pendingFiles.length > 0 && (
        <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", pb: 0.5 }}>
          {pendingFiles.map((pf, index) => (
            <Chip
              key={index}
              label={pf.file.name}
              size="small"
              onDelete={() => removeFile(index)}
              avatar={
                pf.previewUrl ? (
                  <Box
                    component="img"
                    src={pf.previewUrl}
                    sx={{
                      width: 24,
                      height: 24,
                      borderRadius: "50%",
                      objectFit: "cover",
                    }}
                  />
                ) : undefined
              }
              sx={{ maxWidth: 200 }}
            />
          ))}
        </Box>
      )}
      <Box sx={{ display: "flex", alignItems: "center" }}>
        <Box
          component="textarea"
          ref={textareaRef}
          placeholder={resolvedPlaceholder}
          autoFocus={autoFocus}
          rows={1}
          onKeyDown={handleKeyDown}
          onInput={handleInput}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          sx={{
            flex: 1,
            border: "none",
            outline: "none",
            bgcolor: "transparent",
            color: "text.primary",
            fontSize: "0.875rem",
            fontFamily: "inherit",
            lineHeight: 1.5,
            resize: "none",
            overflow: "auto",
            py: 0.5,
            "&::placeholder": {
              color: "text.secondary",
              opacity: 1,
            },
          }}
        />
        {!expanded && (showCallButton || showCallConnecting) && (
          <Box
            onMouseDown={(event: React.MouseEvent) => event.preventDefault()}
            sx={{ flexShrink: 0, ml: 0.5 }}
          >
            {showCallConnecting ? (
              <CircularProgress size={18} sx={{ mx: "7px" }} />
            ) : (
              <IconButton
                size="small"
                onClick={onStartVoiceCall}
                sx={{
                  width: 32,
                  height: 32,
                  color: "text.secondary",
                  "&:hover": { color: "success.main" },
                }}
              >
                <PhoneRounded fontSize="small" />
              </IconButton>
            )}
          </Box>
        )}
      </Box>
      <input
        type="file"
        ref={fileInputRef}
        multiple
        onChange={handleFileChange}
        style={{ display: "none" }}
      />
      {(expanded ||
        showStop ||
        pendingFiles.length > 0 ||
        uploading ||
        voiceState !== "idle") && (
        <Box
          onMouseDown={(event: React.MouseEvent) => event.preventDefault()}
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-end",
            gap: 0.5,
          }}
        >
          {voiceState === "recording" && (
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 0.5,
                mr: "auto",
              }}
            >
              <Box
                sx={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: "error.main",
                  animation: "pulse 1.5s infinite",
                  "@keyframes pulse": {
                    "0%, 100%": { opacity: 1 },
                    "50%": { opacity: 0.4 },
                  },
                }}
              />
              <Typography variant="caption" color="text.secondary">
                {Math.floor(duration / 60)}:
                {String(duration % 60).padStart(2, "0")}
              </Typography>
            </Box>
          )}
          {voiceState === "transcribing" && (
            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 0.5,
                mr: "auto",
              }}
            >
              <CircularProgress size={14} />
              <Typography variant="caption" color="text.secondary">
                {t("settings.transcribing")}
              </Typography>
            </Box>
          )}
          {expanded && modelPicker}
          {!modelPicker &&
            displayModel &&
            expanded &&
            voiceState === "idle" && (
              <Box
                component="span"
                sx={{
                  fontSize: "0.75rem",
                  color: "text.secondary",
                }}
              >
                {displayModel}
              </Box>
            )}
          {expanded && voiceState === "idle" && (
            <IconButton
              size="small"
              onClick={() => fileInputRef.current?.click()}
              sx={{
                flexShrink: 0,
                width: 32,
                height: 32,
                color: "text.secondary",
                "&:hover": { color: "primary.main" },
              }}
            >
              <AttachFileRounded fontSize="small" />
            </IconButton>
          )}
          {expanded && showMic && voiceState === "idle" && (
            <IconButton
              size="small"
              onClick={startRecording}
              sx={{
                flexShrink: 0,
                width: 32,
                height: 32,
                color: "text.secondary",
                "&:hover": { color: "primary.main" },
              }}
            >
              <MicRounded fontSize="small" />
            </IconButton>
          )}
          {expanded && showCallButton && (
            <IconButton
              size="small"
              onClick={onStartVoiceCall}
              sx={{
                flexShrink: 0,
                width: 32,
                height: 32,
                color: "text.secondary",
                "&:hover": { color: "success.main" },
              }}
            >
              <PhoneRounded fontSize="small" />
            </IconButton>
          )}
          {voiceState === "recording" ? (
            <IconButton
              size="small"
              color="error"
              onClick={stopRecording}
              sx={{ flexShrink: 0, width: 32, height: 32 }}
            >
              <StopRounded fontSize="small" />
            </IconButton>
          ) : (
            (expanded || showStop) && (
              <IconButton
                size="small"
                color={showStop ? "error" : "primary"}
                onClick={showStop ? onAbort : handleSend}
                disabled={
                  uploading ||
                  voiceState === "transcribing" ||
                  (!showStop && !hasContent)
                }
                sx={{
                  flexShrink: 0,
                  width: 32,
                  height: 32,
                }}
              >
                {uploading ? (
                  <CircularProgress size={16} color="primary" />
                ) : showStop ? (
                  <StopRounded fontSize="small" />
                ) : (
                  <SendRounded fontSize="small" />
                )}
              </IconButton>
            )
          )}
        </Box>
      )}
    </Box>
  );

  if (bare) return inputBox;

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
      {inputBox}
    </Container>
  );
}
