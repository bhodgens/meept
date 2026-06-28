// Package session provides session management for multi-client attachment.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	sid "github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// Constants for session management.
const (
	// BranchMain is the default branch ID for sessions.
	BranchMain = "main"

	// Map key constants for session payloads and queries.
	KeyStatus    = "status"
	KeyMessage   = "message"
	KeySessionID = "session_id"
)

// DesignationStatus represents the status of a session designation.
type DesignationStatus string

const (
	DesignationNone              DesignationStatus = "none"
	DesignationWaitingHuman      DesignationStatus = "waiting_human"
	DesignationHumanResponded    DesignationStatus = "human_responded"
	DesignationBotThinking       DesignationStatus = "bot_thinking"
	DesignationRequiresApproval  DesignationStatus = "requires_approval"
)

// SessionDesignation tracks a session's special status requiring attention.
type SessionDesignation struct {
	Status         DesignationStatus `json:"status"`
	Reason         string            `json:"reason"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at,omitempty"`
	Priority       string            `json:"priority"` // low, normal, high, urgent
}

// Session represents an active conversation session that can be shared
// by multiple clients.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Session struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Description     string              `json:"description,omitempty"`
	ConversationID  string              `json:"conversation_id"` // DEPRECATED: use thread.ConversationID
	CreatedAt       time.Time           `json:"created_at"`
	LastActivity    time.Time           `json:"last_activity"`
	AttachedClients []string            `json:"attached_clients"`
	WorkerIDs       []string            `json:"worker_ids,omitempty"`
	LeafMessageID   *int64              `json:"leaf_message_id,omitempty"`
	ProjectID       string              `json:"project_id,omitempty"`
	ProjectPath     string              `json:"project_path,omitempty"`
	NoFence         bool                `json:"no_fence,omitempty"`

	// Archived indicates the session has been soft-archived. Archived sessions
	// are excluded from the default visible set and sort to the bottom of
	// listings; their data is preserved.
	Archived bool `json:"archived,omitempty"`

	// Thread-based context partitioning (NEW)
	Threads        map[string]*Thread `json:"threads,omitempty"` // threadID -> Thread
	ActiveThreadID string             `json:"active_thread_id,omitempty"`

	// Session designation (Plan 4.1)
	Designation    *SessionDesignation `json:"designation,omitempty"`

	// designationHistory is an optional store for recording designation transitions.
	// When nil, SetDesignation skips history recording (nil-safe).
	designationHistory DesignationHistoryStore `json:"-"`
}

// GetActiveThread returns the currently active thread.
func (s *Session) GetActiveThread() *Thread {
	if s.ActiveThreadID == "" || s.Threads == nil {
		return nil
	}
	return s.Threads[s.ActiveThreadID]
}

// GetOrCreateThread returns existing thread or creates new one.
func (s *Session) GetOrCreateThread(threadID, topicLabel string) *Thread {
	if s.Threads == nil {
		s.Threads = make(map[string]*Thread)
	}

	if thread, exists := s.Threads[threadID]; exists {
		return thread
	}

	thread := &Thread{
		ID:             threadID,
		SessionID:      s.ID,
		TopicLabel:     topicLabel,
		ConversationID: s.ConversationID + "-" + threadID, // Unique per thread
		CreatedAt:      time.Now().UTC(),
		LastActivityAt: time.Now().UTC(),
		IsActive:       true,
	}

	// Deactivate other threads
	for _, t := range s.Threads {
		t.IsActive = false
	}
	thread.IsActive = true
	s.ActiveThreadID = threadID
	s.Threads[threadID] = thread

	return thread
}

