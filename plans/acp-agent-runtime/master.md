# ACP Agent Runtime for Meept — Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 branch orchestrators, 8 leaves
- **Scope:** ACP (Agent Client Protocol) client runtime in meept: drive external agents (Codex, OpenCode, Gemini CLI) as full agents over JSON-RPC stdio, configurable, disabled by default.

## Goal

Meept can already BE an MCP server (verified live 2026-08-28: `meept mcp-chat-server` handshake, tools/list, tools/call all green). This tree adds the missing direction: meept as an ACP **client** that drives other agents as peers. An external agent process (codex-acp, opencode acp, claude-agent-acp, gemini --experimental-acp) is spawned as a subprocess per the catalog entry; meept speaks the Agent Client Protocol over stdio JSON-RPC; tool calls, permission requests, and session updates flow over the wire; results land in the meept tool registry with SecurityEngine gating, same as every other tool family.

Disabled by default. Zero behavior change when `[acp] enabled = false` (the default): no subprocess spawns, no config files read, no schema changes in tool definitions surfaced to models.

## Architecture

New package `internal/acp` (client side): protocol types, JSON-RPC framing over stdio, one session per spawned agent process, request/response correlation, notification fan-out. New tool family `internal/tools/builtin/acp_agent.go` implements the catalog-driven `acp_agent` tool (launch/send/read/wait/stop). Config section `[acp]` in `internal/config/schema.go` (disabled by default) points at an agents catalog file `~/.meept/acp_agents.json5`, same pattern as `mcp_servers.json5`. SecurityEngine prefix rules gate every call. Wiring: `internal/daemon/components.go` constructs the manager when enabled; tool registration and system-prompt hints follow the MCP family's existing pattern.

ACP protocol version: pin `ACP_PROTOCOL_VERSION = "1.0"` at authoring time; verify the exact semver string from agentclientprotocol.com during Leaf 1.1 and record it in SHARED-CONVENTIONS.md before Waves 2-3 dispatch (amber contract, see Notes).

## Interface Contracts

### Contract 1: Config section `[acp]` (owner: 01-config-catalog leaf)

```go
// File: internal/config/schema.go
// Add to Config struct (alphabetical placement near MCP field at line 72):
ACP ACPConfig `json:"acp" toml:"acp"`

// gendoc markers: //gendoc:section(acp) desc: Agent Client Protocol client runtime...
type ACPConfig struct {
	Enabled        bool   `json:"enabled"         toml:"enabled"`         // default false — zero behavior change
	AgentsFile     string `json:"agents_file"     toml:"agents_file"`     // default "~/.meept/acp_agents.json5"
	DialTimeout    int    `json:"dial_timeout"    toml:"dial_timeout"`    // seconds, default 10
	CallTimeout    int    `json:"call_timeout"    toml:"call_timeout"`    // seconds, default 120
	MaxAgents      int    `json:"max_agents"      toml:"max_agents"`      // concurrent subprocesses, default 3
	PermissionMode string `json:"permission_mode" toml:"permission_mode"` // "permissive"|"deny"; default "permissive" (DECISION Q1)
}
// DefaultConfig(): ACP: ACPConfig{Enabled: false, AgentsFile: "~/.meept/acp_agents.json5",
//   DialTimeout: 10, CallTimeout: 120, MaxAgents: 3, PermissionMode: "permissive"}
```

### Contract 2: Agents catalog file (owner: 01-config-catalog leaf)

```json5
// File: ~/.meept/acp_agents.json5 (template shipped at repo config/acp_agents.json5)
{
  "agents": [
    {
      "id": "codex",
      "description": "OpenAI Codex via codex-acp adapter",
      "command": ["npx", "-y", "@agentclientprotocol/codex-acp"],
      "env": {},
      "cwd": "",              // empty = inherit session working dir at call time
      "default_mode": "read-only",
      "enabled": false        // per-agent enable, independent of [acp] enabled
    }
  ]
}
```

### Contract 3: Protocol types (owner: 02-protocol-types leaf)

