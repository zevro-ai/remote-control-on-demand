import { describe, expect, it } from "vitest";
import {
  extractImageFilesFromItems,
  filterImageFiles,
  hasFileItems,
  isImageLikeFile,
} from "./imageAttachments";

describe("isImageLikeFile", () => {
  it("accepts image mime types", () => {
    expect(isImageLikeFile({ name: "wall.bin", type: "image/png" })).toBe(true);
  });

  it("falls back to common image file extensions", () => {
    expect(isImageLikeFile({ name: "wall.jpeg", type: "" })).toBe(true);
    expect(isImageLikeFile({ name: "diagram.SVG", type: "" })).toBe(true);
  });

  it("rejects non-image files", () => {
    expect(isImageLikeFile({ name: "notes.txt", type: "text/plain" })).toBe(false);
    expect(isImageLikeFile({ name: "archive.tar", type: "" })).toBe(false);
  });
});

describe("filterImageFiles", () => {
  it("keeps only image-like files and preserves order", () => {
    const files = [
      { name: "readme.md", type: "text/markdown" },
      { name: "shot-1.png", type: "image/png" },
      { name: "shot-2.webp", type: "" },
    ];

    expect(filterImageFiles(files)).toEqual([
      { name: "shot-1.png", type: "image/png" },
      { name: "shot-2.webp", type: "" },
    ]);
  });
});

describe("extractImageFilesFromItems", () => {
  it("keeps only clipboard file items with image mime types", () => {
    const imageFile = { name: "paste.png", type: "image/png" };

    const items = [
      {
        kind: "string",
        type: "text/plain",
        getAsFile: () => null,
      },
      {
        kind: "file",
        type: "image/png",
        getAsFile: () => imageFile,
      },
      {
        kind: "file",
        type: "application/pdf",
        getAsFile: () => ({ name: "doc.pdf", type: "application/pdf" }),
      },
    ];

    expect(extractImageFilesFromItems(items)).toEqual([imageFile]);
  });
});

describe("hasFileItems", () => {
  it("returns true when drag items include files even without mime type metadata", () => {
    const items = [
      {
        kind: "file",
        type: "",
        getAsFile: () => ({ name: "screenshot.png", type: "" }),
      },
    ];

    expect(hasFileItems(items)).toBe(true);
  });

  it("returns false when drag items contain no files", () => {
    const items = [
      {
        kind: "string",
        type: "text/plain",
        getAsFile: () => null,
      },
    ];

    expect(hasFileItems(items)).toBe(false);
  });
});
