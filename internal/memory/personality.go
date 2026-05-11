package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultPersonality = `# Meept Personality Profile

## Communication Style
- Clear, concise, and helpful
- Adapts formality to match the conversation partner
- Prefers actionable answers over vague suggestions

## Areas of Expertise Observed
- General-purpose assistance

## Creator Preferences
- No specific preferences recorded yet

## Recurring Themes
- None observed yet

## Interaction Notes
- Profile initialised; will evolve with further interactions.
`

// PersonalityMemory manages the bot's evolving self-model as a Markdown file.
// It tracks communication style, expertise areas, user preferences, and
// interaction patterns.
type PersonalityMemory struct {
	dataDir          string
	filePath         string
	description      string
	interactionCount int
	lastUpdated      *time.Time
	mu               sync.RWMutex
	logger           *slog.Logger
}

// PersonalityMemoryConfig holds configuration for personality memory.
type PersonalityMemoryConfig struct {
	// DataDir is the directory for the personality file.
	DataDir string
	// Logger for operations.
	Logger *slog.Logger
}

// NewPersonalityMemory creates a new personality memory instance.
func NewPersonalityMemory(cfg PersonalityMemoryConfig) *PersonalityMemory {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &PersonalityMemory{
		dataDir:     cfg.DataDir,
		filePath:    filepath.Join(cfg.DataDir, "personality.md"),
		description: defaultPersonality,
		logger:      cfg.Logger,
	}
}

// Load loads the personality profile from disk, or creates the default.
func (p *PersonalityMemory) Load(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure directory exists
	if err := os.MkdirAll(p.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Try to load existing file
	data, err := os.ReadFile(p.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default profile
			p.description = defaultPersonality
			if err := p.saveUnlocked(); err != nil {
				return err
			}
			p.logger.Info("Created default personality profile", "path", p.filePath)
			return nil
		}
		return fmt.Errorf("failed to read personality file: %w", err)
	}

	p.description = string(data)
	p.logger.Info("Loaded personality profile", "path", p.filePath)
	return nil
}

// Update integrates new interaction data into the personality profile.
// The summary should describe recent interaction patterns or preferences.
func (p *PersonalityMemory) Update(ctx context.Context, interactionSummary string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.interactionCount++
	nowISO := time.Now().UTC().Format(time.RFC3339)

	// Append the summary under Interaction Notes
	noteLine := fmt.Sprintf("- [%s] %s", nowISO, interactionSummary)
	p.description = appendToSection(p.description, "## Interaction Notes", noteLine)

	now := time.Now()
	p.lastUpdated = &now

	if err := p.saveUnlocked(); err != nil {
		return err
	}

	p.logger.Info("Personality profile updated",
		"interaction_count", p.interactionCount,
	)
	return nil
}

// UpdateKey updates or adds a specific key-value pair in the personality.
// This is useful for tracking specific preferences.
func (p *PersonalityMemory) UpdateKey(ctx context.Context, section string, key string, value string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find or create the section and update/add the key
	entry := fmt.Sprintf("- %s: %s", key, value)
	p.description = updateInSection(p.description, "## "+section, key, entry)

	now := time.Now()
	p.lastUpdated = &now

	return p.saveUnlocked()
}

// GetDescription returns the current personality description as Markdown.
func (p *PersonalityMemory) GetDescription() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.description
}

// GetPersonality returns the personality as a parsed map of sections.
func (p *PersonalityMemory) GetPersonality() map[string][]string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return parsePersonality(p.description)
}

// InteractionCount returns the number of updates since load.
func (p *PersonalityMemory) InteractionCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.interactionCount
}

// LastUpdated returns the timestamp of the most recent update.
func (p *PersonalityMemory) LastUpdated() *time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastUpdated
}

// Save persists the current personality profile to disk.
func (p *PersonalityMemory) Save() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveUnlocked()
}

