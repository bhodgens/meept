// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

const (
	// MaxReadSize is the maximum file size we will read (5 MB).
	MaxReadSize = 5 * 1024 * 1024
	// MaxWriteSize is the maximum file size we will write (10 MB).
	MaxWriteSize = 10 * 1024 * 1024
	// MaxListEntries is the maximum number of entries to return from list_directory.
	MaxListEntries = 500
)

// ReadFileTool reads the contents of a file.
type ReadFileTool struct {
	checker *security.PermissionChecker
}

// NewReadFileTool creates a new file read tool.
func NewReadFileTool(checker *security.PermissionChecker) *ReadFileTool {
	return &ReadFileTool{checker: checker}
}

func (t *ReadFileTool) Name() string { return "file_read" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path. Returns the text content. Optionally read a specific line range."
}

func (t *ReadFileTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        "string",
				Description: "Absolute or ~-prefixed path to the file.",
			},
			"offset": {
				Type:        "integer",
				Description: "Line number to start reading from (1-based, optional).",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of lines to read (optional).",
			},
		},
		Required: []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args["path"].(string)
	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
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
		return nil, fmt.Errorf("file not found: %s", resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", resolved)
	}
	if info.Size() > MaxReadSize {
		return nil, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), MaxReadSize)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	text := string(content)

	// Apply line range if requested
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	if offset > 0 || limit > 0 {
		lines := strings.Split(text, "\n")
		start := 0
		if offset > 0 {
			start = int(offset) - 1 // Convert to 0-based
			if start < 0 {
				start = 0
			}
			if start >= len(lines) {
				return "", nil // Empty result if offset beyond file
			}
		}
		end := len(lines)
		if limit > 0 {
			end = start + int(limit)
			if end > len(lines) {
				end = len(lines)
			}
		}
		text = strings.Join(lines[start:end], "\n")
	}

	return text, nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct {
	checker *security.PermissionChecker
}

// NewWriteFileTool creates a new file write tool.
func NewWriteFileTool(checker *security.PermissionChecker) *WriteFileTool {
	return &WriteFileTool{checker: checker}
}

func (t *WriteFileTool) Name() string { return "file_write" }

func (t *WriteFileTool) Description() string {
	return "Write text content to a file. Creates the file if it does not exist, overwrites if it does. Parent directories are created automatically."
}

func (t *WriteFileTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        "string",
				Description: "Absolute or ~-prefixed path to the file.",
			},
			"content": {
				Type:        "string",
				Description: "The text content to write.",
			},
			"append": {
				Type:        "boolean",
				Description: "If true, append instead of overwrite (default false).",
			},
		},
		Required: []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args["path"].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)

	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
	}

	if len(content) > MaxWriteSize {
		return nil, fmt.Errorf("content too large (max %d bytes)", MaxWriteSize)
	}

	resolved, err := resolvePath(rawPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// Permission check
	if t.checker != nil && !t.checker.CheckPath(resolved) {
		return nil, fmt.Errorf("access denied: %s", resolved)
	}

	// Create parent directories
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	var flag int
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	} else {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	f, err := os.OpenFile(resolved, flag, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	action := "wrote"
	if appendMode {
		action = "appended to"
	}

	return fmt.Sprintf("Successfully %s %s (%d bytes)", action, resolved, len(content)), nil
}

// DeleteFileTool deletes a file from the filesystem.
type DeleteFileTool struct {
	checker *security.PermissionChecker
}

// NewDeleteFileTool creates a new file delete tool.
func NewDeleteFileTool(checker *security.PermissionChecker) *DeleteFileTool {
	return &DeleteFileTool{checker: checker}
}

func (t *DeleteFileTool) Name() string { return "file_delete" }

func (t *DeleteFileTool) Description() string {
	return "Delete a file at the given path. This is a destructive operation and cannot be undone."
}

func (t *DeleteFileTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        "string",
				Description: "Absolute or ~-prefixed path to the file to delete.",
			},
		},
		Required: []string{"path"},
	}
}

