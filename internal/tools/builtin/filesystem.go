// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	checker   *security.PermissionChecker
	readCache *ReadCache
}

// NewReadFileTool creates a new file read tool.
func NewReadFileTool(checker *security.PermissionChecker, readCache *ReadCache) *ReadFileTool {
	return &ReadFileTool{checker: checker, readCache: readCache}
}

func (t *ReadFileTool) Name() string { return "file_read" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file at the given path. Returns the text content. Optionally read a specific line range."
}

func (t *ReadFileTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Absolute or ~-prefixed path to the file.",
			},
			"offset": {
				Type:        schemaTypeInteger,
				Description: "Line number to start reading from (1-based, optional).",
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of lines to read (optional).",
			},
			"raw": {
				Type:        schemaTypeBoolean,
				Description: "If true, return content without hashline tags (default false). Use when you need the raw content, not for editing.",
			},
		},
		Required: []string{schemaPropPath},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.executeRead(args, nil)
}

// ExecuteStreaming implements tools.StreamingTool with progress updates during file reads.
func (t *ReadFileTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	return t.executeRead(args, onUpdate)
}

// executeRead is the shared core logic for Execute and ExecuteStreaming.
// progress may be nil; all progress calls are guarded.
func (t *ReadFileTool) executeRead(args map[string]any, progress func(tools.ProgressUpdate)) (any, error) {
	rawPath, _ := args[schemaPropPath].(string)
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

	if progress != nil {
		progress(tools.ProgressUpdate{
			Message: fmt.Sprintf("reading %s (%d bytes)...", resolved, info.Size()),
			Percent: 10,
		})
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	text := string(content)

	// Apply line range if requested
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	// Split into lines for range selection and context expansion.
	allFileLines := strings.Split(text, "\n")

	// Determine raw mode early so context expansion only applies to hashline output.
	raw, _ := args["raw"].(bool)

	if offset > 0 || limit > 0 {
		lines := allFileLines
		start := 0
		if offset > 0 {
			start = max(
				// Convert to 0-based
				int(offset)-1, 0)
			if start >= len(lines) {
				return "", nil // Empty result if offset beyond file
			}
		}
		end := len(lines)
		if limit > 0 {
			end = min(start+int(limit), len(lines))
		}

		if raw {
			// Raw mode: exact range, no context expansion.
			text = strings.Join(lines[start:end], "\n")
		} else {
			// Hashline mode: expand range with context lines for better editing anchors.
			// Add 1 leading context line when offset > 1.
			contextStart := start
			if offset > 1 && start > 0 {
				contextStart = start - 1
			}
			// Add up to 3 trailing context lines when limit is finite.
			contextEnd := end
			if limit > 0 {
				contextEnd = min(end+3, len(lines))
			}
			text = strings.Join(lines[contextStart:contextEnd], "\n")
		}
	}

	// Store full file snapshot in read cache for edit recovery
	if t.readCache != nil {
		snapshotLines := allFileLines
		if len(snapshotLines) > 0 && snapshotLines[len(snapshotLines)-1] == "" {
			snapshotLines = snapshotLines[:len(snapshotLines)-1]
		}
		t.readCache.Store(resolved, snapshotLines)
	}

	// Apply hashline formatting unless raw mode
	if !raw {
		var linesToFormat []string
		var startLineNum int
		if offset > 0 || limit > 0 {
			linesToFormat = strings.Split(text, "\n")
			// Determine the actual starting line number accounting for context expansion.
			contextStart := 0
			if offset > 0 {
				contextStart = max(int(offset)-1, 0)
				if offset > 1 && contextStart > 0 {
					contextStart-- // 1 leading context line
				}
			}
			startLineNum = contextStart + 1 // 1-based
		} else {
			linesToFormat = strings.Split(text, "\n")
			if len(linesToFormat) > 0 && linesToFormat[len(linesToFormat)-1] == "" {
				linesToFormat = linesToFormat[:len(linesToFormat)-1]
			}
			startLineNum = 1
		}
		text = FormatHashLines(linesToFormat, startLineNum)
	}

	// Compute evidence: file stat and hash
	evInfo, err := os.Stat(resolved)
	var evidence []models.Evidence
	if err == nil {
		evidence = append(evidence, models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("size=%d", evInfo.Size()),
			t.Name(),
		))
	}

	// Compute SHA256 hash of file content
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceFileHash,
		resolved,
		hash,
		t.Name(),
	))

	if progress != nil {
		partialJSON, _ := json.Marshal(map[string]any{"path": resolved, "size": len(content)})
		progress(tools.ProgressUpdate{Message: "read complete", Percent: 100, PartialResult: partialJSON})
	}

	return tools.ToolResult{
		Success:  true,
		Result:   text,
		Evidence: evidence,
	}, nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct {
	checker     *security.PermissionChecker
	lspNotifier LSPWriteNotifier
}

