package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/queue"
)

// AmendmentHandlers provides built-in handlers for amendment types.
type AmendmentHandlers struct {
	registry  *Registry
	queue     queue.Queue
	stepStore *StepStore
}

// NewAmendmentHandlers creates amendment handlers.
func NewAmendmentHandlers(registry *Registry, q queue.Queue) *AmendmentHandlers {
	return &AmendmentHandlers{
		registry:  registry,
		queue:     q,
		stepStore: registry.StepStore(),
	}
}

// RegisterAll registers all built-in handlers.
func (h *AmendmentHandlers) RegisterAll(mgr *AmendmentManager) {
	mgr.RegisterHandler(AmendmentInjectContext, h.handleInjectContext)
	mgr.RegisterHandler(AmendmentSkipStep, h.handleSkipStep)
	mgr.RegisterHandler(AmendmentAddStep, h.handleAddStep)
	mgr.RegisterHandler(AmendmentReprioritize, h.handleReprioritize)
	mgr.RegisterHandler(AmendmentChangeAgent, h.handleChangeAgent)
}

// handleInjectContext injects context into active agent loops.
func (h *AmendmentHandlers) handleInjectContext(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
	// Get task
	task, err := h.registry.Get(ctx, req.TaskID)
	if err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("task not found: %v", err),
		}, nil
	}

	// Inject context by adding to task's context query or memory refs
	if task.ContextQuery != "" {
		task.ContextQuery += "\n" + req.Content
	} else {
		task.ContextQuery = req.Content
	}

	if err := h.registry.Update(ctx, task); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("failed to update task: %v", err),
		}, nil
	}

	return &AmendmentReply{
		RequestID: req.ID,
		Success:   true,
		Message:   "Context injected successfully",
	}, nil
}

// handleSkipStep marks a step as skipped.
func (h *AmendmentHandlers) handleSkipStep(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
	if req.StepID == "" {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   "step_id required for skip_step",
		}, nil
	}

	// Verify step exists
	if _, err := h.stepStore.GetByID(req.StepID); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("step not found: %v", err),
		}, nil
	}

	if err := h.stepStore.SetState(req.StepID, StepSkipped); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("failed to skip step: %v", err),
		}, nil
	}

	// Promote newly unblocked steps
	h.stepStore.PromoteReadySteps(req.TaskID)

	return &AmendmentReply{
		RequestID: req.ID,
		Success:   true,
		Message:   fmt.Sprintf("Step %s skipped", req.StepID),
	}, nil
}

// handleAddStep adds a new step to a task.
func (h *AmendmentHandlers) handleAddStep(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
	var metadata struct {
		Description string   `json:"description"`
		ToolHint    string   `json:"tool_hint,omitempty"`
		DependsOn   []string `json:"depends_on,omitempty"`
	}
	if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("invalid metadata: %v", err),
		}, nil
	}

	// Get existing steps to determine sequence
	steps, err := h.stepStore.ListByTaskID(req.TaskID)
	if err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("failed to list steps: %v", err),
		}, nil
	}
	sequence := len(steps) + 1

	step := NewTaskStep(req.TaskID, metadata.Description, sequence)
	step.ToolHint = metadata.ToolHint
	step.DependsOn = metadata.DependsOn

	if err := h.stepStore.Create(step); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("failed to create step: %v", err),
		}, nil
	}

	// Update task total jobs
	task, _ := h.registry.Get(ctx, req.TaskID)
	if task != nil {
		task.IncrementJobs()
		h.registry.Update(ctx, task)
	}

	return &AmendmentReply{
		RequestID: req.ID,
		Success:   true,
		Message:   fmt.Sprintf("Step %s added", step.ID),
		Metadata:  json.RawMessage(fmt.Sprintf(`{"step_id":%q}`, step.ID)),
	}, nil
}

// handleReprioritize changes step sequence/priority.
func (h *AmendmentHandlers) handleReprioritize(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
	var metadata struct {
		StepIDs []string `json:"step_ids"` // New order
	}
	if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("invalid metadata: %v", err),
		}, nil
	}

	// Re-sequence steps
	for i, stepID := range metadata.StepIDs {
		step, err := h.stepStore.GetByID(stepID)
		if err != nil {
			continue
		}
		step.Sequence = i
		if err := h.stepStore.Update(step); err != nil {
			h.stepStore.logger.Warn("Failed to update step sequence", "step_id", stepID, "error", err)
		}
	}

	return &AmendmentReply{
		RequestID: req.ID,
		Success:   true,
		Message:   "Steps reprioritized",
	}, nil
}

// handleChangeAgent reassigns a step to a different agent.
func (h *AmendmentHandlers) handleChangeAgent(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
	var metadata struct {
		StepID  string `json:"step_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("invalid metadata: %v", err),
		}, nil
	}

	step, err := h.stepStore.GetByID(metadata.StepID)
	if err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("step not found: %v", err),
		}, nil
	}

	step.AgentID = metadata.AgentID
	if err := h.stepStore.Update(step); err != nil {
		return &AmendmentReply{
			RequestID: req.ID,
			Success:   false,
			Message:   fmt.Sprintf("failed to update step: %v", err),
		}, nil
	}

	return &AmendmentReply{
		RequestID: req.ID,
		Success:   true,
		Message:   fmt.Sprintf("Step %s reassigned to %s", metadata.StepID, metadata.AgentID),
	}, nil
}
