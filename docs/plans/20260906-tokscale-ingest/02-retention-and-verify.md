# Retention Exemption + Ingest Verification — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Exempt `llm_calls` from the retention purge and prove the
  tokscale read surface (master.md Contract D) with an integration test.
- **Dependencies:** 01-schema-and-capture.md (requires the schema it lands:
  session_id / reasoning_tokens / cache_creation_tokens columns).
  **Dispatch only after Leaf 01 is reviewed and committed.**
- **Estimated Context:** ~40K (exploration ~10K + generation ~12K + iteration ~12K + overhead ~6K)
- **Concurrency Group:** A (sequentially after Leaf 01)

## Goal

Two deliverables:

1. **Durability.** The retention purge (`internal/metrics/store.go` ~513,
   `retentionTables`) deletes `llm_calls` rows older than
   `DefaultRetentionDays` (30). Tokscale aggregates year-scale history, so
   `llm_calls` must stop being purged.
2. **Proof.** An integration test that writes a synthetic multi-session /
   multi-model fixture through `RecordLLMCall`, then executes the exact
   aggregation SQL the future tokscale parser will run, asserting the
   per-(session, model) projection matches master.md Contract D. This pins
   the read surface so the tokscale-side parser can be authored without
   touching meept again.

## Context

Meept's metrics store runs a periodic purge over time-based tables. The
purge list is a slice of table names in `internal/metrics/store.go` (~513):
`retentionTables := []string{"events", "error_records", "dispatch_log",
"response_quality", "lint_runs", "test_runs", "llm_calls"}`. Removing
`llm_calls` from that list is the entire retention change — everything else
keeps current behavior.

Key files to understand before implementing:
- `internal/metrics/store.go` — purge loop + retentionTables (~513),
  `RecordLLMCall` (~610), `QueryLLMCallUsage` (~690).
- `internal/metrics/store_test.go` — existing store test helpers
  (constructor pattern, `t.TempDir()` usage).
- `docs/plans/20260906-tokscale-ingest/master.md` Contract D — the frozen
  read surface (column names, semantics, dedup rule). Restated below.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/metrics/store.go — purge configuration change:
