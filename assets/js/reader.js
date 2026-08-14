import ePub from "epubjs";
import * as pdfjsLib from "pdfjs-dist";
import { bookmarkLabel, boundedNumber, columnCount, deviceLabel as formatDeviceLabel, normalizePDFPage as normalizePage, tocKey } from "./reader-utils.js";

const app = document.querySelector("#reader-app");
const id = Number(app?.dataset.bookId);
const format = app?.dataset.format;
const csrf = document.querySelector('meta[name="csrf-token"]')?.content || "";
const endpoint = `/api/books/${id}/progress`;
const pdfTones = ["paper", "sepia", "light"];
const preferenceKey = `bookshelf:v2:reader:${id}`;
const layoutKey = "bookshelf:v1:reader-layout";
const defaults = { fontSize: 100, lineHeight: 1.5, textWidth: 52, flow: "paginated", columns: 2, pdfZoom: 1.25, pdfTone: "paper" };
const hoverCapable = window.matchMedia("(hover: hover)").matches;
const state = {
  phase: "booting",
  active: false,
  acknowledged: null,
  pending: null,
  observed: null,
  retry: 0,
  timer: null,
  chromeTimer: null,
  book: null,
  rendition: null,
  pdf: null,
  pdfTextLayers: [],
  pdfPage: 1,
  pdfRenderToken: 0,
  pinchedUntil: 0,
  locations: null,
  currentLocation: null,
  tocItems: [],
  tocFlat: [],
  activeTOC: -1,
  bookmarks: [],
  percent: 0,
  savedPercent: 0,
  panel: "",
  ready: false,
  resizeTimer: null,
  preferences: loadPreferences(),
  layout: loadLayout(),
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
  void loadBookmarks();
  app.classList.remove("is-loading");
  document.querySelector("#reader-loading")?.remove();
  state.ready = true;
  setPhase("ready");
  flashChrome();
}

async function bootEPUB(saved) {
  const viewer = document.querySelector("#epub-viewer");
  viewer.hidden = false;
  const book = ePub(app.dataset.fileUrl, { requestCredentials: true, openAs: "epub" });
  const rendition = book.renderTo("epub-viewer", { width: "100%", height: "100%", spread: "auto" });
  state.book = book;
  state.rendition = rendition;
  installEPUBColumns(rendition);
  loadEPUBLocations(book);
  rendition.flow(state.preferences.flow === "scrolled" ? "scrolled-doc" : "paginated");
  applyEPUBStyles();
  rendition.on("relocated", onEPUBRelocated);
  rendition.on("rendered", attachEPUBContents);
  await rendition.display(saved.position || undefined);
  const navigation = await book.loaded.navigation;
  state.tocItems = navigation.toc || [];
  state.tocFlat = flattenTOC(state.tocItems);
  for (const entry of state.tocFlat) entry.number = spineIndexFor(book, entry.item.href);
  renderTOC();
  if (!state.locations) {
    setProgressMessage("Preparing progress...");
    void prepareEPUBLocations(book);
  }
  for (const contents of rendition.getContents()) attachEPUBContents(contents);
}

function spineIndexFor(book, href) {
  if (!href) return null;
  try {
    const section = book.spine?.get(href);
    return Number.isInteger(section?.index) ? section.index + 1 : null;
  } catch {
    return null;
  }
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
  setPDFTone(state.preferences.pdfTone, false);
  pdfjsLib.GlobalWorkerOptions.workerSrc = app.dataset.workerUrl;
  state.pdf = await pdfjsLib.getDocument({ url: app.dataset.fileUrl, withCredentials: true }).promise;
  state.pdfPage = normalizePage(saved.page || 1, state.pdf.numPages, pdfUsesSpread());
  await renderPDFPage();
  try {
    const outline = await state.pdf.getOutline();
    state.tocItems = outline || [];
    state.tocFlat = flattenTOC(state.tocItems);
    await resolvePDFOutlinePages();
    renderTOC();
    markActivePDFTOC();
  } catch (error) {
    console.warn("Could not load PDF outline", error);
    renderTOC();
  }
}

async function resolvePDFOutlinePages() {
  await Promise.all(state.tocFlat.map(async entry => {
    try {
      const raw = entry.item.dest;
      const destination = typeof raw === "string" ? await state.pdf.getDestination(raw) : raw;
      if (!destination?.[0]) return;
      entry.number = (await state.pdf.getPageIndex(destination[0])) + 1;
    } catch {
      /* An unresolvable outline entry simply shows no page number. */
    }
  }));
}

async function renderPDFPage() {
  const spreadMode = pdfUsesSpread();
  state.pdfPage = normalizePage(state.pdfPage, state.pdf.numPages, spreadMode);
  const pageNumbers = [state.pdfPage];
  if (spreadMode && state.pdfPage > 1 && state.pdfPage < state.pdf.numPages) pageNumbers.push(state.pdfPage + 1);
  const spread = document.querySelector("#pdf-spread");
  const sheets = [...document.querySelectorAll(".pdf-sheet")];
  const token = ++state.pdfRenderToken;
  for (const textLayer of state.pdfTextLayers) textLayer.cancel();
  state.pdfTextLayers = [];
  spread.classList.toggle("is-single", pageNumbers.length === 1);
  sheets[1].hidden = pageNumbers.length === 1;
  const pages = await Promise.all(pageNumbers.map(pageNumber => state.pdf.getPage(pageNumber)));
  if (token !== state.pdfRenderToken) return;
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  /* Measured off the spread rather than the pages on screen so a lone last
     sheet is not suddenly drawn at twice the width of the spread before it. */
  const scale = pdfFitScale(pages[0], spreadMode ? 2 : 1) * state.preferences.pdfZoom;
  spread.style.transform = "";
  await Promise.all(pages.map((page, index) => renderPDFSheet(page, sheets[index], dpr, scale, token)));
  if (token !== state.pdfRenderToken) return;
  markActivePDFTOC();
  updatePDFProgress();
}

