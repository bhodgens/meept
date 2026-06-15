# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Build daemon
go build -o bin/meept-daemon ./cmd/meept-daemon

# Build CLI
go build -o bin/meept ./cmd/meept

# Build everything (daemon + CLI + gendoc + GUI)
make build

# Build only the Flutter GUI
make build-gui

# Run all tests
go test ./... -v

# Run specific package tests
go test ./internal/agent/... -v
# Run tests with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run tests with race detection
go test -race ./...

# TUI testing with agent-tui (https://lib.rs/crates/agent-tui)
agent-tui ./bin/meept chat

# Start daemon (foreground)
./bin/meept-daemon -f

# Or use make targets
make go-daemon         # Build and start daemon
make go-daemon-debug   # Build and start with debug logging

# CLI commands
./bin/meept status
./bin/meept chat "What's the weather like?"
./bin/meept chat  # Interactive TUI mode

# macOS MenuBar app (macOS only)
make menubar                   # Build menubar app (release)
make menubar-install           # Build and install to /Applications
make menubar-clean             # Clean menubar build artifacts
# cd menubar
# swift build                    # Build menubar app
# swift run         # Run menubar app
# cp -r  # Install

# Config management (replaces the old `meept models` command)
./bin/meept config                    # Interactive config editor TUI
./bin/meept config <section>          # Open TUI at specific section
./bin/meept config list               # List config file paths and status
./bin/meept config get <keypath>      # Get a config value
./bin/meept config set <keypath> <v>  # Set a config value
# Sections: daemon, transport, llm, models, agents, memory, security,
#           mcp, client/tui, scheduler, plus ~20 advanced sections
# Use `meept config models` for model management (replaces `meept models`)

# Self-improvement commands
./bin/meept selfimprove detect
./bin/meept selfimprove full-cycle
./bin/meept selfimprove status

# Q Agent commands (meta-agent for agent optimization)
./bin/meept q status                   # Show Q Agent status and configuration
./bin/meept q analyze                  # Analyze sessions for improvement opportunities
./bin/meept q analyze --force          # Force analysis even if disabled
./bin/meept q analyze --json           # Output results as JSON

# Project context commands
./bin/meept projects                    # List registered projects
./bin/meept projects add <path|url>     # Register project
./bin/meept projects remove <name>      # Unregister
./bin/meept projects sync <name>        # Pull latest
./bin/meept projects status <name>      # Show git status
./bin/meept chat --project <name>       # Chat bound to specific project
./bin/meept chat --nofence              # Disable path fencing for this session

# Plan commands
./bin/meept plans list                    # List all plans
./bin/meept plans show <id>               # Show plan details
./bin/meept plans approve <id>            # Approve a pending plan
./bin/meept plans reject <id>             # Reject a pending plan
./bin/meept plans confirm <id>            # Confirm a completed plan
```

## Architecture Overview

Meept is a **Go daemon** with skill-based task orchestration, LLM integration, memory management, and external integrations.

### Request Flow

```
User Input (CLI/Telegram/Web/MenuBar)
    → CommServer (Unix socket JSON-RPC) OR HTTP REST API
    → MessageBus (pub/sub)
    → AgentLoop
        → Dispatcher.ClassifyAndRoute()
            → IntentPair → PairOrchestrator (bus-channel pairing)
        → Planner.Decompose() → TaskSteps
        → CollaborativePlanner (review/approval workflow)
        → WorkspaceManager (git-backed task tracking)
        → SecurityEngine.Check()
        → Tool execution
        → Memory injection
    → Response
