// Package agent provides the agent loop and related components.
package agent

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// ChatHandler bridges the message bus to the AgentLoop.
// It subscribes to chat.request and publishes responses to chat.response.
type ChatHandler struct {
	loop         *AgentLoop
	dispatcher   *Dispatcher // Optional: if set, routes through multi-agent dispatch
	bus          *bus.MessageBus
	logger       *slog.Logger
	metricsStore *metrics.Store  // Optional: metrics store for duration estimates
	stepStore    *task.StepStore // Optional: step store for fetching step summaries
	taskStore    *task.Store     // Optional: task store for looking up linked sessions

	// Budget tracking for async dispatch pre-check (Issue 0039)
	budget *llm.Budget

	// CollaborationEngine for starting collaboration sessions from IntentCollaborate
	collabEngine *CollaborationEngine

	// Synchronous dispatch mode: when true, async-dispatched tasks wait
	// for completion instead of returning immediately (Issue 0022).
	syncMode bool

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
	Message        string              `json:"message"`
	ConversationID string              `json:"conversation_id"`
	SourceClient   string              `json:"source_client,omitempty"`
	Parts          []llm.ContentPart   `json:"parts,omitempty"`
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
	chatSub := h.bus.Subscribe(SourceChatHandler, "chat.request")

	// Subscribe to worker list requests
	workerSub := h.bus.Subscribe("worker-handler", "agent.workers.list")

	// Subscribe to task completion events for result push-back
	taskCompletedSub := h.bus.Subscribe(SourceChatHandler, "task.completed")
	taskFailedSub := h.bus.Subscribe(SourceChatHandler, "task.failed")

	// Subscribe to agent progress events to keep worker state in sync with
	// the agent loop's stage transitions (thinking vs. executing tools).
	progressSub := h.bus.Subscribe(SourceChatHandler, "agent.progress")

	// Subscribe to review events to push review feedback to linked sessions
	reviewCompletedSub := h.bus.Subscribe(SourceChatHandler, "task.review_completed")

	// Subscribe to pair result events to push results back to chat sessions
	pairResultSub := h.bus.Subscribe(SourceChatHandler, TopicPairResult)

	// Subscribe to collaboration result events to push results back to chat sessions
	collabResultSub := h.bus.Subscribe(SourceChatHandler, TopicCollabResult)

	h.wg.Add(8)

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

	// Agent progress handler - syncs worker state with loop stage transitions
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(progressSub)
				return
			case msg, ok := <-progressSub.Channel:
				if !ok {
					return
				}
				h.handleAgentProgress(msg)
			}
		}
	}()

	// Review completed handler - push review feedback to linked sessions
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(reviewCompletedSub)
				return
			case msg, ok := <-reviewCompletedSub.Channel:
				if !ok {
					return
				}
				h.handleReviewCompleted(msg)
			}
		}
	}()

	// Pair result handler - push pair session results back to chat
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(pairResultSub)
				return
			case msg, ok := <-pairResultSub.Channel:
				if !ok {
					return
				}
				h.handlePairResult(msg)
			}
		}
	}()

	// Collaboration result handler - push collaboration session results back to chat
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(collabResultSub)
				return
			case msg, ok := <-collabResultSub.Channel:
				if !ok {
					return
				}
				h.handleCollabResult(msg)
			}
		}
	}()

	h.logger.Info("ChatHandler started")
	return nil
}

