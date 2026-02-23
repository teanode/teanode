import { describe, expect, it } from "vitest";
import { parseClipboardImages } from "./inputAreaPaste";

function makeFile({
  name,
  type,
  size,
  lastModified = 0,
}: {
  name: string;
  type: string;
  size: number;
  lastModified?: number;
}): File {
  return { name, type, size, lastModified } as File;
}

describe("parseClipboardImages", () => {
  it("extracts image files from clipboard items and reports text presence", () => {
    const imageFile = makeFile({
      name: "screenshot.png",
      type: "image/png",
      size: 1024,
      lastModified: 1,
    });
    const textFile = makeFile({
      name: "note.txt",
      type: "text/plain",
      size: 12,
      lastModified: 2,
    });

    const parsed = parseClipboardImages({
      items: [
        {
          kind: "file",
          type: imageFile.type,
          getAsFile: () => imageFile,
        },
        {
          kind: "file",
          type: textFile.type,
          getAsFile: () => textFile,
        },
      ],
      getData: () => "hello",
    });

    expect(parsed.imageFiles).toEqual([imageFile]);
    expect(parsed.hasText).toBe(true);
  });

  it("deduplicates files found in both items and files", () => {
    const imageFile = makeFile({
      name: "clip.jpg",
      type: "image/jpeg",
      size: 2048,
      lastModified: 3,
    });

    const parsed = parseClipboardImages({
      items: [
        {
          kind: "file",
          type: imageFile.type,
          getAsFile: () => imageFile,
        },
      ],
      files: [imageFile],
      getData: () => "",
    });

    expect(parsed.imageFiles).toEqual([imageFile]);
    expect(parsed.hasText).toBe(false);
  });
});
