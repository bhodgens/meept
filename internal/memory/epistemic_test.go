package memory

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// newTestManager constructs an initialized SQLite-backed Manager rooted in a
// temp dir, following the epistemic_sqlite_store_test.go construction pattern.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

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

func TestStoreClaimMetadataMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		claim      Claim
		wantKeys   map[string]bool // keys that MUST be present
		absentKeys []string        // keys that MUST be absent
	}{
		{
			name:       "zero-value temporal fields produce no new keys",
			claim:      Claim{Text: "legacy-shaped claim", Status: ClaimStatusConfirmed},
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
			name:     "rev>0 persists",
			claim:    Claim{Text: "successor", Rev: 4},
			wantKeys: map[string]bool{"rev": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t)
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

	// Load the superseded claim by the SAME ID. NOTE (leaf 01 review note):
	// GetByID cannot be used here — it filters "is_current = 1 OR
	// is_current = ''" (manager.go), and MarkSuperseded's pre-existing
	// markVersionNonCurrent sets is_current = 0 on the old row, so GetByID
	// returns ErrNotFound for any superseded claim regardless of stamping.
	// GetVersionHistory reads by the same ID with no is_current filter.
	history, err := m.GetVersionHistory(ctx, oldID)
	if err != nil {
		t.Fatalf("load old history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("version history for old claim = %d rows, want 1 (in-place stamp keeps the same row)", len(history))
	}
	oldMem := &history[0]
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

func TestClaimStatusTrustWeight(t *testing.T) {
	cases := []struct {
		status     ClaimStatus
		autoWeight float64
		want       float64
	}{
		{ClaimStatusConfirmed, 0.5, 1.0},
		{ClaimStatusPromoted, 0.5, 1.0},
		{ClaimStatusAuto, 0.5, 0.5},
		{ClaimStatusAuto, 0.0, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 1.5, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, -0.1, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 0.8, 0.8},
		{ClaimStatusRejected, 0.5, 0.0},
		{ClaimStatus("bogus"), 0.5, 0.0},
	}
	for _, c := range cases {
		got := c.status.TrustWeight(c.autoWeight)
		if got != c.want {
			t.Errorf("status=%q autoWeight=%v: got %v, want %v", c.status, c.autoWeight, got, c.want)
		}
	}
}

func TestEffectiveAutoTrustWeight(t *testing.T) {
	if EffectiveAutoTrustWeight(0) != DefaultAutoClaimTrustWeight {
		t.Error("zero value should yield default")
	}
	if EffectiveAutoTrustWeight(0.9) != 0.9 {
		t.Error("explicit value should pass through")
	}
	if EffectiveAutoTrustWeight(1.5) != DefaultAutoClaimTrustWeight {
		t.Error("out-of-range should yield default")
	}
}

func TestIsEpistemicType(t *testing.T) {
	for _, mt := range []MemoryType{MemoryTypeClaim, MemoryTypeDecision, MemoryTypePrediction, MemoryTypeQuestion} {
		if !IsEpistemicType(mt) {
			t.Errorf("%q should be epistemic", mt)
		}
	}
	if IsEpistemicType(MemoryTypeEpisodic) {
		t.Error("episodic should not be epistemic")
	}
}

func TestStoreClaimRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreClaim(context.Background(), Claim{Text: "x", Status: ClaimStatusConfirmed}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStoreDecisionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreDecision(context.Background(), Decision{Call: "x"}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStorePredictionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StorePrediction(context.Background(), Prediction{Forecast: "x", Horizon: time.Now()}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStoreQuestionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreQuestion(context.Background(), Question{Text: "x"}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestPromoteRejectUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if err := m.PromoteClaim(context.Background(), "x"); err == nil {
		t.Error("PromoteClaim should fail on uninitialized manager")
	}
	if err := m.RejectClaim(context.Background(), "x"); err == nil {
		t.Error("RejectClaim should fail on uninitialized manager")
	}
	if _, err := m.ListAutoClaims(context.Background(), time.Now(), 10); err == nil {
		t.Error("ListAutoClaims should fail on uninitialized manager")
	}
	if _, _, err := m.ListPendingReviews(context.Background(), time.Now()); err == nil {
		t.Error("ListPendingReviews should fail on uninitialized manager")
	}
	if _, err := m.FindCanonicalFor(context.Background(), "topic"); err == nil {
		t.Error("FindCanonicalFor should fail on uninitialized manager")
	}
}

func TestOutcomeOverlapScore(t *testing.T) {
	if got := outcomeOverlapScore("same", "same"); got != 1.0 {
		t.Errorf("identical: got %v, want 1.0", got)
	}
	if got := outcomeOverlapScore("a", "b"); got != 0.0 {
		t.Errorf("disjoint: got %v, want 0.0", got)
	}
}

func TestMarkSupersededUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.MarkSuperseded(context.Background(), "a", "b"); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestMarkResolvedUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.MarkResolved(context.Background(), "x", "outcome"); err == nil {
		t.Error("expected error")
	}
}

func TestRecordReviewUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.RecordReview(context.Background(), "x", "actual"); err == nil {
		t.Error("expected error")
	}
}

func TestManagerSetEpistemicDetector(t *testing.T) {
	m := NewManager(ManagerConfig{})
	// Setter should accept nil without panic (defense in depth).
	m.SetEpistemicDetector(nil)
	// Setter should accept a real detector.
	d := NewEpistemicDetector(EpistemicDetectorConfig{})
	m.SetEpistemicDetector(d)
}

// fakeClassifier implements ClassifierLLM for tests, capturing the candidate
// memories it receives so tests can assert on the detector's filtering.
type fakeClassifier struct {
	sawCandidates []Memory
}

func (f *fakeClassifier) ClassifyRelationships(_ context.Context, _ Memory, candidates []Memory) ([]EdgeVerdict, error) {
	f.sawCandidates = append(f.sawCandidates, candidates...)
	return nil, nil
}

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
		// Direct metadata injection: StoreClaim alone cannot express
		// "stored earlier with a now-past valid_to" without a real clock,
		// so stamp the window on the stored row (same in-place path leaf 01
		// added).
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

func TestListExpiredClaimsUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.ListExpiredClaims(context.Background(), 10); err == nil {
		t.Error("ListExpiredClaims should fail on uninitialized manager")
	}
}

func TestDetectRelationshipsExcludesExpiredCandidates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		validTo       time.Time // zero = unbounded
		wantCandidate bool
	}{
		{"live candidate passes", time.Now().UTC().Add(time.Hour), true},
		{"expired candidate excluded", time.Now().UTC().Add(-time.Hour), false},
		{"unbounded candidate passes", time.Time{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t)
			ctx := context.Background()

			// Fixture text shares every token with the probe content so the
			// candidate is found by both the FTS5 (AND) and LIKE search
			// backends.
			id, err := m.StoreClaim(ctx, Claim{Text: "zapneon probe fact", Status: ClaimStatusConfirmed})
			if err != nil {
				t.Fatalf("store: %v", err)
			}
			if !tc.validTo.IsZero() {
				mem, err := m.GetByID(ctx, id)
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				mem.Metadata["valid_to"] = tc.validTo.Format(time.RFC3339)
				if err := m.stampMetadataInPlace(ctx, mem); err != nil {
					t.Fatalf("stamp: %v", err)
				}
			}

			fc := &fakeClassifier{}
			d := NewEpistemicDetector(EpistemicDetectorConfig{Manager: m, Classifier: fc})
			if _, err := d.DetectRelationships(ctx, Memory{
				ID:      "probe-1",
				Type:    MemoryTypeClaim,
				Content: "zapneon probe",
			}); err != nil {
				t.Fatalf("DetectRelationships: %v", err)
			}
			saw := false
			for _, c := range fc.sawCandidates {
				if c.ID == id {
					saw = true
					break
				}
			}
			if saw != tc.wantCandidate {
				t.Fatalf("candidate %s seen = %v, want %v (saw %d candidates)",
					id, saw, tc.wantCandidate, len(fc.sawCandidates))
			}
		})
	}
}

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

func TestFindCanonicalForUnboundedStillEligible(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()

	// No validity metadata: unbounded claims must remain canonical-eligible.
	id, err := m.StoreClaim(ctx, Claim{Text: "quixote config", Status: ClaimStatusConfirmed})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	got, err := m.FindCanonicalFor(ctx, "quixote config")
	if err != nil {
		t.Fatalf("FindCanonicalFor: %v", err)
	}
	if got.ID != id {
		t.Errorf("canonical = %s, want %s", got.ID, id)
	}
}
