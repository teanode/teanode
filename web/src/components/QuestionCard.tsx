import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import type { PendingQuestion } from "../types";

interface QuestionCardProps {
  question: PendingQuestion;
  onAnswer: (questionId: string, answer: string) => void;
}

export default function QuestionCard({ question, onAnswer }: QuestionCardProps) {
  const { t } = useTranslation();
  const [answered, setAnswered] = useState(false);

  function handleChoice(choice: string) {
    if (answered) return;
    setAnswered(true);
    onAnswer(question.id, choice);
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
            disabled={answered}
            onClick={() => handleChoice(choice)}
            sx={{ textTransform: "none" }}
          >
            {choice}
          </Button>
        ))}
      </Box>
    </Box>
  );
}