```go
// File: internal/acp/protocol.go
package acp

const ProtocolVersion = "1.0" // amber: verify exact semver from agentclientprotocol.com in this leaf

// JSON-RPC request/response IDs are int64 or string per JSON-RPC 2.0; ACP uses increasing ints.
type Request struct {
	JSONRPC string `json:"jsonrpc"` // always "2.0"
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `session/update"` // etc
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
```

### Contract 4: Session lifecycle API (owner: 03-session-lifecycle leaf)

```go
// File: internal/acp/session.go
package acp

type Session struct { /* manager-owned handle: proc, conn, pending map, subs */ }

// Start spawns the agent subprocess and runs the ACP handshake.
func Start(ctx context.Context, cfg SessionConfig) (*Session, error)

// Send submits a user turn. Returns after the agent's final chat message.
func (s *Session) Send(ctx context.Context, text string, blocks []ContentBlock) (string, error)

// Cancel interrupts the in-flight turn.
func (s *Session) Cancel() error

// Close kills the subprocess and releases resources. Idempotent.
func (s *Session) Close() error
```

### Contract 5: Manager + tool call path (owner: 04-manager leaf)

```go
// File: internal/acp/manager.go
package acp

type Manager struct { /* registry of live sessions keyed by agent id */ }

func NewManager(cfg acpconfig.ACPConfig, agents []AgentEntry) *Manager
func (m *Manager) GetOrCreate(ctx context.Context, agentID string, workdir string) (*Session, error)
func (m *Manager) Stop(agentID string) error
func (m *Manager) StopAll()  // daemon shutdown hook
func (m *Manager) Enabled() bool
func (m *Manager) Tools() []tools.Tool  // zero tools when disabled
```

### Contract 6: Tool call path (owner: 05-tool leaf)

```go
// File: internal/tools/builtin/acp_agent.go
// Tool name: "acp_agent" (single meta-tool; params select verb)
// Params: {"agent": string, "verb": "launch|send|read|stop", "message": string, "session": string}
// Executes through acp.Manager; errors are tool Result errors, never panics.
```

### Contract 7: SecurityEngine gating (owner: 06-security leaf)

```go
// File: pkg/security/engine.go lookupBaseRule additions (prefix rules, cua-driver pattern):
// acp_agent verb=launch|send  -> HIGH (subprocess spawn / message into an external agent)
// acp_agent verb=read|stop    -> LOW
// Unknown acp_agent.* name    -> HIGH fail-closed (cua-driver precedent)
```

### Contract 8: Daemon wiring (owner: 07-daemon-wiring leaf)

```go
// File: internal/daemon/components.go
// When cfg.ACP.Enabled: construct acp.NewManager, register its tools via the
// same path MCP tools use; on shutdown call StopAll(). When disabled: construct
// nothing, register zero tools, zero log lines about ACP.
```

### Contract 9: TUI/GUI status surfacing (owner: 08-status-surface leaf)

```
// TUI statusbar token: "acp:N" (N = live agent sessions) when acp.Enabled; absent when disabled.
// HTTP: GET /api/v1/acp/agents — {enabled, agents:[{id, running, uptime_s}]}
// Requires mcp:true-equivalent auth posture already used by /api/v1/mcp/servers.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-config-catalog.md | leaf | none | 45K | A |
| 02 | 02-protocol-types.md | leaf | 01 (config types for compile) | 50K | B |
| 03 | 03-session-lifecycle.md | leaf | 02 | 80K | C |
| 04 | 04-manager.md | leaf | 01, 03 | 60K | C |
| 05 | 05-tool.md | leaf | 04 | 50K | D |
| 06 | 06-security.md | leaf | none | 35K | A |
| 07 | 07-daemon-wiring.md | leaf | 04, 05 | 55K | D |
| 08 | 08-status-surface.md | leaf | 07 | 45K | E |
| 09 | 09-flutter-parity.md | leaf | 08 | 40K | F |

**Concurrency groups:** A runs immediately; B needs 01's config struct to compile against; C needs B's types; D needs C's sessions and 05 needs 04; E is the Go status surface; F is the Flutter surface (depends on E's endpoint). 01 and 06 are fully parallel from second zero.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

1. **Read** `01-config-catalog.md` and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-config-catalog.md"
   - Context: full leaf text + Contract 1 + Contract 2 + coding conventions + the `[acp]` schema insertion point (Config struct at internal/config/schema.go:72, MCPConfig at 1761, DefaultConfig MCP block at ~2391) INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include the read_file/read-back prohibitions verbatim from the template
