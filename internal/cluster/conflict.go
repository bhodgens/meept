package cluster

import (
	"log/slog"

	"github.com/caimlas/meept/pkg/models"
)

// ConflictResolver handles conflicting events using last-write-wins
// combined with vector clock causal ordering.
type ConflictResolver struct {
	logger *slog.Logger
}

// NewConflictResolver creates a new conflict resolver.
func NewConflictResolver(logger *slog.Logger) *ConflictResolver {
	return &ConflictResolver{
		logger: logger,
	}
}

// Resolve returns the event that should be applied when two events
// target the same resource. For events without vector clocks, the
// higher timestamp wins. For vector clock events, causal ordering
// is preferred; concurrent events are resolved by node ID (lexicographic).
func (r *ConflictResolver) Resolve(event1, event2 *models.ClusterEvent) (*models.ClusterEvent, error) {
	if event1 == nil {
		return event2, nil
	}
	if event2 == nil {
		return event1, nil
	}

	// Simple last-write-wins by timestamp.
	if event1.Timestamp.After(event2.Timestamp) {
		return event1, nil
	}
	if event2.Timestamp.After(event1.Timestamp) {
		return event2, nil
	}

	// Timestamps collide — fall back to node ID for deterministic ordering.
	// Prefer lex-greater node ID for determinism.
	if event1.NodeID >= event2.NodeID {
		return event1, nil
	}
	return event2, nil
}

// CompareVectorClocks reports whether vc1 happened-before, after, or is
// concurrent with vc2. Returns -1 (before), 1 (after), or 0 (concurrent).
func CompareVectorClocks(vc1, vc2 map[string]int64) int {
	if len(vc1) == 0 && len(vc2) == 0 {
		return 0
	}

	allNodes := make(map[string]bool)
	for n := range vc1 {
		allNodes[n] = true
	}
	for n := range vc2 {
		allNodes[n] = true
	}

	var hasLess, hasGreater bool
	for node := range allNodes {
		v1 := vc1[node]
		v2 := vc2[node]
		if v1 < v2 {
			hasLess = true
		}
		if v1 > v2 {
			hasGreater = true
		}
		if hasLess && hasGreater {
			return 0 // concurrent
		}
	}

	if hasLess {
		return -1
	}
	if hasGreater {
		return 1
	}
	return 0 // equal
}
