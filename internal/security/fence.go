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

// CheckCommand validates a shell command and its working directory.
// It checks both the working directory and any explicit file path arguments
// in the command to prevent access to files outside the fence boundary.
//
// C-03 FIX: Instead of substring-matching a hardcoded list of sensitive paths,
// we tokenize the command and validate each path-like token against CheckPath.
// This catches absolute paths and parent-traversal sequences in any command,
// not just a fixed set of file-access utilities.
func (fc *FenceChecker) CheckCommand(cmd string, workDir string) error {
	if fc.cfg.NoFence || !fc.cfg.Enabled {
		return nil
	}

	// First check the working directory
	if err := fc.CheckPath(workDir, "exec"); err != nil {
		return err
	}

	// Tokenize the command and extract path-like arguments for validation.
	// We do not attempt to parse shell metacharacters — we just identify
	// tokens that look like file paths and validate each against the fence.
	tokens := tokenizeCommand(cmd)
	for i, tok := range tokens {
		// Check for shell redirection operators — the following token is
		// the redirect target and must be validated as a write path.
		if tok == ">" || tok == ">>" || tok == "2>" || tok == "&>" || tok == "1>" {
			if i+1 < len(tokens) {
				target := extractPathFromToken(tokens[i+1])
				if target != "" {
					if err := fc.CheckPath(target, "write"); err != nil {
						return fmt.Errorf("fence: redirect target outside project root: %q", target)
					}
				}
			}
			continue
		}
		// Check for inline redirection (e.g. ">/outside/file")
		if strings.HasPrefix(tok, ">") || strings.HasPrefix(tok, "2>") || strings.HasPrefix(tok, "&>") {
			if remainder := strings.TrimLeft(tok, ">&12"); remainder != "" && remainder != tok {
				target := extractPathFromToken(remainder)
				if target != "" {
					if err := fc.CheckPath(target, "write"); err != nil {
						return fmt.Errorf("fence: redirect target outside project root: %q", target)
					}
				}
				continue
			}
		}
		path := extractPathFromToken(tok)
		if path == "" {
			continue
		}
		if err := fc.CheckPath(path, "read"); err != nil {
			return fmt.Errorf("fence: command references path outside project root: %q", path)
		}
	}

	return nil
}

// tokenizeCommand splits a command string into whitespace-delimited tokens,
// stripping surrounding quotes from each token. This is intentionally simple —
// we don't evaluate shell syntax, we just need to identify path-like arguments.
func tokenizeCommand(cmd string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && i+1 < len(cmd):
			// Preserve escape pair
			current.WriteByte(c)
			current.WriteByte(cmd[i+1])
			i++
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// extractPathFromToken checks whether a token looks like a file path and
// returns the path if so. Returns "" for non-path tokens (flags, env vars,
// command names without path separators).
func extractPathFromToken(tok string) string {
	if tok == "" {
		return ""
	}

	// Skip flags (-f, --verbose)
	if strings.HasPrefix(tok, "-") {
		return ""
	}

	// Skip env variable assignments (FOO=bar)
	if strings.Contains(tok, "=") && !strings.HasPrefix(tok, "/") && !strings.HasPrefix(tok, "~") {
		return ""
	}

	// Skip single-token command names with no path component
	isPathLike := false

	// Absolute paths
	if strings.HasPrefix(tok, "/") {
		isPathLike = true
	}

	// Home directory paths
	if strings.HasPrefix(tok, "~") {
		isPathLike = true
	}

	// Relative paths with parent traversal (../)
	if strings.Contains(tok, "..") {
		isPathLike = true
	}

	// Relative paths with explicit ./ prefix
	if strings.HasPrefix(tok, "./") {
		isPathLike = true
	}

	if !isPathLike {
		return ""
	}

	return tok
}

// IsNoFence returns true if fencing is disabled.
func (fc *FenceChecker) IsNoFence() bool {
	return fc.cfg.NoFence
}
