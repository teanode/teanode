import type { Attachment } from "./types";

export interface ExtractedContent {
  text: string;
  attachments?: Attachment[];
}

interface ContentBlock {
  type: string;
  text?: string;
  mediaId?: string;
  format?: string;
  filename?: string;
}

function extractFromBlocks(blocks: ContentBlock[]): ExtractedContent {
  let text = "";
  const attachments: Attachment[] = [];
  for (const block of blocks) {
    if (block.type === "text") text += block.text || "";
    else if (block.type === "attachment") {
      attachments.push({
        mediaId: block.mediaId!,
        format: block.format!,
        filename: block.filename!,
      });
    }
  }
  return {
    text,
    attachments: attachments.length > 0 ? attachments : undefined,
  };
}

/**
 * Normalizes message content from the backend into a plain text string plus
 * optional attachments. The backend stores content as json.RawMessage which
 * can be either a JSON string or an array of content blocks like:
 *   [{type:"text", text:"..."}, {type:"attachment", mediaId:"...", ...}]
 */
export function normalizeContent(
  content: unknown,
): ExtractedContent {
  if (!content) return { text: "" };

  // Already a parsed array of content blocks.
  if (
    Array.isArray(content) &&
    content.length > 0 &&
    content[0].type
  ) {
    return extractFromBlocks(content);
  }

  if (typeof content === "string") {
    try {
      const parsed = JSON.parse(content);
      if (typeof parsed === "string") return { text: parsed };
      if (Array.isArray(parsed) && parsed.length > 0 && parsed[0].type) {
        return extractFromBlocks(parsed);
      }
      return { text: content };
    } catch {
      return { text: content };
    }
  }

  // Content is some other parsed JS value — stringify it.
  return { text: JSON.stringify(content) };
}