```

### Key Components

| Layer | Go Packages |
|-------|-------------|
| **Server Core** | `cmd/meept-daemon`, `internal/daemon`, `internal/rpc`, `internal/bus` |
| **Agent** | `internal/agent` (loop, orchestrator, planner, collaborative, workspace) |
| **Project** | `internal/project` (manager, store, worktree) |
| **Security** | `internal/security` (engine, sanitizer, tirith, tls, fence) |
| **LLM** | `internal/llm` (client, resolver, budget, providers, token cache, context compactor, context firewall) |
| **Skills** | `internal/skills` (discovery, registry, parser, models) |
| **Memory** | `internal/memory` (manager, episodic, task, consolidation, ftstore) |
| **Tools** | `internal/tools` (registry, builtin/*, mcp) |
| **STT** | `internal/stt` (transcriber, recorder, whisper, parakeet, native) |
| **Code Intel** | `internal/code/ast` (tree-sitter parser + symbol extraction), `internal/code/lsp` (LSP client/manager), `internal/code/tools` (ast_* and lsp_* agent tools) |
| **Self-Improve** | `internal/selfimprove` (controller, detector, analyzer, generator, validator, applier) |
| **Scheduler** | `internal/scheduler` (scheduler, jobs) |
| **External** | `internal/comm/telegram`, `internal/comm/web`, `internal/calendar` |
| **CLI** | `cmd/meept` (chat, status, config, jobs, memory, selfimprove) |
| **MenuBar** | `menubar/` (SwiftUI app), `internal/comm/http` (REST API) |
| **Metrics** | `internal/metrics` (store, collector) |
| **Plans** | `internal/plan` (plan, store, manager, parser, writer, handler) |

### Skill/Model Resolution

Skills declare `requires: [code, reasoning]` in YAML frontmatter; models declare `capabilities: [code, tool_use]` in `config/models.json5`. The resolver (`internal/llm/resolver.go`) finds the cheapest model satisfying requirements.

### Model Reassignment

Users can override default agent model assignments via natural language instructions:

```bash
# Use specific model for a task type
meept chat "Research best practices, then use glm-4.7 for synthesis"

# Use provider-specific models
meept chat "Use local models for research, GLM for coding"

# Interactive clarification (if ambiguous)
meept chat "Use GLM models for this"
# Dispatcher will ask: "Which GLM model? glm-4.7 or glm-4.5-air?"
```

The dispatcher parses model reassignment instructions, asks clarifying questions if ambiguous, and attaches model overrides to matching task steps. See [Multi-Agent System - Model Reassignment](docs/concepts/multi-agent.md#model-reassignment) for details.

### Memory Storage Architecture

`EpisodicMemory` and `TaskMemory` share a common `SQLiteFTSStore` base type (`internal/memory/ftstore.go`) that eliminates code duplication for SQLite pool management, FTS5 schema initialization, CRUD operations, timestamp queries, and result scanning. Each memory type creates an `FTSConfig` specifying its table name, schema SQL, FTS triggers, and category field name (`"category"` for episodic, `"domain"` for task). Row scanning is unified via `SQLiteFTSStore.ScanResults()` which accepts a `ScanRowConfig` parameter controlling the memory type label and source format string. `TaskMemory` retains its domain-specific `FindDuplicates()` method by delegating to `SQLiteFTSStore.FindDuplicateGroups()`.

### Security Layers

1. **InputSanitizer**: Prompt injection pattern detection
2. **SecurityEngine**: SQLite-backed permission checks, tool gating, audit log
3. **Tirith**: Pre-execution shell command scanning
4. **TLS**: Server/client TLS configuration helpers

## Configuration

All configuration uses **JSON5** format (JSON with comments and trailing commas). Legacy TOML config is still readable for backward compatibility.

- **Main config**: `~/.meept/meept.json5` (see `config/meept.json5` for template)
- **Models**: `config/models.json5` (JSON5 format with capability tags)
- **Presets**: `config/presets.json5` (model presets), `~/.meept/presets.json5` (user overrides)
- **MCP servers**: `~/.meept/mcp_servers.json5`
- **Q Agent**: `~/.meept/q_agent.json5`
- **Client**: `~/.meept/client.json5` (TUI keybindings/rendering)
- **Menubar**: `~/.meept/menubar.json5` (menubar app settings)
- **Metrics DB**: `~/.meept/metrics.db` (SQLite time-series storage)
- **launchd Plist**: `~/Library/LaunchAgents/com.caimlas.meept-daemon.plist` (macOS)

Templates are in `config/` and copied on `make install` if not present.

### Development API Key

The default development API key is defined in exactly one place:
`pkg/constants/api_key.go` (`constants.DefaultDevAPIKey`).

- **Go code**: Always reference `constants.DefaultDevAPIKey`. Never hardcode the literal.
- **Flutter**: Use `--dart-define=MEEPT_DEV_API_KEY=<key>` at build time.
  The `defaultApiKey` constant uses `String.fromEnvironment()` so it is empty in release builds.
  Debug builds (flutter run): pass the dart-define flag.
- **Swift (MenuBar)**: Gated behind `#if DEBUG`. The `MenubarConfigService.apiToken`
  property returns `nil` in release builds when no config token is set, triggering
  a clear authentication error rather than silently using a known key.
