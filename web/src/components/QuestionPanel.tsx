import React, { useState, useRef, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Container from "@mui/material/Container";
import ChevronLeftRounded from "@mui/icons-material/ChevronLeftRounded";
import ChevronRightRounded from "@mui/icons-material/ChevronRightRounded";
import HelpOutlineRounded from "@mui/icons-material/HelpOutlineRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import type { PendingQuestion } from "../types";

/** Per-question local state tracked by the panel. */
export interface QuestionAnswer {
  /** The selected predefined choice, or null if Other is active. */
  selected: string | null;
  /** Whether the user is in "Other" freeform input mode. */
  showOther: boolean;
  /** The freeform text when in Other mode. */
  otherText: string;
}

/** Build a blank answer state for a question. */
function emptyAnswer(): QuestionAnswer {
  return { selected: null, showOther: false, otherText: "" };
}

/** Check whether a single question has been answered. */
export function isAnswered(a: QuestionAnswer): boolean {
  if (a.showOther) return a.otherText.trim().length > 0;
  return a.selected !== null;
}

/** Check whether all questions have been answered. */
export function allAnswered(
  questions: PendingQuestion[],
  answers: Map<string, QuestionAnswer>,
): boolean {
  return questions.every((q) => {
    const a = answers.get(q.id);
    return a != null && isAnswered(a);
  });
}

interface QuestionPanelProps {
  questions: PendingQuestion[];
  /** Called once with all answers when the user submits. */
  onSubmitAll: (
    answers: { questionId: string; answer: string; other?: string }[],
  ) => Promise<void> | void;
  /** Externally disable all actions (e.g. when the backend is disconnected). */
  disabled?: boolean;
}

export default function QuestionPanel({
  questions,
  onSubmitAll,
  disabled: externalDisabled = false,
}: QuestionPanelProps) {
  const { t } = useTranslation();
  const [currentIndex, setCurrentIndex] = useState(0);
  const [answers, setAnswers] = useState<Map<string, QuestionAnswer>>(
    () => new Map(),
  );
  const [submitted, setSubmitted] = useState(false);
  const disabled = submitted || externalDisabled;
  const otherInputRef = useRef<HTMLInputElement>(null);

  // Keep answers map in sync when questions change (new questions arrive,
  // answered questions disappear). Preserve existing answers.
  useEffect(() => {
    setAnswers((prev) => {
      const next = new Map(prev);
      let changed = false;
      for (const q of questions) {
        if (!next.has(q.id)) {
          next.set(q.id, emptyAnswer());
          changed = true;
        }
      }
      // Remove stale entries for questions that are no longer pending.
      const ids = new Set(questions.map((q) => q.id));
      for (const key of next.keys()) {
        if (!ids.has(key)) {
          next.delete(key);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [questions]);

  // Clamp current index when questions array shrinks.
  useEffect(() => {
    if (currentIndex >= questions.length && questions.length > 0) {
      setCurrentIndex(questions.length - 1);
    }
  }, [questions.length, currentIndex]);

  // Focus the Other text input when switching to Other mode.
  const currentQ = questions[currentIndex];
  const currentAnswer = currentQ ? answers.get(currentQ.id) : undefined;
  useEffect(() => {
    if (currentAnswer?.showOther) {
      requestAnimationFrame(() => otherInputRef.current?.focus());
    }
  }, [currentAnswer?.showOther]);

  // Reset submitted state when questions change (new batch arrives).
  useEffect(() => {
    setSubmitted(false);
  }, [questions.length]);

  const setCurrentAnswer = useCallback(
    (updater: (prev: QuestionAnswer) => QuestionAnswer) => {
      if (!currentQ) return;
      setAnswers((prev) => {
        const next = new Map(prev);
        const old = next.get(currentQ.id) || emptyAnswer();
        next.set(currentQ.id, updater(old));
        return next;
      });
    },
    [currentQ],
  );

  function handleChoiceClick(choice: string) {
    if (disabled) return;
    setCurrentAnswer((a) => ({ ...a, selected: choice, showOther: false }));
  }

  function handleOtherClick() {
    if (disabled) return;
    setCurrentAnswer((a) => ({ ...a, selected: null, showOther: true }));
  }

  function handleBack() {
    if (disabled) return;
    // Preserve otherText so user can come back to it.
    setCurrentAnswer((a) => ({ ...a, showOther: false }));
  }

  function handleOtherTextChange(text: string) {
    setCurrentAnswer((a) => ({ ...a, otherText: text }));
  }

  function handleOtherKeyDown(e: React.KeyboardEvent) {
    // Enter advances to next unanswered question (or submits if all done).
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (canSubmitAll) {
        handleSubmitAll();
      } else {
        // Go to next unanswered question.
        const nextIdx = questions.findIndex(
          (q, i) =>
            i > currentIndex && !isAnswered(answers.get(q.id) || emptyAnswer()),
        );
        if (nextIdx >= 0) setCurrentIndex(nextIdx);
      }
    }
  }

  // Swipe support.
  const touchStartX = useRef<number | null>(null);

  function handleTouchStart(e: React.TouchEvent) {
    touchStartX.current = e.touches[0].clientX;
  }

  function handleTouchEnd(e: React.TouchEvent) {
    if (touchStartX.current === null) return;
    const dx = e.changedTouches[0].clientX - touchStartX.current;
    touchStartX.current = null;
    const SWIPE_THRESHOLD = 50;
    if (dx > SWIPE_THRESHOLD && currentIndex > 0) {
      setCurrentIndex(currentIndex - 1);
    } else if (dx < -SWIPE_THRESHOLD && currentIndex < questions.length - 1) {
      setCurrentIndex(currentIndex + 1);
    }
  }

  const canSubmitAll = !disabled && allAnswered(questions, answers);

  function handleSubmitAll() {
    if (!canSubmitAll) return;
    setSubmitted(true);
    const result: { questionId: string; answer: string; other?: string }[] = [];
    for (const q of questions) {
      const a = answers.get(q.id)!;
      if (a.showOther) {
        const otherLabel = q.otherLabel || t("tool.askUserOther");
        result.push({
          questionId: q.id,
          answer: otherLabel,
          other: a.otherText.trim(),
        });
      } else {
        result.push({ questionId: q.id, answer: a.selected! });
      }
    }
    Promise.resolve(onSubmitAll(result)).catch(() => {
      // Re-enable the submit button so the user can retry.
      setSubmitted(false);
    });
  }

  if (questions.length === 0) return null;
  if (!currentQ) return null;

  const otherLabel = currentQ.otherLabel || t("tool.askUserOther");
  const a = currentAnswer || emptyAnswer();
  const hasManyQuestions = questions.length > 1;

  return (
    <Container maxWidth="md" sx={{ py: 1.5 }}>
      <Box
        onTouchStart={hasManyQuestions ? handleTouchStart : undefined}
        onTouchEnd={hasManyQuestions ? handleTouchEnd : undefined}
        sx={{
          bgcolor: "surface2",
          borderRadius: 1.5,
          border: 1,
          borderColor: "primary.main",
          px: 2,
          py: 1.5,
        }}
      >
        {/* Header row: icon + label + card counter + nav arrows */}
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 0.75,
            mb: 1,
          }}
        >
          <HelpOutlineRounded sx={{ fontSize: 16, color: "primary.main" }} />
          <Typography
            variant="caption"
            color="primary.main"
            sx={{ fontWeight: 600 }}
          >
            {t("tool.askUserQuestion")}
          </Typography>
          {hasManyQuestions && (
            <>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ ml: "auto" }}
              >
                {t("tool.questionCount", {
                  current: currentIndex + 1,
                  total: questions.length,
                })}
              </Typography>
              <IconButton
                size="small"
                disabled={currentIndex === 0}
                onClick={() => setCurrentIndex(currentIndex - 1)}
                aria-label={t("tool.questionPrev")}
                sx={{ width: 28, height: 28 }}
              >
                <ChevronLeftRounded fontSize="small" />
              </IconButton>
              <IconButton
                size="small"
                disabled={currentIndex === questions.length - 1}
                onClick={() => setCurrentIndex(currentIndex + 1)}
                aria-label={t("tool.questionNext")}
                sx={{ width: 28, height: 28 }}
              >
                <ChevronRightRounded fontSize="small" />
              </IconButton>
            </>
          )}
        </Box>

        {/* Question text */}
        <Typography variant="body2" sx={{ mb: 1.5, overflowWrap: "anywhere" }}>
          {currentQ.question}
        </Typography>

        {/* Choices */}
        {!a.showOther && (
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
            {currentQ.choices.map((choice) => (
              <Button
                key={choice}
                variant={a.selected === choice ? "contained" : "outlined"}
                size="small"
                disabled={disabled}
                onClick={() => handleChoiceClick(choice)}
                sx={{ textTransform: "none" }}
              >
                {choice}
              </Button>
            ))}
            {currentQ.allowOther && (
              <Button
                variant="outlined"
                size="small"
                disabled={disabled}
                onClick={handleOtherClick}
                sx={{ textTransform: "none", fontStyle: "italic" }}
              >
                {otherLabel}
              </Button>
            )}
          </Box>
        )}

        {/* Other input */}
        {a.showOther && (
          <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
            <TextField
              inputRef={otherInputRef}
              size="small"
              fullWidth
              disabled={disabled}
              placeholder={
                currentQ.otherPlaceholder || t("tool.askUserOtherPlaceholder")
              }
              value={a.otherText}
              onChange={(e) => handleOtherTextChange(e.target.value)}
              onKeyDown={handleOtherKeyDown}
              sx={{ "& .MuiInputBase-input": { fontSize: "0.875rem" } }}
            />
            <Button
              variant="text"
              size="small"
              disabled={disabled}
              onClick={handleBack}
              sx={{ textTransform: "none", whiteSpace: "nowrap" }}
            >
              {t("tool.askUserBack")}
            </Button>
          </Box>
        )}

        {/* Answered indicator for current question */}
        {isAnswered(a) && !a.showOther && (
          <Typography
            variant="caption"
            color="success.main"
            sx={{ mt: 0.5, display: "block", overflowWrap: "anywhere" }}
          >
            {a.selected}
          </Typography>
        )}

        {/* Submit all button */}
        <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 1.5 }}>
          <IconButton
            size="small"
            color="primary"
            disabled={!canSubmitAll}
            onClick={handleSubmitAll}
            aria-label={t("tool.askUserSubmit")}
            title={t("tool.askUserSubmit")}
            sx={{ width: 32, height: 32 }}
          >
            <SendRounded fontSize="small" />
          </IconButton>
        </Box>
      </Box>
    </Container>
  );
}
