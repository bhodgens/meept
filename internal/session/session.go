// Package session provides session management for multi-client attachment.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Session represents an active conversation session that can be shared
// by multiple clients.
type Session struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ConversationID string    `json:"conversation_id"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivity   time.Time `json:"last_activity"`
	AttachedClients []string `json:"attached_clients"`
	WorkerIDs      []string  `json:"worker_ids,omitempty"`
}

// MemoryStore manages sessions with thread-safe operations (in-memory, non-persistent).
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *slog.Logger
}

// NewStore creates a new in-memory session store.
// Deprecated: Use NewSQLiteStore for persistent sessions.
func NewStore(logger *slog.Logger) *MemoryStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryStore{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore(logger *slog.Logger) *MemoryStore {
	return NewStore(logger)
}

// Create creates a new session.
func (s *MemoryStore) Create(name string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("session-%d", time.Now().UnixNano())
	session := &Session{
		ID:              id,
		Name:            name,
		ConversationID:  fmt.Sprintf("conv-%d", time.Now().UnixNano()),
		CreatedAt:       time.Now(),
		LastActivity:    time.Now(),
		AttachedClients: []string{},
		WorkerIDs:       []string{},
	}

	s.sessions[id] = session
	s.logger.Info("Session created", "id", id, "name", name)
	return session
}

// Get returns a session by ID.
func (s *MemoryStore) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

// GetByConversationID retrieves a session by its conversation ID.
func (s *MemoryStore) GetByConversationID(conversationID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, session := range s.sessions {
		if session.ConversationID == conversationID {
			return session
		}
	}
	return nil
}

// GetMostRecent returns the most recently active session.
func (s *MemoryStore) GetMostRecent() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var mostRecent *Session
	for _, session := range s.sessions {
		if mostRecent == nil || session.LastActivity.After(mostRecent.LastActivity) {
			mostRecent = session
		}
	}
	return mostRecent
}

// List returns all sessions.
func (s *MemoryStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// Delete removes a session.
func (s *MemoryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return false
	}

	delete(s.sessions, id)
	s.logger.Info("Session deleted", "id", id)
	return true
}

// Attach adds a client to a session.
func (s *MemoryStore) Attach(sessionID, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if already attached
	for _, c := range session.AttachedClients {
		if c == clientID {
			return nil // Already attached
		}
	}

	session.AttachedClients = append(session.AttachedClients, clientID)
	session.LastActivity = time.Now()
	s.logger.Info("Client attached to session", "session", sessionID, "client", clientID)
	return nil
}

// Detach removes a client from a session.
func (s *MemoryStore) Detach(sessionID, clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for i, c := range session.AttachedClients {
		if c == clientID {
			session.AttachedClients = append(session.AttachedClients[:i], session.AttachedClients[i+1:]...)
			session.LastActivity = time.Now()
			s.logger.Info("Client detached from session", "session", sessionID, "client", clientID)
			return nil
		}
	}

	return nil // Client wasn't attached
}

// UpdateActivity updates the last activity timestamp.
func (s *MemoryStore) UpdateActivity(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		session.LastActivity = time.Now()
	}
}

// AddWorker adds a worker ID to a session.
func (s *MemoryStore) AddWorker(sessionID, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for _, w := range session.WorkerIDs {
		if w == workerID {
			return nil
		}
	}

	session.WorkerIDs = append(session.WorkerIDs, workerID)
	session.LastActivity = time.Now()
	return nil
}

// RemoveWorker removes a worker ID from a session.
func (s *MemoryStore) RemoveWorker(sessionID, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for i, w := range session.WorkerIDs {
		if w == workerID {
			session.WorkerIDs = append(session.WorkerIDs[:i], session.WorkerIDs[i+1:]...)
			session.LastActivity = time.Now()
			return nil
		}
	}

	return nil
}

// Close is a no-op for in-memory store.
func (s *MemoryStore) Close() error {
	return nil
}

// Ensure MemoryStore implements Store interface.
var _ Store = (*MemoryStore)(nil)

// Handler handles session-related RPC requests via the message bus.
type Handler struct {
	store  Store
	bus    *bus.MessageBus
	logger *slog.Logger
	cancel context.CancelFunc
}

// NewHandler creates a new session handler.
func NewHandler(store Store, msgBus *bus.MessageBus, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		store:  store,
		bus:    msgBus,
		logger: logger,
	}
}

// Start begins listening for session requests.
func (h *Handler) Start(ctx context.Context) error {
	ctx, h.cancel = context.WithCancel(ctx)

	// Subscribe to session topics
	topics := []string{
		"session.create",
		"session.list",
		"session.get",
		"session.get_most_recent",
		"session.attach",
		"session.detach",
		"session.delete",
	}

	for _, topic := range topics {
		sub := h.bus.Subscribe("session-handler-"+topic, topic)
		go h.handleTopic(ctx, sub, topic)
	}

	h.logger.Info("SessionHandler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	return nil
}

// handleTopic handles messages for a specific topic.
func (h *Handler) handleTopic(ctx context.Context, sub *bus.Subscriber, topic string) {
	for {
		select {
		case <-ctx.Done():
			h.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			h.handleMessage(topic, msg)
		}
	}
}

// handleMessage routes messages to the appropriate handler.
func (h *Handler) handleMessage(topic string, msg *models.BusMessage) {
	var response any
	var err error

	switch topic {
	case "session.create":
		response, err = h.handleCreate(msg)
	case "session.list":
		response, err = h.handleList(msg)
	case "session.get":
		response, err = h.handleGet(msg)
	case "session.get_most_recent":
		response, err = h.handleGetMostRecent(msg)
	case "session.attach":
		response, err = h.handleAttach(msg)
	case "session.detach":
		response, err = h.handleDetach(msg)
	case "session.delete":
		response, err = h.handleDelete(msg)
	default:
		err = fmt.Errorf("unknown topic: %s", topic)
	}

	// Send response
	h.sendResponse(msg.ID, "session.result", response, err)
}

// handleCreate creates a new session.
func (h *Handler) handleCreate(msg *models.BusMessage) (any, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	session := h.store.Create(params.Name)
	return session, nil
}

// handleList lists all sessions.
func (h *Handler) handleList(msg *models.BusMessage) (any, error) {
	sessions := h.store.List()
	return map[string]any{"sessions": sessions}, nil
}

// handleGet gets a session by ID.
func (h *Handler) handleGet(msg *models.BusMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	session := h.store.Get(params.ID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", params.ID)
	}
	return session, nil
}

// handleGetMostRecent gets the most recently active session.
func (h *Handler) handleGetMostRecent(msg *models.BusMessage) (any, error) {
	session := h.store.GetMostRecent()
	if session == nil {
		return nil, fmt.Errorf("no sessions found")
	}
	return session, nil
}

// handleAttach attaches a client to a session.
func (h *Handler) handleAttach(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
		ClientID  string `json:"client_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.store.Attach(params.SessionID, params.ClientID); err != nil {
		return nil, err
	}

	return map[string]string{"status": "attached"}, nil
}

// handleDetach detaches a client from a session.
func (h *Handler) handleDetach(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
		ClientID  string `json:"client_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.store.Detach(params.SessionID, params.ClientID); err != nil {
		return nil, err
	}

	return map[string]string{"status": "detached"}, nil
}

// handleDelete deletes a session.
func (h *Handler) handleDelete(msg *models.BusMessage) (any, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if !h.store.Delete(params.ID) {
		return nil, fmt.Errorf("session not found: %s", params.ID)
	}

	return map[string]string{"status": "deleted"}, nil
}

// sendResponse publishes a response to the bus.
func (h *Handler) sendResponse(replyTo, topic string, response any, err error) {
	var payload []byte

	if err != nil {
		payload, _ = json.Marshal(map[string]string{"error": err.Error()})
	} else {
		payload, _ = json.Marshal(response)
	}

	msg := &models.BusMessage{
		ID:        fmt.Sprintf("session-resp-%d", time.Now().UnixNano()),
		Type:      models.MessageTypeResponse,
		Topic:     topic,
		Source:    "session-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish(topic, msg)
}
