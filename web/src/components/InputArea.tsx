import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Container from '@mui/material/Container';
import IconButton from '@mui/material/IconButton';
import AttachFileRounded from '@mui/icons-material/AttachFileRounded';
import SendRounded from '@mui/icons-material/SendRounded';
import StopRounded from '@mui/icons-material/StopRounded';
import type { Attachment } from '../types';

interface PendingFile {
  file: File;
  previewUrl?: string;
}

async function uploadMedia(file: File): Promise<Attachment> {
  const formData = new FormData();
  formData.append('file', file);
  const response = await fetch('/api/v1/media/upload', {
    method: 'POST',
    body: formData,
  });
  if (!response.ok) {
    throw new Error(`Upload failed: ${response.status}`);
  }
  return response.json();
}

function isImageFile(file: File): boolean {
  return file.type.startsWith('image/');
}

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
  onSend: (text: string, attachments?: Attachment[]) => void;
  onAbort?: () => void;
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
  onSend,
  onAbort,
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

  // Restore draft when draftKey changes (conversation switch).
  useEffect(() => {
    const element = textareaRef.current;
    if (!element) return;
    const saved = draftKey ? localStorage.getItem(`draft:${draftKey}`) : null;
    element.value = saved || '';
    element.style.height = 'auto';
    if (saved) {
      element.style.height = Math.min(element.scrollHeight, 150) + 'px';
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
          pendingFiles.map((pf) => uploadMedia(pf.file))
        );
        onSend(text, attachments);
      } catch (err) {
        console.error('File upload failed:', err);
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

    element.value = '';
    element.style.height = 'auto';
    setHasText(false);
    if (draftKeyRef.current) {
      localStorage.removeItem(`draft:${draftKeyRef.current}`);
    }
  }, [onSend, pendingFiles]);

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        handleSend();
      }
    },
    [handleSend]
  );

  const handleInput = useCallback(() => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = Math.min(element.scrollHeight, 150) + 'px';
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

  const handleDrop = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    setDragOver(false);
    if (event.dataTransfer.files.length > 0) {
      addFiles(event.dataTransfer.files);
    }
  }, [addFiles]);

  const handleFileChange = useCallback((event: React.ChangeEvent<HTMLInputElement>) => {
    if (event.target.files && event.target.files.length > 0) {
      addFiles(event.target.files);
      event.target.value = '';
    }
  }, [addFiles]);

  const hasContent = hasText || pendingFiles.length > 0;
  const showStop = isRunning && !hasContent && !!onAbort;

  // Extract the short model name (after the colon) for display.
  const displayModel = model ? (model.includes(':') ? model.split(':').slice(1).join(':') : model) : null;

  const resolvedPlaceholder = placeholder || t('conversations.reply', { agentName });

  const inputBox = (
    <Box
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        bgcolor: 'surface2',
        borderRadius: 1.5,
        border: 1,
        borderColor: dragOver ? 'primary.main' : 'divider',
        px: 1.5,
        py: 1,
        gap: 0.5,
        '&:focus-within': {
          borderColor: 'primary.main',
        },
      }}
    >
      {pendingFiles.length > 0 && (
        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', pb: 0.5 }}>
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
                    sx={{ width: 24, height: 24, borderRadius: '50%', objectFit: 'cover' }}
                  />
                ) : undefined
              }
              sx={{ maxWidth: 200 }}
            />
          ))}
        </Box>
      )}
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
          width: '100%',
          border: 'none',
          outline: 'none',
          bgcolor: 'transparent',
          color: 'text.primary',
          fontSize: '0.875rem',
          fontFamily: 'inherit',
          lineHeight: 1.5,
          resize: 'none',
          overflow: 'auto',
          py: 0.5,
          '&::placeholder': {
            color: 'text.secondary',
            opacity: 1,
          },
        }}
      />
      <input
        type="file"
        ref={fileInputRef}
        multiple
        onChange={handleFileChange}
        style={{ display: 'none' }}
      />
      {(focused || showStop || pendingFiles.length > 0 || uploading) && (
        <Box
          onMouseDown={(event: React.MouseEvent) => event.preventDefault()}
          sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 0.5 }}
        >
          {modelPicker}
          {!modelPicker && displayModel && focused && (
            <Box
              component="span"
              sx={{
                fontSize: '0.75rem',
                color: 'text.secondary',
              }}
            >
              {displayModel}
            </Box>
          )}
          <IconButton
            size="small"
            onClick={() => fileInputRef.current?.click()}
            sx={{ flexShrink: 0, width: 32, height: 32, color: 'text.secondary', '&:hover': { color: 'primary.main' } }}
          >
            <AttachFileRounded fontSize="small" />
          </IconButton>
          <IconButton
            size="small"
            color={showStop ? 'error' : 'primary'}
            onClick={showStop ? onAbort : handleSend}
            disabled={uploading || (!showStop && !hasContent)}
            sx={{
              flexShrink: 0,
              width: 32,
              height: 32,
            }}
          >
            {uploading ? <CircularProgress size={16} color="primary" /> : showStop ? <StopRounded fontSize="small" /> : <SendRounded fontSize="small" />}
          </IconButton>
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
