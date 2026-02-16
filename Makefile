.PHONY: help install install-dev setup start stop restart cli menubar test lint format clean install-service uninstall self-improve-detect self-improve-analyze self-improve-fix self-improve-validate self-improve-apply self-improve self-improve-status self-regression go-build go-build-all go-build-daemon go-build-cli go-test go-test-verbose go-bench go-daemon go-daemon-debug go-clean go-lint go-install

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Setup:"
	@echo "  install          Create venv and install meept"
	@echo "  install-dev      Create venv and install with dev dependencies"
	@echo "  setup            Create ~/.meept directory and default config"
	@echo ""
	@echo "Python Daemon:"
	@echo "  start            Start the Python daemon (background)"
	@echo "  stop             Stop the running daemon"
	@echo "  restart          Stop and restart the daemon"
	@echo ""
	@echo "Interfaces:"
	@echo "  cli              Launch the Python interactive CLI"
	@echo "  menubar          Build the Tauri menubar app"
	@echo ""
	@echo "Development:"
	@echo "  test             Run the Python test suite"
	@echo "  lint             Check code style with ruff"
	@echo "  format           Auto-format code with ruff"
	@echo "  clean            Remove venv, build artifacts, and __pycache__"
	@echo ""
	@echo "Service:"
	@echo "  install-service  Install as a system service (launchd/systemd)"
	@echo "  uninstall        Remove the system service"
	@echo ""
	@echo "Go Build:"
	@echo "  go-build-all     Build all Go binaries (daemon + CLI)"
	@echo "  go-build-daemon  Build only the Go daemon binary"
	@echo "  go-build-cli     Build only the Go CLI binary"
	@echo "  go-build         Alias for go-build-all"
	@echo "  go-install       Install Go binaries to GOPATH/bin"
	@echo ""
	@echo "Go Development:"
	@echo "  go-test          Run Go unit tests (short mode)"
	@echo "  go-test-verbose  Run Go tests with verbose output"
	@echo "  go-test-cover    Run Go tests with coverage report"
	@echo "  go-bench         Run Go benchmarks"
	@echo "  go-lint          Run golangci-lint"
	@echo "  go-clean         Remove Go build artifacts"
	@echo ""
	@echo "Go Daemon:"
	@echo "  go-daemon        Build and run Go daemon (foreground)"
	@echo "  go-daemon-debug  Run Go daemon with debug logging"

VENV := .venv
PYTHON := $(VENV)/bin/python
PIP := $(VENV)/bin/pip
MEEPT_HOME := $(HOME)/.meept
SOCK := $(MEEPT_HOME)/meept.sock
PID := $(MEEPT_HOME)/meept.pid

install:
	python3 -m venv $(VENV)
	$(PIP) install -e ".[all]"

install-dev:
	python3 -m venv $(VENV)
	$(PIP) install -e ".[all,dev]"

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

start: setup
	$(PYTHON) -m meept

stop:
	@if [ -f $(PID) ]; then \
		kill $$(cat $(PID)) 2>/dev/null || true; \
		rm -f $(PID) $(SOCK); \
		echo "Daemon stopped"; \
	else \
		echo "No PID file found"; \
	fi

restart: stop start

cli:
	$(PYTHON) -m cli

menubar:
	cd menubar && npm install && cargo tauri build

test:
	$(PYTHON) -m pytest tests/ -v --tb=short

lint:
	$(VENV)/bin/ruff check src/ cli/ tests/
	$(VENV)/bin/ruff format --check src/ cli/ tests/

format:
	$(VENV)/bin/ruff check --fix src/ cli/ tests/
	$(VENV)/bin/ruff format src/ cli/ tests/

clean:
	rm -rf $(VENV) dist/ build/ *.egg-info
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true

install-service:
	@case "$$(uname)" in \
		Darwin) \
			sed "s|{{PYTHON}}|$$(pwd)/$(PYTHON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
				service/com.meept.daemon.plist > ~/Library/LaunchAgents/com.meept.daemon.plist; \
			launchctl load ~/Library/LaunchAgents/com.meept.daemon.plist; \
			echo "Installed launchd service"; \
			;; \
		Linux) \
			sed "s|{{PYTHON}}|$$(pwd)/$(PYTHON)|g; s|{{MEEPT_DIR}}|$$(pwd)|g" \
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
# Self-Improvement System
# =============================================================================

self-improve-detect:
	@echo "Detecting issues..."
	$(PYTHON) -m meept.selfimprove.cli detect

self-improve-analyze:
	@echo "Analyzing root causes..."
	$(PYTHON) -m meept.selfimprove.cli analyze

self-improve-fix:
	@echo "Generating fixes..."
	$(PYTHON) -m meept.selfimprove.cli generate-fixes

self-improve-validate:
	@echo "Validating fixes in sandbox..."
	$(PYTHON) -m meept.selfimprove.cli validate

self-improve-apply:
	@echo "Applying validated fixes (requires approval)..."
	$(PYTHON) -m meept.selfimprove.cli apply --require-approval

