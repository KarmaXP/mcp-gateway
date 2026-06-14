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

# Colors
BLUE 	:= \033[1;34m
YELLOW  := \033[1;33m
CYAN    := \033[36m
RESET   := \033[0m

.DEFAULT_GOAL := help
.PHONY: help bootstrap demo demo-backends demo-backends-stop demo-full verify-e2e \
        sre-backends sre-backends-stop sre-up sre-down sre-smoke gen-router-eval-catalog \
        build run stop test test-cover test-integration ci smoke smoke-e2e fmt lint clean tidy \
        docker-build docker-up docker-up-full docker-up-demo docker-up-sre docker-down docker-logs docker-clean calibration-up

# Help: sectioned list of targets (descriptions are defined here only)
help:
	@printf "$(YELLOW)Usage:$(RESET)\n"
	@printf "  make [target]\n\n"
	@printf "$(BLUE)▶ General$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "help" "Show this help message"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "bootstrap" "Copy .env.example to .env if missing"
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
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "stop" "Stop gateway on PORT/GATEWAY_PORT (.env); not demo/SRE mocks"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test" "Run all unit tests with race detection"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-cover" "go test -race with coverage report (internal/*)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-integration" "go test -tags=integration (JWT policy + optional Qdrant/embed/OTLP; see docs/DEVELOPER.md)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "smoke" "curl MCP flow against gateway + scripts/smoke_upstream (sets SMOKE_AUTO_START_GATEWAY=1)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "smoke-e2e" "curl MCP handshake/tools flow against an already-running gateway"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "fmt" "gofmt -w . then normalize const/var '=' spacing (gofmt re-aligns)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lint" "Run golangci-lint + check for column-aligned '=' in Go sources"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "ci" "Same checks as GitHub Actions lint-and-unit job (lint + vet + race tests, -count=1)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "gen-router-eval-catalog" "Write docs/evaluation/router-eval-catalog.json from SyntheticCatalog()"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "tidy" "Clean up and verify Go modules"
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
	@echo "🛠️  Building $(BINARY_NAME)..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME) $(MAIN_PATH)

bootstrap:
	@if [ ! -f .env ]; then cp -n .env.example .env && printf "Created .env from .env.example\n"; else printf ".env already present\n"; fi
	@printf "Requires Go (see go.mod). Docker is optional (make docker-up).\n"

demo:
	@echo "🎯 Plug-and-play demo (no Docker)..."
	@chmod +x scripts/smoke_test.sh
	@MCP_GATEWAY_CONFIG=$(DEMO_CONFIG) SMOKE_AUTO_START_GATEWAY=1 DEMO_PRINT_HELP=1 bash scripts/smoke_test.sh

demo-backends:
	@chmod +x scripts/demo_backends.sh
	@bash scripts/demo_backends.sh start

demo-backends-stop:
	@chmod +x scripts/demo_backends.sh
	@bash scripts/demo_backends.sh stop

demo-full: bootstrap demo-backends
	@echo "🎯 Multi-backend demo (gateway.example.yaml + alpha__echo)..."
	@chmod +x scripts/demo_multibackend_smoke.sh
	@MCP_GATEWAY_CONFIG=$(EXAMPLE_CONFIG) bash scripts/demo_multibackend_smoke.sh
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
	@chmod +x scripts/sre_backends.sh
	@bash scripts/sre_backends.sh start

sre-backends-stop:
	@chmod +x scripts/sre_backends.sh
	@bash scripts/sre_backends.sh stop

sre-up: bootstrap sre-backends
	@echo "🐳 Starting compose dependencies (Qdrant, embed, OTel)..."
	@$(COMPOSE) up -d || printf "WARN: docker-up failed — start Docker for router=on; mocks on 3201–3203 are up\n"
	@echo "SRE mocks ready. Run: make sre-smoke (router=on when Qdrant+embed healthy)"

sre-down: sre-backends-stop
	@echo "SRE mocks stopped (docker compose deps still up; use make docker-down to stop all)"

sre-smoke:
	@chmod +x scripts/sre_smoke.sh
	@MCP_GATEWAY_CONFIG=$(SRE_CONFIG) bash scripts/sre_smoke.sh

gen-router-eval-catalog:
	@go run ./tools/gen-router-eval-catalog/main.go > docs/evaluation/router-eval-catalog.json
	@echo "Wrote docs/evaluation/router-eval-catalog.json"

run:
	@echo "🚀 Starting $(BINARY_NAME)..."
	@bash -c 'set -a && ([ -f .env ] && . ./.env || true) && set +a && \
		: "$${MCP_GATEWAY_CONFIG:=$(DEMO_CONFIG)}" && export MCP_GATEWAY_CONFIG && \
		exec go run $(MAIN_PATH)'

SMOKE_JWT_GATEWAY_PORT ?= 18082

stop:
	@echo "🛑 Stopping $(BINARY_NAME)..."
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
	@echo "🧪 Running vet + tests..."
	@go vet ./...
	@go test -v -race ./...

test-cover:
	@echo "🧪 Coverage (internal packages)..."
	@mkdir -p bin
	@go vet ./...
	@go test -race -coverprofile=bin/coverage.out -covermode=atomic ./internal/...
	@go tool cover -func=bin/coverage.out | tail -n 25

test-integration:
	@echo "🧪 Integration tests (JWT/RPC always; Qdrant+embed+OTLP when reachable — see docs/DEVELOPER.md)..."
	@go vet ./...
	@QDRANT_URL=$${QDRANT_URL:-http://127.0.0.1:6333} \
	 EMBED_URL=$${EMBED_URL:-http://127.0.0.1:8001} \
	 OTEL_EXPORTER_OTLP_ENDPOINT=$${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4318} \
	 go test -tags=integration -race -count=1 \
		./internal/gateway/httpserver/... \
		./internal/router/... \
		./internal/telemetry/...

ci:
	@echo "🤖 CI parity (.github/workflows/ci.yml — lint-and-unit)..."
	@$(MAKE) lint
	@go vet ./...
	@go test -race -count=1 ./...

smoke:
	@echo "🔥 Smoke test (smoke_upstream + gateway MCP over curl)..."
	@chmod +x scripts/smoke_test.sh
	@SMOKE_AUTO_START_GATEWAY=1 bash scripts/smoke_test.sh

smoke-e2e:
	@echo "🔥 Smoke E2E (expects already-running gateway/upstream)..."
	@chmod +x scripts/smoke_e2e.sh
	@bash scripts/smoke_e2e.sh

fmt:
	@echo "📝 gofmt + normalize '=' spacing in const/var blocks..."
	@gofmt -w .
	@chmod +x scripts/normalize-go-eq-spacing.sh
	@./scripts/normalize-go-eq-spacing.sh

lint:
	@echo "🔍 golangci-lint..."
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...
	@chmod +x scripts/check-go-eq-spacing.sh
	@./scripts/check-go-eq-spacing.sh

tidy:
	@echo "📦 Tidying Go modules..."
	@go mod tidy
	@go mod verify

clean:
	@echo "🧹 Cleaning..."
	@rm -rf bin/
	@rm -rf tmp/

docker-build:
	@echo "🐳 Building gateway image..."
	@docker build -t mcp-gateway:dev .

docker-up:
	@echo "🐳 Starting mcp-gateway compose stack (dependencies)..."
	@$(COMPOSE) up -d

docker-up-full: docker-build
	@echo "🐳 Starting mcp-gateway compose stack (dependencies + gateway app)..."
	@$(COMPOSE) --profile gateway up -d

docker-up-demo:
	@echo "🐳 Starting compose profile demo (mock alpha/beta on 3101/3102)..."
	@$(COMPOSE) --profile demo up -d

docker-up-sre:
	@echo "🐳 Starting compose profile sre (mock k8s/prom/gh on 3201–3203)..."
	@$(COMPOSE) --profile sre up -d

docker-down:
	@echo "🛑 Stopping mcp-gateway compose stack..."
	@$(COMPOSE) --profile gateway --profile demo --profile sre down --remove-orphans
	@-$(COMPOSE_LEGACY) --profile gateway --profile demo --profile sre down --remove-orphans

docker-logs:
	@$(COMPOSE) --profile gateway logs -f

docker-clean:
	@echo "🧹 Removing containers, volumes and images..."
	@$(COMPOSE) --profile gateway --profile demo --profile sre down -v --remove-orphans
	@-$(COMPOSE_LEGACY) --profile gateway --profile demo --profile sre down -v --remove-orphans
	@docker rmi mcp-gateway:dev mcp-gateway-embed:dev mcp-gateway-mock:dev 2>/dev/null || true

calibration-up: docker-up-full
	@echo "🧪 Calibration stack status (wait for healthy)..."
	@$(COMPOSE) --profile gateway ps
