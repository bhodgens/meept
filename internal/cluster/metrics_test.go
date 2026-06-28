package cluster

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// TestNewMetrics_NonNil confirms NewMetrics returns a usable struct whose
// initial snapshot is all-zeros (atomic.Int64 zero value).
func TestNewMetrics_NonNil(t *testing.T) {
	m := NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
	snap := m.Snapshot()
	if snap.SessionTurnsPublished != 0 ||
		snap.MemoriesPublished != 0 ||
		snap.MergeConflicts != 0 ||
		snap.EventsReceived != 0 ||
		snap.EventsDeduped != 0 ||
		snap.ConflictResolutionsLocal != 0 ||
		snap.ConflictResolutionsRemote != 0 {
		t.Fatalf("expected zero snapshot, got %+v", snap)
	}
}

// TestNilMetrics_NoPanic exercises every IncX helper on a nil Metrics
// pointer. Every call site at the engine/handler layer nil-guards, but
// the helpers themselves must also be nil-safe so future callers don't
// need to duplicate the nil check.
func TestNilMetrics_NoPanic(t *testing.T) {
	var m *Metrics

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil Metrics panicked: %v", r)
		}
	}()

	m.IncSessionTurnPublished()
	m.IncMemoryPublished()
	m.IncMergeConflict()
	m.IncEventReceived()
	m.IncEventDeduped()
	m.IncConflictResolution("local")
	m.IncConflictResolution("remote")
	m.IncConflictResolution("bogus") // unknown winner must be silently ignored

	snap := m.Snapshot()
	if snap != (MetricsSnapshot{}) {
		t.Fatalf("nil snapshot should be zero-value, got %+v", snap)
	}

	// MetricsJSON on nil Metrics returns a fully-shaped zero snapshot so
	// HTTP/RPC consumers see a stable schema even when cluster metrics are
	// unwired (consistent with the http handler returning an empty object).
	got := m.MetricsJSON()
	var zeroSnap MetricsSnapshot
	if err := json.Unmarshal(got, &zeroSnap); err != nil {
		t.Fatalf("nil MetricsJSON produced invalid JSON: %v (raw: %s)", err, string(got))
	}
	if zeroSnap != (MetricsSnapshot{}) {
		t.Fatalf("nil MetricsJSON should unmarshal to zero snapshot, got %+v", zeroSnap)
	}
}

// TestMetricsIncrement verifies each IncX helper actually advances the
// corresponding counter, and that ConflictResolution labels route to the
// correct per-winner counter.
func TestMetricsIncrement(t *testing.T) {
	m := NewMetrics()

	m.IncSessionTurnPublished()
	m.IncSessionTurnPublished()
	m.IncMemoryPublished()
	m.IncMergeConflict()
	m.IncMergeConflict()
	m.IncMergeConflict()
	m.IncEventReceived()
	m.IncEventDeduped()
	m.IncEventDeduped()
	m.IncConflictResolution("local")
	m.IncConflictResolution("remote")
	m.IncConflictResolution("remote")

	snap := m.Snapshot()
	if snap.SessionTurnsPublished != 2 {
		t.Errorf("SessionTurnsPublished = %d, want 2", snap.SessionTurnsPublished)
	}
	if snap.MemoriesPublished != 1 {
		t.Errorf("MemoriesPublished = %d, want 1", snap.MemoriesPublished)
	}
	if snap.MergeConflicts != 3 {
		t.Errorf("MergeConflicts = %d, want 3", snap.MergeConflicts)
	}
	if snap.EventsReceived != 1 {
		t.Errorf("EventsReceived = %d, want 1", snap.EventsReceived)
	}
	if snap.EventsDeduped != 2 {
		t.Errorf("EventsDeduped = %d, want 2", snap.EventsDeduped)
	}
	if snap.ConflictResolutionsLocal != 1 {
		t.Errorf("ConflictResolutionsLocal = %d, want 1", snap.ConflictResolutionsLocal)
	}
	if snap.ConflictResolutionsRemote != 2 {
		t.Errorf("ConflictResolutionsRemote = %d, want 2", snap.ConflictResolutionsRemote)
	}
}

