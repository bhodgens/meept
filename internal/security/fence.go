package security

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FenceConfig controls path fencing for a session.
type FenceConfig struct {
	Enabled   bool     // Whether fencing is active
	RootPath  string   // The project worktree path (sandbox root)
	AllowRead []string // System paths allowed for read even outside root
	NoFence   bool     // Per-session override from --nofence
}

// FenceChecker validates paths against fence boundaries.
//
// A single FenceChecker is shared by every tool in the process, so its
// configuration may be updated per-session (SetRootPath / SetNoFence).
// All reads take an RLock and operate on a snapshot of the config;
// writes take the full lock (mutexio rule).
type FenceChecker struct {
	mu     sync.RWMutex
	cfg    FenceConfig
	valid  bool   // Whether RootPath is valid
	logger *slog.Logger
}

// NewFenceChecker creates a new fence checker.
func NewFenceChecker(cfg FenceConfig, logger *slog.Logger) *FenceChecker {
	fc := &FenceChecker{cfg: cfg, logger: logger}
	// Validate RootPath on construction
	if cfg.Enabled && !cfg.NoFence {
		if err := validateRootPath(cfg.RootPath); err != nil {
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

// validateRootPath checks that root is absolute and not a trivial path.
func validateRootPath(root string) error {
	if root == "" {
		return fmt.Errorf("RootPath is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("cannot resolve RootPath: %w", err)
	}
	if absRoot == "/" || absRoot == "." {
		return fmt.Errorf("RootPath resolves to %q - too permissive", absRoot)
	}
	return nil
}

// SetRootPath updates the sandbox root for this session. It validates the
// candidate root before applying it; on validation failure the previous
// configuration is left untouched and the error is returned.
func (fc *FenceChecker) SetRootPath(root string) error {
	if err := validateRootPath(root); err != nil {
		return fmt.Errorf("fence: SetRootPath rejected %q: %w", root, err)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.cfg.RootPath = root
	fc.recomputeValidLocked()
	return nil
}

// SetNoFence toggles the per-session no-fence override (--nofence).
func (fc *FenceChecker) SetNoFence(enabled bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.cfg.NoFence = enabled
	fc.recomputeValidLocked()
}

// snapshot returns a copy of the current config under RLock.
// AllowRead is shared (read-only by convention); callers must not mutate it.
func (fc *FenceChecker) snapshot() FenceConfig {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.cfg
}

// recomputeValidLocked recomputes fc.valid. Caller must hold fc.mu.
func (fc *FenceChecker) recomputeValidLocked() {
	if fc.cfg.Enabled && !fc.cfg.NoFence {
		fc.valid = validateRootPath(fc.cfg.RootPath) == nil
	} else {
		fc.valid = true // Not enforcing, so no validation needed
	}
}

// Valid returns false if the FenceChecker is misconfigured (invalid RootPath).
// When invalid, CheckPath will return an error for all operations.
func (fc *FenceChecker) Valid() bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.valid
}

// resolveSymlinks resolves symlinks in a path, handling non-existent final
// components (common for write operations). It resolves symlinks by finding
// the longest-existing prefix and resolving from there.
//
// FAIL-HARD behavior: If the filesystem root "/" cannot be resolved, returns
// ("", false). This is defense-in-depth against crafted paths designed to
// exploit resolution errors.
func resolveSymlinks(path string) (string, bool) {
	// Normalize the path first
	path = filepath.Clean(path)

	// Try direct resolution - works for existing paths
	evaled, err := filepath.EvalSymlinks(path)
	if err == nil {
		return evaled, true
	}

	// Path doesn't exist. Find the longest-existing prefix by walking down
	// from root, resolving each component and building the result incrementally.
	parts := splitPath(path)
	resolvedSoFar := "/"

	for i, part := range parts {
		// Build candidate by appending the next component to what we've resolved
		candidate := filepath.Join(resolvedSoFar, part)
		if evaledCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
			resolvedSoFar = evaledCandidate
		} else {
			// This component doesn't exist. Append remaining components as-is.
			remaining := filepath.Join(parts[i:]...)
			if resolvedSoFar == "/" {
				return "/" + remaining, true
			}
			return resolvedSoFar + "/" + remaining, true
		}
	}

	// All components existed (should have been caught by initial EvalSymlinks)
	return resolvedSoFar, true
}

// splitPath splits an absolute path into its components.
// E.g., "/a/b/c" -> ["a", "b", "c"]
func splitPath(path string) []string {
	path = filepath.Clean(path)
	if path == "/" {
		return nil
	}
	if path[0] == '/' {
		path = path[1:]
	}
	return strings.Split(path, "/")
}

// CheckPath validates a path against the fence.
// op is "read", "write", or "exec".
// Returns nil if allowed, error if blocked or misconfigured.
func (fc *FenceChecker) CheckPath(path string, op string) error {
	cfg := fc.snapshot()
	if cfg.NoFence || !cfg.Enabled {
		return nil
	}

	// If fence is enabled but misconfigured, block all operations
	if !fc.Valid() {
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
	rootAbs, err := filepath.Abs(cfg.RootPath)
	if err != nil {
		return fmt.Errorf("fence: cannot resolve root path: %w", err)
	}
	root, ok := resolveSymlinks(rootAbs)
	if !ok {
		return fmt.Errorf("fence: cannot resolve symlinks for root %q", cfg.RootPath)
	}
	if strings.HasPrefix(abs, root+string(os.PathSeparator)) || abs == root {
		return nil
	}

	// Check allow-read system paths
	if op == "read" {
		for _, allowed := range cfg.AllowRead {
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

	return fmt.Errorf("fence: %s access denied for %q (outside project root %q)", op, path, cfg.RootPath)
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
	cfg := fc.snapshot()
	if cfg.NoFence || !cfg.Enabled {
		return nil
	}

	// First check the working directory
	if err := fc.CheckPath(workDir, "exec"); err != nil {
		return err
	}

// SECURITY FIX: Detect shell command substitution patterns that could
	// bypass path validation. These patterns execute nested commands that
	// we cannot validate because they're hidden inside the substitution.
	// Patterns: $(), ``, $(()), >(), <()
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "=$((") {
		return fmt.Errorf("fence: command substitution $() is not allowed: %q", cmd)
	}
	if strings.Contains(cmd, "`") {
		return fmt.Errorf("fence: backtick command substitution is not allowed: %q", cmd)
	}
	// Process substitution <() and >() can also hide commands
	if strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") {
		return fmt.Errorf("fence: process substitution <() >() is not allowed: %q", cmd)
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
	return fc.snapshot().NoFence
}
