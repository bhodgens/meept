package placement

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// staleNodeTTL is how long since LastSeen a node is considered Active
// before heartbeat-driven UpdateNode refreshes it. Callers may use
// RemoveNode to force a sooner removal.
//
// Note: this is only applied at observation time (Decide) — there's no
// background goroutine sweeping stale nodes. That's deliberate: the
// caller (daemon) already runs a heartbeat pacer.
const staleNodeTTL = 90 * time.Second

// PlacementScheduler is the cluster-aware scheduler (spec §4.5, §2.3 β).
// It consumes heartbeat metadata and emits PlacementDecisions.
//
// PlacementScheduler is safe for concurrent use. All I/O (none in this
// package) is the caller's responsibility — this scheduler is pure
// in-memory policy.
type PlacementScheduler struct {
	localNodeID string
	logger      *slog.Logger
	policy      string // "queue" (default) or "run_local"
	fallback    string // "always" | "never" | "if_capacity" (peer_fallback_policy)

	mu    sync.RWMutex
	nodes map[string]*NodeInfo // nodeID -> latest heartbeat info
}

// NewPlacementScheduler constructs a scheduler with the given local node ID
// and default policies ("queue" + "if_capacity").
func NewPlacementScheduler(localNodeID string, logger *slog.Logger) *PlacementScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlacementScheduler{
		localNodeID: localNodeID,
		logger:      logger,
		policy:      PolicyQueue,
		fallback:    FallbackIfCapacity,
		nodes:       make(map[string]*NodeInfo),
	}
}

// SetPolicy sets the scheduler_no_capacity_policy. Accepts "queue" or
// "run_local"; unknown values are ignored to keep the decision surface
// bounded. Nil-equivalent guard: empty string is ignored.
func (s *PlacementScheduler) SetPolicy(p string) {
	switch p {
	case PolicyQueue, PolicyRunLocal:
		s.mu.Lock()
		s.policy = p
		s.mu.Unlock()
	default:
		// Ignore unknown values.
	}
}

// SetFallback sets the peer_fallback_policy. Accepts "always", "never",
// "if_capacity"; unknown values are ignored.
func (s *PlacementScheduler) SetFallback(f string) {
	switch f {
	case FallbackAlways, FallbackNever, FallbackIfCapacity:
		s.mu.Lock()
		s.fallback = f
		s.mu.Unlock()
	default:
		// Ignore unknown values.
	}
}

// Policy returns the current scheduler_no_capacity_policy value.
func (s *PlacementScheduler) Policy() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

// Fallback returns the current peer_fallback_policy value.
func (s *PlacementScheduler) Fallback() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fallback
}

// UpdateNode records or refreshes heartbeat metadata for a node. If
// info.NodeID is empty the call is a no-op. The local node is allowed
// (callers may include it in heartbeats); Decide filters it out as a
// placement candidate.
func (s *PlacementScheduler) UpdateNode(info NodeInfo) {
	if info.NodeID == "" {
		return
	}
	// Defensive copy of the CachedHashes slice so a caller mutation
	// after UpdateNode doesn't corrupt our state.
	hashes := make([]string, len(info.CachedHashes))
	copy(hashes, info.CachedHashes)

	s.mu.Lock()
	node := s.nodes[info.NodeID]
	if node == nil {
		node = &NodeInfo{}
		s.nodes[info.NodeID] = node
	}
	*node = info
	node.CachedHashes = hashes
	// Active is sticky-true until RemoveNode is called or the heartbeat
	// is stale past staleNodeTTL at observation time (see isStale).
	s.mu.Unlock()
}

// RemoveNode drops a node from the scheduler's view. Called when a node
// leaves the cluster or is detected as dead. Idempotent.
func (s *PlacementScheduler) RemoveNode(nodeID string) {
	if nodeID == "" {
		return
	}
	s.mu.Lock()
	delete(s.nodes, nodeID)
	s.mu.Unlock()
}

// NodeCount returns the number of nodes currently tracked. Includes
// inactive nodes whose entries haven't been removed via RemoveNode.
func (s *PlacementScheduler) NodeCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.nodes)
}

