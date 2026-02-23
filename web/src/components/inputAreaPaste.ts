export interface ClipboardImageParseResult {
  imageFiles: File[];
  hasText: boolean;
}

interface ClipboardItemLike {
  kind: string;
  type: string;
  getAsFile: () => File | null;
}

interface ClipboardDataLike {
  items?: ArrayLike<ClipboardItemLike>;
  files?: ArrayLike<File>;
  getData?: (format: string) => string;
}

function fileKey(file: File): string {
  return `${file.name}:${file.type}:${file.size}:${file.lastModified}`;
}

export function parseClipboardImages(
  clipboardData: ClipboardDataLike | null | undefined,
): ClipboardImageParseResult {
  if (!clipboardData) return { imageFiles: [], hasText: false };

  const files: File[] = [];
  const seen = new Set<string>();

  const pushImage = (file: File | null | undefined) => {
    if (!file || !file.type.startsWith("image/")) return;
    const key = fileKey(file);
    if (seen.has(key)) return;
    seen.add(key);
    files.push(file);
  };

  if (clipboardData.items) {
    for (const item of Array.from(clipboardData.items)) {
      if (item.kind === "file" && item.type.startsWith("image/")) {
        pushImage(item.getAsFile());
      }
    }
  }

  if (clipboardData.files) {
    for (const file of Array.from(clipboardData.files)) {
      pushImage(file);
    }
  }

  const rawText = clipboardData.getData?.("text/plain") || "";
  return {
    imageFiles: files,
    hasText: rawText.trim().length > 0,
  };
}
