# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Build & Development Commands

```bash
# Build
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
make build              # Everything
make build-gui          # Flutter GUI only
make menubar-install    # macOS MenuBar app

# Test
go test ./... -v
go test -race ./...
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out

# Run
./bin/meept-daemon -f           # Daemon foreground
./bin/meept chat "message"      # CLI chat
./bin/meept chat                # Interactive TUI
agent-tui ./bin/meept chat      # TUI testing

# Config
./bin/meept config              # Interactive editor
./bin/meept config get <key>    # Get value
./bin/meept config set <key> <v> # Set value

# Agents (AI Employees)
./bin/meept agents list                 # List employees
./bin/meept agents show <id>            # Full definition
./bin/meept agents create <def.json5>   # Register new employee
./bin/meept agents pause <id> / resume <id>
./bin/meept agents goals [--employee=<id>]
./bin/meept agents audit <id> [--since=6h]

# Plans
./bin/meept plans list/show/approve/reject/confirm <id>

# Projects
./bin/meept projects list/add/remove/sync/status <name>
```

See `cmd/meept/`, `cmd/meept-daemon/`, and `Makefile` for full command reference.

## Architecture Overview

Meept is a **Go daemon** with skill-based task orchestration, LLM integration, memory management, and external integrations.

### Request Flow

```
User Input → CommServer (RPC/HTTP) → MessageBus → AgentLoop → Dispatcher → Planner → Tools → Response
```

### Key Components

| Layer | Packages |
|-------|----------|
| **Server** | `cmd/meept-daemon`, `internal/daemon`, `internal/rpc`, `internal/bus` |
| **Agent** | `internal/agent` (loop, orchestrator, planner, collaborative, workspace) |
| **LLM** | `internal/llm` (client, resolver, budget, providers, token cache, context firewall) |
| **Memory** | `internal/memory` (manager, episodic, task, ftstore) |
| **Tools** | `internal/tools` (registry, builtin/*, mcp) |
| **Security** | `internal/security` (engine, sanitizer, tirith, tls, fence) |
| **Employee** | `internal/employee` (constitution, goal, goal_loop, enforcement, authority, manager) |

See `docs/concepts/architecture.md` for full documentation.

## Coding Practices

### Optimization Posture

**Prefer early optimization over defensible-but-suboptimal defaults.** Pick the optimized approach when cost/benefit is reasonable.

- Share genuinely-sharable state (configs, registries, builders) across instances
- Prefer structural isolation over convention-based isolation
- Don't over-analyze micro-optimations (<100KB memory, <10ms latency)

### Wiring/Integration Requirement

**Implementations MUST include wiring — data structures without user-facing interfaces are INCOMPLETE.**

Every feature must answer: **"How does a user actually use this?"**

**Complete feature checklist:**
- [ ] **Core logic** — data structures, interfaces, business logic
- [ ] **At least ONE interface** — CLI (`cmd/meept/`), TUI (`internal/tui/`), GUI (`ui/flutter_ui/`), or HTTP API (`internal/comm/http/`)
- [ ] **Agent wiring** — dispatcher routing, intent classification, tool exposure (if agents should use it)
- [ ] **Tests**

**Exception for prototypes:** Experimental features can ship with partial wiring if the PR explicitly notes which interfaces are deferred and why.

**Red flags:**
- Files only in `internal/` with no changes to `cmd/`, `internal/tui/`, `ui/`, or `internal/comm/http/`

### Typed-nil interface guard

Nil `*ConcreteType` assigned to an interface produces a non-nil interface that panics on method calls. Guard at call sites and in `With*` functions:

```go
if tokenCache != nil {
    opts = append(opts, WithTokenCache(tokenCache))
}
```

### Setter methods

Every `Set*` method MUST include a nil guard. Verified by `internal/tools/builtin/setters_test.go`:

```go
func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
    if fc != nil {
        t.fenceChecker = fc
    }
}
```

### Mutex scope

Never hold a mutex across I/O operations. Use "collect under lock, release, then operate":

```go
mu.Lock()
cfg := m.config  // snapshot
mu.Unlock()
result, err := doNetworkCall(ctx, cfg)  // I/O outside lock
```

## UI Conventions

- **All UI text must be lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- For TUI, use bubblezone for positioning
- Default to clickable elements for context switching
- **TUI and Flutter GUI features must be kept at parity.** When a feature is added or changed in one surface, the other surface gets the same capability. This includes: status bar elements, command palette items, keyboard shortcuts (prefer identical keys across surfaces — e.g., `Ctrl+V` for verbosity on all platforms, not `Cmd+V` on mac), session/agent/tab semantics (e.g., archive vs delete), and tab affordances. Document surface-specific deviations explicitly with a justification.

## Configuration

All config uses **JSON5** format. Templates in `config/`, copied on `make install`.

- **Main**: `~/.meept/meept.json5`
- **Models**: `config/models.json5` (capability-based resolution)
- **MCP servers**: `~/.meept/mcp_servers.json5` (21 preconfigured, 4 enabled by default)
- **Client**: `~/.meept/client.json5` (TUI keybindings)

See `docs/configuration/` for full reference.

## Feature Documentation Requirements

All code changes to feature implementations must have corresponding documentation updates.

**Documentation locations:**
- `docs/workflows/` — Feature specifications
- `docs/concepts/` — Architecture
- `docs/reference/` — CLI, API, tools

**Feature mapping:** `internal/<pkg>/` → `docs/workflows/<pkg>.md`
