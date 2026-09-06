package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// anchorDirName is the subdirectory (under the employees data dir) holding
// periodic digest snapshots.
const anchorDirName = "anchors"

// anchorFileName is the JSONL file digests append to.
const anchorFileName = "audit-anchors.jsonl"

// ExportDigest appends the current chain head to <dir>/anchors/
// audit-anchors.jsonl as one canonical JSON line:
// {"exported_at":"<RFC3339Nano>","seq":<N>,"chain_head":"<hex>"}
// An empty chain writes no line. Returns the file path.
func (s *Store) ExportDigest(ctx context.Context, dir string) (string, error) {
	head, ok, err := s.Head(ctx)
	if err != nil {
		return "", fmt.Errorf("export digest: %w", err)
	}
	anchorPath := filepath.Join(dir, anchorDirName, anchorFileName)
	if err := os.MkdirAll(filepath.Dir(anchorPath), 0o700); err != nil {
		return "", fmt.Errorf("export digest: %w", err)
	}
	if !ok {
		// Empty chain: no line is written, but the anchor file is still
		// created so off-host copy tooling always finds the JSONL.
		f, err := os.OpenFile(anchorPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("export digest: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("export digest: %w", err)
		}
		return anchorPath, nil
	}
	line, err := json.Marshal(map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339Nano),
		"seq":         head.Seq,
		"chain_head":  head.RecordHash,
	})
	if err != nil {
		return "", fmt.Errorf("export digest: %w", err)
	}
	f, err := os.OpenFile(anchorPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("export digest: %w", err)
	}
	defer f.Close() //nolint:errcheck // write error below is the reported one
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", fmt.Errorf("export digest: %w", err)
	}
	return anchorPath, nil
}

// DefaultAnchorInterval is the export cadence when a job is constructed
// with a non-positive interval.
const DefaultAnchorInterval = time.Hour

// AnchorJob periodically appends the chain digest to the anchors JSONL file.
// It holds no mutex: Run is the only mutation path and it is single-goroutine
// by construction (mutexio-safe).
type AnchorJob struct {
	store    *Store
	dir      string
	interval time.Duration
	logger   *slog.Logger
}

// NewAnchorJob constructs the periodic digest exporter. A non-positive
// interval falls back to DefaultAnchorInterval. A nil logger falls back to
// slog.Default().
func NewAnchorJob(store *Store, dir string, interval time.Duration, logger *slog.Logger) *AnchorJob {
	if interval <= 0 {
		interval = DefaultAnchorInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AnchorJob{store: store, dir: dir, interval: interval, logger: logger}
}

// ExportOnce exports the digest immediately and returns the file path.
func (j *AnchorJob) ExportOnce(ctx context.Context) (string, error) {
	return j.store.ExportDigest(ctx, j.dir)
}

// Run blocks, exporting every interval, until ctx is done.
func (j *AnchorJob) Run(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			path, err := j.ExportOnce(ctx)
			if err != nil {
				j.logger.Warn("audit anchor export failed", "err", err)
				continue
			}
			j.logger.Debug("audit anchor exported", "path", path)
		}
	}
}
