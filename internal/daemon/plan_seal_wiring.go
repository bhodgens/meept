package daemon

// Plan-compiler seal pipeline wiring (plan-compiler leaf 04, Contract C).
// These helpers adapt the daemon's live components to the rpc.PlanSealHandler
// injected-func seams. Kept out of daemon.go so the integration test can
// exercise the exact production closures over a real component graph.
//
// Persistence goes ONLY through existing paths: the compiled phase
// declarations land via the same PlanManager.CreatePhase call the
// planPhaseSink uses, steps via StepStore inside StrategicPlanner.SealPlan
// (mirror of ApprovePlan's tail), tree files via direct file write of the
// emitted tree under <data_dir>/plan-trees/<task-id>/. No new persistence
// formats.
//
// H8 (daemon audit 2026-09-08): persisted phase names stay CLEAN — the
// per-leaf path annotation used to ride the name ("Phase [tree leaf: 01-x.md]")
// and broke every orchestrator name-join (steps carry the clean phase name,
// so startPhase found zero steps / startNextPhase never matched). The leaf
// mapping now lives out-of-band in a sidecar JSON next to the emitted tree
// (<plan-trees>/<task-id>/phase_leaves.json, map phase-index→leaf-path).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/rpc"
)

// phaseLeafSidecarName is the sidecar file (under the task's plan-trees dir)
// carrying the phase-index→leaf-path map for tree-mode seals. Out-of-band by
// design: PlanPhase has no free-text metadata field, and annotating the
// persisted phase name broke the orchestrator's name-joins (H8).
const phaseLeafSidecarName = "phase_leaves.json"

// sealPipelineState carries the compiled phases from the Persist seam to the
// Execute seam. Both seams run inside the synchronous plan.seal call, but
// they are distinct closures, so the staging is the handoff between them
// (Execute's signature is taskID-only, mirroring ApproveFunc).
//
// M13 (daemon audit 2026-09-08): staging is keyed by taskID. A single
// shared slot used to cross-assign compiled phases between concurrent
// seals — seal B's Compile overwrote the slot while seal A was between
// Persist and Execute, and A executed B's phases. plan.seal calls are
// serialized per CLI, but nothing guarantees it across future callers
// (RPC surface), so the map closes the race; the mutex keeps it race-clean.
type sealPipelineState struct {
	mu     sync.Mutex
	phases map[string][]agent.PlanPhaseSpec
}

// take returns and clears the compiled phases staged for taskID.
func (s *sealPipelineState) take(taskID string) []agent.PlanPhaseSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phases == nil {
		return nil
	}
	p := s.phases[taskID]
	delete(s.phases, taskID)
	return p
}

// store remembers the compiled phases for taskID.
func (s *sealPipelineState) store(taskID string, p []agent.PlanPhaseSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phases == nil {
		s.phases = make(map[string][]agent.PlanPhaseSpec)
	}
	s.phases[taskID] = p
}

// adaptCompileSealed wraps plan.CompileSealed into the handler's Compile
// seam. Returns the compiler-native []plan.PhaseSpec (the handler's
// gate/emitter helpers consume that shape) plus hash/warnings/problems;
// compiled phase specs are staged for the Execute seam by adaptPersistPhases
// (which owns the taskID — the Compile seam does not).
func adaptCompileSealed() func(markdown string, maxPhases int) (any, string, []string, []rpc.CompileProblemView, error) {
	return func(markdown string, maxPhases int) (any, string, []string, []rpc.CompileProblemView, error) {
		cp, err := plan.CompileSealed(markdown, maxPhases)
		if err != nil {
			var ce *plan.CompileError
			if asCompileError(err, &ce) {
				views := make([]rpc.CompileProblemView, 0, len(ce.Problems))
				for _, p := range ce.Problems {
					views = append(views, rpc.CompileProblemView{Line: p.Line, Message: p.Message})
				}
				return nil, "", nil, views, nil
			}
			return nil, "", nil, nil, err
		}
		return cp.Phases, cp.Hash, cp.Warnings, nil, nil
	}
}