/* Zoom reads as a multiple of a page that fits the stage. Without the fit
   factor a phone rendered every sheet far wider than the screen, the stylesheet
   clamped the canvas back down to the stage, and the zoom control moved
   nothing. A stage with room to spare keeps the page at its natural size, so
   this only ever scales down. */
function pdfFitScale(page, sheetCount) {
  const natural = page.getViewport({ scale: 1 }).width;
  const available = stageContentWidth() / sheetCount;
  if (!(available > 0) || !(natural > 0)) return 1;
  return Math.min(1, available / natural);
}

/* clientWidth still counts the stage's own side padding, which the page never
   gets to use. */
function stageContentWidth() {
  const stage = document.querySelector("#reader-stage");
  if (!stage) return 0;
  const style = getComputedStyle(stage);
  return stage.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
}

function clampPDFZoom(zoom) {
  return Math.min(3, Math.max(0.5, zoom));
}

async function renderPDFSheet(page, sheet, dpr, scale, token) {
  const viewport = page.getViewport({ scale: scale * dpr });
  const canvas = sheet.querySelector("canvas");
  const textLayerContainer = sheet.querySelector(".pdf-text-layer");
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
  const textContent = await page.getTextContent();
  if (token !== state.pdfRenderToken) return;
  const textLayer = new pdfjsLib.TextLayer({
    textContentSource: textContent,
    container: textLayerContainer,
    viewport: page.getViewport({ scale: textScale }),
  });
  state.pdfTextLayers.push(textLayer);
  await textLayer.render();
}

/* ---------- controls ---------- */

function installControls() {
  applyLayout();

  on("#sidebar-toggle", "click", toggleSidebar);
  on("#sidebar-close", "click", closeSidebar);
  on("#sidebar-pin", "click", toggleSidebarPin);
  on("#reader-backdrop", "click", () => { closeSidebar(); closePanels(); });
  for (const tab of document.querySelectorAll("[data-sidebar-tab]")) tab.addEventListener("click", () => selectSidebarTab(tab.dataset.sidebarTab));
  installSidebarResize();

  on("#bookmark-toggle", "click", toggleBookmark);
  on("#bookmark-add", "click", toggleBookmark);
  on("#settings-toggle", "click", () => togglePanel("font"));
  on("#color-toggle", "click", () => togglePanel("color"));
  on("#fullscreen", "click", toggleFullscreen);
  for (const tab of document.querySelectorAll("[data-footer-tab]")) {
    tab.addEventListener("click", () => {
      const name = tab.dataset.footerTab;
      if (name === "toc") toggleSidebar();
      else if (name === "fullscreen") toggleFullscreen();
      else togglePanel(name);
    });
  }

  on("#reader-prev", "click", () => navigate(-1));
  on("#reader-next", "click", () => navigate(1));
  on("#reader-prev-zone", "click", () => navigate(-1));
  on("#reader-next-zone", "click", () => navigate(1));
  on("#reader-prev-section", "click", () => navigateSection(-1));
  on("#reader-next-section", "click", () => navigateSection(1));
  on("#reader-prev-section-m", "click", () => navigateSection(-1));
  on("#reader-next-section-m", "click", () => navigateSection(1));

  for (const slider of sliders()) {
    slider.addEventListener("change", event => seek(Number(event.currentTarget.value) / 100));
    slider.addEventListener("input", event => {
      const percent = Number(event.currentTarget.value) / 100;
      syncRange(event.currentTarget, previewProgress(percent));
    });
  }
  for (const jump of jumpInputs()) {
    jump.addEventListener("keydown", event => { if (event.key === "Enter") { event.preventDefault(); applyJump(event.currentTarget.value); event.currentTarget.blur(); } });
    jump.addEventListener("blur", event => { event.currentTarget.value = jumpPlaceholder(); });
  }

  on("#font-size", "input", event => updatePreference("fontSize", Number(event.currentTarget.value), applyEPUBStyles));
  on("#line-height", "input", event => updatePreference("lineHeight", Number(event.currentTarget.value), applyEPUBStyles));
  on("#text-width", "input", event => updatePreference("textWidth", Number(event.currentTarget.value), applyEPUBStyles));
  on("#pdf-zoom", "input", event => updatePreference("pdfZoom", Number(event.currentTarget.value) / 100, () => renderPDFPage()));
  on("#reset-settings", "click", resetPreferences);
  for (const wrap of document.querySelectorAll("[data-range]")) {
    const input = wrap.querySelector('input[type="range"]');
    /* The progress sliders label their bubble with a page or percent, so they
       carry their own listener above rather than the generic numeric one. */
    if (!input || input.id.startsWith("reader-progress-slider")) continue;
    input.addEventListener("input", event => syncRange(event.currentTarget));
  }
  for (const button of document.querySelectorAll("[data-flow]")) button.addEventListener("click", () => setEPUBFlow(button.dataset.flow));
  for (const button of document.querySelectorAll("[data-columns]")) button.addEventListener("click", () => setColumns(Number(button.dataset.columns)));
  for (const button of document.querySelectorAll("[data-theme-mode]")) button.addEventListener("click", () => setTheme(button.dataset.themeMode === "dark"));
  for (const button of document.querySelectorAll("[data-pdf-tone]")) button.addEventListener("click", () => setPDFTone(button.dataset.pdfTone));

  installChromeTriggers();
  document.addEventListener("keydown", handleKeydown);
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") save(true); });
  addEventListener("pagehide", () => save(true));
  document.addEventListener("fullscreenchange", updateFullscreenButton);
  observeStage();
  document.querySelector("#reader-stage").addEventListener("click", event => {
    if (["reader-stage", "pdf-page", "pdf-spread", "pdf-canvas", "pdf-canvas-secondary"].includes(event.target.id)) toggleChrome();
  });
  bindTouchSurface(document.querySelector("#reader-stage"));
  if (format === "pdf") bindPDFPinch(document.querySelector("#reader-stage"));

  updateSettingsUI();
  updateThemeButtons();
  updateFullscreenButton();
  if (!document.fullscreenEnabled && !app.requestFullscreen && !app.webkitRequestFullscreen) {
    for (const button of document.querySelectorAll('#fullscreen, [data-footer-tab="fullscreen"]')) button.hidden = true;
  }
}

