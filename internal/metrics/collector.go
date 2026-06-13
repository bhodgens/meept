// Package metrics provides metrics collection and storage for Meept.
package metrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
	"github.com/jmoiron/sqlx"
)

// AgentEventType identifies a typed agent event. This mirrors the type from
// internal/agent/events.go to avoid an import cycle (agent -> metrics).
type AgentEventType string

// Dimension key constants for metric labels.
const (
	DimModel    = "model"
	DimReviewer = "reviewer"
	DimOutcome  = "outcome"
)

// Typed event type constants used by the metrics collector.
const (
	AgentEventAfterProviderResponse AgentEventType = "after_provider_response"
	AgentEventTurnEnd               AgentEventType = "turn_end"
	AgentEventSessionEnd            AgentEventType = "session_end"
	AgentEventToolExecutionStart    AgentEventType = "tool_execution_start"
	AgentEventToolExecutionEnd      AgentEventType = "tool_execution_end"
)

// AgentEventData is the interface all event payloads implement.
// This mirrors the interface from internal/agent/events.go to avoid an import cycle.
type AgentEventData interface {
	agentEventData()
}

// AgentEvent is the envelope for typed agent events.
type AgentEvent struct {
	Type           AgentEventType `json:"type"`
	Timestamp      time.Time      `json:"timestamp"`
	AgentID        string         `json:"agent_id"`
	ConversationID string         `json:"conversation_id"`
	Iteration      int            `json:"iteration"`
	Data           any            `json:"data"`
}

// --- Event data types used by the metrics collector ---
// These mirror the types from internal/agent/events.go.

// AfterProviderResponseData is emitted after receiving an LLM provider response.
type AfterProviderResponseData struct {
	ModelID        string        `json:"model_id"`
	StatusCode     int           `json:"status_code"`
	ResponseTokens int           `json:"response_tokens"`
	Latency        time.Duration `json:"latency"`
	Cached         bool          `json:"cached"`
	Error          string        `json:"error,omitempty"`
}

func (AfterProviderResponseData) agentEventData() {}

// TurnEndData is emitted at the end of each loop iteration.
type TurnEndData struct {
	TurnNumber     int    `json:"turn_number"`
	HadToolCalls   bool   `json:"had_tool_calls"`
	ToolCallCount  int    `json:"tool_call_count"`
	ResponseTokens int    `json:"response_tokens"`
	StoppedBy      string `json:"stopped_by"`
}

func (TurnEndData) agentEventData() {}

// SessionEndData is emitted when an agent session ends.
type SessionEndData struct {
	SessionID   string        `json:"session_id"`
	Outcome     string        `json:"outcome"`
	Duration    time.Duration `json:"duration"`
	TotalTokens int           `json:"total_tokens"`
	TotalIter   int           `json:"total_iter"`
	Error       string        `json:"error,omitempty"`
}

func (SessionEndData) agentEventData() {}

// ToolExecutionStartData is emitted before a tool is executed.
type ToolExecutionStartData struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
}

func (ToolExecutionStartData) agentEventData() {}

// ToolExecutionEndData is emitted after a tool execution completes.
type ToolExecutionEndData struct {
	ToolCallID  string        `json:"tool_call_id"`
	ToolName    string        `json:"tool_name"`
	Success     bool          `json:"success"`
	Result      string        `json:"result"`
	Error       string        `json:"error,omitempty"`
	Cached      bool          `json:"cached"`
	Duration    time.Duration `json:"duration"`
	Blocked     bool          `json:"blocked"`
	BlockReason string        `json:"block_reason,omitempty"`
}

func (ToolExecutionEndData) agentEventData() {}

// TypedEventEmitter is the interface satisfied by agent.EventEmitter.
// Defined here to avoid an import cycle (agent -> metrics).
type TypedEventEmitter interface {
	// On registers a synchronous listener for a specific event type.
	On(eventType AgentEventType, name string, listener func(ctx context.Context, event AgentEvent))
	// OnAsync registers an asynchronous listener for a specific event type.
	OnAsync(eventType AgentEventType, name string, listener func(ctx context.Context, event AgentEvent))
}

