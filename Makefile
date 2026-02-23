# Makefile for bit-rot-detector Go utility

BINARY      := bit-rot-detector
MODULE      := github.com/YaoxuanZhang/bit-rot-detector
CMD_DIR     := ./cmd/bit-rot-detector
OUTPUT_DIR  := ./bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO          := go
GOFLAGS     := -trimpath

.PHONY: all build build-go clean clean-ui test lint vet fmt tidy dev run ui ui-dev help

## all: build the UI then the Go binary (default target)
all: ui build-go

## build: alias for build-go (Go binary only, assumes UI already built)
build: build-go

## build-go: compile a static Go binary to ./bin/
build-go:
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY) $(CMD_DIR)
	@echo "Built $(OUTPUT_DIR)/$(BINARY) (version=$(VERSION))"

## ui: install npm deps and build the React UI into internal/api/static/
ui:
	cd web && npm install && npm run build

## ui-dev: run Vite dev server with HMR (proxies /api to :8080)
ui-dev:
	cd web && npm install && npm run dev

## build-linux: cross-compile a static Linux amd64 binary
build-linux:
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(OUTPUT_DIR)/$(BINARY)-linux-amd64 $(CMD_DIR)

## build-linux-arm64: cross-compile a static Linux arm64 binary
build-linux-arm64:
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(OUTPUT_DIR)/$(BINARY)-linux-arm64 $(CMD_DIR)

## test: run all Go tests
test:
	$(GO) test -v -race -count=1 ./...

## test-cover: run tests with coverage report
test-cover:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint (must be installed separately)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed; skipping"; exit 0; }
	golangci-lint run ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format all Go source files
fmt:
	$(GO) fmt ./...

## tidy: tidy and verify go.mod / go.sum
tidy:
	$(GO) mod tidy
	$(GO) mod verify

## clean: remove build artefacts
clean:
	rm -rf $(OUTPUT_DIR) coverage.out coverage.html

## clean-ui: remove web node_modules and Vite cache
clean-ui:
	rm -rf web/node_modules web/dist web/.vite

## dev: build Go binary and run the web server locally (loads .env; default addr :8080)
dev: build-go
	$(OUTPUT_DIR)/$(BINARY) -web -addr :8080

## run: run the detector once with go run (loads .env; pass ARGS= for extra flags)
run:
	$(GO) run $(CMD_DIR) $(ARGS)

## help: show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
