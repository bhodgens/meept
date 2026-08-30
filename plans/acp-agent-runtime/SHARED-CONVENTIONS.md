# SHARED-CONVENTIONS.md — ACP Agent Runtime Tree

Referenced by master.md and every leaf. One source of truth for cross-leaf contracts and repo conventions. Leaves receive the relevant § inlined at dispatch.

## §1 Frozen config shape

```go
type ACPConfig struct {
	Enabled      bool   `json:"enabled"       toml:"enabled"`       // default false
	AgentsFile   string `json:"agents_file"   toml:"agents_file"`   // default "~/.meept/acp_agents.json5"
	DialTimeout  int    `json:"dial_timeout"  toml:"dial_timeout"`  // seconds, default 10
	CallTimeout  int    `json:"call_timeout"  toml:"call_timeout"`  // seconds, default 120
	MaxAgents    int    `json:"max_agents"    toml:"max_agents"`    // default 3
}
```

Catalog entry shape (`config/acp_agents.json5`):

```json5
{ "agents": [ { "id": "codex", "description": "...", "command": ["npx","-y","@agentclientprotocol/codex-acp"],
  "env": {}, "cwd": "", "default_mode": "read-only", "enabled": false } ] }
```

## §2 Frozen protocol constants

Verified 2026-08-29 from https://agentclientprotocol.com/protocol/v1/initialization.md and transports.md:

- `protocolVersion` is a **JSON integer `1`**, NOT a string. Wire field: `"protocolVersion": 1`.
- Framing: **newline-delimited JSON-RPC 2.0** over stdio. Messages MUST NOT contain embedded newlines. NOT LSP Content-Length.
- Core methods (client→agent): `initialize`, `session/new`, `session/prompt`, `session/cancel` (notification).
- Core methods (agent→client): `session/update` (notification), `session/requestPermission` (request).
- JSON object keys: camelCase. Discriminator string values: snake_case.
- File paths MUST be absolute.

Go constant: `const ProtocolVersion = 1` (int, not string).

## §3 Tool surface

Single meta-tool `acp_agent`. Params: `{"agent": string, "verb": "launch|send|read|stop", "message": string, "session": string}`. Registered name, schema, and Result envelope follow the existing tool family conventions (internal/tools/registry + builtin patterns; value-type ToolResult with string Evidence).

## §4 Repo conventions (from meept-development skill)

- IDs: pkg/id.Generate() only. predid analyzer active.
- Errors: %w wrapping, no new `_ =` lines, no bare panic. Pre-commit errors gate.
- Map payloads: two-value type assertions.
- Mutex: collect-under-lock, operate outside; mutexio analyzer active.
- gofmt on touched files only.
- Config edits: struct tags aligned + DefaultConfig + Validate() + gendoc markers.
- Tests: -race clean, table-driven where natural, internal test packages reuse shared fixtures (grep before redeclaring).
- UI text lowercase.

## §5 Commit policy

Leaves never commit. Orchestrator stages EXACT leaf file paths only (sibling sessions share this index — never `git add .`). Pre-commit verify: `git diff --cached --stat` shows only that leaf's files. Post-commit verify: `git show <sha> --stat`.

## §6 Out of scope for this tree

Live codex validation (DECISION Q2: chosen follow-up target, run after landing as a separate task). Streaming of agent output mid-turn (v2). ACP serverside (meept AS an ACP agent for editors) — separate future tree. Roster/employee integration for ACP agents (DECISION Q5: plain tools in v1). NOTE: Flutter GUI parity is IN SCOPE (DECISION Q3, leaf 09) — not a deferral.
