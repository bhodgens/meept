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

	if iteration >= rl.config.MaxIterations {
		rl.logger.Warn("Max Ralph loop iterations reached, marking complete",
			"task_id", taskID, "iterations", iteration)
		return true, nil, false
	}

	// Parse result to extract completion evidence
	var resultData struct {
		Success  bool     `json:"success,omitempty"`
		Result   string   `json:"result,omitempty"`
		Evidence []string `json:"evidence,omitempty"`
	}
	if err := json.Unmarshal(result, &resultData); err != nil {
		rl.logger.Warn("Failed to parse task result", "task_id", taskID, "error", err)
		return false, nil, true // Needs replan due to parse failure
	}

	// Check for evidence if required
	if rl.config.EvidenceRequired && len(resultData.Evidence) == 0 {
		rl.logger.Info("Task completed without evidence, triggering replan",
			"task_id", taskID, "description", t.Description)
		return false, nil, true
	}

	// Verify evidence substantiates the task goal
	if !rl.validateEvidence(t.Description, resultData.Evidence) {
		rl.logger.Info("Evidence insufficient, triggering replan",
			"task_id", taskID, "evidence_count", len(resultData.Evidence))
		return false, resultData.Evidence, true
	}

	// Validate checklists if required
	if rl.config.ChecklistRequired {
		allComplete, total, completed, incomplete := rl.validateChecklists(taskID)
		if !allComplete && total > 0 {
			rl.logger.Info("Checklist incomplete, triggering replan",
				"task_id", taskID, "completed", completed, "total", total,
				"incomplete_count", len(incomplete))
			return false, resultData.Evidence, true
		}
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
func (rl *RalphLoop) TriggerReplan(ctx context.Context, taskID string, previousEvidence []string) error {
	rl.mu.Lock()
	rl.iterations[taskID]++
	iteration := rl.iterations[taskID]
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

	// Publish replan request to bus
	replanMsg := &models.BusMessage{
		Source:  "ralph_loop",
		Topic:   "orchestrator.replan",
		Payload: json.RawMessage(fmt.Sprintf(`{"task_id": "%s", "iteration": %d, "context": %q}`, taskID, iteration, replanContext)),
	}

	if n := rl.bus.Publish("orchestrator.replan", replanMsg); n == 0 {
		rl.logger.Warn("Replan request published but no subscribers", "task_id", taskID)
	}

	rl.logger.Info("Triggered replan",
		"task_id", taskID,
		"iteration", iteration,
		"max_iterations", rl.config.MaxIterations)

	return nil
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
