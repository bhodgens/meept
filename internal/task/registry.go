package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Map key constants for task operations.
const (
	KeyTaskID = "task_id"
	KeyStatus = "status"
)

// Registry manages tasks and provides a unified API.
type Registry struct {
	store           *Store
	bus             *bus.MessageBus
	logger          *slog.Logger
	interruptMgr    *InterruptManager
	interruptMgrBus *bus.MessageBus
	amendmentMgr    *AmendmentManager
	amendmentMgrBus *bus.MessageBus

	mu     sync.RWMutex
	closed bool
}

// NewRegistry creates a new task registry.
func NewRegistry(dbPath string, msgBus *bus.MessageBus, logger *slog.Logger) (*Registry, error) {
	if logger == nil {
		logger = slog.Default()
	}

	store, err := NewStore(dbPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	reg := &Registry{
		store:           store,
		bus:             msgBus,
		logger:          logger,
		interruptMgr:    NewInterruptManager(logger.With("component", "interrupt-mgr")),
		interruptMgrBus: msgBus,
		amendmentMgr:    NewAmendmentManager(msgBus, logger.With("component", "amendment-mgr")),
		amendmentMgrBus: msgBus,
	}

	// Wire up amendment handlers
	amendmentHandlers := NewAmendmentHandlers(reg, nil)
	amendmentHandlers.RegisterAll(reg.amendmentMgr)

	logger.Info("Task registry initialized", "path", dbPath)
	return reg, nil
}

// Create creates a new task.
func (r *Registry) Create(ctx context.Context, name, description string) (*Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, fmt.Errorf("registry is closed")
	}

	task := NewTask(name, description)
	if err := r.store.Create(task); err != nil {
		return nil, err
	}

	r.publishEvent("task.create", map[string]any{
		KeyTaskID: task.ID,
		"name":    task.Name,
	})

	return task, nil
}

// Get retrieves a task by ID.
func (r *Registry) Get(ctx context.Context, taskID string) (*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.store.GetByID(taskID)
}

// Update updates a task.
func (r *Registry) Update(ctx context.Context, task *Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("registry is closed")
	}

	if err := r.store.Update(task); err != nil {
		return err
	}

	r.publishEvent("task.update", map[string]any{
		KeyTaskID: task.ID,
		"state":   task.State.String(),
	})

	return nil
}

// Delete removes a task.
func (r *Registry) Delete(ctx context.Context, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("registry is closed")
	}

	if err := r.store.Delete(taskID); err != nil {
		return err
	}

	r.publishEvent("task.delete", map[string]any{
		KeyTaskID: taskID,
	})

	return nil
}

// List returns all tasks, optionally filtered by state.
func (r *Registry) List(ctx context.Context, state *TaskState, limit int) ([]*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	return r.store.List(state, limit)
}

// ListActive returns all active tasks.
func (r *Registry) ListActive(ctx context.Context) ([]*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.store.ListActive()
}

// ListSummaries returns lightweight summaries of all tasks.
func (r *Registry) ListSummaries(ctx context.Context, limit int) ([]TaskSummary, error) {
	tasks, err := r.List(ctx, nil, limit)
	if err != nil {
		return nil, err
	}

	summaries := make([]TaskSummary, len(tasks))
	for i, t := range tasks {
		summaries[i] = t.Summary()
	}
	return summaries, nil
}

// UpdateState changes a task's state.
func (r *Registry) UpdateState(ctx context.Context, taskID string, state TaskState) error {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.SetState(state)
	return r.Update(ctx, task)
}

// LinkSession links a session to a task.
func (r *Registry) LinkSession(ctx context.Context, taskID, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("registry is closed")
	}

	// Verify task exists
	task, err := r.store.GetByID(taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	if err := r.store.LinkSession(taskID, sessionID); err != nil {
		return err
	}

	r.publishEvent("task.link", map[string]any{
		KeyTaskID:    taskID,
		"session_id": sessionID,
	})

	return nil
}

