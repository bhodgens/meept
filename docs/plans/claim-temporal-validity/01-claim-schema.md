# Leaf 01 — Claim Schema: Temporal Fields + Monotonic Rev

<!-- DISPATCH INSTRUCTION
Dispatch as ONE implementation-agent session. Go only. Files to touch are
exactly the three listed in Scope. The agent reads this leaf + master.md
(Interface Contracts + Coding Conventions sections) and the source files
listed in Dependencies before writing any code.
-->

## Scope

Exactly three changed files:

1. `internal/memory/epistemic.go` — extend `Claim`, extend `StoreClaim`
   metadata mapping, add `claimRev` + `claimInForce` helpers, add the
   unexported in-place stamp helper, extend `MarkSuperseded` to stamp
   rev/valid_to.
2. `internal/memory/epistemic_test.go` — new table-driven tests.
3. `internal/memory/episodic.go` — ONLY IF the in-place stamp cannot be
   implemented purely in epistemic.go without touching it (expected: no
   change; see Task 4.2).

**No other files.** If a fourth file seems necessary, STOP and report the
conflict to the orchestrator instead of proceeding (it becomes an
OPEN-QUESTIONS entry).

## Dependencies

Read before coding (anchors verified 2026-09-05):

- `docs/plans/claim-temporal-validity/master.md` — Interface Contracts C1-C5,
  Coding Conventions.
- `AGENTS.md` — two-value assertions, mutexio, error handling, setter guards,
  AGENTS.md maintenance rule.
- `internal/memory/epistemic.go` (whole file, 605 lines) — `Claim` at :88-95,
  `StoreClaim` at :167-190, `MarkSuperseded` at :458-533, `generateAuditID`
  at :598.
- `internal/memory/manager.go:1359-1402` — `StoreOptions`/`StoreVersioned`/
  `markVersionNonCurrent` (the in-place stamp mirrors the latter's DB access
  pattern via `m.episodic.store.GetDB()`).
- `internal/memory/episodic.go:24` (`metadata_json` column), :133-180
  (`EpisodicMemory.Store` — how metadata JSON is written), :222-405 (SELECTs
  that parse metadata back).
- `internal/memory/types.go:136` — `MetadataJSON` (how the map serializes).
- `internal/memory/epistemic_test.go` — existing test style
  (`TestClaimStatusTrustWeight` :9, `TestMarkSupersededUninitialized` :112).

## Estimated context

~60K tokens (leaf doc + master contracts + ~2,400 lines of Go across the read
list + writing ~250 lines of code/tests).

## Interface Contract

Implements master contracts **C1-C5** verbatim:

- C1: the extended `Claim` struct (exact field comments included).
- C2: metadata keys `observed_at`, `valid_from`, `valid_to`, `rev`; rev is
  read back through a `float64` assertion (`claimRev`).
- C3: `claimInForce(mem Memory, now time.Time) bool` — exact body pinned in
  master.
- C5: supersede stamps `rev = oldRev + 1` on the successor and
  `valid_to` + `superseded_at` on the superseded claim, **in place** (same
  row IDs), before the existing mark-non-current + edge write.

Deviations are not permitted; report conflicts to the orchestrator.

## Tasks

### Task 1 — Extend the Claim struct (TDD: test first)

1. Add to `internal/memory/epistemic_test.go` a struct-shape test (compiles
   the contract in; also serves as documentation):

```go
func TestClaimTemporalFields(t *testing.T) {
	vt := time.Now().UTC().Add(-time.Hour)
	c := Claim{
		Text:       "x",
		Status:     ClaimStatusConfirmed,
		ObservedAt: time.Now().UTC().Add(-2 * time.Hour),
		ValidTo:    &vt,
		Rev:        3,
	}
	if c.ValidTo == nil || !c.ValidTo.Equal(vt) {
		t.Fatalf("ValidTo roundtrip failed")
	}
	if c.Rev != 3 {
		t.Fatalf("Rev = %d, want 3", c.Rev)
	}
	// Zero-value backward compat: new fields must not alter zero-value use.
	var zero Claim
	if !zero.ObservedAt.IsZero() || zero.ValidFrom != nil || zero.ValidTo != nil || zero.Rev != 0 {
		t.Fatalf("zero-value Claim gained non-zero temporal/rev defaults")
	}
}
```

