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
# CA certs for any outbound TLS (and good hygiene for a server image).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /muesli /usr/local/bin/muesli
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/muesli"]