- **Config files**: Set `"api_token": null` in `menubar.json5`; the app falls back to
  the `#if DEBUG` constant in debug builds. For production, generate a custom key
  via `meept token generate --save`.

### Transport Configuration

The daemon supports two transports (can be enabled independently):

```json5
{
  transport: {
    rpc: {
      enabled: true,                // Unix socket JSON-RPC
      socket_path: "~/.meept/meept.sock",
    },
    http: {
      enabled: false,               // REST API for menubar (enable if using menubar app)
      addr": ":8081",
      // Production security: TLS + auth enabled by default
      use_tls: true,                // Enable HTTPS (self-signed cert for localhost)
      auto_tls_cert: true,          // Auto-generate self-signed cert on first run
      require_auth: true,           // Require API key authentication
      api_keys: [],                 // List of valid API keys (use `meept token generate`)

      // Modular endpoint configuration (unified HTTP server)
      rest: true,                   // REST API at /api/v1/* (default: true)
      websocket: false,             // WebSocket at /ws for Flutter UI (default: false)
      ws_path: "/ws",               // WebSocket endpoint path
      mcp: false,                   // MCP over HTTP+SSE for AI agents (default: false)
      mcp_path: "/mcp",             // MCP endpoint path
    },
  },
}
```

**Unified HTTP Server Architecture:**

When HTTP transport is enabled, a single HTTP server at the configured port (default: 8081) serves all HTTP endpoints:

| Endpoint | Method | Purpose | Config Flag |
|----------|--------|---------|-------------|
| `/api/v1/*` | Various | REST API (40+ endpoints) | `rest` |
| `/ws` | GET | WebSocket for real-time updates | `websocket` |
| `/mcp` | POST | MCP JSON-RPC requests | `mcp` |
| `/mcp/sse` | GET | MCP Server-Sent Events stream | `mcp` |

Clients connect via `--transport` flag:
```bash
meept --transport=rpc chat          // Default; uses Unix socket
meept --transport=http --http-url=http://localhost:8081 chat
```

Menubar app uses HTTP exclusively. Its config (`menubar.json5`) controls the daemon URL:
```json5
{
  daemon: {
    transport: "http",
    http_url: "http://localhost:8081",
  },
}
```

### HTTP API Architecture

The HTTP API uses a **service layer pattern** to share business logic between RPC and HTTP transports:

```
┌─────────────┐    ┌──────────────┐    ┌─────────────────┐
│ HTTP Client │───▶│ HTTP Handlers│───▶│ Service Layer   │
└─────────────┘    └──────────────┘    └────────┬────────┘
                                                │
┌─────────────┐    ┌──────────────┐    ┌────────▼────────┐
│ RPC Client  │───▶│ RPC Handlers │───▶│ (same funcs)    │
└─────────────┘    └──────────────┘    └─────────────────┘
```

**Service Layer** (`internal/services/`):
- `ServiceRegistry` - holds all service instances
- `ChatService` - conversation management via message bus
- `MemoryService` - query, recent, export operations
- `TaskService` - task CRUD operations
- `QueueService` - job queue management (enqueue, claim, complete, fail, retry)
- `SessionService` - session lifecycle
- `WorkerService` - worker management
- `SkillsService` - skill discovery and execution
- `SelfImproveService` - self-improvement workflow
- `CacheService` - token cache management
- `SecurityService` - security checks and overrides
- `SchedulerService` - cron job management
- `BusService` - message bus operations

**HTTP Handlers** (`internal/comm/http/api_handlers.go`):
- RESTful endpoints under `/api/v1/*`
- CORS support for web clients
- Optional API key authentication via `Authorization: Bearer <key>` header
- Error responses in JSON format: `{"error": "message"}`

**Authentication**: When `require_auth: true`, all endpoints except `/health` require a valid API key. Keys are validated using constant-time comparison to prevent timing attacks.

**Full endpoint documentation**: `docs/reference/http-api.md`
**OpenAPI specification**: `docs/reference/http-api/openapi.yaml`

### Programmatic component options

