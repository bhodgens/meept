# Agentic Pairs: Option D — Tool-Based Pairing (Inline Review Tool)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the actor agent (coder) a `request_review` tool that synchronously invokes the reviewer agent inline during its own execution, enabling lightweight self-initiated review within a single agent loop iteration.

**Architecture:** A new RequestReviewTool implements the standard Tool interface. When called, it invokes the appropriate reviewer agent via the existing delegateRegistry (same mechanism as delegate_task). The reviewer's structured response returns as a tool result in the actor's conversation, so the actor can immediately address feedback within the same RunOnce() cycle. The coder's system prompt is updated to encourage calling request_review after completing logical units.

**Tech Stack:** Go 1.22+, existing Tool interface, delegateRegistry, AgentRegistry

---

## Phase 1: Constants and Permission Wiring

### Task 1: Add tool name constant and ToolActionMap entry

Add the `ToolRequestReview` constant alongside existing tool constants, and add a permission mapping entry in ToolActionMap so the executor knows how to categorize it.

**File:** `internal/agent/cache.go`

Add `ToolRequestReview` to the const block (after the `ToolCodeRead` entry):

```go
	ToolRequestReview  = "request_review"
```

The full const block becomes:

```go
const (
	ToolFileRead         = "file_read"
	ToolFileWrite        = "file_write"
	ToolFileDelete       = "file_delete"
	ToolShellExecute     = "shell_execute"
	ToolListDirectory    = "list_directory"
	ToolMemorySearch     = "memory_search"
	ToolMemoryGetContext = "memory_get_context"
	ToolMemoryStore      = "memory_store"
	ToolMemoryDelete     = "memory_delete"
	ToolPlatformStatus   = "platform_status"
	ToolPlatformAgents   = "platform_agents"
	ToolPlatformTools    = "platform_tools"
	ToolWebSearch        = "web_search"
	ToolWebFetch         = "web_fetch"
	ToolCodeRead         = "code_read"
	ToolRequestReview    = "request_review"
)
```

**File:** `internal/agent/executor.go`

Add the permission mapping in `ToolActionMap` (after the `"delegate_task": "agent_delegate"` entry):

```go
	// Agent delegation
	"delegate_task":  "agent_delegate",
	ToolRequestReview: "agent_delegate",
```

**Verify:**

```bash
go build ./internal/agent/...
```

- [x] `ToolRequestReview` constant added to `internal/agent/cache.go`
- [x] `ToolRequestReview` mapped to `"agent_delegate"` in `ToolActionMap` in `internal/agent/executor.go`
- [x] `go build ./internal/agent/...` passes

---

## Phase 2: RequestReviewTool Implementation

### Task 2: Implement RequestReviewTool (with tests)

Create the tool implementation and its test file. The tool reuses the existing `delegateRegistry` interface (already defined in `platform.go`) so it can be tested with the same mock pattern as `DelegateTaskTool`.

**File:** `internal/tools/builtin/review_tools.go`

