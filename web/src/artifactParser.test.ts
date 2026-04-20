import { describe, expect, it } from "vitest";
import {
  hasFencedBlocks,
  parseArtifacts,
  parseArtifactsStreaming,
} from "./artifactParser";

describe("hasFencedBlocks", () => {
  it("detects artifact blocks", () => {
    expect(hasFencedBlocks(':::artifact{title="Test"}\ncontent\n:::')).toBe(
      true,
    );
  });

  it("detects colon chart blocks", () => {
    expect(hasFencedBlocks(':::chart{title="Test"}\n{}\n:::')).toBe(true);
  });

  it("detects backtick chart blocks", () => {
    expect(hasFencedBlocks("```chart\n{}\n```")).toBe(true);
    expect(hasFencedBlocks('```chart title="Test"\n{}\n```')).toBe(true);
  });

  it("returns false for plain text", () => {
    expect(hasFencedBlocks("Hello world")).toBe(false);
  });
});

describe("parseArtifacts with chart blocks", () => {
  it("parses a chart block", () => {
    const text =
      'Here is a chart:\n:::chart{title="Revenue"}\n{"chartType":"line","series":[{"name":"A","data":[1]}]}\n:::\nDone.';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(3);
    expect(segments[0]).toEqual({ kind: "text", content: "Here is a chart:" });
    expect(segments[1]).toEqual({
      kind: "chart",
      index: 0,
      title: "Revenue",
      content: '{"chartType":"line","series":[{"name":"A","data":[1]}]}',
    });
    expect(segments[2]).toEqual({ kind: "text", content: "Done." });
  });

  it("parses mixed artifact and chart blocks", () => {
    const text =
      ':::artifact{title="Plan"}\nSome plan\n:::\n:::chart{title="Data"}\n{"chartType":"bar","series":[{"name":"X","data":[1]}]}\n:::';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(2);
    expect(segments[0].kind).toBe("artifact");
    expect(segments[1].kind).toBe("chart");
  });

  it("handles unclosed chart block as plain text", () => {
    const text = ':::chart{title="Incomplete"}\n{"chartType":"line"}';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(1);
    expect(segments[0].kind).toBe("text");
    expect(segments[0].content).toContain(":::chart{");
  });
});

describe("parseArtifacts with backtick chart blocks", () => {
  it("parses a backtick chart block with title", () => {
    const text =
      'Before\n```chart title="Revenue"\n{"chartType":"line","series":[{"name":"A","data":[1]}]}\n```\nAfter';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(3);
    expect(segments[0]).toEqual({ kind: "text", content: "Before" });
    expect(segments[1]).toEqual({
      kind: "chart",
      index: 0,
      title: "Revenue",
      content: '{"chartType":"line","series":[{"name":"A","data":[1]}]}',
    });
    expect(segments[2]).toEqual({ kind: "text", content: "After" });
  });

  it("parses a backtick chart block without title", () => {
    const text =
      '```chart\n{"chartType":"bar","series":[{"name":"X","data":[1]}]}\n```';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(1);
    expect(segments[0]).toEqual({
      kind: "chart",
      index: 0,
      title: "",
      content: '{"chartType":"bar","series":[{"name":"X","data":[1]}]}',
    });
  });

  it("handles unclosed backtick chart block as plain text", () => {
    const text = '```chart title="Incomplete"\n{"chartType":"line"}';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(1);
    expect(segments[0].kind).toBe("text");
    expect(segments[0].content).toContain("```chart");
  });

  it("parses mixed colon and backtick chart blocks", () => {
    const text =
      ':::chart{title="Colon"}\n{"chartType":"line","series":[{"name":"A","data":[1]}]}\n:::\n```chart title="Backtick"\n{"chartType":"bar","series":[{"name":"B","data":[2]}]}\n```';
    const segments = parseArtifacts(text);
    expect(segments).toHaveLength(2);
    expect(segments[0]).toMatchObject({ kind: "chart", title: "Colon" });
    expect(segments[1]).toMatchObject({ kind: "chart", title: "Backtick" });
  });
});

describe("parseArtifactsStreaming with chart blocks", () => {
  it("returns pending chart for unclosed chart block", () => {
    const text = 'Text\n:::chart{title="Revenue"}\n{"chartType":"line"';
    const result = parseArtifactsStreaming(text);
    expect(result.pendingChart).not.toBeNull();
    expect(result.pendingChart?.title).toBe("Revenue");
    expect(result.pendingArtifact).toBeNull();
    expect(result.segments).toHaveLength(1);
    expect(result.segments[0]).toEqual({ kind: "text", content: "Text" });
  });

  it("returns pending artifact for unclosed artifact block", () => {
    const text = ':::artifact{title="Plan"}\nSome plan';
    const result = parseArtifactsStreaming(text);
    expect(result.pendingArtifact).not.toBeNull();
    expect(result.pendingChart).toBeNull();
  });

  it("returns completed chart with no pending", () => {
    const text =
      ':::chart{title="Done"}\n{"chartType":"pie","series":[{"name":"A","data":[1]}]}\n:::';
    const result = parseArtifactsStreaming(text);
    expect(result.pendingChart).toBeNull();
    expect(result.segments).toHaveLength(1);
    expect(result.segments[0].kind).toBe("chart");
  });

  it("returns pending chart for unclosed backtick chart block", () => {
    const text = 'Text\n```chart title="Revenue"\n{"chartType":"line"';
    const result = parseArtifactsStreaming(text);
    expect(result.pendingChart).not.toBeNull();
    expect(result.pendingChart?.title).toBe("Revenue");
    expect(result.pendingArtifact).toBeNull();
    expect(result.segments).toHaveLength(1);
    expect(result.segments[0]).toEqual({ kind: "text", content: "Text" });
  });

  it("returns completed backtick chart with no pending", () => {
    const text =
      '```chart title="Done"\n{"chartType":"pie","series":[{"name":"A","data":[1]}]}\n```';
    const result = parseArtifactsStreaming(text);
    expect(result.pendingChart).toBeNull();
    expect(result.segments).toHaveLength(1);
    expect(result.segments[0].kind).toBe("chart");
  });
});
