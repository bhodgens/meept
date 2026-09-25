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
	"github.com/caimlas/meept/internal/comm/wsclass"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// SessionStoreReader is the narrow interface ChatHandler needs from a session
// store: looking up a session by ID to inspect its project path. Using a
// local interface avoids requiring tests to implement the full session.Store
// (30+ methods).
type SessionStoreReader interface {
	Get(id string) *session.Session
	// GetByConversationID looks up a session by its conversation ID (not
	// the session's primary ID). Use this instead of Get when the caller
	// only has the conversation ID, as session IDs and conversation IDs
	// are distinct identifiers.
	GetByConversationID(conversationID string) *session.Session
}

// SessionMessageSaver persists chat messages to the session store.
// Wired by the daemon to bridge ChatHandler → session.Store.SaveMessages.
type SessionMessageSaver interface {
	SaveMessages(sessionID string, messages []session.Message) error
}

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
	// syncWaitCeiling overrides waitForTaskCompletion's fixed sync-reply
	// bound (test seam; 0 = stall-based ceiling per syncWaitStall/
	// syncWaitMax, negative = the legacy fixed 110s production default).
	// Must stay well below the CLI's ~120s socket read so a stuck task
	// surfaces a degraded reply instead of an i/o timeout (e2e run 3,
	// 2026-09-10).
	syncWaitCeiling time.Duration
	// syncWaitStall bounds the legacy sync wait by INACTIVITY (stall-based
	// ceiling): the wait polls the task store every 2s and returns the
	// degraded still-running reply when neither the task state nor the
	// step set has changed for this long. Zero (the config default) means
	// stall detection is enabled with syncWaitMax as the hard elapsed cap;
	// negative disables stall detection entirely (legacy fixed ceiling —
	// see syncWaitCeiling). Set by SetSyncWaitStall; test seam.
	syncWaitStall time.Duration
	// syncWaitMax is the hard elapsed cap when stall detection is enabled:
	// even a visibly progressing task gets the degraded reply once the
	// wait has run this long. Set by SetSyncWaitStall; test seam.
	syncWaitMax time.Duration

	// NotificationPublisher for desktop/notification-system events (Plan 4.3).
	// When nil, task completion events still flow via the message bus.
	notificationPublisher NotificationPublisher

	// Budget tracking for async dispatch pre-check (Issue 0039)
	budget *llm.Budget

	// CollaborationEngine for starting collaboration sessions from IntentCollaborate
	collabEngine *CollaborationEngine

	// Per-session AgentLoop manager for session-scoped dispatch.
	// When set and a session exists with a project path, direct-mode
	// requests route to a session-scoped AgentLoop instead of the
	// singleton loop. Falls back to `loop` when nil or session unknown.
	loopManager *Manager

	// SessionStoreReader for looking up session project paths.
	sessionStore SessionStoreReader

	// defaultWorkingDir is the daemon's configured default working
	// directory, used only when the session itself resolves no working
	// directory (no worktree, no project, no detection-context CWD).
	// There is deliberately NO global active-project fallback: projects
	// are scoped per session, so a session resolves its OWN binding only.
	// Empty (the default) means such a turn genuinely has no working
	// directory and filesystem tools fail with tools.ErrNoWorkingDir —
	// never with the daemon's own process CWD (AGENTS.md: Daemon CWD is
	// NOT the user's project).
	defaultWorkingDir string

	// FenceController is the per-session fence sandbox controller (optional).
	// When set, sessionLoop updates the shared FenceChecker with the
	// session's working directory (sandbox root) and no-fence override as
	// each session binds to its loop. Nil-safe per setter convention.
	fenceController FenceController

	// messageSaver persists chat messages to the session store after each
	// exchange. Wired by the daemon via SetMessageSaver.
	messageSaver SessionMessageSaver

	// budgetResumeWatcher parks turns interrupted by budget exhaustion and
	// retries them when the budget window clears.
	budgetResumeWatcher *BudgetResumeWatcher

	// quotaResumeWatcher parks turns interrupted by provider quota errors
	// and retries them when the quota window lifts (quota-reset-resilience
	// leaf 06 deferral). Nil disables quota deferral.
	quotaResumeWatcher *QuotaResumeWatcher

	// Synchronous dispatch mode: when true, async-dispatched tasks wait
	// for completion instead of returning immediately (Issue 0022).
	syncMode bool

	// effectsResumeHook is the parked-turn-resume reconcile callback
	// (effects tree leaf 02). Wired by the daemon via SetEffectsResumeHook;
	// nil disables the hook. Defined as a local func type so package agent
	// never imports the daemon.
	effectsResumeHook EffectsResumeHook

	// Worker tracking
	workers   map[string]*Worker
	workersMu sync.RWMutex

	// turnRegistry tracks async-submitted turns (async-turn-migration leaf
	// 02/03): submit-time Register, per-worker-event Touch, and Complete
	// adjacent to every turn.terminal emission. Nil (legacy path) disables
	// tracking entirely — only explicit chat.submit turns are ever tracked.
	turnRegistry *TurnRegistry

	// Shutdown
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// FenceController is the narrow interface the ChatHandler needs from the
// daemon's shared FenceChecker to configure the sandbox per-session.
type FenceController interface {
	SetRootPath(root string) error
	SetNoFence(enabled bool)
}

// SetFenceController wires the shared fence checker so session-scoped loops
// get a correct sandbox root and --nofence handling. Safe to call with nil.
func (h *ChatHandler) SetFenceController(fc FenceController) {
	if h == nil {
		return
	}
	h.fenceController = fc
}

// EffectsResumeHook is the parked-turn-resume reconcile callback (effects
// tree leaf 02). Defined here so package agent does not import the daemon
// (dependency direction: daemon -> agent, never inverted).
type EffectsResumeHook func(ctx context.Context)

// SetEffectsResumeHook wires the reconciler. Nil-guarded (setter
// convention); nil disables the hook.
func (h *ChatHandler) SetEffectsResumeHook(fn EffectsResumeHook) {
	if h == nil || fn == nil {
		return
	}
	h.effectsResumeHook = fn
}

// runEffectsResumeHook invokes the best-effort resume reconcile hook with a
// 15s bound. A panicking hook is recovered and logged: resume MUST proceed
// regardless of reconcile health (best-effort by contract).
func (h *ChatHandler) runEffectsResumeHook(ctx context.Context) {
	if h.effectsResumeHook == nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("effects resume hook panicked", "panic", rec)
		}
	}()
	h.effectsResumeHook(rctx)
}

// Worker represents an active agent processing a request.
type Worker struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SessionID      string    `json:"session_id,omitempty"` // original session ID for WS routing
	RequestID      string    `json:"request_id"`
	State          string    `json:"state"` // "processing", "executing_tool", "completed", "error"
	StartTime      time.Time `json:"start_time"`
	LastActivity   time.Time `json:"last_activity"`
	CurrentTool    string    `json:"current_tool,omitempty"`
	// TurnID is the async-submit turn identity (leaf 03). Empty on legacy
	// `chat` turns, which are never registry-tracked.
	TurnID string `json:"turn_id,omitempty"`
}

// ChatRequest is the expected payload for chat.request messages.
type ChatRequest struct {
	Message        string            `json:"message"`
	ConversationID string            `json:"conversation_id"`
	SessionID      string            `json:"session_id,omitempty"` // original session ID for persistence
	AgentID        string            `json:"agent_id,omitempty"`   // agent override from the client
	SourceClient   string            `json:"source_client,omitempty"`
	Parts          []llm.ContentPart `json:"parts,omitempty"`
	// Model optionally names the model that serves THIS turn ("provider/
	// model-id" ref or alias name). It is applied through the loop's
	// one-shot SetModelOverride seam — the SAME precedence slot as a
	// parsed user model directive — so it outranks the alias resolution
	// for exactly this turn and auto-clears after it (never persisted to
	// the config). Empty = unchanged alias/default behavior.
	Model string `json:"model,omitempty"`
	// TurnID is the caller-chosen turn identity from chat.submit (leaf 02
	// of async-turn-migration). Non-empty turns are tracked in the
	// TurnRegistry (Register/Touch/Complete); the legacy `chat` path
	// leaves it empty and stays UNTRACKED.
	TurnID string `json:"turn_id,omitempty"`
}

// ChatResponse is the payload for chat.response messages.
type ChatResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id,omitempty"` // original session ID for WS routing
	Error          string `json:"error,omitempty"`
	// Meta carries classification provenance for the reply (leaf 01 of
	// classifier-observability): which classifier served it, which model,
	// the analyzer's ambiguity score, and whether session context was used.
	// Populated only on the classified reply path (result.Intent != nil);
	// omitted entirely otherwise. Additive metadata — reply text unchanged.
	Meta map[string]string `json:"meta,omitempty"`
}

// TurnTerminalEvent is the frozen payload published on the "turn.terminal"
// bus topic when a chat turn reaches its terminal state. Field set is
// CLOSED: add nothing, remove nothing, rename nothing without updating
// every consumer (TUI, GUI, bench) and the contract in
// docs/plans/20260916-turn-lifecycle-events/master.md.
type TurnTerminalEvent struct {
	ConversationID string `json:"conversation_id"`         // required, non-empty
	SessionID      string `json:"session_id,omitempty"`    //
	TurnID         string `json:"turn_id"`                 // uuid per chat request
	TaskID         string `json:"task_id,omitempty"`       // set iff the turn dispatched a task
	IntentType     string `json:"intent_type,omitempty"`   //
	AgentID        string `json:"agent_id,omitempty"`      //
	HandlerCase    string `json:"handler_case"`            // matches dispatch_log handler_case values
	Status         string `json:"status"`                  // completed | failed | timeout | parked  (CLOSED set)
	Reply          string `json:"reply"`                   // final user-facing reply text (stub included if that is what was returned)
	DurationMS     int64  `json:"duration_ms"`             //
	ClassifiedBy   string `json:"classified_by,omitempty"` // intent.Method provenance
	Model          string `json:"model,omitempty"`         //
	Error          string `json:"error,omitempty"`         // non-empty iff Status=="failed"
}

