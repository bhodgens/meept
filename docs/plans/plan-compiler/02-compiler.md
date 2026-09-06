# 02-compiler.md — CompileSealed: sealed markdown → PlanPhaseSpec (TDD leaf)

## Meta

- **Parent:** ../master.md
- **Scope:** Implement the pure Go compiler for plan-dialect v1 with full
  problem reporting, depends_on inference, and cycle detection.
- **Dependencies:** 01-dialect-spec.md (the grammar is its spec; implement
  from Contract B + the dialect doc, which is dispatched in parallel —
  its content is INLINED in dispatch context)
- **Estimated Context:** ~70K
- **Concurrency Group:** B

## Goal

`CompileSealed(markdown string, maxPhases int) (*CompiledPlan, error)` —
a pure function that turns a sealed dialect-v1 document into
`[]PlanPhaseSpec` plus the input's sha256, collecting ALL problems
instead of failing on the first. This is the deterministic replacement
for `parsePhaseOutput`' LLM-JSON path: no ExtractJSON, no repair passes,
no unmarshal guessing. Compile errors are line-numbered, human-readable
plan problems that feed the next brainstorm round.

## Context

The compiler lives in package `plan` (internal/plan/) beside parser.go —
the existing persisted-format parser, whose regex state-machine style is
the house pattern for line-oriented markdown parsing. PlanPhaseSpec
(internal/agent/strategic.go:70) is aliased into plan via the existing
pattern (agent references plan.Artifact through a type alias;
PlanPhaseSpec itself is defined in the agent package — the compiler
returns the plan-package shape and leaf 04 converts, OR the compiler
defines its own output struct that leaf 04 maps; see Contract B note).

CRITICAL import-cycle note: PlanPhaseSpec is in package agent. Package
plan CANNOT import agent (agent imports plan). Therefore the compiler
defines:

```go
// PhaseSpec mirrors agent.PlanPhaseSpec 1:1 (same JSON tags). Leaf 04
// converts via a trivial map; a round-trip test pins field parity.
type PhaseSpec struct {
    Name        string     `json:"name"`
    Description string     `json:"description"`
    Steps       []StepSpec `json:"steps"`
    Produces    []Artifact `json:"produces"`
    Consumes    []Artifact `json:"consumes"`
    DependsOn   []int      `json:"depends_on,omitempty"`
}
type StepSpec struct {
    Description string `json:"description"`
    ToolHint    string `json:"tool_hint,omitempty"`
    DependsOn   []int  `json:"depends_on,omitempty"`
}
```

Key files to understand before implementing:
- internal/plan/parser.go — house regex state-machine style; artifact
  and phase parsing precedents; bufio scanner with 1MB buffer
- internal/plan/artifacts.go — Artifact struct + IsValidKind
- internal/plan/plan.go — PhaseState constants for mapping
- docs/workflows/plan-dialect.md — THE normative grammar (inlined at
  dispatch; if any conflict with Contract B, the dialect doc wins and
  the deviation is reported)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
File: internal/plan/compiler.go (package plan)

func CompileSealed(markdown string, maxPhases int) (*CompiledPlan, error)
type CompiledPlan struct {
    Phases   []PhaseSpec
    Hash     string   // sha256 hex of the input markdown, verbatim
    Warnings []string // e.g. "phase 2 depends_on inferred from consumes"
}
type CompileError struct{ Problems []CompileProblem }
func (e *CompileError) Error() string // "plan compile failed: N problems"
type CompileProblem struct{ Line int; Message string }

