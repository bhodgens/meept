package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// rejectedCandidateRecord is one line of the rejected-candidate JSONL: an
// ambient candidate the confidence/category gate dropped. This is the
// calibration population — the model's own "I'm unsure" signal paired with
// the exact text it was unsure about. Raw conversation text: gitignored path
// only, never enters the repo.
type rejectedCandidateRecord struct {
	TS         string  `json:"ts"`
	Intent     string  `json:"intent"`
	Text       string  `json:"text"`
	Type       string  `json:"type"`
	Category   string  `json:"category,omitempty"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"` // "confidence" | "category" | "max_per_turn"
	Threshold  float64 `json:"threshold"`
}

// RejectedCandidateLogger appends filtered-out ambient candidates to a JSONL
// file under the memory data dir. Nil-safe: a nil logger (or an empty path)
// disables logging entirely. Writes are serialized; each line is flushed on
// write so a daemon crash loses at most the in-flight record.
type RejectedCandidateLogger struct {
	mu   sync.Mutex
	path string
	log  *slog.Logger
}

// NewRejectedCandidateLogger creates a logger writing to <dataDir>/rejected_candidates.jsonl.
// The directory is created on first write, not at construction, so a
// read-only environment stays construction-safe.
func NewRejectedCandidateLogger(dataDir string, log *slog.Logger) *RejectedCandidateLogger {
	if dataDir == "" {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &RejectedCandidateLogger{
		path: filepath.Join(dataDir, "rejected_candidates.jsonl"),
		log:  log.With("component", "rejected-candidates"),
	}
}

// LogRejected appends the given filtered-out candidates. Best-effort: an
// unwritable path logs a warning once per writer and stops attempting (a
// broken calibration log must never break extraction).
func (r *RejectedCandidateLogger) LogRejected(intent string, threshold float64, rejected []rejectedCandidateRecord) {
	if r == nil || len(rejected) == 0 {
		return
	}
	// Collect the path under the lock, then write outside it: the mutex
	// protects the disable-once state, and a dedicated write mutex serializes
	// concurrent appends so I/O never holds the state lock (mutexio rule).
	r.mu.Lock()
	path := r.path
	r.mu.Unlock()
	if path == "" {
		return
	}
	if err := r.appendAll(path, intent, threshold, rejected); err != nil {
		r.mu.Lock()
		r.path = ""
		r.mu.Unlock()
		r.log.Warn("rejected-candidates logging disabled", "error", err)
	}
}

func (r *RejectedCandidateLogger) appendAll(path, intent string, threshold float64, rejected []rejectedCandidateRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, rec := range rejected {
		rec.TS = time.Now().UTC().Format(time.RFC3339)
		rec.Intent = intent
		rec.Threshold = threshold
		if err := enc.Encode(rec); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// splitFiltered separates gate-passing candidates from the rejected remainder,
// mirroring filterAmbientCandidates' gates. It returns the same passing slice
// the filter would, plus a rejection reason per dropped candidate so the
// calibration log records WHY each one was dropped.
func splitFiltered(in []memory.AmbientCandidate, threshold float64, excludedCat map[string]struct{}, max int) (passed []memory.AmbientCandidate, rejected []rejectedCandidateRecord) {
	for _, c := range in {
		switch {
		case c.Confidence < threshold:
			rejected = append(rejected, rejectRec(c, "confidence"))
		case catExcluded(c.Category, excludedCat):
			rejected = append(rejected, rejectRec(c, "category"))
		case len(passed) >= max:
			rejected = append(rejected, rejectRec(c, "max_per_turn"))
		default:
			passed = append(passed, c)
		}
	}
	return passed, rejected
}

func rejectRec(c memory.AmbientCandidate, reason string) rejectedCandidateRecord {
	return rejectedCandidateRecord{
		Text:       c.Text,
		Type:       c.Type,
		Category:   c.Category,
		Confidence: c.Confidence,
		Reason:     reason,
	}
}

func catExcluded(cat string, excluded map[string]struct{}) bool {
	_, ok := excluded[cat]
	return ok
}
