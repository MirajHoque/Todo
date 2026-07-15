# ============================================================
# Makefile for todo-app
# Usage: make <target>
# ============================================================

APP_NAME   := todo-app
CMD_PATH   := ./cmd/server
BIN_DIR    := ./bin
BIN        := $(BIN_DIR)/$(APP_NAME)

.PHONY: all build run dev tidy lint test clean help

## all: build the binary (default target)
all: build

## build: compile the Go binary into ./bin/
build:
	@mkdir -p $(BIN_DIR)
	@echo "→ Building $(APP_NAME)..."
	go build -o $(BIN) $(CMD_PATH)
	@echo "✓ Binary at $(BIN)"

## run: build and run (requires .env to be present)
run: build
	@echo "→ Loading .env and starting server..."
	@export $$(grep -v '^#' .env | xargs) && $(BIN)

## dev: run without building binary (faster for development)
dev:
	@echo "→ Starting in dev mode (go run)..."
	@export $$(grep -v '^#' .env | xargs) && go run $(CMD_PATH)/main.go

## tidy: tidy and verify go modules
tidy:
	go mod tidy
	go mod verify

## lint: run golangci-lint (install: brew install golangci-lint)
lint:
	golangci-lint run ./...

## test: run all tests with race detector
test:
	go test -race -v ./...

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)

## help: show this help message
help:
	@grep -E '^## ' Makefile | sed 's/## //'