# Agent Reference: Architecture Overview

Moved from the root `AGENTS.md` to keep the root under the size cap. The root
keeps the package routing table; this file holds the request flow and the
connectivity-graph map. Update both in the same commit when topology changes.

## Architecture Overview

Meept is a **Go platform** with skill-based task orchestration, LLM integration,
memory management, and external integrations.

### Request Flow

```
User Input → CommServer (RPC/HTTP) → MessageBus → AgentLoop → Dispatcher → Planner → Tools → Response
```

### Key Components

| Layer | Packages | Contract |
|-------|----------|----------|
| Server | `internal/daemon`, `internal/rpc`, `internal/bus`, `internal/comm`, `cmd/meept-daemon` | `internal/daemon/AGENTS.md`, `internal/comm/AGENTS.md` |
| Agent | `internal/agent` | `internal/agent/AGENTS.md` |
| LLM | `internal/llm`, `internal/llm/warm.go` | `internal/llm/AGENTS.md`, `docs/workflows/llm-routing-quality.md` |
| Memory | `internal/memory` | — |
| Tools | `internal/tools`, `internal/acp`, `internal/hermes` | `docs/workflows/github.md` |
| Codemode | `internal/codemode` | `docs/workflows/codemode.md` |
| Security | `internal/security`, `internal/auth`, `pkg/security` | — |
| Employee | `internal/employee` | — |
| Audit chain | `internal/auditlog` | — |
| Session | `internal/session` | `internal/session/AGENTS.md` |
| Effects | `internal/effects` | — |
| GitHub | `internal/githubsvc`, github_* tools in `internal/tools/builtin` | `docs/workflows/github.md` |
| Intake | `internal/intake`, `internal/rpc/intake_status.go` | `docs/workflows/intake.md` |
| Kanban | `internal/kanban`, `internal/kanbansvc`, `cmd/meept-kanban` | `internal/kanban/AGENTS.md`, `internal/kanbansvc/AGENTS.md`, `docs/workflows/kanban.md` |
| SMS leg | `internal/sms`, `internal/telnyx`, `internal/voice/telnyx`, `internal/comm/http/messaging.go` | `docs/workflows/sms.md` |
| Project | `internal/project` | `docs/agents/projects-and-workdirs.md` |
| Voice | `internal/voice`, `internal/voiceframe`, `internal/voicechild`, `cmd/meept-voice` | `docs/agents/voice-invariants.md` (load only for voice work), `internal/voice/AGENTS.md`, `docs/workflows/voice-sidecar.md` |
| TUI / GUI | `internal/tui`, `ui/flutter_ui` | `docs/workflows/tui.md`, `docs/workflows/flutter_gui.md` |
| Scheduling | `internal/scheduler`, `internal/queue`, `internal/worker` | — |
| Skills | `internal/skills`, `internal/selfimprove` | — |
| Infra | `internal/config`, `internal/metrics`, `internal/transport`, `internal/pty`, `internal/process`, `internal/eval`, `internal/gate`, `internal/langcode` | `docs/workflows/langcode.md` |

See `docs/concepts/architecture.md` for the full component map. Package-level
rules are merged by Hermes on the directory chain; the contracts column is the
read-first index.

### Connectivity Graph (Bus / RPC / HTTP / WS)

The daemon's components communicate via string-typed bus topics, RPC handlers,
and HTTP routes — all invisible to the compiler. A generated connectivity graph
maps every publish/subscribe edge so you can trace cross-boundary data flow
without reading the code:

- **`docs/generated/bus-topology.md`** — human-readable: every bus topic with
  publishers, subscribers, payload keys; orphan analysis; WS event
  classification; RPC handler map; HTTP route map.
- **`docs/generated/bus-topology.json`** — machine-readable version.
- **`docs/generated/rpc-handlers.json`**, **`http-routes.json`**,
  **`ws-event-map.json`** — individual layer exports.

Regenerate with `make graphs` (runs automatically on every `make build`).

**When debugging cross-boundary issues** (events not reaching clients, wrong
payload fields, orphaned listeners), start with `docs/generated/bus-topology.md`
before reading source.


## Sidecar pairing detail

The kanban sidecar pairing code TTL is 10 minutes (`pairCodeTTL`,
`internal/kanbansvc/pairing.go`); an expired code gets the same uniform 401 as
an unknown code. The pre-commit pre-flight repairs `core.bare=true` flips that
break `go build -buildvcs` and `git status`; see `.githooks/README.md`.
Go-toolchain gates run against the prepared-commit tree (staged index
materialized into a temp tree with `GIT_WORK_TREE`/`GIT_INDEX_FILE` pinned); a
snapshot failure warns and falls back to working-tree mode. Package
classification reads `go list -e` STDOUT only; stderr is progress noise and
must never be folded in. A syntax error or unresolved import is NOT `broken`
(rc=0, empty `.Error`); a directory with no non-test Go files reports
`nofiles`, a skip. The root-artifact check blocks only entries a build can
create (gitignored binaries, `*.test`, `package main` basenames).