function on(selector, event, handler) {
  document.querySelector(selector)?.addEventListener(event, handler);
}

const sliders = () => [...document.querySelectorAll('input[type="range"][id^="reader-progress-slider"]')];
const jumpInputs = () => [...document.querySelectorAll(".reader-jump-input")];

/* ---------- chrome ---------- */

function installChromeTriggers() {
  const header = document.querySelector("#reader-header");
  const footer = document.querySelector("#reader-footer");
  for (const trigger of document.querySelectorAll(".reader-hover-trigger")) {
    trigger.addEventListener("mouseenter", showChrome);
  }
  for (const bar of [header, footer]) {
    bar.addEventListener("mouseleave", event => {
      if (!hoverCapable || event.relatedTarget?.closest?.(".reader-bar")) return;
      hideChrome();
    });
    bar.addEventListener("focusin", showChrome);
  }
}

function showChrome() {
  clearTimeout(state.chromeTimer);
  app.classList.add("chrome-visible");
}

/* Reveal the bars, then retreat — used on load and after a tap so the reader
   learns the chrome is there without it sitting over the page. */
function flashChrome() {
  showChrome();
  state.chromeTimer = setTimeout(() => {
    if (document.querySelector(".reader-bar:hover")) return;
    hideChrome();
  }, 2600);
}

function hideChrome() {
  clearTimeout(state.chromeTimer);
  if (state.panel || (isSidebarOpen() && !app.classList.contains("sidebar-pinned"))) return;
  app.classList.remove("chrome-visible");
  document.activeElement?.closest?.(".reader-bar")?.blur?.();
}

function toggleChrome() {
  if (closePanels()) return;
  if (!app.classList.contains("sidebar-pinned") && closeSidebar()) return;
  if (app.classList.contains("chrome-visible")) hideChrome();
  else showChrome();
}

/* ---------- sidebar ---------- */

function isSidebarOpen() {
  return !document.querySelector("#reader-sidebar").hidden;
}

function toggleSidebar() {
  if (isSidebarOpen()) closeSidebar();
  else openSidebar();
}

function openSidebar() {
  closePanels();
  document.querySelector("#reader-sidebar").hidden = false;
  document.querySelector("#sidebar-toggle").setAttribute("aria-expanded", "true");
  updateBackdrop();
  showChrome();
  applyLayout();
  markPanelControls();
  scrollActiveTOCIntoView();
  return true;
}

function closeSidebar() {
  if (!isSidebarOpen()) return false;
  document.querySelector("#reader-sidebar").hidden = true;
  document.querySelector("#sidebar-toggle").setAttribute("aria-expanded", "false");
  updateBackdrop();
  applyLayout();
  markPanelControls();
  return true;
}

function toggleSidebarPin() {
  state.layout.pinned = !state.layout.pinned;
  saveLayout();
  applyLayout();
  updateBackdrop();
}

function selectSidebarTab(name) {
  for (const tab of document.querySelectorAll("[data-sidebar-tab]")) {
    const active = tab.dataset.sidebarTab === name;
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  }
  for (const panel of document.querySelectorAll("[data-sidebar-panel]")) panel.hidden = panel.dataset.sidebarPanel !== name;
}

function applyLayout() {
  const pinned = state.layout.pinned && hoverCapable && isSidebarOpen();
  app.classList.toggle("sidebar-pinned", pinned);
  app.style.setProperty("--sidebar", `${state.layout.width}rem`);
  const pin = document.querySelector("#sidebar-pin");
  pin.setAttribute("aria-pressed", String(state.layout.pinned));
  pin.title = state.layout.pinned ? "Unpin sidebar" : "Pin sidebar";
  pin.setAttribute("aria-label", pin.title);
}

function updateBackdrop() {
  const covering = isSidebarOpen() && !app.classList.contains("sidebar-pinned");
  document.querySelector("#reader-backdrop").hidden = !covering;
}

function installSidebarResize() {
  const handle = document.querySelector("#sidebar-resize");
  const sidebar = document.querySelector("#reader-sidebar");
  const root = Number.parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  handle.addEventListener("pointerdown", event => {
    event.preventDefault();
    handle.setPointerCapture(event.pointerId);
    const move = pointer => {
      const width = (pointer.clientX - sidebar.getBoundingClientRect().left) / root;
      state.layout.width = Math.min(Math.max(width, 15), 34);
      app.style.setProperty("--sidebar", `${state.layout.width}rem`);
    };
    const stop = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", stop);
      saveLayout();
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", stop);
  });
  handle.addEventListener("keydown", event => {
    const step = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
    if (!step) return;
    event.preventDefault();
    state.layout.width = Math.min(Math.max(state.layout.width + step, 15), 34);
    app.style.setProperty("--sidebar", `${state.layout.width}rem`);
    saveLayout();
  });
}

/* ---------- panels ---------- */

function togglePanel(name) {
  if (state.panel === name) return closePanels();
  closePanels();
  const panel = document.querySelector(`[data-panel="${name}"]`);
  if (!panel) return false;
  closeSidebar();
  panel.hidden = false;
  state.panel = name;
  app.classList.add("panel-open");
  markPanelControls();
  showChrome();
  return true;
}

function closePanels() {
  if (!state.panel) return false;
  document.querySelector(`[data-panel="${state.panel}"]`).hidden = true;
  state.panel = "";
  app.classList.remove("panel-open");
  markPanelControls();
  return true;
}

