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

// DiscoverGlobalRules finds and loads the global rules content.
// Priority: .meept/RULES.md > ~/.meept/RULES.md > embedded default
func (r *RulesDiscovery) DiscoverGlobalRules() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	// Search paths in priority order
	searchPaths := []string{
		".meept/RULES.md",
		filepath.Join(homeDir, ".meept", "RULES.md"),
	}

	for _, searchPath := range searchPaths {
		path := pathutil.ExpandPath(searchPath)
		content, err := os.ReadFile(path)
		if err == nil {
			r.logger.Debug("Loaded global rules", "path", path)
			return string(content)
		}
	}

	r.logger.Debug("Using embedded default rules")
	return embeddedRules
}

// RulesPath returns the path where the highest-priority rules were found,
// or empty string if using embedded defaults.
func (r *RulesDiscovery) RulesPath() string {
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
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
