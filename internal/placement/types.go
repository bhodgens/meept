// Package placement implements cluster-aware task placement for the
// cluster resource model.
//
// PlacementScheduler consumes heartbeat metadata (node capacity, cached
// hashes from bloom-filter advertisement) and emits placement decisions
// honoring `preferred_node` hints. When no peer is suitable, it applies
// the configured scheduler_no_capacity_policy ("queue" default or
// "run_local").
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.5, §2.3 β, §2.5
package placement

import "time"

// NodeInfo captures the subset of heartbeat metadata that PlacementScheduler
// uses for placement decisions. Updated via UpdateNode on each heartbeat.
type NodeInfo struct {
	// NodeID is the cluster-unique identifier for the node.
	NodeID string `json:"node_id"`

	// Capacity is the number of additional job slots the node reports as
	// available in its latest heartbeat.
	Capacity int `json:"capacity"`

	// CachedHashes is the bloom-filter-style advertisement of CAS blobs
	// the node has locally (spec §4.5). For scoring by cache locality.
	// Each entry is a prefixed hash like "blake3:abcd...".
	CachedHashes []string `json:"cached_hashes,omitempty"`

	// LastSeen is the timestamp of the most recent heartbeat.
	LastSeen time.Time `json:"last_seen"`

	// Active is true when the node is considered reachable and willing to
	// accept dispatched jobs. Heartbeat staleness or explicit leave flips
	// this to false.
	Active bool `json:"active"`
}

// PlacementRequest is the input to PlacementScheduler.Decide.
type PlacementRequest struct {
	// AgentID is the role/agent the task is targeted at. Placement does
	// not filter on this (agents are daemon-local); it is carried for
	// logging/telemetry only.
	AgentID string `json:"agent_id"`

	// RequiredResources is the list of resource hashes the task needs.
	// Used to score candidate nodes by cache locality (intersection with
	// NodeInfo.CachedHashes).
	RequiredResources []string `json:"required_resources,omitempty"`

	// PreferredNode is an optional hint from the caller. When set and the
	// named node is Active with Capacity > 0, Decide picks it directly
	// (spec §2.5: `preferred_node` hints honored).
	PreferredNode string `json:"preferred_node,omitempty"`

	// MaxNodes caps the number of candidates considered. Zero means
	// unlimited. Used to bound scoring work when the cluster is large.
	MaxNodes int `json:"max_nodes,omitempty"`
}

// PlacementDecision is the output of PlacementScheduler.Decide.
type PlacementDecision struct {
	// TargetNode is the chosen node ID. Empty string indicates the
	// "no suitable peer" outcome (caller applies no_capacity_policy).
	TargetNode string `json:"target_node,omitempty"`

	// Local is true when the decision is to run locally. This mirrors
	// the case where no peer is suitable and the policy is "run_local".
	Local bool `json:"local"`

	// Reason is a machine-readable label for the decision:
	//   - "preferred_node"  — hint was honored
	//   - "cache_locality"  — highest cache-locality score won
	//   - "capacity"        — capacity-only tiebreak (no cache overlap)
	//   - "no_capacity_queue"     — no peer; queue policy
	//   - "no_capacity_run_local" — no peer; run_local policy
	Reason string `json:"reason"`
}

// Policy constants (spec §2.5). Validate-then-accept on SetPolicy.
const (
	// PolicyQueue queues the task for later retry when no suitable peer
	// is found. The Decide call returns PlacementDecision{Local: false,
	// Reason: "no_capacity_queue"} and the caller is responsible for
	// enqueuing.
	PolicyQueue = "queue"

	// PolicyRunLocal runs the task on the local daemon when no suitable
	// peer is found. PlacementDecision{Local: true, Reason:
	// "no_capacity_run_local"}.
	PolicyRunLocal = "run_local"
)

// Fallback policy constants (spec §2.5, peer_fallback_policy). Applied by
// callers (not PlacementScheduler itself, which only handles
// scheduler_no_capacity_policy). Kept here for shared reference.
const (
	FallbackAlways     = "always"
	FallbackNever      = "never"
	FallbackIfCapacity = "if_capacity"
)