// Collector collects metrics from various sources.
type Collector struct {
	subs          []*bus.Subscriber
	store           *Store
	bus             *bus.MessageBus
	stopChan        chan struct{}
	wg              sync.WaitGroup
	getQueueDepth   func() int
	getActiveAgents func() int
	logger          *slog.Logger
}

// CollectorConfig configures the metrics collector.
type CollectorConfig struct {
	CollectionInterval time.Duration
	Enabled            bool
	GetQueueDepth      func() int // Returns total pending + claimed jobs
	GetActiveAgents    func() int // Returns active agent count
}

// DefaultCollectorConfig returns default collector configuration.
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		CollectionInterval: 5 * time.Second,
		Enabled:            true,
	}
}

// NewCollector creates a new metrics collector.
func NewCollector(store *Store, messageBus *bus.MessageBus, cfg *CollectorConfig) *Collector {
	if cfg == nil {
		cfg = DefaultCollectorConfig()
	}

	c := &Collector{
		store:           store,
		bus:             messageBus,
		stopChan:        make(chan struct{}),
		getQueueDepth:   cfg.GetQueueDepth,
		getActiveAgents: cfg.GetActiveAgents,
		logger:          slog.Default().With("component", "metrics-collector"),
	}

	if cfg.Enabled {
		c.startCollection(cfg.CollectionInterval)
		c.subscribeToBus()
	}

	return c
}

// startCollection starts the background collection goroutine.
func (c *Collector) startCollection(interval time.Duration) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.collect()
			case <-c.stopChan:
				return
			}
		}
	}()
}

// subscribeToBus subscribes to relevant bus messages for metrics.
func (c *Collector) subscribeToBus() {
	if c.bus == nil {
		return
	}

	// Subscribe to metrics topic and process messages in a goroutine
	sub := c.bus.Subscribe("metrics-collector", "metrics")
	c.subs = append(c.subs, sub)
	go func() {
		for msg := range sub.Channel {
			c.handleBusMessage(msg)
		}
	}()

	// Subscribe to review events for review metrics
	reviewSub := c.bus.Subscribe("metrics-collector-review", "step.*")
	c.subs = append(c.subs, reviewSub)
	go func() {
		for msg := range reviewSub.Channel {
			c.handleBusMessage(msg)
		}
	}()
}

// handleBusMessage processes bus messages for metrics collection.
func (c *Collector) handleBusMessage(msg *models.BusMessage) {
	switch msg.Topic {
	case "llm.request":
		c.store.RecordEvent("llm.request", "info", "LLM request completed", map[string]any{
			"source": msg.Source,
		})
	case "llm.error":
		c.store.RecordEvent("llm.error", "error", "LLM request failed", map[string]any{
			"source": msg.Source,
		})
	case "agent.iteration":
		c.store.Record("agent.iterations", 1, map[string]string{
			"agent": msg.Source,
		})
	case "tool.call":
		c.store.Record("tool.calls", 1, map[string]string{
			"tool": msg.Source,
		})
	case "model.failover":
		c.store.RecordEvent("model.failover", "warn", "Model failover occurred", map[string]any{
			"source": msg.Source,
		})
	case "step.review_completed":
		c.recordReviewMetrics(msg)
	}
}

// collect collects system-wide metrics.
func (c *Collector) collect() {
	// Collect queue depth if getter is available
	queueDepth := 0
	if c.getQueueDepth != nil {
		queueDepth = c.getQueueDepth()
	}
	c.store.Record("queue.depth", float64(queueDepth), nil)

	// Collect active agents count if getter is available
	activeAgents := 0
	if c.getActiveAgents != nil {
		activeAgents = c.getActiveAgents()
	}
	c.store.Record("agent.active", float64(activeAgents), nil)

	// Collect memory usage
	// c.store.Record("memory.entities", count, nil)

	// Collect scheduler jobs
	// c.store.Record("scheduler.jobs", count, nil)
}

// RecordLLMCall records an LLM API call.
func (c *Collector) RecordLLMCall(model string, inputTokens, outputTokens int, latency time.Duration) {
	c.store.Record("llm.calls", 1, map[string]string{
		DimModel: model,
	})
	c.store.Record("tokens.input", float64(inputTokens), map[string]string{
		DimModel: model,
	})
	c.store.Record("tokens.output", float64(outputTokens), map[string]string{
		DimModel: model,
	})
	c.store.Record("llm.latency", latency.Seconds(), map[string]string{
		DimModel: model,
	})
}

