import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import type { PendingQuestion } from "../types";

interface QuestionCardProps {
  question: PendingQuestion;
  onAnswer: (questionId: string, answer: string, other?: string) => void;
}

export default function QuestionCard({
  question,
  onAnswer,
}: QuestionCardProps) {
  const { t } = useTranslation();
  const [answered, setAnswered] = useState(false);
  const [showOtherInput, setShowOtherInput] = useState(false);
  const [otherText, setOtherText] = useState("");

  const otherLabel = question.otherLabel || t("tool.askUserOther");

  function handleChoice(choice: string) {
    if (answered) return;
    setAnswered(true);
    onAnswer(question.id, choice);
  }

  function handleOtherClick() {
    if (answered) return;
    setShowOtherInput(true);
  }

  function handleOtherSubmit() {
    if (answered || !otherText.trim()) return;
    setAnswered(true);
    onAnswer(question.id, otherLabel, otherText.trim());
  }

  function handleOtherKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleOtherSubmit();
    }
  }

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
      <Typography variant="body2" sx={{ mb: 1.5 }}>
        {question.question}
      </Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
        {question.choices.map((choice) => (
          <Button
            key={choice}
            variant="outlined"
            size="small"
            disabled={answered || showOtherInput}
            onClick={() => handleChoice(choice)}
            sx={{ textTransform: "none" }}
          >
            {choice}
          </Button>
        ))}
        {question.allowOther && !showOtherInput && (
          <Button
            variant="outlined"
            size="small"
            disabled={answered}
            onClick={handleOtherClick}
            sx={{ textTransform: "none", fontStyle: "italic" }}
          >
            {otherLabel}
          </Button>
        )}
      </Box>
      {showOtherInput && (
        <Box sx={{ display: "flex", gap: 1, mt: 1.5, alignItems: "center" }}>
          <TextField
            size="small"
            autoFocus
            fullWidth
            disabled={answered}
            placeholder={
              question.otherPlaceholder || t("tool.askUserOtherPlaceholder")
            }
            value={otherText}
            onChange={(e) => setOtherText(e.target.value)}
            onKeyDown={handleOtherKeyDown}
            sx={{ "& .MuiInputBase-input": { fontSize: "0.875rem" } }}
          />
          <Button
            variant="contained"
            size="small"
            disabled={answered || !otherText.trim()}
            onClick={handleOtherSubmit}
            sx={{ textTransform: "none", whiteSpace: "nowrap" }}
          >
            {t("common.save")}
          </Button>
        </Box>
      )}
    </Box>
  );
}
