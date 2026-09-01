# escalation_model Config Surface - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Add `escalation_model` to AgentSpec + AGENT.md frontmatter
  parsing + optional per-agent defaults + configuration docs.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Decision references:** D1, D2, D3, D14

## Goal

Every roster agent (and therefore every employee — employees are agents)
can declare one escalation model. AGENT.md frontmatter gains:

```yaml
escalation_model: smarter-alias     # alias name OR "provider/model" ref
```

Empty/absent = escalation disabled (today's behavior). The field parses
through the same path as the existing `model:` frontmatter key — wherever
that is read (spec loading from AGENT.md, see internal/agent/registry.go:836
"Model: prefer AGENT.md if set"), `escalation_model` reads alongside it.

## Context

**RESOLVED BY AUDIT (2026-09-01, read-only subagent against HEAD 90fd2afb):
there is NO dual-parser ambiguity — both packages are halves of one
pipeline.** `internal/agents` (models.go `AgentMetadata` + parser.go
`ParseAgentFile`/`ParseAgentText`/`parseMetadata`, yaml tags) IS the
production AGENT.md frontmatter parser; `internal/agent` (registry.go
`definitionToSpec` ~:906 / `mergeSpec` ~:836-848, plus
`verificationFromMetadata` verification_config.go:40) converts its output
into `AgentSpec`. `internal/agents` is alive (imported by
cmd/meept/runtime.go, internal/daemon/components.go, internal/agent/registry.go,
internal/agent/verification_config.go). The `escalation_model` key is
therefore added in BOTH halves — this is the sanctioned pattern, not a
third parsing path.

Key files:
- `internal/agents/models.go` — `AgentMetadata` with yaml tags; `Model`
  at ~:50, `EnhancerModel` at ~:55, `Verification *VerificationMetadata`
  at ~:105; `VerificationMetadata.MaxFixLoops` at ~:192. The
  escalation_model yaml field goes here beside EnhancerModel.
- `internal/agent/spec.go` — AgentSpec json/yaml tags; `Model` ~:98,
  `EnhancerModel` ~:101. The EscalationModel spec field goes here beside
  EnhancerModel; consider `EffectiveEscalationModel()` mirroring
  `EffectiveEnhancerModel()` (spec.go:189-194) if a default alias is
  wanted later.
- `internal/agent/registry.go` — TWO wiring sites: `definitionToSpec`
  (next to the Model/EnhancerModel conversions ~:906-907) for new
  agents, AND `mergeSpec` (~:836-848) for re-loads of existing specs.
  Missing either site means the field silently drops on AGENT.md
  re-load. NOTE: mergeSpec currently does not handle Verification or
  Gate at all (pre-existing gap) — escalation_model is verification-
  adjacent; the leaf may mirror the Verification treatment
  (definitionToSpec only + daemon config default) and must DOCUMENT the
  choice in Deviations.
- `internal/agents/parser.go` — `ParseAgentFile` :38 → `ParseAgentText`
  :62 → `parseMetadata` :68 (yaml.Unmarshal into AgentMetadata; rejects
  AGENT.md without `id:` at :73-75).
- `internal/config/schema.go:595-596` — daemon-side
  `Verification.MaxFixLoops` default 3 (:2228) — the daemon-config
  default pattern if a defaults block is added (D14).
- `docs/configuration/` — the agents/roster config reference page for
  Task 3.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/spec.go (modify AgentSpec)
// EscalationModel is the model (alias name or "provider/model" ref) used
// for the next fix attempt when verification exhausts max_fix_loops.
// Empty = escalation disabled; hook falls back to escalate-to-user.
EscalationModel string `json:"escalation_model,omitempty" yaml:"escalation_model,omitempty"`
```

Frontmatter key: `escalation_model`. Parsing accepts:
- an alias name (validated lazily at resolution time in leaf 04 — this
  leaf does NOT resolve),
- a `"provider/model"` string (no validation here),
- empty/absent (disabled).

### What This Leaf Consumes

Nothing new — reads existing frontmatter parsing paths.

## Tasks

### Task 1: AgentSpec field

**Objective:** Add the field with exact tag shape; zero behavior change.

**Files:**
- Modify: `internal/agent/spec.go` (AgentSpec struct, after EnhancerModel)
- Test: `internal/agent/spec_test.go` (create if absent; check what
  registry_test.go covers first)

**Step 1: Write failing test**

```go
func TestAgentSpec_EscalationModel_DefaultEmpty(t *testing.T) {
    spec := &AgentSpec{ID: "x", Enabled: true}
    if spec.EscalationModel != "" {
        t.Errorf("EscalationModel = %q, want empty (disabled)", spec.EscalationModel)
    }
}
```

**Step 2:** `go test ./internal/agent/ -run TestAgentSpec_EscalationModel -v` → FAIL (field missing).

**Step 3:** Add the field per the contract.

**Step 4:** test → PASS. `go build ./...` clean.

### Task 2: AGENT.md frontmatter parsing (BOTH pipeline halves)

**Objective:** `escalation_model:` in frontmatter populates the field;
absent key leaves it empty.

**Files:**
- Modify: `internal/agents/models.go` — `AgentMetadata` gains
  `EscalationModel string yaml:"escalation_model,omitempty"` beside
  EnhancerModel (~:55).
- Modify: `internal/agent/spec.go` — `AgentSpec` gains the twin field
  beside EnhancerModel (~:101).
- Modify: `internal/agent/registry.go` — wire BOTH conversion sites:
  `definitionToSpec` (~:906) AND `mergeSpec` (~:836-848). Missing
  mergeSpec loses the field on AGENT.md re-load. (Exception: if you
  follow the Verification treatment — definitionToSpec only — document
  that choice in Deviations per the Context note.)
- Test: `internal/agents/` parser test (frontmatter cases) +
  `internal/agent/registry_test.go` (spec conversion cases; follow the
  existing EnhancerModel test at registry_test.go:326 as the template).

**Step 1:** Failing tests: parser test — load a temp AGENT.md with
`escalation_model: my-alias` → AgentMetadata.EscalationModel ==
"my-alias"; without the key → empty. Conversion tests —
definitionToSpec maps the field to AgentSpec; mergeSpec preserves it on
re-load (or documents the Verification-treatment deviation).

**Step 2:** FAIL.

**Step 3:** Implement all four touch points (metadata field, spec field,
both conversion sites).

**Step 4:** PASS; full `go test ./internal/agents/... ./internal/agent/
-count=1` green.

### Task 3: Docs

**Objective:** Document the key.

**Files:**
- Modify: `docs/configuration/` agents reference page (locate the page
  documenting AGENT.md frontmatter via search_files).
- Modify: one roster AGENT.md is NOT changed — do not set
  `escalation_model` on any shipped agent (D14: config surface only).

**Verify:** the doc names both accepted forms (alias | provider/model),
states empty = disabled, and cross-references the verification fix-loop
trigger (max_fix_loops exhausted).

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Field tag exactly `json:"escalation_model,omitempty" yaml:"escalation_model,omitempty"`
- [ ] No shipped AGENT.md sets the field (D14)
- [ ] No behavior change anywhere (field is inert until leaf 02)
- [ ] gofmt clean; `go vet ./internal/agent/` clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; every test present and passing
- [ ] Contract shape matches parent master.md Contract 1 exactly
- [ ] Parsing lives beside the model/enhancer_model frontmatter keys (same loader, no second parser)
- [ ] Docs updated; no scope creep; no artifacts

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- AUDIT RESOLUTION (2026-09-01): the former "which parser wins"
  question is closed — internal/agents is the parser, internal/agent
  the converter; Task 2 wires all four touch points. If
  `internal/agents/models.go` grows new validation, keep it there (the
  parser owns validation; the converter owns defaults).
- The hook's TurnModification already has a `ModelOverride string` field
  (hooks.go:53) — the "no field" premise is wrong; leaf 04's
  ApplyEscalation SETS that existing field instead of extending the
  struct. This leaf must compile WITHOUT
  touching TurnModification's definition — keep the decision in
  `h.pendingEscalation` so leaf 04 has a clean seam.
- If the hook cannot reach the AgentSpec at its construction site
  without large plumbing changes, accept passing just the
  `EscalationModel` string into the constructor and record that in
  Deviations.
