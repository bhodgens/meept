package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// mockReviewRegistry implements delegateRegistry for review tool tests.
type mockReviewRegistry struct {
	specs    []*agent.AgentSpec
	response string
	err      error
}

func (m *mockReviewRegistry) GetSpec(id string) (*agent.AgentSpec, bool) {
	for _, s := range m.specs {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

func (m *mockReviewRegistry) ListSpecs() []*agent.AgentSpec {
	return m.specs
}

func (m *mockReviewRegistry) RunAgent(_ context.Context, _, _, _ string) (string, error) {
	return m.response, m.err
}

func testReviewSpecs() []*agent.AgentSpec {
	return []*agent.AgentSpec{
		{ID: "code-reviewer", Name: "Code Reviewer", Role: agent.RoleReviewer},
		{ID: "debug-reviewer", Name: "Debug Reviewer", Role: agent.RoleReviewer},
		{ID: "coder", Name: "Coder", Role: agent.RoleExecutor},
	}
}

func TestRequestReviewTool_Name(t *testing.T) {
	tool := &RequestReviewTool{}
	if tool.Name() != "request_review" {
		t.Errorf("expected name 'request_review', got %q", tool.Name())
	}
}

func TestRequestReviewTool_Execute_Approved(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: `{"status": "approved", "feedback": "Looks good", "issues": [], "confidence": 0.9}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":         "Implemented the request_review tool",
		"caller_agent_id": "coder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if !rr.Approved {
		t.Errorf("expected approved=true, got false")
	}
	if rr.Status != "approved" {
		t.Errorf("expected status=approved, got %q", rr.Status)
	}
	if rr.ReviewerID != "code-reviewer" {
		t.Errorf("expected reviewer=code-reviewer, got %q", rr.ReviewerID)
	}
}

func TestRequestReviewTool_Execute_Rejected(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: `{"status": "rejected", "feedback": "Missing error handling", "issues": ["No error check on line 42"]}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":         "Added new feature",
		"work_content":    "func NewThing() *Thing { return &Thing{} }",
		"caller_agent_id": "coder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.Approved {
		t.Error("expected approved=false for rejection")
	}
	if len(rr.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(rr.Issues))
	}
	if rr.Issues[0] != "No error check on line 42" {
		t.Errorf("unexpected issue: %q", rr.Issues[0])
	}
}

func TestRequestReviewTool_Execute_NilRegistry(t *testing.T) {
	tool := &RequestReviewTool{
		registry:      nil,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message": "review this",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *tools.ToolResult, got %T", result)
	}
	if tr.Success {
		t.Error("expected failure for nil registry")
	}
}

func TestRequestReviewTool_Execute_MissingMessage(t *testing.T) {
	tool := &RequestReviewTool{
		registry:      &mockReviewRegistry{},
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *tools.ToolResult, got %T", result)
	}
	if tr.Success {
		t.Error("expected failure for missing message")
	}
}

func TestRequestReviewTool_Execute_UnknownReviewer(t *testing.T) {
	reg := &mockReviewRegistry{
		specs: testReviewSpecs(),
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":     "review this",
		"reviewer_id": "nonexistent-reviewer",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *tools.ToolResult, got %T", result)
	}
	if tr.Success {
		t.Error("expected failure for unknown reviewer")
	}
}

func TestRequestReviewTool_Execute_ExplicitReviewerID(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: `{"status": "approved", "feedback": "Debug fix looks correct", "issues": []}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":     "Fixed null pointer in handler",
		"reviewer_id": "debug-reviewer",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.ReviewerID != "debug-reviewer" {
		t.Errorf("expected reviewer=debug-reviewer, got %q", rr.ReviewerID)
	}
}

func TestRequestReviewTool_Execute_NonJSONResponse(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: "The code looks good overall but I couldn't generate structured output.",
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":         "review this",
		"caller_agent_id": "coder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.Status != "needs_info" {
		t.Errorf("expected status=needs_info for non-JSON response, got %q", rr.Status)
	}
	if rr.Approved {
		t.Error("expected approved=false for non-JSON response")
	}
}

func TestRequestReviewTool_Execute_DefaultReviewerFallback(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: `{"status": "approved", "feedback": "ok", "issues": []}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message": "review this",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.ReviewerID != "code-reviewer" {
		t.Errorf("expected default reviewer=code-reviewer, got %q", rr.ReviewerID)
	}
}