- `ShellExecuteTool.SetKnownSafeCommands([]string)` — base command names treated
  as `RiskMedium` instead of the default `RiskHigh` for unknown commands. Use for
  project-local CLIs (`mytool`, `mycli`) that the operator has vetted.
- `LLMClassifierConfig.Timeout time.Duration` — per-classification HTTP timeout;
  defaults to 10 s when unset. Daemon wiring uses 15 s explicitly.
- `ContextFirewall.Stats()` returns `FirewallStats` with counters for
  `SummarizationFailures`, `DroppedMessages`, `DropEvents`, `CompactionEvents`,
  `CompactionTokensSaved`, and `CompactionFallbacks` so operators can monitor
  context-pressure indicators and compaction effectiveness.

### Text-to-Speech Configuration

TTS is client-side only (TUI and Flutter). Uses Piper TTS or platform-native synthesis:
- piper: `piper` binary with ONNX voice models at `~/.meept/tts/voices/`
- platform: macOS `say` command / Windows SAPI (no extra dependencies)

Default voice: `danny-medium` (English US, medium quality)

Config: `meept config tts`


### Speech-to-Text Configuration

STT is client-side only (TUI and Flutter). Requires external tools depending on engine:
- whisper: `whisper-cli` + `ffmpeg`, model at `~/.meept/models/ggml-base.en.bin`
- parakeet: parakeet CLI + `ffmpeg`, model at `~/.meept/models/`
- native: macOS Speech framework or Windows SAPI (no external deps)

Config: `meept config stt`

## Skills Discovery

Three-tier with priority shadowing:
```
.meept/skills/              # Project-local (highest)
~/.meept/skills/            # User-global
~/.config/meept/skills/     # System-wide
```

## Multi-Agent Architecture

Meept uses a multi-agent architecture where specialist agents handle different types of tasks:

| Agent ID | Role | Purpose |
|----------|------|---------|
| `dispatcher` | Dispatcher | Intake, classify, route to specialists |
| `chat` | Executor | General conversation |
| `coder` | Executor | File ops, shell, coding tasks |
| `debugger` | Executor | Troubleshooting, bug fixing |
| `planner` | Executor | Task decomposition, planning |
| `analyst` | Executor | Research, data analysis |
| `committer` | Executor | Git operations |
| `scheduler` | Executor | Job scheduling |

### Coworker Awareness

Agents discover and delegate to each other using platform tools:
- `platform_agents`: List available agents and their capabilities
- `platform_status`: Get platform health status
- `platform_tools`: List registered tools
- `delegate_task`: Route a task to a specific agent (synchronous, blocking)
- `request_handoff`: Dynamically inject a new step into the running task DAG and route to another agent (async, non-blocking, with dependency rewiring)

### Job Queue Routing

Jobs can be targeted to specific agents via `agent_id`:
- If `agent_id` is set, only that agent can claim the job
- If `agent_id` is empty, any agent with matching capabilities can claim
- Jobs are prioritized by: targeted agent match > priority > creation time

### Channel-Based Pairing (Option C)

Two agents share a named bus topic and take turns for free-form collaborative tasks (research debates, brainstorming, exploratory debugging). The `PairOrchestrator` manages the actor-reviewer loop via the message bus.

Bus topics: `pair.start`, `pair.{sessionID}.turn`, `pair.result`, `pair.error`

Triggered by `IntentPair` intent type. Default actor/reviewer mapping: analyst/planner, coder/planner, debugger/coder, planner/analyst.

## Code Conventions

- Go 1.22+
- Standard library preferred where possible
- `log/slog` for structured logging
- Context propagation throughout
- Interfaces for testability
- Table-driven tests

## UI Conventions

- **All UI element text must be explicitly lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- This applies to: button labels, menu items, tooltips, status messages, dialog titles, hints
- for TUI, use bubblezone library to help with positioning.
- Default to making elements clickable for switching context, if possible.

## Coding Practices

- Always implement complete wired features, do not leave stub code or partial implementations
- Always check your work
- **Typed-nil interface guard**: When passing a concrete pointer to a function that accepts an interface, always nil-check the concrete pointer first. A nil `*ConcreteType` assigned to an interface variable produces a non-nil interface that passes `!= nil` but panics on method calls. Guard at the call site AND inside `With*` option functions:
  ```go
  // WRONG: typed-nil panic
  var cache *TokenCacheCoordinator  // nil
  WithTokenCache(cache)             // non-nil interface wrapping nil pointer

  // RIGHT: guard at call site
  if tokenCache != nil {
      opts = append(opts, WithTokenCache(tokenCache))
  }

  // RIGHT: defense in depth inside option function
  func WithTokenCache(cache ResponseCache) ClientOption {
      return func(c *Client) {
          if cache != nil {
              c.tokenCache = cache
          }
      }
  }
  ```

