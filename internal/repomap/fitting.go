// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// RenderingProvider is an interface for rendering tags to text and counting tokens.
// This abstracts away the actual rendering implementation.
type RenderingProvider interface {
	Render(ranked RankedTags) RenderedMap
}

// DefaultRenderer is a simple default implementation for token estimation
// when the full ContextRenderer is not available.
type DefaultRenderer struct{}

// Render renders the ranked tags into a tree format and counts tokens.
func (r *DefaultRenderer) Render(ranked RankedTags) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{Content: "", Tokens: 0}
	}

	var lines []string

	// Group by file
	byFile := groupByFileRanked(ranked)

	for file, tags := range byFile {
		lines = append(lines, fmt.Sprintf("%s:", file))

		// Sort tags in this file by score (descending) and line number
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Score == tags[j].Score {
				return tags[i].Line < tags[j].Line
			}
			return tags[i].Score > tags[j].Score
		})

		// Output up to 20 symbols per file to keep output manageable
		maxTagsPerFile := 20
		if len(tags) > maxTagsPerFile {
			tags = tags[:maxTagsPerFile]
		}

		for _, tag := range tags {
			kindIndicator := getKindIndicator(tag.Kind)
			lines = append(lines, fmt.Sprintf("    %s %s (line %d)", kindIndicator, tag.Name, tag.Line+1))
		}
	}

	content := strings.Join(lines, "\n")
	return RenderedMap{
		Content: content,
		Tokens:  EstimateTokens(content),
	}
}

// getKindIndicator returns a short indicator for the symbol kind.
func getKindIndicator(kind string) string {
	switch kind {
	case "function":
		return "fn"
	case "method":
		return "fn"
	case "class":
		return "type"
	case "struct":
		return "type"
	case "type":
		return "type"
	case "constant":
		return "const"
	case "variable":
		return "var"
	case "property":
		return "field"
	case "interface":
		return "type"
	default:
		return "sym"
	}
}

// groupByFileRanked groups ranked tags by their file path.
func groupByFileRanked(ranked RankedTags) map[string]RankedTags {
	result := make(map[string]RankedTags)
	for _, rt := range ranked {
		result[rt.RelFname] = append(result[rt.RelFname], rt)
	}
	return result
}

// FittingConfig holds token budget parameters for fitting ranked tags to a budget.
type FittingConfig struct {
	// MaxMapTokens is the target token count for the rendered map.
	// Default: 1024
	MaxMapTokens int
	// Tolerance is the acceptable deviation from MaxMapTokens as a fraction (0.0-1.0).
	// Default: 0.15 (15%)
	Tolerance float64
	// MapMulNoFiles is a multiplier applied to estimate initial token count
	// when no files are in the chat (to show more context).
	// Default: 8.0
	MapMulNoFiles float64
	// MinTags is the minimum number of tags to include even if over budget.
	// Default: 5
	MinTags int
	// MaxTags is the maximum number of tags to consider.
	// Default: 500
	MaxTags int
}

// DefaultFittingConfig returns a FittingConfig with default values.
func DefaultFittingConfig() FittingConfig {
	return FittingConfig{
		MaxMapTokens:  1024,
		Tolerance:     0.15,
		MapMulNoFiles: 8.0,
		MinTags:       5,
		MaxTags:       500,
	}
}

// RenderedMap is the final output injected into LLM context.
type RenderedMap struct {
	Content string
	Tokens  int
}

// FitToBudget uses binary search to find the optimal subset of ranked tags
// that fits within the token budget. It tries to get as close as possible to
// the target token count while staying under or at the budget (within tolerance).
func FitToBudget(ranked RankedTags, config FittingConfig, renderer RenderingProvider) RenderedMap {
	// Apply defaults
	if config.MaxMapTokens == 0 {
		config.MaxMapTokens = 1024
	}
	if config.Tolerance == 0 {
		config.Tolerance = 0.15
	}
	if config.MinTags == 0 {
		config.MinTags = 5
	}
	if config.MaxTags == 0 {
		config.MaxTags = 500
	}

	if len(ranked) == 0 {
		return RenderedMap{}
	}

	// Clamp to valid range
	numTags := len(ranked)
	if numTags > config.MaxTags {
		numTags = config.MaxTags
	}

	// Use binary search to find optimal count
	// We search in the range [0, numTags] to find the best fit
	low, high := 0, numTags
	bestMap := RenderedMap{}
	bestDiff := math.MaxFloat64

	// Pre-allocate to avoid allocations in the loop
	var candidate RankedTags

	for low <= high {
		mid := (low + high) / 2

		// Create candidate subset (top 'mid' tags)
		if mid == 0 {
			candidate = nil
		} else if mid >= len(ranked) {
			candidate = ranked
		} else {
			candidate = ranked[:mid]
		}

		// Render the candidate
		rendered := renderer.Render(candidate)
		tokens := rendered.Tokens

		// Calculate deviation
		diff := math.Abs(float64(tokens) - float64(config.MaxMapTokens))
		pctErr := diff / float64(config.MaxMapTokens)

		// If within tolerance and better than current best, save it
		if pctErr <= config.Tolerance {
			if diff < bestDiff {
				bestMap = rendered
				bestDiff = diff
			}
		}

		// Adjust search range based on token count
		if tokens < config.MaxMapTokens {
			// Under budget - try to fit more
			low = mid + 1
		} else {
			// Over budget - need fewer
			high = mid - 1
		}
	}

	// If we didn't find a solution within tolerance, return the closest under-budget
	// or the smallest over-budget result
	if bestMap.Tokens == 0 {
		bestMap = findClosestFit(ranked, config, renderer)
	}

	// Ensure minimum tags if we have any but went below minimum
	if len(bestMap.Content) > 0 && bestMap.Tokens > 0 {
		actualTags := countTagsInRendered(bestMap.Content)
		if actualTags < config.MinTags && len(ranked) >= config.MinTags {
			// Re-render with minimum tags
			minCandidate := ranked[:config.MinTags]
			if config.MinTags > len(ranked) {
				minCandidate = ranked
			}
			bestMap = renderer.Render(minCandidate)
		}
	}

	return bestMap
}

