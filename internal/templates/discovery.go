package templates

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// DiscoveryTier represents a directory tier for template discovery.
type DiscoveryTier struct {
	Path     string
	Priority int
}

// DefaultTiers returns the standard 3-tier discovery paths for templates.
func DefaultTiers() []DiscoveryTier {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	return []DiscoveryTier{
		{Path: ".meept/templates", Priority: PriorityProject},
		{Path: filepath.Join(homeDir, ".meept", "templates"), Priority: PriorityUser},
		{Path: filepath.Join(homeDir, ".config", "meept", "templates"), Priority: PrioritySystem},
	}
}

// Discovery scans filesystem tiers to discover template .md files.
type Discovery struct {
	tiers     []DiscoveryTier
	templates map[string]*Template // name -> template (shadowed by priority)
	logger    *slog.Logger
}

// DiscoveryOption is a functional option for configuring Discovery.
type DiscoveryOption func(*Discovery)

// WithTiers sets custom discovery tiers.
func WithTiers(tiers []DiscoveryTier) DiscoveryOption {
	return func(d *Discovery) {
		d.tiers = tiers
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
		tiers:     DefaultTiers(),
		templates: make(map[string]*Template),
		logger:    slog.Default(),
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Discover scans all tiers and returns discovered templates.
// Higher-priority tiers (lower Priority value) shadow lower ones.
func (d *Discovery) Discover() ([]*Template, error) {
	d.templates = make(map[string]*Template)

	// Sort tiers by priority (lowest first) so we can build the map
	// with higher-priority templates overwriting lower-priority ones.
	sortedTiers := make([]DiscoveryTier, len(d.tiers))
	copy(sortedTiers, d.tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		return sortedTiers[i].Priority > sortedTiers[j].Priority
	})

	for _, tier := range sortedTiers {
		if err := d.scanTier(tier); err != nil {
			d.logger.Warn("Failed to scan tier",
				"path", tier.Path,
				"error", err,
			)
			// Continue with other tiers.
		}
	}

	// Convert map to slice.
	result := make([]*Template, 0, len(d.templates))
	for _, tmpl := range d.templates {
		result = append(result, tmpl)
	}

	// Sort by name for consistent ordering.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	d.logger.Info("Template discovery complete",
		"count", len(result),
		"tiers", len(d.tiers),
	)

	return result, nil
}

// scanTier scans a single tier directory for templates.
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
			// Look for TEMPLATE.md inside the directory.
			templateFile := filepath.Join(entryPath, "TEMPLATE.md")
			if _, err := os.Stat(templateFile); err == nil {
				d.loadTemplateFile(templateFile, tier.Priority)
			}
		} else if isTemplateFile(entry.Name()) {
			// Support flat .md files.
			d.loadTemplateFile(entryPath, tier.Priority)
		}
	}

	return nil
}

// loadTemplateFile loads a single template file and adds it to the index.
func (d *Discovery) loadTemplateFile(path string, priority int) {
	tmpl, err := ParseTemplateFile(path)
	if err != nil {
		d.logger.Warn("Failed to parse template file",
			"path", path,
			"error", err,
		)
		return
	}

	tmpl.Priority = priority

	// Check if we should shadow an existing template.
	existing, exists := d.templates[normalizeName(tmpl.Name)]
	if exists {
		if tmpl.Priority <= existing.Priority {
			d.logger.Debug("Template shadowed by higher priority",
				"name", tmpl.Name,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			// Don't overwrite with lower priority.
			return
		}
	}

	d.templates[normalizeName(tmpl.Name)] = tmpl
	d.logger.Debug("Loaded template",
		"name", tmpl.Name,
		"path", path,
		"priority", priority,
	)
}

// isTemplateFile checks if a filename is a valid template file.
func isTemplateFile(name string) bool {
	lower := strings.ToLower(name)

	// Must be .md file.
	if !strings.HasSuffix(lower, ".md") {
		return false
	}

	// Exclude common non-template markdown files.
	excluded := []string{"readme.md", "changelog.md", "license.md", "contributing.md"}
	return !slices.Contains(excluded, lower)
}

// normalizeName normalizes a template name for case-insensitive comparison.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GetTemplate returns a template by name from the last discovery.
func (d *Discovery) GetTemplate(name string) *Template {
	return d.templates[normalizeName(name)]
}

// ListTemplates returns all discovered template names.
func (d *Discovery) ListTemplates() []string {
	names := make([]string, 0, len(d.templates))
	for _, tmpl := range d.templates {
		names = append(names, tmpl.Name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of discovered templates.
func (d *Discovery) Count() int {
	return len(d.templates)
}
