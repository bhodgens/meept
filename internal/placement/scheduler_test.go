package placement

import (
	"testing"
	"time"
)

// --- UpdateNode / RemoveNode ---

func TestPlacementScheduler_UpdateAndRemoveNode(t *testing.T) {
	s := NewPlacementScheduler("local", nil)

	if got := s.NodeCount(); got != 0 {
		t.Fatalf("initial NodeCount: want 0, got %d", got)
	}

	s.UpdateNode(NodeInfo{
		NodeID:   "node-A",
		Capacity: 2,
		Active:   true,
	})
	if got := s.NodeCount(); got != 1 {
		t.Fatalf("after UpdateNode: want 1, got %d", got)
	}

	s.RemoveNode("node-A")
	if got := s.NodeCount(); got != 0 {
		t.Fatalf("after RemoveNode: want 0, got %d", got)
	}

	// Idempotent remove.
	s.RemoveNode("node-A")
	if got := s.NodeCount(); got != 0 {
		t.Fatalf("after double RemoveNode: want 0, got %d", got)
	}
}

func TestPlacementScheduler_UpdateNode_EmptyID(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{NodeID: "", Capacity: 1})
	if got := s.NodeCount(); got != 0 {
		t.Fatalf("empty-ID UpdateNode should be ignored, got %d", got)
	}
}

