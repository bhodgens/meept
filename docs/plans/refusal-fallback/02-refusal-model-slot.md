# refusal_model Config Slot - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Add the `refusal_model` slot — global default in config PLUS a per-agent `refusal_model` field in AgentSpec.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

The refusal fallback target is configurable at two levels (user decision,
2026-09-16): a GLOBAL default slot in models.json5 and a PER-AGENT
`refusal_model` field on every AI employee spec (any provider/model ref or
alias the user defines). Empty on both levels = feature off for that
agent. This leaf adds both declarations, the overlay merge, and the boot
pre-warm inclusion.

## Context

models.json5 slots are declared in two places and consumed in one:

1. `internal/config/config.go` ModelsConfig (line ~355-360) — daemon-side config.
2. `internal/llm/providers.go` ProvidersConfig (line ~103-116) + overlay merge
   (line ~296-298 for ExtractModel).
3. `internal/llm/inuse.go` ModelSlots + BuildModelsInUse — adds slot values to
   the boot pre-warm set so local endpoints start before first use.

The existing `internal/llm/register_local_model.go` shows how slot values are
resolved to provider/model form. Bare alias names are legal for slots
(classifier_model uses them); alias expansion in inuse.go covers pre-warm.

Key files to understand before implementing:
- internal/config/config.go:350-365 - ModelsConfig slot fields
- internal/llm/providers.go:100-130 - ProvidersConfig slot fields
- internal/llm/providers.go:280-310 - overlay merge (applyOverlay)
- internal/llm/inuse.go:11-100 - ModelSlots and BuildModelsInUse
- internal/agent/spec.go:100-112 - AgentSpec per-agent model fields
  (Model, EnhancerModel, EscalationModel) — the block where the per-agent
  refusal_model field lands

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/config/config.go — ModelsConfig gains (after ExtractModel):
RefusalModel string `json:"refusal_model"` // global default refusal
// fallback: provider/id or alias name. Empty = no global default; a
// per-agent spec refusal_model overrides this.

// internal/llm/providers.go — ProvidersConfig gains the same field with
// the same comment; applyOverlay gains:
if overlay.RefusalModel != "" {
    out.RefusalModel = overlay.RefusalModel
}

// internal/agent/spec.go — AgentSpec gains, beside EscalationModel
// (spec.go:108-112):
RefusalModel string `json:"refusal_model,omitempty" yaml:"refusal_model,omitempty"`
// Per-agent refusal fallback (alias name or "provider/model" ref).
// Empty = inherit the global models.json5 refusal_model slot; both empty
// = refusal fallback disabled for this agent.

