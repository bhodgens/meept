# Task-Loop Schema Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Task-scoped agent loops created via AgentRegistry.GetForTask inherit the tool-schema-mode config, ending the 75-tool full-catalog context burn.
- **Dependencies:** 03 (shares internal/daemon/components.go — dispatch only after 03 is committed)
- **Estimated Context:** 30K
- **Audit references:** finding F4 (coder loop hit 49k/50k context on a trivial task; "Approaching conversation token budget" in logs)

## Goal

Only the primary AgentLoop receives SetSchemaModeConfig
(internal/daemon/components.go:1422). Task-scoped loops are built lazily by
AgentRegistry.createLoop (internal/agent/registry.go:337) with their OWN
filtered tool registry, and never learn the schema mode — so specialists
ship all ~75 full schemas every turn. The indexed mode (default-on,
loop-economics leaf 02) collapses non-core tools to one-liners with
tool_view expansion (internal/tools/registry.go:212-234). This leaf wires
the config into the registry so every task-scoped loop gets indexed mode.

## Context

Resolution order to mirror (loop.go:994-1007): resolved per-model
schema_mode > provider schema_mode > global [agent.tools].schema_mode >
"indexed"; always_full falls back to config.DefaultAlwaysFullTools()
(internal/config/schema.go:1361: shell, file_read, file_edit, file_write,
memory_search, memory_store, web_fetch, websearch, platform_status,
tool_view). The loop applies it via an interface assertion on its registry;
the registry-side implementation is SetSchemaMode(mode SchemaMode,
alwaysFull []string) on internal/tools/Registry.

Key files to understand before implementing:
- internal/agent/registry.go - AgentRegistry struct/fields (~430-500), createLoop (337-460+), GetForTask (285)
- internal/agent/loop.go:973-1040 - SetSchemaModeConfig + applySchemaModeLocked (the pattern to mirror)
- internal/config/schema.go - AgentToolsConfig (SchemaMode, AlwaysFull fields ~1333-1345), DefaultAlwaysFullTools (1361)
- internal/daemon/components.go:1419-1422 - existing wiring site for the primary loop

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/registry.go
//   func (r *AgentRegistry) SetSchemaModeConfig(cfg config.AgentToolsConfig)
//     - stores cfg (mutex-guarded; mirror existing field-guard style)
//     - applies immediately to any already-cached task loops AND is
//       consulted by createLoop for every new loop
//   createLoop: after the tool registry option is built, apply schema mode
//     with the same resolution as loop.go:994-1007 (global > indexed
//     default; AlwaysFull fallback DefaultAlwaysFullTools()).
// internal/daemon/components.go
//   alongside components.go:1422: c.AgentRegistry.SetSchemaModeConfig(cfg.Agent.Tools)
//   (exact receiver name verified against components.go at implementation time)
// platform_tools / platform_agents / tool_view must remain usable (either
// in AlwaysFull or reachable via tool_view expansion).
// Owner: 04. Consumers: 10 (e2e context-budget check).
```

### What This Leaf Consumes

```
// config.AgentToolsConfig, config.DefaultAlwaysFullTools()
// tools.SchemaMode / SchemaModeIndexed / SchemaModeFull, Registry.SetSchemaMode
```

## Tasks

### Task 1: registry-side schema-mode config

**Objective:** AgentRegistry stores config and applies it in createLoop.

**Files:**
- Modify: `internal/agent/registry.go` (struct fields + SetSchemaModeConfig + createLoop)
- Test: `internal/agent/registry_test.go` (exists; follow its setup helpers)

**Step 1: Write failing test**

```go
func TestAgentRegistry_TaskScopedLoopGetsIndexedSchema(t *testing.T) {
	r := newTestAgentRegistry(t) // mirror existing registry test construction:
	// real tools registry with several dummy tools + spec registered
	r.SetSchemaModeConfig(config.AgentToolsConfig{}) // zero value = indexed default
	loop, err := r.GetForTask("coder", "task-schema-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	defs := loopRegistryDefinitions(t, loop) // helper: fetch the loop's registry and call ToLLMDefinitions
	for _, d := range defs {
		if d.Name == "shell" {
			continue // always-full core tool
		}
		if len(d.Parameters) > 0 { // stubbed tools carry empty object schemas
			t.Logf("tool %s still ships full schema (%d params)", d.Name, len(d.Parameters))
		}
	}
	// Assert: at least one non-core tool is stubbed to a one-line
	// description containing "use tool_view{".
}
```

Adjust assertions to the real ToLLMDefinitions shape (check
internal/tools/registry_schema_test.go for how stubbing is asserted —
reuse its style so the test matches actual wire shapes).

**Step 2: verify failure** — `go test -p 2 ./internal/agent/ -run TestAgentRegistry_TaskScopedLoopGetsIndexedSchema -v` → FAIL (full schemas today).

**Step 3: implement**

- Add fields `schemaModeCfg config.AgentToolsConfig; schemaModeSet bool` to
  AgentRegistry (near existing config fields, same mutex).
- SetSchemaModeConfig stores both, then walks cached task loops applying
  the resolution (best-effort; loops expose their registry — if not,
  apply only in createLoop and note the deviation).
- In createLoop, after the `r.tools` branch (registry.go:395-399), resolve
  mode: `mode := tools.SchemaModeIndexed; if schemaModeSet && cfg.SchemaMode == "full" { mode = tools.SchemaModeFull }`
  and alwaysFull: `cfg.AlwaysFull` else `config.DefaultAlwaysFullTools()`;
  call `filtered.SetSchemaMode(mode, alwaysFull)` (guard with the same
  interface assertion pattern loop.go:1005 uses so placeholder registries
  are safe).

**Step 4: verify pass** — same run → PASS; full package: `go test -p 2 ./internal/agent/ -run TestAgentRegistry -v` → PASS.

### Task 2: daemon wiring

**Objective:** The daemon feeds the registry the same config the primary loop gets.

**Files:**
- Modify: `internal/daemon/components.go` (immediately after line 1422)

**Step 1: Write failing test** — components-level wiring is covered by
integration (leaf 10). Add a compile-level guard: assert in an existing
components test that `c.AgentRegistry` (actual field name per source)
non-nil after initialization when tools are configured. If a components
test harness does not exist, cover via registry_test (Task 1) plus a
manual-integration note instead — record as deviation.

**Step 2: implement** — one line beside components.go:1422.

**Step 3: verify** — `go build ./... && go test -p 2 ./internal/daemon/ -run Schema -v` → PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Placeholder registries (no SetSchemaMode) remain safe — no panic
- [ ] `go build ./...` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] SetSchemaModeConfig present, mutex-guarded, nil-safe
- [ ] createLoop applies resolution matching loop.go:994-1007 semantics
- [ ] Daemon wiring beside components.go:1422
- [ ] DefaultAlwaysFullTools still honored; tool_view never stubbed
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- master Open Questions: if per-loop application is impossible, the
  fallback site is components.go:7179-7203 (GetForTask branch in
  AgentJobProcessor.Process) applying schema mode right after loop
  creation. Prefer the registry approach; use the fallback only with a
  documented Deviation.
- Context-firewall budget numbers live in AgentConfig.MaxConversationTokens
  — do NOT change budgets; the schema shrink is the fix.
- components.go is hot: keep the hunk to the one wiring line.
