import React, { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import AttachFileRounded from "@mui/icons-material/AttachFileRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import MessageBubble from "../../components/MessageBubble";
import ToolInvoke from "../../components/ToolInvoke";
import ToolResult from "../../components/ToolResult";
import { dateLabelFor } from "../../dateUtils";
import DateSeparator from "../../components/DateSeparator";
import { sendRpc, onEvent, getBaseUrl } from "./rpc";
import type { RpcEventFrame } from "../shared/types";
import type { Attachment } from "../../types";
import { normalizeContent } from "../../contentUtils";

interface Message {
  role: "user" | "assistant" | "tool_call" | "tool_result";
  content: string;
  toolName?: string;
  attachments?: Attachment[];
  timestamp?: number;
}

interface PendingFile {
  file: File;
  previewUrl?: string;
}

interface Props {
  agentId: string;
  conversationId: string;
  onConversationCreated?: (id: string) => void;
  agentAvatarMediaId?: string;
  agentName?: string;
  userAvatarMediaId?: string;
  userName?: string;
}

function isImageFile(file: File): boolean {
  return file.type.startsWith("image/");
}

async function uploadMedia(file: File): Promise<Attachment> {
  const { url, token } = await getBaseUrl();
  const base = url.replace(/\/+$/, "");
  const formData = new FormData();
  formData.append("file", file);
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const response = await fetch(`${base}/api/v1/media/upload`, {
    method: "POST",
    body: formData,
    headers,
  });
  if (!response.ok) throw new Error(`Upload failed: ${response.status}`);
  return response.json();
}

export function ChatView({
  agentId,
  conversationId,
  onConversationCreated,
  agentAvatarMediaId,
  agentName,
  userAvatarMediaId,
  userName,
}: Props) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const [isRunning, setIsRunning] = useState(false);
  const [toolActivity, setToolActivity] = useState<string | null>(null);
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([]);
  const [uploading, setUploading] = useState(false);

  // Resolve backend base URL + token so avatar media URLs work in the
  // extension context (chrome-extension:// origin can't use relative paths).
  const [baseInfo, setBaseInfo] = useState<{
    url: string;
    token: string;
  } | null>(null);
  useEffect(() => {
    getBaseUrl().then(setBaseInfo);
  }, []);

  function resolveMediaUrl(mediaId: string | undefined): string | undefined {
    if (!mediaId || !baseInfo) return undefined;
    const base = baseInfo.url.replace(/\/+$/, "");
    let url = `${base}/api/v1/media/${mediaId}`;
    if (baseInfo.token) url += `?token=${encodeURIComponent(baseInfo.token)}`;
    return url;
  }
  const bottomRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Track the active conversation id via ref so event handlers always see the
  // latest value, even before the parent re-renders with a new prop.
  const activeConvIdRef = useRef(conversationId);
  useEffect(() => {
    activeConvIdRef.current = conversationId;
  }, [conversationId]);

  // When a new conversation is created via handleSend, skip the next history
  // load triggered by the conversationId prop change (optimistic messages are
  // already in state).
  const skipNextHistoryLoadRef = useRef(false);

  // Load history on conversation change.
  useEffect(() => {
    if (!conversationId) {
      setMessages([]);
      setIsRunning(false);
      setToolActivity(null);
      return;
    }
    // Skip history load when we just created this conversation via handleSend
    // (the optimistic user message is already in state).
    if (skipNextHistoryLoadRef.current) {
      skipNextHistoryLoadRef.current = false;
      return;
    }
    setMessages([]);
    setStreamingText("");
    setIsRunning(false);
    setToolActivity(null);
    sendRpc("conversations.history", { conversationId })
      .then((payload) => {
        const data = payload as {
          messages?: Array<{
            role: string;
            content: unknown;
            name?: string;
            attachments?: Attachment[];
            timestamp?: number;
          }>;
          activeRunId?: string;
          activeRunState?: { phase: string; toolName?: string };
        };
        if (data.messages) {
          setMessages(
            data.messages
              .filter((m) => m.role === "user" || m.role === "assistant")
              .map((m) => {
                const extracted = normalizeContent(m.content);
                return {
                  role: m.role as "user" | "assistant",
                  content: extracted.text,
                  attachments: extracted.attachments ?? m.attachments,
                  timestamp: m.timestamp,
                };
              }),
          );
        }
        // Restore busy state from server if a run is active.
        if (data.activeRunId) {
          setIsRunning(true);
          if (data.activeRunState?.phase === "tool") {
            setToolActivity(data.activeRunState.toolName || null);
          }
        }
      })
      .catch(() => {});
  }, [conversationId]);

  // Subscribe to conversation events.
  // Uses activeConvIdRef so events for a just-created conversation are caught
  // immediately, even before the parent re-renders with the new prop value.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "conversation") return;
      const p = frame.payload as Record<string, unknown>;
      if (
        !activeConvIdRef.current ||
        p.conversationId !== activeConvIdRef.current
      )
        return;

      switch (p.state) {
        case "queued":
          setIsRunning(true);
          setToolActivity(null);
          break;
        case "delta":
          setIsRunning(true);
          setToolActivity(null);
          setStreaming(true);
          setStreamingText((prev) => prev + (p.text as string));
          break;
        case "tool_call":
          setIsRunning(true);
          setStreaming(false);
          setToolActivity((p.toolName as string) || null);
          setMessages((prev) => [
            ...prev,
            {
              role: "tool_call",
              content: (p.arguments as string) || "",
              toolName: p.toolName as string,
            },
          ]);
          break;
        case "tool_result":
          setToolActivity(null);
          setMessages((prev) => [
            ...prev,
            {
              role: "tool_result",
              content: ((p.result as string) || "").slice(0, 500),
              toolName: p.toolName as string,
            },
          ]);
          break;
        case "final":
          setIsRunning(false);
          setStreaming(false);
          setToolActivity(null);
          setMessages((prev) => [
            ...prev,
            {
              role: "assistant",
              content: (p.text as string) || "",
              timestamp: Date.now(),
            },
          ]);
          setStreamingText("");
          break;
        case "error":
          setIsRunning(false);
          setStreaming(false);
          setToolActivity(null);
          setStreamingText("");
          break;
        case "aborted":
          setIsRunning(false);
          setStreaming(false);
          setToolActivity(null);
          setStreamingText("");
          break;
      }
    });
  }, []);

  // Auto-scroll.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingText, isRunning]);

  // Clean up file preview URLs.
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
    const text = input.trim();
    if ((!text && pendingFiles.length === 0) || streaming) return;

    setInput("");
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }

    let attachments: Attachment[] | undefined;
    if (pendingFiles.length > 0) {
      setUploading(true);
      try {
        attachments = await Promise.all(
          pendingFiles.map((pf) => uploadMedia(pf.file)),
        );
      } catch (err) {
        console.error("File upload failed:", err);
        setUploading(false);
        return;
      }
      pendingFiles.forEach((pf) => {
        if (pf.previewUrl) URL.revokeObjectURL(pf.previewUrl);
      });
      setPendingFiles([]);
      setUploading(false);
    }

    setMessages((prev) => [
      ...prev,
      { role: "user", content: text, attachments, timestamp: Date.now() },
    ]);
    setIsRunning(true);
    setToolActivity(null);

    try {
      const res = (await sendRpc("conversations.send", {
        conversationId: conversationId || "",
        agentId,
        message: text,
        attachments,
        origin: "webui",
      })) as { conversationId?: string } | undefined;

      // When sending without a conversationId, the server creates a new one
      // and returns it. Update the ref immediately so incoming events for this
      // conversation are matched, then notify the parent.
      if (!conversationId && res?.conversationId) {
        activeConvIdRef.current = res.conversationId;
        skipNextHistoryLoadRef.current = true;
        onConversationCreated?.(res.conversationId);
      }
    } catch {
      // Error will come via event.
    }
  }, [
    input,
    conversationId,
    agentId,
    streaming,
    pendingFiles,
    onConversationCreated,
  ]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 120) + "px";
  }, []);

  const handleFileChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (e.target.files && e.target.files.length > 0) {
        addFiles(e.target.files);
        e.target.value = "";
      }
    },
    [addFiles],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      if (e.dataTransfer.files.length > 0) addFiles(e.dataTransfer.files);
    },
    [addFiles],
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const items = e.clipboardData.items;
      const images: File[] = [];
      for (let i = 0; i < items.length; i++) {
        if (items[i].type.startsWith("image/")) {
          const file = items[i].getAsFile();
          if (file) images.push(file);
        }
      }
      if (images.length > 0) {
        e.preventDefault();
        addFiles(images);
      }
    },
    [addFiles],
  );

  const { t } = useTranslation();
  const hasContent = !!input.trim() || pendingFiles.length > 0;

  // Handle copy button clicks in code blocks (delegated).
  const handleMessageAreaClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const target = e.target as HTMLElement;
      const copyBtn = target.closest(".copy-btn") as HTMLElement | null;
      if (!copyBtn) return;
      const codeBlock = copyBtn.closest(".code-block");
      const code = codeBlock?.querySelector("pre code");
      if (code) {
        navigator.clipboard.writeText(code.textContent || "").then(() => {
          copyBtn.classList.add("copied");
          setTimeout(() => copyBtn.classList.remove("copied"), 1500);
        });
      }
    },
    [],
  );

  return (
    <Box
      sx={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Messages */}
      <Box
        sx={{
          flex: 1,
          overflowY: "auto",
          px: 1,
          py: 1,
          display: "flex",
          flexDirection: "column",
          gap: 1,
        }}
        onClick={handleMessageAreaClick}
      >
        {messages.map((msg, i) => {
          const elements: React.ReactNode[] = [];

          // Insert date separator when the date changes between user/assistant messages.
          if (
            msg.timestamp &&
            (msg.role === "user" || msg.role === "assistant")
          ) {
            const label = dateLabelFor(msg.timestamp, t);
            let prevLabel = "";
            for (let j = i - 1; j >= 0; j--) {
              const prev = messages[j];
              if (
                prev.timestamp &&
                (prev.role === "user" || prev.role === "assistant")
              ) {
                prevLabel = dateLabelFor(prev.timestamp, t);
                break;
              }
            }
            if (label !== prevLabel) {
              elements.push(<DateSeparator key={`sep-${i}`} label={label} />);
            }
          }

          if (msg.role === "tool_call") {
            elements.push(
              <ToolInvoke
                key={i}
                toolName={msg.toolName || "unknown"}
                args={msg.content}
              />,
            );
          } else if (msg.role === "tool_result") {
            elements.push(
              <ToolResult
                key={i}
                toolName={msg.toolName || "unknown"}
                content={msg.content}
              />,
            );
          } else {
            elements.push(
              <MessageBubble
                key={i}
                role={msg.role}
                content={msg.content}
                timestamp={msg.timestamp}
                attachments={msg.attachments}
                avatarMediaId={
                  msg.role === "user" ? userAvatarMediaId : agentAvatarMediaId
                }
                avatarSrc={resolveMediaUrl(
                  msg.role === "user" ? userAvatarMediaId : agentAvatarMediaId,
                )}
                avatarFallback={
                  msg.role === "user" ? userName || "You" : agentName || "Agent"
                }
              />,
            );
          }

          return elements;
        })}
        {streaming && streamingText && (
          <MessageBubble
            role="assistant"
            content=""
            isStreaming
            streamText={streamingText}
            avatarMediaId={agentAvatarMediaId}
            avatarSrc={resolveMediaUrl(agentAvatarMediaId)}
            avatarFallback={agentName || "Agent"}
          />
        )}
        {isRunning && !streaming && (
          <Box
            sx={{
              alignSelf: "flex-start",
              px: 1.5,
              py: 0.5,
              display: "flex",
              alignItems: "center",
              gap: 1,
            }}
          >
            <CircularProgress size={12} color="primary" />
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontStyle: "italic" }}
            >
              {toolActivity ? `Calling ${toolActivity}...` : "Thinking..."}
            </Typography>
          </Box>
        )}
        <div ref={bottomRef} />
      </Box>

      {/* Input area with file upload (B5) */}
      <Box
        sx={{ px: 1, py: 1, borderTop: 1, borderColor: "divider" }}
        onDragOver={(e: React.DragEvent) => e.preventDefault()}
        onDrop={handleDrop}
      >
        {/* Pending files chips */}
        {pendingFiles.length > 0 && (
          <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", mb: 0.5 }}>
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
                        width: 20,
                        height: 20,
                        borderRadius: "50%",
                        objectFit: "cover",
                      }}
                    />
                  ) : undefined
                }
                sx={{ maxWidth: 180, fontSize: 11 }}
              />
            ))}
          </Box>
        )}
        <Box
          sx={{
            display: "flex",
            alignItems: "flex-end",
            gap: 0.5,
            bgcolor: "surface2",
            borderRadius: 1.5,
            border: 1,
            borderColor: "divider",
            px: 1,
            py: 0.5,
            "&:focus-within": { borderColor: "primary.main" },
          }}
        >
          <Box
            component="textarea"
            ref={textareaRef}
            value={input}
            onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
              setInput(e.target.value)
            }
            onKeyDown={handleKeyDown}
            onInput={handleInput}
            onPaste={handlePaste}
            placeholder={
              conversationId
                ? "Type a message..."
                : "Start a new conversation..."
            }
            disabled={streaming}
            rows={1}
            sx={{
              flex: 1,
              border: "none",
              outline: "none",
              bgcolor: "transparent",
              color: "text.primary",
              fontSize: "0.8125rem",
              fontFamily: "inherit",
              lineHeight: 1.5,
              resize: "none",
              overflow: "auto",
              py: 0.5,
              "&::placeholder": { color: "text.secondary", opacity: 1 },
            }}
          />
          <input
            type="file"
            ref={fileInputRef}
            multiple
            onChange={handleFileChange}
            style={{ display: "none" }}
          />
          <IconButton
            size="small"
            onClick={() => fileInputRef.current?.click()}
            disabled={streaming}
            sx={{
              width: 28,
              height: 28,
              color: "text.secondary",
              "&:hover": { color: "primary.main" },
            }}
          >
            <AttachFileRounded sx={{ fontSize: 16 }} />
          </IconButton>
          <IconButton
            size="small"
            color="primary"
            onClick={handleSend}
            disabled={uploading || streaming || !hasContent}
            sx={{ width: 28, height: 28 }}
          >
            <SendRounded sx={{ fontSize: 16 }} />
          </IconButton>
        </Box>
        {uploading && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ mt: 0.5, display: "block" }}
          >
            Uploading...
          </Typography>
        )}
      </Box>
    </Box>
  );
}
