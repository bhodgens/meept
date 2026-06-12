package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// FileFindTool searches for files and directories matching a glob pattern.
type FileFindTool struct {
	checker      *security.PermissionChecker
	fenceChecker FenceChecker
}

// NewFileFindTool creates a new file find tool.
func NewFileFindTool(checker *security.PermissionChecker) *FileFindTool {
	return &FileFindTool{checker: checker}
}

// SetFenceChecker sets the fence boundary checker for path validation.
func (t *FileFindTool) SetFenceChecker(fc FenceChecker) {
	t.fenceChecker = fc
}

func (t *FileFindTool) Name() string { return "file_find" }

func (t *FileFindTool) Category() string { return "filesystem" }

func (t *FileFindTool) Description() string {
	return "Search for files and directories matching a glob pattern. Walks the directory tree recursively. Skips .git directories."
}

func (t *FileFindTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"pattern": {
				Type:        schemaTypeString,
				Description: "Glob pattern to match (e.g., \"**/*.go\", \"*.txt\").",
			},
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Search root directory (default: current working directory).",
			},
			schemaPropType: {
				Type:        schemaTypeString,
				Description: "Filter by type: \"file\", \"dir\", or \"any\" (default: \"file\").",
			},
			"max_results": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of results to return (default: 100).",
			},
		},
		Required: []string{"pattern"},
	}
}

// FindResult is the result of a file find operation.
type FindResult struct {
	Path      string      `json:"path"`
	Results   []FindEntry `json:"results"`
	Count     int         `json:"count"`
	Truncated bool        `json:"truncated"`
}

// FindEntry represents a single matched file or directory.
type FindEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (t *FileFindTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
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

	// Fence check
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckPath(resolved, "read"); err != nil {
			return nil, err
		}
	}

	info, err := os.Stat(resolved)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("directory not found: %s", resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", resolved)
	}

	// Parse type filter
	typeFilter, _ := args[schemaPropType].(string)
	if typeFilter == "" {
		typeFilter = "file"
	}
	if typeFilter != "file" && typeFilter != "dir" && typeFilter != "any" {
		return nil, fmt.Errorf("invalid type filter: %q (must be \"file\", \"dir\", or \"any\")", typeFilter)
	}

	// Parse max_results
	maxResults := 100
	if maxVal, ok := args["max_results"].(float64); ok && maxVal > 0 {
		maxResults = int(maxVal)
	}

	// Determine if pattern uses ** (recursive matching on full relative path)
	usesDoubleStar := strings.Contains(pattern, "**")

	var results []FindEntry
	truncated := false

	err = filepath.WalkDir(resolved, func(walkPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip the root itself
		if walkPath == resolved {
			return nil
		}

		// Skip .git and other common ignored directories
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}

		// Type filter
		if typeFilter == "file" && d.IsDir() {
			return nil
		}
		if typeFilter == "dir" && !d.IsDir() {
			return nil
		}

		// Match against pattern
		matched := false
		if usesDoubleStar {
			// For ** patterns, match against the full relative path
			relPath, relErr := filepath.Rel(resolved, walkPath)
			if relErr == nil {
				matched, _ = doubleStarMatch(pattern, relPath)
			}
		} else {
			// Simple glob match on base name
			matched, _ = filepath.Match(pattern, d.Name())
		}

		if !matched {
			return nil
		}

		// Check max_results cap
		if len(results) >= maxResults {
			truncated = true
			return filepath.SkipAll
		}

		entryType := "file"
		if d.IsDir() {
			entryType = "dir"
		}

		results = append(results, FindEntry{
			Path: walkPath,
			Type: entryType,
		})
		return nil
	})
	if err != nil {
		// Non-fatal; return what we have
		slog.Debug("file_find: walk error", "error", err)
	}

	result := FindResult{
		Path:      resolved,
		Results:   results,
		Count:     len(results),
		Truncated: truncated,
	}

	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("pattern=%s,matches=%d,truncated=%v", pattern, len(results), truncated),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

// shouldSkipDir returns true for directories that should be skipped during walks.
func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", ".tox", "__pycache__":
		return true
	}
	return false
}

// doubleStarMatch matches a glob pattern containing ** against a relative path.
// It converts the ** pattern into a form that filepath.Match can handle by
// expanding ** into a series of path-matching attempts.
func doubleStarMatch(pattern, relPath string) (bool, error) {
	// Normalize separators
	relPath = filepath.ToSlash(relPath)
	pattern = filepath.ToSlash(pattern)

	// Split pattern into segments separated by /**
	parts := strings.SplitN(pattern, "**", 2)
	if len(parts) != 2 {
		// No ** present, fall back to simple match
		return filepath.Match(pattern, filepath.Base(relPath))
	}

	prefix := parts[0]
	suffix := strings.TrimPrefix(parts[1], "/")

	// If pattern starts with **, any prefix is acceptable
	if prefix == "" {
		// Match suffix against all trailing segments
		pathParts := strings.Split(relPath, "/")
		for i := range pathParts {
			subPath := strings.Join(pathParts[i:], "/")
			if m, _ := filepath.Match(suffix, subPath); m {
				return true, nil
			}
			// Also try matching suffix against just the last component
			if m, _ := filepath.Match(suffix, filepath.Base(subPath)); m {
				return true, nil
			}
		}
		// Also match suffix as a base name if suffix has no /
		if !strings.Contains(suffix, "/") {
			matched, err := filepath.Match(suffix, filepath.Base(relPath))
			return matched, err
		}
		return false, nil
	}

	// Pattern has a prefix before **
	prefix = strings.TrimSuffix(prefix, "/")

	// Check if relPath starts with prefix
	if !strings.HasPrefix(relPath, prefix) {
		return false, nil
	}

	remaining := strings.TrimPrefix(relPath, prefix)
	remaining = strings.TrimPrefix(remaining, "/")

	if suffix == "" {
		return true, nil
	}

	// Match suffix against remaining path
	if m, _ := filepath.Match(suffix, remaining); m {
		return true, nil
	}

	// Try matching suffix against trailing segments
	pathParts := strings.Split(remaining, "/")
	for i := range pathParts {
		subPath := strings.Join(pathParts[i:], "/")
		if m, _ := filepath.Match(suffix, subPath); m {
			return true, nil
		}
	}

	return false, nil
}

// Ensure FileFindTool implements the Tool interface.
var _ tools.Tool = (*FileFindTool)(nil)
