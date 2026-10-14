package l0

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// datePath is the shared path layout: <dir>/YYYY-MM-DD.jsonl.
func datePath(dir, date string) string {
	return filepath.Join(dir, date+".jsonl")
}

// Prune deletes whole daily JSONL files in dir whose date is strictly older
// than the cutoff day (cutoff date minus maxAgeDays), returning the number
// of files removed. Only files matching the frozen YYYY-MM-DD.jsonl layout
// are considered; anything else in dir is left alone. Called from the
// consolidation run (wired in the integration leaf, not here).
func Prune(dir string, maxAgeDays int) (int, error) {
	if maxAgeDays < 0 {
		return 0, fmt.Errorf("l0: prune: maxAgeDays must be >= 0, got %d", maxAgeDays)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("l0: prune: read dir %s: %w", dir, err)
	}

	// Cutoff is the oldest day that survives: today minus maxAgeDays. A file
	// dated exactly at the cutoff SURVIVES (boundary is exclusive below it),
	// so maxAgeDays=1 keeps today and yesterday.
	//
	// The day frame is the LOCAL calendar date — the same frame the recorder
	// names L0 files with (recorder.go via memory.LocalDayKey; audit H4). A
	// UTC-truncated cutoff against locally-named files moved the boundary by
	// a day for every local time west of UTC between (24-offset) and
	// midnight, deleting (or keeping) a file up to a day off its documented
	// age.
	today := memory.LocalDayStart(time.Now())
	cutoff := today.AddDate(0, 0, -maxAgeDays)

	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		dateStr := strings.TrimSuffix(name, ".jsonl")
		day, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
		if err != nil {
			continue // not a daily file; skip
		}
		if !day.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("l0: prune: remove %s: %w", name, err)
		}
		removed++
	}
	return removed, nil
}