// RecordAgentIteration records an agent iteration.
func (c *Collector) RecordAgentIteration(agentID string) {
	c.store.Record("agent.iterations", 1, map[string]string{
		"agent_id": agentID,
	})
}

// RecordToolCall records a tool execution.
func (c *Collector) RecordToolCall(toolName string, duration time.Duration, success bool) {
	tags := map[string]string{
		"tool_name": toolName,
	}
	if !success {
		tags["error"] = "true"
	}

	c.store.Record("tool.calls", 1, tags)
	c.store.Record("tool.duration", duration.Seconds(), tags)

	if !success {
		c.store.RecordEvent("tool.error", "error", "Tool call failed", map[string]any{
			"tool": toolName,
		})
	}
}


// RecordReviewResult records a review result metric.
func (c *Collector) RecordReviewResult(status, reviewerID string, confidence float64) {
	c.store.Record("review.completed", 1, map[string]string{
		"status":    status,
		DimReviewer: reviewerID,
	})
	c.store.Record("review.confidence", confidence, map[string]string{
		DimReviewer: reviewerID,
	})

	switch status {
	case "approved":
		c.store.Record("review.pass_rate", 1, nil)
	case "rejected":
		c.store.Record("review.revision_rate", 1, nil)
	case "needs_info":
		c.store.Record("review.escalation_rate", 1, nil)
	}
}

