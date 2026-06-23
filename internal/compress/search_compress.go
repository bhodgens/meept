package compress

import (
	"fmt"
	"regexp"
	"strings"
)

// SearchCompressor compresses grep/search results.
//
// Strategy:
// - Group by file
// - Show matching lines with context
// - Replace non-matching blocks with line ranges
//
// Typical savings: 80-95% on large search results.
type SearchCompressor struct {
	// ContextLines is the number of context lines around matches
	ContextLines int
	// MaxMatchesPerFile is the maximum matches to show per file
	MaxMatchesPerFile int
	// GroupByFile groups results by file
	GroupByFile bool
}

// SearchCompressorConfig configures the SearchCompressor.
type SearchCompressorConfig struct {
	ContextLines      int  `json:"context_lines" toml:"context_lines"`
	MaxMatchesPerFile int  `json:"max_matches_per_file" toml:"max_matches_per_file"`
	GroupByFile       bool `json:"group_by_file" toml:"group_by_file"`
}

// DefaultSearchCompressorConfig returns default configuration.
func DefaultSearchCompressorConfig() SearchCompressorConfig {
	return SearchCompressorConfig{
		ContextLines:      2,
		MaxMatchesPerFile: 10,
		GroupByFile:       true,
	}
}

// NewSearchCompressor creates a SearchCompressor with the given config.
func NewSearchCompressor(cfg SearchCompressorConfig) *SearchCompressor {
	return &SearchCompressor{
		ContextLines:      cfg.ContextLines,
		MaxMatchesPerFile: cfg.MaxMatchesPerFile,
		GroupByFile:       cfg.GroupByFile,
	}
}

// Crush compresses search/grep results.
// Query is used for relevance filtering (empty = keep all matches).
func (sc *SearchCompressor) Crush(content string, query string) (string, CompressionResult) {
	result := CompressionResult{
		OriginalContent: content,
		Strategy:        StrategySearch,
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= 20 {
		// Short results: passthrough
		result.CompressedContent = content
		result.OriginalTokens = countTokens(content)
		result.CompressedTokens = result.OriginalTokens
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = []string{"passthrough:short_search"}
		return result.CompressedContent, result
	}

	// Parse grep-style output (filename:line:content)
	fileResults := sc.parseGrepOutput(lines)

	// Compress results
	compressedLines := sc.compressSearchResults(fileResults, query)
	compressed := strings.Join(compressedLines, "\n")

	// Calculate metrics
	result.CompressedContent = compressed
	result.OriginalTokens = countTokens(content)
	result.CompressedTokens = countTokens(compressed)
	result.TokensSaved = max(0, result.OriginalTokens-result.CompressedTokens)
	result.CompressionRatio = float64(result.CompressedTokens) / float64(max(1, result.OriginalTokens))
	result.TransformsApplied = []string{"search_compression"}

	// Injection guard
	if result.CompressedTokens > result.OriginalTokens {
		result.CompressedContent = content
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = append(result.TransformsApplied, "inflation_guard:reverted")
	}

	return result.CompressedContent, result
}

// grepLinePattern matches grep output: filename:lineNum:content
var grepLinePattern = regexp.MustCompile(`^([^:]+):(\d+):(.*)$`)

// fileResult holds parsed results for a single file.
type fileResult struct {
	filename string
	matches  []matchLine
}

// matchLine represents a matching line.
type matchLine struct {
	lineNum int
	content string
}

// parseGrepOutput parses grep-style output into structured results.
func (sc *SearchCompressor) parseGrepOutput(lines []string) []fileResult {
	fileMap := make(map[string]*fileResult)
	var fileOrder []string

	for _, line := range lines {
		matches := grepLinePattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		filename := matches[1]
		lineNum := 0
		for _, c := range matches[2] {
			if c >= '0' && c <= '9' {
				lineNum = lineNum*10 + int(c-'0')
			}
		}
		content := matches[3]

		fr, ok := fileMap[filename]
		if !ok {
			fr = &fileResult{filename: filename}
			fileMap[filename] = fr
			fileOrder = append(fileOrder, filename)
		}

		fr.matches = append(fr.matches, matchLine{
			lineNum: lineNum,
			content: content,
		})
	}

	// Convert map to slice
	results := make([]fileResult, 0, len(fileMap))
	for _, filename := range fileOrder {
		results = append(results, *fileMap[filename])
	}

	return results
}

// compressSearchResults compresses search results.
func (sc *SearchCompressor) compressSearchResults(results []fileResult, query string) []string {
	var output []string

	output = append(output, "=== Search Results Summary ===")
	output = append(output, fmt.Sprintf("Files searched: %d", len(results)))

	totalMatches := 0
	for _, fr := range results {
		totalMatches += len(fr.matches)
	}
	output = append(output, fmt.Sprintf("Total matches: %d", totalMatches))
	output = append(output, "")

	// Compress per file
	for _, fr := range results {
		if sc.GroupByFile {
			output = append(output, fmt.Sprintf("--- %s (%d matches) ---", fr.filename, len(fr.matches)))
		}

		matches := fr.matches
		if sc.MaxMatchesPerFile > 0 && len(matches) > sc.MaxMatchesPerFile {
			// Keep first and last matches
			keepFirst := sc.MaxMatchesPerFile / 2
			keepLast := sc.MaxMatchesPerFile - keepFirst

			output = append(output, formatMatches(matches[:keepFirst])...)
			if len(matches) > sc.MaxMatchesPerFile {
				output = append(output, fmt.Sprintf("  ... %d more matches ...", len(matches)-sc.MaxMatchesPerFile))
			}
			output = append(output, formatMatches(matches[len(matches)-keepLast:])...)
		} else {
			output = append(output, formatMatches(matches)...)
		}

		output = append(output, "")
	}

	return output
}

// formatMatches formats match lines with line numbers.
func formatMatches(matches []matchLine) []string {
	output := make([]string, 0, len(matches))
	for _, m := range matches {
		output = append(output, fmt.Sprintf("  %5d: %s", m.lineNum, truncate(m.content, 120)))
	}
	return output
}
