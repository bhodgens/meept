package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// PushType represents the category of a push notification.
type PushType string

const (
	PushTypeNotification PushType = "notification"
	PushTypeAlert        PushType = "alert"
	PushTypeSummary      PushType = "summary"
)

// PushPriority represents the urgency of a push notification.
type PushPriority string

const (
	PushPriorityLow    PushPriority = "low"
	PushPriorityNormal PushPriority = "normal"
	PushPriorityHigh   PushPriority = "high"
	PushPriorityUrgent PushPriority = "urgent"
)

// PushService handles bot-to-user push notifications over the message bus
// and registered push channels (Telegram, CLI, TUI, HTTP).
//
// Messages are published as bus events on per-session topics so that any
// subscriber (TUI, menubar, web, Telegram adapter, etc.) can pick them up.
type PushService struct {
	bus      *bus.MessageBus
	channels *ChannelRegistry
	logger   *slog.Logger
	// effects is the external-effect idempotency ledger. Nil (tests,
	// feature off) means Push runs its legacy unclaimed path.
	effects effects.Ledger
}

// PushRequest describes a push notification to send.
type PushRequest struct {
	// SessionIDs identifies the recipient sessions. Empty means all sessions.
	SessionIDs []string
	// Source identifies the subsystem originating the push.
	Source string
	// Type categorizes the notification.
	Type PushType
	// Content is the human-readable body of the notification.
	Content string
	// Priority indicates urgency.
	Priority PushPriority
	// Extra is an optional map attached to the payload for downstream consumers.
	Extra map[string]any `json:"-"`
}

// PushResult holds the outcome of a push operation.
type PushResult struct {
	Delivered int `json:"delivered"`
	Skipped   int `json:"skipped"`
}

// NewPushService creates a push service with bus-only delivery.
// For channel routing (Telegram, CLI, TUI, HTTP), use NewPushServiceWithChannels.
func NewPushService(
	sessionMgr session.Store,
	msgBus *bus.MessageBus,
	logger *slog.Logger,
	_ ...PushServiceOption,
) *PushService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PushService{
		bus:    msgBus,
		logger: logger,
	}
}

// NewPushServiceWithChannels creates a push service with channel routing.
func NewPushServiceWithChannels(
	msgBus *bus.MessageBus,
	channels *ChannelRegistry,
	logger *slog.Logger,
) *PushService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PushService{
		bus:      msgBus,
		channels: channels,
		logger:   logger,
	}
}

// PushToChannels sends a push notification through all registered channels.
// This method bypasses the bus and delivers directly to channel transports.
func (s *PushService) PushToChannels(ctx context.Context, req *PushRequest) (*PushResult, error) {
	if req == nil {
		return nil, wrapError("push", "PushToChannels", ErrInvalidInput)
	}
	if req.Content == "" {
		return nil, wrapError("push", "PushToChannels", ErrInvalidInput)
	}
	if s.channels == nil {
		return nil, wrapError("push", "PushToChannels", ErrUnavailable)
	}

	if req.Source == "" {
		req.Source = "svc.push"
	}
	if req.Type == "" {
		req.Type = PushTypeNotification
	}
	if req.Priority == "" {
		req.Priority = PushPriorityNormal
	}

	msg := &PushMessage{
		SessionID: req.SessionIDs[0],
		Source:    req.Source,
		Type:      req.Type,
		Priority:  req.Priority,
		Content:   req.Content,
		Timestamp: time.Now(),
		Metadata:  req.Extra,
	}

	delivered := s.channels.Push(ctx, msg)
	return &PushResult{Delivered: delivered}, nil
}

// PushServiceOption configures a PushService.
type PushServiceOption func(*PushService)

// SetEffectsLedger wires the external-effect ledger. When nil (tests,
// ledger disabled), Push keeps its legacy behavior exactly — no claiming.
func (s *PushService) SetEffectsLedger(l effects.Ledger) {
	if l != nil {
		s.effects = l
	}
}

// pushEffectsKey derives the pinned effect key for a push. Fixed order
// (leaf 02 Contract): tool name, marshaled session list, source, type,
// content. Content participates because two pushes with identical
// body/source/sessions ARE the same user-visible effect; sha256 handles
// length, so nothing is truncated.
func pushEffectsKey(req *PushRequest) (string, error) {
	sessions := req.SessionIDs
	if sessions == nil {
		sessions = []string{}
	}
	sessionsJSON, err := json.Marshal(sessions)
	if err != nil {
		return "", fmt.Errorf("push effect key: marshal sessions: %w", err)
	}
	return effects.EffectKey("push.notify", string(sessionsJSON), req.Source, string(req.Type), req.Content), nil
}

// publishToSessions performs the actual bus delivery: one publish on
// "push.<session>" per requested session plus the fan-out event on
// "push.notify". Returns the delivered count. Extracted verbatim from the
// legacy Push body so the ledger-claiming path and the nil-ledger path
// run identical delivery code.
func (s *PushService) publishToSessions(ctx context.Context, req *PushRequest, msgID string, payloadBytes []byte) (PushResult, error) {
	busMsg := &models.BusMessage{
		ID:      msgID,
		Type:    models.MessageTypeEvent,
		Topic:   "push.notify",
		Source:  "svc.push",
		Payload: payloadBytes,
	}

	var result PushResult

	for _, sessID := range req.SessionIDs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		s.logPush(sessID, req)
		s.bus.Publish("push."+sessID, busMsg)
		result.Delivered++
	}

	return result, nil
}

