package daemon

import (
	"os"
	"path/filepath"
)

// resolveBundledPath resolves a bundled asset path ("config/agents") so the
// daemon finds shipped repo assets regardless of its working directory.
//
// Resolution order:
//  1. If the path exists relative to the current working directory, use it
//     (repo development: daemon launched from the repo root).
//  2. Else, if it exists relative to the executable's directory, use it
//     (installed layouts where config/ ships next to the binary).
//  3. Else fall back to the original relative path — discovery logs a
//     warning when nothing is found, and the user-tier copy in
//     ~/.meept/{agents,prompts,skills} (populated by `make install` /
//     `make sync-config`) is the effective source.
//
// The CWD-first order preserves repo-dev behavior exactly: a developer
// working from the repo root always sees the working tree, even if a stale
// config/ copy sits next to an old binary.
func resolveBundledPath(rel string) string {
	if rel == "" || filepath.IsAbs(rel) {
		return rel
	}
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return rel
}
