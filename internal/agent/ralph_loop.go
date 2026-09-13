package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// RalphLoopConfig holds configuration for the Ralph loop.
type RalphLoopConfig struct {
	Enabled           bool `json:"enabled"`
	MaxIterations     int  `json:"max_iterations"`     // Maximum replan cycles per task
	EvidenceRequired  bool `json:"evidence_required"`  // Require evidence for completion claims
	ChecklistRequired bool `json:"checklist_required"` // Require checklist completion
}

// DefaultRalphLoopConfig returns default Ralph loop configuration.
func DefaultRalphLoopConfig() RalphLoopConfig {
	return RalphLoopConfig{
		Enabled:           true,
		MaxIterations:     3,
		EvidenceRequired:  true,
		ChecklistRequired: true,
	}
}

// RalphLoop manages self-referential plan execution with automatic replanning.
type RalphLoop struct {
	config       RalphLoopConfig
	orchestrator *Orchestrator
	taskStore    *task.Store
	stepStore    *task.StepStore
	planManager  *plan.PlanManager
	bus          *bus.MessageBus
	logger       slog.Logger

	// Iteration tracking: task_id -> iteration count
	iterations map[string]int
	mu         sync.Mutex
}

// NewRalphLoop creates a new Ralph loop manager.
func NewRalphLoop(config RalphLoopConfig, orchestrator *Orchestrator, taskStore *task.Store, stepStore *task.StepStore, planManager *plan.PlanManager, bus *bus.MessageBus, logger *slog.Logger) *RalphLoop {
	if logger == nil {
		logger = slog.Default()
	}
	return &RalphLoop{
		config:       config,
		orchestrator: orchestrator,
		taskStore:    taskStore,
		stepStore:    stepStore,
		planManager:  planManager,
		bus:          bus,
		logger:       *logger,
		iterations:   make(map[string]int),
	}
}

// SetPlanManager sets the plan manager. This is called by the daemon after
// the PlanManager is created (the plan system is initialized after agent
// components in NewComponents, so the value passed to NewRalphLoop is nil).
func (rl *RalphLoop) SetPlanManager(pm *plan.PlanManager) {
	if pm != nil {
		rl.planManager = pm
	}
}

// PlanManager returns the plan manager, if configured.
func (rl *RalphLoop) PlanManager() *plan.PlanManager {
	return rl.planManager
}

