package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// EscalationConfig holds configuration for the escalation manager.
type EscalationConfig struct {
	// Enabled turns on escalation handling
	Enabled bool
	// MaxEscalationLevels is the maximum re-planning levels before human notification (default: 3)
	MaxEscalationLevels int
	// NotificationTopic is the bus topic for human intervention notifications
	NotificationTopic string
}

// DefaultEscalationConfig returns sensible defaults.
func DefaultEscalationConfig() EscalationConfig {
	return EscalationConfig{
		Enabled:             true,
		MaxEscalationLevels: 3,
		NotificationTopic:   "escalation.human_intervention",
	}
}

// FailureContext captures failure details for re-planning.
type FailureContext struct {
	TaskID       string    `json:"task_id"`
	StepID       string    `json:"step_id"`
	AgentID      string    `json:"agent_id"`
	Error        string    `json:"error"`
	Stage        string    `json:"stage"` // which stage failed
	RetryCount   int       `json:"retry_count"`
	PartialState string    `json:"partial_state,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// EscalationLevel tracks escalation history for a task.
type EscalationLevel struct {
	TaskID       string    `json:"task_id"`
	Level        int       `json:"level"`
	Reason       string    `json:"reason"`
	OriginalTask string    `json:"original_task"`
	ReplanResult string    `json:"replan_result,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// EscalationManager handles task escalation and re-planning when tasks fail.
type EscalationManager struct {
	mu        sync.RWMutex
	config    EscalationConfig
	planner   *StrategicPlanner
	taskStore *task.Store
	bus       *bus.MessageBus
	logger    *slog.Logger

	// Track escalation levels per task
	escalations map[string]*EscalationLevel // taskID -> current level
}

// EscalationManagerConfig holds configuration for creating an EscalationManager.
type EscalationManagerConfig struct {
	Config    EscalationConfig
	Planner   *StrategicPlanner
	TaskStore *task.Store
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewEscalationManager creates a new escalation manager.
func NewEscalationManager(cfg EscalationManagerConfig) *EscalationManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Config.NotificationTopic == "" {
		cfg.Config.NotificationTopic = "escalation.human_intervention"
	}

	return &EscalationManager{
		config:      cfg.Config,
		planner:     cfg.Planner,
		taskStore:   cfg.TaskStore,
		bus:         cfg.Bus,
		logger:      cfg.Logger.With("component", "escalation-manager"),
		escalations: make(map[string]*EscalationLevel),
	}
}

// Escalate triggers re-planning for a failed task.
// It checks the current escalation level and either re-plans into smaller tasks
// or notifies for human intervention if the max level has been reached.
func (em *EscalationManager) Escalate(ctx context.Context, failure FailureContext) error {
	if !em.config.Enabled {
		em.logger.Debug("Escalation disabled, ignoring failure",
			"task_id", failure.TaskID,
			"error", failure.Error,
		)
		return nil
	}

	em.mu.Lock()
	level, exists := em.escalations[failure.TaskID]
	if !exists {
		// Fetch the original task description from the store
		originalTaskDesc := ""
		if em.taskStore != nil {
			if t, err := em.taskStore.GetByID(failure.TaskID); err == nil && t != nil {
				originalTaskDesc = t.Description
			}
		}
		level = &EscalationLevel{
			TaskID:       failure.TaskID,
			Level:        0,
			OriginalTask: originalTaskDesc,
			Timestamp:    time.Now(),
		}
		em.escalations[failure.TaskID] = level
	}

	level.Level++
	level.Reason = failure.Error
	level.Timestamp = time.Now()
	currentLevel := level.Level
	em.mu.Unlock()

	em.logger.Info("Escalating task",
		"task_id", failure.TaskID,
		"step_id", failure.StepID,
		"agent_id", failure.AgentID,
		"level", currentLevel,
		"max_levels", em.config.MaxEscalationLevels,
		"error", failure.Error,
	)

	// Check if max escalation reached
	if currentLevel >= em.config.MaxEscalationLevels {
		em.logger.Warn("Max escalation level reached, requesting human intervention",
			"task_id", failure.TaskID,
			"level", currentLevel,
		)
		return em.notifyHumanIntervention(ctx, failure, currentLevel)
	}

	// Attempt re-planning
	return em.triggerReplan(ctx, failure, currentLevel)
}

// EscalateForValidation triggers escalation specifically for validation failures.
// Validation failures get a dedicated escalation path with more context.
func (em *EscalationManager) EscalateForValidation(ctx context.Context, failure FailureContext, validationLoops int) error {
	if !em.config.Enabled {
		return nil
	}

	em.logger.Info("Escalating validation failure",
		"task_id", failure.TaskID,
		"step_id", failure.StepID,
		"validation_loops", validationLoops,
		"error", failure.Error,
	)

	// For validation failures, we always try re-planning first
	// since the work was partially done but didn't meet validation criteria
	return em.Escalate(ctx, failure)
}

// GetEscalationLevel returns the current escalation level for a task.
func (em *EscalationManager) GetEscalationLevel(taskID string) int {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if level, ok := em.escalations[taskID]; ok {
		return level.Level
	}
	return 0
}

// ClearEscalation removes escalation tracking for a completed task.
func (em *EscalationManager) ClearEscalation(taskID string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.escalations, taskID)
}

