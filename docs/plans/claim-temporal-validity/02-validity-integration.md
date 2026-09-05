# Leaf 02 — Validity Enforcement in the Trust/Detection Path

<!-- DISPATCH INSTRUCTION
Dispatch as ONE implementation-agent session AFTER leaf 01 is merged into the
working tree (this leaf calls claimRev/claimInForce and ListExpiredClaims
depends on leaf 01 helpers). Go only. Files to touch are exactly the three
listed in Scope. The agent reads this leaf + master.md (Interface Contracts +
Coding Conventions) and the source files listed in Dependencies first.
-->

## Scope

Exactly three changed files:

1. `internal/memory/epistemic.go` — add `Manager.ListExpiredClaims`; add the
   in-force check to `FindCanonicalFor`.
2. `internal/memory/epistemic_detection.go` — add the in-force check to the
   detection candidate filter (the loop at :116-128 that already excludes
   rejected claims).
3. `internal/memory/epistemic_test.go` — table-driven tests for all of the
   above.

**No other files.** Survey-only (read, do not modify): `epistemic_ambient.go`
(AmbientExtractor/WriteCandidates flow) to confirm the ambient/skeptic path
needs no changes — its claims are new auto-claims with no validity metadata,
so they are in-force by construction (see Task 4).

## Dependencies

Read before coding:

- `docs/plans/claim-temporal-validity/master.md` — contracts C3, C4; the
  hard-exclude decision context.
- Leaf 01 in the working tree — `claimRev`, `claimInForce` (must exist; this
  leaf consumes them, never re-implements).
- `internal/memory/epistemic.go` — `ListAutoClaims` (:303-339, the pattern to
  mirror for ListExpiredClaims), `FindCanonicalFor` (:408-451), the
  initialized-guard idiom (:304-309).
- `internal/memory/epistemic_detection.go` — `DetectRelationships` pipeline
  (:88-131; candidate filter at :116-128).
- `internal/memory/epistemic_ambient.go` — `Extract` (:88), `WriteCandidates`
  (:109-129), and the ambient `StoreClaim` call (the path that mints
  auto-claims).
- `internal/memory/epistemic_test.go` + `epistemic_detection_test.go` —
  existing detector test scaffolding (fake classifier, in-memory manager
  construction) to reuse.

## Estimated context

~65K tokens (leaf doc + master + ~1,500 lines of Go reads + ~200 lines of
code/tests written).

## Interface Contract

Implements master contracts **C3 (consumption) + C4**:

- **C4 — exact signature:**

```go
// ListExpiredClaims returns non-rejected claims whose valid_to is in the past
// at call time, newest first, up to limit (default 20).
func (m *Manager) ListExpiredClaims(ctx context.Context, limit int) ([]MemoryResult, error)
```

- **C3 consumption policy (pinned):** expired = `!claimInForce(mem, now)`.
  Expiry is enforced by **hard exclusion** from candidate sets and canonical
  selection — not down-weighting — per the master recommendation (resolving
  OPEN-QUESTIONS Q2 is the orchestrator's call; this leaf implements
  hard-exclude because it is the documented recommendation with an explicit
  'expired' reason available to UI surfaces). Every exclusion the leaf adds
  is accompanied by an slog debug line carrying `claim_id` and
  `reason: "expired"` so any surface can show WHY a claim dropped out.

- Detection semantics preserved: rejected-claim exclusion
  (`epistemic_detection.go:122-126`) stays exactly as-is; the expired check
  is added alongside it, not merged into it.

## Tasks

### Task 1 — ListExpiredClaims (TDD)

1. Test first, in `internal/memory/epistemic_test.go`:

