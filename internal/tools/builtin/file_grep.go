package builtin

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// FileGrepTool searches for regex patterns in files.
type FileGrepTool struct {
	checker *security.PermissionChecker
}

// NewFileGrepTool creates a new file grep tool.
func NewFileGrepTool(checker *security.PermissionChecker) *FileGrepTool {
	return &FileGrepTool{checker: checker}
}

func (t *FileGrepTool) Name() string { return "file_grep" }

func (t *FileGrepTool) Description() string {
	return "Search for a regex pattern in files. Supports content output with line numbers, file listing, and per-file count modes. Skips binary files and .git directories."
}

func (t *FileGrepTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"pattern": {
				Type:        schemaTypeString,
				Description: "Regular expression pattern to search for.",
			},
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Search root directory (default: current working directory).",
			},
			"glob": {
				Type:        schemaTypeString,
				Description: "File name glob filter (e.g., \"*.go\"). Only search files matching this pattern.",
			},
			"output_mode": {
				Type:        schemaTypeString,
				Description: "Output format: \"content\" (with lines), \"files_with_matches\", or \"count\" (default: \"content\").",
			},
			"context": {
				Type:        schemaTypeInteger,
				Description: "Number of context lines before and after each match (default: 2).",
			},
			"max_results": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of results to return (default: 50).",
			},
		},
		Required: []string{"pattern"},
	}
}

// GrepResult is the result of a file grep operation.
type GrepResult struct {
	Query     string      `json:"query"`
	Path      string      `json:"path"`
	Mode      string      `json:"mode"`
	Output    string      `json:"output,omitempty"`
	Files     []string    `json:"files,omitempty"`
	Counts    []GrepCount `json:"counts,omitempty"`
	Matches   int         `json:"matches"`
	Truncated bool        `json:"truncated"`
}

// GrepCount is a per-file match count for count mode.
type GrepCount struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

// grepMatch represents a single matched line.
type grepMatch struct {
	file    string
	lineNum int
	line    string
}