// CachedHashes defensive copy.
func TestPlacementScheduler_UpdateNode_HashesDefensiveCopy(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	hashes := []string{"blake3:abc"}
	s.UpdateNode(NodeInfo{
		NodeID:       "node-A",
		Capacity:     1,
		Active:       true,
		CachedHashes: hashes,
		LastSeen:     time.Now(),
	})

	// Mutate caller's slice.
	hashes[0] = "blake3:corrupted"

	dec := s.Decide(PlacementRequest{
		RequiredResources: []string{"blake3:abc"},
	})
	if dec.TargetNode != "node-A" {
		t.Fatalf("expect node-A to remain match, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
}

// --- PreferredNode hint ---

func TestPlacementDecision_PreferredNode(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{NodeID: "node-A", Capacity: 1, Active: true, LastSeen: time.Now()})
	s.UpdateNode(NodeInfo{NodeID: "node-B", Capacity: 5, Active: true, LastSeen: time.Now()})

	dec := s.Decide(PlacementRequest{
		PreferredNode: "node-A",
	})
	if dec.TargetNode != "node-A" {
		t.Fatalf("preferred node hint should win, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
	if dec.Reason != "preferred_node" {
		t.Fatalf("reason want preferred_node, got %s", dec.Reason)
	}
}

func TestPlacementDecision_PreferredNodeNotSuitable_FallsThrough(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	// node-A exists but with no capacity.
	s.UpdateNode(NodeInfo{NodeID: "node-A", Capacity: 0, Active: true, LastSeen: time.Now()})
	// node-B exists with capacity.
	s.UpdateNode(NodeInfo{NodeID: "node-B", Capacity: 3, Active: true, LastSeen: time.Now()})

	dec := s.Decide(PlacementRequest{
		PreferredNode: "node-A",
	})
	if dec.TargetNode != "node-B" {
		t.Fatalf("preferred node not suitable → fallback, want node-B, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
}

func TestPlacementDecision_PreferredNodeUnknown_FallsThrough(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{NodeID: "node-A", Capacity: 1, Active: true, LastSeen: time.Now()})

	dec := s.Decide(PlacementRequest{
		PreferredNode: "node-ghost",
	})
	if dec.TargetNode != "node-A" {
		t.Fatalf("unknown preferred → fallback to node-A, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
}

// --- Cache locality ---

func TestPlacementDecision_CacheLocalityWins(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{
		NodeID:       "node-A",
		Capacity:     5, // more capacity
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:other"},
	})
	s.UpdateNode(NodeInfo{
		NodeID:       "node-B",
		Capacity:     2,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:needed"},
	})

	dec := s.Decide(PlacementRequest{
		RequiredResources: []string{"blake3:needed"},
	})
	if dec.TargetNode != "node-B" {
		t.Fatalf("cache locality should beat capacity, want node-B, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
	if dec.Reason != "cache_locality" {
		t.Fatalf("reason want cache_locality, got %s", dec.Reason)
	}
}

func TestPlacementDecision_CacheLocalityTiebreakByCapacity(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	// Two nodes with same cache score; node-B has more capacity.
	s.UpdateNode(NodeInfo{
		NodeID:       "node-A",
		Capacity:     1,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:needed"},
	})
	s.UpdateNode(NodeInfo{
		NodeID:       "node-B",
		Capacity:     5,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:needed"},
	})

	dec := s.Decide(PlacementRequest{
		RequiredResources: []string{"blake3:needed"},
	})
	if dec.TargetNode != "node-B" {
		t.Fatalf("tiebreak should pick higher capacity, want node-B, got %q", dec.TargetNode)
	}
}

func TestPlacementDecision_CacheLocalityTiebreakByNodeID(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	// Two nodes with same cache score and same capacity; lower NodeID wins.
	s.UpdateNode(NodeInfo{
		NodeID:       "node-Z",
		Capacity:     3,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:needed"},
	})
	s.UpdateNode(NodeInfo{
		NodeID:       "node-A",
		Capacity:     3,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:needed"},
	})

	dec := s.Decide(PlacementRequest{
		RequiredResources: []string{"blake3:needed"},
	})
	if dec.TargetNode != "node-A" {
		t.Fatalf("lexicographic tiebreak: want node-A, got %q", dec.TargetNode)
	}
}

// --- Capacity-only (no cache overlap) ---

func TestPlacementDecision_CapacityOnly(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{
		NodeID:       "node-A",
		Capacity:     5,
		Active:       true,
		LastSeen:     time.Now(),
		CachedHashes: []string{"blake3:other"},
	})
	s.UpdateNode(NodeInfo{
		NodeID:   "node-B",
		Capacity: 2,
		Active:   true,
		LastSeen: time.Now(),
	})

	dec := s.Decide(PlacementRequest{
		RequiredResources: []string{"blake3:needed"},
	})
	if dec.TargetNode != "node-A" {
		t.Fatalf("capacity-only decision: want node-A (5 slots), got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
	if dec.Reason != "capacity" {
		t.Fatalf("reason want capacity, got %s", dec.Reason)
	}
}

// --- No capacity policy fallbacks ---

func TestPlacementDecision_NoCapacity_Queue(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	// No peers registered.
	dec := s.Decide(PlacementRequest{})
	if dec.Local {
		t.Fatalf("queue policy: Local should be false")
	}
	if dec.Reason != "no_capacity_queue" {
		t.Fatalf("reason want no_capacity_queue, got %s", dec.Reason)
	}
	if dec.TargetNode != "" {
		t.Fatalf("queue policy: TargetNode should be empty, got %q", dec.TargetNode)
	}
}

func TestPlacementDecision_NoCapacity_RunLocal(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.SetPolicy(PolicyRunLocal)

	dec := s.Decide(PlacementRequest{})
	if !dec.Local {
		t.Fatalf("run_local policy: Local should be true")
	}
	if dec.Reason != "no_capacity_run_local" {
		t.Fatalf("reason want no_capacity_run_local, got %s", dec.Reason)
	}
}

func TestPlacementDecision_NoCapacity_InactiveNodesFiltered(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{NodeID: "node-A", Capacity: 5, Active: false, LastSeen: time.Now()})

	dec := s.Decide(PlacementRequest{})
	if dec.TargetNode != "" {
		t.Fatalf("inactive node should be filtered, got %q", dec.TargetNode)
	}
	if dec.Reason != "no_capacity_queue" {
		t.Fatalf("reason want no_capacity_queue, got %s", dec.Reason)
	}
}

func TestPlacementDecision_NoCapacity_ZeroCapacityFiltered(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{NodeID: "node-A", Capacity: 0, Active: true, LastSeen: time.Now()})

	dec := s.Decide(PlacementRequest{})
	if dec.TargetNode != "" {
		t.Fatalf("zero-capacity node should be filtered, got %q", dec.TargetNode)
	}
}

// --- Staleness ---

func TestPlacementDecision_StaleNodeFiltered(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{
		NodeID:   "node-A",
		Capacity: 5,
		Active:   true,
		// LastSeen old enough to be stale.
		LastSeen: time.Now().Add(-staleNodeTTL - time.Minute),
	})

	dec := s.Decide(PlacementRequest{})
	if dec.TargetNode != "" {
		t.Fatalf("stale node should be filtered, got %q", dec.TargetNode)
	}
}

func TestPlacementDecision_FreshLastSeen(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{
		NodeID:   "node-A",
		Capacity: 1,
		Active:   true,
		LastSeen: time.Now(),
	})

	dec := s.Decide(PlacementRequest{})
	if dec.TargetNode != "node-A" {
		t.Fatalf("fresh node should be a candidate, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
}

// --- Local node excluded ---

func TestPlacementDecision_LocalNodeExcluded(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	s.UpdateNode(NodeInfo{
		NodeID:   "local",
		Capacity: 99,
		Active:   true,
		LastSeen: time.Now(),
	})

	dec := s.Decide(PlacementRequest{})
	if dec.TargetNode != "" {
		t.Fatalf("local node should not be a placement target, got %q", dec.TargetNode)
	}
}

// --- SetPolicy / SetFallback validation ---

func TestSetPolicy_Validation(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	if got := s.Policy(); got != PolicyQueue {
		t.Fatalf("default policy: want queue, got %s", got)
	}

	s.SetPolicy(PolicyRunLocal)
	if got := s.Policy(); got != PolicyRunLocal {
		t.Fatalf("after SetPolicy(run_local): want run_local, got %s", got)
	}

	// Unknown value rejected.
	s.SetPolicy("bogus")
	if got := s.Policy(); got != PolicyRunLocal {
		t.Fatalf("unknown policy should be rejected, want run_local, got %s", got)
	}

	// Empty value rejected.
	s.SetPolicy("")
	if got := s.Policy(); got != PolicyRunLocal {
		t.Fatalf("empty policy should be rejected, want run_local, got %s", got)
	}
}

func TestSetFallback_Validation(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	if got := s.Fallback(); got != FallbackIfCapacity {
		t.Fatalf("default fallback: want if_capacity, got %s", got)
	}

	s.SetFallback(FallbackAlways)
	if got := s.Fallback(); got != FallbackAlways {
		t.Fatalf("after SetFallback(always): want always, got %s", got)
	}

	s.SetFallback("bogus")
	if got := s.Fallback(); got != FallbackAlways {
		t.Fatalf("unknown fallback should be rejected, want always, got %s", got)
	}
}

// --- MaxNodes cap ---

func TestPlacementDecision_MaxNodesCap(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	for _, id := range []string{"a", "b", "c", "d"} {
		s.UpdateNode(NodeInfo{
			NodeID:   id,
			Capacity: 1,
			Active:   true,
			LastSeen: time.Now(),
		})
	}

	// MaxNodes=2 — only two candidates considered. Since none of them
	// match a preferred node hint, the decision still picks one (we
	// can't predict which from this test alone, but the result is a
	// non-empty target).
	dec := s.Decide(PlacementRequest{
		MaxNodes: 2,
	})
	if dec.TargetNode == "" {
		t.Fatalf("MaxNodes=2 with 4 available: should still pick one, got empty")
	}
}

// --- First registration without heartbeat ---

func TestPlacementDecision_FirstRegistrationZeroLastSeen(t *testing.T) {
	s := NewPlacementScheduler("local", nil)
	// Active=true, Capacity>0, but LastSeen is zero-value.
	s.UpdateNode(NodeInfo{
		NodeID:   "node-A",
		Capacity: 1,
		Active:   true,
		// LastSeen left zero.
	})

	dec := s.Decide(PlacementRequest{})
	// Zero LastSeen should NOT be treated as stale (first registration).
	if dec.TargetNode != "node-A" {
		t.Fatalf("first registration with zero LastSeen should be eligible, got %q (reason=%s)",
			dec.TargetNode, dec.Reason)
	}
}