2. **Read** `06-security.md` and dispatch via `delegate_task`:
   - Same inclusions; inline the cua-driver rule shape from pkg/security/computer_use.go

### Phase 2: Groups B and C (serial by dependency)

Dispatch 02 after 01 reaches REVIEWED; dispatch 03 and 04 after 02 reaches REVIEWED (03 and 04 can overlap: 03 owns session.go, 04 owns manager.go — disjoint files, shared types from 02).

### Phase 3: Group D (wiring)

Dispatch 05 and 07 after 04 reaches REVIEWED. 05 touches internal/tools/builtin/, 07 touches internal/daemon/components.go — disjoint files.

### Phase 4: Group E and F, integration

Dispatch 08 after 07 reaches REVIEWED. Dispatch 09 after 08 reaches REVIEWED (Flutter consumes the endpoint and payload shape 08 freezes). Then run the Integration Test Plan.

### Review and Commit (per child, every phase)

The orchestrator reviews in-session (main model, NOT a delegated reviewer — delegated reviewers inherit delegation.model). Per child:

1. Read changed files; check against leaf spec + contracts + Review Checklist
2. Gaps → re-dispatch with specific feedback (max 3 cycles, then escalate)
3. Pass → `git add <exact leaf file paths>` && commit `feat(acp): <leaf subject>` — update tracking table to REVIEWED
4. Note: this repo has parallel sibling sessions. Stage EXACT paths only. Never `git add .`. Before commit, `git diff --cached --stat` must show only that leaf's files.

### Integration Review (root, after all children REVIEWED)

1. `go build ./...` green
2. `go test ./internal/acp/ ./internal/tools/builtin/ ./internal/config/ ./pkg/security/ -race -count=1` green
3. `make lint-ci` green (or document pre-existing failures untouched by this tree)
4. Contract verification: every contract in this doc satisfied; grep `[acp]` default enabled=false in DefaultConfig
5. Disabled-by-default proof: start daemon with no `[acp]` section → zero ACP log lines, zero acp tools in registry
6. gofmt only on touched files
7. Pristine worktree validation: `git worktree add /tmp/acp-verify HEAD` + full build there before claiming complete (repo convention from multi-session hazards)

## Review Checklist

- [ ] All tasks from each leaf implemented
- [ ] Interface contracts from this doc satisfied exactly
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD)
- [ ] Disabled by default: zero behavior change with `[acp]` absent
- [ ] No scope creep
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption (`grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/acp/ internal/tools/builtin/ internal/config/ pkg/security/` returns zero)
- [ ] No predictable IDs (pkg/id.Generate() convention; predid analyzer passes)

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept), stdlib + existing deps only — check go.mod before adding any import
- **Naming:** exported PascalCase, unexported camelCase
- **Imports:** stdlib first, then module-local; no unused imports (U1000 pre-commit gate is active)
- **Error handling:** wrap with %w; never `_ = expr` on new lines (pre-commit errors gate); map access two-value form; no bare panic
- **IDs:** pkg/id.Generate() only — never time.Now().UnixNano() or math/rand (predid analyzer)
- **Testing:** table-driven where natural; tests alongside source; -race clean
- **Formatting:** gofmt on touched files only; never pass .md to gofmt
- **Mutex:** collect-under-lock-release-then-operate; mutexio analyzer active
- **Config schema edits:** aligned struct tags, DefaultConfig entry, Validate() block, gendoc markers — see meept-development skill
- **UI text:** lowercase everywhere (TUI tokens, HTTP status strings)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-config-catalog | COMPLETE | 1 | 5358bff3 |
| 02-protocol-types | COMPLETE | 1 | 646c084b |
| 03-session-lifecycle | COMPLETE | 1 | f90c930a (orchestrator filled after 03 timeout) |
| 04-manager | COMPLETE | 1 | b1e18dc1 |
| 05-tool | COMPLETE | 1 | 34364945 |
| 06-security | COMPLETE | 1 | 6ceec0e5 |
| 07-daemon-wiring | COMPLETE | 1 | 1079c726 |
| 08-status-surface | COMPLETE | 1 | daf61f7c |
| 09-flutter-parity | COMPLETE | 1 | bfbb8116 |

