// Package session provides session management for multi-client attachment.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Session represents an active conversation session that can be shared
// by multiple clients.
type Session struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	ConversationID  string    `json:"conversation_id"`
	CreatedAt       time.Time `json:"created_at"`
	LastActivity    time.Time `json:"last_activity"`
	AttachedClients []string  `json:"attached_clients"`
	WorkerIDs       []string  `json:"worker_ids,omitempty"`
}

// MemoryStore manages sessions with thread-safe operations (in-memory, non-persistent).
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	messages map[string][]Message // sessionID -> messages
	logger   *slog.Logger
}

// NewMemoryStore creates a new in-memory session store.
// For persistent sessions, use NewSQLiteStore instead.
func NewMemoryStore(logger *slog.Logger) *MemoryStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryStore{
		sessions: make(map[string]*Session),
		messages: make(map[string][]Message),
		logger:   logger,
	}
}

// Create creates a new session.
func (s *MemoryStore) Create(name string) (*Session, error) {
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
	return session, nil
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

// List returns all sessions that have assistant responses.
func (s *MemoryStore) List() ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		// Filter: only include sessions with at least one assistant message
		msgs := s.messages[session.ID]
		hasResponse := false
		for _, msg := range msgs {
			if msg.Role == "assistant" {
				hasResponse = true
				break
			}
		}
		if hasResponse {
			sessions = append(sessions, session)
		}
	}

	// Sort by last activity descending
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})

	return sessions, nil
}

// Delete removes a session.
func (s *MemoryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return false
	}

	delete(s.sessions, id)
	delete(s.messages, id)
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
func (s *MemoryStore) UpdateActivity(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		session.LastActivity = time.Now()
		return nil
	}
	return fmt.Errorf("session not found: %s", sessionID)
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

// SaveMessages batch-inserts messages for a session.
func (s *MemoryStore) SaveMessages(sessionID string, messages []Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[sessionID]; !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	existing := s.messages[sessionID]
	nextID := int64(len(existing) + 1)
	for i := range messages {
		messages[i].ID = nextID + int64(i)
		messages[i].SessionID = sessionID
	}
	s.messages[sessionID] = append(existing, messages...)
	return nil
}

// GetMessages retrieves messages for a session with pagination.
func (s *MemoryStore) GetMessages(sessionID string, offset, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[sessionID]
	if offset >= len(msgs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(msgs) {
		end = len(msgs)
	}
	result := make([]Message, end-offset)
	copy(result, msgs[offset:end])
	return result, nil
}

// GetMessageCount returns the number of messages in a session.
func (s *MemoryStore) GetMessageCount(sessionID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages[sessionID]), nil
}

// UpdateDescription updates a session's description.
func (s *MemoryStore) UpdateDescription(sessionID, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.Description = description
	return nil
}

// UpdateName updates a session's name.
func (s *MemoryStore) UpdateName(sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.Name = name
	return nil
}

// HasResponses checks if a session has any assistant messages.
func (s *MemoryStore) HasResponses(sessionID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, msg := range s.messages[sessionID] {
		if msg.Role == "assistant" {
			return true, nil
		}
	}
	return false, nil
}

// Close is a no-op for in-memory store.
func (s *MemoryStore) Close() error {
	return nil
}

// Ensure MemoryStore implements Store interface.
var _ Store = (*MemoryStore)(nil)

// Handler handles session-related RPC requests via the message bus.
type Handler struct {
	handler    *bus.SubscriptionHandler
	store      Store
	bus        *bus.MessageBus
	logger     *slog.Logger
	summarizer *Summarizer
}

// HandlerOption configures the session handler.
type HandlerOption func(*Handler)

// WithSummarizer sets the summarizer for LLM-based description generation.
func WithSummarizer(s *Summarizer) HandlerOption {
	return func(h *Handler) {
		h.summarizer = s
	}
}