// WSClass implements wsclass.WSClassified: turn lifecycle events render
// as agent_progress - never chat_message (blank-bubble invariant).
func (TurnTerminalEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

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

	// Start the budget resume watcher if configured.
	if h.budgetResumeWatcher != nil {
		h.budgetResumeWatcher.Start(ctx)
	}

	// Start the quota resume watcher if configured.
	if h.quotaResumeWatcher != nil {
		h.quotaResumeWatcher.Start(ctx)
	}

	// Subscribe to chat requests
	chatSub := h.bus.Subscribe(SourceChatHandler, "chat.request")

	// Subscribe to worker list requests
	workerSub := h.bus.Subscribe("worker-handler", "agent.workers.list")

	// Subscribe to task completion events for result push-back
	taskCompletedSub := h.bus.Subscribe(SourceChatHandler, "task.completed")
	taskFailedSub := h.bus.Subscribe(SourceChatHandler, "task.failed")

	// Async-turn liveness (relay-fix follow-up, bench gate 2026-09-17):
	// task.progress events fire on task/step transitions — including long
	// LLM/tool executions between step boundaries. Touch the attached
	// tracked turn so the client's liveness window and the turn watchdog
	// both see progress during multi-minute step execution. task.progress
	// carries task_id; the registry resolves task→turn.
	taskProgressSub := h.bus.Subscribe(SourceChatHandler, "task.progress")

	// Subscribe to agent progress events to keep worker state in sync with
	// the agent loop's stage transitions (thinking vs. executing tools).
	progressSub := h.bus.Subscribe(SourceChatHandler, "agent.progress")

	// Subscribe to review events to push review feedback to linked sessions
	reviewCompletedSub := h.bus.Subscribe(SourceChatHandler, "task.review_completed")

	// Subscribe to pair result events to push results back to chat sessions
	pairResultSub := h.bus.Subscribe(SourceChatHandler, TopicPairResult)

	// Subscribe to collaboration result events to push results back to chat sessions
	collabResultSub := h.bus.Subscribe(SourceChatHandler, TopicCollabResult)

	h.wg.Add(9)

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

	// Task progress handler - async-turn liveness (relay-fix follow-up):
	// every task.progress for a tracked turn's task Touches that turn, so
	// multi-minute step execution keeps the client liveness window and the
	// watchdog fed between worker lifecycle events.
	go func() {
		defer h.wg.Done()
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(taskProgressSub)
				return
			case msg, ok := <-taskProgressSub.Channel:
				if !ok {
					return
				}
				h.handleTaskProgressLiveness(msg)
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
			go h.publishWorkerEvent("chat.worker.state_changed", &snapshot)
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
	// Stop the budget resume watcher first.
	if h.budgetResumeWatcher != nil {
		h.budgetResumeWatcher.Stop()
	}

	// Stop the quota resume watcher (quota-reset-resilience leaf 06):
	// prevents a goroutine leak on shutdown and stops parked-turn resumes
	// from firing mid-shutdown. Nil-safe and idempotent.
	if h.quotaResumeWatcher != nil {
		h.quotaResumeWatcher.Stop()
	}

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

	// turn.terminal bookkeeping (async-turn-migration step 1): every
	// terminal exit path of this function emits exactly one turn.terminal
	// event through publishTurnTerminal. The early format-error returns
	// below have no conversation yet, so they emit with the request ID as
	// a synthetic conversation sentinel (consumers key on request id via
	// the existing error response).
	// turnID comes from the submit-time identity when present (chat.submit
	// sets ChatRequest.TurnID so terminal events correlate with the ack);
	// legacy `chat` turns get a fresh synthetic id. start is captured
	// before any exit path so duration_ms always spans the full turn.
	start := time.Now()

	// Parse request payload
	var req ChatRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.logger.Error("Failed to parse chat request", "error", err)
		h.sendError(msg.ID, "invalid request format: "+err.Error())
		h.publishTurnTerminal(TurnTerminalEvent{
			ConversationID: msg.ID,
			TurnID:         id.Generate("turn-"),
			HandlerCase:    "invalid_request",
			Status:         "failed",
			Reply:          "I encountered an error: invalid request format: " + err.Error(),
			DurationMS:     time.Since(start).Milliseconds(),
			Error:          "invalid request format: " + err.Error(),
		})
		return
	}

	turnID := req.TurnID
	if turnID == "" {
		turnID = id.Generate("turn-")
	}

	if req.Message == "" {
		h.sendError(msg.ID, "message is required")
		h.publishTurnTerminal(TurnTerminalEvent{
			ConversationID: req.ConversationID,
			TurnID:         turnID,
			HandlerCase:    "empty_message",
			Status:         "failed",
			Reply:          "I encountered an error: message is required",
			DurationMS:     time.Since(start).Milliseconds(),
			Error:          "message is required",
		})
		return
	}

	// Generate conversation ID if not provided
	conversationID := req.ConversationID
	if conversationID == "" {
		conversationID = generateConversationID()
	}

	// Async-turn tracking (leaf 03): register the submit-time turn under
	// its explicit turn id. Only chat.submit turns carry TurnID — the
	// legacy `chat` path has it empty and stays untracked. Registration is
	// idempotent (a duplicate chat.request for the same turn_id is the
	// submit handler's dedupe problem; here a re-registered turn simply
	// keeps its original record).
	if req.TurnID != "" && h.turnRegistry != nil {
		h.turnRegistry.Register(req.TurnID, conversationID)
	}

	// Broadcast chat.message.received for bilateral visibility.
	// All session participants see who sent what.
	if req.SourceClient != "" {
		// Use the original session ID for WS routing; fall back to
		// conversationID when no session ID is available (legacy clients).
		wsSessionID := req.SessionID
		if wsSessionID == "" {
			wsSessionID = conversationID
		}
		broadcastPayload, _ := json.Marshal(map[string]string{
			"session_id":    wsSessionID,
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
		h.bus.PublishExternalOnly("chat.message.received", broadcastMsg)
	}

	// Create worker to track this request
	workerID := generateWorkerID()
	worker := &Worker{
		ID:             workerID,
		ConversationID: conversationID,
		SessionID:      req.SessionID, // original session ID for WS routing
		RequestID:      msg.ID,
		State:          "processing",
		StartTime:      time.Now(),
		LastActivity:   time.Now(),
		TurnID:         req.TurnID, // async-submit identity; "" on legacy turns
	}
	h.registerWorker(worker)
	defer h.unregisterWorker(workerID)

	// Publish worker started event
	h.publishWorkerEvent("chat.worker.started", worker)

	// Publish chat.processing event immediately so SSE clients get
	// instant feedback that the request was received and is being handled.
	processingPayload, _ := json.Marshal(map[string]any{
		"request_id":      msg.ID,
		"conversation_id": conversationID,
		"status":          "processing",
		"worker_id":       workerID,
	})
	processingMsg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     "chat.processing",
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   processingPayload,
		ReplyTo:   msg.ID,
	}
	h.bus.Publish("chat.processing", processingMsg)

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
	// handlerCase tracks which switch case was selected for audit logging.
	// Declared at function scope so the turn.terminal funnel (below) emits
	// the same vocabulary the dispatch_log row records.
	handlerCase := "direct_mode"
	// recordOnTaskPath is set when the reply was produced by the task path
	// (route_to_agent or sync_dispatch). Only those paths need the exchange
	// mirrored into the session conversation: the direct path already
	// records both sides via RunOnce's AddUserMessage/AddAssistantMessage,
	// so recording here too would double-append.
	recordOnTaskPath := false

	// turn.terminal accumulators (async-turn-migration step 1): the sync
	// ceiling flag and the terminal event emitted by the single funnel at
	// the bottom of this function. syncTaskID carries the dispatched task
	// for the event payload on task-path turns.
	syncTurnTimeout := false
	syncTaskID := ""
	syncIntentType := ""
	syncAgentID := ""
	syncClassifiedBy := ""
	syncModel := ""

	if h.dispatcher != nil {
		// Multi-agent mode: classify and route through dispatcher.
		// The per-request model (req.Model) rides along so the routed
		// specialist's loop serves this turn on the requested model
		// (same precedence slot as a parsed user directive).
		var dispatchErr error
		result, dispatchErr = h.dispatcher.ClassifyAndRoute(ctx, req.Message, conversationID, req.Parts, req.AgentID, req.Model)

		// handlerCase is declared at function scope above; re-sync the
		// direct-mode default for this dispatch attempt.
		handlerCase = "direct_mode"
		inputSummary := extractSummary(req.Message)
		hasParts := len(req.Parts) > 0

		switch {
		case result != nil && result.ClarificationReply != "":
			handlerCase = "clarification"
			h.logger.Info("Returning clarification reply",
				"conversation", conversationID,
			)
			reply = result.ClarificationReply
		case result != nil && result.Response != "" && (result.Intent == nil || !h.dispatcher.ShouldDispatchAsync(result)):
			handlerCase = "direct_response"
			h.logger.Debug("Returning direct dispatch response",
				"agent", result.AgentID,
			)
			reply = result.Response
		case dispatchErr != nil:
			handlerCase = "dispatch_error"
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
			handlerCase = "pair"
			// Pair-channel mode is a text-only specialist workflow. If the
			// user attached multimodal parts, log that they are being dropped
			// rather than silently swallowing the content.
			if len(req.Parts) > 0 {
				h.logger.Warn("Dropping multimodal parts for pair route",
					"conversation", conversationID,
					"parts_count", len(req.Parts),
				)
			}
			// Route to pair-channel mode for dual-agent conversation
			h.logger.Info("Routing to pair-channel mode",
				"session", conversationID,
				"actor", result.AgentID,
			)
			reply = h.startPairSession(result, conversationID)
		case h.dispatcher.ShouldRouteToCollaborate(result):
			handlerCase = "collaborate"
			// Collaboration engine is also a text-only specialist workflow.
			if len(req.Parts) > 0 {
				h.logger.Warn("Dropping multimodal parts for collaboration route",
					"conversation", conversationID,
					"parts_count", len(req.Parts),
				)
			}
			// Route to collaboration engine for multi-agent collaboration
			h.logger.Info("Routing to collaboration engine",
				"session", conversationID,
				"agent", result.AgentID,
			)
			reply, err = h.startCollaborationSession(ctx, result, conversationID)
		case h.dispatcher.ShouldDispatchAsync(result) && result.Task != nil:
			handlerCase = "async_dispatch"
			// LEGACY (async-turn-migration leaf 07): headless benchmark
			// clients (source_client prefix "meept-bench") used to be
			// force-switched into synchronous dispatch so the chat RPC
			// reply carried the final result. The bench now speaks
			// submit-ack (leaf 02), so the special-case survives only
			// under the legacy opt-in: sync_chat_enabled=true. Under the
			// default config the wait path is unreachable for every
			// client, bench included — the ack is the reply and the
			// result arrives via turn.terminal.
			if h.syncMode && strings.HasPrefix(req.SourceClient, "meept-bench") {
				h.syncMode = true
			}
			// Async dispatch goes through the orchestrator pipeline, which is
			// currently text-only. Warn if parts are being dropped.
			if len(req.Parts) > 0 {
				h.logger.Warn("Dropping multimodal parts for async dispatch",
					"conversation", conversationID,
					"parts_count", len(req.Parts),
				)
			}
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
					handlerCase = "budget_blocked"
					break
				}
			}

			// turn.terminal liveness (week bughunt 2026-09-17 F1): record
			// the orchestrator task on the tracked turn BEFORE the
			// sync/async branch — both paths dispatch the same task and the
			// task-end relay needs the task→turn edge to re-broadcast the
			// real result under the SUBMITTED turn id. Pre-fix both call
			// sites passed the arguments swapped/wrong (async: task id as
			// the turn key; sync: an empty turn id), so turn-scoped clients
			// never received the task result. No-op for legacy untracked
			// turns (req.TurnID == "").
			h.attachTask(req.TurnID, result.Task.ID)

			if h.syncMode {
				handlerCase = "sync_dispatch"
				// Issue 0022: synchronous mode -- wait for task completion
				h.logger.Info("Sync dispatch: publishing plan request and waiting for completion",
					"task_id", result.Task.ID,
					"agent", result.AgentID,
					"intent", result.Intent.Type,
				)
				h.publishPlanRequest(result, conversationID)
				reply = h.waitForTaskCompletion(ctx, result.Task.ID)
				recordOnTaskPath = true
				// turn.terminal ceiling detection: the stub reply is NOT
				// sniffed — the wait hit the ceiling (or the task failed)
				// when the task is still non-terminal in the store after
				// the wait. That maps to Status "timeout" (StateFailed is
				// reported through the task.failed relay instead).
				if h.taskStore != nil {
					if tk, tkErr := h.taskStore.GetByID(result.Task.ID); tkErr == nil && tk != nil && !tk.State.IsTerminal() {
						syncTurnTimeout = true
					}
				}
				syncTaskID = result.Task.ID
				if result.Intent != nil {
					syncIntentType = result.Intent.Type
					syncClassifiedBy = result.Intent.Method
					syncModel = result.Intent.Model
				}
				syncAgentID = result.AgentID
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

				// turn.terminal provenance for the ack event (the ack IS
				// the RPC turn's terminal state; the real result arrives
				// later via the task_completed_relay event).
				syncTaskID = result.Task.ID
				if result.Intent != nil {
					syncIntentType = result.Intent.Type
					syncClassifiedBy = result.Intent.Method
					syncModel = result.Intent.Model
				}
				syncAgentID = result.AgentID

				// Publish plan request to orchestrator
				h.publishPlanRequest(result, conversationID)
			}
		default:
			handlerCase = "route_to_agent"
			if result == nil || result.Intent == nil {
				handlerCase = "nil_result_error"
				err = fmt.Errorf("dispatch returned no actionable result")
				h.logger.Error("Dispatch returned nil result or intent",
					"result_nil", result == nil,
				)
			} else {
				h.logger.Debug("Dispatched to agent",
					"agent", result.AgentID,
					"intent", result.Intent.Type,
					"confidence", result.Intent.Confidence,
				)
				reply, err = h.dispatcher.RouteToAgent(ctx, result, conversationID)
				recordOnTaskPath = true
			}
		}

		// Record the routing decision for debugging and persistent audit trail.
		h.dispatcher.RecordDispatch(conversationID, handlerCase, inputSummary, result, hasParts, dispatchErr)
	} else {
		// Direct mode: prefer a session-scoped AgentLoop when a manager
		// is wired and the session has a project path. This isolates
		// per-session execution contexts (working directory, project).
		// Falls back to the singleton loop when the manager is unset,
		// the session is unknown, or GetOrCreateWired fails.
		loop := h.sessionLoop(conversationID)
		h.applyRequestModel(loop, req.Model, conversationID)
		reply, err = loop.RunOnceWithParts(ctx, req.Message, req.Parts, conversationID)
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
		h.publishWorkerEvent("chat.worker.error", worker)
	} else {
		worker.State = "completed"
		h.publishWorkerEvent("chat.worker.completed", worker)
	}

	// Build response
	response := ChatResponse{
		ConversationID: conversationID,
		SessionID:      req.SessionID, // original session ID for WS routing
	}

	// Resolve the persistence/session ID up front: prefer the original session
	// ID from the client, fall back to the resolved conversation ID. Used both
	// for message persistence and for parking budget-interrupted turns.
	persistID := req.SessionID
	if persistID == "" {
		persistID = conversationID
	}
	// Scopes-2 audit HIGH (deleg_9fb259e7, 2026-09-18): GUI chat.submit
	// requests carry no session_id (sdk_client sends only
	// conversation_id). Gating the WS chat_message push on
	// req.SessionID meant the daemon never published the reply bubble
	// for GUI-submitted direct replies — persisted but never rendered.
	// Route on the resolved persistence ID instead.
	response.SessionID = persistID

	if err != nil {
		// Check for BudgetExceededError to provide user-friendly message
		var budgetErr *llm.BudgetExceededError
		if errors.As(err, &budgetErr) {
			response.Error = budgetErr.UserMessage()
			response.Reply = budgetErr.UserMessage()
			// If this is a time-windowed limit that will clear on its own,
			// park the turn for automatic retry and tell the user.
			if h.budgetResumeWatcher != nil && isTimeWindowedBudget(budgetErr.Reason) {
				if h.budgetResumeWatcher.Park(ParkedTurn{
					SessionID:      persistID,
					ConversationID: conversationID,
					Message:        req.Message,
					Parts:          req.Parts,
					AgentID:        req.AgentID,
					SourceClient:   req.SourceClient,
					TurnID:         turnID, // F15: the resume emits the final terminal event under this id
				}) {
					response.Reply = budgetErr.UserMessage() +
						"\n\nYour message has been queued and will be sent automatically when the budget window resets."
					response.Error = ""
				}
			}
			// Quota deferral (quota-reset-resilience leaf 06): park
			// quota-interrupted turns for automatic retry at the reset time.
		} else if quotaErr, ok := llm.AsQuotaResetError(err); ok {
			unblockAt := quotaErr.ResetAt
			if unblockAt.IsZero() && quotaErr.RetryAfter > 0 {
				unblockAt = time.Now().Add(quotaErr.RetryAfter)
			}
			response.Error = quotaErr.UserMessage()
			response.Reply = quotaErr.UserMessage()
			if h.quotaResumeWatcher != nil && !unblockAt.IsZero() {
				if h.quotaResumeWatcher.Park(QuotaParkedTurn{
					SessionID:      persistID,
					ConversationID: conversationID,
					Message:        req.Message,
					Parts:          req.Parts,
					AgentID:        req.AgentID,
					SourceClient:   req.SourceClient,
					ProviderID:     quotaErr.ProviderID,
					UnblockAt:      unblockAt,
					TurnID:         turnID, // F15: the resume emits the final terminal event under this id
				}) {
					response.Reply = quotaErr.UserMessage() +
						"\n\nyour message is queued and will run automatically when the quota resets."
					response.Error = ""
				}
			}
		} else {
			response.Error = err.Error()
			response.Reply = reply // AgentLoop returns a user-friendly message even on error
		}
	} else {
		response.Reply = reply
	}
	// Single reply choke point (e2e run 5, 2026-09-11 A2/t4): the platform
	// introspection fast path (Dispatcher.RouteToAgent →
	// handlePlatformIntrospection) returns the agent-roster catalog without
	// an LLM turn, so the RunOnce applyReplyGuard seam never ran and the
	// roster shipped to the user. Guard here — the LAST writer before
	// persistence/push — so every reply path (direct, routed, sync-wait,
	// platform fast path) is covered. sanitizeCatalogReply passes genuine
	// prose through unchanged.
	response.Reply = applyReplyGuardLogged(response.Reply, h.logger, replyGuardContext{
		Agent:          replyGuardAgent(req.AgentID, result),
		Intent:         replyGuardIntent(result),
		SessionID:      req.SessionID,
		ConversationID: conversationID,
	})

	// Classification provenance (leaf 01 of classifier-observability):
	// attach metadata describing how this reply was classified — method,
	// serving model, ambiguity, session-digest usage. Only on the
	// classified path (result.Intent != nil); direct-mode replies carry no
	// Meta at all (additive metadata, reply text unchanged).
	if result != nil && result.Intent != nil {
		response.Meta = provenanceMeta(result.Intent)
	}

	// Persist the user message and assistant reply so HTTP-only clients
	// (Flutter GUI) can reload conversation history after switching sessions.
	// Resolve the effective agent ID for message attribution: prefer the
	// user-specified override, fall back to the dispatcher's decision.
	effectiveAgentID := req.AgentID
	if effectiveAgentID == "" && result != nil {
		effectiveAgentID = result.AgentID
	}
	// Session-continuity (task-turn session record): mirror the exchange into
	// the session conversation so follow-up turns share task context. The
	// recorder is best-effort and must never break the reply path.
	if recordOnTaskPath {
		h.recordExchangeInSessionConv(conversationID, req.Message, response.Reply)
	}
	h.persistExchange(persistID, req.Message, req.Parts, response.Reply, effectiveAgentID)

	// Send response
	h.sendResponse(msg.ID, response)

	// turn.terminal funnel (async-turn-migration step 1): exactly one
	// terminal event per turn, emitted here with the reply this path
	// produced. Status is "timeout" when the sync wait hit its ceiling
	// with the task still non-terminal, "failed" on error paths, and
	// "completed" otherwise (including the async ack, which IS this RPC
	// turn's terminal state). Parked turns (budget/quota deferral) keep
	// the error-free reply and report "parked".
	turnStatus := "completed"
	turnError := ""
	turnReply := response.Reply
	if err != nil {
		if response.Error == "" && response.Reply != "" {
			// Parked turn: the error was deferred, the reply carries the
			// queue notice — the RPC turn ended in the parked state.
			turnStatus = "parked"
		} else {
			turnStatus = "failed"
			turnError = response.Error
			if turnError == "" {
				turnError = err.Error()
			}
			if turnReply == "" {
				// The user-facing reply documents the failure even when
				// the error-only response left it empty.
				turnReply = "I encountered an error: " + turnError
			}
		}
	} else if syncTurnTimeout {
		turnStatus = "timeout"
	} else if handlerCase == "async_dispatch" && syncTaskID != "" {
		// Async-ack for a dispatched task (async-turn-migration relay
		// fix, bench gate 2026-09-16): the ack is NOT the work's terminal
		// state — it means "accepted, work continuing elsewhere".
		// Reporting "completed" here made awaiting clients take the ack
		// text as the final result (the bench graded the ack as a fail).
		// "parked" is the closed vocabulary's accepted-not-finished
		// state; clients skip it and keep waiting for the
		// task_completed_relay event, which now carries this turn's id.
		turnStatus = "parked"
	}
	h.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: conversationID,
		SessionID:      req.SessionID,
		TurnID:         turnID,
		TaskID:         syncTaskID,
		IntentType:     syncIntentType,
		AgentID:        syncAgentID,
		HandlerCase:    handlerCase,
		Status:         turnStatus,
		Reply:          turnReply,
		DurationMS:     time.Since(start).Milliseconds(),
		ClassifiedBy:   syncClassifiedBy,
		Model:          syncModel,
		Error:          turnError,
	})

	// Async-turn completion (leaf 03): the submitted turn leaves the
	// registry when its turn.terminal event fires — EXCEPT when a task is
	// still pending on it (relay fix, bench gate 2026-09-16): the
	// task_completed_relay needs TurnIDForTask to re-broadcast the real
	// result under the SAME turn id, so the registry entry (the task→turn
	// edge) must survive until the relay fires. The relay emits under the
	// recovered id and the reaper bounds any orphan. No-op for untracked
	// (legacy) turns.
	if req.TurnID != "" && syncTaskID == "" {
		h.completeTurn(req.TurnID)
	}
}