// SetDesignation sets the session's designation status.
// If a DesignationHistoryStore is attached, the transition is recorded.
func (s *Session) SetDesignation(status DesignationStatus, reason, priority string) {
	now := time.Now()
	var fromStatus DesignationStatus
	if s.Designation != nil {
		fromStatus = s.Designation.Status
	} else {
		fromStatus = DesignationNone
	}

	if s.Designation == nil {
		s.Designation = &SessionDesignation{
			Status:      status,
			Reason:      reason,
			Priority:    priority,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	} else {
		s.Designation.Status = status
		s.Designation.Reason = reason
		s.Designation.Priority = priority
		s.Designation.UpdatedAt = now
	}

	// Record the transition if a history store is attached.
	// Skip recording if the status did not actually change.
	if s.designationHistory != nil && fromStatus != status {
		ctx := context.Background()
		if err := s.designationHistory.Record(ctx, s.ID, fromStatus, status, reason); err != nil {
			// History recording is best-effort; don't fail the designation.
			slog.Warn("failed to record designation history",
				"session_id", s.ID,
				"from", string(fromStatus),
				"to", string(status),
				"error", err)
		}
	}
}

// SetDesignationHistoryStore attaches a store for recording designation transitions.
// Pass nil to disable history recording. Nil-guarded per CLAUDE.md rules.
func (s *Session) SetDesignationHistoryStore(store DesignationHistoryStore) {
	if s != nil {
		s.designationHistory = store
	}
}

// ClearDesignation clears the session's designation.
func (s *Session) ClearDesignation() {
	s.Designation = nil
}

// HasDesignation returns true if the session has an active designation.
func (s *Session) HasDesignation() bool {
	return s.Designation != nil && s.Designation.Status != DesignationNone
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

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create creates a new session.
func (s *MemoryStore) Create(name string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}
	id = "session-" + id

	convID, err := randomHex(8)
	if err != nil {
		return nil, fmt.Errorf("failed to generate conversation ID: %w", err)
	}

	session := &Session{
		ID:              id,
		Name:            name,
		ConversationID:  "conv-" + convID,
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
	if slices.Contains(session.AttachedClients, clientID) {
		return nil // Already attached
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

	if slices.Contains(session.WorkerIDs, workerID) {
		return nil
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
	end := min(offset+limit, len(msgs))
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

// UpdateDesignation sets the session's designation status.
func (s *MemoryStore) UpdateDesignation(sessionID string, status DesignationStatus, reason, priority string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.SetDesignation(status, reason, priority)
	return nil
}

// GetDesignatedSessionIDs returns session IDs with active designation, ordered by priority.
func (s *MemoryStore) GetDesignatedSessionIDs() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type sessionPriority struct {
		id     string
		order int
	}
	prios := make([]*sessionPriority, 0)
	for _, sess := range s.sessions {
		if sess.HasDesignation() {
			var order int
			switch sess.Designation.Priority {
			case "urgent": order = 0
			case "high": order = 1
			case "normal": order = 2
			case "low": order = 3
			default: order = 4
			}
			prios = append(prios, &sessionPriority{id: sess.ID, order: order})
		}
	}
	sort.Slice(prios, func(i, j int) bool { return prios[i].order < prios[j].order })
	ids := make([]string, len(prios))
	for i, p := range prios {
		ids[i] = p.id
	}
	return ids, nil
}

// ClearDesignation clears the session's designation.
func (s *MemoryStore) ClearDesignation(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.ClearDesignation()
	return nil
}

// SetProject sets the project for a session.
func (s *MemoryStore) SetProject(sessionID, projectID, projectPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.ProjectID = projectID
	session.ProjectPath = projectPath
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

// GetLeafMessageID returns the current leaf message ID for a session.
func (s *MemoryStore) GetLeafMessageID(sessionID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return 0, fmt.Errorf("session not found: %s", sessionID)
	}
	if session.LeafMessageID == nil {
		return 0, nil
	}
	return *session.LeafMessageID, nil
}

// SetLeafMessageID updates the leaf message ID for a session.
func (s *MemoryStore) SetLeafMessageID(sessionID string, messageID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.LeafMessageID = &messageID
	return nil
}

// GetMessagePath returns messages from root to the given leaf.
// For MemoryStore, this walks the flat message slice by ID.
func (s *MemoryStore) GetMessagePath(sessionID string, leafID int64) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[sessionID]
	if len(msgs) == 0 {
		return nil, nil
	}

	// Find the leaf message index
	leafIdx := -1
	for i, msg := range msgs {
		if msg.ID == leafID {
			leafIdx = i
			break
		}
	}
	if leafIdx < 0 {
		return nil, fmt.Errorf("message %d not found in session %s", leafID, sessionID)
	}

	// Walk from the leaf back to root via ParentID chain
	var path []Message
	current := msgs[leafIdx]
	for {
		path = append(path, current)
		if current.ParentID == nil {
			break
		}
		// Find parent message
		found := false
		for _, msg := range msgs {
			if msg.ID == *current.ParentID {
				current = msg
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// Reverse to get root-to-leaf order
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

// GetMessageBranches returns branch information for a session.
// MemoryStore returns a single BranchMain branch if messages exist.
func (s *MemoryStore) GetMessageBranches(sessionID string) ([]Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[sessionID]
	if len(msgs) == 0 {
		return nil, nil
	}

	// Collect unique branch IDs with per-branch max ID
	branchMap := make(map[string]int)     // branchID -> count
	branchMaxID := make(map[string]int64) // branchID -> max message ID
	for _, msg := range msgs {
		bid := msg.BranchID
		if bid == "" {
			bid = BranchMain
		}
		branchMap[bid]++
		if msg.ID > branchMaxID[bid] {
			branchMaxID[bid] = msg.ID
		}
	}

	branches := make([]Branch, 0, len(branchMap))
	for bid, count := range branchMap {
		branches = append(branches, Branch{
			ID:           bid,
			LeafID:       branchMaxID[bid],
			MessageCount: count,
		})
	}
	return branches, nil
}

// GetTree returns tree nodes for a session.
// MemoryStore returns all messages as flat tree nodes.
func (s *MemoryStore) GetTree(sessionID string) ([]TreeNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[sessionID]
	if len(msgs) == 0 {
		return nil, nil
	}

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	leafID := int64(0)
	if session.LeafMessageID != nil {
		leafID = *session.LeafMessageID
	}

	nodes := make([]TreeNode, 0, len(msgs))
	for _, msg := range msgs {
		parentID := int64(0)
		if msg.ParentID != nil {
			parentID = *msg.ParentID
		}
		nodes = append(nodes, TreeNode{
			ID:        msg.ID,
			ParentID:  parentID,
			Role:      msg.Role,
			EntryType: msg.EntryType,
			BranchID:  msg.BranchID,
			Content:   msg.Content,
			Timestamp: msg.Timestamp.Format(time.RFC3339),
			IsLeaf:    msg.ID == leafID,
		})
	}
	return nodes, nil
}

// NavigateToBranch is not fully implemented for MemoryStore.
func (s *MemoryStore) NavigateToBranch(sessionID string, targetMessageID int64) (int64, error) {
	return 0, fmt.Errorf("not implemented: NavigateToBranch in MemoryStore")
}

// ForkSession creates a new session by copying messages up to fromMessageID from the
// source session. For MemoryStore, this copies messages and remaps parent IDs.
func (s *MemoryStore) ForkSession(sourceSessionID string, fromMessageID int64, newName string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate source session exists
	source, exists := s.sessions[sourceSessionID]
	if !exists {
		return nil, fmt.Errorf("source session not found: %s", sourceSessionID)
	}

	sourceMsgs := s.messages[sourceSessionID]
	if len(sourceMsgs) == 0 {
		return nil, fmt.Errorf("no messages in source session")
	}

	// Find the path from root to fromMessageID by walking parent chain
	// First, find the target message
	targetIdx := -1
	for i, msg := range sourceMsgs {
		if msg.ID == fromMessageID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return nil, fmt.Errorf("message %d not found in session %s", fromMessageID, sourceSessionID)
	}

	// Collect the path by walking parent chain from target to root
	pathSet := make(map[int64]bool)
	current := sourceMsgs[targetIdx]
	for {
		pathSet[current.ID] = true
		if current.ParentID == nil {
			break
		}
		found := false
		for _, msg := range sourceMsgs {
			if msg.ID == *current.ParentID {
				current = msg
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// Create new session
	now := time.Now().UTC()
	newID := sid.Generate("session-")
	newConvID := sid.Generate("conv-")
	if newName == "" {
		newName = "fork of " + source.Name
	}

	newSession := &Session{
		ID:              newID,
		Name:            newName,
		ConversationID:  newConvID,
		CreatedAt:       now,
		LastActivity:    now,
		AttachedClients: []string{},
		WorkerIDs:       []string{},
	}

	s.sessions[newID] = newSession

	// Copy messages in path, ordered by ID
	oldToNew := make(map[int64]int64)
	nextID := int64(1)
	var newLeafID int64

	for _, msg := range sourceMsgs {
		if !pathSet[msg.ID] {
			continue
		}
		newMsg := Message{
			SessionID:  newID,
			Role:       msg.Role,
			Content:    msg.Content,
			Timestamp:  msg.Timestamp,
			EntryType:  msg.EntryType,
			BranchID:   msg.BranchID,
			Model:      msg.Model,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		newMsg.ID = nextID
		if msg.ParentID != nil {
			if newPID, ok := oldToNew[*msg.ParentID]; ok {
				newMsg.ParentID = &newPID
			}
			// If old parent wasn't in path, this becomes root
		}
		oldToNew[msg.ID] = newMsg.ID
		newLeafID = newMsg.ID
		s.messages[newID] = append(s.messages[newID], newMsg)
		nextID++
	}

	newSession.LeafMessageID = &newLeafID

	s.logger.Info("Session forked (memory)",
		"source_id", sourceSessionID,
		"new_id", newID,
		"from_message", fromMessageID,
		"copied_messages", len(s.messages[newID]),
	)

	return newSession, nil
}

// InsertCompaction is not fully implemented for MemoryStore.
func (s *MemoryStore) InsertCompaction(sessionID string, parentID int64, summary string, compressedIDs []int64) (int64, error) {
	return 0, fmt.Errorf("not implemented: InsertCompaction in MemoryStore")
}

// ReparentAfterCompaction is not fully implemented for MemoryStore.
func (s *MemoryStore) ReparentAfterCompaction(sessionID string, afterID, compactionID int64) error {
	return fmt.Errorf("not implemented: ReparentAfterCompaction in MemoryStore")
}

// GetCompactionEntries retrieves compaction entries from in-memory messages.
// It filters messages by entry_type 'compaction' and parses the JSON content
// to extract CompressedIDs.
func (s *MemoryStore) GetCompactionEntries(sessionID string) ([]CompactionEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[sessionID]
	var entries []CompactionEntry
	for _, msg := range msgs {
		if msg.EntryType != "compaction" {
			continue
		}
		entry := CompactionEntry{
			ID:        msg.ID,
			SessionID: msg.SessionID,
			ParentID:  msg.ParentID,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
		var content CompactionContent
		if err := json.Unmarshal([]byte(msg.Content), &content); err == nil { //nolint:mutexio // unmarshal of in-memory msg.Content under RLock
			entry.CompressedIDs = content.CompressedIDs
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// SaveToolCalls is not supported for MemoryStore (tool calls are only persisted in SQLite).
func (s *MemoryStore) SaveToolCalls(messageID int64, toolCalls []ToolCall) error {
	return nil
}

// GetToolCalls returns empty for MemoryStore (tool calls are only persisted in SQLite).
func (s *MemoryStore) GetToolCalls(messageID int64) ([]ToolCall, error) {
	return nil, nil
}

// GetToolCallsForMessages returns empty for MemoryStore (tool calls are only persisted in SQLite).
func (s *MemoryStore) GetToolCallsForMessages(messageIDs []int64) (map[int64][]ToolCall, error) {
	return make(map[int64][]ToolCall), nil
}

// SearchMessages performs a case-insensitive substring search across all
// messages in all sessions. Used as an in-memory fallback when SQLite is
// unavailable (tests, ephemeral runs).
func (s *MemoryStore) SearchMessages(ctx context.Context, query string, limit int) ([]MessageSearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if query == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	needle := strings.ToLower(query)
	var results []MessageSearchResult
	for sid, msgs := range s.messages {
		for _, msg := range msgs {
			if msg.Role != "user" && msg.Role != "assistant" {
				continue
			}
			if idx := strings.ToLower(strings.TrimSpace(msg.Content)); strings.Contains(idx, needle) {
				results = append(results, MessageSearchResult{
					MessageID: msg.ID,
					SessionID: sid,
					Role:      msg.Role,
					Content:   msg.Content,
					Snippet:   truncateSnippet(msg.Content, 200),
					Relevance: 1.0,
					Timestamp: msg.Timestamp.Format(time.RFC3339),
				})
				if len(results) >= limit {
					return results, nil
				}
			}
		}
	}
	return results, nil
}

// SearchMessagesSemantic is unsupported in MemoryStore.
func (s *MemoryStore) SearchMessagesSemantic(ctx context.Context, embedding []float32, limit int) ([]MessageSearchResult, error) {
	return nil, ErrSemanticUnavailable
}

// StoreEmbedding is a no-op in MemoryStore (embeddings are only persisted in SQLite).
func (s *MemoryStore) StoreEmbedding(ctx context.Context, messageID int64, embedding []float32) error {
	return nil
}

// UnembeddedMessages returns empty in MemoryStore (embeddings are not tracked).
func (s *MemoryStore) UnembeddedMessages(ctx context.Context, limit int) ([]MessageSearchResult, error) {
	return nil, nil
}

// GetActiveThread returns the active thread for a session.
func (s *MemoryStore) GetActiveThread(ctx context.Context, sessionID string) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, nil
	}
	return session.GetActiveThread(), nil
}

// ListThreadsBySession returns all threads for a session.
func (s *MemoryStore) ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists || session.Threads == nil {
		return []*Thread{}, nil
	}

	threads := make([]*Thread, 0, len(session.Threads))
	for _, t := range session.Threads {
		threads = append(threads, t)
	}
	return threads, nil
}

// CreateThread persists a new thread on a session.
func (s *MemoryStore) CreateThread(ctx context.Context, thread *Thread) error {
	if thread == nil {
		return fmt.Errorf("nil thread")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[thread.SessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", thread.SessionID)
	}
	if sess.Threads == nil {
		sess.Threads = make(map[string]*Thread)
	}
	sess.Threads[thread.ID] = thread
	return nil
}

// GetThread retrieves a thread by ID across all sessions (thread IDs are globally unique).
func (s *MemoryStore) GetThread(ctx context.Context, threadID string) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.Threads != nil {
			if t, ok := sess.Threads[threadID]; ok {
				return t, nil
			}
		}
	}
	return nil, fmt.Errorf("thread not found: %s", threadID)
}

// UpdateThread updates an existing thread.
func (s *MemoryStore) UpdateThread(ctx context.Context, thread *Thread) error {
	if thread == nil {
		return fmt.Errorf("nil thread")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[thread.SessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", thread.SessionID)
	}
	if sess.Threads == nil {
		return fmt.Errorf("thread not found: %s", thread.ID)
	}
	if _, ok := sess.Threads[thread.ID]; !ok {
		return fmt.Errorf("thread not found: %s", thread.ID)
	}
	sess.Threads[thread.ID] = thread
	return nil
}

// DeleteThread removes a thread by ID.
func (s *MemoryStore) DeleteThread(ctx context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.Threads != nil {
			if _, ok := sess.Threads[threadID]; ok {
				delete(sess.Threads, threadID)
				if sess.ActiveThreadID == threadID {
					sess.ActiveThreadID = ""
				}
				return nil
			}
		}
	}
	return fmt.Errorf("thread not found: %s", threadID)
}

// SetActiveThread marks threadID as active in its session and deactivates others.
func (s *MemoryStore) SetActiveThread(ctx context.Context, sessionID, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	if sess.Threads == nil {
		return fmt.Errorf("thread not found: %s", threadID)
	}
	target, ok := sess.Threads[threadID]
	if !ok {
		return fmt.Errorf("thread not found: %s", threadID)
	}
	for _, t := range sess.Threads {
		t.IsActive = false
	}
	target.IsActive = true
	target.LastActivityAt = time.Now().UTC()
	sess.ActiveThreadID = threadID
	return nil
}

// Ensure MemoryStore implements Store interface.
var _ Store = (*MemoryStore)(nil)

// Handler handles session-related RPC requests via the message bus.
type Handler struct {
	handler       *bus.SubscriptionHandler
	store         Store
	bus           *bus.MessageBus
	logger        *slog.Logger
	summarizer    *Summarizer
	branchManager *BranchManager
}

// HandlerOption configures the session handler.
type HandlerOption func(*Handler)

// WithSummarizer sets the summarizer for LLM-based description generation.
func WithSummarizer(s *Summarizer) HandlerOption {
	return func(h *Handler) {
		h.summarizer = s
	}
}

// WithBranchManager sets the branch manager for branch navigation operations.
func WithBranchManager(bm *BranchManager) HandlerOption {
	return func(h *Handler) {
		h.branchManager = bm
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
		"session.branch.navigate":      h.handleBranchNavigate,
		"session.branches.list":        h.handleBranchesList,
		"session.fork":                 h.handleSessionFork,
		"session.tree.get":             h.handleSessionTreeGet,
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

func (h *Handler) handleSessionCreate(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionList(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGet(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetMostRecent(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionAttach(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionDetach(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionDelete(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionSaveMessages(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetMessages(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionUpdateDescription(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGenerateDescription(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionStop(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionGetChildTasks(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleBranchNavigate(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleBranchesList(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionFork(ctx context.Context, topic string, msg any) {
	h.handleMessage(topic, msg.(*models.BusMessage))
}

func (h *Handler) handleSessionTreeGet(ctx context.Context, topic string, msg any) {
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
	case "session.branch.navigate":
		response, err = h.handleBranchNavigateMsg(msg)
	case "session.branches.list":
		response, err = h.handleBranchesListMsg(msg)
	case "session.fork":
		response, err = h.handleForkMsg(msg)
	case "session.tree.get":
		response, err = h.handleTreeGetMsg(msg)
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
func (h *Handler) handleList(_ *models.BusMessage) (any, error) {
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
func (h *Handler) handleGetMostRecent(_ *models.BusMessage) (any, error) {
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

	return map[string]string{KeyStatus: "attached"}, nil
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

	return map[string]string{KeyStatus: "detached"}, nil
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

	return map[string]string{KeyStatus: "deleted"}, nil
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

	return map[string]string{KeyStatus: "saved"}, nil
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

	return map[string]string{KeyStatus: "updated"}, nil
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
		KeySessionID, params.SessionID,
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
			KeySessionID, params.SessionID,
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
				KeySessionID, params.SessionID,
			)
			fallback := extractSimpleResult(params.FirstMessage)
			name = fallback.Name
			description = fallback.Description
		} else {
			h.logger.Info("LLM summarization succeeded",
				KeySessionID, params.SessionID,
				"name", result.Name,
				"description", result.Description,
			)
			name = result.Name
			description = result.Description
		}
	} else {
		h.logger.Warn("No summarizer available, using simple extraction",
			KeySessionID, params.SessionID,
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
				KeySessionID, params.SessionID,
				"name", name,
			)
			// Continue even if name update fails
		}
	}

	// Save the generated description
	if err := h.store.UpdateDescription(params.SessionID, description); err != nil {
		h.logger.Error("Failed to save generated description",
			"error", err,
			KeySessionID, params.SessionID,
			"description", description,
		)
		return nil, err
	}

	h.logger.Info("Session name and description generated and saved",
		KeySessionID, params.SessionID,
		"name", name,
		"description", description,
	)

	return map[string]string{
		KeySessionID:  params.SessionID,
		"name":        name,
		"description": description,
		KeyStatus:     "generated",
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
			KeySessionID: params.SessionID,
			"action":     "stop",
		})
		stopMsg := &models.BusMessage{
			ID:        sid.Generate("stop-"),
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
		KeySessionID, params.SessionID,
		"workers_stopped", len(stoppedWorkers),
	)

	return map[string]any{
		KeyStatus:         "stopped",
		KeySessionID:      params.SessionID,
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
		KeySessionID: params.SessionID,
		"tasks":      session.WorkerIDs,
	}, nil
}

// handleBranchNavigateMsg handles a branch navigate request.
func (h *Handler) handleBranchNavigateMsg(msg *models.BusMessage) (any, error) {
	if h.branchManager == nil {
		return nil, fmt.Errorf("branch manager not configured")
	}
	return handleBranchNavigate(h.branchManager, msg.Payload)
}

// handleBranchesListMsg handles a branches list request.
func (h *Handler) handleBranchesListMsg(msg *models.BusMessage) (any, error) {
	if h.branchManager == nil {
		return nil, fmt.Errorf("branch manager not configured")
	}
	return handleBranchesList(h.branchManager, msg.Payload)
}

// handleForkMsg handles a session fork request.
func (h *Handler) handleForkMsg(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID     string `json:"session_id"`
		FromMessageID int64  `json:"from_message_id"`
		Name          string `json:"name"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if params.FromMessageID == 0 {
		return nil, fmt.Errorf("from_message_id is required")
	}

	newSession, err := h.store.ForkSession(params.SessionID, params.FromMessageID, params.Name)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"session":        newSession,
		"new_session_id": newSession.ID,
	}, nil
}

// handleTreeGetMsg handles a tree get request.
func (h *Handler) handleTreeGetMsg(msg *models.BusMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &params); err != nil {
		return nil, err
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	nodes, err := h.store.GetTree(params.SessionID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"nodes": nodes}, nil
}

// sendResponse publishes a response to the bus.
func (h *Handler) sendResponse(replyTo, topic string, response any, err error) {
	var payload []byte
	var mErr error

	if err != nil {
		payload, mErr = json.Marshal(map[string]string{"error": err.Error()})
		if mErr != nil {
			slog.Warn("failed to marshal session error response", "error", mErr)
		}
	} else {
		payload, mErr = json.Marshal(response)
		if mErr != nil {
			slog.Warn("failed to marshal session response", "error", mErr)
		}
	}

	msg := &models.BusMessage{
		ID:        sid.Generate("session-resp-"),
		Type:      models.MessageTypeResponse,
		Topic:     topic,
		Source:    "session-handler",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		ReplyTo:   replyTo,
	}

	h.bus.Publish(topic, msg)
}
