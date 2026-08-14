import { describe, expect, test } from "bun:test";
import { bookmarkLabel, boundedNumber, columnCount, deviceLabel, normalizePDFPage, tocKey } from "./reader-utils.js";

describe("boundedNumber", () => {
  test("accepts bounds and rejects invalid values", () => {
    expect(boundedNumber("2", 1, 1, 3)).toBe(2);
    expect(boundedNumber("1", 9, 1, 3)).toBe(1);
    expect(boundedNumber("3", 9, 1, 3)).toBe(3);
    for (const raw of ["nope", Infinity, 0, 4]) expect(boundedNumber(raw, 9, 1, 3)).toBe(9);
  });
});

describe("columnCount", () => {
  test("treats the preference as a ceiling, not a target", () => {
    expect(columnCount(2000, 1)).toBe(1);
    expect(columnCount(2000, 2)).toBe(2);
    expect(columnCount(2000, 4)).toBe(4);
  });

  test("drops columns the stage is too narrow to hold", () => {
    expect(columnCount(799, 2)).toBe(1);
    expect(columnCount(800, 2)).toBe(2);
    expect(columnCount(1000, 4)).toBe(2);
    expect(columnCount(0, 4)).toBe(1);
  });

  test("falls back to a single column for unusable maximums", () => {
    expect(columnCount(2000, "nope")).toBe(1);
    expect(columnCount(2000, 9)).toBe(1);
  });
});

describe("normalizePDFPage", () => {
  test("clamps invalid and out-of-range pages", () => {
    expect(normalizePDFPage("nope", 5)).toBe(1);
    expect(normalizePDFPage(0, 5)).toBe(1);
    expect(normalizePDFPage(9, 5)).toBe(5);
    expect(normalizePDFPage(4, 0)).toBe(4);
  });

  test("aligns spread pages while keeping the first page single", () => {
    expect(normalizePDFPage(1, 8, true)).toBe(1);
    expect(normalizePDFPage(2, 8, true)).toBe(2);
    expect(normalizePDFPage(3, 8, true)).toBe(2);
    expect(normalizePDFPage(4, 8, true)).toBe(4);
    expect(normalizePDFPage(4, 8, false)).toBe(4);
  });
});

describe("tocKey", () => {
  test("removes query, fragment, and path", () => {
    expect(tocKey("OPS/chapter.xhtml?x=1#part")).toBe("chapter.xhtml");
    expect(tocKey("chapter.xhtml#part")).toBe("chapter.xhtml");
    expect(tocKey("chapter.xhtml?x=1")).toBe("chapter.xhtml");
    expect(tocKey("")).toBe("");
    expect(tocKey(null)).toBe("");
  });
});

describe("deviceLabel", () => {
  test("identifies browser and operating system combinations", () => {
    expect(deviceLabel({ userAgent: "Edg/120", platform: "Windows" })).toBe("Edge on Windows");
    expect(deviceLabel({ userAgent: "Firefox/120", platform: "Linux" })).toBe("Firefox on Linux");
    expect(deviceLabel({ userAgent: "Chrome/120", platform: "macOS" })).toBe("Chrome on macOS");
    expect(deviceLabel({ userAgent: "CriOS/120", platform: "iPhone" })).toBe("Chrome on iOS/iPadOS");
    expect(deviceLabel({ userAgent: "Safari/120", platform: "iPad" })).toBe("Safari on iOS/iPadOS");
    expect(deviceLabel({ userAgent: "Other", platform: "Android" })).toBe("Other on Android");
    expect(deviceLabel({ userAgent: "Other", platform: "Other" })).toBe("Other on Other");
    expect(deviceLabel({ brands: [{ brand: "Microsoft Edge" }], platform: "Windows" })).toBe("Edge on Windows");
    expect(deviceLabel({ brands: [{ brand: "Firefox" }], platform: "Linux" })).toBe("Firefox on Linux");
    expect(deviceLabel({ brands: [{ brand: "Chromium" }], platform: "Linux" })).toBe("Chrome on Linux");
  });

});

describe("bookmarkLabel", () => {
  test("prefers the chapter title when the book has one", () => {
    expect(bookmarkLabel({ format: "epub", chapter: "  Chapter Two  ", percent: 0.5 })).toBe("Chapter Two");
    expect(bookmarkLabel({ format: "pdf", chapter: "Appendix", page: 12 })).toBe("Appendix");
  });

  test("falls back to the page for PDFs and the percentage for EPUBs", () => {
    expect(bookmarkLabel({ format: "pdf", page: 12 })).toBe("Page 12");
    expect(bookmarkLabel({ format: "pdf", page: 0 })).toBe("Page 1");
    expect(bookmarkLabel({ format: "epub", percent: 0.426 })).toBe("43%");
    expect(bookmarkLabel({ format: "epub" })).toBe("0%");
    expect(bookmarkLabel({ format: "epub", percent: Number.NaN })).toBe("0%");
    expect(bookmarkLabel()).toBe("0%");
  });

  test("truncates an overlong chapter title", () => {
    expect(bookmarkLabel({ format: "epub", chapter: "x".repeat(250) })).toHaveLength(200);
  });
});
