# Deployment

One container runs everything: the Go API, image processing and the built
React frontend. State lives in the `/data` volume (`gallery.db` plus
`photos/`).

```
cp deploy/.env.example deploy/.env      # set SESSION_SECRET, PUBLIC_BASE_URL
cd deploy && docker compose up -d --build
docker compose exec gallery gallery admin create-api-key --label "Lightroom"
```

Paste the printed key into the Lightroom plug-in (Publishing Manager > Photo
Gallery). Other admin commands: `list-albums`, `list-api-keys`,
`revoke-api-key <id>`, `set-password <slug> [--clear]`, `gc [--dry-run]`.

## Reverse proxy

TLS and hostnames are handled by your proxy (Traefik in the reference setup).
Two things matter for this service:

- **Timeouts.** Album zip downloads stream for as long as the album is large.
  Raise the proxy's response/idle timeouts well above the default (Traefik:
  `--entrypoints.websecure.transport.respondingTimeouts.writeTimeout=0` or a
  generous value, and no request body limit below `MAX_UPLOAD_MB`).
- **Client IPs.** Set `TRUSTED_PROXY_CIDR` to the proxy's network so the
  unlock rate limiter uses the visitor's IP from `X-Forwarded-For` rather than
  the proxy's.

The container sets `X-Content-Type-Options` and `Content-Security-Policy`
itself; add HSTS at the proxy.

## Backups

`deploy/backup.sh DATA_DIR DEST` snapshots the database with `VACUUM INTO`
and rsyncs photos and snapshot to `DEST`. Run it from cron on the host
(mount the volume path, e.g. `/var/lib/docker/volumes/deploy_gallery-data/_data`).

## Development

```
cd backend && SESSION_SECRET=$(openssl rand -base64 48) DATA_DIR=/tmp/gallery go run ./cmd/gallery serve
cd frontend && npm run dev          # http://localhost:5173, proxies /api to :8080
```
