# Bookshelf

Bookshelf is a self-hosted Readest integration with the existing Readest web reader and library UI, plus a small Go/SQLite service for a shared library, authentication, ebook storage, and synchronization. Readest is pinned as the `readest/` Git submodule; the parent repository tracks only integration overrides.

The frontend remains the Readest frontend. In personal mode, the library loads from the Go API, EPUB/PDF uploads include extracted metadata and covers when available, and book files are streamed to the browser for the existing Foliate/PDF reader. The production container runs one Go process and contains no Node.js or Bun runtime.

## Current Status

The personal deployment currently supports:

- Session-cookie authentication with CSRF protection and admin/user roles.
- Library listing and search, EPUB/PDF upload, metadata and cover extraction, metadata editing, visibility changes, and deletion.
- Authenticated EPUB/PDF file streaming with range requests and ETags.
- Reading progress synchronization through the personal API.
- Bookmark, highlight, and note synchronization through the personal API.
- User administration for admins.

The standalone `/api/sync` cursor endpoint is available for sync clients, while the current personal reader integration uses the dedicated progress, bookmark, and annotation endpoints directly.

## Local Development

Requirements: Bun 1.4.2 and Go 1.27.1.

```bash
bun install
bun run prepare:frontend
bun run typecheck
bun run lint
bun run build:frontend
go test ./...
go vet ./...
```

Set local values in the root `.env`, then start the backend:

```bash
bun run dev:backend
```

Use `.env.example` as the production configuration template. The root `.env` is ignored by Git. `bun run build:frontend` also loads the root `.env`, so `PERSONAL_STATIC=true` and `NEXT_PUBLIC_PERSONAL_APP=true` do not need to be supplied on the command line.

For frontend-only work, run `bun run prepare:frontend` followed by `bun --cwd .build/readest/apps/readest-app run dev-web`. The personal production build is `bun run build:frontend`; it writes static output to `.build/readest/apps/readest-app/out/`.

`prepare:frontend` syncs `readest/` into `.build/` incrementally and keeps the installed dependencies and Next.js cache between runs, so only the first run pays full price. After bumping the submodule revision, force a clean regeneration with `PREPARE_CLEAN=1 bun run prepare:frontend`.

## Environment Variables

- `DATA_DIR`: persistent data directory, `/data` in Docker. It contains the SQLite database and storage directories described below.
- `PORT`: HTTP port, normally `3000`.
- `SESSION_SECRET`: required when `ENV=production`.
- `ENV`: set to `production` to enable secure cookies.
- `ADMIN_USERNAME` and `ADMIN_PASSWORD`: create the first admin when the database has no admin. They must be set together. Existing pre-role databases promote their oldest account to admin during startup.
- `MAX_UPLOAD_BYTES`: upload limit; default is 512 MiB.
- `STATIC_DIR`: generated frontend directory; default is `./web`.
- `LOG_LEVEL`: `debug`, `info`, `warn`, or `error`; default is `info`.
- `LOG_FORMAT`: `json` or `text`; default is `json`.

The server writes structured logs to stderr. Every request produces one access-log line — at `error` for 5xx, `warn` for 4xx, `info` otherwise — carrying a `requestId` that is also returned in the `X-Request-ID` response header. Failures log the underlying cause; clients only receive a generic message. Failed login attempts receive an in-memory progressive delay: 250 ms, doubling to a maximum of 8 seconds. A successful login resets the delay, and entries expire after 15 minutes. The delay tracker is process-local and resets when the server restarts.

After the first admin is created, remove the admin password from the runtime environment. User records, roles, and sessions are stored in SQLite. All pages except login and its OAuth callback are authenticated.

## Docker Compose

Set the production values in `.env` beside `compose.yaml` using `.env.example` as the template, then run:

```bash
docker compose up -d
curl http://127.0.0.1:3000/api/health
```

The compose service binds only to `127.0.0.1:3000`. The `./data` volume survives container replacement and should not be exposed as a web root.

## Data Layout

`DATA_DIR` uses relative database paths and content-addressed file storage:

