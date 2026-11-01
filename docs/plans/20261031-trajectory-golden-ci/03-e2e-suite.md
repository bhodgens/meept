# Trajectory-Golden E2E Suite + Manifest - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** e2e/suites/trajectory-golden hermetic suite gating a
  scripted run's tool sequence against its golden fixture + manifest
  entry in the same commit.
- **Dependencies:** 01-comparator.md (REVIEWED), 02-fixture-capture.md
  (REVIEWED)
- **Estimated Context:** 65K
- **Concurrency Group:** D
- **Audit references:** master C6; Requirements R3, R5, R6

## Goal

Close the loop: a hermetic e2e suite that scripts a FakeLLM scenario,
replays it through a real scratch daemon, reads the recorded sequence,
grades it against e2e/fixtures/golden/<scenario>.golden.json using
trajectorydiff, and FAILS the suite on regression (blocking within the
suite - the first landing decision). Registering the suite + path_map in
e2e/manifest.json makes it reachable by `make e2e-affected` and a member
of the already-required `make e2e-fast` CI gate with zero new CI wiring.

## Context

- e2e/suites/eval-judge/eval-judge_test.go - existing eval suite; the
  package-name and build-tag pattern to mirror (`//go:build e2e`,
  package <suitename> with dashes removed).
- e2e/manifest.json - 69 suites; path_map currently has
  "internal/eval/": ["eval-judge"]. Add BOTH a suites entry and a
  path_map entry for the new package.
- e2e/harness/fakellm.go - scripted substrate; e2e/harness/golden.go -
  capture harness from leaf 02 (reuse its script-shape conventions so a
  regenerated fixture replays identically).
- Makefile:1575 e2e-fast; scripts/e2e-affected.sh maps path_map ->
  suites - do NOT modify either; manifest entry is sufficient.
- Policy: blocking within this suite; suite-level flake attribution
  (the suite is hermetic/FakeLLM-scripted, so flake here means a real
  harness bug - report it, do not add retry loops).
- Live tier (e2e-chat) is NEVER sequence-gated - this leaf must not
  touch it.

## Verified Anchors

- [e2e/suites/eval-judge/eval-judge_test.go:1-11] - build tag + package
  + doc-comment pattern for suites
- [e2e/manifest.json] - suites[] + path_map shape; insert target
- [e2e/harness/fakellm.go:66-69, 349, 379] - ToolCall, NewFakeLLM,
  EnqueueToolCalls
- [e2e/harness/golden.go] - CaptureOptions/CaptureGolden (leaf 02
  output) - script reuse for identical replay
