package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// stubSubmitter is a test double for DispatchSubmitter.
type stubSubmitter struct {
	ack       DispatchJobAck
	ackErr    error
	status    JobStatus
	statusErr error
	results   []DispatchResult
	resultErr error

	lastReq DispatchJobRequest
}

func (s *stubSubmitter) Submit(_ context.Context, job DispatchJobRequest) (DispatchJobAck, error) {
	s.lastReq = job
	return s.ack, s.ackErr
}

func (s *stubSubmitter) Status(_ context.Context, jobID string) (JobStatus, error) {
	return s.status, s.statusErr
}

func (s *stubSubmitter) Results(_ context.Context, jobID string) ([]DispatchResult, error) {
	return s.results, s.resultErr
}

// --- handleSubmit ---

func TestDispatchHandler_Submit_Success(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{
		ack: DispatchJobAck{JobID: "job-1", Accepted: true, Message: "ok"},
	}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{
		"target_node": "node-a",
		"agent_id": "coder",
		"task_description": "refactor foo"
	}`)
	result, err := h.handleSubmit(context.Background(), params)
	if err != nil {
		t.Fatalf("handleSubmit() unexpected error: %v", err)
	}

	ack, ok := result.(DispatchJobAck)
	if !ok {
		t.Fatalf("expected DispatchJobAck, got %T", result)
	}
	if ack.JobID != "job-1" || !ack.Accepted {
		t.Errorf("unexpected ack: %+v", ack)
	}
	if stub.lastReq.TargetNode != "node-a" {
		t.Errorf("expected target node 'node-a', got %q", stub.lastReq.TargetNode)
	}
	if stub.lastReq.AgentID != "coder" {
		t.Errorf("expected agent 'coder', got %q", stub.lastReq.AgentID)
	}
}

func TestDispatchHandler_Submit_NilSubmitter(t *testing.T) {
	t.Parallel()
	h := NewDispatchHandler(nil, nil)

	params := json.RawMessage(`{"target_node":"n","agent_id":"a","task_description":"t"}`)
	_, err := h.handleSubmit(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when submitter is nil")
	}
	if err.Error() != "dispatch feature not enabled" {
		t.Errorf("expected 'dispatch feature not enabled', got %q", err.Error())
	}
}

func TestDispatchHandler_Submit_MissingTargetNode(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ack: DispatchJobAck{Accepted: true}}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"agent_id":"a","task_description":"t"}`)
	_, err := h.handleSubmit(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing target_node")
	}
}

func TestDispatchHandler_Submit_MissingAgentID(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ack: DispatchJobAck{Accepted: true}}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"target_node":"n","task_description":"t"}`)
	_, err := h.handleSubmit(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing agent_id")
	}
}

func TestDispatchHandler_Submit_MissingTaskDescription(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ack: DispatchJobAck{Accepted: true}}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"target_node":"n","agent_id":"a"}`)
	_, err := h.handleSubmit(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing task_description")
	}
}

func TestDispatchHandler_Submit_InvalidJSON(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ack: DispatchJobAck{Accepted: true}}
	h := NewDispatchHandler(stub, nil)

	_, err := h.handleSubmit(context.Background(), json.RawMessage(`{bad json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDispatchHandler_Submit_SubmitterError(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ackErr: errors.New("network unreachable")}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"target_node":"n","agent_id":"a","task_description":"t"}`)
	_, err := h.handleSubmit(context.Background(), params)
	if err == nil {
		t.Fatal("expected error from submitter")
	}
}

// --- handleStatus ---

func TestDispatchHandler_Status_Success(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{
		status: JobStatus{JobID: "job-1", State: "running"},
	}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"job_id":"job-1"}`)
	result, err := h.handleStatus(context.Background(), params)
	if err != nil {
		t.Fatalf("handleStatus() unexpected error: %v", err)
	}

	status, ok := result.(JobStatus)
	if !ok {
		t.Fatalf("expected JobStatus, got %T", result)
	}
	if status.State != "running" {
		t.Errorf("expected state 'running', got %q", status.State)
	}
}

func TestDispatchHandler_Status_NilSubmitter(t *testing.T) {
	t.Parallel()
	h := NewDispatchHandler(nil, nil)

	params := json.RawMessage(`{"job_id":"job-1"}`)
	_, err := h.handleStatus(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when submitter is nil")
	}
}

func TestDispatchHandler_Status_MissingJobID(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{}`)
	_, err := h.handleStatus(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for missing job_id")
	}
}

// --- handleResults ---