// UnlinkSession removes a session-task link.
func (r *Registry) UnlinkSession(ctx context.Context, taskID, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("registry is closed")
	}

	if err := r.store.UnlinkSession(taskID, sessionID); err != nil {
		return err
	}

	r.publishEvent("task.unlink", map[string]any{
		KeyTaskID:    taskID,
		"session_id": sessionID,
	})

	return nil
}

// GetLinkedSessions returns sessions linked to a task.
func (r *Registry) GetLinkedSessions(ctx context.Context, taskID string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.store.GetLinkedSessions(taskID)
}

// GetTasksForSession returns tasks linked to a session.
func (r *Registry) GetTasksForSession(ctx context.Context, sessionID string) ([]*Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.store.GetTasksForSession(sessionID)
}

// IncrementJobCount increments the total job count for a task.
func (r *Registry) IncrementJobCount(ctx context.Context, taskID string) error {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.IncrementJobs()
	return r.Update(ctx, task)
}

// CompleteJob marks a job as completed for a task.
func (r *Registry) CompleteJob(ctx context.Context, taskID string) error {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.CompleteJob()

	// Auto-complete task if all jobs done
	if task.CompletedJobs == task.TotalJobs && task.TotalJobs > 0 {
		task.SetState(StateCompleted)
	}

	return r.Update(ctx, task)
}

// FailJob marks a job as failed for a task.
func (r *Registry) FailJob(ctx context.Context, taskID string) error {
	task, err := r.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	task.FailJob()
	return r.Update(ctx, task)
}

// Close closes the registry.
// StepStore returns the underlying step store.
func (r *Registry) StepStore() *StepStore {
	return r.store.StepStore()
}

// Store returns the underlying task store.
func (r *Registry) Store() *Store {
	return r.store
}

// InterruptManager returns the interrupt manager.
func (r *Registry) InterruptManager() *InterruptManager {
	return r.interruptMgr
}

// AmendmentManager returns the amendment manager.
func (r *Registry) AmendmentManager() *AmendmentManager {
	return r.amendmentMgr
}

func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	return r.store.Close()
}

func (r *Registry) publishEvent(topic string, data map[string]any) {
	if r.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "task-registry", data)
	if err != nil {
		r.logger.Error("Failed to create bus message", "error", err)
		return
	}

	r.bus.Publish(topic, msg)
}

// Handler handles task-related requests on the message bus.
type Handler struct {
	handler  *bus.SubscriptionHandler
	registry *Registry
	bus      *bus.MessageBus
	logger   *slog.Logger
}

// NewHandler creates a new task handler.
func NewHandler(registry *Registry, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		handler:  bus.NewSubscriptionHandler(msgBus, logger.With("component", "task-handler")),
		registry: registry,
		bus:      msgBus,
		logger:   logger,
	}

	// Subscribe to all task topics
	topics := map[string]bus.MessageCallback{
		"task.create":        h.handleTaskCreate,
		"task.get":           h.handleTaskGet,
		"task.update":        h.handleTaskUpdate,
		"task.cancel":        h.handleTaskCancel,
		"task.delete":        h.handleTaskDelete,
		"task.list":          h.handleTaskList,
		"task.list_extended": h.handleTaskListExtended,
		"task.link":          h.handleTaskLink,
		"task.unlink":        h.handleTaskUnlink,
		"task.steps":         h.handleTaskSteps,
	}

	for topic, callback := range topics {
		h.handler.Subscribe(topic, callback)
	}

	return h
}