// CheckCompletion verifies if a completed task actually achieved its goal.
// Returns (isComplete bool, evidence []string, needsReplan bool).
func (rl *RalphLoop) CheckCompletion(ctx context.Context, taskID string, result json.RawMessage) (bool, []string, bool) {
	if !rl.config.Enabled {
		return true, nil, false
	}

	// Get task details
	t, err := rl.taskStore.GetByID(taskID)
	if err != nil {
		rl.logger.Warn("Failed to get task for completion check", "task_id", taskID, "error", err)
		return true, nil, false
	}
	_ = ctx // context not needed for store lookup

	// Track iteration count
	rl.mu.Lock()
	iteration := rl.iterations[taskID]
	rl.mu.Unlock()

	// Parse result to extract completion evidence BEFORE the cap check
	// (F5, e2e run 3/8): the cap branch used to return first, so the last
	// granted attempt's evidence was never evaluated and a task that
	// finally succeeded on attempt MaxIterations+1 was terminalized
	// StateFailed with the reason "without sufficient evidence" — a claim
	// the code had not checked.
	var resultData struct {
		Success  bool     `json:"success,omitempty"`
		Result   string   `json:"result,omitempty"`
		Evidence []string `json:"evidence,omitempty"`
	}
	if err := json.Unmarshal(result, &resultData); err != nil {
		rl.logger.Warn("Failed to parse task result", "task_id", taskID, "error", err)
		if iteration >= rl.config.MaxIterations {
			// Cap reached and the final attempt's result cannot be
			// parsed: nothing verifies the work, so the task fails.
			rl.logger.Warn("Max Ralph loop iterations reached with an unparseable final result, failing task",
				"task_id", taskID, "iterations", iteration)
			rl.failTaskAtCap(taskID, "max ralph loop iterations reached without sufficient evidence")
			return false, nil, false
		}
		return false, nil, true // Needs replan due to parse failure
	}

	// Evaluate this attempt's evidence and checklists exactly once, so the
	// cap decision below judges the attempt it is about to kill.
	evidenceSufficient := true
	if rl.config.EvidenceRequired && len(resultData.Evidence) == 0 {
		rl.logger.Info("Task completed without evidence",
			"task_id", taskID, "description", t.Description)
		evidenceSufficient = false
	}
	if evidenceSufficient && !rl.validateEvidence(t.Description, resultData.Evidence) {
		rl.logger.Info("Evidence insufficient",
			"task_id", taskID, "evidence_count", len(resultData.Evidence))
		evidenceSufficient = false
	}
	if evidenceSufficient && rl.config.ChecklistRequired {
		allComplete, total, completed, incomplete := rl.validateChecklists(taskID)
		if !allComplete && total > 0 {
			rl.logger.Info("Checklist incomplete",
				"task_id", taskID, "completed", completed, "total", total,
				"incomplete_count", len(incomplete))
			evidenceSufficient = false
		}
	}

	if iteration >= rl.config.MaxIterations {
		if evidenceSufficient && hasIndependentRalphEvidence(resultData.Evidence) {
			// The final granted attempt DID produce verifiable evidence:
			// report completion instead of discarding the result (F5).
			rl.logger.Info("Max Ralph loop iterations reached but the final attempt produced sufficient evidence; completing",
				"task_id", taskID, "iterations", iteration)
			return true, resultData.Evidence, false
		}
		// Cap reached (e2e run 3/8, 2026-09-11): the previous contract
		// returned (true, nil, false) — "complete" — so the orchestrator
		// reset the counter via TaskOutcome and the NEXT evidence failure
		// replanned from iteration 1 again (MiKsNh daemon.log: 3→1→2→1).
		// At the cap the task must terminalize as FAILED instead; the
		// orchestrator's TaskOutcome-gated Reset then never fires and the
		// counter stays armed.
		rl.logger.Warn("Max Ralph loop iterations reached, failing task",
			"task_id", taskID, "iterations", iteration)
		rl.failTaskAtCap(taskID, "max ralph loop iterations reached without sufficient evidence")
		return false, nil, false
	}

	if !evidenceSufficient {
		return false, resultData.Evidence, true
	}

	return true, resultData.Evidence, false
}

// validateEvidence checks if evidence supports the task completion claim.
func (rl *RalphLoop) validateEvidence(taskDescription string, evidence []string) bool {
	// Simple heuristic: at least one evidence item should mention key terms from the task
	// In production, this would use LLM-based validation
	if len(evidence) == 0 {
		return false
	}

	// Extract key terms from task description (simple word extraction)
	keyTerms := extractKeyTerms(taskDescription)
	if len(keyTerms) == 0 {
		return len(evidence) > 0 // Accept any evidence if no key terms extracted
	}

	// Check if any evidence mentions at least one key term
	for _, ev := range evidence {
		matches := 0
		for _, term := range keyTerms {
			if strings.Contains(strings.ToLower(ev), strings.ToLower(term)) {
				matches++
			}
		}
		if matches > 0 {
			return true
		}
	}

	return false
}

// hasIndependentRalphEvidence reports whether the evidence list carries at
// least one entry that is NOT the daemon's own job-completion stamp. That
// stamp — internal/daemon/components.go emits
// "job <id> completed by agent <x>: <narration>" for every step job — embeds
// the MODEL'S OWN narration, so validateEvidence can be satisfied by the
// claim being checked rather than by independently observed proof. At the
// replan cap that is the difference between the cap staying armed and the
// model talking itself back to complete: the cap's whole point is that
// repeated attempts have NOT produced verifiable work, so a completion
// granted on the synthetic stamp alone resets the counter (via the
// orchestrator's TaskOutcome) on the model's say-so. Non-cap passes are
// unaffected — this guard applies only to the cap decision.
func hasIndependentRalphEvidence(evidence []string) bool {
	for _, ev := range evidence {
		trimmed := strings.TrimSpace(ev)
		if !strings.HasPrefix(trimmed, "job ") {
			return true
		}
		rest := trimmed[len("job "):]
		// The stamp's marker must sit after a non-empty job id.
		if idx := strings.Index(rest, " completed by agent "); idx > 0 {
			continue // synthetic stamp: the model's narration, not proof
		}
		return true
	}
	return false
}

