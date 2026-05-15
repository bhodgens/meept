package context

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Artifacts represents all Claude artifacts found in a directory
type Artifacts struct {
	WorkingDir    string
	CLAUDEMD      *CLAUDEDocument
	ClaudeDir     *ClaudeDirectory
	READMEContent string // raw content of README.md (nil/empty if absent)
	Available     bool
	LastScanned   time.Time
}

// CLAUDEDocument represents a parsed CLAUDE.md file
type CLAUDEDocument struct {
	Path           string
	RawContent     string
	WorkingDir     string

	// Parsed sections following Claude Code conventions
	BuildCommands  []BuildCommand
	Architecture   *ArchitectureSection
	Components     []ComponentMapping
	Agents         []AgentDefinition
	SecurityLayers []SecurityLayer
	Configuration  []ConfigReference
	Conventions    *CodeConventions
	ProjectStructure *ProjectTree

	// Metadata
	LastModified time.Time
}

// BuildCommand represents a command from CLAUDE.md
type BuildCommand struct {
	Description string
	Command     string
	Category    string // build, test, run, deploy, etc.
	Context     []string // When this command is relevant
	Requires    []string // Tools or setup needed
}

// ArchitectureSection represents the architecture section from CLAUDE.md
type ArchitectureSection struct {
	RequestFlow    []string
	KeyComponents  []ComponentMapping
	SecurityLayers []SecurityLayer
	DataFlow       []DataFlowStep
}

// ComponentMapping maps layers to packages
type ComponentMapping struct {
	Layer    string
	Packages []string
}

// SecurityLayer represents a security layer from CLAUDE.md
type SecurityLayer struct {
	Name        string
	Description string
	Components  []string
}

// DataFlowStep represents a step in the data flow
type DataFlowStep struct {
	From   string
	To     string
	Action string
}

// AgentDefinition represents an agent from CLAUDE.md
type AgentDefinition struct {
	ID           string
	Name         string
	Role         string
	Purpose      string
	Capabilities []string
	Model        string
	Color        string
}

// ConfigReference represents a configuration file reference
type ConfigReference struct {
	Path        string
	Description string
	Format      string
}

// CodeConventions represents coding conventions from CLAUDE.md
type CodeConventions struct {
	Language     string
	Style        string
	Patterns     []string
	UIDirectives []string
}

// ProjectTree represents the project structure
type ProjectTree struct {
	Root     string
	Directories []string
	Files     []string
}

// ClaudeDirectory represents the .claude/ directory structure
type ClaudeDirectory struct {
	Path         string
	Skills       []*Skill
	Agents       []*AgentDefinition
	MindFile     string
	SessionFiles []string
	LastScanned  time.Time
}

// Skill represents a Claude skill from .claude/skills/
type Skill struct {
	Slug        string
	Path        string
	Name        string
	Description string
	Version     string
	Requires    []string // Capabilities required
	Content     string
	Category    string
	Triggers    []string // When to trigger this skill
}

// NewArtifacts creates a new Artifacts instance
func NewArtifacts(workingDir string) *Artifacts {
	return &Artifacts{
		WorkingDir: workingDir,
		Available:  false,
	}
}

// HasCLAUDEMD returns true if CLAUDE.md is present
func (a *Artifacts) HasCLAUDEMD() bool {
	return a.CLAUDEMD != nil
}

// HasClaudeDir returns true if .claude/ directory is present
func (a *Artifacts) HasClaudeDir() bool {
	return a.ClaudeDir != nil
}

// HasSkills returns true if skills are available
func (a *Artifacts) HasSkills() bool {
	return a.ClaudeDir != nil && len(a.ClaudeDir.Skills) > 0
}

// HasREADME returns true if README.md content is available
func (a *Artifacts) HasREADME() bool {
	return a.READMEContent != ""
}

// GetCommandsForCategory returns commands for a specific category
func (a *Artifacts) GetCommandsForCategory(category string) []BuildCommand {
	if !a.HasCLAUDEMD() {
		return nil
	}

	var commands []BuildCommand
	for _, cmd := range a.CLAUDEMD.BuildCommands {
		if cmd.Category == category {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// GetAgentForTask finds an agent suitable for a task
func (a *Artifacts) GetAgentForTask(task string) *AgentDefinition {
	if !a.HasCLAUDEMD() {
		return nil
	}

	// Simple keyword matching - can be enhanced
	for _, agent := range a.CLAUDEMD.Agents {
		for _, capability := range agent.Capabilities {
			if contains(task, capability) {
				return &agent
			}
		}
	}
	return nil
}

// contains is a helper for substring matching
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || substr == "" ||
		findSubstring(s, substr))
}

// findSubstring is a simple substring finder
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ClaudeSession represents a Claude session file
type ClaudeSession struct {
	SessionID string
	Source    string
	StartTime int64
}

// ArtifactCache manages cached artifacts
type ArtifactCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry // workingDir -> entry
	ttl     time.Duration
}

// CacheEntry represents a cached artifact entry
type CacheEntry struct {
	artifacts  *Artifacts
	lastAccess time.Time
	size       int64
}

// NewArtifactCache creates a new artifact cache
func NewArtifactCache(ttl time.Duration) *ArtifactCache {
	return &ArtifactCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves artifacts from cache
func (ac *ArtifactCache) Get(dir string) (*Artifacts, bool) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	entry, exists := ac.entries[dir]
	if !exists {
		return nil, false
	}

	// Check TTL
	if time.Since(entry.lastAccess) > ac.ttl {
		return nil, false
	}

	entry.lastAccess = time.Now()
	return entry.artifacts, true
}

// Put stores artifacts in cache
func (ac *ArtifactCache) Put(dir string, artifacts *Artifacts) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	size := int64(0)
	if artifacts.CLAUDEMD != nil {
		size = int64(len(artifacts.CLAUDEMD.RawContent))
	}
	size += int64(len(artifacts.READMEContent))

	ac.entries[dir] = &CacheEntry{
		artifacts:  artifacts,
		lastAccess: time.Now(),
		size:       size,
	}
}

// Invalidate removes a directory from cache
func (ac *ArtifactCache) Invalidate(dir string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	delete(ac.entries, dir)
}

// Clear removes all entries from cache
func (ac *ArtifactCache) Clear() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.entries = make(map[string]*CacheEntry)
}

// SetLastAccessForTest sets the lastAccess time for a cache entry (testing only)
func (ac *ArtifactCache) SetLastAccessForTest(dir string, t time.Time) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if entry, exists := ac.entries[dir]; exists {
		entry.lastAccess = t
	}
}

// NormalizePath normalizes a file path
func NormalizePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists checks if a directory exists
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
