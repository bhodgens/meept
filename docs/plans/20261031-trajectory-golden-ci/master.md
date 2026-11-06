# Golden-Path Trajectory Grading in CI - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 5 leaf documents (flat)
- **Scope:** Golden-path trajectory grading in CI (proposal 4): golden
  sequence storage + graded sequence comparator + CI consumption.

## Goal

Meept grades trajectory OUTCOMES today (internal/eval Judge(): a failed
oracle marks a step; a passed oracle marks a clean run) but has NO
golden-trajectory concept: nothing pins WHICH tool calls an agent made,
in what order, so a prompt or planner tweak that silently drops the
verify step, or reorders read-after-write, lands undetected. The bench
gate (meept-bench diff) sees only benchmark VERDICTS; the e2e-fast gate
sees only suite pass/fail.

This tree closes that gap: a graded tool-call sequence comparator
(ordered-subsequence tolerance scoring with configurable threshold plus
an exact-match mode), golden fixtures captured ONLY from FakeLLM-scripted
hermetic runs, a new hermetic e2e suite that replays a scripted scenario
and gates the observed sequence against its golden, and a regenerate
workflow for intentional behavior changes. The suite rides the existing
required `make e2e-fast` CI gate, so CI wiring is near-free.

Policy (pinned):
- Goldens NEVER derive from live-model runs. Live tier (e2e-chat) is
  never sequence-gated.
- The gate is BLOCKING within the new trajectory-golden suite. Suite-level
  flake attribution per repo rules (the suite, not the comparator, owns
  retry/attribution).
- Fixture churn rule: a golden diff failure = investigate-then-regenerate,
  NEVER blind-regen.
- No evolver trace stores in the CI path (opt-in-defaults invariant).

## Architecture

Deterministic substrate: e2e harness FakeLLM (e2e/harness/fakellm.go)
scripts executor turns as an ordered EnqueueToolCalls queue; the daemon
executes them; session_tool_calls (internal/session/store_sqlite.go)
records (tool_name, arguments, seq). The comparator package
(internal/eval/trajectorydiff) normalizes recorded calls (tool name +
argument SHAPE; per-tool argument rules; stable JSON key ordering) and
scores actual vs golden: EXACT, IN_ORDER (ordered subsequence), or
ANY_ORDER subsequence matching against a configurable pass threshold
(>= 1.0). A capture harness (leaf 02) turns a scripted run's recorded
calls into a committed golden JSON fixture. The e2e suite (leaf 03)
replays, grades, and fails RED on regression. Research (leaf R0)
informed the match-mode and tolerance design (tau-bench-style outcome +
process grading; ADK/agentevals match modes; ordered-subsequence
tolerance scoring).

## Interface Contracts

<!-- Each contract: ID + ONE signature block. Owning + consuming leaves.
     Leaves list consumed contract IDs + exact signatures only - never
     restate full definitions in a leaf; the parent holds them once. -->

### C1: NormalizedCall

```
package trajectorydiff // internal/eval/trajectorydiff

// NormalizedCall is one recorded tool call, normalized for comparison:
// tool name + argument SHAPE (per-tool argument rules applied; JSON
// object keys sorted; list order preserved; variable values such as
// generated IDs, timestamps, and paths under the temp workdir replaced
// per Rule). Equality of two NormalizedCall values is deterministic.
type NormalizedCall struct {
    Name       string         `json:"name"`
    ArgsShape  map[string]any `json:"args_shape"` // sorted-key canonical JSON
    ArgsRaw    string         `json:"args_raw"`   // original arguments, verbatim
    InstanceID string         `json:"instance_id"` // stable per-position ID (name#ordinal)
}
```
- Owner: 01-comparator.md
- Consumers: 02-fixture-capture.md, 03-e2e-suite.md

### C2: Comparison modes and result

