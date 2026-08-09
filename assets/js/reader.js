import ePub from "epubjs";
import * as pdfjsLib from "pdfjs-dist";

const app = document.querySelector("#reader-app");
const id = Number(app?.dataset.bookId);
const format = app?.dataset.format;
const csrf = document.querySelector('meta[name="csrf-token"]')?.content || "";
const endpoint = `/api/books/${id}/progress`;
const pdfTones = ["paper", "sepia", "light"];
const preferenceKey = `bookshelf:v2:reader:${id}`;
const defaults = { fontSize: 100, lineHeight: 1.5, textWidth: 52, flow: "paginated", pdfZoom: 1.25, pdfTone: "paper" };
const state = {
  phase: "booting",
  active: false,
  acknowledged: null,
  pending: null,
  observed: null,
  retry: 0,
  timer: null,
  hideTimer: null,
  book: null,
  rendition: null,
  pdf: null,
  pdfTextLayer: null,
  pdfPage: 1,
  pdfRenderToken: 0,
  locations: null,
  currentLocation: null,
  tocItems: [],
  percent: 0,
  savedPercent: 0,
  preferences: loadPreferences(),
  boundDocuments: new WeakSet(),
};

if (app) boot().catch(fail);

async function boot() {
  setLoading("Restoring your place...");
  const response = await fetch(endpoint, { credentials: "same-origin" });
  if (!response.ok) throw new Error("Could not restore reading position");
  const saved = await response.json();
  state.savedPercent = Number.isFinite(saved.percent) ? saved.percent : 0;
  setLoading("Preparing the first page...");
  if (format === "epub") await bootEPUB(saved);
  else await bootPDF(saved);
  installControls();
  app.classList.remove("is-loading");
  document.querySelector("#reader-loading")?.remove();
  setPhase("ready");
  setChromeVisible(true);
  scheduleChromeHide();
}

async function bootEPUB(saved) {
  const viewer = document.querySelector("#epub-viewer");
  viewer.hidden = false;
  const book = ePub(app.dataset.fileUrl, { requestCredentials: true });
  const rendition = book.renderTo("epub-viewer", { width: "100%", height: "100%", spread: "auto" });
  state.book = book;
  state.rendition = rendition;
  loadEPUBLocations(book);
  rendition.flow(state.preferences.flow === "scrolled" ? "scrolled-doc" : "paginated");
  applyEPUBStyles();
  rendition.on("relocated", onEPUBRelocated);
  rendition.on("rendered", attachEPUBContents);
  await rendition.display(saved.position || undefined);
  const navigation = await book.loaded.navigation;
  state.tocItems = navigation.toc || [];
  renderTOC(state.tocItems, item => rendition.display(item.href));
  if (!state.locations) {
    setProgressMessage("Preparing reading progress...");
    void prepareEPUBLocations(book);
  }
  for (const contents of rendition.getContents()) attachEPUBContents(contents);
}

function loadEPUBLocations(book) {
  const cacheKey = `bookshelf:v1:locations:${id}:${app.dataset.fileHash}`;
  const cached = storageGet(cacheKey);
  if (!cached) return;
  try {
    book.locations.load(cached);
    state.locations = book.locations;
  } catch {
    storageRemove(cacheKey);
  }
}

async function prepareEPUBLocations(book) {
  const cacheKey = `bookshelf:v1:locations:${id}:${app.dataset.fileHash}`;
  try {
    await book.locations.generate(1600);
    state.locations = book.locations;
    storageSet(cacheKey, book.locations.save());
    if (state.currentLocation?.start?.cfi) onEPUBRelocated(state.currentLocation);
  } catch {
    setProgressMessage("Position saved without percentage");
  }
}

