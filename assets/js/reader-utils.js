export function boundedNumber(raw, fallback, min, max) {
  const value = Number(raw);
  return Number.isFinite(value) && value >= min && value <= max ? value : fallback;
}

/* readest's "maximum number of columns": the pick is a ceiling, and the stage
   only reaches it while every column stays wide enough to read. */
export function columnCount(width, maxColumns, minColumnWidth = 400) {
  const cap = boundedNumber(maxColumns, 1, 1, 4);
  const fits = Math.floor((Number(width) || 0) / minColumnWidth);
  return Math.min(cap, Math.max(fits, 1));
}

export function normalizePDFPage(page, numPages, spread = false) {
  const value = Math.min(Math.max(Number(page) || 1, 1), numPages || Number.MAX_SAFE_INTEGER);
  return !spread || value === 1 ? value : 2 + Math.floor((value - 2) / 2) * 2;
}

export function tocKey(value) {
  return String(value || "").split("#")[0].split("?")[0].split("/").pop();
}

/* A bookmark names itself after wherever it was dropped: the chapter if the book
   has one, otherwise the page or the percentage the reader can see on the bar. */
export function bookmarkLabel({ format, chapter = "", page = 0, percent = 0 } = {}) {
  const title = String(chapter || "").trim();
  if (title) return title.slice(0, 200);
  if (format === "pdf") return `Page ${Math.max(Math.round(Number(page) || 1), 1)}`;
  const value = Number(percent);
  return `${Math.round((Number.isFinite(value) ? value : 0) * 100)}%`;
}

export function deviceLabel({ userAgent = "", brands = [], platform = "" } = {}) {
  const brandString = brands.map(item => item.brand).join(" ");
  const browser = /Edge|Microsoft Edge/i.test(brandString) || /Edg\//.test(userAgent) ? "Edge"
    : /Firefox/i.test(brandString) || /Firefox\//.test(userAgent) ? "Firefox"
      : /Chrom/i.test(brandString) || /CriOS|Chrome\//.test(userAgent) ? "Chrome"
        : /Safari\//.test(userAgent) ? "Safari" : "Other";
  const source = `${platform} ${userAgent}`;
  const os = /Android/i.test(source) ? "Android"
    : /iOS|iPad|iPhone|iPod/i.test(source) ? "iOS/iPadOS"
      : /macOS|Mac OS X/i.test(source) ? "macOS"
        : /Windows/i.test(source) ? "Windows"
          : /Linux/i.test(source) ? "Linux" : "Other";
  return `${browser} on ${os}`.slice(0, 100);
}
