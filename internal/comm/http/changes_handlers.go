package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// Change review endpoints (containment leaf 07). These expose the pending
// changes registry and the change journal to human reviewers (TUI, Flutter,
// curl) without going through the agent. Auth is inherited from the server's
// APIKeyAuth middleware like every other /api/v1/* route.
//
// Wired via WithChangesAPI; when the changes API is not wired, list routes
// answer 503 and accept/reject/revert answer 503.

// pendingChangeListItem is the list-endpoint representation of a pending
// change. Deliberately excludes Original/Modified so the endpoint streams
// diffs (small) rather than full file bodies (potentially megabytes).
type pendingChangeListItem struct {
	ID        string  `json:"id"`
	FilePath  string  `json:"file_path"`
	Diff      string  `json:"diff"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt *string `json:"expires_at"`
}

// handleListPendingChanges returns the staged pending changes for one
// session: GET /api/v1/sessions/{sid}/pending-changes.
func (s *Server) handleListPendingChanges(w http.ResponseWriter, r *http.Request) {
	if s.changesRegistry == nil {
		s.writeError(w, http.StatusServiceUnavailable, "pending changes API not enabled")
		return
	}

	sessionID := r.PathValue("sid")
	if sessionID == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	changes := s.changesRegistry.GetBySession(sessionID)
	items := make([]pendingChangeListItem, 0, len(changes))
	for _, c := range changes {
		item := pendingChangeListItem{
			ID:        c.ID,
			FilePath:  c.FilePath,
			Diff:      c.Diff,
			CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		}
		if c.ExpiresAt != nil {
			expires := c.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z07:00")
			item.ExpiresAt = &expires
		}
		items = append(items, item)
	}

	s.writeJSON(w, http.StatusOK, items)
}

// handleAcceptPendingChange applies a staged change:
// POST /api/v1/pending-changes/{id}/accept. Acceptance reuses the resolve
// tool's shared accept path (fence re-validation, drift check, write,
// journal record) so agent-driven and human-driven accepts never diverge.
func (s *Server) handleAcceptPendingChange(w http.ResponseWriter, r *http.Request) {
	if s.changesResolve == nil {
		s.writeError(w, http.StatusServiceUnavailable, "pending changes API not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "change id is required")
		return
	}

	if _, err := s.changesResolve.AcceptChange(id); err != nil {
		switch {
		case errors.Is(err, builtin.ErrChangeNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, builtin.ErrChangeDrift):
			s.writeError(w, http.StatusConflict, err.Error())
		default:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// handleRejectPendingChange discards a staged change without touching the
// file: POST /api/v1/pending-changes/{id}/reject.
func (s *Server) handleRejectPendingChange(w http.ResponseWriter, r *http.Request) {
	if s.changesRegistry == nil {
		s.writeError(w, http.StatusServiceUnavailable, "pending changes API not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "change id is required")
		return
	}

	if _, ok := s.changesRegistry.Get(id); !ok {
		s.writeError(w, http.StatusNotFound, "pending change not found: "+id)
		return
	}

	// Reject: drop the staged change; the on-disk file stays untouched.
	s.changesRegistry.Remove(id)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// journalListItem is the list-endpoint representation of a journal entry.
// Pre-image bytes stay server-side; only the byte count is exposed so
// clients can show a revertable/size column without a megabyte transfer.
type journalListItem struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	FilePath     string   `json:"file_path"`
	PostSHA      string   `json:"post_sha"`
	AppliedAt    string   `json:"applied_at"`
	ChangeIDs    []string `json:"change_ids"`
	PreImageSize int64    `json:"pre_image_size"`
}

// handleListJournal lists applied changes:
// GET /api/v1/changes/journal?session=<sid>&limit=<n>.
func (s *Server) handleListJournal(w http.ResponseWriter, r *http.Request) {
	if s.changesJournal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "change journal not enabled")
		return
	}

	sessionID := r.URL.Query().Get("session")
	limit := 0 // journal.List applies its own default for non-positive limits
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		// Malformed or negative limits fall back to the journal default
		// rather than failing: this is a read-only list endpoint polled by
		// UI panels, and limit is only an optimization hint.
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	entries, err := s.changesJournal.List(sessionID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]journalListItem, 0, len(entries))
	for _, e := range entries {
		changeIDs := e.ChangeIDs
		if changeIDs == nil {
			changeIDs = []string{}
		}
		size, sizeErr := s.changesJournal.PreImageSize(e.ID)
		if sizeErr != nil {
			s.logger.Warn("journal list: pre-image size lookup failed", "entry_id", e.ID, "error", sizeErr)
		}
		items = append(items, journalListItem{
			ID:           e.ID,
			SessionID:    e.SessionID,
			FilePath:     e.FilePath,
			PostSHA:      e.PostSHA,
			AppliedAt:    e.AppliedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
			ChangeIDs:    changeIDs,
			PreImageSize: size,
		})
	}

	s.writeJSON(w, http.StatusOK, items)
}

// handleRevertJournalEntry restores a file to its pre-change content:
// POST /api/v1/changes/journal/{id}/revert. Drift (file changed since
// apply) maps to 409; a size-capped entry without a journaled pre-image
// maps to 400; an unknown entry maps to 404.
func (s *Server) handleRevertJournalEntry(w http.ResponseWriter, r *http.Request) {
	if s.changesJournal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "change journal not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "journal entry id is required")
		return
	}

	// Reuse the fence the resolve tool already carries so reverts respect
	// the same path policy as accepts.
	var fence builtin.FenceChecker
	if s.changesResolve != nil {
		fence = s.changesResolve.FenceChecker()
	}

	path, err := s.changesJournal.Revert(id, fence)
	if err != nil {
		switch {
		case errors.Is(err, builtin.ErrEntryNotFound):
			s.writeError(w, http.StatusNotFound, err.Error())
		case strings.Contains(err.Error(), "pre-image not journaled"):
			s.writeError(w, http.StatusBadRequest, err.Error())
		case strings.Contains(err.Error(), "changed since apply"):
			s.writeError(w, http.StatusConflict, err.Error())
		case strings.Contains(err.Error(), "fence refused"):
			s.writeError(w, http.StatusForbidden, err.Error())
		default:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"restored_path": path})
}
