import React, { useCallback, useEffect, useState } from "react";
import Box from "@mui/material/Box";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import CircularProgress from "@mui/material/CircularProgress";
import CloseRounded from "@mui/icons-material/CloseRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import CheckRounded from "@mui/icons-material/CheckRounded";
import ArticleRounded from "@mui/icons-material/ArticleRounded";
import { renderMarkdown } from "../markdown";
import { useArtifactPanel } from "./ArtifactPanelProvider";
import { useArtifactContent } from "../hooks/useArtifactContent";

export default function ArtifactSidePanel() {
  const { closeArtifactPanel } = useArtifactPanel();
  const artifact = useArtifactContent();
  const [copied, setCopied] = useState(false);

  // Close on Escape.
  useEffect(() => {
    if (!artifact) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeArtifactPanel();
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [artifact, closeArtifactPanel]);

  // Reset copied state when artifact changes.
  useEffect(() => {
    setCopied(false);
  }, [artifact?.title]);

  const handleCopy = useCallback(() => {
    if (!artifact) return;
    navigator.clipboard.writeText(artifact.content).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }, [artifact]);

  if (!artifact) return null;

  return (
    <Box
      sx={{
        width: { md: 440, lg: 560, xl: 680 },
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        borderLeft: 1,
        borderColor: "divider",
        bgcolor: "background.default",
        minHeight: 0,
      }}
    >
      {/* Header */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          px: 3,
          py: 1.5,
          borderBottom: 1,
          borderColor: "divider",
          flexShrink: 0,
        }}
      >
        <ArticleRounded
          sx={{ fontSize: 20, color: "primary.main", flexShrink: 0 }}
        />
        <Typography
          variant="subtitle2"
          sx={{
            flex: 1,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {artifact.title}
        </Typography>
        {artifact.isStreaming && (
          <CircularProgress size={14} sx={{ flexShrink: 0 }} />
        )}
        <Tooltip title={copied ? "Copied" : "Copy content"}>
          <IconButton size="small" onClick={handleCopy}>
            {copied ? (
              <CheckRounded sx={{ fontSize: 16, color: "success.main" }} />
            ) : (
              <ContentCopyRounded sx={{ fontSize: 16 }} />
            )}
          </IconButton>
        </Tooltip>
        <IconButton size="small" onClick={closeArtifactPanel}>
          <CloseRounded sx={{ fontSize: 18 }} />
        </IconButton>
      </Box>

      {/* Body */}
      <Box
        className="markdown-content"
        sx={{
          flex: 1,
          overflowY: "auto",
          px: 3.5,
          py: 3,
          lineHeight: 1.6,
          wordBreak: "break-word",
        }}
      >
        <div
          dangerouslySetInnerHTML={{
            __html: renderMarkdown(artifact.content),
          }}
        />
      </Box>
    </Box>
  );
}
