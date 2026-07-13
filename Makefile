.PHONY: run test test-db test-db-stop tidy build-admin build up dev lint check smoke new-migration check-test-determinism

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

# Each test call gets its own PostgreSQL schema, so packages run in parallel safely.
test:
	go test ./... -p 4 -parallel 2

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
