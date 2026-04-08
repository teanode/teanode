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

export type MessageSegment = TextSegment | ArtifactSegment;

export interface StreamingParseResult {
  segments: MessageSegment[];
  pendingArtifact: {
    index: number;
    title: string;
    content: string;
  } | null;
}

const ARTIFACT_OPEN_RE = /^:::artifact\{title="([^"]+)"\}\s*$/;
const ARTIFACT_CLOSE_RE = /^:::\s*$/;
const CODE_FENCE_RE = /^(`{3,})/;

/** Quick check — avoids full parsing when no artifacts are present. */
export function hasArtifacts(text: string): boolean {
  return text.includes(":::artifact{");
}

/**
 * Parse completed message text into segments of plain text and artifacts.
 * Code fences (```) are tracked so that `:::` inside a fenced code block
 * is not mistaken for an artifact boundary.
 */
export function parseArtifacts(text: string): MessageSegment[] {
  if (!hasArtifacts(text)) {
    return [{ kind: "text", content: text }];
  }

  const lines = text.split("\n");
  const segments: MessageSegment[] = [];
  let artifactIndex = 0;
  let plainLines: string[] = [];
  let artifactTitle = "";
  let artifactLines: string[] = [];
  let insideArtifact = false;
  let codeFenceDepth = 0;

  for (const line of lines) {
    if (insideArtifact) {
      // Track code fences inside the artifact body.
      const fenceMatch = line.match(CODE_FENCE_RE);
      if (fenceMatch) {
        codeFenceDepth = codeFenceDepth === 0 ? fenceMatch[1].length : 0;
      }

      if (codeFenceDepth === 0 && ARTIFACT_CLOSE_RE.test(line)) {
        // Close the artifact.
        segments.push({
          kind: "artifact",
          index: artifactIndex,
          title: artifactTitle,
          content: artifactLines.join("\n"),
        });
        artifactIndex++;
        insideArtifact = false;
        codeFenceDepth = 0;
        artifactLines = [];
        artifactTitle = "";
      } else {
        artifactLines.push(line);
      }
    } else {
      const openMatch = line.match(ARTIFACT_OPEN_RE);
      if (openMatch) {
        // Flush accumulated plain text.
        if (plainLines.length > 0) {
          segments.push({ kind: "text", content: plainLines.join("\n") });
          plainLines = [];
        }
        insideArtifact = true;
        codeFenceDepth = 0;
        artifactTitle = openMatch[1];
      } else {
        plainLines.push(line);
      }
    }
  }

  // Flush remaining content. If we're still inside an unclosed artifact,
  // treat the opener + body as plain text (incomplete artifact in committed
  // content is unlikely but handled gracefully).
  if (insideArtifact) {
    plainLines.push(`:::artifact{title="${artifactTitle}"}`);
    plainLines.push(...artifactLines);
  }
  if (plainLines.length > 0) {
    segments.push({ kind: "text", content: plainLines.join("\n") });
  }

  return segments;
}

/**
 * Streaming-safe variant. If the text ends with an unclosed artifact fence,
 * it is returned as `pendingArtifact` rather than being folded into plain
 * text. A partial opening fence at the very end (e.g. `:::artif`) is kept
 * as trailing plain text to avoid premature detection.
 */
export function parseArtifactsStreaming(text: string): StreamingParseResult {
  if (!hasArtifacts(text)) {
    return {
      segments: [{ kind: "text", content: text }],
      pendingArtifact: null,
    };
  }

  const lines = text.split("\n");
  const segments: MessageSegment[] = [];
  let artifactIndex = 0;
  let plainLines: string[] = [];
  let artifactTitle = "";
  let artifactLines: string[] = [];
  let insideArtifact = false;
  let codeFenceDepth = 0;

  for (const line of lines) {
    if (insideArtifact) {
      const fenceMatch = line.match(CODE_FENCE_RE);
      if (fenceMatch) {
        codeFenceDepth = codeFenceDepth === 0 ? fenceMatch[1].length : 0;
      }

      if (codeFenceDepth === 0 && ARTIFACT_CLOSE_RE.test(line)) {
        segments.push({
          kind: "artifact",
          index: artifactIndex,
          title: artifactTitle,
          content: artifactLines.join("\n"),
        });
        artifactIndex++;
        insideArtifact = false;
        codeFenceDepth = 0;
        artifactLines = [];
        artifactTitle = "";
      } else {
        artifactLines.push(line);
      }
    } else {
      const openMatch = line.match(ARTIFACT_OPEN_RE);
      if (openMatch) {
        if (plainLines.length > 0) {
          segments.push({ kind: "text", content: plainLines.join("\n") });
          plainLines = [];
        }
        insideArtifact = true;
        codeFenceDepth = 0;
        artifactTitle = openMatch[1];
      } else {
        plainLines.push(line);
      }
    }
  }

  // If still inside an artifact, return it as pending (streaming).
  if (insideArtifact) {
    if (plainLines.length > 0) {
      segments.push({ kind: "text", content: plainLines.join("\n") });
    }
    return {
      segments,
      pendingArtifact: {
        index: artifactIndex,
        title: artifactTitle,
        content: artifactLines.join("\n"),
      },
    };
  }

  // No pending artifact — flush trailing text.
  if (plainLines.length > 0) {
    segments.push({ kind: "text", content: plainLines.join("\n") });
  }

  return { segments, pendingArtifact: null };
}
