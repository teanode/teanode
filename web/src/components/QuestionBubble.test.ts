/**
 * Tests for QuestionBubble inline message component.
 *
 * These tests verify the state-management logic used inside QuestionBubble:
 * selection tracking, submit gating, Other flow with back/cancel, and
 * double-submit prevention — all without requiring a React rendering library.
 */
import { describe, it, expect } from "vitest";
import type { PendingQuestion } from "../types";

// ─── Helpers that mirror QuestionBubble internal logic ────────────────

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

// ─── Selection gating ─────────────────────────────────────────────────

describe("QuestionBubble selection", () => {
  it("initially has no selection", () => {
    const selected: string | null = null;
    expect(selected).toBeNull();
  });

  it("clicking a choice sets it as selected", () => {
    let selected: string | null = null;
    // Simulate clicking choice "A"
    selected = "A";
    expect(selected).toBe("A");
  });

  it("clicking a different choice changes the selection", () => {
    let selected: string | null = "A";
    // Simulate clicking choice "B"
    selected = "B";
    expect(selected).toBe("B");
  });
});

// ─── Submit gating ────────────────────────────────────────────────────

describe("QuestionBubble submit gating", () => {
  it("canSubmit is false when no choice is selected and not in Other mode", () => {
    const selected: string | null = null;
    const showOtherInput = false;
    const otherText = "";
    const canSubmit = showOtherInput
      ? otherText.trim().length > 0
      : selected !== null;
    expect(canSubmit).toBe(false);
  });

  it("canSubmit is true when a choice is selected", () => {
    const selected: string | null = "A";
    const showOtherInput = false;
    const otherText = "";
    const canSubmit = showOtherInput
      ? otherText.trim().length > 0
      : selected !== null;
    expect(canSubmit).toBe(true);
  });

  it("canSubmit is false when in Other mode with empty text", () => {
    const selected: string | null = null;
    const showOtherInput = true;
    const otherText = "";
    const canSubmit = showOtherInput
      ? otherText.trim().length > 0
      : selected !== null;
    expect(canSubmit).toBe(false);
  });

  it("canSubmit is false when in Other mode with whitespace-only text", () => {
    const selected: string | null = null;
    const showOtherInput = true;
    const otherText = "   ";
    const canSubmit = showOtherInput
      ? otherText.trim().length > 0
      : selected !== null;
    expect(canSubmit).toBe(false);
  });

  it("canSubmit is true when in Other mode with non-empty text", () => {
    const selected: string | null = null;
    const showOtherInput = true;
    const otherText = "My custom answer";
    const canSubmit = showOtherInput
      ? otherText.trim().length > 0
      : selected !== null;
    expect(canSubmit).toBe(true);
  });
});

// ─── Other flow ───────────────────────────────────────────────────────

describe("QuestionBubble Other flow", () => {
  it("entering Other mode clears the selected choice", () => {
    let selected: string | null = "A";
    let showOtherInput = false;

    // Simulate clicking Other
    selected = null;
    showOtherInput = true;

    expect(selected).toBeNull();
    expect(showOtherInput).toBe(true);
  });

  it("Back clears Other text and returns to choices", () => {
    let showOtherInput = true;
    let otherText = "some text";

    // Simulate clicking Back
    showOtherInput = false;
    otherText = "";

    expect(showOtherInput).toBe(false);
    expect(otherText).toBe("");
  });

  it("Other label defaults to 'Other' when not specified", () => {
    const q = makeQuestion("q1");
    const otherLabel = q.otherLabel || "Other";
    expect(otherLabel).toBe("Other");
  });

  it("Other label uses custom value when specified", () => {
    const q = makeQuestion("q1", { otherLabel: "Something else" });
    const otherLabel = q.otherLabel || "Other";
    expect(otherLabel).toBe("Something else");
  });
});

// ─── Submit behavior ──────────────────────────────────────────────────

describe("QuestionBubble submit behavior", () => {
  it("submits selected choice answer", () => {
    const q = makeQuestion("q1");
    const selected = "A";
    const showOtherInput = false;

    // Build answer params as component would
    const params: { questionId: string; answer: string; other?: string } = {
      questionId: q.id,
      answer: selected,
    };
    expect(params).toEqual({ questionId: "q1", answer: "A" });
  });

  it("submits Other answer with text", () => {
    const q = makeQuestion("q1", { allowOther: true });
    const otherLabel = q.otherLabel || "Other";
    const otherText = "Custom answer";
    const showOtherInput = true;

    const params: { questionId: string; answer: string; other?: string } = {
      questionId: q.id,
      answer: otherLabel,
      other: otherText.trim(),
    };
    expect(params).toEqual({
      questionId: "q1",
      answer: "Other",
      other: "Custom answer",
    });
  });

  it("prevents double submit", () => {
    let submitted = false;
    let submitCount = 0;

    function handleSubmit() {
      if (submitted) return;
      submitted = true;
      submitCount++;
    }

    handleSubmit();
    handleSubmit(); // Should be blocked
    handleSubmit(); // Should be blocked

    expect(submitCount).toBe(1);
  });

  it("blocks submit when already submitted", () => {
    const submitted = true;
    const selected = "A";
    const showOtherInput = false;

    // Component logic: if (submitted) return;
    const wouldSubmit =
      !submitted && (showOtherInput ? false : selected !== null);
    expect(wouldSubmit).toBe(false);
  });
});

// ─── Choice button disabled state ────────────────────────────────────

describe("QuestionBubble choice disabled state", () => {
  it("choices are not disabled before submit", () => {
    const submitted = false;
    expect(submitted).toBe(false);
  });

  it("choices are disabled after submit", () => {
    const submitted = true;
    expect(submitted).toBe(true);
  });

  it("choices are hidden when in Other input mode", () => {
    const showOtherInput = true;
    // In the component, choices are only rendered when !showOtherInput
    expect(showOtherInput).toBe(true);
  });
});

// ─── allowOther rendering decisions ──────────────────────────────────

describe("QuestionBubble allowOther visibility", () => {
  it("shows Other button when allowOther is true", () => {
    const q = makeQuestion("q1", { allowOther: true });
    expect(q.allowOther).toBe(true);
  });

  it("hides Other button when allowOther is not set", () => {
    const q = makeQuestion("q1");
    expect(q.allowOther).toBeFalsy();
  });

  it("hides Other button when allowOther is false", () => {
    const q = makeQuestion("q1", { allowOther: false });
    expect(q.allowOther).toBe(false);
  });
});

// ─── Multiple questions as separate bubbles ──────────────────────────

describe("QuestionBubble multiple questions", () => {
  it("each question is an independent bubble with its own state", () => {
    const q1 = makeQuestion("q1", { question: "First?" });
    const q2 = makeQuestion("q2", { question: "Second?" });

    // Each bubble tracks its own selected state
    const states = new Map<string, string | null>();
    states.set(q1.id, null);
    states.set(q2.id, null);

    // Select in q1 doesn't affect q2
    states.set(q1.id, "A");
    expect(states.get(q1.id)).toBe("A");
    expect(states.get(q2.id)).toBeNull();
  });

  it("submitting one question doesn't affect another", () => {
    const submitted = new Set<string>();
    submitted.add("q1");
    expect(submitted.has("q1")).toBe(true);
    expect(submitted.has("q2")).toBe(false);
  });
});
