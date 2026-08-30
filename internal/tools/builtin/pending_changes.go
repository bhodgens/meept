package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// sha256Hex computes the lowercase hex SHA256 of s.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// shortHash returns a compact prefix of a hex digest for error messages.
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// PendingChange represents a file modification awaiting acceptance.
type PendingChange struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	FilePath  string `json:"file_path"`
	Original  string `json:"original"` // Original file content
	Modified  string `json:"modified"` // Modified content (preview)
	Diff      string `json:"diff"`     // Unified diff preview
	// PreImageSHA256 is the lowercase hex SHA256 of Original at staging time.
	// Empty means legacy change created before integrity tracking; accept
	// treats it as "proceed with warning" for mid-upgrade compatibility.
	PreImageSHA256 string         `json:"pre_image_sha256"`
	CreatedAt      time.Time      `json:"created_at"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// PendingChangesRegistry manages session-scoped pending changes.
type PendingChangesRegistry struct {
	mu       sync.RWMutex
	changes  map[string]*PendingChange // keyed by change ID
	sessions map[string][]string       // sessionID -> change IDs

	// Background expiration lifecycle. stopCh/doneCh are lazily allocated
	// inside Start() so that registries which never call Start (e.g. test
	// fixtures, fallback local registries) don't need explicit teardown.
	stopCh         chan struct{}
	doneCh         chan struct{}
	expireInterval time.Duration
	startOnce      sync.Once
	stopOnce       sync.Once
}

// NewPendingChangesRegistry creates a new pending changes registry.
func NewPendingChangesRegistry() *PendingChangesRegistry {
	return &PendingChangesRegistry{
		changes:  make(map[string]*PendingChange),
		sessions: make(map[string][]string),
	}
}

// Add registers a new pending change.
func (r *PendingChangesRegistry) Add(change *PendingChange) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.changes[change.ID] = change
	r.sessions[change.SessionID] = append(r.sessions[change.SessionID], change.ID)
}

// StageWrite stages a file write for later acceptance: it computes a diff
// preview, records the SHA256 of the pre-image (original content) so drift
// can be detected at accept time, registers the change, and returns it.
//
// For file creation, pass original == nil or empty; PreImageSHA256 is then
// sha256("") and accept verifies the file does not exist / is empty.
//
// I/O note: no filesystem access happens here — hashing and diffing operate
// on in-memory bytes only, so this method performs no I/O while holding the
// registry mutex.
func (r *PendingChangesRegistry) StageWrite(sessionID, path string, original, modified []byte) (*PendingChange, error) {
	if path == "" {
		return nil, fmt.Errorf("StageWrite: path is required")
	}

	now := time.Now()
	expiresAt := now.Add(30 * time.Minute)

	change := &PendingChange{
		ID:             id.Generate("stage-"),
		SessionID:      sessionID,
		FilePath:       path,
		Original:       string(original),
		Modified:       string(modified),
		Diff:           generateUnifiedDiff(path, string(original), string(modified)),
		PreImageSHA256: sha256Hex(string(original)),
		CreatedAt:      now,
		ExpiresAt:      &expiresAt,
		Metadata: map[string]any{
			"tool": "stage_write",
			"new":  len(original) == 0,
		},
	}

	r.Add(change)
	return change, nil
}

// generateUnifiedDiff creates a unified-diff-style preview between original
// and modified content. This is the shared implementation used by StageWrite;
// FileEditTool.generateDiffPreview delegates here so all staged changes carry
// identically-formatted diffs.
func generateUnifiedDiff(filePath, original, modified string) string {
	// Simple unified diff format
	lines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	var diff []string
	diff = append(diff, fmt.Sprintf("--- a/%s", filePath))
	diff = append(diff, fmt.Sprintf("+++ b/%s", filePath))

	// Simple line-by-line comparison
	maxLen := max(len(lines), len(modLines))

	for i := range maxLen {
		oldLine := ""
		newLine := ""
		if i < len(lines) {
			oldLine = lines[i]
		}
		if i < len(modLines) {
			newLine = modLines[i]
		}

		if i >= len(lines) {
			// Added line
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		} else if i >= len(modLines) {
			// Deleted line
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
		} else if oldLine != newLine {
			// Changed line
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		}
	}

	return strings.Join(diff, "\n")
}

// Get retrieves a pending change by ID.
func (r *PendingChangesRegistry) Get(id string) (*PendingChange, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	change, ok := r.changes[id]
	return change, ok
}

// Remove removes a change by ID (after accept/reject).
func (r *PendingChangesRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.changes[id]
	if !ok {
		return
	}

	// Remove from session tracking
	if change.SessionID != "" {
		sessionChanges, exists := r.sessions[change.SessionID]
		if exists {
			for i, cid := range sessionChanges {
				if cid == id {
					r.sessions[change.SessionID] = append(sessionChanges[:i], sessionChanges[i+1:]...)
					break
				}
			}
			// Clean up empty session entry
			if len(r.sessions[change.SessionID]) == 0 {
				delete(r.sessions, change.SessionID)
			}
		}
	}

	delete(r.changes, id)
}

// GetBySession returns all pending changes for a session.
func (r *PendingChangesRegistry) GetBySession(sessionID string) []*PendingChange {
	r.mu.RLock()
	defer r.mu.RUnlock()

	changeIDs, ok := r.sessions[sessionID]
	if !ok {
		return nil
	}

	changes := make([]*PendingChange, 0, len(changeIDs))
	for _, id := range changeIDs {
		if change, ok := r.changes[id]; ok {
			changes = append(changes, change)
		}
	}
	return changes
}

// RemoveBySession removes all pending changes for a session (e.g., on session end).
func (r *PendingChangesRegistry) RemoveBySession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	changeIDs, ok := r.sessions[sessionID]
	if !ok {
		return
	}

	for _, id := range changeIDs {
		delete(r.changes, id)
	}
	delete(r.sessions, sessionID)
}

// Expire removes changes that have passed their expiration time.
func (r *PendingChangesRegistry) Expire() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for id, change := range r.changes {
		if change.ExpiresAt != nil && now.After(*change.ExpiresAt) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		change, exists := r.changes[id]
		if !exists {
			continue // Already removed by another goroutine
		}
		// Remove from session tracking
		if change.SessionID != "" {
			sessionChanges, sessExists := r.sessions[change.SessionID]
			if sessExists {
				for i, cid := range sessionChanges {
					if cid == id {
						r.sessions[change.SessionID] = append(sessionChanges[:i], sessionChanges[i+1:]...)
						break
					}
				}
				// Clean up empty session entry
				if len(r.sessions[change.SessionID]) == 0 {
					delete(r.sessions, change.SessionID)
				}
			}
		}
		delete(r.changes, id)
	}
}

// SetExpiry sets an expiration time for a change.
func (r *PendingChangesRegistry) SetExpiry(id string, expiresAt time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.changes[id]
	if !ok {
		return false
	}

	change.ExpiresAt = &expiresAt
	return true
}

// Start launches a background goroutine that periodically calls Expire() to
// reap pending changes whose ExpiresAt has passed. interval is the tick
// cadence; a value <= 0 falls back to the default of 5 minutes.
//
// Start is idempotent: calling it multiple times is a no-op after the
// first successful invocation. The caller is expected to invoke Stop() to
// release the background goroutine.
func (r *PendingChangesRegistry) Start(interval time.Duration) {
	if r == nil {
		return
	}

	r.startOnce.Do(func() {
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		r.expireInterval = interval
		r.stopCh = make(chan struct{})
		r.doneCh = make(chan struct{})

		go func() {
			defer close(r.doneCh)

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					r.Expire()
				case <-r.stopCh:
					return
				}
			}
		}()
	})
}

// Stop signals the background expiration goroutine to exit and blocks until
// it has terminated. Safe to call when Start was never invoked (no-op) and
// idempotent on subsequent calls.
func (r *PendingChangesRegistry) Stop() {
	if r == nil {
		return
	}

	r.stopOnce.Do(func() {
		if r.stopCh != nil {
			close(r.stopCh)
		}
	})

	if r.doneCh != nil {
		<-r.doneCh
	}
}