func (p *PersonalityMemory) saveUnlocked() error {
	if err := os.MkdirAll(p.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := os.WriteFile(p.filePath, []byte(p.description), 0644); err != nil {
		return fmt.Errorf("failed to write personality file: %w", err)
	}
	p.logger.Debug("Saved personality profile", "path", p.filePath)
	return nil
}

// Close saves the personality profile.
func (p *PersonalityMemory) Close() error {
	return p.Save()
}

// appendToSection appends a line after the last bullet under a heading.
func appendToSection(document, heading, line string) string {
	lines := strings.Split(document, "\n")
	var insertIndex int
	inSection := false

	for i, rawLine := range lines {
		stripped := strings.TrimSpace(rawLine)
		if stripped == heading {
			inSection = true
			insertIndex = i + 1
			continue
		}
		if inSection {
			// Another heading means we left the target section
			if strings.HasPrefix(stripped, "## ") {
				break
			}
			// Track last non-empty line within the section
			if stripped != "" {
				insertIndex = i + 1
			}
		}
	}

	if insertIndex > 0 {
		// Insert the line
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertIndex]...)
		newLines = append(newLines, line)
		newLines = append(newLines, lines[insertIndex:]...)
		return strings.Join(newLines, "\n")
	}

	// Heading not found - append at end
	return document + "\n\n" + heading + "\n" + line
}

// updateInSection updates or adds an entry in a section.
func updateInSection(document, heading, key, newEntry string) string {
	lines := strings.Split(document, "\n")
	var _, sectionEnd int
	inSection := false
	keyFound := false

	for i, rawLine := range lines {
		stripped := strings.TrimSpace(rawLine)
		if stripped == heading {
			inSection = true
			_ = i + 1 // sectionStart
			continue
		}
		if inSection {
			if strings.HasPrefix(stripped, "## ") {
				sectionEnd = i
				break
			}
			// Check if this line contains the key
			if strings.Contains(stripped, key+":") || strings.HasPrefix(stripped, "- "+key) {
				lines[i] = newEntry
				keyFound = true
			}
			sectionEnd = i + 1
		}
	}

	if keyFound {
		return strings.Join(lines, "\n")
	}

	// Key not found - add it to the section
	if sectionEnd > 0 {
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:sectionEnd]...)
		newLines = append(newLines, newEntry)
		newLines = append(newLines, lines[sectionEnd:]...)
		return strings.Join(newLines, "\n")
	}

	// Section not found - create it
	return document + "\n\n" + heading + "\n" + newEntry
}

// parsePersonality parses a personality document into sections.
func parsePersonality(document string) map[string][]string {
	result := make(map[string][]string)
	lines := strings.Split(document, "\n")
	currentSection := ""

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(stripped, "## "); ok {
			currentSection = after
			result[currentSection] = []string{}
			continue
		}
		if currentSection != "" && strings.HasPrefix(stripped, "- ") {
			entry := strings.TrimPrefix(stripped, "- ")
			result[currentSection] = append(result[currentSection], entry)
		}
	}

	return result
}

// PersonalityStore provides a key-value interface over personality data.
type PersonalityStore struct {
	memory *PersonalityMemory
}

// NewPersonalityStore wraps a PersonalityMemory with key-value access.
func NewPersonalityStore(memory *PersonalityMemory) *PersonalityStore {
	return &PersonalityStore{memory: memory}
}

// Get retrieves a preference value.
func (s *PersonalityStore) Get(ctx context.Context, key string) (string, error) {
	if s.memory == nil {
		return "", errors.New("personality memory not initialized")
	}

	personality := s.memory.GetPersonality()
	for _, entries := range personality {
		for _, entry := range entries {
			if after, ok := strings.CutPrefix(entry, key+":"); ok {
				return strings.TrimSpace(after), nil
			}
		}
	}

	return "", nil // Not found, not an error
}

// Set stores a preference value.
func (s *PersonalityStore) Set(ctx context.Context, key, value string) error {
	if s.memory == nil {
		return errors.New("personality memory not initialized")
	}

	return s.memory.UpdateKey(ctx, "Creator Preferences", key, value)
}

// GetAll returns all personality data as a map.
func (s *PersonalityStore) GetAll(ctx context.Context) (map[string][]string, error) {
	if s.memory == nil {
		return nil, errors.New("personality memory not initialized")
	}

	return s.memory.GetPersonality(), nil
}