// NewHandler creates a new session handler.
func NewHandler(store Store, msgBus *bus.MessageBus, logger *slog.Logger, opts ...HandlerOption) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		handler: bus.NewSubscriptionHandler(msgBus, logger.With("component", "session-handler")),
		store:   store,
		bus:     msgBus,
		logger:  logger,
	}
	for _, opt := range opts {
		opt(h)
	}

	// Subscribe to all session topics
	topics := map[string]bus.MessageCallback{
		"session.create":               h.handleSessionCreate,
		"session.list":                 h.handleSessionList,
		"session.get":                  h.handleSessionGet,
		"session.get_most_recent":      h.handleSessionGetMostRecent,
		"session.attach":               h.handleSessionAttach,
		"session.detach":               h.handleSessionDetach,
		"session.delete":               h.handleSessionDelete,
		"session.messages.save":        h.handleSessionSaveMessages,
		"session.messages.get":         h.handleSessionGetMessages,
		"session.update_description":   h.handleSessionUpdateDescription,
		"session.generate_description": h.handleSessionGenerateDescription,
		"session.stop":                 h.handleSessionStop,
		"session.get_child_tasks":      h.handleSessionGetChildTasks,
	}

	for topic, callback := range topics {
		h.handler.Subscribe(topic, callback)
	}

	return h
}

// Start begins listening for session requests.
func (h *Handler) Start(ctx context.Context) error {
	h.handler.Start(ctx)
	h.logger.Info("SessionHandler started")
	return nil
}

// Stop stops the handler.
func (h *Handler) Stop(ctx context.Context) error {
	h.handler.Stop()
	return nil
}

