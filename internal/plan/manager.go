package plan

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TaskCreator abstracts task creation, decoupling PlanManager from task.Store.
// Implemented by task.Registry or a daemon-provided adapter.
type TaskCreator interface {
	CreateTask(ctx context.Context, name, description string) (*task.Task, error)
	CreateTaskStep(ctx context.Context, taskID, description string, sequence int) (*task.TaskStep, error)
	LinkSession(ctx context.Context, taskID, sessionID string) error
}

// PlanManager orchestrates plan lifecycle: creation, approval, synthesis
// into tasks, progress tracking, and confirmation.
type PlanManager struct {
	store        PlanStore
	bus          *bus.MessageBus
	config       config.PlansConfig
	taskCreator  TaskCreator
	logger       *slog.Logger
	phaseTaskMap map[string]string // taskID -> phaseID, populated during Synthesize
	taskPlanMap  map[string]string // taskID -> planID, populated during Synthesize
}

// NewPlanManager creates a new PlanManager.
func NewPlanManager(store PlanStore, bus *bus.MessageBus, cfg config.PlansConfig, taskCreator TaskCreator, logger *slog.Logger) *PlanManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlanManager{
		store:        store,
		bus:          bus,
		config:       cfg,
		taskCreator:  taskCreator,
		logger:       logger.With("component", "plan-manager"),
		phaseTaskMap: make(map[string]string),
		taskPlanMap:  make(map[string]string),
	}
}

// ---------------------------------------------------------------------------
// Lifecycle methods
// ---------------------------------------------------------------------------

// CreatePlan creates a new plan, stores it, writes the initial plan.md, and
// publishes a plan.created event.
func (m *PlanManager) CreatePlan(ctx context.Context, title, description, projectID, projectPath, sessionID string) (*Plan, error) {
	dir := m.resolvePlanDir(projectPath)
	filePath := filepath.Join(dir, slugify(title)+".md")

	plan := NewPlan(title, description, projectID, filePath, sessionID)

	if err := m.store.CreatePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}

	if sessionID != "" {
		if err := m.store.LinkSession(ctx, plan.ID, sessionID); err != nil {
			m.logger.Warn("failed to link session", "plan_id", plan.ID, "error", err)
		}
	}

	// Write initial plan.md with empty phases.
	if err := WritePlanMarkdown(filePath, plan, nil); err != nil {
		m.logger.Warn("failed to write initial plan markdown", "path", filePath, "error", err)
	}

	// Transition from planning -> draft.
	plan.State = StateDraft
	plan.UpdatedAt = time.Now().UTC()
	if err := m.store.SetPlanState(ctx, plan.ID, StateDraft); err != nil {
		return nil, fmt.Errorf("set plan state to draft: %w", err)
	}

	m.publishEvent("plan.created", map[string]any{
		"plan_id":    plan.ID,
		"title":      plan.Title,
		"project_id": plan.ProjectID,
	})

	m.logger.Info("plan created", "plan_id", plan.ID, "title", title)
	return plan, nil
}

// SubmitPlan transitions a plan from draft to pending_approval (or auto-approves).
func (m *PlanManager) SubmitPlan(ctx context.Context, planID string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.State != StateDraft {
		return fmt.Errorf("plan %s is in state %s, expected draft", planID, plan.State)
	}

	if err := m.store.SetPlanState(ctx, planID, StatePendingApproval); err != nil {
		return fmt.Errorf("set plan state: %w", err)
	}

	m.publishEvent("plan.submitting", map[string]any{
		"plan_id": planID,
	})

	// Auto-approve if approval is not required.
	if !m.config.Approval.RequireApproval {
		m.logger.Info("auto-approving plan", "plan_id", planID)
		// Use empty session/by for auto-approval.
		if err := m.ApprovePlan(ctx, planID, "", "system"); err != nil {
			return fmt.Errorf("auto-approve plan: %w", err)
		}
	}

	m.logger.Info("plan submitted", "plan_id", planID)
	return nil
}

// ApprovePlan approves a pending plan, records the signoff, and triggers synthesis.
func (m *PlanManager) ApprovePlan(ctx context.Context, planID, sessionID, by string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.State != StatePendingApproval {
		return fmt.Errorf("plan %s is in state %s, expected pending_approval", planID, plan.State)
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "approved", "")
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	now := time.Now().UTC()
	plan.State = StateApproved
	plan.ApprovedAt = &now
	plan.ApprovedBy = by
	plan.UpdatedAt = now
	if err := m.store.UpdatePlan(ctx, plan); err != nil {
		return fmt.Errorf("update plan: %w", err)
	}

	m.publishEvent("plan.approved", map[string]any{
		"plan_id": planID,
		"by":      by,
	})

	m.logger.Info("plan approved", "plan_id", planID, "by", by)

	// Trigger synthesis.
	if err := m.Synthesize(ctx, planID); err != nil {
		return fmt.Errorf("synthesize plan: %w", err)
	}

	return nil
}

