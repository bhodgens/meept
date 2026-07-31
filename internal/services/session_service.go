package services

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// SessionService handles session operations.
type SessionService struct {
	store  session.Store
	pm     ProjectResolver
	logger *slog.Logger
}

// ProjectResolver is the narrow interface SessionService needs from the
// project manager: ensuring a default project exists and returning it.
type ProjectResolver interface {
	EnsureDefault(ctx context.Context) (*project.Project, error)
	GetActive(ctx context.Context) (*project.Project, error)
	// Get retrieves a project by ID. Used when the client explicitly
	// requests a specific project for a new session.
	Get(ctx context.Context, id string) (*project.Project, error)
	// CreateOrResolve resolves or registers a project from a filesystem
	// path. Used when the client provides a CWD (detection context) so the
	// session binds to the user's actual repo instead of a synthetic default.
	CreateOrResolve(ctx context.Context, arg string) (*project.Project, error)
}

// NewSessionService creates a session service.
func NewSessionService(s session.Store) *SessionService {
	return &SessionService{store: s, logger: slog.Default()}
}

// SetProjectManager wires the project manager for default project resolution
// on session creation.
func (s *SessionService) SetProjectManager(pm ProjectResolver) {
	if pm != nil {
		s.pm = pm
	}
}

// CreateSessionRequest contains session creation parameters.
type CreateSessionRequest struct {
	Name              string                      `json:"name,omitempty"`
	ProjectID         string                      `json:"project_id,omitempty"`
	DetectionContext  *session.DetectionContext    `json:"detection_context,omitempty"`
}

// CreateSession creates a new session.
func (s *SessionService) CreateSession(ctx context.Context, req CreateSessionRequest) (*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "CreateSession", ErrUnavailable)
	}
	name := req.Name
	if name == "" {
		name = "default"
	}
	sess, err := s.store.Create(name)
	if err != nil {
		return nil, wrapError("session", "CreateSession", err)
	}

	// Store the client-provided detection context (cwd, etc.) on the
	// session so downstream components can resolve the working directory.
	if req.DetectionContext != nil {
		sess.DetectionContext = req.DetectionContext
	}

	// Project resolution priority:
	//   1. Explicit project_id from the client (GUI project selection)
	//   2. CWD from detection context (binds to user's actual repo)
	//   3. Fall back to the active/default project
	if s.pm != nil {
		var p *project.Project
		if req.ProjectID != "" {
			p, err = s.pm.Get(ctx, req.ProjectID)
			if err != nil {
				s.logger.Warn("explicit project lookup failed, falling back to default",
					"session_id", sess.ID,
					"project_id", req.ProjectID,
					"error", err,
				)
				p = nil
			}
		}
		if p == nil && req.DetectionContext != nil && req.DetectionContext.CWD != "" {
			p, err = s.pm.CreateOrResolve(ctx, req.DetectionContext.CWD)
			if err != nil {
				s.logger.Warn("CWD-based project resolution failed, falling back to default",
					"session_id", sess.ID,
					"cwd", req.DetectionContext.CWD,
					"error", err,
				)
				p = nil
			}
		}
		if p == nil {
			p, err = s.pm.EnsureDefault(ctx)
			if err != nil {
				s.logger.Warn("EnsureDefault failed during session creation",
					"session_id", sess.ID,
					"error", err,
				)
			}
		}
		// Last resort: if EnsureDefault returned nil (no active project and
		// it failed to create one), try the daemon's working directory. When
		// the daemon is started from a project root, os.Getwd() IS the user's
		// project. Skip "/" to avoid binding to filesystem root.
		if p == nil {
			if wd, wdErr := os.Getwd(); wdErr == nil && wd != "" && wd != "/" {
				p, err = s.pm.CreateOrResolve(ctx, wd)
				if err != nil {
					s.logger.Debug("daemon CWD project resolution failed",
						"session_id", sess.ID,
						"cwd", wd,
						"error", err,
					)
					p = nil
				}
			}
		}
		if p != nil {
			if err := s.store.SetProject(sess.ID, p.ID, p.LocalPath); err != nil {
				s.logger.Warn("SetProject failed during session creation",
					"session_id", sess.ID,
					"project_id", p.ID,
					"error", err,
				)
			} else {
				sess.ProjectID = p.ID
				sess.ProjectPath = p.LocalPath
			}
		}
	}

	return sess, nil
}

// GetSessionRequest contains get parameters.
type GetSessionRequest struct {
	ID string `json:"id"`
}

// GetSession retrieves a session by ID.
func (s *SessionService) GetSession(ctx context.Context, req GetSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "GetSession", ErrNotFound)
	}
	// Migration: backfill DetectionContext from ProjectPath for legacy sessions
	migrateSessionDetectionContext(sess)
	return sess, nil
}

