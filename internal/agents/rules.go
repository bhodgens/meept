package agents

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/pathutil"
)

//go:embed embedded_rules.md
var embeddedRules string

// DefaultRulesContent returns the default global rules content.
// This is used when no RULES.md is found in any tier.
func DefaultRulesContent() string {
	return embeddedRules
}

// RulesDiscovery searches for RULES.md files in a priority hierarchy.
type RulesDiscovery struct {
	logger *slog.Logger
}

// NewRulesDiscovery creates a new RulesDiscovery instance.
func NewRulesDiscovery(logger *slog.Logger) *RulesDiscovery {
	if logger == nil {
		logger = slog.Default()
	}
	return &RulesDiscovery{logger: logger}
}

// findRulesFile returns the path and content of the highest-priority rules file.
// Returns ("", embeddedRules) if no file is found in any tier.
func (r *RulesDiscovery) findRulesFile() (path, content string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	searchPaths := []string{
		".meept/RULES.md",
		filepath.Join(homeDir, ".meept", "RULES.md"),
	}

	for _, searchPath := range searchPaths {
		path := pathutil.ExpandPath(searchPath)
		content, err := os.ReadFile(path)
		if err == nil {
			return path, string(content)
		}
	}

	return "", embeddedRules
}

// DiscoverWithPath returns the global rules content and the file path it was loaded from.
// Path is empty when using embedded defaults.
func (r *RulesDiscovery) DiscoverWithPath() (content, path string) {
	path, content = r.findRulesFile()
	return content, path
}

// DiscoverGlobalRules finds and loads the global rules content.
// Priority: .meept/RULES.md > ~/.meept/RULES.md > embedded default
func (r *RulesDiscovery) DiscoverGlobalRules() string {
	_, content := r.findRulesFile()
	return content
}

// RulesPath returns the path where the highest-priority rules were found,
// or empty string if using embedded defaults.
func (r *RulesDiscovery) RulesPath() string {
	path, _ := r.findRulesFile()
	return path
}