// findClosestFit finds the closest fit to the target when no exact match is found.
// It prefers under-budget results and returns the one closest to the target.
func findClosestFit(ranked RankedTags, config FittingConfig, renderer RenderingProvider) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{}
	}

	// Try a few specific sizes: 25%, 50%, 75%, 100%
	percentages := []int{25, 50, 75, 100}
	var best RenderedMap
	bestDiff := math.MaxFloat64

	for _, pct := range percentages {
		count := len(ranked) * pct / 100
		if count < config.MinTags {
			count = config.MinTags
		}
		if count > len(ranked) {
			count = len(ranked)
		}
		if count == 0 {
			count = 1
		}

		candidate := ranked[:count]
		rendered := renderer.Render(candidate)
		tokens := rendered.Tokens

		diff := math.Abs(float64(tokens) - float64(config.MaxMapTokens))

		// Prefer under-budget results (allow up to 50% over budget for final fallback)
		if tokens <= config.MaxMapTokens+config.MaxMapTokens/2 {
			if diff < bestDiff {
				best = rendered
				bestDiff = diff
			}
		}
	}

	// If still no result, just return the full set
	if best.Tokens == 0 {
		best = renderer.Render(ranked)
	}

	return best
}

// countTagsInRendered estimates the number of tags in rendered content.
func countTagsInRendered(content string) int {
	// Count lines that look like tag definitions (contain "(line " or indicators)
	lines := strings.Split(content, "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "fn ") ||
			strings.HasPrefix(trimmed, "type ") ||
			strings.HasPrefix(trimmed, "var ") ||
			strings.HasPrefix(trimmed, "const ") ||
			strings.HasPrefix(trimmed, "field ") ||
			strings.HasPrefix(trimmed, "sym ") {
			count++
		}
	}
	return count
}

// EstimateTokens estimates the number of tokens in a string.
// Uses a simple approximation: ~4 characters per token for code.
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	// Average token is about 4 characters for code
	// Add 20% overhead for special tokens
	estimate := len(text) / 4
	overhead := estimate / 5
	return estimate + overhead
}

// FitToBudgetSimple provides a simpler interface that doesn't require a renderer.
// It uses token estimation based on average tokens per tag.
func FitToBudgetSimple(ranked RankedTags, config FittingConfig) RenderedMap {
	// Apply defaults
	if config.MaxMapTokens == 0 {
		config.MaxMapTokens = 1024
	}
	if config.MinTags == 0 {
		config.MinTags = 5
	}
	if config.MaxTags == 0 {
		config.MaxTags = 500
	}

	if len(ranked) == 0 {
		return RenderedMap{}
	}

	// Estimate average tokens per tag (including file header and formatting)
	// This is ~20 tokens per tag based on typical output format
	avgTokensPerTag := 20

	// Calculate max tags based on budget
	maxTags := config.MaxMapTokens / avgTokensPerTag
	if maxTags < config.MinTags {
		maxTags = config.MinTags
	}
	if maxTags > len(ranked) {
		maxTags = len(ranked)
	}
	if maxTags > config.MaxTags {
		maxTags = config.MaxTags
	}

	// Use top tags up to maxTags
	selected := ranked
	if len(ranked) > maxTags {
		selected = ranked[:maxTags]
	}

	// Estimate tokens
	estimatedTokens := len(selected) * avgTokensPerTag

	return RenderedMap{
		Content: fmt.Sprintf("(estimated %d tags, ~%d tokens)", len(selected), estimatedTokens),
		Tokens:  estimatedTokens,
	}
}

