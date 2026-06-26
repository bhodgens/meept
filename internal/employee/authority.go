// Package employee — authority.go contains escalation-resolution
// helpers: cycle detection over the escalates_to graph and an
// escalation routing stub. These run at load time so an invalid
// authority graph never reaches the runtime.
package employee

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
)

// EscalationGraph captures the minimum interface
// DetectEscalationCycles needs to walk the escalates_to graph. The
// manager implements this; tests can pass a stub.
//
// Lookup must return the Constitution for the given agent ID, or
// (nil, false) if the ID is unknown. Unknown IDs are reported as a
// separate error category (unresolved references) rather than cycle
// findings.
type EscalationGraph interface {
	Lookup(agentID string) (Constitution, bool)
}

// CycleFinding describes one escalation cycle detected in the graph.
// Findings are sorted by cycle length (shortest first) so the most
// specific problem surfaces first. AgentIDs lists the cycle in walk
// order, starting from the alphabetically smallest member for
// deterministic output.
type CycleFinding struct {
	AgentIDs []string // e.g. ["a", "b", "a"] for a→b→a
}

// String renders the cycle as a human-readable arrow chain.
func (c CycleFinding) String() string {
	if len(c.AgentIDs) == 0 {
		return "(empty cycle)"
	}
	out := ""
	for i, id := range c.AgentIDs {
		if i > 0 {
			out += " -> "
		}
		out += id
	}
	return out
}

// DetectEscalationCycles walks the escalates_to graph for the given
// employee IDs and returns every cycle it finds. A cycle is any path
// that returns to a previously visited node: direct (X→X) or
// transitive (X→Y→X, X→Y→Z→X, ...).
//
// agentIDs is the set of employee IDs to seed the walk from. Passing
// nil/empty returns nil(nil) — there's nothing to check. The graph is
// resolved via g.Lookup for each encountered ID.
//
// Unknown IDs referenced in EscalatesTo are reported as a separate
// joined error (not cycle findings); they are surfaced at load time
// by CheckEscalationReferences, but this function also surfaces them
// so callers don't need to run both checks independently.
//
// The algorithm is a standard DFS with a recursion-stack colour map
// (white/grey/black). It runs in O(V+E) where V is the number of
// distinct employee IDs reachable from agentIDs and E is the total
// number of escalates_to edges. Self-loops are detected without
// recursion so a degenerate graph with N self-escalations still
// terminates in O(N).
func DetectEscalationCycles(g EscalationGraph, agentIDs []string) ([]CycleFinding, error) {
	if isNilGraph(g) || len(agentIDs) == 0 {
		return nil, nil
	}

	// visited state: 0 = white (unseen), 1 = grey (on current DFS
	// stack), 2 = black (fully explored, no cycle through it).
	const (
		white = 0
		grey  = 1
		black = 2
	)

	color := make(map[string]int)
	var cycles []CycleFinding
	var unresolved []string

	// path holds the current DFS stack, used to reconstruct the cycle
	// when we encounter a grey node.
	var path []string

	// seenCycle deduplicates cycle reports. Two different seeds may
	// surface the same cycle (e.g. X→Y→X from X and from Y); we keep
	// only one copy per cycle by keying on the sorted member set.
	seenCycle := make(map[string]struct{})

	// Role sentinels ("role:user", legacy "user", etc.) are terminal
	// escalation sinks, not agent IDs. They never participate in cycle
	// detection or unresolved-agent reporting. See IsRoleSentinel for
	// the full set of recognised sentinels.

	var visit func(id string)
	visit = func(id string) {
		if color[id] == black {
			return
		}
		// Role sentinels are always terminal sinks — never an
		// unresolved reference, never a cycle participant.
		if IsRoleSentinel(id) {
			color[id] = black
			return
		}
		// Look up the constitution for this ID. Unknown IDs are
		// flagged separately and treated as leaves (no out-edges).
		c, ok := g.Lookup(id)
		if !ok {
			color[id] = black
			unresolved = append(unresolved, id)
			return
		}

		// Grey means we've reached a node currently on the DFS stack:
		// a cycle. Slice the path from the previous occurrence to
		// here to recover the cycle members.
		if color[id] == grey {
			cycle := appendCycleFromPath(path, id)
			key := cycleKey(cycle)
			if _, dup := seenCycle[key]; !dup {
				seenCycle[key] = struct{}{}
				cycles = append(cycles, CycleFinding{AgentIDs: cycle})
			}
			return
		}

		color[id] = grey
		path = append(path, id)

		// Walk out-edges. We iterate over a copy so concurrent
		// modifications to c.EscalatesTo during the walk don't
		// corrupt the DFS — defensive, the manager shouldn't mutate
		// while loading but cheap insurance.
		edges := append([]string(nil), c.EscalatesTo...)
		for _, next := range edges {
			visit(next)
		}

		path = path[:len(path)-1]
		color[id] = black
	}

	// Seed from the requested IDs. De-duplicate seeds so we don't
	// waste work, but preserve the original order for deterministic
	// output ordering.
	seenSeed := make(map[string]struct{}, len(agentIDs))
	for _, id := range agentIDs {
		if _, dup := seenSeed[id]; dup {
			continue
		}
		seenSeed[id] = struct{}{}
		visit(id)
	}

	// Deterministic ordering: shortest cycle first, then by the first
	// agent ID. Stabilises the output so tests can compare exactly.
	sort.SliceStable(cycles, func(i, j int) bool {
		if len(cycles[i].AgentIDs) != len(cycles[j].AgentIDs) {
			return len(cycles[i].AgentIDs) < len(cycles[j].AgentIDs)
		}
		return cycles[i].AgentIDs[0] < cycles[j].AgentIDs[0]
	})

	var err error
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		err = fmt.Errorf("unresolved agent IDs in escalates_to graph: %v", unresolved)
	}
	if len(cycles) > 0 && err != nil {
		err = errors.Join(err, fmt.Errorf("%d escalation cycle(s) detected", len(cycles)))
	} else if len(cycles) > 0 {
		err = fmt.Errorf("%d escalation cycle(s) detected", len(cycles))
	}
	return cycles, err
}

