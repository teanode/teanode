import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import CheckIcon from "@mui/icons-material/Check";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import { highlightJson } from "../markdown";

interface ToolInvokeProps {
  toolName: string;
  args: string;
}

export default function ToolInvoke({ toolName, args }: ToolInvokeProps) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(args).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  // Special rendering for ask_user_question tool invocations.
  if (toolName === "ask_user_question") {
    try {
      const parsed = JSON.parse(args);
      if (parsed.question) {
        return (
          <Box
            sx={{
              alignSelf: "flex-start",
              maxWidth: "75%",
              px: 1.5,
              py: 1,
              borderRadius: 1,
              fontSize: "0.75rem",
              bgcolor: (theme) =>
                theme.palette.mode === "dark"
                  ? "rgba(25, 118, 210, 0.08)"
                  : "rgba(25, 118, 210, 0.05)",
              border: 1,
              borderColor: "primary.main",
            }}
          >
            <Box
              sx={{ display: "flex", alignItems: "center", gap: 0.75, mb: 0.5 }}
            >
              <HelpOutlineRounded
                sx={{ fontSize: 14, color: "primary.main" }}
              />
              <Typography
                variant="caption"
                color="primary.main"
                sx={{ fontWeight: 600 }}
              >
                {t("tool.askUserQuestion")}
              </Typography>
            </Box>
            <Typography variant="body2" sx={{ mb: 0.5 }}>
              {parsed.question}
            </Typography>
            {parsed.choices && (
              <Typography variant="caption" color="text.secondary">
                {t("tool.askUserChoices")}: {parsed.choices.join(", ")}
                {parsed.allowOther &&
                  `, ${parsed.otherLabel || t("tool.askUserOther")}`}
              </Typography>
            )}
          </Box>
        );
      }
    } catch {
      // Fall through to default rendering.
    }
  }

  return (
    <Box
      sx={{
        alignSelf: "flex-start",
        maxWidth: "75%",
        px: 1.5,
        py: 1,
        borderRadius: 1,
        fontSize: "0.75rem",
        bgcolor: "toolBg",
        border: 1,
        borderColor: (theme) =>
          theme.palette.mode === "dark" ? "#3a3a20" : "#d5d5a0",
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
        <Chip
          label={toolName}
          size="small"
          color="primary"
          sx={{
            height: 18,
            fontSize: "10px",
            fontWeight: 600,
            fontFamily: "monospace",
            textTransform: "uppercase",
            letterSpacing: "0.05em",
          }}
        />
        <Typography variant="caption">{t("tool.called")}</Typography>
        <IconButton
          size="small"
          onClick={handleCopy}
          sx={{
            marginLeft: "auto",
            padding: "2px",
            color: copied ? "primary.main" : "text.secondary",
            "&:hover": { color: copied ? "primary.main" : "text.primary" },
          }}
        >
          {copied ? (
            <CheckIcon sx={{ fontSize: 14 }} />
          ) : (
            <ContentCopyIcon sx={{ fontSize: 14 }} />
          )}
        </IconButton>
      </Box>
      <Box
        component="pre"
        sx={{
          color: "text.secondary",
          fontFamily: "monospace",
          fontSize: "11px",
          mt: 0.5,
          px: 1,
          py: 0.75,
          bgcolor: (theme) =>
            theme.palette.mode === "dark"
              ? "rgba(0,0,0,0.15)"
              : "rgba(0,0,0,0.05)",
          borderRadius: 0.5,
          maxHeight: 160,
          overflowY: "auto",
          overflowX: "auto",
          m: 0,
        }}
      >
        <code
          className="hljs language-json"
          style={{
            fontSize: "11px",
            fontFamily: "monospace",
            backgroundColor: "transparent",
            padding: 0,
          }}
          dangerouslySetInnerHTML={{ __html: highlightJson(args) }}
        />
      </Box>
    </Box>
  );
}