2. Apply contract C1 to `internal/memory/epistemic.go:88-95` — append the
   four new fields after `Status`, keeping existing fields and comments
   byte-identical.
3. `go build ./... && go test -p 2 -run 'TestClaimTemporalFields' ./internal/memory/`

### Task 2 — StoreClaim metadata mapping (TDD)

1. Test first, in `internal/memory/epistemic_test.go` (table-driven):

```go
func TestStoreClaimMetadataMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		claim       Claim
		wantKeys    map[string]bool // keys that MUST be present
		absentKeys  []string        // keys that MUST be absent
	}{
		{
			name:     "zero-value temporal fields produce no new keys",
			claim:    Claim{Text: "legacy-shaped claim", Status: ClaimStatusConfirmed},
			absentKeys: []string{"observed_at", "valid_from", "valid_to", "rev"},
		},
		{
			name: "full temporal fields",
			claim: func() Claim {
				vf := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				vt := vf.Add(90 * 24 * time.Hour)
				return Claim{Text: "bounded claim", ObservedAt: vf, ValidFrom: &vf, ValidTo: &vt}
			}(),
			wantKeys: map[string]bool{"observed_at": true, "valid_from": true, "valid_to": true},
		},
		{
			name:  "rev>0 persists",
			claim: Claim{Text: "successor", Rev: 4},
			wantKeys: map[string]bool{"rev": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t) // existing helper in this package's tests; see epistemic_sqlite_store_test.go for construction pattern
			_, err := m.StoreClaim(context.Background(), tc.claim)
			if err != nil {
				t.Fatalf("StoreClaim: %v", err)
			}
			// Read back through the same path ListAutoClaims uses.
			results, err := m.Search(context.Background(), MemoryQuery{Type: MemoryTypeClaim, Limit: 10})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			var mem *Memory
			for i := range results {
				if results[i].Memory.Content == tc.claim.Text {
					mem = &results[i].Memory
					break
				}
			}
			if mem == nil {
				t.Fatalf("stored claim not found")
			}
			for k := range tc.wantKeys {
				if _, ok := mem.Metadata[k]; !ok {
					t.Errorf("metadata key %q missing", k)
				}
			}
			for _, k := range tc.absentKeys {
				if _, ok := mem.Metadata[k]; ok {
					t.Errorf("metadata key %q must be absent for this case", k)
				}
			}
			// RFC3339 + float64 assertions for present values.
			if s, ok := mem.Metadata["valid_to"].(string); ok && tc.claim.ValidTo != nil {
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					t.Errorf("valid_to not RFC3339: %v", err)
				}
			}
			if f, ok := mem.Metadata["rev"].(float64); ok && tc.claim.Rev > 0 {
				if int64(f) != tc.claim.Rev {
					t.Errorf("rev roundtrip = %d, want %d", int64(f), tc.claim.Rev)
				}
			}
		})
	}
}
```

2. Implementation in `StoreClaim` (`epistemic.go:167-190`): keep the existing
   mapping untouched, then append:

```go
	if !c.ObservedAt.IsZero() {
		meta["observed_at"] = c.ObservedAt.Format(time.RFC3339)
	}
	if c.ValidFrom != nil {
		meta["valid_from"] = c.ValidFrom.Format(time.RFC3339)
	}
	if c.ValidTo != nil {
		meta["valid_to"] = c.ValidTo.Format(time.RFC3339)
	}
	if c.Rev != 0 {
		meta["rev"] = c.Rev
	}
```

   (Insert after the `len(c.Tags)` block, before `return m.Store(...)`.)

