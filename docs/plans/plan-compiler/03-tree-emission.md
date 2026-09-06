# 03-tree-emission.md — EmitTree: CompiledPlan → hierarchical plan tree (TDD leaf)

## Meta

- **Parent:** ../master.md
- **Scope:** Implement the pure emitter that renders a CompiledPlan as a
  hierarchical-planning-style tree (master.md + leaves), with the
  flat-vs-tree decision gate and a Go section-presence check that
  mirrors the skill's compliance scan.
- **Dependencies:** 02-compiler.md's CompiledPlan/PhaseSpec TYPES ONLY
  (signatures pinned in master.md Contract B — no code dependency; both
  leaves dispatch in parallel)
- **Estimated Context:** ~60K
- **Concurrency Group:** B

## Goal

`EmitTree(cp *CompiledPlan, opts TreeEmitOptions) (*EmittedTree, error)`
— render a compiled plan as the same master.md + numbered-leaf tree the
hierarchical-planning skill produces, so oversized phases become
constrained, single-agent-scoped leaf documents with clean segregation
lines instead of one fat flat step list. The user's requirement: agent
scope-of-work stays bounded per implementation agent, and the compiled
plan is expressible in the SAME tree/branch/leaf format already used
across this repo's plan trees (docs/plans/*/master.md pattern).

## Context

The hierarchical-planning skill (Hermes-side, NOT in this repo) defines
the required sections per orchestrator: `## Dispatch Protocol`,
`## Interface Contracts`, `## Child Index`, `## Review Checklist`,
`## Coding Conventions`, `## Completion Tracking Table`,
`## Integration Test Plan` — and per leaf: "Do NOT commit" + a
Self-Verification Checklist. This repo's docs/plans/phase-frontier-parallel/
is a worked example (master.md has all sections; see its Interface
Contracts + Child Index shape). The emitter reproduces that shape
deterministically from compiled data.

Key files to understand:
- docs/plans/phase-frontier-parallel/master.md — the house tree-root
  shape (read its section structure; do NOT copy its prose)
- internal/plan/artifacts.go — Artifact (produces/consumes become the
  tree's frozen Interface Contracts)
- internal/plan/compiler.go — CompiledPlan/PhaseSpec (leaf 02, parallel
  dispatch; signatures pinned in master.md Contract B)
- internal/plan/parser.go — regex/style conventions of the package

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
File: internal/plan/treeemit.go (package plan)

type TreeEmitOptions struct {
    MaxLeavesPerPhase int // default 3 when zero
    LeafCharBudget    int // default 14_000 when zero (~128K tokens of
                          // agent context worth of spec text)
}
type LeafFile struct{ Path, Content string }
type EmittedTree struct {
    Root   string      // master.md content
    Leaves []LeafFile  // e.g. "01-auth.md", "02-store.md"
}
func EmitTree(cp *CompiledPlan, opts TreeEmitOptions) (*EmittedTree, error)

Decision gate (exported for leaf 04 to reuse):
  func ShouldEmitTree(cp *CompiledPlan, opts TreeEmitOptions) bool
    true when: any phase has steps whose per-leaf share would exceed
    MaxLeavesPerPhase * steps-per-leaf sizing, OR any phase intent
    text exceeds LeafCharBudget. Flat (false) when total steps ≤ 6
    AND every phase fits.

Root content — sections in this order, ALL present:
  # <plan title> — Implementation Orchestrator
  ## Meta (Role: Root, Parent: none, Children: N, Scope)
  ## Goal  ← the plan's Goal prose
  ## Architecture ← per-phase intent paragraphs
  ## Interface Contracts (frozen)
     one "### Contract <letter>: <name>" per phase; body = that
     phase's produces (exposed) + consumes (consumed) as Go-ish
     comment blocks, matching the house style
  ## Child Document Index (table: #/Document/Type/Dependencies/
     Est. Context/Concurrency — concurrency groups computed from
     consumes edges via the same frontier logic as the compiler)
  ## Dispatch Protocol (per concurrency group; delegate_task goal +
     "Do NOT commit" + read-only exploration rules, matching the
     house template)
  ## Review Checklist (root)
  ## Coding Conventions
  ## Completion Tracking Table (all leaves PENDING)
  ## Integration Test Plan (from the plan's Notes/acceptance text)
  ## Notes
Leaf content — sections:
  # <leaf title> — Implementation Leaf
  ## Meta (Parent: master.md, Scope, Dependencies, Est. Context,
     Concurrency Group)
  ## Goal ← phase intent slice assigned to this leaf
  ## Interface Contracts (From Parent) ← copied from root contracts
  ## Tasks ← phase steps as bite-sized task skeletons
     (Objective/Files/verify-command per step; TDD steps rendered as
     guidance, not Go code — emitter does not invent code)
  ## Self-Verification Checklist (incl. the "DO NOT COMMIT" rule)
  ## Review Checklist (For Review Agent)
  ## Notes
Guarantees:
  - leaf Path values are "<NN>-<slug>.md", zero-padded, unique
  - steps are partitioned across leaves NEVER splitting a step
  - every consumes edge either intra-leaf or declared in the parent
    leaf's Dependencies
  - section presence: EmitTree output passes sectionPresenceCheck
    (below) for both root and every leaf
```

```
File: internal/plan/treeemit_test.go — includes:

