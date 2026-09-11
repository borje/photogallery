# Smugbox (Lightroom publish service + Go backend + React frontend)

Design document: `lightroom-gallery-implementation-plan.md` (Swedish). Its decisions
are fixed; do not re-open them without asking. Code, comments and docs are in English.

## Layout

- `backend/` — Go module `github.com/bege/smugbox/backend` (Go 1.26, CGO + libvips).
  - `cmd/smugbox` — `serve` and `admin` subcommands (API keys, albums, passwords, gc).
  - `internal/api` — all HTTP under `/api/`; `/api/publish/*` is for the Lightroom
    plugin (Bearer API key), `/api/albums/*` for visitors (cookie for locked albums).
  - `internal/db` — SQLite via `modernc.org/sqlite`, goose migrations embedded from
    `internal/db/migrations`. Single connection (`SetMaxOpenConns(1)`).
  - `internal/storage` — `<DATA_DIR>/photos/<album>/<photo>/<variant>.jpg`; writes go to
    `<DATA_DIR>/incoming` and are renamed into place. Paths only from UUIDs + variant whitelist.
  - `internal/image` — libvips via `github.com/cshum/vipsgen/vips816` (Debian 13 libvips 8.16).
  - `internal/auth` — API keys (sha256), album passwords (bcrypt), session cookie (HMAC).
  - `internal/web` — serves the frontend build from `FRONTEND_DIR` with SPA fallback.
- `frontend/` — Vite + React + TypeScript (Tailwind v4, shadcn/ui, react-photo-album,
  yet-another-react-lightbox, TanStack Query, Vitest + MSW).
- `lightroom-plugin/smugbox.lrplugin/` — Lightroom Classic publish service (Lua).
- `deploy/` — Dockerfile (build from repo root), docker-compose, backup script.

## Working on the backend

```
cd backend
go build -p 1 ./...      # -p 1: this dev machine has 2 GB RAM; modernc.org/libc is heavy
go test -p 1 ./...
DATA_DIR=/tmp/smugbox go run ./cmd/smugbox serve
go run ./cmd/smugbox admin create-api-key --label "Lightroom laptop"
```

Requires `libvips-dev` and `pkg-config` (apt). Tests use a temp SQLite file and temp
storage per test; clock and randomness are injected through `api.Deps`.

## Conventions

- Errors to clients: JSON `{"error": "<snake_case_code>", "message"?: "..."}`.
- Timestamps in the DB are RFC 3339 UTC text; `taken_at` is stored as Lightroom sends it.
- Never build filesystem paths from user input; go through `storage.Store`.
- Write tests in the same change as the code. Keep `go vet` clean.
