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

To exercise the API against a local server, run the server and the `curl`
checks inside a single self-contained script that kills the server before it
exits. Do not start it with a bare trailing `&` (or `nohup`/`setsid`) in a
standalone tool call: the shell waits on the background process's open
stdout/stderr and the call hangs until it times out. Point `DATA_DIR` and
`CONFIG_FILE` at temp paths so a test run never touches the repo's `data/` or
`config.toml`, e.g.:

```sh
CONFIG_FILE=/tmp/t.toml FRONTEND_DIR=frontend DATA_DIR=$(mktemp -d) PORT=18080 ./tyschem &
srv=$!
# ... curl ...
kill "$srv"
```

## Layout

- `backend/` — package `main`, Go 1.21. Entrypoint `main.go`; store/models in `schematics.go` + `accounts.go`; config in `config.go`; HTTP handlers in `*_handlers.go` (admin routes in `admin_handlers.go`). Routes are dispatched manually in `register*Routes` (no router library).
- `frontend/` — served as-is. `index.html` + `app.js` (single-file vanilla SPA, hash routing) + `style.css`. Third-party libs (`three.min.js`, `lodestone.umd.js`, `default-pack/`) are vendored and loaded via `<script>`; do not add a bundler.
- `data/` — runtime only, gitignored. SQLite `schematics.db` plus `files/<id>.litematic` blobs.
- `config.example.toml` — committed template; copy to `config.toml` (gitignored) to set the port and admin account.
- `designreference/` — gitignored Vue/Tailwind mockup, NOT part of the built app. Do not edit or wire it in.

## Runtime facts

- Config via env: `CONFIG_FILE` (default `../config.toml`), `PORT` (overrides the config file), `FRONTEND_DIR` (default `../frontend`), `DATA_DIR` (default `../data`). Defaults assume the process runs from `backend/`.
- `config.toml` is TOML: `port` plus `[admin] username`/`password`. A missing file uses built-in defaults (port 8080, no admin). Unknown keys are rejected. The admin account is created/promoted at startup and its password reset to the configured value when it differs (config is authoritative).
- `github.com/mattn/go-sqlite3` requires cgo: `CGO_ENABLED=1` and a C compiler must be available.
- Schema is created/migrated at startup (`ensureColumn`); adding a column means updating both the `schema` string and the migration guard.
- WAL mode with `SetMaxOpenConns(1)` intentionally serializes DB access.

## API

- Auth is one endpoint: `POST /api/auth` logs in when the username exists, otherwise registers (200 vs 201). Session token in an HttpOnly cookie; only its SHA-256 hash is stored. `GET /api/auth/me` returns the session user; `PUT/PATCH /api/auth/me` edits the signed-in account (`username`, `bio`, and optional `currentPassword`+`newPassword`).
- Profiles: `GET /api/users/{username}` returns the public account plus `stats` (uploads, likes, avgRating, ratings) and the owner's `best` (by rating) and `latest` schematics.
- Admin (accounts with `isAdmin: true` only; 403 otherwise): `GET /api/admin/users` lists accounts; `DELETE /api/admin/users/{id}` deletes an account plus its schematics, feedback and sessions; `PATCH /api/admin/users/{id}` sets `{isAdmin?, password?}`. Admins cannot delete or demote themselves. Admins may also edit/delete any schematic through the normal schematic endpoints.
- Schematics: `GET/POST /api/schematics`, `GET/PUT/PATCH/DELETE /api/schematics/{id}`, `GET /api/schematics/{id}/file` (alias `/download`). List filters: `q,name,description,owner=me,sort,order,from,to,limit,offset,page,pageSize` (page/pageSize are 1-based aliases; response includes `total,totalPages`). `sort` accepts `uploadDate|updatedDate|name|size|rating|likes`. The frontend infinite-scrolls filtered pages instead of paginating client-side.
- Uploads are multipart, max 50 MiB, and must start with the gzip magic bytes `1f 8b`.

## Conventions

- No comments in code unless they explain non-obvious intent; the existing files follow this.
- Build artifacts and `data/` are gitignored — never commit them.
