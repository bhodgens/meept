package agents

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// DefaultTiers returns the standard discovery tiers for agents.
func DefaultTiers() []DiscoveryTier {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	return []DiscoveryTier{
		{Path: ".meept/agents", Priority: PriorityProject},
		{Path: filepath.Join(homeDir, ".meept", "agents"), Priority: PriorityUser},
		{Path: filepath.Join(homeDir, ".config", "meept", "agents"), Priority: PrioritySystem},
	}
}

// Discovery scans filesystem tiers to discover AGENT.md files.
type Discovery struct {
	tiers       []DiscoveryTier
	bundledPath string
	agents      map[string]*AgentDefinition // id -> agent (shadowed by priority)
	logger      *slog.Logger
}

// DiscoveryOption is a functional option for configuring Discovery.
type DiscoveryOption func(*Discovery)

// WithTiers sets custom discovery tiers.
func WithTiers(tiers []DiscoveryTier) DiscoveryOption {
	return func(d *Discovery) {
		d.tiers = tiers
	}
}

// WithBundledPath sets the path to bundled agent definitions.
func WithBundledPath(path string) DiscoveryOption {
	return func(d *Discovery) {
		d.bundledPath = path
	}
}

// WithDiscoveryLogger sets the logger for discovery operations.
func WithDiscoveryLogger(logger *slog.Logger) DiscoveryOption {
	return func(d *Discovery) {
		d.logger = logger
	}
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(opts ...DiscoveryOption) *Discovery {
	d := &Discovery{
		tiers:  DefaultTiers(),
		agents: make(map[string]*AgentDefinition),
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Discover scans all tiers and returns discovered agent definitions.
// Higher-priority tiers (lower Priority value) shadow lower ones.
func (d *Discovery) Discover() ([]*AgentDefinition, error) {
	d.agents = make(map[string]*AgentDefinition)

	// Build all tiers including bundled (if set)
	allTiers := make([]DiscoveryTier, 0, len(d.tiers)+1)
	allTiers = append(allTiers, d.tiers...)
	if d.bundledPath != "" {
		allTiers = append(allTiers, DiscoveryTier{
			Path:     d.bundledPath,
			Priority: PriorityBundled,
		})
	}

	// Sort tiers by priority (lowest first) so we can build the map
	// with higher-priority agents overwriting lower-priority ones.
	sortedTiers := make([]DiscoveryTier, len(allTiers))
	copy(sortedTiers, allTiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		// Higher priority value first (bundled), so lower values overwrite later
		return sortedTiers[i].Priority > sortedTiers[j].Priority
	})

	for _, tier := range sortedTiers {
		if err := d.scanTier(tier); err != nil {
			d.logger.Warn("Failed to scan tier",
				"path", tier.Path,
				"error", err,
			)
			// Continue with other tiers
		}
	}

	// Convert map to slice
	result := make([]*AgentDefinition, 0, len(d.agents))
	for _, agent := range d.agents {
		result = append(result, agent)
	}

	// Sort by ID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	d.logger.Info("Agent discovery complete",
		"count", len(result),
		"tiers", len(allTiers),
	)

	return result, nil
}

// scanTier scans a single tier directory for agent definitions.
func (d *Discovery) scanTier(tier DiscoveryTier) error {
	path := pathutil.ExpandPath(tier.Path)

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		d.logger.Debug("Tier directory does not exist", "path", path)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())

		if entry.IsDir() {
			// Look for AGENT.md inside the directory
			agentFile := filepath.Join(entryPath, "AGENT.md")
			if _, err := os.Stat(agentFile); err == nil {
				d.loadAgentFile(agentFile, tier.Priority)
			}
		} else if isAgentFile(entry.Name()) {
			// Support flat .md files that could be agent definitions
			d.loadAgentFile(entryPath, tier.Priority)
		}
	}

	return nil
}

// loadAgentFile loads a single agent file and adds it to the index.
func (d *Discovery) loadAgentFile(path string, priority int) {
	agent, err := ParseAgentFile(path)
	if err != nil {
		d.logger.Warn("Failed to parse agent file",
			"path", path,
			"error", err,
		)
		return
	}

	agent.Priority = priority

	// Check if we should shadow an existing agent
	key := normalizeID(agent.ID)
	existing, exists := d.agents[key]
	if exists {
		if agent.Priority <= existing.Priority {
			d.logger.Debug("Agent shadowed by higher priority",
				"id", agent.ID,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			// Don't overwrite with lower priority
			return
		}
	}

	d.agents[key] = agent
	d.logger.Debug("Loaded agent definition",
		"id", agent.ID,
		"path", path,
		"priority", priority,
	)
}

// isAgentFile checks if a filename is a valid agent file.
func isAgentFile(name string) bool {
	lower := strings.ToLower(name)

	// Must be .md file
	if !strings.HasSuffix(lower, ".md") {
		return false
	}

	// Exclude common non-agent markdown files
	excluded := []string{"readme.md", "changelog.md", "license.md", "contributing.md", "rules.md"}
	return !slices.Contains(excluded, lower)
}

// normalizeID normalizes an agent ID for case-insensitive comparison.
func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// GetAgent returns an agent by ID from the last discovery.
func (d *Discovery) GetAgent(id string) *AgentDefinition {
	return d.agents[normalizeID(id)]
}

// ListAgents returns all discovered agent IDs.
func (d *Discovery) ListAgents() []string {
	ids := make([]string, 0, len(d.agents))
	for _, agent := range d.agents {
		ids = append(ids, agent.ID)
	}
	sort.Strings(ids)
	return ids
}

// Count returns the number of discovered agents.
func (d *Discovery) Count() int {
	return len(d.agents)
}