// adaptPersistPhases persists compiled phases via the plan store (the same
// CreatePhase call the planPhaseSink uses), writes the emitted tree (tree
// mode) under the plan-trees root, and stages the agent-shaped phase specs
// for the Execute seam under the task's ID (M13). Flat phases persist in
// both modes — the orchestrator executes phases; the tree is the
// human/leaf-agent artifact. Phase names persist CLEAN (H8): the leaf
// mapping rides the sidecar JSON, never the name.
func adaptPersistPhases(state *sealPipelineState, planMgr *plan.PlanManager, treeRoot string, logger *slog.Logger) func(taskID string, phases any, tree *plan.EmittedTree) error {
	return func(taskID string, phases any, tree *plan.EmittedTree) error {
		if planMgr == nil {
			return fmt.Errorf("plan store not available")
		}
		specs, ok := phases.([]plan.PhaseSpec)
		if !ok {
			return fmt.Errorf("unexpected compiled phase type %T", phases)
		}

		ctx := context.Background()
		// Phases hang off a plan row (plan_phases.plan_id FK): ensure the
		// task has a container plan and hang the phases off ITS id.
		container, err := planMgr.EnsureTaskPlan(ctx, taskID, "Sealed plan")
		if err != nil {
			logger.Error("plan-seal persist: EnsureTaskPlan failed",
				"task_id", taskID, "error", err)
			return fmt.Errorf("failed to ensure container plan: %w", err)
		}
		for i, p := range specs {
			phaseRecord := plan.NewPlanPhase(container.ID, p.Name, i, len(p.Steps))
			phaseRecord.Produces = p.Produces
			phaseRecord.Consumes = p.Consumes
			if err := planMgr.CreatePhase(ctx, phaseRecord); err != nil {
				logger.Error("plan-seal persist: CreatePhase failed",
					"task_id", taskID, "phase", p.Name, "error", err)
				return fmt.Errorf("failed to persist phase %q: %w", p.Name, err)
			}
		}

		if tree != nil {
			dir := filepath.Join(treeRoot, taskID)
			if err := writeEmittedTree(dir, tree); err != nil {
				return fmt.Errorf("failed to write plan tree: %w", err)
			}
			// Sidecar: phase-index → first leaf path. Written after the
			// tree so a reader never sees the map before the leaves exist.
			if err := writePhaseLeafSidecar(dir, tree, specs); err != nil {
				return fmt.Errorf("failed to write phase-leaf sidecar: %w", err)
			}
			logger.Info("Plan tree emitted",
				"task_id", taskID, "dir", dir, "leaves", len(tree.Leaves))
		}

		// Stage for Execute under THIS task's ID (M13): concurrent seals
		// can no longer cross-assign compiled phases.
		state.store(taskID, agent.PhaseSpecsFromPlan(specs))
		return nil
	}
}

// adaptExecute seals through StrategicPlanner.SealPlan: the ApprovePlan
// mirror (persist steps → spec → executing → promote → schedule). The
// compiled phases come from the task-keyed pipeline state.
func adaptExecute(sp *agent.StrategicPlanner, state *sealPipelineState) func(taskID string) error {
	return func(taskID string) error {
		phases := state.take(taskID)
		if len(phases) == 0 {
			return fmt.Errorf("no compiled phases staged for task %s", taskID)
		}
		// The phase declarations were already persisted by
		// adaptPersistPhases; SealPlan's persistPhases hook is spent, so it
		// is passed as nil and SealPlan carries steps/spec/state/schedule.
		return sp.SealPlan(context.Background(), taskID, phases, nil)
	}
}

// wirePlanSealHandler builds the handler over the live components and
// registers plan.seal/plan.draft. Called from daemon.go beside the
// SetParallelPhases site when fullCfg.Plans.PlanCompilerEnabled.
func wirePlanSealHandler(sp *agent.StrategicPlanner, planMgr *plan.PlanManager, treeRoot string, logger *slog.Logger) (*rpc.PlanSealHandler, error) {
	if sp == nil {
		return nil, fmt.Errorf("strategic planner not available")
	}
	if logger == nil {
		logger = slog.Default()
	}

	state := &sealPipelineState{}
	handler := rpc.NewPlanSealHandler(
		planSealDraftSource(sp),
		adaptCompileSealed(),
		adaptPersistPhases(state, planMgr, treeRoot, logger),
		adaptExecute(sp, state),
		sp.MaxPhases(),
	)
	// plan.draft seams.
	handler.SaveDraft = sp.SaveDraft
	handler.GetDraft = func(taskID string) (string, int, bool) {
		d, ok := sp.DraftFor(taskID)
		if !ok {
			return "", 0, false
		}
		return d.Markdown, d.Version, true
	}
	return handler, nil
}

// planSealDraftSource adapts the planner's draft store to the rpc seam.
func planSealDraftSource(sp *agent.StrategicPlanner) rpc.DraftSource {
	return &strategicDraftSource{sp: sp}
}

// strategicDraftSource implements rpc.DraftSource over StrategicPlanner.
// A struct pointer (never a typed nil) avoids the typed-nil interface hazard.
type strategicDraftSource struct {
	sp *agent.StrategicPlanner
}

func (s *strategicDraftSource) DraftFor(taskID string) (string, string, bool) {
	d, ok := s.sp.DraftFor(taskID)
	if !ok {
		return "", "", false
	}
	return d.Markdown, d.SealedHash, true
}