```
package trajectorydiff

type MatchMode string
const (
    MatchExact    MatchMode = "exact"     // same length, position-wise equal
    MatchInOrder  MatchMode = "in_order"  // golden is an ordered subsequence of actual
    MatchAnyOrder MatchMode = "any_order" // golden multiset contained in actual multiset
)

type ArgRule string
const (
    ArgRuleExact   ArgRule = "exact"   // JSON-value equality
    ArgRuleSubset  ArgRule = "subset"  // expected keys/values contained in actual object
    ArgRuleIgnore  ArgRule = "ignore"  // key excluded from comparison
)

type Options struct {
    Mode          MatchMode          // default MatchInOrder
    Threshold     float64            // minimum score to pass; default 1.0
    ArgRules      map[string]ArgRule // per-tool-name argument rule override
    IgnoreCallSet map[string]bool    // tool names excluded entirely (e.g. brittle text tools)
}

type StepDiff struct {
    Golden  *NormalizedCall // nil when the step is extra (in actual only)
    Actual  *NormalizedCall // nil when the step is missing (in golden only)
    Matched bool
}

type Result struct {
    Passed    bool
    Score     float64    // length(golden LCS in-order matches)/length(golden); 1.0 for exact-equal
    Mode      MatchMode
    Steps     []StepDiff // stable, diagnostic ordering
    FirstDiff int        // 1-based golden index of first mismatch; 0 when Passed
    Summary   string     // one-line human-readable verdict
}

func Compare(golden, actual []NormalizedCall, opts Options) Result
```
- Owner: 01-comparator.md
- Consumers: 02-fixture-capture.md, 03-e2e-suite.md
- Scoring (frozen): Score = |LCS(golden, actual)| / len(golden) under
  in_order; for exact, 1.0 iff equal else length-ratio penalty; for
  any_order, matched-multiset/len(golden). Passed = Score >= Threshold.
  Float comparisons use epsilon (Pitfall 26); sort map keys before
  canonical JSON emission.

### C3: Normalizer

```
package trajectorydiff

// Normalize maps recorded session tool calls to comparable shape.
// - session.ToolCall: internal/session/store.go:29
//    (ID, MessageID, ToolName, ToolCallID, Arguments, Result, Seq)
// - eval.Step (internal/eval/record.go) name from Step.Name
// Variable-value scrubbing (Rule): replace values whose JSON key matches
// (case-insensitive) id|uuid|timestamp|time|path|token|session with the
// placeholder "<var>"; nested keys recurse. Lists keep order and element
// shapes. Unknown/duplicate keys: last-writer-wins on flat maps only.
func Normalize(name string, argumentsJSON string) (NormalizedCall, error)
func NormalizeSessionCalls(calls []session.ToolCall) ([]NormalizedCall, error)
func StepsFromGolden(calls []NormalizedCall) []eval.Step // for judge reuse
```
- Owner: 01-comparator.md
- Consumers: 02-fixture-capture.md, 03-e2e-suite.md

### C4: Golden fixture format

```
// e2e/fixtures/golden/<scenario>.golden.json
{
  "schema_version": 1,
  "scenario": "<suite scenario name>",
  "captured_from": "fakellm-scripted",  // MUST be this literal; live is invalid
  "match": {"mode": "in_order", "threshold": 1.0, "arg_rules": {}, "ignore_tools": []},
  "golden": [
    {"name": "read_file", "args_shape": {"path": "<var>", "op": "read"}}
  ]
}
// Owner: 02-fixture-capture.md (writer), 03-e2e-suite.md (reader)
// Consumers: 03-e2e-suite.md
```

### C5: Capture CLI

