package agent

import "sort"

// This file implements Contract A of the phase-frontier-parallel plan
// (docs/plans/phase-frontier-parallel/master.md, leaf
// 01-frontier-compute.md). Contract A presents the identifiers as
// PhaseNode / ComputePhaseFrontier; per leaf 01's pinned conventions the
// Go identifiers are the unexported phaseNode / computePhaseFrontier so
// they stay package-internal like their peers (checkPhaseReady,
// artifactStore). Leaf 02 consumes them directly from the same package.
//
// computePhaseFrontier is PURE: no I/O, no locks, no clocks. It takes a
// snapshot of the incomplete phase graph plus artifact-store and busy
// sets, and returns the ready subset in deterministic dispatch order.
// The function takes no recovery action on a detected cycle; that is the
// caller's job (Contract B).

// phaseNode is the plan-phase snapshot consumed by computePhaseFrontier.
type phaseNode struct {
	Name           string
	Sequence       int
	Produces       []string
	Consumes       []Artifact
	DependsOnPhase []string
}

// computePhaseFrontier returns the phases ready to start:
//
//   - every Required consume is present in availableArtifacts (optional
//     consumes never gate — checkPhaseReady parity), and
//   - no dependency edge from the phase reaches a busyPhases member.
//
// Edges come from every consume (required or optional) whose producer is
// in the graph, plus in-graph DependsOnPhase entries. A phase consuming
// its own artifact creates no self-edge, and consumes whose producer is
// NOT in the graph create no edge at all — the artifact-store gate at
// phase start still protects those (checkPhaseReady behavior). Duplicate
// Names keep the first occurrence; later duplicates are ignored.
//
// cycleDetected is true iff the non-empty node set yields an empty ready
// set, i.e. the remaining graph cannot advance.
func computePhaseFrontier(
	nodes []phaseNode,
	availableArtifacts map[string]bool,
	busyPhases map[string]bool,
) (ready []phaseNode, cycleDetected bool) {
	if len(nodes) == 0 {
		return nil, false
	}

	// Dedup by Name: first occurrence wins.
	seen := make(map[string]bool, len(nodes))
	unique := make([]phaseNode, 0, len(nodes))
	for _, n := range nodes {
		if seen[n.Name] {
			continue
		}
		seen[n.Name] = true
		unique = append(unique, n)
	}

	// Index in-graph producers: artifact -> first producing phase.
	producerOf := make(map[string]string)
	for _, n := range unique {
		for _, p := range n.Produces {
			if _, ok := producerOf[p]; !ok {
				producerOf[p] = n.Name
			}
		}
	}

	// Dependency edges: X depends on Y iff some consume of X is produced
	// by in-graph Y != X, or Y is an in-graph DependsOnPhase entry.
	depsOf := make(map[string]map[string]struct{}, len(unique))
	addDep := func(from, to string) {
		set, ok := depsOf[from]
		if !ok {
			set = make(map[string]struct{})
			depsOf[from] = set
		}
		set[to] = struct{}{}
	}
	for _, n := range unique {
		for _, c := range n.Consumes {
			producer, ok := producerOf[c.Name]
			if !ok || producer == n.Name {
				continue // no in-graph producer, or self-produce: no edge
			}
			addDep(n.Name, producer)
		}
		for _, dep := range n.DependsOnPhase {
			if seen[dep] && dep != n.Name {
				addDep(n.Name, dep)
			}
		}
	}

	ready = make([]phaseNode, 0, len(unique))
	for _, n := range unique {
		ok := true
		// Gate (a): required consumes must be available; optional
		// consumes never block.
		for _, c := range n.Consumes {
			if c.Required && !availableArtifacts[c.Name] {
				ok = false
				break
			}
		}
		// Gate (b): no dependency edge into a busy phase.
		if ok {
			for dep := range depsOf[n.Name] {
				if busyPhases[dep] {
					ok = false
					break
				}
			}
		}
		if ok {
			ready = append(ready, n)
		}
	}

	// Deterministic order: Sequence ascending, Name ascending tiebreak.
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].Sequence != ready[j].Sequence {
			return ready[i].Sequence < ready[j].Sequence
		}
		return ready[i].Name < ready[j].Name
	})

	if len(ready) == 0 {
		return nil, true
	}
	return ready, false
}
