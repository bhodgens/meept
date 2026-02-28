package context

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ArtifactScanner scans for Claude artifacts in a directory
type ArtifactScanner struct {
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
	// Normalize the working directory
	normalizedDir, err := NormalizePath(as.workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize working directory: %w", err)
	}

	// Check cache first
	if as.cache != nil {
		if cached, found := as.cache.Get(normalizedDir); found {
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
	if as.cache != nil {
		as.cache.Put(normalizedDir, artifacts)
	}

	return artifacts, nil
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
			// Log but don't fail the entire scan
			fmt.Printf("Warning: failed to parse skill file %s: %v\n", path, err)
			return nil
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
			fmt.Printf("Warning: failed to parse agent file %s: %v\n", path, err)
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

	if as.cache != nil {
		as.cache.Invalidate(normalizedDir)
	}
}

// SetWorkingDir changes the working directory
func (as *ArtifactScanner) SetWorkingDir(dir string) {
	as.workingDir = dir
}

// GetWorkingDir returns the current working directory
func (as *ArtifactScanner) GetWorkingDir() string {
	return as.workingDir
}