// AdjustBudgetForContext adjusts the token budget based on conversation context.
// When the user hasn't specified any files yet, we can show more context.
func AdjustBudgetForConfig(config FittingConfig, chatFiles []string, mentionedIdentifiers []string) FittingConfig {
	result := config

	// If no chat files and few identifiers mentioned, use MapMulNoFiles multiplier
	// to show more context (the user is exploring the repo)
	if len(chatFiles) == 0 && len(mentionedIdentifiers) < 3 {
		result.MaxMapTokens = int(float64(config.MaxMapTokens) * config.MapMulNoFiles)
	}

	// If many files mentioned, reduce budget per file
	if len(chatFiles) > 5 {
		result.MaxMapTokens = result.MaxMapTokens * 3 / 4
	}

	// Ensure minimum budget
	if result.MaxMapTokens < 256 {
		result.MaxMapTokens = 256
	}

	return result
}

// TokenBudgetForFiles calculates an appropriate token budget when specific files are involved.
func TokenBudgetForFiles(numFiles int, baseBudget int) int {
	// Base case: single file
	if numFiles <= 1 {
		return baseBudget
	}

	// Scale down as more files are involved
	// We want to ensure each file gets reasonable representation
	minPerFile := 100
	maxPerFile := baseBudget / 2

	perFileBudget := baseBudget / numFiles
	if perFileBudget < minPerFile {
		perFileBudget = minPerFile
	}
	if perFileBudget > maxPerFile {
		perFileBudget = maxPerFile
	}

	return numFiles * perFileBudget
}

// ValidateConfig validates the fitting configuration.
func ValidateConfig(config FittingConfig) error {
	if config.MaxMapTokens <= 0 {
		return fmt.Errorf("MaxMapTokens must be positive, got %d", config.MaxMapTokens)
	}
	if config.Tolerance < 0 || config.Tolerance > 1.0 {
		return fmt.Errorf("Tolerance must be between 0 and 1, got %f", config.Tolerance)
	}
	if config.MapMulNoFiles <= 0 {
		return fmt.Errorf("MapMulNoFiles must be positive, got %f", config.MapMulNoFiles)
	}
	if config.MinTags < 0 {
		return fmt.Errorf("MinTags must be non-negative, got %d", config.MinTags)
	}
	if config.MaxTags < config.MinTags {
		return fmt.Errorf("MaxTags (%d) must be >= MinTags (%d)", config.MaxTags, config.MinTags)
	}
	return nil
}

// FitToBudgetWithStrategy allows choosing different fitting strategies.
type FitStrategy int

const (
	// FitStrategyBinarySearch uses binary search for optimal fit.
	FitStrategyBinarySearch FitStrategy = iota
	// FitStrategyProportional allocates budget proportionally.
	FitStrategyProportional
	// FitStrategyTopN takes top N tags regardless of budget.
	FitStrategyTopN
)

// FitToBudgetWithStrategy fits ranked tags to budget using the specified strategy.
func FitToBudgetWithStrategy(ranked RankedTags, config FittingConfig, renderer RenderingProvider, strategy FitStrategy) RenderedMap {
	switch strategy {
	case FitStrategyBinarySearch:
		return FitToBudget(ranked, config, renderer)
	case FitStrategyProportional:
		return fitProportional(ranked, config, renderer)
	case FitStrategyTopN:
		return fitTopN(ranked, config, renderer)
	default:
		return FitToBudget(ranked, config, renderer)
	}
}

// fitProportional allocates budget proportionally to the score distribution.
func fitProportional(ranked RankedTags, config FittingConfig, renderer RenderingProvider) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{}
	}

	// Calculate cumulative scores
	var totalScore float64
	for _, rt := range ranked {
		totalScore += rt.Score
	}

	if totalScore == 0 {
		// All scores are 0, fall back to top N
		return fitTopN(ranked, config, renderer)
	}

	// Find how many tags we can include while staying under budget
	// by iteratively adding tags until we hit the budget
	var selected RankedTags
	runningScore := 0.0

	for _, rt := range ranked {
		selected = append(selected, rt)
		runningScore += rt.Score

		// Estimate tokens for current selection
		rendered := renderer.Render(selected)

		// If we've included enough (based on score proportion)
		if runningScore/totalScore >= 0.95 {
			break
		}
		if rendered.Tokens > config.MaxMapTokens*2 {
			// Too many, remove last addition
			selected = selected[:len(selected)-1]
			break
		}
	}

	if len(selected) == 0 {
		selected = ranked[:config.MinTags]
	}

	return renderer.Render(selected)
}

// fitTopN selects the top N tags, ignoring budget.
func fitTopN(ranked RankedTags, config FittingConfig, renderer RenderingProvider) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{}
	}

	count := config.MinTags
	if count > len(ranked) {
		count = len(ranked)
	}

	selected := ranked[:count]
	return renderer.Render(selected)
}
