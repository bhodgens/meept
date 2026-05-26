package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// FileEditTool performs incremental file edits using hashline-anchored line references.
type FileEditTool struct {
	checker     *security.PermissionChecker
	readCache   *ReadCache
	lspNotifier LSPWriteNotifier
}

// NewFileEditTool creates a new file edit tool.
func NewFileEditTool(checker *security.PermissionChecker, readCache *ReadCache) *FileEditTool {
	return &FileEditTool{checker: checker, readCache: readCache}
}

// SetLSPNotifier sets the LSP write notifier for post-write notifications.
// This is called after tool registration when LSP is available.
func (t *FileEditTool) SetLSPNotifier(notifier LSPWriteNotifier) {
	if notifier != nil {
		t.lspNotifier = notifier
	}
}

func (t *FileEditTool) Name() string { return "file_edit" }

func (t *FileEditTool) Description() string {
	return "Edit a file using hashline anchors. Each line from file_read is tagged as LINE:HASH|content. Reference those tags to replace, insert, or delete lines. All anchors must match the current file content."
}

func (t *FileEditTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropPath: {
				Type:        schemaTypeString,
				Description: "Absolute or ~-prefixed path to the file to edit.",
			},
			"edits": {
				Type:        schemaTypeArray,
				Description: `Array of edit operations. Each edit has: "op" (replace|insert_after|insert_before|delete), "anchor" (LINE:HASH, BOF, or EOF), optional "end_anchor" (for replace/delete ranges), and optional "content" (for replace/insert ops).`,
			},
		},
		Required: []string{schemaPropPath, "edits"},
	}
}

// editOp represents a single parsed edit operation.
type editOp struct {
	Op        string // "replace", "insert_after", "insert_before", "delete"
	Anchor    string // "LINE:HASH", "BOF", "EOF"
	EndAnchor string // for range ops
	Content   string // new content (empty for delete)
}

