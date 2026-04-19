import { describe, expect, it } from "vitest";
import {
  parseSuggestionMarker,
  hidePartialSuggestionMarker,
} from "./suggestions";

describe("parseSuggestionMarker", () => {
  it("returns original text when no marker present", () => {
    const result = parseSuggestionMarker("Hello, how can I help you?");
    expect(result).toEqual({
      displayText: "Hello, how can I help you?",
      suggestions: [],
    });
  });

  it("extracts valid marker at end", () => {
    const result = parseSuggestionMarker(
      'Which do you prefer?\n<!--suggestions:["Option A","Option B","Option C"]-->',
    );
    expect(result).toEqual({
      displayText: "Which do you prefer?\n",
      suggestions: ["Option A", "Option B", "Option C"],
    });
  });

  it("extracts marker in middle of text", () => {
    const result = parseSuggestionMarker(
      'Before <!--suggestions:["Yes","No"]--> after',
    );
    expect(result).toEqual({
      displayText: "Before  after",
      suggestions: ["Yes", "No"],
    });
  });

  it("extracts six options", () => {
    const result = parseSuggestionMarker(
      'Pick one:\n<!--suggestions:["A","B","C","D","E","F"]-->',
    );
    expect(result.suggestions).toEqual(["A", "B", "C", "D", "E", "F"]);
    expect(result.displayText).toBe("Pick one:\n");
  });

  it("rejects too few options (1)", () => {
    const input = '<!--suggestions:["Solo"]-->';
    expect(parseSuggestionMarker(input).suggestions).toEqual([]);
    expect(parseSuggestionMarker(input).displayText).toBe(input);
  });

  it("rejects too many options (7)", () => {
    const input = '<!--suggestions:["A","B","C","D","E","F","G"]-->';
    expect(parseSuggestionMarker(input).suggestions).toEqual([]);
  });

  it("rejects empty option strings", () => {
    const input = '<!--suggestions:["Good",""]-->';
    expect(parseSuggestionMarker(input).suggestions).toEqual([]);
  });

  it("rejects malformed JSON", () => {
    const input = "<!--suggestions:[not valid]-->";
    expect(parseSuggestionMarker(input).suggestions).toEqual([]);
  });

  it("handles marker-only text", () => {
    const result = parseSuggestionMarker('<!--suggestions:["Yes","No"]-->');
    expect(result).toEqual({
      displayText: "",
      suggestions: ["Yes", "No"],
    });
  });

  it("handles unicode options", () => {
    const result = parseSuggestionMarker(
      '<!--suggestions:["Oui","Non","Peut-\u00eatre"]-->',
    );
    expect(result.suggestions).toEqual(["Oui", "Non", "Peut-\u00eatre"]);
  });
});

describe("hidePartialSuggestionMarker", () => {
  it("returns text unchanged when no partial marker", () => {
    expect(hidePartialSuggestionMarker("Hello world")).toBe("Hello world");
  });

  it("hides partial <!--", () => {
    expect(hidePartialSuggestionMarker("Hello <!--")).toBe("Hello ");
  });

  it("hides partial <!--suggest", () => {
    expect(hidePartialSuggestionMarker("Hello <!--suggest")).toBe("Hello ");
  });

  it("hides partial <!--suggestions:", () => {
    expect(hidePartialSuggestionMarker("Hello <!--suggestions:")).toBe(
      "Hello ",
    );
  });

  it("hides partial marker body", () => {
    expect(
      hidePartialSuggestionMarker('Hello <!--suggestions:["Yes","No"'),
    ).toBe("Hello ");
  });

  it("does not hide complete marker (already terminated)", () => {
    const text = 'Hello <!--suggestions:["Yes","No"]-->';
    expect(hidePartialSuggestionMarker(text)).toBe(text);
  });

  it("does not hide unrelated HTML comments", () => {
    // An unterminated "<!--" that does NOT look like a suggestion marker
    expect(hidePartialSuggestionMarker("Hello <!-- other stuff")).toBe(
      "Hello <!-- other stuff",
    );
  });

  it("handles text with no angle brackets", () => {
    expect(hidePartialSuggestionMarker("Just plain text")).toBe(
      "Just plain text",
    );
  });

  it("handles empty string", () => {
    expect(hidePartialSuggestionMarker("")).toBe("");
  });
});
