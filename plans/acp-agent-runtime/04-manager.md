# ACP Manager + Registry — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/acp/manager.go: catalog-driven registry of live sessions, GetOrCreate/Stop/StopAll, disabled-path (Enabled()==false -> zero tools)
- **Dependencies:** 01 (config + catalog types), 03 (Session)
- **Estimated Context:** 60K
- **Concurrency Group:** C (file-disjoint from 03: owns manager.go only)

## Goal

The Manager is the single object the daemon holds. It loads the agents catalog, enforces MaxAgents and per-agent enabled flags, lazily creates sessions per agent id, and exposes the tool-facing surface. The disabled path is the most important behavior: when `[acp] enabled=false` (default), Manager.Tools() returns an empty slice, GetOrCreate returns a disabled error, and StopAll is a no-op — so the daemon wiring leaf (07) has literally nothing to do in the default configuration.

## Context

- internal/config/acp.go (from leaf 01) — ACPAgentEntry/ACPAgentsConfig, LoadACPAgents
- internal/acp/session.go (from leaf 03) — Start/Send/Cancel/Close/Events
- internal/tools/registry.go — tools.Tool interface shape for the Tools() bridge (read-only; leaf 05 owns the tool file)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/acp/manager.go
type Manager struct { /* mu, cfg, agents catalog, sessions map */ }

func NewManager(cfg config.ACPConfig, catalog *config.ACPAgentsConfig) *Manager
func NewManagerFromFiles(cfg config.ACPConfig) (*Manager, error) // LoadACPAgents(cfg.AgentsFile) inside

func (m *Manager) Enabled() bool                 // cfg.Enabled
func (m *Manager) Agents() []config.ACPAgentEntry // catalog snapshot
func (m *Manager) GetOrCreate(ctx context.Context, agentID string, workdir string) (*Session, error)
// errors: ErrDisabled (acp disabled), ErrAgentNotFound, ErrAgentDisabled, ErrMaxAgents (sentinel errors, errors.Is-able)
func (m *Manager) Stop(agentID string) error
func (m *Manager) StopAll()                      // daemon shutdown hook; idempotent
func (m *Manager) LiveSessions() map[string]SessionState // status surfacing (leaf 08)
```

### What This Leaf Consumes

From 01: config.ACPConfig, config.ACPAgentEntry, LoadACPAgents. From 03: Session, Start, SessionConfig, SessionState.

## Tasks

### Task 1: Failing tests — disabled path

**Files:** Create: internal/acp/manager_test.go

NewManager with Enabled=false: Enabled()==false, Tools()-adjacent behavior, GetOrCreate returns ErrDisabled (errors.Is), StopAll() no-op without panic. No subprocess spawned (assert via counting Start calls with an injectable start hook or by absence of any process side effects).

### Task 2: Failing tests — enabled path with fake sessions

**Files:** Extend: internal/acp/manager_test.go

Inject a session-start function (manager takes an unexported `startFn` overridable in tests — constructor injection, not a global) returning fake sessions. GetOrCreate creates and caches by agent id; second call returns same session. ErrAgentNotFound for unknown id; ErrAgentDisabled for catalog entry enabled=false; ErrMaxAgents at limit. Stop removes from map and closes session; StopAll closes all; LiveSessions reflects states.

### Task 3: Manager implementation

**Files:** Create: internal/acp/manager.go

Sessions map guarded by mu (collect-under-lock pattern). Catalog snapshot at construction (Reload() optional — only if trivial; otherwise defer to a future leaf and note it). Workdir: catalog Cwd overrides; empty Cwd -> the workdir argument (tool layer passes session working dir per the "daemon CWD is not the user's project" invariant). MaxAgents enforced across concurrent GetOrCreate (test with 5 parallel goroutines against MaxAgents=2).

### Task 4: Wire-behavior test against real fakeagent binary

**Files:** Extend: internal/acp/manager_test.go

One test uses the leaf-03 fakeagent binary end-to-end through the Manager (enabled config, temp catalog file): NewManagerFromFiles -> GetOrCreate -> Send -> StopAll. Proves config->catalog->session plumbing without any tool layer.

## Self-Verification Checklist

- [ ] go build ./internal/acp/ green
- [ ] go test ./internal/acp/ -race -count=1 green (full package incl. leaf 03 tests)
- [ ] Disabled path: zero subprocess spawns, zero log lines (grep test output)
- [ ] Sentinel errors exported and errors.Is-able
- [ ] mutexio clean; no lock across session Start/Send/Close calls
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] All six error/lifecycle behaviors tested and correct
- [ ] Concurrent GetOrCreate respects MaxAgents
- [ ] Disabled path proven inert
- [ ] Workdir precedence (catalog Cwd > argument) tested
- [ ] File scope: manager.go + manager_test.go ONLY — no session.go edits (03 owns it); report foreign hunks

## Notes

- Manager intentionally knows nothing about tools.Tool — the bridge lives in leaf 05 to keep this file dependency-clean (acp -> config/session only; tools -> acp one-directional).
- Reload-while-running is explicitly deferred; StopAll+NewManagerFromFiles is the supported restart path (daemon restart already does this).
