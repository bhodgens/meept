# Lazy Tool Schemas (Indexed Mode + tool_view) - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Indexed schema mode stubbing rare tools to one line; tool_view builtin loads full schema on demand with LRU.
- **Deps:** none (coordinates with 01 only via prompt ordering — tools section unstable already) | **Context:** 65K | **Group:** A

## Goal

Registry.GetDefinitions() ships every tool's full parameter schema on every call (registry.go:169-187). This costs tokens against EVERY provider — cloud models pay per token too, and large schemas bloat cached prefixes whenever toolsets change. Indexed mode becomes DEFAULT-ON for all providers: non-core tools collapse to one-line descriptions instructing `tool_view{name}`; a new builtin returns the full definition as tool-result JSON; expansions LRU-cached.

TRADE-OFF ANALYSIS (user-requested, 2026-08-24):

Pros of indexed-by-default:
- Token conservation on every call for every provider (cloud bills shrink too)
- Smaller stable prefix -> cheaper provider cache writes; toolset changes disturb fewer bytes
- Small local models become viable (schema flood is their top failure cause)
- Forces honest tool descriptions (the one-liner IS the pitch)

Cons / risks:
- Two-step tool use: model must call tool_view before first use of a rare tool (+1 round trip, +latency)
- Weak models may NOT realize they should call tool_view (mitigation: description suffix instructs exactly that; always_full list keeps common tools full)
- Tool-selection accuracy can DROP when the model picks from one-liners without seeing parameter shapes (mitigation: keep core set full; measure via benchmark before/after)
- Failure modes shift from "wrong args" to "no attempt" if descriptions undersell tools
- More moving parts in prompt assembly (mode switching mid-session must not churn the cached prefix — SetSchemaMode changes definitions payload = cache invalidation by design; document)

Verdict: default ON with a curated always_full core set; [agent.tools] schema_mode="full" restores legacy. Measure selection-quality delta in meept-bench GAIA runs.

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

Config [agent.tools]: schema_mode ("indexed" DEFAULT per user directive), always_full list (default: shell,file_read,file_edit,file_write,memory_search,memory_store,web_fetch,websearch,platform_status,tool_view). Loop passes mode through existing wiring path used by FilterToolsForSkill so filtered registries inherit mode+alwaysFull.

PER-MODEL / PER-PROVIDER OVERRIDES (user directive, 2026-08-24):

Resolution order (most specific wins; unset falls through):
1. Model entry in config/models.json5: `schema_mode` field on a model definition
2. Provider block: `schema_mode` on the provider
3. Global [agent.tools].schema_mode

```json5
// models.json5 shape (fields added to existing structures):
providers.anthropic: { ..., schema_mode: "indexed" }        // provider default
providers.anthropic.models["claude-opus-4-5"]: { ..., schema_mode: "full" }
// opus gets full schemas; every other anthropic model indexed.
// Unset at both levels -> global [agent.tools].schema_mode ("indexed").
```

Implementation surface:
- internal/llm/models_catalog.go ModelCatalogEntry gains `SchemaMode string`
- Provider config struct gains same field
- Resolver helper `EffectiveSchemaMode(providerID, modelID string) SchemaMode` in llm package (catalog lookup -> provider -> global default); loop consults it where it currently reads [agent.tools] only
- always_full list stays global (per-model tool lists would explode config surface; revisit only if benchmarks demand)
- Validation: unknown mode strings rejected at config load with pointer to the offending model/provider line

## Tasks

1. Failing tests registry-side: indexed mode stubs non-core (params empty, description contains tool_view); core untouched; switching modes mid-flight safe under -race; FilteredToolRegistry inherits.
1b. Failing tests EffectiveSchemaMode resolution: model override beats provider, provider beats global, all-unset -> global default, unknown string -> config-load error (test via config parse path).
2. Implement SetSchemaMode + GetDefinitions branch.
3. Failing tests tool_view: known tool returns parseable JSON w/ params; unknown errors; LRU eviction bounded (size cap respected); nil-registry guard.
4. Implement builtin; register in registerBuiltinTools (components.go ~1680 block) — meta category excluded from indexing itself.
5. Config plumbing + loop call-site honoring [agent.tools]; docs paragraph in docs/reference/tools page (locate).
6. Size assertion test: indexed payload bytes < full payload bytes for a fixture registry of 20 tools (regression guard).

## Self-Verification Checklist
- [ ] -race green internal/tools internal/agent
- [ ] Indexed mode is DEFAULT; full mode restores legacy byte-identical payloads
- [ ] tool_view not indexable (always in alwaysFull implicitly)
- [ ] Docs updated incl. trade-off table from Goal section

## Review Checklist
- [ ] Contracts exact; no schema loss in expansion roundtrip (JSON marshal/unmarshal equality test)
- [ ] No lock held during JSON marshaling of large defs (mutexio)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: GBNF leaf (12) later consumes grammar from FULL definitions — keep full fidelity inside tool_view output.
