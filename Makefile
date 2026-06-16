.PHONY: sdk-generate sdk-generate-go sdk-generate-dart sdk-clean help build build-all uninstall-all uninstall-gui build-daemon build-cli build-gui test test-verbose test-cover test-race bench bench-all daemon daemon-debug devbuild status clean lint fmt vet mod-tidy deps update-deps install setup hooks build-linux build-darwin build-cross docs-serve docs-build docs-generate menubar menubar-clean menubar-install menubar-xcode menubar-install-app gui-deps gui-clean gui-web gui-web-run gui-dev-server

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Setup:"
	@echo "  setup            Create ~/.meept directory and default config"
	@echo "  hooks            Install git hooks (pre-commit lint)"
	@echo "  deps             Download Go dependencies"
	@echo ""
	@echo "Build:"
	@echo "  build            Build all binaries (daemon + CLI + gendoc + lite)"
	@echo "  build-daemon     Build only the daemon binary"
	@echo "  build-cli        Build only the CLI binary"
	@echo "  build-gendoc     Build only the documentation generator"
	@echo "  build-gui        Build Flutter GUI (macOS/linux/windows)"
	@echo "  build-release    Build with version info from git"
	@echo "  menubar          Build macOS menubar app (Swift, binary)"
	@echo "  menubar-xcode    Build macOS menubar app (Xcode, .app bundle)"
	@echo "  menubar          Build menubar app (SPM binary)"
	@echo "  menubar-app      Create .app bundle from menubar binary"
	@echo "  menubar-install  Install menubar binary to ~/Applications"
	@echo "  menubar-install-app-bundle  Install .app bundle to ~/Applications"
	@echo "  menubar-app          Build menubar as .app bundle"
	@echo "  menubar-install-app-bundle  Install menubar .app to ~/Applications"
	@echo "  install          Install binaries + GUI to GOPATH/bin"
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
	@echo "  gui-deps         Install Flutter/CocoaPods dependencies (macOS)"
	@echo "  gui-web          Build Flutter web app (release)"
	@echo "  gui-web-run      Run Flutter web dev server with hot reload (Chrome)"
	@echo "  gui-dev-server   Run Flutter web dev server (web-server backend)"
	@echo ""
	@echo "Daemon:"
	@echo "  daemon           Build and run daemon (foreground)"
	@echo "  daemon-debug     Run daemon with debug logging"
	@echo "  devbuild         Rebuild daemon+CLI+GUI (incremental), install (preserves ~/.meept)"
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
	@echo "  uninstall-all    Remove ALL binaries, apps, config, and databases"
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

# Flutter GUI directory and platform (needed by multiple targets)
FLUTTER_UI_DIR := ui/flutter_ui
ifeq ($(shell uname -s),Darwin)
  GUI_PLATFORM := macos
else ifeq ($(shell uname -s),Linux)
  GUI_PLATFORM := linux
else
  GUI_PLATFORM := windows
endif
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

build-all: build-daemon build-cli build-gendoc build-gui build-lite
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

install: build menubar-app build-gui
	@echo "Installing binaries to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "Installing meept-lite..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-lite
	@echo "Installing GUI app to ~/Applications..."
	mkdir -p ~/Applications
	@if [ -d $(BIN_DIR)/meept_gui.app ]; then \
		rm -rf ~/Applications/Meept\ Client\ GUI.app; \
		cp -r $(BIN_DIR)/meept_gui.app ~/Applications/Meept\ Client\ GUI.app; \
		touch ~/Applications/Meept\ Client\ GUI.app/.metadata_never_index; \
		rm -rf $(BIN_DIR)/meept_gui.app; \
		echo "Installed: ~/Applications/Meept Client GUI.app"; \
	else \
		echo "Skipping GUI app (not built — run 'make build-gui' first)"; \
	fi
	@echo "Installing menubar app bundle to ~/Applications..."
	rm -rf ~/Applications/MeeptMenuBar.app
	cp -r $(MENUBAR_APP) ~/Applications/
	@touch ~/Applications/MeeptMenuBar.app/.metadata_never_index
	@rm -rf $(MENUBAR_DIR)/.build
	@echo "Installed: ~/Applications/MeeptMenuBar.app"
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

# =============================================================================
# Development: fast iteration build
# =============================================================================

