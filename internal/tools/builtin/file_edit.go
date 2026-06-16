package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// RecoveryConfig controls the behavior of stale-anchor recovery.
type RecoveryConfig struct {
	FuzzFactor     float64 // multiplier for fuzzy threshold (unused; reserved)
	RecoveryWindow int     // ±line search radius for relocation
	FuzzyThreshold float64 // minimum Levenshtein ratio for fuzzy matching
}

// DefaultRecoveryConfig returns the default recovery configuration.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		FuzzFactor:     0.6,
		RecoveryWindow: 10,
		FuzzyThreshold: 0.6,
	}
}

// BlockResolver resolves a syntactic block span from a file path and line number.
// This is satisfied by *ast.ParserManager.FindBlockSpan.
type BlockResolver interface {
	ResolveBlock(filePath string, lineNum int) (startLine int, endLine int, err error)
}

// FileEditTool performs incremental file edits using hashline-anchored line references.
type FileEditTool struct {
	checker                *security.PermissionChecker
	readCache              *ReadCache
	lspNotifier            LSPWriteNotifier
	blockResolver          BlockResolver
	pendingChangesRegistry *PendingChangesRegistry
	fenceChecker           FenceChecker
	secOrch                *intsecurity.Orchestrator
	recoveryConfig         RecoveryConfig
}

// NewFileEditTool creates a new file edit tool.
func NewFileEditTool(checker *security.PermissionChecker, readCache *ReadCache) *FileEditTool {
	return &FileEditTool{checker: checker, readCache: readCache, recoveryConfig: DefaultRecoveryConfig()}
}

// SetLSPNotifier sets the LSP write notifier for post-write notifications.
// This is called after tool registration when LSP is available.
func (t *FileEditTool) SetLSPNotifier(notifier LSPWriteNotifier) {
	if notifier != nil {
		t.lspNotifier = notifier
	}
}

// SetBlockResolver sets the block resolver for syntactic block operations.
func (t *FileEditTool) SetBlockResolver(resolver BlockResolver) {
	if resolver != nil {
		t.blockResolver = resolver
	}
}

// SetPendingChangesRegistry sets the pending changes registry for preview/accept workflow.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *FileEditTool) SetPendingChangesRegistry(registry *PendingChangesRegistry) {
	if registry != nil {
		t.pendingChangesRegistry = registry
	}
}

// SetRecoveryConfig sets the recovery configuration.
func (t *FileEditTool) SetRecoveryConfig(cfg RecoveryConfig) {
	t.recoveryConfig = cfg
}

// SetFenceChecker sets the fence checker for path-based sandboxing.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *FileEditTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

// SetSecurityOrchestrator sets the security orchestrator for content scanning.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *FileEditTool) SetSecurityOrchestrator(orch *intsecurity.Orchestrator) {
	if orch != nil {
		t.secOrch = orch
	}
}

func (t *FileEditTool) Name() string { return "file_edit" }

func (t *FileEditTool) Category() string { return "filesystem" }