// NewWriteFileTool creates a new file write tool.
func NewWriteFileTool(checker *security.PermissionChecker) *WriteFileTool {
	return &WriteFileTool{checker: checker}
}

// SetLSPNotifier sets the LSP write notifier for post-write notifications.
// This is called after tool registration when LSP is available.
func (t *WriteFileTool) SetLSPNotifier(notifier LSPWriteNotifier) {
	if notifier != nil {
		t.lspNotifier = notifier
	}
}

func (t *WriteFileTool) Name() string { return "file_write" }

func (t *WriteFileTool) Description() string {
	return "Write text content to a file. Creates the file if it does not exist, overwrites if it does. Parent directories are created automatically."
}

func (t *WriteFileTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Absolute or ~-prefixed path to the file.",
			},
			schemaPropContent: {
				Type:        schemaTypeString,
				Description: "The text content to write.",
			},
			"append": {
				Type:        schemaTypeBoolean,
				Description: "If true, append instead of overwrite (default false).",
			},
		},
		Required: []string{schemaPropPath, schemaPropContent},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.executeWrite(ctx, args, nil)
}

// ExecuteStreaming implements tools.StreamingTool with progress updates during file writes.
func (t *WriteFileTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	return t.executeWrite(ctx, args, onUpdate)
}

// lspNotifyWrite is a helper that calls the LSP notifier and appends results to the summary.
func (t *WriteFileTool) lspNotifyWrite(ctx context.Context, resolved, content string) string {
	if t.lspNotifier == nil {
		return ""
	}
	result := t.lspNotifier.NotifyWrite(ctx, resolved, content)
	if result == nil {
		return ""
	}
	return result.String()
}

// executeWrite is the shared core logic for Execute and ExecuteStreaming.
// progress may be nil; all progress calls are guarded.
func (t *WriteFileTool) executeWrite(ctx context.Context, args map[string]any, progress func(tools.ProgressUpdate)) (any, error) {
	rawPath, _ := args[schemaPropPath].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)

	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
	}

	// Defensively strip any accidental hashline prefixes from content.
	content = stripHashlinePrefixes(content)

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

	if progress != nil {
		progress(tools.ProgressUpdate{
			Message: fmt.Sprintf("writing %s (%d bytes)...", resolved, len(content)),
			Percent: 10,
		})
	}

	// Create parent directories
	dir := filepath.Dir(resolved)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	var flag int
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	} else {
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	//nolint:gosec // user config directory/file permissions
	f, err := os.OpenFile(resolved, flag, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// LSP writethrough notification
	lspSuffix := t.lspNotifyWrite(ctx, resolved, content)

	// Compute evidence: file stat and hash
	info, err := os.Stat(resolved)
	if err != nil {
		slog.Warn("WriteFileTool: failed to stat file for evidence", "path", resolved, "error", err)
	}

	var hash string
	hashData, err := os.ReadFile(resolved)
	if err != nil {
		slog.Warn("WriteFileTool: failed to read file for hash", "path", resolved, "error", err)
	} else {
		h := sha256.Sum256(hashData)
		hash = hex.EncodeToString(h[:])
	}

	action := "wrote"
	if appendMode {
		action = "appended to"
	}

	// Build evidence list
	evidence := []models.Evidence{}
	if err == nil {
		evidence = append(evidence, models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("size=%d", info.Size()),
			t.Name(),
		))
	}
	if hash != "" {
		evidence = append(evidence, models.NewEvidence(
			models.EvidenceFileHash,
			resolved,
			hash,
			t.Name(),
		))
	}

	if progress != nil {
		partialJSON, _ := json.Marshal(map[string]any{"path": resolved, "bytes": len(content)})
		progress(tools.ProgressUpdate{Message: "write complete", Percent: 100, PartialResult: partialJSON})
	}

	msg := fmt.Sprintf("Successfully %s %s (%d bytes)", action, resolved, len(content))
	if lspSuffix != "" {
		msg += lspSuffix
	}

	return tools.ToolResult{
		Success:  true,
		Result:   msg,
		Evidence: evidence,
	}, nil
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
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Absolute or ~-prefixed path to the file to delete.",
			},
		},
		Required: []string{schemaPropPath},
	}
}