func TestRequestReviewTool_DefaultReviewerMapping(t *testing.T) {
	m := DefaultReviewerMapping()
	expected := map[string]string{
		"coder":     "code-reviewer",
		"debugger":  "debug-reviewer",
		"planner":   "planner-reviewer",
		"analyst":   "analyst-reviewer",
		"committer": "code-reviewer",
	}
	for k, v := range expected {
		if m[k] != v {
			t.Errorf("mapping[%q] = %q, want %q", k, m[k], v)
		}
	}
}

func TestNewRequestReviewTool_NilMapping(t *testing.T) {
	tool := NewRequestReviewTool(nil, nil)
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.registry != nil {
		t.Error("expected nil registry")
	}
	if tool.reviewMapping == nil {
		t.Error("expected default mapping when nil provided")
	}
	if tool.reviewMapping["coder"] != "code-reviewer" {
		t.Errorf("expected coder->code-reviewer mapping, got %q", tool.reviewMapping["coder"])
	}
}

// --- Integration tests (Task 4) ---

func TestRequestReviewTool_Integration_CoderSpec(t *testing.T) {
	// Verify the coder spec includes request_review
	spec := agent.CoderAgentSpec()
	if !spec.HasTool("request_review") {
		t.Error("coder spec should include request_review in available tools")
	}
}

func TestRequestReviewTool_Integration_ReviewPolicyMapping(t *testing.T) {
	// Verify the tool's default mapping matches the ReviewPolicy reviewer mapping
	toolMapping := DefaultReviewerMapping()
	policy := agent.DefaultReviewPolicy()

	for callerID, expectedReviewer := range toolMapping {
		policyReviewer := policy.ReviewerMapping[callerID]
		if policyReviewer != "" && policyReviewer != expectedReviewer {
			t.Errorf("mapping mismatch for %q: tool=%q policy=%q",
				callerID, expectedReviewer, policyReviewer)
		}
	}
}

func TestRequestReviewTool_Integration_FullFlow(t *testing.T) {
	reg := &mockReviewRegistry{
		specs: testReviewSpecs(),
		response: `{
			"status": "rejected",
			"feedback": "Missing error handling in the new function",
			"issues": ["No nil check on the return value", "Missing context cancellation handling"],
			"confidence": 0.85
		}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":         "Implemented RequestReviewTool with Execute, Parameters, and parseResponse methods",
		"work_content":    "func (t *RequestReviewTool) Execute(ctx context.Context, args map[string]any) (any, error) { ... }",
		"caller_agent_id": "coder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.ReviewerID != "code-reviewer" {
		t.Errorf("expected code-reviewer, got %q", rr.ReviewerID)
	}
	if rr.Approved {
		t.Error("expected rejection")
	}
	if len(rr.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(rr.Issues), rr.Issues)
	}

	data, err := json.Marshal(rr)
	if err != nil {
		t.Fatalf("failed to serialize review result: %v", err)
	}
	var roundTripped InlineReviewResult
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("failed to deserialize: %v", err)
	}
	if roundTripped.ReviewerID != rr.ReviewerID {
		t.Error("round-trip mismatch: reviewer_id")
	}
}

func TestRequestReviewTool_Integration_DebuggerToDebugReviewer(t *testing.T) {
	reg := &mockReviewRegistry{
		specs:    testReviewSpecs(),
		response: `{"status": "approved", "feedback": "Root cause identified correctly", "issues": []}`,
	}
	tool := &RequestReviewTool{
		registry:      reg,
		reviewMapping: DefaultReviewerMapping(),
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"message":         "Fixed the race condition in the queue manager",
		"caller_agent_id": "debugger",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr, ok := result.(InlineReviewResult)
	if !ok {
		t.Fatalf("expected InlineReviewResult, got %T", result)
	}
	if rr.ReviewerID != "debug-reviewer" {
		t.Errorf("expected debug-reviewer for debugger, got %q", rr.ReviewerID)
	}
}