- **Setter methods**: Every `Set*` method on a tool/service struct that accepts an
  interface or pointer type MUST include a nil guard as the first line. The test
  suite (`internal/tools/builtin/setters_test.go`) verifies this project-wide.
  ```go
  // RIGHT: nil guard prevents typed-nil panic
  func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
      if fc != nil {
          t.fenceChecker = fc
      }
  }

  // WRONG: direct assignment allows typed-nil panic
  func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
      t.fenceChecker = fc  // fc could be a typed-nil interface
  }
  ```

- **Mutex scope**: Never hold a mutex across I/O operations (network calls,
  disk reads/writes, LLM calls, channel sends). Use the "collect under lock,
  release, then operate" pattern:
  ```go
  // RIGHT: snapshot under lock, operate without lock
  mu.Lock()
  cfg := m.config  // immutable after construction
  mu.Unlock()
  result, err := doNetworkCall(ctx, cfg)

  // WRONG: lock held across network call
  mu.Lock()
  defer mu.Unlock()
  result, err := doNetworkCall(ctx, m.config)  // blocks all other callers during I/O
  ```

## Project Structure

```
cmd/
  meept/           # CLI application
  meept-daemon/    # Daemon application
  gendoc/          # Documentation generator tool
internal/
  agent/           # Agent loop, planner, workspace, collaborative
  bus/             # Message bus (pub/sub)
  calendar/        # Google Calendar integration
  comm/
    telegram/      # Telegram bot
    web/           # HTTP API server
    http/          # REST API for menubar app (NEW)
  config/          # Configuration loading
  daemon/          # Daemon lifecycle
  llm/             # LLM client and resolution
  memory/          # Memory management (SQLite+FTS5)
  metrics/         # Metrics storage and collection (NEW)
  project/         # Project context: registry, worktrees, fencing (NEW)
  registry/        # Service registry
  rpc/             # JSON-RPC server
  scheduler/       # Job scheduling
  security/        # Security engine, sanitizer, tirith
  selfimprove/     # Self-improvement system
  skills/          # Skill discovery and parsing
  tts/             # Text-to-speech (synthesizer interface, piper, platform)
  stt/             # Speech-to-text (transcriber interface, whisper, parakeet, native)
  tools/           # Tool registry and builtins
  runtime/         # Containerized execution backends (local, Docker)
  pty/             # Pseudo-terminal sessions for interactive streaming
config/            # Configuration templates
  runtime.json5    # Runtime backend config template
  pty.json5        # PTY streaming config template
  presets.json5    # Model presets (NEW)
docs/              # MkDocs documentation site
menubar/           # macOS MenuBar app (NEW)
  MeeptMenuBar/    # Main SwiftUI app
  Views/           # Menu, Settings, Analytics views
  Services/        # APIClient, DaemonController, ConfigService
  Models/          # Data models and presets
tests/             # Integration tests
archive/           # Legacy Python code (reference only)
```

## macOS MenuBar App

The menubar app provides native macOS integration for monitoring and controlling Meept:

### Features
- **Status menu**: Shows daemon running/stopped state with uptime
- **Daemon control**: Start, stop, restart via launchd
- **Settings**: Edit client.json5, models.json5, manage agents
- **Analytics**: Live metrics dashboard with historical charts

### Architecture
```
MenuBar App (SwiftUI)
    ↓ HTTP REST (localhost:8081)
internal/comm/http/
    ↓
Daemon components
```

### HTTP API Endpoints

**Config:**
- `GET/POST /api/v1/config/client` - Client configuration
- `GET/POST /api/v1/config/models` - Models configuration
- `GET/POST/DELETE /api/v1/config/agents/:id` - Agent management

**Daemon:**
- `GET /api/v1/daemon/status` - Running state, PID, uptime
- `POST /api/v1/daemon/restart` - Graceful restart