func (t *DeleteFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args[schemaPropPath].(string)
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

	// Store file info for evidence before deletion
	fileSize := info.Size()

	// Verify deletion
	_, verifyErr := os.Stat(resolved)
	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			"deleted",
			t.Name(),
		),
	}
	if verifyErr == nil {
		// File still exists - deletion may have failed
		slog.Warn("DeleteFileTool: file still exists after deletion", "path", resolved)
	}

	return tools.ToolResult{
		Success:  true,
		Result:   fmt.Sprintf("Successfully deleted %s (%d bytes)", resolved, fileSize),
		Evidence: evidence,
	}, nil
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
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Absolute or ~-prefixed path to the directory.",
			},
			"recursive": {
				Type:        schemaTypeBoolean,
				Description: "If true, list recursively (default false).",
			},
			"max_entries": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of entries to return (default 200).",
			},
		},
		Required: []string{schemaPropPath},
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
	rawPath, _ := args[schemaPropPath].(string)
	recursive, _ := args["recursive"].(bool)
	maxEntries := 200
	if maxVal, ok := args["max_entries"].(float64); ok && maxVal > 0 {
		maxEntries = min(int(maxVal), MaxListEntries)
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
	var truncated bool

	if recursive {
		entries, truncated = t.listRecursive(resolved, maxEntries)
	} else {
		var err error
		entries, truncated, err = t.listDirect(resolved, maxEntries)
		if err != nil {
			return ListResult{}, fmt.Errorf("failed to list directory: %w", err)
		}
	}

	result := ListResult{
		Path:      resolved,
		Entries:   entries,
		Count:     len(entries),
		Truncated: truncated,
	}

	// Build evidence: directory listing confirmation
	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			resolved,
			fmt.Sprintf("entries=%d,recursive=%v,truncated=%v", len(entries), recursive, truncated),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:  true,
		Result:   result,
		Evidence: evidence,
	}, nil
}

func (t *ListDirectoryTool) listDirect(dir string, maxEntries int) ([]DirEntry, bool, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}

	entries := make([]DirEntry, 0, len(dirEntries))
	truncated := false

	for _, de := range dirEntries {
		if len(entries) >= maxEntries {
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

	return entries, truncated, nil
}

func (t *ListDirectoryTool) listRecursive(root string, maxEntries int) ([]DirEntry, bool) {
	entries := make([]DirEntry, 0)
	truncated := false
	errorCount := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errorCount++
			slog.Debug("listRecursive: skipping entry", "path", path, "error", err)
			return nil // Skip errors (permission denied, etc.)
		}

		// Skip the root directory itself
		if path == root {
			return nil
		}

		if len(entries) >= maxEntries {
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
		slog.Warn("listRecursive: walk terminated with error", "root", root, "error", err, "skipped_entries", errorCount)
		return entries, truncated
	}
	if errorCount > 0 {
		slog.Warn("listRecursive: walk completed with skipped entries", "root", root, "skipped_entries", errorCount)
	}

	return entries, truncated
}

// hashlinePrefixRe matches hashline prefixes like "123:ab|" at the start of a line.
// Pattern: one or more digits, colon, two lowercase letters, pipe.
var hashlinePrefixRe = regexp.MustCompile(`(?m)^\d+:[a-z]{2}\|`)

// stripHashlinePrefixes removes any "LINE:HASH|" prefixes from content lines.
// This defensively handles the case where the model accidentally includes
// hashline tags in content meant to be written to a file.
func stripHashlinePrefixes(content string) string {
	return hashlinePrefixRe.ReplaceAllLiteralString(content, "")
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

	// Ensure streaming tools implement StreamingTool
	_ tools.StreamingTool = (*ReadFileTool)(nil)
	_ tools.StreamingTool = (*WriteFileTool)(nil)
)
