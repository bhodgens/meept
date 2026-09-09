package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/tools"
)

// resolveToolPath expands a leading "~" and resolves relative paths
// against the session working directory (tools.WorkingDirFromContext),
// returning an absolute cleaned path. No os.Getwd anywhere: the daemon
// carries the working dir through the context (AGENTS.md). With no
// working dir in ctx, relative paths resolve against the process cwd
// (filepath.Abs) — same fallback the filesystem tools rely on.
func resolveToolPath(ctx context.Context, rawPath string) (string, error) {
	p := rawPath
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand %q: home directory unknown", rawPath)
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		if wd := tools.WorkingDirFromContext(ctx); wd != "" {
			p = filepath.Join(wd, p)
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", rawPath, err)
	}
	return filepath.Clean(abs), nil
}

// containPath verifies that path stays inside one of the allowed roots,
// resolving symlinks on the deepest EXISTING ancestor so not-yet-existing
// target files/dirs still work (EvalSymlinks fails on missing paths; the
// existing-prefix walk degrades to the cleaned path when no ancestor
// exists yet). Roots are compared as absolute paths.
func containPath(path string, roots ...string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	abs = filepath.Clean(abs)

	// Resolve symlinks along the deepest existing ancestor chain so a
	// symlinked directory inside a root cannot smuggle the target out.
	resolved := abs
	dir := abs
	for {
		if _, statErr := os.Stat(dir); statErr == nil {
			if real, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
				resolved = filepath.Join(real, strings.TrimPrefix(resolved, dir))
				// Re-clean: Join/TrimPrefix can reintroduce separators.
				resolved = filepath.Clean(resolved)
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = filepath.Clean(rootAbs)
		// Resolve the root's own symlinks (macOS /var/folders ->
		// /private/var/folders, symlinked workspaces) so root and
		// target are compared in the same real-path space. The root
		// may itself not exist yet (a fallback dir created on first
		// write), so walk to its deepest existing ancestor.
		rootReal := rootAbs
		rdir := rootAbs
		for {
			if _, statErr := os.Stat(rdir); statErr == nil {
				if real, evalErr := filepath.EvalSymlinks(rdir); evalErr == nil {
					rootReal = filepath.Join(real, strings.TrimPrefix(rootReal, rdir))
					rootReal = filepath.Clean(rootReal)
				}
				break
			}
			parent := filepath.Dir(rdir)
			if parent == rdir {
				break
			}
			rdir = parent
		}
		rel, err := filepath.Rel(rootReal, resolved)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside the allowed directories", path)
}

// truncateUTF8 cuts s to at most max bytes without splitting a UTF-8
// rune: when the boundary lands mid-rune, the tail is trimmed back to
// the last valid rune start (utf8.RuneStart loop).
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