// Push sends a push notification to the requested session(s).
//
// The message is published on the internal message bus on a per-session
// topic so that all subscribers (TUI, menubar, HTTP clients, adapter
// services) can react.
//
// When an effects ledger is wired, the delivery is wrapped in the
// pinned protocol (Claim -> execute -> RecordReceipt -> Complete): a
// duplicate Push with identical inputs returns the prior receipt as an
// idempotent no-op without re-publishing. Push declares
// ProviderIdempotent=false — re-delivery would double-render in
// TUI/Telegram — so reconcile surfaces its stuck records, never
// auto-retries them.
func (s *PushService) Push(ctx context.Context, req *PushRequest) (*PushResult, error) {
	if req == nil {
		return nil, wrapError("push", "Push", ErrInvalidInput)
	}
	if req.Content == "" {
		return nil, wrapError("push", "Push", ErrInvalidInput)
	}
	if s.bus == nil {
		return nil, wrapError("push", "Push", ErrUnavailable)
	}

	if req.Source == "" {
		req.Source = "svc.push"
	}
	if req.Type == "" {
		req.Type = PushTypeNotification
	}
	if req.Priority == "" {
		req.Priority = PushPriorityNormal
	}

	msgID := id.Generate("push-")

	payload := map[string]any{
		"push_id":   msgID,
		"type":      string(req.Type),
		"priority":  string(req.Priority),
		"content":   req.Content,
		"source":    req.Source,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range req.Extra {
		payload[k] = v
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, wrapError("push", "Push", err)
	}

	// Nil ledger (tests, feature off): legacy path, byte-for-byte behavior.
	if s.effects == nil {
		result, pubErr := s.publishToSessions(ctx, req, msgID, payloadBytes)
		if pubErr != nil {
			return &result, pubErr
		}
		s.logPushResult(msgID, req, result)
		return &result, nil
	}

	// Ledger-claiming path: Claim -> publish -> RecordReceipt -> Complete.
	// Payload is the request identity the reconciler needs to surface (or,
	// for future provider-idempotent tools, rebuild) the effect.
	reqPayload, err := json.Marshal(map[string]any{
		"session_ids": req.SessionIDs,
		"source":      req.Source,
		"type":        req.Type,
		"priority":    req.Priority,
		"content":     req.Content,
	})
	if err != nil {
		return nil, wrapError("push", "Push", fmt.Errorf("marshal effect payload: %w", err))
	}

	key, err := pushEffectsKey(req)
	if err != nil {
		return nil, wrapError("push", "Push", err)
	}

	receipt, reused, err := effects.Run(ctx, s.effects, key, effects.EffectMeta{
		Tool:               "push.notify",
		ProviderIdempotent: false,
		Payload:            reqPayload,
	}, func(ctx context.Context) (json.RawMessage, error) {
		result, pubErr := s.publishToSessions(ctx, req, msgID, payloadBytes)
		if pubErr != nil {
			return nil, pubErr
		}
		return json.Marshal(map[string]any{
			"push_id":   msgID,
			"delivered": result.Delivered,
			"sessions":  req.SessionIDs,
		})
	})
	if err != nil {
		return nil, wrapError("push", "Push", fmt.Errorf("effects: %w", err))
	}
	if reused {
		// Completed prior: idempotent no-op. Parse the prior receipt so the
		// duplicate caller is indistinguishable in success semantics from
		// the first; do NOT publish again.
		var prior struct {
			Delivered int `json:"delivered"`
		}
		if err := json.Unmarshal(receipt, &prior); err != nil {
			return nil, wrapError("push", "Push", fmt.Errorf("parse prior push receipt: %w", err))
		}
		s.logger.Debug("push already delivered (effects ledger no-op)",
			"key", key,
			"delivered", prior.Delivered,
		)
		return &PushResult{Delivered: prior.Delivered}, nil
	}

	// This caller executed the effect: write the completion marker.
	if err := s.effects.Complete(ctx, key); err != nil {
		return nil, wrapError("push", "Push", fmt.Errorf("effects complete %s: %w", key, err))
	}

	var receiptOut struct {
		Delivered int `json:"delivered"`
	}
	if err := json.Unmarshal(receipt, &receiptOut); err != nil {
		return nil, wrapError("push", "Push", fmt.Errorf("parse push receipt: %w", err))
	}
	result := PushResult{Delivered: receiptOut.Delivered}
	s.logPushResult(msgID, req, result)
	return &result, nil
}

func (s *PushService) logPushResult(msgID string, req *PushRequest, result PushResult) {
	s.logger.Debug("push delivered",
		"id", msgID,
		"delivered", result.Delivered,
		"skipped", result.Skipped,
		"type", req.Type,
		"priority", req.Priority,
	)
}

func (s *PushService) logPush(sessID string, req *PushRequest) {
	s.logger.Debug("push delivered to bus",
		"session_id", sessID,
		"type", req.Type,
		"priority", req.Priority,
		"source", req.Source,
	)
}