```go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// RequestReviewTool invokes the appropriate reviewer agent inline during
// an actor agent's execution. It uses the same delegateRegistry mechanism as
// DelegateTaskTool so that the reviewer's structured feedback returns as a
// tool result in the actor's conversation.
//
// The caller specifies the work to review. If reviewer_id is not provided,
// the tool auto-selects a reviewer based on the caller's agent type using the
// default reviewer mapping (coder -> code-reviewer, debugger -> debug-reviewer, etc.).
type RequestReviewTool struct {
	registry      delegateRegistry
	reviewMapping map[string]string // caller agent ID -> default reviewer ID
}

// NewRequestReviewTool creates a new request review tool.
// The registry is the same AgentRegistry used by DelegateTaskTool.
// reviewMapping maps caller agent IDs to default reviewer agent IDs
// (e.g., "coder" -> "code-reviewer"). If nil, DefaultReviewerMapping is used.
func NewRequestReviewTool(registry delegateRegistry, reviewMapping map[string]string) *RequestReviewTool {
	if reviewMapping == nil {
		reviewMapping = DefaultReviewerMapping()
	}
	return &RequestReviewTool{
		registry:      registry,
		reviewMapping: reviewMapping,
	}
}

// DefaultReviewerMapping returns the standard mapping from actor agent IDs to
// their paired reviewer agent IDs.
func DefaultReviewerMapping() map[string]string {
	return map[string]string{
		"coder":     "code-reviewer",
		"debugger":  "debug-reviewer",
		"planner":   "planner-reviewer",
		"analyst":   "analyst-reviewer",
		"committer": "code-reviewer",
	}
}

func (t *RequestReviewTool) Name() string { return "request_review" }

func (t *RequestReviewTool) Description() string {
	return "Request an inline code review from a reviewer agent. Call this after completing a logical unit of work (e.g., after writing a file, after a set of changes). Returns structured feedback: approved/rejected with specific issues. If rejected, address the feedback and continue."
}

func (t *RequestReviewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropMessage: {
				Type:        schemaTypeString,
				Description: "Description of the work to review. Include what was done, what files were changed, and what the intended outcome is.",
			},
			"work_content": {
				Type:        schemaTypeString,
				Description: "Optional: the actual code or content to review. Include this for best results.",
			},
			"reviewer_id": {
				Type:        schemaTypeString,
				Description: "Optional: specific reviewer agent ID (e.g., 'code-reviewer'). If omitted, the default reviewer for this agent type is used.",
			},
			"caller_agent_id": {
				Type:        schemaTypeString,
				Description: "The ID of the agent calling this tool (e.g., 'coder'). Used to select the default reviewer.",
			},
		},
		Required: []string{schemaPropMessage},
	}
}

// InlineReviewResult is the structured result returned by RequestReviewTool.
type InlineReviewResult struct {
	ReviewerID string   `json:"reviewer_id"`
	Status     string   `json:"status"` // "approved", "rejected", "needs_info"
	Feedback   string   `json:"feedback"`
	Issues     []string `json:"issues,omitempty"`
	Approved   bool     `json:"approved"`
}

func (t *RequestReviewTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return tools.NewErrorResult("agent registry not available"), nil
	}

	message, _ := args[schemaPropMessage].(string)
	if message == "" {
		return tools.NewErrorResult("message is required: describe the work to review"), nil
	}

	workContent, _ := args["work_content"].(string)
	reviewerID, _ := args["reviewer_id"].(string)
	callerAgentID, _ := args["caller_agent_id"].(string)

	// Resolve reviewer: explicit > mapping > fallback
	if reviewerID == "" {
		reviewerID = t.resolveReviewer(callerAgentID)
	}

	// Verify the reviewer agent exists
	spec, ok := t.registry.GetSpec(reviewerID)
	if !ok {
		available := t.availableReviewers()
		return tools.NewErrorResult(fmt.Sprintf(
			"reviewer agent not found: %s. Available reviewers: %s",
			reviewerID, joinStrings(available),
		)), nil
	}

	// Build the review request prompt
	reviewPrompt := t.buildReviewPrompt(message, workContent, callerAgentID)

	// Generate an isolated conversation ID for this review
	conversationID := "review-" + spec.ID + "-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Invoke the reviewer synchronously (same mechanism as delegate_task)
	response, err := t.registry.RunAgent(ctx, spec.ID, reviewPrompt, conversationID)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("reviewer execution failed: %v", err)), nil
	}

	// Parse the reviewer's structured JSON response
	return t.parseResponse(reviewerID, response)
}

// resolveReviewer returns the reviewer ID for a given caller agent.
// Falls back to "code-reviewer" if no mapping exists.
func (t *RequestReviewTool) resolveReviewer(callerAgentID string) string {
	if callerAgentID != "" {
		if mapped, ok := t.reviewMapping[callerAgentID]; ok {
			return mapped
		}
	}
	return "code-reviewer"
}

// availableReviewers returns a list of agent IDs that look like reviewers.
func (t *RequestReviewTool) availableReviewers() []string {
	specs := t.registry.ListSpecs()
	var reviewers []string
	for _, s := range specs {
		if s.Role == agent.RoleReviewer {
			reviewers = append(reviewers, s.ID)
		}
	}
	if len(reviewers) == 0 {
		return []string{"(none registered)"}
	}
	return reviewers
}

// buildReviewPrompt constructs the message sent to the reviewer agent.
func (t *RequestReviewTool) buildReviewPrompt(message, workContent, callerAgentID string) string {
	prompt := "## Inline Review Request\n\n"
	if callerAgentID != "" {
		prompt += fmt.Sprintf("**Requesting agent:** %s\n\n", callerAgentID)
	}
	prompt += fmt.Sprintf("## Work Description\n%s\n\n", message)
	if workContent != "" {
		prompt += fmt.Sprintf("## Content to Review\n```\n%s\n```\n\n", workContent)
	}
	prompt += "Review this work for correctness, style, security, and completeness. " +
		"Respond with JSON: {\"status\": \"approved\"|\"rejected\"|\"needs_info\", " +
		"\"feedback\": \"...\", \"issues\": [...]}"
	return prompt
}

// parseResponse extracts the structured review result from the reviewer's response.
func (t *RequestReviewTool) parseResponse(reviewerID, response string) (any, error) {
	// Try to extract JSON from the response (may be wrapped in code block)
	data, extractErr := ExtractJSONFromText(response)
	if extractErr != nil {
		// If the reviewer didn't return structured JSON, wrap the raw text
		return InlineReviewResult{
			ReviewerID: reviewerID,
			Status:     "needs_info",
			Feedback:   response,
			Approved:   false,
		}, nil
	}

	status, _ := data["status"].(string)
	feedback, _ := data["feedback"].(string)
	approved := status == "approved"

	var issues []string
	if rawIssues, ok := data["issues"]; ok {
		if issueSlice, ok := rawIssues.([]any); ok {
			for _, iss := range issueSlice {
				if s, ok := iss.(string); ok {
					issues = append(issues, s)
				}
			}
		}
	}

	return InlineReviewResult{
		ReviewerID: reviewerID,
		Status:     status,
		Feedback:   feedback,
		Issues:     issues,
		Approved:   approved,
	}, nil
}

// Ensure RequestReviewTool implements the Tool interface.
var _ tools.Tool = (*RequestReviewTool)(nil)
```