```
// e2e/harness/golden.go (package harness)
// CaptureGolden replays a scripted FakeLLM scenario against a scratch
// daemon, reads session_tool_calls (ORDER BY seq) for the assistant
// turn's message, normalizes via C3, and writes the C4 fixture.
// Refuses (error) if scenarioSource != "fakellm-scripted".
func CaptureGolden(t testing.TB, opts CaptureOptions) (wrotePath string, err error)
type CaptureOptions struct {
    ScenarioName string
    GoldenDir    string            // e2e/fixtures/golden
    Script       func(f *FakeLLM)  // exact FakeLLM script used by the suite
    Match        trajectorydiff.Options
}
// Makefile target (leaf 02):
//   make golden-regenerate SCENARIO=<name>   # regenerates ONE scenario
//   make golden-regenerate-all               # regenerates every fixture
// Each regen re-runs the suite first and prints the diff for
// investigate-then-regenerate; --force skips only that investigation print.
```
- Owner: 02-fixture-capture.md
- Consumers: 03-e2e-suite.md, 04-docs.md

### C6: Suite and manifest entry

```
e2e/suites/trajectory-golden/trajectory_golden_test.go  (//go:build e2e)
e2e/manifest.json: suites += {"name": "trajectory-golden",
  "dir": "e2e/suites/trajectory-golden", "status": "implemented"}
e2e/manifest.json: path_map += {"internal/eval/trajectorydiff/": ["trajectory-golden"]}
// Same commit as the package (pre-commit-e2e requires it).
// Gate: `make e2e-fast` already REQUIRED in ci.yml ci-success; the suite
// joins that aggregate gate with zero new CI wiring.
```
- Owner: 03-e2e-suite.md
- Consumers: (CI)

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 25K | A |
| 01 | 01-comparator.md | leaf | R0 | 60K | B |
| 02 | 02-fixture-capture.md | leaf | 01 | 55K | C |
| 03 | 03-e2e-suite.md | leaf | 01, 02 | 65K | D |
| 04 | 04-docs.md | leaf | 01, 02, 03 | 30K | E |

Same-letter children have no inter-dependencies; this tree is a single
chain, so dispatch is sequential (R0 first; 04 last). Estimated total
across leaves ~1-2k LOC, 2-3 days.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

This tree uses leaf-internal TDD instead of separate 00T- test-gate
leaves: every implementation leaf authors its own tests FIRST (RED) then
the implementation (GREEN), and the suite leaf (03) is itself the e2e
gate. Rationale: single-language Go tree, four cohesive seams, chain
dependencies leave no parallelism for gate leaves to protect. The Test
Gate matrix below is still validated by the control agent before 03
dispatches.

### Phase 1: Dispatch Concurrency Group [A] - R0 Research

Dispatch R0 via `delegate_task`: goal = "Complete the research tasks from
docs/plans/20261031-trajectory-golden-ci/R0-research.md"; context = full
leaf text + the pinned policy from this master. Research leaf returns a
digest, not code.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts (C1-C6), Review Checklist; run tests; no gate-test
diffs exist by construction (leaf-internal TDD). Pass: commit + status
REVIEWED. Gaps: re-dispatch with findings, max 3 cycles, then escalate.

Special review rule for 02/03 (fixture churn): if a golden diff fails
during leaf work, the leaf MUST report the investigation (what changed
and why) before regenerating; blind regeneration is automatic
NEEDS_FIXES.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts C1-C6 hold between leaves;
run `go test -p 2 ./internal/eval/... ` and `make e2e-fast` (or at minimum
`make e2e-fast-area AREA=trajectory-golden`) plus `make e2e-affected`;
run the line-number corruption check (`grep -rn '^\s*[0-9]\+|' internal/
eval/trajectorydiff e2e/suites/trajectory-golden` = zero matches); verify
manifest has 70 suites with trajectory-golden present. Gaps: re-dispatch
the failing child with contract fixes (uncommitted). Pass: normalize
formatting (gofmt), commit integration changes, set children COMPLETE.

### Pull ordering (single chain)

R0 -> 01 -> 02 -> 03 -> 04. 01 and 02 may overlap ONLY after 01's
contract tests pass and only if the orchestrator inlines 01's final
signatures into 02's dispatch context; default is strict sequence.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths; C1-C6)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed; RED reasons observed)
- [ ] Code follows project conventions (incl. no time.Now/math/rand IDs;
      epsilon float compares; no ignored errors)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Policy holds: no golden captured from a live run; no evolver trace
      store in the CI path; regenerate flow requires investigate-first
      output before writing

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go (repo toolchain per go.mod); ASCII only in
  all files including fixtures and docs.