// RejectPlan rejects a pending plan.
func (m *PlanManager) RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.State != StatePendingApproval {
		return fmt.Errorf("plan %s is in state %s, expected pending_approval", planID, plan.State)
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "rejected", reason)
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	if err := m.store.SetPlanState(ctx, planID, StateCancelled); err != nil {
		return fmt.Errorf("set plan state: %w", err)
	}

	m.publishEvent("plan.rejected", map[string]any{
		"plan_id": planID,
		"by":      by,
		"reason":  reason,
	})

	m.logger.Info("plan rejected", "plan_id", planID, "by", by)
	return nil
}

// RevisePlan requests revision of a plan, sending it back to planning state.
func (m *PlanManager) RevisePlan(ctx context.Context, planID, sessionID, feedback string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.State != StatePendingApproval && plan.State != StateCancelled {
		return fmt.Errorf("plan %s is in state %s, expected pending_approval or cancelled", planID, plan.State)
	}

	// Check revision count.
	if m.config.Approval.MaxRevisions > 0 {
		revCount, err := m.store.GetRevisionCount(ctx, planID)
		if err != nil {
			return fmt.Errorf("get revision count: %w", err)
		}
		if revCount >= m.config.Approval.MaxRevisions {
			return fmt.Errorf("plan %s has reached max revisions (%d)", planID, m.config.Approval.MaxRevisions)
		}
	}

	signoff := NewPlanSignoff(planID, "", sessionID, "", "revision_requested", feedback)
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	plan.RevisionCount++
	plan.State = StatePlanning
	plan.UpdatedAt = time.Now().UTC()
	if err := m.store.UpdatePlan(ctx, plan); err != nil {
		return fmt.Errorf("update plan: %w", err)
	}

	m.publishEvent("plan.revised", map[string]any{
		"plan_id":          planID,
		"revision_count":   plan.RevisionCount,
		"feedback":         feedback,
	})

	m.logger.Info("plan revised", "plan_id", planID, "revision_count", plan.RevisionCount)
	return nil
}

// ConfirmPlan confirms a completed plan after review.
func (m *PlanManager) ConfirmPlan(ctx context.Context, planID, sessionID, by string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.State != StateCompleted {
		return fmt.Errorf("plan %s is in state %s, expected completed", planID, plan.State)
	}

	signoff := NewPlanSignoff(planID, "", sessionID, by, "confirmed", "")
	if err := m.store.CreateSignoff(ctx, signoff); err != nil {
		return fmt.Errorf("create signoff: %w", err)
	}

	now := time.Now().UTC()
	plan.State = StateConfirmed
	plan.ConfirmedAt = &now
	plan.ConfirmedBy = by
	plan.UpdatedAt = now
	if err := m.store.UpdatePlan(ctx, plan); err != nil {
		return fmt.Errorf("update plan: %w", err)
	}

	// Update plan.md to reflect confirmed state.
	phases, _ := m.store.GetPhases(ctx, planID)
	if err := UpdatePlanStatus(plan.FilePath, StateConfirmed, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on confirm", "plan_id", planID, "error", err)
	}

	m.publishEvent("plan.confirmed", map[string]any{
		"plan_id": planID,
		"by":      by,
	})

	m.logger.Info("plan confirmed", "plan_id", planID, "by", by)
	return nil
}

// CancelPlan cancels a plan for the given reason.
func (m *PlanManager) CancelPlan(ctx context.Context, planID, reason string) error {
	if err := m.store.SetPlanState(ctx, planID, StateCancelled); err != nil {
		return fmt.Errorf("set plan state: %w", err)
	}

	m.publishEvent("plan.cancelled", map[string]any{
		"plan_id": planID,
		"reason":  reason,
	})

	m.logger.Info("plan cancelled", "plan_id", planID, "reason", reason)
	return nil
}

// ---------------------------------------------------------------------------
// Synthesis
// ---------------------------------------------------------------------------

