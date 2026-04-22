# Global Settings
# Use bash as the shell for consistent behavior on macOS/Linux
SHELL := /bin/bash

# Variables
BINARY_NAME=mcp-gateway
MAIN_PATH=cmd/gateway/main.go
COMPOSE=docker compose -f deployments/docker-compose.yaml --env-file .env
GATEWAY_PORT ?= 18080

# Colors
BLUE 	:= \033[1;34m
YELLOW  := \033[1;33m
CYAN    := \033[36m
RESET   := \033[0m

.DEFAULT_GOAL := help
.PHONY: help build run stop test test-cover test-integration lint clean tidy \
        docker-build docker-up docker-up-full docker-down docker-logs docker-clean

# Help: sectioned list of targets (descriptions are defined here only)
help:
	@printf "$(YELLOW)Usage:$(RESET)\n"
	@printf "  make [target]\n\n"
	@printf "$(BLUE)▶ General$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "help" "Show this help message"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "clean" "Remove binary files and build artifacts"
	@printf "\n"
	@printf "$(BLUE)▶ Development$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "build" "Compile the Go binary"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "run" "Start the Gateway in development mode"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "stop" "Stop the running Gateway process"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test" "Run all unit tests with race detection"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-cover" "go test -race with coverage report (internal/*)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test-integration" "go test -tags=integration (Qdrant + embed + OTLP; needs compose)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "lint" "Run golangci-lint (install via go run if missing)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "tidy" "Clean up and verify Go modules"
	@printf "\n"
	@printf "$(BLUE)▶ Infrastructure & Docker$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-build"    "Build the gateway Docker image"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up"       "Start infra (Qdrant · OTel · Tempo · Prometheus · Grafana)"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up-full"  "Start infra + gateway container"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-down"     "Stop and remove all containers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-logs"     "Follow logs from all running services"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-clean"    "Remove containers, volumes and built image"
	@printf "\n"

# Targets Implementation

build:
	@echo "🛠️  Building $(BINARY_NAME)..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME) $(MAIN_PATH)

run:
	@echo "🚀 Starting $(BINARY_NAME)..."
	@set -a && ([ -f .env ] && . ./.env || true) && set +a && go run $(MAIN_PATH)

stop:
	@echo "🛑 Stopping $(BINARY_NAME)..."
	@lsof -ti :$(GATEWAY_PORT) | xargs kill 2>/dev/null || echo "No running gateway process found"

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
	@echo "🧪 Integration tests (docker: qdrant, embed, otel-collector recommended)..."
	@go vet ./...
	@QDRANT_URL=$${QDRANT_URL:-http://127.0.0.1:6333} \
	 EMBED_URL=$${EMBED_URL:-http://127.0.0.1:18001} \
	 OTEL_EXPORTER_OTLP_ENDPOINT=$${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4318} \
	 go test -tags=integration -race -count=1 \
		./internal/router/... \
		./internal/telemetry/...

lint:
	@echo "🔍 golangci-lint..."
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...

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
	@echo "🐳 Starting infrastructure..."
	@$(COMPOSE) up -d

docker-up-full: docker-build
	@echo "🐳 Starting full stack (infra + gateway)..."
	@$(COMPOSE) --profile gateway up -d

docker-down:
	@echo "🛑 Stopping all containers..."
	@$(COMPOSE) --profile gateway down

docker-logs:
	@$(COMPOSE) --profile gateway logs -f

docker-clean:
	@echo "🧹 Removing containers, volumes and image..."
	@$(COMPOSE) --profile gateway down -v --remove-orphans
	@docker rmi mcp-gateway:dev 2>/dev/null || true