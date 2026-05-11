.PHONY: help build build-all build-daemon build-cli test test-verbose test-cover test-race bench bench-all daemon daemon-debug status clean lint fmt vet mod-tidy deps update-deps install setup hooks build-linux build-darwin build-cross docs-serve docs-build docs-generate menubar menubar-clean menubar-install menubar-xcode menubar-install-app

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Setup:"
	@echo "  setup            Create ~/.meept directory and default config"
	@echo "  hooks            Install git hooks (pre-commit lint)"
	@echo "  deps             Download Go dependencies"
	@echo ""
	@echo "Build:"
	@echo "  build            Build all binaries (daemon + CLI + gendoc)"
	@echo "  build-daemon     Build only the daemon binary"
	@echo "  build-cli        Build only the CLI binary"
	@echo "  build-gendoc     Build only the documentation generator"
	@echo "  build-release    Build with version info from git"
	@echo "  menubar          Build macOS menubar app (Swift, binary)"
	@echo "  menubar-xcode    Build macOS menubar app (Xcode, .app bundle)"
	@echo "  menubar          Build menubar app (SPM binary)"
	@echo "  menubar-app      Create .app bundle from menubar binary"
	@echo "  menubar-install  Install menubar binary to ~/Applications"
	@echo "  menubar-install-app-bundle  Install .app bundle to ~/Applications"
	@echo "  menubar-app          Build menubar as .app bundle"
	@echo "  menubar-install-app-bundle  Install menubar .app to ~/Applications"
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
	@echo "  menubar-clean    Remove menubar build artifacts"
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

# Config templates to install
CONFIG_FILES := \
	$(MEEPT_HOME)/meept.json5 \
	$(MEEPT_HOME)/models.json5 \
	$(MEEPT_HOME)/presets.json5 \
	$(MEEPT_HOME)/client.json5 \
	$(MEEPT_HOME)/mcp_servers.json5 \
	$(MEEPT_HOME)/q_agent.json5 \
	$(MEEPT_HOME)/menubar.json5

setup:
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@if [ ! -f $(MEEPT_HOME)/meept.json5 ] && [ ! -f $(MEEPT_HOME)/meept.toml ]; then \
		cp config/meept.json5 $(MEEPT_HOME)/meept.json5; \
		echo "Created $(MEEPT_HOME)/meept.json5"; \
	fi
	@echo "Setup complete."

hooks:
	@echo "Installing git hooks..."
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Installed pre-commit hook (runs golangci-lint on staged packages)"

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

install: build
	@echo "Installing binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installing config files..."
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@echo "Copying config templates (if not present)..."
	@for f in $(CONFIG_FILES); do \
		if [ ! -f $$f ]; then \
			src="config/$$(basename $$f)"; \
			if [ -f $$src ]; then \
				cp $$src $$f; \
				echo "  created $$f"; \
			else \
				echo "  template $$src not found (skipping $$f)"; \
			fi; \
		else \
			echo "  skipping $$f (already exists)"; \
		fi; \
	done
	@echo "Copying agent definitions..."
	@if [ -d config/agents ]; then \
		cp -r config/agents/* $(MEEPT_HOME)/agents/ 2>/dev/null || true; \
		echo "  copied agent definitions"; \
	fi
	@echo "Copying prompts..."
	@if [ -d config/prompts ]; then \
		cp -r config/prompts/* $(MEEPT_HOME)/prompts/ 2>/dev/null || true; \
		echo "  copied prompts"; \
	fi
	@echo ""
	@echo "Install complete. Edit $(MEEPT_HOME)/meept.json5 to configure."

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
	@echo "Checking docs dependencies..."
	@if ! python3 -c "import mkdocs" 2>/dev/null; then \
		echo "Installing docs dependencies (creating venv)..."; \
		if [ -d docs/.venv ]; then \
			. docs/.venv/bin/activate && pip install -q --no-warn-script-location -r docs/requirements.txt 2>&1 | grep -v "notice"; \
		else \
			python3 -m venv docs/.venv && . docs/.venv/bin/activate && pip install -q --no-warn-script-location -r docs/requirements.txt 2>&1 | grep -v "notice"; \
		fi; \
	else \
		echo "Docs dependencies already installed."; \
	fi

docs-serve: docs-deps
	@echo "Starting docs dev server..."
	@if [ -d docs/.venv ]; then \
		. docs/.venv/bin/activate && mkdocs serve; \
	else \
		mkdocs serve; \
	fi

docs-build: docs-deps
	@echo "Building docs..."
	@if [ -d docs/.venv ]; then \
		. docs/.venv/bin/activate && mkdocs build -d site; \
	else \
		mkdocs build -d site; \
	fi

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

# =============================================================================
# macOS MenuBar App
# =============================================================================

MENUBAR_DIR := menubar
MENUBAR_BIN := $(MENUBAR_DIR)/.build/release/MeeptMenuBar
MENUBAR_APP := $(MENUBAR_DIR)/.build/Release/MeeptMenuBar.app
MENUBAR_XCODEPROJ := $(MENUBAR_DIR)/MeeptMenuBar.xcodeproj

# Build using Swift Package Manager (binary output, fast)
menubar:
	@echo "Building menubar app (SPM)..."
	cd $(MENUBAR_DIR) && swift build -c release
	@echo "Built $(MENUBAR_BIN)"

# Create .app bundle structure
menubar-app: menubar
	@echo "Creating .app bundle..."
	rm -rf $(MENUBAR_APP)
	mkdir -p $(MENUBAR_APP)/Contents/MacOS
	mkdir -p $(MENUBAR_APP)/Contents/Resources
	cp $(MENUBAR_BIN) $(MENUBAR_APP)/Contents/MacOS/
	@printf '<?xml version="1.0" encoding="UTF-8"?>\n\
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n\
<plist version="1.0">\n\
<dict>\n\
    <key>CFBundleExecutable</key>\n\
    <string>MeeptMenuBar</string>\n\
    <key>CFBundleIdentifier</key>\n\
    <string>com.caimlas.meept.menubar</string>\n\
    <key>CFBundleName</key>\n\
    <string>Meept MenuBar</string>\n\
    <key>CFBundlePackageType</key>\n\
    <string>APPL</string>\n\
    <key>CFBundleShortVersionString</key>\n\
    <string>1.0</string>\n\
    <key>CFBundleVersion</key>\n\
    <string>1</string>\n\
    <key>LSMinimumSystemVersion</key>\n\
    <string>13.0</string>\n\
    <key>LSUIElement</key>\n\
    <true/>\n\
    <key>NSPrincipalClass</key>\n\
    <string>NSApplication</string>\n\
</dict>\n\
</plist>\n' > $(MENUBAR_APP)/Contents/Info.plist
	@echo "Created $(MENUBAR_APP)"

menubar-clean:
	rm -rf $(MENUBAR_DIR)/.build
	rm -rf $(MENUBAR_APP)

menubar-install: menubar
	@echo "Installing menubar binary to ~/Applications..."
	mkdir -p ~/Applications
	cp $(MENUBAR_BIN) ~/Applications/MeeptMenuBar
	@echo "Installed: ~/Applications/MeeptMenuBar"

menubar-install-app-bundle: menubar-app
	@echo "Installing .app bundle to ~/Applications..."
	rm -rf ~/Applications/MeeptMenuBar.app
	cp -r $(MENUBAR_APP) ~/Applications/
	@echo "Installed: ~/Applications/MeeptMenuBar.app"
