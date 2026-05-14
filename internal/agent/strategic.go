package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// PlanRequest is the input to the strategic planner.
type PlanRequest struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	Intent    string `json:"intent"`

	// Compound support (Phase 3)
	IsCompound   bool   `json:"is_compound,omitempty"`
	CompoundType string `json:"compound_type,omitempty"`
}

// plannerStep is the JSON structure expected from the planner LLM output.
type plannerStep struct {
	Description string   `json:"description"`
	ToolHint    string   `json:"tool_hint,omitempty"`
	DependsOn   []int    `json:"depends_on,omitempty"` // 0-indexed sequence references
}

// plannerOutput is the structured JSON output from the planner agent.
type plannerOutput struct {
	Steps []plannerStep `json:"steps"`
}

const plannerPromptTemplate = `You are a task planner. Decompose the following request into discrete, executable steps.
Each step should be a single unit of work that can be assigned to a specialist agent.

Available tool hints (use these to indicate what kind of agent should handle each step):
- "code" or "refactor" → coding specialist
- "debug" or "fix" → debugging specialist
- "analyze" or "research" → analysis specialist
- "git" or "commit" → git operations specialist
- "plan" → further planning/decomposition
- "chat" → general conversation

Output ONLY valid JSON in this exact format (no markdown, no explanation):
{
  "steps": [
    {"description": "step description", "tool_hint": "code", "depends_on": []},
    {"description": "step description", "tool_hint": "code", "depends_on": [0]},
    {"description": "step description", "tool_hint": "git", "depends_on": [0, 1]}
  ]
}

The "depends_on" field uses 0-based step indices. Steps with empty depends_on can run in parallel.
Keep the plan to %d steps maximum. Be specific and actionable.

Request to decompose:
%s`

// StrategicPlanner decomposes tasks into steps using an LLM planner agent.
type StrategicPlanner struct {
	registry  *AgentRegistry
	taskStore *task.Store
	stepStore *task.StepStore
	bus       *bus.MessageBus
	logger    *slog.Logger

	maxPlanSteps   int
	plannerTimeout time.Duration
}

// StrategicPlannerConfig holds configuration for the strategic planner.
type StrategicPlannerConfig struct {
	Registry       *AgentRegistry
	TaskStore      *task.Store
	StepStore      *task.StepStore
	Bus            *bus.MessageBus
	Logger         *slog.Logger
	MaxPlanSteps   int
	PlannerTimeout time.Duration
}

// NewStrategicPlanner creates a new strategic planner.
func NewStrategicPlanner(cfg StrategicPlannerConfig) *StrategicPlanner {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxPlanSteps <= 0 {
		cfg.MaxPlanSteps = 10
	}
	if cfg.PlannerTimeout <= 0 {
		cfg.PlannerTimeout = 120 * time.Second
	}

	return &StrategicPlanner{
		registry:       cfg.Registry,
		taskStore:      cfg.TaskStore,
		stepStore:      cfg.StepStore,
		bus:            cfg.Bus,
		logger:         cfg.Logger,
		maxPlanSteps:   cfg.MaxPlanSteps,
		plannerTimeout: cfg.PlannerTimeout,
	}
}

