import React, { useState, useEffect, useRef, useCallback } from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import AttachFileRounded from "@mui/icons-material/AttachFileRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import MessageBubble from "../../components/MessageBubble";
import ToolInvoke from "../../components/ToolInvoke";
import ToolResult from "../../components/ToolResult";
import { sendRpc, onEvent, getBaseUrl } from "./rpc";
import type { RpcEventFrame } from "../shared/types";
import type { Attachment } from "../../types";

interface Message {
  role: "user" | "assistant" | "tool_call" | "tool_result";
  content: string;
  toolName?: string;
  attachments?: Attachment[];
}

interface PendingFile {
  file: File;
  previewUrl?: string;
}

interface Props {
  agentId: string;
  conversationId: string;
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

export function ChatView({ agentId, conversationId }: Props) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([]);
  const [uploading, setUploading] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Load history on conversation change.
  useEffect(() => {
    if (!conversationId) {
      setMessages([]);
      return;
    }
    setMessages([]);
    setStreamingText("");
    sendRpc("conversations.history", { conversationId })
      .then((payload) => {
        const data = payload as {
          messages?: Array<{
            role: string;
            content: string;
            name?: string;
            attachments?: Attachment[];
          }>;
        };
        if (data.messages) {
          setMessages(
            data.messages
              .filter((m) => m.role === "user" || m.role === "assistant")
              .map((m) => ({
                role: m.role as "user" | "assistant",
                content: m.content,
                attachments: m.attachments,
              })),
          );
        }
      })
      .catch(() => {});
  }, [conversationId]);

  // Subscribe to conversation events.
  useEffect(() => {
    return onEvent((frame: RpcEventFrame) => {
      if (frame.event !== "conversation") return;
      const p = frame.payload as Record<string, unknown>;
      if (p.conversationId !== conversationId) return;

      switch (p.state) {
        case "delta":
          setStreaming(true);
          setStreamingText((prev) => prev + (p.text as string));
          break;
        case "tool_call":
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
          setStreaming(false);
          setMessages((prev) => [
            ...prev,
            { role: "assistant", content: (p.text as string) || "" },
          ]);
          setStreamingText("");
          break;
        case "error":
          setStreaming(false);
          setStreamingText("");
          break;
        case "aborted":
          setStreaming(false);
          setStreamingText("");
          break;
      }
    });
  }, [conversationId]);

  // Auto-scroll.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingText]);

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
    if ((!text && pendingFiles.length === 0) || !conversationId || streaming)
      return;

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
      { role: "user", content: text, attachments },
    ]);

    try {
      await sendRpc("conversations.send", {
        conversationId,
        agentId,
        message: text,
        attachments,
        origin: "webui",
      });
    } catch {
      // Error will come via event.
    }
  }, [input, conversationId, agentId, streaming, pendingFiles]);

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
          if (msg.role === "tool_call") {
            return (
              <ToolInvoke
                key={i}
                toolName={msg.toolName || "unknown"}
                args={msg.content}
              />
            );
          }
          if (msg.role === "tool_result") {
            return (
              <ToolResult
                key={i}
                toolName={msg.toolName || "unknown"}
                content={msg.content}
              />
            );
          }
          return (
            <MessageBubble
              key={i}
              role={msg.role}
              content={msg.content}
              attachments={msg.attachments}
            />
          );
        })}
        {streaming && streamingText && (
          <MessageBubble
            role="assistant"
            content=""
            isStreaming
            streamText={streamingText}
          />
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
              conversationId ? "Type a message..." : "Select a conversation"
            }
            disabled={!conversationId || streaming}
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
            disabled={!conversationId || streaming}
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
            disabled={uploading || streaming || !hasContent || !conversationId}
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