// Start begins listening for task requests.
func (h *Handler) Start(ctx context.Context) error {
	h.handler.Start(ctx)
	h.logger.Info("Task handler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	h.handler.Stop()
	return nil
}

func (h *Handler) handleTaskCreate(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskGet(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskUpdate(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskDelete(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskCancel(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskList(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskListExtended(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskLink(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskUnlink(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleTaskSteps(ctx context.Context, topic string, msg any) {
	h.handleMessage(ctx, topic, msg.(*models.BusMessage))
}

func (h *Handler) handleMessage(ctx context.Context, topic string, msg *models.BusMessage) {
	var response any
	var err error

	switch topic {
	case "task.create":
		response, err = h.handleCreate(ctx, msg)
	case "task.get":
		response, err = h.handleGet(ctx, msg)
	case "task.update":
		response, err = h.handleUpdate(ctx, msg)
	case "task.list":
		response, err = h.handleList(ctx, msg)
	case "task.list_extended":
		response, err = h.handleListExtended(ctx, msg)
	case "task.delete":
		response, err = h.handleDelete(ctx, msg)
	case "task.cancel":
		response, err = h.handleCancel(ctx, msg)
	case "task.link":
		response, err = h.handleLink(ctx, msg)
	case "task.unlink":
		response, err = h.handleUnlink(ctx, msg)
	case "task.steps":
		response, err = h.handleSteps(ctx, msg)
	default:
		err = fmt.Errorf("unknown topic: %s", topic)
	}

	h.sendResponse(msg.ID, "task.result", response, err)
}

func (h *Handler) handleCreate(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	return h.registry.Create(ctx, params.Name, params.Description)
}

func (h *Handler) handleUpdate(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		ID          string `json:"id"`
		Name        string `json:"name,omitempty"`
		Description string `json:"description,omitempty"`
		State       string `json:"state,omitempty"`
		ProjectDir  string `json:"project_dir,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	task, err := h.registry.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", params.ID)
	}

	if params.Name != "" {
		task.Name = params.Name
	}
	if params.Description != "" {
		task.Description = params.Description
	}
	if params.State != "" {
		task.State = TaskState(params.State)
	}
	if params.ProjectDir != "" {
		task.ProjectDir = params.ProjectDir
	}

	if err := h.registry.Update(ctx, task); err != nil {
		return nil, err
	}

	return task, nil
}

func (h *Handler) handleGet(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	return h.registry.Get(ctx, params.ID)
}

func (h *Handler) handleList(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		State string `json:"state,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	var state *TaskState
	if params.State != "" {
		s := TaskState(params.State)
		state = &s
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}

	tasks, err := h.registry.List(ctx, state, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks}, nil
}

// TaskExtendedResponse represents a task with all extended fields for TUI display.
//
//nolint:revive // stutter with package name is intentional for API clarity
type TaskExtendedResponse struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Description     string      `json:"description,omitempty"`
	ProjectDir      string      `json:"project_dir,omitempty"`
	WorkspaceDir    string      `json:"workspace_dir,omitempty"`
	State           string      `json:"state"`
	GitRepo         string      `json:"git_repo,omitempty"`
	MemvidZone      string      `json:"memvid_zone,omitempty"`
	TotalJobs       int         `json:"total_jobs"`
	CompletedJobs   int         `json:"completed_jobs"`
	FailedJobs      int         `json:"failed_jobs"`
	LinkedSessions  []string    `json:"linked_sessions,omitempty"`
	MemoryRefs      []string    `json:"memory_refs,omitempty"`
	ContextQuery    string      `json:"context_query,omitempty"`
	InheritedFrom   string      `json:"inherited_from,omitempty"`
	CreatedMemories []string    `json:"created_memories,omitempty"`
	AssignedAgent   string      `json:"assigned_agent,omitempty"`
	CreatedAt       string      `json:"created_at"`
	UpdatedAt       string      `json:"updated_at"`
	Steps           []*TaskStep `json:"steps,omitempty"`
}

func (h *Handler) handleListExtended(ctx context.Context, _ *models.BusMessage) (any, error) {
	// Get all tasks (no filter, reasonable limit)
	tasks, err := h.registry.List(ctx, nil, 100)
	if err != nil {
		return nil, err
	}

	// Get step store for enriching with step data
	stepStore := h.registry.StepStore()

	// Build extended response
	extended := make([]TaskExtendedResponse, len(tasks))
	for i, t := range tasks {
		ext := TaskExtendedResponse{
			ID:              t.ID,
			Name:            t.Name,
			Description:     t.Description,
			ProjectDir:      t.ProjectDir,
			WorkspaceDir:    t.WorkspaceDir,
			State:           string(t.State),
			GitRepo:         t.GitRepo,
			MemvidZone:      t.MemvidZone,
			TotalJobs:       t.TotalJobs,
			CompletedJobs:   t.CompletedJobs,
			FailedJobs:      t.FailedJobs,
			LinkedSessions:  t.LinkedSessions,
			MemoryRefs:      t.MemoryRefs,
			ContextQuery:    t.ContextQuery,
			InheritedFrom:   t.InheritedFrom,
			CreatedMemories: t.CreatedMemories,
			AssignedAgent:   t.AssignedAgent,
			CreatedAt:       t.CreatedAt.Format(time.RFC3339),
			UpdatedAt:       t.UpdatedAt.Format(time.RFC3339),
		}

		// Fetch steps if step store is available
		if stepStore != nil {
			steps, err := stepStore.ListByTaskID(t.ID)
			if err == nil {
				ext.Steps = steps
			}
		}

		extended[i] = ext
	}

	return map[string]any{"tasks": extended}, nil
}

func (h *Handler) handleDelete(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.registry.Delete(ctx, params.ID); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "deleted"}, nil
}

// handleCancel flips a task into StateCancelled and triggers the interrupt token.
func (h *Handler) handleCancel(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		ID      string `json:"id"`
		Reason  string `json:"reason,omitempty"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}
	if params.ID == "" {
		return nil, fmt.Errorf("task id is required")
	}

	task, err := h.registry.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", params.ID)
	}

	if task.State.IsTerminal() {
		return map[string]any{
			KeyStatus: "noop",
			"state":   string(task.State),
			"message": "task already in terminal state",
		}, nil
	}

	// Trigger interrupt token (this cancels the context for in-flight jobs)
	reason := InterruptReason(params.Reason)
	if reason == "" {
		reason = ReasonUserCancelled
	}
	msgText := params.Message
	if msgText == "" {
		msgText = "Cancelled by user"
	}

	if err := h.registry.interruptMgr.Trigger(params.ID, reason, msgText); err != nil {
		h.logger.Warn("Failed to trigger interrupt", KeyTaskID, params.ID, "error", err)
	}

	// Update task state
	task.SetState(StateCancelled)
	if err := h.registry.Update(ctx, task); err != nil {
		return nil, err
	}

	// Publish cancellation event
	h.registry.publishEvent("task.cancelled", map[string]any{
		KeyTaskID: params.ID,
		"reason":  reason,
		"message": msgText,
	})

	return map[string]any{
		KeyStatus: "cancelled",
		"state":   string(task.State),
		"reason":  reason,
	}, nil
}

func (h *Handler) handleLink(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		TaskID    string `json:"task_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.registry.LinkSession(ctx, params.TaskID, params.SessionID); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "linked"}, nil
}

func (h *Handler) handleUnlink(ctx context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		TaskID    string `json:"task_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.registry.UnlinkSession(ctx, params.TaskID, params.SessionID); err != nil {
		return nil, err
	}

	return map[string]string{KeyStatus: "unlinked"}, nil
}

func (h *Handler) handleSteps(_ context.Context, msg *models.BusMessage) (any, error) {
	var params struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	stepStore := h.registry.StepStore()
	if stepStore == nil {
		return map[string]any{"steps": []any{}}, nil
	}

	steps, err := stepStore.ListByTaskID(params.TaskID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"steps": steps}, nil
}

func (h *Handler) sendResponse(replyTo, topic string, response any, err error) {
	var payload []byte

	if err != nil {
		payload, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else {
		payload, _ = json.Marshal(response)
	}

	msg := &models.BusMessage{
		ID:        fmt.Sprintf("task-resp-%d", time.Now().UnixNano()),
		Type:      models.MessageTypeResponse,
		Topic:     topic,
		Source:    "task-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish(topic, msg)
}