// appendCycleFromPath reconstructs the cycle from the current DFS path
// starting at the first occurrence of id, then appends id itself to
// close the loop. e.g. path=["a","b","c"], id="b" -> ["b","c","b"].
func appendCycleFromPath(path []string, id string) []string {
	start := -1
	for i, p := range path {
		if p == id {
			start = i
			break
		}
	}
	if start < 0 {
		// Defensive: id not on path. Should not happen given how the
		// caller invokes us, but return a single-element cycle so we
		// still report something rather than panicking.
		return []string{id, id}
	}
	cycle := append([]string(nil), path[start:]...)
	cycle = append(cycle, id)
	return cycle
}

// cycleKey produces a stable identifier for a cycle so two seeds that
// surface the same cycle produce one report. The key is the sorted
// unique member set joined by "|" — order-independent so X→Y→X and
// Y→X→Y (the same cycle, seeded differently) collapse to one entry.
func cycleKey(cycle []string) string {
	seen := make(map[string]struct{}, len(cycle))
	members := make([]string, 0, len(cycle))
	for _, id := range cycle {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		members = append(members, id)
	}
	sort.Strings(members)
	out := ""
	for i, m := range members {
		if i > 0 {
			out += "|"
		}
		out += m
	}
	return out
}

// isNilGraph returns true if g is a nil interface or a typed-nil value
// (e.g. a nil StaticEscalationGraph map or any nil pointer). Typed-nil
// values in an interface are != nil in Go, so we use reflect to distinguish.
func isNilGraph(g EscalationGraph) bool {
	if g == nil {
		return true
	}
	v := reflect.ValueOf(g)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	}
	return false
}

// StaticEscalationGraph is a trivial EscalationGraph backed by a map.
// Useful for tests and for the manager's load-time validation pass
// where the full set of constitutions is already in memory.
type StaticEscalationGraph map[string]Constitution

// ---------------------------------------------------------------------------
// G6: GoalSource validation by tier
// ---------------------------------------------------------------------------

// ValidateGoalSource checks whether a given GoalSource is compatible with the
// employee's AutonomyTier.
//
//   - Tier 1 (Reactive): rejects SourceSelfProposed (tier-1 employees are
//     reactive-only and cannot self-propose goals).
//   - Tier 2 (Propose): requires a human/user source for externally-assigned
//     goals, but allows self_proposed, trigger, and audit_finding sources.
//   - Tier 3 (Autonomous): all sources are valid.
//
// Returns nil if the source is valid for the tier; an error describing the
// mismatch otherwise.
func ValidateGoalSource(source GoalSource, tier AutonomyTier) error {
	switch tier {
	case Tier1Reactive:
		if source == SourceSelfProposed {
			return fmt.Errorf("tier 1 (reactive) employees cannot self-propose goals; source must be %q or %q",
				SourceUser, SourceTrigger)
		}
	case Tier2Propose:
		// Tier 2 can receive goals from users, triggers, self-proposals
		// (approved), and audit findings. All sources are valid.
		// However, the spec says "tier 2 requires human/user source" for
		// externally-assigned goals. This means SourceUser is always
		// valid; SourceSelfProposed requires signoff (enforced elsewhere).
		// No extra validation needed here for tier 2 beyond what the
		// amendment policy already covers.
	case Tier3Autonomous:
		// All sources valid.
	default:
		return fmt.Errorf("unknown autonomy tier: %d", tier)
	}
	return nil
}

// Lookup implements EscalationGraph.
func (s StaticEscalationGraph) Lookup(agentID string) (Constitution, bool) {
	c, ok := s[agentID]
	return c, ok
}

// EscalationRouter is the load-time escalation-routing stub. The real
// routing (Plan signoff dispatch) is wired in a later phase when the
// Plan/manager packages are integrated. For now this captures the
// contract so callers can mock it.
type EscalationRouter struct {
	logger *slog.Logger
}

// NewEscalationRouter returns a router that logs escalation events.
// Pass slog.Default() if you don't have a structured logger handy.
// Nil logger falls back to slog.Default() so the type is always safe
// to call.
func NewEscalationRouter(logger *slog.Logger) *EscalationRouter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EscalationRouter{logger: logger}
}

// SetLogger replaces the router's logger. Nil is silently ignored so
// callers can't accidentally blank the logger. Per CLAUDE.md setter
// guard rule.
func (r *EscalationRouter) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	r.logger = logger
}

// RouteEscalation records that the given employee needs to escalate
// the given action to the listed approvers. Phase 1 logs the event
// and returns nil — the Plan-signoff dispatch lands in a later phase
// once the manager wiring exists. ctx is accepted so the future
// implementation can respect cancellation without a signature change.
func (r *EscalationRouter) RouteEscalation(ctx context.Context, employeeID, action, reason string, approvers []string) error {
	_ = ctx // accepted for forward compatibility; not used in the stub
	if r == nil {
		return errors.New("escalation router is nil")
	}
	if employeeID == "" {
		return errors.New("employeeID is required")
	}
	if len(approvers) == 0 {
		return fmt.Errorf("employee %q has no approvers to route escalation to", employeeID)
	}
	r.logger.Info("escalation routed (stub routing in phase 1)",
		"employee_id", employeeID,
		"action", action,
		"reason", reason,
		"approvers", approvers,
	)
	return nil
}