// validateChecklists checks if all step checklists are complete for a task.
// Returns (allComplete bool, totalItems int, completedItems int, incompleteSteps []string).
func (rl *RalphLoop) validateChecklists(taskID string) (bool, int, int, []string) {
	if rl.stepStore == nil {
		// If no step store, consider checklists as satisfied
		return true, 0, 0, nil
	}

	steps, err := rl.stepStore.ListByTaskID(taskID)
	if err != nil {
		rl.logger.Warn("Failed to list steps for checklist validation", "task_id", taskID, "error", err)
		return true, 0, 0, nil // Don't block on error
	}

	if len(steps) == 0 {
		return true, 0, 0, nil // No steps, checklists satisfied
	}

	totalItems := 0
	completedItems := 0
	var incompleteSteps []string

	for _, step := range steps {
		if step.Checklist == nil || len(step.Checklist.Items) == 0 {
			// No checklist for this step, skip
			continue
		}

		for _, item := range step.Checklist.Items {
			totalItems++
			if item.Completed {
				completedItems++
			} else {
				incompleteSteps = append(incompleteSteps, fmt.Sprintf("%s:%s", step.ID, item.Text))
			}
		}
	}

	allComplete := totalItems == 0 || completedItems == totalItems
	return allComplete, totalItems, completedItems, incompleteSteps
}

// TriggerReplan creates a new planning step for incomplete tasks.
// E2E run 3 (2026-09-10): an eager Reset(taskID) on task-completed events
// kept zeroing the iteration counter mid-flight (log shows iteration=1
// three separate times for one task), so the MaxIterations cap never held
// and the task replan-looped until the CLI's 120s socket read died. The cap
// is now enforced HERE — at the single point that increments the counter —
// so once iterations reaches MaxIterations the task is marked failed and
// no further replan request is published.
func (rl *RalphLoop) TriggerReplan(ctx context.Context, taskID string, previousEvidence []string) error {
	rl.mu.Lock()
	iteration := rl.iterations[taskID] + 1
	if iteration > rl.config.MaxIterations {
		rl.mu.Unlock()
		rl.logger.Warn("Replan cap reached, failing task instead of re-enqueueing",
			"task_id", taskID,
			"iterations", rl.iterations[taskID],
			"max_iterations", rl.config.MaxIterations)
		rl.failTaskAtCap(taskID, "replan iteration cap reached")
		return nil
	}
	rl.iterations[taskID] = iteration
	rl.mu.Unlock()

	_, err := rl.taskStore.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Create replan context with previous attempt info
	replanContext := fmt.Sprintf("Previous attempt (iteration %d/%d) completed without sufficient evidence.\n",
		iteration-1, rl.config.MaxIterations)
	if len(previousEvidence) > 0 {
		replanContext += "Evidence from previous attempt:\n"
		for i, ev := range previousEvidence {
			replanContext += fmt.Sprintf("  %d. %s\n", i+1, ev)
		}
	}
	replanContext += "\nPlease revise the approach to ensure verifiable completion."

	// Publish replan request to bus. Nil-guarded (e2e-fix loop 2026-09-11):
	// TriggerReplan is invoked from orchestrator event handlers, and a
	// RalphLoop constructed without a bus (unit harnesses, degraded wiring)
	// must not SIGSEGV the handler goroutine.
	replanMsg := &models.BusMessage{
		Source:  "ralph_loop",
		Topic:   "orchestrator.replan",
		Payload: json.RawMessage(fmt.Sprintf(`{"task_id": "%s", "iteration": %d, "context": %q}`, taskID, iteration, replanContext)),
	}

	if rl.bus == nil {
		rl.logger.Warn("Replan request not published: no bus wired", "task_id", taskID)
	} else if n := rl.bus.Publish("orchestrator.replan", replanMsg); n == 0 {
		rl.logger.Warn("Replan request published but no subscribers", "task_id", taskID)
	}

	rl.logger.Info("Triggered replan",
		"task_id", taskID,
		"iteration", iteration,
		"max_iterations", rl.config.MaxIterations)

	return nil
}

// TaskIsTerminal reports whether the named task is in a terminal state.
// Used by the orchestrator so ralph-loop replanning cannot run after
// OnJobCompleted has already finished the task.
func (rl *RalphLoop) TaskIsTerminal(taskID string) bool {
	if rl == nil || rl.taskStore == nil || taskID == "" {
		return false
	}
	t, err := rl.taskStore.GetByID(taskID)
	return err == nil && t != nil && t.State.IsTerminal()
}

