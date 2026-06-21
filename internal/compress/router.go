package compress

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ContentType identifies the type of content for routing.
type ContentType string

const (
	ContentJSON     ContentType = "json"
	ContentCode     ContentType = "code"
	ContentLogs     ContentType = "logs"
	ContentSearch   ContentType = "search"
	ContentDiff     ContentType = "diff"
	ContentText     ContentType = "text"
	ContentUnknown  ContentType = "unknown"
)

// ContentRouter routes content to the appropriate compressor.
type ContentRouter struct {
	smartCrusher     *SmartCrusher
	codeCompressor   *CodeCompressor
	logCompressor    *LogCompressor
	searchCompressor *SearchCompressor
}

// ContentRouterConfig configures the ContentRouter.
type ContentRouterConfig struct {
	SmartCrusher     SmartCrusherConfig
	CodeCompressor   CodeCompressorConfig
	LogCompressor    LogCompressorConfig
	SearchCompressor SearchCompressorConfig
}

// DefaultContentRouterConfig returns default configuration.
func DefaultContentRouterConfig() ContentRouterConfig {
	return ContentRouterConfig{
		SmartCrusher:     DefaultSmartCrusherConfig(),
		CodeCompressor:   DefaultCodeCompressorConfig(),
		LogCompressor:    DefaultLogCompressorConfig(),
		SearchCompressor: DefaultSearchCompressorConfig(),
	}
}

// NewContentRouter creates a ContentRouter with the given config.
func NewContentRouter(cfg ContentRouterConfig) *ContentRouter {
	return &ContentRouter{
		smartCrusher:     NewSmartCrusher(cfg.SmartCrusher),
		codeCompressor:   NewCodeCompressor(cfg.CodeCompressor),
		logCompressor:    NewLogCompressor(cfg.LogCompressor),
		searchCompressor: NewSearchCompressor(cfg.SearchCompressor),
	}
}

// DetectType identifies the content type based on heuristics.
func (r *ContentRouter) DetectType(content string) ContentType {
	// Trim whitespace for detection
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return ContentUnknown
	}

	// JSON: Try parsing
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var v interface{}
		if err := json.Unmarshal([]byte(content), &v); err == nil {
			return ContentJSON
		}
	}

	// Diff: Check for diff markers
	if isDiffContent(trimmed) {
		return ContentDiff
	}

	// Logs: Check for log patterns
	if isLogContent(trimmed) {
		return ContentLogs
	}

	// Search: Check for grep-style output
	if isSearchContent(trimmed) {
		return ContentSearch
	}

	// Code: Check for code patterns
	if isCodeContent(trimmed) {
		return ContentCode
	}

	// Fallback: text
	return ContentText
}

// Compress routes content to the appropriate compressor and returns the result.
func (r *ContentRouter) Compress(content string, contentType ContentType, query string) (string, CompressionResult) {
	switch contentType {
	case ContentJSON:
		return r.smartCrusher.Crush(content)
	case ContentCode:
		return r.codeCompressor.Crush(content, "")
	case ContentLogs:
		return r.logCompressor.Crush(content)
	case ContentSearch:
		return r.searchCompressor.Crush(content, query)
	case ContentDiff:
		// Diff: use special diff compressor (for now, passthrough)
		return content, CompressionResult{
			OriginalContent:  content,
			CompressedContent: content,
			OriginalTokens:   countTokens(content),
			CompressedTokens: countTokens(content),
			TokensSaved:      0,
			CompressionRatio: 1.0,
			Strategy:         StrategyPassthrough,
			TransformsApplied: []string{"diff_passthrough"},
		}
	default:
		// Text: use SmartCrusher if it looks like structured data
		if strings.Contains(content, ":") && strings.Contains(content, "{") {
			return r.smartCrusher.Crush(content)
		}
		// Plain text: passthrough
		return content, CompressionResult{
			OriginalContent:  content,
			CompressedContent: content,
			OriginalTokens:   countTokens(content),
			CompressedTokens: countTokens(content),
			TokensSaved:      0,
			CompressionRatio: 1.0,
			Strategy:         StrategyPassthrough,
			TransformsApplied: []string{"text_passthrough"},
		}
	}
}

// Log content patterns
var logPattern = regexp.MustCompile(`(?m)^\d{4}[-/]\d{2}[-/]\d{2}`) // Date at line start
var logLevelPattern = regexp.MustCompile(`(?i)\b(ERROR|WARN|WARNING|INFO|DEBUG|FATAL|TRACE)\b`)

func isLogContent(content string) bool {
	// Check for log patterns
	if logPattern.MatchString(content) {
		return true
	}
	// Check for log levels
	matches := logLevelPattern.FindAllString(content, -1)
	return len(matches) >= 2
}

// Search content patterns (grep-style: filename:line:content)
var searchPattern = regexp.MustCompile(`(?m)^[^:\s]+:\d+:`)

func isSearchContent(content string) bool {
	lines := strings.Split(content, "\n")
	matchCount := 0
	for _, line := range lines[:min(len(lines), 10)] {
		if searchPattern.MatchString(line) {
			matchCount++
		}
	}
	return matchCount >= 2
}

// Diff content patterns
var diffPattern = regexp.MustCompile(`(?m)^(diff --git|index |--- |\+\+\+ |@@ )`)

func isDiffContent(content string) bool {
	return diffPattern.MatchString(content)
}

// Code content patterns
var codePatterns = []string{
	"func ", "function ", "def ", "fn ",
	"import ", "#include", "from ",
	"interface ", "class ", "struct ",
	"const ", "let ", "var ",
}

func isCodeContent(content string) bool {
	for _, pattern := range codePatterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}
	return false
}
