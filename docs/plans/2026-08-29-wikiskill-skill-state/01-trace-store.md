# Trace Store — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ./master.md
- **Scope:** Immutable on-disk trace store (`raw/` layer, arXiv:2608.27454) plus
  persistence of failure trajectories from the AgentLoop learning hook.
- **Dependencies:** none (Wave A)
- **Estimated Context:** ~50K
- **Concurrency Group:** A
- **Research reference:** arXiv:2608.27454 (WikiSkill) §3.1 Raw Layer +
  Appendix C sampling. Commit message MUST cite the arXiv id (see master
  Dispatch Protocol).

## Goal

Meept's evolver cannot learn from failures: `buildTrajectory`
(internal/agent/loop.go:2431) hardcodes `Success: true` and no trajectory is
ever persisted. This leaf adds (1) `TraceStore`, an immutable
`<wikiDir>/traces/<yyyy-mm-dd>/<id>.json` writer with stratified sampling, and
(2) an AgentLoop hook that writes a trace for EVERY learning-eligible turn —
success AND failure — before the Judge call.

This is the evidence base every later leaf consumes. It changes no prompt, no
retrieval, and no user-facing surface.

## Context

Key files to understand before implementing:

- internal/selfimprove/learning.go — `Trajectory`, `TrajectoryStep`,
  `TrajectoryOutcome` types and `Judge` flow. This leaf lives in the same
  package. Tests are INTERNAL (`package selfimprove`); grep for existing
  helpers before redeclaring.
- internal/agent/loop.go:2149-2166 — the learning goroutine block: snapshots
  `injectedSkills`, calls `triggerLearning` in a `l.wg`-tracked goroutine.
  The write hook goes next to this.
- internal/agent/loop.go:2377-2462 — `triggerLearning` and `buildTrajectory`.
  Triggered only when `l.learningPipeline != nil && err == nil` today.
- internal/skills/lifecycle/writer.go — the atomic `.tmp` + `os.Rename` write
  pattern to copy.
- pkg/id — `Generate()` for IDs. NEVER time.Now().UnixNano() or math/rand
  (predid analyzer blocks it).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/selfimprove/trace_store.go
package selfimprove

type TraceStep struct {
    Action  string `json:"action"`
    Input   string `json:"input,omitempty"`
    Output  string `json:"output,omitempty"`
    Success bool   `json:"success"`
}

// Outcome constants for TraceRecord.Outcome.
const (
    TraceOutcomeSuccess = "success"
    TraceOutcomeFailure = "failure"
)

type TraceRecord struct {
    ID             string      `json:"id"`
    SessionID      string      `json:"session_id"`
    Domain         string      `json:"domain,omitempty"`
    Outcome        string      `json:"outcome"`
    Error          string      `json:"error,omitempty"`
    InjectedSkills []string    `json:"injected_skills,omitempty"`
    Steps          []TraceStep `json:"steps"`
    Summary        string      `json:"summary,omitempty"`
    CreatedAt      time.Time   `json:"created_at"`
}

type TraceStore struct { /* dir + logger */ }