// mirrors the Hermes skill's check_template_compliance.py section list
func sectionPresent(doc, section string) bool  // "## <name>" exact
TestEmitTree_RootHasRequiredSections:
  required = ["## Meta", "## Goal", "## Architecture",
    "## Interface Contracts", "## Child Document Index",
    "## Dispatch Protocol", "## Review Checklist",
    "## Coding Conventions", "## Completion Tracking Table",
    "## Integration Test Plan", "## Notes"]
TestEmitTree_LeafHasRequiredSections:
  required = ["## Meta", "## Goal", "## Interface Contracts",
    "## Tasks", "## Self-Verification Checklist",
    "## Review Checklist", "## Notes", "DO NOT COMMIT"]
```

### What This Leaf Consumes

```
From master.md Contract B (pinned types): CompiledPlan, PhaseSpec,
  StepSpec, Artifact — all package plan, available even though leaf 02
  is in flight (the types are defined in THIS leaf's sibling commit;
  if 02 has not landed when you finish, gate your build on the pinned
  struct definitions being present in compiler.go — they are Contract B
  frozen, so define nothing yourself)
From the skill shape: docs/plans/phase-frontier-parallel/master.md
  (section structure reference)
```

## Tasks

### Task 1: ShouldEmitTree gate

**Objective:** The flat-vs-tree decision, unit-tested.

**Files:**
- Create: `internal/plan/treeemit.go`
- Test: `internal/plan/treeemit_test.go`

**Step 1: Write failing test**

```go
func TestShouldEmitTree(t *testing.T) {
    small := &CompiledPlan{Phases: []PhaseSpec{{
        Name: "only", Steps: []StepSpec{
            {Description: "a"}, {Description: "b"},
        }}},
    } // 2 steps, 1 phase → flat
    require.False(t, ShouldEmitTree(small, TreeEmitOptions{}))

    big := &CompiledPlan{Phases: []PhaseSpec{{
        Name: "p1", Description: strings.Repeat("x", 20_000),
        Steps: []StepSpec{{Description: "a"}},
    }}}
    require.True(t, ShouldEmitTree(big, TreeEmitOptions{}))
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/plan/ -run TestShouldEmitTree -v`

**Step 3: Implement gate** → **Step 4: verify pass**

### Task 2: Root emitter

**Objective:** Full master.md rendering with all required sections.

**Files:**
- Modify: `internal/plan/treeemit.go`
- Test: `internal/plan/treeemit_test.go`

**Step 1: Write failing test**

TestEmitTree_RootHasRequiredSections over a 3-phase compiled fixture
(2 parallel producers, 1 consumer). Also assert: Child Index rows count
== leaf count; Interface Contracts count == phase count; concurrency
groups place the two producers in the same group and the consumer in a
later group.

**Step 2: Run to verify failure** → **Step 3: Implement** →
**Step 4: verify pass**

### Task 3: Leaf emitter + step partitioning

**Objective:** Leaves per phase; steps partitioned without splitting;
per-leaf required sections; "DO NOT COMMIT" present.

**Files:**
- Modify: `internal/plan/treeemit.go`
- Test: `internal/plan/treeemit_test.go`

**Step 1: Write failing tests**

- 4-step phase, MaxLeavesPerPhase=3 ⇒ 2 leaves, no step duplicated,
  union of leaf steps == phase steps, order preserved.
- LeafCharBudget forces a split of a long phase even below the leaf cap.
- Every leaf passes the leaf section-presence check + contains
  "DO NOT COMMIT".

**Step 2: verify fail** → **Step 3: implement** → **Step 4: verify pass**

### Task 4: Determinism + fixtures

**Objective:** Same input ⇒ byte-identical output; golden fixture.

**Files:**
- Test: `internal/plan/treeemit_test.go`

**Step 1: Write failing test**

Run EmitTree twice on a fixed fixture; assert equal bytes. Golden file
test: first render compared against an inline expected snippet (the
root's Meta + Child Index sections verbatim).

**Step 2: verify fail** → **Step 3: fix nondeterminism (map iteration
order etc.)** → **Step 4: verify pass**

Run: `go test -race -p 2 ./internal/plan/ -run EmitTree`
Expected: ALL PASS

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All four root sections-groups + leaf sections present per tests
- [ ] ShouldEmitTree gate matches Contract D (flat: ≤6 steps AND fits)
- [ ] Step partitioning never splits a step; union == phase steps
- [ ] Emitter is deterministic (double-render equality test)
- [ ] `go test -race -p 2 ./internal/plan/` green
- [ ] No I/O, no clocks; slug/sort all derived from input data
- [ ] Only treeemit.go + treeemit_test.go created

**DO NOT COMMIT.** The orchestrator handles git after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Section names EXACTLY match the hierarchical-planning skill list
      (compare against master.md Contract D — "Child Document Index"
      not "Child Index", etc.)
- [ ] Emitted root is dispatch-ready: an agent could follow the
      Dispatch Protocol without edits
- [ ] Concurrency grouping derives from consumes edges, not phase order
- [ ] race clean, table-driven tests, `-p 2`

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- check_template_compliance.py is Hermes-side; this leaf implements the
  section-presence check in Go (sectionPresent helper) so the guarantee
  is testable in-repo. Keep the section list as a package-level var so
  leaf 04's integration test can import it.
- The emitter NEVER invents implementation code: leaf Tasks sections
  render step descriptions + file hints as guidance. Code arrives when
  a leaf agent executes the tree.
- Tree emission is DETERMINISTIC given CompiledPlan — no timestamps in
  output (the "Meta → updated" fields come from leaf 04 at write time).
