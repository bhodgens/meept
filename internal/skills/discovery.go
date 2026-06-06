package skills

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillSource provides skills from a specific source.
type SkillSource interface {
	// Name returns a human-readable name for this source.
	Name() string
	// Discover scans the source and returns discovered skills.
	Discover(ctx context.Context) ([]*Skill, error)
}

// DiscoveryTier represents a directory tier for skill discovery.
type DiscoveryTier struct {
	Path     string
	Priority int
}

// DefaultTiers returns the standard 3-tier filesystem discovery paths.
// Claude skills (~/.claude/skills/) are handled by the dedicated ClaudeSource.
// Discovery priority (highest to lowest): project > user > system.
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

// Discovery orchestrates skill discovery across pluggable sources with priority shadowing.
type Discovery struct {
	sources []SkillSource
	skills  map[string]*Skill // name -> skill (shadowed by priority)
	logger  *slog.Logger
}

// DiscoveryOption is a functional option for configuring Discovery.
type DiscoveryOption func(*Discovery)

// WithTiers sets custom discovery tiers. This creates a FileSource internally
// and appends it to the source list.
func WithTiers(tiers []DiscoveryTier) DiscoveryOption {
	return func(d *Discovery) {
		d.sources = append(d.sources, NewFileSource(tiers, d.logger))
	}
}

// WithSources adds custom skill sources. These replace any default sources.
func WithSources(sources ...SkillSource) DiscoveryOption {
	return func(d *Discovery) {
		d.sources = append(d.sources, sources...)
	}
}

// WithDiscoveryLogger sets the logger for discovery operations.
func WithDiscoveryLogger(logger *slog.Logger) DiscoveryOption {
	return func(d *Discovery) {
		d.logger = logger
	}
}

// NewDiscovery creates a new Discovery instance with default sources
// (filesystem tiers + Claude source).
func NewDiscovery(opts ...DiscoveryOption) *Discovery {
	d := &Discovery{
		skills: make(map[string]*Skill),
		logger: slog.Default(),
	}

	// Apply logger option first so sources can use it.
	for _, opt := range opts {
		opt(d)
	}

	// If no sources were set by options, create the default set.
	if len(d.sources) == 0 {
		d.sources = []SkillSource{
			NewFileSource(DefaultTiers(), d.logger),
			NewClaudeSource(d.logger),
		}
	}

	return d
}

// Sources returns the list of configured skill sources.
func (d *Discovery) Sources() []SkillSource {
	return d.sources
}

// Discover scans all sources and returns discovered skills.
// Higher-priority skills (lower Priority value) shadow lower ones across all sources.
func (d *Discovery) Discover() ([]*Skill, error) {
	return d.DiscoverWithContext(context.Background())
}

// DiscoverWithContext scans all sources and returns discovered skills.
// It accepts a context for cancellation support.
func (d *Discovery) DiscoverWithContext(ctx context.Context) ([]*Skill, error) {
	d.skills = make(map[string]*Skill)

	for _, source := range d.sources {
		sourceSkills, err := source.Discover(ctx)
		if err != nil {
			d.logger.Warn("Source discovery failed",
				"source", source.Name(),
				"error", err,
			)
			// Continue with other sources
			continue
		}

		// Merge with priority shadowing
		for _, skill := range sourceSkills {
			key := normalizeName(skill.Name)
			existing, exists := d.skills[key]
			if exists {
				if skill.Priority <= existing.Priority {
					d.logger.Debug("Skill shadowed by higher priority",
						"name", skill.Name,
						"source", source.Name(),
						"new_path", skill.Path,
						"old_path", existing.Path,
					)
				} else {
					// Don't overwrite with lower priority
					continue
				}
			}
			d.skills[key] = skill
		}
	}

	// Convert map to sorted slice
	result := make([]*Skill, 0, len(d.skills))
	for _, skill := range d.skills {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	d.logger.Info("Skill discovery complete",
		"count", len(result),
		"sources", len(d.sources),
	)

	return result, nil
}

// DiscoverMetadataOnly scans all sources and returns skill index entries (metadata only).
// This is faster than Discover() as it skips parsing skill bodies.
// For FileSource, it uses the dedicated metadata-only path. For other sources,
// it falls back to Discover() and strips bodies.
func (d *Discovery) DiscoverMetadataOnly() ([]*SkillIndexEntry, error) {
	entries := make(map[string]*SkillIndexEntry)

	for _, source := range d.sources {
		var sourceEntries []*SkillIndexEntry

		// Use optimized metadata path for FileSource.
		if fs, ok := source.(*FileSource); ok {
			meta, err := fs.DiscoverMetadata(context.Background())
			if err != nil {
				d.logger.Warn("Source metadata discovery failed",
					"source", source.Name(),
					"error", err,
				)
				continue
			}
			sourceEntries = meta
		} else {
			// Fallback: discover full skills and strip bodies.
			skills, err := source.Discover(context.Background())
			if err != nil {
				d.logger.Warn("Source discovery failed for metadata",
					"source", source.Name(),
					"error", err,
				)
				continue
			}
			for _, skill := range skills {
				sourceEntries = append(sourceEntries, &SkillIndexEntry{
					Name:         skill.Name,
					Description:  skill.Description,
					Requires:     skill.Requires,
					Tags:         skill.Tags,
					Path:         skill.Path,
					Priority:     skill.Priority,
					RiskLevel:    skill.RiskLevel,
					AllowedTools: skill.AllowedTools,
					Examples:     skill.Examples,
				})
			}
		}

		// Merge with priority shadowing.
		for _, entry := range sourceEntries {
			key := normalizeName(entry.Name)
			existing, exists := entries[key]
			if exists {
				if entry.Priority <= existing.Priority {
					d.logger.Debug("Skill metadata shadowed by higher priority",
						"name", entry.Name,
						"source", source.Name(),
					)
				} else {
					continue
				}
			}
			entries[key] = entry
		}
	}

	result := make([]*SkillIndexEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	d.logger.Info("Skill metadata discovery complete",
		"count", len(result),
		"sources", len(d.sources),
	)

	return result, nil
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

// normalizeName normalizes a skill name for case-insensitive comparison.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