// handleAgentProgress updates worker state/current tool based on agent.progress
// events so the TUI viz reflects reasoning vs tool-execution phases.
func (h *ChatHandler) handleAgentProgress(msg *models.BusMessage) {
	var payload struct {
		ConversationID string `json:"conversation_id"`
		Stage          string `json:"stage"`
		Detail         string `json:"detail"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	if payload.ConversationID == "" {
		return
	}

	h.workersMu.Lock()
	defer h.workersMu.Unlock()
	for _, w := range h.workers {
		if w.ConversationID != payload.ConversationID {
			continue
		}
		// Don't override terminal states.
		if w.State == ReportStatusCompleted || w.State == string(MessageTypeError) {
			continue
		}
		changed := false
		switch payload.Stage {
		case "executing":
			if w.State != "executing_tool" || w.CurrentTool != payload.Detail {
				w.State = "executing_tool"
				w.CurrentTool = payload.Detail
				changed = true
			}
		case "thinking":
			if w.State != "processing" || w.CurrentTool != "" {
				w.State = "processing"
				w.CurrentTool = ""
				changed = true
			}
		}
		w.LastActivity = time.Now()
		if changed {
			// Snapshot before releasing the lock so the publish goroutine
			// doesn't race with future mutations of w.
			snapshot := *w
			go h.publishWorkerEvent("worker.state_changed", &snapshot)
		}
	}
}

// handleReviewCompleted handles task.review_completed events and pushes
// review feedback to chat sessions linked to the task.
func (h *ChatHandler) handleReviewCompleted(msg *models.BusMessage) {
	var payload struct {
		TaskID        string  `json:"task_id"`
		StepID        string  `json:"step_id"`
		Status        string  `json:"status"`
		Feedback      string  `json:"feedback"`
		Confidence    float64 `json:"confidence"`
		Reviewer      string  `json:"reviewer"`
		RevisionCount int     `json:"revision_count"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Error("Failed to parse review completed payload", "error", err)
		return
	}

	h.logger.Info("Review completed, pushing feedback to linked sessions",
		"task_id", payload.TaskID,
		"step_id", payload.StepID,
		"status", payload.Status,
		"revision_count", payload.RevisionCount,
	)

	// Look up linked sessions from task store
	var linkedSessions []string
	if h.taskStore != nil && payload.TaskID != "" {
		sessions, err := h.taskStore.GetLinkedSessions(payload.TaskID)
		if err != nil {
			h.logger.Warn("Failed to get linked sessions for review feedback", "task_id", payload.TaskID, "error", err)
		} else {
			linkedSessions = sessions
		}
	}

	// Build human-readable review feedback message
	reply := h.formatReviewFeedback(payload.StepID, payload.Status, payload.Feedback, payload.RevisionCount, payload.Reviewer)

	response := ChatResponse{
		Reply: reply,
	}

	// Send to linked sessions if we have them
	if len(linkedSessions) > 0 {
		for _, sessionID := range linkedSessions {
			response.ConversationID = sessionID
			h.sendResponse("review-completed-"+payload.StepID, response)
		}
	} else {
		// No linked sessions found - broadcast for any listening client
		h.sendResponse("review-completed-"+payload.StepID, response)
	}
}