async function bootPDF(saved) {
  document.querySelector("#pdf-page").hidden = false;
  state.pdfZoom = state.preferences.pdfZoom;
  setPDFTone(state.preferences.pdfTone, false);
  pdfjsLib.GlobalWorkerOptions.workerSrc = app.dataset.workerUrl;
  state.pdf = await pdfjsLib.getDocument({ url: app.dataset.fileUrl, withCredentials: true }).promise;
  state.pdfPage = Math.min(Math.max(saved.page || 1, 1), state.pdf.numPages);
  await renderPDFPage();
  try {
    const outline = await state.pdf.getOutline();
    state.tocItems = outline || [];
    renderTOC(state.tocItems, async item => {
      const destination = typeof item.dest === "string" ? await state.pdf.getDestination(item.dest) : item.dest;
      if (!destination) return;
      const pageIndex = await state.pdf.getPageIndex(destination[0]);
      state.pdfPage = pageIndex + 1;
      await renderPDFPage();
      observe({ page: state.pdfPage });
    });
  } catch (error) {
    console.warn("Could not load PDF outline", error);
  }
}

async function renderPDFPage() {
  const page = await state.pdf.getPage(state.pdfPage);
  const token = ++state.pdfRenderToken;
  state.pdfTextLayer?.cancel();
  state.pdfTextLayer = null;
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  const viewport = page.getViewport({ scale: state.preferences.pdfZoom * dpr });
  const canvas = document.querySelector("#pdf-canvas");
  const textLayerContainer = document.querySelector("#pdf-text-layer");
  const context = canvas.getContext("2d", { alpha: false });
  textLayerContainer.replaceChildren();
  canvas.width = viewport.width;
  canvas.height = viewport.height;
  canvas.style.width = `${viewport.width / dpr}px`;
  await page.render({ canvasContext: context, viewport }).promise;
  if (token !== state.pdfRenderToken) return;
  canvas.hidden = false;
  const baseViewport = page.getViewport({ scale: 1 });
  const displayedWidth = canvas.getBoundingClientRect().width || viewport.width / dpr;
  const textScale = displayedWidth / baseViewport.width;
  textLayerContainer.style.setProperty("--total-scale-factor", String(textScale));
  const textLayer = new pdfjsLib.TextLayer({
    textContentSource: await page.getTextContent(),
    container: textLayerContainer,
    viewport: page.getViewport({ scale: textScale }),
  });
  state.pdfTextLayer = textLayer;
  await textLayer.render();
  if (token !== state.pdfRenderToken) return;
  updatePDFProgress();
}

function installControls() {
  const tocToggle = document.querySelector("#toc-toggle");
  const settingsToggle = document.querySelector("#settings-toggle");
  tocToggle.addEventListener("click", () => togglePanel("toc"));
  settingsToggle.addEventListener("click", () => togglePanel("reader-settings"));
  document.querySelector("#toc-close").addEventListener("click", closePanels);
  document.querySelector("#settings-close").addEventListener("click", closePanels);
  document.querySelector("#reader-backdrop").addEventListener("click", closePanels);
  document.querySelector("#reader-prev").addEventListener("click", () => navigate(-1));
  document.querySelector("#reader-next").addEventListener("click", () => navigate(1));
  document.querySelector("#reader-prev-zone").addEventListener("click", () => navigate(-1));
  document.querySelector("#reader-next-zone").addEventListener("click", () => navigate(1));
  document.querySelector("#reader-progress-slider").addEventListener("change", event => seek(Number(event.currentTarget.value) / 100));
  document.querySelector("#reader-progress-slider").addEventListener("input", event => previewProgress(Number(event.currentTarget.value) / 100));
  document.querySelector("#font-size").addEventListener("input", event => updatePreference("fontSize", Number(event.currentTarget.value), applyEPUBStyles));
  document.querySelector("#line-height").addEventListener("input", event => updatePreference("lineHeight", Number(event.currentTarget.value), applyEPUBStyles));
  document.querySelector("#text-width").addEventListener("input", event => updatePreference("textWidth", Number(event.currentTarget.value), applyEPUBStyles));
  document.querySelector("#pdf-zoom").addEventListener("input", event => updatePreference("pdfZoom", Number(event.currentTarget.value) / 100, () => renderPDFPage()));
  document.querySelector("#reader-flow").addEventListener("change", event => setEPUBFlow(event.currentTarget.value));
  document.querySelector("#reset-settings").addEventListener("click", resetPreferences);
  document.querySelector("#theme").addEventListener("click", toggleTheme);
  document.querySelector("#fullscreen").addEventListener("click", toggleFullscreen);
  for (const button of document.querySelectorAll("[data-pdf-tone]")) button.addEventListener("click", () => setPDFTone(button.dataset.pdfTone));

  document.addEventListener("keydown", handleKeydown);
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") save(true); });
  addEventListener("pagehide", () => save(true));
  addEventListener("resize", () => { if (format === "pdf" && state.pdf) renderPDFPage().catch(error => console.warn("Could not resize PDF", error)); });
  document.querySelector("#reader-stage").addEventListener("click", event => {
    if (["reader-stage", "pdf-page", "pdf-canvas"].includes(event.target.id)) toggleChrome();
  });
  bindTouchSurface(document.querySelector("#reader-stage"));
  updateSettingsUI();
  updateThemeButton();
  if (!document.fullscreenEnabled && !app.requestFullscreen && !app.webkitRequestFullscreen) document.querySelector("#fullscreen").hidden = true;
}

