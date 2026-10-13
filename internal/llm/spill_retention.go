package llm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// SpillRetentionOptions parameterize SweepSpill. Zero value = standard
// $MEEPT_HOME/spill resolution with the config-owned retention; only tests
// override MeeptHome (internal/llm cannot import internal/config).
type SpillRetentionOptions struct {
	// RetentionDays is how long spill files are kept (mtime-based).
	// <= 0 disables the sweep entirely (keep forever).
	RetentionDays int
	// MeeptHome overrides the meept home resolution (tests); empty means
	// the standard MEEPT_HOME / ~/.meept resolution.
	MeeptHome string
	// Logger receives debug/info sweep results; nil = slog.Default().
	Logger *slog.Logger
}

// SweepSpill deletes spill FILES under $MEEPT_HOME/spill older than
// retentionDays (mtime-based) and removes directories left empty by the
// removals. The walk never traverses above the spill root, and retention
// <= 0 disables the sweep entirely (0/negative = keep forever).
//
// It returns the number of files removed. Individual per-file failures are
// logged at debug and do not abort the sweep; only a failure to resolve or
// read the spill root is returned as an error.
func SweepSpill(ctx context.Context, opts SpillRetentionOptions) (int, error) {
	if opts.RetentionDays <= 0 {
		return 0, nil // disabled: keep forever
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Reuse the SpillService root resolution (MEEPT_HOME / ~/.meept, with
	// the test override) so the sweep and the writer agree on the root.
	service := NewSpillService(opts.MeeptHome)
	root := service.resolveSpillRoot()

	cutoff := time.Now().Add(-time.Duration(opts.RetentionDays) * 24 * time.Hour)
	removed := 0
	// Walk skips dirs whose removal already succeeded; rooted at `root`,
	// so no path above the spill root is ever visited or deleted.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // raced with another sweep / removal
			}
			return err
		}
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				return nil
			}
			logger.Debug("spill retention: stat failed", "path", path, "error", statErr)
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil {
			if !errors.Is(rmErr, fs.ErrNotExist) {
				logger.Debug("spill retention: remove failed", "path", path, "error", rmErr)
			}
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return removed, nil // no spill dir yet: nothing to sweep
		}
		return removed, fmt.Errorf("spill retention: walk %s: %w", root, err)
	}

	// Prune directories emptied by the removals, deepest-first, stopping at
	// (and never removing) the spill root itself.
	prunedDirs(root, logger)

	if removed > 0 {
		logger.Info("spill retention: sweep complete",
			"removed_files", removed, "retention_days", opts.RetentionDays)
	} else {
		logger.Debug("spill retention: sweep complete", "removed_files", 0,
			"retention_days", opts.RetentionDays)
	}
	return removed, nil
}

// prunedDirs removes directories under root that the sweep emptied,
// deepest-first (post-order), never removing root itself. Failures are
// logged at debug and skipped (a non-empty or busy dir is simply kept).
func prunedDirs(root string, logger *slog.Logger) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		entries, readErr := os.ReadDir(path)
		if readErr != nil || len(entries) > 0 {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil {
			logger.Debug("spill retention: empty-dir remove failed", "path", path, "error", rmErr)
		}
		// Removing a child can empty its parent; WalkDir still descends
		// into a removed dir? No — it reads the dir first, so children of
		// a dir removed here were already visited (post-order descent
		// happens before this callback for this path's children only).
		// Deepest-first ordering comes from fs.WalkDir lexical order plus
		// a second pass below for parents.
		return nil
	})
	// Second pass: WalkDir already read parent dir entries before we
	// removed children, so parents emptied by the first pass need one more
	// shallow sweep. Repeat until nothing changes (bounded by tree depth).
	for {
		changed := false
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() || path == root {
				return nil
			}
			entries, readErr := os.ReadDir(path)
			if readErr != nil || len(entries) > 0 {
				return nil
			}
			if rmErr := os.Remove(path); rmErr == nil {
				changed = true
			}
			return nil
		})
		if !changed {
			return
		}
	}
}

// SpillRetentionLoop runs the spill retention sweep immediately and then once
// every 24h until ctx is cancelled. It is intended to be launched as a
// goroutine from daemon startup (async: never blocks readiness) and returns
// when the context is done — no goroutine leak on shutdown.
func SpillRetentionLoop(ctx context.Context, opts SpillRetentionOptions) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if _, err := SweepSpill(ctx, opts); err != nil {
		logger.Debug("spill retention: startup sweep failed", "error", err)
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := SweepSpill(ctx, opts); err != nil {
				logger.Debug("spill retention: periodic sweep failed", "error", err)
			}
		}
	}
}