function markPanelControls() {
  document.querySelector("#settings-toggle").setAttribute("aria-expanded", String(state.panel === "font"));
  document.querySelector("#color-toggle").setAttribute("aria-expanded", String(state.panel === "color"));
  for (const tab of document.querySelectorAll("[data-footer-tab]")) {
    const active = tab.dataset.footerTab === "toc" ? isSidebarOpen() : tab.dataset.footerTab === state.panel;
    tab.classList.toggle("is-active", active);
  }
}

/* ---------- keyboard ---------- */

function handleKeydown(event) {
  if (["INPUT", "SELECT", "TEXTAREA"].includes(event.target.tagName)) return;
  if (event.metaKey || event.ctrlKey || event.altKey) return;
  if (event.key === "Escape") {
    if (closePanels() || closeSidebar()) event.preventDefault();
    else hideChrome();
    return;
  }
  if (event.target.tagName === "BUTTON" && event.key === " ") return;
  const key = event.key.toLowerCase();
  if (key === "t") { toggleSidebar(); event.preventDefault(); return; }
  if (key === "s") { togglePanel("font"); event.preventDefault(); return; }
  if (key === "c") { togglePanel("color"); event.preventDefault(); return; }
  if (key === "f") { toggleFullscreen(); event.preventDefault(); return; }
  if (key === "b") { void toggleBookmark(); event.preventDefault(); return; }
  if (["+", "=", "-", "_"].includes(event.key)) { adjustZoom(event.key === "+" || event.key === "=" ? 1 : -1); event.preventDefault(); return; }
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

/* Swipes only. A tap raises touchend *and* a synthesised click, so toggling the
   chrome from both cancelled itself out and left the bars unreachable on touch
   — the click handler owns that job. */
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
    if (horizontallyPannable()) return;
    if (Math.abs(dx) > 52 && Math.abs(dx) > Math.abs(dy) * 1.35) navigate(dx < 0 ? 1 : -1);
  }, { passive: true });
  target.addEventListener("touchcancel", () => { touch = null; }, { passive: true });
}

/* A sheet zoomed past the stage pans sideways, and that drag is the reader
   moving across the page rather than asking for the next one. */
function horizontallyPannable() {
  if (format !== "pdf") return false;
  const stage = document.querySelector("#reader-stage");
  return !!stage && stage.scrollWidth - stage.clientWidth > 1;
}

/* Pinch to zoom the sheet. The slider is two taps deep inside a sheet panel,
   which is a long way to reach mid-page on a phone, and the browser's own pinch
   scales the chrome along with the book. Rasterising a PDF page per touchmove
   is far too slow to track fingers, so the gesture previews with a transform
   and only re-renders once they lift. */
function bindPDFPinch(stage) {
  const spread = document.querySelector("#pdf-spread");
  const spanOf = touches => Math.hypot(touches[0].clientX - touches[1].clientX, touches[0].clientY - touches[1].clientY);
  let pinch = null;

  stage.addEventListener("touchstart", event => {
    if (event.touches.length !== 2) return;
    const span = spanOf(event.touches);
    if (!span) return;
    pinch = { span, zoom: state.preferences.pdfZoom, ratio: 1 };
  }, { passive: true });

  stage.addEventListener("touchmove", event => {
    if (!pinch || event.touches.length !== 2) return;
    event.preventDefault();
    pinch.ratio = clampPDFZoom(pinch.zoom * (spanOf(event.touches) / pinch.span)) / pinch.zoom;
    spread.style.transform = `scale(${pinch.ratio})`;
  }, { passive: false });

  const settle = () => {
    if (!pinch) return;
    const zoom = clampPDFZoom(pinch.zoom * pinch.ratio);
    pinch = null;
    state.pinchedUntil = Date.now() + 400;
    if (Math.abs(zoom - state.preferences.pdfZoom) < 0.005) { spread.style.transform = ""; return; }
    updatePreference("pdfZoom", zoom, () => renderPDFPage());
  };
  stage.addEventListener("touchend", settle, { passive: true });
  stage.addEventListener("touchcancel", settle, { passive: true });

  /* Lifting two fingers can still synthesise a click, which would otherwise
     turn a zoom into a page turn or hide the chrome. */
  stage.addEventListener("click", event => {
    if (Date.now() >= state.pinchedUntil) return;
    event.preventDefault();
    event.stopPropagation();
  }, true);
}

/* ---------- navigation ---------- */

