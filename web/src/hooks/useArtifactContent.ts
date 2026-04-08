import { useMemo } from "react";
import { useAppContext } from "../context";
import { useArtifactPanel } from "../components/ArtifactPanelProvider";
import {
  parseArtifacts,
  parseArtifactsStreaming,
  hasArtifacts,
} from "../artifactParser";

export interface ArtifactContent {
  title: string;
  content: string;
  isStreaming: boolean;
}

/**
 * Derives the currently-open artifact's content from the source of truth
 * (backend.messages + backend.streamText). Returns null when no artifact
 * is open or the referenced artifact no longer exists.
 */
export function useArtifactContent(): ArtifactContent | null {
  const { openMessageId, openArtifactIndex } = useArtifactPanel();
  const { backend } = useAppContext();

  return useMemo(() => {
    if (openMessageId === null || openArtifactIndex === null) return null;

    // Find the message in the display messages list.
    const message = backend.messages.find(
      (message) => message.id === openMessageId,
    );
    if (!message) return null;

    // Determine if this is the currently streaming message.
    // The last assistant message for the active run receives streaming text.
    let rawText = message.content;
    let isStreaming = false;

    if (backend.isStreaming && backend.currentRunId) {
      // Walk backwards to find the last assistant message for the active run.
      for (let index = backend.messages.length - 1; index >= 0; index--) {
        const candidate = backend.messages[index];
        if (
          candidate.type === "assistant" &&
          candidate.runId === backend.currentRunId
        ) {
          if (candidate.id === openMessageId) {
            rawText = backend.streamText || message.content;
            isStreaming = true;
          }
          break;
        }
      }
    }

    if (!hasArtifacts(rawText)) return null;

    if (isStreaming) {
      const result = parseArtifactsStreaming(rawText);
      // Check completed segments first.
      for (const segment of result.segments) {
        if (
          segment.kind === "artifact" &&
          segment.index === openArtifactIndex
        ) {
          return {
            title: segment.title,
            content: segment.content,
            isStreaming: false,
          };
        }
      }
      // Check the pending (still-streaming) artifact.
      if (
        result.pendingArtifact &&
        result.pendingArtifact.index === openArtifactIndex
      ) {
        return {
          title: result.pendingArtifact.title,
          content: result.pendingArtifact.content,
          isStreaming: true,
        };
      }
      return null;
    }

    // Committed message — use the non-streaming parser.
    const segments = parseArtifacts(rawText);
    for (const segment of segments) {
      if (segment.kind === "artifact" && segment.index === openArtifactIndex) {
        return {
          title: segment.title,
          content: segment.content,
          isStreaming: false,
        };
      }
    }

    return null;
  }, [
    openMessageId,
    openArtifactIndex,
    backend.messages,
    backend.streamText,
    backend.isStreaming,
    backend.currentRunId,
  ]);
}