func (t *FileEditTool) Description() string {
	return "Edit a file using hashline anchors. Each line from file_read is tagged as LINE:HASH|content. Reference those tags to replace, insert, or delete lines. All anchors must match the current file content. Note: consecutive blank lines produce identical hashes; use the line number to distinguish them."
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
	Op          string // "replace", "insert_after", "insert_before", "delete", "replace_block", "delete_block"
	Anchor      string // "LINE:HASH", "LINE:TAG:HASH", "BOF", "EOF"
	EndAnchor   string // for range ops
	Content     string // new content (empty for delete)
	SnapshotTag string // optional snapshot tag for cache lookup
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

	// Fence check
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckPath(resolved, "write"); err != nil {
			return nil, err
		}
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
		op.SnapshotTag, _ = editMap["tag"].(string)

		if op.Op == "" {
			return nil, fmt.Errorf("edit %d: missing 'op' field", i+1)
		}
		if op.Anchor == "" {
			return nil, fmt.Errorf("edit %d: missing 'anchor' field", i+1)
		}

		switch op.Op {
		case "replace", "insert_after", "insert_before", "delete", "replace_block", "delete_block":
			// valid
		default:
			return nil, fmt.Errorf("edit %d: invalid op %q (must be replace, replace_block, insert_after, insert_before, delete, or delete_block)", i+1, op.Op)
		}

		ops = append(ops, op)
	}

	// Validate patch grammar before any file I/O or block resolution.
	if errs := ValidatePatchFromOps(ops); len(errs) > 0 {
		var msgs []string
		for _, e := range errs {
			msgs = append(msgs, fmt.Sprintf("edit %d %s: %s", e.EditIndex, e.Field, e.Message))
		}
		return nil, fmt.Errorf("patch validation failed:\n%s", strings.Join(msgs, "\n"))
	}

	// Resolve block operations before validation.
	// Block ops are converted to regular replace/delete with computed end_anchor.
	for i, op := range ops {
		if op.Op != "replace_block" && op.Op != "delete_block" {
			continue
		}
		if t.blockResolver == nil {
			return nil, fmt.Errorf("edit %d: block operations require a block resolver, but none is configured", i+1)
		}
		lineNum, _, _, err := ParseSnapshotAnchor(op.Anchor)
		if err != nil {
			return nil, fmt.Errorf("edit %d: invalid block anchor: %w", i+1, err)
		}
		blockStart, blockEnd, resolveErr := t.blockResolver.ResolveBlock(resolved, lineNum)
		if resolveErr != nil {
			return nil, fmt.Errorf("edit %d: could not resolve block at line %d: %w (try using explicit start_anchor and end_anchor instead)", i+1, lineNum, resolveErr)
		}
		// Rewrite the op: replace_block → replace, delete_block → delete
		if op.Op == "replace_block" {
			ops[i].Op = "replace"
		} else {
			ops[i].Op = "delete"
		}
		// Update anchor and build end_anchor from resolved block boundaries
		if blockStart >= 1 && blockStart <= len(lines) {
			ops[i].Anchor = fmt.Sprintf("%d:%s", blockStart, ComputeLineHash(lines[blockStart-1]))
		}
		if blockEnd >= 1 && blockEnd <= len(lines) {
			ops[i].EndAnchor = fmt.Sprintf("%d:%s", blockEnd, ComputeLineHash(lines[blockEnd-1]))
		}
	}

	// Validate all anchors before applying any changes
	var mismatches []string
	for i, op := range ops {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, snapTag, hash, err := ParseSnapshotAnchor(op.Anchor)
		if err != nil {
			return nil, fmt.Errorf("edit %d: %w", i+1, err)
		}
		// If tag was provided separately in the "tag" field but not in anchor, use it
		if snapTag == "" && op.SnapshotTag != "" {
			snapTag = op.SnapshotTag
		}
		// Update the op with the parsed tag for recovery later
		ops[i].SnapshotTag = snapTag

		if !ValidateAnchor(lines, lineNum, hash) {
			actualContent := "(beyond file)"
			if lineNum >= 1 && lineNum <= len(lines) {
				actualContent = lines[lineNum-1]
			}
			mismatches = append(mismatches, FormatHashLine(lineNum, actualContent))
		}

		// Validate end_anchor for range ops
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, _, endHash, err := ParseSnapshotAnchor(op.EndAnchor)
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
			var cachedLines []string
			// Try to find cached snapshot by tag first (tag-based recovery)
			for _, op := range ops {
				if op.SnapshotTag != "" {
					cachedLines = t.readCache.GetByTag(op.SnapshotTag)
					if cachedLines != nil {
						break
					}
				}
			}
			// Fall back to path-based lookup if no tag match
			if cachedLines == nil {
				cachedLines = t.readCache.Get(resolved)
			}
			if cachedLines != nil {
				recovered, strategy, recoverErr := t.attemptRecovery(cachedLines, lines, ops)
				if recoverErr == nil {
					// Recovery succeeded -- write the recovered content
					result := strings.Join(recovered, "\n")
					if err := os.WriteFile(resolved, []byte(result), 0o644); err != nil {
						return nil, fmt.Errorf("recovery succeeded but write failed: %w", err)
					}
					msg := fmt.Sprintf("Edit applied with stale-anchor recovery to %s (%d lines, strategy: %s)", resolved, len(recovered), strategy)
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

			// Attempt session chain recovery: walk edit history newest to oldest.
			history := t.readCache.GetHistory(resolved)
			if len(history) > 0 {
				recovered, strategy, recoverErr := t.attemptSessionChainRecovery(lines, history, ops)
				if recoverErr == nil {
					result := strings.Join(recovered, "\n")
					if err := os.WriteFile(resolved, []byte(result), 0o644); err != nil {
						return nil, fmt.Errorf("session chain recovery succeeded but write failed: %w", err)
					}
					msg := fmt.Sprintf("Edit applied with session chain recovery to %s (%d lines, strategy: %s)", resolved, len(recovered), strategy)
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

	// Build output content
	output := strings.Join(result, "\n")

	// Check if preview/accept workflow is enabled
	if t.pendingChangesRegistry != nil {
		// Create pending change instead of applying directly
		originalContent := string(content)

		// Generate unified diff preview
		diff := t.generateDiffPreview(resolved, originalContent, output)

		// Create pending change with session ID from context (or generate one)
		sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
		if sid, ok := ctx.Value("session_id").(string); ok && sid != "" {
			sessionID = sid
		}

		now := time.Now()
		expiresAt := now.Add(30 * time.Minute) // Default expiry: 30 minutes

		change := &PendingChange{
			ID:        fmt.Sprintf("change_%s_%d", resolved, now.UnixNano()),
			SessionID: sessionID,
			FilePath:  resolved,
			Original:  originalContent,
			Modified:  output,
			Diff:      diff,
			CreatedAt: now,
			ExpiresAt: &expiresAt,
			Metadata: map[string]any{
				"tool":      "file_edit",
				"edits":     len(ops),
				"old_lines": len(lines),
				"new_lines": len(result),
			},
		}

		t.pendingChangesRegistry.Add(change)

		return tools.ToolResult{
			Success: true,
			Result: fmt.Sprintf("Created pending change %s for %s (%d edits, %d -> %d lines). Use 'resolve' tool to accept or reject.",
				change.ID, resolved, len(ops), len(lines), len(result)),
			Evidence: []models.Evidence{
				models.NewEvidence("pending_change_created", resolved, change.ID, t.Name()),
			},
		}, nil
	}

	// Direct mode: write result immediately
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
		startLine, _, _, err := ParseSnapshotAnchor(op.Anchor)
		if err != nil || startLine < 1 {
			continue
		}

		endLine := startLine
		if op.EndAnchor != "" {
			endLine, _, _, _ = ParseSnapshotAnchor(op.EndAnchor)
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

// fuzzyMatchMaxLength is the maximum line length for fuzzy matching (skip longer lines).
const fuzzyMatchMaxLength = 500

// attemptRecovery tries to remap stale anchors from the cached snapshot onto the
// current file content using a tiered strategy: exact match, hash-only match, then
// fuzzy (Levenshtein) match. For each anchor, it looks up the cached line at that
// line number, then scans the current file within a ±10 line window. If all anchors
// are found, the ops are rewritten with the new line numbers and applied against
// the current file. The strategy name used for the first anchor that required recovery
// is returned for reporting.
func (t *FileEditTool) attemptRecovery(cachedLines, currentLines []string, ops []editOp) ([]string, string, error) {
	// remap maps old (cached) line numbers to new (current) line numbers.
	remap := make(map[int]int)

	// Track the best (strongest) strategy used for reporting.
	bestStrategy := "exact"

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
			lineNum, _, hash, err := ParseSnapshotAnchor(anchor)
			if err != nil {
				return nil, "", fmt.Errorf("recovery: invalid anchor %q: %w", anchor, err)
			}

			// Already remapped?
			if _, done := remap[lineNum]; done {
				continue
			}

			// Validate anchor against cached snapshot.
			if !ValidateAnchor(cachedLines, lineNum, hash) {
				return nil, "", fmt.Errorf("recovery: anchor %q does not match cached snapshot", anchor)
			}

			// Get the cached line content.
			cachedIdx := lineNum - 1
			if cachedIdx < 0 || cachedIdx >= len(cachedLines) {
				return nil, "", fmt.Errorf("recovery: cached line %d out of range", lineNum)
			}
			cachedContent := cachedLines[cachedIdx]

			// Search current file within ±recoveryWindow lines using tiered strategies.
			searchStart := max(lineNum-1-t.recoveryConfig.RecoveryWindow, 0) // 0-based
			searchEnd := min(lineNum-1+t.recoveryConfig.RecoveryWindow, len(currentLines)-1)

			newLine, strategy, found := findMatchingLine(cachedContent, hash, currentLines, searchStart, searchEnd, t.recoveryConfig.FuzzyThreshold)
			if !found {
				return nil, "", fmt.Errorf("recovery: could not relocate cached line %d in current file", lineNum)
			}
			remap[lineNum] = newLine

			// Track the strongest (worst) strategy used: exact < hash < fuzzy
			if strategyRank(strategy) > strategyRank(bestStrategy) {
				bestStrategy = strategy
			}
		}
	}

	// Build remapped ops by adjusting anchor line numbers and hashes.
	// For hash/fuzzy recovery, the remapped anchor hash must match the current
	// file content, not the original cached content.
	remappedOps := make([]editOp, len(ops))
	for i, op := range ops {
		remappedOps[i] = op
		remappedOps[i].Anchor = remapAnchorWithHash(op.Anchor, remap, currentLines)
		if op.EndAnchor != "" {
			remappedOps[i].EndAnchor = remapAnchorWithHash(op.EndAnchor, remap, currentLines)
		}
	}

	// Validate remapped anchors against current file.
	for _, op := range remappedOps {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, _, hash, _ := ParseSnapshotAnchor(op.Anchor)
		if !ValidateAnchor(currentLines, lineNum, hash) {
			return nil, "", fmt.Errorf("recovery: remapped anchor %q does not match current file", op.Anchor)
		}
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, _, endHash, _ := ParseSnapshotAnchor(op.EndAnchor)
			if !ValidateAnchor(currentLines, endLineNum, endHash) {
				return nil, "", fmt.Errorf("recovery: remapped end_anchor %q does not match current file", op.EndAnchor)
			}
		}
	}

	// Apply the remapped edits against current file content.
	result := t.applyEdits(currentLines, remappedOps)
	return result, bestStrategy, nil
}

// attemptRecoveryWithConfig performs recovery using the given cached snapshot and
// configuration values. This is extracted so session chain recovery can reuse it.
func (t *FileEditTool) attemptRecoveryWithConfig(cachedLines, currentLines []string, ops []editOp, window int, threshold float64) ([]string, string, error) {
	// remap maps old (cached) line numbers to new (current) line numbers.
	remap := make(map[int]int)

	// Track the best (strongest) strategy used for reporting.
	bestStrategy := "exact"

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
			lineNum, _, hash, err := ParseSnapshotAnchor(anchor)
			if err != nil {
				return nil, "", fmt.Errorf("recovery: invalid anchor %q: %w", anchor, err)
			}

			// Already remapped?
			if _, done := remap[lineNum]; done {
				continue
			}

			// Validate anchor against cached snapshot.
			if !ValidateAnchor(cachedLines, lineNum, hash) {
				return nil, "", fmt.Errorf("recovery: anchor %q does not match cached snapshot", anchor)
			}

			// Get the cached line content.
			cachedIdx := lineNum - 1
			if cachedIdx < 0 || cachedIdx >= len(cachedLines) {
				return nil, "", fmt.Errorf("recovery: cached line %d out of range", lineNum)
			}
			cachedContent := cachedLines[cachedIdx]

			// Search current file within ±window lines using tiered strategies.
			searchStart := max(lineNum-1-window, 0)
			searchEnd := min(lineNum-1+window, len(currentLines)-1)

			newLine, strategy, found := findMatchingLine(cachedContent, hash, currentLines, searchStart, searchEnd, threshold)
			if !found {
				return nil, "", fmt.Errorf("recovery: could not relocate cached line %d in current file", lineNum)
			}
			remap[lineNum] = newLine

			// Track the strongest (worst) strategy used: exact < hash < fuzzy
			if strategyRank(strategy) > strategyRank(bestStrategy) {
				bestStrategy = strategy
			}
		}
	}

	// Build remapped ops by adjusting anchor line numbers and hashes.
	remappedOps := make([]editOp, len(ops))
	for i, op := range ops {
		remappedOps[i] = op
		remappedOps[i].Anchor = remapAnchorWithHash(op.Anchor, remap, currentLines)
		if op.EndAnchor != "" {
			remappedOps[i].EndAnchor = remapAnchorWithHash(op.EndAnchor, remap, currentLines)
		}
	}

	// Validate remapped anchors against current file.
	for _, op := range remappedOps {
		if op.Anchor == "BOF" || op.Anchor == "EOF" {
			continue
		}
		lineNum, _, hash, _ := ParseSnapshotAnchor(op.Anchor)
		if !ValidateAnchor(currentLines, lineNum, hash) {
			return nil, "", fmt.Errorf("recovery: remapped anchor %q does not match current file", op.Anchor)
		}
		if (op.Op == "replace" || op.Op == "delete") && op.EndAnchor != "" {
			endLineNum, _, endHash, _ := ParseSnapshotAnchor(op.EndAnchor)
			if !ValidateAnchor(currentLines, endLineNum, endHash) {
				return nil, "", fmt.Errorf("recovery: remapped end_anchor %q does not match current file", op.EndAnchor)
			}
		}
	}

	// Apply the remapped edits against current file content.
	result := t.applyEdits(currentLines, remappedOps)
	return result, bestStrategy, nil
}

// attemptSessionChainRecovery walks the edit history (newest to oldest) and
// tries recovery against each historical snapshot. Returns the first successful
// result with a strategy "session_chain(snapshotTag, recoveryStrategy)".
func (t *FileEditTool) attemptSessionChainRecovery(currentLines []string, history []SnapshotEntry, ops []editOp) ([]string, string, error) {
	for _, entry := range history {
		recovered, strategy, err := t.attemptRecoveryWithConfig(
			entry.Lines, currentLines, ops,
			t.recoveryConfig.RecoveryWindow,
			t.recoveryConfig.FuzzyThreshold,
		)
		if err == nil {
			return recovered, fmt.Sprintf("session_chain(%s, %s)", entry.Tag, strategy), nil
		}
	}
	return nil, "", fmt.Errorf("session chain recovery: no historical snapshot matched")
}

// findMatchingLine searches currentLines[searchStart..searchEnd] for a line
// matching cachedContent using a tiered strategy: exact match, hash-only match,
// then fuzzy Levenshtein match. Returns the 1-based line number, strategy name,
// and whether a match was found.
func findMatchingLine(cachedContent, cachedHash string, currentLines []string, searchStart, searchEnd int, threshold float64) (int, string, bool) {
	// Strategy 1: exact content match.
	for i := searchStart; i <= searchEnd; i++ {
		if currentLines[i] == cachedContent {
			return i + 1, "exact", true
		}
	}

	// Strategy 2: hash-only match (same xxhash bigram).
	for i := searchStart; i <= searchEnd; i++ {
		if ComputeLineHash(currentLines[i]) == cachedHash {
			return i + 1, "hash", true
		}
	}

	// Strategy 3: fuzzy match (Levenshtein ratio above threshold).
	// Skip for lines longer than fuzzyMatchMaxLength to avoid O(n^2) cost.
	if len(cachedContent) <= fuzzyMatchMaxLength {
		bestIdx := -1
		bestRatio := 0.0
		for i := searchStart; i <= searchEnd; i++ {
			candidate := currentLines[i]
			// Skip candidates that are too long as well.
			if len(candidate) > fuzzyMatchMaxLength {
				continue
			}
			ratio := levenshteinRatio(cachedContent, candidate)
			if ratio > bestRatio {
				bestRatio = ratio
				bestIdx = i
			}
		}
		if bestIdx >= 0 && bestRatio >= threshold {
			return bestIdx + 1, "fuzzy", true
		}
	}

	return 0, "", false
}

// strategyRank returns a numeric rank for recovery strategy reporting.
// Higher rank = weaker strategy. Used to report the worst strategy needed.
func strategyRank(s string) int {
	switch s {
	case "exact":
		return 0
	case "hash":
		return 1
	case "fuzzy":
		return 2
	default:
		return 3
	}
}

// levenshteinDistance computes the Levenshtein edit distance between two strings
// using standard DP. Only intended for short strings (< 500 chars).
func levenshteinDistance(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use two rows to save memory.
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min(del, min(ins, sub))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// levenshteinRatio returns the similarity ratio between two strings in [0.0, 1.0].
// 1.0 means identical, 0.0 means completely different.
func levenshteinRatio(a, b string) float64 {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshteinDistance(a, b)
	return 1.0 - float64(dist)/float64(maxLen)
}

// remapAnchor adjusts an anchor string using the remap table.
// BOF and EOF anchors are returned unchanged.
func remapAnchor(anchor string, remap map[int]int) string {
	if anchor == "BOF" || anchor == "EOF" {
		return anchor
	}
	lineNum, tag, hash, err := ParseSnapshotAnchor(anchor)
	if err != nil {
		return anchor
	}
	if newLine, ok := remap[lineNum]; ok {
		if tag != "" {
			return fmt.Sprintf("%d:%s:%s", newLine, tag, hash)
		}
		return fmt.Sprintf("%d:%s", newLine, hash)
	}
	return anchor
}

// remapAnchorWithHash adjusts an anchor string using the remap table and updates
// the hash to match the current file content. This is needed for hash/fuzzy
// recovery where the remapped line may have different content (and thus hash)
// than the original cached line.
// BOF and EOF anchors are returned unchanged.
func remapAnchorWithHash(anchor string, remap map[int]int, currentLines []string) string {
	if anchor == "BOF" || anchor == "EOF" {
		return anchor
	}
	lineNum, tag, _, err := ParseSnapshotAnchor(anchor)
	if err != nil {
		return anchor
	}
	newLine, ok := remap[lineNum]
	if !ok {
		return anchor
	}
	// Compute the hash from the current file content at the remapped position.
	idx := newLine - 1
	if idx < 0 || idx >= len(currentLines) {
		return anchor
	}
	newHash := ComputeLineHash(currentLines[idx])
	if tag != "" {
		return fmt.Sprintf("%d:%s:%s", newLine, tag, newHash)
	}
	return fmt.Sprintf("%d:%s", newLine, newHash)
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
			startLine, _, _, _ := ParseSnapshotAnchor(op.Anchor)
			endLine := startLine
			if op.EndAnchor != "" {
				endLine, _, _, _ = ParseSnapshotAnchor(op.EndAnchor)
			}
			lineOps = append(lineOps, lineOp{
				opType:    "delete_range",
				startLine: startLine,
				endLine:   endLine,
			})

		case "replace":
			startLine, _, _, _ := ParseSnapshotAnchor(op.Anchor)
			endLine := startLine
			if op.EndAnchor != "" {
				endLine, _, _, _ = ParseSnapshotAnchor(op.EndAnchor)
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
				insertLine, _, _, _ = ParseSnapshotAnchor(op.Anchor)
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
				insertLine, _, _, _ = ParseSnapshotAnchor(op.Anchor)
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
			if start >= len(result) {
				// Start is past the end of the file — nothing to delete.
				continue
			}
			if end >= len(result) {
				end = len(result) - 1
			}
			if start > end {
				continue
			}
			// Copy into a fresh backing array to avoid slice aliasing between
			// result[:start] and result[end+1:] which share the same array.
			// The in-place append mutation corrupts data when subsequent
			// operations reference the same backing array.
			kept := make([]string, 0, len(result)-(end-start+1))
			kept = append(kept, result[:start]...)
			kept = append(kept, result[end+1:]...)
			result = kept

		case "insert":
			idx := op.startLine - 1
			if idx < 0 {
				idx = 0
			}
			if idx > len(result) {
				idx = len(result)
			}
			// Copy replacement slices into fresh backing arrays before
			// modifying to avoid aliasing with the result slice.
			insertSlice := make([]string, len(op.content))
			copy(insertSlice, op.content)
			// Build a new slice rather than mutating result in place to
			// avoid the classic append(result[:idx], ...) aliasing bug.
			newResult := make([]string, 0, len(result)+len(insertSlice))
			newResult = append(newResult, result[:idx]...)
			newResult = append(newResult, insertSlice...)
			newResult = append(newResult, result[idx:]...)
			result = newResult
		}
	}

	return result
}

// generateDiffPreview creates a unified diff preview between original and modified content.
func (t *FileEditTool) generateDiffPreview(filePath, original, modified string) string {
	// Simple unified diff format
	lines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	var diff []string
	diff = append(diff, fmt.Sprintf("--- a/%s", filePath))
	diff = append(diff, fmt.Sprintf("+++ b/%s", filePath))

	// Simple line-by-line comparison
	maxLen := len(lines)
	if len(modLines) > maxLen {
		maxLen = len(modLines)
	}

	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""
		if i < len(lines) {
			oldLine = lines[i]
		}
		if i < len(modLines) {
			newLine = modLines[i]
		}

		if i >= len(lines) {
			// Added line
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		} else if i >= len(modLines) {
			// Deleted line
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
		} else if oldLine != newLine {
			// Changed line
			diff = append(diff, fmt.Sprintf("-%s", oldLine))
			diff = append(diff, fmt.Sprintf("+%s", newLine))
		}
	}

	return strings.Join(diff, "\n")
}

// Ensure FileEditTool implements the Tool interface.
var _ tools.Tool = (*FileEditTool)(nil)