func (t *DeleteFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args["path"].(string)
	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
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
		return nil, fmt.Errorf("file not found: %s", resolved)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", resolved)
	}

	if err := os.Remove(resolved); err != nil {
		return nil, fmt.Errorf("failed to delete file: %w", err)
	}

	return fmt.Sprintf("Successfully deleted %s", resolved), nil
}

// ListDirectoryTool lists the contents of a directory.
type ListDirectoryTool struct {
	checker *security.PermissionChecker
}

// NewListDirectoryTool creates a new list directory tool.
func NewListDirectoryTool(checker *security.PermissionChecker) *ListDirectoryTool {
	return &ListDirectoryTool{checker: checker}
}

func (t *ListDirectoryTool) Name() string { return "list_directory" }

func (t *ListDirectoryTool) Description() string {
	return "List files and directories at the given path. Returns names, types, and sizes."
}

func (t *ListDirectoryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        "string",
				Description: "Absolute or ~-prefixed path to the directory.",
			},
			"recursive": {
				Type:        "boolean",
				Description: "If true, list recursively (default false).",
			},
			"max_entries": {
				Type:        "integer",
				Description: "Maximum number of entries to return (default 200).",
			},
		},
		Required: []string{"path"},
	}
}

// DirEntry represents a single entry in a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size *int64 `json:"size,omitempty"`
}

// ListResult is the result of a directory listing.
type ListResult struct {
	Path      string     `json:"path"`
	Entries   []DirEntry `json:"entries"`
	Count     int        `json:"count"`
	Truncated bool       `json:"truncated"`
}

func (t *ListDirectoryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)
	maxEntries := 200
	if max, ok := args["max_entries"].(float64); ok && max > 0 {
		maxEntries = int(max)
		if maxEntries > MaxListEntries {
			maxEntries = MaxListEntries
		}
	}

	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
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
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", resolved)
	}

	var entries []DirEntry
	truncated := false

	if recursive {
		entries, truncated = t.listRecursive(resolved, maxEntries)
	} else {
		entries, truncated = t.listDirect(resolved, maxEntries)
	}

	return ListResult{
		Path:      resolved,
		Entries:   entries,
		Count:     len(entries),
		Truncated: truncated,
	}, nil
}

func (t *ListDirectoryTool) listDirect(dir string, max int) ([]DirEntry, bool) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}

	entries := make([]DirEntry, 0, len(dirEntries))
	truncated := false

	for _, de := range dirEntries {
		if len(entries) >= max {
			truncated = true
			break
		}

		entry := DirEntry{
			Name: de.Name(),
		}

		if de.IsDir() {
			entry.Type = "directory"
		} else {
			entry.Type = "file"
			if info, err := de.Info(); err == nil {
				size := info.Size()
				entry.Size = &size
			}
		}

		entries = append(entries, entry)
	}

	return entries, truncated
}

func (t *ListDirectoryTool) listRecursive(root string, max int) ([]DirEntry, bool) {
	entries := make([]DirEntry, 0)
	truncated := false

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors (permission denied, etc.)
		}

		// Skip the root directory itself
		if path == root {
			return nil
		}

		if len(entries) >= max {
			truncated = true
			return filepath.SkipAll
		}

		relPath, _ := filepath.Rel(root, path)

		entry := DirEntry{
			Name: relPath,
		}

		if d.IsDir() {
			entry.Type = "directory"
		} else {
			entry.Type = "file"
			if info, err := d.Info(); err == nil {
				size := info.Size()
				entry.Size = &size
			}
		}

		entries = append(entries, entry)
		return nil
	})

	if err != nil {
		return entries, truncated
	}

	return entries, truncated
}

// resolvePath expands ~ and resolves to absolute path.
func resolvePath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve absolute path: %w", err)
	}

	return filepath.Clean(absPath), nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*ReadFileTool)(nil)
	_ tools.Tool = (*WriteFileTool)(nil)
	_ tools.Tool = (*DeleteFileTool)(nil)
	_ tools.Tool = (*ListDirectoryTool)(nil)
)
