import ePub from "epubjs";
import * as pdfjsLib from "pdfjs-dist";

const app = document.querySelector("#reader-app");
const state = { phase: "booting", observed: null, acknowledged: null, pending: null, active: false, retry: 0, timer: null, rendition: null, pdf: null, pdfPage: 1, locations: null };
const csrf = document.querySelector('meta[name="csrf-token"]')?.content || "";
const id = Number(app?.dataset.bookId);
const format = app?.dataset.format;
const endpoint = `/api/books/${id}/progress`;
const sync = document.querySelector("#sync-state");
const progressLabel = document.querySelector("#reader-progress");
const pdfTones = ["paper", "sepia", "light"];
if (app) boot().catch(fail);

async function boot() {
  setLoading("Restoring your place…");
  const response = await fetch(endpoint, { credentials: "same-origin" });
  if (!response.ok) throw new Error("Could not restore reading position");
  const saved = await response.json();
  setLoading("Preparing the first page…");
  if (format === "epub") await bootEPUB(saved);
  else await bootPDF(saved);
  installControls();
  app.classList.remove("is-loading");
  document.querySelector("#reader-loading")?.remove();
  setPhase("ready");
}

async function bootEPUB(saved) {
  const book = ePub(app.dataset.fileUrl, { requestCredentials: true });
  const rendition = book.renderTo("epub-viewer", { width: "100%", height: "100%", spread: "auto" });
  state.rendition = rendition;
  const font = boundedNumber(localStorage.getItem("bookshelf:v1:epub-font"), 100, 75, 180);
  rendition.themes.fontSize(`${font}%`);
  const dark = document.documentElement.classList.contains("dark");
  rendition.themes.default({ body: { color: dark ? "#f5f5f4" : "#1c1917", background: dark ? "#0c0a09" : "#fafaf9" } });
  await rendition.display(saved.position || undefined);
  rendition.on("relocated", location => observe({ position: location.start.cfi, percent: percentFor(location.start.cfi) }));
  const navigation = await book.loaded.navigation;
  renderTOC(navigation.toc, href => rendition.display(href));
  const cacheKey = `bookshelf:v1:locations:${id}:${app.dataset.fileHash}`;
  const cached = localStorage.getItem(cacheKey);
  if (cached) { try { book.locations.load(cached); state.locations = book.locations; } catch { localStorage.removeItem(cacheKey); } }
  if (!state.locations) {
    progressLabel.textContent = "Progress unavailable while locations are prepared";
    queueMicrotask(async () => { try { await book.locations.generate(1600); state.locations = book.locations; localStorage.setItem(cacheKey, book.locations.save()); if (state.observed) observe({ ...state.observed, percent: percentFor(state.observed.position) }); } catch { /* CFI saves still work. */ } });
  }
}

async function bootPDF(saved) {
  setPDFTone(localStorage.getItem("bookshelf:v1:pdf-tone") || "paper");
  pdfjsLib.GlobalWorkerOptions.workerSrc = app.dataset.workerUrl;
  const zoom = boundedNumber(localStorage.getItem("bookshelf:v1:pdf-zoom"), 1.25, .5, 3);
  state.pdfZoom = zoom;
  state.pdf = await pdfjsLib.getDocument({ url: app.dataset.fileUrl, withCredentials: true }).promise;
  state.pdfPage = Math.min(Math.max(saved.page || 1, 1), state.pdf.numPages);
  await renderPDFPage();
  // Some PDFs contain malformed or unsupported outline entries even though
  // their pages are readable. The outline is optional, so it must not prevent
  // the document itself from opening.
  try {
    const outline = await state.pdf.getOutline();
    renderTOC(outline || [], async item => { const dest = typeof item.dest === "string" ? await state.pdf.getDestination(item.dest) : item.dest; if (dest) { const ref = await state.pdf.getPageIndex(dest[0]); state.pdfPage = ref + 1; await renderPDFPage(); observe({ page: state.pdfPage }); } });
  } catch (error) {
    console.warn("Could not load PDF outline", error);
  }
}

async function renderPDFPage() {
  const page = await state.pdf.getPage(state.pdfPage);
  const viewport = page.getViewport({ scale: state.pdfZoom * devicePixelRatio });
  const canvas = document.querySelector("#pdf-canvas");
  const ctx = canvas.getContext("2d", { alpha: false });
  canvas.width = viewport.width; canvas.height = viewport.height;
  canvas.style.width = `${viewport.width / devicePixelRatio}px`;
  await page.render({ canvasContext: ctx, viewport }).promise;
  canvas.hidden = false;
  progressLabel.textContent = `Page ${state.pdfPage} of ${state.pdf.numPages}`;
}

function observe(value) {
  state.observed = value; state.pending = value;
  clearTimeout(state.timer); state.timer = setTimeout(save, 700);
}

async function save(beacon = false) {
  clearTimeout(state.timer);
  if (!state.pending) return;
  const snapshot = state.pending;
  const payload = format === "epub" ? { position: snapshot.position, device_label: deviceLabel() } : { page: snapshot.page, device_label: deviceLabel() };
  if (format === "epub" && Number.isFinite(snapshot.percent)) payload.percent = snapshot.percent;
  if (beacon && navigator.sendBeacon) { payload.csrf_token = csrf; navigator.sendBeacon(endpoint, new Blob([JSON.stringify(payload)], { type: "application/json" })); setPhase("saving"); return; }
  if (state.active) return;
  state.active = true; setPhase("saving");
  let retryable = true;
  try {
    const response = await fetch(endpoint, { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(payload) });
    if (!response.ok) { retryable = response.status >= 500; throw new Error(retryable ? "temporary" : "Position was rejected"); }
    state.acknowledged = snapshot; if (state.pending === snapshot) state.pending = null; state.retry = 0; setPhase("saved");
  } catch (error) {
    if (retryable && state.retry < 3) { const delay = 500 * 2 ** state.retry++; setTimeout(save, delay); }
    else setPhase("failed");
  } finally { state.active = false; if (state.pending && state.pending !== snapshot) save(); }
}

