# 01-dialect-spec.md — Plan Dialect v1 Specification (docs leaf)

## Meta

- **Parent:** ../master.md
- **Scope:** Author the normative plan-dialect v1 spec + the agent-facing
  draft template. Two markdown files, no Go code.
- **Dependencies:** none
- **Estimated Context:** ~40K
- **Concurrency Group:** A

## Goal

Produce the two documents that everything else in this tree consumes:
`docs/workflows/plan-dialect.md` (normative spec, human + compiler
audience) and `config/prompts/planner/plan_draft.md` (the instruction
template the planner agent renders when running a brainstorm session).

The dialect is markdown-only. It borrows the Codex ExecPlan living-document
shape (Goal / Decisions / Open Questions / prose phase sections) and maps
onto meept's persisted plan format (internal/plan/parser.go, writer.go).
It must contain NO JSON and NO numeric cross-phase indices — named
artifacts only. These two properties are the whole point: they remove the
LLM-JSON failure class observed live (2026-09-06: `cannot unmarshal
string into depends_on.0`, `no JSON found in phase planner output`).

## Context

meept today: `spec_plan` mode prompts the LLM for strict JSON
(config/prompts/planner/decompose_spec.md), then repairs it in
`parsePhaseOutput` (internal/agent/strategic.go:1527). The persisted plan
format is the markdown in internal/plan/writer.go (`## Phase N: name
[state]`, `N. step [status] (depends: M)`), parsed by parser.go's regex
state machine. The dialect SUPERSETS that format so the sealed draft
round-trips through existing persistence.

Key existing files to understand:
- internal/plan/parser.go — the persisted-format parser (what the sealed
  draft must remain compatible with at the phase/step level)
- internal/plan/writer.go — how plans render to markdown today
- internal/plan/artifacts.go — Artifact{Name,Kind,Description,Required};
  kind ∈ {file, interface, schema, decision, test_suite}
- internal/agent/strategic.go:56-84 — plannerStep / PlanPhaseSpec / the
  JSON envelope being replaced
- config/prompts/planner/interview.md — the interview prompt this
  dialect supersedes when plan_compiler_enabled

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
File: docs/workflows/plan-dialect.md
  - Normative dialect v1: every section, field, regex-level grammar,
    artifact naming rule, error class, and example. Must be complete
    enough that leaf 02 implements the compiler WITHOUT asking questions.
  - MUST contain, verbatim, the rules block from master.md Contract A
    (artifact kebab-case uniqueness, consume-before-produce check,
    Open-Questions seal gate, tool_hint enum, no-JSON rule).
  - MUST define the mapping to the persisted format: `### Phase N:
    <name>` → `## Phase N: name [pending]`; numbered steps →
    `N. step [pending] (depends: …)`; produces/consumes blocks →
    writer.go's Produces/Consumes artifact blocks.
  - MUST include three complete examples: minimal (2 phases, 3 steps),
    parallel (2 phases with a shared foundation, wave-style), and
    compile-error (unknown consume + open question non-empty).

File: config/prompts/planner/plan_draft.md
  - Agent-facing template: instructions for the planner agent running a
    brainstorm turn. Frontmatter `name: planner.plan_draft`.
  - Behavior spec inside the template: on first turn, scaffold the
    dialect skeleton with Goal + Open Questions from the request; on
    later turns, apply the user's requested edits to the draft and
    re-render the FULL document (no diffs); when Open Questions is
    empty and the user confirms, tell them to run `meept plan seal`.
  - MUST instruct the agent to keep decisions in `## Decisions` with
    rationale, and to ask at most 3 questions per turn.
```

### What This Leaf Consumes

```
From master.md: Contract A (the rules block), the Codex ExecPlan
  reference summary in the Goal section.
From repo: the four files listed under Context (read-only).
```

## Tasks

### Task 1: Write docs/workflows/plan-dialect.md

**Objective:** The normative dialect spec per Contract A.

**Files:**
- Create: `docs/workflows/plan-dialect.md`

**Step 1: Confirm old state**

Run: `ls docs/workflows/plan-dialect.md`
Expected: does not exist.

**Step 2: Write the spec**

Structure (required sections):
1. Overview — what the dialect is, the no-JSON/no-indices rules, who
   consumes it (planner agent writes, user edits, compiler reads).
2. Grammar — per-section rules at regex precision (mirror master.md
   Contract A exactly; expand with line-level examples).
3. Artifact rules — naming, kinds, uniqueness, consume/produce
   ordering, Required-when-consumed semantics.
4. Steps + needs references — `needs:` values are artifact names or
   `PhaseN.S<step#>` step refs; cross-phase step refs allowed only to
   EARLIER phases.
5. Seal gate — Open Questions empty; what sealing does (hash + compile).
6. Mapping to persisted format — the round-trip table.
7. Compile error classes — each with example input and the exact
   problem message the compiler should emit (leaf 02 implements these).
8. Examples — minimal / parallel / compile-error, complete documents.
9. Versioning — `## Meta` has `version: 1`; unknown major versions
   refuse to compile.

**Step 3: Verify cross-references resolve**

- Every rule mentioned in master.md Contract A appears in the spec.
- Every error class in section 7 has an example in section 8.
- The three examples parse by hand against the grammar (walk each line).

### Task 2: Write config/prompts/planner/plan_draft.md

**Objective:** The agent-facing brainstorm template per Contract A.

**Files:**
- Create: `config/prompts/planner/plan_draft.md`

**Step 1: Confirm old state**

Run: `ls config/prompts/planner/plan_draft.md`
Expected: does not exist. Note the existing interview.md for tone.

**Step 2: Write the template**

- Frontmatter: name/description matching the other planner prompts.
- Turn protocol: scaffold vs edit vs ready-to-seal (see Contract A).
- The full dialect skeleton inlined as the scaffold.
- Hard rules: never emit JSON; never use numeric phase references;
  keep answers to ≤3 questions per turn; re-render the whole document
  each turn (the draft is the artifact of record).

**Step 3: Verify**

- Template renders the skeleton that matches the spec's grammar.
- No JSON example anywhere in the file (grep for `{` outside the
  skeleton's markdown fences returns nothing).

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] docs/workflows/plan-dialect.md contains every Contract A rule verbatim
- [ ] Mapping table to parser.go/writer.go format present and correct
- [ ] Three complete examples present and grammar-consistent
- [ ] plan_draft.md template present with turn protocol
- [ ] Zero JSON syntax in either file outside illustrative prose
- [ ] No numeric cross-phase indices anywhere (PhaseN.S refs allowed)

**DO NOT COMMIT.** The orchestrator handles git after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Spec is complete enough to implement the compiler with zero questions
- [ ] Grammar rules are line-precision (regex-level where applicable)
- [ ] Error-class messages are concrete strings leaf 02 can reuse
- [ ] Template's scaffold matches the spec grammar exactly
- [ ] Docs follow repo conventions (docs/workflows/ layout, lowercase UI strings)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The Codex ExecPlan reference: living document, Decision Log, prose-first,
  human verifies approach before execution. Borrow the SHAPE (sections),
  not the fenced-envelope rule — meept drafts live on task metadata and
  render via the CLI, not as single fenced blocks.
- GSD wave structure: the dialect expresses waves implicitly via
  consumes (two phases consuming the same foundation artifact can run
  in parallel — the orchestrator's frontier computes this). Do NOT add
  explicit `wave:` fields; leaf 02's compiler + the existing frontier
  derive everything.
