// Package l0 records raw conversations as append-only JSONL: the L0 tier of
// the layered async-distillation memory pipeline (Tree 01). One JSON object
// per line in <dataDir>/l0/YYYY-MM-DD.jsonl, schema frozen in
// docs/plans/20261012-memory-capability-forest/SHARED-CONVENTIONS.md §2.1.
package l0

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/id"
)

// Record is one L0 line. The JSON shape is frozen (SHARED-CONVENTIONS §2.1):
// {"id","ts","session_id","role","content","meta"}.
type Record struct {
	ID        string         `json:"id"`
	TS        string         `json:"ts"` // RFC3339
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"` // user | assistant
	Content   string         `json:"content"`
	Meta      map[string]any `json:"meta"`
}

// Recorder appends raw conversation turns to daily JSONL files under
// <dataDir>/l0/. Appends from concurrent goroutines serialize on the
// Recorder's mutex; each append marshals under the lock and writes via a
// single O_APPEND write outside it (mutexio rule — no I/O under lock; one
// write syscall keeps concurrent Recorder instances on the same file
// line-atomic in practice).
//
// The zero value is not usable; construct with NewRecorder.
type Recorder struct {
	mu    sync.Mutex
	dir   string // <dataDir>/l0
	clock func() time.Time
}

// NewRecorder creates a recorder writing into <dataDir>/l0. A nil location
// means time.Local.
func NewRecorder(dataDir string, loc *time.Location) *Recorder {
	if loc == nil {
		loc = time.Local
	}
	return &Recorder{
		dir:   filepath.Join(dataDir, "l0"),
		clock: func() time.Time { return time.Now().In(loc) },
	}
}

// Append records one conversation turn. Injected <memory-context> spans are
// stripped from content before persisting (feedback-loop guard; the
// stripping logic lives in internal/memory.SanitizeForStore — do not
// duplicate it here).
func (r *Recorder) Append(sessionID, role, content string, meta map[string]any) error {
	r.mu.Lock()
	now := r.clock()
	dir := r.dir
	rec := Record{
		ID:        id.Generate("msg"),
		TS:        now.Format(time.RFC3339),
		SessionID: sessionID,
		Role:      role,
		Content:   memory.SanitizeForStore(content),
		Meta:      meta,
	}
	line, err := json.Marshal(rec)
	r.mu.Unlock()
	if err != nil {
		return fmt.Errorf("l0: marshal record: %w", err)
	}
	line = append(line, '\n')

	path := filepath.Join(dir, memory.LocalDayKey(now)+".jsonl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("l0: create dir %s: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("l0: open %s: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("l0: write %s: %w", path, err)
	}
	return f.Close()
}

// DatePath returns the JSONL path the recorder writes for the given
// YYYY-MM-DD date (readers and the async-distill job payload use this).
func (r *Recorder) DatePath(date string) string {
	return filepath.Join(r.dir, date+".jsonl")
}