function installControls() {
  document.querySelector("#toc-toggle").addEventListener("click", event => { const toc = document.querySelector("#toc"); toc.hidden = !toc.hidden; event.currentTarget.setAttribute("aria-expanded", String(!toc.hidden)); });
  document.querySelector("#theme").addEventListener("click", () => { const dark = document.documentElement.classList.toggle("dark"); localStorage.setItem("bookshelf:v1:theme", dark ? "dark" : "light"); if (state.rendition) state.rendition.themes.default({ body: { color: dark ? "#f5f5f4" : "#1c1917", background: dark ? "#0c0a09" : "#fafaf9" } }); });
  document.querySelector("#pdf-tone").addEventListener("click", () => { const current = app.dataset.pdfTone || "paper"; const next = pdfTones[(pdfTones.indexOf(current) + 1) % pdfTones.length]; setPDFTone(next); });
  document.querySelector("#increase").addEventListener("click", () => adjust(1));
  document.querySelector("#decrease").addEventListener("click", () => adjust(-1));
  document.addEventListener("keydown", async event => { if (["INPUT","SELECT","TEXTAREA"].includes(event.target.tagName)) return; if (["ArrowRight","PageDown"].includes(event.key)) await navigate(1); if (["ArrowLeft","PageUp"].includes(event.key)) await navigate(-1); });
  let touch = null; document.querySelector("#reader-stage").addEventListener("touchstart", e => { touch = { x: e.touches[0].clientX, y: e.touches[0].clientY }; }, { passive: true }); document.querySelector("#reader-stage").addEventListener("touchend", e => { if (!touch) return; const dx = e.changedTouches[0].clientX - touch.x; const dy = e.changedTouches[0].clientY - touch.y; touch = null; if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) navigate(dx < 0 ? 1 : -1); }, { passive: true });
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") save(true); });
  addEventListener("pagehide", () => save(true));
}

async function navigate(direction) { if (format === "epub") return direction > 0 ? state.rendition.next() : state.rendition.prev(); const next = Math.min(Math.max(state.pdfPage + direction, 1), state.pdf.numPages); if (next !== state.pdfPage) { state.pdfPage = next; await renderPDFPage(); observe({ page: next }); } }
async function adjust(direction) { if (format === "epub") { const value = boundedNumber(localStorage.getItem("bookshelf:v1:epub-font"), 100, 75, 180) + direction * 10; localStorage.setItem("bookshelf:v1:epub-font", value); state.rendition.themes.fontSize(`${value}%`); } else { state.pdfZoom = Math.min(Math.max(state.pdfZoom + direction * .15, .5), 3); localStorage.setItem("bookshelf:v1:pdf-zoom", state.pdfZoom); await renderPDFPage(); } }
function renderTOC(items, activate) { const list = document.querySelector("#toc-list"); for (const item of items) { const li = document.createElement("li"); const link = document.createElement("a"); link.href = "#"; link.textContent = item.label || item.title || "Section"; link.addEventListener("click", event => { event.preventDefault(); activate(item.href ? item.href : item); }); li.append(link); list.append(li); } }
function percentFor(cfi) { if (!state.locations) return undefined; const value = state.locations.percentageFromCfi(cfi); return Number.isFinite(value) ? value : undefined; }
function boundedNumber(raw, fallback, min, max) { const value = Number(raw); return Number.isFinite(value) && value >= min && value <= max ? value : fallback; }
function setPhase(phase) { state.phase = phase; sync.textContent = phase[0].toUpperCase() + phase.slice(1); sync.dataset.state = phase; }
function setLoading(message) { const loading = document.querySelector("#reader-loading-text"); if (loading) loading.textContent = message; }
function setPDFTone(tone) { const value = pdfTones.includes(tone) ? tone : "paper"; app.dataset.pdfTone = value; localStorage.setItem("bookshelf:v1:pdf-tone", value); const button = document.querySelector("#pdf-tone"); if (button) { const label = value[0].toUpperCase() + value.slice(1); button.textContent = label; button.setAttribute("aria-label", `PDF page background: ${label}. Activate to change`); button.title = `PDF page background: ${label}`; } }
function fail(error) { console.error(error); app?.classList.remove("is-loading"); setPhase("failed"); setLoading("This book could not be opened."); }
function deviceLabel() { const ua = navigator.userAgent; const brands = navigator.userAgentData?.brands?.map(item => item.brand).join(" ") || ""; const platform = navigator.userAgentData?.platform || ""; const browser = /Edge|Microsoft Edge/i.test(brands) || /Edg\//.test(ua) ? "Edge" : /Firefox/i.test(brands) || /Firefox\//.test(ua) ? "Firefox" : /Chrom/i.test(brands) || /CriOS|Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Other"; const source = `${platform} ${ua}`; const os = /Android/i.test(source) ? "Android" : /iOS|iPad|iPhone|iPod/i.test(source) ? "iOS/iPadOS" : /macOS|Mac OS X/i.test(source) ? "macOS" : /Windows/i.test(source) ? "Windows" : /Linux/i.test(source) ? "Linux" : "Other"; return `${browser} on ${os}`.slice(0, 100); }