func (t *FileEditTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawPath, _ := args[schemaPropPath].(string)
	if rawPath == "" {
		return nil, fmt.Errorf("no path specified")
	}

	editsRaw, ok := args["edits"].([]any)
	if !ok || len(editsRaw) == 0 {
		return nil, fmt.Errorf("no edits specified")
	}

	resolved, err := resolvePath(rawPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if t.checker != nil && !t.checker.CheckPath(resolved) {
		return nil, fmt.Errorf("access denied: %s", resolved)
	}

	// Read current file
	content, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	// Handle trailing newline: last element after split on trailing newline is ""
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Parse edit operations
	var ops []editOp
	for i, raw := range editsRaw {
		editMap, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit %d: expected object", i+1)
		}

		op := editOp{}
		op.Op, _ = editMap["op"].(string)
		op.Anchor, _ = editMap["anchor"].(string)
		op.EndAnchor, _ = editMap["end_anchor"].(string)
		op.Content, _ = editMap["content"].(string)

		if op.Op == "" {
			return nil, fmt.Errorf("edit %d: missing 'op' field", i+1)
		}
		if op.Anchor == "" {
			return nil, fmt.Errorf("edit %d: missing 'anchor' field", i+1)
		}

		switch op.Op {
		case "replace", "insert_after", "insert_before", "delete":
			// valid
		default:
			return nil, fmt.Errorf("edit %d: invalid op %q (must be replace, insert_after, insert_before, or delete)", i+1, op.Op)
		}

		ops = append(ops, op)
	}

	// Validate all anchors before applying any changes
	var mismatches []string
	for i, op := range ops {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, hash, err := ParseAnchor(op.Anchor)
		if err != nil {
			return nil, fmt.Errorf("edit %d: %w", i+1, err)
		}
		if !ValidateAnchor(lines, lineNum, hash) {
			actualContent := "(beyond file)"
			if lineNum >= 1 && lineNum <= len(lines) {
				actualContent = lines[lineNum-1]
			}
			mismatches = append(mismatches, FormatHashLine(lineNum, actualContent))
		}

		// Validate end_anchor for range ops
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, endHash, err := ParseAnchor(op.EndAnchor)
			if err != nil {
				return nil, fmt.Errorf("edit %d end_anchor: %w", i+1, err)
			}
			if !ValidateAnchor(lines, endLineNum, endHash) {
				actualContent := "(beyond file)"
				if endLineNum >= 1 && endLineNum <= len(lines) {
					actualContent = lines[endLineNum-1]
				}
				mismatches = append(mismatches, FormatHashLine(endLineNum, actualContent))
			}
		}
	}

	if len(mismatches) > 0 {
		// Attempt 3-way merge recovery from read cache
		if t.readCache != nil {
			cachedLines := t.readCache.Get(resolved)
			if cachedLines != nil {
				recovered, recoverErr := t.attemptRecovery(cachedLines, lines, ops)
				if recoverErr == nil {
					// Recovery succeeded -- write the recovered content
					result := strings.Join(recovered, "\n")
					if err := os.WriteFile(resolved, []byte(result), 0o644); err != nil {
						return nil, fmt.Errorf("recovery succeeded but write failed: %w", err)
					}
					msg := fmt.Sprintf("Edit applied with stale-anchor recovery to %s (%d lines)", resolved, len(recovered))
					if t.lspNotifier != nil {
						if lspResult := t.lspNotifier.NotifyWrite(ctx, resolved, result); lspResult != nil {
							msg += lspResult.String()
						}
					}
					return tools.ToolResult{
						Success: true,
						Result:  msg,
					}, nil
				}
			}
		}

		// Return error with fresh hashline content
		freshContent := FormatHashLines(lines, 1)
		return tools.ToolResult{
			Success: false,
			Error: fmt.Sprintf("Edit rejected: %d anchor(s) do not match the current file.\n\n%s\n\nThe edit was NOT applied. Use the updated content below and re-issue:\n%s",
				len(mismatches),
				strings.Join(mismatches, "\n"),
				freshContent,
			),
		}, nil
	}

	// Apply boundary absorption to handle the model including duplicate context lines.
	ops = absorbBoundaries(lines, ops)

	// Apply edits using the shared helper.
	result := t.applyEdits(lines, ops)

	// Write result
	output := strings.Join(result, "\n")
	if err := os.WriteFile(resolved, []byte(output), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Compute evidence and summary (shared across both branches)
	h := sha256.Sum256([]byte(output))
	hash := hex.EncodeToString(h[:])

	evidence := []models.Evidence{
		models.NewEvidence(models.EvidenceFileExists, resolved, fmt.Sprintf("size=%d", len(output)), t.Name()),
		models.NewEvidence(models.EvidenceFileHash, resolved, hash, t.Name()),
	}

	summary := fmt.Sprintf("Applied %d edit(s) to %s (%d lines -> %d lines)", len(ops), resolved, len(lines), len(result))

	// Append LSP writethrough suffix if available
	if t.lspNotifier != nil {
		if lspResult := t.lspNotifier.NotifyWrite(ctx, resolved, output); lspResult != nil {
			if suffix := lspResult.String(); suffix != "" {
				summary += suffix
			}
		}
	}

	return tools.ToolResult{
		Success:  true,
		Result:   summary,
		Evidence: evidence,
	}, nil
}

// absorbBoundaries detects when the model includes context lines in its replacement
// content that already exist at the boundaries of the edit range, and shrinks the
// content to avoid duplicating those lines. Only applies to "replace" ops.
func absorbBoundaries(fileLines []string, ops []editOp) []editOp {
	result := make([]editOp, len(ops))
	for i, op := range ops {
		result[i] = op

		if op.Op != "replace" || op.Content == "" {
			continue
		}

		// Parse anchor to get the start line number.
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		startLine, _, _ := ParseAnchor(op.Anchor)
		if startLine < 1 {
			continue
		}

		endLine := startLine
		if op.EndAnchor != "" {
			endLine, _, _ = ParseAnchor(op.EndAnchor)
		}

		content := strings.Split(op.Content, "\n")
		// Handle trailing newline
		if len(content) > 1 && content[len(content)-1] == "" {
			content = content[:len(content)-1]
		}

		// Check leading boundary: if the first line of content matches the line
		// just before the anchor, skip it from the content and expand the anchor
		// upward to include that line in the replacement range.
		if len(content) > 0 && startLine > 1 {
			beforeIdx := startLine - 2 // 0-based index of line before anchor
			if beforeIdx >= 0 && beforeIdx < len(fileLines) && content[0] == fileLines[beforeIdx] {
				content = content[1:]
				startLine--
				// Rebuild anchor with new line number and hash.
				newHash := ComputeLineHash(fileLines[beforeIdx])
				result[i].Anchor = fmt.Sprintf("%d:%s", startLine, newHash)
			}
		}

		// Check trailing boundary: if the last line of content matches the line
		// just after the end_anchor, skip it from the content and expand the
		// end_anchor downward.
		if len(content) > 0 {
			afterIdx := endLine // 0-based index of line after end_anchor
			if afterIdx < len(fileLines) && content[len(content)-1] == fileLines[afterIdx] {
				content = content[:len(content)-1]
				endLine++
				newHash := ComputeLineHash(fileLines[afterIdx])
				result[i].EndAnchor = fmt.Sprintf("%d:%s", endLine, newHash)
			}
		}

		result[i].Content = strings.Join(content, "\n")
	}

	return result
}

