package sync

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Handler listens to message bus events and triggers sync operations.
// It subscribes to queue.job.claimed and queue.job.completed events.
type Handler struct {
	manager *SyncManager
	bus     *bus.MessageBus
	logger  *slog.Logger

	cancel context.CancelFunc
}

// NewHandler creates a new sync event handler.
func NewHandler(manager *SyncManager, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		manager: manager,
		bus:     msgBus,
		logger:  logger,
	}
}

// Start begins listening for sync-relevant events.
func (h *Handler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	// Subscribe to job lifecycle events
	claimedSub := h.bus.Subscribe("sync-handler", "queue.job.claimed")
	completedSub := h.bus.Subscribe("sync-handler", "queue.job.completed")

	go func() {
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(claimedSub)
				h.bus.Unsubscribe(completedSub)
				return

			case msg, ok := <-claimedSub.Channel:
				if !ok {
					return
				}
				h.handleJobClaimed(ctx, msg)

			case msg, ok := <-completedSub.Channel:
				if !ok {
					return
				}
				h.handleJobCompleted(ctx, msg)
			}
		}
	}()

	h.logger.Info("Sync handler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// handleJobClaimed processes a job claimed event.
func (h *Handler) handleJobClaimed(ctx context.Context, msg *models.BusMessage) {
	var payload JobEventPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Warn("Failed to parse job claimed event", "error", err)
		return
	}

	h.logger.Debug("Processing job claimed event",
		"job_id", payload.JobID,
		"task_id", payload.TaskID,
	)

	if err := h.manager.HandleJobClaimed(ctx, payload.JobID, payload.TaskID); err != nil {
		h.logger.Warn("Hydration failed",
			"job_id", payload.JobID,
			"error", err,
		)
	}
}

// handleJobCompleted processes a job completed event.
func (h *Handler) handleJobCompleted(ctx context.Context, msg *models.BusMessage) {
	var payload JobEventPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		h.logger.Warn("Failed to parse job completed event", "error", err)
		return
	}

	h.logger.Debug("Processing job completed event",
		"job_id", payload.JobID,
		"task_id", payload.TaskID,
		"agent_id", payload.AgentID,
	)

	if err := h.manager.HandleJobCompleted(ctx, payload.JobID, payload.TaskID, payload.AgentID); err != nil {
		h.logger.Warn("Distillation failed",
			"job_id", payload.JobID,
			"error", err,
		)
	}
}

// JobEventPayload is the expected format for job lifecycle events.
// This helps document the expected message format.
type JobEventPayload struct {
	JobID    string `json:"job_id"`
	TaskID   string `json:"task_id,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
	WorkerID string `json:"worker_id,omitempty"`
}