self-improve:
	@echo "Running full self-improvement cycle..."
	$(PYTHON) -m meept.selfimprove.cli full-cycle --interactive

self-improve-status:
	$(PYTHON) -m meept.selfimprove.cli status

self-regression:
	@echo "Running regression tests after self-modification..."
	$(MAKE) test && $(PYTHON) -m meept.selfimprove.cli regression-check

# =============================================================================
# Go Build System
# =============================================================================

GO_BIN_DIR := bin
GO_DAEMON := $(GO_BIN_DIR)/meept-daemon
GO_CLI := $(GO_BIN_DIR)/meept

# Build flags
GO_LDFLAGS := -s -w
GO_BUILD_FLAGS := -ldflags "$(GO_LDFLAGS)"

# Version info (if available from git)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Inject version info
GO_LDFLAGS_VERSION := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

go-build: go-build-all

go-build-all: go-build-daemon go-build-cli
	@echo ""
	@echo "Build complete:"
	@ls -lh $(GO_BIN_DIR)/

go-build-daemon:
	@mkdir -p $(GO_BIN_DIR)
	@echo "Building Go daemon..."
	go build $(GO_BUILD_FLAGS) -o $(GO_DAEMON) ./cmd/meept-daemon
	@echo "Built $(GO_DAEMON) ($$(du -h $(GO_DAEMON) | cut -f1))"

go-build-cli:
	@mkdir -p $(GO_BIN_DIR)
	@echo "Building Go CLI..."
	go build $(GO_BUILD_FLAGS) -o $(GO_CLI) ./cmd/meept
	@echo "Built $(GO_CLI) ($$(du -h $(GO_CLI) | cut -f1))"

go-build-release: GO_BUILD_FLAGS := -ldflags "$(GO_LDFLAGS) $(GO_LDFLAGS_VERSION)"
go-build-release: go-build-all
	@echo "Release build with version $(VERSION)"

go-install:
	@echo "Installing Go binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installed: meept-daemon, meept"

# =============================================================================
# Go Testing
# =============================================================================

go-test:
	@echo "Running Go tests (short mode)..."
	go test ./... -short

go-test-verbose:
	@echo "Running Go tests (verbose)..."
	go test ./... -v

go-test-cover:
	@echo "Running Go tests with coverage..."
	@mkdir -p coverage
	go test ./... -coverprofile=coverage/coverage.out
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report: coverage/coverage.html"

go-test-race:
	@echo "Running Go tests with race detector..."
	go test ./... -race

go-bench:
	@echo "Running Go benchmarks..."
	go test ./pkg/security/... -bench=. -benchmem
	go test ./internal/rpc/... -bench=. -benchmem
	go test ./internal/bus/... -bench=. -benchmem

go-bench-all:
	@echo "Running all Go benchmarks..."
	go test ./... -bench=. -benchmem -run=^$$ | tee bench.txt

# =============================================================================
# Go Daemon Runtime
# =============================================================================

go-daemon: go-build-daemon setup
	@echo "Starting Go daemon..."
	$(GO_DAEMON) --foreground

go-daemon-debug: go-build-daemon setup
	@echo "Starting Go daemon (debug mode)..."
	$(GO_DAEMON) --foreground --log-level debug

go-status: go-build-cli
	@$(GO_CLI) status

# =============================================================================
# Go Development Tools
# =============================================================================

go-clean:
	rm -rf $(GO_BIN_DIR)/ coverage/
	go clean -cache -testcache

go-lint:
	@echo "Running Go linter..."
	@which golangci-lint > /dev/null 2>&1 || (echo "Install: brew install golangci-lint" && exit 1)
	golangci-lint run ./...

go-fmt:
	@echo "Formatting Go code..."
	go fmt ./...
	@echo "Done"

go-vet:
	@echo "Running go vet..."
	go vet ./...

go-mod-tidy:
	@echo "Tidying Go modules..."
	go mod tidy

go-deps:
	@echo "Downloading Go dependencies..."
	go mod download

go-update-deps:
	@echo "Updating Go dependencies..."
	go get -u ./...
	go mod tidy

# Cross-compilation targets
go-build-linux:
	@mkdir -p $(GO_BIN_DIR)
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-daemon-linux-amd64 ./cmd/meept-daemon
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-linux-amd64 ./cmd/meept
	GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-daemon-linux-arm64 ./cmd/meept-daemon
	GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-linux-arm64 ./cmd/meept
	@echo "Linux builds complete"

go-build-darwin:
	@mkdir -p $(GO_BIN_DIR)
	GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-daemon-darwin-amd64 ./cmd/meept-daemon
	GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-darwin-amd64 ./cmd/meept
	GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-daemon-darwin-arm64 ./cmd/meept-daemon
	GOOS=darwin GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(GO_BIN_DIR)/meept-darwin-arm64 ./cmd/meept
	@echo "macOS builds complete"

go-build-cross: go-build-linux go-build-darwin
	@echo ""
	@echo "Cross-compilation complete:"
	@ls -lh $(GO_BIN_DIR)/
