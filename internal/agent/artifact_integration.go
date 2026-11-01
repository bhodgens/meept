package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	artifactcontext "github.com/caimlas/meept/internal/context"
	"github.com/caimlas/meept/internal/llm"
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

	// Cache results and update context builder under lock to prevent
	// data races with concurrent readers of am.contextBuilder.
	am.mu.Lock()
	am.artifactCache[dir] = artifacts
	if artifacts.Available {
		am.contextBuilder = artifactcontext.NewContextBuilder(artifacts)
	}
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

// getContextBuilder returns the current context builder under a read lock.
// Callers must use the returned pointer rather than reading am.contextBuilder
// directly to avoid data races with ScanDirectory which writes the field.
func (am *ArtifactManager) getContextBuilder() *artifactcontext.ContextBuilder {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.contextBuilder
}

// BuildContext builds relevant context for a task based on Claude artifacts.
func (am *ArtifactManager) BuildContext(taskDesc, workingDir string) (string, bool) {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	cb := am.getContextBuilder()
	if err != nil || !artifacts.Available || cb == nil {
		return "", false
	}

	// Build context for task
	taskCtx := cb.BuildForTask(taskDesc)

	// Format for prompt
	promptContext := cb.FormatForPrompt(taskCtx)

	// Check if context should be injected
	if !cb.ShouldInjectContext(taskDesc) {
		return "", false
	}

	return promptContext, true
}

// GetRelevantCommands returns relevant commands from CLAUDE.md for a task.
func (am *ArtifactManager) GetRelevantCommands(taskDesc, workingDir string) []artifactcontext.BuildCommand {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	cb := am.getContextBuilder()
	if err != nil || !artifacts.Available || cb == nil {
		return nil
	}

	return cb.GetRelevantCommands(taskDesc)
}

// FindSkillForTask finds a relevant skill from .claude/skills/ for a task.
func (am *ArtifactManager) FindSkillForTask(taskDesc, workingDir string) *artifactcontext.Skill {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	cb := am.getContextBuilder()
	if err != nil || !artifacts.Available || cb == nil {
		return nil
	}

	return cb.FindSkillForTask(taskDesc)
}