3. **Note on the "0 on create" contract:** `StoreClaim` persists `rev` only
   when non-zero — a fresh claim stored with `Rev: 0` and a *later* supersede
   gets its rev from the MarkSuperseded stamp (Task 4). This keeps the
   zero-value path byte-identical to today's metadata for backward compat.
   If the orchestrator prefers always-writing `rev: 0`, that is a one-line
   change + test tweak — flag it in the review notes, do not decide unilaterally.
4. Run: `go test -p 2 -run 'TestStoreClaimMetadataMapping' ./internal/memory/`

### Task 3 — claimRev + claimInForce helpers (TDD)

1. Add to `internal/memory/epistemic.go` (below the `asString` helper,
   ~:130):

```go
// claimRev reads the monotonic revision counter from claim metadata.
// Absent or non-numeric values read as 0 (backward compatibility with
// claims stored before temporal metadata existed). Metadata that has
// round-tripped through JSON stores numbers as float64.
func claimRev(meta map[string]any) int64 {
	if v, ok := meta["rev"].(float64); ok {
		return int64(v)
	}
	return 0
}

// claimInForce reports whether a claim memory is inside its validity window
// at the given instant. Absent bounds are unbounded. Malformed timestamps are
// treated as unbounded (fail-open) so a corrupt key cannot silently hide a
// claim from enforcement.
func claimInForce(mem Memory, now time.Time) bool {
	if vf, ok := mem.Metadata["valid_from"].(string); ok && vf != "" {
		if t, err := time.Parse(time.RFC3339, vf); err == nil && t.After(now) {
			return false // not yet in force
		}
	}
	if vt, ok := mem.Metadata["valid_to"].(string); ok && vt != "" {
		if t, err := time.Parse(time.RFC3339, vt); err == nil && t.Before(now) {
			return false // expired
		}
	}
	return true
}
```

2. Tests in `internal/memory/epistemic_test.go` (table-driven):

```go
func TestClaimInForce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	rfc := func(t time.Time) string { return t.Format(time.RFC3339) }
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"no metadata", nil, true},
		{"empty strings", map[string]any{"valid_from": "", "valid_to": ""}, true},
		{"valid_to in past", map[string]any{"valid_to": rfc(past)}, false},
		{"valid_to exactly now (in force)", map[string]any{"valid_to": rfc(now)}, true},
		{"valid_from in future", map[string]any{"valid_from": rfc(future)}, false},
		{"window straddling now", map[string]any{"valid_from": rfc(past), "valid_to": rfc(future)}, true},
		{"window closed before now", map[string]any{"valid_from": rfc(now.Add(-2 * time.Hour)), "valid_to": rfc(past)}, false},
		{"malformed valid_to fail-open", map[string]any{"valid_to": "not-a-time"}, true},
		{"numeric valid_to ignored", map[string]any{"valid_to": 1.5}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimInForce(Memory{Type: MemoryTypeClaim, Metadata: tc.meta}, now)
			if got != tc.want {
				t.Fatalf("claimInForce = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimRev(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta map[string]any
		want int64
	}{
		{"absent", nil, 0},
		{"float64 roundtrip", map[string]any{"rev": 7.0}, 7},
		{"non-numeric", map[string]any{"rev": "seven"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimRev(tc.meta); got != tc.want {
				t.Fatalf("claimRev = %d, want %d", got, tc.want)
			}
		})
	}
}
```

3. Run: `go test -p 2 -run 'TestClaimInForce|TestClaimRev' ./internal/memory/`

### Task 4 — Supersede rev/valid_to stamping (TDD)

1. First, the in-place stamp helper in `internal/memory/epistemic.go` (near
   `MarkSuperseded`):

