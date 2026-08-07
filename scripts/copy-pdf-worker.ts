import { copyFile } from "node:fs/promises";

await copyFile("node_modules/pdfjs-dist/build/pdf.worker.mjs", "internal/web/dist/pdf.worker.js");