func (h *Handler) handleSessionCreate(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionList(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGet(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetMostRecent(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionAttach(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionDetach(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionDelete(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionSaveMessages(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetMessages(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionUpdateDescription(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGenerateDescription(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionStop(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetChildTasks(ctx context.Context, topic string, msg interface{}) {
	h.handleMessage(topic, msg.(*models.BusMessage))
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
	case "session.messages.save":
		response, err = h.handleSaveMessages(msg)
	case "session.messages.get":
		response, err = h.handleGetMessages(msg)
	case "session.update_description":
		response, err = h.handleUpdateDescription(msg)
	case "session.generate_description":
		response, err = h.handleGenerateDescription(msg)
	case "session.stop":
		response, err = h.handleStop(msg)
	case "session.get_child_tasks":
		response, err = h.handleGetChildTasks(msg)
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

	session, err := h.store.Create(params.Name)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// handleList lists all sessions.
func (h *Handler) handleList(msg *models.BusMessage) (any, error) {
	sessions, err := h.store.List()
	if err != nil {
		return nil, err
	}
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

// handleSaveMessages saves messages for a session.
func (h *Handler) handleSaveMessages(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string    `json:"session_id"`
		Messages  []Message `json:"messages"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.store.SaveMessages(params.SessionID, params.Messages); err != nil {
		return nil, err
	}

	return map[string]string{"status": "saved"}, nil
}

// handleGetMessages retrieves messages for a session.
func (h *Handler) handleGetMessages(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
		Offset    int    `json:"offset"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if params.Limit <= 0 {
		params.Limit = 1000
	}

	messages, err := h.store.GetMessages(params.SessionID, params.Offset, params.Limit)
	if err != nil {
		return nil, err
	}

	count, _ := h.store.GetMessageCount(params.SessionID)

	return map[string]any{
		"messages": messages,
		"total":    count,
	}, nil
}

// handleUpdateDescription updates a session's description.
func (h *Handler) handleUpdateDescription(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID   string `json:"session_id"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	if err := h.store.UpdateDescription(params.SessionID, params.Description); err != nil {
		return nil, err
	}

	return map[string]string{"status": "updated"}, nil
}

// handleGenerateDescription generates a description using LLM summarization.
func (h *Handler) handleGenerateDescription(msg *models.BusMessage) (any, error) {
	h.logger.Info("Generate description request received")

	var params struct {
		SessionID    string `json:"session_id"`
		FirstMessage string `json:"first_message"`
		ProjectName  string `json:"project_name,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		h.logger.Error("Failed to unmarshal generate description params", "error", err)
		return nil, err
	}

	h.logger.Debug("Generate description params",
		"session_id", params.SessionID,
		"first_message_len", len(params.FirstMessage),
		"project_name", params.ProjectName,
	)

	if params.SessionID == "" || params.FirstMessage == "" {
		h.logger.Warn("Missing required params for generate description",
			"has_session_id", params.SessionID != "",
			"has_first_message", params.FirstMessage != "",
		)
		return nil, fmt.Errorf("session_id and first_message are required")
	}

	var name, description string
	if h.summarizer != nil {
		h.logger.Info("Using LLM-based summarization",
			"session_id", params.SessionID,
		)
		// Use LLM-based summarization
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, err := h.summarizer.GenerateDescription(ctx, SummarizeRequest{
			FirstMessage: params.FirstMessage,
			ProjectName:  params.ProjectName,
		})
		if err != nil {
			h.logger.Warn("Summarization failed, using fallback",
				"error", err,
				"session_id", params.SessionID,
			)
			fallback := extractSimpleResult(params.FirstMessage)
			name = fallback.Name
			description = fallback.Description
		} else {
			h.logger.Info("LLM summarization succeeded",
				"session_id", params.SessionID,
				"name", result.Name,
				"description", result.Description,
			)
			name = result.Name
			description = result.Description
		}
	} else {
		h.logger.Warn("No summarizer available, using simple extraction",
			"session_id", params.SessionID,
		)
		// Fallback to simple extraction
		fallback := extractSimpleResult(params.FirstMessage)
		name = fallback.Name
		description = fallback.Description
	}

	// Save the generated name if different from default
	if name != "" && name != "default" && name != "chat" {
		if err := h.store.UpdateName(params.SessionID, name); err != nil {
			h.logger.Error("Failed to save generated name",
				"error", err,
				"session_id", params.SessionID,
				"name", name,
			)
			// Continue even if name update fails
		}
	}

	// Save the generated description
	if err := h.store.UpdateDescription(params.SessionID, description); err != nil {
		h.logger.Error("Failed to save generated description",
			"error", err,
			"session_id", params.SessionID,
			"description", description,
		)
		return nil, err
	}

	h.logger.Info("Session name and description generated and saved",
		"session_id", params.SessionID,
		"name", name,
		"description", description,
	)

	return map[string]string{
		"session_id":  params.SessionID,
		"name":        name,
		"description": description,
		"status":      "generated",
	}, nil
}

// handleStop stops all work for a session (cancels workers).
func (h *Handler) handleStop(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	session := h.store.Get(params.SessionID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", params.SessionID)
	}

	// Publish a stop request for each worker associated with this session
	stoppedWorkers := make([]string, 0, len(session.WorkerIDs))
	for _, workerID := range session.WorkerIDs {
		// Publish stop event to worker
		stopPayload, _ := json.Marshal(map[string]string{
			"worker_id":  workerID,
			"session_id": params.SessionID,
			"action":     "stop",
		})
		stopMsg := &models.BusMessage{
			ID:        fmt.Sprintf("stop-%d", time.Now().UnixNano()),
			Type:      models.MessageTypeRequest,
			Topic:     "worker.stop",
			Source:    "session-handler",
			Timestamp: time.Now().UTC(),
			Payload:   stopPayload,
		}
		h.bus.Publish("worker.stop", stopMsg)
		stoppedWorkers = append(stoppedWorkers, workerID)
	}

	h.logger.Info("Session stop requested",
		"session_id", params.SessionID,
		"workers_stopped", len(stoppedWorkers),
	)

	return map[string]any{
		"status":          "stopped",
		"session_id":      params.SessionID,
		"workers_stopped": stoppedWorkers,
	}, nil
}

// handleGetChildTasks returns tasks associated with a session.
func (h *Handler) handleGetChildTasks(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}

	session := h.store.Get(params.SessionID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", params.SessionID)
	}

	// Return worker IDs as "child tasks" for now
	// A more complete implementation would query a task store
	return map[string]any{
		"session_id": params.SessionID,
		"tasks":      session.WorkerIDs,
	}, nil
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