// Decide produces a PlacementDecision for the given request (spec §4.5).
//
// Algorithm:
//  1. If req.PreferredNode is set and that node is Active, fresh, and
//     has Capacity > 0, pick it. Reason = "preferred_node".
//  2. Filter Active, fresh, Capacity > 0 nodes (excluding the local
//     node, which the caller handles separately).
//  3. Score by cache locality: intersection count of
//     req.RequiredResources with node.CachedHashes. Highest wins. If the
//     top score is 0 (no cache overlap), pick by capacity only.
//  4. Tie-break by Capacity (most first), then NodeID (lexicographic).
//  5. If no peer qualifies, apply scheduler_no_capacity_policy.
func (s *PlacementScheduler) Decide(req PlacementRequest) PlacementDecision {
	// Snapshot candidates under lock, then operate on the snapshot
	// outside the lock (mutex-scope rule — no I/O here but we keep the
	// pattern for clarity and in case scoring grows).
	s.mu.RLock()
	candidates := make([]NodeInfo, 0, len(s.nodes))
	for _, n := range s.nodes {
		if n == nil {
			continue
		}
		// Exclude the local node from placement candidates. The caller
		// owns local-vs-peer decisions; PlacementScheduler emits a peer
		// target or a no-peer-available signal.
		if n.NodeID == s.localNodeID {
			continue
		}
		// Filter by Active + freshness.
		if !n.Active || s.isStaleLocked(n) {
			continue
		}
		if n.Capacity <= 0 {
			continue
		}
		candidates = append(candidates, *n)
	}
	policy := s.policy
	s.mu.RUnlock()

	// Cap candidates if MaxNodes is set.
	if req.MaxNodes > 0 && len(candidates) > req.MaxNodes {
		candidates = candidates[:req.MaxNodes]
	}

	// 1. Preferred node.
	if req.PreferredNode != "" {
		for _, c := range candidates {
			if c.NodeID == req.PreferredNode {
				return PlacementDecision{
					TargetNode: c.NodeID,
					Reason:     "preferred_node",
				}
			}
		}
		// Preferred not suitable (not Active, no capacity, stale, or
		// unknown) — fall through to normal scoring.
	}

	// 2. Filter done above.

	// 3. Score by cache locality.
	requiredSet := toSet(req.RequiredResources)
	type scored struct {
		node       NodeInfo
		cacheScore int
	}
	scoredNodes := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		score := 0
		for _, h := range c.CachedHashes {
			if requiredSet[h] {
				score++
			}
		}
		scoredNodes = append(scoredNodes, scored{node: c, cacheScore: score})
	}

	// 4. Sort: highest cacheScore, then Capacity desc, then NodeID asc.
	sort.Slice(scoredNodes, func(i, j int) bool {
		if scoredNodes[i].cacheScore != scoredNodes[j].cacheScore {
			return scoredNodes[i].cacheScore > scoredNodes[j].cacheScore
		}
		if scoredNodes[i].node.Capacity != scoredNodes[j].node.Capacity {
			return scoredNodes[i].node.Capacity > scoredNodes[j].node.Capacity
		}
		return scoredNodes[i].node.NodeID < scoredNodes[j].node.NodeID
	})

	if len(scoredNodes) > 0 {
		winner := scoredNodes[0]
		reason := "cache_locality"
		if winner.cacheScore == 0 {
			// No cache overlap — decision driven by capacity alone.
			reason = "capacity"
		}
		return PlacementDecision{
			TargetNode: winner.node.NodeID,
			Reason:     reason,
		}
	}

	// 5. No peer qualifies: apply scheduler_no_capacity_policy.
	switch policy {
	case PolicyRunLocal:
		return PlacementDecision{
			Local:  true,
			Reason: "no_capacity_run_local",
		}
	default:
		// "queue" (and any unknown policy — defensive).
		return PlacementDecision{
			Local:  false,
			Reason: "no_capacity_queue",
		}
	}
}

// isStaleLocked reports true if the node's LastSeen is older than
// staleNodeTTL. Caller MUST hold s.mu (read or write).
func (s *PlacementScheduler) isStaleLocked(n *NodeInfo) bool {
	if n == nil {
		return true
	}
	if n.LastSeen.IsZero() {
		// No heartbeat ever recorded — treat as not stale so the first
		// registration can take effect immediately.
		return false
	}
	return time.Since(n.LastSeen) > staleNodeTTL
}

// toSet converts a slice to a set[string] for O(1) membership tests.
// The strings are used verbatim (case-sensitive).
func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = true
		}
	}
	return out
}
