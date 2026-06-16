package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	artifactcontext "github.com/caimlas/meept/internal/context"
	"github.com/caimlas/meept/internal/project"
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
	mu            sync.RWMutex

	// Cache expiry
	cacheExpiry time.Duration

	// projectRoot is the root directory of the current project, used for
	// hierarchical AGENTS.md loading.
	projectRoot string

	// Logger
	logger *slog.Logger
}

// NewArtifactManager creates a new artifact manager.
func NewArtifactManager(logger *slog.Logger) *ArtifactManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &ArtifactManager{
		claudeManager:  artifactcontext.NewArtifactManager(5 * time.Minute),
		contextBuilder: nil, // Will be created on first scan
		artifactCache:  make(map[string]*artifactcontext.Artifacts),
		cacheExpiry:    5 * time.Minute,
		logger:         logger,
	}
}

// ScanDirectory scans a directory for Claude artifacts and caches the results.
func (am *ArtifactManager) ScanDirectory(dir string) (*artifactcontext.Artifacts, error) {
	// Check cache first
	am.mu.RLock()
	if cached, ok := am.artifactCache[dir]; ok {
		am.mu.RUnlock()
		return cached, nil
	}
	am.mu.RUnlock()

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
	am.mu.Lock()
	am.artifactCache[dir] = artifacts
	am.mu.Unlock()

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
			"readme", artifacts.HasREADME(),
			"claude_dir", artifacts.ClaudeDir != nil,
			"skills", skillCount,
			"agents", agentCount,
		)
	}

	return artifacts, nil
}

// BuildContext builds relevant context for a task based on Claude artifacts.
func (am *ArtifactManager) BuildContext(taskDesc, workingDir string) (string, bool) {
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
	am.mu.Lock()
	delete(am.artifactCache, dir)
	am.mu.Unlock()
	if err := am.claudeManager.Invalidate(dir); err != nil {
		am.logger.Warn("failed to invalidate artifact cache", "dir", dir, "error", err)
	}
}

// InvalidateAll clears all cached artifacts.
func (am *ArtifactManager) InvalidateAll() {
	am.mu.Lock()
	am.artifactCache = make(map[string]*artifactcontext.Artifacts)
	am.mu.Unlock()
	am.claudeManager.InvalidateAll()
}

