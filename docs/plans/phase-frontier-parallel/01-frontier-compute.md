# leaf 01 — Pure Frontier Computation (phase_frontier.go)

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task in this document
using TDD. **Do NOT commit. Do NOT run `git add`.** Write changes, run
verification, report results. The orchestrator handles all git
operations.

- Parent: `docs/plans/phase-frontier-parallel/master.md` — read its
  Interface Contracts section (Contract A is frozen) before coding.
- Scope: `internal/agent/phase_frontier.go` (NEW) +
  `internal/agent/phase_frontier_test.go` (NEW). **NO wiring** — this
  leaf touches nothing else; the function must compile unused (Go
  permits unexported unused funcs; do NOT reference it from any
  existing file).
- Dependencies: none.
- Estimated context: ~60-90K.
- Test command: `go test -p 2 ./internal/agent/...` (ALWAYS `-p 2`:
  macOS ephemeral-port rule).

## Interface Contract (Contract A, frozen — implement verbatim)

```
File:        internal/agent/phase_frontier.go
Pure:        no I/O, no locks, no clocks — deterministic

type PhaseNode struct {
    Name           string
    Sequence       int
    Produces       []string
    Consumes       []Artifact // agent.Artifact = plan.Artifact (already aliased in this package)
    DependsOnPhase []string
}

func ComputePhaseFrontier(
    nodes []PhaseNode,
    availableArtifacts map[string]bool,
    busyPhases map[string]bool,
) (ready []PhaseNode, cycleDetected bool)
```

Semantics (all pinned by master.md; deviations go to OPEN-QUESTIONS.md):

