# Photo Gallery

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

The design document with all decisions is
`lightroom-gallery-implementation-plan.md` (Swedish).

## How it works

1. In Lightroom you create a published collection under the **Photo Gallery**
   service, optionally give it a password, and click *Publish*.
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
cp deploy/.env.example deploy/.env      # set SESSION_SECRET and PUBLIC_BASE_URL
cd deploy && docker compose up -d --build
docker compose exec gallery gallery admin create-api-key --label "Lightroom"
```

Then in Lightroom Classic: *File > Plug-in Manager > Add* and choose
`lightroom-plugin/gallery.lrplugin`. In the Library module set up the
**Photo Gallery** publish service with your server URL and the API key,
click *Test connection*, and publish your first collection.

Put a TLS-terminating reverse proxy in front of the container. See
`deploy/README.md` for proxy timeouts, trusted proxy networks and backups.

## Admin commands

Run inside the container (`docker compose exec gallery gallery admin ...`)
or locally with `DATA_DIR` set:

```
gallery admin create-api-key --label "Lightroom laptop"
gallery admin list-api-keys
gallery admin revoke-api-key <id>
gallery admin list-albums
gallery admin set-password <slug> [--clear]
gallery admin gc [--dry-run]           # remove orphaned files
```

## Development

Requirements: Go 1.26, `libvips-dev` and `pkg-config`, Node 22.

```
# backend on :8080
cd backend
SESSION_SECRET=$(openssl rand -base64 48) DATA_DIR=/tmp/gallery go run ./cmd/gallery serve
go test ./...

# frontend on :5173, proxies /api to :8080
cd frontend
npm install
npm run dev
npm test
```

The Lightroom plugin has no automated tests; `lightroom-plugin/TESTING.md`
is the manual checklist to run against a live server.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `SESSION_SECRET` | required | Signs the visitor session cookie (32+ characters). |
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
