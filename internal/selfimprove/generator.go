// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/google/uuid"
)

const generationPrompt = `You are a senior software engineer fixing a code issue.

Root Cause Analysis:
- Root Cause: %s
- Contributing Factors: %s
- Affected Files: %s
- Confidence: %.2f

Original Issue:
- Type: %s
- Severity: %s
- Description: %s

Current Code:
%s

Generate a fix for this issue. Your response MUST follow this exact format:

FILE: <path to file>
RISK: <low|medium|high>
DESCRIPTION: <brief description of the fix>
DIFF:
<<<<<<< ORIGINAL
<original code>
=======
<fixed code>
>>>>>>> FIXED

Important:
- Only modify what's necessary
- Ensure the fix is minimal and focused
- Consider edge cases
- Don't introduce new issues`

// PatchGenerator generates fixes for analyzed issues.
type PatchGenerator struct {
	config      AIInfraConfig
	safety      SafetyConfig
	llmClient   *llm.Client
	projectRoot string
	logger      *slog.Logger

	compiledProtectedPatterns []*regexp.Regexp
}

// NewPatchGenerator creates a new PatchGenerator.
func NewPatchGenerator(aiCfg AIInfraConfig, safetyCfg SafetyConfig, llmClient *llm.Client, projectRoot string, logger *slog.Logger) *PatchGenerator {
	if logger == nil {
		logger = slog.Default()
	}
	// Pre-compile protected patterns to avoid recompilation on every call.
	compiled := make([]*regexp.Regexp, 0, len(safetyCfg.ProtectedPatterns))
	for _, p := range safetyCfg.ProtectedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			logger.Warn("invalid protected pattern, skipping", "pattern", p, "error", err)
			continue
		}
		compiled = append(compiled, re)
	}

	return &PatchGenerator{
		config:                    aiCfg,
		safety:                    safetyCfg,
		llmClient:                 llmClient,
		projectRoot:               projectRoot,
		logger:                    logger,
		compiledProtectedPatterns: compiled,
	}
}

// Generate generates a fix for an analysis.
func (g *PatchGenerator) Generate(ctx context.Context, analysis *RootCauseAnalysis, issue Issue) (*ProposedFix, error) {
	// Read current code from affected files
	currentCode := g.readAffectedFiles(analysis.AffectedFiles)

	// Check if files are protected
	for _, file := range analysis.AffectedFiles {
		if g.isProtected(file) {
			g.logger.Warn("skipping protected file", "file", file)
			return nil, fmt.Errorf("file %s is protected", file)
		}
	}

	if g.llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	prompt := fmt.Sprintf(generationPrompt,
		analysis.RootCause,
		strings.Join(analysis.Contributing, ", "),
		strings.Join(analysis.AffectedFiles, ", "),
		analysis.Confidence,
		issue.Type,
		issue.Severity,
		issue.Description,
		currentCode,
	)

	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}

	resp, err := g.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	return g.parseGenerationResponse(analysis.IssueID, resp.Content)
}

// GenerateBatch generates fixes for multiple analyses.
func (g *PatchGenerator) GenerateBatch(ctx context.Context, analyses []*RootCauseAnalysis, issues []Issue) ([]*ProposedFix, error) {
	// Create issue lookup map
	issueMap := make(map[string]Issue)
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	fixes := make([]*ProposedFix, 0, len(analyses))

	for _, analysis := range analyses {
		select {
		case <-ctx.Done():
			return fixes, ctx.Err()
		default:
		}

		issue, ok := issueMap[analysis.IssueID]
		if !ok {
			g.logger.Warn("issue not found for analysis", "issue_id", analysis.IssueID)
			continue
		}

		fix, err := g.Generate(ctx, analysis, issue)
		if err != nil {
			g.logger.Warn("failed to generate fix", "issue_id", analysis.IssueID, "error", err)
			continue
		}
		if fix != nil {
			fixes = append(fixes, fix)
		}
	}

	return fixes, nil
}

// readAffectedFiles reads content from affected files.
func (g *PatchGenerator) readAffectedFiles(files []string) string {
	var sb strings.Builder

	for _, file := range files {
		fullPath := file
		if !strings.HasPrefix(file, "/") {
			fullPath = g.projectRoot + "/" + file
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Fprintf(&sb, "// Unable to read %s: %v\n", file, err)
			continue
		}

		fmt.Fprintf(&sb, "// File: %s\n", file)
		sb.WriteString(string(content))
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// isProtected checks if a file matches protected patterns.
func (g *PatchGenerator) isProtected(file string) bool {
	for _, re := range g.compiledProtectedPatterns {
		if re.MatchString(file) {
			return true
		}
	}
	return false
}

// parseGenerationResponse parses the LLM response into a ProposedFix.
func (g *PatchGenerator) parseGenerationResponse(issueID, response string) (*ProposedFix, error) {
	fix := &ProposedFix{
		ID:          uuid.New().String()[:16],
		IssueID:     issueID,
		GeneratedAt: time.Now(),
	}

	lines := strings.Split(response, "\n")
	inDiff := false
	var diffLines []string

	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "FILE:"); ok {
			fix.FilePath = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "RISK:"); ok {
			fix.Risk = strings.ToLower(strings.TrimSpace(after))
		} else if after, ok := strings.CutPrefix(line, "DESCRIPTION:"); ok {
			fix.Description = strings.TrimSpace(after)
		} else if strings.HasPrefix(line, "DIFF:") {
			inDiff = true
		} else if inDiff {
			diffLines = append(diffLines, line)
		}
	}

	fix.Diff = strings.Join(diffLines, "\n")

	// Determine fix type based on content
	switch {
	case strings.Contains(fix.Description, "refactor"):
		fix.Type = FixTypeRefactor
	case strings.Contains(fix.Description, "config"):
		fix.Type = FixTypeConfigChange
	default:
		fix.Type = FixTypeCodeChange
	}

	// Validate we have essential fields
	if fix.FilePath == "" || fix.Diff == "" {
		return nil, fmt.Errorf("invalid fix response: missing file path or diff")
	}

	return fix, nil
}

// Close cleans up resources.
func (g *PatchGenerator) Close() error {
	return nil
}
