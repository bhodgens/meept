// Package preferences provides storage and discovery for user instructions.
// Instructions are persisted as YAML frontmatter markdown files and discovered
// from a tiered directory hierarchy (project > user > system).
package preferences

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultTiers returns the standard filesystem discovery paths for user
// instructions. Discovery priority (highest to lowest): project-local,
// user-global, system-wide.
var DefaultTiers = []string{
	".meept/instructions",
	"~/.meept/instructions",
	"~/.config/meept/instructions",
}

// UserInstruction represents a single automation rule.
type UserInstruction struct {
	ID        string            `yaml:"id"`
	Name      string            `yaml:"name"`
	Trigger   string            `yaml:"trigger"`
	Action    string            `yaml:"action"`
	ActionArgs map[string]any   `yaml:"action_args,omitempty"`
	Enabled   bool              `yaml:"enabled"`
	Scope     string            `yaml:"scope"`
	Priority  string            `yaml:"priority"`
	CreatedAt time.Time         `yaml:"created_at"`
	UpdatedAt time.Time         `yaml:"updated_at,omitempty"`
	SourceTier int          `yaml:"-"`
	SourcePath string          `yaml:"-"`
	Body      string            `yaml:"-"`
}

// Store handles persistence and discovery of user instructions across
// multiple tiered directories.
type Store struct {
	tiers        []string
	instructions map[string]*UserInstruction
	mu           sync.RWMutex
	logger       logger
}

type logger interface {
	Debug(string, ...any)
	Warn(string, ...any)
	Info(string, ...any)
}

// storeLogger bridges the standard library logger interface expected by Store.
//nolint:unused -- reserved for future dynamic logger updates
func (s *Store) setLogger(l logger) {
	s.logger = l
}

// nopLogger is a no-op logger used when none is provided.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Info(string, ...any)  {}

// NewUserInstructionStore creates a new store with the given discovery tiers.
func NewUserInstructionStore(tiers []string) *Store {
	s := &Store{
		tiers:        resolveTiers(tiers),
		instructions: make(map[string]*UserInstruction),
		logger:       nopLogger{},
	}
	return s
}

// resolveTiers expands ~ prefixes in tier paths.
func resolveTiers(tiers []string) []string {
	result := make([]string, 0, len(tiers))
	for _, t := range tiers {
		result = append(result, expandTilde(t))
	}
	return result
}

// expandTilde replaces a leading ~ with the user home directory.
func expandTilde(path string) string {
	if len(path) >= 2 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
		return "~" + path[1:]
	}
	return path
}