// Plan decomposes a task into executable steps.
func (sp *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) error {
	sp.logger.Info("Starting strategic planning",
		"task_id", req.TaskID,
		"session_id", req.SessionID,
		"intent", req.Intent,
	)

	// Set task state to planning
	t, err := sp.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", req.TaskID)
	}
	t.SetState(task.StatePlanning)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task state to planning", "error", err)
	}

	// Copy parent MemoryRefs for context inheritance
	parentMemoryRefs := t.MemoryRefs

	var steps []*task.TaskStep

	// Fast-path: simple requests don't need LLM decomposition
	if !sp.shouldDecompose(req) {
		sp.logger.Info("Fast-path: skipping decomposition for simple request",
			"task_id", req.TaskID,
			"intent", req.Intent,
		)
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	} else {
		// Use planner agent to generate plan
		var err error
		steps, err = sp.generatePlan(ctx, req)
		if err != nil {
			sp.logger.Warn("Plan generation failed, creating single-step fallback",
				"task_id", req.TaskID,
				"error", err,
			)
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
		}
	}

	// Inject parent MemoryRefs to first step (if any exist and steps were created)
	if len(steps) > 0 && len(parentMemoryRefs) > 0 {
		for _, ref := range parentMemoryRefs {
			steps[0].AddMemoryRef(ref)
		}
		sp.logger.Info("Copied parent MemoryRefs to first step",
			"task_id", req.TaskID,
			"refs", len(parentMemoryRefs),
		)
	}

	// Persist steps
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			sp.logger.Error("Failed to persist step", "step_id", step.ID, "error", err)
			return fmt.Errorf("failed to persist steps: %w", err)
		}
	}

	// Update task job count
	t.TotalJobs = len(steps)
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after planning", "error", err)
	}

	// Promote root steps (no dependencies) to ready
	promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		sp.logger.Error("Failed to promote ready steps", "error", err)
	} else {
		sp.logger.Info("Promoted root steps to ready",
			"task_id", req.TaskID,
			"promoted", len(promoted),
			"total_steps", len(steps),
		)
	}

	// Publish task.planned event for TUI
	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID:     req.TaskID,
		"session_id":  req.SessionID,
		"total_steps": len(steps),
		"ready_steps": len(promoted),
	})

	// Publish orchestrator.schedule to trigger tactical scheduling
	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: req.TaskID,
	})

	return nil
}

// generatePlan uses the planner agent to decompose the request.
func (sp *StrategicPlanner) generatePlan(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("planner agent not available: %w", err)
	}

	// Build prompt
	prompt := fmt.Sprintf(plannerPromptTemplate, sp.maxPlanSteps, req.Input)

	// Run with timeout
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("plan-%s-%d", req.TaskID, time.Now().UnixNano())
	output, err := plannerLoop.RunOnce(planCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("planner failed: %w", err)
	}

	// Parse JSON output
	return sp.parsePlanOutput(req.TaskID, output)
}

// parsePlanOutput extracts steps from the planner LLM output.
func (sp *StrategicPlanner) parsePlanOutput(taskID, output string) ([]*task.TaskStep, error) {
	// Try to find JSON in the output (LLM might wrap it in markdown)
	jsonStr := extractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in planner output")
	}

	var plan plannerOutput
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("planner returned empty plan")
	}

	// Cap to max steps
	if len(plan.Steps) > sp.maxPlanSteps {
		plan.Steps = plan.Steps[:sp.maxPlanSteps]
	}

	// Convert to TaskStep objects
	steps := make([]*task.TaskStep, len(plan.Steps))
	stepIDs := make([]string, len(plan.Steps))

	// First pass: create steps and collect IDs
	for i, ps := range plan.Steps {
		step := task.NewTaskStep(taskID, ps.Description, i)
		step.ToolHint = ps.ToolHint
		steps[i] = step
		stepIDs[i] = step.ID
	}

	// Second pass: resolve dependency indices to step IDs
	for i, ps := range plan.Steps {
		if len(ps.DependsOn) > 0 {
			deps := make([]string, 0, len(ps.DependsOn))
			for _, depIdx := range ps.DependsOn {
				if depIdx >= 0 && depIdx < len(stepIDs) && depIdx != i {
					deps = append(deps, stepIDs[depIdx])
				}
			}
			steps[i].DependsOn = deps
		}
	}

	sp.logger.Info("Parsed plan output",
		"task_id", taskID,
		"steps", len(steps),
	)

	return steps, nil
}

