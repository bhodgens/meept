package project

import (
	"os"
	"path/filepath"
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
	for i := len(stack) - 1; i >= 0; i-- {
		e := stack[i]
		agentsPath := filepath.Join(e.absPath, "AGENTS.md")
		if data, err := os.ReadFile(agentsPath); err == nil {
			results = append(results, AgentsMD{RelPath: e.relPath, Content: string(data)})
		}
	}

	return results, nil
}

// LoadAllAgentsMD walks the entire projectRoot tree and returns ALL AGENTS.md
// files found at any depth, ordered root-to-leaf (breadth-first among siblings).
func LoadAllAgentsMD(projectRoot string) ([]AgentsMD, error) {
	if projectRoot == "" {
		return nil, nil
	}

	var results []AgentsMD

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip directories we can't access
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "AGENTS.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip unreadable files
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