async function navigate(direction) {
  closePanels();
  if (format === "epub") {
    if (direction > 0) await state.rendition.next();
    else await state.rendition.prev();
    return;
  }
  const next = pdfUsesSpread()
    ? direction > 0
      ? state.pdfPage === 1 ? 2 : state.pdfPage + 2
      : state.pdfPage <= 2 ? 1 : state.pdfPage - 2
    : state.pdfPage + direction;
  const normalized = normalizePage(next, state.pdf.numPages, pdfUsesSpread());
  if (normalized === state.pdfPage) return;
  state.pdfPage = normalized;
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

async function navigateSection(direction) {
  const target = state.activeTOC < 0
    ? direction > 0 ? 0 : -1
    : state.activeTOC + direction;
  const entry = state.tocFlat[target];
  if (!entry) return;
  await goToTOC(entry);
  state.activeTOC = target;
  markActiveTOCIndex(target);
}

async function goToTOC(entry) {
  closePanels();
  if (format === "epub") {
    await state.rendition.display(entry.item.href);
    return;
  }
  const page = entry.number ?? await resolvePDFPage(entry.item.dest);
  if (!page) return;
  state.pdfPage = normalizePage(page, state.pdf.numPages, pdfUsesSpread());
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

async function resolvePDFPage(dest) {
  try {
    const destination = typeof dest === "string" ? await state.pdf.getDestination(dest) : dest;
    if (!destination?.[0]) return null;
    return (await state.pdf.getPageIndex(destination[0])) + 1;
  } catch {
    return null;
  }
}

async function seek(percent) {
  closePanels();
  if (format === "epub") {
    if (!state.locations || typeof state.locations.cfiFromPercentage !== "function") return;
    const cfi = state.locations.cfiFromPercentage(Math.min(Math.max(percent, 0), 1));
    if (cfi) await state.rendition.display(cfi);
    return;
  }
  state.pdfPage = normalizePage(Math.round(percent * Math.max(state.pdf.numPages - 1, 1)) + 1, state.pdf.numPages, pdfUsesSpread());
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

async function goToEdge(end) {
  if (format === "epub") {
    if (end && state.locations?.cfiFromPercentage) await state.rendition.display(state.locations.cfiFromPercentage(1));
    else if (!end) await state.rendition.display();
    return;
  }
  state.pdfPage = end ? normalizePage(state.pdf.numPages, state.pdf.numPages, pdfUsesSpread()) : 1;
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

function applyJump(raw) {
  const value = Number.parseFloat(String(raw).replace(/[^\d.]/g, ""));
  if (!Number.isFinite(value)) return;
  if (format === "pdf") void seek((normalizePage(Math.round(value), state.pdf.numPages, pdfUsesSpread()) - 1) / Math.max(state.pdf.numPages - 1, 1));
  else void seek(Math.min(Math.max(value, 0), 100) / 100);
}

function jumpPlaceholder() {
  return format === "pdf" ? String(state.pdfPage) : `${Math.round(state.percent * 100)}%`;
}

function pdfUsesSpread() {
  return state.preferences.columns >= 2 && window.matchMedia("(min-width: 701px)").matches;
}

/* ---------- table of contents ---------- */

function flattenTOC(items, depth = 0, out = []) {
  for (const item of items || []) {
    out.push({ item, depth, number: null });
    if (item.subitems?.length) flattenTOC(item.subitems, depth + 1, out);
  }
  return out;
}

function renderTOC() {
  const list = document.querySelector("#toc-list");
  list.replaceChildren();
  document.querySelector("#toc-empty").hidden = state.tocFlat.length > 0;
  state.tocFlat.forEach((entry, index) => {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "reader-toc-item";
    button.style.setProperty("--depth", String(entry.depth));
    button.dataset.href = entry.item.href || "";
    button.dataset.index = String(index);
    const label = document.createElement("span");
    label.textContent = entry.item.label?.trim() || entry.item.title?.trim() || "Section";
    button.append(label);
    if (entry.number) {
      const number = document.createElement("small");
      number.textContent = String(entry.number);
      button.append(number);
    }
    button.addEventListener("click", async () => {
      await goToTOC(entry);
      state.activeTOC = index;
      markActiveTOCIndex(index);
      if (!app.classList.contains("sidebar-pinned")) closeSidebar();
    });
    li.append(button);
    list.append(li);
  });
}

function markActiveTOC(href) {
  const key = tocKey(href);
  const index = key ? state.tocFlat.findIndex(entry => tocKey(entry.item.href) === key) : -1;
  state.activeTOC = index;
  markActiveTOCIndex(index);
}

function markActivePDFTOC() {
  if (format !== "pdf" || !state.tocFlat.length) return;
  let index = -1;
  state.tocFlat.forEach((entry, position) => {
    if (entry.number && entry.number <= state.pdfPage) index = position;
  });
  state.activeTOC = index;
  markActiveTOCIndex(index);
}

/* Mirrors readest's "current position" row: a synthetic entry slotted under the
   active chapter so the sidebar shows how far into it the reader has come. */
function markActiveTOCIndex(index) {
  document.querySelector("#toc-current")?.remove();
  let active = null;
  for (const button of document.querySelectorAll(".reader-toc-item")) {
    const matches = Number(button.dataset.index) === index;
    button.classList.toggle("is-active", matches);
    if (matches) active = button;
  }
  if (active) {
    const row = document.createElement("li");
    row.id = "toc-current";
    row.className = "reader-toc-current";
    row.style.setProperty("--depth", String((state.tocFlat[index]?.depth ?? 0) + 1));
    row.innerHTML = '<svg class="icon" aria-hidden="true"><use href="#i-book-open"></use></svg>';
    const label = document.createElement("span");
    label.textContent = "Current position";
    const value = document.createElement("small");
    value.textContent = format === "pdf" ? `p. ${state.pdfPage}` : `${Math.round(state.percent * 100)}%`;
    row.append(label, value);
    active.parentElement.after(row);
  }
  scrollActiveTOCIntoView();
}

function scrollActiveTOCIntoView() {
  if (!isSidebarOpen()) return;
  document.querySelector(".reader-toc-item.is-active")?.scrollIntoView({ block: "nearest" });
}

function currentTOCLabel() {
  const entry = state.tocFlat[state.activeTOC];
  return entry?.item.label?.trim() || entry?.item.title?.trim() || "";
}

/* ---------- bookmarks ---------- */

const bookmarksEndpoint = `/api/books/${id}/bookmarks`;

async function loadBookmarks() {
  try {
    const response = await fetch(bookmarksEndpoint, { credentials: "same-origin" });
    if (!response.ok) throw new Error(`Bookmarks unavailable (${response.status})`);
    state.bookmarks = await response.json();
  } catch (error) {
    /* A book that opens without its bookmarks is still readable, so this stays
       out of the fatal path that boot() takes. */
    console.warn("Could not load bookmarks", error);
    state.bookmarks = [];
  }
  renderBookmarks();
  updateBookmarkButton();
}

/* The spot the reader is looking at, in the same shape the API stores: a CFI for
   EPUB, a page for PDF. */
function currentBookmark() {
  const chapter = currentTOCLabel();
  if (format === "pdf") {
    if (!state.pdf) return null;
    return { position: String(state.pdfPage), page: state.pdfPage, percent: state.percent, label: bookmarkLabel({ format, chapter, page: state.pdfPage }) };
  }
  const position = state.currentLocation?.start?.cfi;
  if (!position) return null;
  const percent = Number.isFinite(state.percent) ? state.percent : 0;
  return { position, page: 0, percent, label: bookmarkLabel({ format, chapter, percent }) };
}

function findBookmarkAt(position) {
  return state.bookmarks.find(item => item.position === position) || null;
}

async function toggleBookmark() {
  const target = currentBookmark();
  if (!target) return;
  const existing = findBookmarkAt(target.position);
  try {
    if (existing) {
      const response = await fetch(`${bookmarksEndpoint}/${existing.id}`, { method: "DELETE", credentials: "same-origin", headers: { "X-CSRF-Token": csrf } });
      if (!response.ok && response.status !== 404) throw new Error(`Could not remove bookmark (${response.status})`);
      state.bookmarks = state.bookmarks.filter(item => item.id !== existing.id);
    } else {
      const response = await fetch(bookmarksEndpoint, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: JSON.stringify({ position: target.position, page: target.page, percent: target.percent, label: target.label }),
      });
      if (!response.ok) throw new Error(`Could not save bookmark (${response.status})`);
      state.bookmarks = [...state.bookmarks, await response.json()];
    }
  } catch (error) {
    console.warn(error);
    return;
  }
  renderBookmarks();
  updateBookmarkButton();
}

async function removeBookmark(bookmark) {
  try {
    const response = await fetch(`${bookmarksEndpoint}/${bookmark.id}`, { method: "DELETE", credentials: "same-origin", headers: { "X-CSRF-Token": csrf } });
    if (!response.ok && response.status !== 404) throw new Error(`Could not remove bookmark (${response.status})`);
  } catch (error) {
    console.warn(error);
    return;
  }
  state.bookmarks = state.bookmarks.filter(item => item.id !== bookmark.id);
  renderBookmarks();
  updateBookmarkButton();
}

function renderBookmarks() {
  const list = document.querySelector("#bookmark-list");
  if (!list) return;
  list.replaceChildren();
  document.querySelector("#bookmark-empty").hidden = state.bookmarks.length > 0;
  for (const bookmark of state.bookmarks) {
    const li = document.createElement("li");
    li.className = "reader-bookmark-row";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "reader-bookmark-item";
    const label = document.createElement("span");
    label.textContent = bookmark.label;
    const meta = document.createElement("small");
    meta.textContent = format === "pdf" ? `p. ${bookmark.page}` : `${Math.round((bookmark.percent || 0) * 100)}%`;
    button.append(label, meta);
    button.addEventListener("click", async () => {
      await goToBookmark(bookmark);
      if (!app.classList.contains("sidebar-pinned")) closeSidebar();
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "reader-bookmark-remove";
    remove.title = "Remove bookmark";
    remove.setAttribute("aria-label", `Remove bookmark ${bookmark.label}`);
    remove.innerHTML = '<svg class="icon" aria-hidden="true"><use href="#i-trash"></use></svg>';
    remove.addEventListener("click", () => void removeBookmark(bookmark));
    li.append(button, remove);
    list.append(li);
  }
}

async function goToBookmark(bookmark) {
  closePanels();
  if (format === "epub") {
    await state.rendition.display(bookmark.position);
    return;
  }
  state.pdfPage = normalizePage(bookmark.page || Number(bookmark.position) || 1, state.pdf.numPages, pdfUsesSpread());
  await renderPDFPage();
  observe({ page: state.pdfPage });
}

function updateBookmarkButton() {
  const button = document.querySelector("#bookmark-toggle");
  if (!button) return;
  const target = currentBookmark();
  const marked = Boolean(target && findBookmarkAt(target.position));
  button.setAttribute("aria-pressed", String(marked));
  button.title = marked ? "Remove this bookmark (B)" : "Bookmark this spot (B)";
  button.setAttribute("aria-label", marked ? "Remove this bookmark" : "Bookmark this spot");
  setText("#bookmark-add", marked ? "Remove this bookmark" : "Bookmark this spot");
}

/* ---------- progress readouts ---------- */

function onEPUBRelocated(location) {
  state.currentLocation = location;
  const position = location?.start?.cfi;
  if (!position) return;
  const percent = percentFor(position);
  state.percent = Number.isFinite(percent) ? percent : state.savedPercent;
  observe({ position, percent: Number.isFinite(percent) ? percent : undefined });
  markActiveTOC(location.start.href);
  updateEPUBProgress();
}

function updateEPUBProgress() {
  const percent = state.percent;
  const known = Number.isFinite(percent);
  const chapter = currentTOCLabel();
  setText("#reader-progress", known ? `${Math.round(percent * 100)}%` : "Reading");
  setText("#reader-remaining", pagesLeftLabel());
  setText("#reader-section", chapter || "Reading");
  setText("#reader-footer-chapter", chapter);
  setText("#detail-progress", known ? `${Math.round(percent * 100)}% read` : "Unknown");
  setText("#detail-section", chapter || "—");
  setSliders(percent);
  syncJumpInputs();
  updateBookmarkButton();
}

function updatePDFProgress() {
  const percent = (state.pdfPage - 1) / Math.max(state.pdf.numPages - 1, 1);
  state.percent = percent;
  const lastPage = pdfUsesSpread() && state.pdfPage > 1 ? Math.min(state.pdfPage + 1, state.pdf.numPages) : state.pdfPage;
  const label = state.pdfPage === lastPage ? String(state.pdfPage) : `${state.pdfPage}-${lastPage}`;
  const remaining = Math.max(state.pdf.numPages - lastPage, 0);
  setText("#reader-progress", `${label} / ${state.pdf.numPages}`);
  setText("#reader-remaining", remaining ? `${remaining} page${remaining === 1 ? "" : "s"} left in book` : "Last page");
  setText("#reader-section", currentTOCLabel() || `Page ${label}`);
  setText("#reader-footer-chapter", currentTOCLabel());
  setText("#detail-progress", `${Math.round(percent * 100)}% read`);
  setText("#detail-section", `Page ${label} of ${state.pdf.numPages}`);
  setSliders(percent);
  syncJumpInputs();
  updateBookmarkButton();
}

/* The bands are covered while the footer is out, so the jump input and the
   slider bubble double as the live readout during a scrub — the role
   readest gives its PageJumpInput. Returns the label for the bubble. */
function previewProgress(percent) {
  if (format !== "pdf") {
    const label = `${Math.round(percent * 100)}%`;
    setText("#reader-progress", label);
    setJumpInputs(label);
    return label;
  }
  const page = normalizePage(Math.round(percent * Math.max(state.pdf.numPages - 1, 1)) + 1, state.pdf.numPages, pdfUsesSpread());
  const lastPage = pdfUsesSpread() && page > 1 ? Math.min(page + 1, state.pdf.numPages) : page;
  setText("#reader-progress", `${page === lastPage ? page : `${page}-${lastPage}`} / ${state.pdf.numPages}`);
  setJumpInputs(String(page));
  return String(page);
}

function setSliders(percent) {
  const value = String(Math.round((Number.isFinite(percent) ? percent : 0) * 1000) / 10);
  for (const slider of sliders()) {
    if (document.activeElement === slider) continue;
    slider.value = value;
    syncRange(slider, jumpPlaceholder());
  }
}

/* Drives readest's pill slider: the fill width, the floating bubble position
   and, unless the bubble carries an icon, the value it shows. */
function syncRange(input, label) {
  const wrap = input.closest("[data-range]");
  if (!wrap) return;
  const min = Number(input.min);
  const max = Number(input.max);
  const position = max > min ? ((Number(input.value) - min) / (max - min)) * 100 : 0;
  wrap.style.setProperty("--pos", String(Math.min(Math.max(position, 0), 100)));
  const bubble = wrap.querySelector(".reader-range-bubble");
  if (!bubble || bubble.classList.contains("reader-range-bubble-icon")) return;
  bubble.textContent = label ?? `${Math.round(Number(input.value))}${wrap.dataset.bubbleSuffix || ""}`;
}

function syncRanges() {
  for (const wrap of document.querySelectorAll("[data-range]")) {
    const input = wrap.querySelector('input[type="range"]');
    if (input) syncRange(input, input.id.startsWith("reader-progress-slider") ? jumpPlaceholder() : undefined);
  }
}

function syncJumpInputs() {
  setJumpInputs(jumpPlaceholder());
}

function setJumpInputs(value) {
  for (const jump of jumpInputs()) {
    if (document.activeElement !== jump) jump.value = value;
  }
}

/* readest's "N pages left in chapter". epub.js reports the page within the
   current section on every relocate; scrolled flow has no page count, so the
   percentage remaining stands in. */
function pagesLeftLabel() {
  const displayed = state.currentLocation?.start?.displayed;
  if (displayed?.total > 0 && displayed.page > 0) {
    const left = Math.max(displayed.total - displayed.page, 0);
    if (!left) return "Last page in chapter";
    return `${left} page${left === 1 ? "" : "s"} left in chapter`;
  }
  if (!Number.isFinite(state.percent)) return "";
  return `${Math.max(Math.round((1 - state.percent) * 100), 0)}% left in book`;
}

function percentFor(cfi) {
  if (!state.locations || typeof state.locations.percentageFromCfi !== "function") return undefined;
  const value = state.locations.percentageFromCfi(cfi);
  return Number.isFinite(value) ? value : undefined;
}

/* ---------- appearance ---------- */

function applyEPUBStyles() {
  if (format !== "epub" || !state.rendition) return;
  const dark = document.documentElement.classList.contains("dark");
  const preferences = state.preferences;
  state.rendition.themes.fontSize(`${preferences.fontSize}%`);
  state.rendition.themes.default({
    html: { height: "100%" },
    body: {
      "box-sizing": "border-box",
      color: dark ? "#f2f0ea" : "#242321",
      /* Same value as --canvas: the page has to be one continuous surface with
         the stage around it, or the iframe box shows as a seam. */
      background: dark ? "#191816" : "#f7f6f3",
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
  updateSettingsUI();
  await applyReadingLayout();
}

async function setColumns(value) {
  state.preferences.columns = boundedNumber(value, defaults.columns, 1, 4);
  savePreferences();
  updateSettingsUI();
  await applyReadingLayout();
}

/* Flow and column count both change how a page is measured, so they share one
   re-layout: hand the rendition its new shape, then land back on the current
   page. A PDF has no flow, only the one-or-two sheet spread. */
async function applyReadingLayout() {
  if (format === "pdf") {
    if (!state.pdf) return;
    await renderPDFPage();
    observe({ page: state.pdfPage });
    return;
  }
  if (!state.rendition) return;
  try {
    state.rendition.flow(state.preferences.flow === "scrolled" ? "scrolled-doc" : "paginated");
    await state.rendition.display(state.currentLocation?.start?.cfi || undefined);
  } catch (error) {
    console.warn("Could not change the reading layout", error);
  }
}

/* epub.js counts columns as a "spread" — one or two, never more. Its layout
   arithmetic is redone here for the column count the reader asked for, the way
   readest's maximum column count works. The rendition builds a fresh Layout
   once the book's metadata lands, so the patch rides on the factory rather
   than on the instance that exists now. */
function installEPUBColumns(rendition) {
  const layoutFor = rendition.layout.bind(rendition);
  rendition.layout = settings => {
    const layout = layoutFor(settings);
    if (settings && layout) patchColumnLayout(layout);
    return layout;
  };
  if (rendition._layout) patchColumnLayout(rendition._layout);
}

function patchColumnLayout(layout) {
  const calculate = layout.calculate.bind(layout);
  layout.calculate = (width, height, gap) => {
    calculate(width, height, gap);
    if (layout.name !== "reflowable" || layout.flow() !== "paginated") return;
    const divisor = columnCount(layout.width, state.preferences.columns);
    if (divisor === layout.divisor) return;
    const columnWidth = divisor > 1 ? layout.width / divisor - layout.gap : layout.width;
    const pageWidth = divisor > 1 ? columnWidth + layout.gap : layout.width;
    const spreadWidth = columnWidth * divisor + layout.gap;
    Object.assign(layout, { columnWidth, pageWidth, spreadWidth, divisor });
    layout.update({ columnWidth, pageWidth, spreadWidth, divisor });
  };
}

function updatePreference(key, value, apply) {
  state.preferences[key] = value;
  savePreferences();
  updateSettingsUI();
  Promise.resolve(apply()).catch(error => console.warn("Could not apply reader setting", error));
}

function adjustFont(delta) {
  updatePreference("fontSize", Math.min(Math.max(state.preferences.fontSize + delta, 75), 180), applyEPUBStyles);
}

function adjustZoom(direction) {
  if (format === "epub") {
    adjustFont(direction * 5);
    return;
  }
  updatePreference("pdfZoom", clampPDFZoom(state.preferences.pdfZoom + direction * 0.05), () => renderPDFPage());
}

function resetPreferences() {
  state.preferences = { ...defaults };
  savePreferences();
  updateSettingsUI();
  applyEPUBStyles();
  if (format === "pdf") setPDFTone(defaults.pdfTone);
  applyReadingLayout().catch(error => console.warn("Could not reset reader settings", error));
}

function updateSettingsUI() {
  const preferences = state.preferences;
  setValue("#font-size", preferences.fontSize);
  setValue("#line-height", preferences.lineHeight);
  setValue("#text-width", preferences.textWidth);
  setValue("#pdf-zoom", Math.round(preferences.pdfZoom * 100));
  for (const button of document.querySelectorAll("[data-flow]")) button.classList.toggle("is-active", button.dataset.flow === preferences.flow);
  for (const button of document.querySelectorAll("[data-columns]")) button.classList.toggle("is-active", Number(button.dataset.columns) === preferences.columns);
  for (const button of document.querySelectorAll("[data-pdf-tone]")) button.classList.toggle("is-active", button.dataset.pdfTone === preferences.pdfTone);
  /* The column picker only means something on a paged surface. */
  if (format === "epub") app.dataset.flow = preferences.flow;
  syncRanges();
}

function setValue(selector, value) {
  const element = document.querySelector(selector);
  if (element) element.value = value;
}

function setText(selector, value) {
  const element = document.querySelector(selector);
  if (element) element.textContent = value;
}

function setPDFTone(tone, persist = true) {
  const value = pdfTones.includes(tone) ? tone : "paper";
  state.preferences.pdfTone = value;
  app.dataset.pdfTone = value;
  if (persist) savePreferences();
  updateSettingsUI();
}

function setTheme(dark) {
  document.documentElement.classList.toggle("dark", dark);
  storageSet("bookshelf:v1:theme", dark ? "dark" : "light");
  applyEPUBStyles();
  updateThemeButtons();
}

function updateThemeButtons() {
  const dark = document.documentElement.classList.contains("dark");
  for (const button of document.querySelectorAll("[data-theme-mode]")) {
    button.classList.toggle("is-active", (button.dataset.themeMode === "dark") === dark);
  }
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

function updateFullscreenButton() {
  const active = Boolean(document.fullscreenElement || document.webkitFullscreenElement);
  const label = active ? "Exit fullscreen" : "Enter fullscreen";
  for (const button of document.querySelectorAll('#fullscreen, [data-footer-tab="fullscreen"]')) {
    button.querySelector("use").setAttribute("href", active ? "#i-contract" : "#i-expand");
    button.title = active ? "Exit fullscreen (F)" : "Fullscreen (F)";
    button.setAttribute("aria-label", label);
  }
}

/* The stage changes width when the sidebar is pinned or resized, not just when
   the window is — epub.js and the PDF canvas both need a nudge either way. */
function observeStage() {
  const stage = document.querySelector("#reader-stage");
  new ResizeObserver(() => {
    if (!state.ready) return;
    clearTimeout(state.resizeTimer);
    state.resizeTimer = setTimeout(handleStageResize, 150);
  }).observe(stage);
}

function handleStageResize() {
  if (format === "pdf" && state.pdf) {
    renderPDFPage().catch(error => console.warn("Could not resize PDF", error));
    return;
  }
  try {
    state.rendition?.resize();
  } catch (error) {
    console.warn("Could not resize the book", error);
  }
}

/* ---------- persistence ---------- */

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
  const label = phase[0].toUpperCase() + phase.slice(1);
  const sync = document.querySelector("#sync-state");
  if (sync) {
    sync.dataset.state = phase;
    sync.querySelector(".sync-label").textContent = label;
    sync.title = `Sync: ${label}`;
  }
  setText("#detail-sync", label);
}

function setProgressMessage(message) {
  setText("#reader-progress", message);
}

function setLoading(message) {
  setText("#reader-loading-text", message);
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
    columns: boundedNumber(stored.columns, defaults.columns, 1, 4),
    pdfZoom: boundedNumber(stored.pdfZoom, defaults.pdfZoom, 0.5, 3),
    pdfTone: pdfTones.includes(stored.pdfTone) ? stored.pdfTone : defaults.pdfTone,
  };
}

function saveLayout() { storageSet(layoutKey, JSON.stringify(state.layout)); }

function loadLayout() {
  let stored = {};
  try { stored = JSON.parse(storageGet(layoutKey) || "{}") || {}; } catch { stored = {}; }
  return { width: boundedNumber(stored.width, 20, 15, 34), pinned: stored.pinned === true };
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
  return formatDeviceLabel({ userAgent: navigator.userAgent, brands: navigator.userAgentData?.brands, platform: navigator.userAgentData?.platform });
}
