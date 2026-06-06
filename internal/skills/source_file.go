package skills

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// FileSource discovers skills from filesystem directories organized in tiers.
type FileSource struct {
	tiers  []DiscoveryTier
	logger *slog.Logger
}

// NewFileSource creates a FileSource that scans the given directory tiers.
func NewFileSource(tiers []DiscoveryTier, logger *slog.Logger) *FileSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &FileSource{
		tiers:  tiers,
		logger: logger,
	}
}

// Name returns the human-readable name of this source.
func (s *FileSource) Name() string {
	return "filesystem"
}

// Discover scans all configured tiers and returns discovered skills.
// Higher-priority tiers (lower Priority value) shadow lower ones.
func (s *FileSource) Discover(ctx context.Context) ([]*Skill, error) {
	skills := make(map[string]*Skill)

	// Sort tiers by priority (lowest priority value first processed, so higher
	// priority overwrites lower). We sort descending by priority value so that
	// lower-valued (higher-priority) tiers overwrite later.
	sortedTiers := make([]DiscoveryTier, len(s.tiers))
	copy(sortedTiers, s.tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		return sortedTiers[i].Priority > sortedTiers[j].Priority
	})

	for _, tier := range sortedTiers {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := s.scanTier(ctx, tier, skills); err != nil {
			s.logger.Warn("Failed to scan tier",
				"path", tier.Path,
				"error", err,
			)
			// Continue with other tiers
		}
	}

	// Convert map to sorted slice
	result := make([]*Skill, 0, len(skills))
	for _, skill := range skills {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	s.logger.Info("FileSource discovery complete",
		"count", len(result),
		"tiers", len(s.tiers),
	)

	return result, nil
}

// DiscoverMetadata scans all tiers and returns skill metadata entries (no bodies).
func (s *FileSource) DiscoverMetadata(ctx context.Context) ([]*SkillIndexEntry, error) {
	entries := make(map[string]*SkillIndexEntry)

	sortedTiers := make([]DiscoveryTier, len(s.tiers))
	copy(sortedTiers, s.tiers)
	sort.Slice(sortedTiers, func(i, j int) bool {
		return sortedTiers[i].Priority > sortedTiers[j].Priority
	})

	for _, tier := range sortedTiers {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if err := s.scanTierMetadataOnly(ctx, tier, entries); err != nil {
			s.logger.Warn("Failed to scan tier for metadata",
				"path", tier.Path,
				"error", err,
			)
		}
	}

	result := make([]*SkillIndexEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// scanTier scans a single tier directory for skills.
func (s *FileSource) scanTier(ctx context.Context, tier DiscoveryTier, skills map[string]*Skill) error {
	path := pathutil.ExpandPath(tier.Path)

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		s.logger.Debug("Tier directory does not exist", "path", path)
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

	for _, entry := range dirEntries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		entryPath := filepath.Join(path, entry.Name())

		if entry.IsDir() {
			// Look for SKILL.md inside the directory
			skillFile := filepath.Join(entryPath, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				s.loadSkillFile(skillFile, tier.Priority, skills)
			}
		} else if isSkillFile(entry.Name()) {
			// Support flat .md files (not SKILL.md, which would be at root)
			s.loadSkillFile(entryPath, tier.Priority, skills)
		}
	}

	return nil
}

// loadSkillFile loads a single skill file and adds it to the skills map with priority shadowing.
func (s *FileSource) loadSkillFile(path string, priority int, skills map[string]*Skill) {
	skill, err := ParseSkillFile(path)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			s.logger.Warn("Skill file has no frontmatter, using slug as name",
				"path", path,
			)
		} else {
			s.logger.Warn("Failed to parse skill file",
				"path", path,
				"error", err,
			)
			return
		}
	}

	if skill.Name == "" {
		s.logger.Warn("Skill has no name, skipping",
			"path", path,
		)
		return
	}

	skill.Priority = priority
	if skill.Source == "" {
		skill.Source = "meept"
	}

	key := normalizeName(skill.Name)
	existing, exists := skills[key]
	if exists {
		if skill.Priority <= existing.Priority {
			s.logger.Debug("Skill shadowed by higher priority",
				"name", skill.Name,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			// Don't overwrite with lower priority
			return
		}
	}

	skills[key] = skill
	s.logger.Debug("Loaded skill",
		"name", skill.Name,
		"path", path,
		"priority", priority,
	)
}

// scanTierMetadataOnly scans a single tier directory for skill metadata.
func (s *FileSource) scanTierMetadataOnly(ctx context.Context, tier DiscoveryTier, entries map[string]*SkillIndexEntry) error {
	path := pathutil.ExpandPath(tier.Path)

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		s.logger.Debug("Tier directory does not exist", "path", path)
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		entryPath := filepath.Join(path, dirEntry.Name())

		if dirEntry.IsDir() {
			skillFile := filepath.Join(entryPath, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				s.loadSkillMetadata(skillFile, tier.Priority, entries)
			}
		} else if isSkillFile(dirEntry.Name()) {
			s.loadSkillMetadata(entryPath, tier.Priority, entries)
		}
	}

	return nil
}

// loadSkillMetadata loads skill metadata from a file and adds it to entries.
func (s *FileSource) loadSkillMetadata(path string, priority int, entries map[string]*SkillIndexEntry) {
	entry, err := ParseSkillMetadataOnly(path)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			s.logger.Warn("Skill file has no frontmatter, using slug as name",
				"path", path,
			)
		} else {
			s.logger.Warn("Failed to parse skill metadata",
				"path", path,
				"error", err,
			)
			return
		}
	}

	entry.Priority = priority

	key := normalizeName(entry.Name)
	existing, exists := entries[key]
	if exists {
		if entry.Priority <= existing.Priority {
			s.logger.Debug("Skill metadata shadowed by higher priority",
				"name", entry.Name,
				"new_path", path,
				"old_path", existing.Path,
			)
		} else {
			return
		}
	}

	entries[key] = entry
	s.logger.Debug("Loaded skill metadata",
		"name", entry.Name,
		"path", path,
		"priority", priority,
	)
}

// fileExcludedFiles is the list of common non-skill markdown files excluded from discovery.
// Exported so tests can verify the exclusion list if needed.
var fileExcludedFiles = []string{"readme.md", "changelog.md", "license.md", "contributing.md"}

// isSkillFile checks if a filename is a valid skill file.
func isSkillFile(name string) bool {
	lower := strings.ToLower(name)

	if !strings.HasSuffix(lower, ".md") {
		return false
	}

	return !slices.Contains(fileExcludedFiles, lower)
}
