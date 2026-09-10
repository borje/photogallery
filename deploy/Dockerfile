# syntax=docker/dockerfile:1
# Build from the repository root: docker build -f deploy/Dockerfile .

# --- Go backend -------------------------------------------------------------
FROM golang:1.26-trixie AS backend
RUN apt-get update && apt-get install -y --no-install-recommends libvips-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/gallery ./cmd/gallery

# --- Runtime ----------------------------------------------------------------
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends libvips42t64 ca-certificates wget tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --user-group --home-dir /data --no-create-home gallery \
    && mkdir -p /data /srv/frontend && chown gallery:gallery /data
COPY --from=backend /out/gallery /usr/local/bin/gallery
ENV DATA_DIR=/data \
    FRONTEND_DIR=/srv/frontend \
    LISTEN_ADDR=:8080 \
    LOG_FORMAT=json
USER gallery
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["gallery"]
CMD ["serve"]
