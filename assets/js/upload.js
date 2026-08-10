import * as pdfjsLib from "pdfjs-dist";
import { formatFileSize, isSupported } from "./upload-utils.js";

const form = document.querySelector("#upload-form");
let dropzone;
let dropzoneTitle;
let dropzoneHint;
let fileList;
let summary;
if (form) {
  const input = form.querySelector("#books");
  dropzone = form.querySelector("#upload-dropzone");
  dropzoneTitle = form.querySelector("#upload-dropzone-title");
  dropzoneHint = form.querySelector("#upload-dropzone-hint");
  fileList = form.querySelector("#upload-file-list");
  summary = form.querySelector("#upload-file-summary");
  const dialog = document.querySelector("#upload-dialog");
  pdfjsLib.GlobalWorkerOptions.workerSrc = form.dataset.workerUrl;
  input.addEventListener("change", updateFileSummary);
  dropzone.addEventListener("dragover", event => { event.preventDefault(); dropzone.classList.add("is-dragging"); });
  dropzone.addEventListener("dragleave", () => dropzone.classList.remove("is-dragging"));
  dropzone.addEventListener("drop", event => {
    event.preventDefault();
    dropzone.classList.remove("is-dragging");
    setFiles([...event.dataTransfer.files]);
  });
  form.addEventListener("submit", submit);
  document.querySelector("#upload-open")?.addEventListener("click", () => dialog.showModal());
  document.querySelectorAll("[data-dialog-close]").forEach(button => button.addEventListener("click", () => dialog.close()));
}

const libraryResults = document.querySelector("#library-results");
if (libraryResults) bindLibraryFilters(libraryResults);
window.addEventListener("popstate", () => {
  const results = document.querySelector("#library-results");
  if (results) updateLibraryResults(new URL(window.location.href), results, false);
});

async function submit(event) {
  const input = form.querySelector("#books");
  const files = [...input.files];
  const invalid = files.some(file => !isSupported(file));
  if (invalid || !files.length || files.length > 20) {
    event.preventDefault();
    document.querySelector("#upload-status").textContent = invalid ? "Choose EPUB or PDF files only." : "Choose between 1 and 20 books.";
    return;
  }
  event.preventDefault();
  const status = document.querySelector("#upload-status");
  const submitButton = form.querySelector('button[type="submit"]');
  const body = new FormData();
  submitButton.disabled = true;
  body.set("csrf_token", form.elements.csrf_token.value);
  body.set("category", form.elements.category.value);
  for (const [index, file] of files.entries()) {
    body.set(`books[${index}]`, file, file.name);
    if (file.name.toLowerCase().endsWith(".pdf")) {
      status.textContent = `Preparing cover ${index + 1} of ${files.length}…`;
      try { const cover = await renderCover(file); body.set(`covers[${index}]`, cover, "cover.png"); }
      catch { body.set(`covers[${index}]`, new Blob([]), ""); }
    }
  }
  status.textContent = "Uploading… Keep this page open.";
  try {
    const response = await fetch(form.action, { method: "POST", credentials: "same-origin", headers: { "X-CSRF-Token": form.elements.csrf_token.value, "Accept": "application/json" }, body });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error?.message || "Upload failed");
    location.assign("/");
  } catch (error) { status.textContent = error.message; submitButton.disabled = false; }
}

function setFiles(files) {
  const transfer = new DataTransfer();
  for (const file of files.slice(0, 20)) transfer.items.add(file);
  form.querySelector("#books").files = transfer.files;
  updateFileSummary();
}

function updateFileSummary() {
  const files = [...form.querySelector("#books").files];
  const invalid = files.some(file => !isSupported(file));
  const hasFiles = files.length > 0 && !invalid;
  const count = files.length;
  const label = `${count} book${count === 1 ? "" : "s"}`;

  summary.textContent = invalid ? "Unsupported file type" : hasFiles ? `${label} selected` : "No books selected yet";
  summary.classList.toggle("has-files", hasFiles);
  summary.classList.toggle("has-error", invalid);
  dropzone.classList.toggle("has-files", hasFiles);
  dropzone.classList.toggle("has-error", invalid);
  dropzoneTitle.textContent = invalid ? "Unsupported file type" : hasFiles ? `${label} ready to upload` : "Drop files here or choose from your device";
  dropzoneHint.textContent = invalid ? "Choose EPUB or PDF files only" : hasFiles ? count === 20 ? "20-book limit reached" : "Drop more files or choose again to replace" : "Up to 20 books at a time";
  renderFileList(files);
}

function renderFileList(files) {
  fileList.replaceChildren();
  fileList.hidden = files.length === 0;
  for (const file of files) {
    const item = document.createElement("li");
    const name = document.createElement("span");
    const size = document.createElement("small");
    name.textContent = file.name;
    size.textContent = formatFileSize(file.size);
    item.className = isSupported(file) ? "" : "is-invalid";
    item.append(name, size);
    fileList.append(item);
  }
}

function bindLibraryFilters(results) {
  const filterForm = results.querySelector(".library-filters");
  filterForm.addEventListener("submit", event => {
    event.preventDefault();
    const url = new URL(window.location.href);
    url.search = new URLSearchParams(new FormData(filterForm)).toString();
    url.searchParams.delete("page");
    updateLibraryResults(url, results);
  });
  results.querySelectorAll(".pagination a").forEach(link => link.addEventListener("click", event => {
    event.preventDefault();
    updateLibraryResults(new URL(link.href), results);
  }));
}

async function updateLibraryResults(url, results, pushHistory = true) {
  results.setAttribute("aria-busy", "true");
  try {
    const response = await fetch(url, { headers: { "X-Requested-With": "fetch" }, credentials: "same-origin" });
    if (!response.ok) throw new Error("Unable to update the library");
    const html = await response.text();
    const nextResults = new DOMParser().parseFromString(html, "text/html").querySelector("#library-results");
    if (!nextResults) throw new Error("Invalid library response");
    if (pushHistory) history.pushState({}, "", url);
    results.replaceWith(nextResults);
    bindLibraryFilters(nextResults);
  } catch (error) {
    results.removeAttribute("aria-busy");
    window.dispatchEvent(new CustomEvent("bookshelf:filter-error", { detail: error.message }));
  }
}

async function renderCover(file) {
  const pdf = await pdfjsLib.getDocument({ data: await file.arrayBuffer() }).promise;
  const page = await pdf.getPage(1);
  const base = page.getViewport({ scale: 1 });
  const viewport = page.getViewport({ scale: 400 / base.width });
  const canvas = document.createElement("canvas");
  canvas.width = Math.ceil(viewport.width); canvas.height = Math.ceil(viewport.height);
  await page.render({ canvasContext: canvas.getContext("2d", { alpha: false }), viewport }).promise;
  return new Promise((resolve, reject) => canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error("cover failed")), "image/png"));
}
