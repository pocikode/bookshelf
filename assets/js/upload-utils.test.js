import { describe, expect, test } from "bun:test";
import { formatFileSize, isSupported } from "./upload-utils.js";

describe("formatFileSize", () => {
  test("formats kilobytes with a minimum of one", () => {
    expect(formatFileSize(0)).toBe("1 KB");
    expect(formatFileSize(1024)).toBe("1 KB");
    expect(formatFileSize(1536)).toBe("2 KB");
    expect(formatFileSize(1024 * 1024 - 1)).toBe("1024 KB");
  });

  test("formats megabytes with one decimal", () => {
    expect(formatFileSize(1024 * 1024)).toBe("1.0 MB");
    expect(formatFileSize(2.5 * 1024 * 1024)).toBe("2.5 MB");
  });
});

describe("isSupported", () => {
  test("accepts EPUB and PDF extensions case-insensitively", () => {
    expect(isSupported({ name: "book.epub" })).toBe(true);
    expect(isSupported({ name: "BOOK.PDF" })).toBe(true);
    expect(isSupported({ name: "book.epub.bak" })).toBe(false);
    expect(isSupported({ name: "book.txt" })).toBe(false);
  });
});