**Metrics:**
- `GET /api/v1/metrics/live` - Current metrics snapshot
- `GET /api/v1/metrics/historical?from=&to=&resolution=` - Historical data

### Model Presets

Built-in presets for common tasks:
- `development` (temp: 0.3) - Balanced for coding
- `debugging` (temp: 0.2) - Methodical troubleshooting
- `planning` (temp: 0.4) - Structured thinking
- `creative` (temp: 0.9) - High creativity
- `research` (temp: 0.5) - Analytical tasks
- `fast` - Quick responses
- `detailed` - Comprehensive answers

## Runtime (Containerized Execution)

The runtime package (`internal/runtime`) provides isolated command execution backends:

```bash
# Test runtime backends
go test ./internal/runtime/... -v

# Build with runtime support
go build -o bin/meept-daemon ./cmd/meept-daemon
```

**Backends:**
- `LocalBackend`: Direct shell execution via `exec.Command` (default, always available)
- `DockerBackend`: Containerized execution with full isolation (requires Docker daemon)

**Configuration:** `config/runtime.json5`, `~/.meept/meept.json5`

**Key types:**
- `ExecutionBackend` interface: `Execute(ctx, Command) (*CommandResult, error)`
- `Manager`: Backend lifecycle and lookup
- `TestHarness`: Optional validation pipeline for verifying changes

See `docs/concepts/runtime.md` for full documentation.

## PTY (Pseudo-Terminal Streaming)

The PTY package (`internal/pty`) provides interactive terminal sessions with real-time output streaming:

```bash
# Test PTY sessions
go test ./internal/pty/... -v

# Requires: github.com/creack/pty, github.com/gorilla/websocket
```

**Features:**
- Interactive debuggers (gdb, delve, pdb)
- REPLs (ipython, node, go run)
- Long-running servers during testing
- PTY fallback to subprocess pipes when unavailable

**Configuration:** `config/pty.json5`, `~/.meept/meept.json5`

**Key types:**
- `Session` interface: `Write`, `Read`, `Output()`, `Resize`, `Close`
- `Manager`: Session lifecycle with concurrency control
- `SessionInfo`: API response metadata

**HTTP Endpoints** (when wired):
- `POST /api/v1/pty/sessions` - Create session
- `GET /api/v1/pty/sessions/{id}` - WebSocket stream
- `POST /api/v1/pty/sessions/{id}` - Write input
- `DELETE /api/v1/pty/sessions/{id}` - Close session

See `docs/concepts/pty-streaming.md` for full documentation.

## Documentation

Meept uses MkDocs Material for its documentation site. The site is built from markdown files under `docs/`.

### Documentation Commands

```bash
make docs-serve       # Start local dev server (auto-reload)
make docs-build       # Build static site to site/
make docs-generate    # Generate reference docs from Go source
```

### Documentation Structure

| Directory | Purpose |
|-----------|---------|
| `docs/getting-started/` | Installation, quick start, troubleshooting |
| `docs/concepts/` | Architecture, agents, memory, tools, skills |
| `docs/configuration/` | Config reference with examples |
| `docs/workflows/` | Feature specifications (15 features) |
| `docs/reference/` | CLI, RPC API, tools, logging, metrics |
| `docs/reference/generated/` | Auto-generated from Go structs via `cmd/gendoc` |
| `docs/plans/archive/` | Historical implementation plans |

## Documentation Maintenance

**IMPORTANT**: When making changes to the implementation, always update related documentation to stay in sync:

1. **concepts/architecture.md**: Update architecture diagrams when adding/modifying components, agents, tools, or data flows
2. **CLAUDE.md**: Update this file when changing architecture patterns, adding new agents, or modifying key behaviors
3. **Code comments**: Keep inline documentation accurate when modifying function signatures or behavior
4. **features.md**: Always update features as they are implemented or changed to match the code.

### Documentation Rules

- All features must have a feature spec in `docs/workflows/` using the standard template
- Struct changes in `internal/config/schema.go` require running `make docs-generate`
- New CLI commands require updating `docs/reference/cli.md`
- New agents require updating `docs/concepts/multi-agent.md`
- All doc pages must be linked from `mkdocs.yml` nav
- README.md feature list must stay in sync with `docs/workflows/`

Documentation should always reflect the current implementation state. If you add a new agent, tool, or architectural component, document it immediately.
