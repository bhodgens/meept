// Package memory provides memory storage and retrieval for meept.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// Memory boundary markers used to wrap retrieved content so downstream LLM
// consumers can distinguish stored memory from live instructions. The opening
// marker includes the memory type so policy engines and prompt-injection
// detectors can apply type-specific handling.
const (
	memoryContentOpenFmt  = "<<<MEMORY_CONTENT:%s>>>\n"
	memoryContentClose    = "\n<<<END_MEMORY_CONTENT>>>"
	memoryContentOpenTmpl = "<<<MEMORY_CONTENT:%s>>>" //nolint:unused // reserved for external callers
)

// Handler bridges the message bus to the MemoryManager.
// It subscribes to memory.query and memory.recent, responding with memory.result.
//
// When a SecurityOrchestrator is wired (via SetSecurityOrchestrator or
// NewHandlerWithSecurity), retrieved memory content is:
//  1. Re-sanitized through InputSanitizer to catch patterns added after the
//     memory was originally stored.
//  2. Wrapped in boundary markers so downstream LLM context can distinguish
//     stored memory from live user/system instructions.
type Handler struct {
	manager *Manager
	bus     *bus.MessageBus
	logger  *slog.Logger
	cancel  context.CancelFunc

	secOrch *intsecurity.Orchestrator
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

// NewHandlerWithSecurity creates a new memory handler with security protection
// (re-sanitization and boundary wrapping) enabled.
func NewHandlerWithSecurity(manager *Manager, msgBus *bus.MessageBus, secOrch *intsecurity.Orchestrator, logger *slog.Logger) *Handler {
	h := NewHandler(manager, msgBus, logger)
	if secOrch != nil {
		h.secOrch = secOrch
	}
	return h
}

// SetSecurityOrchestrator wires a security orchestrator for retrieval-time
// re-sanitization. Nil is accepted and simply disables protection.
func (h *Handler) SetSecurityOrchestrator(secOrch *intsecurity.Orchestrator) {
	if secOrch != nil {
		h.secOrch = secOrch
	}
}

// protectContent applies retrieval-time defense to memory content:
//  1. Re-sanitize through InputSanitizer (if wired) to catch injection patterns
//     that may have been stored before the pattern DB was updated.
//  2. Wrap content in boundary markers so LLM context consumers can distinguish
//     stored memory from live instructions.
//
// The memoryType label is embedded in the opening boundary marker.
// Returns the protected (possibly sanitized + wrapped) content.
func (h *Handler) protectContent(content string, memoryType MemoryType) string {
	if content == "" {
		return content
	}

	// Layer 1: Re-sanitize on retrieval.
	if h.secOrch != nil {
		if sanitizer := h.secOrch.InputSanitizer(); sanitizer != nil {
			result := sanitizer.Sanitize(content)
			if result.WasModified || len(result.ThreatsDetected) > 0 {
				h.logger.Debug("Memory content sanitized on retrieval",
					"memory_type", string(memoryType),
					"threats", len(result.ThreatsDetected),
					"was_modified", result.WasModified)
			}
			content = result.CleanText
		}
	}

	// Layer 2: Boundary-marker wrapping.
	wrapped := fmt.Sprintf(memoryContentOpenFmt, string(memoryType)) +
		content +
		memoryContentClose
	return wrapped
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
// Each result's content is passed through protectContent before publication
// so that downstream consumers receive boundary-wrapped, re-sanitized text.
func (h *Handler) sendResults(replyTo string, results []MemoryResult) {
	// Convert to a simpler format for the response
	items := make([]map[string]any, len(results))
	for i, r := range results {
		protectedContent := h.protectContent(r.Memory.Content, r.Memory.Type)
		items[i] = map[string]any{
			"id":              r.Memory.ID,
			"content":         protectedContent,
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