**File:** `internal/tools/builtin/review_tools_test.go`

```go
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
		specs: testReviewSpecs(),
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
		specs: testReviewSpecs(),
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
		specs: testReviewSpecs(),
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
		specs: testReviewSpecs(),
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
```

**Verify:**

```bash
go test ./internal/tools/builtin/... -run TestRequestReview -v
go vet ./internal/tools/builtin/...
```

- [x] `internal/tools/builtin/review_tools.go` created with RequestReviewTool
- [x] `internal/tools/builtin/review_tools_test.go` created with 11 test cases
- [x] All tests pass: `go test ./internal/tools/builtin/... -run TestRequestReview -v`

---

## Phase 3: Wire Tool into Agent Ecosystem

### Task 3: Register tool and add to coder spec

Wire the tool into the daemon's component registration and add `request_review` to the coder agent's AdditionalTools.

**File:** `internal/agent/spec.go`

Modify `CoderAgentSpec()` to add `ToolRequestReview` to AdditionalTools:

```go
func CoderAgentSpec() *AgentSpec {
	constraints := DefaultConstraints()
	constraints.Temperature = ptr(0.3) // Low for deterministic code
	return &AgentSpec{
		ID:   "coder",
		Name: "Coder Agent",
		Role: RoleExecutor,
		Purpose: `You are a coding specialist. You can read, write, and modify files, execute shell commands, and work with MCP servers.

## Review Workflow

After completing each logical unit of work (e.g., implementing a function, fixing a bug, modifying a module), call request_review with:
1. A description of what was done
2. The content of the changes (in work_content)
3. Your agent ID as caller_agent_id ("coder")

The reviewer will return approved/rejected with specific feedback. If rejected, address the feedback immediately and call request_review again for the revised work.`,
		Model: "",
		AdditionalTools: []string{
			ToolFileRead,
			ToolFileWrite,
			ToolFileDelete,
			ToolListDirectory,
			ToolShellExecute,
			ToolRequestReview,
		},
		Constraints: constraints,
	}
}
```

**File:** `internal/daemon/components.go`

In the tool registration function where `DelegateTaskTool` is registered, add registration of the RequestReviewTool after it:

```go
	// Delegate task tool (for multi-agent routing)
	registry.Register(builtin.NewDelegateTaskTool(agentRegistry))

	// Request review tool (inline review during agent execution)
	registry.Register(builtin.NewRequestReviewTool(agentRegistry, nil))
```

**Verify:**

```bash
go build ./internal/agent/...
go build ./internal/daemon/...
```

- [x] `ToolRequestReview` added to `CoderAgentSpec().AdditionalTools` in `internal/agent/spec.go`
- [x] `NewRequestReviewTool` registered in `internal/daemon/components.go`
- [x] `go build ./internal/daemon/...` passes

---

## Phase 4: Integration Tests

### Task 4: Integration tests for coder spec and policy alignment

**File:** `internal/tools/builtin/review_tools_test.go`

Append these integration tests to the existing test file:

```go
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
		specs: testReviewSpecs(),
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
```

**Verify:**

```bash
go test ./internal/tools/builtin/... -run "TestRequestReview" -v
go test ./internal/agent/... -run "TestCoder" -v
```

- [x] Integration test `TestRequestReviewTool_Integration_CoderSpec` verifies coder spec has tool
- [x] Integration test `TestRequestReviewTool_Integration_ReviewPolicyMapping` verifies mapping alignment
- [x] Integration test `TestRequestReviewTool_Integration_FullFlow` validates end-to-end flow
- [x] Integration test `TestRequestReviewTool_Integration_DebuggerToDebugReviewer` validates debugger routing
- [x] All tests pass

---

## Final Verification

After all tasks are complete, run the full test suite and build:

```bash
go test ./internal/agent/... ./internal/tools/builtin/... -v
go build ./...
```

- [x] `go test ./internal/agent/... ./internal/tools/builtin/... -v` passes
- [x] `go build ./...` passes
