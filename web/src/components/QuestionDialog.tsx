import React, { useState, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import List from "@mui/material/List";
import ListItemButton from "@mui/material/ListItemButton";
import ListItemText from "@mui/material/ListItemText";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import ChevronLeftRounded from "@mui/icons-material/ChevronLeftRounded";
import ChevronRightRounded from "@mui/icons-material/ChevronRightRounded";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import type { PendingQuestion } from "../types";

interface QuestionDialogProps {
  pendingQuestions: PendingQuestion[];
  onAnswer: (questionId: string, answer: string, other?: string) => void;
}

export default function QuestionDialog({
  pendingQuestions,
  onAnswer,
}: QuestionDialogProps) {
  const { t } = useTranslation();
  const [activeIndex, setActiveIndex] = useState(0);
  const [showOtherInput, setShowOtherInput] = useState(false);
  const [otherText, setOtherText] = useState("");
  const [answeredIds, setAnsweredIds] = useState<Set<string>>(new Set());
  const otherInputRef = useRef<HTMLInputElement>(null);

  const open = pendingQuestions.length > 0;

  // Clamp activeIndex when questions change (e.g. one was answered and removed).
  useEffect(() => {
    if (pendingQuestions.length > 0 && activeIndex >= pendingQuestions.length) {
      setActiveIndex(pendingQuestions.length - 1);
    }
  }, [pendingQuestions.length, activeIndex]);

  // Reset other-input state when active question changes.
  useEffect(() => {
    setShowOtherInput(false);
    setOtherText("");
  }, [activeIndex, pendingQuestions.length]);

  // Focus the Other text input when it appears.
  useEffect(() => {
    if (showOtherInput) {
      // Defer to allow the DOM to render the input.
      requestAnimationFrame(() => otherInputRef.current?.focus());
    }
  }, [showOtherInput]);

  if (!open) return null;

  const question = pendingQuestions[activeIndex] ?? pendingQuestions[0];
  if (!question) return null;

  const isAnswered = answeredIds.has(question.id);
  const otherLabel = question.otherLabel || t("tool.askUserOther");
  const multiple = pendingQuestions.length > 1;

  function handleChoice(choice: string) {
    if (isAnswered) return;
    setAnsweredIds((prev) => new Set(prev).add(question.id));
    onAnswer(question.id, choice);
  }

  function handleOtherClick() {
    if (isAnswered) return;
    setShowOtherInput(true);
  }

  function handleOtherSubmit() {
    if (isAnswered || !otherText.trim()) return;
    setAnsweredIds((prev) => new Set(prev).add(question.id));
    onAnswer(question.id, otherLabel, otherText.trim());
  }

  function handleOtherKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleOtherSubmit();
    }
  }

  function handlePrev() {
    setActiveIndex((i) => Math.max(0, i - 1));
  }

  function handleNext() {
    setActiveIndex((i) => Math.min(pendingQuestions.length - 1, i + 1));
  }

  return (
    <Dialog
      open={open}
      maxWidth="sm"
      fullWidth
      disableEscapeKeyDown
      onClose={(_event, reason) => {
        // Prevent closing via backdrop click while questions are pending.
        if (reason === "backdropClick" || reason === "escapeKeyDown") return;
      }}
      aria-labelledby="question-dialog-title"
    >
      <DialogTitle
        id="question-dialog-title"
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          pb: 1,
        }}
      >
        <HelpOutlineRounded sx={{ fontSize: 20, color: "primary.main" }} />
        <Typography variant="h6" component="span" sx={{ flex: 1 }}>
          {t("tool.askUserQuestion")}
        </Typography>
        {multiple && (
          <Typography variant="caption" color="text.secondary">
            {t("tool.questionCount", {
              current: activeIndex + 1,
              total: pendingQuestions.length,
            })}
          </Typography>
        )}
      </DialogTitle>

      <DialogContent sx={{ pt: 0 }}>
        {/* Question list sidebar for multiple questions */}
        {multiple && (
          <List
            dense
            sx={{
              mb: 2,
              bgcolor: "action.hover",
              borderRadius: 1,
              maxHeight: 160,
              overflow: "auto",
            }}
          >
            {pendingQuestions.map((q, idx) => (
              <ListItemButton
                key={q.id}
                selected={idx === activeIndex}
                onClick={() => setActiveIndex(idx)}
                sx={{ borderRadius: 1 }}
              >
                <ListItemText
                  primary={q.question}
                  primaryTypographyProps={{
                    variant: "body2",
                    noWrap: true,
                  }}
                />
              </ListItemButton>
            ))}
          </List>
        )}

        {/* Active question */}
        <Typography variant="body1" sx={{ mb: 2 }}>
          {question.question}
        </Typography>

        {/* Choice buttons */}
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
          {question.choices.map((choice) => (
            <Button
              key={choice}
              variant="outlined"
              size="small"
              disabled={isAnswered || showOtherInput}
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
              disabled={isAnswered}
              onClick={handleOtherClick}
              sx={{ textTransform: "none", fontStyle: "italic" }}
            >
              {otherLabel}
            </Button>
          )}
        </Box>

        {/* Other text input */}
        {showOtherInput && (
          <Box sx={{ display: "flex", gap: 1, mt: 2, alignItems: "center" }}>
            <TextField
              inputRef={otherInputRef}
              size="small"
              fullWidth
              disabled={isAnswered}
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
              disabled={isAnswered || !otherText.trim()}
              onClick={handleOtherSubmit}
              sx={{ textTransform: "none", whiteSpace: "nowrap" }}
            >
              {t("common.save")}
            </Button>
          </Box>
        )}

        {/* Prev / Next navigation */}
        {multiple && (
          <Box
            sx={{
              display: "flex",
              justifyContent: "flex-end",
              alignItems: "center",
              mt: 2,
              gap: 0.5,
            }}
          >
            <IconButton
              size="small"
              disabled={activeIndex === 0}
              onClick={handlePrev}
              aria-label={t("tool.questionPrev")}
            >
              <ChevronLeftRounded />
            </IconButton>
            <IconButton
              size="small"
              disabled={activeIndex === pendingQuestions.length - 1}
              onClick={handleNext}
              aria-label={t("tool.questionNext")}
            >
              <ChevronRightRounded />
            </IconButton>
          </Box>
        )}
      </DialogContent>
    </Dialog>
  );
}