// FindAgentForTask finds a relevant agent from .claude/agents/ for a task.
func (am *ArtifactManager) FindAgentForTask(taskDesc, workingDir string) *artifactcontext.AgentDefinition {
	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	cb := am.getContextBuilder()
	if err != nil || !artifacts.Available || cb == nil {
		return nil
	}

	return cb.FindAgentForTask(taskDesc)
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

// --- Doc budgets (leaf 02-doc-budgets, docs/plans/20261031-context-memory-engine/) ---

// DefaultDocBudgetTokens is the fallback budget for dynamic doc sections
// when no allocator-derived budget is supplied. It matches the scale of
// MaxMemoryContextTokens (2000 tokens) so a caller that has not wired the
// allocator yet still gets a bounded prompt instead of today's unbounded
// CLAUDE.md injection.
const DefaultDocBudgetTokens = 2000

// docBudgetTruncationNotice is the ASCII marker appended when doc content is
// trimmed to budget. It carries the original and budgeted token counts via
// fmt.
const docBudgetTruncationNotice = "[... CLAUDE.md truncated: %d/%d tokens ...]"

// llmHeuristicTokenizerAdapter adapts llm.HeuristicTokenizer to the
// docBudgetTokenizer seam (kept distinct from the llm package type so tests
// can assert the fallback choice by concrete type).
type llmHeuristicTokenizerAdapter struct {
	inner llm.HeuristicTokenizer
}

func (a *llmHeuristicTokenizerAdapter) CountTokens(text string) int {
	return a.inner.CountTokens(text)
}

// docBudgetTokenizer is the minimal token-counting surface this leaf needs.
// llm.Tokenizer satisfies it structurally.
type docBudgetTokenizer interface {
	CountTokens(text string) int
}

// newDocBudgetTokenizer resolves the tokenizer for doc budgeting:
// a non-empty model id goes through llm.NewTokenizerForModel (accurate
// provider tokenizer); anything else falls back to the 3-chars-per-token
// HeuristicTokenizer. Both shapes satisfy docBudgetTokenizer.
func newDocBudgetTokenizer(model string) docBudgetTokenizer {
	if model != "" {
		if tk := llm.NewTokenizerForModel(model); tk != nil {
			return tk
		}
	}
	return &llmHeuristicTokenizerAdapter{inner: llm.HeuristicTokenizer{}}
}

// truncateDocToBudget keeps the head of content until the budget is reached
// and appends the ASCII truncation notice. noticeOriginal is the ORIGINAL
// token count reported in the notice (callers pass the untrimmed count so
// the notice always carries original vs budget); budgetTokens bounds what
// the returned string may cost INCLUDING the notice. Trimming walks back to
// the last newline boundary so a line is never cut mid-token-run, then
// re-checks the budget (the walk-back only shrinks the head, so the result
// stays within budget). Deterministic: identical content + budget +
// tokenizer produce identical output.
//
// Head-truncation over tail-keeping: findings-context-budgeting.md R-A4 /
// Budget Practice 2 (component-appropriate reduction, docs hard-capped) and
// P2 (Claude Code caps surviving skill/doc bodies, oldest dropped first);
// project instructions cluster at the top of CLAUDE.md/README files.
func truncateDocToBudget(content string, budgetTokens int, noticeOriginal int, tk docBudgetTokenizer) (string, bool) {
	original := tk.CountTokens(content)
	if original <= budgetTokens {
		return content, false
	}

	notice := fmt.Sprintf(docBudgetTruncationNotice, noticeOriginal, budgetTokens)
	noticeTokens := tk.CountTokens("\n\n" + notice)

	// Reserve room for the notice, then find the longest byte prefix whose
	// token count fits the remaining budget. Token counts are monotone in
	// prefix length, so a binary search bounds this deterministically.
	avail := budgetTokens - noticeTokens
	if avail < 0 {
		avail = 0
	}
	lo, hi := 0, len(content)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if tk.CountTokens(content[:mid]) <= avail {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	head := content[:lo]

	// Walk back to the last newline so no line is cut mid-sentence. This
	// only removes bytes, so the token count cannot grow past the budget.
	if idx := strings.LastIndex(head, "\n"); idx > 0 {
		head = head[:idx]
	}

	return head + "\n\n" + notice, true
}

// buildFullArtifactContextSections assembles the UNBOUNDED section list in
// exactly the order and shape of BuildFullArtifactContext (CLAUDE.md
// verbatim, summarized README, skill refs, agent refs, "\n\n---\n\n"
// separator). Keeping this shared means the budgeted builder is
// byte-identical to the unbounded one whenever everything fits.
func (am *ArtifactManager) buildFullArtifactContextSections(taskDesc string, artifacts *artifactcontext.Artifacts) string {
	var sections []string

	if artifacts.HasCLAUDEMD() && artifacts.CLAUDEMD.RawContent != "" {
		sections = append(sections, "# CLAUDE.md (full content)\n\n"+artifacts.CLAUDEMD.RawContent)
	}
	if artifacts.HasREADME() {
		sections = append(sections, "# README.md\n\n"+summarizeREADME(artifacts.READMEContent))
	}
	if skill := am.BuildSkillReferences(taskDesc, artifacts.WorkingDir); skill != "" {
		sections = append(sections, skill)
	}
	if agent := am.BuildAgentReferences(taskDesc, artifacts.WorkingDir); agent != "" {
		sections = append(sections, agent)
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n---\n\n")
}

// BuildFullArtifactContextBudgeted is the budgeted replacement for
// BuildFullArtifactContext: it combines the same sections (CLAUDE.md
// verbatim, README, skill/agent references) but bounds the assembled
// section at docBudgetTokens tokens, closing the unbounded-CLAUDE.md hole.
//
// Budget semantics:
//   - docBudgetTokens <= 0 applies DefaultDocBudgetTokens (2000, the
//     MaxMemoryContextTokens scale) — the unbounded behavior is replaced,
//     never preserved, at this entry point.
//   - tk selects the token counter: pass a tokenizer from
//     llm.NewTokenizerForModel when the serving model is known; tk == nil
//     falls back to llm.HeuristicTokenizer. The SAME tokenizer counts the
//     budget and the content at build time, so the two counts agree.
//   - Under budget the output is byte-identical to the unbounded builder
//     (stable-prefix rule: trimming only engages over budget).
//   - Over budget, doc content is trimmed deterministically in priority
//     order (CLAUDE.md first — it is the authoritative instruction doc the
//     hole was in — then README) to the longest head that fits, each with
//     the ASCII notice "[... CLAUDE.md truncated: NNNN/TTTT tokens ...]"
//     carrying original and budgeted counts. Headers, separators, and the
//     concise skill/agent references are counted against the budget, so the
//     TOTAL section respects it.
//
// Doc content is per-project DYNAMIC and stays in the unstable prompt
// region; this function only produces the section body and never affects
// the stablePrefixHash classification (pinned by
// TestBaselineStablePrefixHash / TestStablePrefixHashInvariantUnderDocVariation).
//
// The budget source in the wired path is Allocation.SystemDynamicDocs from
// internal/agent/contextalloc (window-fraction budget, R-A4); the default
// here is only the pre-wiring fallback.
//
// Truncation basis: findings-context-budgeting.md R-A4 (docs hard-capped),
// Budget Practice 2 (component-appropriate reduction), P2 (Claude Code
// capped doc bodies).
//
// The old BuildFullArtifactContext signature and behavior are UNCHANGED for
// existing callers (caller rewiring is leaf 03-integration's scope).
func (am *ArtifactManager) BuildFullArtifactContextBudgeted(
	taskDesc, workingDir string, docBudgetTokens int, tk docBudgetTokenizer,
) (section string, truncated bool) {
	if docBudgetTokens <= 0 {
		docBudgetTokens = DefaultDocBudgetTokens
	}
	if tk == nil {
		tk = newDocBudgetTokenizer("")
	}

	// Ensure directory is scanned
	artifacts, err := am.ScanDirectory(workingDir)
	if err != nil || !artifacts.Available {
		return "", false
	}

	unbounded := am.buildFullArtifactContextSections(taskDesc, artifacts)
	if unbounded == "" {
		return "", false
	}
	if tk.CountTokens(unbounded) <= docBudgetTokens {
		// Stable-prefix rule: everything fits, so the output is byte
		// identical to the unbounded builder.
		return unbounded, false
	}

	// Over budget: trim doc content in priority order (CLAUDE.md, then
	// README) until the joined section fits. Each trim budget counts every
	// OTHER byte of the section (headers, separators, refs, the other doc's
	// trimmed remainder), so the total always lands within budget. The
	// truncation notice reports the ORIGINAL doc size against the SECTION
	// budget (per the interface contract: "[... CLAUDE.md truncated:
	// NNNN/TTTT tokens ...]" carries original and budgeted counts), so the
	// section budget is threaded through as noticeBudget.
	//
	// parts holds the rendered blocks (header + content) in order; contents
	// holds the still-trimmable content per block ("" for refs and already
	// trimmed docs).
	var parts []string
	var contents []string
	if artifacts.HasCLAUDEMD() && artifacts.CLAUDEMD.RawContent != "" {
		parts = append(parts, "# CLAUDE.md (full content)\n\n"+artifacts.CLAUDEMD.RawContent)
		contents = append(contents, artifacts.CLAUDEMD.RawContent)
	}
	if artifacts.HasREADME() {
		readme := summarizeREADME(artifacts.READMEContent)
		parts = append(parts, "# README.md\n\n"+readme)
		contents = append(contents, readme)
	}
	if skill := am.BuildSkillReferences(taskDesc, workingDir); skill != "" {
		parts = append(parts, skill)
		contents = append(contents, "")
	}
	if agent := am.BuildAgentReferences(taskDesc, workingDir); agent != "" {
		parts = append(parts, agent)
		contents = append(contents, "")
	}

	join := func() string { return strings.Join(parts, "\n\n---\n\n") }
	trimmable := 0
	for _, c := range contents {
		trimmable += len(c)
	}

	anyTruncated := false
	for i := range parts {
		if tk.CountTokens(join()) <= docBudgetTokens {
			break
		}
		if contents[i] == "" {
			continue // concise ref section, not trimmable
		}
		original := contents[i]
		parts[i] = parts[i][:len(parts[i])-len(original)] // keep the header only
		rest := tk.CountTokens(join())
		avail := docBudgetTokens - rest
		if avail < 0 {
			avail = 0
		}
		trimmed, wasTruncated := truncateDocToBudget(original, avail, tk.CountTokens(original), tk)
		parts[i] += trimmed
		contents[i] = ""
		trimmable -= len(original)
		if wasTruncated {
			anyTruncated = true
		}
	}

	section = join()
	return section, anyTruncated
}