// Synthesize creates the task hierarchy from the plan: a parent task with child
// tasks for each phase, and TaskSteps for each parsed step within a phase.
func (m *PlanManager) Synthesize(ctx context.Context, planID string) error {
	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		return fmt.Errorf("get phases: %w", err)
	}

	// Parse the plan.md file for step details.
	var parsed *ParsedPlan
	if plan.FilePath != "" {
		parsed, err = ParsePlan(plan.FilePath)
		if err != nil {
			m.logger.Warn("failed to parse plan.md for synthesis, using phases only", "error", err)
		}
	}

	// Build parsed phase lookup by sequence.
	parsedPhases := make(map[int]ParsedPhase)
	if parsed != nil {
		for _, pp := range parsed.Phases {
			parsedPhases[pp.Sequence] = pp
		}
	}

	// If no phases in store, create them from parsed plan.
	if len(phases) == 0 && parsed != nil {
		for _, pp := range parsed.Phases {
			phase := NewPlanPhase(planID, pp.Name, pp.Sequence, len(pp.Steps))
			if err := m.store.CreatePhase(ctx, phase); err != nil {
				return fmt.Errorf("create phase %s: %w", pp.Name, err)
			}
			phases = append(phases, phase)
		}
	}

	// Create parent task.
	parentTask, err := m.taskCreator.CreateTask(ctx, plan.Title, plan.Description)
	if err != nil {
		return fmt.Errorf("create parent task: %w", err)
	}

	// Link the plan's source session to the parent task.
	if plan.SourceSession != "" {
		if err := m.taskCreator.LinkSession(ctx, parentTask.ID, plan.SourceSession); err != nil {
			m.logger.Warn("failed to link session to parent task", "error", err)
		}
	}

	// Store the parent TaskID on the plan.
	plan.TaskID = parentTask.ID
	plan.State = StateExecuting

	// Track parent task -> plan mapping for OnTaskCompleted.
	m.taskPlanMap[parentTask.ID] = planID
	plan.UpdatedAt = time.Now().UTC()
	if err := m.store.UpdatePlan(ctx, plan); err != nil {
		return fmt.Errorf("update plan with task ID: %w", err)
	}

	// Create child tasks for each phase and steps within them.
	for _, phase := range phases {
		phaseLabel := fmt.Sprintf("Phase %d: %s", phase.Sequence, phase.Name)
		childTask, err := m.taskCreator.CreateTask(ctx, phaseLabel, fmt.Sprintf("Plan %s - %s", plan.Title, phaseLabel))
		if err != nil {
			return fmt.Errorf("create child task for phase %s: %w", phase.Name, err)
		}

		// Track mapping: childTask.ID -> phase.ID, childTask.ID -> plan.ID
		m.phaseTaskMap[childTask.ID] = phase.ID
		m.taskPlanMap[childTask.ID] = planID

		// Create TaskSteps from parsed step details.
		pp, hasParsed := parsedPhases[phase.Sequence]
		if hasParsed {
			for seq, step := range pp.Steps {
				ts, err := m.taskCreator.CreateTaskStep(ctx, childTask.ID, step.Description, seq+1)
				if err != nil {
					return fmt.Errorf("create task step %d for phase %s: %w", step.Number, phase.Name, err)
				}

				// Set dependencies from parsed depends_on (step numbers -> step IDs).
				// We resolve these after all steps are created, so store a marker for now.
				_ = ts // Step is persisted via CreateTaskStep
			}
		} else {
			// No parsed steps; create a single placeholder step.
			_, err := m.taskCreator.CreateTaskStep(ctx, childTask.ID, phaseLabel, 1)
			if err != nil {
				return fmt.Errorf("create placeholder step for phase %s: %w", phase.Name, err)
			}
		}

		// Mark phase in progress.
		if err := m.store.SetPhaseState(ctx, phase.ID, PhaseInProgress); err != nil {
			m.logger.Warn("failed to set phase state to in_progress", "phase_id", phase.ID, "error", err)
		}
	}

	// Update plan.md to reflect executing state.
	if err := UpdatePlanStatus(plan.FilePath, StateExecuting, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on synthesize", "error", err)
	}

	m.publishEvent("plan.executing", map[string]any{
		"plan_id":     planID,
		"task_id":     parentTask.ID,
		"phase_count": len(phases),
	})

	m.logger.Info("plan synthesized", "plan_id", planID, "task_id", parentTask.ID, "phases", len(phases))
	return nil
}

// ---------------------------------------------------------------------------
// Progress tracking
// ---------------------------------------------------------------------------

