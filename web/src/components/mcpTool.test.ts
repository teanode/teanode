import { describe, it, expect } from "vitest";
import { parseMcpToolName } from "./mcpTool";

describe("parseMcpToolName", () => {
  it("parses a namespaced MCP tool name", () => {
    expect(parseMcpToolName("mcp__robinhood__get_quote")).toEqual({
      server: "robinhood",
      tool: "get_quote",
    });
  });

  it("preserves separators inside the tool name", () => {
    expect(parseMcpToolName("mcp__server__weird__tool")).toEqual({
      server: "server",
      tool: "weird__tool",
    });
  });

  it("returns null for non-MCP tools", () => {
    expect(parseMcpToolName("shell")).toBeNull();
    expect(parseMcpToolName("web_search")).toBeNull();
  });

  it("returns null for malformed MCP names", () => {
    expect(parseMcpToolName("mcp__")).toBeNull();
    expect(parseMcpToolName("mcp__server__")).toBeNull();
    expect(parseMcpToolName("mcp____tool")).toBeNull();
    expect(parseMcpToolName("mcp__onlyserver")).toBeNull();
  });
});
