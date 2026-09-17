package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// chatSubmitAck is the shape of the chat.submit ack. NEVER blocks on agent
// work — the result reaches clients later via the turn.terminal event
// (async-turn-migration leaf 01).
//
// The map key order below is the documented wire contract; JSON objects are
// unordered so only the KEY SET is binding:
//
//	{"turn_id", "conversation_id", "session_id", "accepted", "note"}
//
// Shared by the "chat.submit" RPC handler and the HTTP POST
// /api/v1/chat/submit endpoint (leaf 04 of the same plan) — both call
// BuildChatSubmitAck so validation and ack semantics stay identical.
//
// Wire note (import direction): the payload is built as a map with exactly
// agent.ChatRequest's JSON keys — internal/rpc must NOT import
// internal/agent, and makeProxy already passes opaque payloads, so the JSON
// key set IS the wire contract.

// ChatSubmitRequest is the parsed chat.submit params. Field names mirror
// agent.ChatRequest's JSON keys one-for-one.
type ChatSubmitRequest struct {
	Message        string            `json:"message"`
	SessionID      string            `json:"session_id,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	AgentID        string            `json:"agent_id,omitempty"`
	SourceClient   string            `json:"source_client,omitempty"`
	Parts          []json.RawMessage `json:"parts,omitempty"`
	Model          string            `json:"model,omitempty"`
	TurnID         string            `json:"turn_id,omitempty"`
}

// chatSubmitRegistry is the slice of TurnRegistry behavior the submit path
// needs, defined locally so internal/rpc never imports internal/agent (the
// *agent.TurnRegistry satisfies it structurally in the daemon wiring).
type chatSubmitRegistry interface {
	Register(turnID, conversationID string) bool
}

// SubmitHandler serves the fire-and-forget chat submit surface
// ("chat.submit" RPC). It publishes a chat.request on the bus and returns
// the ack immediately — the ack path has no bus subscribe and no wait.
type SubmitHandler struct {
	bus      *bus.MessageBus
	registry chatSubmitRegistry // optional; nil disables dedupe registration
	logger   *slog.Logger
}

// NewSubmitHandler creates a new handler. bus must be non-nil for useful
// behavior (publish is a no-op-ish drop without subscribers, but nil bus is
// still tolerated by BuildChatSubmitAck callers that only validate).
func NewSubmitHandler(msgBus *bus.MessageBus, registry chatSubmitRegistry, logger *slog.Logger) *SubmitHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SubmitHandler{bus: msgBus, registry: registry, logger: logger}
}

// RegisterSubmitMethods registers chat.submit on the RPC server.
func (h *SubmitHandler) RegisterSubmitMethods(server *Server) {
	server.RegisterHandler("chat.submit", h.handleChatSubmit)
}

// Submit implements http.ChatSubmitter.
func (h *SubmitHandler) Submit(ctx context.Context, params json.RawMessage) (any, error) {
	return h.BuildChatSubmitAck(ctx, params)
}

// handleChatSubmit validates params, publishes chat.request (with turn_id),
// and returns the ack synchronously. It never waits for a response.
func (h *SubmitHandler) handleChatSubmit(ctx context.Context, params json.RawMessage) (any, error) {
	return h.BuildChatSubmitAck(ctx, params)
}

// BuildChatSubmitAck is the single shared validation+publish+ack function
// used by BOTH the RPC handler and the HTTP endpoint (leaf 04). It performs
// no bus subscription and never blocks on agent work.
func (h *SubmitHandler) BuildChatSubmitAck(ctx context.Context, params json.RawMessage) (any, error) {
	var req ChatSubmitRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return h.buildAck(req)
}

// buildAck carries the real work of BuildChatSubmitAck.
func (h *SubmitHandler) buildAck(req ChatSubmitRequest) (any, error) {
	// Validation first: an invalid submit must NOT publish anything and
	// must NOT consume/mint a turn id.
	if strings.TrimSpace(req.Message) == "" {
		return map[string]any{
			"turn_id":         "",
			"conversation_id": req.ConversationID,
			"session_id":      req.SessionID,
			"accepted":        false,
			"note":            "message is required",
		}, nil
	}

	// Turn id: client-supplied explicit turn ids enable idempotent retry
	// dedupe (re-submitting the same turn_id returns the same ack without
	// double-publishing); otherwise a fresh id is minted.
	turnID := req.TurnID
	if turnID == "" {
		turnID = id.Generate("turn-")
	}

	conversationID := req.ConversationID
	if conversationID == "" {
		conversationID = id.Generate("conv-")
	}

	if h.registry != nil {
		if existing := h.registry.Register(turnID, conversationID); existing {
			// Idempotent retry: the turn is already tracked. Do NOT
			// re-publish chat.request — the original submission is either
			// running or queued, and a duplicate would run the work twice.
			h.logger.Info("chat.submit idempotent retry suppressed",
				"turn_id", turnID,
				"conversation_id", conversationID,
			)
			return map[string]any{
				"turn_id":         turnID,
				"conversation_id": conversationID,
				"session_id":      req.SessionID,
				"accepted":        true,
				"note":            "turn already submitted; result arrives via turn.terminal",
			}, nil
		}
	}

	// chat.request payload: exactly agent.ChatRequest's JSON keys. The
	// legacy `chat` consumer (ChatHandler) parses this payload — adding
	// turn_id is additive; every key is set explicitly rather than
	// marshaling the struct so the wire shape is auditable here.
	payload := map[string]any{
		"message":         req.Message,
		"conversation_id": conversationID,
		"session_id":      req.SessionID,
		"agent_id":        req.AgentID,
		"source_client":   req.SourceClient,
		"model":           req.Model,
		"turn_id":         turnID,
	}
	if len(req.Parts) > 0 {
		payload["parts"] = req.Parts
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chat.submit payload: %w", err)
	}

	if h.bus != nil {
		msg := &models.BusMessage{
			ID:      id.Generate("submit-"),
			Type:    models.MessageTypeRequest,
			Topic:   "chat.request",
			Source:  "rpc.submit",
			Payload: payloadBytes,
			ReplyTo: "chat.response",
		}
		h.bus.Publish("chat.request", msg)
	}

	h.logger.Info("chat.submit accepted",
		"turn_id", turnID,
		"conversation_id", conversationID,
		"session_id", req.SessionID,
	)

	return map[string]any{
		"turn_id":         turnID,
		"conversation_id": conversationID,
		"session_id":      req.SessionID,
		"accepted":        true,
		"note":            "accepted; result arrives via turn.terminal",
	}, nil
}
