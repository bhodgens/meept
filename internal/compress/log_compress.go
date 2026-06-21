package compress

import (
	"fmt"
	"strings"
)

// LogCompressor compresses log files by keeping errors and anomalies.
//
// Strategy:
// - Keep: ERROR, WARN, FATAL lines
// - Keep: First/last N lines of repetitive blocks
// - Compress: Repeated stack traces to summary
// - Preserve: Timestamps for context
//
// Typical savings: 70-90% on verbose logs.
type LogCompressor struct {
	// KeepErrorLevels keeps ERROR/WARN/FATAL lines
	KeepErrorLevels bool
	// MaxRepetitions is the max repeated lines to keep
	MaxRepetitions int
	// KeepFirstN lines to always keep
	KeepFirstN int
	// KeepLastN lines to always keep
	KeepLastN int
}

// LogCompressorConfig configures the LogCompressor.
type LogCompressorConfig struct {
	KeepErrorLevels  bool `json:"keep_error_levels" toml:"keep_error_levels"`
	MaxRepetitions   int  `json:"max_repetitions" toml:"max_repetitions"`
	KeepFirstN       int  `json:"keep_first_n" toml:"keep_first_n"`
	KeepLastN        int  `json:"keep_last_n" toml:"keep_last_n"`
}

// DefaultLogCompressorConfig returns default configuration.
func DefaultLogCompressorConfig() LogCompressorConfig {
	return LogCompressorConfig{
		KeepErrorLevels: true,
		MaxRepetitions:  3,
		KeepFirstN:      20,
		KeepLastN:       20,
	}
}

// NewLogCompressor creates a LogCompressor with the given config.
func NewLogCompressor(cfg LogCompressorConfig) *LogCompressor {
	return &LogCompressor{
		KeepErrorLevels: cfg.KeepErrorLevels,
		MaxRepetitions:  cfg.MaxRepetitions,
		KeepFirstN:      cfg.KeepFirstN,
		KeepLastN:       cfg.KeepLastN,
	}
}

// Crush compresses log content.
func (lc *LogCompressor) Crush(content string) (string, CompressionResult) {
	result := CompressionResult{
		OriginalContent: content,
		Strategy:        StrategyLog,
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= 50 {
		// Short logs: passthrough
		result.CompressedContent = content
		result.OriginalTokens = countTokens(content)
		result.CompressedTokens = result.OriginalTokens
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = []string{"passthrough:short_log"}
		return result.CompressedContent, result
	}

	// Compress the log
	compressedLines := lc.compressLogLines(lines)
	compressed := strings.Join(compressedLines, "\n")

	// Calculate metrics
	result.CompressedContent = compressed
	result.OriginalTokens = countTokens(content)
	result.CompressedTokens = countTokens(compressed)
	result.TokensSaved = max(0, result.OriginalTokens-result.CompressedTokens)
	result.CompressionRatio = float64(result.CompressedTokens) / float64(max(1, result.OriginalTokens))
	result.TransformsApplied = []string{"log_compression"}

	// Injection guard
	if result.CompressedTokens > result.OriginalTokens {
		result.CompressedContent = content
		result.TokensSaved = 0
		result.CompressionRatio = 1.0
		result.TransformsApplied = append(result.TransformsApplied, "inflation_guard:reverted")
	}

	return result.CompressedContent, result
}

// compressLogLines compresses log lines.
func (lc *LogCompressor) compressLogLines(lines []string) []string {
	result := make([]string, 0)

	// Keep first N lines
	keepFirst := lc.KeepFirstN
	if keepFirst > len(lines) {
		keepFirst = len(lines)
	}
	result = append(result, lines[:keepFirst]...)

	// Track repetitions
	type repetition struct {
		line  string
		count int
	}
	var reps []repetition
	currentRep := ""
	currentCount := 0

	middleStart := keepFirst
	middleEnd := len(lines) - lc.KeepLastN
	if middleEnd < middleStart {
		middleEnd = middleStart
	}

	// Process middle section for repetitions
	for i := middleStart; i < middleEnd; i++ {
		line := lines[i]

		// Check if this is an important line (error/warn)
		if lc.KeepErrorLevels && isErrorLine(line) {
			// Flush current repetition
			if currentCount > 1 {
				reps = append(reps, repetition{currentRep, currentCount})
			}
			reps = append(reps, repetition{line, 1})
			currentRep = ""
			currentCount = 0
			continue
		}

		// Check for repetition
		if line == currentRep {
			currentCount++
		} else {
			if currentCount > 1 {
				reps = append(reps, repetition{currentRep, currentCount})
			}
			currentRep = line
			currentCount = 1
		}
	}

	// Flush final repetition
	if currentCount > 0 {
		reps = append(reps, repetition{currentRep, currentCount})
	}

	// Build compressed output from repetitions
	for _, rep := range reps {
		if rep.count > lc.MaxRepetitions {
			result = append(result, fmt.Sprintf("// ... repeated %d times: %s", rep.count, truncate(rep.line, 80)))
		} else {
			for i := 0; i < rep.count; i++ {
				result = append(result, rep.line)
			}
		}
	}

	// Add summary for middle section
	if middleEnd > middleStart {
		omittedCount := middleEnd - middleStart
		if omittedCount > 0 {
			// Already handled by repetitions
		}
	}

	// Keep last N lines
	keepLast := lc.KeepLastN
	startIdx := len(lines) - keepLast
	if startIdx < keepFirst {
		startIdx = keepFirst
	}
	if startIdx < len(lines) {
		result = append(result, lines[startIdx:]...)
	}

	return result
}

// isErrorLine checks if a line is an error/warning.
func isErrorLine(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERROR") ||
		strings.Contains(upper, "FATAL") ||
		strings.Contains(upper, "WARN") ||
		strings.Contains(upper, "EXCEPTION") ||
		strings.Contains(upper, "FAILED")
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