// internal/llm/inuse.go — ModelSlots gains RefusalModel string;
// BuildModelsInUse adds:
add(slots.RefusalModel)
// and the doc comment "five slot fields" becomes "six slot fields".
```

### What This Leaf Consumes

Nothing new.

## Tasks

### Task 1: ModelsConfig + ProvidersConfig + overlay merge

**Objective:** Declare the slot in both config structs and merge it in overlays.

**Files:**
- Modify: `internal/config/config.go` (~line 360, after ExtractModel)
- Modify: `internal/llm/providers.go` (~line 114, after ExtractModel;
  ~line 296-298 region, after the ExtractModel overlay block)
- Test: `internal/llm/providers_config_test.go` (create if absent; otherwise
  append to the existing providers config test file — check with search_files
  for `ExtractModel` in internal/llm/*_test.go and use that file)

**Step 1: Write failing test**

```go
func TestProvidersConfig_RefusalModelOverlay(t *testing.T) {
    base := []byte(`{"model": "a/b", "refusal_model": ""}`)
    overlay := []byte(`{"refusal_model": "ollama/dolphin-mixtral"}`)
    var b, o ProvidersConfig
    if err := json5.Unmarshal(base, &b); err != nil { t.Fatal(err) }
    if err := json5.Unmarshal(overlay, &o); err != nil { t.Fatal(err) }
    out := applyOverlayProviders(b, o) // use the REAL merge function name —
    // find it with search_files("overlay.ExtractModel", path="internal/llm/providers.go")
    if out.RefusalModel != "ollama/dolphin-mixtral" {
        t.Fatalf("overlay did not apply: %q", out.RefusalModel)
    }
}
```

NOTE: match the actual JSON parsing mechanism used by the existing config
tests (some use encoding/json, some a json5 shim). Read a neighboring test
first (search_files, not read_file) and copy its setup style.

**Step 2: Run to verify failure** (compile error: no RefusalModel field).

**Step 3: Implement** — the three edits above.

**Step 4: Run to verify pass** — `go test -p 2 ./internal/llm/ ./internal/config/ -short`

### Task 2: Boot pre-warm inclusion

**Objective:** A configured refusal model's local endpoint pre-warms at boot.

**Files:**
- Modify: `internal/llm/inuse.go` (ModelSlots struct + add() call + comment)
- Test: extend the existing BuildModelsInUse test file (search_files for
  `BuildModelsInUse` in internal/llm/*_test.go)

**Step 1: Write failing test**

Case: slots with only RefusalModel set to "mlx-local/uncensored-8b" and an
empty agents list => in-use set contains "mlx-local/uncensored-8b".

**Step 2: Run to verify failure.**

**Step 3: Implement** the add() call + struct field + comment bump.

**Step 4: Run to verify pass.**

### Task 3: Config round-trip plumbing check

**Objective:** The daemon's models-config load path carries the new field
end to end.

**Files:**
- Test: whichever existing test covers slot loading (search_files for
  `extract_model` across internal/config and internal/llm test files; mirror it)
- Modify: only if the load path filters fields explicitly (it should not —
  verify by test).

**Step 1: Write failing test** — a models.json5 string with
`refusal_model: "mlx-local/uncensored-8b"` parsed through the REAL load
function used by the daemon (find it via the extract_model test) yields
ProvidersConfig.RefusalModel == "mlx-local/uncensored-8b".

**Step 2: Run. Expect pass already** (struct-tag driven). If it passes,
keep the test as a regression pin — record "no code change needed" in the
report. If it FAILS, there is an explicit field filter; fix it.

**Step 3: Run full package tests.**

### Task 4: Per-agent spec field

**Objective:** AgentSpec gains the per-agent `refusal_model` field with
JSON5+YAML tags (employees are defined in JSON5; specs also round-trip
YAML in places — mirror EscalationModel's dual tags exactly).

**Files:**
- Modify: `internal/agent/spec.go` (~line 112, after EscalationModel)
- Test: extend the existing spec-parsing test (search_files for
  `escalation_model` in internal/agent/*_test.go)

**Step 1: Write failing test** — an employee JSON5 with
`refusal_model: "mlx-local/uncensored-8b"` parses into
spec.RefusalModel; a spec without the field leaves it empty; the field
marshals back out with the exact `refusal_model` key.

**Step 2: Run to verify failure.**

**Step 3: Implement** the field.

**Step 4: Run to verify pass** — `go test -p 2 ./internal/agent/ -short -run Spec`

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] gofmt clean; `go vet ./internal/llm/ ./internal/config/ ./internal/agent/` passes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Slot present in BOTH config structs with identical json tags and comments
- [ ] AgentSpec field present with json+yaml tags matching EscalationModel style
- [ ] Overlay merge present and tested
- [ ] Pre-warm inclusion present and tested
- [ ] Round-trip test present
- [ ] No changes to alias handling or resolver
- [ ] Code follows project conventions; no scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Do NOT resolve either value into a model config here — leaf 03 does that
  in the loop via Resolver.ResolveEscalationRef. This leaf is declaration
  + pre-warm only. Precedence (spec > global > off) is leaf 03's logic.
- GUI/TUI exposure of the slot is out of scope (config-file and employee
  JSON5 only for phase 1).
