package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bus"
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
	PushPriorityLow     PushPriority = "low"
	PushPriorityNormal  PushPriority = "normal"
	PushPriorityHigh    PushPriority = "high"
	PushPriorityUrgent  PushPriority = "urgent"
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

// Push sends a push notification to the requested session(s).
//
// The message is published on the internal message bus on a per-session
// topic so that all subscribers (TUI, menubar, HTTP clients, adapter
// services) can react.
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
			return &result, err
		}
		s.logPush(sessID, req)
		s.bus.Publish("push."+sessID, busMsg)
		result.Delivered++
	}

	s.logger.Debug("push delivered",
		"id", msgID,
		"delivered", result.Delivered,
		"skipped", result.Skipped,
		"type", req.Type,
		"priority", req.Priority,
	)
	return &result, nil
}

func (s *PushService) logPush(sessID string, req *PushRequest) {
	s.logger.Debug("push delivered to bus",
		"session_id", sessID,
		"type", req.Type,
		"priority", req.Priority,
		"source", req.Source,
	)
}