// Discovery scans all tiers and returns active instructions.
// Higher-priority tiers shadow lower ones by instruction name (case-insensitive).
func (s *Store) Discovery() ([]*UserInstruction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instructions := make(map[string]*UserInstruction)
	seenPaths := make(map[string]bool)

	for i, tier := range s.tiers {
		docs, err := scanTier(tier, i)
		if err != nil {
			s.logger.Warn("instruction tier scan failed", "tier", tier, "error", err)
			continue
		}
		for _, instr := range docs {
			key := strings.ToLower(instr.Name)
			if existing, ok := instructions[key]; ok {
				// Shadowing: skip if current tier has lower priority (higher index)
				if i >= existing.SourceTier {
					continue
				}
			}
			instructions[key] = instr
			seenPaths[instr.SourcePath] = true
		}
	}

	result := make([]*UserInstruction, 0, len(instructions))
	for _, instr := range instructions {
		if instr.Enabled {
			result = append(result, instr)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	s.instructions = instructions
	return result, nil
}

// scanTier reads all .yaml/.yml/.md files from a single tier directory.
func scanTier(tier string, tierPriority int) ([]*UserInstruction, error) {
	info, err := os.Stat(tier)
	if err != nil {
		return nil, nil //nolint:nilerr // Missing tier is not an error
	}
	if !info.IsDir() {
		return nil, nil
	}

	var docs []*UserInstruction
	err = filepath.Walk(tier, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip unreadable entries
		}
		if info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".git" || ext == ".meept" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isInstructionFile(info.Name()) {
			return nil
		}
		doc, err := parseInstructionFile(path, tierPriority)
		if err != nil {
			return nil //nolint:nilerr // Skip individual parse errors
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

// isInstructionFile checks whether the filename looks like an instruction document.
func isInstructionFile(name string) bool {
	ext := strings.ToLower(name)
	return strings.HasSuffix(ext, ".yaml") ||
		strings.HasSuffix(ext, ".yml") ||
		strings.HasSuffix(ext, ".md")
}

// parseInstructionFile reads a single instruction file with YAML frontmatter.
func parseInstructionFile(path string, tierPriority int) (*UserInstruction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var frontmatter map[string]any
	body := string(data)

	// Try to extract YAML frontmatter (delimited by ---)
	content := extractFrontmatter(data, &frontmatter)
	if len(frontmatter) > 0 {
		body = strings.TrimSpace(content)
	}

	name := frontmatter["name"]
	if name == nil {
		// Use filename without extension as fallback
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	instr := &UserInstruction{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:         sanitizeName(fmt.Sprintf("%v", name)),
		Trigger:      getString(frontmatter, "trigger", ""),
		Action:       getString(frontmatter, "action", ""),
		ActionArgs:   getArgs(frontmatter, "action_args"),
		Enabled:      getBool(frontmatter, "enabled", true),
		Scope:        getString(frontmatter, "scope", "project"),
		Priority:     getString(frontmatter, "priority", "normal"),
		CreatedAt:    getCreateTime(frontmatter),
		SourceTier:   tierPriority,
		SourcePath:   path,
		Body:         body,
	}

	if frontmatter["id"] != nil {
		instr.ID = fmt.Sprintf("%v", frontmatter["id"])
	}
	if frontmatter["created_at"] != nil {
		instr.CreatedAt = getCreateTime(frontmatter)
	}

	return instr, nil
}

// extractFrontmatter splits data into frontmatter and body sections.
func extractFrontmatter(data []byte, frontmatter *map[string]any) string {
	content := string(data)
	firstLine := strings.Index(content, "\n")
	if firstLine < 0 {
		firstLine = len(content)
	}
	if strings.TrimSpace(content[:firstLine]) == "---" {
		// Find closing ---
		rest := content[firstLine+1:]
		closeIdx := strings.Index(rest, "---")
		if closeIdx > 0 {
			yamlBlock := strings.TrimSpace(rest[:closeIdx])
			_ = yaml.Unmarshal([]byte(yamlBlock), frontmatter)
			return strings.TrimSpace(rest[closeIdx+3:])
		}
	}
	return content
}

// Save persists an instruction to the specified tier.
func (s *Store) Save(instr *UserInstruction, tier string) error {
	if instr == nil {
		return fmt.Errorf("instruction is nil")
	}
	if instr.ID == "" {
		instr.ID = fmt.Sprintf("instr_%s", generateSaveID())
	}
	if instr.Name == "" {
		instr.Name = sanitizeName(instr.Trigger)
	}
	if instr.CreatedAt.IsZero() {
		instr.CreatedAt = time.Now().UTC()
	}
	instr.UpdatedAt = time.Now().UTC()

	// Resolve the tier to an absolute path
	dir := expandTilde(tier)
	if dir == tier {
		dir = tier
	}
	if !strings.HasSuffix(dir, "/") {
		dir = dir + "/"
	}

	// Ensure tier directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tier directory %s: %w", dir, err)
	}

	// Build filename: if name is set, use it; otherwise use ID
	filename := instr.Name
	if filename == "" {
		filename = instr.ID
	}
	// Strip invalid filename characters
	filename = sanitizeFilename(filename)
	filepath := dir + filename + ".yaml"

	// Marshal the instruction to YAML
	data, err := yaml.Marshal(instr)
	if err != nil {
		return fmt.Errorf("failed to marshal instruction: %w", err)
	}

	// Write atomically (write to temp file then rename)
	tmpFile := filepath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write instruction file: %w", err)
	}
	if err := os.Rename(tmpFile, filepath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename instruction file: %w", err)
	}

	instr.SourcePath = filepath
	instr.SourceTier = findTierIndex(s.tiers, dir)

	s.mu.Lock()
	s.instructions[strings.ToLower(instr.Name)] = instr
	s.mu.Unlock()

	return nil
}

// Delete removes an instruction by ID from the highest-priority tier that contains it.
func (s *Store) Delete(id string) error {
	s.mu.RLock()
	var target *UserInstruction
	for _, instr := range s.instructions {
		if instr.ID == id {
			target = instr
			break
		}
	}
	s.mu.RUnlock()

	if target == nil {
		return fmt.Errorf("instruction not found: %s", id)
	}

	if target.SourcePath == "" {
		return fmt.Errorf("instruction has no source path: %s", id)
	}

	if err := os.Remove(target.SourcePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove instruction file %s: %w", target.SourcePath, err)
	}

	s.mu.Lock()
	delete(s.instructions, strings.ToLower(target.Name))
	s.mu.Unlock()

	return nil
}

// Get returns a single instruction by ID, or nil if not found.
func (s *Store) Get(id string) *UserInstruction {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, instr := range s.instructions {
		if instr.ID == id {
			return instr
		}
	}
	return nil
}

// GetActive returns all enabled instructions from the last discovery.
func (s *Store) GetActive() []*UserInstruction {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*UserInstruction, 0)
	for _, instr := range s.instructions {
		if instr.Enabled {
			result = append(result, instr)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ExistsByName reports whether an instruction with the given name exists.
func (s *Store) ExistsByName(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.instructions[strings.ToLower(name)]
	return ok
}

// Count returns the total number of discovered instructions.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.instructions)
}

// findTierIndex returns the priority index of tier matching the given path prefix.
func findTierIndex(tiers []string, target string) int {
	for i, tier := range tiers {
		tierPath := expandTilde(tier)
		if strings.HasPrefix(target, tierPath) {
			return i
		}
	}
	return 0
}

// helper functions for frontmatter field extraction.

func getString(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return defaultVal
}

func getBool(m map[string]any, key string, defaultVal bool) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getArgs(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if args, ok := v.(map[string]any); ok {
			return args
		}
	}
	return nil
}

func getCreateTime(m map[string]any) time.Time {
	v := m["created_at"]
	if v == nil {
		return time.Time{}
	}
	ts := fmt.Sprintf("%v", v)
	if ts == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	return time.Time{}
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ToLower(name)
	return name
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "_")
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isValidFilenameByte(c) {
			name = name[:i] + "_" + name[i+1:]
		}
	}
	return name
}

func isValidFilenameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
}

func generateSaveID() string {
	hexBytes := make([]byte, 8)
	_, _ = randBytes(hexBytes)
	return fmt.Sprintf("%x", hexBytes)
}

// randBytes generates random bytes for ID generation.
func randBytes(b []byte) (int, error) {
	// Simple non-crypto random for save IDs; deterministic for tests.
	r := uint64(time.Now().UnixNano())
	for i := range b {
		r *= 6364136223846793005
		r += 1
		b[i] = byte(r)
	}
	return len(b), nil
}

// DefaultTier returns the default tier for saving new instructions.
func (s *Store) DefaultTier() string {
	if len(s.tiers) > 0 {
		return s.tiers[0] // Highest priority tier (project-local)
	}
	return ""
}
