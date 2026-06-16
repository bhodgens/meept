// Package http provides HTTP handlers for notification events.
package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTypeInfo    NotificationType = "info"
	NotificationTypeSuccess NotificationType = "success"
	NotificationTypeWarning NotificationType = "warning"
	NotificationTypeError   NotificationType = "error"
)

// NotificationEvent represents a notification event sent to clients.
type NotificationEvent struct {
	ID        string                 `json:"id"`
	Timestamp string                 `json:"timestamp"` // RFC3339
	Type      NotificationType      `json:"type"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	TaskID    string                 `json:"task_id,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
}

// NotificationEmitter is an interface for publishing and subscribing to notification events.
type NotificationEmitter interface {
	Subscribe() chan *NotificationEvent
	Unsubscribe(ch chan *NotificationEvent)
	GetEventsSince(t time.Time) []*NotificationEvent
}

// NotificationHandler handles notification event requests.
type NotificationHandler struct {
	emitter NotificationEmitter
	logger  *slog.Logger
}

// NewNotificationHandler creates a new notification handler.
func NewNotificationHandler(emitter NotificationEmitter, logger *slog.Logger) *NotificationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationHandler{
		emitter: emitter,
		logger:  logger,
	}
}

// ServeWebSocket handles WebSocket connections for real-time notifications.
func (h *NotificationHandler) ServeWebSocket(w http.ResponseWriter, req *http.Request) {
	// Auth is enforced by middleware.

	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		CompressionMode:     websocket.CompressionContextTakeover,
		OriginPatterns:      defaultWSOrigins,
		InsecureSkipVerify:  true, // Allow non-TLS for localhost
		})
	if err != nil {
		h.logger.Error("failed to accept websocket connection", "error", err)
		http.Error(w, "failed to accept connection", http.StatusBadRequest)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	// Subscribe to events
	eventChan := h.emitter.Subscribe()
	defer h.emitter.Unsubscribe(eventChan)

	h.logger.Debug("notification websocket connected", "remote", req.RemoteAddr)

	// Send initial events from buffer
	events := h.emitter.GetEventsSince(time.Time{})
	for _, event := range events {
		if err := h.sendEvent(req.Context(), conn, event); err != nil {
			h.logger.Warn("failed to send initial event", "error", err)
			break
		}
	}

	// Wait for events and send to client
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				h.logger.Debug("notification channel closed")
				return
			}
			if err := h.sendEvent(req.Context(), conn, event); err != nil {
				h.logger.Warn("failed to send event", "error", err)
				return
			}
		case <-req.Context().Done():
			h.logger.Debug("notification websocket context cancelled")
			return
		}
	}
}

// sendEvent sends a notification event over WebSocket.
func (h *NotificationHandler) sendEvent(ctx context.Context, conn *websocket.Conn, event *NotificationEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// ServeHTTP handles HTTP polling for notifications.
func (h *NotificationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET method
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse 'since' parameter
	var since time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			http.Error(w, "invalid 'since' parameter: use RFC3339 format", http.StatusBadRequest)
			return
		}
	} else {
		// Default to 1 hour ago
		since = time.Now().Add(-1 * time.Hour)
	}

	events := h.emitter.GetEventsSince(since)

	w.Header().Set("Content-Type", "application/json")
	origin := r.Header.Get("Origin")
	if origin == "" || isLocalOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"count":  len(events),
	}); err != nil {
		h.logger.Error("failed to encode notifications response", "error", err)
	}
}