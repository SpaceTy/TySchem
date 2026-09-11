# AGENTS.md

Single Go binary that serves a vanilla-JS SPA and a .litematic schematic library API. No JavaScript build step, no npm in the app.

## Commands

Run from repo root (Makefile `cd`s into `backend/`):

- `make dev` — run backend with live source (`go run .`)
- `make build` — build `backend/tyschem`
- `make run` — build then run the binary
- `make tidy` — `go mod tidy`
- `go test ./...` from `backend/` — no test files exist; add tests as `*_test.go` in `backend/`

There is no lint/format config; use standard `gofmt` / `go vet`.

## Layout

- `backend/` — package `main`, Go 1.21. Entrypoint `main.go`; store/models in `schematics.go` + `accounts.go`; HTTP handlers in `*_handlers.go`. Routes are dispatched manually in `register*Routes` (no router library).
- `frontend/` — served as-is. `index.html` + `app.js` (single-file vanilla SPA, hash routing) + `style.css`. Third-party libs (`three.min.js`, `lodestone.umd.js`, `default-pack/`) are vendored and loaded via `<script>`; do not add a bundler.
- `data/` — runtime only, gitignored. SQLite `schematics.db` plus `files/<id>.litematic` blobs.
- `designreference/` — gitignored Vue/Tailwind mockup, NOT part of the built app. Do not edit or wire it in.

## Runtime facts

- Config via env: `PORT` (default 8080), `FRONTEND_DIR` (default `../frontend`), `DATA_DIR` (default `../data`). Defaults assume the process runs from `backend/`.
- `github.com/mattn/go-sqlite3` requires cgo: `CGO_ENABLED=1` and a C compiler must be available.
- Schema is created/migrated at startup (`ensureColumn`); adding a column means updating both the `schema` string and the migration guard.
- WAL mode with `SetMaxOpenConns(1)` intentionally serializes DB access.

## API

- Auth is one endpoint: `POST /api/auth` logs in when the username exists, otherwise registers (200 vs 201). Session token in an HttpOnly cookie; only its SHA-256 hash is stored.
- Schematics: `GET/POST /api/schematics`, `GET/PUT/PATCH/DELETE /api/schematics/{id}`, `GET /api/schematics/{id}/file` (alias `/download`). List filters: `q,name,description,owner=me,sort,order,from,to,limit,offset,page,pageSize` (page/pageSize are 1-based aliases; response includes `total,totalPages`). The frontend infinite-scrolls filtered pages instead of paginating client-side.
- Uploads are multipart, max 50 MiB, and must start with the gzip magic bytes `1f 8b`.

## Conventions

- No comments in code unless they explain non-obvious intent; the existing files follow this.
- Build artifacts and `data/` are gitignored — never commit them.