// GetArtifacts returns the cached artifacts for a directory.
func (am *ArtifactManager) GetArtifacts(dir string) *artifactcontext.Artifacts {
	am.mu.RLock()
	defer am.mu.RUnlock()
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
		fmt.Fprintf(&sb, "- **%s**: `%s`\n", cmd.Description, cmd.Command)
		if cmd.Category != "" {
			fmt.Fprintf(&sb, "  Category: %s\n", cmd.Category)
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
	fmt.Fprintf(&sb, "**%s** (v%s)\n\n", skill.Name, skill.Version)
	fmt.Fprintf(&sb, "%s\n\n", skill.Description)

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
	fmt.Fprintf(&sb, "**%s** (%s)\n\n", agent.Name, agent.Role)
	fmt.Fprintf(&sb, "%s\n\n", agent.Purpose)

	return sb.String()
}

// BuildFullArtifactContext builds a comprehensive context section combining
// project context, commands, skills, and agents.
//
// CLAUDE.md content is included verbatim (not lossy-summarized) because it
// contains authoritative project instructions. README.md content is included
// as a summary since READMEs can be very large.
func (am *ArtifactManager) BuildFullArtifactContext(taskDesc, workingDir string) string {
	var sections []string

	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available {
		return ""
	}

	// P3b: Include CLAUDE.md verbatim — bypass the lossy ContextBuilder.
	// The ContextBuilder decomposes CLAUDE.md into subsections and only
	// injects task-relevant pieces, dropping most of the content. We need
	// the full content because it contains authoritative project instructions.
	if artifacts.HasCLAUDEMD() && artifacts.CLAUDEMD.RawContent != "" {
		claudeSection := fmt.Sprintf("# CLAUDE.md (full content)\n\n%s", artifacts.CLAUDEMD.RawContent)
		sections = append(sections, claudeSection)
	}

	// P3a: Include README.md content (summary for large files).
	if artifacts.HasREADME() {
		readmeSummary := summarizeREADME(artifacts.READMEContent)
		readmeSection := fmt.Sprintf("# README.md\n\n%s", readmeSummary)
		sections = append(sections, readmeSection)
	}

	// Add skill references (these are concise and safe to include as-is)
	if skill := am.BuildSkillReferences(taskDesc, workingDir); skill != "" {
		sections = append(sections, skill)
	}

	// Add agent references (these are concise and safe to include as-is)
	if agent := am.BuildAgentReferences(taskDesc, workingDir); agent != "" {
		sections = append(sections, agent)
	}

	if len(sections) == 0 {
		return ""
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// summarizeREADME produces a condensed version of a README when the full
// content would be too large for the context window. For reasonably sized
// READMEs (under 4KB), the full content is returned as-is. Larger READMEs
// are truncated to the first 4KB with a truncation notice.
func summarizeREADME(content string) string {
	const maxReadmeBytes = 4 * 1024 // 4KB

	if len(content) <= maxReadmeBytes {
		return content
	}

	truncated := content[:maxReadmeBytes]
	// Walk back to the last newline to avoid cutting mid-line
	if idx := strings.LastIndex(truncated, "\n"); idx > 0 {
		truncated = truncated[:idx]
	}
	truncated += "\n\n[... README truncated at 4KB ...]"
	return truncated
}

// maxAgentsContextBytes caps the total size of combined AGENTS.md content injected
// into the prompt. This prevents deeply-nested hierarchies from consuming too much
// context window space.
const maxAgentsContextBytes = 8 * 1024 // 8KB

// WithProjectRoot sets the project root directory for hierarchical AGENTS.md loading.
// This must be called before LoadAgentsContext to have effect.
func (am *ArtifactManager) WithProjectRoot(root string) *ArtifactManager {
	am.projectRoot = root
	return am
}

// LoadAgentsContext loads hierarchical AGENTS.md files relative to filePath and
// returns them joined as a single string suitable for prompt injection.
// If projectRoot is not set, or no AGENTS.md files are found, returns "".
// The total output is capped at maxAgentsContextBytes.
func (am *ArtifactManager) LoadAgentsContext(filePath string) string {
	if am.projectRoot == "" {
		return ""
	}

	loaded, err := project.LoadAgentsMDForPath(am.projectRoot, filePath)
	if err != nil {
		am.logger.Debug("AGENTS.md loading failed", "error", err)
		return ""
	}
	if len(loaded) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, l := range loaded {
		if sb.Len() > 0 {
			sb.WriteString("\n---\n")
		}
		if l.RelPath != "" {
			fmt.Fprintf(&sb, "<!-- %s/AGENTS.md -->\n", l.RelPath)
		} else {
			sb.WriteString("<!-- AGENTS.md (root) -->\n")
		}
		sb.WriteString(l.Content)
	}

	result := sb.String()
	if len(result) > maxAgentsContextBytes {
		result = truncateAtNewline(result, maxAgentsContextBytes)
		result += "\n\n[... AGENTS.md context truncated at 8KB ...]"
	}

	return result
}

// truncateAtNewline truncates s to at most maxLen bytes, walking back to the
// last newline boundary to avoid cutting mid-line.
func truncateAtNewline(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	truncated := s[:maxLen]
	if idx := strings.LastIndex(truncated, "\n"); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated
}

// GetCacheStats returns cache statistics for debugging.
func (am *ArtifactManager) GetCacheStats() map[string]any {
	claudeStats := am.claudeManager.GetCacheStats()

	am.mu.RLock()
	dirsCached := len(am.artifactCache)
	am.mu.RUnlock()

	return map[string]any{
		"directories_cached": dirsCached,
		"claude_stats":       claudeStats,
		"cache_expiry":       am.cacheExpiry.String(),
	}
}
