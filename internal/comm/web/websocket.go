package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
)

var wsAllowedOrigins = map[string]struct{}{
	"localhost": {},
	"127.0.0.1": {},
	"::1":       {},
}

func isAllowedWSOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if _, ok := wsAllowedOrigins[strings.ToLower(host)]; ok {
		return true
	}
	if _, ok := wsAllowedOrigins[strings.ToLower(origin)]; ok {
		return true
	}
	return false
}

// WSMessage represents a message sent/received over WebSocket.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// WebSocketHub manages WebSocket client connections and broadcasts messages.
//
//nolint:revive // stutter with package name is intentional for API clarity
type WebSocketHub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	// writeMu guards per-connection writes. golang.org/x/net/websocket
	// requires that no two goroutines call Write concurrently on the same
	// conn. The read loop in handleWSMessage writes pong/subscribed
	// responses while Broadcast may also be writing from another goroutine,
	// so we serialize all hub-initiated writes per-connection.
	writeMu sync.Map // map[*websocket.Conn]*sync.Mutex
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

// connWriteMu returns (creating if needed) the per-connection write mutex.
// Callers must hold h.mu (read or write) OR be in a code path where conn
// is known to still be registered, otherwise the map can grow unbounded
// for transient conns. We always pair this with Unregister cleanup.
func (h *WebSocketHub) connWriteMu(conn *websocket.Conn) *sync.Mutex {
	v, _ := h.writeMu.LoadOrStore(conn, &sync.Mutex{})
	return v.(*sync.Mutex)
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
	h.writeMu.Delete(conn)
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

	// Collect connections under RLock, then release before writing to
	// avoid holding the lock during potentially blocking writes.
	h.mu.RLock()
	conns := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		conns = append(conns, conn)
	}
	// Pre-fetch write mutexes under RLock so we don't race with Unregister
	// deleting them from the map mid-broadcast.
	writeMus := make([]*sync.Mutex, len(conns))
	for i, conn := range conns {
		writeMus[i] = h.connWriteMu(conn)
	}
	h.mu.RUnlock()

	// Write to each connection outside the lock, serialized per-conn.
	var failedConns []*websocket.Conn
	for i, conn := range conns {
		writeMus[i].Lock()
		_, err := conn.Write(payload)
		writeMus[i].Unlock()
		if err != nil {
			h.logger.Warn("ws write error, will remove client", "error", err)
			failedConns = append(failedConns, conn)
		}
	}

	// Unregister failed connections (takes its own write lock).
	for _, conn := range failedConns {
		h.Unregister(conn)
	}
}

// handleWebSocket upgrades an HTTP connection to WebSocket and manages the lifecycle.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	wsServer := &websocket.Server{
		Handler: func(conn *websocket.Conn) {
			s.wsHub.Register(conn)
			defer s.wsHub.Unregister(conn)

			for {
				var msg WSMessage
				if err := websocket.JSON.Receive(conn, &msg); err != nil {
					return
				}
				s.handleWSMessage(conn, &msg)
			}
		},
		Handshake: func(config *websocket.Config, request *http.Request) error {
			origin := request.Header.Get("Origin")
			if !isAllowedWSOrigin(origin) {
				return fmt.Errorf("origin not allowed: %s", origin)
			}
			return nil
		},
	}

	wsServer.ServeHTTP(w, r)
}

// handleWSMessage processes incoming WebSocket messages.
func (s *Server) handleWSMessage(conn *websocket.Conn, msg *WSMessage) {
	switch msg.Type {
	case "ping":
		s.safeConnSend(conn, WSMessage{Type: "pong"})
	case "subscribe":
		// Client subscribes to real-time updates; already registered in hub.
		s.safeConnSend(conn, WSMessage{Type: "subscribed"})
	default:
		s.logger.Debug("ws unknown message type", "type", msg.Type)
	}
}

// safeConnSend serializes a JSON send on conn with the hub's per-connection
// write mutex, preventing races between the read-loop's pong/subscribed
// responses and hub.Broadcast writes. golang.org/x/net/websocket requires
// that no two goroutines call Write concurrently on the same conn.
//
// The mutex's entire purpose is to serialize this write — it is NOT
// protecting unrelated state from concurrent access while I/O happens.
// The mutexio analyzer's "no I/O under mutex" rule does not apply to
// locks whose sole reason for existing is to serialize the I/O itself;
// this is a justified analyzer false positive.
func (s *Server) safeConnSend(conn *websocket.Conn, msg WSMessage) {
	mu := s.wsHub.connWriteMu(conn)
	mu.Lock()
	defer mu.Unlock()
	_ = websocket.JSON.Send(conn, msg)
}
