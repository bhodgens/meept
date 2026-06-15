// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/code/ast"
)

// ContextRenderer renders code structure with surrounding context for LLM consumption.
type ContextRenderer struct {
	maxLineLength  int    // Default: 100
	maxTagsPerFile int    // Default: 20
	contextLines   int    // Lines of context around each symbol
	treeCache      map[string]string
	mu             sync.RWMutex // protects treeCache
	logger         *slog.Logger
	parser         *ast.ParserManager
}

// RendererConfig holds rendering configuration options.
type RendererConfig struct {
	// MaxLineLength is the maximum length of a line before truncation.
	// Default: 100
	MaxLineLength int
	// ContextLines is the number of lines of context to show around symbols.
	// Default: 2
	ContextLines int
	// EnableTreeView enables the hierarchical tree view format.
	// Default: true
	EnableTreeView bool
	// ShowScore shows the PageRank score next to each symbol.
	// Default: false
	ShowScore bool
	// MaxTagsPerFile limits symbols shown per file.
	// Default: 20
	MaxTagsPerFile int
}

// DefaultRendererConfig returns a RendererConfig with default values.
func DefaultRendererConfig() RendererConfig {
	return RendererConfig{
		MaxLineLength:  100,
		ContextLines:   2,
		EnableTreeView: true,
		ShowScore:      false,
		MaxTagsPerFile: 20,
	}
}

// NewContextRenderer creates a new ContextRenderer with the given configuration.
func NewContextRenderer(config RendererConfig, logger *slog.Logger) *ContextRenderer {
	// Apply defaults
	if config.MaxLineLength == 0 {
		config.MaxLineLength = 100
	}
	if config.ContextLines == 0 {
		config.ContextLines = 2
	}
	if config.MaxTagsPerFile == 0 {
		config.MaxTagsPerFile = 20
	}

	return &ContextRenderer{
		maxLineLength:  config.MaxLineLength,
		maxTagsPerFile: config.MaxTagsPerFile,
		contextLines:   config.ContextLines,
		treeCache:      make(map[string]string),
		logger:         logger,
		parser:         ast.NewParserManager(ast.DefaultParserConfig()),
	}
}

// Render creates the tree view for ranked tags.
// This is the main entry point that converts ranked tags into rendered output.
func (r *ContextRenderer) Render(ranked RankedTags) RenderedMap {
	return r.renderWithContextLines(ranked, r.contextLines)
}

// renderWithContextLines is the shared renderer that uses the provided
// contextLines override instead of mutating r.contextLines.
func (r *ContextRenderer) renderWithContextLines(ranked RankedTags, contextLines int) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{Content: "", Tokens: 0}
	}

	// Group by file
	byFile := groupByFileRankedTags(ranked)

	// Sort files by the highest score of any tag in them
	sortedFiles := r.sortFilesByRelevance(byFile)

	var lines []string

	for _, file := range sortedFiles {
		tags := byFile[file]
		if len(tags) == 0 {
			continue
		}

		// Render file header
		lines = append(lines, fmt.Sprintf("%s:", file))

		// Limit tags per file
		if len(tags) > r.maxTagsPerFile {
			tags = tags[:r.maxTagsPerFile]
		}

		// Render each symbol with context
		for _, tag := range tags {
			symbolLine := r.renderSymbolWithContext(tag, contextLines)
			lines = append(lines, symbolLine)
		}
	}

	content := strings.Join(lines, "\n")
	return RenderedMap{
		Content: content,
		Tokens:  EstimateTokens(content),
	}
}