Validation (each ⇒ one CompileProblem; ALL collected):
  - title missing / Meta missing or version != 1
  - Open Questions present with ≥1 entry (seal gate)
  - phase count > maxPhases (>0 check only when maxPhases > 0)
  - empty phase (no steps, no produces, no prose intent)
  - unknown artifact kind (anything outside IsValidKind's set)
  - duplicate artifact name across all produces
  - consumes name with no matching earlier produces
  - consumes referencing the SAME phase's produces (self-consume)
  - step needs: unknown artifact or unknown PhaseN.S ref;
    PhaseN.S ref pointing to a LATER phase
  - unknown tool_hint (set: code, refactor, debug, fix, analyze,
    research, git, plan, chat, bash)
  - phase dependency cycle (computed over the artifact edges)
Behavior:
  - DependsOn inferred from consumes when section omits "Depends on:"
    (adds a Warning per inferred edge)
  - steps within a phase: DependsOn built from needs refs that name
    steps (S refs), sequential-by-default numbering preserved
  - Produces[i].Required = true when a later phase consumes it;
    Required=false otherwise (matches Artifact semantics)
  - Line numbers in problems are 1-based input lines
```

### What This Leaf Consumes

```
From 01-dialect-spec.md: the grammar + exact problem-message strings
  (the spec's error-class section defines them; copy verbatim).
From repo: internal/plan/artifacts.go (Artifact), parser.go style.
```

## Tasks

### Task 1: Skeleton + Meta/Goal/Open-Questions parsing

**Objective:** Parse the document envelope; reject version/gate problems.

**Files:**
- Create: `internal/plan/compiler.go`
- Test: `internal/plan/compiler_test.go`

**Step 1: Write failing test**

```go
func TestCompileSealed_OpenQuestionsBlock(t *testing.T) {
    md := "# Plan: X\n\n## Meta\n\n- version: 1\n\n## Goal\n\ndo it\n\n" +
        "## Open Questions\n\n- what size?\n\n## Phases\n"
    _, err := CompileSealed(md, 5)
    var ce *CompileError
    require.ErrorAs(t, err, &ce)
    require.Len(t, ce.Problems, 1)
    require.Contains(t, ce.Problems[0].Message, "open question")
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/plan/ -run TestCompileSealed_OpenQuestionsBlock -v`
Expected: FAIL (CompileSealed undefined)

**Step 3: Write minimal implementation**

Envelope parsing: title regex, `## Meta` block (key: value lines),
Goal prose capture, Open Questions bullet capture. Collect problems,
don't return early.

**Step 4: Run test to verify pass**

Run: `go test ./internal/plan/ -run TestCompileSealed_OpenQuestionsBlock -v`
Expected: PASS

### Task 2: Phase + artifact parsing with all validations

**Objective:** Parse `### Phase N:` sections, produces/consumes/steps;
run every artifact/step validation; collect all problems.

**Files:**
- Modify: `internal/plan/compiler.go`
- Test: `internal/plan/compiler_test.go`

**Step 1: Write failing tests** (table-driven)

Cases (each its own subtest, each expecting the dialect doc's exact
message): unknown kind; duplicate artifact; consume-before-produce;
self-consume; unknown needs artifact; needs to later phase; unknown
tool hint; >maxPhases; empty phase.

**Step 2: Run to verify failure**

Run: `go test ./internal/plan/ -run TestCompileSealed -v`
Expected: new subtests FAIL

**Step 3: Implement**

State machine over lines, mirroring parser.go's scanner style. Phase
sections accumulate produces/consumes/steps; artifact tables built per
phase; needs-resolution and kind/name checks after full parse (so
forward references within the doc are visible); ALL problems collected.

**Step 4: Run to verify pass**

Run: `go test ./internal/plan/ -run TestCompileSealed -v`
Expected: PASS

### Task 3: DependsOn inference + cycle detection + hash

**Objective:** Derive phase deps from consumes; detect cycles; return
sha256; emit warnings.

**Files:**
- Modify: `internal/plan/compiler.go`
- Test: `internal/plan/compiler_test.go`

**Step 1: Write failing tests**

- Inference: phase omitting "Depends on:" gets DependsOn from consumes'
  producer phases; Warning emitted per inference.
- Explicit "Depends on: Phases 0, 1" parsed and validated (unknown
  phase index, self-dep, later-dep ⇒ problems).
- Cycle: A consumes b from B, B consumes a from A ⇒ cycle problem.
- Hash: CompiledPlan.Hash equals sha256 of input (precomputed literal).

**Step 2: Run to verify failure**

Run: `go test ./internal/plan/ -run 'TestCompileSealed_(Infer|Cycle|Hash)' -v`

**Step 3: Implement**

DFS cycle check over the produces/consumes edges. Hash via
crypto/sha256 on the raw input string.

**Step 4: Run to verify pass**

Run: `go test ./internal/plan/ -run TestCompileSealed -v`
Expected: ALL PASS

### Task 4: Golden round-trip vs spec examples

**Objective:** The three complete examples from docs/workflows/
plan-dialect.md compile cleanly; the compile-error example produces
exactly the spec'd problems.

**Files:**
- Test: `internal/plan/compiler_test.go`

**Step 1: Write failing test**

Embed the three examples as testdata string constants (copied verbatim
from the dialect doc at implementation time) and assert: minimal →
2 phases, expected artifact Required flags; parallel → both foundation
consumers get DependsOn [0]; error example → problem count + lines match.

**Step 2: Run to verify failure** → **Step 3: fix spec drift if any**
(drift found ⇒ REPORT it; do not silently bend the compiler) →
**Step 4: verify pass**

Run: `go test -race -p 2 ./internal/plan/ -run TestCompileSealed`
Expected: ALL PASS under race

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All validations from Contract B present, all problems collected (no early return)
- [ ] DependsOn inference + warnings; cycle detection; sha256 hash
- [ ] PhaseSpec mirrors agent.PlanPhaseSpec JSON tags 1:1
- [ ] Golden tests embed the dialect doc's examples verbatim
- [ ] `go test -race -p 2 ./internal/plan/` green
- [ ] No I/O, no clocks, no globals in compiler.go
- [ ] No changes to parser.go, strategic.go, or any existing file
      besides creating compiler.go + compiler_test.go

**DO NOT COMMIT.** The orchestrator handles git after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] CompileError.Error() aggregates count; problems carry 1-based lines
- [ ] Every error-class message matches the dialect doc verbatim
- [ ] Table-driven tests; race clean; `-p 2` used
- [ ] Pure function: grep compiler.go for os./time./http. ⇒ zero hits
- [ ] Existing plan package tests still green (parser untouched)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Problem messages come from the dialect doc's error-class section —
  the compiler is a slave to that spec. If the spec is ambiguous,
  report it; do not invent wording.
- The `bash` tool hint exists in the live planner vocabulary (observed
  in daemon logs 2026-09-06) even though decompose.md omits it — the
  dialect doc's hint set is authoritative.
- CompileSealed does NOT touch task state, stores, or the bus. Wiring
  is leaf 04's job entirely.
