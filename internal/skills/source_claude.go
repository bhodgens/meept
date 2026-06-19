package skills

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/caimlas/meept/internal/pathutil"
)

// ClaudeSource discovers skills from ~/.claude/skills/ and applies ClaudeSkillAdapter
// normalization to each discovered skill.
type ClaudeSource struct {
	path   string
	logger *slog.Logger
}

// NewClaudeSource creates a ClaudeSource that scans ~/.claude/skills/.
func NewClaudeSource(logger *slog.Logger) *ClaudeSource {
	if logger == nil {
		logger = slog.Default()
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	return &ClaudeSource{
		path:   filepath.Join(homeDir, ".claude", "skills"),
		logger: logger,
	}
}

// NewClaudeSourceWithPath creates a ClaudeSource with a custom path (useful for testing).
func NewClaudeSourceWithPath(path string, logger *slog.Logger) *ClaudeSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &ClaudeSource{
		path:   path,
		logger: logger,
	}
}

// Name returns the human-readable name of this source.
func (s *ClaudeSource) Name() string {
	return "claude"
}

// Discover scans the Claude skills directory and returns discovered skills.
// Each skill is adapted via ClaudeSkillAdapter.AdaptSkill().
func (s *ClaudeSource) Discover(ctx context.Context) ([]*Skill, error) {
	path := pathutil.ExpandPath(s.path)

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		s.logger.Debug("Claude skills directory does not exist", "path", path)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	skills, err := s.scanDir(ctx, path)
	if err != nil {
		return nil, err
	}

	s.logger.Info("ClaudeSource discovery complete",
		"count", len(skills),
		"path", path,
	)

	return skills, nil
}

// scanDir scans the Claude skills directory for skill files.
func (s *ClaudeSource) scanDir(ctx context.Context, path string) ([]*Skill, error) {
	var skills []*Skill

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	adapter := &ClaudeSkillAdapter{}

	for _, entry := range dirEntries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		entryPath := filepath.Join(path, entry.Name())

		var skill *Skill
		if entry.IsDir() {
			// Look for SKILL.md inside the directory
			skillFile := filepath.Join(entryPath, "SKILL.md")
			if _, statErr := os.Stat(skillFile); statErr == nil {
				skill, err = s.loadAndAdapt(skillFile, adapter)
			}
		} else if isSkillFile(entry.Name()) {
			skill, err = s.loadAndAdapt(entryPath, adapter)
		}
		if err != nil {
			s.logger.Warn("Failed to parse Claude skill file",
				"path", entryPath,
				"error", err,
			)
			continue
		}

		if skill != nil {
			skills = append(skills, skill)
		}
	}

	// Sort by name
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	return skills, nil
}

// loadAndAdapt loads a skill file and applies ClaudeSkillAdapter.
func (s *ClaudeSource) loadAndAdapt(path string, adapter *ClaudeSkillAdapter) (*Skill, error) {
	skill, err := ParseSkillFile(path)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			s.logger.Warn("Claude skill file has no frontmatter, using slug as name",
				"path", path,
			)
			// Return the partial skill if we got one from ErrNoFrontmatter
			if skill != nil {
				skill.Priority = PriorityClaude
				return adapter.AdaptSkill(skill), nil
			}
		}
		return nil, err
	}

	if skill.Name == "" {
		s.logger.Warn("Claude skill has no name, skipping",
			"path", path,
		)
		return nil, nil
	}

	skill.Priority = PriorityClaude
	return adapter.AdaptSkill(skill), nil
}
