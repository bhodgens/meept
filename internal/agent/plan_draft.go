package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// planDraftMetadataKey is the task.Metadata key holding the brainstorm draft
// (plan-compiler leaf 04, Contract C). Same metadata-bag pattern as
// planning_context / pending_steps.
const planDraftMetadataKey = "plan_draft"

// planDraftTemplate is the agent-facing draft template path (leaf 01). When
// present in the prompts tiers it is rendered for the scaffold; otherwise
// fallbackDraftTemplate below is used (the template's own document skeleton).
const planDraftTemplate = "planner/plan_draft.md"

// PlanDraft is the brainstorm draft carried on a task's metadata while the
// user and planner agent iterate in plan-dialect v1 markdown. Sealing records
// the sealed document's sha256 hex in SealedHash; the draft bytes themselves
// are the immutable compile input.
type PlanDraft struct {
	Markdown   string    `json:"markdown"`
	Version    int       `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
	SealedHash string    `json:"sealed_hash,omitempty"`
}

func marshalPlanDraft(d *PlanDraft) ([]byte, error) {
	return json.Marshal(d)
}

func unmarshalPlanDraft(raw []byte) (*PlanDraft, error) {
	d := &PlanDraft{}
	if len(raw) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("failed to parse plan draft: %w", err)
	}
	return d, nil
}

// SetPlanCompilerEnabled toggles the draft/seal/compile pipeline on the
// planner. Nil-guarded per the repo's setter convention; also wired from
// daemon.go next to the SetParallelPhases site.
func (sp *StrategicPlanner) SetPlanCompilerEnabled(enabled bool) {
	if sp == nil {
		return
	}
	sp.planCompilerEnabled = enabled
}

// planCompilerFlag reads the flag nil-safely.
func (sp *StrategicPlanner) planCompilerFlag() bool {
	if sp == nil {
		return false
	}
	return sp.planCompilerEnabled
}

// DraftFor returns the task's current draft (ok=false when none exists).
// Reads through the task store so drafts survive process restarts exactly
// like every other piece of task metadata.
func (sp *StrategicPlanner) DraftFor(taskID string) (*PlanDraft, bool) {
	if sp == nil || sp.taskStore == nil || taskID == "" {
		return nil, false
	}
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return nil, false
	}
	return sp.draftFromMetadata(t)
}

// draftFromMetadata extracts the draft from a task's metadata bag.
func (sp *StrategicPlanner) draftFromMetadata(t *task.Task) (*PlanDraft, bool) {
	if t == nil || len(t.Metadata) == 0 {
		return nil, false
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(t.Metadata, &meta); err != nil {
		return nil, false
	}
	raw, ok := meta[planDraftMetadataKey]
	if !ok {
		return nil, false
	}
	d, err := unmarshalPlanDraft(raw)
	if err != nil {
		sp.logger.Warn("Corrupt plan_draft metadata; ignoring", "task_id", t.ID, "error", err)
		return nil, false
	}
	return d, true
}

// SaveDraft stores (or replaces) the task's brainstorm draft. Version is
// incremented per save and updated_at stamped. Refuses tasks that are not in
// the planning state — drafts belong to the brainstorm phase only.
func (sp *StrategicPlanner) SaveDraft(taskID, markdown string) error {
	if sp == nil || sp.taskStore == nil {
		return fmt.Errorf("draft store not available")
	}
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.State != task.StatePlanning {
		return fmt.Errorf("task %s is in state %q; drafts may only be edited while planning", taskID, t.State)
	}

	d, _ := sp.draftFromMetadata(t)
	if d == nil {
		d = &PlanDraft{}
	}
	d.Markdown = markdown
	d.Version++
	d.UpdatedAt = time.Now().UTC()
	d.SealedHash = "" // content changed: a prior seal hash is stale

	if err := sp.storeDraft(t, d); err != nil {
		return err
	}
	sp.logger.Info("Plan draft saved", "task_id", taskID, "version", d.Version)
	return nil
}

// SealDraft stamps the sealed document's hash on the draft (sealing is a
// state change, not an edit). Errors when no draft exists.
func (sp *StrategicPlanner) SealDraft(taskID, hash string) error {
	if sp == nil || sp.taskStore == nil {
		return fmt.Errorf("draft store not available")
	}
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	d, ok := sp.draftFromMetadata(t)
	if !ok {
		return fmt.Errorf("no draft for task %s", taskID)
	}
	d.SealedHash = hash
	d.UpdatedAt = time.Now().UTC()
	return sp.storeDraft(t, d)
}

// storeDraft merges the draft into the task's metadata and persists the task.
func (sp *StrategicPlanner) storeDraft(t *task.Task, d *PlanDraft) error {
	raw, err := marshalPlanDraft(d)
	if err != nil {
		return fmt.Errorf("failed to marshal plan draft: %w", err)
	}
	t.Metadata = mergeMetadata(t.Metadata, map[string]json.RawMessage{
		planDraftMetadataKey: raw,
	})
	if err := sp.taskStore.Update(t); err != nil {
		return fmt.Errorf("failed to persist plan draft: %w", err)
	}
	return nil
}

// renderDraftScaffold renders the initial draft for a fresh brainstorm task:
// the leaf-01 template (config/prompts/planner/plan_draft.md) when a tier
// carries it, else the built-in skeleton. No LLM is involved: Goal and the
// task_id meta come straight from the request.
func (sp *StrategicPlanner) renderDraftScaffold(taskID, request string) (string, error) {
	if sp.templateLoader != nil {
		if md, err := sp.templateLoader.render(planDraftTemplate, map[string]any{
			"TaskID": taskID,
			"Input":  strings.TrimSpace(request),
		}); err == nil {
			return md, nil
		}
		// Missing or malformed template: fall through to the built-in
		// skeleton rather than failing task creation on a config issue.
	}
	return fallbackDraftTemplate(taskID, request), nil
}

// fallbackDraftTemplate mirrors the leaf-01 template's document skeleton
// (docs/workflows/plan-dialect.md section 2) with Goal seeded from the
// request and Open Questions empty.
func fallbackDraftTemplate(taskID, request string) string {
	goal := strings.TrimSpace(request)
	if goal == "" {
		goal = "(describe the goal)"
	}
	if len(goal) > 300 {
		goal = goal[:297] + "..."
	}
	updated := time.Now().UTC().Format("2006-01-02")
	return fmt.Sprintf(`# Plan: %s

## Meta

- task_id: %s
- version: 1
- status: draft
- updated: %s

## Goal

%s.

## Decisions

## Open Questions

## Phases

## Notes
`, firstLineAsTitle(request), taskID, updated, goal)
}

// firstLineAsTitle derives a one-line plan title from the request.
func firstLineAsTitle(request string) string {
	line := strings.TrimSpace(request)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		line = "Untitled plan"
	}
	if len(line) > 80 {
		line = line[:77] + "..."
	}
	return line
}

// FlattenPlanPhasesToSteps converts phase specs into executable TaskSteps.
// Each step gets Phase = phase.Name; inter-phase dependencies: the first
// step of phase N+1 depends on the last step of phase N (unless the step
// already has explicit deps). maxStepsPerPhase <= 0 disables the per-phase
// cap. Extracted from planMultiPhase (plan-compiler leaf 04) so the seal
// path flattens identically to the LLM spec_plan path.
//
// H4 regression guard: the original extraction dropped the legacy per-phase
// cap that caf61fb2 enforced inline (`len(stepIDsInPhase) >= cap → break`),
// letting un-flagged phases exceed the cap and break byte-identical legacy
// behavior. The cap parameter is threaded explicitly; callers pass
// sp.maxStepsPerPhase (or sp.MaxStepsPerPhase()).
func FlattenPlanPhasesToSteps(taskID string, phases []PlanPhaseSpec, maxStepsPerPhase int) []*task.TaskStep {
	var steps []*task.TaskStep
	var prevPhaseLastStepID string
	for phaseIdx, phase := range phases {
		var stepIDsInPhase []string
		for stepIdx, ps := range phase.Steps {
			// Cap per-phase steps (legacy caf61fb2 invariant).
			if maxStepsPerPhase > 0 && len(stepIDsInPhase) >= maxStepsPerPhase {
				break
			}
			seq := phaseIdx*1000 + stepIdx // stable sequence across phases
			step := task.NewTaskStep(taskID, ps.Description, seq)
			step.ToolHint = ps.ToolHint
			step.Phase = phase.Name
			// Within-phase dependencies (0-indexed → step IDs).
			for _, depIdx := range ps.DependsOn {
				if depIdx >= 0 && depIdx < len(stepIDsInPhase) {
					step.DependsOn = append(step.DependsOn, stepIDsInPhase[depIdx])
				}
			}
			// Inter-phase dependency: first step of phase N+1 depends on
			// last step of phase N (unless this is phase 0 or already has deps).
			if stepIdx == 0 && prevPhaseLastStepID != "" && len(step.DependsOn) == 0 {
				step.DependsOn = append(step.DependsOn, prevPhaseLastStepID)
			}
			steps = append(steps, step)
			stepIDsInPhase = append(stepIDsInPhase, step.ID)
		}
		if len(stepIDsInPhase) > 0 {
			prevPhaseLastStepID = stepIDsInPhase[len(stepIDsInPhase)-1]
		}
	}
	return steps
}

// MaxPhases returns the compile phase cap for the seal path — the strategic
// planner's own multi-phase cap (MaxPhases config; default 12), reused per
// master.md leaf-04 Notes rather than adding a second knob.
func (sp *StrategicPlanner) MaxPhases() int {
	if sp == nil || sp.maxPhases <= 0 {
		return 12
	}
	return sp.maxPhases
}

// MaxStepsPerPhase returns the per-phase step cap (0 = uncapped).
func (sp *StrategicPlanner) MaxStepsPerPhase() int {
	if sp == nil {
		return 0
	}
	return sp.maxStepsPerPhase
}

// PhaseSpecsFromPlan converts compiler output ([]plan.PhaseSpec) into agent
// phase specs (field-parity map pinned by leaf 02's round-trip test).
// Exported for the daemon's seal wiring — plannerStep is unexported, so the
// conversion must live in this package. NOTE the depends_on semantics shift:
// the compiler writes 1-based step numbers (PhaseN.S<#> refs); the LLM
// plannerStep contract is 0-indexed — decrement here so FlattenPlanPhasesTo
// Steps resolves the same edges for both producers.
func PhaseSpecsFromPlan(in []plan.PhaseSpec) []PlanPhaseSpec {
	out := make([]PlanPhaseSpec, 0, len(in))
	for _, p := range in {
		// Phase-level DependsOn: the compiler writes 1-based ordinals
		// (same dialect as step refs), but PlanPhaseSpec.DependsOn is
		// 0-indexed — decrement with a >=1 guard, mirroring the step-dep
		// conversion below/above. (2026-09-08 audit M11: ordinals passed
		// through unchanged violated the plan-compiler OPEN-QUESTIONS
		// errata "must subtract 1".)
		phaseDeps := make([]int, 0, len(p.DependsOn))
		for _, d := range p.DependsOn {
			if d >= 1 {
				phaseDeps = append(phaseDeps, d-1)
			}
		}
		steps := make([]plannerStep, 0, len(p.Steps))
		for _, s := range p.Steps {
			deps := make([]int, 0, len(s.DependsOn))
			for _, d := range s.DependsOn {
				if d >= 1 {
					deps = append(deps, d-1)
				}
			}
			steps = append(steps, plannerStep{
				Description: s.Description,
				ToolHint:    s.ToolHint,
				DependsOn:   deps,
			})
		}
		out = append(out, PlanPhaseSpec{
			Name:        p.Name,
			Description: p.Description,
			Steps:       steps,
			Produces:    p.Produces,
			Consumes:    p.Consumes,
			DependsOn:   phaseDeps,
		})
	}
	return out
}

// PhaseSpecsToPlan converts agent phase specs back into the pure
// compiler/emitter shape for the flat-vs-tree gate.
func PhaseSpecsToPlan(in []PlanPhaseSpec) *plan.CompiledPlan {
	cp := &plan.CompiledPlan{}
	for _, p := range in {
		steps := make([]plan.StepSpec, 0, len(p.Steps))
		for _, s := range p.Steps {
			steps = append(steps, plan.StepSpec{
				Description: s.Description,
				ToolHint:    s.ToolHint,
				DependsOn:   s.DependsOn,
			})
		}
		cp.Phases = append(cp.Phases, plan.PhaseSpec{
			Name:        p.Name,
			Description: p.Description,
			Steps:       steps,
			Produces:    p.Produces,
			Consumes:    p.Consumes,
			DependsOn:   p.DependsOn,
		})
	}
	return cp
}

// SealPlan runs the full seal pipeline for a brainstorm task, mirroring
// ApprovePlan's tail with the compiled phases in place of pending steps:
// flatten → persist steps → generate spec → executing → promote ready →
// task.planned + orchestrator.schedule. PersistPhases (the compiled phase
// declarations) runs before the task state change; failures there abort the
// seal with the task still in planning.
func (sp *StrategicPlanner) SealPlan(ctx context.Context, taskID string, phases []PlanPhaseSpec, persistPhases func(taskID string, phases []PlanPhaseSpec) error) error {
	if sp == nil || sp.taskStore == nil || sp.stepStore == nil {
		return fmt.Errorf("plan seal not available")
	}
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.State != task.StatePlanning {
		return fmt.Errorf("task %s is in state %q, expected %q (only planning tasks seal)", taskID, t.State, task.StatePlanning)
	}

	// Flatten phases into executable steps (same shape as spec_plan).
	// H4: pass the planner's per-phase cap (0 = uncapped) so the seal path
	// enforces the same legacy invariant as the LLM spec_plan path.
	steps := FlattenPlanPhasesToSteps(taskID, phases, sp.MaxStepsPerPhase())
	if len(steps) == 0 {
		return fmt.Errorf("sealed plan produced no executable steps")
	}

	// Compiled phase declarations → plan store (existing persistence).
	if persistPhases != nil {
		if err := persistPhases(taskID, phases); err != nil {
			return fmt.Errorf("failed to persist compiled phases: %w", err)
		}
	}

	// Persist steps (mirror ApprovePlan).
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			sp.logger.Error("Failed to persist sealed step", "step_id", step.ID, "error", err)
			return fmt.Errorf("failed to persist steps: %w", err)
		}
	}

	// Generate spec from planned steps and store on the task. The spec is
	// merged key-wise (not via StoreSpecInTask, which rewrites the metadata
	// blob and would drop the plan_draft sealed-hash key stamped moments
	// ago). Metadata is re-read first: SealDraft wrote the hash between our
	// initial fetch and here, and Store.Update rewrites the whole blob.
	if latest, latestErr := sp.taskStore.GetByID(taskID); latestErr == nil && latest != nil {
		t.Metadata = latest.Metadata
	}
	spec := GenerateSpecFromSteps(steps)
	if specJSON, specErr := json.Marshal(spec); specErr == nil {
		t.Metadata = mergeMetadata(t.Metadata, map[string]json.RawMessage{
			"spec": specJSON,
		})
	}

	// H12: atomic counter set + counter-free state write — the full-row
	// Update would write stale snapshot counters over concurrent increments
	// (and re-erase the metadata the re-read above deliberately refreshed).
	if err := sp.taskStore.SetPlanCounters(taskID, len(steps), 0, 0); err != nil {
		sp.logger.Error("Failed to set task counters after seal", "error", err)
	}
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.UpdateWithoutCounters(t); err != nil {
		sp.logger.Error("Failed to update task after seal", "error", err)
		return fmt.Errorf("failed to update task: %w", err)
	}

	// Promote root steps (no dependencies) to ready.
	promoted, err := sp.stepStore.PromoteReadySteps(taskID)
	if err != nil {
		sp.logger.Error("Failed to promote sealed steps", "error", err)
	} else {
		sp.logger.Info("Promoted root sealed steps",
			"task_id", taskID,
			"promoted", len(promoted),
			"total_steps", len(steps),
		)
	}

	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID:     taskID,
		"total_steps": len(steps),
		"ready_steps": len(promoted),
		"mode":        "plan_compiler",
	})
	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: taskID,
	})

	sp.logger.Info("Plan sealed and scheduled",
		"task_id", taskID,
		"phases", len(phases),
		"steps", len(steps),
	)
	return nil
}

// seedDraftFromRequest stores the initial scaffold for taskID (used by the
// interview gate in Plan()).
func (sp *StrategicPlanner) seedDraftFromRequest(taskID, request string) error {
	md, err := sp.renderDraftScaffold(taskID, request)
	if err != nil {
		return fmt.Errorf("render draft scaffold: %w", err)
	}
	return sp.SaveDraft(taskID, md)
}
