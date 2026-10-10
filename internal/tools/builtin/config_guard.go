package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// protectedConfigNames are the files inside MEEPT_HOME that configure the daemon
// itself. An agent tool must never create or overwrite them: the daemon reads
// them at startup, so a bad write makes the daemon unbootable until an operator
// repairs the file by hand.
var protectedConfigNames = []string{
	"meept.json5",
	"meept.toml",
}

// protectedConfigDirs are MEEPT_HOME subdirectories that hold operator
// credentials and daemon-owned state. They are covered as a whole so a new
// sensitive file added later is protected without a code change.
var protectedConfigDirs = []string{
	"skills",  // installed skill definitions
	"agents",  // employee definitions
	"keys",    // credential material
	"secrets", // credential material
}

// guardProtectedConfigPath refuses a write whose destination is a
// MEEPT_HOME control file or credential directory.
//
// WHY THIS EXISTS — the 2026-10-09 config destruction. At 14:28 the coder
// agent, running a benchmark connectivity probe, called
//
//	file_write {"path":"/Users/caimlas/.meept/meept.json5",
//	            "content":"probed.","append":false,"direct":true}
//
// which replaced a 30 KB operator config with 7 bytes. The daemon then refused
// to start (hujson: invalid literal: probed.) and every setting the operator
// had changed was lost, because WriteMainConfigAtomic's .bak had never been
// produced for this file.
//
// The two existing gates did not catch it, for reasons worth recording:
//
//   - The fence (internal/security/fence.go) bounds paths to a project
//     RootPath. A benchmark task runs with the fence disabled per-session via
//     session.set_nofence / --nofence (internal/security/fence.go:173 returns
//     nil immediately when NoFence is set), so there was no bound at all.
//   - The permission checker (t.checker.CheckPath) is about operator allowlists
//     for system paths, not about daemon-owned state.
//
// So the guard is deliberately INDEPENDENT of both: it keys off the resolved
// MEEPT_HOME and the destination path alone. Disabling the fence, running
// headless, or setting direct:true cannot bypass it, which is precisely the
// configuration in which the incident happened.
//
// Append mode is refused too. The config is JSON5, so appending to it produces
// a file that fails to parse just as badly as overwriting it, and there is no
// legitimate agent use for appending to the daemon's own config.
func guardProtectedConfigPath(resolved string, appendMode bool) error {
	// Resolve symlinks on BOTH sides. On macOS a temp dir arrives as /var/... but
	// resolves to /private/var/..., so comparing a resolved home against an
	// unresolved target never matches and the guard silently passes everything.
	// Symlink resolution is the point of the check: it also stops a symlink
	// inside the home from pointing out at a protected file.
	home, err := filepath.EvalSymlinks(config.MeeptHome())
	if err != nil {
		// MeeptHome does not exist yet (fresh install). Nothing is protected
		// from a path that cannot exist; let the normal write proceed.
		return nil
	}
	target, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		// Target does not exist yet, so EvalSymlinks fails. Fall back to the
		// lexical absolute path so a CREATE of ~/.meept/meept.json5 is still
		// caught, resolving the deepest existing ancestor so the prefix
		// comparison still holds against the resolved home.
		abs, aerr := filepath.Abs(resolved)
		if aerr != nil {
			return nil
		}
		target = resolveExistingAncestor(filepath.Clean(abs))
	}

	if !pathWithin(target, home) {
		return nil
	}

	base := filepath.Base(target)
	inProtectedDir := false
	for _, d := range protectedConfigDirs {
		if pathWithin(target, filepath.Join(home, d)) {
			inProtectedDir = true
			break
		}
	}
	if inProtectedDir {
		return fmt.Errorf(
			"refusing to write %s: %s is daemon-owned operator state inside %s. "+
				"Agents must not create or modify credentials, skills, or agent "+
				"definitions; a bad write here breaks the next daemon start",
			target, target, home)
	}

	for _, name := range protectedConfigNames {
		if base != name {
			continue
		}
		if appendMode {
			return fmt.Errorf(
				"refusing to append to %s: it is the daemon's own %s config, and "+
					"appending JSON5 text makes it unparseable", target, name)
		}
		return fmt.Errorf(
			"refusing to overwrite %s: it is the daemon's own %s config. "+
				"Use `meept config set` (which validates, backs up to %s.bak, and "+
				"writes atomically) instead of a file write",
			target, name, target)
	}
	return nil
}

// pathWithin reports whether target is root itself or lives beneath it. Both
// sides must already be absolute and, ideally, symlink-resolved.
func pathWithin(target, root string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

// resolveExistingAncestor returns the symlink-resolved form of the deepest
// existing ancestor of path, with the non-existent remainder appended. A
// not-yet-created file under an existing directory therefore still compares
// correctly against a resolved root — which is what makes the guard fire on
// CREATE as well as overwrite.
func resolveExistingAncestor(path string) string {
	remainder := ""
	cur := path
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if remainder == "" {
				return resolved
			}
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the filesystem root without finding anything that exists.
			return path
		}
		remainder = filepath.Join(filepath.Base(cur), remainder)
		cur = parent
	}
}
