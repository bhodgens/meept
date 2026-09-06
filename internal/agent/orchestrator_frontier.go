package agent

// orchestrator_frontier.go implements Contract B of the
// phase-frontier-parallel plan (docs/plans/phase-frontier-parallel/master.md,
// leaf 02-store-state.md): on a phase-terminal step event, dispatch ALL
// ready phases at once (the frontier) instead of the serial next-in-list
// order, when plans.parallel_phases is enabled.
//
// Pure frontier computation lives in phase_frontier.go (Contract A, leaf 01);
// this file owns the I/O orchestration: phase/step classification, artifact
// snapshotting, per-task single-flight, and the cycle fallback. Dispatch
// entry is maybeTransitionPhase (orchestrator.go) when o.parallelPhases is
// true.

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/caimlas/meept/internal/plan"
)

// advancePhasesFrontier computes the ready frontier across the task's plan
// phases and starts every ready phase (Contract B).
//
// Per-task single-flight WITHOUT holding a mutex across store I/O
// (mutexio): inflight maps taskID -> *atomic.Bool; CompareAndSwap(false,
// true) guards re-entry when a second terminal event for the same task
// arrives while an advance is still running; defer Store(false) releases
// it. Snapshot-then-operate: all store reads happen up front, phase starts
// happen after the snapshot is complete.
func (o *Orchestrator) advancePhasesFrontier(ctx context.Context, taskID string) {
	parked, _ := o.frontierInflight.LoadOrStore(taskID, &atomic.Bool{})
	if !parked.(*atomic.Bool).CompareAndSwap(false, true) {
		// Another terminal event for this task is already advancing.
		return
	}
	defer parked.(*atomic.Bool).Store(false)

	phases, err := o.planManager.GetPhasesByTask(ctx, taskID)
	if err != nil {
		o.logger.Warn("phase frontier: get phases failed",
			"task_id", taskID, "error", err)
		return
	}
	if len(phases) == 0 {
		return // non-plan task (GetPhasesByTask -> nil, nil)
	}

	// Classify phases from step states (IsPhaseComplete predicate parity:
	// task/step.go — all steps IsSuccessfullyTerminal OR no steps at all
	// counts complete; >=1 non-successfully-terminal step means busy).
	nodes := make([]phaseNode, 0, len(phases))
	busyPhases := make(map[string]bool, len(phases))
	consumeNames := make(map[string]struct{})
	for _, p := range phases {
		steps, err := o.stepStore.GetPhaseSteps(taskID, p.Name)
		if err != nil {
			o.logger.Warn("phase frontier: get phase steps failed",
				"task_id", taskID, "phase", p.Name, "error", err)
			return
		}
		allDone := true
		hasSteps := len(steps) > 0
		for _, s := range steps {
			if !s.State.IsSuccessfullyTerminal() {
				allDone = false
				break
			}
		}
		done := !hasSteps || allDone
		if done {
			continue // completed phases leave the node set
		}
		// Incomplete phase: has >=1 non-successfully-terminal step => busy.
		busyPhases[p.Name] = true
		// Resolve cross-phase step DependsOn edges: a DependsOn entry whose
		// target step lives in a different phase creates a phase-level edge.
		// GetByID on an unknown ID returns (nil, nil) — skip those.
		dependsOnPhase := make([]string, 0)
		for _, s := range steps {
			for _, depID := range s.DependsOn {
				dep, err := o.stepStore.GetByID(depID)
				if err != nil || dep == nil || dep.Phase == "" || dep.Phase == p.Name {
					continue
				}
				dependsOnPhase = append(dependsOnPhase, dep.Phase)
			}
		}
		for _, c := range p.Consumes {
			consumeNames[c.Name] = struct{}{}
		}
		nodes = append(nodes, phaseNode{
			Name:           p.Name,
			Sequence:       p.Sequence,
			Produces:       artifactNames(p.Produces),
			Consumes:       artifactDecls(p.Consumes),
			DependsOnPhase: dependsOnPhase,
		})
	}
	if len(nodes) == 0 {
		return // everything complete; nothing to advance
	}

	// Artifact snapshot over the union of all nodes' consume names, via the
	// artifact store's public read API only. Nil store = empty map (free
	// nil-safety; flag-off behavior never reaches this path anyway).
	availableArtifacts := make(map[string]bool, len(consumeNames))
	for name := range consumeNames {
		if o.artifacts != nil && o.artifacts.Has(name) {
			availableArtifacts[name] = true
		}
	}

	ready, cycleDetected := computePhaseFrontier(nodes, availableArtifacts, busyPhases)

	if cycleDetected {
		// Anti-cycle escape, never a busy loop: the in-flight guard plus the
		// !done node filter bound it. Legacy serial behavior: start the
		// first incomplete phase in list order (the contract's fallback).
		o.logger.Warn("phase frontier stalled (cycle or unmet dependency)",
			"task_id", taskID,
			"incomplete", nodeNames(nodes),
		)
		fallback := listOrderFirstIncomplete(nodes)
		if fallback != nil {
			if phase := phaseByName(phases, fallback.Name); phase != nil {
				if err := o.startPhase(ctx, taskID, phase, ""); err != nil {
					o.logger.Warn("phase frontier fallback start failed",
						"task_id", taskID, "phase", fallback.Name, "error", err)
				}
			}
		}
		return
	}

	started := make([]string, 0, len(ready))
	for _, node := range ready {
		phase := phaseByName(phases, node.Name)
		if phase == nil {
			continue
		}
		if err := o.startPhase(ctx, taskID, phase, ""); err != nil {
			// checkPhaseReady failures are expected Warn-able conditions
			// (e.g. a required consume the graph cannot see), not fatals.
			o.logger.Warn("phase frontier start failed",
				"task_id", taskID, "phase", node.Name, "error", err)
			continue
		}
		started = append(started, node.Name)
	}

	o.logger.Info("Phase frontier advanced",
		"task_id", taskID,
		"ready", fmt.Sprintf("%v", started),
		"started", len(started),
		"incomplete", len(nodes),
	)
}

// listOrderFirstIncomplete returns the lowest-Sequence node of the incomplete
// set (the legacy serial fallback pick), or nil when nodes is empty.
func listOrderFirstIncomplete(nodes []phaseNode) *phaseNode {
	if len(nodes) == 0 {
		return nil
	}
	first := &nodes[0]
	for i := range nodes {
		if nodes[i].Sequence < first.Sequence ||
			(nodes[i].Sequence == first.Sequence && nodes[i].Name < first.Name) {
			first = &nodes[i]
		}
	}
	return first
}

// artifactNames converts plan artifact declarations to plain names
// (phaseNode.Produces shape).
func artifactNames(produces []plan.Artifact) []string {
	if len(produces) == 0 {
		return nil
	}
	names := make([]string, 0, len(produces))
	for _, a := range produces {
		names = append(names, a.Name)
	}
	return names
}

// artifactDecls converts plan artifact declarations to the agent.Artifact
// (plan.Artifact alias) slice phaseNode.Consumes carries.
func artifactDecls(consumes []plan.Artifact) []Artifact {
	if len(consumes) == 0 {
		return nil
	}
	out := make([]Artifact, 0, len(consumes))
	for _, a := range consumes {
		out = append(out, Artifact(a))
	}
	return out
}

// phaseByName resolves a *plan.PlanPhase by Name from the loaded phase list.
func phaseByName(phases []*plan.PlanPhase, name string) *plan.PlanPhase {
	for _, p := range phases {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// nodeNames returns node names for logging.
func nodeNames(nodes []phaseNode) []string {
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	return names
}