// recordExchangeInSessionConv best-effort mirrors a completed task-path
// exchange (route_to_agent / sync_dispatch) into the SESSION conversation so
// the next turn's model context includes what was just done. The task path
// normally records only into the task-scoped step conversation; without this
// mirror the session conversation never sees the exchange.
//
// Best-effort by contract: every step is nil-safe, failures are logged at
// Warn level, and no error is ever propagated to the reply path. The
// assistant entry is appended only when the reply is non-empty. Passing the
// same (conversationID, userMsg, reply) pair twice is a no-op (last-user
// entry guard), and the call site runs at most once per exchange — the
// direct path is never recorded here (RunOnce already records it, and
// double-appending would corrupt context).
func (h *ChatHandler) recordExchangeInSessionConv(conversationID, userMsg, reply string) {
	if h == nil || conversationID == "" {
		return
	}
	loop := h.sessionLoop(conversationID)
	if loop == nil {
		h.logger.Warn("session exchange record skipped: no loop",
			"conversation", conversationID)
		return
	}
	conv := loop.SessionConversation(conversationID)
	if conv == nil {
		h.logger.Warn("session exchange record skipped: no conversation",
			"conversation", conversationID)
		return
	}
	// Guard against a duplicate append for the same exchange: if the last
	// user entry already carries this exact request, the exchange was
	// recorded once already.
	if last := conv.LastUserMessage(); last == userMsg {
		return
	}
	conv.AddUserMessage(userMsg)
	if reply != "" {
		conv.AddAssistantMessage(reply)
	}
}