## Integration Test Plan

1. Unit: `go test ./internal/acp/ -race` — fake agent process (test helper that speaks ACP over stdio pipes) covers handshake, send, cancel, close, malformed response, process death mid-call
2. Tool: `go test ./internal/tools/builtin/ -run TestACPAgent` — disabled config returns tool-not-available; enabled config with fake manager returns proper results
3. Security: `go test ./pkg/security/ -run ACP` — launch/send HIGH, read/stop LOW, unknown fail-closed
4. Config: `go test ./internal/config/ -run ACP` — defaults (enabled=false), validation errors
5. Wiring smoke (manual, orchestrator): build, run daemon with `[acp] enabled=true` + a fake agent binary from the test helpers, call `acp_agent` through the tool registry, verify response; then remove the section, restart, verify zero ACP surface
6. Cross-boundary: `make graphs` regenerates clean (no new bus topics expected; if the wiring adds any, the graph must reflect them)

## Structural Completeness Check (Before Dispatch)

After writing every document in this tree, run:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py plans/acp-agent-runtime --strict-leaves
```

Required sections per orchestrator: Dispatch Protocol, Interface Contracts, Review Checklist, Coding Conventions, Completion Tracking Table, Integration Test Plan. Every leaf: "Do NOT commit", Self-Verification Checklist. Re-run until ALL TREES COMPLIANT: True.

## Open Questions — RESOLVED (user decisions 2026-08-28)

| # | Question | Decision | Landed in |
|---|----------|----------|-----------|
| Q1 | External agent permission requests (`session/requestPermission`) | **Configurable, default permissive.** `ACPConfig.PermissionMode`: `"permissive"` (auto-approve all requests; log each to events) \| `"deny"` (deny-closed, log). Validate() rejects other values. | Contract 1 (PermissionMode); leaf 01 (config+validation); leaf 03 (answer logic) |
| Q2 | Live-agent validation set | **Codex** (via `npx -y @agentclientprotocol/codex-acp`). Follow-up task after landing; never during leaf implementation (fake agent only). | Notes; follow-up task outside this tree |
| Q3 | Flutter GUI parity | **In scope.** Leaf 09 ships Flutter status surface consuming leaf 08's endpoint. Parity deviation removed. | Leaf 09 |
| Q4 | MaxAgents default/cap | **Configurable with defaults 3 / cap 32** (Validate allows 1-32; DefaultConfig sets 3). | Contract 1; leaf 01 |
| Q5 | Tools vs roster members | **Plain tools in v1.** Roster/employee integration explicitly deferred; do not grow leaves toward it. | Notes; SHARED-CONVENTIONS §6 |

Mechanical items (no user decision needed): protocol version + framing verified by leaf 02 amber check; Makefile template copy verified by leaf 01; CLI status passthrough verified by leaf 08.

## Notes

- **Amber contract:** `ACP_PROTOCOL_VERSION` and the exact wire shape of session/update params must be verified against agentclientprotocol.com during Leaf 02 implementation; if they differ from this plan, Leaf 02 updates SHARED-CONVENTIONS.md and this master's Contract 3, and downstream leaves dispatch with the corrected text. Do not let leaves implement against invented values without that verification step.
- **Live-agent validation is out of scope** for this tree. DECISION Q2: codex (via codex-acp) is the chosen follow-up validation target, executed as a separate task after the tree lands — never during leaf implementation (the machine's codex install had an XProtect incident). Integration Test Plan step 5 uses a test fake agent binary, not a real one.
- **Sibling sessions:** this repo runs parallel agent sessions. Leaves stage nothing; the orchestrator stages exact paths. Poll `go build ./internal/acp/` if siblings break the tree, per meept-development skill.
- **Pre-commit gates:** errors gate scans ADDED lines; U1000 unused-code gate; comma-ok exemption active. See references in meept-development skill.
- AGENTS.md feature-wiring requirement: this tree includes CLI/status surfacing (leaf 08) + tool exposure (leaf 05) + config (leaf 01) — wiring checklist satisfied. Docs updates: each leaf includes its docs-workflows section edit per the repo's feature documentation requirement.