// TestMetricsJSON_Unmarshal confirms the JSON shape produced by MetricsJSON
// matches the documented metric names so external scrapers (and the
// /api/v1/cluster/metrics handler) see stable field names.
func TestMetricsJSON_Unmarshal(t *testing.T) {
	m := NewMetrics()
	m.IncSessionTurnPublished()
	m.IncMemoryPublished()

	var got MetricsSnapshot
	if err := json.Unmarshal(m.MetricsJSON(), &got); err != nil {
		t.Fatalf("MetricsJSON unmarshal failed: %v", err)
	}
	if got.SessionTurnsPublished != 1 || got.MemoriesPublished != 1 {
		t.Errorf("unexpected JSON snapshot: %+v", got)
	}
}

// TestWithMetrics_Option verifies the functional option and setter both
// attach Metrics to a GossipEngine, and that nil values are silently
// ignored (CLAUDE.md nil-guard rule).
func TestWithMetrics_Option(t *testing.T) {
	t.Run("option sets metrics", func(t *testing.T) {
		m := NewMetrics()
		g := newTestEngineWithMetrics(t, WithMetrics(m))
		if g.Metrics() != m {
			t.Fatal("WithMetrics did not attach Metrics to engine")
		}
	})

	t.Run("option ignores nil", func(t *testing.T) {
		g := newTestEngineWithMetrics(t, WithMetrics(nil))
		if g.Metrics() != nil {
			t.Fatal("WithMetrics(nil) attached a non-nil Metrics")
		}
	})

	t.Run("SetMetrics ignores nil", func(t *testing.T) {
		g := newTestEngineWithMetrics(t)
		g.SetMetrics(nil)
		if g.Metrics() != nil {
			t.Fatal("SetMetrics(nil) attached a non-nil Metrics")
		}
	})

	t.Run("SetMetrics attaches metrics", func(t *testing.T) {
		g := newTestEngineWithMetrics(t)
		m := NewMetrics()
		g.SetMetrics(m)
		if g.Metrics() != m {
			t.Fatal("SetMetrics did not attach Metrics")
		}
	})
}

// TestPublish_IncrementsCounters confirms Publish advances the
// SessionTurnsPublished / MemoriesPublished counters based on event type.
func TestPublish_IncrementsCounters(t *testing.T) {
	m := NewMetrics()
	g := newTestEngineWithMetrics(t, WithMetrics(m))

	// Publish a SESSION_TURN event — should bump SessionTurnsPublished.
	g.Publish(&models.ClusterEvent{
		EventType: models.EventTypeSessionTurn,
	})
	g.Publish(&models.ClusterEvent{
		EventType: models.EventTypeSessionTurn,
	})
	g.Publish(&models.ClusterEvent{
		EventType: models.EventTypeMemoryStored,
	})
	// An unrelated event type should not move any metric.
	g.Publish(&models.ClusterEvent{
		EventType: models.EventNodeHeartbeat,
	})

	snap := m.Snapshot()
	if snap.SessionTurnsPublished != 2 {
		t.Errorf("SessionTurnsPublished = %d, want 2", snap.SessionTurnsPublished)
	}
	if snap.MemoriesPublished != 1 {
		t.Errorf("MemoriesPublished = %d, want 1", snap.MemoriesPublished)
	}
}

// TestPublish_NilMetrics_NoPanic confirms Publish with no metrics attached
// doesn't panic — the production path when metrics wiring isn't wired.
func TestPublish_NilMetrics_NoPanic(t *testing.T) {
	g := newTestEngineWithMetrics(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Publish panicked with nil metrics: %v", r)
		}
	}()

	g.Publish(&models.ClusterEvent{EventType: models.EventTypeSessionTurn})
	g.Publish(&models.ClusterEvent{EventType: models.EventTypeMemoryStored})
}

// newTestEngineWithMetrics builds a minimal GossipEngine suitable for the
// metrics tests. It reuses NewGossipEngine so option wiring exercises the
// real production path.
func newTestEngineWithMetrics(t *testing.T, opts ...GossipOption) *GossipEngine {
	t.Helper()
	cfg := &Config{}
	cfg.setDefault()
	g := NewGossipEngine(cfg, "test-node", nil, testLogger(t), opts...)
	return g
}