func TestDispatchHandler_Results_Success(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{
		results: []DispatchResult{{JobID: "job-1", OutputRef: "blake3:abc"}},
	}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"job_id":"job-1"}`)
	result, err := h.handleResults(context.Background(), params)
	if err != nil {
		t.Fatalf("handleResults() unexpected error: %v", err)
	}

	results, ok := result.([]DispatchResult)
	if !ok {
		t.Fatalf("expected []DispatchResult, got %T", result)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestDispatchHandler_Results_NilReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{results: nil}
	h := NewDispatchHandler(stub, nil)

	params := json.RawMessage(`{"job_id":"job-1"}`)
	result, err := h.handleResults(context.Background(), params)
	if err != nil {
		t.Fatalf("handleResults() unexpected error: %v", err)
	}

	results, ok := result.([]DispatchResult)
	if !ok {
		t.Fatalf("expected []DispatchResult, got %T", result)
	}
	if len(results) != 0 {
		t.Errorf("expected empty slice, got %d items", len(results))
	}
}

func TestDispatchHandler_Results_NilSubmitter(t *testing.T) {
	t.Parallel()
	h := NewDispatchHandler(nil, nil)

	params := json.RawMessage(`{"job_id":"job-1"}`)
	_, err := h.handleResults(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when submitter is nil")
	}
}

// --- DispatchTaskViaNode ---

func TestDispatchTaskViaNode_Success(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{
		ack: DispatchJobAck{JobID: "job-2", Accepted: true},
	}
	ack, err := DispatchTaskViaNode(context.Background(), stub, "node-b", "analyst", "review code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ack.JobID != "job-2" || !ack.Accepted {
		t.Errorf("unexpected ack: %+v", ack)
	}
	if stub.lastReq.TargetNode != "node-b" {
		t.Errorf("expected target node 'node-b', got %q", stub.lastReq.TargetNode)
	}
	if stub.lastReq.AgentID != "analyst" {
		t.Errorf("expected agent 'analyst', got %q", stub.lastReq.AgentID)
	}
}

func TestDispatchTaskViaNode_NilSubmitter(t *testing.T) {
	t.Parallel()
	_, err := DispatchTaskViaNode(context.Background(), nil, "n", "a", "t")
	if err == nil {
		t.Fatal("expected error when submitter is nil")
	}
}

func TestDispatchTaskViaNode_EmptyArgs(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{}

	if _, err := DispatchTaskViaNode(context.Background(), stub, "", "a", "t"); err == nil {
		t.Error("expected error for empty nodeID")
	}
	if _, err := DispatchTaskViaNode(context.Background(), stub, "n", "", "t"); err == nil {
		t.Error("expected error for empty agentID")
	}
	if _, err := DispatchTaskViaNode(context.Background(), stub, "n", "a", ""); err == nil {
		t.Error("expected error for empty task")
	}
}

// --- SplitNodePrefixedAgentID ---

func TestSplitNodePrefixedAgentID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input      string
		wantNode   string
		wantAgent  string
		wantOK     bool
	}{
		{"node:abc-123:coder", "abc-123", "coder", true},
		{"node:xyz:debugger", "xyz", "debugger", true},
		{"node:abc-123", "abc-123", "", true},
		{"coder", "", "", false},
		{"", "", "", false},
		{"node:", "", "", true},
	}
	for _, tc := range cases {
		nodeID, agentID, ok := SplitNodePrefixedAgentID(tc.input)
		if ok != tc.wantOK {
			t.Errorf("SplitNodePrefixedAgentID(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if nodeID != tc.wantNode {
			t.Errorf("SplitNodePrefixedAgentID(%q) nodeID = %q, want %q", tc.input, nodeID, tc.wantNode)
		}
		if agentID != tc.wantAgent {
			t.Errorf("SplitNodePrefixedAgentID(%q) agentID = %q, want %q", tc.input, agentID, tc.wantAgent)
		}
	}
}

// --- RegisterDispatchMethods ---

func TestRegisterDispatchMethods(t *testing.T) {
	t.Parallel()
	stub := &stubSubmitter{ack: DispatchJobAck{JobID: "j", Accepted: true}}
	h := NewDispatchHandler(stub, nil)

	// Use a minimal in-process server to verify registration.
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterDispatchMethods(srv)

	// Verify methods are registered by dispatching through CallMethod.
	// We don't call Start; just verify the handlers map.
	srv.mu.RLock()
	_, hasSubmit := srv.handlers["dispatch.submit"]
	_, hasStatus := srv.handlers["dispatch.status"]
	_, hasResults := srv.handlers["dispatch.results"]
	srv.mu.RUnlock()

	if !hasSubmit {
		t.Error("dispatch.submit not registered")
	}
	if !hasStatus {
		t.Error("dispatch.status not registered")
	}
	if !hasResults {
		t.Error("dispatch.results not registered")
	}
}

// --- SetSubmitter ---

func TestSetSubmitter(t *testing.T) {
	t.Parallel()
	h := NewDispatchHandler(nil, nil)

	// Initially nil — should error.
	_, err := h.submitterOrErr()
	if err == nil {
		t.Fatal("expected error before SetSubmitter")
	}

	// Set a non-nil submitter.
	stub := &stubSubmitter{ack: DispatchJobAck{Accepted: true}}
	h.SetSubmitter(stub)

	_, err = h.submitterOrErr()
	if err != nil {
		t.Fatalf("expected no error after SetSubmitter, got: %v", err)
	}

	// Setting nil should be a no-op (nil-guard).
	h.SetSubmitter(nil)
	_, err = h.submitterOrErr()
	if err != nil {
		t.Fatalf("SetSubmitter(nil) should not clear existing submitter")
	}
}