# devbuild: Rebuild only changed Go code and the Flutter GUI (incremental,
# no dependency re-download), install to GOPATH/bin and ~/Applications,
# and run setup to recreate config templates.
#
# Speed notes:
#   - Go: uses go build directly (incremental Go build cache)
#   - Flutter: uses flutter build (incremental, skips if no .dart changes)
#   - Does NOT run gui-deps, pod install, or dependency downloads
#   - Does NOT build menubar, gendoc, or meept-lite
#   - Does NOT wipe ~/.meept (preserves config and data)
.PHONY: devbuild
devbuild:
	@echo "==> Building daemon + CLI (incremental)..."
	@mkdir -p $(BIN_DIR)
	@go build $(GO_BUILD_FLAGS) -o $(DAEMON) ./cmd/meept-daemon
	@go build $(GO_BUILD_FLAGS) -o $(CLI) ./cmd/meept
	@echo "==> Installing Go binaries to GOPATH/bin..."
	@go install $(GO_BUILD_FLAGS) ./cmd/meept-daemon
	@go install $(GO_BUILD_FLAGS) ./cmd/meept
	@echo "==> Setting up directories and config files..."
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@for f in $(CONFIG_FILES); do \
		src="config/$$(basename $$f)"; \
		if [ -f $$src ]; then \
			cp $$src $$f; \
			echo "  created $$f"; \
		else \
			echo "  skipping $$f (no template)"; \
		fi; \
	done
	@if [ -d config/agents ]; then \
		cp -r config/agents/* $(MEEPT_HOME)/agents/ 2>/dev/null || true; \
		echo "  copied agent definitions"; \
	fi
	@if [ -d config/prompts ]; then \
		cp -r config/prompts/* $(MEEPT_HOME)/prompts/ 2>/dev/null || true; \
		echo "  copied prompts"; \
	fi
ifeq ($(GUI_PLATFORM),macos)
	@echo "==> Building Flutter GUI (incremental)..."
	@cd $(FLUTTER_UI_DIR) && flutter build $(GUI_PLATFORM) --release 2>&1 | tail -1
	@echo "==> Installing GUI to ~/Applications..."
	@mkdir -p ~/Applications
	@rm -rf ~/Applications/Meept\ Client\ GUI.app
	@cp -r "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app" \
		~/Applications/Meept\ Client\ GUI.app
	@touch ~/Applications/Meept\ Client\ GUI.app/.metadata_never_index
endif
	@echo "==> devbuild complete."
	@echo "    binaries:  $$(go env GOPATH)/bin/meept{,-daemon}"
ifeq ($(GUI_PLATFORM),macos)
	@echo "    gui:       ~/Applications/Meept Client GUI.app"
endif
	@echo "    config:    $(MEEPT_HOME)"
	@echo "    start:     meept-daemon -f"

status: build-cli
	@$(CLI) status

# =============================================================================
# Development Tools
# =============================================================================

clean:
	rm -rf $(BIN_DIR)/meept_gui.app $(BIN_DIR)/meept-gui-* coverage/
	rm -rf $(MENUBAR_DIR)/MeeptMenuBar.app $(MENUBAR_DIR)/.build
	rm -rf $$(go env GOPATH)/bin/meept_gui.app $$(go env GOPATH)/bin/meept_ui.app
	@cd $(FLUTTER_UI_DIR) && flutter clean 2>/dev/null || true
	go clean -cache -testcache

lint: gosec
	@echo "Running linter..."
	@which golangci-lint > /dev/null 2>&1 || (echo "Install: brew install golangci-lint" && exit 1)
	golangci-lint run ./...

gosec:
	@echo "Running gosec security scan (G201, G202)..."
	@which gosec > /dev/null 2>&1 || (echo "Install: go install github.com/securego/gosec/v2/cmd/gosec@latest" && exit 1)
	gosec -include=G201,G202 ./...

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
	@$(MAKE) build
	@echo "Installing meept-daemon as a system service..."
	@./bin/meept-daemon service install && \
		echo "Service installed. Starting..." && \
		./bin/meept-daemon service start && \
		echo "Service started." || echo "Service install failed."

uninstall: uninstall-gui
	@./bin/meept-daemon service stop 2>/dev/null || true
	@./bin/meept-daemon service uninstall
	@echo "Service uninstalled (data preserved at $(MEEPT_HOME))"

uninstall-gui:
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Removing GUI apps and Spotlight registrations..."; \
		for app in ~/Applications/Meept\ Client\ GUI.app \
		           ~/Applications/meept_gui.app \
		           ~/Applications/MeeptMenuBar.app \
		           $$(go env GOPATH)/bin/meept_gui.app \
		           $$(go env GOPATH)/bin/meept_ui.app \
		           $(BIN_DIR)/meept_gui.app; do \
			if [ -d "$$app" ]; then \
				echo "  Removing Spotlight index for $$app"; \
				mdutil -i off "$$app" 2>/dev/null || true; \
				rm -rf "$$app"; \
			fi; \
		done; \
		rm -rf $(MENUBAR_DIR)/MeeptMenuBar.app $(MENUBAR_DIR)/.build; \
		echo "Flushing Spotlight cache..."; \
		mdimport -r /System/Library/Frameworks/CoreServices.framework/Frameworks/Metadata.framework/Versions/A/Support/mdimporter 2>/dev/null || mdimport ~/Applications 2>/dev/null || true; \
		echo "GUI apps removed. Spotlight may take a few minutes to update."; \
	fi

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
	mage -d magefiles docsGenerate

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

FLUTTER_UI_DIR := ui/flutter_ui
UNAME_S := $(shell uname -s 2>/dev/null || echo Linux)
ifeq ($(UNAME_S),Darwin)
  GUI_BIN := $(BIN_DIR)/meept-gui-darwin-$(shell uname -m)
else ifeq ($(UNAME_S),Linux)
  GUI_BIN := $(BIN_DIR)/meept-gui-linux-$(shell uname -m)
else
  GUI_BIN := $(BIN_DIR)/meept-gui-windows-amd64.exe
endif

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
	@touch $(MENUBAR_APP)/.metadata_never_index
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
	@touch ~/Applications/MeeptMenuBar.app/.metadata_never_index
	@rm -rf $(MENUBAR_DIR)/.build
	@echo "Installed: ~/Applications/MeeptMenuBar.app"

# =============================================================================
# Flutter GUI (meept-gui)
# =============================================================================

gui-deps:
	@echo "Checking Flutter and CocoaPods dependencies..."
	@if ! command -v flutter >/dev/null 2>&1; then \
		echo "Error: Flutter is not installed. Install from https://flutter.dev"; \
		exit 1; \
	fi
	@if ! flutter --version >/dev/null 2>&1; then \
		echo "Error: Flutter is not working correctly. Run 'flutter doctor'."; \
		exit 1; \
	fi
	@echo "Flutter version: $$(flutter --version --machine 2>/dev/null | head -1 || flutter --version)"
	@echo "Resolving Flutter packages..."
	cd $(FLUTTER_UI_DIR) && flutter pub get
ifeq ($(GUI_PLATFORM),macos)
	@if ! command -v pod >/dev/null 2>&1; then \
		echo "CocoaPods not found. Installing..."; \
		sudo gem install cocoapods; \
	fi
	@echo "Running CocoaPods install for macOS Flutter app..."
	cd $(FLUTTER_UI_DIR)/macos && pod install
	@echo "Applying Swift 6 compatibility patch for flutter_tts..."
	@if [ -f "$(FLUTTER_UI_DIR)/patch-flutter-tts.sh" ]; then \
		$(FLUTTER_UI_DIR)/patch-flutter-tts.sh; \
	else \
		echo "Warning: patch-flutter-tts.sh not found, Swift 6 warnings may appear"; \
	fi
endif
	@echo "Flutter dependencies check complete."

gui-clean:
	rm -rf $(FLUTTER_UI_DIR)/build

build-gui: gui-deps
	@mkdir -p $(BIN_DIR)
	@echo "Building meept-gui for $(GUI_PLATFORM)..."
	cd $(FLUTTER_UI_DIR) && flutter build $(GUI_PLATFORM) --release
ifeq ($(GUI_PLATFORM),macos)
	@echo "Setting version $(VERSION) in macOS Info.plist..."
	@# Inject version into the built app bundle's Info.plist so the
	@# Finder and Spotlight display the correct name.
	@plutil -replace CFBundleName -string "Meept Client GUI" \
	    "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app/Contents/Info.plist"
	@plutil -replace CFBundleDisplayName -string "Meept Client GUI" \
	    "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app/Contents/Info.plist"
	@plutil -replace CFBundleShortVersionString -string "$(VERSION)" \
	    "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app/Contents/Info.plist"
	@mv "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app" \
	    "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept Client GUI.app"
	@# Re-sign ad-hoc after modifying Info.plist so macOS Gatekeeper is happy
	@codesign --force --deep --sign - \
	    "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept Client GUI.app" >/dev/null 2>&1 || true
	@rm -rf $(BIN_DIR)/meept_gui.app
	@cp -r "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept Client GUI.app" $(BIN_DIR)/meept_gui.app
	@echo "Built $(BIN_DIR)/meept_gui.app ($$(du -h $(BIN_DIR)/meept_gui.app | cut -f1))"
	@touch $(BIN_DIR)/meept_gui.app/.metadata_never_index
	@rm -rf $(FLUTTER_UI_DIR)/build/macos/Build/Products/Release
else ifeq ($(GUI_PLATFORM),linux)
	cp $(FLUTTER_UI_DIR)/build/linux/x64/release/bundle/meept_ui $(GUI_BIN)
	@echo "Built $(GUI_BIN) ($$(du -h $(GUI_BIN) | cut -f1))"
else
	cp $(FLUTTER_UI_DIR)/build/windows/x64/runner/Release/meept_ui.exe $(GUI_BIN)
	@echo "Built $(GUI_BIN) ($$(du -h $(GUI_BIN) | cut -f1))"
endif

# =============================================================================
# meept-lite (minimalistic TUI client)
# =============================================================================

LITE := $(BIN_DIR)/meept-lite

build-lite:
	@mkdir -p $(BIN_DIR)
	@echo "Building meept-lite..."
	go build $(GO_BUILD_FLAGS) -o $(LITE) ./cmd/meept-lite
	@echo "Built $(LITE) ($$(du -h $(LITE) | cut -f1))"

install-lite: build-lite
	@echo "Installing meept-lite to GOPATH/bin..."
	go install $(GO_BUILD_FLAGS) ./cmd/meept-lite
	@echo "Installed meept-lite to $$(go env GOPATH)/bin/meept-lite"


# Flutter Web Development
gui-web:
	@echo "Building Flutter web app..."
	cd $(FLUTTER_UI_DIR) && flutter build web --release
	@echo "Built $(FLUTTER_UI_DIR)/build/web"

gui-web-run:
	@echo "Starting Flutter web dev server with hot reload..."
	@echo ""
	@echo "============================================"
	@echo "  Meept Flutter Web Dev Server"
	@echo "============================================"
	@echo ""
	@echo "  Dev URL: http://localhost:59714"
	@echo "  API:     http://localhost:8081/api/v1"
	@echo ""
	@echo "  Hot reload: press 'r'"
	@echo "  Hot restart: press 'R'"
	@echo "  Quit: press 'q'"
	@echo ""
	@if ! curl -s http://localhost:8081/health > /dev/null 2>&1; then \
		echo "  [!] WARNING: Daemon not detected on port 8081"; \
		echo "  The app will not work without the daemon running."; \
		echo ""; \
		echo "  Fix: Run 'make daemon' in another terminal"; \
		echo "       Or enable HTTP in ~/.meept/meept.json5:"; \
		echo ""; \
		echo "       transport: {"; \
		echo "         http: { enabled: true, addr: \":8081\" }"; \
		echo "       }"; \
		echo ""; \
		echo "  Continuing anyway..."; \
		echo ""; \
	else \
		echo "  [OK] Daemon detected on port 8081"; \
		echo ""; \
	fi
	cd $(FLUTTER_UI_DIR) && flutter run -d chrome --web-port=59714 

gui-dev-server:
	@echo "Starting Flutter web dev server (web-server target)..."
	@echo "Open http://localhost:59714 in your browser"
	cd $(FLUTTER_UI_DIR) && flutter run -d web-server --web-port=59714 

# =============================================================================
# Full Uninstall - Remove all binaries, apps, and configurations
# =============================================================================

uninstall-all: uninstall uninstall-gui
	@echo "Uninstalling all Meept components..."
	@echo ""
	@echo "Removing Go binaries from GOPATH/bin..."
	rm -f $$(go env GOPATH)/bin/meept
	rm -f $$(go env GOPATH)/bin/meept-daemon
	rm -f $$(go env GOPATH)/bin/meept-lite
	rm -f $$(go env GOPATH)/bin/meept-gui
	@echo "Removing local build artifacts..."
	rm -rf $(BIN_DIR)/meept
	rm -rf $(BIN_DIR)/meept-daemon
	rm -rf $(BIN_DIR)/meept-lite
	rm -rf $(BIN_DIR)/meept_gui.app
	rm -rf $(BIN_DIR)/meept_ui.app
	@echo "Removing configuration directory ($(MEEPT_HOME))..."
	rm -rf $(MEEPT_HOME)
	@echo "Removing session/task databases..."
	rm -f ~/.meept/sessions.db
	rm -f ~/.meept/tasks.db
	rm -f ~/.meept/queue.db
	rm -f ~/.meept/plans.db
	rm -f ~/.meept/metrics.db
	rm -f ~/.meept/projects.db
	@echo "Removing memory databases..."
	rm -rf ~/.meept/memory/
	@echo "Removing cached skills and plugins..."
	rm -rf ~/.meept/skills/
	rm -rf ~/.meept/plugins/
	@echo "Removing workspaces..."
	rm -rf ~/.meept/workspaces/
	@echo ""
	@echo "Full uninstall complete."
	@echo "Note: Flutter build cache not removed. Run 'cd ui/flutter_ui && flutter clean' if needed."

# Documentation generation
	@echo "Regenerating all documentation..."
	go generate ./...
	@echo "Documentation generation complete"

# =============================================================================
# OpenAPI SDK Generation
# =============================================================================

SDK_DIR := sdk

.PHONY: sdk-generate sdk-generate-go sdk-generate-dart sdk-clean

# Generate all SDKs from OpenAPI spec
sdk-generate: sdk-generate-go sdk-generate-dart
	@echo "SDK generation complete."

# =============================================================================
# OpenAPI SDK Generation
# =============================================================================

SDK_DIR := sdk

.PHONY: sdk-generate sdk-generate-go sdk-generate-dart sdk-clean sdk-test

# Generate all SDKs from OpenAPI spec
sdk-generate: sdk-generate-go sdk-generate-dart
	@echo ""
	@echo "SDK generation complete."
	@echo "  Go SDK:  $(SDK_DIR)/go/"
	@echo "  Dart SDK: $(SDK_DIR)/dart/"

# Generate Go SDK
sdk-generate-go:
	@echo "Generating Go SDK..."
	@mkdir -p $(SDK_DIR)/go
	@openapi-generator-cli generate \
		-i docs/reference/http-api/openapi.yaml \
		-g go \
		-o $(SDK_DIR)/go \
		--skip-validate-spec \
		--additional-properties=packageName=meeptclient,packageVersion=0.2.0,generateInterfaces=true,interfaceMode=deferentially
	@echo "Go SDK generated at $(SDK_DIR)/go/"

# Generate Dart SDK  
sdk-generate-dart:
	@echo "Generating Dart SDK..."
	@mkdir -p $(SDK_DIR)/dart
	@openapi-generator-cli generate \
		-i docs/reference/http-api/openapi.yaml \
		-g dart \
		-o $(SDK_DIR)/dart \
		--skip-validate-spec \
		--additional-properties=packageName=meept_client,packageVersion=0.2.0,pubName=meept_client,useNullSafety=true
	@echo "Dart SDK generated at $(SDK_DIR)/dart/"

# Clean generated SDKs
sdk-clean:
	@echo "Cleaning generated SDKs..."
	rm -rf $(SDK_DIR)/go $(SDK_DIR)/dart
	@echo "SDKs cleaned."

# Test SDKs compile
sdk-test: sdk-test-go sdk-test-dart

sdk-test-go:
	@echo "Testing Go SDK compiles..."
	cd $(SDK_DIR)/go && go build ./...
	@echo "Go SDK compiles OK"

sdk-test-dart:
	@echo "Testing Dart SDK compiles..."
	cd $(SDK_DIR)/dart && flutter pub get && dart analyze
	@echo "Dart SDK compiles OK"
