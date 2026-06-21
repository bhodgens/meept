package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// epistemicGraph is the minimal graph interface MarkSupersededTool needs.
// Defined locally so the tool can accept either *memory.KnowledgeGraph or
// any future test double without a hard dependency on the concrete type.
type epistemicGraph interface {
	EdgeCountForMemory(ctx context.Context, memoryID string) (int, error)
}

// truncatePreview shortens s to maxLen characters, appending "..." when
// truncation occurs. Strings at or below maxLen are returned unchanged.
func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// ============================================================================
// MarkSupersededTool
// ============================================================================

// MarkSupersededTool marks an old memory as superseded by a new one and
// redirects incoming evidence edges. Requires confirmation because it
// flips is_current on the old memory and mutates the graph.
type MarkSupersededTool struct {
	manager *memory.Manager
	graph   epistemicGraph
}

// NewMarkSupersededTool constructs the tool. graph may be nil; the preview
// just reports zero affected edges in that case.
func NewMarkSupersededTool(manager *memory.Manager, graph epistemicGraph) *MarkSupersededTool {
	return &MarkSupersededTool{manager: manager, graph: graph}
}

// SetGraph sets the graph reference. Nil-safe per CLAUDE.md setter convention.
func (t *MarkSupersededTool) SetGraph(g epistemicGraph) {
	if g != nil {
		t.graph = g
	}
}

func (t *MarkSupersededTool) Name() string     { return "mark_superseded" }
func (t *MarkSupersededTool) Category() string { return "memory" }

func (t *MarkSupersededTool) Description() string {
	return "Mark an old memory as superseded by a newer one. The old memory " +
		"becomes non-current, a 'superseded' edge is written from old to new, " +
		"and any incoming evidence_for/evidence_against edges are redirected " +
		"to the new memory. Requires explicit confirmation."
}

func (t *MarkSupersededTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"old_id": {
				Type:        schemaTypeString,
				Description: "ID of the memory being superseded.",
			},
			"new_id": {
				Type:        schemaTypeString,
				Description: "ID of the superseding memory.",
			},
			"confirmed": {
				Type:        schemaTypeBoolean,
				Description: "Set to true to execute. Omit or false to get a confirmation preview.",
			},
		},
		Required: []string{"old_id", "new_id"},
	}
}

func (t *MarkSupersededTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	oldID := asStringArg(args["old_id"])
	if oldID == "" {
		return nil, fmt.Errorf("old_id is required")
	}
	newID := asStringArg(args["new_id"])
	if newID == "" {
		return nil, fmt.Errorf("new_id is required")
	}
	confirmed, _ := args["confirmed"].(bool)

	// Phase 1: preview.
	if !confirmed {
		oldPreview := truncatePreview(memoryPreview(t.manager, ctx, oldID), 80)
		newPreview := truncatePreview(memoryPreview(t.manager, ctx, newID), 80)
		affectedEdges := 0
		if t.graph != nil {
			if c, err := t.graph.EdgeCountForMemory(ctx, oldID); err == nil {
				affectedEdges = c
			}
		}
		return ConfirmationResponse("mark_superseded", false,
			fmt.Sprintf("supersede %s with %s", oldID, newID),
			map[string]any{
				"old_preview":    oldPreview,
				"new_preview":    newPreview,
				"affected_edges": affectedEdges,
			}), nil
	}

	// Phase 2: execute.
	redirected, auditID, err := t.manager.MarkSuperseded(ctx, oldID, newID)
	if err != nil {
		return nil, fmt.Errorf("mark superseded: %w", err)
	}
	return map[string]any{
		"success":          true,
		"audit_id":         auditID,
		"redirected_edges": redirected,
	}, nil
}

// memoryPreview returns the first line of the memory's content for display,
// or "<unavailable>" when the memory cannot be loaded.
func memoryPreview(m *memory.Manager, ctx context.Context, id string) string {
	if m == nil {
		return "<no-manager>"
	}
	mem, err := m.GetByID(ctx, id)
	if err != nil || mem == nil {
		return "<unavailable>"
	}
	return mem.Content
}

// ============================================================================
// MarkResolvedTool
// ============================================================================

// MarkResolvedTool closes a prediction with an observed outcome. Requires
// confirmation because it writes a new memory version and sets status.
type MarkResolvedTool struct {
	manager *memory.Manager
}

// NewMarkResolvedTool constructs the tool.
func NewMarkResolvedTool(manager *memory.Manager) *MarkResolvedTool {
	return &MarkResolvedTool{manager: manager}
}

func (t *MarkResolvedTool) Name() string     { return "mark_resolved" }
func (t *MarkResolvedTool) Category() string { return "memory" }

