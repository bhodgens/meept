.PHONY: sdk-generate sdk-generate-go sdk-generate-dart sdk-clean localcert localcert-check localcert-install help build build-all uninstall-all uninstall-gui build-daemon build-cli build-gui test test-verbose test-cover test-race bench bench-all daemon daemon-debug devbuild status clean lint fmt fmt-gui fmt-check-gui vet mod-tidy deps deps-go deps-llama deps-llama-check update-deps install setup hooks build-linux build-darwin build-cross docs-serve docs-build docs-generate docs-check menubar menubar-clean menubar-install menubar-xcode menubar-install-app gui-deps gui-clean gui-web gui-web-run gui-dev-server webui graphs graphs-check compare-prep config-bootstrap dev-key gui-connect-setup gui-connect-check sync-config
	@echo "  localcert        Generate trusted SSL cert for localhost (requires mkcert)"
	@echo "  localcert-install Install mkcert and local CA (one-time setup)"

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
	@echo "  test-multiuser   Run multiuser cluster-pooling integration tests (race, 10x)"
	@echo "  totem-status     Show totem test-cluster state (users, heartbeats, peers)"
	@echo "  totem-start      Start daemons on totem1/2/3"
	@echo "  totem-stop       Stop daemons on totem1/2/3"
	@echo "  test-verbose     Run tests with verbose output"
	@echo "  test-cover       Run tests with coverage report"
	@echo "  test-race        Run tests with race detector"
	@echo "  bench            Run benchmarks"
	@echo ""
	@echo "Development:"
	@echo "  lint             Run golangci-lint"
	@echo "  fmt              Format code"
	@echo "  vet              Run go vet"
	@echo "  graphs           Regenerate connectivity graphs (bus/RPC/HTTP/WS)"
	@echo "  graphs-check     Verify connectivity graphs are fresh (local only; not wired into CI — see Makefile:graphs-check)"
	@echo "  compare-prep     Clone competitor repos into TMPDIR/meept-compare"
	@echo "  mod-tidy         Tidy go modules"
	@echo "  clean            Remove build artifacts"
	@echo "  menubar-clean    Remove menubar build artifacts"
	@echo "  gui-deps         Install Flutter/CocoaPods dependencies (macOS)"
	@echo "  gui-web          Build Flutter web app (release)"
	@echo "  gui-web-run      Run Flutter web dev server with hot reload (Chrome)"
	@echo "  webui            Alias for gui-web-run - run Flutter web dev server (Chrome)"
	@echo "  gui-dev-server   Run Flutter web dev server (web-server backend)"
	@echo "  fmt-gui          Format the Flutter/Dart tree (dart format)"
	@echo "  fmt-check-gui    Verify the Flutter/Dart tree is formatted (CI + pre-commit)"
	@echo "  localcert        Generate trusted SSL cert for localhost (requires mkcert)"
	@echo "  localcert-install Install mkcert and local CA (one-time setup)"
	@echo ""
	@echo "Comparison:"
	@echo "  compare-prep     Clone competitor repos into TMPDIR/meept-compare"
	@echo ""
	@echo "Dependencies:"
	@echo "  deps             Download Go modules + check the llama.cpp build floor"
	@echo "  deps-llama-check Verify llama.cpp >= b$(LLAMA_CPP_MIN_BUILD) (LFM2.5 tool-call floor)"
	@echo "  deps-llama       Build/install llama.cpp into $(LLAMA_CPP_PREFIX) (idempotent)"
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
	@echo "  docs-build       Build static docs site (includes llms-readme-full.txt)"
	@echo "  docs-generate    Generate reference docs from Go source"
	@echo "  docs-check       Verify generated reference docs are fresh (needs mage + gomarkdoc)"

MEEPT_HOME ?= $(HOME)/.meept
BIN_DIR := bin
DAEMON := $(BIN_DIR)/meept-daemon
CLI := $(BIN_DIR)/meept