func (s *strategicDraftSource) SealDraft(taskID, hash string) error {
	return s.sp.SealDraft(taskID, hash)
}

// writeEmittedTree writes the master document and leaf files of an emitted
// tree under dir. os.WriteFile of already-generated content — no new
// persistence format.
func writeEmittedTree(dir string, tree *plan.EmittedTree) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plan tree dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "master.md"), []byte(tree.Root), 0o644); err != nil {
		return fmt.Errorf("write plan tree root: %w", err)
	}
	for _, leaf := range tree.Leaves {
		if err := os.WriteFile(filepath.Join(dir, leaf.Path), []byte(leaf.Content), 0o644); err != nil {
			return fmt.Errorf("write plan tree leaf %s: %w", leaf.Path, err)
		}
	}
	return nil
}

// treeLeavesPerPhase returns, for each phase index, how many emitted leaves
// it owns. EmittedTree.Leaves carry no phase ownership (the emitter's
// emitLeaf.phaseIdx is unexported and dropped from the public shape), but
// the partition is deterministic from the phase specs: a phase with n steps
// fits ⌈n/maxLeaves⌉ leaves capped at maxLeaves (and character-budget
// splits never REDUCE that count below 1 for a non-empty phase). Phases
// with zero steps emit no leaf. Mirrors plan.partitionPhases faithfully —
// pinned by TestPhaseLeafCounts_MatchesEmitterPartition.
func treeLeavesPerPhase(specs []plan.PhaseSpec, maxLeaves int) []int {
	if maxLeaves <= 0 {
		maxLeaves = 3
	}
	counts := make([]int, len(specs))
	for i, p := range specs {
		n := len(p.Steps)
		if n == 0 {
			continue
		}
		perLeaf := n
		if n > maxLeaves {
			perLeaf = (n + maxLeaves - 1) / maxLeaves
		}
		counts[i] = (n + perLeaf - 1) / perLeaf
		if counts[i] > maxLeaves {
			counts[i] = maxLeaves
		}
	}
	return counts
}

// treeLeafPathForPhase picks the emitted leaf that carries a phase's work
// items — its FIRST leaf (M12: the old global-index aliasing mapped phase 1
// onto phase 0's second leaf once a phase split into multiple leaves, so
// later phases could resolve to the wrong document outright).
//
// Leaves are ordered by phase (the emitter walks phases in order and never
// interleaves), so the per-phase leaf counts reconstruct the leaf→phase
// boundaries without phase ownership on EmittedTree itself.
func treeLeafPathForPhase(tree *plan.EmittedTree, specs []plan.PhaseSpec, phaseIdx int) string {
	if tree == nil || len(tree.Leaves) == 0 || phaseIdx < 0 || phaseIdx >= len(specs) {
		return ""
	}
	counts := treeLeavesPerPhase(specs, 0)
	offset := 0
	for i := 0; i < phaseIdx; i++ {
		offset += counts[i]
	}
	if offset >= len(tree.Leaves) {
		return ""
	}
	return tree.Leaves[offset].Path
}

// writePhaseLeafSidecar persists the phase-index→leaf-path map next to the
// emitted tree (H8: out-of-band carriage of the leaf mapping; the persisted
// phase records keep clean names). Layout: {"0": "01-extract.md", ...}.
func writePhaseLeafSidecar(dir string, tree *plan.EmittedTree, specs []plan.PhaseSpec) error {
	sidecar := make(map[string]string, len(specs))
	counts := treeLeavesPerPhase(specs, 0)
	offset := 0
	for i := range specs {
		if counts[i] == 0 {
			continue
		}
		if offset < len(tree.Leaves) {
			sidecar[fmt.Sprintf("%d", i)] = tree.Leaves[offset].Path
		}
		offset += counts[i]
	}
	raw, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal phase-leaf sidecar: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plan tree dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, phaseLeafSidecarName), raw, 0o644); err != nil {
		return fmt.Errorf("write phase-leaf sidecar: %w", err)
	}
	return nil
}

// readPhaseLeafSidecar loads a task's phase-index→leaf-path map from its
// plan-trees dir. Returns nil (not an error) when absent — flat-mode seals
// write no sidecar, and older tree-mode seals predate it.
func readPhaseLeafSidecar(treeRoot, taskID string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(treeRoot, taskID, phaseLeafSidecarName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read phase-leaf sidecar: %w", err)
	}
	var sidecar map[string]string
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return nil, fmt.Errorf("decode phase-leaf sidecar: %w", err)
	}
	return sidecar, nil
}

// asCompileError is a local errors.As helper (daemon package has no other
// use for the plan.CompileError type).
func asCompileError(err error, target **plan.CompileError) bool {
	ce, ok := err.(*plan.CompileError)
	if ok {
		*target = ce
	}
	return ok
}