// persistExchange saves the user message and assistant reply to the session
// store so HTTP-only clients (Flutter GUI) can reload conversation history.
// Errors are logged but never propagate — persistence failure must not break
// the chat response path.
func (h *ChatHandler) persistExchange(sessionID, userMsg string, parts []llm.ContentPart, reply string, agentID string) {
	if h.messageSaver == nil || sessionID == "" {
		return
	}

	now := time.Now().UTC()
	var msgs []session.Message

	// User message
	userEntry := session.Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   userMsg,
		Timestamp: now,
		EntryType: "message",
		BranchID:  "main",
	}
	if len(parts) > 0 {
		userEntry.Parts = parts
	}
	msgs = append(msgs, userEntry)

	// Assistant reply (skip empty replies, e.g. async-dispatch acks)
	if reply != "" {
		msgs = append(msgs, session.Message{
			SessionID: sessionID,
			Role:      "assistant",
			Content:   reply,
			Timestamp: now,
			EntryType: "message",
			BranchID:  "main",
			AgentID:   agentID,
		})
	}

	if err := h.messageSaver.SaveMessages(sessionID, msgs); err != nil {
		h.logger.Warn("failed to persist chat exchange",
			"session_id", sessionID,
			"message_count", len(msgs),
			"error", err,
		)
	}
}