- [Makefile:1575] - e2e-fast target the suite joins

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/trajectory-golden/trajectory_golden_test.go  (//go:build e2e)
// TestTrajectoryGolden_GoldenPathSequencePasses
// TestTrajectoryGolden_SequenceRegressionFails
//   (in-test negative drill: same script with one tool reordered/removed
//    graded against the committed fixture must FAIL - proves the gate
//    bites without a second fixture)
e2e/manifest.json:
  suites += {"name":"trajectory-golden","dir":"e2e/suites/trajectory-golden","status":"implemented"}
  path_map += {"internal/eval/trajectorydiff/":["trajectory-golden"]}
```

### What This Leaf Consumes

- C1 NormalizedCall / C2 Compare+Options / C3 NormalizeSessionCalls -
  `trajectorydiff.Compare(golden, actual []NormalizedCall, opts Options) Result`
  - owning leaf 01-comparator.md
- C4 fixture format (reader) - owning leaf 02-fixture-capture.md
- C5 CaptureOptions script-shape conventions - owning leaf
  02-fixture-capture.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Capture the suite's own golden on first authoring: script a deterministic 3+ tool-call scenario (e.g. read_file -> write_file -> read_file verify) via the leaf-02 capture path and commit e2e/fixtures/golden/trajectory-golden-basic.golden.json | Create: e2e/fixtures/golden/trajectory-golden-basic.golden.json (via capture, then committed) | `python3 -c "import json;d=json.load(open('e2e/fixtures/golden/trajectory-golden-basic.golden.json'));assert d['captured_from']=='fakellm-scripted' and len(d['golden'])>=3"` | fixture missing | fixture parses, captured_from literal correct, >= 3 entries | Given the scripted scenario; When captured; Then the committed fixture matches C4 with >= 3 normalized entries |
| T2 | Suite: replay the scripted scenario against a scratch daemon, read session_tool_calls ORDER BY seq, normalize, Compare vs the committed fixture (Options from fixture "match" block), require Result.Passed; negative drill test: reorder one call in-memory and assert Compare fails with a StepDiff-bearing Summary | Create: e2e/suites/trajectory-golden/trajectory_golden_test.go | `make e2e-fast-area AREA=trajectory-golden` | suite dir missing | suite passes both tests | Given the committed golden and the identical script; When replay+Compare; Then Passed=true; Given one call reordered; When Compare; Then Passed=false and Summary names the first diff |
| T3 | Manifest: add the suites entry + path_map entry for internal/eval/trajectorydiff/ in e2e/manifest.json (same commit as the package - repo rule) | Modify: e2e/manifest.json | `scripts/e2e-affected.sh --list internal/eval/trajectorydiff/x.go` prints trajectory-golden; `make e2e-fast` includes the suite | entry missing | affected mapping resolves; full fast tier green | Given the changed path internal/eval/trajectorydiff/; When e2e-affected --list; Then trajectory-golden is listed |

**Worked example (canonical row expansion):**

T2:
1. Write failing test (suite skeleton):

```go
//go:build e2e

package trajectorygolden

func TestTrajectoryGolden_GoldenPathSequencePasses(t *testing.T) {
    // replay via harness FakeLLM + scratch daemon (mirror eval-judge
    // suite's daemon wiring), then:
    // golden := readFixture(t, "trajectory-golden-basic")
    // actual := harness-normalized recorded calls
    // res := trajectorydiff.Compare(golden, actual, optsFromFixture)
    // if !res.Passed { t.Fatalf("sequence regressed: %s", res.Summary) }
}
```

2. Verify failure: `make e2e-fast-area AREA=trajectory-golden`
   Expected: FAIL - golden call has no replay/compare implementation
   behind it yet.
3. Minimal implementation: full replay + compare path per T2 row.
4. Verify pass: same command, PASS (both tests).

Tests-first gate branch: n/a - leaf-internal TDD.

## Self-Verification Checklist

- [ ] All tasks implemented and suite passing via
      `make e2e-fast-area AREA=trajectory-golden`
- [ ] Interface contracts satisfied exactly (C4 read path, C2 Compare
      usage, C6 manifest shape)
- [ ] All files at exact specified paths; manifest entry + suite in the
      same commit
- [ ] Negative drill proves the gate bites (reordered sequence fails)
- [ ] No retry loops, no live-tier changes, no advisory mode
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - no comparator changes, no capture changes
- [ ] gofmt clean; ASCII only; `-p 2` respected in any go test command

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; suite green in the fast tier
- [ ] Manifest suites entry + path_map entry both present and correct
- [ ] Suite is hermetic (FakeLLM only; no network, no live model)
- [ ] Failure output is investigable (Summary/StepDiff surfaced in the
      t.Fatal message)
- [ ] Conventions followed; no bugs; no debug artifacts or line-number
      corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Fixture read path: e2e/fixtures/golden/ relative to repo root - suites
  run from the repo root (existing suites rely on this; verify with the
  eval-judge suite's assumptions).
- The suite's Options come from the fixture's "match" block, not
  hardcoded - so threshold/mode tuning is a fixture edit plus an
  explicit regenerate review, never a code edit.
- If the daemon writes extra internal tool calls (e.g. memory or
  scheduler tools) into the recorded sequence, handle via the fixture's
  ignore_tools list captured at T1 time - do not widen the comparator.
