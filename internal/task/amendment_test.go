package task

import (
	"testing"
)

func TestNewAmendmentRequest(t *testing.T) {
	req := NewAmendmentRequest("task-1", AmendmentInjectContext, "skip the tests")

	if req.TaskID != "task-1" {
		t.Errorf("wrong task ID: %s", req.TaskID)
	}
	if req.Type != AmendmentInjectContext {
		t.Errorf("wrong type: %v", req.Type)
	}
	if req.Content != "skip the tests" {
		t.Errorf("wrong content: %s", req.Content)
	}
	if req.Status != AmendmentPending {
		t.Errorf("wrong status: %v", req.Status)
	}
	if req.ID == "" {
		t.Error("ID should not be empty")
	}
	if req.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestAmendmentType_String(t *testing.T) {
	tests := []struct {
		typ  AmendmentType
		want string
	}{
		{AmendmentInjectContext, "inject_context"},
		{AmendmentReprioritize, "reprioritize"},
		{AmendmentSkipStep, "skip_step"},
		{AmendmentAddStep, "add_step"},
		{AmendmentChangeAgent, "change_agent"},
		{AmendmentCancelTask, "cancel_task"},
	}

	for _, tt := range tests {
		if tt.typ.String() != tt.want {
			t.Errorf("%v.String() = %q, want %q", tt.typ, tt.typ.String(), tt.want)
		}
	}
}

func TestAmendmentStatus_String(t *testing.T) {
	tests := []struct {
		status AmendmentStatus
		want   string
	}{
		{AmendmentPending, "pending"},
		{AmendmentApplied, "applied"},
		{AmendmentRejected, "rejected"},
		{AmendmentIgnored, "ignored"},
	}

	for _, tt := range tests {
		if tt.status.String() != tt.want {
			t.Errorf("%v.String() = %q, want %q", tt.status, tt.status.String(), tt.want)
		}
	}
}
