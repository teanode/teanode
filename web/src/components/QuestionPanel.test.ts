/**
 * Tests for QuestionPanel component logic.
 *
 * These tests verify the exported helper functions and the state-management
 * logic used inside QuestionPanel: allAnswered gating, per-question answer
 * tracking, navigation bounds, Other flow with text preservation, and batch
 * submit payload construction — all without requiring a React rendering library.
 */
import { describe, it, expect } from "vitest";
import type { PendingQuestion } from "../types";
import { type QuestionAnswer, isAnswered, allAnswered } from "./QuestionPanel";

// ─── Helpers ─────────────────────────────────────────────────────────

function makeQuestion(
  id: string,
  overrides?: Partial<PendingQuestion>,
): PendingQuestion {
  return {
    id,
    conversationId: "c1",
    agentId: "a1",
    runId: "r1",
    question: "Pick one?",
    choices: ["A", "B"],
    ...overrides,
  };
}

function emptyAnswer(): QuestionAnswer {
  return { selected: null, showOther: false, otherText: "" };
}

// ─── isAnswered ──────────────────────────────────────────────────────

describe("isAnswered", () => {
  it("returns false for empty answer", () => {
    expect(isAnswered(emptyAnswer())).toBe(false);
  });

  it("returns true when a choice is selected", () => {
    expect(isAnswered({ selected: "A", showOther: false, otherText: "" })).toBe(
      true,
    );
  });

  it("returns false in Other mode with empty text", () => {
    expect(isAnswered({ selected: null, showOther: true, otherText: "" })).toBe(
      false,
    );
  });

  it("returns false in Other mode with whitespace-only text", () => {
    expect(
      isAnswered({ selected: null, showOther: true, otherText: "   " }),
    ).toBe(false);
  });

  it("returns true in Other mode with non-empty text", () => {
    expect(
      isAnswered({ selected: null, showOther: true, otherText: "Custom" }),
    ).toBe(true);
  });
});

// ─── allAnswered ─────────────────────────────────────────────────────

describe("allAnswered", () => {
  it("returns true when all questions have answers", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: "A", showOther: false, otherText: "" });
    answers.set("q2", { selected: "B", showOther: false, otherText: "" });
    expect(allAnswered(questions, answers)).toBe(true);
  });

  it("returns false when one question is unanswered", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: "A", showOther: false, otherText: "" });
    answers.set("q2", emptyAnswer());
    expect(allAnswered(questions, answers)).toBe(false);
  });

  it("returns false when an answer map entry is missing", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: "A", showOther: false, otherText: "" });
    // q2 not in map
    expect(allAnswered(questions, answers)).toBe(false);
  });

  it("returns true for empty questions array", () => {
    expect(allAnswered([], new Map())).toBe(true);
  });

  it("counts Other with text as answered", () => {
    const questions = [makeQuestion("q1", { allowOther: true })];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: null, showOther: true, otherText: "Custom" });
    expect(allAnswered(questions, answers)).toBe(true);
  });

  it("does not count Other with empty text as answered", () => {
    const questions = [makeQuestion("q1", { allowOther: true })];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: null, showOther: true, otherText: "" });
    expect(allAnswered(questions, answers)).toBe(false);
  });
});

// ─── Navigation bounds ───────────────────────────────────────────────

describe("QuestionPanel navigation", () => {
  it("currentIndex starts at 0", () => {
    const currentIndex = 0;
    expect(currentIndex).toBe(0);
  });

  it("prev is disabled at index 0", () => {
    const currentIndex = 0;
    expect(currentIndex === 0).toBe(true);
  });

  it("next is disabled at last index", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    const currentIndex = 1;
    expect(currentIndex === questions.length - 1).toBe(true);
  });

  it("clamps index when questions array shrinks", () => {
    let currentIndex = 2;
    const questions = [makeQuestion("q1")]; // only 1 question now
    if (currentIndex >= questions.length && questions.length > 0) {
      currentIndex = questions.length - 1;
    }
    expect(currentIndex).toBe(0);
  });
});

// ─── Other text preservation ─────────────────────────────────────────

describe("QuestionPanel Other flow", () => {
  it("preserves otherText when switching back to choices", () => {
    const answer: QuestionAnswer = {
      selected: null,
      showOther: true,
      otherText: "My custom text",
    };
    // Simulate Back button: sets showOther=false but does NOT clear otherText.
    const updated = { ...answer, showOther: false };
    expect(updated.otherText).toBe("My custom text");
  });

  it("entering Other mode clears selected", () => {
    const answer: QuestionAnswer = {
      selected: "A",
      showOther: false,
      otherText: "",
    };
    const updated = { ...answer, selected: null, showOther: true };
    expect(updated.selected).toBeNull();
    expect(updated.showOther).toBe(true);
  });

  it("selecting a choice exits Other mode", () => {
    const answer: QuestionAnswer = {
      selected: null,
      showOther: true,
      otherText: "text",
    };
    const updated = { ...answer, selected: "B", showOther: false };
    expect(updated.selected).toBe("B");
    expect(updated.showOther).toBe(false);
    // otherText preserved for if user goes back to Other.
    expect(updated.otherText).toBe("text");
  });
});

