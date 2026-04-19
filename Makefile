.PHONY: help build build-all build-daemon build-cli test test-verbose test-cover test-race bench bench-all daemon daemon-debug status clean lint fmt vet mod-tidy deps update-deps install setup build-linux build-darwin build-cross docs-serve docs-build docs-generate

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Setup:"
	@echo "  setup            Create ~/.meept directory and default config"
	@echo "  deps             Download Go dependencies"
	@echo ""
	@echo "Build:"
	@echo "  build            Build all binaries (daemon + CLI + gendoc)"
	@echo "  build-daemon     Build only the daemon binary"
	@echo "  build-cli        Build only the CLI binary"
	@echo "  build-gendoc     Build only the documentation generator"
	@echo "  build-release    Build with version info from git"
	@echo "  install          Install binaries to GOPATH/bin"
	@echo ""
	@echo "Testing:"
	@echo "  test             Run tests (short mode)"
	@echo "  test-verbose     Run tests with verbose output"
	@echo "  test-cover       Run tests with coverage report"
	@echo "  test-race        Run tests with race detector"
	@echo "  bench            Run benchmarks"
	@echo ""
	@echo "Development:"
	@echo "  lint             Run golangci-lint"
	@echo "  fmt              Format code"
	@echo "  vet              Run go vet"
	@echo "  mod-tidy         Tidy go modules"
	@echo "  clean            Remove build artifacts"
	@echo ""
	@echo "Daemon:"
	@echo "  daemon           Build and run daemon (foreground)"
	@echo "  daemon-debug     Run daemon with debug logging"
	@echo "  status           Check daemon status"
	@echo ""
	@echo "Cross-compilation:"
	@echo "  build-linux      Build for Linux (amd64/arm64)"
	@echo "  build-darwin     Build for macOS (amd64/arm64)"
	@echo "  build-cross      Build for all platforms"
	@echo ""
	@echo "Service:"
	@echo "  install-service  Install as a system service (launchd/systemd)"
	@echo "  uninstall        Remove the system service"
	@echo ""
	@echo "Documentation:"
	@echo "  docs-serve       Start local docs dev server"
	@echo "  docs-build       Build static docs site"
	@echo "  docs-generate    Generate reference docs from Go source"

MEEPT_HOME := $(HOME)/.meept
BIN_DIR := bin
DAEMON := $(BIN_DIR)/meept-daemon
CLI := $(BIN_DIR)/meept

# Build flags
GO_LDFLAGS := -s -w

# Version info (if available from git)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Inject version info
GO_LDFLAGS_VERSION := -X github.com/caimlas/meept/internal/version.Version=$(VERSION) -X github.com/caimlas/meept/internal/version.Commit=$(COMMIT) -X github.com/caimlas/meept/internal/version.BuildTime=$(BUILD_TIME)

# Build flags (after version info so it can reference GO_LDFLAGS_VERSION)
GO_BUILD_FLAGS := -ldflags "$(GO_LDFLAGS) $(GO_LDFLAGS_VERSION)"
# =============================================================================
# Setup
# =============================================================================

setup:
	mkdir -p $(MEEPT_HOME)
	@if [ ! -f $(MEEPT_HOME)/meept.toml ]; then \
		cp config/meept.toml $(MEEPT_HOME)/meept.toml; \
		echo "Created $(MEEPT_HOME)/meept.toml - edit with your LLM settings"; \
	fi
	@if [ ! -d $(MEEPT_HOME)/plugins ]; then \
		mkdir -p $(MEEPT_HOME)/plugins; \
	fi
	@if [ ! -d $(MEEPT_HOME)/memory ]; then \
		mkdir -p $(MEEPT_HOME)/memory; \
	fi
	@if [ ! -d $(MEEPT_HOME)/workspaces ]; then \
		mkdir -p $(MEEPT_HOME)/workspaces; \
	fi

deps:
	@echo "Downloading Go dependencies..."
	go mod download

# =============================================================================
# Build
# =============================================================================

build: build-all

build-all: build-daemon build-cli build-gendoc
	@echo ""
	@echo "Build complete:"
	@ls -lh $(BIN_DIR)/

build-daemon:
	@mkdir -p $(BIN_DIR)
	@echo "Building daemon..."
	go build $(GO_BUILD_FLAGS) -o $(DAEMON) ./cmd/meept-daemon
	@echo "Built $(DAEMON) ($$(du -h $(DAEMON) | cut -f1))"

build-cli:
	@mkdir -p $(BIN_DIR)
	@echo "Building CLI..."
	go build $(GO_BUILD_FLAGS) -o $(CLI) ./cmd/meept
	@echo "Built $(CLI) ($$(du -h $(CLI) | cut -f1))"

build-gendoc:
	@mkdir -p $(BIN_DIR)
	@echo "Building gendoc tool..."
	go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/gendoc ./cmd/gendoc
	@echo "Built $(BIN_DIR)/gendoc ($$(du -h $(BIN_DIR)/gendoc | cut -f1))"

