export interface TextSegment {
  kind: "text";
  content: string;
}

export interface ArtifactSegment {
  kind: "artifact";
  index: number;
  title: string;
  content: string;
}

export interface ChartSegment {
  kind: "chart";
  index: number;
  title: string;
  content: string;
}

export type MessageSegment = TextSegment | ArtifactSegment | ChartSegment;

export interface StreamingParseResult {
  segments: MessageSegment[];
  pendingArtifact: {
    index: number;
    title: string;
    content: string;
  } | null;
  pendingChart: {
    index: number;
    title: string;
    content: string;
  } | null;
}

const ARTIFACT_OPEN_RE = /^:::artifact\{title="([^"]+)"\}\s*$/;
const CHART_OPEN_RE = /^:::chart\{title="([^"]+)"\}\s*$/;
const FENCE_CLOSE_RE = /^:::\s*$/;
const CODE_FENCE_RE = /^(`{3,})/;

/** Quick check — avoids full parsing when no fenced blocks are present. */
export function hasFencedBlocks(text: string): boolean {
  return text.includes(":::artifact{") || text.includes(":::chart{");
}

/** @deprecated Use hasFencedBlocks instead. */
export function hasArtifacts(text: string): boolean {
  return hasFencedBlocks(text);
}

/**
 * Parse completed message text into segments of plain text, artifacts, and charts.
 * Code fences (```) are tracked so that `:::` inside a fenced code block
 * is not mistaken for a block boundary.
 */
export function parseArtifacts(text: string): MessageSegment[] {
  if (!hasFencedBlocks(text)) {
    return [{ kind: "text", content: text }];
  }

  const lines = text.split("\n");
  const segments: MessageSegment[] = [];
  let artifactIndex = 0;
  let chartIndex = 0;
  let plainLines: string[] = [];
  let blockTitle = "";
  let blockLines: string[] = [];
  let insideBlock: "artifact" | "chart" | false = false;
  let codeFenceDepth = 0;

  for (const line of lines) {
    if (insideBlock) {
      // Track code fences inside the block body.
      const fenceMatch = line.match(CODE_FENCE_RE);
      if (fenceMatch) {
        codeFenceDepth = codeFenceDepth === 0 ? fenceMatch[1].length : 0;
      }

      if (codeFenceDepth === 0 && FENCE_CLOSE_RE.test(line)) {
        // Close the block.
        if (insideBlock === "artifact") {
          segments.push({
            kind: "artifact",
            index: artifactIndex,
            title: blockTitle,
            content: blockLines.join("\n"),
          });
          artifactIndex++;
        } else {
          segments.push({
            kind: "chart",
            index: chartIndex,
            title: blockTitle,
            content: blockLines.join("\n"),
          });
          chartIndex++;
        }
        insideBlock = false;
        codeFenceDepth = 0;
        blockLines = [];
        blockTitle = "";
      } else {
        blockLines.push(line);
      }
    } else {
      const artifactMatch = line.match(ARTIFACT_OPEN_RE);
      const chartMatch = !artifactMatch ? line.match(CHART_OPEN_RE) : null;
      if (artifactMatch || chartMatch) {
        // Flush accumulated plain text.
        if (plainLines.length > 0) {
          segments.push({ kind: "text", content: plainLines.join("\n") });
          plainLines = [];
        }
        insideBlock = artifactMatch ? "artifact" : "chart";
        codeFenceDepth = 0;
        blockTitle = (artifactMatch || chartMatch)![1];
      } else {
        plainLines.push(line);
      }
    }
  }

  // Flush remaining content. If we're still inside an unclosed block,
  // treat the opener + body as plain text (incomplete block in committed
  // content is unlikely but handled gracefully).
  if (insideBlock) {
    const tag = insideBlock === "artifact" ? "artifact" : "chart";
    plainLines.push(`:::${tag}{title="${blockTitle}"}`);
    plainLines.push(...blockLines);
  }
  if (plainLines.length > 0) {
    segments.push({ kind: "text", content: plainLines.join("\n") });
  }

  return segments;
}

/**
 * Streaming-safe variant. If the text ends with an unclosed artifact or chart
 * fence, it is returned as `pendingArtifact`/`pendingChart` rather than being
 * folded into plain text. A partial opening fence at the very end (e.g.
 * `:::artif`) is kept as trailing plain text to avoid premature detection.
 */
export function parseArtifactsStreaming(text: string): StreamingParseResult {
  if (!hasFencedBlocks(text)) {
    return {
      segments: [{ kind: "text", content: text }],
      pendingArtifact: null,
      pendingChart: null,
    };
  }

  const lines = text.split("\n");
  const segments: MessageSegment[] = [];
  let artifactIndex = 0;
  let chartIndex = 0;
  let plainLines: string[] = [];
  let blockTitle = "";
  let blockLines: string[] = [];
  let insideBlock: "artifact" | "chart" | false = false;
  let codeFenceDepth = 0;

  for (const line of lines) {
    if (insideBlock) {
      const fenceMatch = line.match(CODE_FENCE_RE);
      if (fenceMatch) {
        codeFenceDepth = codeFenceDepth === 0 ? fenceMatch[1].length : 0;
      }

      if (codeFenceDepth === 0 && FENCE_CLOSE_RE.test(line)) {
        if (insideBlock === "artifact") {
          segments.push({
            kind: "artifact",
            index: artifactIndex,
            title: blockTitle,
            content: blockLines.join("\n"),
          });
          artifactIndex++;
        } else {
          segments.push({
            kind: "chart",
            index: chartIndex,
            title: blockTitle,
            content: blockLines.join("\n"),
          });
          chartIndex++;
        }
        insideBlock = false;
        codeFenceDepth = 0;
        blockLines = [];
        blockTitle = "";
      } else {
        blockLines.push(line);
      }
    } else {
      const artifactMatch = line.match(ARTIFACT_OPEN_RE);
      const chartMatch = !artifactMatch ? line.match(CHART_OPEN_RE) : null;
      if (artifactMatch || chartMatch) {
        if (plainLines.length > 0) {
          segments.push({ kind: "text", content: plainLines.join("\n") });
          plainLines = [];
        }
        insideBlock = artifactMatch ? "artifact" : "chart";
        codeFenceDepth = 0;
        blockTitle = (artifactMatch || chartMatch)![1];
      } else {
        plainLines.push(line);
      }
    }
  }

  // If still inside a block, return it as pending (streaming).
  if (insideBlock) {
    if (plainLines.length > 0) {
      segments.push({ kind: "text", content: plainLines.join("\n") });
    }
    const pending = {
      index: insideBlock === "artifact" ? artifactIndex : chartIndex,
      title: blockTitle,
      content: blockLines.join("\n"),
    };
    return {
      segments,
      pendingArtifact: insideBlock === "artifact" ? pending : null,
      pendingChart: insideBlock === "chart" ? pending : null,
    };
  }

  // No pending blocks — flush trailing text.
  if (plainLines.length > 0) {
    segments.push({ kind: "text", content: plainLines.join("\n") });
  }

  return { segments, pendingArtifact: null, pendingChart: null };
}