// llm_calls is absent from retentionTables; raw call rows persist
// indefinitely (no per-table day cap introduced in this leaf).
// A short comment at the retentionTables definition documents WHY:
// "llm_calls is tokscale's ingest source (see
// docs/plans/20260906-tokscale-ingest/master.md Contract D) — exempt
// from time-based retention."
```

Test-level exposure only (no production API): a new test file proving
Contract D's projection. No exported symbols change in this leaf.

### What This Leaf Consumes

From Leaf 01 (committed before this leaf dispatches):
- `llm_calls` columns: session_id, reasoning_tokens, cache_creation_tokens
- `LLMCallRecord{SessionID, ReasoningTokens, CacheCreationTokens}`
- `TokenUsage{ReasoningTokens, CacheCreationTokens}` (used to write the fixture)

## Tasks

### Task 1: Exempt llm_calls from the retention purge

**Objective:** `llm_calls` survives the daily purge; all other tables purge
as before.

**Files:**
- Modify: `internal/metrics/store.go` (~513 retentionTables slice + doc
  comment)
- Test: `internal/metrics/store_test.go`

**Step 1: Write failing test**

```go
func TestStore_RetentionPreservesLLMCalls(t *testing.T) {
	store := newTestStore(t) // same helper pattern as Leaf 01's tests
	defer store.Close()

	old := time.Now().UTC().Add(-90 * 24 * time.Hour)
	store.RecordLLMCall(LLMCallRecord{
		Timestamp:  old,
		Provider:   "anthropic",
		ModelID:    "claude-old",
		SessionID:  "conv-old",
		TokensSent: 10, TokensRecv: 5,
	})
	// A control table that must still purge: dispatch_log (old row).
	if _, err := store.db.Exec(
		`INSERT INTO dispatch_log (timestamp, session_id) VALUES (?, 's1')`,
		old.Format(time.RFC3339)); err != nil {
		t.Fatal(err) // adapt column list to the actual dispatch_log schema
	}

	store.runRetentionPurge() // adapt to the real method/loop entry point name

	var llm int
	if err := store.db.Get(&llm, `SELECT COUNT(*) FROM llm_calls`); err != nil {
		t.Fatal(err)
	}
	if llm != 1 {
		t.Fatalf("llm_calls rows after purge = %d, want 1 (exempt)", llm)
	}
	var disp int
	if err := store.db.Get(&disp, `SELECT COUNT(*) FROM dispatch_log`); err != nil {
		t.Fatal(err)
	}
	if disp != 0 {
		t.Fatalf("dispatch_log rows after purge = %d, want 0 (still purged)", disp)
	}
}
```

Adapt two things to reality before running (read the source, do not guess):
the purge entry-point name (`runRetentionPurge` above is a guess — find the
actual method that iterates retentionTables; if it is only reachable via
time-based loop, factor the per-table DELETE into a small unexported method
and call that in tests), and the dispatch_log INSERT column list.

**Step 2: Run test to verify failure**

Run: `go test ./internal/metrics/ -run TestStore_RetentionPreservesLLMCalls -v`
Expected: FAIL — llm_calls row is deleted by the current purge.

**Step 3: Write minimal implementation**

Remove `"llm_calls"` from the retentionTables slice. Add the WHY comment
per the contract. Touch nothing else in the purge path.

**Step 4: Run test to verify pass**

Run: `go test ./internal/metrics/ -run TestStore_RetentionPreservesLLMCalls -v`
Expected: PASS. Then `go test ./internal/metrics/ -count=1` — package green.

### Task 2: Contract D ingest-verification test

**Objective:** Prove the tokscale read surface end-to-end: write through
`RecordLLMCall`, read through Contract D's aggregation SQL, assert the
projection.

**Files:**
- Test: `internal/metrics/tokscale_ingest_test.go` (new file)

**Step 1: Write failing test**

```go
// tokscale_ingest_test.go
//
// Pins the read surface the tokscale CLI parser will issue against
// ~/.meept/metrics.db (docs/plans/20260906-tokscale-ingest/master.md,
// Contract D). If this test breaks, tokscale's parser breaks — change
// Contract D in the master plan, not silently here.
func TestTokscaleIngestProjection(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	rows := []LLMCallRecord{
		// Session A, claude-test: two calls (one with cache + reasoning)
		{Timestamp: base, Provider: "anthropic", ModelID: "claude-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 1000, TokensRecv: 500, TokensCached: 200,
			ReasoningTokens: 100, CacheCreationTokens: 150},
		{Timestamp: base.Add(time.Minute), Provider: "anthropic", ModelID: "claude-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 800, TokensRecv: 300},
		// Session A, second model
		{Timestamp: base.Add(2 * time.Minute), Provider: "anthropic", ModelID: "other-test", AgentID: "coder",
			SessionID: "convA", TokensSent: 100, TokensRecv: 40},
		// Session B
		{Timestamp: base.Add(3 * time.Minute), Provider: "openai", ModelID: "gpt-test", AgentID: "reviewer",
			SessionID: "convB", TokensSent: 50, TokensRecv: 25},
		// Error row: usage zero, must be excluded by the projection
		{Timestamp: base.Add(4 * time.Minute), Provider: "openai", ModelID: "gpt-test", AgentID: "reviewer",
			SessionID: "convB", IsError: true, ErrorMessage: "boom"},
	}
	for _, r := range rows {
		store.RecordLLMCall(r)
	}

	// Contract D projection: per (session_id, model_id) sums, error rows excluded.
	type projRow struct {
		SessionID           string `db:"session_id"`
		ModelID             string `db:"model_id"`
		InputTokens         int    `db:"input_tokens"`
		OutputTokens        int    `db:"output_tokens"`
		CacheReadTokens     int    `db:"cache_read_tokens"`
		CacheWriteTokens    int    `db:"cache_write_tokens"`
		ReasoningTokens     int    `db:"reasoning_tokens"`
	}
	want := map[string]projRow{
		"convA|claude-test": {SessionID: "convA", ModelID: "claude-test",
			InputTokens: 1800, OutputTokens: 800, CacheReadTokens: 200,
			CacheWriteTokens: 150, ReasoningTokens: 100},
		"convA|other-test": {SessionID: "convA", ModelID: "other-test",
			InputTokens: 100, OutputTokens: 40},
		"convB|gpt-test": {SessionID: "convB", ModelID: "gpt-test",
			InputTokens: 50, OutputTokens: 25},
	}

	var got []projRow
	if err := store.db.Select(&got, `
		SELECT session_id, model_id,
		       SUM(tokens_sent)              AS input_tokens,
		       SUM(tokens_received)          AS output_tokens,
		       SUM(tokens_cached)            AS cache_read_tokens,
		       SUM(cache_creation_tokens)    AS cache_write_tokens,
		       SUM(reasoning_tokens)         AS reasoning_tokens
		FROM llm_calls
		WHERE error = 0
		GROUP BY session_id, model_id
		ORDER BY session_id, model_id`); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("projection rows = %d, want %d: %+v", len(got), len(want), got)
	}
	for _, g := range got {
		key := g.SessionID + "|" + g.ModelID
		w, ok := want[key]
		if !ok {
			t.Fatalf("unexpected projection key %q", key)
		}
		if g != w {
			t.Fatalf("%s: got %+v, want %+v", key, g, w)
		}
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/metrics/ -run TestTokscaleIngestProjection -v`
Expected: FAIL — unknown column (Leaf 01 not applied) if dispatched out of
order; otherwise PASS trivially. If it passes first try, that is fine —
it is a contract pin, not a bug fix. Record that in your report.

**Step 3: Write implementation** (none — test-only leaf task; the
"implementation" is Leaf 01's schema. If this test fails here, STOP and
report BLOCKED against Leaf 01 rather than patching production code.)

**Step 4: Verify**

Run: `go test ./internal/metrics/ -count=1 -race` — all pass, including
the new pin and Leaf 01's tests.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] Both tasks implemented; all tests passing (`go test ./internal/metrics/... -count=1 -race`)
- [ ] Interface contracts (above) satisfied — retentionTables no longer
      contains llm_calls; WHY comment present at the definition
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] `go build ./...` clean; `go vet ./internal/metrics/...` clean; `gofmt -l internal/metrics` empty
- [ ] Grep production callers: `rg -n "retentionTables" internal/metrics/` —
      single definition, llm_calls absent
- [ ] No debug artifacts, no TODOs, no placeholder values

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] retentionTables excludes llm_calls and the WHY comment cites Contract D
- [ ] Other purged tables unaffected (control-table assertion exists)
- [ ] Contract D projection SQL matches the master.md contract column-for-column
- [ ] Error rows excluded by the projection (error = 0 filter asserted)
- [ ] Code follows project conventions
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks
- [ ] No debug artifacts

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The purge entry point may be a loop inside a goroutine (aggregationLoop /
  flushLoop neighborhood). If it is not directly callable, factor the
  per-table delete into a small unexported method — that refactor is in
  scope; changing purge behavior for OTHER tables is not.
- dispatch_log's INSERT in Task 1 is a control assertion (proves the purge
  still runs). If its schema differs, adapt the INSERT, never weaken the
  assertion.
- Contract D's dedup rule (tokscale dedups by row id) does not affect this
  test — each RecordLLMCall is one row, ids are distinct by construction.
- Keep this leaf test-only except for the retentionTables edit. If you find
  yourself wanting to change RecordLLMCall or the schema, that is Leaf 01
  territory — report BLOCKED instead.
