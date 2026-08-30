package selfimprove

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TraceOutcome values for TraceRecord.Outcome.
const (
	TraceOutcomeSuccess = "success"
	TraceOutcomeFailure = "failure"
)

// TraceStep is one action within a recorded turn.
type TraceStep struct {
	Action  string `json:"action"`
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	Success bool   `json:"success"`
}

// TraceRecord is the immutable, persisted record of a single learning-eligible
// turn. This is the raw layer of the evidence base (arXiv:2608.27454 §3.1)
// consumed by later sampling/distillation stages; it changes no prompt or
// retrieval surface.
type TraceRecord struct {
	ID             string      `json:"id"`
	SessionID      string      `json:"session_id"`
	Domain         string      `json:"domain,omitempty"`
	Outcome        string      `json:"outcome"`
	Error          string      `json:"error,omitempty"`
	InjectedSkills []string    `json:"injected_skills,omitempty"`
	Steps          []TraceStep `json:"steps"`
	Summary        string      `json:"summary,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
}

// TraceStore persists TraceRecords as immutable JSON files under
// <dir>/traces/<yyyy-mm-dd>/<id>.json using an atomic .tmp + os.Rename
// pattern.
//
// Concurrency: no mutex is held. Write is per-call independent — each call
// derives its own dated directory, writes a uniquely-named .tmp file (IDs from
// pkg/id are collision-resistant), and the rename(2) is atomic, so concurrent
// writers to the same directory cannot corrupt one another's files. No I/O is
// ever performed under any lock (CLAUDE.md mutex-scope rule).
type TraceStore struct {
	dir    string
	logger *slog.Logger
}

// NewTraceStore creates a TraceStore rooted at dir. A nil logger falls back to
// slog.Default().
func NewTraceStore(dir string, logger *slog.Logger) *TraceStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &TraceStore{dir: dir, logger: logger}
}

// Write persists rec as <dir>/traces/<yyyy-mm-dd>/<rec.ID>.json atomically
// (.tmp write + os.Rename). The dated directory is derived from rec.CreatedAt
// so the on-disk layout matches the record's logical turn time rather than the
// wall clock of the write. Returns the final file path.
//
// The record is never mutated; on-disk traces are treated as immutable once
// written.
func (ts *TraceStore) Write(rec *TraceRecord) (string, error) {
	if rec == nil {
		return "", fmt.Errorf("trace store: nil record")
	}
	if rec.ID == "" {
		return "", fmt.Errorf("trace store: record id must not be empty")
	}
	dateDir := rec.CreatedAt.Format("2006-01-02")
	outDir := filepath.Join(ts.dir, "traces", dateDir)
	//nolint:gosec // trace dirs are user-readable project data
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("trace store: mkdir %s: %w", outDir, err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("trace store: marshal: %w", err)
	}
	finalPath := filepath.Join(outDir, rec.ID+".json")
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil { //nolint:gosec // trace files are user-readable project data
		return "", fmt.Errorf("trace store: write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("trace store: rename %s -> %s: %w", tmpPath, finalPath, err)
	}
	return finalPath, nil
}

// truncationMarker is appended to step Input/Output strings capped by
// Sample's maxChars budget so consumers (the evolver prompt) can be honest
// about truncated evidence.
const truncationMarker = "...[truncated]"

// Sample returns up to maxFails failure records followed by up to maxPasses
// success records, walking date directories newest-first. Within a date
// directory, files are sorted descending by filename (IDs sort lexically;
// acceptable ordering for v1 — we do not keep an index).
//
// Each returned record's step text is capped to maxChars total across Input
// and Output strings; overflowing strings are truncated with a
// "...[truncated]" marker. Corrupt or unreadable individual files are logged
// at Debug and skipped, never returned as errors; only directory-level I/O
// failures are surfaced.
func (ts *TraceStore) Sample(maxFails, maxPasses, maxChars int) ([]TraceRecord, error) {
	tracesRoot := filepath.Join(ts.dir, "traces")
	dateDirs, err := os.ReadDir(tracesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("trace store: read %s: %w", tracesRoot, err)
	}

	// Newest date directory first.
	sort.Slice(dateDirs, func(i, j int) bool {
		return dateDirs[i].Name() > dateDirs[j].Name()
	})

	var (
		fails  []TraceRecord
		passes []TraceRecord
	)
	for _, dd := range dateDirs {
		if !dd.IsDir() || !isDateDirName(dd.Name()) {
			continue
		}
		dirPath := filepath.Join(tracesRoot, dd.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			ts.logger.Debug("trace store: skipping unreadable date dir",
				"dir", dirPath, "error", err)
			continue
		}
		// Descending filename order within the dir (lexical-by-ID, v1).
		sort.Slice(files, func(i, j int) bool {
			return files[i].Name() > files[j].Name()
		})
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			if len(fails) >= maxFails && len(passes) >= maxPasses {
				return append(fails, passes...), nil
			}
			rec, rerr := ts.readTraceFile(filepath.Join(dirPath, f.Name()), maxChars)
			if rerr != nil {
				ts.logger.Debug("trace store: skipping unreadable trace file",
					"file", f.Name(), "error", rerr)
				continue
			}
			switch rec.Outcome {
			case TraceOutcomeFailure:
				if len(fails) < maxFails {
					fails = append(fails, *rec)
				}
			default: // anything non-"failure" counts as a pass bucket record
				if len(passes) < maxPasses {
					passes = append(passes, *rec)
				}
			}
		}
	}
	return append(fails, passes...), nil
}

// readTraceFile loads and unmarshals a single trace file and applies the
// per-record maxChars cap to its step text.
func (ts *TraceStore) readTraceFile(path string, maxChars int) (*TraceRecord, error) {
	//nolint:gosec // path is constructed from the traces root
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("trace store: read %s: %w", path, err)
	}
	var rec TraceRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("trace store: unmarshal %s: %w", path, err)
	}
	if rec.ID == "" {
		return nil, fmt.Errorf("trace store: %s has empty id", path)
	}
	capSteps(&rec, maxChars)
	return &rec, nil
}

// capSteps truncates the record's step Input/Output strings so their combined
// length fits within maxChars, appending the truncation marker. Step Action
// and Success are never touched. maxChars <= 0 disables capping.
func capSteps(rec *TraceRecord, maxChars int) {
	if maxChars <= 0 {
		return
	}
	budget := maxChars
	for i := range rec.Steps {
		step := &rec.Steps[i]
		step.Input = truncateWithMarker(step.Input, &budget)
		step.Output = truncateWithMarker(step.Output, &budget)
	}
}

// truncateWithMarker returns s, or a truncated s + "...[truncated]" if s does
// not fit the remaining budget. The budget is decremented by the returned
// length.
func truncateWithMarker(s string, budget *int) string {
	if s == "" || *budget <= 0 {
		return ""
	}
	if len(s) <= *budget {
		*budget -= len(s)
		return s
	}
	keep := *budget - len(truncationMarker)
	if keep < 0 {
		keep = 0
	}
	*budget = 0
	return s[:keep] + truncationMarker
}

// isDateDirName reports whether name looks like a yyyy-mm-dd directory.
func isDateDirName(name string) bool {
	if len(name) != len("2006-01-02") {
		return false
	}
	if _, err := time.Parse("2006-01-02", name); err != nil {
		return false
	}
	return true
}