// Cleanup removes escalation entries whose Timestamp is older than maxAge
// (S1-12). This prevents unbounded growth of the escalations map for
// abandoned or long-completed tasks. Callers should invoke this periodically
// (e.g. from a scheduler job); it is not auto-scheduled.
func (em *EscalationManager) Cleanup(maxAge time.Duration) {
	em.mu.Lock()
	defer em.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, level := range em.escalations {
		if level.Timestamp.Before(cutoff) {
			delete(em.escalations, id)
		}
	}
}

// triggerReplan attempts to re-plan a failed task into smaller, more manageable steps.
func (em *EscalationManager) triggerReplan(ctx context.Context, failure FailureContext, level int) error {
	// Publish escalation event on the bus
	em.publishEscalationEvent(ctx, failure, level, "replan")

	// If we have a planner, attempt re-planning
	if em.planner == nil {
		em.logger.Warn("No planner available for re-planning, requesting human intervention",
			"task_id", failure.TaskID,
		)
		return em.notifyHumanIntervention(ctx, failure, level)
	}

	// Get original task description
	t, err := em.taskStore.GetByID(failure.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("failed to get task for re-planning: %w", err)
	}

	// Build a more constrained plan request
	replanDescription := fmt.Sprintf(
		"RE-PLAN (escalation level %d): Original task failed at step '%s' with error: %s. "+
			"Please break this into smaller, more focused steps that avoid the previous failure. "+
			"Original description: %s",
		level, failure.StepID, failure.Error, t.Description,
	)

	req := PlanRequest{
		TaskID:    failure.TaskID,
		SessionID: "",
		Input:     replanDescription,
		Intent:    string(IntentPlan),
	}

	if err := em.planner.Plan(ctx, req); err != nil {
		em.logger.Error("Re-planning failed",
			"task_id", failure.TaskID,
			"level", level,
			"error", err,
		)
		return fmt.Errorf("re-planning failed: %w", err)
	}

	em.logger.Info("Re-planning succeeded",
		"task_id", failure.TaskID,
		"level", level,
	)

	return nil
}

// notifyHumanIntervention publishes a notification requesting human intervention.
func (em *EscalationManager) notifyHumanIntervention(ctx context.Context, failure FailureContext, level int) error {
	em.logger.Warn("Notifying for human intervention",
		"task_id", failure.TaskID,
		"level", level,
	)

	// Publish escalation event on the bus
	em.publishEscalationEvent(ctx, failure, level, "human_intervention")

	return nil
}

// publishEscalationEvent publishes an escalation event to the message bus.
func (em *EscalationManager) publishEscalationEvent(_ context.Context, failure FailureContext, level int, action string) {
	if em.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "escalation-manager", map[string]any{
		KeyTaskID:                failure.TaskID,
		KeyStepID:                failure.StepID,
		KeyAgentID:               failure.AgentID,
		"level":                  level,
		"action":                 action,
		string(MessageTypeError): failure.Error,
		"timestamp":              time.Now().UTC(),
	})
	if err != nil {
		em.logger.Error("Failed to create escalation event", "error", err)
		return
	}

	topic := "escalation.event"
	em.bus.Publish(topic, msg)
	em.logger.Debug("Published escalation event",
		"topic", topic,
		"task_id", failure.TaskID,
		"level", level,
		"action", action,
	)
}
