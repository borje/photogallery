# Smugbox

A self-hosted photo gallery published straight from Lightroom Classic.
Publish a collection in Lightroom and it becomes an album on your own
server: full-size originals kept byte for byte, display sizes generated on
the server, optional password per album, and one-click download of a single
photo or the whole album as a zip.

Three parts, one repository:

| Directory | What it is |
|---|---|
| `lightroom-plugin/` | Lightroom Classic publish service (Lua). One published collection = one album. |
| `backend/` | Go server: publish API for the plugin, visitor API, image processing with libvips, SQLite, static hosting of the frontend. |
| `frontend/` | React single-page app: album list, album page with responsive grid, lightbox, password gate, downloads. |
| `deploy/` | Dockerfile, docker-compose, backup script, deployment notes. |
| `tools/loadtest/` | Python load-test client for the publish API plus a throwaway test server. |

The design document with all decisions is
`lightroom-gallery-implementation-plan.md` (Swedish).

## How it works

1. In Lightroom you create a published collection under the **Smugbox**
   service, optionally give it a password, and click *Publish*. Editing the
   collection's settings afterwards lets you pick a cover photo; otherwise the
   first photo in the collection's sort order is used.
2. The plugin exports full-size JPEGs and uploads each one with its title,
   caption, keywords, capture time and camera data. Re-publishing an edited
   photo replaces it in place; removing a photo or the collection removes it
   on the server.
3. The server stores the original untouched and generates `large`, `medium`,
   `small`, `thumb` and a tiny blurred placeholder, all upright, sRGB and
   stripped of metadata except the colour profile.
4. Visitors browse `https://your-host/`. Public albums open directly;
   protected albums show a password prompt and stay unlocked for 24 hours in
   that browser. The album list only ever shows blurred covers for locked
   albums.

## Quick start (Docker)

```
cp deploy/.env.example deploy/.env      # set PUBLIC_BASE_URL
cd deploy && docker compose up -d --build
docker compose exec smugbox smugbox admin create-api-key --label "Lightroom"
```

Then in Lightroom Classic: *File > Plug-in Manager > Add* and choose
`lightroom-plugin/smugbox.lrplugin`. In the Library module set up the
**Smugbox** publish service with your server URL and the API key,
click *Test connection*, and publish your first collection.

Put a TLS-terminating reverse proxy in front of the container. See
`deploy/README.md` for proxy timeouts, trusted proxy networks and backups.

## Admin commands

Run inside the container (`docker compose exec smugbox smugbox admin ...`)
or locally with `DATA_DIR` set:

```
smugbox admin create-api-key --label "Lightroom laptop"
smugbox admin list-api-keys
smugbox admin revoke-api-key <id>
smugbox admin list-albums
smugbox admin set-password <slug> [--clear]
smugbox admin gc [--dry-run]           # remove orphaned files
```

`gc` removes photo directories without a database row, stray entries under
`photos/`, and empty album directories and `incoming/` files older than an
hour. It is safe to run while the server is up. Note that every `admin`
command opens the database and applies pending migrations, so run the
upgraded binary's `admin` only when you are ready to upgrade `serve` too.

## Development

Requirements: Go 1.26, `libvips-dev` and `pkg-config`, Node 22.

```
# backend on :8080
cd backend
DATA_DIR=/tmp/smugbox go run ./cmd/smugbox serve
go test ./...

# frontend on :5173, proxies /api to :8080
cd frontend
npm install
npm run dev
npm test
```

The Lightroom plugin has no automated tests; `lightroom-plugin/TESTING.md`
is the manual checklist to run against a live server.

`tools/loadtest/` stands in for Lightroom when you want to know how the
server behaves under load: it uploads a folder of JPEGs through the publish
API and reports latency, throughput and how long it takes before the
uploaded photos are visible to visitors.

```
cd tools/loadtest
./smugbox_loadtest.py --docker --photos ~/Pictures/export --albums 3 --workers 4
```

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `PUBLIC_BASE_URL` | `http://localhost:8080` | Public origin used in links the plugin records. |
| `DATA_DIR` | `./data` | SQLite database and photo files. |
| `FRONTEND_DIR` | unset | Built frontend to serve; unset gives 404 for non-API paths. |
| `LISTEN_ADDR` | `:8080` | Listen address. |
| `TRUSTED_PROXY_CIDR` | unset | Networks whose `X-Forwarded-For` is trusted. |
| `MAX_UPLOAD_MB` | `100` | Largest accepted upload. |
| `LOG_LEVEL`, `LOG_FORMAT` | `info`, `text` | Logging; use `json` in production. |

## API overview

- `POST/PUT/DELETE /api/publish/albums[/{id}]` and
  `POST/PUT/DELETE /api/publish/albums/{id}/photos[/{photo_id}]`,
  `PUT .../order`, `GET .../photos`, `GET /api/publish/ping`.
  Bearer API key required. Used by the plugin.
- `GET /api/albums`, `GET /api/albums/{slug}`, `GET .../cover`,
  `GET .../photos/{id}/{variant}[?download=1]`, `GET .../download`,
  `POST .../unlock`. Used by the frontend.
- `GET /api/healthz`.

Errors are JSON: `{"error": "snake_case_code", "message": "optional detail"}`.