- **Naming:** exported for C1-C6 API; unexported helpers; package
  trajectorydiff.
- **Imports:** stdlib grouping, no unused imports; no import cycles
  (trajectorydiff may import internal/eval and internal/session types
  ONLY; never internal/tools/builtin).
- **Error handling:** wrap with %w and package prefix "trajectorydiff:";
  no ignored errors; no bare panic(err).
- **IDs:** never time.Now().UnixNano() or math/rand; pkg/id.Generate()
  fallback semantics apply if any ID is generated (none expected).
- **Testing:** tests-first inside each leaf; table-driven where natural;
  new feature tests live in the hermetic e2e tier (suite 03); comparator
  unit tests live beside the package as its TDD vehicle (the package is
  test infrastructure for the e2e tier, and its unit tests are the
  existing-pattern exception mirroring internal/eval judge_test.go).
- **Determinism:** epsilon compare floats; sort map keys before canonical
  JSON; no wall-clock or randomness in outputs.
- **macOS sweeps:** `go test -p 2` ALWAYS for ./... sweeps (ephemeral
  port exhaustion); suite verification uses `make e2e-fast-area
  AREA=trajectory-golden`.
- **Formatting:** gofmt before reporting.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | golden sequence storage | C4 fixture format; 02-fixture-capture.md T1-T3 | unit test round-trip; fixture file exists |
| R2 | graded sequence comparator | C2/C3; 01-comparator.md T1-T5 | go test ./internal/eval/trajectorydiff |
| R3 | CI consumption | C6; 03-e2e-suite.md T1-T3 | make e2e-fast-area AREA=trajectory-golden; manifest entry |
| R4 | goldens never derive from live-model runs | C4 captured_from literal; 02 T2; policy in Goal | review checklist; capture refuses non-scripted source |
| R5 | live tier never sequence-gated | policy in Goal; 03 scope excludes e2e-chat | review checklist |
| R6 | blocking vs advisory decision for first landing | Goal policy: blocking within the suite; suite-level flake attribution | 03 suite fail-fast behavior; review checklist |
| R7 | explicit regenerate workflow for intentional behavior changes | C5 make golden-regenerate; 02 T4 | make -n golden-regenerate SCENARIO=x; docs |
| R8 | fixture churn: investigate-then-regenerate, never blind-regen | C5 regen runs suite + prints diff first; Phase 2 review rule | leaf 02 report; review checklist |
| R9 | research grounding for match modes/tolerance | R0-research.md T1-T2 | R0 digest |
| R10 | docs per feature-docs rule (internal/<pkg> -> docs/workflows/<pkg>.md) | 04-docs.md T1-T2 | docs/workflows/trajectory-grading.md exists, cross-refs resolve |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research | COMPLETE | 1 | landed c5ef67e9 (R0-findings.md, reviewed) |
| 01-comparator | COMPLETE | 1 | landed e3b2130b (internal/eval/trajectorydiff + unit tests) |
| 02-fixture-capture | COMPLETE | 1 | landed 92fa3350 (issues 83, 88) |
| 03-e2e-suite | COMPLETE | 1 | landed 9c142af1 (issue 83) |
| 04-docs | COMPLETE | 1 | docs/workflows/trajectory-grading.md landed 4d8179cf; cross-refs resolve |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: 02 does not dispatch until 01 reaches REVIEWED; 03 does not
dispatch until 01 AND 02 reach REVIEWED; 04 dispatches last.

## Integration Test Plan

1. `go test -p 2 ./internal/eval/trajectorydiff/...` - comparator unit
   tests GREEN (modes, threshold, normalization rules, epsilon).
2. `make e2e-fast-area AREA=trajectory-golden` - new suite GREEN:
   replay -> normalize -> compare vs golden -> pass.
