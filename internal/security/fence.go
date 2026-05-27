package security

import (
	"fmt"
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
	cfg FenceConfig
}

// NewFenceChecker creates a new fence checker.
func NewFenceChecker(cfg FenceConfig) *FenceChecker {
	return &FenceChecker{cfg: cfg}
}

// resolveSymlinks resolves symlinks in a path, even if the final component
// doesn't exist yet. It walks up to the longest existing ancestor, resolves
// symlinks there, then appends the remaining non-existent suffix.
func resolveSymlinks(path string) string {
	if evaled, err := filepath.EvalSymlinks(path); err == nil {
		return evaled
	}
	// Walk up to find an existing ancestor, then re-append the rest.
	p := path
	suffix := ""
	for {
		if evaled, err := filepath.EvalSymlinks(p); err == nil {
			if suffix == "" {
				return evaled
			}
			return filepath.Join(evaled, suffix)
		}
		suffix = filepath.Join(filepath.Base(p), suffix)
		p = filepath.Dir(p)
		if p == "/" || p == "." {
			return path
		}
	}
}

// CheckPath validates a path against the fence.
// op is "read", "write", or "exec".
// Returns nil if allowed, error if blocked.
func (fc *FenceChecker) CheckPath(path string, op string) error {
	if fc.cfg.NoFence || !fc.cfg.Enabled {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("fence: cannot resolve path: %w", err)
	}
	abs = resolveSymlinks(abs)

	// Check if path is within root
	root, _ := filepath.Abs(fc.cfg.RootPath)
	root = resolveSymlinks(root)
	if strings.HasPrefix(abs, root+string(os.PathSeparator)) || abs == root {
		return nil
	}

	// Check allow-read system paths
	if op == "read" {
		for _, allowed := range fc.cfg.AllowRead {
			allowedAbs, _ := filepath.Abs(allowed)
			allowedAbs = resolveSymlinks(allowedAbs)
			if strings.HasPrefix(abs, allowedAbs+string(os.PathSeparator)) || abs == allowedAbs {
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
