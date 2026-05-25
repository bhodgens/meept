package debug

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AdapterConfig describes a DAP debug adapter and how to launch it.
type AdapterConfig struct {
	Name        string   // Human-readable adapter name (e.g. "dlv", "gdb")
	Command     string   // Executable name or path
	Args        []string // Additional arguments before DAP subcommand
	FileTypes   []string // File extensions this adapter handles (e.g. ".go")
	RootMarkers []string // Project root marker files (e.g. "go.mod")
	Transport   string   // "stdio" or "socket" (default: "stdio")
}

// defaultAdapters lists built-in adapter configurations, ordered by specificity.
var defaultAdapters = []AdapterConfig{
	{
		Name:        "dlv",
		Command:     "dlv",
		Args:        []string{"dap"},
		FileTypes:   []string{".go"},
		RootMarkers: []string{"go.mod"},
		Transport:   "stdio",
	},
	{
		Name:        "gdb",
		Command:     "gdb",
		Args:        []string{"-i", "dap"},
		FileTypes:   []string{".c", ".cpp", ".cc", ".cxx", ".h", ".hpp"},
		RootMarkers: []string{"Makefile", "CMakeLists.txt"},
		Transport:   "stdio",
	},
	{
		Name:        "lldb-dap",
		Command:     "lldb-dap",
		Args:        nil,
		FileTypes:   []string{".c", ".cpp", ".cc", ".cxx", ".m", ".mm", ".swift", ".rs"},
		RootMarkers: []string{"Package.swift", "Cargo.toml"},
		Transport:   "stdio",
	},
	{
		Name:        "debugpy",
		Command:     "python3",
		Args:        []string{"-m", "debugpy.adapter"},
		FileTypes:   []string{".py"},
		RootMarkers: []string{"requirements.txt", "pyproject.toml", "setup.py", "Pipfile"},
		Transport:   "stdio",
	},
	{
		Name:        "codelldb",
		Command:     "codelldb",
		Args:        nil,
		FileTypes:   []string{".c", ".cpp", ".cc", ".rs"},
		RootMarkers: []string{"Cargo.toml"},
		Transport:   "stdio",
	},
}

// DetectAdapter determines the best DAP adapter for the given program path
// and working directory. It checks file extension first, then project root
// markers, and finally whether the adapter binary is available in $PATH.
func DetectAdapter(program string, workDir string) (*AdapterConfig, error) {
	ext := strings.ToLower(filepath.Ext(program))

	// Score each adapter by how well it matches.
	type scored struct {
		adapter *AdapterConfig
		score   int
	}
	var candidates []scored

	for i := range defaultAdapters {
		a := &defaultAdapters[i]
		s := 0

		// Check file extension match
		for _, ft := range a.FileTypes {
			if ext == ft {
				s += 10
				break
			}
		}

		// Check root markers
		for _, marker := range a.RootMarkers {
			p := filepath.Join(workDir, marker)
			if _, err := os.Stat(p); err == nil {
				s += 5
				break
			}
		}

		// Check binary availability
		if _, err := exec.LookPath(a.Command); err == nil {
			s += 2
		}

		if s > 0 {
			candidates = append(candidates, scored{adapter: a, score: s})
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no DAP adapter found for %q (extension %q); install dlv, gdb, lldb-dap, debugpy, or codelldb", program, ext)
	}

	// Pick highest-scoring candidate.
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}

	return best.adapter, nil
}

// FindAdapterByName returns a built-in adapter config by name.
// Returns nil if not found.
func FindAdapterByName(name string) *AdapterConfig {
	for i := range defaultAdapters {
		if defaultAdapters[i].Name == name {
			return &defaultAdapters[i]
		}
	}
	return nil
}