// GetMessagesRequest contains get-messages parameters.
type GetMessagesRequest struct {
	ID     string `json:"id"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GetMessages retrieves messages for a session with pagination.
func (s *SessionService) GetMessages(ctx context.Context, req GetMessagesRequest) ([]session.Message, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetMessages", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetMessages", ErrUnavailable)
	}
	if req.Limit <= 0 {
		req.Limit = 1000
	}
	return s.store.GetMessages(req.ID, req.Offset, req.Limit)
}

// DeleteSessionRequest contains delete parameters.
type DeleteSessionRequest struct {
	ID string `json:"id"`
}

// DeleteSession removes a session.
func (s *SessionService) DeleteSession(ctx context.Context, req DeleteSessionRequest) error {
	if req.ID == "" {
		return wrapError("session", "DeleteSession", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "DeleteSession", ErrUnavailable)
	}
	if !s.store.Delete(req.ID) {
		return wrapError("session", "DeleteSession", ErrNotFound)
	}
	return nil
}

// ListSessionsRequest contains list parameters.
type ListSessionsRequest struct {
	Limit       int `json:"limit,omitempty"`
	Designation *string `json:"designation,omitempty"`
}

// migrateSessionDetectionContext backfills DetectionContext from ProjectPath
// for legacy sessions that were created before DetectionContext was added.
// This is an in-memory migration only - the session store is not modified.
func migrateSessionDetectionContext(sess *session.Session) {
	if sess == nil {
		return
	}
	// Only backfill if DetectionContext is nil but ProjectPath is set
	if sess.DetectionContext == nil && sess.ProjectPath != "" {
		sess.DetectionContext = &session.DetectionContext{
			CWD: sess.ProjectPath,
		}
	}
}

// GetMostRecent returns the most recently active session, or nil if none exist.
func (s *SessionService) GetMostRecent(ctx context.Context) (*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "GetMostRecent", ErrUnavailable)
	}
	sessions, err := s.store.List()
	if err != nil {
		return nil, wrapError("session", "GetMostRecent", err)
	}
	if len(sessions) == 0 {
		return nil, wrapError("session", "GetMostRecent", ErrNotFound)
	}
	// Sessions from store.List() are ordered by most recent first.
	sess := sessions[0]
	// Migration: backfill DetectionContext from ProjectPath for legacy sessions
	migrateSessionDetectionContext(sess)
	return sess, nil
}

// List returns all sessions, optionally filtered by designation status.
func (s *SessionService) List(ctx context.Context, req ListSessionsRequest) ([]*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "List", ErrUnavailable)
	}
	sessions, err := s.store.List()
	if err != nil {
		return nil, wrapError("session", "List", err)
	}

	// Migration: backfill DetectionContext from ProjectPath for legacy sessions
	for _, sess := range sessions {
		migrateSessionDetectionContext(sess)
	}

	// Apply optional designation filter
	if req.Designation != nil && *req.Designation != "" {
		targetStatus := session.DesignationStatus(*req.Designation)
		filtered := make([]*session.Session, 0, len(sessions))
		for _, sess := range sessions {
			if sess.Designation == nil {
				continue
			}
			if sess.Designation.Status == targetStatus {
				filtered = append(filtered, sess)
			} else if targetStatus == session.DesignationWaitingHuman &&
				sess.Designation.Status != session.DesignationNone {
				// "waiting_human" filter also accepts "human_responded" and "requires_approval"
				filtered = append(filtered, sess)
			}
		}
		sessions = filtered
	}

	// Apply limit if specified
	if req.Limit > 0 && len(sessions) > req.Limit {
		sessions = sessions[:req.Limit]
	}
	return sessions, nil
}

// AttachSessionRequest contains attach parameters.
type AttachSessionRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

// Attach adds an agent to a session.
func (s *SessionService) Attach(ctx context.Context, req AttachSessionRequest) (*session.Session, error) {
	if req.ID == "" || req.AgentID == "" {
		return nil, wrapError("session", "Attach", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "Attach", ErrUnavailable)
	}
	if err := s.store.Attach(req.ID, req.AgentID); err != nil {
		return nil, wrapError("session", "Attach", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "Attach", ErrNotFound)
	}
	return sess, nil
}

// DetachSessionRequest contains detach parameters.
type DetachSessionRequest struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
}

// Detach removes an agent from a session.
func (s *SessionService) Detach(ctx context.Context, req DetachSessionRequest) (*session.Session, error) {
	if req.ID == "" || req.AgentID == "" {
		return nil, wrapError("session", "Detach", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "Detach", ErrUnavailable)
	}
	if err := s.store.Detach(req.ID, req.AgentID); err != nil {
		return nil, wrapError("session", "Detach", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "Detach", ErrNotFound)
	}
	return sess, nil
}

// ForkSessionRequest contains fork parameters.
type ForkSessionRequest struct {
	SessionID     string `json:"session_id"`
	FromMessageID int64  `json:"from_message_id"`
	Name          string `json:"name,omitempty"`
}

// ForkSession creates a new session by copying messages from the source session
// up to the specified message ID.
func (s *SessionService) ForkSession(ctx context.Context, req ForkSessionRequest) (*session.Session, error) {
	if req.SessionID == "" {
		return nil, wrapError("session", "ForkSession", ErrInvalidInput)
	}
	if req.FromMessageID == 0 {
		return nil, wrapError("session", "ForkSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ForkSession", ErrUnavailable)
	}
	newSession, err := s.store.ForkSession(req.SessionID, req.FromMessageID, req.Name)
	if err != nil {
		return nil, wrapError("session", "ForkSession", err)
	}
	return newSession, nil
}

// ResumeSessionRequest contains resume parameters.
type ResumeSessionRequest struct {
	ID string `json:"id"`
}

// ResumeSession restores a session into active memory by returning its current state.
// The caller (agent loop) handles restoring the conversation from the session store.
func (s *SessionService) ResumeSession(ctx context.Context, req ResumeSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "ResumeSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ResumeSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "ResumeSession", ErrNotFound)
	}
	if err := s.store.UpdateActivity(req.ID); err != nil {
		return nil, wrapError("session", "ResumeSession", err)
	}
	return sess, nil
}

// BranchSessionRequest contains branch navigation parameters.
type BranchSessionRequest struct {
	ID              string `json:"id"`
	TargetMessageID int64  `json:"target_message_id"`
}

// BranchSession navigates to a branch point in the session tree.
func (s *SessionService) BranchSession(ctx context.Context, req BranchSessionRequest) (*session.Session, error) {
	if req.ID == "" {
		return nil, wrapError("session", "BranchSession", ErrInvalidInput)
	}
	if req.TargetMessageID == 0 {
		return nil, wrapError("session", "BranchSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "BranchSession", ErrUnavailable)
	}
	// Navigate the branch in the store
	_, err := s.store.NavigateToBranch(req.ID, req.TargetMessageID)
	if err != nil {
		return nil, wrapError("session", "BranchSession", err)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "BranchSession", ErrNotFound)
	}
	return sess, nil
}

// ListBranchesRequest contains parameters for listing branches.
type ListBranchesRequest struct {
	ID string `json:"id"`
}

// ListBranches returns all branches for a session.
func (s *SessionService) ListBranches(ctx context.Context, req ListBranchesRequest) ([]session.Branch, error) {
	if req.ID == "" {
		return nil, wrapError("session", "ListBranches", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "ListBranches", ErrUnavailable)
	}
	branches, err := s.store.GetMessageBranches(req.ID)
	if err != nil {
		return nil, wrapError("session", "ListBranches", err)
	}
	return branches, nil
}

// GetTreeRequest contains parameters for getting the tree structure.
type GetTreeRequest struct {
	ID string `json:"id"`
}

// GetTree returns the full tree structure for a session.
func (s *SessionService) GetTree(ctx context.Context, req GetTreeRequest) ([]session.TreeNode, error) {
	if req.ID == "" {
		return nil, wrapError("session", "GetTree", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "GetTree", ErrUnavailable)
	}
	nodes, err := s.store.GetTree(req.ID)
	if err != nil {
		return nil, wrapError("session", "GetTree", err)
	}
	return nodes, nil
}

// CompactSessionRequest contains parameters for triggering compaction.
type CompactSessionRequest struct {
	ID string `json:"id"`
}

// CompactSession triggers compaction on a session by inserting a compaction entry.
// This is a manual trigger; normally compaction happens automatically via maybeCompact.
// DesignatedSessionSummary is a minimal view of a session with active designation.
type DesignatedSessionSummary struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	LastActivity string                 `json:"last_activity"`
	Designation  *session.SessionDesignation `json:"designation"`
}

// GetDesignated returns sessions whose designation is non-trivial (not "none").
func (s *SessionService) GetDesignated(ctx context.Context) (int, []DesignatedSessionSummary, error) {
	if s.store == nil {
		return 0, nil, wrapError("session", "GetDesignated", ErrUnavailable)
	}

	designatedIDs, err := s.store.GetDesignatedSessionIDs()
	if err != nil {
		return 0, nil, wrapError("session", "GetDesignated", err)
	}

	// Build map of designated IDs for quick lookup
	designatedMap := make(map[string]bool, len(designatedIDs))
	for _, id := range designatedIDs {
		designatedMap[id] = true
	}

	sessions, err := s.store.List()
	if err != nil {
		return 0, nil, wrapError("session", "GetDesignated", err)
	}

	designatedCount := 0
	result := make([]DesignatedSessionSummary, 0)
	for _, sess := range sessions {
		if !designatedMap[sess.ID] {
			continue
		}
		designation := sess.Designation
		if designation != nil && designation.Status != session.DesignationNone {
			designatedCount++
			result = append(result, DesignatedSessionSummary{
				ID:           sess.ID,
				Name:         sess.Name,
				LastActivity: sess.LastActivity.Format(time.RFC3339),
				Designation:  designation,
			})
		}
	}
	return designatedCount, result, nil
}

func (s *SessionService) CompactSession(ctx context.Context, req CompactSessionRequest) (map[string]any, error) {
	if req.ID == "" {
		return nil, wrapError("session", "CompactSession", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, wrapError("session", "CompactSession", ErrUnavailable)
	}
	sess := s.store.Get(req.ID)
	if sess == nil {
		return nil, wrapError("session", "CompactSession", ErrNotFound)
	}

	// Get current leaf and message count
	leafID, err := s.store.GetLeafMessageID(sess.ID)
	if err != nil {
		return nil, wrapError("session", "CompactSession", err)
	}
	if leafID == 0 {
		return nil, wrapError("session", "CompactSession", ErrInvalidInput)
	}

	// Get current path to check if compaction is needed
	path, err := s.store.GetMessagePath(sess.ID, leafID)
	if err != nil {
		return nil, wrapError("session", "CompactSession", err)
	}

	if len(path) == 0 {
		return map[string]any{
			"status":  "no_messages",
			"message": "no messages to compact",
		}, nil
	}

	return map[string]any{
		"status":        "counted",
		"session_id":    sess.ID,
		"message_count": len(path),
	}, nil
}

// GetDesignation retrieves the designation of a specific session.
func (s *SessionService) GetDesignation(ctx context.Context, sessionID string) (*session.Session, *session.SessionDesignation, error) {
	if sessionID == "" {
		return nil, nil, wrapError("session", "GetDesignation", ErrInvalidInput)
	}
	if s.store == nil {
		return nil, nil, wrapError("session", "GetDesignation", ErrUnavailable)
	}
	sess := s.store.Get(sessionID)
	if sess == nil {
		return nil, nil, wrapError("session", "GetDesignation", ErrNotFound)
	}
	return sess, sess.Designation, nil
}

// AcknowledgeDesignation clears a session's designation and marks it as acknowledged.
func (s *SessionService) AcknowledgeDesignation(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return wrapError("session", "AcknowledgeDesignation", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "AcknowledgeDesignation", ErrUnavailable)
	}

	if err := s.store.ClearDesignation(sessionID); err != nil {
		return wrapError("session", "AcknowledgeDesignation", err)
	}

	return nil
}

// ClearMessages removes all persisted messages for a session, resetting
// the conversation history. The session itself is preserved.
func (s *SessionService) ClearMessages(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return wrapError("session", "ClearMessages", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "ClearMessages", ErrUnavailable)
	}
	if err := s.store.ClearMessages(sessionID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return wrapError("session", "ClearMessages", ErrNotFound)
		}
		return wrapError("session", "ClearMessages", err)
	}
	return nil
}

// ArchiveSessionRequest contains archive parameters.
type ArchiveSessionRequest struct {
	ID       string `json:"id"`
	Archived bool   `json:"archived"`
}

// ArchiveSession sets or clears the archived flag on a session. Archived
// sessions are preserved but sorted to the bottom of the list.
func (s *SessionService) ArchiveSession(ctx context.Context, req ArchiveSessionRequest) error {
	if req.ID == "" {
		return wrapError("session", "ArchiveSession", ErrInvalidInput)
	}
	if s.store == nil {
		return wrapError("session", "ArchiveSession", ErrUnavailable)
	}
	if err := s.store.Archive(req.ID, req.Archived); err != nil {
		// store.Archive returns an error whose message contains "not found"
		// when the session ID does not exist; map to ErrNotFound for
		// consistent HTTP 404 mapping via handleServiceError.
		if strings.Contains(err.Error(), "not found") {
			return wrapError("session", "ArchiveSession", ErrNotFound)
		}
		return wrapError("session", "ArchiveSession", err)
	}
	return nil
}
