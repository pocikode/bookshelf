# Readest Personal Architecture

## Revision and Fork Boundary

This project uses Readest revision `9f7daf813e075a41eba67e8ce377bad421088dd7` (Readest app version `0.12.1`) as the pinned `readest/` Git submodule. The parent repository does not copy or track the Readest source tree.

The browser reader dependencies are supplied by the Readest submodule:

- `readest/packages/foliate-js/` at submodule revision `f65836f7`
- `readest/packages/js-mdict/` at submodule revision `d01bf62a`
- `readest/packages/simplecc-wasm/` at submodule revision `5e5b56f`

The original Readest checkout and its `graphify-out/` remain untouched outside this repository. Graphify is build-time/developer-time analysis only: no application source imports it, and production does not depend on Python or Graphify.

## Runtime Shape

```text
Browser
  | same-origin /api/* and static assets
  v
Nginx on host, TLS termination
  |
  v
One Docker container on 127.0.0.1:3000
  |- Go net/http server
  |- generated Readest frontend assets
  |- SQLite /data/app.sqlite
  `- ebook files /data/books/
```

The Go server is the only production application server. It serves API routes under `/api/`, streams authenticated book files, and falls back to the frontend entry point for client-side routes. Nginx is outside the image. The backend currently has the core auth, upload, metadata, progress, annotation, and sync primitives; the remaining full Readest data synchronization and mutation adapters are intentionally isolated as follow-up integration work.

## Frontend Baseline

Readest is a Next.js 16 + React 19 application with both App Router and Pages Router routes. The important runtime paths are:

- `readest/apps/readest-app/src/app/library/page.tsx` and its library components provide the existing library UI.
- `src/pages/reader/[ids].tsx` is the normal web reader entry.
- `src/app/reader/page.tsx` is the App Router reader entry used by the Tauri/PWA-style path.
- `src/app/reader/components/Reader.tsx` and `FoliateViewer.tsx` retain EPUB/PDF rendering in the browser.
- `src/services/webAppService.ts` stores browser files and per-book JSON in IndexedDB.
- `src/store/bookDataStore.ts` and `src/store/readerProgressStore.ts` hold local progress/config state.
- `BookConfig.location` and `BookProgress.location` carry Readest's CFI locator representation.
- `BookConfig.booknotes` stores bookmarks and annotations as `BookNote` records. Bookmark notes use `type: 'bookmark'`; highlights and notes use Readest's annotation representation and soft deletion.

The personal integration must adapt these existing records rather than introduce a competing reader model. The backend stores locators as opaque JSON or the equivalent serialized Readest payload and does not render EPUB/PDF content. Remote book URL materialization currently uses the server file endpoint; complete local IndexedDB caching and native Readest import/upload translation remain follow-up work.

## Web Build and Static Serving

The upstream web build uses a normal Next server/OpenNext worker. The Tauri build already opts into `output: 'export'`, but its platform flag changes runtime behavior. The personal fork uses a dedicated static build flag so the web platform remains enabled while the generated output is static. Any remaining server-only Next API behavior is outside the personal runtime and must not be needed by the personal frontend.

The Go server owns SPA fallback. A request for `/library`, `/reader/<id>`, or `/settings` is served the generated frontend entry when no matching static file exists. API requests never fall through to the frontend.

The browser API boundary is same-origin `/api/*`. The personal API client is kept under `apps/readest-app/src/services/personal/`; raw personal API calls are not scattered through reader components.

## Personal Backend

The backend lives under `server/` in the root Go module. It uses `net/http`, `database/sql`, and a pure-Go SQLite driver. `scripts/apply-readest-overrides.sh` copies the pinned submodule into ignored `.build/readest/`, applies only the tracked files in `frontend-overrides/`, installs its frontend dependencies, and prepares generated browser vendor assets. Packages are separated by responsibility:

- `internal/config`: environment and deployment configuration.
- `internal/database`: SQLite initialization, connection settings, and embedded migrations.
- `internal/auth`: password hashing, sessions, cookies, and CSRF checks.
- `internal/storage`: server-controlled ebook paths, upload validation, hashing, and file access.
- `internal/books`: book metadata and filesystem/database coordination.
- `internal/progress`: Readest-compatible locator and progress persistence.
- `internal/annotations`: bookmarks, highlights, notes, and soft deletion.
- `internal/sync`: cursor-based change log and latest-version conflict policy.
- `internal/httpapi`: routing, authentication middleware, JSON responses, streaming, and static fallback.

SQLite is initialized at `/data/app.sqlite` and uses WAL mode, foreign keys, and a five-second busy timeout. The only persistent directory is `/data`; binaries are never stored in SQLite.

## Data and Sync Model

The initial schema contains `users`, `sessions`, `books`, `reading_progress`, `bookmarks`, `annotations`, `devices`, and `sync_changes`. User-owned records include `user_id` even though the initial deployment is single-user.

Mutable entities have server-assigned versions. Changes are appended to `sync_changes` in the same transaction as the entity mutation. The client sends a device ID and cursor, and the server returns changes after that cursor. Stale writes are resolved by the server's version ordering and return current state where a conflict is meaningful; this is deliberately not a CRDT.

Progress is local-first in the browser and sent through a debounce/lifecycle-aware adapter. Bookmarks and annotations retain the Readest locator, selected text, note text, style/color metadata where available, timestamps, versions, and deletion state.

## Authentication and Security

Login creates a server-side session represented by an HTTP-only cookie. Session tokens are stored hashed. Cookies are `Secure` in production and use `SameSite=Lax`. State-changing cookie-authenticated requests require a CSRF token and same-origin checks where configured.

Uploads are bounded, written to a temporary file, validated as EPUB/PDF, hashed, renamed using a server-generated ID, and then recorded in SQLite. Client filenames are metadata only. File endpoints use the database row's server-controlled path and support range streaming, ETag, length, content type, and last-modified headers.

## Maintainability Rules

Upstream reader internals, themes, visual components, Foliate integration, and PDF/EPUB rendering remain unchanged unless an integration seam requires a small adapter. Personal API code should remain isolated from Readest's existing cloud provider code so upstream updates can be merged without carrying a backend rewrite.

Graphify artifacts remain available for repository analysis. Because the submodule's intermediate Graphify files contain machine-specific absolute paths and the report is tied to the selected upstream revision, they are documentation/audit artifacts rather than runtime inputs.
