package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// startNextPhase transitions a task from a completed phase to the next.
// It keeps the serial list-order selection (next phase after
// completedPhaseName) and delegates the actual phase start to startPhase.
//
// Returns nil (no-op) if there is no next phase after completedPhaseName.
func (o *Orchestrator) startNextPhase(ctx context.Context, taskID, completedPhaseName string) error {
	if o.planManager == nil {
		return fmt.Errorf("plan manager not wired")
	}

	// 1. Find next phase after the completed one.
	phases, err := o.planManager.GetPhasesByTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get phases: %w", err)
	}
	var nextPhase *plan.PlanPhase
	foundCompleted := false
	for i := range phases {
		if foundCompleted {
			nextPhase = phases[i]
			break
		}
		if phases[i].Name == completedPhaseName {
			foundCompleted = true
		}
	}
	if nextPhase == nil {
		// No next phase; task may be complete.
		return nil
	}

	return o.startPhase(ctx, taskID, nextPhase, completedPhaseName)
}

// startPhase activates a single phase: it loads the phase spec, gates on
// checkPhaseReady, renders startup context, stamps fresh conversationIDs
// (phase-<phaseID>-<stepID>) plus accumulated context over the phase's
// steps, and notifies phase-transition subscribers.
//
// Extracted verbatim from startNextPhase's steps 2-5 so both the serial
// path (startNextPhase, fromPhase = completed phase name) and the frontier
// path (advancePhasesFrontier, fromPhase = "") share one implementation.
//
// Re-entrancy guard: if every step of p is already past StepPending, the
// phase was already started and stamping would be a no-op overwrite — skip
// it (protection against double terminal events under frontier dispatch).
func (o *Orchestrator) startPhase(ctx context.Context, taskID string, p *plan.PlanPhase, fromPhase string) error {
	// Find the phase spec to get consumes/produces declarations.
	phaseSpec, err := o.getPlanPhaseSpec(ctx, taskID, p.Name)
	if err != nil {
		o.logger.Warn("could not load phase spec for context injection",
			"phase", p.Name, "error", err)
		phaseSpec = &PlanPhaseSpec{Name: p.Name}
	}

	// Gate on consumes readiness.
	if o.artifacts != nil {
		if err := checkPhaseReady(phaseSpec, o.artifacts); err != nil {
			return fmt.Errorf("phase not ready: %w", err)
		}
	}

	// Build startup context.
	startupCtx := o.renderPhaseStartup(phaseSpec, o.artifacts)

	// Update steps: fresh conversationID + startup context.
	if o.stepStore == nil {
		return fmt.Errorf("step store not wired")
	}
	steps, err := o.stepStore.GetPhaseSteps(taskID, p.Name)
	if err != nil {
		return fmt.Errorf("get steps by phase: %w", err)
	}
	// Re-entrancy guard: any step past StepPending means the phase already
	// started (stamping again would be a no-op anyway). Uses the shared
	// task state constants rather than reimplementing state checks.
	allPastPending := true
	for _, step := range steps {
		if step.State == task.StepPending {
			allPastPending = false
			break
		}
	}
	if allPastPending {
		o.logger.Debug("phase already started; skipping double-start",
			"task_id", taskID, "phase", p.Name)
		return nil
	}
	for _, step := range steps {
		step.ConversationID = fmt.Sprintf("phase-%s-%s", p.ID, step.ID)
		step.AccumulatedContext = startupCtx
	}
	if err := o.stepStore.UpdatePhaseSteps(steps); err != nil {
		return fmt.Errorf("update phase steps: %w", err)
	}

	// Notify phase-transition subscribers (e.g., hierarchical budget advancement
	// on active AgentLoops). Best-effort: panic-safe so a buggy subscriber never
	// breaks a phase transition.
	if o.onPhaseTransition != nil {
		func() {
			defer func() { _ = recover() }()
			o.onPhaseTransition(taskID, fromPhase, p.Name)
		}()
	}

	o.logger.Info("Phase transition",
		"task_id", taskID,
		"from", fromPhase,
		"to", p.Name,
		"steps", len(steps),
	)
	return nil
}

// getPlanPhaseSpec reads the phase spec (produces/consumes declarations) for
// a given phase. If a test override is present, it takes precedence.
//
// Task 7 persists produces/consumes on PlanPhase records (internal/plan/plan.go
// Produces/Consumes fields, stored as JSON columns in plan_phases). We read
// them back here so artifact gating and phase-startup context injection work
// at runtime — not just under test overrides.
func (o *Orchestrator) getPlanPhaseSpec(ctx context.Context, taskID, phaseName string) (*PlanPhaseSpec, error) {
	// Test override takes precedence.
	if o.phaseSpecOverride != nil {
		if spec, ok := o.phaseSpecOverride[phaseName]; ok {
			return spec, nil
		}
	}

	// Read the persisted PlanPhase record (produces/consumes populated by
	// plan store, Task 7).
	phases, err := o.planManager.GetPhasesByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get phases for spec: %w", err)
	}
	for _, p := range phases {
		if p.Name == phaseName {
			return &PlanPhaseSpec{
				Name:     p.Name,
				Produces: p.Produces,
				Consumes: p.Consumes,
			}, nil
		}
	}
	return nil, fmt.Errorf("phase %q not found in task %q", phaseName, taskID)
}

// renderPhaseStartup builds the structured context injected into the first
// step of a new phase. Contains: phase header, description, and consumed
// artifacts. NO raw history from prior phases is included.
func (o *Orchestrator) renderPhaseStartup(phase *PlanPhaseSpec, store *artifactStore) string {
	return renderPhaseStartup(phase, store)
}

// renderPhaseStartup is the free-function implementation (testable without an
// Orchestrator instance).
func renderPhaseStartup(phase *PlanPhaseSpec, store *artifactStore) string {
	var sb strings.Builder
	sb.WriteString("## Phase: " + phase.Name + "\n\n")
	if phase.Description != "" {
		sb.WriteString(phase.Description + "\n\n")
	}
	if len(phase.Consumes) > 0 && store != nil {
		sb.WriteString("## Inputs from prior phases\n\n")
		for _, c := range phase.Consumes {
			art, ok := store.Get(c.Name)
			if !ok {
				if c.Required {
					sb.WriteString(fmt.Sprintf("- MISSING: %s (required)\n", c.Name))
				}
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", art.Name, art.Kind, art.Description))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
