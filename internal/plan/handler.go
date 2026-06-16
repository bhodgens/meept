package plan

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// PlanHandler subscribes to task events and routes progress to PlanManager.
type PlanHandler struct {
	manager *PlanManager
	bus     *bus.MessageBus
	logger  *slog.Logger
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(manager *PlanManager, bus *bus.MessageBus, logger *slog.Logger) *PlanHandler {
	return &PlanHandler{
		manager: manager,
		bus:     bus,
		logger:  logger,
	}
}

// Start subscribes to task events and begins routing progress to PlanManager.
func (h *PlanHandler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	topics := map[string]func(context.Context, *models.BusMessage){
		"task.step_completed": h.handleStepCompleted,
		"task.completed":      h.handleTaskCompleted,
	}

	for topic, handler := range topics {
		sub := h.bus.Subscribe("plan-handler-"+topic, topic)
		h.wg.Add(1)
		go h.runSubscription(ctx, sub, handler)
	}

	h.logger.Info("plan handler started")
	return nil
}

// Stop cancels all subscriptions and waits for goroutines to finish.
func (h *PlanHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

// runSubscription drains a subscriber channel, dispatching each message to the
// provided handler until the context is cancelled or the channel is closed.
func (h *PlanHandler) runSubscription(ctx context.Context, sub *bus.Subscriber, handler func(context.Context, *models.BusMessage)) {
	defer h.wg.Done()
	defer h.bus.Unsubscribe(sub)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			handler(ctx, msg)
		}
	}
}

func (h *PlanHandler) handleStepCompleted(ctx context.Context, msg *models.BusMessage) {
	var payload struct {
		TaskID string `json:"task_id"`
		StepID string `json:"step_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Error("plan handler: failed to parse step completed event", "error", err)
		return
	}
	// Retry transient (SQLite busy/locked) errors with exponential backoff
	// before giving up and logging (S2-16).
	backoffs := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}
	var lastErr error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		err := h.manager.OnStepCompleted(ctx, payload.TaskID, payload.StepID)
		if err == nil {
			return
		}
		lastErr = err
		if !isTransientDBError(err) {
			break
		}
		if attempt < len(backoffs) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoffs[attempt]):
			}
		}
	}
	if lastErr != nil {
		h.logger.Error("plan handler: failed to process step completed",
			"task_id", payload.TaskID, "error", lastErr)
	}
}

// isTransientDBError reports whether err is a transient SQLite error worth retrying.
func isTransientDBError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY")
}

func (h *PlanHandler) handleTaskCompleted(ctx context.Context, msg *models.BusMessage) {
	var payload struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Error("plan handler: failed to parse task completed event", "error", err)
		return
	}
	if err := h.manager.OnTaskCompleted(ctx, payload.TaskID); err != nil {
		h.logger.Error("plan handler: failed to process task completed", "task_id", payload.TaskID, "error", err)
	}
}