3. Deliberate-regression drill (orchestrator-run, then revert): edit the
   suite's scripted tool order, confirm the suite fails RED with a
   StepDiff-style message, restore, confirm GREEN. This proves the gate
   is non-decorative.
4. `make e2e-affected` with internal/eval/trajectorydiff/ staged -
   trajectory-golden appears in the affected set.
5. `python3 -c "import json; m=json.load(open('e2e/manifest.json')); assert any(s['name']=='trajectory-golden' for s in m['suites'])"` - manifest entry present.
6. `go build ./... && go vet ./internal/eval/trajectorydiff/...` - clean.
7. Cross-boundary: judge reuse - StepsFromGolden(C1 calls) feeds
   internal/eval Judge() in the suite without import cycle.

## Test Gate

N/A as separate 00T- leaves - leaf-internal TDD tree (rationale in
Dispatch Protocol Phase 0). Substitute verification matrix, validated
in-session by the control agent before leaf 03 dispatches:

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | normalize: shape + var scrub + sorted keys | 01 T1/T2, C3 | TestNormalize* | trajectory-golden suite (implicit) | VALIDATED |
| F2 | compare: exact / in_order / any_order + threshold + epsilon | 01 T3/T4, C2 | TestCompare* | suite fail-drill | VALIDATED |
| F3 | capture: scripted run -> golden fixture file | 02 T1/T2, C5 | TestCaptureGolden* | suite regenerate path | VALIDATED |
| F4 | regenerate target: suite-first + diff print | 02 T4, C5, R8 | n/a (make target) | make -n / dry run | VALIDATED |
| F5 | gate: replay suite fails on sequence regression | 03 T1/T2, R6 | n/a | TestTrajectoryGolden_* RED drill | VALIDATED |
| F6 | manifest + path_map entry, same commit | 03 T3, C6 | n/a | manifest assert | VALIDATED |
| F7 | docs workflow page | 04 T1, R10 | n/a | cross-ref resolve | VALIDATED |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Open Questions

None blocking. Pre-authoring decisions already made (per approved scope):
blocking gate within the suite (R6); in_order default mode with exact
available (C2); goldens only from FakeLLM-scripted runs (R4). If R0
research surfaces a materially better tolerance scheme (e.g. weighted
step classes), the orchestrator may amend C2 BEFORE 01 dispatches as a
dated addenda to the frozen leaf - never mid-execution after.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- Verified anchors (grep-confirmed at authoring time):
  internal/eval/judge.go:152 Judge(); TrajectoryJudgment :50;
  DiskStore.SaveJudgment :214 / LoadJudgment :240;
  internal/eval/record.go:39 Attempt{ModelID,Passed,Oracle,TrajectoryID};
  internal/session/store.go:29 ToolCall struct;
  internal/session/store_sqlite.go:206-219 session_tool_calls schema
  (UNIQUE(message_id, seq)); GetToolCalls :2271 ORDER BY seq;
  e2e/harness/fakellm.go:349 NewFakeLLM, :379 EnqueueToolCalls, :66
  ToolCall{Name,Arguments}; e2e/harness/tasksdb.go read-only DSN pattern;
  e2e/manifest.json 69 suites + path_map["internal/eval/"]=["eval-judge"];
  Makefile:1575 e2e-fast (CI gate member); scripts/e2e-affected.sh
  path_map -> suite mapping.
- No golden-trajectory concept exists in the repo today (grep golden =
  TUI snapshots + gbnf grammar goldens only) - this tree introduces it.
- Do NOT pull evolver trace stores into this path (opt-in-defaults
  invariant, docs/agents/opt-in-defaults.md).
- macOS full sweeps MUST use -p 2 (ephemeral port exhaustion).
- ASCII only: fixtures, docs, and code. The regeneration drill in the
  Integration Test Plan is destructive-but-revertible; run it on a clean
  tree and restore immediately.
- Forest closeout 2026-11-05: tables reconciled against HEAD by read-only audit; all leaves verified landed + gates green.
