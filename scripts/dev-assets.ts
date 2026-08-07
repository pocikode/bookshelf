import { mkdir } from "node:fs/promises";

await mkdir("internal/web/dist", { recursive: true });

const commands = [
  ["bunx", "@tailwindcss/cli", "-i", "assets/css/app.css", "-o", "internal/web/dist/app.css", "--watch"],
  ["bun", "build", "assets/js/reader.js", "assets/js/upload.js", "--outdir", "internal/web/dist", "--target", "browser", "--sourcemap", "--watch"],
  ["bun", "build", "node_modules/pdfjs-dist/build/pdf.worker.mjs", "--outfile", "internal/web/dist/pdf.worker.js", "--target", "browser", "--sourcemap", "--watch"]
];

const processes = commands.map(command => Bun.spawn(command, { stdout: "inherit", stderr: "inherit" }));
const stop = () => processes.forEach(process => process.kill());
process.on("SIGINT", stop);
process.on("SIGTERM", stop);
await Promise.all(processes.map(process => process.exited));
