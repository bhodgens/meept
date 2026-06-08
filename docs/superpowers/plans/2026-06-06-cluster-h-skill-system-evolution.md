# Cluster H: Skill System Evolution - Implementation Plan

## Plan Review

**Status:** COMPLETE

### Key Findings from Codebase Analysis

1. **ClawSkills is dead code** - `internal/clawskills/` has a registry client but it's never wired into the daemon. The RPC handler exists but is never instantiated. `meept clawskills install` would fail at runtime.
2. **Only 1 file imports clawskills** - `internal/rpc/clawskills.go`
3. **3-tier discovery** - `DefaultTiers()` returns project/user/system tiers. Plan adds a 4th Claude tier.
4. **SkillMetadata** has YAML tags with both hyphen and underscore variants already supported.
5. **Claude skills** use nearly identical SKILL.md format with `allowedTools` field (camelCase). Need adapter.

## Tasks

### Task 1: Remove ClawSkills Package
**Status:** DONE
**Complexity:** Mechanical (delete files, remove references)
**Files:** `internal/clawskills/clawskills.go`, `internal/rpc/clawskills.go`, `cmd/meept/clawskills.go`, docs references
- `internal/clawskills/` directory deleted
- Zero Go source file references to clawskills remain
- Stale references exist only in docs/auto-analysis files (not Go code)

### Task 2: Add MCP-Embedded Skills Support
**Status:** DONE
**Complexity:** Integration (new field + runtime lifecycle)
**Files:** `internal/skills/models.go`, `internal/skills/mcp_runtime.go`, `internal/skills/executor.go`
- `MCPServerConfig` struct in `models.go` with Name, Command, Args, Env fields
- `MCPRuntime` in `mcp_runtime.go`: lifecycle management (Start/Shutdown/Tools), mutex-protected, graceful shutdown with JSON-RPC shutdown notification
- `SkillExecutor` in `executor.go`: integrates MCP runtime into Execute/ExecuteWithMessages, defers Shutdown
- `SkillMCPTool` in `skill_mcp_tool.go`: wraps MCP tools as `tools.Tool` for agent-loop integration
- Tests: `mcp_runtime_test.go` (12 tests), `executor_test.go` (8 tests including MCP scenarios), `skill_mcp_tool_test.go` (6 tests)

### Task 3: Add Claude Skills Discovery Tier
**Status:** DONE
**Complexity:** Integration (new tier + format adapter)
**Files:** `internal/skills/discovery.go`, `internal/skills/adapter.go`, `internal/skills/source_claude.go`, `internal/skills/parser.go`
- `PriorityClaude = 2` constant in `discovery.go` (between user=1 and system=3)
- `ClaudeSource` in `source_claude.go`: scans `~/.claude/skills/`, applies adapter, context support
- `ClaudeSkillAdapter` in `adapter.go`: sets Source="claude", derives description from body, derives tags from parent directory
- CamelCase field normalization in `parser.go`: `allowedTools`, `riskLevel`, `maxIterations`, `maxTokens` all mapped
- Trigger-to-Tags mapping in `parser.go`
- Tests: `source_claude_test.go` (8 tests), `adapter_test.go` (6 test groups), `parser_test.go` (camelCase + trigger tests)

### Task 4: Unified Skill Loader with Pluggable Sources
**Status:** DONE
**Complexity:** Architecture (refactor discovery into interface-based system)
**Files:** `internal/skills/discovery.go`, `internal/skills/source_file.go`, `internal/skills/source_claude.go`
- `SkillSource` interface: `Name() string` + `Discover(ctx context.Context) ([]*Skill, error)`
- `FileSource`: multi-tier filesystem discovery with priority shadowing, metadata-only path, context cancellation
- `ClaudeSource`: dedicated source for `~/.claude/skills/` with adapter integration
- `Discovery` orchestrator: pluggable via `WithSources()`, `WithTiers()`, `WithDiscoveryLogger()`; default includes FileSource + ClaudeSource
- `DiscoverMetadataOnly()`: optimized path using FileSource metadata-only scanning, fallback for other sources
- `NewDiscovery()` default creates both FileSource (3 tiers) and ClaudeSource
- Tests: `discovery_test.go` (14 tests), `discovery_sources_test.go` (9 tests), `source_file_test.go` (12 tests)

## Verification

All 95 tests pass:
```
ok  github.com/caimlas/meept/internal/skills  0.352s
```

Package compiles cleanly: `go build ./internal/skills/...` succeeds.

## Execution Order

Tasks 1 is independent. Tasks 2, 3, and 4 can be done sequentially since they build on each other.
Task 1 can run in parallel with nothing (it's a cleanup). Task 2 and 3 are independent of each other.
Task 4 depends on both 2 and 3.

Recommended: Task 1 first (cleanup), then Task 2 and 3 in parallel, then Task 4.
