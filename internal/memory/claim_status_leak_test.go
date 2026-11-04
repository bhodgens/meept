package memory

// Probe tests (leaf 05-claim-status-stamping): write-through-real-store
// roundtrips verifying that MarkResolved / RecordReview do NOT leak stale
// pre-transition rows into Search. These are bug-fix-tier unit pins per the
// leaf spec — the transitions must stamp status in place (same pattern as
// the verdict path), because Search has no is_current filter.

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// newClaimStatusProbeManager mirrors newTestManager (epistemic_test.go) but
// stays local so this file is self-contained for the probe.
func newClaimStatusProbeManager(t *testing.T) *Manager {
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

// countTextMatches returns the Search results matching the exact text of the
// given memory (same shard/type query shape the epistemic list paths use).
func countTextMatches(t *testing.T, m *Manager, memType MemoryType, text string) []MemoryResult {
	t.Helper()
	res, err := m.Search(context.Background(), MemoryQuery{
		Type:  memType,
		Query: text,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var matches []MemoryResult
	for _, r := range res {
		if r.Memory.Content == text {
			matches = append(matches, r)
		}
	}
	return matches
}

// dumpRows logs each matching row (id + status + is_current) as probe
// evidence.
func dumpRows(t *testing.T, label string, rows []MemoryResult) {
	t.Helper()
	for _, r := range rows {
		t.Logf("%s: id=%s status=%q is_current=%v", label, r.Memory.ID,
			r.Memory.Metadata["status"], r.Memory.Metadata["is_current"])
	}
}

// TestClaimStatusMarkResolvedNoStaleRow probes MarkResolved: after resolving
// a prediction, a Search for its text must return exactly one row — the
// stamped one — never a stale pre-transition duplicate.
func TestClaimStatusMarkResolvedNoStaleRow(t *testing.T) {
	m := newClaimStatusProbeManager(t)
	ctx := context.Background()

	id, err := m.StorePrediction(ctx, Prediction{
		Forecast: "probe forecast: cache warms by tuesday",
		Horizon:  time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("StorePrediction: %v", err)
	}

	if _, err := m.MarkResolved(ctx, id, "hit"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}

	matches := countTextMatches(t, m, MemoryTypePrediction, "probe forecast: cache warms by tuesday")
	dumpRows(t, "markresolved row", matches)
	if len(matches) != 1 {
		t.Fatalf("LEAK CONFIRMED (MarkResolved): Search returned %d rows for the prediction text after resolve, want 1", len(matches))
	}
	if got := matches[0].Memory.Metadata["status"]; got != "resolved" {
		t.Fatalf("status = %v, want resolved", got)
	}
}

// TestClaimStatusRecordReviewNoStaleRow probes RecordReview: after reviewing
// a decision, a Search for its text must return exactly one row — the
// stamped one — never a stale pre-transition duplicate.
func TestClaimStatusRecordReviewNoStaleRow(t *testing.T) {
	m := newClaimStatusProbeManager(t)
	ctx := context.Background()

	id, err := m.StoreDecision(ctx, Decision{
		Call:            "probe decision: roll back the canary",
		ExpectedOutcome: "canary stays green",
	})
	if err != nil {
		t.Fatalf("StoreDecision: %v", err)
	}

	if _, _, err := m.RecordReview(ctx, id, "canary went red"); err != nil {
		t.Fatalf("RecordReview: %v", err)
	}

	matches := countTextMatches(t, m, MemoryTypeDecision, "probe decision: roll back the canary")
	dumpRows(t, "recordreview row", matches)
	if len(matches) != 1 {
		t.Fatalf("LEAK CONFIRMED (RecordReview): Search returned %d rows for the decision text after review, want 1", len(matches))
	}
	if got := matches[0].Memory.Metadata["status"]; got != "reviewed" {
		t.Fatalf("status = %v, want reviewed", got)
	}
}
