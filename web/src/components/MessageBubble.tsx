import React from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import InsertDriveFileRounded from "@mui/icons-material/InsertDriveFileRounded";
import StopRounded from "@mui/icons-material/StopRounded";
import VolumeUpRounded from "@mui/icons-material/VolumeUpRounded";
import { renderMarkdown } from "../markdown";
import {
  hasFencedBlocks,
  parseArtifacts,
  parseArtifactsStreaming,
} from "../artifactParser";
import type { Attachment } from "../types";
import ConversationAvatar from "./ConversationAvatar";
import ArtifactChip from "./ArtifactChip";
import ChartRenderer from "./ChartRenderer";

interface MessageBubbleProps {
  role: "user" | "assistant";
  messageId?: string;
  content: string;
  isStreaming?: boolean;
  streamText?: string;
  timestamp?: number;
  attachments?: Attachment[];
  avatarMediaId?: string;
  /** Pre-resolved avatar URL (for extension contexts where withToken doesn't work). */
  avatarSrc?: string;
  avatarFallback?: string;
  /** Resolve a media ID to a full URL (for extension contexts where relative paths don't work). */
  resolveMediaUrl?: (mediaId: string) => string;
  voiceEnabled?: boolean;
  isSpeakingThis?: boolean;
  onSpeak?: (text: string) => void;
  onStopSpeaking?: () => void;
}

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function isImageFormat(format: string): boolean {
  return ["png", "jpeg", "jpg", "gif", "webp"].includes(format.toLowerCase());
}

function AttachmentDisplay({
  attachment,
  resolveMediaUrl,
}: {
  attachment: Attachment;
  resolveMediaUrl?: (mediaId: string) => string;
}) {
  const mediaUrl = resolveMediaUrl
    ? resolveMediaUrl(attachment.mediaId)
    : `/api/media/${attachment.mediaId}`;
  if (isImageFormat(attachment.format)) {
    return (
      <Box
        component="img"
        src={mediaUrl}
        alt={attachment.filename}
        sx={{
          maxWidth: 300,
          maxHeight: 200,
          borderRadius: 1,
          objectFit: "contain",
          cursor: "pointer",
        }}
        onClick={() => window.open(mediaUrl, "_blank")}
      />
    );
  }
  return (
    <Chip
      icon={<InsertDriveFileRounded />}
      label={
        attachment.filename || `${attachment.mediaId}.${attachment.format}`
      }
      size="small"
      variant="outlined"
      component="a"
      href={mediaUrl}
      target="_blank"
      clickable
      sx={{ maxWidth: 250 }}
    />
  );
}

