package context

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ArtifactScanner scans for Claude artifacts in a directory
type ArtifactScanner struct {
	mu         sync.RWMutex
	workingDir string
	cache      *ArtifactCache
}

// NewArtifactScanner creates a new artifact scanner
func NewArtifactScanner(workingDir string, cache *ArtifactCache) *ArtifactScanner {
	return &ArtifactScanner{
		workingDir: workingDir,
		cache:      cache,
	}
}

// Scan scans the working directory for Claude artifacts
func (as *ArtifactScanner) Scan() (*Artifacts, error) {
	// Snapshot workingDir under read lock; release before doing any I/O
	// (filesystem traversal, cache reads/writes).
	as.mu.RLock()
	workingDir := as.workingDir
	cache := as.cache
	as.mu.RUnlock()

	// Normalize the working directory
	normalizedDir, err := NormalizePath(workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize working directory: %w", err)
	}

	// Check cache first
	if cache != nil {
		if cached, found := cache.Get(normalizedDir); found {
			return cached, nil
		}
	}

	// Create new artifacts instance
	artifacts := NewArtifacts(normalizedDir)

	// Scan for CLAUDE.md
	claudeMDPath := filepath.Join(normalizedDir, "CLAUDE.md")
	if FileExists(claudeMDPath) {
		doc, err := ParseCLAUDEMD(claudeMDPath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CLAUDE.md: %w", err)
		}
		artifacts.CLAUDEMD = doc
		artifacts.Available = true
	}

	// Scan for README.md (try common casing variants)
	readmeContent := scanReadme(normalizedDir)
	if readmeContent != "" {
		artifacts.READMEContent = readmeContent
		artifacts.Available = true
	}

	// Scan for .claude/ directory
	claudeDirPath := filepath.Join(normalizedDir, ".claude")
	if DirExists(claudeDirPath) {
		claudeDir, err := ScanClaudeDirectory(claudeDirPath)
		if err != nil {
			return nil, fmt.Errorf("failed to scan .claude directory: %w", err)
		}
		artifacts.ClaudeDir = claudeDir
		artifacts.Available = true
	}

	artifacts.LastScanned = time.Now()

	// Cache the results
	as.mu.RLock()
	cache = as.cache
	as.mu.RUnlock()
	if cache != nil {
		cache.Put(normalizedDir, artifacts)
	}

	return artifacts, nil
}

// scanReadme attempts to find and read a README file in the given directory.
// It checks README.md, readme.md, Readme.md, and README (no extension).
// Returns the file content or an empty string if no README is found.
func scanReadme(dir string) string {
	candidates := []string{
		"README.md",
		"readme.md",
		"Readme.md",
		"README",
		"readme",
	}

	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if FileExists(path) {
			data, err := os.ReadFile(path)
			if err != nil {
				slog.Default().Warn("failed to read README file", "path", path, "error", err)
				continue
			}
			return string(data)
		}
	}

	return ""
}

// ScanClaudeDirectory scans the .claude/ directory
func ScanClaudeDirectory(claudeDirPath string) (*ClaudeDirectory, error) {
	claudeDir := &ClaudeDirectory{
		Path:        claudeDirPath,
		LastScanned: time.Now(),
	}

	// Scan for skills
	skillsDir := filepath.Join(claudeDirPath, "skills")
	if DirExists(skillsDir) {
		skills, err := ScanSkillsDirectory(skillsDir)
		if err != nil {
			return nil, fmt.Errorf("failed to scan skills directory: %w", err)
		}
		claudeDir.Skills = skills
	}

	// Scan for agents
	agentsDir := filepath.Join(claudeDirPath, "agents")
	if DirExists(agentsDir) {
		agents, err := ScanAgentsDirectory(agentsDir)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agents directory: %w", err)
		}
		claudeDir.Agents = agents
	}

	// Check for mind file
	mindFile := filepath.Join(claudeDirPath, ".mind.mv2")
	if FileExists(mindFile) {
		claudeDir.MindFile = mindFile
	}

	// Scan for session files
	sessionFiles, err := filepath.Glob(filepath.Join(claudeDirPath, "mind-session-*.json"))
	if err == nil {
		claudeDir.SessionFiles = sessionFiles
	}

	return claudeDir, nil
}

// ScanSkillsDirectory scans .claude/skills/ for SKILL.md files
func ScanSkillsDirectory(skillsDir string) ([]*Skill, error) {
	var skills []*Skill

	// Walk the skills directory recursively
	err := filepath.Walk(skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process SKILL.md files
		if info.IsDir() {
			return nil
		}

		if filepath.Base(path) != "SKILL.md" {
			return nil
		}

		// Parse the skill file
		skill, err := ParseSkillFile(path)
		if err != nil {
			if errors.Is(err, ErrNoFrontmatter) {
				// Missing frontmatter — still usable, just warn.
				slog.Default().Warn("skill file has no frontmatter, using slug as name",
					"path", path,
				)
			} else {
				// Hard parse failure — skip this file.
				slog.Default().Warn("failed to parse skill file",
					"path", path,
					"error", err,
				)
				return nil
			}
		}

		skills = append(skills, skill)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk skills directory: %w", err)
	}

	return skills, nil
}

// ScanAgentsDirectory scans .claude/agents/ for agent definitions
func ScanAgentsDirectory(agentsDir string) ([]*AgentDefinition, error) {
	var agents []*AgentDefinition

	// Walk the agents directory
	err := filepath.Walk(agentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process markdown files
		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".md" {
			return nil
		}

		// Parse the agent file
		agentDefs, err := ParseAgentFile(path)
		if err != nil {
			// Log but don't fail the entire scan
			slog.Default().Warn("failed to parse agent file",
				"path", path,
				"error", err,
			)
			return nil
		}

		agents = append(agents, agentDefs...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk agents directory: %w", err)
	}

	return agents, nil
}

// InvalidateCache invalidates cached artifacts for a directory
func (as *ArtifactScanner) InvalidateCache(dir string) {
	normalizedDir, err := NormalizePath(dir)
	if err != nil {
		return
	}

	as.mu.RLock()
	cache := as.cache
	as.mu.RUnlock()
	if cache != nil {
		cache.Invalidate(normalizedDir)
	}
}

// SetWorkingDir changes the working directory.
//
// Safe for concurrent use with Scan/GetWorkingDir/InvalidateCache. Callers that
// want atomic "set dir then scan" semantics must hold their own higher-level
// coordination — this method only guards the workingDir field itself.
func (as *ArtifactScanner) SetWorkingDir(dir string) {
	as.mu.Lock()
	as.workingDir = dir
	as.mu.Unlock()
}

// GetWorkingDir returns the current working directory
func (as *ArtifactScanner) GetWorkingDir() string {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.workingDir
}
