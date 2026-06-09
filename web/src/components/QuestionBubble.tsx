import React, { useState, useRef, useEffect } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import type { PendingQuestion } from "../types";

interface QuestionBubbleProps {
  question: PendingQuestion;
  onAnswer: (questionId: string, answer: string, other?: string) => void;
}

export default function QuestionBubble({
  question,
  onAnswer,
}: QuestionBubbleProps) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<string | null>(null);
  const [showOtherInput, setShowOtherInput] = useState(false);
  const [otherText, setOtherText] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const otherInputRef = useRef<HTMLInputElement>(null);

  const otherLabel = question.otherLabel || t("tool.askUserOther");

  // Focus the Other text input when it appears.
  useEffect(() => {
    if (showOtherInput) {
      requestAnimationFrame(() => otherInputRef.current?.focus());
    }
  }, [showOtherInput]);

  function handleChoiceClick(choice: string) {
    if (submitted) return;
    if (showOtherInput) return;
    setSelected(choice);
  }

  function handleOtherClick() {
    if (submitted) return;
    setSelected(null);
    setShowOtherInput(true);
  }

  function handleBack() {
    setShowOtherInput(false);
    setOtherText("");
  }

  function handleSubmit() {
    if (submitted) return;
    if (showOtherInput) {
      if (!otherText.trim()) return;
      setSubmitted(true);
      onAnswer(question.id, otherLabel, otherText.trim());
    } else {
      if (!selected) return;
      setSubmitted(true);
      onAnswer(question.id, selected);
    }
  }

  function handleOtherKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  }

  const canSubmit = showOtherInput
    ? otherText.trim().length > 0
    : selected !== null;

  return (
    <Box
      sx={{
        alignSelf: "flex-start",
        maxWidth: "75%",
        px: 2,
        py: 1.5,
        borderRadius: 1,
        bgcolor: (theme) =>
          theme.palette.mode === "dark"
            ? "rgba(25, 118, 210, 0.08)"
            : "rgba(25, 118, 210, 0.05)",
        border: 1,
        borderColor: "primary.main",
      }}
    >
      {/* Header */}
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, mb: 1 }}>
        <HelpOutlineRounded sx={{ fontSize: 16, color: "primary.main" }} />
        <Typography
          variant="caption"
          color="primary.main"
          sx={{ fontWeight: 600 }}
        >
          {t("tool.askUserQuestion")}
        </Typography>
      </Box>

      {/* Question text */}
      <Typography variant="body2" sx={{ mb: 1.5, overflowWrap: "anywhere" }}>
        {question.question}
      </Typography>

      {/* Choice buttons */}
      {!showOtherInput && (
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
          {question.choices.map((choice) => (
            <Button
              key={choice}
              variant={selected === choice ? "contained" : "outlined"}
              size="small"
              disabled={submitted}
              onClick={() => handleChoiceClick(choice)}
              sx={{ textTransform: "none" }}
            >
              {choice}
            </Button>
          ))}
          {question.allowOther && (
            <Button
              variant="outlined"
              size="small"
              disabled={submitted}
              onClick={handleOtherClick}
              sx={{ textTransform: "none", fontStyle: "italic" }}
            >
              {otherLabel}
            </Button>
          )}
        </Box>
      )}

      {/* Other text input */}
      {showOtherInput && (
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <TextField
            inputRef={otherInputRef}
            size="small"
            fullWidth
            disabled={submitted}
            placeholder={
              question.otherPlaceholder || t("tool.askUserOtherPlaceholder")
            }
            value={otherText}
            onChange={(e) => setOtherText(e.target.value)}
            onKeyDown={handleOtherKeyDown}
            sx={{ "& .MuiInputBase-input": { fontSize: "0.875rem" } }}
          />
          <Button
            variant="text"
            size="small"
            disabled={submitted}
            onClick={handleBack}
            sx={{ textTransform: "none", whiteSpace: "nowrap" }}
          >
            {t("tool.askUserBack")}
          </Button>
        </Box>
      )}

      {/* Submit button */}
      <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 1.5 }}>
        <Button
          variant="contained"
          size="small"
          disabled={submitted || !canSubmit}
          onClick={handleSubmit}
          sx={{ textTransform: "none" }}
        >
          {t("tool.askUserSubmit")}
        </Button>
      </Box>
    </Box>
  );
}
