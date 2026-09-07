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

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/rpc"
)

// sealPipelineState carries the compiled phases from the Compile seam to the
// Execute seam (Execute's signature is taskID-only, mirroring ApproveFunc).
// plan.seal is a synchronous single-caller CLI action; the mutex keeps the
// seams race-clean regardless.
type sealPipelineState struct {
	mu     sync.Mutex
	phases []agent.PlanPhaseSpec
}

// take returns and clears the compiled phases.
func (s *sealPipelineState) take() []agent.PlanPhaseSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.phases
	s.phases = nil
	return p
}

// store remembers the compiled phases.
func (s *sealPipelineState) store(p []agent.PlanPhaseSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phases = p
}

// adaptCompileSealed wraps plan.CompileSealed into the handler's Compile
// seam. Returns the compiler-native []plan.PhaseSpec (the handler's
// gate/emitter helpers consume that shape) plus hash/warnings/problems;
// compiled phases are staged for the Execute seam.
func adaptCompileSealed(state *sealPipelineState) func(markdown string, maxPhases int) (any, string, []string, []rpc.CompileProblemView, error) {
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
		state.store(agent.PhaseSpecsFromPlan(cp.Phases))
		return cp.Phases, cp.Hash, cp.Warnings, nil, nil
	}
}

// adaptPersistPhases persists compiled phases via the plan store (the same
// CreatePhase call the planPhaseSink uses) and writes the emitted tree (tree
// mode) under the plan-trees root. Flat phases persist in both modes — the
// orchestrator executes phases; the tree is the human/leaf-agent artifact
// whose per-leaf paths ride the persisted phase name for the future
// leaf-dispatch tree (integration point documented in
// docs/workflows/agent-orchestration.md).
func adaptPersistPhases(planMgr *plan.PlanManager, treeRoot string, logger *slog.Logger) func(taskID string, phases any, tree *plan.EmittedTree) error {
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
			if tree != nil {
				// Tree-mode integration point: the phase records its leaf
				// file path (PlanPhase has no free-text field; the path
				// annotation rides the name) for the leaf-dispatch layer
				// (follow-up tree; NOT wired here).
				if path := treeLeafPathForPhase(tree, i); path != "" {
					phaseRecord.Name = fmt.Sprintf("%s [tree leaf: %s]", p.Name, path)
				}
			}
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
			logger.Info("Plan tree emitted",
				"task_id", taskID, "dir", dir, "leaves", len(tree.Leaves))
		}
		return nil
	}
}

// adaptExecute seals through StrategicPlanner.SealPlan: the ApprovePlan
// mirror (persist steps → spec → executing → promote → schedule). The
// compiled phases come from the staged pipeline state.
func adaptExecute(sp *agent.StrategicPlanner, state *sealPipelineState) func(taskID string) error {
	return func(taskID string) error {
		phases := state.take()
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
		adaptCompileSealed(state),
		adaptPersistPhases(planMgr, treeRoot, logger),
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

// treeLeafPathForPhase picks the emitted leaf corresponding to a phase index
// (leaves are ordered; each phase's first leaf carries its work items).
func treeLeafPathForPhase(tree *plan.EmittedTree, phaseIdx int) string {
	if tree == nil || len(tree.Leaves) == 0 {
		return ""
	}
	if phaseIdx < len(tree.Leaves) {
		return tree.Leaves[phaseIdx].Path
	}
	return tree.Leaves[len(tree.Leaves)-1].Path
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
