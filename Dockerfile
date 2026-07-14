# syntax=docker/dockerfile:1

# ---- Stage 1: build the admin SPA -------------------------------------------
# Produces the real bundle into internal/adminui/dist, which the Go build embeds
# via //go:embed dist. (The repo ships placeholder fixtures there; this overwrites
# them inside the image only.)
FROM node:22-slim AS admin
WORKDIR /app/web/admin
# Install deps first for better layer caching.
COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci
# Bring in the rest of the admin source and build. Vite's outDir is
# ../../internal/adminui/dist (see web/admin/vite.config.ts), so we lay the
# source out under the same relative path the config expects.
COPY web/admin/ ./
RUN npm run build
# After build, /app/internal/adminui/dist holds the real bundle.

# ---- Stage 2: build the Go server -------------------------------------------
FROM golang:1.25 AS build
WORKDIR /src
# Module graph first for caching.
COPY go.mod go.sum ./
RUN go mod download
# Copy the Go source.
COPY cmd ./cmd
COPY internal ./internal
# Overlay the freshly built admin bundle so go:embed picks up the real SPA
# instead of the committed placeholders.
COPY --from=admin /app/internal/adminui/dist ./internal/adminui/dist
# Static, CGO-free binary (pure-Go pgx driver).
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /muesli ./cmd/muesli

# ---- Stage 3: minimal runtime ----------------------------------------------
FROM debian:bookworm-slim AS runtime
ARG VERSION=dev
# CA certs for any outbound TLS (and good hygiene for a server image).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
LABEL org.opencontainers.image.source="https://github.com/abedegno/muesli" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.title="muesli"
RUN groupadd --system --gid 65532 muesli \
    && useradd --system --uid 65532 --gid 65532 --create-home --home-dir /app --shell /usr/sbin/nologin muesli \
    && mkdir -p /app \
    && chown -R muesli:muesli /app
WORKDIR /app
COPY --from=build /muesli /usr/local/bin/muesli
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD curl --fail --silent --show-error http://127.0.0.1:8080/readyz || exit 1
USER muesli:muesli
ENTRYPOINT ["/usr/local/bin/muesli"]
