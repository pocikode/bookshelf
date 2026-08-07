import * as pdfjsLib from "pdfjs-dist";

const form = document.querySelector("#upload-form");
if (form) {
  const input = form.querySelector("#books");
  const dropzone = form.querySelector("#upload-dropzone");
  const summary = form.querySelector("#upload-file-summary");
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
  for (const file of files.filter(isSupported).slice(0, 20)) transfer.items.add(file);
  form.querySelector("#books").files = transfer.files;
  updateFileSummary();
}

function updateFileSummary() {
  const files = [...form.querySelector("#books").files];
  const invalid = files.some(file => !isSupported(file));
  summary.textContent = invalid ? "EPUB or PDF files only" : files.length ? `${files.length} book${files.length === 1 ? "" : "s"} selected` : "No books selected yet";
}

function isSupported(file) { return /\.(epub|pdf)$/i.test(file.name); }

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
