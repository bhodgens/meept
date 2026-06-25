package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/plan"
)

// startNextPhase transitions a task from a completed phase to the next.
// It assigns fresh conversationIDs (no raw history propagation), injects
// consumes artifacts + phase description as structured context, and gates
// on checkPhaseReady.
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

	// 2. Find the phase spec to get consumes/produces declarations.
	phaseSpec, err := o.getPlanPhaseSpec(taskID, nextPhase.Name)
	if err != nil {
		o.logger.Warn("could not load phase spec for context injection",
			"phase", nextPhase.Name, "error", err)
		phaseSpec = &PlanPhaseSpec{Name: nextPhase.Name}
	}

	// 3. Gate on consumes readiness.
	if o.artifacts != nil {
		if err := checkPhaseReady(phaseSpec, o.artifacts); err != nil {
			return fmt.Errorf("phase not ready: %w", err)
		}
	}

	// 4. Build startup context.
	startupCtx := o.renderPhaseStartup(phaseSpec, o.artifacts)

	// 5. Update steps: fresh conversationID + startup context.
	if o.stepStore == nil {
		return fmt.Errorf("step store not wired")
	}
	steps, err := o.stepStore.GetPhaseSteps(taskID, nextPhase.Name)
	if err != nil {
		return fmt.Errorf("get steps by phase: %w", err)
	}
	for _, step := range steps {
		step.ConversationID = fmt.Sprintf("phase-%s-%s", nextPhase.ID, step.ID)
		step.AccumulatedContext = startupCtx
		if err := o.stepStore.Update(step); err != nil {
			return fmt.Errorf("update step %s: %w", step.ID, err)
		}
	}

	o.logger.Info("Phase transition",
		"task_id", taskID,
		"from", completedPhaseName,
		"to", nextPhase.Name,
		"steps", len(steps),
	)
	return nil
}

// getPlanPhaseSpec reads the phase spec (produces/consumes declarations) for
// a given phase. If a test override is present, it takes precedence.
//
// Until Task 7 persists produces/consumes on PlanPhase records, this returns
// a synthetic spec containing only the Name. The artifact gating still works
// because phaseSpecOverride (set by tests or by the planPhaseSink) can inject
// full specs.
func (o *Orchestrator) getPlanPhaseSpec(taskID, phaseName string) (*PlanPhaseSpec, error) {
	// Test override takes precedence.
	if o.phaseSpecOverride != nil {
		if spec, ok := o.phaseSpecOverride[phaseName]; ok {
			return spec, nil
		}
	}

	// Return synthetic spec from PlanPhase fields.
	// After Task 7, produces/consumes will be persisted on PlanPhase and
	// populated here.
	phases, err := o.planManager.GetPhasesByTask(context.Background(), taskID)
	if err != nil {
		return nil, fmt.Errorf("get phases for spec: %w", err)
	}
	for _, p := range phases {
		if p.Name == phaseName {
			return &PlanPhaseSpec{
				Name: p.Name,
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
