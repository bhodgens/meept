package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// DiscoveryTier represents a directory tier for skill discovery.
type DiscoveryTier struct {
	Path     string
	Priority int
}

// DefaultTiers returns the standard 3-tier discovery paths.
func DefaultTiers() []DiscoveryTier {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	return []DiscoveryTier{
		{Path: ".meept/skills", Priority: PriorityProject},
		{Path: filepath.Join(homeDir, ".meept", "skills"), Priority: PriorityUser},
		{Path: filepath.Join(homeDir, ".config", "meept", "skills"), Priority: PrioritySystem},
	}
}

// Discovery scans filesystem tiers to discover SKILL.md files.
type Discovery struct {
	tiers  []DiscoveryTier
	skills map[string]*Skill // name -> skill (shadowed by priority)
	logger *slog.Logger
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
		tiers:  DefaultTiers(),
		skills: make(map[string]*Skill),
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// DiscoverMetadataOnly scans all tiers and returns skill index entries (metadata only).
// This is faster than Discover() as it skips parsing skill bodies.
func (d *Discovery) DiscoverMetadataOnly() ([]*SkillIndexEntry, error) {
	entries := make(map[string]*SkillIndexEntry)

	// Sort tiers by priority (lowest first) so we can build the map
	// with higher-priority skills overwriting lower-priority ones.
	sortedTiers := make([]DiscoveryTier, len(d.tiers))
	copy(sortedTiers, d.tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		return sortedTiers[i].Priority > sortedTiers[j].Priority
	})

	for _, tier := range sortedTiers {
		if err := d.scanTierMetadataOnly(tier, entries); err != nil {
			d.logger.Warn("Failed to scan tier for metadata",
				"path", tier.Path,
				"error", err,
			)
		}
	}

	// Convert map to slice
	result := make([]*SkillIndexEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}

	// Sort by name for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	d.logger.Info("Skill metadata discovery complete",
		"count", len(result),
		"tiers", len(d.tiers),
	)

	return result, nil
}

// scanTierMetadataOnly scans a single tier directory for skill metadata.
func (d *Discovery) scanTierMetadataOnly(tier DiscoveryTier, entries map[string]*SkillIndexEntry) error {
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

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, dirEntry := range dirEntries {
		entryPath := filepath.Join(path, dirEntry.Name())

		if dirEntry.IsDir() {
			// Look for SKILL.md inside the directory
			skillFile := filepath.Join(entryPath, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				d.loadSkillMetadata(skillFile, tier.Priority, entries)
			}
		} else if isSkillFile(dirEntry.Name()) {
			// Support flat .md files (not SKILL.md, which would be at root)
			d.loadSkillMetadata(entryPath, tier.Priority, entries)
		}
	}

	return nil
}

// loadSkillMetadata loads skill metadata from a file and adds it to entries.
func (d *Discovery) loadSkillMetadata(path string, priority int, entries map[string]*SkillIndexEntry) {
	entry, err := ParseSkillMetadataOnly(path)
	if err != nil {
		d.logger.Warn("Failed to parse skill metadata",
			"path", path,
			"error", err,
		)
		return
	}

	entry.Priority = priority

	// Check if we should shadow an existing entry
	key := normalizeName(entry.Name)
	existing, exists := entries[key]
	if exists {
		if entry.Priority <= existing.Priority {
			d.logger.Debug("Skill metadata shadowed by higher priority",
				"name", entry.Name,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			// Don't overwrite with lower priority
			return
		}
	}

	entries[key] = entry
	d.logger.Debug("Loaded skill metadata",
		"name", entry.Name,
		"path", path,
		"priority", priority,
	)
}

// Discover scans all tiers and returns discovered skills.
// Higher-priority tiers (lower Priority value) shadow lower ones.
func (d *Discovery) Discover() ([]*Skill, error) {
	d.skills = make(map[string]*Skill)

	// Sort tiers by priority (lowest first) so we can build the map
	// with higher-priority skills overwriting lower-priority ones.
	sortedTiers := make([]DiscoveryTier, len(d.tiers))
	copy(sortedTiers, d.tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		// Higher priority value first (system), so lower values overwrite later
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
	result := make([]*Skill, 0, len(d.skills))
	for _, skill := range d.skills {
		result = append(result, skill)
	}

	// Sort by name for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	d.logger.Info("Skill discovery complete",
		"count", len(result),
		"tiers", len(d.tiers),
	)

	return result, nil
}

// scanTier scans a single tier directory for skills.
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
			// Look for SKILL.md inside the directory
			skillFile := filepath.Join(entryPath, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				d.loadSkillFile(skillFile, tier.Priority)
			}
		} else if isSkillFile(entry.Name()) {
			// Support flat .md files (not SKILL.md, which would be at root)
			d.loadSkillFile(entryPath, tier.Priority)
		}
	}

	return nil
}

// loadSkillFile loads a single skill file and adds it to the index.
func (d *Discovery) loadSkillFile(path string, priority int) {
	skill, err := ParseSkillFile(path)
	if err != nil {
		d.logger.Warn("Failed to parse skill file",
			"path", path,
			"error", err,
		)
		return
	}

	skill.Priority = priority

	// Check if we should shadow an existing skill
	existing, exists := d.skills[normalizeName(skill.Name)]
	if exists {
		if skill.Priority <= existing.Priority {
			d.logger.Debug("Skill shadowed by higher priority",
				"name", skill.Name,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			// Don't overwrite with lower priority
			return
		}
	}

	d.skills[normalizeName(skill.Name)] = skill
	d.logger.Debug("Loaded skill",
		"name", skill.Name,
		"path", path,
		"priority", priority,
	)
}

// isSkillFile checks if a filename is a valid skill file.
func isSkillFile(name string) bool {
	lower := strings.ToLower(name)

	// Must be .md file
	if !strings.HasSuffix(lower, ".md") {
		return false
	}

	// Exclude common non-skill markdown files
	excluded := []string{"readme.md", "changelog.md", "license.md", "contributing.md"}
	return !slices.Contains(excluded, lower)
}

// normalizeName normalizes a skill name for case-insensitive comparison.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// GetSkill returns a skill by name from the last discovery.
func (d *Discovery) GetSkill(name string) *Skill {
	return d.skills[normalizeName(name)]
}

// ListSkills returns all discovered skill names.
func (d *Discovery) ListSkills() []string {
	names := make([]string, 0, len(d.skills))
	for _, skill := range d.skills {
		names = append(names, skill.Name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of discovered skills.
func (d *Discovery) Count() int {
	return len(d.skills)
}
