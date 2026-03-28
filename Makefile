# Global Settings
# Use bash as the shell for consistent behavior on macOS/Linux
SHELL := /bin/bash

# Variables
BINARY_NAME=mcp-gateway
MAIN_PATH=cmd/gateway/main.go

# Colors
BLUE 	:= \033[1;34m
YELLOW  := \033[1;33m
CYAN    := \033[36m
RESET   := \033[0m

.DEFAULT_GOAL := help
.PHONY: help build run test clean docker-up docker-down docker-logs tidy

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
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "test" "Run all unit tests with race detection"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "tidy" "Clean up and verify Go modules"
	@printf "\n"
	@printf "$(BLUE)▶ Infrastructure & Docker$(RESET)\n"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-up" "Spin up Qdrant and MCP mock servers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-down" "Stop and remove all containers"
	@printf "  $(CYAN)%-20s$(RESET) %s\n" "docker-logs" "Follow logs from infrastructure services"
	@printf "\n"

# Targets Implementation

build:
	@echo "🛠️  Building $(BINARY_NAME)..."
	@mkdir -p bin
	@go build -o bin/$(BINARY_NAME) $(MAIN_PATH)

run:
	@echo "🚀 Starting $(BINARY_NAME)..."
	@go run $(MAIN_PATH)

test:
	@echo "🧪 Running tests..."
	@go test -v -race ./...

tidy:
	@echo "📦 Tidying Go modules..."
	@go mod tidy
	@go mod verify

clean:
	@echo "🧹 Cleaning..."
	@rm -rf bin/
	@rm -rf tmp/

docker-up:
	@echo "🐳 Starting Docker infrastructure..."
	@docker-compose -f deployments/docker-compose.yaml up -d

docker-down:
	@echo "🛑 Stopping infrastructure..."
	@docker-compose -f deployments/docker-compose.yaml down

docker-logs:
	@docker-compose -f deployments/docker-compose.yaml logs -f