// attemptRecovery tries to remap stale anchors from the cached snapshot onto the
// current file content. For each anchor, it looks up the cached line at that line
// number, then scans the current file for a matching line within a ±5 line window.
// If all anchors are found, the ops are rewritten with the new line numbers and
// applied against the current file.
func (t *FileEditTool) attemptRecovery(cachedLines, currentLines []string, ops []editOp) ([]string, error) {
	// remap maps old (cached) line numbers to new (current) line numbers.
	remap := make(map[int]int)

	for _, op := range ops {
		anchors := []string{op.Anchor}
		if op.Op == "replace" || op.Op == "delete" {
			if op.EndAnchor != "" {
				anchors = append(anchors, op.EndAnchor)
			}
		}

		for _, anchor := range anchors {
			if anchor == "BOF" || anchor == "EOF" {
				continue
			}
			lineNum, hash, err := ParseAnchor(anchor)
			if err != nil {
				return nil, fmt.Errorf("recovery: invalid anchor %q: %w", anchor, err)
			}

			// Already remapped?
			if _, done := remap[lineNum]; done {
				continue
			}

			// Validate anchor against cached snapshot.
			if !ValidateAnchor(cachedLines, lineNum, hash) {
				return nil, fmt.Errorf("recovery: anchor %q does not match cached snapshot", anchor)
			}

			// Get the cached line content.
			cachedIdx := lineNum - 1
			if cachedIdx < 0 || cachedIdx >= len(cachedLines) {
				return nil, fmt.Errorf("recovery: cached line %d out of range", lineNum)
			}
			cachedContent := cachedLines[cachedIdx]

			// Search current file for the same content within ±5 lines of the original position.
			found := false
			searchStart := max(lineNum-1-5, 0) // 0-based
			searchEnd := min(lineNum-1+5, len(currentLines)-1)
			for i := searchStart; i <= searchEnd; i++ {
				if currentLines[i] == cachedContent {
					remap[lineNum] = i + 1 // 1-based
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("recovery: could not relocate cached line %d in current file", lineNum)
			}
		}
	}

	// Build remapped ops by adjusting anchor line numbers.
	remappedOps := make([]editOp, len(ops))
	for i, op := range ops {
		remappedOps[i] = op
		remappedOps[i].Anchor = remapAnchor(op.Anchor, remap)
		if op.EndAnchor != "" {
			remappedOps[i].EndAnchor = remapAnchor(op.EndAnchor, remap)
		}
	}

	// Validate remapped anchors against current file.
	for _, op := range remappedOps {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, hash, _ := ParseAnchor(op.Anchor)
		if !ValidateAnchor(currentLines, lineNum, hash) {
			return nil, fmt.Errorf("recovery: remapped anchor %q does not match current file", op.Anchor)
		}
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, endHash, _ := ParseAnchor(op.EndAnchor)
			if !ValidateAnchor(currentLines, endLineNum, endHash) {
				return nil, fmt.Errorf("recovery: remapped end_anchor %q does not match current file", op.EndAnchor)
			}
		}
	}

	// Apply the remapped edits against current file content.
	result := t.applyEdits(currentLines, remappedOps)
	return result, nil
}