build-release: GO_BUILD_FLAGS := -ldflags "$(GO_LDFLAGS) $(GO_LDFLAGS_VERSION)"
build-release: build-all
	@echo "Release build with version $(VERSION)"

install:
	@echo "Installing binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installed: meept-daemon, meept"

# =============================================================================
# Testing
# =============================================================================

test:
	@echo "Running tests (short mode)..."
	go test ./... -short

test-verbose:
	@echo "Running tests (verbose)..."
	go test ./... -v

test-cover:
	@echo "Running tests with coverage..."
	@mkdir -p coverage
	go test ./... -coverprofile=coverage/coverage.out
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report: coverage/coverage.html"

test-race:
	@echo "Running tests with race detector..."
	go test ./... -race

bench:
	@echo "Running benchmarks..."
	go test ./pkg/security/... -bench=. -benchmem
	go test ./internal/rpc/... -bench=. -benchmem
	go test ./internal/bus/... -bench=. -benchmem

bench-all:
	@echo "Running all benchmarks..."
	go test ./... -bench=. -benchmem -run=^$$ | tee bench.txt

# =============================================================================
# Daemon Runtime
# =============================================================================

daemon: build-daemon setup
	@echo "Starting daemon..."
	$(DAEMON) --foreground

daemon-debug: build-daemon setup
	@echo "Starting daemon (debug mode)..."
	$(DAEMON) --foreground --log-level debug

status: build-cli
	@$(CLI) status

# =============================================================================
# Development Tools
# =============================================================================

clean:
	rm -rf $(BIN_DIR)/ coverage/
	go clean -cache -testcache

lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null 2>&1 || (echo "Install: brew install golangci-lint" && exit 1)
	golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Done"

vet:
	@echo "Running go vet..."
	go vet ./...

mod-tidy:
	@echo "Tidying Go modules..."
	go mod tidy

update-deps:
	@echo "Updating Go dependencies..."
	go get -u ./...
	go mod tidy

# =============================================================================
# Cross-compilation
# =============================================================================

build-linux:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-daemon-linux-amd64 ./cmd/meept-daemon
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-linux-amd64 ./cmd/meept
	GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-daemon-linux-arm64 ./cmd/meept-daemon
	GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-linux-arm64 ./cmd/meept
	@echo "Linux builds complete"

build-darwin:
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-daemon-darwin-amd64 ./cmd/meept-daemon
	GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-darwin-amd64 ./cmd/meept
	GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-daemon-darwin-arm64 ./cmd/meept-daemon
	GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(BIN_DIR)/meept-darwin-arm64 ./cmd/meept
	@echo "macOS builds complete"

build-cross: build-linux build-darwin
	@echo ""
	@echo "Cross-compilation complete:"
	@ls -lh $(BIN_DIR)/

# =============================================================================
# Service Installation
# =============================================================================

install-service:
	@case "$$(uname)" in \
		Darwin) \
			sed "s|{{DAEMON}}|$$(pwd)/$(DAEMON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
				service/com.meept.daemon.plist > ~/Library/LaunchAgents/com.meept.daemon.plist; \
			launchctl load ~/Library/LaunchAgents/com.meept.daemon.plist; \
			echo "Installed launchd service"; \
			;; \
		Linux) \
			sed "s|{{DAEMON}}|$$(pwd)/$(DAEMON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
				service/meept.service > ~/.config/systemd/user/meept.service; \
			systemctl --user daemon-reload; \
			systemctl --user enable meept; \
			systemctl --user start meept; \
			echo "Installed systemd service"; \
			;; \
	esac

uninstall:
	@case "$$(uname)" in \
		Darwin) \
			launchctl unload ~/Library/LaunchAgents/com.meept.daemon.plist 2>/dev/null || true; \
			rm -f ~/Library/LaunchAgents/com.meept.daemon.plist; \
			;; \
		Linux) \
			systemctl --user stop meept 2>/dev/null || true; \
			systemctl --user disable meept 2>/dev/null || true; \
			rm -f ~/.config/systemd/user/meept.service; \
			;; \
	esac
	@echo "Service uninstalled (data preserved at $(MEEPT_HOME))"

# =============================================================================
# Documentation
# =============================================================================

docs-deps:
	@echo "Installing docs dependencies..."
	pip3 install -r docs/requirements.txt

docs-serve: docs-deps
	@echo "Starting docs dev server..."
	mkdocs serve

docs-build: docs-deps
	@echo "Building docs..."
	mkdocs build -d site

docs-generate:
	@echo "Generating reference docs from Go source..."
	go run ./cmd/gendoc

# =============================================================================
# Legacy Aliases (for backwards compatibility)
# =============================================================================

go-build: build
go-build-all: build-all
go-build-daemon: build-daemon
go-build-cli: build-cli
go-test: test
go-test-verbose: test-verbose
go-test-cover: test-cover
go-bench: bench
go-daemon: daemon
go-daemon-debug: daemon-debug
go-clean: clean
go-lint: lint
go-install: install
