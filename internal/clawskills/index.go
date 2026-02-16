// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const indexVersion = "1.0"
const indexFileName = "index.json"

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

// Get returns an installed skill by slug.
func (i *Index) Get(slug string) *InstalledSkill {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.Skills[slug]
}

// Set adds or updates an installed skill.
func (i *Index) Set(slug string, skill *InstalledSkill) {
	i.mu.Lock()
	defer i.mu.Unlock()
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

// List returns all installed skills.
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