// remapAnchor adjusts an anchor string using the remap table.
// BOF and EOF anchors are returned unchanged.
func remapAnchor(anchor string, remap map[int]int) string {
	if anchor == "BOF" || anchor == "EOF" {
		return anchor
	}
	lineNum, hash, err := ParseAnchor(anchor)
	if err != nil {
		return anchor
	}
	if newLine, ok := remap[lineNum]; ok {
		return fmt.Sprintf("%d:%s", newLine, hash)
	}
	return anchor
}

// applyEdits applies the given edit operations against the provided lines and
// returns the resulting lines. This is extracted from Execute so that recovery
// can reuse the same logic.
func (t *FileEditTool) applyEdits(lines []string, ops []editOp) []string {
	type lineOp struct {
		opType    string // "delete_range", "insert"
		startLine int    // 1-based
		endLine   int    // 1-based, inclusive
		content   []string
	}

	var lineOps []lineOp
	for _, op := range ops {
		switch op.Op {
		case "delete":
			startLine, _, _ := ParseAnchor(op.Anchor)
			endLine := startLine
			if op.EndAnchor != "" {
				endLine, _, _ = ParseAnchor(op.EndAnchor)
			}
			lineOps = append(lineOps, lineOp{
				opType:    "delete_range",
				startLine: startLine,
				endLine:   endLine,
			})

		case "replace":
			startLine, _, _ := ParseAnchor(op.Anchor)
			endLine := startLine
			if op.EndAnchor != "" {
				endLine, _, _ = ParseAnchor(op.EndAnchor)
			}
			content := strings.Split(op.Content, "\n")
			if len(content) > 1 && content[len(content)-1] == "" {
				content = content[:len(content)-1]
			}
			lineOps = append(lineOps, lineOp{
				opType:    "delete_range",
				startLine: startLine,
				endLine:   endLine,
			})
			lineOps = append(lineOps, lineOp{
				opType:    "insert",
				startLine: startLine,
				content:   content,
			})

		case "insert_before":
			var insertLine int
			if op.Anchor == "BOF" {
				insertLine = 1
			} else if op.Anchor == "EOF" {
				insertLine = len(lines) + 1
			} else {
				insertLine, _, _ = ParseAnchor(op.Anchor)
			}
			content := strings.Split(op.Content, "\n")
			if len(content) > 1 && content[len(content)-1] == "" {
				content = content[:len(content)-1]
			}
			lineOps = append(lineOps, lineOp{
				opType:    "insert",
				startLine: insertLine,
				content:   content,
			})

		case "insert_after":
			var insertLine int
			if op.Anchor == "BOF" {
				insertLine = 1
			} else if op.Anchor == "EOF" {
				insertLine = len(lines) + 1
			} else {
				insertLine, _, _ = ParseAnchor(op.Anchor)
				insertLine++
			}
			content := strings.Split(op.Content, "\n")
			if len(content) > 1 && content[len(content)-1] == "" {
				content = content[:len(content)-1]
			}
			lineOps = append(lineOps, lineOp{
				opType:    "insert",
				startLine: insertLine,
				content:   content,
			})
		}
	}

	// Sort: primary by descending startLine, secondary by delete before insert.
	sort.SliceStable(lineOps, func(i, j int) bool {
		if lineOps[i].startLine != lineOps[j].startLine {
			return lineOps[i].startLine > lineOps[j].startLine
		}
		return lineOps[i].opType == "delete_range" && lineOps[j].opType == "insert"
	})

	result := make([]string, len(lines))
	copy(result, lines)

	for _, op := range lineOps {
		switch op.opType {
		case "delete_range":
			start := op.startLine - 1
			end := op.endLine - 1
			if start < 0 {
				start = 0
			}
			if end >= len(result) {
				end = len(result) - 1
			}
			if start > end {
				continue
			}
			result = append(result[:start], result[end+1:]...)

		case "insert":
			idx := op.startLine - 1
			if idx < 0 {
				idx = 0
			}
			if idx > len(result) {
				idx = len(result)
			}
			insertSlice := make([]string, len(op.content))
			copy(insertSlice, op.content)
			result = append(result[:idx], append(insertSlice, result[idx:]...)...)
		}
	}

	return result
}

// Ensure FileEditTool implements the Tool interface.
var _ tools.Tool = (*FileEditTool)(nil)
