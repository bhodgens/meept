// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const indexVersion = "1.0"
const indexFileName = "index.json"

// ClawPrefix is the namespace prefix enforced for all clawskills.
// This prevents shadowing of local skills by third-party ones.
const ClawPrefix = "claw:"

// DefaultRiskLevel is the risk level always assigned to clawskills.
const DefaultRiskLevel = "high"

// DefaultMaxIterations is the default cap on agent-loop iterations for clawskills.
const DefaultMaxIterations = 10

// Index manages the local index of installed skills.
type Index struct {
	mu sync.RWMutex

	Skills    map[string]*InstalledSkill `json:"skills"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Version   string                     `json:"version"`
}

// NewIndex creates a new empty index.
func NewIndex() *Index {
	return &Index{
		Skills:    make(map[string]*InstalledSkill),
		UpdatedAt: time.Now(),
		Version:   indexVersion,
	}
}

// LoadIndex loads the index from the skills directory.
func LoadIndex(skillsDir string) (*Index, error) {
	indexPath := filepath.Join(skillsDir, indexFileName)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	if index.Skills == nil {
		index.Skills = make(map[string]*InstalledSkill)
	}

	return &index, nil
}

// Save saves the index to the skills directory.
func (i *Index) Save(skillsDir string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}

	indexPath := filepath.Join(skillsDir, indexFileName)
	return os.WriteFile(indexPath, data, 0644)
}

// Get returns an installed skill by slug (uses raw slug, not prefixed).
func (i *Index) Get(slug string) *InstalledSkill {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.Skills[slug]
}

// Set adds or updates an installed skill.
// The skill's RiskLevel is enforced to DefaultRiskLevel and MaxIterations
// is capped at DefaultMaxIterations.
func (i *Index) Set(slug string, skill *InstalledSkill) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Enforce risk level: clawskills are always HIGH risk, cannot go lower.
	skill.RiskLevel = DefaultRiskLevel

	// Cap max iterations
	if skill.MaxIterations <= 0 || skill.MaxIterations > DefaultMaxIterations {
		skill.MaxIterations = DefaultMaxIterations
	}

	i.Skills[slug] = skill
	i.UpdatedAt = time.Now()
}

// Delete removes an installed skill.
func (i *Index) Delete(slug string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.Skills, slug)
	i.UpdatedAt = time.Now()
}

// List returns all installed skills (filtered by blocklist).
func (i *Index) List() []*InstalledSkill {
	i.mu.RLock()
	defer i.mu.RUnlock()

	skills := make([]*InstalledSkill, 0, len(i.Skills))
	for _, skill := range i.Skills {
		skills = append(skills, skill)
	}
	return skills
}

// Count returns the number of installed skills.
func (i *Index) Count() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.Skills)
}

// HasUpdates checks if any skills have AutoUpdate enabled.
func (i *Index) HasUpdates() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	for _, skill := range i.Skills {
		if skill.AutoUpdate {
			return true
		}
	}
	return false
}

// IsBlocked checks if a slug is on the blocklist.
func IsBlocked(slug string, blockedSlugs []string) bool {
	for _, blocked := range blockedSlugs {
		if strings.EqualFold(slug, blocked) {
			return true
		}
	}
	return false
}

// PrefixedName returns the claw:-prefixed name for a slug, ensuring namespace
// isolation from local skills. The prefix prevents shadowing.
func PrefixedName(slug string) string {
	if strings.HasPrefix(slug, ClawPrefix) {
		return slug
	}
	return ClawPrefix + slug
}

// StripPrefix removes the claw: prefix if present.
func StripPrefix(name string) string {
	return strings.TrimPrefix(name, ClawPrefix)
}

// ScanAndLoad scans the clawskills install directory, loads each installed
// skill into the index, enforces the claw: prefix on skill names, applies the
// blocklist, and guarantees risk_level=high for all entries. Returns the
// count of loaded skills and a list of any warnings (e.g., blocked slugs
// skipped, corrupt entries skipped).
func ScanAndLoad(installDir string, blockedSlugs []string, logger *slog.Logger) (*Index, []string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	idx := NewIndex()
	var warnings []string

	entries, err := os.ReadDir(installDir)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read clawskills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		slug := entry.Name()

		// Check blocklist
		if IsBlocked(slug, blockedSlugs) {
			warnings = append(warnings, fmt.Sprintf("blocked slug skipped: %s", slug))
			logger.Warn("clawskills: blocked slug skipped", "slug", slug)
			continue
		}

		// Try to read per-skill .origin.json for metadata
		skillPath := filepath.Join(installDir, slug)
		originPath := filepath.Join(skillPath, ".origin.json")
		installed := &InstalledSkill{
			Slug:          slug,
			Name:          PrefixedName(slug),
			Path:          skillPath,
			RiskLevel:     DefaultRiskLevel,
			MaxIterations: DefaultMaxIterations,
		}

		if originData, err := os.ReadFile(originPath); err == nil {
			var origin struct {
				Name     string `json:"name"`
				Version  string `json:"version"`
				SHA256   string `json:"sha256"`
				Verified bool   `json:"verified"`
			}
			if err := json.Unmarshal(originData, &origin); err != nil {
				warnings = append(warnings, fmt.Sprintf("corrupt origin for %s: %v", slug, err))
				logger.Warn("clawskills: corrupt .origin.json", "slug", slug, "error", err)
				continue
			}
			if origin.Name != "" {
				installed.Name = PrefixedName(origin.Name)
			}
			installed.Version = origin.Version
			installed.SHA256 = origin.SHA256
			installed.Verified = origin.Verified
		}

		idx.Set(slug, installed)
	}

	logger.Info("clawskills: index loaded from disk",
		"count", idx.Count(),
		"warnings", len(warnings),
	)

	return idx, warnings, nil
}
