// Package metrics provides metrics collection and storage for Meept.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Collector collects metrics from various sources.
type Collector struct {
	store          *Store
	bus            *bus.MessageBus
	stopChan       chan struct{}
	wg             sync.WaitGroup
	getQueueDepth  func() int
	getActiveAgents func() int
}

// CollectorConfig configures the metrics collector.
type CollectorConfig struct {
	CollectionInterval time.Duration
	Enabled            bool
	GetQueueDepth      func() int  // Returns total pending + claimed jobs
	GetActiveAgents    func() int  // Returns active agent count
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
		store:          store,
		bus:            messageBus,
		stopChan:       make(chan struct{}),
		getQueueDepth:  cfg.GetQueueDepth,
		getActiveAgents: cfg.GetActiveAgents,
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
		"model": model,
	})
	c.store.Record("tokens.input", float64(inputTokens), map[string]string{
		"model": model,
	})
	c.store.Record("tokens.output", float64(outputTokens), map[string]string{
		"model": model,
	})
	c.store.Record("llm.latency", latency.Seconds(), map[string]string{
		"model": model,
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
func (c *Collector) RecordModelResolution(modelID string, provider string) {
	c.store.Record("model.resolutions", 1, map[string]string{
		"model_id": modelID,
		"provider": provider,
	})
}

// RecordReviewResult records a review result metric.
func (c *Collector) RecordReviewResult(status string, reviewerID string, confidence float64) {
	c.store.Record("review.completed", 1, map[string]string{
		"status":   status,
		"reviewer": reviewerID,
	})
	c.store.Record("review.confidence", confidence, map[string]string{
		"reviewer": reviewerID,
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
			"reviewer": payload.Reviewer,
		})
	}

	c.store.RecordEvent("review.completed", "info",
		fmt.Sprintf("Review %s by %s (confidence %.2f, revisions %d)", payload.Status, payload.Reviewer, payload.Confidence, payload.RevisionCount),
		map[string]any{
			"status":         payload.Status,
			"reviewer":       payload.Reviewer,
			"confidence":     payload.Confidence,
			"revision_count": payload.RevisionCount,
		},
	)
}

// Shutdown stops the collector.
func (c *Collector) Shutdown() {
	close(c.stopChan)
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

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
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
	}()

	return c
}

// Shutdown stops the periodic collector.
func (c *PeriodicCollector) Shutdown() {
	close(c.stopChan)
	c.wg.Wait()
}
