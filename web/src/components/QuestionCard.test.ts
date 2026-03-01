/**
 * Tests for ask_user_question Other-option logic.
 *
 * Since there's no React testing library available, these tests verify the
 * data-flow logic: ToolResult display formatting, ToolInvoke allowOther
 * rendering decisions, and PendingQuestion event construction.
 */
import { describe, it, expect } from "vitest";
import type { PendingQuestion, ConversationQuestionsEvent } from "../types";

// ─── ToolResult display logic (mirrors ToolResult.tsx) ───────────────

function formatToolResultAnswer(content: string): string | null {
  try {
    const parsed = JSON.parse(content);
    if (!parsed.answer) return null;
    return parsed.other
      ? `${parsed.answer}: ${parsed.other}`
      : parsed.answer;
  } catch {
    return null;
  }
}

describe("ToolResult answer display", () => {
  it("formats a normal choice answer", () => {
    const result = formatToolResultAnswer('{"answer":"PostgreSQL"}');
    expect(result).toBe("PostgreSQL");
  });

  it("formats an Other answer with freeform text", () => {
    const result = formatToolResultAnswer(
      '{"answer":"Other","other":"MongoDB with sharding"}'
    );
    expect(result).toBe("Other: MongoDB with sharding");
  });

  it("formats a custom Other label with text", () => {
    const result = formatToolResultAnswer(
      '{"answer":"Custom","other":"My custom DB"}'
    );
    expect(result).toBe("Custom: My custom DB");
  });

  it("returns null for invalid JSON", () => {
    expect(formatToolResultAnswer("not json")).toBeNull();
  });

  it("returns null when answer is missing", () => {
    expect(formatToolResultAnswer('{"error":"something"}')).toBeNull();
  });

  it("does not include other key for normal choice", () => {
    // Backend omits "other" key when answer is a normal choice
    const result = formatToolResultAnswer('{"answer":"SQLite"}');
    expect(result).toBe("SQLite");
    expect(result).not.toContain(":");
  });
});

// ─── ToolInvoke choices display (mirrors ToolInvoke.tsx) ─────────────

function formatToolInvokeChoices(args: string): string | null {
  try {
    const parsed = JSON.parse(args);
    if (!parsed.choices) return null;
    let text = parsed.choices.join(", ");
    if (parsed.allowOther) {
      text += `, ${parsed.otherLabel || "Other"}`;
    }
    return text;
  } catch {
    return null;
  }
}

describe("ToolInvoke choices display", () => {
  it("lists choices without Other when allowOther is not set", () => {
    const result = formatToolInvokeChoices(
      '{"question":"Pick","choices":["A","B"]}'
    );
    expect(result).toBe("A, B");
  });

  it("appends default Other label when allowOther is true", () => {
    const result = formatToolInvokeChoices(
      '{"question":"Pick","choices":["A","B"],"allowOther":true}'
    );
    expect(result).toBe("A, B, Other");
  });

  it("appends custom Other label", () => {
    const result = formatToolInvokeChoices(
      '{"question":"Pick","choices":["A","B"],"allowOther":true,"otherLabel":"Custom"}'
    );
    expect(result).toBe("A, B, Custom");
  });

  it("does not append Other when allowOther is false", () => {
    const result = formatToolInvokeChoices(
      '{"question":"Pick","choices":["A","B"],"allowOther":false}'
    );
    expect(result).toBe("A, B");
  });
});

// ─── PendingQuestion construction from event ─────────────────────────

function buildPendingQuestion(
  payload: ConversationQuestionsEvent,
): PendingQuestion {
  const q: PendingQuestion = {
    id: payload.questionId,
    conversationId: payload.conversationId!,
    agentId: payload.agentId || "",
    runId: payload.runId || "",
    question: payload.question || "",
    choices: payload.choices || [],
  };
  if (payload.allowOther) {
    q.allowOther = true;
    if (payload.otherLabel) q.otherLabel = payload.otherLabel;
    if (payload.otherPlaceholder) q.otherPlaceholder = payload.otherPlaceholder;
  }
  return q;
}

describe("PendingQuestion from event", () => {
  it("builds a basic question without Other fields", () => {
    const q = buildPendingQuestion({
      action: "asked",
      questionId: "q1",
      conversationId: "c1",
      question: "Pick",
      choices: ["A", "B"],
    });
    expect(q.allowOther).toBeUndefined();
    expect(q.otherLabel).toBeUndefined();
    expect(q.otherPlaceholder).toBeUndefined();
  });

  it("builds a question with allowOther and labels", () => {
    const q = buildPendingQuestion({
      action: "asked",
      questionId: "q1",
      conversationId: "c1",
      question: "Pick",
      choices: ["A", "B"],
      allowOther: true,
      otherLabel: "Something else",
      otherPlaceholder: "Describe it",
    });
    expect(q.allowOther).toBe(true);
    expect(q.otherLabel).toBe("Something else");
    expect(q.otherPlaceholder).toBe("Describe it");
  });

  it("builds a question with allowOther but no custom labels", () => {
    const q = buildPendingQuestion({
      action: "asked",
      questionId: "q1",
      conversationId: "c1",
      question: "Pick",
      choices: ["A", "B"],
      allowOther: true,
    });
    expect(q.allowOther).toBe(true);
    expect(q.otherLabel).toBeUndefined();
    expect(q.otherPlaceholder).toBeUndefined();
  });

  it("ignores Other fields when allowOther is false", () => {
    const q = buildPendingQuestion({
      action: "asked",
      questionId: "q1",
      conversationId: "c1",
      question: "Pick",
      choices: ["A", "B"],
      allowOther: false,
      otherLabel: "Custom",
    });
    expect(q.allowOther).toBeUndefined();
    expect(q.otherLabel).toBeUndefined();
  });
});

// ─── RPC answer params construction ──────────────────────────────────

function buildAnswerParams(
  questionId: string,
  answer: string,
  other?: string,
): Record<string, string> {
  const params: Record<string, string> = { questionId, answer };
  if (other) params.other = other;
  return params;
}

describe("RPC answer params", () => {
  it("builds params for a normal choice", () => {
    const params = buildAnswerParams("q1", "PostgreSQL");
    expect(params).toEqual({ questionId: "q1", answer: "PostgreSQL" });
    expect("other" in params).toBe(false);
  });

  it("builds params for an Other choice", () => {
    const params = buildAnswerParams("q1", "Other", "MongoDB");
    expect(params).toEqual({
      questionId: "q1",
      answer: "Other",
      other: "MongoDB",
    });
  });

  it("omits other when empty string", () => {
    const params = buildAnswerParams("q1", "Other", "");
    expect("other" in params).toBe(false);
  });
});
