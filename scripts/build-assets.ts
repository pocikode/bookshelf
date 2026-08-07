import { copyFile, mkdir, rm, stat, writeFile } from "node:fs/promises";

const dist = "internal/web/dist";
await rm(dist, { recursive: true, force: true });
await mkdir(dist, { recursive: true });
await writeFile(`${dist}/.gitkeep`, "");

async function run(args: string[]) {
  const proc = Bun.spawn(args, { stdout: "inherit", stderr: "inherit" });
  const code = await proc.exited;
  if (code !== 0) throw new Error(`${args.join(" ")} failed with ${code}`);
}

await run(["bunx", "@tailwindcss/cli", "-i", "assets/css/app.css", "-o", `${dist}/app.css`, "--minify"]);
await run(["bun", "build", "assets/js/reader.js", "assets/js/upload.js", "--outdir", dist, "--target", "browser", "--minify"]);
// pdf.worker.mjs is already a standalone browser ESM. Re-bundling it with
// Bun produces a worker that loads but never completes pdf.js's handshake.
await copyFile("node_modules/pdfjs-dist/build/pdf.worker.mjs", `${dist}/pdf.worker.js`);

for (const output of ["app.css", "reader.js", "upload.js", "pdf.worker.js"]) await stat(`${dist}/${output}`);
if ((await stat(`${dist}/app.css`)).size < 3000) throw new Error("generated app.css is unexpectedly small");
