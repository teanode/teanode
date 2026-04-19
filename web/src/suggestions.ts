// Suggestion marker parser for the frontend.
//
// The LLM appends a marker like <!--suggestions:["A","B"]--> at the end of
// assistant messages when it wants to offer clickable reply suggestions.
// This module extracts and strips those markers for display.

const MARKER_PATTERN = /<!--suggestions:(\[[^\]]*\])-->/;
const MARKER_PREFIX = "<!--suggestions:";

export interface SuggestionParseResult {
  displayText: string;
  suggestions: string[];
}

/**
 * Parse and strip the first suggestion marker from text.
 * Returns cleaned display text and the extracted suggestions (if valid).
 */
export function parseSuggestionMarker(text: string): SuggestionParseResult {
  const match = text.match(MARKER_PATTERN);
  if (!match) {
    return { displayText: text, suggestions: [] };
  }

  try {
    const parsed: unknown = JSON.parse(match[1]);
    if (
      !Array.isArray(parsed) ||
      parsed.length < 2 ||
      parsed.length > 6 ||
      parsed.some((item) => typeof item !== "string" || item === "")
    ) {
      return { displayText: text, suggestions: [] };
    }
    const displayText = text.replace(MARKER_PATTERN, "");
    return { displayText, suggestions: parsed as string[] };
  } catch {
    return { displayText: text, suggestions: [] };
  }
}

/**
 * For streaming text, hide any trailing partial suggestion marker.
 *
 * During streaming the marker arrives character-by-character. Once we detect
 * an unterminated `<!--` that looks like it could become a suggestion marker,
 * we trim it from the display text so the user never sees raw marker syntax.
 */
export function hidePartialSuggestionMarker(text: string): string {
  const idx = text.lastIndexOf("<!--");
  if (idx === -1) return text;

  const tail = text.slice(idx);

  // Already terminated — not a partial marker.
  if (tail.includes("-->")) return text;

  // Short tail: could be a prefix of "<!--suggestions:"
  if (tail.length <= MARKER_PREFIX.length) {
    if (MARKER_PREFIX.startsWith(tail)) {
      return text.slice(0, idx);
    }
  } else {
    // Longer than the prefix — if it starts with the full prefix, it's our
    // marker being streamed and not yet terminated.
    if (tail.startsWith(MARKER_PREFIX)) {
      return text.slice(0, idx);
    }
  }

  return text;
}

export function extractSuggestionsHeuristically(text: string): string[] {
  const lines = text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const options: string[] = [];
  for (const line of lines) {
    const match = line.match(/^(?:[-*]|\d+[.)])\s+(.+)$/);
    if (!match) continue;
    const option = match[1].trim();
    if (option.length === 0 || option.length > 120) continue;
    options.push(option);
    if (options.length >= 6) break;
  }

  if (options.length >= 2 && options.length <= 6) {
    return options;
  }
  return [];
}