# meept-scoped dependency prefix.
# meept keeps its own copies of runtime dependencies under one prefix so a
# Homebrew/system build can never silently shadow them. The daemon prepends
# $(MEEPT_DEPS)/llama.cpp/bin to PATH (internal/daemon/daemonpath.go
# meeptPrefixPathDirs) so a meept-managed binary wins over /opt/homebrew/bin.
MEEPT_DEPS ?= $(MEEPT_HOME)/deps
# Keep the default layout: the daemon searches $(MEEPT_DEPS)/llama.cpp/bin
# first, then $(MEEPT_DEPS)/llama.cpp/build/bin (in-tree CMake build), before
# any other PATH entry -- overriding LLAMA_CPP_PREFIX moves the build out of
# the daemon's precedence path.
LLAMA_CPP_PREFIX ?= $(MEEPT_DEPS)/llama.cpp
# Minimum llama.cpp build for local tool calling. b9660 is the upstream
# release published 2026-06-15T22:05Z: its release notes contain PR #24667
# ("chat : fix LFM2 tool-call parsing double-escaping"), the last of the
# June-2026 LFM2.5 parser changes (#21242/#24071/#24178/#24234), so it is the
# first build with the complete LFM2.5 native tool-call parser. Older builds
# log "Chat format: Generic" and fall back to a generic JSON grammar whose
# root also allows a "response" branch -- a tool-forcing prompt may then
# legally answer in prose, which is the agent narration failure this floor
# prevents. Enforced by `make deps-llama-check` (wired into deps + install).
LLAMA_CPP_MIN_BUILD ?= 9660
# Release date of the floor build; the fallback evidence for untagged dev
# builds (a --depth 1 clone reports "build 1"), verified against the commit
# date of a llama.cpp checkout at the prefix.
LLAMA_CPP_MIN_DATE ?= 2026-06-15

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

# Test package parallelism bound.
#
# WHY: the full ./internal/... sweep spawns ~98 test binaries; at go's default
# -p (GOMAXPROCS), macOS ephemeral ports (net.inet.ip.portrange, 49152-65535)
# run out mid-run and unrelated packages fail with
#   dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address
# Dose-response: -p 4 cut 17 failures to 1 (one late-run burst in the
# heaviest package); -p 2 ran the sweep clean twice. Keep -p 2. Override
# with `make test TEST_PACKAGE_PARALLELISM=N` (e.g. on Linux/CI, or after
# widening the port range via
#   sudo sysctl -w net.inet.ip.portrange.first=10240
# which is a dev-machine workaround, not a repo fix).
TEST_PACKAGE_PARALLELISM ?= 2
GO_TEST_PACKAGE_FLAGS := -p $(TEST_PACKAGE_PARALLELISM)

# Flutter GUI directory and platform (needed by multiple targets)
FLUTTER_UI_DIR := ui/flutter_ui
ifeq ($(shell uname -s),Darwin)
  GUI_PLATFORM := macos
else ifeq ($(shell uname -s),Linux)
  GUI_PLATFORM := linux
else
  GUI_PLATFORM := windows
endif

# Dev API key for the Flutter release build.
# The daemon authenticates HTTP/WebSocket clients with the per-installation key
# at $MEEPT_HOME/dev_key (pkg/constants/api_key.go DevAPIKey). The GUI must
# embed the SAME key: with a missing or guessable key every WebSocket handshake
# is rejected with HTTP 418 and the client retries forever showing
# "connecting...". scripts/ensure-dev-key.sh creates the key file if absent
# (exactly what the daemon does on first run) and NEVER substitutes a public
# default — internal/comm/http/server.go refuses to start with those.
# Recursive (not :=) so the key is resolved at recipe time, after the file
# exists, instead of being frozen when make parses the file.
MEEPT_DEV_API_KEY = $(shell MEEPT_HOME=$(MEEPT_HOME) bash scripts/ensure-dev-key.sh 2>/dev/null)
# GUI layout from client.json5 (web can't read the file at runtime)
MEEPT_GUI_LAYOUT := $(shell grep -o '"layout"[[:space:]]*:[[:space:]]*"[^"]*"' $(MEEPT_HOME)/client.json5 2>/dev/null | head -1 | sed 's/.*"\([^"]*\)"$$/\1/')
# Daemon endpoint the GUI must dial. The Flutter client connects to a fixed
# https://<host>:<port><ws_path> (ui/flutter_ui/lib/core/constants.dart) and
# cannot discover the daemon's transport.http.addr, so the installed endpoint
# is compiled in here. scripts/gui-daemon-connect.py reads
# $MEEPT_HOME/meept.json5 and defaults to localhost:8081/ws.
MEEPT_API_HOST = $(shell MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py endpoint --field host 2>/dev/null || echo localhost)
MEEPT_API_PORT = $(shell MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py endpoint --field port 2>/dev/null || echo 8081)
MEEPT_WS_PATH = $(shell MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py endpoint --field ws_path 2>/dev/null || echo /ws)
FLUTTER_DART_DEFINES = --dart-define=MEEPT_DEV_API_KEY=$(MEEPT_DEV_API_KEY) --dart-define=MEEPT_GUI_LAYOUT=$(MEEPT_GUI_LAYOUT) --dart-define=MEEPT_API_HOST=$(MEEPT_API_HOST) --dart-define=MEEPT_API_PORT=$(MEEPT_API_PORT) --dart-define=MEEPT_WS_PATH=$(MEEPT_WS_PATH)
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
	$(MEEPT_HOME)/acp_agents.json5 \
	$(MEEPT_HOME)/q_agent.json5 \
	$(MEEPT_HOME)/menubar.json5