// formatReviewFeedback builds a human-readable review feedback message.
func (h *ChatHandler) formatReviewFeedback(stepID, status, feedback string, revisionCount int, reviewer string) string {
	var sb strings.Builder

	switch status {
	case "rejected":
		if revisionCount > 0 {
			fmt.Fprintf(&sb, "## review: rejected (revision #%d)\n\n", revisionCount)
		} else {
			sb.WriteString("## review: rejected\n\n")
		}
		if feedback != "" {
			fmt.Fprintf(&sb, "**feedback:** %s\n", truncateString(feedback, 200))
		}
		if revisionCount > 0 {
			sb.WriteString("\nrevision step created and queued.\n")
		}
	case "needs_info":
		sb.WriteString("## review: needs more info\n\n")
		if feedback != "" {
			fmt.Fprintf(&sb, "**feedback:** %s\n", truncateString(feedback, 200))
		}
	case "approved":
		sb.WriteString("## review: approved\n")
		if feedback != "" {
			fmt.Fprintf(&sb, "\n%s\n", truncateString(feedback, 100))
		}
	default:
		fmt.Fprintf(&sb, "## review: %s\n", status)
		if feedback != "" {
			fmt.Fprintf(&sb, "\n%s\n", truncateString(feedback, 200))
		}
	}

	return sb.String()
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
		Source:    SourceChatHandler,
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
	return SourceChatHandler
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

	// Broadcast chat.message.received for bilateral visibility.
	// All session participants see who sent what.
	if req.SourceClient != "" {
		broadcastPayload, _ := json.Marshal(map[string]string{
			"session_id":    conversationID,
			"source_client": req.SourceClient,
			"content":       req.Message,
			"timestamp":     time.Now().UTC().Format(time.RFC3339),
		})
		broadcastMsg := &models.BusMessage{
			ID:        generateMessageID(),
			Type:      models.MessageTypeEvent,
			Topic:     "chat.message.received",
			Source:    SourceChatHandler,
			Timestamp: time.Now().UTC(),
			Payload:   broadcastPayload,
		}
		h.bus.Publish("chat.message.received", broadcastMsg)
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
	var result *DispatchResult

	if h.dispatcher != nil {
		// Multi-agent mode: classify and route through dispatcher
		var dispatchErr error
		result, dispatchErr = h.dispatcher.ClassifyAndRoute(ctx, req.Message, conversationID)
		switch {
		case dispatchErr != nil:
			h.logger.Error("Dispatch failed", "error", dispatchErr)
			err = dispatchErr
			// Include classification failure guidance for the user.
			if result != nil && result.ClassificationNotice != "" {
				guidance := result.ClassificationNotice
				h.logger.Info("Classification failure details",
					"guidance", guidance,
				)
				// Attach guidance to the reply so the user sees actionable info.
				if reply == "" {
					reply = guidance
				}
			}
		case h.dispatcher.ShouldRouteToPair(result):
			// Route to pair-channel mode for dual-agent conversation
			h.logger.Info("Routing to pair-channel mode",
				"session", conversationID,
				"actor", result.AgentID,
			)
			reply = h.startPairSession(result, conversationID)
		case h.dispatcher.ShouldRouteToCollaborate(result):
			// Route to collaboration engine for multi-agent collaboration
			h.logger.Info("Routing to collaboration engine",
				"session", conversationID,
				"agent", result.AgentID,
			)
			reply, err = h.startCollaborationSession(ctx, result, conversationID)
		case h.dispatcher.ShouldDispatchAsync(result) && result.Task != nil:
			// Issue 0039: budget pre-check before async dispatch.
			// Block before creating a zombie task that can never complete.
			if h.budget != nil {
				if budgetResult := h.budget.CheckBudget(); budgetResult.Exceeded {
					// Cancel the task that ClassifyAndRoute just created
					if h.taskStore != nil && result.Task != nil {
						result.Task.SetState(task.StateFailed)
						if updateErr := h.taskStore.Update(result.Task); updateErr != nil {
							h.logger.Warn("Failed to cancel budget-blocked task",
								"task_id", result.Task.ID, "error", updateErr)
						}
					}
					err = &llm.BudgetExceededError{
						Message: budgetResult.Reason.Message(budgetResult.Used, budgetResult.Limit),
						Reason:  budgetResult.Reason,
						Used:    budgetResult.Used,
						Limit:   budgetResult.Limit,
					}
					break
				}
			}

			if h.syncMode {
				// Issue 0022: synchronous mode -- wait for task completion
				h.logger.Info("Sync dispatch: publishing plan request and waiting for completion",
					"task_id", result.Task.ID,
					"agent", result.AgentID,
					"intent", result.Intent.Type,
				)
				h.publishPlanRequest(result, conversationID)
				reply = h.waitForTaskCompletion(ctx, result.Task.ID)
			} else {
				// Async dispatch: send ack immediately, let orchestrator handle it
				h.logger.Info("Async dispatch: sending ack and publishing plan request",
					"task_id", result.Task.ID,
					"agent", result.AgentID,
					"intent", result.Intent.Type,
				)
				// Build human-readable acknowledgment
				// Use dispatcher-provided steps, falling back to step store
				steps := result.Steps
				if len(steps) == 0 {
					steps = h.fetchStepSummaries(result.Task.ID)
				}
				reply = h.FormatEnhancedAsyncTaskAck(result, steps, h.estimateDuration(result.Task.ID, len(steps)), h.getPlanReference(result.Task.ID))

				// Publish plan request to orchestrator
				h.publishPlanRequest(result, conversationID)
			}
		default:
			h.logger.Debug("Dispatched to agent",
				"agent", result.AgentID,
				"intent", result.Intent.Type,
				"confidence", result.Intent.Confidence,
			)
			reply, err = h.dispatcher.RouteToAgent(ctx, result, conversationID)
		}
	} else {
		// Direct mode: send to standalone agent loop
		reply, err = h.loop.RunOnceWithParts(ctx, req.Message, req.Parts, conversationID)
	}

	// Append classification degradation notice when dispatch used a fallback classifier.
	if h.dispatcher != nil && result != nil && result.ClassificationNotice != "" && err == nil {
		if reply != "" {
			reply += "\n\n" + result.ClassificationNotice
		} else {
			reply = result.ClassificationNotice
		}
	}

	// Update worker state
	worker.LastActivity = time.Now()
	if err != nil {
		worker.State = string(MessageTypeError)
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
		// Check for BudgetExceededError to provide user-friendly message
		var budgetErr *llm.BudgetExceededError
		if errors.As(err, &budgetErr) {
			response.Error = budgetErr.UserMessage()
			response.Reply = budgetErr.UserMessage()
		} else {
			response.Error = err.Error()
			response.Reply = reply // AgentLoop returns a user-friendly message even on error
		}
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

	if result.Intent.Type == string(IntentCompound) {
		req.IsCompound = true
		if result.Task != nil && result.Task.Metadata != nil {
			var meta map[string]any
			if json.Unmarshal(result.Task.Metadata, &meta) == nil {
				if ct, ok := meta["compound_type"]; ok {
					req.CompoundType, _ = ct.(string)
				}
			}
		}
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
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	delivered := h.bus.Publish("orchestrator.plan", msg)
	if delivered == 0 {
		h.logger.Warn("Plan request published with no subscribers",
			"task_id", result.Task.ID,
		)
	} else {
		h.logger.Debug("Published plan request",
			"task_id", result.Task.ID,
			"delivered", delivered,
		)
	}
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
		Source:    SourceChatHandler,
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
		msgCopy := *w
		workers = append(workers, &msgCopy)
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
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.bus.Publish(topic, msg)
}

// TaskStepSummary represents a step in a task completion payload.
type TaskStepSummary struct {
	ID                 string `json:"id"`
	Description        string `json:"description"`
	State              string `json:"state"`
	Result             string `json:"result,omitempty"`
	AgentID            string `json:"agent_id,omitempty"`
	ModelOverride      string `json:"model_override,omitempty"`
	AccumulatedContext string `json:"accumulated_context,omitempty"`
}

// handleTaskCompleted handles task.completed events and pushes results back to chat.
func (h *ChatHandler) handleTaskCompleted(msg *models.BusMessage) {
	var payload struct {
		TaskID         string            `json:"task_id"`
		Name           string            `json:"name"`
		CompletedJobs  int               `json:"completed_jobs"`
		TotalJobs      int               `json:"total_jobs"`
		LinkedSessions []string          `json:"linked_sessions"`
		Steps          []TaskStepSummary `json:"steps,omitempty"`
		ExecutionTime  string            `json:"execution_time,omitempty"`
		Result         string            `json:"result,omitempty"`
		TokenUsage     int               `json:"token_usage,omitempty"`
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

	// Build human-readable completion message
	// Use event-provided steps, falling back to step store
	steps := payload.Steps
	if len(steps) == 0 {
		steps = h.fetchStepSummaries(payload.TaskID)
	}
	reply := h.formatTaskCompletedMessage(payload.Name, steps, payload.ExecutionTime, payload.Result, payload.CompletedJobs, payload.TotalJobs, payload.TokenUsage)

	response := ChatResponse{
		Reply: reply,
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

// formatTaskCompletedMessage builds a human-readable task completion message.
func (h *ChatHandler) formatTaskCompletedMessage(name string, steps []TaskStepSummary, executionTime, result string, completed, total, tokenUsage int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## task completed: %s\n\n", strings.ToLower(name))

	if len(steps) > 0 {
		sb.WriteString("### steps:\n")
		for i, step := range steps {
			icon := "+"
			if step.State != "completed" && step.State != "approved" {
				icon = "x"
			}
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, icon, strings.ToLower(step.Description))
			if step.Result != "" {
				resultPreview := truncateString(step.Result, 80)
				fmt.Fprintf(&sb, "   %s\n", resultPreview)
			}
		}
		sb.WriteString("\n")
	} else {
		fmt.Fprintf(&sb, "completed %d/%d steps successfully.\n\n", completed, total)
	}

	if result != "" {
		fmt.Fprintf(&sb, "**summary:** %s\n\n", result)
	}

	if executionTime != "" {
		fmt.Fprintf(&sb, "completed in %s\n", executionTime)
	}

	if tokenUsage > 0 {
		if executionTime != "" {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "**token usage:** %s\n", formatTokenCount(tokenUsage))
	}

	return sb.String()
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

	// Build human-readable error message
	reply := h.formatTaskFailedMessage(payload.Name, payload.Error, payload.FailedStep, payload.FailedJobs, payload.CompletedJobs, payload.TotalJobs)

	response := ChatResponse{
		Reply: reply,
		Error: payload.Error,
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

// formatTaskFailedMessage builds a human-readable task failure message.
func (h *ChatHandler) formatTaskFailedMessage(name, errMsg, failedStep string, failed, completed, total int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## task failed: %s\n\n", strings.ToLower(name))

	fmt.Fprintf(&sb, "**progress:** %d/%d steps completed, %d failed\n\n", completed, total, failed)

	if failedStep != "" {
		fmt.Fprintf(&sb, "**failed at step:** %s\n\n", failedStep)
	}

	if errMsg != "" {
		fmt.Fprintf(&sb, "**error:** %s\n", truncateString(errMsg, 200))
	}

	return sb.String()
}

// FormatAsyncTaskAck builds a human-readable acknowledgment for async task dispatch.
// It delegates to FormatEnhancedAsyncTaskAck with no step details.
func (h *ChatHandler) FormatAsyncTaskAck(result *DispatchResult) string {
	return h.FormatEnhancedAsyncTaskAck(result, nil, 0, result.Task.ID)
}

// FormatEnhancedAsyncTaskAck builds an enhanced acknowledgment for async task
// dispatch that includes subtask count, bulleted summary, estimated duration,
// and plan reference.
func (h *ChatHandler) FormatEnhancedAsyncTaskAck(
	result *DispatchResult,
	steps []TaskStepSummary,
	estimatedMinutes int,
	planRef string,
) string {
	var sb strings.Builder
	sb.WriteString("## starting task\n\n")
	fmt.Fprintf(&sb, "**task:** %s\n", strings.ToLower(result.Task.Name))
	fmt.Fprintf(&sb, "**id:** `%s`\n", result.Task.ID)

	// Plan line: plan reference | subtask count | optional duration
	planLine := fmt.Sprintf("**plan:** `%s` | %d subtasks", planRef, len(steps))
	if estimatedMinutes > 0 {
		planLine += fmt.Sprintf(" | est. %d-%d min", estimatedMinutes, estimatedMinutes+5)
	}
	sb.WriteString(planLine + "\n")

	sb.WriteString("\n")

	// Detect agent diversity
	agentSet := make(map[string]bool)
	for _, step := range steps {
		if step.AgentID != "" {
			agentSet[step.AgentID] = true
		}
	}

	// Add agent summary line if multiple agents
	if len(agentSet) > 1 {
		agents := make([]string, 0, len(agentSet))
		for agent := range agentSet {
			agents = append(agents, agent)
		}
		sort.Strings(agents)
		fmt.Fprintf(&sb, "**agents:** %s\n", strings.Join(agents, ", "))
	}

	sb.WriteString("**subtasks:**\n")

	displayLimit := 5
	for i, step := range steps {
		if i >= displayLimit {
			break
		}
		agentLabel := step.AgentID
		if agentLabel == "" {
			agentLabel = "agent"
		}
		desc := truncateString(step.Description, 50)
		fmt.Fprintf(&sb, "- %s (%s)\n", strings.ToLower(desc), agentLabel)
	}

	if len(steps) > displayLimit {
		fmt.Fprintf(&sb, "- ... and %d more\n", len(steps)-displayLimit)
	}

	sb.WriteString("\n")
	sb.WriteString("you will receive updates as subtasks complete.\n")

	return sb.String()
}

// fetchStepSummaries retrieves step summaries for a task from the step store.
func (h *ChatHandler) fetchStepSummaries(taskID string) []TaskStepSummary {
	if h.stepStore == nil {
		return nil
	}
	steps, err := h.stepStore.ListByTaskID(taskID)
	if err != nil {
		h.logger.Debug("Failed to fetch steps for ACK",
			"task_id", taskID,
			"error", err,
		)
		return nil
	}

	summaries := make([]TaskStepSummary, len(steps))
	for i, s := range steps {
		summaries[i] = TaskStepSummary{
			Description: s.Description,
			AgentID:     s.AgentID,
		}
	}
	return summaries
}

// estimateDuration returns estimated duration based on step count and historical data.
func (h *ChatHandler) estimateDuration(_ string, stepCount int) int {
	if stepCount <= 0 {
		return 0
	}
	if h.metricsStore != nil {
		avgDuration := h.metricsStore.GetAverageStepDuration("orchestrator")
		if avgDuration > 0 {
			totalMin := int(avgDuration.Minutes()) * stepCount
			if totalMin > 0 {
				return totalMin
			}
		}
	}
	return stepCount * 4 // fallback: 4 minutes per step
}

// getPlanReference returns the plan reference for a task.
func (h *ChatHandler) getPlanReference(taskID string) string {
	return taskID
}

// SetMetricsStore sets the metrics store for duration estimates.
func (h *ChatHandler) SetMetricsStore(store *metrics.Store) {
	if store != nil {
		h.metricsStore = store
	}
}

// SetStepStore sets the step store for fetching step summaries.
func (h *ChatHandler) SetStepStore(store *task.StepStore) {
	if store != nil {
		h.stepStore = store
	}
}

// SetTaskStore sets the task store for looking up linked sessions.
func (h *ChatHandler) SetTaskStore(store *task.Store) {
	if store != nil {
		h.taskStore = store
	}
}

// SetBudget sets the token budget tracker for async dispatch pre-checks.
// This prevents zombie tasks from being created when the budget is exceeded.
func (h *ChatHandler) SetBudget(budget *llm.Budget) {
	if budget != nil {
		h.budget = budget
	}
}

// SetSyncMode enables or disables synchronous dispatch mode.
// When enabled, async-dispatched tasks are waited on in the handler
// instead of returning immediately.
func (h *ChatHandler) SetSyncMode(enabled bool) {
	h.syncMode = enabled
}

// waitForTaskCompletion waits for a task to reach a terminal state
// and returns the final result string. Returns immediately if the task
// is already terminal or if the store/ctx is not available.
func (h *ChatHandler) waitForTaskCompletion(ctx context.Context, taskID string) string {
	if h.taskStore == nil || taskID == "" {
		return ""
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	done := time.After(10 * time.Minute) // overall timeout

	for {
		select {
		case <-ctx.Done():
			return ""
		case <-done:
			h.logger.Warn("Task wait timeout exceeded", "task_id", taskID)
			return ""
		case <-ticker.C:
			t, err := h.taskStore.GetByID(taskID)
			if err != nil {
				h.logger.Debug("Failed to poll task status", "task_id", taskID, "error", err)
				continue
			}
			if t == nil {
				continue
			}
			if t.State.IsTerminal() {
				if t.State == task.StateFailed {
					return fmt.Sprintf("Task %s failed after reaching terminal state.", taskID)
				}
				return fmt.Sprintf("Task %s completed.", taskID)
			}
		}
	}
}

// startPairSession initiates a pair-channel session and returns an acknowledgment.
func (h *ChatHandler) startPairSession(result *DispatchResult, conversationID string) string {
	// Determine actor and reviewer from the dispatch result
	actorID := result.AgentID
	if actorID == "" {
		actorID = "analyst"
	}
	reviewerID := h.pairReviewerForActor(actorID)

	req := PairStartRequest{
		SessionID:     conversationID,
		ActorID:       actorID,
		ReviewerID:    reviewerID,
		InitialPrompt: result.Intent.Summary,
		MaxTurns:      5,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		h.logger.Error("Failed to marshal pair start request", "error", err)
		return "Failed to start pair session."
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeRequest,
		Topic:     TopicPairStart,
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	delivered := h.bus.Publish(TopicPairStart, msg)
	if delivered == 0 {
		h.logger.Warn("Pair start published with no subscribers")
		return "Pair session requested but no orchestrator is listening."
	}

	return fmt.Sprintf("## pair session started\n\n**actor:** %s\n**reviewer:** %s\n\nagents are collaborating. you will see updates as turns complete.", actorID, reviewerID)
}

// pairReviewerForActor selects an appropriate reviewer agent for the given actor.
func (h *ChatHandler) pairReviewerForActor(actorID string) string {
	switch actorID {
	case "coder":
		return "planner"
	case "analyst":
		return "planner"
	case "debugger":
		return "coder"
	case "planner":
		return "analyst"
	default:
		return "planner"
	}
}

// SetCollaborationEngine sets the collaboration engine for starting collaboration sessions.
func (h *ChatHandler) SetCollaborationEngine(engine *CollaborationEngine) {
	if engine != nil {
		h.collabEngine = engine
	}
}

// startCollaborationSession initiates a collaboration session via the CollaborationEngine
// and returns an acknowledgment. The session runs asynchronously; results are delivered
// via the collaboration.result bus topic and pushed back to chat by handleCollabResult.
func (h *ChatHandler) startCollaborationSession(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
	if h.collabEngine == nil {
		return "Collaboration engine is not available. Falling back to single-agent processing.", nil
	}

	// Determine mode and participants from the dispatch result.
	mode := "pair_programming"
	if result.Intent.Summary != "" {
		summary := strings.ToLower(result.Intent.Summary)
		if strings.Contains(summary, "differential") || strings.Contains(summary, "a/b") {
			mode = "differential"
		}
	}

	actorID := result.AgentID
	if actorID == "" {
		actorID = IntentCollaborate.DefaultAgent()
	}
	reviewerID := h.pairReviewerForActor(actorID)

	taskID := ""
	if result.Task != nil {
		taskID = result.Task.ID
	}
	if taskID == "" {
		taskID = conversationID
	}

	participants := []string{actorID, reviewerID}
	cfg := DefaultSessionConfig()

	sess, err := h.collabEngine.CreateSession(mode, taskID, participants, cfg)
	if err != nil {
		h.logger.Error("Failed to create collaboration session", "error", err)
		return "", fmt.Errorf("failed to create collaboration session: %w", err)
	}

	// Run the session asynchronously so the chat handler returns immediately.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		runCtx, cancel := context.WithTimeout(ctx, cfg.TimeBudget)
		defer cancel()

		collabResult, runErr := h.collabEngine.RunSession(runCtx, sess.ID)
		if runErr != nil {
			h.logger.Error("Collaboration session failed",
				"session_id", sess.ID,
				"error", runErr,
			)
			return
		}

		h.logger.Info("Collaboration session completed",
			"session_id", collabResult.SessionID,
			"state", collabResult.State,
			"turn_count", collabResult.TurnCount,
		)
	}()

	return fmt.Sprintf("## collaboration started\n\n**mode:** %s\n**participants:** %s\n**session:** `%s`\n\nagents are collaborating. you will see updates as the session progresses.", mode, strings.Join(participants, ", "), sess.ID), nil
}

// handleCollabResult pushes collaboration session results back to chat.
func (h *ChatHandler) handleCollabResult(msg *models.BusMessage) {
	var result CollaborationResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		h.logger.Error("Failed to parse collaboration result", "error", err)
		return
	}

	h.logger.Info("Collaboration session result received",
		"session_id", result.SessionID,
		"state", result.State,
		"turn_count", result.TurnCount,
	)

	reply := h.formatCollabResult(result)

	response := ChatResponse{
		Reply: reply,
	}
	// Use session_id as conversation ID so the originating chat session receives it.
	response.ConversationID = result.SessionID
	h.sendResponse("collab-result-"+result.SessionID, response)
}

// formatCollabResult builds a human-readable collaboration session result.
func (h *ChatHandler) formatCollabResult(result CollaborationResult) string {
	var sb strings.Builder
	sb.WriteString("## collaboration completed\n\n")

	stateLabel := string(result.State)
	if stateLabel == "" {
		stateLabel = "concluded"
	}
	fmt.Fprintf(&sb, "**state:** %s\n", stateLabel)
	fmt.Fprintf(&sb, "**turns:** %d\n", result.TurnCount)

	if result.Duration > 0 {
		fmt.Fprintf(&sb, "**duration:** %s\n", result.Duration.Round(time.Second))
	}

	if result.FinalOutput != "" {
		fmt.Fprintf(&sb, "\n**output:**\n%s\n", truncateString(result.FinalOutput, 500))
	}

	return sb.String()
}

// handlePairResult pushes pair session results back to chat.
func (h *ChatHandler) handlePairResult(msg *models.BusMessage) {
	var result PairResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		h.logger.Error("Failed to parse pair result", "error", err)
		return
	}

	h.logger.Info("Pair session completed",
		"session_id", result.SessionID,
		"total_turns", result.TotalTurns,
		"verdict", result.FinalVerdict,
	)

	reply := h.formatPairResult(result)

	response := ChatResponse{
		ConversationID: result.SessionID,
		Reply:          reply,
	}
	h.sendResponse("pair-result-"+result.SessionID, response)
}

// formatPairResult builds a human-readable pair session result.
func (h *ChatHandler) formatPairResult(result PairResult) string {
	var sb strings.Builder
	sb.WriteString("## pair session completed\n\n")

	verdictLabel := string(result.FinalVerdict)
	if verdictLabel == "" {
		verdictLabel = "concluded"
	}
	fmt.Fprintf(&sb, "**verdict:** %s\n", verdictLabel)
	fmt.Fprintf(&sb, "**turns:** %d\n\n", result.TotalTurns)

	if result.FinalOutput != "" {
		fmt.Fprintf(&sb, "**final output:**\n%s\n", truncateString(result.FinalOutput, 500))
	}

	return sb.String()
}

// generateWorkerID creates a unique worker ID.
func generateWorkerID() string {
	return "worker-" + generateMessageID()
}

// generateMessageID creates a unique message ID.
// Uses timestamp with nanoseconds plus random suffix to avoid collisions.
func generateMessageID() string {
	var randBytes [4]byte
	if _, err := crypto_rand.Read(randBytes[:]); err != nil {
		// Fallback: use nanosecond timestamp uniqueness if crypto/rand fails.
		return time.Now().Format("20060102150405.000000000") + "-" +
			fmt.Sprintf("%0d", time.Now().UnixNano())
	}
	return time.Now().Format("20060102150405.000000000") + "-" + hex.EncodeToString(randBytes[:])
}