```go
func TestListExpiredClaims(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	rfc := func(t time.Time) string { return t.Format(time.RFC3339) }

	store := func(text string, extra map[string]any) string {
		id, err := m.StoreClaim(ctx, Claim{Text: text, Status: ClaimStatusConfirmed})
		if err != nil {
			t.Fatalf("store %q: %v", text, err)
		}
		// Task-4-style direct metadata injection: StoreClaim alone cannot
		// express "stored earlier with a now-past valid_to" without a real
		// clock, so stamp the window on the stored row (same in-place path
		// leaf 01 added).
		mem, err := m.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("load %q: %v", text, err)
		}
		for k, v := range extra {
			mem.Metadata[k] = v
		}
		if err := m.stampMetadataInPlace(ctx, mem); err != nil {
			t.Fatalf("stamp %q: %v", text, err)
		}
		return id
	}

	expiredID := store("expired claim", map[string]any{"valid_to": rfc(past)})
	_ = store("live claim", map[string]any{"valid_to": rfc(future)})
	_ = store("unbounded claim", nil)
	_ = store("expired but rejected", map[string]any{"valid_to": rfc(past), "status": string(ClaimStatusRejected)})

	got, err := m.ListExpiredClaims(ctx, 20)
	if err != nil {
		t.Fatalf("ListExpiredClaims: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1; got %+v", len(got), got)
	}
	if got[0].Memory.ID != expiredID {
		t.Errorf("got %s, want %s", got[0].Memory.ID, expiredID)
	}
}
```

2. Implementation in `internal/memory/epistemic.go`, directly below
   `ListAutoClaims` (mirrors its shape):

```go
// ListExpiredClaims returns non-rejected claims whose valid_to is in the past
// at call time, newest first, up to limit (default 20). Claims with no
// valid_to (unbounded) are never returned. Surfaced by the memory tool / CLI
// so users can see what has silently aged out of trust-weighted results.
func (m *Manager) ListExpiredClaims(ctx context.Context, limit int) ([]MemoryResult, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, errors.New("memory manager not initialized")
	}
	if limit <= 0 {
		limit = 20
	}
	results, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeClaim,
		Limit: limit * 4,
	})
	if err != nil {
		return nil, fmt.Errorf("search expired claims: %w", err)
	}
	var out []MemoryResult
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		if ClaimStatus(asString(r.Memory.Metadata["status"])).IsRejected() {
			continue
		}
		if claimInForce(r.Memory, time.Now()) {
			continue // not expired (includes unbounded claims)
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
```

3. Run: `go test -p 2 -run 'TestListExpiredClaims' ./internal/memory/`

### Task 2 — Expiry exclusion in the detector candidate filter (TDD)

1. Test in `internal/memory/epistemic_test.go` (reuse the fake classifier /
   manager scaffolding pattern from `epistemic_detection_test.go` — read that
   file and copy its construction, do not invent a new harness):

```go
func TestDetectRelationshipsExcludesExpiredCandidates(t *testing.T) {
	t.Parallel()
	// Build exactly as epistemic_detection_test.go does (fake classifier
	// capturing candidates), then store two claims: one expired, one live.
	// Assert the classifier received ONLY the live claim's memory as a
	// candidate.
	//
	// Scaffold sketch (fill in with the real harness types from
	// epistemic_detection_test.go):
	//   m := newTestManager(t)
	//   fc := &fakeClassifier{verdicts: nil} // captures candidates
	//   d := NewEpistemicDetector(EpistemicDetectorConfig{Manager: m, Classifier: fc, Graph: nil})
	//   ... store live claim + expired claim (metadata injection as Task 1) ...
	//   _, err := d.DetectRelationships(ctx, Memory{Type: MemoryTypeClaim, Content: "probe"})
	//   assert fc.sawCandidates contains the live claim ID and NOT the expired one
}
```

2. Implementation in `internal/memory/epistemic_detection.go`, inside the
   candidate loop (`:116-128`), alongside the existing rejected check — same
   block, one new stanza:

```go
		// Exclude expired/not-yet-in-force claims — they can't be
		// relationship targets either. Hard-exclude (plan: claim-temporal-
		// validity); log at debug so surfaces can explain the drop.
		if !claimInForce(r.Memory, time.Now()) {
			d.logger.Debug("detector candidate excluded: expired",
				"claim_id", r.Memory.ID, "reason", "expired")
			continue
		}
```

   (Add `"time"` to the file's imports. Note the existing function has no
   `now` parameter — `time.Now()` at call time is the pinned contract.)

3. Run: `go test -p 2 -run 'TestDetectRelationships' ./internal/memory/`

### Task 3 — Expiry exclusion in FindCanonicalFor (TDD)

1. Test in `internal/memory/epistemic_test.go`:

```go
func TestFindCanonicalForSkipsExpired(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	expiredID, err := m.StoreClaim(ctx, Claim{Text: "zap config", Status: ClaimStatusConfirmed})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// Stamp expired window on the only matching claim.
	mem, err := m.GetByID(ctx, expiredID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mem.Metadata["valid_to"] = past
	if err := m.stampMetadataInPlace(ctx, mem); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	_, err = m.FindCanonicalFor(ctx, "zap config")
	if err == nil {
		t.Fatalf("expected ErrNotFound for fully-expired topic, got a canonical claim")
	}
}
```

2. Implementation in `FindCanonicalFor` (`epistemic.go:408`): in BOTH loops
   (explicit `canonical_for` pass and eligible fallback pass), add before the
   eligibility check:

```go
		if !claimInForce(r.Memory, time.Now()) {
			continue // expired — never canonical
		}
```

3. Run: `go test -p 2 -run 'TestFindCanonicalFor' ./internal/memory/`

### Task 4 — Ambient/skeptic flow compatibility survey (read-only)

Verify and REPORT (no code changes):

1. `internal/memory/epistemic_ambient.go` `WriteCandidates` (:109-129) mints
   auto-claims via `StoreClaim` with no temporal metadata →
   `claimInForce` returns true for them → the ambient/skeptic path is
   compatible with zero changes. Confirm by reading.
2. Confirm `ListAutoClaims` (:303) does NOT need an expiry filter: it is a
   triage surface — an expired auto-claim still deserves triage visibility.
   If you disagree, report the argument; do not change the code.
3. Confirm nothing in the ambient path consumes `TrustWeight` directly in a
   way the expiry exclusion bypasses (search `TrustWeight` callers across
   `internal/memory/`). Report the caller list you found.

Findings go into the session report; orchestrator folds them into
OPEN-QUESTIONS.md if any assumption is violated.

### Task 5 — Full leaf verification

```
go build ./...
go vet ./internal/memory/
go test -p 2 ./internal/memory/...
```

All green before reporting. (Leaf 01's tests must still pass — this leaf
depends on its helpers.)

## Self-Verification Checklist

- [ ] `go build ./...` passes; `go test -p 2 ./internal/memory/...` passes
      with zero regressions.
- [ ] `ListExpiredClaims` matches contract C4 signature verbatim.
- [ ] Detector + canonical paths hard-exclude expired claims and log the
      `reason: "expired"` debug line.
- [ ] Rejected-claim filter in the detector unchanged.
- [ ] Rejected claims are excluded from ListExpiredClaims output.
- [ ] Unbounded claims (no valid_to) never appear in ListExpiredClaims and
      are never excluded from canonical/detection.
- [ ] Task 4 survey findings reported with file:line evidence.
- [ ] Line-number-corruption rule: never pipe read_file output into
      write_file; locate edit sites with search_files/terminal.
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (for the orchestrator reviewing this leaf)

- [ ] Scope respected: only epistemic.go + epistemic_detection.go +
      epistemic_test.go changed.
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] Two-value assertions on all metadata reads; `time` import added to
      epistemic_detection.go only if missing.
- [ ] Exclusions use `claimInForce` — no re-implemented window logic.
- [ ] Table-driven tests; no sleeps; deterministic fixtures.
- [ ] Survey findings (Task 4) present in the report with citations.
- [ ] Do NOT commit. Do NOT run git add. (Orchestrator commits after review.)