func (t *MarkResolvedTool) Description() string {
	return "Resolve a prediction with the observed outcome once its horizon has " +
		"passed. The prediction status transitions to 'resolved' and the outcome " +
		"is recorded for later review scoring. Requires explicit confirmation."
}

func (t *MarkResolvedTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"prediction_id": {
				Type:        schemaTypeString,
				Description: "ID of the prediction to resolve.",
			},
			"outcome": {
				Type:        schemaTypeString,
				Description: "What actually happened (free text, will be scored against expected).",
			},
			"confirmed": {
				Type:        schemaTypeBoolean,
				Description: "Set to true to execute. Omit or false to get a confirmation preview.",
			},
		},
		Required: []string{"prediction_id", "outcome"},
	}
}

func (t *MarkResolvedTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	predID := asStringArg(args["prediction_id"])
	if predID == "" {
		return nil, fmt.Errorf("prediction_id is required")
	}
	outcome := asStringArg(args["outcome"])
	if outcome == "" {
		return nil, fmt.Errorf("outcome is required")
	}
	confirmed, _ := args["confirmed"].(bool)

	if !confirmed {
		preview := truncatePreview(memoryPreview(t.manager, ctx, predID), 80)
		return ConfirmationResponse("mark_resolved", false,
			fmt.Sprintf("resolve prediction %s with outcome: %s", predID, truncatePreview(outcome, 40)),
			map[string]any{
				"prediction_preview": preview,
				"outcome_preview":    truncatePreview(outcome, 80),
			}), nil
	}

	auditID, err := t.manager.MarkResolved(ctx, predID, outcome)
	if err != nil {
		return nil, fmt.Errorf("mark resolved: %w", err)
	}
	return map[string]any{
		"success":  true,
		"audit_id": auditID,
	}, nil
}

// ============================================================================
// RecordReviewTool
// ============================================================================

// RecordReviewTool closes a decision with the actual outcome, scoring the
// expected-vs-actual overlap. Requires confirmation for the same reason as
// MarkResolved.
type RecordReviewTool struct {
	manager *memory.Manager
}

// NewRecordReviewTool constructs the tool.
func NewRecordReviewTool(manager *memory.Manager) *RecordReviewTool {
	return &RecordReviewTool{manager: manager}
}

func (t *RecordReviewTool) Name() string     { return "record_review" }
func (t *RecordReviewTool) Category() string { return "memory" }

func (t *RecordReviewTool) Description() string {
	return "Review a recorded decision by recording the actual outcome. The " +
		"decision status transitions to 'reviewed' and a token-overlap score " +
		"between expected and actual outcome is computed and stored. Requires " +
		"explicit confirmation."
}

func (t *RecordReviewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"decision_id": {
				Type:        schemaTypeString,
				Description: "ID of the decision to review.",
			},
			"actual_outcome": {
				Type:        schemaTypeString,
				Description: "What actually happened (free text).",
			},
			"confirmed": {
				Type:        schemaTypeBoolean,
				Description: "Set to true to execute. Omit or false to get a confirmation preview.",
			},
		},
		Required: []string{"decision_id", "actual_outcome"},
	}
}

func (t *RecordReviewTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	decID := asStringArg(args["decision_id"])
	if decID == "" {
		return nil, fmt.Errorf("decision_id is required")
	}
	actual := asStringArg(args["actual_outcome"])
	if actual == "" {
		return nil, fmt.Errorf("actual_outcome is required")
	}
	confirmed, _ := args["confirmed"].(bool)

	if !confirmed {
		preview := truncatePreview(memoryPreview(t.manager, ctx, decID), 80)
		return ConfirmationResponse("record_review", false,
			fmt.Sprintf("review decision %s", decID),
			map[string]any{
				"decision_preview":   preview,
				"actual_outcome":     truncatePreview(actual, 80),
			}), nil
	}

	score, auditID, err := t.manager.RecordReview(ctx, decID, actual)
	if err != nil {
		return nil, fmt.Errorf("record review: %w", err)
	}
	return map[string]any{
		"success":    true,
		"audit_id":   auditID,
		"score":      score,
	}, nil
}

// ============================================================================
// RejectClaimTool
// ============================================================================

// RejectClaimTool transitions a claim to rejected status. Rejected claims
// are excluded from search ranking and canonical lookup but retained for
// audit. Requires confirmation.
type RejectClaimTool struct {
	manager *memory.Manager
}

// NewRejectClaimTool constructs the tool.
func NewRejectClaimTool(manager *memory.Manager) *RejectClaimTool {
	return &RejectClaimTool{manager: manager}
}

func (t *RejectClaimTool) Name() string     { return "reject_claim" }
func (t *RejectClaimTool) Category() string { return "memory" }

func (t *RejectClaimTool) Description() string {
	return "Reject a claim, excluding it from search ranking and canonical " +
		"lookup while retaining it for audit. Useful for cleaning up ambient " +
		"auto-claims that turned out to be wrong. Requires confirmation."
}