1. **Edges.** Phase X depends on phase Y iff ANY of:
   - some consume `c` of X satisfies `c.Name ∈ Y.Produces` for some
     in-graph Y (REQUIRED and OPTIONAL consumes both create edges), and
     `Y != X` (a phase consuming its own artifact — self-produce — is
     NOT a self-edge);
   - `Y ∈ X.DependsOnPhase` and Y is in the graph.
   Consumes whose producer is NOT in the graph create no edge (the
   artifact-store gate at phase start still protects them — matching
   today's `checkPhaseReady` behavior where a never-produced required
   consume fails the gate, it doesn't deadlock the graph).
2. **Ready rule.** X is ready iff:
   - (a) every `c` in X.Consumes with `c.Required` has
     `availableArtifacts[c.Name] == true` (optional consumes never
     block), AND
   - (b) no edge from X into any `busyPhases[y] == true`.
3. **Ordering.** `ready` is sorted by `Sequence` ascending; ties
   (equal Sequence) fall back to `Name` ascending for determinism.
   Sequence is a TIEBREAK ONLY — it must never make an edge-ready phase
   wait behind a busier one.
4. **Cycle detection.** `cycleDetected == true` iff
   `len(ready) == 0 && len(nodes) > 0`. (nodes = incomplete set, so an
   empty ready set means the remaining graph cannot advance.) The
   function itself takes NO recovery action — that's the caller's
   (leaf 02's) job.
5. **Dup safety.** If `nodes` contains duplicate Names, keep the first
   occurrence and ignore later duplicates (log nothing — pure function).
6. **Empty inputs.** `len(nodes) == 0` → `nil, false`.

`checkPhaseReady` reference for gate parity (do NOT modify it):
internal/agent/strategic.go:1615-1625 — it errors when a Required
consume is missing from the store; optional consumes are best-effort.

## Tasks

### Task 1: RED — failing tests first

Create `internal/agent/phase_frontier_test.go` with a table-driven test
`TestComputePhaseFrontier` covering AT LEAST these cases (all in one
table; build fixtures with small helpers — no shared mutable globals):

| # | case | nodes | available | busy | expect ready (by Name) | cycle |
|---|------|-------|-----------|------|------------------------|-------|
| 1 | empty graph | — | — | — | none | false |
| 2 | all-ready chain (nothing busy, all artifacts present) | A,B,C linear | all | none | A,B,C | false |
| 3 | artifact edge blocks | A produces x; B requires x (required) | {} | A busy | B not ready; A ready if no other gating | false |
| 4 | optional consume never blocks | A produces x; B consumes x OPTIONAL | {} | A busy | A,B (B unblocked) | false |
| 5 | missing required consume gates even when idle | B requires x; no producer in graph | {} | none | none | false |
| 6 | missing required consume passes when present | B requires x; producer NOT in graph | {x} | none | B | false |
| 7 | optional-consume EDGE to busy phase still blocks | A produces x; B consumes x optional; busy(A) | {} | A busy | none (B) if B has no other passes | false |
| 8 | diamond | A→(B,C)→D | all produced, nothing busy | none | A,B,C,D | false |
| 9 | diamond mid-flight | same; busy(B) | {} | B | A,C (D blocked by B) | false |
| 10 | explicit DependsOnPhase edge | B DependsOnPhase=[A]; no artifacts at all | {} | A busy | none (B) | false |
| 11 | depends-on to UNKNOWN phase creates no edge | B DependsOnPhase=[Z]; Z not in nodes | {} | none | B | false |
| 12 | self-produce is not a self-edge | A produces a, consumes a (required), artifact present | {a} | A busy→ but A must still be READY: no self-edge | A | false |
| 13 | sequence tiebreak order | ready set {B(seq 2), A(seq 1)} | all | none | [A,B] in that order | false |
| 14 | sequence never defeats edge | B(seq 1) edge-blocked, C(seq 9) ready | per fixture | B's dep busy | [C] only | false |
| 15 | full stall = cycle | B requires x produced by A; busy(A); A requires y produced by B (2-cycle) | {} | A,B | none | true |
| 16 | duplicates ignored | same Name twice | — | — | single node | false |

Add focused subtests (separate test funcs, still table-driven):

- `TestComputePhaseFrontier_OutputOrdering` — verifies Sequence-asc
  with Name tiebreak exactly (case 13/14 assertions on full order,
  not set membership).
- `TestComputePhaseFrontier_Purity` — pass the same inputs twice;
  results deep-equal; input slices/maps unmodified (copy before
  compare).

Run: `go test -p 2 ./internal/agent/ -run ComputePhaseFrontier` —
MUST FAIL to compile (function doesn't exist yet). That's your RED.

### Task 2: GREEN — implement `internal/agent/phase_frontier.go`

Implement exactly per the contract. Suggested shape (~80-120 lines,
house style: slog not needed here — pure function, no logging; errors:
none expected, no error return per contract):

- index artifacts: `producerOf map[string]string` (artifact → phase
  name; first producer wins) and `producedInGraph map[string]bool`.
- build adjacency: `depsOf map[string]map[string]struct{}` (phase → set
  of in-graph phases it depends on) from consumes (skip
  `producerOf[c.Name] == X.Name`) + DependsOnPhase entries present in
  the graph.
- ready scan per the rule; sort with `sort.Slice` on (Sequence, Name).
- `cycleDetected` per rule 4.

Conventions (master.md): exported-free (package-private identifiers:
Identifiers are UNEXPORTED — `phaseNode` and `computePhaseFrontier` —
matching package-internal peers (`checkPhaseReady`, `artifactStore`,
`PlanPhaseSpec` is the one exception and it predates this plan). Leaf 02
consumes them directly from within the same `package agent`. The
capitalized names in master.md Contract A are presentation only.
Document this mapping in a file-top comment citing Contract A.

Zero new dependencies; imports limited to stdlib (`sort`).

### Task 3: verify

```bash
go build ./...
go vet ./internal/agent/
go test -p 2 ./internal/agent/ -run ComputePhaseFrontier -v
go test -p 2 ./internal/agent/          # full package — no regressions
grep -n "computePhaseFrontier" internal/agent/*.go | grep -v _test   # definition + zero callers outside tests
grep -rn "computePhaseFrontier\|phaseNode" internal/agent/orchestrator*.go internal/agent/tactical.go   # MUST be empty — no wiring
```

## Self-Verification Checklist

- [ ] `internal/agent/phase_frontier.go` is the ONLY new non-test file
- [ ] Function + type unexported (`computePhaseFrontier`, `phaseNode`),
      pure, no I/O/locks/clocks
- [ ] All 16 table cases + 2 focused subtests present and green
- [ ] Optional consumes: never gate (rule 2a) but DO create edges (rule 1)
- [ ] Self-produce creates no self-edge (case 12)
- [ ] ready sorted (Sequence, Name); edge-blocking never defeated by Sequence
- [ ] cycleDetected only when len(ready)==0 && len(nodes)>0
- [ ] Zero references from orchestrator*/tactical* — no wiring
- [ ] `go test -p 2 ./internal/agent/...` green; gofmt clean
- [ ] No debug artifacts, no TODOs, no placeholder values

## Review Checklist (for orchestrator)

- [ ] Contract A signature verbatim (modulo the pinned unexported names)
- [ ] Table fixtures read like the master contract's cases (1-16 mapped)
- [ ] No line-number corruption: files written from clean source, not
      from `read_file`'s `NNN|`-prefixed output. Rule: use
      search_files / `terminal cat` to re-check context before writing;
      NEVER pipe read_file output into write_file. Verify with
      `grep -rcE '^\s*[0-9]+\|' internal/agent/phase_frontier*.go` = 0.
- [ ] Purity test actually detects mutation (deliberately corrupting a
      copy fails it)
- [ ] No debug artifacts, no TODOs, no placeholder values

Do NOT commit. Do NOT run git add.
