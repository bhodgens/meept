# Golden Fixture Capture + Regenerate Workflow - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Capture harness turning FakeLLM-scripted hermetic runs into
  committed golden fixtures + `make golden-regenerate` workflow.
- **Dependencies:** 01-comparator.md (REVIEWED)
- **Estimated Context:** 55K
- **Concurrency Group:** C
- **Audit references:** master C4, C5; Requirements R1, R4, R7, R8

## Goal

Make golden fixtures first-class repo artifacts: a harness function that
replays a scripted FakeLLM scenario against a scratch daemon, pulls the
recorded session_tool_calls, normalizes them, and writes the frozen C4
fixture format; plus Makefile targets that regenerate fixtures ONLY
after re-running the suite and printing the diff (investigate-then-
regenerate, never blind-regen). This leaf ships NO suite - leaf 03
consumes what this leaf produces.

## Context

- e2e/harness/fakellm.go:349 - NewFakeLLM; :379 EnqueueToolCalls;
  :66 ToolCall{Name, Arguments} - the scripted substrate. Requests are
  routed by SHAPE, not arrival order; the executor consumes the tool
  call queue in order.
- e2e/harness/tasksdb.go - read-only sqlite DSN pattern
  (?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro) to
  mirror when reading sessions.db.
- internal/session/store_sqlite.go:2271 - GetToolCalls, ORDER BY seq -
  the authoritative recorded sequence; the harness reads the same table
  read-only.
- e2e/fixtures/ does not exist yet - this leaf creates
  e2e/fixtures/golden/.
- Suites already use harness.NewFakeLLM (grep e2e/suites) - the capture
  harness follows that pattern, not a new one.

## Verified Anchors

- [e2e/harness/fakellm.go:66-69] - ToolCall{Name, Arguments} - script
  shape the capture Script closure uses
- [e2e/harness/fakellm.go:349] - NewFakeLLM - constructor pattern
- [e2e/harness/fakellm.go:379] - EnqueueToolCalls - ordered scripting
- [e2e/harness/tasksdb.go:34-50] - openTasksDB read-only DSN - mirror
  for sessions.db reads
