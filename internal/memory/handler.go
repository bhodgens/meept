// Package memory provides memory storage and retrieval for meept.
package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// Handler bridges the message bus to the MemoryManager.
// It subscribes to memory.query and memory.recent, responding with memory.result.
type Handler struct {
	manager *Manager
	bus     *bus.MessageBus
	logger  *slog.Logger
	cancel  context.CancelFunc
}

// NewHandler creates a new memory handler.
func NewHandler(manager *Manager, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		manager: manager,
		bus:     msgBus,
		logger:  logger,
	}
}

// Start begins listening for memory requests.
func (h *Handler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	// Subscribe to memory query requests
	querySub := h.bus.Subscribe("memory-handler", "memory.query")
	recentSub := h.bus.Subscribe("memory-handler", "memory.recent")

	go func() {
		for {
			select {
			case <-ctx.Done():
				h.bus.Unsubscribe(querySub)
				h.bus.Unsubscribe(recentSub)
				return
			case msg, ok := <-querySub.Channel:
				if !ok {
					return
				}
				h.handleQuery(ctx, msg)
			case msg, ok := <-recentSub.Channel:
				if !ok {
					return
				}
				h.handleRecent(ctx, msg)
			}
		}
	}()

	h.logger.Info("MemoryHandler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// handleQuery processes memory search requests.
func (h *Handler) handleQuery(ctx context.Context, msg *models.BusMessage) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		h.logger.Warn("Failed to parse memory query", "error", err)
		h.sendError(msg.ID, "invalid request format: "+err.Error())
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	// If query is empty or "*", return recent memories instead
	if req.Query == "" || req.Query == "*" {
		results, err := h.manager.GetRecent(ctx, req.Limit)
		if err != nil {
			h.logger.Warn("Failed to get recent memories", "error", err)
			h.sendError(msg.ID, "failed to get recent memories: "+err.Error())
			return
		}
		h.sendResults(msg.ID, results)
		return
	}

	// Search memories
	results, err := h.manager.Search(ctx, MemoryQuery{
		Query: req.Query,
		Limit: req.Limit,
	})
	if err != nil {
		h.logger.Warn("Memory search failed", "error", err)
		h.sendError(msg.ID, "search failed: "+err.Error())
		return
	}

	h.sendResults(msg.ID, results)
}

// handleRecent returns the most recent memories.
func (h *Handler) handleRecent(ctx context.Context, msg *models.BusMessage) {
	var req struct {
		Limit int `json:"limit"`
	}

	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		// Default to 10 if parsing fails
		req.Limit = 10
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, err := h.manager.GetRecent(ctx, req.Limit)
	if err != nil {
		h.logger.Warn("Failed to get recent memories", "error", err)
		h.sendError(msg.ID, "failed to get recent memories: "+err.Error())
		return
	}

	h.sendResults(msg.ID, results)
}

// sendResults publishes memory results.
func (h *Handler) sendResults(replyTo string, results []MemoryResult) {
	// Convert to a simpler format for the response
	items := make([]map[string]any, len(results))
	for i, r := range results {
		items[i] = map[string]any{
			"id":              r.Memory.ID,
			"content":         r.Memory.Content,
			"type":            string(r.Memory.Type),
			"memory_type":     string(r.Memory.Type), // Alias for compatibility
			"category":        r.Memory.Category,
			"relevance_score": r.RelevanceScore,
			"created_at":      r.Memory.CreatedAt.Format(time.RFC3339),
			"source":          r.Source,
			"metadata":        r.Memory.Metadata,
		}
	}

	response := map[string]any{
		"results": items,
		"items":   items, // Alias for compatibility
		"count":   len(items),
	}

	payload, err := json.Marshal(response)
	if err != nil {
		h.logger.Error("Failed to marshal memory results", "error", err)
		return
	}

	// Note: Uses a nanosecond-precision timestamp as the message ID.
	// Collision is theoretically possible if two messages are generated in the same nanosecond,
	// but the probability is near-zero in practice. UUID is used elsewhere in the memory system
	// for identification and comparison where uniqueness is critical.
	respMsg := &models.BusMessage{
		ID:        id.Generate("memory-resp-"),
		Type:      models.MessageTypeResponse,
		Topic:     "memory.result",
		Source:    "memory-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish("memory.result", respMsg)
}

// sendError publishes an error response.
func (h *Handler) sendError(replyTo, errMsg string) {
	response := map[string]any{
		"error":   errMsg,
		"results": []any{},
		"items":   []any{},
		"count":   0,
	}

	payload, _ := json.Marshal(response)

	// Note: Uses a nanosecond-precision timestamp as the message ID.
	// Collision is theoretically possible if two messages are generated in the same nanosecond,
	// but the probability is near-zero in practice. UUID is used elsewhere in the memory system
	// for identification and comparison where uniqueness is critical.
	respMsg := &models.BusMessage{
		ID:        id.Generate("memory-resp-"),
		Type:      models.MessageTypeResponse,
		Topic:     "memory.result",
		Source:    "memory-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish("memory.result", respMsg)
}