// sortFilesByRelevance returns file paths sorted by the highest score of any tag in them.
func (r *ContextRenderer) sortFilesByRelevance(byFile map[string]RankedTags) []string {
	type fileScore struct {
		file  string
		score float64
	}

	var scores []fileScore
	for file, tags := range byFile {
		maxScore := 0.0
		for _, tag := range tags {
			if tag.Score > maxScore {
				maxScore = tag.Score
			}
		}
		scores = append(scores, fileScore{file: file, score: maxScore})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	result := make([]string, len(scores))
	for i, fs := range scores {
		result[i] = fs.file
	}
	return result
}

// renderSymbol renders a single symbol with its context.
func (r *ContextRenderer) renderSymbol(tag RankedTag) string {
	return r.renderSymbolWithContext(tag, r.contextLines)
}

// renderSymbolWithContext renders a single symbol using the provided context
// lines override instead of reading r.contextLines.
func (r *ContextRenderer) renderSymbolWithContext(tag RankedTag, contextLines int) string {
	kindIndicator := getKindIndicator(tag.Kind)

	// Get source context around this symbol if available
	sourceContext := r.getSourceContextWithContext(tag, contextLines)

	if sourceContext != "" {
		// Format: "    fn symbol_name (line N) { context }"
		truncated := truncateLine(sourceContext, r.maxLineLength)
		return fmt.Sprintf("    %s %s (line %d) { %s }", kindIndicator, tag.Name, tag.Line+1, truncated)
	}

	// Fallback: just show the symbol without context
	return fmt.Sprintf("    %s %s (line %d)", kindIndicator, tag.Name, tag.Line+1)
}

// getSourceContext reads the source file and extracts context around the symbol.
func (r *ContextRenderer) getSourceContext(tag RankedTag) string {
	return r.getSourceContextWithContext(tag, r.contextLines)
}

// getSourceContextWithContext reads the source file and extracts context around
// the symbol using the provided context lines override.
func (r *ContextRenderer) getSourceContextWithContext(tag RankedTag, contextLines int) string {
	// Use FName if available, otherwise we can't read the file
	if tag.FName == "" {
		return ""
	}

	// Check cache first
	cacheKey := fmt.Sprintf("%s:%d", tag.FName, tag.Line)
	r.mu.RLock()
	if cached, ok := r.treeCache[cacheKey]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	// Read the source file
	source, err := os.ReadFile(tag.FName)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(source), "\n")
	if tag.Line < 0 || tag.Line >= len(lines) {
		return ""
	}

	// Get context lines around the symbol
	startLine := tag.Line - contextLines
	endLine := tag.Line + contextLines + 1

	if startLine < 0 {
		startLine = 0
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	// Extract the relevant lines
	var extracted []string
	for i := startLine; i < endLine; i++ {
		line := lines[i]
		// Indicate which line has the definition
		if i == tag.Line {
			extracted = append(extracted, "> "+line)
		} else {
			extracted = append(extracted, "  "+line)
		}
	}

	result := strings.Join(extracted, "\n")

	// Cache the result
	r.mu.Lock()
	if r.treeCache != nil {
		r.treeCache[cacheKey] = result
	}
	r.mu.Unlock()

	return result
}

// groupByFileRankedTags groups ranked tags by their file path.
func groupByFileRankedTags(ranked RankedTags) map[string]RankedTags {
	result := make(map[string]RankedTags)
	for _, rt := range ranked {
		result[rt.RelFname] = append(result[rt.RelFname], rt)
	}

	// Sort tags within each file by score (descending) then line number
	for _, tags := range result {
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Score == tags[j].Score {
				return tags[i].Line < tags[j].Line
			}
			return tags[i].Score > tags[j].Score
		})
	}

	return result
}

// truncateLine truncates a line to the specified maximum length.
func truncateLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}

	// Try to truncate at a natural boundary (comma, space)
	truncated := line[:maxLen]
	lastComma := strings.LastIndex(truncated, ",")
	lastSpace := strings.LastIndex(truncated, " ")

	cutoff := lastComma
	if lastSpace > lastComma {
		cutoff = lastSpace
	}

	if cutoff > maxLen/2 {
		return strings.TrimSpace(line[:cutoff]) + "..."
	}

	return strings.TrimSpace(truncated) + "..."
}

// RenderCompact renders ranked tags in a more compact format (less context).
func (r *ContextRenderer) RenderCompact(ranked RankedTags) RenderedMap {
	return r.renderWithContextLines(ranked, 0)
}

// RenderWithFullPath renders ranked tags using full file paths.
func (r *ContextRenderer) RenderWithFullPath(ranked RankedTags) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{Content: "", Tokens: 0}
	}

	byFile := make(map[string]RankedTags)
	for _, rt := range ranked {
		absPath := rt.FName
		if absPath == "" {
			absPath = rt.RelFname
		}
		byFile[absPath] = append(byFile[absPath], rt)
	}

	// Sort files by relevance
	sortedFiles := r.sortFilesByRelevance(byFile)

	var lines []string
	for _, file := range sortedFiles {
		tags := byFile[file]
		if len(tags) == 0 {
			continue
		}

		lines = append(lines, fmt.Sprintf("%s:", file))

		// Show as absolute path in the output
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

// RenderHierarchical renders ranked tags in a hierarchical structure by directory.
func (r *ContextRenderer) RenderHierarchical(ranked RankedTags) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{Content: "", Tokens: 0}
	}

	// Group by directory first
	byDir := make(map[string]map[string]RankedTags) // dir -> file -> tags
	for _, rt := range ranked {
		dir := filepath.Dir(rt.RelFname)
		if dir == "." {
			dir = ""
		}
		if byDir[dir] == nil {
			byDir[dir] = make(map[string]RankedTags)
		}
		byDir[dir][rt.RelFname] = append(byDir[dir][rt.RelFname], rt)
	}

	// Sort directories
	var dirs []string
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var lines []string
	for _, dir := range dirs {
		files := byDir[dir]

		if dir != "" {
			lines = append(lines, fmt.Sprintf("%s/", dir))
		}

		// Sort files within directory
		var fileList []string
		for f := range files {
			fileList = append(fileList, f)
		}
		sort.Strings(fileList)

		for _, file := range fileList {
			tags := files[file]
			prefix := "  "
			if dir == "" {
				prefix = ""
			}
			lines = append(lines, fmt.Sprintf("%s%s:", prefix, file))

			for _, tag := range tags {
				kindIndicator := getKindIndicator(tag.Kind)
				lines = append(lines, fmt.Sprintf("%s    %s %s (line %d)", prefix, kindIndicator, tag.Name, tag.Line+1))
			}
		}
	}

	content := strings.Join(lines, "\n")
	return RenderedMap{
		Content: content,
		Tokens:  EstimateTokens(content),
	}
}

