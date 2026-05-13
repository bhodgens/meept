package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"golang.org/x/net/websocket"
)

// WSMessage represents a message sent/received over WebSocket.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// WebSocketHub manages WebSocket client connections and broadcasts messages.
//nolint:revive // stutter with package name is intentional for API clarity
type WebSocketHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	logger  *slog.Logger
}

// NewWebSocketHub creates a new WebSocket hub.
func NewWebSocketHub(logger *slog.Logger) *WebSocketHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebSocketHub{
		clients: make(map[*websocket.Conn]struct{}),
		logger:  logger,
	}
}

// Register adds a WebSocket client connection.
func (h *WebSocketHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	h.logger.Debug("ws client registered", "remote", conn.RemoteAddr())
}

// Unregister removes a WebSocket client connection and closes it.
func (h *WebSocketHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
	h.logger.Debug("ws client unregistered", "remote", conn.RemoteAddr())
}

// ClientCount returns the number of connected clients.
func (h *WebSocketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends a typed message to all connected WebSocket clients.
func (h *WebSocketHub) Broadcast(msgType string, data any) {
	payload, err := json.Marshal(map[string]any{
		"type": msgType,
		"data": data,
	})
	if err != nil {
		h.logger.Error("ws broadcast marshal error", "error", err)
		return
	}

	// Collect failed connections to unregister after releasing the read lock
	var failedConns []*websocket.Conn

	h.mu.RLock()
	for conn := range h.clients {
		if _, err := conn.Write(payload); err != nil {
			h.logger.Warn("ws write error, will remove client", "error", err)
			failedConns = append(failedConns, conn)
		}
	}
	h.mu.RUnlock()

	// Unregister failed connections outside the read lock to avoid deadlock
	for _, conn := range failedConns {
		h.Unregister(conn)
	}
}

// handleWebSocket upgrades an HTTP connection to WebSocket and manages the lifecycle.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	wsHandler := websocket.Handler(func(conn *websocket.Conn) {
		s.wsHub.Register(conn)
		defer s.wsHub.Unregister(conn)

		for {
			var msg WSMessage
			if err := websocket.JSON.Receive(conn, &msg); err != nil {
				// Client disconnected or error
				return
			}
			s.handleWSMessage(conn, &msg)
		}
	})

	wsHandler.ServeHTTP(w, r)
}

// handleWSMessage processes incoming WebSocket messages.
func (s *Server) handleWSMessage(conn *websocket.Conn, msg *WSMessage) {
	switch msg.Type {
	case "ping":
		_ = websocket.JSON.Send(conn, WSMessage{Type: "pong"})
	case "subscribe":
		// Client subscribes to real-time updates; already registered in hub.
		_ = websocket.JSON.Send(conn, WSMessage{Type: "subscribed"})
	default:
		s.logger.Debug("ws unknown message type", "type", msg.Type)
	}
}
