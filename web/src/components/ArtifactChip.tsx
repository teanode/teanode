import React, { useEffect, useRef } from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import ArticleRounded from "@mui/icons-material/ArticleRounded";
import OpenInNewRounded from "@mui/icons-material/OpenInNewRounded";
import { useArtifactPanel } from "./ArtifactPanelProvider";

interface ArtifactChipProps {
  messageId: string;
  artifactIndex: number;
  title: string;
  isStreaming: boolean;
}

export default function ArtifactChip({
  messageId,
  artifactIndex,
  title,
  isStreaming,
}: ArtifactChipProps) {
  const { openMessageId, openArtifactIndex, dismissedId, openArtifactPanel } =
    useArtifactPanel();

  const isOpen =
    openMessageId === messageId && openArtifactIndex === artifactIndex;

  // Auto-open when a streaming artifact first appears.
  const hasAutoOpened = useRef(false);
  useEffect(() => {
    if (!isStreaming) {
      hasAutoOpened.current = false;
      return;
    }
    if (hasAutoOpened.current) return;
    // Skip if user explicitly dismissed this artifact.
    const artifactId = `${messageId}-${artifactIndex}`;
    if (dismissedId === artifactId) return;

    hasAutoOpened.current = true;
    openArtifactPanel(messageId, artifactIndex);
  }, [isStreaming, messageId, artifactIndex, dismissedId, openArtifactPanel]);

  const handleClick = () => {
    openArtifactPanel(messageId, artifactIndex);
  };

  return (
    <Box
      onClick={handleClick}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1.25,
        px: 2,
        my: 1,
        height: 56,
        lineHeight: 1,
        borderRadius: 1,
        border: 1,
        borderColor: isOpen ? "primary.main" : "divider",
        bgcolor: (theme) =>
          isOpen
            ? theme.palette.mode === "dark"
              ? "rgba(144,202,249,0.08)"
              : "rgba(25,118,210,0.06)"
            : theme.palette.mode === "dark"
              ? "rgba(255,255,255,0.04)"
              : "rgba(0,0,0,0.02)",
        cursor: "pointer",
        overflow: "hidden",
        transition: "border-color 0.2s, background-color 0.2s, box-shadow 0.2s",
        "&:hover": {
          borderColor: "primary.main",
          bgcolor: (theme) =>
            theme.palette.mode === "dark"
              ? "rgba(144,202,249,0.10)"
              : "rgba(25,118,210,0.08)",
          boxShadow: (theme) => `0 0 0 1px ${theme.palette.primary.main}20`,
        },
        ...(isStreaming && {
          animation: "artifact-pulse 2s ease-in-out infinite",
          "@keyframes artifact-pulse": {
            "0%, 100%": { borderColor: "primary.main" },
            "50%": { borderColor: "divider" },
          },
        }),
      }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          width: 28,
          height: 28,
          borderRadius: 1,
          bgcolor: (theme) =>
            theme.palette.mode === "dark"
              ? "rgba(144,202,249,0.12)"
              : "rgba(25,118,210,0.10)",
          flexShrink: 0,
        }}
      >
        <ArticleRounded sx={{ fontSize: 16, color: "primary.main" }} />
      </Box>
      <Box
        component="span"
        sx={{
          flex: 1,
          fontWeight: 500,
          fontSize: "0.875rem",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
          letterSpacing: "0.01em",
          display: "flex",
          alignItems: "center",
          height: "100%",
          margin: 0,
          padding: 0,
        }}
      >
        {title}
      </Box>
      <OpenInNewRounded
        sx={{
          fontSize: 15,
          color: "text.disabled",
          flexShrink: 0,
          opacity: 0.6,
          transition: "opacity 0.15s",
          ".message-row:hover &": { opacity: 1 },
        }}
      />
    </Box>
  );
}
