package cluster

import (
	"encoding/json"
	"sync/atomic"
)

// Metrics holds gossip-engine observability counters. All fields are safe
// for concurrent use via atomic operations.
//
// The counters intentionally use sync/atomic.Int64 rather than a heavy
// dependency like prometheus/client_golang (not in go.mod). The Snapshot
// method exposes the values for any exporter (HTTP handler, log spam, etc.)
// and MetricsJSON renders them directly for the /api/v1/cluster/metrics
// endpoint.
type Metrics struct {
	// SessionTurnsPublished counts locally-published SESSION_TURN events.
	SessionTurnsPublished atomic.Int64

	// MemoriesPublished counts locally-published MEMORY_STORED events.
	MemoriesPublished atomic.Int64

	// MergeConflicts counts the number of entity-level conflicts observed
	// by a registered ConflictResolver (incoming event matched an existing
	// record for the same resource).
	MergeConflicts atomic.Int64

	// EventsReceived counts events received from peer nodes (post-dedup).
	EventsReceived atomic.Int64

	// EventsDeduped counts events dropped because they were already seen
	// (dedup cache hit).
	EventsDeduped atomic.Int64

	// ConflictResolutionsLocal counts conflicts where the local event won.
	ConflictResolutionsLocal atomic.Int64

	// ConflictResolutionsRemote counts conflicts where the remote event won.
	ConflictResolutionsRemote atomic.Int64
}

// NewMetrics returns a zeroed Metrics struct ready for use. atomic.Int64
// has a usable zero value, so this is primarily for explicit construction.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// MetricsSnapshot is an immutable copy of Metrics fields suitable for JSON
// serialization without worrying about concurrent mutation.
type MetricsSnapshot struct {
	SessionTurnsPublished     int64 `json:"session_turns_published_total"`
	MemoriesPublished         int64 `json:"memories_published_total"`
	MergeConflicts            int64 `json:"merge_conflicts_total"`
	EventsReceived            int64 `json:"events_received_total"`
	EventsDeduped             int64 `json:"events_deduped_total"`
	ConflictResolutionsLocal  int64 `json:"conflict_resolutions_local_total"`
	ConflictResolutionsRemote int64 `json:"conflict_resolutions_remote_total"`
}

// Snapshot returns an immutable copy of the current counter values.
func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		SessionTurnsPublished:     m.SessionTurnsPublished.Load(),
		MemoriesPublished:         m.MemoriesPublished.Load(),
		MergeConflicts:            m.MergeConflicts.Load(),
		EventsReceived:            m.EventsReceived.Load(),
		EventsDeduped:             m.EventsDeduped.Load(),
		ConflictResolutionsLocal:  m.ConflictResolutionsLocal.Load(),
		ConflictResolutionsRemote: m.ConflictResolutionsRemote.Load(),
	}
}

// MetricsJSON returns the snapshot marshaled as indented JSON. Returns "null"
// when m is nil (the nil-metrics case is a no-op at the engine level, so this
// helper is only invoked from a handler that already checked for nil).
func (m *Metrics) MetricsJSON() []byte {
	snap := m.Snapshot()
	b, _ := json.MarshalIndent(snap, "", "  ")
	return b
}

// IncSessionTurnPublished increments the SESSION_TURN publish counter.
// Nil-safe.
func (m *Metrics) IncSessionTurnPublished() {
	if m != nil {
		m.SessionTurnsPublished.Add(1)
	}
}

// IncMemoryPublished increments the MEMORY_STORED publish counter.
// Nil-safe.
func (m *Metrics) IncMemoryPublished() {
	if m != nil {
		m.MemoriesPublished.Add(1)
	}
}

// IncMergeConflict increments the merge-conflict counter. Nil-safe.
func (m *Metrics) IncMergeConflict() {
	if m != nil {
		m.MergeConflicts.Add(1)
	}
}

// IncEventReceived increments the events-received counter. Nil-safe.
func (m *Metrics) IncEventReceived() {
	if m != nil {
		m.EventsReceived.Add(1)
	}
}

// IncEventDeduped increments the events-deduped counter. Nil-safe.
func (m *Metrics) IncEventDeduped() {
	if m != nil {
		m.EventsDeduped.Add(1)
	}
}

// IncConflictResolution increments the per-winner conflict-resolution
// counter. winner must be "local" or "remote"; unknown values are ignored
// to keep the cardinality bounded. Nil-safe.
func (m *Metrics) IncConflictResolution(winner string) {
	if m == nil {
		return
	}
	switch winner {
	case "local":
		m.ConflictResolutionsLocal.Add(1)
	case "remote":
		m.ConflictResolutionsRemote.Add(1)
	default:
		// Unknown winner label — silently ignored to prevent cardinality
		// explosions in the (currently atomic-only) counters.
	}
}
