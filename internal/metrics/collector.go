// Package metrics provides metrics collection and storage for Meept.
package metrics

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
	Data           AgentEventData `json:"data"`
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
	c.wg.Go(func() {
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
	})
}

// subscribeToBus subscribes to relevant bus messages for metrics.
func (c *Collector) subscribeToBus() {
	if c.bus == nil {
		return
	}

	// Subscribe to metrics topic and process messages in a goroutine
	sub := c.bus.Subscribe("metrics-collector", "metrics")
	go func() {
		for msg := range sub.Channel {
			c.handleBusMessage(msg)
		}
	}()

	// Subscribe to review events for review metrics
	reviewSub := c.bus.Subscribe("metrics-collector-review", "step.*")
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

// RecordQueueDepth records the current queue depth.
func (c *Collector) RecordQueueDepth(depth int) {
	c.store.Record("queue.depth", float64(depth), nil)
}

// RecordJobDuration records a job's execution duration.
func (c *Collector) RecordJobDuration(jobName string, duration time.Duration, success bool) {
	tags := map[string]string{
		"job_name": jobName,
	}
	if !success {
		tags["error"] = "true"
	}

	c.store.Record("job.duration", duration.Seconds(), tags)
	c.store.Record("job.completions", 1, tags)
}

// RecordMemoryOperation records a memory operation.
func (c *Collector) RecordMemoryOperation(opType string, duration time.Duration) {
	c.store.Record("memory.operations", 1, map[string]string{
		"operation": opType,
	})
	c.store.Record("memory.duration", duration.Seconds(), map[string]string{
		"operation": opType,
	})
}

// RecordModelResolution records a model resolution event.
func (c *Collector) RecordModelResolution(modelID, provider string) {
	c.store.Record("model.resolutions", 1, map[string]string{
		"model_id": modelID,
		"provider": provider,
	})
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
}

// CollectFunc is a function that collects metrics.
type CollectFunc func()

// PeriodicCollector runs a collection function periodically.
type PeriodicCollector struct {
	fn       CollectFunc
	interval time.Duration
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewPeriodicCollector creates a new periodic collector.
func NewPeriodicCollector(ctx context.Context, fn CollectFunc, interval time.Duration) *PeriodicCollector {
	c := &PeriodicCollector{
		fn:       fn,
		interval: interval,
		stopChan: make(chan struct{}),
	}

	c.wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.fn()
			case <-c.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	return c
}

// Shutdown stops the periodic collector.
func (c *PeriodicCollector) Shutdown() {
	select {
	case <-c.stopChan:
		// Already closed
	default:
		close(c.stopChan)
	}
	c.wg.Wait()
}