function stripMarkdown(text: string): string {
  return text
    .replace(/```[\s\S]*?```/g, "") // code blocks
    .replace(/`[^`]*`/g, "") // inline code
    .replace(/!\[.*?\]\(.*?\)/g, "") // images
    .replace(/\[([^\]]*)\]\(.*?\)/g, "$1") // links
    .replace(/#{1,6}\s/g, "") // headings
    .replace(/[*_~]+/g, "") // emphasis
    .replace(/>\s/g, "") // blockquotes
    .replace(/[-*+]\s/g, "") // list markers
    .replace(/\n{2,}/g, "\n") // collapse blank lines
    .trim();
}

export default function MessageBubble({
  role,
  messageId,
  content,
  isStreaming,
  streamText,
  timestamp,
  attachments,
  avatarMediaId,
  avatarSrc,
  avatarFallback,
  resolveMediaUrl,
  voiceEnabled,
  isSpeakingThis,
  onSpeak,
  onStopSpeaking,
}: MessageBubbleProps) {
  const { t } = useTranslation();
  const isUser = role === "user";

  const timeElement = timestamp ? (
    <Typography
      variant="caption"
      color="text.secondary"
      sx={{
        fontSize: "10px",
        userSelect: "none",
        opacity: 0,
        transition: "opacity 0.15s",
        whiteSpace: "nowrap",
        ".message-row:hover &": { opacity: 1 },
      }}
    >
      {formatTime(timestamp)}
    </Typography>
  ) : null;

  let bubble: React.ReactNode;

  if (isUser) {
    bubble = (
      <Paper
        elevation={0}
        sx={{
          minWidth: 0,
          maxWidth: { xs: "95%", md: "85%" },
          px: 2,
          py: 1.5,
          lineHeight: 1.6,
          wordBreak: "break-word",
          whiteSpace: "pre-wrap",
          bgcolor: "userBg",
          border: 1,
          borderColor: (theme) =>
            theme.palette.mode === "dark" ? "#3a4a1a" : "#c5d9a5",
        }}
      >
        {content}
        {attachments && attachments.length > 0 && (
          <Box
            sx={{
              display: "flex",
              gap: 1,
              flexWrap: "wrap",
              mt: content ? 1 : 0,
            }}
          >
            {attachments.map((att, index) => (
              <AttachmentDisplay
                key={index}
                attachment={att}
                resolveMediaUrl={resolveMediaUrl}
              />
            ))}
          </Box>
        )}
      </Paper>
    );
  } else {
    const displayText = isStreaming ? streamText || content : content;

    if (displayText.startsWith("__error__:")) {
      const errorMessage = displayText.substring("__error__:".length);
      bubble = (
        <Box
          sx={{
            minWidth: 0,
            maxWidth: { xs: "95%", md: "85%" },
            px: 2,
            py: 1.5,
            lineHeight: 1.6,
            wordBreak: "break-word",
          }}
        >
          <Typography component="em" color="error.main">
            {t("conversations.error", { message: errorMessage })}
          </Typography>
        </Box>
      );
    } else if (displayText === "__aborted__") {
      bubble = (
        <Box
          sx={{
            minWidth: 0,
            maxWidth: { xs: "95%", md: "85%" },
            px: 2,
            py: 1.5,
            lineHeight: 1.6,
            wordBreak: "break-word",
          }}
        >
          <Typography component="em" color="text.secondary">
            {t("conversations.aborted")}
          </Typography>
        </Box>
      );
    } else if (!displayText) {
      return null;
    } else if (messageId && hasFencedBlocks(displayText)) {
      // Parse artifacts/charts and render text segments as markdown,
      // artifact segments as compact chips, chart segments inline.
      const parsed = isStreaming
        ? parseArtifactsStreaming(displayText)
        : {
            segments: parseArtifacts(displayText),
            pendingArtifact: null,
            pendingChart: null,
          };

      bubble = (
        <Box
          className="markdown-content"
          sx={{
            minWidth: 0,
            maxWidth: { xs: "95%", md: "85%" },
            px: 2,
            py: 1.5,
            lineHeight: 1.6,
            wordBreak: "break-word",
          }}
        >
          {parsed.segments.map((segment, index) => {
            if (segment.kind === "text") {
              return (
                <div
                  key={index}
                  dangerouslySetInnerHTML={{
                    __html: renderMarkdown(segment.content),
                  }}
                />
              );
            }
            if (segment.kind === "chart") {
              return (
                <ChartRenderer
                  key={`chart-${segment.index}`}
                  title={segment.title}
                  content={segment.content}
                />
              );
            }
            return (
              <ArtifactChip
                key={`artifact-${segment.index}`}
                messageId={messageId}
                artifactIndex={segment.index}
                title={segment.title}
                isStreaming={false}
              />
            );
          })}
          {parsed.pendingArtifact && (
            <ArtifactChip
              key={`artifact-${parsed.pendingArtifact.index}`}
              messageId={messageId}
              artifactIndex={parsed.pendingArtifact.index}
              title={parsed.pendingArtifact.title}
              isStreaming={true}
            />
          )}
          {parsed.pendingChart && (
            <ChartRenderer
              key={`chart-pending-${parsed.pendingChart.index}`}
              title={parsed.pendingChart.title}
              content={parsed.pendingChart.content}
              isStreaming={true}
            />
          )}
        </Box>
      );
    } else {
      bubble = (
        <Box
          className="markdown-content"
          sx={{
            minWidth: 0,
            maxWidth: { xs: "95%", md: "85%" },
            px: 2,
            py: 1.5,
            lineHeight: 1.6,
            wordBreak: "break-word",
          }}
        >
          <div
            dangerouslySetInnerHTML={{ __html: renderMarkdown(displayText) }}
          />
        </Box>
      );
    }
  }

  const showSpeaker =
    voiceEnabled &&
    !isUser &&
    !isStreaming &&
    content &&
    !content.startsWith("__error__:") &&
    content !== "__aborted__";

  const speakerElement = showSpeaker ? (
    <IconButton
      size="small"
      onClick={() => {
        if (isSpeakingThis) {
          onStopSpeaking?.();
        } else {
          onSpeak?.(stripMarkdown(content));
        }
      }}
      sx={{
        width: 24,
        height: 24,
        opacity: isSpeakingThis ? 1 : 0,
        transition: "opacity 0.15s",
        ".message-row:hover &": { opacity: 1 },
        color: isSpeakingThis ? "primary.main" : "text.secondary",
      }}
    >
      {isSpeakingThis ? (
        <StopRounded sx={{ fontSize: 16 }} />
      ) : (
        <VolumeUpRounded sx={{ fontSize: 16 }} />
      )}
    </IconButton>
  ) : null;

  if (isUser) {
    return (
      <Box
        className="message-row"
        sx={{
          display: "flex",
          flexDirection: "row-reverse",
          alignItems: "flex-end",
          gap: 0.75,
          alignSelf: "flex-end",
          maxWidth: "100%",
        }}
      >
        <ConversationAvatar
          avatarMediaId={avatarMediaId}
          src={avatarSrc}
          fallback={avatarFallback || "U"}
        />
        {bubble}
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 0.25,
            flexShrink: 0,
          }}
        >
          {speakerElement}
          {timeElement}
        </Box>
      </Box>
    );
  }

  return (
    <Box
      className="message-row"
      sx={{
        display: "flex",
        alignItems: "flex-end",
        gap: 0.75,
        alignSelf: "flex-start",
        maxWidth: "100%",
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "flex-end",
          gap: 0.75,
          minWidth: 0,
          maxWidth: "100%",
        }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "flex-end",
            transform: "translateY(-1px)",
          }}
        >
          <ConversationAvatar
            avatarMediaId={avatarMediaId}
            src={avatarSrc}
            fallback={avatarFallback || "A"}
          />
        </Box>
        {bubble}
      </Box>
      <Box
        sx={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: 0.25,
          flexShrink: 0,
        }}
      >
        {speakerElement}
        {timeElement}
      </Box>
    </Box>
  );
}