setup:
	@mkdir -p $(MEEPT_HOME)/agents $(MEEPT_HOME)/prompts $(MEEPT_HOME)/plugins $(MEEPT_HOME)/memory $(MEEPT_HOME)/workspaces
	@if [ ! -f $(MEEPT_HOME)/meept.json5 ] && [ ! -f $(MEEPT_HOME)/meept.toml ]; then \
		cp config/meept.json5 $(MEEPT_HOME)/meept.json5; \
		echo "Created $(MEEPT_HOME)/meept.json5"; \
	fi
	@echo "Setup complete."

# config-bootstrap: populate $MEEPT_HOME with the shipped config templates.
# Copy-if-absent — never clobbers an existing (possibly user-edited) config.
# Used by `install` and `gui-connect-setup`.
config-bootstrap:
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

# dev-key: provision the per-installation API key the daemon accepts
# ($MEEPT_HOME/dev_key, 0600). Idempotent; `build-gui` embeds this key in the
# Flutter bundle so the GUI authenticates out of the box.
dev-key:
	@MEEPT_HOME=$(MEEPT_HOME) bash scripts/ensure-dev-key.sh >/dev/null
	@chmod 600 $(MEEPT_HOME)/dev_key 2>/dev/null || true
	@echo "    dev key: $(MEEPT_HOME)/dev_key"

# gui-connect-setup: make an installed meept home work with the Flutter GUI.
# The GUI dials a fixed https://host:port/ws endpoint with API-key auth, so the
# daemon must have transport.http enabled with REST + WebSocket. This target
# (a) bootstraps the config templates, (b) turns those keys on in an existing
# config — idempotent, backup kept, addr/TLS/require_auth/api_keys untouched —
# (c) provisions the shared dev key, and (d) prints the endpoint the GUI build
# will embed plus a PASS/FAIL self-check. First prerequisite of `make install`.
gui-connect-setup: config-bootstrap
	@echo "==> Configuring the daemon for the Flutter GUI (transport.http + dev key)..."
	@MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py ensure-config
	@MEEPT_HOME=$(MEEPT_HOME) bash scripts/ensure-dev-key.sh >/dev/null
	@chmod 600 $(MEEPT_HOME)/dev_key 2>/dev/null || true
	@echo "    dev key:  $(MEEPT_HOME)/dev_key (GUI builds embed this key)"
	@echo "    endpoint: $$(MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py endpoint)"
	@MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py check || \
		echo "    ^ resolve the FAIL lines above or the GUI cannot connect"
	@echo "    NOTE: restart the daemon (meept-daemon -f / launchd) to load the transport change."

# gui-connect-check: static self-check of the installed GUI connect path.
gui-connect-check:
	@MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py check


hooks:
	@echo "Installing git hooks (core.hooksPath -> .githooks)..."
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit .githooks/pre-commit-*
	@echo "Installed 17 pre-commit checks via .githooks (see .githooks/pre-commit)."
	@echo "Requires bash >= 3.2; sub-hooks run under whatever 'bash' resolves to"
	@echo "on PATH, so the same suite executes on macOS and on a Linux runner."
	@echo "Bypass with --no-verify."

deps: deps-go deps-llama-check

deps-go:
	@echo "Downloading Go dependencies..."
	@go mod download

# deps-llama-check: enforce the llama.cpp version floor. meept spawns
# `llama-server` from PATH, and a build older than $(LLAMA_CPP_MIN_BUILD)
# predates llama.cpp's LFM2.5 native tool-call parser: it logs
# "Chat format: Generic" and lets a tool-forcing prompt answer in prose
# instead of calling a tool. Fails when the resolved build is too old
# (or missing, with MEEPT_LLAMA_REQUIRE=1).
deps-llama-check:
	@MEEPT_DEPS="$(MEEPT_DEPS)" \
	 LLAMA_CPP_PREFIX="$(LLAMA_CPP_PREFIX)" \
	 LLAMA_CPP_MIN_BUILD="$(LLAMA_CPP_MIN_BUILD)" \
	 LLAMA_CPP_MIN_DATE="$(LLAMA_CPP_MIN_DATE)" \
	 bash scripts/install-llama-cpp.sh check

