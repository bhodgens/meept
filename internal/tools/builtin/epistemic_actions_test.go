package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func TestTruncatePreview(t *testing.T) {
	cases := []struct {
		in    string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exact10ch", 10, "exact10ch"},
		{"this is a long string", 10, "this is..."},
		{"", 10, ""},
	}
	for i, c := range cases {
		got := truncatePreview(c.in, c.max)
		if got != c.want {
			t.Errorf("case %d: truncatePreview(%q,%d) = %q, want %q", i, c.in, c.max, got, c.want)
		}
	}
}

func TestMarkSupersededTool_Metadata(t *testing.T) {
	tool := NewMarkSupersededTool(nil, nil)
	if tool.Name() != "mark_superseded" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
	if tool.Parameters().Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q", tool.Parameters().Type)
	}
}

func TestMarkSupersededTool_NilManager(t *testing.T) {
	tool := NewMarkSupersededTool(nil, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"old_id": "a", "new_id": "b",
	}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestMarkSupersededTool_MissingArgs(t *testing.T) {
	tool := NewMarkSupersededTool(&memory.Manager{}, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing old_id")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"old_id": "a"}); err == nil {
		t.Error("expected error for missing new_id")
	}
}

func TestMarkSupersededTool_Phase1ReturnsConfirmation(t *testing.T) {
	// Uninitialized manager: phase-1 preview should short-circuit with
	// confirmation request before touching the manager. We use a manager
	// value with no init so GetByID fails — but phase 1 should return
	// a confirmation response, not an error from GetByID.
	tool := NewMarkSupersededTool(&memory.Manager{}, nil)
	// We need to get past the arg check but before manager access. The tool
	// must return a confirmation request before hitting the store.
	// Since the graph arg is nil, EdgeCountForMemory will short-circuit.
	res, err := tool.Execute(context.Background(), map[string]any{
		"old_id": "abc", "new_id": "def",
	})
	if err != nil {
		t.Fatalf("phase 1 returned error: %v", err)
	}
	resultMap, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("phase 1 result must be map, got %T", res)
	}
	if !IsConfirmationRequest(resultMap) {
		t.Errorf("phase 1 must return requires_confirmation=true, got %v", resultMap)
	}
}

func TestMarkResolvedTool_Metadata(t *testing.T) {
	tool := NewMarkResolvedTool(nil)
	if tool.Name() != "mark_resolved" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
	if tool.Parameters().Type != schemaTypeObject {
		t.Errorf("Parameters.Type = %q", tool.Parameters().Type)
	}
}

func TestMarkResolvedTool_NilManager(t *testing.T) {
	tool := NewMarkResolvedTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"prediction_id": "x", "outcome": "y",
	}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestMarkResolvedTool_MissingArgs(t *testing.T) {
	tool := NewMarkResolvedTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing prediction_id")
	}
}

func TestMarkResolvedTool_Phase1ReturnsConfirmation(t *testing.T) {
	tool := NewMarkResolvedTool(&memory.Manager{})
	res, err := tool.Execute(context.Background(), map[string]any{
		"prediction_id": "abc", "outcome": "success",
	})
	if err != nil {
		t.Fatalf("phase 1 returned error: %v", err)
	}
	if !IsConfirmationRequest(res.(map[string]any)) {
		t.Errorf("phase 1 must return confirmation request")
	}
}

func TestRecordReviewTool_Metadata(t *testing.T) {
	tool := NewRecordReviewTool(nil)
	if tool.Name() != "record_review" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
}

func TestRecordReviewTool_NilManager(t *testing.T) {
	tool := NewRecordReviewTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"decision_id": "x", "actual_outcome": "y",
	}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestRecordReviewTool_MissingArgs(t *testing.T) {
	tool := NewRecordReviewTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing decision_id")
	}
}

func TestRecordReviewTool_Phase1ReturnsConfirmation(t *testing.T) {
	tool := NewRecordReviewTool(&memory.Manager{})
	res, err := tool.Execute(context.Background(), map[string]any{
		"decision_id": "abc", "actual_outcome": "success",
	})
	if err != nil {
		t.Fatalf("phase 1 returned error: %v", err)
	}
	if !IsConfirmationRequest(res.(map[string]any)) {
		t.Errorf("phase 1 must return confirmation request")
	}
}

func TestRejectClaimTool_Metadata(t *testing.T) {
	tool := NewRejectClaimTool(nil)
	if tool.Name() != "reject_claim" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
}

func TestRejectClaimTool_NilManager(t *testing.T) {
	tool := NewRejectClaimTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"claim_id": "x",
	}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestRejectClaimTool_MissingArgs(t *testing.T) {
	tool := NewRejectClaimTool(&memory.Manager{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for missing claim_id")
	}
}

func TestRejectClaimTool_Phase1ReturnsConfirmation(t *testing.T) {
	tool := NewRejectClaimTool(&memory.Manager{})
	res, err := tool.Execute(context.Background(), map[string]any{
		"claim_id": "abc",
	})
	if err != nil {
		t.Fatalf("phase 1 returned error: %v", err)
	}
	if !IsConfirmationRequest(res.(map[string]any)) {
		t.Errorf("phase 1 must return confirmation request")
	}
}

func TestPurgeAutoClaimsTool_Metadata(t *testing.T) {
	tool := NewPurgeAutoClaimsTool(nil)
	if tool.Name() != "purge_auto_claims" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Category() != "memory" {
		t.Errorf("Category = %q", tool.Category())
	}
	if tool.Description() == "" {
		t.Error("Description must not be empty")
	}
}

func TestPurgeAutoClaimsTool_NilManager(t *testing.T) {
	tool := NewPurgeAutoClaimsTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error for nil manager")
	}
}

func TestPurgeAutoClaimsTool_Phase1ReturnsConfirmation(t *testing.T) {
	tool := NewPurgeAutoClaimsTool(&memory.Manager{})
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("phase 1 returned error: %v", err)
	}
	if !IsConfirmationRequest(res.(map[string]any)) {
		t.Errorf("phase 1 must return confirmation request")
	}
}

func TestSetGraph_NilSafe(t *testing.T) {
	tool := &MarkSupersededTool{}
	// Must not panic on nil.
	tool.SetGraph(nil)
}
