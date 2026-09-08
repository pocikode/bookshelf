# Readest Personal Agent Notes

## Ownership and generated files

- This repository wraps the pinned `readest/` Git submodule with a Go service and frontend overlays. Initialize all nested submodules with `git submodule update --init --recursive`.
- Make personal frontend changes in `frontend-overrides/readest-app/`, preserving paths relative to `readest/apps/readest-app/`. Do not make integration changes directly in `readest/` unless intentionally updating the upstream gitlink.
- Never edit `.build/readest/`. `bun run prepare:frontend` mirrors the current `readest/` working tree into `.build/` (deleting anything upstream no longer has), overlays `frontend-overrides/readest-app/`, installs dependencies, and copies vendor assets. Uncommitted changes inside `readest/` can therefore leak into generated builds. The sync deliberately preserves the generated `node_modules/`, `.next/` and `out/` trees so repeat runs stay cheap; run `PREPARE_CLEAN=1 bun run prepare:frontend` to force the from-scratch rebuild, which is worth doing after moving the pinned Readest revision. Where `rsync` is unavailable — notably the `Dockerfile` builder image — the script falls back to a full copy.
- Overrides replace whole files and cannot delete upstream files. Reconcile full-file overrides such as `package.json` and `next.config.mjs` whenever the Readest revision changes.
- For code mirrored from the upstream app, also follow `readest/apps/readest-app/AGENTS.md`; consult its linked i18n, design, safe-area, TTS, and testing docs when touching those areas.

## Commands

- Required toolchains are Bun `1.4.2` and Go `1.27.x`. Install root dependencies with `bun install --frozen-lockfile`.
- CI order is `bun run typecheck`, `bun run lint`, `bun run build:frontend`, `go test ./...`, then `go vet ./...`.
- `bun run typecheck` runs `tsgo --noEmit`; `bun run lint` runs the same check plus Biome, so in a full CI pass the former is subsumed by the latter. Both regenerate `.build/readest/` first; `build:frontend` does too.
- `bun run build:frontend` creates the static site at `.build/readest/apps/readest-app/out/` with the web platform retained and personal/static flags enabled. The personal build turns `productionBrowserSourceMaps` off — unlike the Tauri export, nothing uploads or symbolicates its maps, and `out/` ships verbatim into the image.
- Frontend-only development: run `bun run prepare:frontend`, then `bun --cwd .build/readest/apps/readest-app run dev-web`. Root `.env` values are inherited by Bun; the app command additionally loads `.env.web` from the generated app.
- Frontend unit tests are not in root CI. Run all once with `bun run test:frontend -- --run`; for one file, prepare once and run `bun --cwd .build/readest/apps/readest-app run test -- --run src/path/to/file.test.ts`. This Vitest lane is jsdom and excludes browser, Tauri, Android, and Playwright E2E tests.
- Focus backend verification with `go test ./server/internal/<package>` or `go test ./server/internal/<package> -run '^TestName$'`; compile the entrypoint with `go build ./server/cmd/readest`.

## Architecture and runtime traps

- The production entrypoint is `server/cmd/readest`; `server/internal/httpapi` composes stores and owns `/api/*` routing plus static SPA fallback. Persistent state is `DATA_DIR/app.sqlite` and `DATA_DIR/books/`.
- Normal backend startup loads `.env` from the current working directory. `ENV=production` requires `SESSION_SECRET`; bootstrap credentials create a user only when both are set and the users table is empty.
- The executable schema source is embedded `server/internal/database/schema.sql`. Keep operator-facing `server/migrations/001_init.sql` synchronized; there is no migration-file runner.
- The Go server is the only production process. The root `Dockerfile` builds static Readest output and a CGO-disabled Go binary; the runtime contains neither Bun nor Node. API requests never fall through to the SPA.
- The personal browser API boundary is same-origin `/api/*`; keep calls under `frontend-overrides/readest-app/src/services/personal/` rather than coupling components directly to endpoints.
