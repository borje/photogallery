# syntax=docker/dockerfile:1
# Build from the repository root:
#   docker build -f deploy/Dockerfile -t smugbox .
# On hosts with little RAM keep GOFLAGS=-p=1 (modernc.org/libc is memory
# hungry to compile); CI with more memory can pass --build-arg GOFLAGS=.

# --- Frontend ---------------------------------------------------------------
FROM node:22-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- Go backend -------------------------------------------------------------
FROM golang:1.26-trixie AS backend
ARG GOFLAGS=-p=1
ENV GOFLAGS=${GOFLAGS}
RUN apt-get update && apt-get install -y --no-install-recommends libvips-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/smugbox ./cmd/smugbox

# --- Runtime ----------------------------------------------------------------
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends libvips42t64 ca-certificates wget tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --user-group --home-dir /data --no-create-home smugbox \
    && mkdir -p /data /srv/frontend && chown smugbox:smugbox /data
COPY --from=backend /out/smugbox /usr/local/bin/smugbox
COPY --from=frontend /src/frontend/dist/ /srv/frontend/
ENV DATA_DIR=/data \
    FRONTEND_DIR=/srv/frontend \
    LOG_FORMAT=json
USER smugbox
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["smugbox"]
CMD ["serve"]