# deps-llama: build/install llama.cpp into $(LLAMA_CPP_PREFIX) at
# $(LLAMA_CPP_MIN_BUILD) or newer. Idempotent: skips when the installed
# binary is already at or above the floor. Default method is the official
# prebuilt tarball on macOS arm64, CMake source build elsewhere
# (override: LLAMA_CPP_METHOD=source|prebuilt, LLAMA_CPP_REF=b9660).
deps-llama:
	@MEEPT_DEPS="$(MEEPT_DEPS)" \
	 LLAMA_CPP_PREFIX="$(LLAMA_CPP_PREFIX)" \
	 LLAMA_CPP_MIN_BUILD="$(LLAMA_CPP_MIN_BUILD)" \
	 LLAMA_CPP_MIN_DATE="$(LLAMA_CPP_MIN_DATE)" \
	 bash scripts/install-llama-cpp.sh install

# =============================================================================
# Build
# =============================================================================

build: build-all

build-all: build-daemon build-cli build-gendoc build-gui build-lite graphs
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

# `gui-connect-setup` runs FIRST: it makes the installed config GUI-ready and
# provisions the dev key, so the GUI built by `build`/`build-gui` below embeds
# the key and endpoint the daemon actually serves (see the FLUTTER_DART_DEFINES
# block near the top of this file).
#
# `deps-llama-check` gates the whole install (LFM2.5 tool-call floor): an
# install must not quietly land a llama.cpp whose generic JSON grammar lets a
# tool-forcing prompt answer in prose. Override for machines that do not serve
# local models with MEEPT_LLAMA_SKIP_CHECK=1.
install: deps-llama-check gui-connect-setup build menubar-app build-gui
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
	@$(MAKE) config-bootstrap
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
	@$(MAKE) sync-config
	@echo "Install complete. Config: $(MEEPT_HOME)/meept.json5"
	@echo "  GUI endpoint: $$(MEEPT_HOME=$(MEEPT_HOME) python3 scripts/gui-daemon-connect.py endpoint)"
	@echo "  GUI key:      embedded from $(MEEPT_HOME)/dev_key at build time"
	@echo "  Restart the daemon to load the transport settings, then launch the GUI."

# sync-config: merge shipped config/{skills,agents,prompts} into the meept
# home ($MEEPT_HOME, default ~/.meept) with no-clobber semantics
# (see scripts/install-sync.py). User-modified files are preserved; new
# defaults land as <file>.new for manual review. Interactive collisions
# prompt when run from a tty. `--drift-test` self-verifies the merge matrix.
# It also reports top-level keys that the shipped models.json5/meept.json5
# declare but the installed copies lack: those flat files are copied only when
# absent (`setup`) or overwritten wholesale (`install`), so a key added to the
# template never propagated on its own. That silence left extract_model and the
# local-extract provider out of an existing install, so json_extract failed
# with "extraction model not configured" on a correctly-configured repo.
# Runs as part of `make install` and standalone.
sync-config:
	@python3 scripts/install-sync.py config
	@python3 scripts/install-sync.py --drift-test

# =============================================================================
# Testing
# =============================================================================

test:
	@echo "Running tests (short mode, package parallelism $(TEST_PACKAGE_PARALLELISM))..."
	go test $(GO_TEST_PACKAGE_FLAGS) ./... -short

test-verbose:
	@echo "Running tests (verbose, package parallelism $(TEST_PACKAGE_PARALLELISM))..."
	go test $(GO_TEST_PACKAGE_FLAGS) ./... -v

test-cover:
	@echo "Running tests with coverage (package parallelism $(TEST_PACKAGE_PARALLELISM))..."
	@mkdir -p coverage
	go test $(GO_TEST_PACKAGE_FLAGS) ./... -coverprofile=coverage/coverage.out
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report: coverage/coverage.html"

test-race:
	@echo "Running tests with race detector (package parallelism $(TEST_PACKAGE_PARALLELISM))..."
	go test $(GO_TEST_PACKAGE_FLAGS) ./... -race

test-multiuser:
	@echo "Running multiuser cluster-pooling integration tests (race, 10x)..."
	go test ./tests/integration/ -run "TestMultiuserUsersSync" -race -count=10 -v

# Repopulate the 3-node totem multiuser test cluster (totem1/2/3, root SSH).
# provision = deps + repo + build + config; start/stop/status/sync/teardown
# control it. See scripts/totem-cluster.sh header for topology.
totem-%:
	./scripts/totem-cluster.sh $*

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
	@cd $(FLUTTER_UI_DIR) && flutter build $(GUI_PLATFORM) --release $(FLUTTER_DART_DEFINES) 2>&1 | tail -1
	@echo "==> Installing GUI to ~/Applications..."
	@mkdir -p ~/Applications
	@rm -rf ~/Applications/Meept\ Client\ GUI.app
	@cp -r "$(FLUTTER_UI_DIR)/build/macos/Build/Products/Release/Meept GUI Client.app" \
		~/Applications/Meept\ Client\ GUI.app
	@touch ~/Applications/Meept\ Client\ GUI.app/.metadata_never_index
