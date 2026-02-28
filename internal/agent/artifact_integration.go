package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	artifactcontext "github.com/caimlas/meept/internal/context"
)

// ArtifactManager integrates Claude artifact detection and context building
// with the agent loop. It automatically detects and utilizes CLAUDE.md and
// .claude/ artifacts to enhance agent prompts with project-specific context.
type ArtifactManager struct {
	// Claude artifact manager for detection and parsing
	claudeManager *artifactcontext.ArtifactManager

	// Context builder for task-aware context
	contextBuilder *artifactcontext.ContextBuilder

	// Cache of scanned artifacts per directory
	artifactCache map[string]*artifactcontext.Artifacts

	// Cache expiry
	cacheExpiry time.Duration

	// Logger
	logger *slog.Logger
}

// NewArtifactManager creates a new artifact manager.
func NewArtifactManager(logger *slog.Logger) *ArtifactManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &ArtifactManager{
		claudeManager: artifactcontext.NewArtifactManager(5 * time.Minute),
		contextBuilder: nil, // Will be created on first scan
		artifactCache:  make(map[string]*artifactcontext.Artifacts),
		cacheExpiry:    5 * time.Minute,
		logger:         logger,
	}
}

// ScanDirectory scans a directory for Claude artifacts and caches the results.
func (am *ArtifactManager) ScanDirectory(dir string) (*artifactcontext.Artifacts, error) {
	// Check cache first
	if cached, ok := am.artifactCache[dir]; ok {
		return cached, nil
	}

	// Scan directory
	artifacts, err := am.claudeManager.ScanDirectory(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	// Update context builder if artifacts found
	if artifacts.Available {
		am.contextBuilder = artifactcontext.NewContextBuilder(artifacts)
	}

	// Cache results
	am.artifactCache[dir] = artifacts

	if artifacts.Available {
		skillCount := 0
		agentCount := 0
		if artifacts.ClaudeDir != nil {
			skillCount = len(artifacts.ClaudeDir.Skills)
			agentCount = len(artifacts.ClaudeDir.Agents)
		}
		am.logger.Debug("Claude artifacts detected",
			"directory", dir,
			"claude_md", artifacts.CLAUDEMD != nil,
			"claude_dir", artifacts.ClaudeDir != nil,
			"skills", skillCount,
			"agents", agentCount,
		)
	}

	return artifacts, nil
}

// BuildContext builds relevant context for a task based on Claude artifacts.
func (am *ArtifactManager) BuildContext(taskDesc string, workingDir string) (string, bool) {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available || am.contextBuilder == nil {
		return "", false
	}

	// Build context for task
	taskCtx := am.contextBuilder.BuildForTask(taskDesc)

	// Format for prompt
	promptContext := am.contextBuilder.FormatForPrompt(taskCtx)

	// Check if context should be injected
	if !am.contextBuilder.ShouldInjectContext(taskDesc) {
		return "", false
	}

	return promptContext, true
}

// GetRelevantCommands returns relevant commands from CLAUDE.md for a task.
func (am *ArtifactManager) GetRelevantCommands(taskDesc, workingDir string) []artifactcontext.BuildCommand {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available || am.contextBuilder == nil {
		return nil
	}

	return am.contextBuilder.GetRelevantCommands(taskDesc)
}

// FindSkillForTask finds a relevant skill from .claude/skills/ for a task.
func (am *ArtifactManager) FindSkillForTask(taskDesc, workingDir string) *artifactcontext.Skill {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available || am.contextBuilder == nil {
		return nil
	}

	return am.contextBuilder.FindSkillForTask(taskDesc)
}

// FindAgentForTask finds a relevant agent from .claude/agents/ for a task.
func (am *ArtifactManager) FindAgentForTask(taskDesc, workingDir string) *artifactcontext.AgentDefinition {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available || am.contextBuilder == nil {
		return nil
	}

	return am.contextBuilder.FindAgentForTask(taskDesc)
}

// HasArtifacts checks if Claude artifacts are available in a directory.
func (am *ArtifactManager) HasArtifacts(workingDir string) bool {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil {
		return false
	}
	return artifacts.Available
}

// InvalidateCache invalidates the cache for a specific directory.
func (am *ArtifactManager) InvalidateCache(dir string) {
	delete(am.artifactCache, dir)
	am.claudeManager.Invalidate(dir)
}

// InvalidateAll clears all cached artifacts.
func (am *ArtifactManager) InvalidateAll() {
	am.artifactCache = make(map[string]*artifactcontext.Artifacts)
	am.claudeManager.InvalidateAll()
}

// GetArtifacts returns the cached artifacts for a directory.
func (am *ArtifactManager) GetArtifacts(dir string) *artifactcontext.Artifacts {
	return am.artifactCache[dir]
}

// BuildArtifactContextSection builds a context section for inclusion in system prompts.
// This is the main integration point with the agent loop.
func (am *ArtifactManager) BuildArtifactContextSection(taskDesc, workingDir string) string {
	// Build context
	promptContext, hasContext := am.BuildContext(taskDesc, workingDir)
	if !hasContext {
		return ""
	}

	// Wrap in section header
	return fmt.Sprintf("# Project Context from Claude Artifacts\n\n%s", promptContext)
}

// BuildCommandSuggestions builds a section with relevant command suggestions.
func (am *ArtifactManager) BuildCommandSuggestions(taskDesc, workingDir string) string {
	commands := am.GetRelevantCommands(taskDesc, workingDir)
	if len(commands) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Relevant Build Commands\n\n")
	sb.WriteString("The following commands from CLAUDE.md may be relevant to this task:\n\n")

	for _, cmd := range commands {
		sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", cmd.Description, cmd.Command))
		if cmd.Category != "" {
			sb.WriteString(fmt.Sprintf("  Category: %s\n", cmd.Category))
		}
	}

	return sb.String()
}

// BuildSkillReferences builds a section with relevant skill references.
func (am *ArtifactManager) BuildSkillReferences(taskDesc, workingDir string) string {
	skill := am.FindSkillForTask(taskDesc, workingDir)
	if skill == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Relevant Skill\n\n")
	sb.WriteString(fmt.Sprintf("**%s** (v%s)\n\n", skill.Name, skill.Version))
	sb.WriteString(fmt.Sprintf("%s\n\n", skill.Description))

	if len(skill.Triggers) > 0 {
		sb.WriteString("Triggers: " + strings.Join(skill.Triggers, ", ") + "\n")
	}

	if skill.Category != "" {
		sb.WriteString("Category: " + skill.Category + "\n")
	}

	return sb.String()
}

// BuildAgentReferences builds a section with relevant agent references.
func (am *ArtifactManager) BuildAgentReferences(taskDesc, workingDir string) string {
	agent := am.FindAgentForTask(taskDesc, workingDir)
	if agent == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Relevant Agent\n\n")
	sb.WriteString(fmt.Sprintf("**%s** (%s)\n\n", agent.Name, agent.Role))
	sb.WriteString(fmt.Sprintf("%s\n\n", agent.Purpose))

	return sb.String()
}

// BuildFullArtifactContext builds a comprehensive context section combining
// project context, commands, skills, and agents.
func (am *ArtifactManager) BuildFullArtifactContext(taskDesc, workingDir string) string {
	var sections []string

	// Add project context
	if ctx := am.BuildArtifactContextSection(taskDesc, workingDir); ctx != "" {
		sections = append(sections, ctx)
	}

	// Add command suggestions
	if cmd := am.BuildCommandSuggestions(taskDesc, workingDir); cmd != "" {
		sections = append(sections, cmd)
	}

	// Add skill references
	if skill := am.BuildSkillReferences(taskDesc, workingDir); skill != "" {
		sections = append(sections, skill)
	}

	// Add agent references
	if agent := am.BuildAgentReferences(taskDesc, workingDir); agent != "" {
		sections = append(sections, agent)
	}

	if len(sections) == 0 {
		return ""
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// GetCacheStats returns cache statistics for debugging.
func (am *ArtifactManager) GetCacheStats() map[string]interface{} {
	claudeStats := am.claudeManager.GetCacheStats()

	return map[string]interface{}{
		"directories_cached": len(am.artifactCache),
		"claude_stats":       claudeStats,
		"cache_expiry":        am.cacheExpiry.String(),
	}
}
