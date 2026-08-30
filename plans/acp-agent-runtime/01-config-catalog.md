# Config Section + Agents Catalog — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit — the orchestrator handles all git operations after review.
> Do NOT use read_file on existing source files — explore with search_files or
> terminal cat. After writing a file, do NOT read it back to verify.

## Meta

- **Parent:** ../master.md
- **Scope:** `[acp]` config section in internal/config/schema.go + agents catalog parse/save in internal/config + template file config/acp_agents.json5
- **Dependencies:** none (first leaf)
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Meept learns a new top-level config section `[acp]` (disabled by default) and a JSON5 catalog of external ACP agents. When disabled (the default and current state), nothing else in the daemon reads or references it. Follow the exact pattern of MCPConfig (internal/config/schema.go:1761) including defaults in DefaultConfig (~line 2391).

## Context

Key files:
- internal/config/schema.go — Config struct (line ~72 holds `MCP MCPConfig`), MCPConfig def (1761), DefaultConfig MCP block (~2391)
- internal/config/mcp.go — LoadMCPConfig/SaveMCPConfig atomic-write pattern to mirror
- config/mcp_servers.json5 — catalog template convention (repo copy, copied to ~/.meept on install)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/config/schema.go
type ACPConfig struct {
	Enabled        bool   `json:"enabled"         toml:"enabled"`
	AgentsFile     string `json:"agents_file"     toml:"agents_file"`
	DialTimeout    int    `json:"dial_timeout"    toml:"dial_timeout"`
	CallTimeout    int    `json:"call_timeout"    toml:"call_timeout"`
	MaxAgents      int    `json:"max_agents"      toml:"max_agents"`
	PermissionMode string `json:"permission_mode" toml:"permission_mode"` // "permissive"|"deny"
}
// + Config field: ACP ACPConfig `json:"acp" toml:"acp"`
// + DefaultConfig(): Enabled=false, AgentsFile="~/.meept/acp_agents.json5", DialTimeout=10,
//   CallTimeout=120, MaxAgents=3, PermissionMode="permissive" (DECISION Q1)
// + Validate(): DialTimeout>0, CallTimeout>0, 1<=MaxAgents<=32 (DECISION Q4),
//   PermissionMode in {"permissive","deny"} — fail fast at load

// internal/config/acp.go
type ACPAgentEntry struct {
	ID          string            `json:"id"`
	Description string            `json:"description,omitempty"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	DefaultMode string            `json:"default_mode,omitempty"`
	Enabled     bool              `json:"enabled"`
}
type ACPAgentsConfig struct { Agents []ACPAgentEntry `json:"agents"` }
func LoadACPAgents(path string) (*ACPAgentsConfig, error)   // missing file -> empty, not error (LoadMCPConfig pattern)
func SaveACPAgents(path string, cfg *ACPAgentsConfig) error // atomic temp+rename (SaveMCPConfig pattern)
```

### What This Leaf Consumes

Nothing new — reuses internal/config and the hujson/json5 helpers already used by mcp.go.

## Tasks

### Task 1: Failing test for ACPConfig defaults

**Files:** Test: internal/config/schema_test.go (add; grep for existing config test file naming first — if a different convention exists, follow it)

Test: with an empty config file / zero struct, applying DefaultConfig yields `ACP.Enabled == false`, `AgentsFile == "~/.meept/acp_agents.json5"`, `DialTimeout == 10`, `CallTimeout == 120`, `MaxAgents == 3`, `PermissionMode == "permissive"`. Also Validate() rejects DialTimeout<=0, MaxAgents<1, MaxAgents>32, and PermissionMode not in {"permissive","deny"}.

### Task 2: Schema + defaults + validation

**Files:** Modify: internal/config/schema.go (Config struct alphabetical slot near MCP at ~line 72; ACPConfig type near MCPConfig at ~1761; DefaultConfig near ~2391; Validate block). Aligned struct tags; gendoc markers if MCP has them.

### Task 3: Failing test for catalog load/save

**Files:** Test: internal/config/acp_test.go

Round-trip: write ACPAgentsConfig with one codex entry -> Save -> Load -> deep-equal. Missing file -> empty config, nil error. Malformed JSON5 -> error mentions path.

### Task 4: Catalog load/save implementation

**Files:** Create: internal/config/acp.go — mirror mcp.go structure (hujson parse, atomic SaveMCPConfig pattern).

### Task 5: Catalog template

**Files:** Create: config/acp_agents.json5 — one example entry (codex via codex-acp, enabled:false) + header comment documenting fields and the "disabled by default" note. Check `make install` hook list (Makefile) to see whether mcp_servers.json5 template copy is special-cased; if so add acp_agents.json5 alongside; if install copies a directory wholesale, no Makefile change is needed — record which in your report.

### Task 6: Docs

**Files:** Modify: docs/configuration/ (the config reference file covering [mcp] — add an [acp] subsection: fields, defaults, disabled-by-default note). Do not touch docs/generated/.

## Self-Verification Checklist

- [ ] go build ./internal/config/ green
- [ ] go test ./internal/config/ -race -count=1 green (full package, not just -run)
- [ ] DefaultConfig has Enabled:false; grep confirms no `ACP: ACPConfig{Enabled: true`
- [ ] Validate() blocks bad values
- [ ] config/acp_agents.json5 exists, parses (go run a tiny load check or the round-trip test), entry disabled
- [ ] Docs section added
- [ ] No TODOs, no debug prints

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Schema field/tags/defaults/validation exactly per contract
- [ ] Catalog API signatures exactly per contract
- [ ] Template file present, disabled entry
- [ ] Tests present and passing (-race)
- [ ] No unrelated schema.go edits (sibling sessions share this file — `git diff internal/config/schema.go` must contain ONLY ACP hunks; report foreign hunks, do not stage them)

## Notes

- schema.go is shared with parallel sibling sessions. Re-grep your additions before reporting (`grep -n "ACPConfig" internal/config/schema.go`) — a sibling rewrite can silently drop your hunks (documented hazard).
- JSON5 parsing: use exactly what mcp.go uses (hujson or equivalent) — do not add a new parsing dependency.
