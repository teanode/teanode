import { describe, expect, it } from "vitest";
import { normalizeContent } from "./contentUtils";

describe("normalizeContent", () => {
  it("returns empty text for null/undefined", () => {
    expect(normalizeContent(null)).toEqual({ text: "" });
    expect(normalizeContent(undefined)).toEqual({ text: "" });
    expect(normalizeContent("")).toEqual({ text: "" });
  });

  it("handles a plain string", () => {
    expect(normalizeContent("Hello world")).toEqual({ text: "Hello world" });
  });

  it("handles a JSON-encoded string", () => {
    expect(normalizeContent('"Hello world"')).toEqual({ text: "Hello world" });
  });

  it("extracts text from a parsed array of content blocks", () => {
    const blocks = [
      { type: "text", text: "Hello " },
      { type: "text", text: "world" },
    ];
    expect(normalizeContent(blocks)).toEqual({ text: "Hello world" });
  });

  it("extracts text + attachments from a parsed array of content blocks", () => {
    const blocks = [
      { type: "text", text: "See image:" },
      {
        type: "attachment",
        mediaId: "m1",
        format: "png",
        filename: "pic.png",
      },
    ];
    const result = normalizeContent(blocks);
    expect(result.text).toBe("See image:");
    expect(result.attachments).toEqual([
      { mediaId: "m1", format: "png", filename: "pic.png" },
    ]);
  });

  it("extracts text from a JSON string containing content blocks", () => {
    const json = JSON.stringify([
      { type: "text", text: "Hello" },
      {
        type: "attachment",
        mediaId: "m2",
        format: "jpg",
        filename: "photo.jpg",
      },
    ]);
    const result = normalizeContent(json);
    expect(result.text).toBe("Hello");
    expect(result.attachments).toEqual([
      { mediaId: "m2", format: "jpg", filename: "photo.jpg" },
    ]);
  });

  it("returns the original string for non-block JSON objects", () => {
    const json = '{"mediaId":"m1"}';
    expect(normalizeContent(json)).toEqual({ text: json });
  });

  it("stringifies non-string non-array values", () => {
    expect(normalizeContent(42)).toEqual({ text: "42" });
    expect(normalizeContent({ foo: "bar" })).toEqual({
      text: '{"foo":"bar"}',
    });
  });

  it("handles blocks with only attachments (no text blocks)", () => {
    const blocks = [
      {
        type: "attachment",
        mediaId: "m3",
        format: "pdf",
        filename: "doc.pdf",
      },
    ];
    const result = normalizeContent(blocks);
    expect(result.text).toBe("");
    expect(result.attachments).toEqual([
      { mediaId: "m3", format: "pdf", filename: "doc.pdf" },
    ]);
  });

  it("ignores unknown block types", () => {
    const blocks = [
      { type: "text", text: "Hi" },
      { type: "tool_use", id: "t1" },
    ];
    const result = normalizeContent(blocks);
    expect(result.text).toBe("Hi");
    expect(result.attachments).toBeUndefined();
  });
});
