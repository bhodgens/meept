# Agent Reference: Build and Development Commands

Moved from the root `AGENTS.md` to keep the root under the size cap. The root
keeps the build/test/channel pointers; this file is the exact command list.
Update both in the same commit when a command changes.

## Build & Development Commands

```bash
# Build
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
go build -o bin/meept-voice ./cmd/meept-voice  # voice child (phone sidecar)
make build              # Everything (daemon + CLI + voice child + gendoc + GUI + lite + graphs)
make build-gui          # Flutter GUI only
make build-gui-dist     # Flutter GUI with NO embedded dev key (distribution; first-run pairing)
make menubar-install    # macOS MenuBar app
make install            # Full install: build + bin + config + GUI + menubar (needs Flutter)
make install-cli        # Headless install: binaries + config only (no Flutter needed)
make install-package    # Stage bin + web payload under DESTDIR= for packaging
# Install destinations (command-line overrides):
#   PREFIX=/opt/meept -> BINDIR=$(PREFIX)/bin, WEBDIR=$(PREFIX)/share/meept/web
#   BINDIR (default GOPATH/bin or PREFIX/bin), APPSDIR (default ~/Applications,
#   NEVER PREFIX-derived), DESTDIR (staging root; MEEPT_HOME is never staged)
make deps-llama-check   # Enforce the llama.cpp build floor (>= b9660, LFM2.5 tool-call
                        # parser); wired into `make deps` and `make install`. `make
                        # deps-llama` installs it into $MEEPT_HOME/deps/llama.cpp
make deps-models        # Download model weights (LFM catalog); prompts for storage
                        # path ($MEEPT_MODELS_DIR, default $MEEPT_HOME/models) and
                        # set (basic = GGUFs any platform; full = + 8B MLX on
                        # Apple Silicon). Optional — never blocks `make install`.
make deps-mlx           # mlx-lm into $MEEPT_DEPS/mlx-venv (Apple Silicon only)
# models.json5 paths use ${MEEPT_MODELS_DIR:-~/.meept/models}; an existing
# drive-mounted layout keeps working by exporting MEEPT_MODELS_DIR (e.g.
# /Volumes/LLMs) in the shell or $MEEPT_HOME/env. `meept doctor` reports
# present/missing per lifecycle endpoint (report-only).

# Test
#
# macOS: full-sweep runs MUST bound package parallelism (-p 2). Unbounded
# `go test ./...` / `./internal/...` opens enough concurrent localhost
# sockets to exhaust the ephemeral port range (49152-65535) mid-run, and
# unrelated packages then fail with
#   dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address
# `make test` and friends already pass -p 2 (override: make test
# TEST_PACKAGE_PARALLELISM=N).
go test -p 2 ./... -v
go test -p 2 -race ./...
go test -p 2 ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
make test            # canonical full suite (short mode, -p 2)
make test-record     # run the suite and append per-test results to
                     # $MEEPT_HOME/test-history/results.jsonl (test-efficacy history)
make test-efficacy   # per-test flake rate / failure count / avg time table
                     # (scripts/test-efficacy.py over the recorded history)

# Run
./bin/meept-daemon -f           # Daemon foreground
./bin/meept chat "message"      # CLI chat
./bin/meept chat                # Interactive TUI
agent-tui ./bin/meept chat      # TUI testing

# Config
./bin/meept config              # Interactive editor
./bin/meept config get <key>    # Get value
./bin/meept config set <key> <v> # Set value

# Soul (user-authored persona, ~/.meept/SOUL.md)
./bin/meept soul show           # Current content + sha256 + size
./bin/meept soul path           # Resolved path (honors MEEPT_HOME)
make config-bootstrap           # Copy missing config templates into $MEEPT_HOME
make dev-key                    # Provision $MEEPT_HOME/dev_key (0600), shared with the GUI
make build-gui-dist             # Flutter GUI with NO embedded dev key (distribution;
                                # first-run pairing via the daemon's loopback handshake)
make gui-connect-setup          # Make an installed home GUI-ready (transport.http + dev key)
make gui-connect-check          # Static self-check of the installed GUI connect path

# Agents (AI Employees) — not the dispatcher roster in config/agents/
./bin/meept agents list                 # List employees
./bin/meept agents show <id>            # Full definition
./bin/meept agents create <def.json5>   # Register new employee
./bin/meept agents pause <id> / resume <id>
./bin/meept agents goals [--employee=<id>]
./bin/meept agents goal-create <employee-id> --title T --mandate M [--gate-command CMD] [--source user|trigger]
./bin/meept agents set-gate <goal-id> --command="go test -p 2 ./..."
./bin/meept agents steer <id> <content...>  # deliver steering to a running episode
./bin/meept agents audit <id> [--since=6h]

# Plans
./bin/meept plans list/show/approve/reject/confirm <id>

# Projects
./bin/meept projects list/add/remove/sync/status <name>

# Connectivity graphs
make graphs               # Regenerate bus/RPC/HTTP/WS topology (writes docs/generated/*)
# Both freshness checks below run in CI (code-quality.yml, job
# "generated-artifacts"). Run `make graphs` after changing bus/RPC/HTTP/WS
# surfaces; the generator still embeds absolute line offsets (audit F66), so a
# red graphs-check after an unrelated edit means "regenerate", not "topology
# changed".
make graphs-check         # Verify generated files are fresh (CI gate)
make research-harness      # verify harness catalog evidence + regenerate techniques md
make research-harness-check # CI: fail if evidence missing or techniques md stale

# Reference docs (magefiles/docs.go)
make docs-generate        # Regenerate docs/reference/generated/* (needs mage + gomarkdoc)
make docs-check           # Verify those pages are fresh (CI gate; needs mage + gomarkdoc)

# Classifier-eval guards (tools/classifier-eval)
make classifier-eval-selftest # CI: corpus/replay guard + coverage-floor self-test
make e2e-sweep-selftest       # CI: offline sweep grading/completion/CLI regressions
make e2e-fast                 # CI: full hermetic e2e suite (ci.yml gate member;
                              #     code-quality.yml advisory)

# Git hooks: 20 pre-commit checks + pre-push e2e/bench gates. Full gate
# semantics, package classification, and prepared-commit tree details:
# .githooks/README.md. `make test` bounds package parallelism at -p 2; keep
# that bound on any new full-sweep command (see the Test note above).
make hooks                # core.hooksPath -> .githooks

# Static analyzers
make analyzers            # mutexio + predid
make lint-ci              # golangci-lint + analyzers + audit scripts + fmt-check
                          # + fmt-check-gui (fmt-check runs scripts/fmt-check.sh,
                          # the gofmt gate scoped to the .golangci.yml `formatters`
                          # block; fmt-check-gui hard-requires the Dart SDK / Flutter)
```

See `cmd/meept/`, `cmd/meept-daemon/`, and `Makefile` for full command reference.

