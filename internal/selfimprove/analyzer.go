// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

const analysisPrompt = `You are analyzing a software issue to determine its root cause.

Issue Details:
- Type: %s
- Severity: %s
- Description: %s
- Source: %s

Context:
%s

Related Code (if available):
%s

Analyze this issue and provide:
1. ROOT CAUSE: The fundamental cause of this issue
2. CONTRIBUTING FACTORS: List any contributing factors
3. AFFECTED FILES: List files that likely need to be modified
4. CONFIDENCE: Your confidence level (0.0 to 1.0) in this analysis

Format your response as:
ROOT_CAUSE: <description>
FACTORS: <comma-separated list>
FILES: <comma-separated list of file paths>
CONFIDENCE: <number>`

// RootCauseAnalyzer analyzes issues to determine root causes.
type RootCauseAnalyzer struct {
	config      AIInfraConfig
	llmClient   *llm.Client
	projectRoot string
	logger      *slog.Logger
}

// NewRootCauseAnalyzer creates a new RootCauseAnalyzer.
func NewRootCauseAnalyzer(cfg AIInfraConfig, llmClient *llm.Client, projectRoot string, logger *slog.Logger) *RootCauseAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &RootCauseAnalyzer{
		config:      cfg,
		llmClient:   llmClient,
		projectRoot: projectRoot,
		logger:      logger,
	}
}

// Analyze analyzes a single issue.
func (a *RootCauseAnalyzer) Analyze(ctx context.Context, issue Issue) (*RootCauseAnalysis, error) {
	// Try to read related code if source is a code file
	relatedCode := ""
	if strings.HasSuffix(issue.Source, ".go") {
		if content, err := os.ReadFile(issue.Source); err == nil {
			// Extract relevant portion around the issue
			relatedCode = a.extractRelevantCode(string(content), issue)
		}
	}

	prompt := fmt.Sprintf(analysisPrompt,
		issue.Type,
		issue.Severity,
		issue.Description,
		issue.Source,
		issue.Context,
		relatedCode,
	)

	if a.llmClient == nil {
		return a.fallbackAnalysis(issue), nil
	}

	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}

	resp, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		a.logger.Warn("LLM analysis failed, using fallback", "error", err)
		return a.fallbackAnalysis(issue), nil
	}

	return a.parseAnalysisResponse(issue.ID, resp.Content), nil
}

// AnalyzeBatch analyzes multiple issues.
func (a *RootCauseAnalyzer) AnalyzeBatch(ctx context.Context, issues []Issue) ([]*RootCauseAnalysis, error) {
	analyses := make([]*RootCauseAnalysis, 0, len(issues))

	for _, issue := range issues {
		select {
		case <-ctx.Done():
			return analyses, ctx.Err()
		default:
		}

		analysis, err := a.Analyze(ctx, issue)
		if err != nil {
			a.logger.Warn("failed to analyze issue", "issue_id", issue.ID, "error", err)
			continue
		}
		analyses = append(analyses, analysis)
	}

	return analyses, nil
}

// extractRelevantCode extracts code around the issue.
func (a *RootCauseAnalyzer) extractRelevantCode(content string, issue Issue) string {
	lines := strings.Split(content, "\n")

	// Try to find the issue line
	lineNum := 0
	if ln, ok := issue.Metadata["line_number"].(int); ok {
		lineNum = ln
	}

	// Extract context around the line
	start := lineNum - 10
	if start < 0 {
		start = 0
	}
	end := lineNum + 10
	if end > len(lines) {
		end = len(lines)
	}

	relevantLines := lines[start:end]
	return strings.Join(relevantLines, "\n")
}

// parseAnalysisResponse parses the LLM response into a RootCauseAnalysis.
func (a *RootCauseAnalyzer) parseAnalysisResponse(issueID, response string) *RootCauseAnalysis {
	analysis := &RootCauseAnalysis{
		IssueID:    issueID,
		AnalyzedAt: time.Now(),
		Confidence: 0.5, // Default
	}

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "ROOT_CAUSE:") {
			analysis.RootCause = strings.TrimSpace(strings.TrimPrefix(line, "ROOT_CAUSE:"))
		} else if strings.HasPrefix(line, "FACTORS:") {
			factorsStr := strings.TrimSpace(strings.TrimPrefix(line, "FACTORS:"))
			for _, f := range strings.Split(factorsStr, ",") {
				if f = strings.TrimSpace(f); f != "" {
					analysis.Contributing = append(analysis.Contributing, f)
				}
			}
		} else if strings.HasPrefix(line, "FILES:") {
			filesStr := strings.TrimSpace(strings.TrimPrefix(line, "FILES:"))
			for _, f := range strings.Split(filesStr, ",") {
				if f = strings.TrimSpace(f); f != "" {
					analysis.AffectedFiles = append(analysis.AffectedFiles, f)
				}
			}
		} else if strings.HasPrefix(line, "CONFIDENCE:") {
			confStr := strings.TrimSpace(strings.TrimPrefix(line, "CONFIDENCE:"))
			var conf float64
			fmt.Sscanf(confStr, "%f", &conf)
			if conf >= 0 && conf <= 1 {
				analysis.Confidence = conf
			}
		}
	}

	// If parsing failed, use the whole response as root cause
	if analysis.RootCause == "" {
		analysis.RootCause = response
		analysis.Confidence = 0.3
	}

	return analysis
}

// fallbackAnalysis creates a basic analysis without LLM.
func (a *RootCauseAnalyzer) fallbackAnalysis(issue Issue) *RootCauseAnalysis {
	analysis := &RootCauseAnalysis{
		IssueID:    issue.ID,
		RootCause:  issue.Description,
		AnalyzedAt: time.Now(),
		Confidence: 0.3,
	}

	// Add source file if it exists
	if issue.Source != "" && strings.HasSuffix(issue.Source, ".go") {
		relPath, _ := filepath.Rel(a.projectRoot, issue.Source)
		analysis.AffectedFiles = []string{relPath}
	}

	return analysis
}

// Close cleans up resources.
func (a *RootCauseAnalyzer) Close() error {
	return nil
}