```text
data/
├── bookshelf.db
├── books/
│   └── <first-two-hash-characters>/<sha256>.<epub-or-pdf>
├── covers/
│   └── <first-two-hash-characters>/<sha256>.<image-extension>
├── uploads/    # temporary upload files; normally empty
└── trash/      # reserved for future retention/cleanup workflows
```

Book files are SHA-256 addressed and sharded by their first two hash characters. Covers use the same hash and shard as their book. Original filenames are stored as metadata and are not used as filesystem paths. Do not rename or delete files manually while they are referenced by the database.

## Upload and Reading

Open the app behind the configured host, sign in with the bootstrap account, and use the existing Readest library import control. The personal API endpoints are available at:

- `GET /api/health`
- `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/password`, `GET /api/auth/me`
- `GET /api/users`, `POST /api/users`, `DELETE /api/users/:id` (admin only)
- `GET /api/books`, `POST /api/books`, `GET /api/books/:id`, `PUT /api/books/:id`, `DELETE /api/books/:id`
- `GET /api/books/:id/file`
- `GET /api/books/:id/cover`
- `GET` and `PUT /api/books/:id/progress`
- `GET`, `PUT`, and `DELETE /api/books/:id/bookmarks`
- `GET`, `PUT`, and `DELETE /api/books/:id/annotations`
- `GET` and `POST /api/sync`

The personal integration uses the existing Readest library import control and reader seams. Native/Tauri import behavior remains upstream functionality; personal browser uploads go through the same-origin Go API.

## Nginx

Use the example in `nginx/readest-personal.conf` and proxy to `http://127.0.0.1:3000`. Nginx terminates HTTPS. Set `client_max_body_size` high enough for the largest ebook you plan to upload, for example `512M`.

Do not expose the container port publicly and do not mount `/data` into a public web root.

## GHCR

The workflow in `.github/workflows/ci.yml` runs Bun install with the frozen lockfile, frontend typecheck/lint/build, Go tests/vet, and builds the container image on pushes. Publishing to `ghcr.io/<owner>/bookshelf` is limited to version tags such as `v0.1.0` and only runs when a `GHCR_TOKEN` secret is configured with package write access.

## Backup

Back up the database and stored files while the service is stopped or during a filesystem snapshot. Stopping the service lets SQLite checkpoint its WAL files before the archive is created:

```bash
docker compose stop
tar -czf "bookshelf-backup-$(date +%Y%m%d).tar.gz" -C data bookshelf.db books covers
docker compose start
```

The `uploads/` directory is temporary and normally does not need to be backed up. Include it only if an upload is actively being recovered. The `trash/` directory is reserved and is currently not part of the deletion workflow.

Do not commit `data/`, ebook binaries, session secrets, or local environment files.

## Upgrade

1. Back up `data/`.
2. Pull the new image.
3. Run `docker compose up -d`.
4. Verify `/api/health`, login, the library, and a previously opened book.

SQLite migrations run at startup. The Readest revision and Graphify preservation notes are recorded in `ARCHITECTURE.md`.

## Readest Submodule

Initialize the pinned Readest revision after cloning:

```bash
git submodule update --init --recursive
```

The parent repository's frontend changes are intentionally limited to `frontend-overrides/readest-app/`. Run `bun run prepare:frontend` after changing the submodule revision or an override.

## Graphify

The selected Readest revision's `readest/apps/readest-app/graphify-out/` artifacts are preserved by the submodule checkout when present. Graphify is developer-time repository analysis, not an application runtime dependency. Its machine-specific intermediate paths should not be treated as portable deployment data.

## License

This project is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE).

It builds on [Readest](https://github.com/readest/readest), which is also AGPL-3.0 licensed. The generated frontend in `.build/readest/` combines upstream Readest sources with the overrides in `frontend-overrides/readest-app/`, so distributed builds are derivative works and remain under the AGPL-3.0. Copyright for the upstream code stays with the Readest authors.

Because the AGPL's network clause applies, any publicly reachable deployment must offer its users the corresponding source of the running version.