function handleKeydown(event) {
  if (["INPUT", "SELECT", "TEXTAREA", "BUTTON"].includes(event.target.tagName)) return;
  if (event.key === "Escape") {
    if (closePanels()) event.preventDefault();
    return;
  }
  if (event.key.toLowerCase() === "t") { togglePanel("toc"); event.preventDefault(); return; }
  if (event.key.toLowerCase() === "s") { togglePanel("reader-settings"); event.preventDefault(); return; }
  if (event.key.toLowerCase() === "f") { toggleFullscreen(); event.preventDefault(); return; }
  if (["+", "=", "-", "_"].includes(event.key) && format === "epub") { adjustFont(event.key === "+" || event.key === "=" ? 5 : -5); event.preventDefault(); return; }
  if (["ArrowRight", "PageDown", " "].includes(event.key)) { navigate(1); event.preventDefault(); return; }
  if (["ArrowLeft", "PageUp"].includes(event.key)) { navigate(-1); event.preventDefault(); return; }
  if (event.key === "Home") { goToEdge(false); event.preventDefault(); return; }
  if (event.key === "End") { goToEdge(true); event.preventDefault(); }
}

function attachEPUBContents(contents) {
  const contentDocument = contents?.document;
  if (!contentDocument || state.boundDocuments.has(contentDocument)) return;
  state.boundDocuments.add(contentDocument);
  enableEPUBTextSelection(contentDocument);
  contentDocument.addEventListener("keydown", handleKeydown);
  contentDocument.addEventListener("click", event => {
    const selection = contentDocument.getSelection?.();
    if (selection && !selection.isCollapsed) return;
    if (!event.target.closest("a")) toggleChrome();
  });
  bindTouchSurface(contentDocument);
}

function enableEPUBTextSelection(contentDocument) {
  const style = contentDocument.createElement("style");
  style.textContent = `
    html, body, body * {
      -webkit-touch-callout: default !important;
      -webkit-user-select: text !important;
      user-select: text !important;
    }
  `;
  contentDocument.head.append(style);
}

function bindTouchSurface(target) {
  let touch = null;
  target.addEventListener("touchstart", event => {
    if (event.touches.length !== 1) { touch = null; return; }
    touch = { x: event.touches[0].clientX, y: event.touches[0].clientY };
  }, { passive: true });
  target.addEventListener("touchend", event => {
    if (!touch || !event.changedTouches[0]) return;
    const dx = event.changedTouches[0].clientX - touch.x;
    const dy = event.changedTouches[0].clientY - touch.y;
    touch = null;
    const selection = target.getSelection?.() || target.ownerDocument?.getSelection?.();
    if (selection && !selection.isCollapsed) return;
    if (Math.abs(dx) > 52 && Math.abs(dx) > Math.abs(dy) * 1.35) navigate(dx < 0 ? 1 : -1);
    else if (Math.abs(dx) < 16 && Math.abs(dy) < 16) setChromeVisible(!app.classList.contains("reader-chrome-hidden"));
  }, { passive: true });
  target.addEventListener("touchcancel", () => { touch = null; }, { passive: true });
}

