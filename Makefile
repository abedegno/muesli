.PHONY: run test test-whisper-cgo test-db test-db-stop tidy build-admin build up dev lint check smoke new-migration check-test-determinism prod-up prod-down prod-logs prod-ps prod-backup prod-upgrade

PROD_DIR ?= .
PROD_COMPOSE = docker compose --env-file $(PROD_DIR)/.env -f $(PROD_DIR)/docker-compose.prod.yml

run:
	go run ./cmd/muesli

tidy:
	go mod tidy

# Build the admin SPA into the Go embed dir (internal/adminui/dist).
build-admin:
	cd web/admin && npm ci && npm run build

# Build the server binary with the admin UI embedded. build-admin runs first so
# the freshly compiled assets are what get embedded by `go build`.
build: build-admin
	go build -o bin/muesli ./cmd/muesli
	go build -o bin/ollama-agent ./cmd/ollama-agent
	go build -o bin/whisper-cpp-transcriber ./cmd/whisper-cpp-transcriber

# Each test call gets its own PostgreSQL schema, so packages run in parallel safely.
test:
	go test ./... -p 4 -parallel 2

# Runner-safe entrypoint for the whisper.cpp cgo build/test path
# (cmd/whisper-cpp-transcriber's `whisper_cgo`-tagged real engine).
# Skips cleanly (exit 0, no `go test` invocation at all) if the vendored
# whisper.cpp headers/libs aren't provisioned via C_INCLUDE_PATH/
# LIBRARY_PATH -- see scripts/test-whisper-cgo.sh for why a Go-level
# t.Skip alone can't do this (cgo link failures happen before any Go
# code runs). Does not affect the default `test` target above, which
# stays pure Go / untagged.
test-whisper-cgo:
	bash scripts/test-whisper-cgo.sh

# Spin up a throwaway Postgres (with pgvector) for tests on :5433
test-db:
	docker run --rm -d --name muesli-test-db -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=muesli_test -p 5433:5432 pgvector/pgvector:pg16
	@echo "export TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5433/muesli_test?sslmode=disable"

test-db-stop:
	docker stop muesli-test-db

# Create a new timestamped migration pair. Usage: make new-migration name=add_foo
new-migration:
	bash scripts/new-migration.sh $(name)

# Bring up the full self-hosted stack (postgres, ollama, whisper, agent, server).
up:
	docker compose up

# Run the Electron desktop client in dev/watch mode (electron-vite dev).
dev:
	npm run dev

# Run Go static analysis. Add golangci-lint / eslint here when config is added.
lint:
	go vet ./...

# Quick sanity check: compile the Go server and typecheck the TS client.
check:
	go build ./... && npm run typecheck

# Run the end-to-end smoke test against a live stack.
# Set MUESLI_URL / MUESLI_EMAIL / MUESLI_PASSWORD to override defaults.
smoke:
	bash scripts/smoke.sh

# Verify no banned time calls (time.Sleep, time.Now) in non-e2e Go test files.
check-test-determinism:
	bash scripts/check-test-determinism.sh

# Operator helpers for the production compose stack.
prod-up:
	$(PROD_COMPOSE) up -d

prod-down:
	$(PROD_COMPOSE) down

prod-logs:
	$(PROD_COMPOSE) logs -f

prod-ps:
	$(PROD_COMPOSE) ps

prod-backup:
	$(PROD_COMPOSE) exec -T postgres pg_dump -U postgres muesli | gzip > muesli-$$(date +%Y%m%d%H%M%S).sql.gz

prod-upgrade:
	sh scripts/upgrade.sh --dir $(PROD_DIR)