// createFallbackSteps creates a single step when planning fails.
func (sp *StrategicPlanner) createFallbackSteps(req PlanRequest, parentRefs []string) []*task.TaskStep {
	step := task.NewTaskStep(req.TaskID, req.Input, 0)
	step.ToolHint = req.Intent
	// Copy parent refs
	for _, ref := range parentRefs {
		step.AddMemoryRef(ref)
	}
	return []*task.TaskStep{step}
}

// shouldDecompose returns true if the request warrants LLM-based task decomposition.
// Simple intents and short requests are handled as single-step tasks to avoid
// over-decomposition and unnecessary LLM calls.
func (sp *StrategicPlanner) shouldDecompose(req PlanRequest) bool {
	// Simple intents that never need decomposition
	switch req.Intent {
	case string(IntentChat), string(IntentReport), string(IntentRecall), string(IntentPlatform), string(IntentSearch), string(IntentAnalyze):
		return false
	}

	// Short requests (<100 chars) without complex action verbs are likely simple
	if len(req.Input) < 100 {
		lower := strings.ToLower(req.Input)

		// Check for complexity indicators that warrant decomposition
		complexityIndicators := []string{
			"and then", "after that", "followed by", "multiple",
			"several", "all of", "each of", "for every",
			"step by step", "steps", "phases",
		}
		for _, indicator := range complexityIndicators {
			if strings.Contains(lower, indicator) {
				return true
			}
		}

		// Simple requests don't need decomposition
		return false
	}

	// Longer requests may benefit from decomposition
	return true
}

// ReplanFailedTask re-plans a failed task into smaller steps for retry.
// This is called by the EscalationManager when a task fails and needs to be
// broken down into more manageable pieces.
func (sp *StrategicPlanner) ReplanFailedTask(ctx context.Context, taskID, failureReason string) error {
	sp.logger.Info("Re-planning failed task",
		"task_id", taskID,
		"failure_reason", failureReason,
	)

	if sp.taskStore == nil {
		return fmt.Errorf("task store not available for re-planning")
	}

	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	// Get any completed steps to avoid re-doing them
	var completedSteps []*task.TaskStep
	if sp.stepStore != nil {
		var err error
		completedSteps, err = sp.stepStore.ListByTaskID(taskID)
		if err != nil {
			sp.logger.Error("Failed to list steps for re-plan", "error", err)
		}
	}

	// Build remaining work description
	var completedDescs []string
	for _, s := range completedSteps {
		if s.State.IsSuccessfullyTerminal() {
			completedDescs = append(completedDescs, s.Description)
		}
	}

	replanDesc := fmt.Sprintf(
		"RE-PLAN: Task '%s' failed with error: %s.\nCompleted steps (do not redo): %s\nRemaining work: %s",
		t.Description,
		failureReason,
		fmt.Sprintf("%v", completedDescs),
		t.Description,
	)

	req := PlanRequest{
		TaskID: taskID,
		Input:  replanDesc,
		Intent: string(IntentPlan),
	}

	return sp.Plan(ctx, req)
}

func (sp *StrategicPlanner) publishEvent(topic string, data map[string]any) {
	if sp.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "strategic-planner", data)
	if err != nil {
		sp.logger.Error("Failed to create bus message", "error", err)
		return
	}

	sp.bus.Publish(topic, msg)
}

// extractJSON finds and extracts a JSON object from text that might contain
// markdown code fences or other wrapping.
func extractJSON(s string) string {
	// Try direct parse first
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return s
	}

	// Try to extract from markdown code fence
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + 7
		end := strings.Index(s[start:], "```")
		if end > 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// Try to extract from generic code fence
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + 3
		// Skip language identifier on the same line
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		end := strings.Index(s[start:], "```")
		if end > 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// Try to find a JSON object by braces
	braceStart := strings.Index(s, "{")
	braceEnd := strings.LastIndex(s, "}")
	if braceStart >= 0 && braceEnd > braceStart {
		candidate := s[braceStart : braceEnd+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return ""
}