// OnStepCompleted is called when a task step completes. It updates the
// corresponding phase progress.
func (m *PlanManager) OnStepCompleted(ctx context.Context, taskID, stepID string) error {
	phaseID, ok := m.phaseTaskMap[taskID]
	if !ok {
		// Not a plan-tracked task; ignore silently.
		return nil
	}

	if err := m.store.IncrementPhaseProgress(ctx, phaseID, "completed_steps", 1); err != nil {
		return fmt.Errorf("increment phase progress: %w", err)
	}

	// Look up the planID for this task to scope the phase query.
	planID := m.taskPlanMap[taskID]

	// Check if the phase is now complete.
	phases, err := m.store.GetPhases(ctx, planID)
	if err != nil {
		return nil // non-fatal
	}

	for _, phase := range phases {
		if phase.ID == phaseID && phase.CompletedSteps >= phase.TotalSteps && phase.TotalSteps > 0 {
			if err := m.store.SetPhaseState(ctx, phaseID, PhaseCompleted); err != nil {
				m.logger.Warn("failed to set phase completed", "phase_id", phaseID, "error", err)
			}
			break
		}
	}

	return nil
}

// OnTaskCompleted is called when a task completes. If the task maps to a phase,
// it updates the phase state. If the task is the plan's parent task, it marks
// the plan as completed.
func (m *PlanManager) OnTaskCompleted(ctx context.Context, taskID string) error {
	phaseID, isPhase := m.phaseTaskMap[taskID]
	if isPhase {
		// Child task (phase) completed.
		if err := m.store.SetPhaseState(ctx, phaseID, PhaseCompleted); err != nil {
			m.logger.Warn("failed to set phase completed", "phase_id", phaseID, "error", err)
		}

		m.publishEvent("plan.phase_completed", map[string]any{
			"task_id":  taskID,
			"phase_id": phaseID,
		})
		return nil
	}

	// Check if this is the plan's parent task via taskPlanMap.
	planID, hasPlan := m.taskPlanMap[taskID]
	if !hasPlan {
		return nil
	}

	plan, err := m.store.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("get plan for completion: %w", err)
	}

	// Only transition if the plan is in executing state.
	if plan.State != StateExecuting {
		return nil
	}

	plan.State = StateCompleted
	plan.UpdatedAt = time.Now().UTC()
	if err := m.store.UpdatePlan(ctx, plan); err != nil {
		return fmt.Errorf("update plan to completed: %w", err)
	}

	// Update plan.md.
	phases, _ := m.store.GetPhases(ctx, plan.ID)
	if err := UpdatePlanStatus(plan.FilePath, StateCompleted, phasesToValues(phases)); err != nil {
		m.logger.Warn("failed to update plan.md on completion", "error", err)
	}

	m.publishEvent("plan.completed", map[string]any{
		"plan_id": plan.ID,
		"task_id": taskID,
	})

	m.logger.Info("plan completed", "plan_id", plan.ID)
	return nil
}

// ---------------------------------------------------------------------------
// Plan creation threshold
// ---------------------------------------------------------------------------

// ShouldCreatePlan determines whether a plan should be created based on config
// mode, step count, intent, and complexity keywords.
func (m *PlanManager) ShouldCreatePlan(intent string, stepCount int) bool {
	switch m.config.Mode {
	case "off":
		return false
	case "always":
		return true
	case "threshold":
		// Check always-plan intents.
		for _, ai := range m.config.Threshold.AlwaysPlanIntents {
			if strings.EqualFold(intent, ai) {
				return true
			}
		}
		// Check step count threshold.
		if m.config.Threshold.MinSteps > 0 && stepCount >= m.config.Threshold.MinSteps {
			return true
		}
		// Check complexity keywords.
		lower := strings.ToLower(intent)
		for _, kw := range m.config.Threshold.ComplexityKeywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolvePlanDir returns the directory where plan files should be stored.
func (m *PlanManager) resolvePlanDir(projectPath string) string {
	if m.config.Storage.ExternalPath != "" {
		expanded := os.ExpandEnv(m.config.Storage.ExternalPath)
		return expanded
	}
	defaultPath := m.config.Storage.DefaultPath
	if defaultPath == "" {
		defaultPath = "docs/plans"
	}
	return filepath.Join(projectPath, defaultPath)
}

// publishEvent creates a BusMessage and publishes it to the message bus.
func (m *PlanManager) publishEvent(eventType string, payload map[string]any) {
	if m.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "plan-manager", payload)
	if err != nil {
		m.logger.Warn("failed to create bus message", "event", eventType, "error", err)
		return
	}

	delivered := m.bus.Publish(eventType, msg)
	m.logger.Debug("published event", "event", eventType, "delivered", delivered)
}

// phasesToValues converts a slice of phase pointers to a slice of values.
func phasesToValues(phases []*PlanPhase) []PlanPhase {
	if phases == nil {
		return nil
	}
	vals := make([]PlanPhase, len(phases))
	for i, p := range phases {
		vals[i] = *p
	}
	return vals
}

// slugify converts a title into a filesystem-safe slug.
var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(title string) string {
	s := strings.ToLower(title)
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
