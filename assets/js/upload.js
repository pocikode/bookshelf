import * as pdfjsLib from "pdfjs-dist";

const form = document.querySelector("#upload-form");
if (form) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = form.dataset.workerUrl;
  form.addEventListener("dragover", event => { event.preventDefault(); form.classList.add("is-dragging"); });
  form.addEventListener("dragleave", () => form.classList.remove("is-dragging"));
  form.addEventListener("drop", event => { event.preventDefault(); form.classList.remove("is-dragging"); document.querySelector("#books").files = event.dataTransfer.files; });
  form.addEventListener("submit", submit);
}

async function submit(event) {
  const files = [...document.querySelector("#books").files];
  if (!files.length || files.length > 20) return;
  event.preventDefault();
  const status = document.querySelector("#upload-status");
  const body = new FormData();
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
  } catch (error) { status.textContent = error.message; }
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