// publishPlanRequest sends a plan request to the orchestrator via the bus.
func (h *ChatHandler) publishPlanRequest(result *DispatchResult, sessionID string) {
	req := PlanRequest{
		TaskID:           result.Task.ID,
		SessionID:        sessionID,
		Input:            result.Task.Description,
		Intent:           result.Intent.Type,
		Mode:             result.SuggestedMode,
		TrueAnalysis:     result.Intent.TrueAnalysis,
		ExecutorModelRef: result.ExecutorModelRef,
		// Client agent override: synthesized steps must dispatch to the
		// requested specialist, not re-picked hint-table agents.
		AssignedAgent: result.Task.AssignedAgent,
		// Per-request model (F18): the ref the client's chat.request
		// carried must reach specialist execution even on async task
		// dispatch, not just the interactive RouteToAgent path.
		RequestModel: result.RequestModel,
	}

	// Session execution context (quickplan-mode leaf 02 / master Contract
	// 6): quick_plan dispatches carry the session's active plan, open
	// tracked tasks, and prior quickplan waves so the orchestrator can
	// make the quickplan-vs-code/git call at execution time. Composed via
	// BuildPlanSessionContext, which ALSO appends the session digest block
	// (prior task state + best result) — e2e run 7 (2026-09-11) T3: the
	// fallback-heuristic quickplan route planned "Ask user for the file
	// path…" for "did the change get made? where is the file?" because the
	// planner saw open-task TITLES but never T1's completed result. The
	// digest block gives the planner the prior artifact so the plan answers
	// from evidence instead of interrogating the user. The current turn's
	// own placeholder task is excluded so the digest describes prior work.
	//
	// Run-8 follow-up (2026-09-11): the SAME question also classifies as
	// git @0.9 on a healthy classifier run. git dispatches async through
	// the same orchestrator pipeline but in "direct" mode, whose
	// createFallbackSteps uses req.Input verbatim — no planner LLM — so the
	// quick_plan-gated injection never ran and the committer executed the
	// bare question with no session context ("I need more information …
	// file path / commit hash / directory structure"). The digest is now
	// attached for EVERY plan request, not just quick_plan: it is bounded,
	// evidence-shaped, and the one session-facts channel that reaches
	// step-job prompts. The quick_plan branch keeps its extra
	// execution-context block; other modes get the digest alone.
	if h.dispatcher != nil {
		if req.Mode == "quick_plan" {
			req.SessionContext = h.dispatcher.BuildPlanSessionContext(context.Background(), sessionID, result.Task.ID)
		} else {
			req.SessionContext = h.dispatcher.PlanDigestContext(sessionID, result.Task.ID)
		}
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
// provenanceMeta builds the classification-provenance metadata attached to
// classified replies (leaf 01 of classifier-observability). Keys:
//   - classification_method: which classifier branch produced the intent
//   - classification_model:  "provider/model" that served an LLM-served
//     classification ("" for deterministic branches — honest provenance)
//   - ambiguity: analyzer ambiguity score, %.2f — only when the analyzer ran
//   - session_digest_used: "true" — only when the analyzer ran (a non-nil
//     TrueAnalysis implies the session digest was part of its context)
//
// intent must be non-nil. Returns nil for analyzer-free classifications'
// optional keys when they don't apply, so the JSON `meta` object stays
// minimal; the method key is always present.
func provenanceMeta(intent *Intent) map[string]string {
	meta := map[string]string{
		"classification_method": intent.Method,
		"classification_model":  intent.Model,
	}
	if intent.TrueAnalysis != nil {
		meta["ambiguity"] = fmt.Sprintf("%.2f", intent.TrueAnalysis.Ambiguity)
		meta["session_digest_used"] = "true"
	}
	return meta
}

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

	// Publish a separate chat_message event for WebSocket push notification.
	// This decouples the RPC reply (chat.response, consumed by ChatService
	// for the HTTP body) from the WS event stream. Without this, WS clients
	// would need to relay chat.response — which duplicates the reply for
	// HTTP+WS clients like the Flutter GUI.
	//
	// Skipped when the reply is empty (e.g. async-dispatch acks that carry
	// no user-visible content) or when this is an error-only response with
	// no reply text.
	if response.Reply != "" && response.SessionID != "" {
		h.publishChatMessage(response, replyTo)
	}
}

// publishChatMessage pushes an assistant reply to WS-connected clients via
// the dedicated chat_message bus topic. This is the single source of truth
// for assistant messages on the WS event stream — chat.response is NOT
// relayed to WS clients (it is RPC-only).
func (h *ChatHandler) publishChatMessage(response ChatResponse, replyTo string) {
	payload, err := json.Marshal(map[string]any{
		"role":            "assistant",
		"content":         response.Reply,
		"session_id":      response.SessionID,
		"conversation_id": response.ConversationID,
		"error":           response.Error,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		h.logger.Error("Failed to marshal chat_message payload", "error", err)
		return
	}

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     "chat_message",
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.PublishExternalOnly("chat_message", msg)
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

	// Async-turn liveness (leaf 03): every worker lifecycle event Touch()es
	// the submit-time turn so the reaper sees progress. w.TurnID carries
	// the chat.submit identity; legacy turns leave it empty and this is a
	// no-op (they are never tracked).
	h.touchTurn(w.TurnID)

	msg := &models.BusMessage{
		ID:        generateMessageID(),
		Type:      models.MessageTypeEvent,
		Topic:     topic,
		Source:    SourceChatHandler,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	h.bus.PublishExternalOnly(topic, msg)
}

// publishTurnTerminal is the single emission point for the turn.terminal
// topic (async-turn-migration step 1). Payload contract is frozen — see
// docs/plans/20260916-turn-lifecycle-events/master.md Interface Contracts.
func (h *ChatHandler) publishTurnTerminal(ev TurnTerminalEvent) {
	if h == nil || h.bus == nil {
		return
	}
	// Pre-marshal to preserve the log-and-drop contract (typed-bus-topics
	// leaf 02): bus.PublishT panics on marshal failure, but a marshal
	// failure here must be logged and never affect the turn (same posture
	// as parked_turn.go's publishParkEvent). TurnTerminalEvent is a
	// closed, marshal-safe field set, so the error path is unreachable in
	// practice and PublishT's panic branch is dead for this payload.
	if _, err := json.Marshal(ev); err != nil {
		h.logger.Error("failed to build turn.terminal event", "error", err)
		return
	}
	bus.PublishBlockingT(h.bus, TopicTurnTerminal, SourceChatHandler, ev)
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
		// Status is the producer's honest completion verdict
		// ("completed" | "failed"; tactical.go publishes "failed" for
		// failed steps or validation exhaustion). Empty on legacy
		// payloads = "completed".
		Status string `json:"status,omitempty"`
		Error  string `json:"error,omitempty"`
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
		"status", payload.Status,
	)

	// Honest relay status (week bughunt 2026-09-17 F2): the producer's
	// status decides how the turn.terminal relay and the notifications are
	// shaped. A failed payload relayed as "completed" made clients grade
	// failed work as success.
	failed := payload.Status == "failed"
	status := "completed"
	if failed {
		status = "failed"
	}

	// Build human-readable completion message
	// Use event-provided steps, falling back to step store
	steps := payload.Steps
	if len(steps) == 0 {
		steps = h.fetchStepSummaries(payload.TaskID)
	}
	reply := h.formatTaskCompletedMessage(payload.Name, steps, payload.ExecutionTime, payload.Result, payload.CompletedJobs, payload.TotalJobs, payload.TokenUsage)

	// Publish notification (Plan 4.3) — severity follows the honest status.
	if failed {
		h.publishNotification("error", "Task Failed",
			fmt.Sprintf("%s failed: %s (%d/%d steps completed)", payload.Name, payload.Result, payload.CompletedJobs, payload.TotalJobs))
	} else {
		h.publishNotification("success", "Task Complete",
			fmt.Sprintf("%s completed (%d/%d steps)", payload.Name, payload.CompletedJobs, payload.TotalJobs))
	}

	// Broadcast session-scoped notifications to linked sessions
	for _, sessionID := range payload.LinkedSessions {
		if h.notificationPublisher != nil {
			severity := "success"
			title := "Task Complete"
			summary := fmt.Sprintf("%s completed (%d/%d steps)", payload.Name, payload.CompletedJobs, payload.TotalJobs)
			if failed {
				severity = "error"
				title = "Task Failed"
				summary = fmt.Sprintf("%s failed: %s (%d/%d steps completed)", payload.Name, payload.Result, payload.CompletedJobs, payload.TotalJobs)
			}
			h.notificationPublisher.PublishSessionNotification(
				sessionID, "task-orchestrator", severity,
				title,
				summary,
			)
		}
	}

	// A failed payload's reply carries the failure reason, not a success
	// summary — the user's reply text must read as a failure too.
	relayReply := reply
	relayError := ""
	if failed {
		relayError = payload.Result
		if relayError == "" {
			relayError = payload.Error
		}
		relayReply = "the task failed: " + relayError
	}

	response := ChatResponse{
		Reply: relayReply,
	}
	if failed {
		response.Error = relayError
	}

	// Send to all linked sessions
	for _, sessionID := range payload.LinkedSessions {
		response.ConversationID = sessionID
		response.SessionID = sessionID // for WS push routing
		h.sendResponse("task-completed-"+payload.TaskID, response)
	}

	// If no linked sessions, broadcast on task.result topic
	if len(payload.LinkedSessions) == 0 {
		h.sendResponse("task-completed-"+payload.TaskID, response)
	}

	// turn.terminal relay (async-turn-migration step 1): the originating
	// RPC turn may have already returned (async ack or sync timeout), so
	// the real result is re-broadcast as a structured terminal event.
	// Session→conversation correlation reuses the live worker map; when no
	// worker matches, the task_id sentinel keys the event for consumers.
	h.publishTaskTerminalRelay(payload.TaskID, payload.LinkedSessions,
		"task_completed_relay", status, relayReply, relayError)
}

// publishTaskTerminalRelay emits the turn.terminal event for a task-end
// relay (task.completed / task.failed). The first linked session that maps
// to a live worker's conversation wins; otherwise the first linked session
// ID is used as the conversation; with no linked sessions at all the
// "task:<taskID>" sentinel keys the event for consumers (Task 3 contract).
func (h *ChatHandler) publishTaskTerminalRelay(taskID string, linkedSessions []string, handlerCase, status, reply, errMsg string) {
	conversationID := ""
	sessionID := ""
	for _, sID := range linkedSessions {
		if sID == "" {
			continue
		}
		if sessionID == "" {
			sessionID = sID
		}
		if conv := h.workerConversationForSession(sID); conv != "" {
			conversationID = conv
			sessionID = sID
			break
		}
	}
	if conversationID == "" {
		if sessionID != "" {
			conversationID = sessionID
		} else {
			conversationID = "task:" + taskID
		}
	}
	// Originating turn id (async-turn-migration fix, bench gate 2026-09-16):
	// clients filter turn.terminal by turn_id — the id their chat.submit ack
	// returned. A fresh id here would make the real result invisible to the
	// awaiting client. The registry's task→turn correlation (AttachTask at
	// dispatch) recovers it; unknown → fresh id (headless/legacy, no
	// turn-scoped awaiter exists).
	turnID := h.turnIDForTask(taskID)
	if turnID == "" {
		turnID = id.Generate("turn-")
	}
	h.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: conversationID,
		SessionID:      sessionID,
		TurnID:         turnID,
		TaskID:         taskID,
		HandlerCase:    handlerCase,
		Status:         status,
		Reply:          reply,
		Error:          errMsg,
	})
	// The task→turn registry edge has served its purpose (the relay
	// recovered the originating id): drop the entry so the watchdog does
	// not later reap an already-answered turn. Untracked turns no-op.
	h.completeTurn(turnID)
}