// RenderJSON renders ranked tags as JSON (useful for debugging).
func (r *ContextRenderer) RenderJSON(ranked RankedTags) (string, int, error) {
	if len(ranked) == 0 {
		return "[]", 0, nil
	}

	type jsonTag struct {
		Name     string  `json:"name"`
		Kind     string  `json:"kind"`
		File     string  `json:"file"`
		Line     int     `json:"line"`
		Def      bool    `json:"is_definition"`
		Score    float64 `json:"score"`
	}

	var tags []jsonTag
	for _, rt := range ranked {
		tags = append(tags, jsonTag{
			Name:  rt.Name,
			Kind:  rt.Kind,
			File:  rt.RelFname,
			Line:  rt.Line + 1, // Convert to 1-based
			Def:   rt.IsDef,
			Score: rt.Score,
		})
	}

	// Simple JSON serialization (to avoid additional dependencies)
	var sb strings.Builder
	sb.WriteString("[\n")
	for i, tag := range tags {
		sb.WriteString(fmt.Sprintf(`  {"name": "%s", "kind": "%s", "file": "%s", "line": %d, "is_definition": %v, "score": %.4f}`,
			escapeJSON(tag.Name), escapeJSON(tag.Kind), escapeJSON(tag.File), tag.Line, tag.Def, tag.Score))
		if i < len(tags)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("]")

	content := sb.String()
	return content, EstimateTokens(content), nil
}

// escapeJSON escapes special characters in JSON strings.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// ClearCache clears the rendering cache.
func (r *ContextRenderer) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.treeCache != nil {
		r.treeCache = make(map[string]string)
	}
}

// CacheSize returns the number of entries in the cache.
func (r *ContextRenderer) CacheSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.treeCache == nil {
		return 0
	}
	return len(r.treeCache)
}

// RenderSummarized provides a very brief summary of the codebase structure.
func (r *ContextRenderer) RenderSummarized(ranked RankedTags, maxFiles int) RenderedMap {
	if len(ranked) == 0 {
		return RenderedMap{Content: "", Tokens: 0}
	}

	byFile := groupByFileRankedTags(ranked)

	// Sort files by relevance and take top maxFiles
	sortedFiles := r.sortFilesByRelevance(byFile)
	if len(sortedFiles) > maxFiles {
		sortedFiles = sortedFiles[:maxFiles]
	}

	var lines []string
	lines = append(lines, "# Repository Map Summary")

	totalTags := 0
	for _, file := range sortedFiles {
		tags := byFile[file]
		totalTags += len(tags)

		// Get unique kinds
		kinds := make(map[string]bool)
		for _, tag := range tags {
			kinds[getKindIndicator(tag.Kind)] = true
		}

		var kindStrs []string
		for k := range kinds {
			kindStrs = append(kindStrs, k)
		}

		lines = append(lines, fmt.Sprintf("- %s: %d symbols (%s)", file, len(tags), strings.Join(kindStrs, ", ")))
	}

	if len(ranked) > totalTags {
		lines = append(lines, fmt.Sprintf("- ... and %d more symbols in %d more files", len(ranked)-totalTags, len(byFile)-len(sortedFiles)))
	}

	content := strings.Join(lines, "\n")
	return RenderedMap{
		Content: content,
		Tokens:  EstimateTokens(content),
	}
}

// Ensure ContextRenderer implements RenderingProvider interface from fitting.go
var _ RenderingProvider = (*ContextRenderer)(nil)