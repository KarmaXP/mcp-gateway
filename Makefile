# Global Settings
# Use bash as the shell for consistent behavior on macOS/Linux
SHELL := /bin/bash

# Variables
BINARY_NAME=mcp-gateway
MAIN_PATH=cmd/gateway/main.go
COMPOSE=docker compose -f deployments/docker-compose.yaml --env-file .env
# Before compose file had `name: mcp-gateway`, the implicit project was `deployments` (directory name).
COMPOSE_LEGACY=docker compose -f deployments/docker-compose.yaml -p deployments
GATEWAY_PORT       ?= 8080
SMOKE_GATEWAY_PORT ?= 18081
DEMO_CONFIG        := deployments/gateway.demo.yaml
EXAMPLE_CONFIG     := deployments/gateway.example.yaml
SRE_CONFIG         := deployments/gateway.sre.example.yaml

ifneq (,$(wildcard .env))
include .env
export PORT GATEWAY_PORT MCP_GATEWAY_CONFIG ROUTER_MODE QDRANT_URL EMBED_URL HOST_PORT_EMBED
export AUTH_MODE JWT_PUBLIC_KEY_FILE JWT_ISS JWT_AUD
export OTEL_EXPORTER_OTLP_ENDPOINT OTEL_SERVICE_NAME
endif

ifndef EMBED_URL
ifneq ($(HOST_PORT_EMBED),)
ifneq ($(HOST_PORT_EMBED),8001)
EMBED_URL := http://127.0.0.1:$(HOST_PORT_EMBED)
export EMBED_URL
endif
endif
endif