func NewTraceStore(dir string, logger *slog.Logger) *TraceStore
// Write persists <dir>/traces/<yyyy-mm-dd>/<id>.json atomically.
func (ts *TraceStore) Write(rec *TraceRecord) (string, error)
// Sample: newest-first walk; up to maxFails failures then up to maxPasses
// successes; each record's step text capped to maxChars total (truncate
// long step inputs/outputs with a "...[truncated]" marker); I/O errors on
// individual files are logged and skipped, never returned.
func (ts *TraceStore) Sample(maxFails, maxPasses, maxChars int) ([]TraceRecord, error)
```

AgentLoop additions (same leaf):

```go
// File: internal/agent/loop.go (+ small new file loop_trace.go is OK)
// agent package does NOT import selfimprove — declare a mirror record type
// and a narrow writer interface to avoid the import:
type traceRecordMirror struct { /* same JSON tags as selfimprove.TraceRecord */ }
type TraceWriter interface {
    WriteTrace(rec *traceRecordMirror) (string, error)
}
func WithTraceWriter(tw TraceWriter) LoopOption // nil guard; mutex-protected field
```

### What This Leaf Consumes

- `pkg/id.Generate()` (existing).
- Existing `Trajectory`/`TrajectoryStep` shapes from internal/agent (do not
  modify their definitions).

## Tasks

### Task 1: TraceStore.Write — atomic persistence

**Objective:** Persist a TraceRecord as immutable JSON under
`<dir>/traces/<yyyy-mm-dd>/<id>.json`.

**Files:**
- Create: `internal/selfimprove/trace_store.go`
- Test: `internal/selfimprove/trace_store_test.go`

**Step 1: Write failing test**

```go
func TestTraceStoreWrite_CreatesDatedFile(t *testing.T) {
    ts := NewTraceStore(t.TempDir(), testLogger())
    rec := &TraceRecord{
        ID:        pkgid.Generate(), // import as used elsewhere in repo
        SessionID: "conv-1",
        Outcome:   TraceOutcomeFailure,
        Error:     "boom",
        Steps:     []TraceStep{{Action: "assistant_response", Output: "x", Success: true}},
        CreatedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC), // fixed noon-UTC convention
    }
    path, err := ts.Write(rec)
    if err != nil {
        t.Fatalf("write: %v", err)
    }
    if !strings.Contains(path, filepath.Join("traces", "2025-06-15")) {
        t.Fatalf("path missing dated dir: %s", path)
    }
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    var got TraceRecord
    if err := json.Unmarshal(data, &got); err != nil {
        t.Fatalf("unmarshal: %v", err)
    }
    if got.ID != rec.ID || got.Outcome != TraceOutcomeFailure {
        t.Fatalf("round-trip mismatch: %+v", got)
    }
    // .tmp leftovers must not exist (atomic rename pattern).
    entries, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
    if len(entries) != 0 {
        t.Fatalf("stray tmp files: %v", entries)
    }
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/selfimprove/ -run TestTraceStoreWrite -v`
Expected: FAIL — undefined: NewTraceStore

**Step 3: Write minimal implementation**

`trace_store.go`: struct holds `dir string` + `logger *slog.Logger`
(default slog when nil). Write: `os.MkdirAll` the dated dir (0o755, gosec
nolint comment per repo convention), marshal with two-space indent, write
`<id>.json.tmp` (0o600), `os.Rename` to final. No mutex needed — Write is
per-call independent and rename is atomic; document that in a comment.

**Step 4: Run test to verify pass**

Run: `go test ./internal/selfimprove/ -run TestTraceStoreWrite -v`
Expected: PASS

### Task 2: TraceStore.Sample — stratified fail/pass sampling

**Objective:** Return up to maxFails failures then maxPasses successes,
newest-first, with per-record char caps and resilient I/O.

**Files:**
- Modify: `internal/selfimprove/trace_store.go`
- Test: `internal/selfimprove/trace_store_test.go`

**Step 1: Write failing test**

```go
func TestTraceStoreSample_Stratified(t *testing.T) {
    dir := t.TempDir()
    ts := NewTraceStore(dir, testLogger())
    base := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
    for i := 0; i < 4; i++ {
        _, err := ts.Write(&TraceRecord{ID: fmt.Sprintf("f%d", i), Outcome: TraceOutcomeFailure, CreatedAt: base})
        if err != nil { t.Fatal(err) }
    }
    for i := 0; i < 4; i++ {
        _, err := ts.Write(&TraceRecord{ID: fmt.Sprintf("s%d", i), Outcome: TraceOutcomeSuccess, CreatedAt: base})
        if err != nil { t.Fatal(err) }
    }
    got, err := ts.Sample(2, 1, 100000)
    if err != nil { t.Fatalf("sample: %v", err) }
    if len(got) != 3 { t.Fatalf("want 3, got %d", len(got)) }
    fails, passes := 0, 0
    for _, r := range got {
        if r.Outcome == TraceOutcomeFailure { fails++ } else { passes++ }
    }
    if fails != 2 || passes != 1 { t.Fatalf("stratification wrong: %d fails, %d passes", fails, passes) }
}
```

Add a second test: corrupt one JSON file on disk, Sample must still return
the rest (error resilience).

**Step 2: Run test to verify failure**

Run: `go test ./internal/selfimprove/ -run TestTraceStoreSample -v`
Expected: FAIL — undefined: TraceStore.Sample

**Step 3: Write minimal implementation**

Walk `<dir>/traces/*/` descending by date-dir name; within a dir sort files
descending by name (ids sort lexically; acceptable for v1 — note in comment).
Unmarshal, bucket by Outcome, enforce the char cap by truncating step
Input/Output strings down to fit `maxChars` per record total. Skip and log
corrupt files.

**Step 4: Run test to verify pass**

Run: `go test ./internal/selfimprove/ -run TestTraceStoreSample -v`
Expected: PASS

### Task 3: AgentLoop trace hook — record failures too

**Objective:** Every learning-eligible turn writes a trace; failure turns get
`Outcome=failure` with the error text.

**Files:**
- Modify: `internal/agent/loop.go` (region 2149-2166 + triggerLearning 2377)
- Create (optional, preferred): `internal/agent/loop_trace.go`
- Test: `internal/agent/loop_trace_test.go`

**Step 1: Write failing test**

Test the pure helper + the interface, not a full loop run:

```go
func TestBuildTraceRecord_FailureCarriesError(t *testing.T) {
    traj := Trajectory{ID: "conv-9", Domain: "code", Steps: []TrajectoryStep{{Action: "user_input", Input: "q"}}}
    rec := buildTraceRecord(traj, "conv-9", []string{"reviewer"}, fmt.Errorf("tool exploded"), "partial answer")
    if rec.Outcome != "failure" || rec.Error != "tool exploded" {
        t.Fatalf("failure not captured: %+v", rec)
    }
    if rec.ID == "" { t.Fatal("id must be generated") }
}
```

Plus a compile-time assertion that a stub satisfies TraceWriter.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestBuildTraceRecord -v`
Expected: FAIL — undefined: buildTraceRecord

**Step 3: Write minimal implementation**

- `WithTraceWriter(tw)` LoopOption (nil guard, mutex-protected `traceWriter`
  field on AgentLoop; mirror the WithUsageTracker pattern at loop.go:883).
- In the learning goroutine block (loop.go:2153), build a mirror record from
  the trajectory + `err`/outcome and call `l.traceWriter.WriteTrace(rec)` —
  BEFORE `triggerLearning`'s judge path; write even when `err != nil` ONLY if
  `conv` is available at that site — check the enclosing block: today the
  block is gated on `err == nil`. Extend the gating so failure turns with a
  non-nil conversation snapshot also write failure traces (keep the
  learningPipeline judge call gated on err == nil as today). Write errors log
  at Debug and never affect the turn.
- `triggerLearning` already has conv + outcome; pass what it needs into
  `buildTraceRecord` (domain from `l.classifyDomain(messages)` reuse is fine
  via the existing trajectory).

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestBuildTraceRecord -race -count=1 -v`
Expected: PASS

### Task 4: Self-verification sweep

- `go build ./...`
- `go test ./internal/selfimprove/ ./internal/agent/ -race -count=1`
- `gofmt -l` on exactly your touched files
- `go run ./tools/analyzers/mutexio/... ./internal/selfimprove/ ./internal/agent/`
- Confirm no prompt-builder (context_injector.go, prompts/*) references the
  new types.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`-race -count=1`)
- [ ] Contracts satisfied exactly (names, signatures, JSON tags)
- [ ] Files at exact specified paths
- [ ] No deviations (or documented below)
- [ ] No scope creep; no prompt-path changes
- [ ] IDs from pkg/id.Generate(); fixed-noon-UTC fixtures for dated dirs

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master §Contract 1 exactly (TraceStore + agent hook)
- [ ] Atomic writes (.tmp+rename); no I/O under any mutex
- [ ] Failure turns actually reach WriteTrace (not just success)
- [ ] No unused exports; no debug artifacts; gofmt clean
- [ ] No wiki/trace type referenced from any inference prompt builder

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The dated-dir convention (`traces/<yyyy-mm-dd>/`) exists so Sample can walk
  by date cheaply; do not add an index file in v1.
- Do NOT wire the store into the daemon (leaf 05 owns wiring).
- Sample's maxChars truncation must mark truncations explicitly
  (`...[truncated]`) so the evolver prompt (leaf 03) can be honest about it.
