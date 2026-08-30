package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrRunNotFound is returned by DiskStore.Get when no run record with the
// given ID exists. Match with errors.Is.
var ErrRunNotFound = errors.New("eval: run not found")

// maxListRuns caps DiskStore.List results, newest first.
const maxListRuns = 50

// DiskStore persists RunRecords as one JSON file per run in a flat
// directory (conventionally <home>/.meept/eval). The directory is created
// lazily on first write; the CALLER resolves the home directory and injects
// the absolute path — nothing here calls os.Getwd or os.UserHomeDir.
type DiskStore struct {
	Dir string
}

// NewDiskStore returns a DiskStore rooted at dir.
func NewDiskStore(dir string) *DiskStore {
	return &DiskStore{Dir: dir}
}

// recordPath returns the file path for a run ID, refusing anything that
// smells like path traversal regardless of where the ID came from.
func (s *DiskStore) recordPath(runID string) (string, error) {
	if runID == "" || runID == "." || runID == ".." ||
		strings.Contains(runID, "/") ||
		strings.Contains(runID, "\\") ||
		strings.Contains(runID, "..") {
		return "", fmt.Errorf("eval: invalid run id %q", runID)
	}
	return filepath.Join(s.Dir, runID+".json"), nil
}

// Save writes rec to disk as <dir>/<id>.json, creating the directory if
// needed. Existing records with the same ID are overwritten.
func (s *DiskStore) Save(ctx context.Context, rec RunRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Dir == "" {
		return errors.New("eval: disk store directory is empty")
	}
	if rec.ID == "" {
		return errors.New("eval: run record has empty id")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("eval: create store dir: %w", err)
	}
	path, err := s.recordPath(rec.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal run record: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("eval: write run record: %w", err)
	}
	return nil
}

// Get reads one run record by ID. Unknown IDs return ErrRunNotFound.
func (s *DiskStore) Get(ctx context.Context, runID string) (RunRecord, error) {
	if err := ctx.Err(); err != nil {
		return RunRecord{}, err
	}
	path, err := s.recordPath(runID)
	if err != nil {
		return RunRecord{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RunRecord{}, ErrRunNotFound
		}
		return RunRecord{}, fmt.Errorf("eval: read run record: %w", err)
	}
	var rec RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return RunRecord{}, fmt.Errorf("eval: parse run record: %w", err)
	}
	return rec, nil
}

// List returns up to maxListRuns run records, newest first by CreatedAt.
// A missing directory is not an error: it yields an empty list.
func (s *DiskStore) List(ctx context.Context) ([]RunRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RunRecord{}, nil
		}
		return nil, fmt.Errorf("eval: list run records: %w", err)
	}
	records := make([]RunRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, entry.Name()))
		if err != nil {
			continue // best effort: skip unreadable entries
		}
		var rec RunRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	if len(records) > maxListRuns {
		records = records[:maxListRuns]
	}
	return records, nil
}
