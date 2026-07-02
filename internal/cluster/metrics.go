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

	// --- Dispatch + CAS + peer counters (spec §8) ---

	// DispatchJobsReceived counts TASK_CREATE events accepted by the
	// ExecutorBridge (the local node is the target).
	DispatchJobsReceived atomic.Int64

	// DispatchJobsCompleted counts dispatch jobs that reached completeJob.
	DispatchJobsCompleted atomic.Int64

	// DispatchJobsFailed counts dispatch jobs that reached failJob.
	DispatchJobsFailed atomic.Int64

	// CASHits counts ResourceManager cache hits (Ensure found the blob
	// locally without peer fetch).
	CASHits atomic.Int64

	// CASMisses counts ResourceManager cache misses.
	CASMisses atomic.Int64

	// CASBytesFetched counts total bytes pulled from peers via
	// PeerFetcher.Fetch.
	CASBytesFetched atomic.Int64

	// CASBytesEvicted counts total bytes reclaimed by eviction sweeps.
	CASBytesEvicted atomic.Int64

	// CASRefcountZeroEligible counts entries whose refcount hit zero and
	// became eviction-eligible.
	CASRefcountZeroEligible atomic.Int64

	// PeerUnreachable counts resource Ensure failures where no peer could
	// supply the blob (ErrResourceUnavailable).
	PeerUnreachable atomic.Int64

	// PeerCorruptionIncidents counts hash-mismatch events from a peer
	// (ErrResourceCorrupt).
	PeerCorruptionIncidents atomic.Int64

	// PeerQuarantined counts the number of times a peer was quarantined
	// (spec §6 security boundaries: N corruption incidents within a
	// window).
	PeerQuarantined atomic.Int64
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

	// Dispatch + CAS + peer counters (spec §8).
	DispatchJobsReceived     int64 `json:"dispatch_jobs_received_total"`
	DispatchJobsCompleted    int64 `json:"dispatch_jobs_completed_total"`
	DispatchJobsFailed       int64 `json:"dispatch_jobs_failed_total"`
	CASHits                  int64 `json:"cas_hits_total"`
	CASMisses                int64 `json:"cas_misses_total"`
	CASBytesFetched          int64 `json:"cas_bytes_fetched_total"`
	CASBytesEvicted          int64 `json:"cas_bytes_evicted_total"`
	CASRefcountZeroEligible  int64 `json:"cas_refcount_zero_eligible_total"`
	PeerUnreachable          int64 `json:"peer_unreachable_total"`
	PeerCorruptionIncidents  int64 `json:"peer_corruption_incidents_total"`
	PeerQuarantined          int64 `json:"peer_quarantined_total"`
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
		DispatchJobsReceived:      m.DispatchJobsReceived.Load(),
		DispatchJobsCompleted:     m.DispatchJobsCompleted.Load(),
		DispatchJobsFailed:        m.DispatchJobsFailed.Load(),
		CASHits:                   m.CASHits.Load(),
		CASMisses:                 m.CASMisses.Load(),
		CASBytesFetched:           m.CASBytesFetched.Load(),
		CASBytesEvicted:           m.CASBytesEvicted.Load(),
		CASRefcountZeroEligible:   m.CASRefcountZeroEligible.Load(),
		PeerUnreachable:           m.PeerUnreachable.Load(),
		PeerCorruptionIncidents:   m.PeerCorruptionIncidents.Load(),
		PeerQuarantined:           m.PeerQuarantined.Load(),
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

// --- Dispatch + CAS + peer counter helpers (spec §8). Nil-safe. ---

// IncDispatchJobsReceived increments the dispatch_jobs_received counter.
func (m *Metrics) IncDispatchJobsReceived() {
	if m != nil {
		m.DispatchJobsReceived.Add(1)
	}
}

// IncDispatchJobsCompleted increments the dispatch_jobs_completed counter.
func (m *Metrics) IncDispatchJobsCompleted() {
	if m != nil {
		m.DispatchJobsCompleted.Add(1)
	}
}

// IncDispatchJobsFailed increments the dispatch_jobs_failed counter.
func (m *Metrics) IncDispatchJobsFailed() {
	if m != nil {
		m.DispatchJobsFailed.Add(1)
	}
}

// IncCASHits increments the cas_hits counter.
func (m *Metrics) IncCASHits() {
	if m != nil {
		m.CASHits.Add(1)
	}
}

// IncCASMisses increments the cas_misses counter.
func (m *Metrics) IncCASMisses() {
	if m != nil {
		m.CASMisses.Add(1)
	}
}

// AddCASBytesFetched adds n to the cas_bytes_fetched counter.
func (m *Metrics) AddCASBytesFetched(n int64) {
	if m != nil {
		m.CASBytesFetched.Add(n)
	}
}

// AddCASBytesEvicted adds n to the cas_bytes_evicted counter.
func (m *Metrics) AddCASBytesEvicted(n int64) {
	if m != nil {
		m.CASBytesEvicted.Add(n)
	}
}

// IncCASRefcountZeroEligible increments the cas_refcount_zero_eligible counter.
func (m *Metrics) IncCASRefcountZeroEligible() {
	if m != nil {
		m.CASRefcountZeroEligible.Add(1)
	}
}

// IncPeerUnreachable increments the peer_unreachable counter.
func (m *Metrics) IncPeerUnreachable() {
	if m != nil {
		m.PeerUnreachable.Add(1)
	}
}

// IncPeerCorruptionIncidents increments the peer_corruption_incidents counter.
func (m *Metrics) IncPeerCorruptionIncidents() {
	if m != nil {
		m.PeerCorruptionIncidents.Add(1)
	}
}

// IncPeerQuarantined increments the peer_quarantined counter.
func (m *Metrics) IncPeerQuarantined() {
	if m != nil {
		m.PeerQuarantined.Add(1)
	}
}
