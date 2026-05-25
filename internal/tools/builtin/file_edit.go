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
	checker   *security.PermissionChecker
	readCache *ReadCache
}

// NewFileEditTool creates a new file edit tool.
func NewFileEditTool(checker *security.PermissionChecker, readCache *ReadCache) *FileEditTool {
	return &FileEditTool{checker: checker, readCache: readCache}
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
					return tools.ToolResult{
						Success: true,
						Result:  fmt.Sprintf("Edit applied with stale-anchor recovery to %s (%d lines)", resolved, len(recovered)),
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

	// Apply edits bottom-up (highest line first) to preserve indices.
	// Convert ops to line-level operations.
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
			// Handle trailing newline
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
				insertLine++ // after = next line
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

	// Sort: primary by descending startLine (bottom-up to preserve indices),
	// secondary by delete before insert at the same line (for replace: delete gap then fill).
	sort.SliceStable(lineOps, func(i, j int) bool {
		if lineOps[i].startLine != lineOps[j].startLine {
			return lineOps[i].startLine > lineOps[j].startLine
		}
		return lineOps[i].opType == "delete_range" && lineOps[j].opType == "insert"
	})

	// Apply in order
	result := make([]string, len(lines))
	copy(result, lines)

	for _, op := range lineOps {
		switch op.opType {
		case "delete_range":
			start := op.startLine - 1 // 0-based
			end := op.endLine - 1     // 0-based inclusive
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
			idx := op.startLine - 1 // 0-based insert position
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

	// Write result
	output := strings.Join(result, "\n")
	if err := os.WriteFile(resolved, []byte(output), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Compute evidence
	h := sha256.Sum256([]byte(output))
	hash := hex.EncodeToString(h[:])

	evidence := []models.Evidence{
		models.NewEvidence(models.EvidenceFileExists, resolved, fmt.Sprintf("size=%d", len(output)), t.Name()),
		models.NewEvidence(models.EvidenceFileHash, resolved, hash, t.Name()),
	}

	summary := fmt.Sprintf("Applied %d edit(s) to %s (%d lines -> %d lines)", len(ops), resolved, len(lines), len(result))

	return tools.ToolResult{
		Success:  true,
		Result:   summary,
		Evidence: evidence,
	}, nil
}

// attemptRecovery tries to apply edits against a cached snapshot and merge
// the result onto the current file content.
func (t *FileEditTool) attemptRecovery(cachedLines, currentLines []string, ops []editOp) ([]string, error) {
	// Validate all anchors against cached snapshot
	for _, op := range ops {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, hash, _ := ParseAnchor(op.Anchor)
		if !ValidateAnchor(cachedLines, lineNum, hash) {
			return nil, fmt.Errorf("anchor mismatch in cached snapshot")
		}
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, endHash, _ := ParseAnchor(op.EndAnchor)
			if !ValidateAnchor(cachedLines, endLineNum, endHash) {
				return nil, fmt.Errorf("end_anchor mismatch in cached snapshot")
			}
		}
	}

	// Full 3-way merge is complex; for now, reject if anchors are stale.
	return nil, fmt.Errorf("stale anchors, recovery not yet fully implemented")
}

// Ensure FileEditTool implements the Tool interface.
var _ tools.Tool = (*FileEditTool)(nil)
