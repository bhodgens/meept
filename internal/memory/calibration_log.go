package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// calibrationRecord is one line of claim_verdicts.jsonl: a human/librarian
// verdict on an auto-claim, paired with the claim's extraction-time
// confidence. This is the confidence-calibration dataset: confidence vs
// human verdict on live traffic.
type calibrationRecord struct {
	TS         string  `json:"ts"`
	ClaimID    string  `json:"claim_id"`
	Verdict    string  `json:"verdict"` // promote | reject | supersede | resolve
	Confidence float64 `json:"confidence"`
	Text       string  `json:"text"` // claim text (raw user content — gitignored path only)
	SourceType string  `json:"source_type,omitempty"`
}

// CalibrationLogger appends claim verdicts to <dataDir>/claim_verdicts.jsonl.
// Nil-safe; write errors disable the logger (best-effort — calibration must
// never break the promote/reject path).
type CalibrationLogger struct {
	mu   sync.Mutex // guards the disable-once state; appends serialize on the OS file
	path string
	log  *slog.Logger
}

// NewCalibrationLogger creates a logger writing into the memory data dir.
// Empty dataDir returns nil (disabled).
func NewCalibrationLogger(dataDir string, log *slog.Logger) *CalibrationLogger {
	if dataDir == "" {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &CalibrationLogger{
		path: filepath.Join(dataDir, "claim_verdicts.jsonl"),
		log:  log.With("component", "claim-calibration"),
	}
}

// LogVerdict appends one verdict record. Snapshot the path under the lock,
// write outside it (mutexio rule); a write error disables the logger.
func (c *CalibrationLogger) LogVerdict(ctx context.Context, claimID, verdict string, mem *Memory) {
	if c == nil {
		return
	}
	c.mu.Lock()
	path := c.path
	c.mu.Unlock()
	if path == "" {
		return
	}
	rec := calibrationRecord{
		TS:         time.Now().UTC().Format(time.RFC3339),
		ClaimID:    claimID,
		Verdict:    verdict,
		Confidence: confFloat(mem.Metadata["confidence"]),
		Text:       mem.Content,
		SourceType: string(mem.Type),
	}
	if err := appendJSONL(path, rec); err != nil {
		c.mu.Lock()
		c.path = ""
		c.mu.Unlock()
		c.log.Warn("calibration logging disabled", "error", err)
	}
}

// appendJSONL appends one value as a JSON line, creating the parent dir.
// Rotation first: the calibration log is size-capped (see RotateIfNeeded) so
// append-heavy verdict traffic cannot grow it unbounded.
func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := RotateIfNeeded(path, maxCalibrationLogBytes); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// confFloat extracts a float64 confidence from stored metadata (stored as
// float64 via map[string]any; tolerate other numerics defensively).
func confFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return -1
	}
}
