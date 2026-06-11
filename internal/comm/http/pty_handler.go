package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for localhost
	},
}

// PTYHandler handles PTY session HTTP requests.
type PTYHandler struct {
	ptyMgr *pty.Manager
	logger *slog.Logger
	subs   map[string][]chan []byte
	mu     sync.RWMutex
}

// PTYSessionRequest holds a session creation request.
type PTYSessionRequest struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Dir  string            `json:"dir,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
	Rows int               `json:"rows,omitempty"`
	Cols int               `json:"cols,omitempty"`
}

// NewPTYHandler creates a new PTY HTTP handler.
func NewPTYHandler(ptyMgr *pty.Manager, logger *slog.Logger) *PTYHandler {
	return &PTYHandler{
		ptyMgr: ptyMgr,
		logger: logger.With("component", "pty-handler"),
		subs:   make(map[string][]chan []byte),
	}
}

// RegisterRoutes registers PTY endpoints.
func (h *PTYHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/pty/sessions", h.handleSessions)
	mux.HandleFunc("/api/v1/pty/sessions/", h.handleSession)
}

// handleSessions handles POST /api/v1/pty/sessions (create session)
func (h *PTYHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PTYSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Cmd == "" {
		http.Error(w, "cmd is required", http.StatusBadRequest)
		return
	}

	sessionID := generateSessionID()
	cfg := pty.SessionConfig{
		Cmd:  req.Cmd,
		Args: req.Args,
		Dir:  req.Dir,
		Rows: req.Rows,
		Cols: req.Cols,
	}

	// Convert env map to slice
	if len(req.Env) > 0 {
		cfg.Env = make([]string, 0, len(req.Env))
		for k, v := range req.Env {
			cfg.Env = append(cfg.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	sess, err := h.ptyMgr.CreateSession(sessionID, cfg)
	if err != nil {
		h.logger.Error("Failed to create session", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Start streaming goroutine
	go h.streamSessionOutput(sessionID, sess)

	info := &pty.SessionInfo{
		ID:        sessionID,
		Cmd:       req.Cmd,
		CreatedAt: time.Now(),
		IsRunning: sess.IsRunning(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleSession handles session-specific endpoints
func (h *PTYHandler) handleSession(w http.ResponseWriter, r *http.Request) {
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check if it's a WebSocket upgrade request
		if websocket.IsWebSocketUpgrade(r) {
			h.streamSessionWS(w, r, sessionID)
		} else {
			http.Error(w, "WebSocket upgrade required", http.StatusBadRequest)
		}
	case http.MethodPost:
		h.writeToSession(w, r, sessionID)
	case http.MethodDelete:
		h.closeSession(w, r, sessionID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *PTYHandler) streamSessionWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	sess := h.ptyMgr.GetSession(sessionID)
	if sess == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Create output channel for this connection
	outputChan := make(chan []byte, 100)
	h.mu.Lock()
	h.subs[sessionID] = append(h.subs[sessionID], outputChan)
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		// Remove this subscriber
		subs := h.subs[sessionID]
		for i, ch := range subs {
			if ch == outputChan {
				h.subs[sessionID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(h.subs[sessionID]) == 0 {
			delete(h.subs, sessionID)
		}
		h.mu.Unlock()
		close(outputChan)
	}()

	for {
		select {
		case output, ok := <-outputChan:
			if !ok {
				return // Channel closed
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, output); err != nil {
				h.logger.Error("WebSocket send failed", "error", err)
				return
			}
		case <-r.Context().Done():
			return // Client disconnected
		}
	}
}

func (h *PTYHandler) streamSessionOutput(sessionID string, sess pty.Session) {
	outputChan := sess.Output()
	for output := range outputChan {
		h.mu.RLock()
		subs := h.subs[sessionID]
		h.mu.RUnlock()

		for _, ch := range subs {
			select {
			case ch <- output:
			default:
				// Channel full, skip
			}
		}
	}
}

func (h *PTYHandler) writeToSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sess := h.ptyMgr.GetSession(sessionID)
	if sess == nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	if _, err := sess.Write([]byte(req.Input)); err != nil {
		h.logger.Error("Write to session failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *PTYHandler) closeSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	err := h.ptyMgr.DestroySession(sessionID)
	if err != nil {
		h.logger.Error("Failed to close session", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "closed"})
}

// Helpers
func generateSessionID() string {
	return fmt.Sprintf("pty-%d", time.Now().UnixNano())
}

func extractSessionID(path string) string {
	// /api/v1/pty/sessions/{id}[/...]
	parts := strings.Split(path, "/")
	if len(parts) >= 5 && parts[4] != "" {
		return parts[4]
	}
	return ""
}
