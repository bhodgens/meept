package services

import (
	"context"

	"github.com/caimlas/meept/internal/task"
)

// TaskService handles task operations.
type TaskService struct {
	registry *task.Registry
}

// NewTaskService creates a task service.
func NewTaskService(reg *task.Registry) *TaskService {
	return &TaskService{registry: reg}
}

// CreateTaskRequest contains task creation parameters.
type CreateTaskRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Create creates a new task.
func (s *TaskService) Create(ctx context.Context, req CreateTaskRequest) (*task.Task, error) {
	if req.Name == "" {
		return nil, wrapError("task", "Create", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("task", "Create", ErrUnavailable)
	}
	t, err := s.registry.Create(ctx, req.Name, req.Description)
	if err != nil {
		return nil, wrapError("task", "Create", err)
	}
	return t, nil
}

// GetTaskRequest contains get parameters.
type GetTaskRequest struct {
	ID string `json:"id"`
}

// Get retrieves a task by ID.
func (s *TaskService) Get(ctx context.Context, req GetTaskRequest) (*task.Task, error) {
	if req.ID == "" {
		return nil, wrapError("task", "Get", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("task", "Get", ErrUnavailable)
	}
	t, err := s.registry.Get(ctx, req.ID)
	if err != nil {
		return nil, wrapError("task", "Get", err)
	}
	if t == nil {
		return nil, wrapError("task", "Get", ErrNotFound)
	}
	return t, nil
}

// TaskListRequest contains list parameters.
type TaskListRequest struct {
	Limit int `json:"limit,omitempty"`
}

// List returns tasks.
func (s *TaskService) List(ctx context.Context, req TaskListRequest) ([]*task.Task, error) {
	if s.registry == nil {
		return nil, wrapError("task", "List", ErrUnavailable)
	}
	// Use default limit
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	tasks, err := s.registry.List(ctx, nil, limit)
	if err != nil {
		return nil, wrapError("task", "List", err)
	}
	return tasks, nil
}

// UpdateTaskRequest contains update parameters.
type UpdateTaskRequest struct {
	ID    string `json:"id"`
	State string `json:"state,omitempty"`
}

// Update updates a task.
func (s *TaskService) Update(ctx context.Context, req UpdateTaskRequest) (*task.Task, error) {
	if req.ID == "" {
		return nil, wrapError("task", "Update", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("task", "Update", ErrUnavailable)
	}
	t, err := s.registry.Get(ctx, req.ID)
	if err != nil {
		return nil, wrapError("task", "Update", err)
	}
	if t == nil {
		return nil, wrapError("task", "Update", ErrNotFound)
	}
	// Update state if provided
	if req.State != "" {
		if err := s.registry.UpdateState(ctx, req.ID, task.TaskState(req.State)); err != nil {
			return nil, wrapError("task", "Update", err)
		}
		// Reload task to get updated state
		t, err = s.registry.Get(ctx, req.ID)
		if err != nil {
			return nil, wrapError("task", "Update", err)
		}
	}
	return t, nil
}

// DeleteTaskRequest contains delete parameters.
type DeleteTaskRequest struct {
	ID string `json:"id"`
}

// Delete removes a task.
func (s *TaskService) Delete(ctx context.Context, req DeleteTaskRequest) error {
	if req.ID == "" {
		return wrapError("task", "Delete", ErrInvalidInput)
	}
	if s.registry == nil {
		return wrapError("task", "Delete", ErrUnavailable)
	}
	return s.registry.Delete(ctx, req.ID)
}

// CancelTaskRequest contains cancel parameters.
type CancelTaskRequest struct {
	ID string `json:"id"`
}

// Cancel marks a task as cancelled.
func (s *TaskService) Cancel(ctx context.Context, req CancelTaskRequest) error {
	if req.ID == "" {
		return wrapError("task", "Cancel", ErrInvalidInput)
	}
	if s.registry == nil {
		return wrapError("task", "Cancel", ErrUnavailable)
	}
	// Use UpdateState to set task to cancelled state
	return s.registry.UpdateState(ctx, req.ID, task.StateCancelled)
}

// GetTaskStepsRequest contains get steps parameters.
type GetTaskStepsRequest struct {
	ID string `json:"id"`
}

// GetSteps retrieves steps for a task.
func (s *TaskService) GetSteps(ctx context.Context, req GetTaskStepsRequest) ([]*task.TaskStep, error) {
	if req.ID == "" {
		return nil, wrapError("task", "GetSteps", ErrInvalidInput)
	}
	if s.registry == nil {
		return nil, wrapError("task", "GetSteps", ErrUnavailable)
	}
	// Verify task exists first
	t, err := s.registry.Get(ctx, req.ID)
	if err != nil {
		return nil, wrapError("task", "GetSteps", err)
	}
	if t == nil {
		return nil, wrapError("task", "GetSteps", ErrNotFound)
	}
	// Get steps from the step store
	return s.registry.StepStore().ListByTaskID(req.ID)
}
