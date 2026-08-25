# Lazy Tool Schemas (Indexed Mode + tool_view) - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Indexed schema mode stubbing rare tools to one line; tool_view builtin loads full schema on demand with LRU.
- **Deps:** none (coordinates with 01 only via prompt ordering — tools section unstable already) | **Context:** 65K | **Group:** A

## Goal

Registry.GetDefinitions() ships every tool's full parameter schema on every call (registry.go:169-187). Against small local models this drowns context; against hosted APIs it bloats cached prefixes when toolsets change. Add opt-in indexed mode: non-core tools collapse to one-line descriptions instructing `tool_view{name}`; a new builtin returns the full definition as tool-result JSON; expansions LRU-cached.

## Context

Existing filtering is capability-based ONLY: FilterToolsForSkill (executor.go:1776) narrows sets for skills; ToLLMDefinitions always full-fidelity. llm.ToolDefinition shape in internal/llm/models.go. Builtin tool pattern: any file in internal/tools/builtin (e.g., confirmation.go) shows Parameters()/Execute() conventions.

Key files: internal/tools/registry.go, internal/llm/models.go (ToolDefinition), internal/tools/builtin/ (new file), internal/config/schema.go ([agent.tools] block), internal/agent/loop.go:2708 call site.

## Interface Contracts (From Parent)

```go
// registry.go:
type SchemaMode string // "full"|"indexed"
func (r *Registry) SetSchemaMode(mode SchemaMode, alwaysFull []string)
// indexed GetDefinitions(): tools in alwaysFull -> unchanged full defs;
// others -> Description = original one-line summary + " use tool_view{name}." ;
//          Function.Parameters = empty object schema; name kept.
// thread-safe; mode switchable at runtime (tests).
```

```go
// internal/tools/builtin/tool_view.go:
// Name(): "tool_view"; Category "meta";
// Execute({name}) -> looks up registry definition; returns JSON-marshalled
//   llm.ToolDefinition-equivalent map as Result text; unknown name -> error.
// Constructor NewToolViewTool(reg *Registry, lruSize int) (default 32).
// Evidence: models.NewEvidence("tool_view", name, hash, t.Name()) if pattern exists nearby.
```

Config [agent.tools]: schema_mode ("full" default), always_full list (default: shell,file_read,file_edit,memory_search,platform_status). Loop passes mode through existing wiring path used by FilterToolsForSkill so filtered registries inherit mode+alwaysFull.

## Tasks

1. Failing tests registry-side: indexed mode stubs non-core (params empty, description contains tool_view); core untouched; switching modes mid-flight safe under -race; FilteredToolRegistry inherits.
2. Implement SetSchemaMode + GetDefinitions branch.
3. Failing tests tool_view: known tool returns parseable JSON w/ params; unknown errors; LRU eviction bounded (size cap respected); nil-registry guard.
4. Implement builtin; register in registerBuiltinTools (components.go ~1680 block) — meta category excluded from indexing itself.
5. Config plumbing + loop call-site honoring [agent.tools]; docs paragraph in docs/reference/tools page (locate).
6. Size assertion test: indexed payload bytes < full payload bytes for a fixture registry of 20 tools (regression guard).

## Self-Verification Checklist
- [ ] -race green internal/tools internal/agent
- [ ] Default behavior byte-identical (mode=full)
- [ ] tool_view not indexable (always in alwaysFull implicitly)
- [ ] Docs updated

## Review Checklist
- [ ] Contracts exact; no schema loss in expansion roundtrip (JSON marshal/unmarshal equality test)
- [ ] No lock held during JSON marshaling of large defs (mutexio)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: GBNF leaf (12) later consumes grammar from FULL definitions — keep full fidelity inside tool_view output.