# Colors
BLUE 	:= \033[1;34m
YELLOW  := \033[1;33m
CYAN    := \033[36m
RESET   := \033[0m

.DEFAULT_GOAL := help
.PHONY: help bootstrap demo demo-backends demo-backends-stop demo-full verify-e2e \
        sre-backends sre-backends-stop sre-up sre-down sre-smoke gen-router-eval-catalog \
        build run run-filter-list stop test test-cover test-integration ci smoke smoke-e2e fmt lint clean tidy \
        docker-build docker-up docker-up-full docker-up-demo docker-up-sre docker-down docker-logs docker-clean calibration-up \
        lab-jwt-keys lab-jwt-env lab-jwt-verify demo-lab-preflight demo-lab-verify

# Help: sectioned list of targets (descriptions are defined here only)
help:
	@printf "$(YELLOW)Usage:$(RESET)\n"
	@printf "  make [target]\n\n"
	@printf "$(BLUE)▶ General$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "help" "Show this help message"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "bootstrap" "Copy .env.example to .env if missing"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lab-jwt-keys" "Ensure /tmp/mcp-lab-jwt.key + .pub.pem for JWT lab sessions"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lab-jwt-env" "Print export JWT_PUBLIC_KEY_FILE + JWT_ADMIN (after lab-jwt-keys)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lab-jwt-verify" "Crypto-check lab keys against gateway JWT validator"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo-lab-preflight" "Pre-demo checks (docker-up + fixture + JWT; no gateway)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo-lab-verify" "Full demo rehearsal (preflight + gateway + catalog + JWT + LangGraph)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo" "Quick start: one mock upstream + gateway + MCP tools/call (no Docker)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo-backends" "Start alpha/beta mock MCP servers on ports 3101 and 3102"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo-backends-stop" "Stop alpha/beta mock servers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "demo-full" "Two backends + gateway.example.yaml; calls alpha__echo"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "sre-backends" "Start k8s/prom/gh mock MCP servers on ports 3201–3203"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "sre-backends-stop" "Stop k8s/prom/gh mock servers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "sre-up" "Docker deps (Qdrant, embed) + SRE mock servers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "sre-smoke" "Gateway + SRE config; tools/call k8s, prom, gh"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "sre-down" "Stop SRE mock servers (Docker deps keep running)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "verify-e2e" "CI + demo + demo-full + sre-smoke (full local E2E check)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "clean" "Remove binary files and build artifacts"
	@printf "\n"
	@printf "$(BLUE)▶ Development$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "build" "Compile the Go binary"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "run" "Start the Gateway in development mode"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "run-filter-list" "make run with ROUTER_MODE=filter_list (see docs/adr/0002)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "stop" "Stop gateway on PORT/GATEWAY_PORT (.env); not demo/SRE mocks"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test" "Run all unit tests with race detection"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-cover" "go test -race with coverage report (internal/*)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-integration" "go test -tags=integration (JWT policy + optional Qdrant/embed/OTLP; see docs/DEVELOPER.md)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "smoke" "curl MCP flow against gateway + scripts/smoke_upstream (sets SMOKE_AUTO_START_GATEWAY=1)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "smoke-jwt" "MCP smoke with JWT auth, what the CI smoke-jwt check runs"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "smoke-e2e" "curl MCP handshake/tools flow against an already-running gateway"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "fmt" "gofmt -w . then normalize const/var '=' spacing (gofmt re-aligns)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lint" "Run golangci-lint + check for column-aligned '=' in Go sources"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "ci" "Same checks as GitHub Actions lint-and-unit job (lint + vet + race tests, -count=1)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "gen-router-eval-catalog" "Write docs/evaluation/router-eval-catalog.json from SyntheticCatalog()"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "tidy" "Clean up and verify Go modules"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "tidy-check" "Fail if go.mod or go.sum is not tidy (CI gate)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "shellcheck" "shellcheck -S warning over scripts/*.sh (CI gate)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "vulncheck" "govulncheck over all packages (CI gate)"
	@printf "\n"
	@printf "$(BLUE)▶ MCP Gateway (Docker Compose)$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-build"    "Build the gateway Docker image"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up"       "Start mcp-gateway stack deps (Qdrant · OTel · Tempo · Prometheus · Grafana)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up-full"  "Start full mcp-gateway stack including gateway container"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up-demo"  "Start compose profile demo (mock alpha/beta on 3101/3102)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up-sre"   "Start compose profile sre (mock k8s/prom/gh on 3201–3203)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-down"     "Stop and remove all containers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-logs"     "Follow logs from all running services"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-clean"    "Remove containers, volumes and built image"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "calibration-up"  "Start full calibration stack and print compose health"
	@printf "\n"

# Targets Implementation

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME) $(MAIN_PATH)

bootstrap:
	@if [ ! -f .env ]; then cp -n .env.example .env && printf "Created .env from .env.example\n"; else printf ".env already present\n"; fi
	@printf "Requires Go (see go.mod). Docker is optional (make docker-up).\n"
	@printf "JWT lab sessions: run 'make lab-jwt-keys' (keys in /tmp, no .env required).\n"

lab-jwt-keys:
	@chmod +x scripts/lab_jwt_keys.sh
	@bash scripts/lab_jwt_keys.sh keys

lab-jwt-env: lab-jwt-keys
	@bash scripts/lab_jwt_keys.sh env

lab-jwt-verify: lab-jwt-keys
	@bash scripts/lab_jwt_keys.sh verify

# The Makefile includes .env, so a lab .env would otherwise turn the router on and point the
# demo at Qdrant and embed, which is exactly the Docker this target promises you do not need.
demo:
	@echo "Plug-and-play demo (no Docker)..."
	@chmod +x scripts/smoke_test.sh
	@MCP_GATEWAY_CONFIG=$(DEMO_CONFIG) ROUTER_MODE=off OTEL_EXPORTER_OTLP_ENDPOINT= \
		SMOKE_AUTO_START_GATEWAY=1 DEMO_PRINT_HELP=1 bash scripts/smoke_test.sh

demo-backends:
	@bash scripts/mock_upstreams.sh demo start

demo-backends-stop:
	@bash scripts/mock_upstreams.sh demo stop

demo-full: bootstrap demo-backends
	@echo "Multi-backend demo (gateway.example.yaml + alpha__echo)..."
	@MCP_GATEWAY_CONFIG=$(EXAMPLE_CONFIG) bash scripts/scenario_smoke.sh demo
	@$(MAKE) demo-backends-stop

verify-e2e: ci
	@echo "Running full E2E checks (demo, multi-backend, SRE)..."
	@$(MAKE) stop
	@$(MAKE) demo
	@$(MAKE) stop
	@$(MAKE) demo-full
	@$(MAKE) stop
	@$(MAKE) sre-up
	@$(MAKE) sre-smoke
	@$(MAKE) stop
	@echo "VERIFY-E2E OK"

sre-backends:
	@bash scripts/mock_upstreams.sh sre start

sre-backends-stop:
	@bash scripts/mock_upstreams.sh sre stop

sre-up: bootstrap sre-backends
	@echo "Starting compose dependencies (Qdrant, embed, OTel)..."
	@$(COMPOSE) up -d || printf "WARN: docker-up failed — start Docker for router=on; mocks on 3201–3203 are up\n"
	@echo "SRE mocks ready. Run: make sre-smoke (router=on when Qdrant+embed healthy)"

sre-down: sre-backends-stop
	@echo "SRE mocks stopped (docker compose deps still up; use make docker-down to stop all)"

sre-smoke:
	@MCP_GATEWAY_CONFIG=$(SRE_CONFIG) bash scripts/scenario_smoke.sh sre

gen-router-eval-catalog:
	@go run ./tools/gen-router-eval-catalog/main.go > docs/evaluation/router-eval-catalog.json
	@echo "Wrote docs/evaluation/router-eval-catalog.json"

run:
	@bash -c 'set -a && ([ -f .env ] && . ./.env || true) && set +a; \
		export ROUTER_MODE="$(ROUTER_MODE)"; \
		hp="$${HOST_PORT_EMBED:-}"; \
		if [ -n "$$hp" ]; then export EMBED_URL="http://127.0.0.1:$$hp"; \
		elif [ -z "$$EMBED_URL" ]; then export EMBED_URL="http://127.0.0.1:8001"; fi; \
		echo "Starting $(BINARY_NAME)..."; \
		echo "PORT=$$PORT ROUTER_MODE=$$ROUTER_MODE EMBED_URL=$$EMBED_URL MCP_GATEWAY_CONFIG=$${MCP_GATEWAY_CONFIG:-$(DEMO_CONFIG)}"; \
		exec env MCP_GATEWAY_CONFIG="$${MCP_GATEWAY_CONFIG:-$(DEMO_CONFIG)}" go run $(MAIN_PATH)'

run-filter-list:
	@$(MAKE) run ROUTER_MODE=filter_list

demo-lab-preflight:
	@chmod +x scripts/demo_lab_preflight.sh
	@bash scripts/demo_lab_preflight.sh

demo-lab-verify:
	@chmod +x scripts/demo_lab_preflight.sh
	@bash scripts/demo_lab_preflight.sh --full

SMOKE_JWT_GATEWAY_PORT ?= 18082

stop:
	@echo "Stopping $(BINARY_NAME)..."
	@bash -c 'set -a && ([ -f .env ] && . ./.env || true) && set +a; \
		seen=""; \
		kill_port() { \
			local p="$$1"; [ -z "$$p" ] && return 0; \
			case " $$seen " in *" $$p "*) return 0;; esac; seen="$$seen $$p"; \
			local ids; ids=$$(lsof -ti :$$p 2>/dev/null || true); \
			[ -n "$$ids" ] && kill $$ids 2>/dev/null || true; \
		}; \
		kill_port "$$PORT"; kill_port "$$GATEWAY_PORT"; kill_port "$(GATEWAY_PORT)"; \
		kill_port "$(SMOKE_GATEWAY_PORT)"; kill_port "$(SMOKE_JWT_GATEWAY_PORT)"; kill_port "31400"; \
		echo "Stopped gateway listeners (PORT/GATEWAY_PORT from .env when set). Use make sre-down / demo-backends-stop for mocks."'

test:
	@echo "Running vet + tests..."
	@go vet ./...
	@go test -v -race ./...

test-cover:
	@echo "Coverage (internal packages)..."
	@mkdir -p bin
	@go vet ./...
	@go test -race -coverprofile=bin/coverage.out -covermode=atomic ./internal/...
	@go tool cover -func=bin/coverage.out | tail -n 25

test-integration:
	@echo "Integration tests (JWT/RPC always; Qdrant+embed+OTLP when reachable — see docs/DEVELOPER.md)..."
	@go vet ./...
	@go vet -tags=integration ./...
	@QDRANT_URL=$${QDRANT_URL:-http://127.0.0.1:6333} \
	 EMBED_URL=$${EMBED_URL:-http://127.0.0.1:8001} \
	 OTEL_EXPORTER_OTLP_ENDPOINT=$${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4318} \
	 go test -tags=integration -race -count=1 \
		./internal/gateway/httpserver/... \
		./internal/router/... \
		./internal/routertest/... \
		./internal/telemetry/...

ci:
	@echo "CI parity (.github/workflows/ci.yml — lint-and-unit)..."
	@$(MAKE) lint
	@go vet ./...
	@go test -race -count=1 ./...
	@$(MAKE) tidy-check
	@$(MAKE) shellcheck
	@$(MAKE) vulncheck

smoke:
	@echo "Smoke test (smoke_upstream + gateway MCP over curl)..."
	@chmod +x scripts/smoke_test.sh
	@SMOKE_AUTO_START_GATEWAY=1 bash scripts/smoke_test.sh

smoke-jwt:
	@echo "MCP JWT smoke (gateway + smoke_upstream + generated RS256 token)..."
	@SMOKE_AUTO_START_GATEWAY=1 bash scripts/smoke_jwt.sh

smoke-e2e:
	@echo "Smoke E2E (expects already-running gateway/upstream)..."
	@chmod +x scripts/smoke_e2e.sh
	@bash scripts/smoke_e2e.sh

fmt:
	@echo "gofmt + normalize '=' spacing in const/var blocks..."
	@gofmt -w .
	@chmod +x scripts/normalize-go-eq-spacing.sh
	@./scripts/normalize-go-eq-spacing.sh

lint:
	@echo "golangci-lint..."
	@go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1 run ./...
	@chmod +x scripts/check-go-eq-spacing.sh
	@./scripts/check-go-eq-spacing.sh

tidy:
	@echo "Tidying Go modules..."
	@go mod tidy
	@go mod verify

tidy-check:
	@echo "Checking go.mod and go.sum are tidy..."
	@go mod tidy
	@git diff --exit-code go.mod go.sum

shellcheck:
	@echo "shellcheck..."
	@shellcheck -S warning scripts/*.sh

# Neither this pin nor the golangci-lint one above is bumped by anything: dependabot's gomod
# ecosystem does not read versions out of a go run argument. Bump both by hand.
vulncheck:
	@echo "govulncheck..."
	@go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf tmp/

docker-build:
	@echo "Building gateway image..."
	@docker build -t mcp-gateway:dev .

docker-up:
	@echo "Starting mcp-gateway compose stack (dependencies)..."
	@$(COMPOSE) up -d

docker-up-full: docker-build
	@echo "Starting mcp-gateway compose stack (dependencies + gateway app)..."
	@$(COMPOSE) --profile gateway up -d

docker-up-demo:
	@echo "Starting compose profile demo (mock alpha/beta on 3101/3102)..."
	@$(COMPOSE) --profile demo up -d

docker-up-sre:
	@echo "Starting compose profile sre (mock k8s/prom/gh on 3201–3203)..."
	@$(COMPOSE) --profile sre up -d

docker-down:
	@echo "Stopping mcp-gateway compose stack..."
	@$(COMPOSE) --profile gateway --profile demo --profile sre down --remove-orphans
	@-$(COMPOSE_LEGACY) --profile gateway --profile demo --profile sre down --remove-orphans

docker-logs:
	@$(COMPOSE) --profile gateway logs -f

docker-clean:
	@echo "Removing containers, volumes and images..."
	@$(COMPOSE) --profile gateway --profile demo --profile sre down -v --remove-orphans
	@-$(COMPOSE_LEGACY) --profile gateway --profile demo --profile sre down -v --remove-orphans
	@docker rmi mcp-gateway:dev mcp-gateway-embed:dev mcp-gateway-mock:dev 2>/dev/null || true

calibration-up: docker-up-full
	@echo "Calibration stack status (wait for healthy)..."
	@$(COMPOSE) --profile gateway ps