async function navigate(direction) {
  closePanels();
  setChromeVisible(true);
  scheduleChromeHide();
  if (format === "epub") {
    if (direction > 0) await state.rendition.next();
    else await state.rendition.prev();
    return;
  }
  const next = Math.min(Math.max(state.pdfPage + direction, 1), state.pdf.numPages);
  if (next === state.pdfPage) return;
  state.pdfPage = next;
  await renderPDFPage();
  observe({ page: next });
}

async function seek(percent) {
  closePanels();
  setChromeVisible(true);
  scheduleChromeHide();
  if (format === "epub") {
    if (!state.locations || typeof state.locations.cfiFromPercentage !== "function") return;
    const cfi = state.locations.cfiFromPercentage(Math.min(Math.max(percent, 0), 1));
    if (cfi) await state.rendition.display(cfi);
    return;
  }
  state.pdfPage = Math.min(Math.max(Math.round(percent * Math.max(state.pdf.numPages - 1, 1)) + 1, 1), state.pdf.numPages);
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

async function goToEdge(end) {
  if (format === "epub") {
    if (end && state.locations?.cfiFromPercentage) await state.rendition.display(state.locations.cfiFromPercentage(1));
    else if (!end) await state.rendition.display();
    return;
  }
  state.pdfPage = end ? state.pdf.numPages : 1;
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

function onEPUBRelocated(location) {
  state.currentLocation = location;
  const position = location?.start?.cfi;
  if (!position) return;
  const percent = percentFor(position);
  state.percent = Number.isFinite(percent) ? percent : state.savedPercent;
  observe({ position, percent: Number.isFinite(percent) ? percent : undefined });
  updateEPUBProgress(location);
  markActiveTOC(location.start.href);
}

function updateEPUBProgress(location) {
  const percent = percentFor(location?.start?.cfi);
  const displayPercent = Number.isFinite(percent) ? percent : state.savedPercent;
  state.percent = displayPercent;
  document.querySelector("#reader-progress").textContent = Number.isFinite(displayPercent) ? `${Math.round(displayPercent * 100)}% read` : "Reading";
  document.querySelector("#reader-section").textContent = currentTOCLabel(location?.start?.href) || "Reading";
  document.querySelector("#reader-chapter").textContent = currentTOCLabel(location?.start?.href) || "";
  document.querySelector("#reader-progress-slider").value = String(Math.round(displayPercent * 1000) / 10);
}

function updatePDFProgress() {
  const percent = (state.pdfPage - 1) / Math.max(state.pdf.numPages - 1, 1);
  state.percent = percent;
  document.querySelector("#reader-progress").textContent = `Page ${state.pdfPage} of ${state.pdf.numPages}`;
  document.querySelector("#reader-section").textContent = "PDF";
  document.querySelector("#reader-chapter").textContent = "";
  document.querySelector("#reader-progress-slider").value = String(Math.round(percent * 1000) / 10);
}

function previewProgress(percent) {
  document.querySelector("#reader-progress").textContent = format === "pdf" ? `Page ${Math.round(percent * Math.max(state.pdf.numPages - 1, 1)) + 1} of ${state.pdf.numPages}` : `${Math.round(percent * 100)}% read`;
}

function renderTOC(items, activate) {
  const list = document.querySelector("#toc-list");
  list.replaceChildren();
  appendTOCItems(list, items, activate);
}

function appendTOCItems(parent, items, activate) {
  for (const item of items || []) {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "reader-toc-item";
    button.textContent = item.label || item.title || "Section";
    button.dataset.href = item.href || "";
    button.addEventListener("click", async () => { await activate(item); closePanels(); setChromeVisible(true); scheduleChromeHide(); });
    li.append(button);
    if (item.subitems?.length) {
      const nested = document.createElement("ol");
      appendTOCItems(nested, item.subitems, activate);
      li.append(nested);
    }
    parent.append(li);
  }
}

function markActiveTOC(href) {
  const key = tocKey(href);
  let active = null;
  for (const button of document.querySelectorAll(".reader-toc-item")) {
    const matches = key && tocKey(button.dataset.href) === key;
    button.classList.toggle("is-active", matches);
    if (matches) active = button;
  }
  active?.scrollIntoView({ block: "nearest" });
}

function currentTOCLabel(href) {
  const key = tocKey(href);
  if (!key) return "";
  const button = [...document.querySelectorAll(".reader-toc-item")].find(item => tocKey(item.dataset.href) === key);
  return button?.textContent || "";
}

function tocKey(value) { return String(value || "").split("#")[0].split("?")[0].split("/").pop(); }

function percentFor(cfi) {
  if (!state.locations || typeof state.locations.percentageFromCfi !== "function") return undefined;
  const value = state.locations.percentageFromCfi(cfi);
  return Number.isFinite(value) ? value : undefined;
}

function applyEPUBStyles() {
  if (format !== "epub" || !state.rendition) return;
  const dark = document.documentElement.classList.contains("dark");
  const preferences = state.preferences;
  state.rendition.themes.fontSize(`${preferences.fontSize}%`);
  state.rendition.themes.default({
    html: { height: "100%" },
    body: {
      "box-sizing": "border-box",
      color: dark ? "#f5f5f4" : "#1c1917",
      background: dark ? "#0c0a09" : "#fafaf9",
      "min-height": "100%",
      "line-height": String(preferences.lineHeight),
      "max-width": `${preferences.textWidth}rem`,
      margin: "0 auto",
    },
  });
  updateSettingsUI();
}

async function setEPUBFlow(flow) {
  if (format !== "epub" || !state.rendition || !["paginated", "scrolled"].includes(flow)) return;
  state.preferences.flow = flow;
  savePreferences();
  try {
    state.rendition.flow(flow === "scrolled" ? "scrolled-doc" : "paginated");
    await state.rendition.display(state.currentLocation?.start?.cfi || undefined);
  } catch (error) {
    console.warn("Could not change reading mode", error);
  }
}

function updatePreference(key, value, apply) {
  state.preferences[key] = value;
  savePreferences();
  updateSettingsUI();
  Promise.resolve(apply()).catch(error => console.warn("Could not apply reader setting", error));
}

function adjustFont(delta) {
  const value = Math.min(Math.max(state.preferences.fontSize + delta, 75), 180);
  updatePreference("fontSize", value, applyEPUBStyles);
}

function resetPreferences() {
  state.preferences = { ...defaults };
  savePreferences();
  updateSettingsUI();
  applyEPUBStyles();
  if (format === "pdf") {
    setPDFTone(defaults.pdfTone);
    renderPDFPage().catch(error => console.warn("Could not reset PDF settings", error));
  }
}

function updateSettingsUI() {
  const preferences = state.preferences;
  setValue("#font-size", preferences.fontSize);
  setValue("#font-size-value", `${preferences.fontSize}%`, true);
  setValue("#line-height", preferences.lineHeight);
  setValue("#line-height-value", preferences.lineHeight.toFixed(1), true);
  setValue("#text-width", preferences.textWidth);
  setValue("#text-width-value", `${preferences.textWidth}rem`, true);
  setValue("#reader-flow", preferences.flow);
  setValue("#pdf-zoom", Math.round(preferences.pdfZoom * 100));
  setValue("#pdf-zoom-value", `${Math.round(preferences.pdfZoom * 100)}%`, true);
  for (const button of document.querySelectorAll("[data-pdf-tone]")) button.classList.toggle("is-active", button.dataset.pdfTone === preferences.pdfTone);
}

function setValue(selector, value, text = false) {
  const element = document.querySelector(selector);
  if (!element) return;
  if (text) element.textContent = value;
  else element.value = value;
}

function setPDFTone(tone, persist = true) {
  const value = pdfTones.includes(tone) ? tone : "paper";
  state.preferences.pdfTone = value;
  app.dataset.pdfTone = value;
  if (persist) savePreferences();
  updateSettingsUI();
}

function toggleTheme() {
  const dark = document.documentElement.classList.toggle("dark");
  storageSet("bookshelf:v1:theme", dark ? "dark" : "light");
  applyEPUBStyles();
  updateThemeButton();
}

function updateThemeButton() {
  const dark = document.documentElement.classList.contains("dark");
  const button = document.querySelector("#theme");
  if (!button) return;
  button.textContent = dark ? "Light" : "Dark";
  button.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
  button.title = dark ? "Switch to light theme" : "Switch to dark theme";
}

function togglePanel(idToToggle) {
  const panel = document.querySelector(`#${idToToggle}`);
  if (!panel) return false;
  const willOpen = panel.hidden;
  closePanels();
  if (!willOpen) return false;
  panel.hidden = false;
  document.querySelector("#reader-backdrop").hidden = false;
  document.querySelector("#reader-app").classList.add("reader-panel-open");
  document.querySelector(`#${idToToggle === "toc" ? "toc-toggle" : "settings-toggle"}`).setAttribute("aria-expanded", "true");
  setChromeVisible(true);
  panel.querySelector("button, input, select")?.focus();
  return true;
}

function closePanels() {
  const toc = document.querySelector("#toc");
  const settings = document.querySelector("#reader-settings");
  const wasOpen = !toc.hidden || !settings.hidden;
  toc.hidden = true;
  settings.hidden = true;
  document.querySelector("#reader-backdrop").hidden = true;
  document.querySelector("#reader-app").classList.remove("reader-panel-open");
  document.querySelector("#toc-toggle").setAttribute("aria-expanded", "false");
  document.querySelector("#settings-toggle").setAttribute("aria-expanded", "false");
  if (wasOpen) scheduleChromeHide();
  return wasOpen;
}

function setChromeVisible(visible) {
  app.classList.toggle("reader-chrome-hidden", !visible);
  if (visible) scheduleChromeHide();
}

function toggleChrome() {
  if (!closePanels()) setChromeVisible(app.classList.contains("reader-chrome-hidden"));
}

function scheduleChromeHide() {
  clearTimeout(state.hideTimer);
  if (app.classList.contains("reader-panel-open")) return;
  state.hideTimer = setTimeout(() => setChromeVisible(false), 3600);
}

async function toggleFullscreen() {
  try {
    if (document.fullscreenElement || document.webkitFullscreenElement) {
      if (document.exitFullscreen) await document.exitFullscreen();
      else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
      return;
    }
    if (app.requestFullscreen) await app.requestFullscreen();
    else if (app.webkitRequestFullscreen) app.webkitRequestFullscreen();
  } catch (error) {
    console.warn("Could not change fullscreen state", error);
  }
}

function observe(value) {
  state.observed = value;
  state.pending = value;
  clearTimeout(state.timer);
  state.timer = setTimeout(save, 700);
}

async function save(beacon = false) {
  clearTimeout(state.timer);
  if (!state.pending) return;
  const snapshot = state.pending;
  const payload = format === "epub" ? { position: snapshot.position, device_label: deviceLabel() } : { page: snapshot.page, device_label: deviceLabel() };
  if (format === "epub" && Number.isFinite(snapshot.percent)) payload.percent = snapshot.percent;
  if (beacon && navigator.sendBeacon) {
    payload.csrf_token = csrf;
    const sent = navigator.sendBeacon(endpoint, new Blob([JSON.stringify(payload)], { type: "application/json" }));
    if (sent) { state.pending = null; setPhase("saved"); return; }
  }
  if (state.active) return;
  state.active = true;
  setPhase("saving");
  let retryable = true;
  try {
    const response = await fetch(endpoint, { method: "PUT", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(payload) });
    if (!response.ok) { retryable = response.status >= 500; throw new Error(retryable ? "temporary" : "Position was rejected"); }
    state.acknowledged = snapshot;
    if (state.pending === snapshot) state.pending = null;
    state.retry = 0;
    setPhase("saved");
  } catch (error) {
    if (retryable && state.retry < 3) { const delay = 500 * 2 ** state.retry++; setTimeout(save, delay); }
    else setPhase("failed");
  } finally {
    state.active = false;
    if (state.pending && state.pending !== snapshot) save();
  }
}

function setPhase(phase) {
  state.phase = phase;
  const sync = document.querySelector("#sync-state");
  if (!sync) return;
  sync.textContent = phase[0].toUpperCase() + phase.slice(1);
  sync.dataset.state = phase;
}

function setProgressMessage(message) {
  const progress = document.querySelector("#reader-progress");
  if (progress) progress.textContent = message;
}

function setLoading(message) {
  const loading = document.querySelector("#reader-loading-text");
  if (loading) loading.textContent = message;
}

function savePreferences() { storageSet(preferenceKey, JSON.stringify(state.preferences)); }

function loadPreferences() {
  const raw = storageGet(preferenceKey);
  let stored = {};
  if (raw) {
    try { stored = JSON.parse(raw) || {}; } catch { stored = {}; }
  }
  if (stored.fontSize === undefined) stored.fontSize = boundedNumber(storageGet("bookshelf:v1:epub-font"), defaults.fontSize, 75, 180);
  if (stored.pdfZoom === undefined) stored.pdfZoom = boundedNumber(storageGet("bookshelf:v1:pdf-zoom"), defaults.pdfZoom, 0.5, 3);
  if (stored.pdfTone === undefined) stored.pdfTone = pdfTones.includes(storageGet("bookshelf:v1:pdf-tone")) ? storageGet("bookshelf:v1:pdf-tone") : defaults.pdfTone;
  return {
    fontSize: boundedNumber(stored.fontSize, defaults.fontSize, 75, 180),
    lineHeight: boundedNumber(stored.lineHeight, defaults.lineHeight, 1.2, 2),
    textWidth: boundedNumber(stored.textWidth, defaults.textWidth, 32, 80),
    flow: ["paginated", "scrolled"].includes(stored.flow) ? stored.flow : defaults.flow,
    pdfZoom: boundedNumber(stored.pdfZoom, defaults.pdfZoom, 0.5, 3),
    pdfTone: pdfTones.includes(stored.pdfTone) ? stored.pdfTone : defaults.pdfTone,
  };
}

function boundedNumber(raw, fallback, min, max) {
  const value = Number(raw);
  return Number.isFinite(value) && value >= min && value <= max ? value : fallback;
}

function storageGet(key) { try { return localStorage.getItem(key); } catch { return null; } }
function storageSet(key, value) { try { localStorage.setItem(key, value); } catch { /* Preferences are optional. */ } }
function storageRemove(key) { try { localStorage.removeItem(key); } catch { /* Preferences are optional. */ } }

function fail(error) {
  console.error(error);
  app?.classList.remove("is-loading");
  setPhase("failed");
  setLoading("This book could not be opened.");
}

function deviceLabel() {
  const ua = navigator.userAgent;
  const brands = navigator.userAgentData?.brands?.map(item => item.brand).join(" ") || "";
  const platform = navigator.userAgentData?.platform || "";
  const browser = /Edge|Microsoft Edge/i.test(brands) || /Edg\//.test(ua) ? "Edge" : /Firefox/i.test(brands) || /Firefox\//.test(ua) ? "Firefox" : /Chrom/i.test(brands) || /CriOS|Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Other";
  const source = `${platform} ${ua}`;
  const os = /Android/i.test(source) ? "Android" : /iOS|iPad|iPhone|iPod/i.test(source) ? "iOS/iPadOS" : /macOS|Mac OS X/i.test(source) ? "macOS" : /Windows/i.test(source) ? "Windows" : /Linux/i.test(source) ? "Linux" : "Other";
  return `${browser} on ${os}`.slice(0, 100);
}
