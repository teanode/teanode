import React, { useCallback, useEffect, useState } from "react";
import Box from "@mui/material/Box";
import Dialog from "@mui/material/Dialog";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import CircularProgress from "@mui/material/CircularProgress";
import Slide from "@mui/material/Slide";
import CloseRounded from "@mui/icons-material/CloseRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import CheckRounded from "@mui/icons-material/CheckRounded";
import ArticleRounded from "@mui/icons-material/ArticleRounded";
import type { TransitionProps } from "@mui/material/transitions";
import { renderMarkdown } from "../markdown";
import { useArtifactPanel } from "./ArtifactPanelProvider";
import { useArtifactContent } from "../hooks/useArtifactContent";

const SlideUp = React.forwardRef(function SlideUp(
  props: TransitionProps & { children: React.ReactElement },
  ref: React.Ref<unknown>,
) {
  return <Slide direction="up" ref={ref} {...props} />;
});

export default function ArtifactMobileOverlay() {
  const { closeArtifactPanel } = useArtifactPanel();
  const artifact = useArtifactContent();
  const [copied, setCopied] = useState(false);

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

  return (
    <Dialog
      open={artifact !== null}
      onClose={closeArtifactPanel}
      fullScreen
      TransitionComponent={SlideUp}
    >
      {artifact && (
        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            height: "100%",
            bgcolor: "background.default",
          }}
        >
          {/* Header */}
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: 1,
              px: 2.5,
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
              px: 3,
              py: 2.5,
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
      )}
    </Dialog>
  );
}
