# Cluster H: Skill System Evolution - Implementation Plan

## Plan Review

**Status:** Ready for execution

### Key Findings from Codebase Analysis

1. **ClawSkills is dead code** - `internal/clawskills/` has a registry client but it's never wired into the daemon. The RPC handler exists but is never instantiated. `meept clawskills install` would fail at runtime.
2. **Only 1 file imports clawskills** - `internal/rpc/clawskills.go`
3. **3-tier discovery** - `DefaultTiers()` returns project/user/system tiers. Plan adds a 4th Claude tier.
4. **SkillMetadata** has YAML tags with both hyphen and underscore variants already supported.
5. **Claude skills** use nearly identical SKILL.md format with `allowedTools` field (camelCase). Need adapter.

## Tasks

### Task 1: Remove ClawSkills Package
**Complexity:** Mechanical (delete files, remove references)
**Files:** `internal/clawskills/clawskills.go`, `internal/rpc/clawskills.go`, `cmd/meept/clawskills.go`, docs references

### Task 2: Add MCP-Embedded Skills Support
**Complexity:** Integration (new field + runtime lifecycle)
**Files:** `internal/skills/models.go`, new `internal/skills/mcp_runtime.go`, `internal/skills/executor.go`

### Task 3: Add Claude Skills Discovery Tier
**Complexity:** Integration (new tier + format adapter)
**Files:** `internal/skills/discovery.go`, new `internal/skills/adapter.go`, `internal/skills/parser.go`

### Task 4: Unified Skill Loader with Pluggable Sources
**Complexity:** Architecture (refactor discovery into interface-based system)
**Files:** `internal/skills/discovery.go`, new source files

## Execution Order

Tasks 1 is independent. Tasks 2, 3, and 4 can be done sequentially since they build on each other.
Task 1 can run in parallel with nothing (it's a cleanup). Task 2 and 3 are independent of each other.
Task 4 depends on both 2 and 3.

Recommended: Task 1 first (cleanup), then Task 2 and 3 in parallel, then Task 4.
