// Package agent provides the agent loop and related components.
package agent

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

// ChatHandler bridges the message bus to the AgentLoop.
// It subscribes to chat.request and publishes responses to chat.response.
type ChatHandler struct {
	loop       *AgentLoop
	dispatcher *Dispatcher // Optional: if set, routes through multi-agent dispatch
	bus        *bus.MessageBus
	logger     *slog.Logger

	// Worker tracking
	workers   map[string]*Worker
	workersMu sync.RWMutex

	// Shutdown
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Worker represents an active agent processing a request.
type Worker struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	RequestID      string    `json:"request_id"`
	State          string    `json:"state"` // "processing", "executing_tool", "completed", "error"
	StartTime      time.Time `json:"start_time"`
	LastActivity   time.Time `json:"last_activity"`
	CurrentTool    string    `json:"current_tool,omitempty"`
}

// ChatRequest is the expected payload for chat.request messages.
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
}

// ChatResponse is the payload for chat.response messages.
type ChatResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`
	Error          string `json:"error,omitempty"`
}

// NewChatHandler creates a new ChatHandler.
// The dispatcher parameter is optional; if nil, requests go directly to the loop.
func NewChatHandler(loop *AgentLoop, dispatcher *Dispatcher, msgBus *bus.MessageBus, logger *slog.Logger) *ChatHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatHandler{
		loop:       loop,
		dispatcher: dispatcher,
		bus:        msgBus,
		logger:     logger,
		workers:    make(map[string]*Worker),
	}
}

// Start begins listening for chat requests.
func (h *ChatHandler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	// Subscribe to chat requests
	chatSub := h.bus.Subscribe("chat-handler", "chat.request")

	// Subscribe to worker list requests
	workerSub := h.bus.Subscribe("worker-handler", "agent.workers.list")

	// Subscribe to task completion events for result push-back
	taskCompletedSub := h.bus.Subscribe("chat-handler", "task.completed")
	taskFailedSub := h.bus.Subscribe("chat-handler", "task.failed")

	h.wg.Add(4)

	// Chat request handler
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(chatSub)
				return
			case msg, ok := <-chatSub.Channel:
				if !ok {
					return
				}
				h.handleRequest(ctx, msg)
			}
		}
	}()

	// Worker list handler
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(workerSub)
				return
			case msg, ok := <-workerSub.Channel:
				if !ok {
					return
				}
				h.handleWorkerListRequest(msg)
			}
		}
	}()

	// Task completed handler - push results back to linked session
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(taskCompletedSub)
				return
			case msg, ok := <-taskCompletedSub.Channel:
				if !ok {
					return
				}
				h.handleTaskCompleted(msg)
			}
		}
	}()

	// Task failed handler - push error back to linked session
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(taskFailedSub)
				return
			case msg, ok := <-taskFailedSub.Channel:
				if !ok {
					return
				}
				h.handleTaskFailed(msg)
			}
		}
	}()

	h.logger.Info("ChatHandler started")
	return nil
}

// handleWorkerListRequest responds to worker list queries.
func (h *ChatHandler) handleWorkerListRequest(msg *models.BusMessage) {
	workers := h.GetWorkers()

	response := map[string]any{
		"workers": workers,
		"count":   len(workers),
	}

	payload, _ := json.Marshal(response)

	respMsg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeResponse,
		Topic:     "agent.workers.result",
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   msg.ID,
	}

	h.bus.Publish("agent.workers.result", respMsg)
}

// Stop gracefully stops the handler.
func (h *ChatHandler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name for the registry.
func (h *ChatHandler) Name() string {
	return "chat-handler"
}

// handleRequest processes a single chat request.
func (h *ChatHandler) handleRequest(ctx context.Context, msg *models.BusMessage) {
	h.logger.Debug("Received chat request", "id", msg.ID)

	// Parse request payload
	var req ChatRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.logger.Error("Failed to parse chat request", "error", err)
		h.sendError(msg.ID, "invalid request format: "+err.Error())
		return
	}

	if req.Message == "" {
		h.sendError(msg.ID, "message is required")
		return
	}

	// Generate conversation ID if not provided
	conversationID := req.ConversationID
	if conversationID == "" {
		conversationID = generateConversationID()
	}

	// Create worker to track this request
	workerID := generateWorkerID()
	worker := &Worker{
		ID:             workerID,
		ConversationID: conversationID,
		RequestID:      msg.ID,
		State:          "processing",
		StartTime:      time.Now(),
		LastActivity:   time.Now(),
	}
	h.registerWorker(worker)
	defer h.unregisterWorker(workerID)

	// Publish worker started event
	h.publishWorkerEvent("worker.started", worker)

	// Process the message
	h.logger.Info("Processing chat message",
		"worker", workerID,
		"conversation", conversationID,
		"message_length", len(req.Message),
		"use_dispatcher", h.dispatcher != nil,
	)

	var reply string
	var err error

	if h.dispatcher != nil {
		// Multi-agent mode: classify and route through dispatcher
		result, dispatchErr := h.dispatcher.ClassifyAndRoute(ctx, req.Message, conversationID)
		if dispatchErr != nil {
			h.logger.Error("Dispatch failed", "error", dispatchErr)
			err = dispatchErr
		} else if h.dispatcher.ShouldDispatchAsync(result) && result.Task != nil {
			// Async dispatch: send ack immediately, let orchestrator handle it
			h.logger.Info("Async dispatch: sending ack and publishing plan request",
				"task_id", result.Task.ID,
				"agent", result.AgentID,
				"intent", result.Intent.Type,
			)
			reply = fmt.Sprintf(`{"async":true,"task_id":"%s","message":"Working on task: %s"}`,
				result.Task.ID, truncateString(result.Task.Name, 80))

			// Publish plan request to orchestrator
			h.publishPlanRequest(result, conversationID)
		} else {
			h.logger.Debug("Dispatched to agent",
				"agent", result.AgentID,
				"intent", result.Intent.Type,
				"confidence", result.Intent.Confidence,
			)
			reply, err = h.dispatcher.RouteToAgent(ctx, result, conversationID)
		}
	} else {
		// Direct mode: send to standalone agent loop
		reply, err = h.loop.RunOnce(ctx, req.Message, conversationID)
	}

	// Update worker state
	worker.LastActivity = time.Now()
	if err != nil {
		worker.State = "error"
		h.logger.Error("Agent loop failed",
			"worker", workerID,
			"error", err,
		)
		h.publishWorkerEvent("worker.error", worker)
	} else {
		worker.State = "completed"
		h.publishWorkerEvent("worker.completed", worker)
	}

	// Build response
	response := ChatResponse{
		ConversationID: conversationID,
	}

	if err != nil {
		response.Error = err.Error()
		response.Reply = reply // AgentLoop returns a user-friendly message even on error
	} else {
		response.Reply = reply
	}

	// Send response
	h.sendResponse(msg.ID, response)
}

// publishPlanRequest sends a plan request to the orchestrator via the bus.
func (h *ChatHandler) publishPlanRequest(result *DispatchResult, sessionID string) {
	req := PlanRequest{
		TaskID:    result.Task.ID,
		SessionID: sessionID,
		Input:     result.Task.Description,
		Intent:    result.Intent.Type,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		h.logger.Error("Failed to marshal plan request", "error", err)
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeRequest,
		Topic:     "orchestrator.plan",
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	delivered := h.bus.Publish("orchestrator.plan", msg)
	h.logger.Debug("Published plan request",
		"task_id", result.Task.ID,
		"delivered", delivered,
	)
}

// sendResponse publishes a chat response.
func (h *ChatHandler) sendResponse(replyTo string, response ChatResponse) {
	payload, err := json.Marshal(response)
	if err != nil {
		h.logger.Error("Failed to marshal response", "error", err)
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeResponse,
		Topic:     "chat.response",
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo, // This matches the original request ID for the proxy
	}

	delivered := h.bus.Publish("chat.response", msg)
	h.logger.Debug("Sent chat response",
		"reply_to", replyTo,
		"delivered", delivered,
	)
}

// sendError sends an error response.
func (h *ChatHandler) sendError(replyTo, errorMsg string) {
	response := ChatResponse{
		Error: errorMsg,
		Reply: "I encountered an error: " + errorMsg,
	}
	h.sendResponse(replyTo, response)
}

// registerWorker adds a worker to the tracking map.
func (h *ChatHandler) registerWorker(w *Worker) {
	h.workersMu.Lock()
	defer h.workersMu.Unlock()
	h.workers[w.ID] = w
}

// unregisterWorker removes a worker from the tracking map.
func (h *ChatHandler) unregisterWorker(id string) {
	h.workersMu.Lock()
	defer h.workersMu.Unlock()
	delete(h.workers, id)
}

// GetWorkers returns a snapshot of active workers.
func (h *ChatHandler) GetWorkers() []*Worker {
	h.workersMu.RLock()
	defer h.workersMu.RUnlock()

	workers := make([]*Worker, 0, len(h.workers))
	for _, w := range h.workers {
		// Create a copy
		copy := *w
		workers = append(workers, &copy)
	}
	return workers
}

// GetWorkerCount returns the number of active workers.
func (h *ChatHandler) GetWorkerCount() int {
	h.workersMu.RLock()
	defer h.workersMu.RUnlock()
	return len(h.workers)
}

// publishWorkerEvent publishes a worker lifecycle event.
func (h *ChatHandler) publishWorkerEvent(topic string, w *Worker) {
	payload, err := json.Marshal(w)
	if err != nil {
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     topic,
		Source:    "chat-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.bus.Publish(topic, msg)
}

// handleTaskCompleted handles task.completed events and pushes results back to chat.
func (h *ChatHandler) handleTaskCompleted(msg *models.BusMessage) {
	var payload struct {
		TaskID         string   `json:"task_id"`
		Name           string   `json:"name"`
		CompletedJobs  int      `json:"completed_jobs"`
		TotalJobs      int      `json:"total_jobs"`
		LinkedSessions []string `json:"linked_sessions"`
		Result         string   `json:"result,omitempty"`
	}

	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Error("Failed to parse task.completed payload", "error", err)
		return
	}

	h.logger.Info("Task completed, pushing result to chat",
		"task_id", payload.TaskID,
		"name", payload.Name,
		"completed", payload.CompletedJobs,
		"total", payload.TotalJobs,
	)

	// Build completion message
	resultSummary := payload.Result
	if resultSummary == "" {
		resultSummary = fmt.Sprintf("Completed %d/%d steps successfully", payload.CompletedJobs, payload.TotalJobs)
	}
	if len(resultSummary) > 200 {
		resultSummary = resultSummary[:197] + "..."
	}

	response := ChatResponse{
		Reply: fmt.Sprintf(`{"task_completed":true,"task_id":"%s","name":"%s","completed":%d,"total":%d,"result":"%s"}`,
			payload.TaskID,
			truncateString(payload.Name, 80),
			payload.CompletedJobs,
			payload.TotalJobs,
			truncateString(resultSummary, 100),
		),
	}

	// Send to all linked sessions
	for _, sessionID := range payload.LinkedSessions {
		response.ConversationID = sessionID
		h.sendResponse("task-completed-"+payload.TaskID, response)
	}

	// If no linked sessions, broadcast on task.result topic
	if len(payload.LinkedSessions) == 0 {
		h.sendResponse("task-completed-"+payload.TaskID, response)
	}
}

// handleTaskFailed handles task.failed events and pushes errors back to chat.
func (h *ChatHandler) handleTaskFailed(msg *models.BusMessage) {
	var payload struct {
		TaskID         string   `json:"task_id"`
		Name           string   `json:"name"`
		FailedJobs     int      `json:"failed_jobs"`
		CompletedJobs  int      `json:"completed_jobs"`
		TotalJobs      int      `json:"total_jobs"`
		LinkedSessions []string `json:"linked_sessions"`
		Error          string   `json:"error,omitempty"`
		FailedStep     string   `json:"failed_step,omitempty"`
	}

	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Error("Failed to parse task.failed payload", "error", err)
		return
	}

	h.logger.Warn("Task failed, pushing error to chat",
		"task_id", payload.TaskID,
		"name", payload.Name,
		"failed", payload.FailedJobs,
		"completed", payload.CompletedJobs,
		"total", payload.TotalJobs,
		"error", payload.Error,
	)

	// Build error message
	errorSummary := payload.Error
	if errorSummary == "" {
		errorSummary = fmt.Sprintf("Task failed: %d/%d steps failed", payload.FailedJobs, payload.TotalJobs)
	}
	if len(errorSummary) > 200 {
		errorSummary = errorSummary[:197] + "..."
	}

	response := ChatResponse{
		Reply: fmt.Sprintf(`{"task_failed":true,"task_id":"%s","name":"%s","failed":%d,"completed":%d,"total":%d,"error":"%s"}`,
			payload.TaskID,
			truncateString(payload.Name, 80),
			payload.FailedJobs,
			payload.CompletedJobs,
			payload.TotalJobs,
			truncateString(errorSummary, 100),
		),
		Error: errorSummary,
	}

	// Send to all linked sessions
	for _, sessionID := range payload.LinkedSessions {
		response.ConversationID = sessionID
		h.sendResponse("task-failed-"+payload.TaskID, response)
	}

	// If no linked sessions, broadcast
	if len(payload.LinkedSessions) == 0 {
		h.sendResponse("task-failed-"+payload.TaskID, response)
	}
}

// generateWorkerID creates a unique worker ID.
func generateWorkerID() string {
	return "worker-" + generateMessageID()
}

// generateMessageID creates a unique message ID.
func generateMessageID() string {
	return time.Now().Format("20060102150405.000000000")
}
