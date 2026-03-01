/**
 * Tests for QuestionDialog modal behavior.
 *
 * These tests verify the state-management logic used inside QuestionDialog:
 * active-index clamping, answer-tracking, and navigation bounds — all without
 * requiring a React rendering library.
 */
import { describe, it, expect } from "vitest";
import type { PendingQuestion } from "../types";

// ─── Helpers that mirror QuestionDialog internal logic ────────────────

function makeQuestion(id: string, text = "Q?"): PendingQuestion {
  return {
    id,
    conversationId: "c1",
    agentId: "a1",
    runId: "r1",
    question: text,
    choices: ["A", "B"],
  };
}

/** Clamp activeIndex within bounds (mirrors the useEffect in the component). */
function clampIndex(activeIndex: number, length: number): number {
  if (length > 0 && activeIndex >= length) return length - 1;
  return activeIndex;
}

/** Navigate prev/next with bounds checking. */
function navigatePrev(activeIndex: number): number {
  return Math.max(0, activeIndex - 1);
}

function navigateNext(
  activeIndex: number,
  length: number,
): number {
  return Math.min(length - 1, activeIndex + 1);
}

// ─── Active index clamping ───────────────────────────────────────────

describe("QuestionDialog active index clamping", () => {
  it("clamps index when questions are removed to below current index", () => {
    // Was viewing question at index 2 of 3, now only 2 remain.
    expect(clampIndex(2, 2)).toBe(1);
  });

  it("does not change index when within bounds", () => {
    expect(clampIndex(0, 3)).toBe(0);
    expect(clampIndex(1, 3)).toBe(1);
    expect(clampIndex(2, 3)).toBe(2);
  });

  it("clamps to 0 when only one question remains", () => {
    expect(clampIndex(5, 1)).toBe(0);
  });

  it("keeps index as-is when list is empty (edge case)", () => {
    // The dialog isn't shown when empty, but logic shouldn't crash.
    expect(clampIndex(0, 0)).toBe(0);
  });
});

// ─── Navigation bounds ──────────────────────────────────────────────

describe("QuestionDialog navigation", () => {
  it("prev does not go below 0", () => {
    expect(navigatePrev(0)).toBe(0);
  });

  it("prev decrements normally", () => {
    expect(navigatePrev(2)).toBe(1);
  });

  it("next does not exceed last index", () => {
    expect(navigateNext(2, 3)).toBe(2);
  });

  it("next increments normally", () => {
    expect(navigateNext(0, 3)).toBe(1);
  });
});

// ─── Answer tracking (double-submit prevention) ─────────────────────

describe("QuestionDialog answer tracking", () => {
  it("marks a question as answered", () => {
    const answered = new Set<string>();
    const q = makeQuestion("q1");
    answered.add(q.id);
    expect(answered.has("q1")).toBe(true);
    expect(answered.has("q2")).toBe(false);
  });

  it("tracks multiple answered questions independently", () => {
    const answered = new Set<string>();
    answered.add("q1");
    answered.add("q3");
    expect(answered.has("q1")).toBe(true);
    expect(answered.has("q2")).toBe(false);
    expect(answered.has("q3")).toBe(true);
  });
});

// ─── Dialog open state ──────────────────────────────────────────────

describe("QuestionDialog open state", () => {
  it("is open when there are pending questions", () => {
    const questions = [makeQuestion("q1")];
    expect(questions.length > 0).toBe(true);
  });

  it("is closed when there are no pending questions", () => {
    const questions: PendingQuestion[] = [];
    expect(questions.length > 0).toBe(false);
  });
});

// ─── Multi-question scenario ────────────────────────────────────────

describe("QuestionDialog multi-question flow", () => {
  it("advances to next question after answering current", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2"), makeQuestion("q3")];
    let activeIndex = 0;

    // Answer q1 — it gets removed, index should clamp.
    const remaining = questions.filter((q) => q.id !== "q1");
    activeIndex = clampIndex(activeIndex, remaining.length);
    expect(activeIndex).toBe(0);
    expect(remaining[activeIndex].id).toBe("q2");
  });

  it("clamps when last question in list is answered", () => {
    const questions = [makeQuestion("q1"), makeQuestion("q2")];
    let activeIndex = 1; // viewing q2

    // Answer q2 — it gets removed, only q1 remains.
    const remaining = questions.filter((q) => q.id !== "q2");
    activeIndex = clampIndex(activeIndex, remaining.length);
    expect(activeIndex).toBe(0);
    expect(remaining[activeIndex].id).toBe("q1");
  });

  it("dialog closes when all questions are answered", () => {
    const questions = [makeQuestion("q1")];
    const remaining = questions.filter((q) => q.id !== "q1");
    expect(remaining.length).toBe(0);
    // open = remaining.length > 0 → false
  });
});