endif
	@echo "==> Building Flutter web app (incremental)..."
	@cd $(FLUTTER_UI_DIR) && flutter build web --release 2>&1 | tail -1
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

# lint-ci runs all checks expected to pass in CI. Use this as the canonical
# "is this branch shippable?" target. Includes project-specific analyzers
# (mutexio for I/O-under-mutex, predid for predictable IDs) plus the audit
# scripts that catch UTF-8 corruption bugs and Dart enum-shadowing, and
# fmt-check-gui for Dart formatting in the Flutter tree.
lint-ci: lint analyzers audit-scripts fmt-check-gui
	@echo "All CI lint checks passed."

fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Done"

# Dart binary for the Flutter formatting targets. Overridable:
#   make fmt-check-gui DART=/path/to/dart
# Resolution order: `dart` on PATH, then the dart bundled with the Flutter
# SDK (flutter/bin/cache/dart-sdk/bin/dart). Empty when neither is found;
# fmt-check-gui treats that as a hard error, never a silent pass.
DART ?= $(shell command -v dart 2>/dev/null || { \
	fb=$$(command -v flutter 2>/dev/null); \
	[ -n "$$fb" ] || exit 0; \
	rp=$$(readlink -f "$$fb" 2>/dev/null || python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$$fb" 2>/dev/null || echo "$$fb"); \
	root=$$(cd "$$(dirname "$$rp")/.." 2>/dev/null && pwd); \
	cand="$$root/bin/cache/dart-sdk/bin/dart"; \
	[ -x "$$cand" ] && echo "$$cand"; \
	true; })

# fmt-gui formats the Flutter (Dart) tree in place. Run it before staging
# Dart changes; see docs/workflows/flutter_gui.md.
fmt-gui:
	@test -n "$(DART)" || { echo "Error: dart not found. Install Flutter or add dart to PATH, then retry."; exit 1; }
	@echo "Formatting Flutter tree ($(FLUTTER_UI_DIR)) with $(DART)..."
	@$(DART) format $(FLUTTER_UI_DIR)
	@echo "Done. dart format applied to $(FLUTTER_UI_DIR); stage the result and re-commit."

# fmt-check-gui is the check-only variant used by `make lint-ci` and the
# pre-commit hook. Exits non-zero when any Dart file needs formatting.
fmt-check-gui:
	@test -n "$(DART)" || { echo "Error: dart not found. Install Flutter or add dart to PATH, then run 'make fmt-gui'."; exit 1; }
	@echo "Checking Dart formatting in $(FLUTTER_UI_DIR)..."
	@$(DART) format --output=none --set-exit-if-changed $(FLUTTER_UI_DIR) || { \
		echo ""; \
		echo "FAIL: Dart files in $(FLUTTER_UI_DIR) are not formatted."; \
		echo "Run 'make fmt-gui' to format them, then stage the changes."; \
		exit 1; \
	}
	@echo "Dart formatting clean."

vet:
	@echo "Running go vet..."
	go vet ./...

.PHONY: mutexio
mutexio:
	@echo "Running mutexio analyzer..."
	@go run ./tools/analyzers/mutexio/ ./...
	@echo ""
	@echo "Tip: This same check runs automatically as a pre-commit hook"
	@echo "     (.git/hooks/pre-commit-mutexio) on staged Go packages."
	@echo "     To bypass it for a single commit, use: git commit --no-verify"

.PHONY: predid
predid:
	@echo "Running predid analyzer..."
	@go run ./tools/analyzers/predid/ ./...

.PHONY: analyzers
analyzers: mutexio predid
	@echo "All Go analyzers complete."

# graphs regenerates the connectivity graph (bus topology, RPC handlers,
# HTTP routes, WS event map) from source. Runs automatically on every
# `make build`. Use `make graphs-check` in CI to verify freshness.
.PHONY: graphs
graphs:
	@echo "Generating connectivity graphs..."
	@python3 scripts/gen-connectivity-graph.py
	@echo "Connectivity graphs written to docs/generated/"

# graphs-check verifies the generated connectivity artifacts are fresh.
#
# NOT WIRED INTO CI OR THE PRE-COMMIT CHAIN: on this tree `--check` is red
# until docs/generated/* is regenerated, and the generator still embeds
# absolute line offsets, so any edit above a publish/subscribe site invalidates
# the artifact again (audit F66). Wire this into .github/workflows/ci.yml only
# after both are true:
#   1. `make graphs` has been run and its docs/generated/* changes committed;
#   2. scripts/gen-connectivity-graph.py emits symbol identity instead of raw
#      line numbers (so the check fails on topology drift, not on insertions).
.PHONY: graphs-check
graphs-check:
	@python3 scripts/gen-connectivity-graph.py --check

# audit-scripts runs the Python-based codebase audits:
#   - dart-enum-name-shadow: flags Dart extensions that shadow Enum.name/index
#     (silent footgun that broke SearchScope.name; see Round 6 findings)
#   - utf8-byte-arithmetic: flags hand-rolled ASCII case-conversion that
#     corrupts multi-byte UTF-8 (e.g., c |= 0x20 on é bytes)
.PHONY: audit-scripts
audit-scripts:
	@echo "Running audit scripts..."
	@if [ -f scripts/audit-dart-enum-name-shadow.py ]; then \
		echo "  dart-enum-name-shadow..."; \
		python3 scripts/audit-dart-enum-name-shadow.py || exit 1; \
	fi
	@if [ -f scripts/audit-utf8-byte-arithmetic.py ]; then \
		echo "  utf8-byte-arithmetic..."; \
		python3 scripts/audit-utf8-byte-arithmetic.py || exit 1; \
	fi
	@if [ -f scripts/research-harness-lit.py ]; then \
		echo "  research-harness-lit..."; \
		python3 scripts/research-harness-lit.py --check || exit 1; \
	fi
	@echo "Audit scripts complete."

.PHONY: research-harness research-harness-check
research-harness:
	@python3 scripts/research-harness-lit.py

research-harness-check:
	@python3 scripts/research-harness-lit.py --check

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
	@echo "Generating LLM flat doc..."
	@go run ./cmd/llmdoc -output docs/generated/llms-readme-full.txt
	@if [ -d docs/.venv ]; then \
		. docs/.venv/bin/activate && mkdocs build -d site; \
	else \
		mkdocs build -d site; \
	fi

docs-generate:
	@echo "Generating reference docs from Go source..."
	mage -d magefiles docsGenerate

# docs-check verifies docs/reference/generated/* matches the current source
# (magefiles/docs.go DocsCheck: "Returns an error if any file differs, suitable
# for use in CI").
#
# WIRED: code-quality.yml job "generated-artifacts" runs `make docs-check`, so
# this is a hard gate. It needs mage + gomarkdoc on PATH (the job installs
# both). Run `make docs-generate` and commit the result after changing any
# documented package's exported surface.
.PHONY: docs-check
docs-check:
	@echo "Verifying generated reference docs are fresh..."
	mage -d magefiles docsCheck

# classifier-eval-selftest exercises the eval guards that gate every acceptance
# number: the corpus<->replay disjointness guard (a leaked case refuses to
# score), the empty-ruler and degenerate-embedding refusals, the near-duplicate
# allowlist semantics, and the coverage floor boundary. It is the pin for logic
# that otherwise has none.
#
# It needs NumPy: the guard logic is stdlib, but eval_harness imports numpy at
# module level, so the CI job installs it first rather than assuming the runner
# image ships it.
.PHONY: classifier-eval-selftest
classifier-eval-selftest:
	@echo "Running classifier-eval guard self-test..."
	python3 tools/classifier-eval/m4_gold_acceptance.py --self-test

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

build-gui: gui-deps dev-key
	@mkdir -p $(BIN_DIR)
	@echo "Building meept-gui for $(GUI_PLATFORM)..."
	cd $(FLUTTER_UI_DIR) && flutter build $(GUI_PLATFORM) --release $(FLUTTER_DART_DEFINES)
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
	@echo "  API:     https://localhost:8081/api/v1"
	@echo ""
	@echo "  Hot reload: press 'r'"
	@echo "  Hot restart: press 'R'"
	@echo "  Quit: press 'q'"
	@echo ""
	@if ! curl -sk https://localhost:8081/health > /dev/null 2>&1; then \
		echo "  [!] WARNING: Daemon not detected on port 8081"; \
		echo "  The app will not work without the daemon running."; \
		echo ""; \
		echo "  Fix: Run 'make daemon' in another terminal"; \
		echo "       Or enable HTTP in ~/.meept/meept.json5:"; \
		echo ""; \
		echo "       transport: {"; \
		echo "         http: { enabled: true, addr: \"127.0.0.1:8081\" }"; \
		echo "       }"; \
		echo ""; \
		echo "       Loopback only (127.0.0.1, not \":8081\"). The GUI"; \
		echo "       endpoint must equal transport.http.addr exactly."; \
		echo ""; \
		echo "  Continuing anyway..."; \
		echo ""; \
	else \
		echo "  [OK] Daemon detected on port 8081"; \
		echo ""; \
	fi
	cd $(FLUTTER_UI_DIR) && flutter run -d chrome --web-port=59714 $(FLUTTER_DART_DEFINES)

gui-dev-server:
	@echo "  localcert        Generate trusted SSL cert for localhost (requires mkcert)"
	@echo "  localcert-install Install mkcert and local CA (one-time setup)"
	@echo "Starting Flutter web dev server (web-server target)..."
	@echo "Open http://localhost:59714 in your browser"
	cd $(FLUTTER_UI_DIR) && flutter run -d web-server --web-port=59714 $(FLUTTER_DART_DEFINES)

# webui: Alias for gui-web-run - Flutter web development with hot reload
# Opens Chrome at http://localhost:59714 for fast UI iteration without recompilation.
.PHONY: webui
webui: gui-web-run 

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
	@echo "Removing bundled skills (user-modified skills are kept)..."
	@if [ -d ~/.meept/skills ]; then \
		for d in ~/.meept/skills/*/; do \
			[ -e "$$d" ] || continue; \
			name=$$(basename $$d); \
			if [ -d "config/skills/$$name" ] && diff -r "$$d" "config/skills/$$name" >/dev/null 2>&1; then \
				rm -rf "$$d"; \
				echo "  removed bundled skill $$name"; \
			else \
				echo "  keeping $$name (user-modified or custom)"; \
			fi; \
		done; \
		if [ -z "$$(ls -A ~/.meept/skills 2>/dev/null)" ]; then \
			rmdir ~/.meept/skills; \
		fi; \
	fi
	@echo "Removing cached plugins..."
	rm -rf ~/.meept/plugins/
	@echo "Removing workspaces..."
	rm -rf ~/.meept/workspaces/
	@echo ""
	@echo "Full uninstall complete."
	@echo "Note: Flutter build cache not removed. Run 'cd ui/flutter_ui && flutter clean' if needed."

# =============================================================================
# OpenAPI SDK Generation
# =============================================================================

SDK_DIR := sdk

.PHONY: sdk-generate sdk-generate-go sdk-generate-dart sdk-clean sdk-test check-java-17

# Generate all SDKs from OpenAPI spec
sdk-generate: sdk-generate-go sdk-generate-dart
	@echo ""
	@echo "SDK generation complete."
	@echo "  Go SDK:  $(SDK_DIR)/go/"
	@echo "  Dart SDK: $(SDK_DIR)/dart/"

# Verify Java 17+ is available (required by openapi-generator-cli)
check-java-17:
	@command -v java >/dev/null 2>&1 || { \
		echo "Error: Java 17+ required by openapi-generator-cli but 'java' not found."; \
		echo "Install with 'brew install openjdk@17' (macOS) or your system package manager."; \
		exit 1; }
	@JAVA_VERSION=$$(java -version 2>&1 | awk -F[\".] -v RS='\n' '/version/ { \
		v=$$2; \
		if (v == "1") { print $$3; exit } \
		else { print v; exit } }'); \
	if [ -z "$$JAVA_VERSION" ] || [ "$$JAVA_VERSION" -lt 17 ]; then \
		echo "Error: Java 17+ required, found major version $$JAVA_VERSION."; \
		echo "Upgrade with 'brew install openjdk@17' (macOS) or your system package manager."; \
		exit 1; fi
	@echo "Java version OK (major version $$JAVA_VERSION)"

# Generate Go SDK
sdk-generate-go: check-java-17
	@echo "Generating Go SDK..."
	@mkdir -p $(SDK_DIR)/go
	@openapi-generator-cli generate \
		-i docs/reference/http-api/openapi.yaml \
		-g go \
		-o $(SDK_DIR)/go \
		--skip-validate-spec \
		--additional-properties=packageName=meeptclient,packageVersion=0.2.0,generateInterfaces=true,interfaceMode=deferentially
	@echo "Go SDK generated at $(SDK_DIR)/go/"

# Generate Dart SDK (dart-dio: package:dio based, typed responses)
# Automatically runs build_runner to regenerate built_value serializers.
sdk-generate-dart: check-java-17
	@echo "Generating Dart SDK (dart-dio)..."
	@mkdir -p $(SDK_DIR)/dart
	@openapi-generator-cli generate \
		-i docs/reference/http-api/openapi.yaml \
		-g dart-dio \
		-o $(SDK_DIR)/dart \
		--skip-validate-spec \
		--additional-properties=packageName=meept_client,pubspecVersion=0.3.0,pubName=meept_client,useNullSafety=true,clientName=MeeptClient
	@cd $(SDK_DIR)/dart && dart pub get
	@cd $(SDK_DIR)/dart && dart run build_runner build --delete-conflicting-outputs
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

# Slash Commands:
#  install-commands  Install pre-built slash command templates
#  commands-clean    Remove installed command templates

.PHONY: install-commands commands-clean

install-commands:
	@echo "Installing slash command templates..."
	@mkdir -p $(MEEPT_HOME)/commands
	@cp config/commands/*.md $(MEEPT_HOME)/commands/ 2>/dev/null || true
	@echo "Commands installed to $(MEEPT_HOME)/commands/"
	@echo "Available commands: research, qa-docker, playwright-test"

commands-clean:
	@echo "Removing installed command templates..."
	@rm -rf $(MEEPT_HOME)/commands
	@echo "Commands removed. To reinstall: make install-commands"

# =============================================================================
# Local SSL Certificates (mkcert)
# =============================================================================

.PHONY: localcert localcert-check localcert-install

localcert-check:
	@echo "Checking for mkcert..."
	@command -v mkcert >/dev/null 2>&1 || { \
		echo "mkcert not found. Install with: brew install mkcert nss"; \
		exit 1; \
	}
	@echo "mkcert found: $$(mkcert --version)"

localcert-install:
	@echo "Installing mkcert local CA..."
	@command -v mkcert >/dev/null 2>&1 || { \
		echo "Installing mkcert..."; \
		brew install mkcert nss; \
	}
	@mkcert -install
	@echo "Local CA installed. You can now generate trusted certs."

# localcert: Generate locally-trusted SSL certificate for localhost
# This eliminates browser certificate warnings when running the Flutter web UI.
# Requires mkcert: brew install mkcert nss
localcert: localcert-check
	@echo "Generating trusted SSL certificate for localhost..."
	@mkdir -p $(MEEPT_HOME)/tls
	@mkcert -cert-file $(MEEPT_HOME)/tls/cert.pem \
		-key-file $(MEEPT_HOME)/tls/key.pem \
		localhost 127.0.0.1 ::1
	@echo ""
	@echo "Certificate generated:"
	@echo "  Cert: $(MEEPT_HOME)/tls/cert.pem"
	@echo "  Key:  $(MEEPT_HOME)/tls/key.pem"
	@echo ""
	@echo "Restart the daemon to use the new certificate:"
	@echo "  meept-daemon -f"
	@echo ""
	@echo "Your browser will now trust the certificate without warnings."

.PHONY: selflock
selflock:
	@echo "Running selflock analyzer (detects self-deadlock patterns)..."
	@go run ./tools/analyzers/selflock/ ./... || (echo "" && \
		echo "SELF-DEADLOCK PATTERN DETECTED" && \
		echo "This usually means:" && \
		echo "  1. obj.Lock() followed by obj.MethodWithInternalLock()" && \
		echo "  2. obj.Lock() followed by store.Update(obj) with callback" && \
		echo "" && \
		echo "See: .claude/skills/go-mutex-self-deadlock-store-callback/SKILL.md" && \
		echo "     https://go.dev/doc/effective_go#concurrency" && \
		exit 1)

analyzers: selflock

# =============================================================================
# Classifier Benchmark
# =============================================================================

# Model-vs-model classification benchmark on the labeled corpus.
#   make classifier-test MODELS="--model-a /path/A --model-b /path/B" BASE_URL=http://127.0.0.1:18081
.PHONY: classifier-test
classifier-test:
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/meept-classifier-test ./cmd/meept-classifier-test
	@./$(BIN_DIR)/meept-classifier-test $(MODELS) --base-url $(BASE_URL) --name classifier-eval-$(shell date +%Y%m%d-%H%M)

# =============================================================================
# Comparison Prep
# =============================================================================

compare-prep:
	@CLEAN="$(CLEAN)" bash scripts/compare-prep.sh

# =============================================================================
# E2E Regression
# =============================================================================

# Naive-user chat regression: boots a scratch daemon (temp state dir, temp
# socket, probed port — never the live daemon), replays the 4-turn transcript,
# asserts reply honesty/continuity/artifact contracts (A1-A6).
#   make e2e-chat              # run once
#   make e2e-chat E2E_ARGS="--keep"   # keep the scratch workdir for forensics
#   MEEPT_E2E_TURN_TIMEOUT=540 make e2e-chat  # raise per-turn timeout
.PHONY: e2e-chat
e2e-chat:
	@bash scripts/e2e-naive-user-chat.sh $(E2E_ARGS)
