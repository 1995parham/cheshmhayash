# cheshmhayash — task runner. `just` or `just --list` to see recipes.
# Backend: Go 1.26 (net/http) · Frontend: React 19 + Vite 8 (in ./frontend)

set shell := ["bash", "-uc"]

frontend := "frontend"
bin      := "bin/cheshmhayash"

# Default: show available recipes.
default:
    @just --list

# ── Build ────────────────────────────────────────────────────────────

# Build the production binary (SPA built first, then embedded-static serve).
build: build-frontend build-backend

# Compile the Go binary.
build-backend:
    go build -o ./{{bin}} .

# Build the React SPA into frontend/dist.
build-frontend:
    cd {{frontend}} && npm run build

# ── Run (local dev) ──────────────────────────────────────────────────

# Run backend (:1378) and the Vite dev server (:5173) together; Ctrl-C stops both.
dev:
    #!/usr/bin/env bash
    set -uo pipefail
    go run . & back=$!
    trap 'kill $back 2>/dev/null' EXIT INT TERM
    cd {{frontend}} && npm run dev
    wait $back

# Run only the Go backend on :1378.
run-backend:
    go run .

# Run only the Vite dev server on :5173 (proxies /api + /healthz to :1378).
run-frontend:
    cd {{frontend}} && npm run dev

# Run the read-only MCP server over stdio. Pass `write=1` for destructive tools.
run-mcp write="0":
    CHESHMHAYASH_MCP_WRITE={{write}} go run . -mcp

# Build then run the production binary (serves API + built SPA on :1378).
run: build
    ./{{bin}}

# ── Dependency upgrades ──────────────────────────────────────────────

# Upgrade both backend and frontend dependencies.
upgrade: upgrade-backend upgrade-frontend

# Bump Go modules to latest and tidy.
upgrade-backend:
    go get -u ./...
    go mod tidy

# Bump frontend deps. `major=1` updates across semver majors (needs network for npx).
upgrade-frontend major="0":
    cd {{frontend}} && \
    if [ "{{major}}" = "1" ]; then npx --yes npm-check-updates -u && npm install; \
    else npm update && npm install; fi

# ── Lint / format ────────────────────────────────────────────────────

# Lint both sides.
lint: lint-backend lint-frontend

# golangci-lint: gofmt/goimports + go vet + ~all linters (.golangci.yml).
lint-backend:
    golangci-lint run

# Biome lint + format check (CI-strict, no writes).
lint-frontend:
    cd {{frontend}} && npm run ci

# Auto-fix formatting on both sides.
fmt: fmt-backend fmt-frontend

fmt-backend:
    golangci-lint fmt

fmt-frontend:
    cd {{frontend}} && npm run lint:fix

# ── Test ─────────────────────────────────────────────────────────────

# Test both sides.
test: test-backend test-frontend

# Run the Go test suite.
test-backend:
    go test ./...

# Go tests with the race detector.
test-backend-race:
    go test -race ./...

# Frontend has no unit-test runner; this is the CI gate: type-check + build.
test-frontend:
    cd {{frontend}} && npm run typecheck && npx vite build

# ── Aggregate ────────────────────────────────────────────────────────

# Everything CI runs: lint, test, build — both sides.
ci: lint test build