// recordReviewMetrics extracts review metrics from a bus message.
func (c *Collector) recordReviewMetrics(msg *models.BusMessage) {
	var payload struct {
		Status        string  `json:"status"`
		Reviewer      string  `json:"reviewer"`
		Confidence    float64 `json:"confidence"`
		RevisionCount int     `json:"revision_count"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.logger.Error("failed to unmarshal review metrics payload", "error", err)
		return
	}

	c.RecordReviewResult(payload.Status, payload.Reviewer, payload.Confidence)

	// Track average revision cycles
	if payload.RevisionCount > 0 {
		c.store.Record("review.revision_cycles", float64(payload.RevisionCount), map[string]string{
			DimReviewer: payload.Reviewer,
		})
	}

	c.store.RecordEvent("review.completed", "info",
		fmt.Sprintf("Review %s by %s (confidence %.2f, revisions %d)", payload.Status, payload.Reviewer, payload.Confidence, payload.RevisionCount),
		map[string]any{
			"status":         payload.Status,
			DimReviewer:      payload.Reviewer,
			"confidence":     payload.Confidence,
			"revision_count": payload.RevisionCount,
		},
	)
}

// RegisterEventListeners subscribes the collector to typed agent events via an
// EventEmitter. This supplements the existing bus subscriptions so the collector
// works with both legacy bus topics and the new typed event system.
//
// All listeners are registered as async since metrics collection should not
// block the agent loop.
func (c *Collector) RegisterEventListeners(emitter TypedEventEmitter) {
	// Track LLM token usage from provider responses.
	emitter.OnAsync(AgentEventAfterProviderResponse, "metrics.token-tracking",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(AfterProviderResponseData)
			if !ok {
				return
			}

			if data.Error != "" {
				c.store.RecordEvent("llm.error", "error", "LLM request failed",
					map[string]any{
						DimModel: data.ModelID,
						"error":  data.Error,
					},
				)
				return
			}

			if data.Cached {
				c.store.Record("llm.cache_hits", 1, map[string]string{
					DimModel: data.ModelID,
				})
			}

			c.RecordLLMCall(data.ModelID, 0, data.ResponseTokens, data.Latency)
		},
	)

	// Track agent iterations at turn boundaries.
	emitter.OnAsync(AgentEventTurnEnd, "metrics.turn-tracking",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(TurnEndData)
			if !ok {
				return
			}

			agentID := event.AgentID
			if agentID == "" {
				agentID = "unknown"
			}
			c.RecordAgentIteration(agentID)

			if data.HadToolCalls {
				c.store.Record("agent.tool_turns", 1, map[string]string{
					"agent_id": agentID,
				})
			}
		},
	)

	// Track session-level metrics when sessions complete.
	emitter.OnAsync(AgentEventSessionEnd, "metrics.session-tracking",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(SessionEndData)
			if !ok {
				return
			}

			c.store.Record("session.duration", data.Duration.Seconds(), map[string]string{
				DimOutcome: data.Outcome,
			})
			c.store.Record("session.tokens", float64(data.TotalTokens), map[string]string{
				DimOutcome: data.Outcome,
			})
			c.store.Record("session.iterations", float64(data.TotalIter), map[string]string{
				DimOutcome: data.Outcome,
			})

			if data.Error != "" {
				c.store.RecordEvent("session.error", "error", "Session ended with error",
					map[string]any{
						"session_id": data.SessionID,
						DimOutcome:   data.Outcome,
						"error":      data.Error,
					},
				)
			}

			c.store.RecordEvent("session.completed", "info",
				fmt.Sprintf("Session %s ended (%s, %d tokens, %d iterations)",
					data.SessionID, data.Outcome, data.TotalTokens, data.TotalIter),
				map[string]any{
					"session_id":   data.SessionID,
					DimOutcome:     data.Outcome,
					"total_tokens": data.TotalTokens,
					"total_iter":   data.TotalIter,
					"duration":     data.Duration.String(),
				},
			)
		},
	)

	// Track tool execution start for tool call counting.
	emitter.OnAsync(AgentEventToolExecutionStart, "metrics.tool-start",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(ToolExecutionStartData)
			if !ok {
				return
			}
			c.store.Record("tool.calls", 1, map[string]string{
				"tool_name": data.ToolName,
			})
		},
	)

	// Track tool execution end for duration and success/failure.
	emitter.OnAsync(AgentEventToolExecutionEnd, "metrics.tool-end",
		func(ctx context.Context, event AgentEvent) {
			data, ok := event.Data.(ToolExecutionEndData)
			if !ok {
				return
			}
			c.RecordToolCall(data.ToolName, data.Duration, data.Success)
		},
	)
}

// Shutdown stops the collector.
func (c *Collector) Shutdown() {
	select {
	case <-c.stopChan:
		// Already closed
	default:
		close(c.stopChan)
	}
	c.wg.Wait()

	// Clean up bus subscriptions to stop goroutines
	for _, sub := range c.subs {
		c.bus.Unsubscribe(sub)
	}
}


// AgentTaskMetrics represents metrics for a single agent task execution.
type AgentTaskMetrics struct {
	TaskID               string
	AgentID              string
	SkillName            string
	Status               string // completed, failed, timeout, abandoned
	Success              bool
	Iterations           int
	DurationMs           int64
	TokensInput          int
	TokensOutput         int
	EstimatedCostCents   float64
	ResponseWellFormed   bool
	SyntaxErrors         int
	IndentationErrors    int
	LazyResponse         bool
	ContextExhausted     bool
	ReflectionIterations int
	ReflectionSuccess    bool
	UserInterventions    int
	UserSatisfaction     int
	ModelID              string
	EditFormat           string
}

// TaskCollector collects agent task metrics with async flush.
type TaskCollector struct {
	db          *sqlx.DB
	logger      *slog.Logger
	flushQueue  chan *AgentTaskMetrics
	flushTicker *time.Ticker
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewTaskCollector creates a new task metrics collector.
// It opens the database at dbPath and starts background flush goroutines.
func NewTaskCollector(dbPath string, logger *slog.Logger) (*TaskCollector, error) {
	// Expand path
	expandedPath := expandPath(dbPath)

	// Ensure directory exists
	dir := filepath.Dir(expandedPath)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create metrics directory: %w", err)
	}

	// Open database
	rawDB, err := sql.Open("sqlite", expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := sqlx.NewDb(rawDB, "sqlite")

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite writes must be serialized
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Initialize schema for agent_task_outcomes
	if err := initAgentTaskSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize agent task schema: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	c := &TaskCollector{
		db:          db,
		logger:      logger.With("component", "task-collector"),
		flushQueue:  make(chan *AgentTaskMetrics, 1000),
		flushTicker: time.NewTicker(5 * time.Second),
		stopChan:    make(chan struct{}),
	}

	// Start background flush loop
	c.wg.Add(1)
	go c.flushLoop()

	return c, nil
}

// initAgentTaskSchema creates the agent_task_outcomes table if it doesn't exist.
func initAgentTaskSchema(db *sqlx.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS agent_task_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    skill_name TEXT,
    status TEXT,
    success BOOLEAN,
    iterations INTEGER,
    duration_ms INTEGER,
    tokens_input INTEGER,
    tokens_output INTEGER,
    estimated_cost_cents REAL,
    response_well_formed BOOLEAN,
    syntax_errors_count INTEGER,
    indentation_errors_count INTEGER,
    lazy_response_detected BOOLEAN,
    context_exhausted BOOLEAN,
    reflection_iterations INTEGER,
    reflection_successful BOOLEAN,
    user_interventions INTEGER,
    user_satisfaction INTEGER,
    model_id TEXT,
    edit_format TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_task_outcomes_task_id ON agent_task_outcomes(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_task_outcomes_agent_id ON agent_task_outcomes(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_task_outcomes_timestamp ON agent_task_outcomes(timestamp);
`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Create agent_errors table for error tracking
	errorsSchema := `
CREATE TABLE IF NOT EXISTS agent_errors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    error_type TEXT,
    error_message TEXT,
    file_path TEXT,
    line_number INTEGER,
    stack_trace TEXT,
    resolved BOOLEAN,
    resolution_method TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_errors_task_id ON agent_errors(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_errors_agent_id ON agent_errors(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_errors_timestamp ON agent_errors(timestamp);
`
	_, err = db.Exec(errorsSchema)
	return err
}

// RecordAgentTask queues an agent task metric for asynchronous writing.
func (c *TaskCollector) RecordAgentTask(m *AgentTaskMetrics) error {
	select {
	case c.flushQueue <- m:
		return nil
	default:
		c.logger.Warn("flush queue full, dropping metric")
		return fmt.Errorf("flush queue full")
	}
}

// flushLoop runs the background flush loop.
func (c *TaskCollector) flushLoop() {
	for {
		select {
		case <-c.flushTicker.C:
			c.flush()
		case <-c.stopChan:
			c.flush() // Final flush
			return
		}
	}
}

// flush writes queued metrics to the database.
func (c *TaskCollector) flush() {
	var metrics []*AgentTaskMetrics

	// Drain the queue
	for {
		select {
		case m := <-c.flushQueue:
			metrics = append(metrics, m)
		default:
			goto flush
		}
	}

flush:
	if len(metrics) == 0 {
		return
	}

	tx, err := c.db.Begin()
	if err != nil {
		c.logger.Error("failed to begin transaction", "error", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO agent_task_outcomes (
			task_id, agent_id, skill_name, status, success,
			iterations, duration_ms, tokens_input, tokens_output,
			estimated_cost_cents, response_well_formed, syntax_errors_count,
			indentation_errors_count, lazy_response_detected, context_exhausted,
			reflection_iterations, reflection_successful, user_interventions,
			model_id, edit_format
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		c.logger.Error("failed to prepare statement", "error", err)
		return
	}
	defer stmt.Close()

	for _, m := range metrics {
		_, err := stmt.Exec(
			m.TaskID, m.AgentID, m.SkillName, m.Status, m.Success,
			m.Iterations, m.DurationMs, m.TokensInput, m.TokensOutput,
			m.EstimatedCostCents, m.ResponseWellFormed, m.SyntaxErrors,
			m.IndentationErrors, m.LazyResponse, m.ContextExhausted,
			m.ReflectionIterations, m.ReflectionSuccess, m.UserInterventions,
			m.ModelID, m.EditFormat,
		)
		if err != nil {
			c.logger.Error("failed to insert metric", "task_id", m.TaskID, "error", err)
		}
	}

	if err := tx.Commit(); err != nil {
		c.logger.Error("failed to commit transaction", "error", err)
	}
}

// Shutdown stops the task collector and flushes pending metrics.
func (c *TaskCollector) Shutdown() {
	select {
	case <-c.stopChan:
		// Already closed
	default:
		close(c.stopChan)
	}
	c.flushTicker.Stop()
	c.wg.Wait()
	_ = c.db.Close()
}