func (t *FileGrepTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Resolve search root
	rawPath, _ := args[schemaPropPath].(string)
	if rawPath == "" {
		rawPath = "."
	}

	resolved, err := resolvePath(rawPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// Permission check
	if t.checker != nil && !t.checker.CheckPath(resolved) {
		return nil, fmt.Errorf("access denied: %s", resolved)
	}

	info, err := os.Stat(resolved)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("directory not found: %s", resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot stat path: %w", err)
	}

	// Allow searching a single file too
	if !info.IsDir() {
		return t.searchSingleFile(resolved, re, args)
	}

	// Parse parameters
	outputMode, _ := args["output_mode"].(string)
	if outputMode == "" {
		outputMode = "content"
	}
	if outputMode != "content" && outputMode != "files_with_matches" && outputMode != "count" {
		return nil, fmt.Errorf("invalid output_mode: %q (must be \"content\", \"files_with_matches\", or \"count\")", outputMode)
	}

	globFilter, _ := args["glob"].(string)

	contextLines := 2
	if ctxVal, ok := args["context"].(float64); ok && ctxVal >= 0 {
		contextLines = int(ctxVal)
	}

	maxResults := 50
	if maxVal, ok := args["max_results"].(float64); ok && maxVal > 0 {
		maxResults = int(maxVal)
	}

	// Collect matches
	var matches []grepMatch
	var fileCounts []GrepCount
	fileSet := make(map[string]bool)
	truncated := false
	totalMatches := 0

	err = filepath.WalkDir(resolved, func(walkPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip .git and common ignored directories
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}

		// Only process files
		if d.IsDir() {
			return nil
		}

		// Apply glob filter
		if globFilter != "" {
			matched, matchErr := filepath.Match(globFilter, d.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}

		// Check file size
		fInfo, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if fInfo.Size() > MaxReadSize {
			return nil
		}

		// Read file and check for binary
		content, readErr := os.ReadFile(walkPath)
		if readErr != nil {
			return nil
		}
		if isBinary(content) {
			return nil
		}

		text := string(content)
		lines := strings.Split(text, "\n")
		// Remove trailing empty line from split
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		fileMatchCount := 0
		for i, line := range lines {
			if re.MatchString(line) {
				fileMatchCount++
				totalMatches++

				if outputMode == "content" {
					if len(matches) < maxResults {
						matches = append(matches, grepMatch{
							file:    walkPath,
							lineNum: i + 1,
							line:    line,
						})
					} else {
						truncated = true
					}
				}
			}
		}

		if fileMatchCount > 0 {
			fileSet[walkPath] = true

			if outputMode == "count" {
				fileCounts = append(fileCounts, GrepCount{
					File:  walkPath,
					Count: fileMatchCount,
				})
			}

			// Check truncation for non-content modes
			if outputMode != "content" && len(fileSet) >= maxResults {
				truncated = true
				return filepath.SkipAll
			}
		}

		return nil
	})
	if err != nil {
		slog.Debug("file_grep: walk error", "error", err)
	}

	result := GrepResult{
		Query:     pattern,
		Path:      resolved,
		Mode:      outputMode,
		Matches:   totalMatches,
		Truncated: truncated,
	}

	// Format output based on mode
	switch outputMode {
	case "content":
		result.Output = formatGrepContent(matches, contextLines)

	case "files_with_matches":
		files := make([]string, 0, len(fileSet))
		for f := range fileSet {
			files = append(files, f)
		}
		result.Files = files

	case "count":
		result.Counts = fileCounts
	}

	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("pattern=%s,mode=%s,matches=%d,truncated=%v", pattern, outputMode, totalMatches, truncated),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

// searchSingleFile handles the case where the path points to a single file.
func (t *FileGrepTool) searchSingleFile(resolved string, re *regexp.Regexp, args map[string]any) (any, error) {
	outputMode, _ := args["output_mode"].(string)
	if outputMode == "" {
		outputMode = "content"
	}

	contextLines := 2
	if ctxVal, ok := args["context"].(float64); ok && ctxVal >= 0 {
		contextLines = int(ctxVal)
	}

	maxResults := 50
	if maxVal, ok := args["max_results"].(float64); ok && maxVal > 0 {
		maxResults = int(maxVal)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if isBinary(content) {
		return nil, fmt.Errorf("cannot grep binary file: %s", resolved)
	}

	text := string(content)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var matches []grepMatch
	totalMatches := 0

	for i, line := range lines {
		if re.MatchString(line) {
			totalMatches++
			if outputMode == "content" && len(matches) < maxResults {
				matches = append(matches, grepMatch{
					file:    resolved,
					lineNum: i + 1,
					line:    line,
				})
			}
		}
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	result := GrepResult{
		Query:   pattern,
		Path:    resolved,
		Mode:    outputMode,
		Matches: totalMatches,
	}

	switch outputMode {
	case "content":
		result.Output = formatGrepContent(matches, contextLines)
	case "files_with_matches":
		if totalMatches > 0 {
			result.Files = []string{resolved}
		}
	case "count":
		if totalMatches > 0 {
			result.Counts = []GrepCount{{File: resolved, Count: totalMatches}}
		}
	}

	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("pattern=%s,mode=%s,matches=%d", pattern, outputMode, totalMatches),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

// formatGrepContent formats matches as text with hashline tags and context.
// It loads file contents from disk to provide context lines.
func formatGrepContent(matches []grepMatch, contextLines int) string {
	if len(matches) == 0 {
		return ""
	}

	// Group matches by file and load file contents for context
	type fileData struct {
		lines   []string
		matches []grepMatch
	}
	files := make(map[string]*fileData)
	for _, m := range matches {
		if _, ok := files[m.file]; !ok {
			content, err := os.ReadFile(m.file)
			if err != nil {
				continue
			}
			text := string(content)
			lines := strings.Split(text, "\n")
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			files[m.file] = &fileData{lines: lines}
		}
		files[m.file].matches = append(files[m.file].matches, m)
	}

	var sb strings.Builder
	firstFile := true

	for _, fd := range files {
		if !firstFile {
			sb.WriteString("---\n")
		}
		firstFile = false

		// Collect all line numbers to show (matches + context)
		showLines := make(map[int]bool)
		for _, m := range fd.matches {
			for ctx := -contextLines; ctx <= contextLines; ctx++ {
				ln := m.lineNum + ctx
				if ln >= 1 && ln <= len(fd.lines) {
					showLines[ln] = true
				}
			}
		}

		// Build sorted list of line numbers to show
		sortedLines := make([]int, 0, len(showLines))
		for ln := range showLines {
			sortedLines = append(sortedLines, ln)
		}
		// Simple sort
		for i := 0; i < len(sortedLines); i++ {
			for j := i + 1; j < len(sortedLines); j++ {
				if sortedLines[j] < sortedLines[i] {
					sortedLines[i], sortedLines[j] = sortedLines[j], sortedLines[i]
				}
			}
		}

		for _, ln := range sortedLines {
			lineContent := fd.lines[ln-1]
			sb.WriteString(FormatHashLine(ln, lineContent))
			sb.WriteByte('\n')
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// isBinary checks if file content appears to be binary by looking for null bytes
// in the first 8KB.
func isBinary(data []byte) bool {
	checkSize := len(data)
	if checkSize > 8192 {
		checkSize = 8192
	}
	return bytes.Contains(data[:checkSize], []byte{0})
}

// Ensure FileGrepTool implements the Tool interface.
var _ tools.Tool = (*FileGrepTool)(nil)
