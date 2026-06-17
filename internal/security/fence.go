package security

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// FenceConfig controls path fencing for a session.
type FenceConfig struct {
	Enabled   bool     // Whether fencing is active
	RootPath  string   // The project worktree path (sandbox root)
	AllowRead []string // System paths allowed for read even outside root
	NoFence   bool     // Per-session override from --nofence
}

// FenceChecker validates paths against fence boundaries.
type FenceChecker struct {
	cfg    FenceConfig
	valid  bool   // Whether RootPath is valid
	logger *slog.Logger
}

// NewFenceChecker creates a new fence checker.
func NewFenceChecker(cfg FenceConfig, logger *slog.Logger) *FenceChecker {
	fc := &FenceChecker{cfg: cfg, logger: logger}
	// Validate RootPath on construction
	if cfg.Enabled && !cfg.NoFence {
		if err := fc.validateRootPath(); err != nil {
			if logger != nil {
				logger.Warn("FenceChecker misconfigured - fencing disabled", "error", err)
			}
			fc.valid = false
		} else {
			fc.valid = true
		}
	} else {
		fc.valid = true // Not enabled, so no validation needed
	}
	return fc
}

// validateRootPath checks that RootPath is absolute and not a trivial path.
func (fc *FenceChecker) validateRootPath() error {
	if fc.cfg.RootPath == "" {
		return fmt.Errorf("RootPath is empty")
	}
	absRoot, err := filepath.Abs(fc.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("cannot resolve RootPath: %w", err)
	}
	if absRoot == "/" || absRoot == "." {
		return fmt.Errorf("RootPath resolves to %q - too permissive", absRoot)
	}
	return nil
}

// Valid returns false if the FenceChecker is misconfigured (invalid RootPath).
// When invalid, CheckPath will return an error for all operations.
func (fc *FenceChecker) Valid() bool {
	return fc.valid
}

// resolveSymlinks resolves symlinks in a path, even if the final component
// doesn't exist yet. It walks up to the longest existing ancestor, resolves
// symlinks there, then appends the remaining non-existent suffix.
//
// Returns (", false) when no existing ancestor could be resolved (i.e.
// EvalSymlinks failed on every ancestor including the filesystem root). In
// normal operation this never happens because EvalSymlinks("/") always
// succeeds; the failure case exists as defense-in-depth for misconfigured or
// broken environments. Callers must treat a false return as "path cannot be
// safely resolved" and refuse the operation rather than falling back to the
// raw input — returning an unresolved path would allow crafted inputs such as
// "/../etc/passwd" to bypass the fence when the filesystem is in an
// unexpected state.
func resolveSymlinks(path string) (string, bool) {
	// Normalize relative paths with .. components before symlink resolution
	path = filepath.Clean(path)
	if evaled, err := filepath.EvalSymlinks(path); err == nil {
		return evaled, true
	}
	// Walk up to find an existing ancestor, then re-append the rest.
	p := path
	suffix := ""
	for {
		if evaled, err := filepath.EvalSymlinks(p); err == nil {
			if suffix == "" {
				return evaled, true
			}
			return filepath.Join(evaled, suffix), true
		}
		suffix = filepath.Join(filepath.Base(p), suffix)
		p = filepath.Dir(p)
		if p == "/" || p == "." {
			// Fail closed: every ancestor failed to resolve, including the
			// filesystem root. Returning the unresolvable input would allow
			// traversal payloads to skip the fence.
			return "", false
		}
	}
}

// CheckPath validates a path against the fence.
// op is "read", "write", or "exec".
// Returns nil if allowed, error if blocked or misconfigured.
func (fc *FenceChecker) CheckPath(path string, op string) error {
	if fc.cfg.NoFence || !fc.cfg.Enabled {
		return nil
	}

	// If fence is enabled but misconfigured, block all operations
	if !fc.valid {
		return fmt.Errorf("fence: misconfigured (invalid RootPath)")
	}

	// filepath.Abs calls filepath.Clean internally; resolveSymlinks cleans
	// again, so an explicit Clean here is redundant (S1-2).
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("fence: cannot resolve path: %w", err)
	}
	abs, ok := resolveSymlinks(abs)
	if !ok {
		return fmt.Errorf("fence: cannot resolve symlinks for %q", path)
	}

	// Check if path is within root
	rootAbs, err := filepath.Abs(fc.cfg.RootPath)
	if err != nil {
		return fmt.Errorf("fence: cannot resolve root path: %w", err)
	}
	root, ok := resolveSymlinks(rootAbs)
	if !ok {
		return fmt.Errorf("fence: cannot resolve symlinks for root %q", fc.cfg.RootPath)
	}
	if strings.HasPrefix(abs, root+string(os.PathSeparator)) || abs == root {
		return nil
	}

	// Check allow-read system paths
	if op == "read" {
		for _, allowed := range fc.cfg.AllowRead {
			allowedAbs, err := filepath.Abs(allowed)
			if err != nil {
				continue
			}
			resolved, ok := resolveSymlinks(allowedAbs)
			if !ok {
				continue
			}
			if strings.HasPrefix(abs, resolved+string(os.PathSeparator)) || abs == resolved {
				return nil
			}
		}
	}

	return fmt.Errorf("fence: %s access denied for %q (outside project root %q)", op, path, fc.cfg.RootPath)
}

// CheckCommand validates a shell command working directory.
func (fc *FenceChecker) CheckCommand(cmd string, workDir string) error {
	if fc.cfg.NoFence || !fc.cfg.Enabled {
		return nil
	}
	return fc.CheckPath(workDir, "exec")
}

// IsNoFence returns true if fencing is disabled.
func (fc *FenceChecker) IsNoFence() bool {
	return fc.cfg.NoFence
}