func (t *RejectClaimTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"claim_id": {
				Type:        schemaTypeString,
				Description: "ID of the claim to reject.",
			},
			"confirmed": {
				Type:        schemaTypeBoolean,
				Description: "Set to true to execute. Omit or false to get a confirmation preview.",
			},
		},
		Required: []string{"claim_id"},
	}
}

func (t *RejectClaimTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	claimID := asStringArg(args["claim_id"])
	if claimID == "" {
		return nil, fmt.Errorf("claim_id is required")
	}
	confirmed, _ := args["confirmed"].(bool)

	if !confirmed {
		preview := truncatePreview(memoryPreview(t.manager, ctx, claimID), 80)
		return ConfirmationResponse("reject_claim", true,
			fmt.Sprintf("reject claim %s", claimID),
			map[string]any{
				"claim_preview": preview,
			}), nil
	}

	if err := t.manager.RejectClaim(ctx, claimID); err != nil {
		return nil, fmt.Errorf("reject claim: %w", err)
	}
	return map[string]any{
		"success":  true,
		"claim_id": claimID,
		"status":   string(memory.ClaimStatusRejected),
	}, nil
}

// ============================================================================
// PurgeAutoClaimsTool
// ============================================================================

// PurgeAutoClaimsTool deletes all ambient-extracted (status=auto) claims
// older than the given cutoff. Bulk destructive — requires confirmation.
type PurgeAutoClaimsTool struct {
	manager *memory.Manager
}

// NewPurgeAutoClaimsTool constructs the tool.
func NewPurgeAutoClaimsTool(manager *memory.Manager) *PurgeAutoClaimsTool {
	return &PurgeAutoClaimsTool{manager: manager}
}

func (t *PurgeAutoClaimsTool) Name() string     { return "purge_auto_claims" }
func (t *PurgeAutoClaimsTool) Category() string { return "memory" }

func (t *PurgeAutoClaimsTool) Description() string {
	return "Permanently delete ambient-extracted (status=auto) claims older " +
		"than the given cutoff. Use this to reclaim storage after a batch of " +
		"ambient claims have been reviewed. Deletion is irreversible; rejected " +
		"claims are retained for audit. Requires explicit confirmation."
}

func (t *PurgeAutoClaimsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"older_than_days": {
				Type:        schemaTypeInteger,
				Description: "Delete auto claims older than N days. Defaults to 30.",
			},
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Maximum claims to delete in one call. Defaults to 100.",
			},
			"confirmed": {
				Type:        schemaTypeBoolean,
				Description: "Set to true to execute. Omit or false to get a confirmation preview.",
			},
		},
		Required: []string{},
	}
}

func (t *PurgeAutoClaimsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	olderThanDays := 30
	if v, ok := args["older_than_days"].(float64); ok && v > 0 {
		olderThanDays = int(v)
	}
	limit := 100
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	confirmed, _ := args["confirmed"].(bool)

	cutoff := time.Now().AddDate(0, 0, -olderThanDays)

	// Preview phase: count without deleting.
	if !confirmed {
		autoClaims, err := t.manager.ListAutoClaims(ctx, cutoff, limit)
		if err != nil {
			// Manager may be uninitialized in tests / fresh installs — surface
			// a zero-count preview rather than a hard error so the caller can
			// still see the action shape.
			autoClaims = nil
		}
		return ConfirmationResponse("purge_auto_claims", false,
			fmt.Sprintf("delete %d auto claims older than %d day(s)", len(autoClaims), olderThanDays),
			map[string]any{
				"claim_count":    len(autoClaims),
				"older_than_days": olderThanDays,
			}), nil
	}

	// Execute phase: list then delete each.
	autoClaims, err := t.manager.ListAutoClaims(ctx, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list auto claims for purge: %w", err)
	}
	var deleted int
	var firstErr error
	for _, r := range autoClaims {
		if err := t.manager.Delete(ctx, r.Memory.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		deleted++
	}
	resp := map[string]any{
		"success":  deleted > 0 || firstErr == nil,
		"deleted":  deleted,
		"attempted": len(autoClaims),
	}
	if firstErr != nil {
		resp["first_error"] = firstErr.Error()
	}
	return resp, nil
}

// Ensure destructive tools satisfy the Tool interface.
var (
	_ tools.Tool = (*MarkSupersededTool)(nil)
	_ tools.Tool = (*MarkResolvedTool)(nil)
	_ tools.Tool = (*RecordReviewTool)(nil)
	_ tools.Tool = (*RejectClaimTool)(nil)
	_ tools.Tool = (*PurgeAutoClaimsTool)(nil)
)

// strings import retained for potential future string utilities in this file.
var _ = strings.TrimSpace