```go
// stampMetadataInPlace persists a claim's metadata onto its existing row
// without minting a new version row or changing the memory ID. Used by the
// supersede path: StoreVersioned would create a NEW row ID, which would
// orphan the EdgeTypeSuperseded edge target and any evidence edges that
// point at these IDs.
//
// Collects the metadata under the manager RLock, releases it, then performs
// the DB write (mutexio: no I/O under lock).
func (m *Manager) stampMetadataInPlace(ctx context.Context, mem *Memory) error {
	m.mu.RLock()
	initialized := m.initialized
	epis := m.episodic
	m.mu.RUnlock()
	if !initialized {
		return errors.New("memory manager not initialized")
	}
	if epis == nil {
		return errors.New("episodic memory not available")
	}
	metaJSON := (&Memory{Metadata: mem.Metadata}).MetadataJSON()
	db := epis.store.GetDB()
	_, err := db.ExecContext(ctx,
		"UPDATE episodic_memories SET metadata_json = ? WHERE id = ?", metaJSON, mem.ID)
	if err != nil {
		return fmt.Errorf("stamp metadata for %s: %w", mem.ID, err)
	}
	return nil
}
```

   **Verify during implementation:** confirm `epis.store.GetDB()` returns
   something with `ExecContext` (manager.go:1398 uses
   `m.episodic.store.GetDB()` identically) and that `MetadataJSON` is the
   serializer `EpisodicMemory.Store` itself uses (episodic.go:140) — if
   either differs, stop and report.

2. Now extend `MarkSuperseded` (`epistemic.go:458`). After the auto-cannot-
   supersede guard and BEFORE `markVersionNonCurrent`, insert:

```go
	// Temporal + revision stamping (plan: claim-temporal-validity).
	// The successor carries old rev + 1; the superseded claim's validity
	// window closes at the supersede instant. Both are stamped in place so
	// the graph edge targets stay valid.
	supersedeAt := time.Now().UTC()
	oldRev := claimRev(oldMem.Metadata)
	if newMem.Metadata == nil {
		newMem.Metadata = make(map[string]any)
	}
	newMem.Metadata["rev"] = oldRev + 1
	if err := m.stampMetadataInPlace(ctx, newMem); err != nil {
		return 0, "", fmt.Errorf("stamp successor rev: %w", err)
	}
	if oldMem.Metadata == nil {
		oldMem.Metadata = make(map[string]any)
	}
	oldMem.Metadata["valid_to"] = supersedeAt.Format(time.RFC3339)
	oldMem.Metadata["superseded_at"] = supersedeAt.Format(time.RFC3339)
	if err := m.stampMetadataInPlace(ctx, oldMem); err != nil {
		return 0, "", fmt.Errorf("stamp superseded validity: %w", err)
	}
```

3. Tests in `internal/memory/epistemic_test.go`:

```go
func TestMarkSupersededStampsRevAndValidity(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()
	oldID, err := m.StoreClaim(ctx, Claim{Text: "v1 claim", Status: ClaimStatusConfirmed, Rev: 2})
	if err != nil {
		t.Fatalf("store old: %v", err)
	}
	newID, err := m.StoreClaim(ctx, Claim{Text: "v2 claim", Status: ClaimStatusConfirmed})
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	if _, _, err := m.MarkSuperseded(ctx, oldID, newID); err != nil {
		t.Fatalf("MarkSuperseded: %v", err)
	}

	oldMem, err := m.GetByID(ctx, oldID)
	if err != nil {
		t.Fatalf("load old: %v", err)
	}
	if got := claimRev(oldMem.Metadata); got != 2 {
		t.Errorf("old claim rev = %d, want unchanged 2", got)
	}
	vtStr, ok := oldMem.Metadata["valid_to"].(string)
	if !ok {
		t.Fatalf("old claim valid_to missing/not string")
	}
	vt, err := time.Parse(time.RFC3339, vtStr)
	if err != nil {
		t.Fatalf("valid_to not RFC3339: %v", err)
	}
	if time.Since(vt) > time.Minute {
		t.Errorf("valid_to = %s, want ~now", vtStr)
	}
	if _, ok := oldMem.Metadata["superseded_at"]; !ok {
		t.Errorf("superseded_at missing")
	}

	newMem, err := m.GetByID(ctx, newID)
	if err != nil {
		t.Fatalf("load new: %v", err)
	}
	if got := claimRev(newMem.Metadata); got != 3 {
		t.Errorf("successor rev = %d, want 3 (old 2 + 1)", got)
	}

	// Backward-compat sibling: a claim with NO rev metadata supersedes into rev 1.
	legacyID, err := m.StoreClaim(ctx, Claim{Text: "legacy v1", Status: ClaimStatusConfirmed})
	if err != nil {
		t.Fatalf("store legacy: %v", err)
	}
	legacyNewID, err := m.StoreClaim(ctx, Claim{Text: "legacy v2", Status: ClaimStatusConfirmed})
	if err != nil {
		t.Fatalf("store legacy new: %v", err)
	}
	if _, _, err := m.MarkSuperseded(ctx, legacyID, legacyNewID); err != nil {
		t.Fatalf("MarkSuperseded legacy: %v", err)
	}
	legacyNew, err := m.GetByID(ctx, legacyNewID)
	if err != nil {
		t.Fatalf("load legacy new: %v", err)
	}
	if got := claimRev(legacyNew.Metadata); got != 1 {
		t.Errorf("legacy lineage successor rev = %d, want 1", got)
	}
}
```