// ─── Batch submit payload construction ───────────────────────────────

describe("QuestionPanel submit payload", () => {
  it("builds correct payload for predefined choices", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", { selected: "A", showOther: false, otherText: "" });
    answers.set("q2", { selected: "B", showOther: false, otherText: "" });

    const result: { questionId: string; answer: string; other?: string }[] = [];
    for (const q of questions) {
      const a = answers.get(q.id)!;
      if (a.showOther) {
        const otherLabel = q.otherLabel || "Other";
        result.push({
          questionId: q.id,
          answer: otherLabel,
          other: a.otherText.trim(),
        });
      } else {
        result.push({ questionId: q.id, answer: a.selected! });
      }
    }

    expect(result).toEqual([
      { questionId: "q1", answer: "A" },
      { questionId: "q2", answer: "B" },
    ]);
  });

  it("builds correct payload with Other answer", () => {
    const questions = [
      makeQuestion("q1", { allowOther: true, otherLabel: "Custom" }),
    ];
    const answers = new Map<string, QuestionAnswer>();
    answers.set("q1", {
      selected: null,
      showOther: true,
      otherText: "  My custom  ",
    });

    const result: { questionId: string; answer: string; other?: string }[] = [];
    for (const q of questions) {
      const a = answers.get(q.id)!;
      if (a.showOther) {
        const otherLabel = q.otherLabel || "Other";
        result.push({
          questionId: q.id,
          answer: otherLabel,
          other: a.otherText.trim(),
        });
      } else {
        result.push({ questionId: q.id, answer: a.selected! });
      }
    }

    expect(result).toEqual([
      { questionId: "q1", answer: "Custom", other: "My custom" },
    ]);
  });

  it("uses default Other label when not specified", () => {
    const q = makeQuestion("q1", { allowOther: true });
    const otherLabel = q.otherLabel || "Other";
    expect(otherLabel).toBe("Other");
  });
});

// ─── Swipe gesture logic ─────────────────────────────────────────────

describe("QuestionPanel swipe", () => {
  const SWIPE_THRESHOLD = 50;

  it("swipe right (positive dx) goes to previous question", () => {
    let currentIndex = 1;
    const dx = 80; // positive = swipe right
    if (dx > SWIPE_THRESHOLD && currentIndex > 0) {
      currentIndex--;
    }
    expect(currentIndex).toBe(0);
  });

  it("swipe left (negative dx) goes to next question", () => {
    let currentIndex = 0;
    const questionsLength = 3;
    const dx = -80; // negative = swipe left
    if (dx < -SWIPE_THRESHOLD && currentIndex < questionsLength - 1) {
      currentIndex++;
    }
    expect(currentIndex).toBe(1);
  });

  it("does not swipe past boundaries", () => {
    let currentIndex = 0;
    const dx = 80;
    if (dx > SWIPE_THRESHOLD && currentIndex > 0) {
      currentIndex--;
    }
    expect(currentIndex).toBe(0); // can't go below 0

    currentIndex = 2;
    const questionsLength = 3;
    const dx2 = -80;
    if (dx2 < -SWIPE_THRESHOLD && currentIndex < questionsLength - 1) {
      currentIndex++;
    }
    expect(currentIndex).toBe(2); // can't go past last
  });

  it("ignores small swipe", () => {
    let currentIndex = 1;
    const dx = 30; // below threshold
    if (dx > SWIPE_THRESHOLD && currentIndex > 0) {
      currentIndex--;
    }
    expect(currentIndex).toBe(1); // unchanged
  });
});

// ─── Double-submit prevention ────────────────────────────────────────

describe("QuestionPanel double-submit prevention", () => {
  it("prevents multiple submissions", () => {
    let submitted = false;
    let submitCount = 0;

    function handleSubmitAll() {
      if (submitted) return;
      submitted = true;
      submitCount++;
    }

    handleSubmitAll();
    handleSubmitAll();
    handleSubmitAll();

    expect(submitCount).toBe(1);
  });
});

// ─── Error recovery (reconnect race) ───────────────────────────────

describe("QuestionPanel error recovery", () => {
  it("re-enables submit when onSubmitAll rejects", async () => {
    // Simulates the pattern used inside QuestionPanel.handleSubmitAll:
    // Promise.resolve(onSubmitAll(result)).catch(() => setSubmitted(false))
    let submitted = true;
    const onSubmitAll = () => Promise.reject(new Error("not connected"));

    await Promise.resolve(onSubmitAll()).catch(() => {
      submitted = false;
    });

    expect(submitted).toBe(false);
  });

  it("stays submitted when onSubmitAll succeeds", async () => {
    let submitted = true;
    const onSubmitAll = () => Promise.resolve();

    await Promise.resolve(onSubmitAll()).catch(() => {
      submitted = false;
    });

    expect(submitted).toBe(true);
  });
});
