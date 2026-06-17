// Package metrics provides metrics collection and storage for Meept.
package metrics

import (
	"regexp"
	"strings"
)

// codeBlockPattern matches fenced code blocks in markdown responses.
// It is compiled once at package init rather than on every Analyze call
// to avoid repeated regex compilation overhead (S6-15).
var codeBlockPattern = regexp.MustCompile("```[^`]*```")

// ResponseAnalyzer analyzes LLM responses for quality metrics.
type ResponseAnalyzer struct {
	lazyPatterns []*regexp.Regexp
}

// ResponseQuality holds the results of response quality analysis.
type ResponseQuality struct {
	WellFormed      bool
	ParseErrors     []string
	HasCodeBlocks   bool
	HasExplanations bool
	IsLazy          bool
	LazyReason      string
	TokenCount      int
	CodeTokenPct    float64
}

// NewResponseAnalyzer creates a new ResponseAnalyzer with lazy detection patterns.
func NewResponseAnalyzer() *ResponseAnalyzer {
	// Patterns to detect lazy/abbreviated responses
	lazyPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)//\s*rest of code`),
		regexp.MustCompile(`(?i)#\s*rest of the file`),
		regexp.MustCompile(`(?i)/\s*\.+\s*\*/`),
		regexp.MustCompile(`(?i)\.\.\.\s*existing code`),
		regexp.MustCompile(`(?i)#\s*\.+\s*`),
		regexp.MustCompile(`(?i)//\s*etc\.`),
	}

	return &ResponseAnalyzer{
		lazyPatterns: lazyPatterns,
	}
}

// Analyze analyzes an LLM response for quality metrics.
func (a *ResponseAnalyzer) Analyze(response string, tokenCount int) *ResponseQuality {
	editFormat := detectEditFormat(response)

	quality := &ResponseQuality{
		TokenCount: tokenCount,
		WellFormed: isWellFormed(response, editFormat),
	}

	// Detect code blocks (check for ```)
	quality.HasCodeBlocks = strings.Contains(response, "```")

	// Detect explanations (check for common explanatory phrases)
	explanationPhrases := []string{"here's", "i'll", "let me", "sure!", "of course"}
	lowerResponse := strings.ToLower(response)
	for _, phrase := range explanationPhrases {
		if strings.Contains(lowerResponse, phrase) {
			quality.HasExplanations = true
			break
		}
	}

	// Detect lazy responses using regex patterns
	for _, pattern := range a.lazyPatterns {
		if pattern.MatchString(response) {
			quality.IsLazy = true
			quality.LazyReason = pattern.String()
			// Remove the (?i) prefix from the reason for clarity
			quality.LazyReason = strings.TrimPrefix(quality.LazyReason, "(?i)")
			break
		}
	}

	// Calculate code token percentage if there are code blocks
	if quality.HasCodeBlocks && tokenCount > 0 {
		// Estimate code tokens by measuring code block content
		codeBlocks := codeBlockPattern.FindAllString(response, -1)
		if len(codeBlocks) > 0 {
			var totalCodeChars int
			for _, block := range codeBlocks {
				// Remove the ``` markers (6 chars) and estimate token count
				content := strings.Trim(block, "`")
				content = strings.TrimPrefix(content, "``")
				content = strings.TrimPrefix(content, "``")
				totalCodeChars += len(content)
			}
			// Rough estimate: ~4 characters per token
			codeTokens := totalCodeChars / 4
			quality.CodeTokenPct = float64(codeTokens) / float64(tokenCount) * 100
			if quality.CodeTokenPct > 100 {
				quality.CodeTokenPct = 100
			}
		}
	}

	return quality
}

// detectEditFormat determines which edit format is being used based on response content.
func detectEditFormat(response string) string {
	// Check for editblock-fenced: has code fences AND conflict markers
	hasFence := strings.Contains(response, "```")
	hasConflictMarkers := strings.Contains(response, "<<<<<<<")
	if hasFence && hasConflictMarkers {
		return "editblock-fenced"
	}

	// Check for editblock: has conflict markers without code fences
	if hasConflictMarkers {
		return "editblock"
	}

	// Check for udiff: has unified diff markers
	hasMinus := strings.Contains(response, "--- a/")
	hasPlus := strings.Contains(response, "+++ b/")
	if hasMinus && hasPlus {
		return "udiff"
	}

	// No recognized edit format
	return ""
}

// isWellFormed validates that a response has the correct format markers.
func isWellFormed(response string, editFormat string) bool {
	switch editFormat {
	case "editblock":
		// Check for conflict markers
		hasSearch := strings.Contains(response, "<<<<<<< SEARCH")
		hasReplace := strings.Contains(response, ">>>>>>> REPLACE")
		return hasSearch && hasReplace
	case "editblock-fenced":
		// Check for both code fences and conflict markers
		hasFence := strings.Contains(response, "```")
		hasSearch := strings.Contains(response, "<<<<<<< SEARCH")
		return hasFence && hasSearch
	case "udiff":
		// Check for unified diff markers
		hasMinus := strings.Contains(response, "--- a/")
		hasPlus := strings.Contains(response, "+++ b/")
		return hasMinus && hasPlus
	default:
		// Unknown format - assume well-formed
		return true
	}
}