4. Run: `go test -p 2 -run 'TestMarkSuperseded' ./internal/memory/`

### Task 5 — Storage verification (the metadata-JSON round trip)

1. Confirm (by reading, and note the confirmation in your final report):
   `internal/memory/episodic.go` `metadata_json TEXT NOT NULL DEFAULT '{}'`
   (:24) means NO schema migration is needed — new keys ride the existing
   JSON blob. If you find any code path that whitelists metadata keys on
   write or read (search for `"rev"`-style literal key filtering around the
   store/search path), report it; expected: none, keys pass through
   opaquely.
2. Confirm `EpisodicMemory.Store` (episodic.go:133-180) extracts only
   `parent_id`/`version`/`is_current` into SQL columns and stores everything
   else inside `metadata_json` — i.e., our new keys never touch SQL columns.
3. No code change expected in this task. `internal/memory/episodic.go` stays
   untouched unless step 1/2 reveals a filter — in which case STOP and report
   (it would violate the ≤3-file scope and need an orchestrator decision).

### Task 6 — Full leaf verification

```
go build ./...
go vet ./internal/memory/
go test -p 2 ./internal/memory/...
```

All green before reporting.

## Self-Verification Checklist

- [ ] `go build ./...` passes.
- [ ] `go test -p 2 ./internal/memory/...` passes (all pre-existing tests
      too — no regressions).
- [ ] New tests exist and run: `TestClaimTemporalFields`,
      `TestStoreClaimMetadataMapping`, `TestClaimInForce`, `TestClaimRev`,
      `TestMarkSupersededStampsRevAndValidity`.
- [ ] Existing `Claim` field order/comments unchanged (only appended).
- [ ] Zero-value `Claim` metadata output is byte-identical to pre-change
      (backward-compat case in Task 2 proves it).
- [ ] Supersede stamps are in place (same memory IDs before/after) — the
      Task 4 test loads by the SAME IDs.
- [ ] All `map[string]any` reads use two-value assertions.
- [ ] `stampMetadataInPlace` releases the lock before the DB write.
- [ ] Line-number-corruption rule: never pipe read_file output into
      write_file; locate edit sites with search_files/terminal grep against
      real content.
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator reviewing this leaf)

- [ ] Scope respected: only epistemic.go + epistemic_test.go changed
      (episodic.go untouched unless the documented stop-condition fired).
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] Two-value assertions everywhere on metadata reads.
- [ ] Error wrapping style matches house pattern (`fmt.Errorf("...: %w", err)`).
- [ ] Table-driven tests as specified; no test-only sleeps to fake timing —
      the valid_to window check uses a 1-minute tolerance, not a sleep.
- [ ] No ignored errors introduced (`go run ./tools/analyzers/...` /
      `make lint-ci` spot check on changed files).
- [ ] Task 5 storage verification findings reported (metadata-JSON pass-
      through confirmed or escalation raised).
- [ ] Do NOT commit. Do NOT run git add. (Orchestrator commits after review.)