// TaskOutcome reports whether a terminal task completed successfully
// (vs failed/cancelled/rejected). Used by the orchestrator's
// job-completed handler to distinguish "goal achieved — reset the replan
// counter" from "capped-out — keep the counter so the cap stays armed"
// (e2e run 3, 2026-09-10: the eager Reset let the counter restart from
// zero mid-task and the replan loop never terminated). A non-terminal or
// unknown task reports (false, false).
func (rl *RalphLoop) TaskOutcome(taskID string) (completed bool, terminal bool) {
	if rl == nil || rl.taskStore == nil || taskID == "" {
		return false, false
	}
	t, err := rl.taskStore.GetByID(taskID)
	if err != nil || t == nil || !t.State.IsTerminal() {
		return false, false
	}
	return t.State == task.StateCompleted, true
}

// GetIterationCount returns the current iteration count for a task.
func (rl *RalphLoop) GetIterationCount(taskID string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.iterations[taskID]
}

// Reset clears iteration tracking for a task.
func (rl *RalphLoop) Reset(taskID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.iterations, taskID)
}

// failTaskAtCap marks a task failed after TriggerReplan hit MaxIterations
// without sufficient evidence. Every failure path is best-effort: a store
// error or absent bus must not panic the orchestrator's event handler —
// the Warn above is the durable record. Without the terminal state the
// sync chat reply (waitForTaskCompletion) would hold its full 10-minute
// wait against the CLI's ~120s socket read (e2e run 3 T2/T3/T4).
func (rl *RalphLoop) failTaskAtCap(taskID, reason string) {
	t, err := rl.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return
	}
	// F6: never re-terminalize. Another path — tactical's finalize block,
	// cancellation, startup recovery — may already have put the task in a
	// terminal state. Overwriting StateCompleted with StateFailed (or the
	// reverse) would emit a second task event and hand the orchestrator's
	// TaskOutcome a terminal task it then Resets, disarming the cap.
	if t.State.IsTerminal() {
		return
	}
	t.State = task.StateFailed
	if err := rl.taskStore.Update(t); err != nil {
		rl.logger.Warn("Failed to mark task failed at replan cap",
			"task_id", taskID, "error", err)
		return
	}
	if rl.bus == nil {
		return
	}
	// The payload key set is the subscriber's (handler.ChatHandler
	// handleTaskFailed decodes name/error/failed_jobs/completed_jobs/
	// total_jobs/linked_sessions). The early keys (task_id/reason/source)
	// are kept for existing consumers, but on their own they rendered the
	// user-facing failure as "## task failed: \n**error:**" — empty name,
	// empty error (F39).
	failedJobs := 0
	if rl.stepStore != nil {
		if steps, serr := rl.stepStore.ListByTaskID(taskID); serr == nil {
			for _, s := range steps {
				if s.State == task.StepFailed {
					failedJobs++
				}
			}
		}
	}
	payload, err := json.Marshal(map[string]any{
		"task_id":         taskID,
		"name":            t.Name,
		"error":           reason,
		"reason":          reason,
		"source":          "ralph_loop",
		"failed_jobs":     failedJobs,
		"completed_jobs":  t.CompletedJobs,
		"total_jobs":      t.TotalJobs,
		"linked_sessions": t.LinkedSessions,
	})
	if err != nil {
		return
	}
	msg := &models.BusMessage{
		Source:  "ralph_loop",
		Topic:   "task.failed",
		Payload: payload,
	}
	rl.bus.Publish("task.failed", msg)
}

// Cleanup removes iteration entries that haven't been touched within maxAge
// (S1-18). This prevents unbounded growth of the iterations map from
// abandoned or long-completed tasks. Callers should invoke this periodically
// (e.g. from a scheduler job); it is not auto-scheduled.
func (rl *RalphLoop) Cleanup(maxAge time.Duration, lastTouched func(string) time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for taskID := range rl.iterations {
		ts := lastTouched(taskID)
		if ts.IsZero() {
			continue // unknown — skip
		}
		if now.Sub(ts) > maxAge {
			delete(rl.iterations, taskID)
		}
	}
}

// extractKeyTerms extracts important terms from a task description.
func extractKeyTerms(desc string) []string {
	// Remove common stop words and extract meaningful terms
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"could": true, "should": true, "may": true, "might": true, "must": true,
	}

	words := strings.Fields(strings.ToLower(desc))
	var terms []string
	seen := make(map[string]bool)

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'()[]{}")
		if len(word) > 3 && !stopWords[word] && !seen[word] {
			terms = append(terms, word)
			seen[word] = true
		}
	}

	return terms
}
