package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// AgentsMD represents a loaded AGENTS.md file with its relative path and content.
type AgentsMD struct {
	// RelPath is the relative path from the project root (e.g. "", "internal/agent").
	RelPath string
	// Content is the raw file content.
	Content string
}

// LoadAgentsMDForPath walks from the project root toward the directory containing
// filePath, collecting all AGENTS.md files found along the way. Results are
// ordered root-to-leaf.
//
// If filePath is empty, only the root AGENTS.md is checked.
// If no AGENTS.md files are found, an empty slice is returned.
func LoadAgentsMDForPath(projectRoot, filePath string) ([]AgentsMD, error) {
	if projectRoot == "" {
		return nil, nil
	}

	// If no file path, just check root.
	if filePath == "" {
		rootAgents := filepath.Join(projectRoot, "AGENTS.md")
		if data, err := os.ReadFile(rootAgents); err == nil {
			return []AgentsMD{{RelPath: "", Content: string(data)}}, nil
		}
		return nil, nil
	}

	var results []AgentsMD

	// Walk from project root toward the file's directory, collecting AGENTS.md
	// at every level.
	dir := filepath.Dir(filePath)
	if dir == "" || dir == "." {
		dir = projectRoot
	}

	// Walk up from file's directory, pushing onto a stack so we can emit
	// root-to-leaf order.
	type entry struct {
		relPath string
		absPath string
	}
	var stack []entry

	current := dir
	for {
		r, err := filepath.Rel(projectRoot, current)
		if err != nil {
			break
		}
		if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			// Walked past project root.
			break
		}
		stack = append(stack, entry{relPath: r, absPath: current})

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// Reverse to get root-to-leaf order.
	for _, e := range slices.Backward(stack) {
		agentsPath := filepath.Join(e.absPath, "AGENTS.md")
		if data, err := os.ReadFile(agentsPath); err == nil {
			results = append(results, AgentsMD{RelPath: e.relPath, Content: string(data)})
		}
	}

	return results, nil
}

// FindProjectRoot walks up from startDir looking for the nearest directory
// that marks a project root (a ".git" entry — directory for a normal repo,
// file for a worktree). When no marker is found, startDir itself is returned
// so callers always get a usable (conservative) root: the chain then covers
// just startDir, never the whole filesystem.
func FindProjectRoot(startDir string) string {
	if startDir == "" {
		return ""
	}
	current := filepath.Clean(startDir)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding a marker.
			return filepath.Clean(startDir)
		}
		current = parent
	}
}

// LoadAgentsMDChain returns the AGENTS.md files along the directory chain
// from projectRoot down to dir (inclusive), ordered root-to-leaf. This is the
// ambient-context scoping for issue #193: a session working in dir gets the
// root conventions plus every ancestor-directory invariant doc, and NOTHING
// from sibling or deeper packages. dir outside projectRoot yields only the
// files on the shared chain (root-to-common-ancestor); an empty dir yields
// only the root file when present.
//
// RelPath is normalised to "" for the project root (unlike
// LoadAgentsMDForPath, which reports the raw filepath.Rel value ".").
func LoadAgentsMDChain(projectRoot, dir string) ([]AgentsMD, error) {
	if projectRoot == "" || dir == "" {
		return nil, nil
	}
	// LoadAgentsMDForPath keys off filepath.Dir(filePath); join a sentinel
	// filename so the walk starts AT dir.
	loaded, err := LoadAgentsMDForPath(projectRoot, filepath.Join(filepath.Clean(dir), "AGENTS.md"))
	if err != nil {
		return nil, err
	}
	for i := range loaded {
		if loaded[i].RelPath == "." {
			loaded[i].RelPath = ""
		}
	}
	return loaded, nil
}

// LoadAllAgentsMD walks the entire projectRoot tree and returns ALL AGENTS.md
// files found at any depth, ordered root-to-leaf (breadth-first among siblings).
//
// Issue #193: ambient per-turn injection defaults to LoadAgentsMDChain (root
// -> cwd ancestors only); this whole-tree loader now serves only the explicit
// AgentConfig.AgentsContextWholeTree escape hatch and full-audit callers —
// do not use it for per-turn ambient context.
func LoadAllAgentsMD(projectRoot string) ([]AgentsMD, error) {
	if projectRoot == "" {
		return nil, nil
	}

	var results []AgentsMD

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip directories we can't access
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "AGENTS.md" {
			return nil
		}
		//nolint:gosec // G122: path comes from the WalkDir callback; this is a
		// best-effort read-only loader over an already-trusted project root.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil //nolint:nilerr // Skip unreadable files
		}
		rel, _ := filepath.Rel(projectRoot, filepath.Dir(path))
		if rel == "." {
			rel = ""
		}
		results = append(results, AgentsMD{RelPath: rel, Content: string(data)})
		return nil
	})

	if err != nil {
		return nil, err
	}
	return results, nil
}