// workerConversationForSession looks up the conversation a live worker bound
// to the given session (best-effort correlation; "" when unmapped).
func (h *ChatHandler) workerConversationForSession(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	h.workersMu.RLock()
	defer h.workersMu.RUnlock()
	for _, w := range h.workers {
		if w != nil && (w.SessionID == sessionID || w.ConversationID == sessionID) {
			return w.ConversationID
		}
	}
	return ""
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

	// Publish failure notification (Plan 4.3)
	h.publishNotification("error", "Task Failed",
		fmt.Sprintf("%s failed: %s (%d/%d steps completed)", payload.Name, payload.Error, payload.CompletedJobs, payload.TotalJobs))

	// Broadcast session-scoped notifications to linked sessions
	for _, sessionID := range payload.LinkedSessions {
		if h.notificationPublisher != nil {
			h.notificationPublisher.PublishSessionNotification(
				sessionID, "task-orchestrator", "error",
				"Task Failed",
				fmt.Sprintf("%s failed: %s (%d/%d steps completed)", payload.Name, payload.Error, payload.CompletedJobs, payload.TotalJobs),
			)
		}
	}

	response := ChatResponse{
		Reply: reply,
		Error: payload.Error,
	}

	// Send to all linked sessions
	for _, sessionID := range payload.LinkedSessions {
		response.ConversationID = sessionID
		response.SessionID = sessionID // for WS push routing
		h.sendResponse("task-failed-"+payload.TaskID, response)
	}

	// If no linked sessions, broadcast
	if len(payload.LinkedSessions) == 0 {
		h.sendResponse("task-failed-"+payload.TaskID, response)
	}

	// turn.terminal relay (async-turn-migration step 1): the failure
	// mirrors the completed-task relay — same correlation, Status
	// "failed" with the error text.
	h.publishTaskTerminalRelay(payload.TaskID, payload.LinkedSessions,
		"task_failed_relay", "failed", reply, payload.Error)
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

// modeToLabel translates a SuggestedMode value into a human-readable label
// for display in the async task ACK. The labels are lowercase per the
// project UI convention (see CLAUDE.md "UI Conventions").
func modeToLabel(mode string) string {
	switch mode {
	case "direct":
		return "executing directly"
	case "plan":
		return "planned"
	case "quick_plan":
		return "quick plan"
	case "spec_plan":
		return "spec-planned (multi-phase)"
	case "spec_pair":
		return "pair session"
	default:
		return "planned"
	}
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
	fmt.Fprintf(&sb, "**mode:** %s\n", modeToLabel(result.SuggestedMode))

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

// bestStepResult picks the user-facing reply from executed steps: the
// Result of the highest-sequence completed/approved step that has
// non-empty text; falls back to any non-empty Result.
func bestStepResult(steps []*task.TaskStep) string {
	// Strip-then-select: a step's raw result can be ONLY the machine-facing
	// claims/evidence envelope (loop.go evidenceSection). Such a result is
	// useless as a user-facing reply even when it is the newest step, so
	// strip every candidate first and prefer the highest-sequence step that
	// still carries prose. Fall back to the raw result only when EVERY
	// candidate was envelope-only (better than an empty reply).
	best := ""
	bestSeq := -1
	fallback := ""
	fallbackSeq := -1
	for _, s := range steps {
		if s == nil || s.Result == "" {
			continue
		}
		done := s.State == task.StepCompleted || s.State == task.StepApproved
		// StripClaimsEvidenceOrProse: an envelope-ONLY result still carries
		// the answer inside its evidence strings — recover that prose
		// instead of discarding the step (2026-09-25 run NKZiEl).
		stripped := StripClaimsEvidenceOrProse(s.Result)
		if done {
			if stripped != "" && s.Sequence > bestSeq {
				best, bestSeq = stripped, s.Sequence
			}
		}
		if fallback == "" || s.Sequence > fallbackSeq {
			fallback = s.Result
			fallbackSeq = s.Sequence
		}
	}
	if best != "" {
		return best
	}
	// Fallback: any non-empty result (e.g. approved-state variants).
	return fallback
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

// SetTurnRegistry wires the async-turn registry (async-turn-migration leaf
// 03). Nil-guarded INCLUDING typed-nil per the project invariant (`if tr !=
// nil` does NOT survive a typed-nil *TurnRegistry in an any-typed check —
// the explicit nil test here handles both because the parameter type is the
// concrete pointer). A nil registry disables turn tracking: the legacy
// `chat` path (empty ChatRequest.TurnID) is untracked regardless.
func (h *ChatHandler) SetTurnRegistry(registry *TurnRegistry) {
	if h == nil || registry == nil {
		return
	}
	h.turnRegistry = registry
}

// handleTaskProgressLiveness feeds task.progress events into the turn
// registry (async-turn liveness, relay-fix follow-up): a task.progress
// carrying task_id Touches the turn that dispatched it, so multi-minute
// step execution — where no worker lifecycle event fires — still counts as
// progress for the client liveness window and the turn watchdog.
func (h *ChatHandler) handleTaskProgressLiveness(msg *models.BusMessage) {
	if h == nil || h.turnRegistry == nil || msg == nil {
		return
	}
	var payload struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.TaskID == "" {
		return
	}
	if turnID := h.turnIDForTask(payload.TaskID); turnID != "" {
		h.touchTurn(turnID)
	}
}

// touchTurn registers-or-progresses a tracked turn. Cheap (mutex + map
// write); fires per worker lifecycle event. turnID "" (legacy path) is a
// no-op.
func (h *ChatHandler) touchTurn(turnID string) {
	if h == nil || turnID == "" || h.turnRegistry == nil {
		return
	}
	if existing := h.turnRegistry.Register(turnID, ""); existing {
		h.turnRegistry.Touch(turnID)
	}
}

// completeTurn removes a finished tracked turn. Called adjacent to every
// publishTurnTerminal emission in handleRequest. turnID "" is a no-op.
func (h *ChatHandler) completeTurn(turnID string) {
	if h == nil || turnID == "" || h.turnRegistry == nil {
		return
	}
	h.turnRegistry.Complete(turnID)
}

// attachTask records the orchestrator task created for a tracked turn
// (leaf 06 task 2): a reaped turn's terminal event then carries the
// task_id. Both args empty-guarded; no-op for legacy untracked turns.
func (h *ChatHandler) attachTask(turnID, taskID string) {
	if h == nil || turnID == "" || h.turnRegistry == nil {
		return
	}
	h.turnRegistry.AttachTask(turnID, taskID)
}

// turnIDForTask resolves the originating turn id for a finished task
// (async-turn-migration relay fix): the task-end relay must re-broadcast
// the real result under the turn id the client's chat.submit ack returned,
// or a turn-scoped awaiter never matches it. "" when unresolvable.
func (h *ChatHandler) turnIDForTask(taskID string) string {
	if h == nil || h.turnRegistry == nil {
		return ""
	}
	return h.turnRegistry.TurnIDForTask(taskID)
}

// EmitTurnTerminal is the exported emit seam for the TurnWatchdog (leaf
// 06): it forwards to the frozen publishTurnTerminal funnel. Exported
// because package agent's watchdog must not reach back into the handler
// through an unexported method reference from the daemon composition.
func (h *ChatHandler) EmitTurnTerminal(ev TurnTerminalEvent) {
	h.publishTurnTerminal(ev)
}

// SetTaskStore sets the task store for looking up linked sessions.
func (h *ChatHandler) SetTaskStore(store *task.Store) {
	if store != nil {
		h.taskStore = store
	}
}

// SetBudget sets the token budget tracker for async dispatch pre-checks and
// enables auto-resume of turns interrupted by budget exhaustion. This prevents
// zombie tasks from being created when the budget is exceeded, and parks
// time-windowed budget failures (hourly/daily tokens or cost) for automatic
// retry once the window clears.
func (h *ChatHandler) SetBudget(budget *llm.Budget) {
	if budget == nil {
		return
	}
	h.budget = budget
	h.budgetResumeWatcher = NewBudgetResumeWatcher(budget, h.logger, h.resumeParkedTurn)
}

// SetThrottleParker wires the loop's throttle-resume parker (tree 03 leaf 02
// Task 3): the parker's resume callback routes by record class — throttle
// records re-enter the loop via resumeThrottledTurn; quota records (when a
// parker other than the handler's own quota watcher is shared) delegate to
// the existing quota resume callback. Nil-guarded.
func (h *ChatHandler) SetThrottleParker(parker *TurnParker) {
	if h == nil || parker == nil {
		return
	}
	if loop := h.loop; loop != nil {
		loop.SetThrottleParker(h, parker)
	}
}

// resumeRouterDefault is the non-throttle resume fallback: a quota (or
// unknown-class) record that surfaces on a parker the handler does not own
// is logged and dropped rather than mis-dispatched.
func (h *ChatHandler) resumeRouterDefault(ctx context.Context, rec ParkedTurnRecord) {
	h.logger.Warn("parked turn resumed with no router wired — dropping",
		"class", rec.Class,
		"session_id", rec.SessionID,
	)
}

// SetQuotaResumeConfig wires the quota deferral watcher
// (quota-reset-resilience leaf 06) from llm.quota_retry settings. The
// watcher's resume callback is bound internally (mirrors SetBudget).
// maxWait <= 0 falls back to the llm package default. Nil-safe.
func (h *ChatHandler) SetQuotaResumeConfig(maxWait time.Duration) {
	if h == nil || h.quotaResumeWatcher != nil {
		return // already wired; idempotent
	}
	h.quotaResumeWatcher = NewQuotaResumeWatcher(h.logger, h.resumeQuotaParkedTurn, maxWait)
}

// QuotaResumeWatcher exposes the configured watcher so the daemon can Start
// it on its lifecycle. Returns nil when quota deferral is not wired.
func (h *ChatHandler) QuotaResumeWatcher() *QuotaResumeWatcher {
	if h == nil {
		return nil
	}
	return h.quotaResumeWatcher
}

// resumeQuotaParkedTurn re-runs a turn that was parked due to a provider
// quota wall. Called by the QuotaResumeWatcher once the quota window lifts.
func (h *ChatHandler) resumeQuotaParkedTurn(ctx context.Context, turn QuotaParkedTurn) {
	h.runEffectsResumeHook(ctx) // best-effort reconcile; logs its own errors
	h.logger.Info("resuming parked turn after quota reset",
		"session_id", turn.SessionID,
		"conversation_id", turn.ConversationID,
		"provider", turn.ProviderID,
		"parked_at", turn.ParkedAt,
	)

	loop := h.sessionLoop(turn.ConversationID)
	// H3 (bughunt 2026-09-03): resumed turns must not re-add the user
	// message — the parked attempt already placed it in the conversation.
	reply, err := loop.RunOnceWithParts(WithResumedTurn(ctx), turn.Message, turn.Parts, turn.ConversationID)
	if err != nil {
		// Still quota-blocked (reset drifted) or a new failure: park again
		// if the error is quota and the window is knowable; otherwise push
		// the error to the session.
		var quotaErr *llm.QuotaResetError
		if errors.As(err, &quotaErr) {
			unblockAt := quotaErr.ResetAt
			if unblockAt.IsZero() && quotaErr.RetryAfter > 0 {
				unblockAt = time.Now().Add(quotaErr.RetryAfter)
			}
			if !unblockAt.IsZero() && h.quotaResumeWatcher.Park(QuotaParkedTurn{
				SessionID:      turn.SessionID,
				ConversationID: turn.ConversationID,
				Message:        turn.Message,
				Parts:          turn.Parts,
				AgentID:        turn.AgentID,
				SourceClient:   turn.SourceClient,
				TurnID:         turn.TurnID, // F15: preserve the turn identity across re-parks
				ProviderID:     quotaErr.ProviderID,
				UnblockAt:      unblockAt,
			}) {
				h.logger.Info("re-parked turn after fresh quota error",
					"session_id", turn.SessionID,
					"provider", quotaErr.ProviderID,
				)
				return
			}
		}
		h.logger.Error("resumed turn failed",
			"session_id", turn.SessionID,
			"error", err,
		)
		h.sendResponse("quota-resume-"+turn.SessionID, ChatResponse{
			ConversationID: turn.SessionID,
			Error:          "Auto-resume failed: " + err.Error(),
		})

		// Final turn.terminal for the submitted turn (F15): the resume
		// failed after the re-park declined — report it honestly so the
		// awaiter resolves as failed. Legacy parked turns stay silent.
		if turn.TurnID != "" {
			h.publishTurnTerminal(TurnTerminalEvent{
				ConversationID: turn.ConversationID,
				SessionID:      turn.SessionID,
				TurnID:         turn.TurnID,
				HandlerCase:    "quota_resume",
				Status:         "failed",
				Reply:          "I encountered an error: Auto-resume failed: " + err.Error(),
				Error:          err.Error(),
			})
		}
		return
	}

	// Persist the exchange
	h.persistExchange(turn.SessionID, turn.Message, turn.Parts, reply, turn.AgentID)

	// Push the result back to the session via the bus.
	h.sendResponse("quota-resume-"+turn.SessionID, ChatResponse{
		ConversationID: turn.SessionID,
		SessionID:      turn.SessionID, // for WS push routing
		Reply:          reply,
	})

	// Final turn.terminal for the submitted turn (F15): a parked
	// chat.submit turn ended here — the client's awaiter resolves only if
	// the terminal event fires under the id the ack returned. Legacy
	// parked turns (TurnID "") stay silent.
	if turn.TurnID != "" {
		h.publishTurnTerminal(TurnTerminalEvent{
			ConversationID: turn.ConversationID,
			SessionID:      turn.SessionID,
			TurnID:         turn.TurnID,
			HandlerCase:    "quota_resume",
			Status:         "completed",
			Reply:          reply,
		})
	}

	h.logger.Info("resumed turn completed after quota reset",
		"session_id", turn.SessionID,
		"reply_length", len(reply),
	)
}

// SetSyncMode enables or disables synchronous dispatch mode.
// When enabled, async-dispatched tasks are waited on in the handler
// instead of returning immediately.
//
// Deprecated (async-turn-migration leaf 07): this is the seam for the
// LEGACY blocking-chat opt-in only — production wiring calls
// SetSyncMode(cfg.Orchestrator.SyncChatEnabled). Under the default
// (sync_chat_enabled=false) the sync wait is unreachable: chat.submit
// acks and results arrive via turn.terminal. No in-tree caller forces
// sync by itself any more (the former meept-bench source special-case
// now also requires the flag).
func (h *ChatHandler) SetSyncMode(enabled bool) {
	h.syncMode = enabled
}

// SetNotificationPublisher provides the ChatHandler with a notification publisher
// so it can emit task completion/failure events to the notification system.
func (h *ChatHandler) SetNotificationPublisher(p NotificationPublisher) {
	h.notificationPublisher = p
}

// publishNotification sends a session notification through the publisher if available.
func (h *ChatHandler) publishNotification(typ, title, message string) {
	if h.notificationPublisher != nil {
		h.notificationPublisher.PublishSessionNotification("", "chat-handler", typ, title, message)
	}
}

// SetSyncWaitStall wires the stall-based sync-wait ceiling (orchestrator.
// sync_wait_stall / orchestrator.sync_wait_max). Nil-guarded per the
// setter convention (the ChatHandler itself is never nil here, but the
// daemon calls this unconditionally next to the other Set* wiring, so the
// guard protects a partially-constructed handler).
func (h *ChatHandler) SetSyncWaitStall(stall, hardMax time.Duration) {
	if h != nil {
		h.syncWaitStall = stall
		h.syncWaitMax = hardMax
	}
}

// syncWaitMode selects which ceiling waitForTaskCompletion enforces.
type syncWaitMode int

const (
	// syncWaitModeStall: inactivity-based ceiling with a hard elapsed cap
	// (the new default when sync_wait_stall is unset/zero).
	syncWaitModeStall syncWaitMode = iota
	// syncWaitModeFixed: the legacy fixed elapsed ceiling (110s), selected
	// by syncWaitCeiling > 0 (test seam) or sync_wait_stall < 0.
	syncWaitModeFixed
)

// syncWaitPlan resolves the ChatHandler's sync-wait knobs into a ceiling
// mode + durations. Kept pure so the selection table is unit-testable
// without a task store.
func (h *ChatHandler) syncWaitPlan() (mode syncWaitMode, stall, hardMax time.Duration) {
	if h.syncWaitCeiling > 0 {
		// Explicit test ceiling always wins (legacy fixed semantics).
		return syncWaitModeFixed, 0, h.syncWaitCeiling
	}
	if h.syncWaitStall < 0 {
		// Escape hatch: sync_wait_stall < 0 restores the byte-identical
		// legacy fixed 110s ceiling (syncWaitCeiling test seam may still
		// raise it in tests; production keeps 110s).
		return syncWaitModeFixed, 0, 110 * time.Second
	}
	// Stall mode. 0 = "enabled by default"; use the legacy 110s window as
	// the stall threshold only when the operator set a positive value —
	// otherwise any positive value they chose applies directly. A zero
	// stall with a positive hardMax would mean "instant stall", which is
	// meaningless, so zero means the documented default window (2 minutes
	// of inactivity) — see the sync_wait_stall config comment.
	stall = h.syncWaitStall
	if stall == 0 {
		stall = 2 * time.Minute
	}
	hardMax = h.syncWaitMax
	if hardMax <= 0 {
		hardMax = 30 * time.Minute
	}
	// NOTE: no normalization when hardMax < stall — the hard cap is hard:
	// a config with sync_wait_max below sync_wait_stall simply means the
	// elapsed cap fires before inactivity ever could.
	return syncWaitModeStall, stall, hardMax
}

// degradedSyncReply returns the degraded still-running reply: the best
// APPROVED/completed step result when one exists (e2e run 5, 2026-09-11 —
// at ceiling time the user's answer may already sit in a finished step
// while later bookkeeping steps still run), else the generic stub.
func (h *ChatHandler) degradedSyncReply(taskID string) string {
	h.logger.Warn("Task wait timeout exceeded", "task_id", taskID)
	if h.stepStore != nil {
		if steps, err := h.stepStore.ListByTaskID(taskID); err == nil {
			if result := bestStepResult(steps); result != "" {
				return result
			}
		}
	}
	return fmt.Sprintf("Task %s is still running; results will arrive when it completes.", taskID)
}

// syncFingerprint captures everything the stall detector treats as
// progress: the task state plus every step's ID/state pair (count and any
// per-step state transition both change the string). Intentionally cheap:
// one task read + one step listing per 2s poll.
func (h *ChatHandler) syncFingerprint(t *task.Task) string {
	var b strings.Builder
	b.WriteString(string(t.State))
	if h.stepStore != nil {
		steps, err := h.stepStore.ListByTaskID(t.ID)
		if err != nil {
			// An unreadable step set must NOT look like progress (that
			// would stall the detector open-endedly): encode the error so
			// the fingerprint differs from any healthy one.
			b.WriteString("|steps-error")
			return b.String()
		}
		for _, s := range steps {
			b.WriteString("|")
			b.WriteString(s.ID)
			b.WriteString(":")
			b.WriteString(string(s.State))
		}
	}
	return b.String()
}

// waitForTaskCompletion waits for a task to reach a terminal state
// and returns the final result string. Returns immediately if the task
// is already terminal or if the store/ctx is not available.
//
// The wait is bounded two ways (stall-based sync-wait ceiling):
//   - Stall mode (default): the 2s task-store poll compares the task
//     state and the step set against the previous poll; no change for
//     longer than the stall window returns the degraded reply even
//     though the task is still non-terminal, and a hard elapsed cap
//     bounds even visibly-progressing tasks.
//   - Fixed mode (escape hatch, sync_wait_stall < 0): the legacy fixed
//     elapsed ceiling (110s), byte-identical with the pre-stall code.
//
// A task that cannot finish in time still runs to completion
// asynchronously — only the reply is bounded.
func (h *ChatHandler) waitForTaskCompletion(ctx context.Context, taskID string) string {
	if h.taskStore == nil || taskID == "" {
		return ""
	}

	mode, stall, hardMax := h.syncWaitPlan()

	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var fixedDone <-chan time.Time
	if mode == syncWaitModeFixed {
		fixedDone = time.After(hardMax)
	}

	// Progress fingerprint for stall mode: task state + every step's
	// ID/state pair. Any poll that observes a different fingerprint resets
	// the stall timer. Empty (and never compared) in fixed mode.
	lastProgress := start
	var lastFingerprint string
	if mode == syncWaitModeStall {
		if t, err := h.taskStore.GetByID(taskID); err == nil && t != nil {
			lastFingerprint = h.syncFingerprint(t)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ""
		case <-fixedDone:
			// Fixed-mode ceiling (legacy semantics). Degraded still-running
			// reply (e2e run 5, 2026-09-11): return the best
			// APPROVED/completed step result when one exists — at ceiling
			// time the task may have already produced the user's answer in
			// a finished step while later bookkeeping steps (review,
			// "return final confirmation") still run. Bounded well below
			// the CLI's ~120s socket read either way.
			return h.degradedSyncReply(taskID)
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
					// A failed task may still hold a finished step with the
					// user's answer (e2e run 2026-09-23 gp2KnX: T1's file-write
					// step completed but a later bookkeeping step failed). Relay
					// the best approved/completed step result when one exists —
					// the generic "failed" sentence is the LAST resort, not the
					// first, matching the degraded-reply behavior at the wait
					// ceiling above.
					if h.stepStore != nil {
						if steps, err := h.stepStore.ListByTaskID(taskID); err == nil {
							if result := bestStepResult(steps); result != "" {
								return result
							}
						}
					}
					return fmt.Sprintf("Task %s failed after reaching terminal state.", taskID)
				}
				if h.stepStore != nil {
					if steps, err := h.stepStore.ListByTaskID(taskID); err == nil {
						if result := bestStepResult(steps); result != "" {
							return result
						}
					} else {
						h.logger.Debug("Failed to fetch steps for sync reply",
							"task_id", taskID, "error", err)
					}
				}
				return fmt.Sprintf("Task %s completed.", taskID)
			}
			if mode == syncWaitModeStall {
				now := time.Now()
				if now.Sub(start) > hardMax {
					// Hard cap: the task may still be visibly progressing,
					// but the sync reply must land inside the proxy timeout.
					return h.degradedSyncReply(taskID)
				}
				fp := h.syncFingerprint(t)
				if fp != lastFingerprint {
					// Visible progress: state or step set changed since the
					// previous poll — reset the stall timer.
					lastFingerprint = fp
					lastProgress = now
					continue
				}
				if now.Sub(lastProgress) > stall {
					return h.degradedSyncReply(taskID)
				}
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

// SetAgentLoopManager registers a per-session AgentLoop manager.
// When set, direct-mode chat requests route to a session-scoped
// AgentLoop (created via GetOrCreateWired from the singleton template)
// if the session has a project path. Falls back to the singleton loop.
func (h *ChatHandler) SetAgentLoopManager(m *Manager) {
	if m != nil {
		h.loopManager = m
	}
}

// SetSessionStore registers a session store for project-path lookup.
// Accepts any type with a Get(id string) *session.Session method (e.g.
// session.Store or the narrow SessionStoreReader).
func (h *ChatHandler) SetSessionStore(s SessionStoreReader) {
	if s != nil {
		h.sessionStore = s
	}
}

// SetMessageSaver wires the session message persistence callback.
// When set, ChatHandler persists user and assistant messages after each
// successful exchange so the Flutter GUI (and any HTTP-only client) can
// reload conversation history.
func (h *ChatHandler) SetMessageSaver(s SessionMessageSaver) {
	if s != nil {
		h.messageSaver = s
	}
}

// ClearConversation removes the in-memory conversation cache for a session.
// Called by the /reset RPC handler after clearing persisted messages.
func (h *ChatHandler) ClearConversation(conversationID string) {
	if h.loop != nil && h.loop.conversations != nil {
		h.loop.conversations.Delete(conversationID)
	}
}

// resumeParkedTurn re-runs a turn that was parked due to budget exhaustion.
// Called by the BudgetResumeWatcher once the budget window clears.
func (h *ChatHandler) resumeParkedTurn(ctx context.Context, turn ParkedTurn) {
	h.runEffectsResumeHook(ctx) // best-effort reconcile; logs its own errors
	h.logger.Info("resuming parked turn after budget clearance",
		"session_id", turn.SessionID,
		"conversation_id", turn.ConversationID,
		"parked_at", turn.ParkedAt,
	)

	// Run the turn through the same path as a normal chat request.
	// H3 (bughunt 2026-09-03): resumed turns must not re-add the user
	// message — the parked attempt already placed it in the conversation.
	loop := h.sessionLoop(turn.ConversationID)
	reply, err := loop.RunOnceWithParts(WithResumedTurn(ctx), turn.Message, turn.Parts, turn.ConversationID)

	if err != nil {
		h.logger.Error("resumed turn failed",
			"session_id", turn.SessionID,
			"error", err,
		)
		// Push error back to session
		h.sendResponse("budget-resume-"+turn.SessionID, ChatResponse{
			ConversationID: turn.SessionID,
			Error:          "Auto-resume failed: " + err.Error(),
		})

		// Final turn.terminal for the submitted turn (F15): the resume
		// failed — report it honestly so the awaiter resolves as failed.
		// Legacy parked turns (TurnID "") stay silent.
		if turn.TurnID != "" {
			h.publishTurnTerminal(TurnTerminalEvent{
				ConversationID: turn.ConversationID,
				SessionID:      turn.SessionID,
				TurnID:         turn.TurnID,
				HandlerCase:    "budget_resume",
				Status:         "failed",
				Reply:          "I encountered an error: Auto-resume failed: " + err.Error(),
				Error:          err.Error(),
			})
		}
		return
	}

	// Persist the exchange
	h.persistExchange(turn.SessionID, turn.Message, turn.Parts, reply, turn.AgentID)

	// Push the result back to the session via the bus. sendResponse publishes
	// both chat.response (RPC reply) and chat_message (WS push notification).
	h.sendResponse("budget-resume-"+turn.SessionID, ChatResponse{
		ConversationID: turn.SessionID,
		SessionID:      turn.SessionID, // for WS push routing
		Reply:          reply,
	})

	// Final turn.terminal for the submitted turn (F15): a parked
	// chat.submit turn ended here — the client's awaiter resolves only if
	// the terminal event fires under the id the ack returned. Legacy
	// parked turns stay silent.
	if turn.TurnID != "" {
		h.publishTurnTerminal(TurnTerminalEvent{
			ConversationID: turn.ConversationID,
			SessionID:      turn.SessionID,
			TurnID:         turn.TurnID,
			HandlerCase:    "budget_resume",
			Status:         "completed",
			Reply:          reply,
		})
	}

	h.logger.Info("resumed turn completed successfully",
		"session_id", turn.SessionID,
		"reply_length", len(reply),
	)
}

// applyRequestModel arms a chat.request "model" ref on the turn's loop via
// the loop's one-shot ApplyRequestModel seam (same precedence slot as a
// parsed user directive; consumed by this turn, never persisted). Nil-safe;
// empty refs no-op inside the loop.
func (h *ChatHandler) applyRequestModel(loop *AgentLoop, modelRef, conversationID string) {
	if loop == nil || modelRef == "" {
		return
	}
	h.logger.Info("Chat request carries a per-request model",
		"conversation", conversationID,
		"model", modelRef,
	)
	loop.ApplyRequestModel(modelRef)
}

// sessionLoop returns a per-session AgentLoop when a manager is wired
// and the session resolves a working directory; otherwise returns the
// singleton.
//
// Resolving the working directory here is the turn-start half of the
// "always have a working directory" contract (fresh-rig daemon11,
// 2026-09-13: a turn ran with no session working directory, so every
// filesystem tool failed with the bare "no path specified" and the model
// retried the identical call until the cycle guard aborted the turn).
// Precedence is the repo-wide one (session.ResolveWorkingDir):
//
//	WorktreePath > ProjectPath > DetectionContext.CWD
//
// then, only when the session resolves nothing, the daemon's configured
// default working dir. There is NO global active-project fallback:
// projects are scoped per session, so a session resolves its OWN binding
// only. The daemon's process CWD is never a source (AGENTS.md).
func (h *ChatHandler) sessionLoop(conversationID string) *AgentLoop {
	if h.loopManager == nil {
		return h.loop
	}
	var sess *session.Session
	if h.sessionStore != nil {
		sess = h.sessionStore.GetByConversationID(conversationID)
		if sess == nil {
			// The Flutter client sends the session's primary ID in the
			// conversation_id field; accept both lookups, mirroring the
			// dual lookup in the daemon's resolveStepWorkingDir.
			sess = h.sessionStore.Get(conversationID)
		}
	}
	// Legacy session fallback: if ProjectPath is empty but ProjectID is set,
	// look up the project's LocalPath from the project manager (available
	// via the loop manager) before falling back to the singleton loop.
	if sess != nil && sess.ProjectPath == "" && sess.ProjectID != "" {
		if path := h.loopManager.ResolveProjectPath(context.Background(), sess.ProjectID); path != "" {
			sess.ProjectPath = path
		}
	}
	// Resolve the effective working path: worktree overrides project path,
	// which overrides the client's detection CWD.
	workingPath, wdSource := h.effectiveWorkingDir(sess)
	if workingPath == "" {
		// No working directory anywhere: the turn keeps the singleton loop
		// (which has whatever dir the daemon was configured with) and every
		// filesystem tool that needs a session dir will fail with the
		// actionable tools.ErrNoWorkingDir. Log the cause loudly — this was
		// silent before, and a silent unbound turn is a 20-tool failure
		// cascade that looks like a model problem.
		h.logger.Warn("chat turn has no working directory bound; filesystem tools will require an explicit path",
			"conversation_id", conversationID,
			"has_session", sess != nil,
			"hint", "bind a project to this session (project.set) or start the client from the project directory (--cwd)")
		return h.loop
	}
	// Configure the shared fence sandbox for this session: the sandbox root
	// follows the session's working directory, and the per-session
	// --nofence override (persisted on the session) disables fencing.
	if h.fenceController != nil {
		if err := h.fenceController.SetRootPath(workingPath); err != nil {
			h.logger.Warn("fence: failed to set session root; fencing stays blocked until a valid root is set",
				"session", conversationID,
				"root", workingPath,
				"error", err,
			)
		}
		h.fenceController.SetNoFence(sess != nil && sess.NoFence)
	}
	// Create a session-scoped loop via the manager. This avoids mutating
	// the shared singleton's workingDir (which races with concurrent
	// sessions). If manager creation fails, fall back to the singleton
	// with a warning.
	h.logger.Debug("chat turn bound to a session working directory",
		"conversation_id", conversationID,
		"working_dir", workingPath,
		"source", wdSource,
	)
	loop, err := h.loopManager.GetOrCreateWired(conversationID, workingPath, h.loop)
	if err != nil {
		h.logger.Warn("session-scoped loop creation failed; using singleton",
			"session", conversationID,
			"working_path", workingPath,
			"error", err,
		)
		// Last resort: mutate the singleton. This is a known race under
		// concurrent multi-session load but better than no workingDir.
		h.loop.SetWorkingDir(workingPath)
		return h.loop
	}
	// Wire session identity + project context onto the loop so the system
	// prompt's "Session Context" section is populated. ConfigSnapshot
	// deliberately excludes these per-session fields, so they must be set
	// here on every lookup (idempotent for an already-cached loop).
	if sess != nil {
		loop.SetProjectID(sess.ProjectID)
		if sess.DetectionContext != nil {
			loop.SetDetectionContext(&DetectionContext{
				CWD:               sess.DetectionContext.CWD,
				DetectedProjectID: sess.DetectionContext.DetectedProjectID,
				CLIArgs:           sess.DetectionContext.CLIArgs,
			})
		}
	}
	// Note: project_info tool resolution is handled via context injection
	// in AgentLoop.executeToolCalls (tools.ContextWithWorkingDir), not via
	// SetWorkingDirFunc here. The context approach avoids a race condition
	// where multiple sessions sharing the same tool registry pointer would
	// overwrite each other's working directory resolver.
	return loop
}

// effectiveWorkingDir resolves the working directory for a chat turn and a
// diagnostic label for where it came from. Order:
//
//  1. session.ResolveWorkingDir — WorktreePath > ProjectPath > DetectionContext.CWD
//  2. the daemon's configured default working dir (defaultWorkingDir)
//
// Per-session project scoping: there is no global active-project fallback,
// so a session with no worktree, no project and no client CWD resolves only
// the configured default. Returns ("", "none") when nothing resolves;
// callers must fail actionably rather than guessing a directory. The
// daemon's own process CWD is never consulted (AGENTS.md: Daemon CWD is NOT
// the user's project).
func (h *ChatHandler) effectiveWorkingDir(sess *session.Session) (string, string) {
	if dir, src := session.ResolveWorkingDir(sess); dir != "" {
		return dir, string(src)
	}
	if h.defaultWorkingDir != "" {
		return h.defaultWorkingDir, string(session.WorkingDirFromDefault)
	}
	return "", string(session.WorkingDirFromNone)
}

// SetDefaultWorkingDir wires the daemon's configured default working
// directory. It is the LAST resort, used only for turns whose session binds
// no working directory of its own. Empty string (the default) leaves such a
// turn unbound: filesystem tools then return tools.ErrNoWorkingDir instead
// of silently operating on the daemon's own directory.
func (h *ChatHandler) SetDefaultWorkingDir(dir string) {
	if h == nil {
		return
	}
	h.defaultWorkingDir = dir
}

// LookupLoop returns the AgentLoop responsible for a given conversation/session
// ID. It is the public accessor over sessionLoop for external callers (HTTP
// server state queries, RPC handlers, etc.). Returns nil if the handler has no
// loop available.
func (h *ChatHandler) LookupLoop(conversationID string) *AgentLoop {
	if h == nil {
		return nil
	}
	return h.sessionLoop(conversationID)
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
	// Use context.Background() to detach from the request context - the session
	// has its own lifecycle governed by cfg.TimeBudget, not the request deadline.
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		runCtx, cancel := context.WithTimeout(context.Background(), cfg.TimeBudget)
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
		return time.Now().Format("20060102150405.000000000") + "-fallback"
	}
	return time.Now().Format("20060102150405.000000000") + "-" + hex.EncodeToString(randBytes[:])
}