- [internal/session/store_sqlite.go:206-219] - session_tool_calls schema
- [Makefile:1575] - e2e-fast target - Makefile style reference
- [scripts/e2e-affected.sh] - bash-3.2 + python3 JSON convention for
  any helper scripting

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// e2e/harness/golden.go (package harness)
type CaptureOptions struct {
    ScenarioName string
    GoldenDir    string
    Script       func(f *FakeLLM)
    Match        trajectorydiff.Options
}
func CaptureGolden(t testing.TB, opts CaptureOptions) (wrotePath string, err error)
// C4 fixture format (frozen in master):
// {"schema_version":1, "scenario":..., "captured_from":"fakellm-scripted",
//  "match":{...}, "golden":[{name, args_shape}...]}
// Makefile targets (frozen names):
//   make golden-regenerate SCENARIO=<name>
//   make golden-regenerate-all
```

### What This Leaf Consumes

- C1 NormalizedCall / C3 Normalize / NormalizeSessionCalls -
  `trajectorydiff.Normalize(name, argumentsJSON) (NormalizedCall, error)`
  - owning leaf 01-comparator.md
- C2 Options - `trajectorydiff.Options` - owning leaf 01-comparator.md
- harness.FakeLLM + EnqueueToolCalls (e2e/harness/fakellm.go) - existing

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | CaptureGolden: replay a scripted scenario end-to-end against a scratch daemon (FakeLLM configured as the daemon's model endpoint), read session_tool_calls ORDER BY seq for the assistant turn, normalize via C3, write the C4 fixture JSON to GoldenDir/<ScenarioName>.golden.json with captured_from == "fakellm-scripted" | Create: e2e/harness/golden.go; Test: e2e/harness/golden_test.go (build tag e2e) | `go test -p 2 -tags e2e ./e2e/harness/ -run TestCaptureGolden` | golden.go does not exist | capture test passes and writes a parseable fixture | Given a script enqueueing read_file then write_file; When CaptureGolden; Then the fixture contains 2 golden entries with scrubbed args_shape and captured_from literal |
| T2 | Refuse non-scripted capture: CaptureGolden errors when the scenario did not run through FakeLLM scripting (no daemon, live endpoint configured, or zero recorded calls) - enforcement of the "goldens NEVER derive from live-model runs" policy | same files as T1 | same as T1 | refusal path unimplemented | refusal tests pass | Given CaptureOptions with an empty Script; When CaptureGolden; Then err != nil and NO fixture file is written |
| T3 | Golden-regenerate suite-first helper: a Go helper (same package or small cmd under scripts/ mirroring repo conventions) that, per scenario: runs the trajectory-golden suite first, prints the actual-vs-golden diff for investigation, and only then regenerates the fixture; refusing to write when the suite cannot run | Create: e2e/harness/golden_regen.go (helper); Test: e2e/harness/golden_regen_test.go | `go test -p 2 -tags e2e ./e2e/harness/ -run TestRegen` | helper missing | regen-helper tests pass (suite-not-runnable case refuses) | Given the suite currently fails and --force is absent; When regenerate is invoked; Then the diff is printed, the fixture is NOT overwritten, exit code names the refusal |
| T4 | Makefile targets golden-regenerate (one scenario) + golden-regenerate-all (every fixture file), wiring the T3 helper; documented usage comments in Makefile style | Modify: Makefile (near the e2e targets, ~line 1570) | `make -n golden-regenerate SCENARIO=demo` prints the helper invocation; `make golden-regenerate SCENARIO=__nonexistent__` fails with a clear error | targets missing | make -n shows correct recipe; bad scenario errors clearly | Given SCENARIO=demo; When make -n; Then the printed recipe calls the regen helper with scenario=demo; Given SCENARIO with no fixture; When make golden-regenerate; Then a clear error names the missing scenario |

**Worked example (canonical row expansion):**

T2:
1. Write failing test:

```go
func TestCaptureGoldenRefusesUnscripted(t *testing.T) {
    dir := t.TempDir()
    _, err := CaptureGolden(t, CaptureOptions{
        ScenarioName: "no-script", GoldenDir: dir, Script: nil,
    })
    if err == nil {
        t.Fatal("want refusal error for unscripted capture")
    }
    entries, _ := os.ReadDir(dir)
    if len(entries) != 0 {
        t.Fatalf("no fixture may be written on refusal, got %d files", len(entries))
    }
}
```

2. Verify failure: `go test -p 2 -tags e2e ./e2e/harness/ -run TestCaptureGoldenRefusesUnscripted`
   Expected: FAIL - CaptureGolden undefined.
3. Minimal implementation: nil/empty Script -> wrapped error; never
   touch GoldenDir on the refusal path.
4. Verify pass: same command, PASS.

Tests-first gate branch: n/a - leaf-internal TDD.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C4 format, C5 signatures,
      make target names)
- [ ] All files at exact specified paths
- [ ] captured_from is the literal "fakellm-scripted" on every written
      fixture; no code path can write any other value
- [ ] Refusal paths never write partial fixtures
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - no suite, no manifest entry, no CI changes
- [ ] gofmt clean; ASCII only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Fixture bytes match C4 exactly (field names, schema_version 1)
- [ ] Live-run capture is structurally impossible (policy R4)
- [ ] Regen path prints investigation diff BEFORE writing (policy R8)
- [ ] Read-only DB access follows the tasksdb.go DSN pattern; no writes
      to daemon state
- [ ] Conventions followed; no bugs; no debug artifacts or line-number
      corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The daemon under test must point its model endpoint at the FakeLLM
  URL (existing suites show the wiring; session-teardown and
  runtime-hang are close examples).
- sessions.db path: resolve under the scratch MEEPT_HOME the harness
  already sets (mirror how tasksdb.go callers locate tasks.db).
- The regen helper may shell out to `go test` via exec.Command - keep it
  bash-3.2/py3-compatible in spirit; no bashisms in any script.
- Do NOT create the suite or manifest entry here - leaf 03 owns them;
  T3's "run the suite" step must degrade gracefully (clear refusal) if
  the suite does not exist yet, since this leaf lands first.
