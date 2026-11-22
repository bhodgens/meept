package rpc

// 01-safety TEST GATE T1 (00T-gate.md): reject honesty.
//
// W1 acceptance: task.reject must REJECT. The current handler has only an
// ApproveFunc seam and handleReject calls it (internal/rpc/task_approval.go),
// so a rejection APPROVES and executes the plan today. These tests pin the
// NEW RejectFunc seam the 01-reject-honesty leaf adds beside ApproveFunc:
//
//   - RejectFunc(ctx, taskID, reason) receives the rejection reason
//     (StrategicPlanner.RejectPlan is the production wiring);
//   - ApproveFunc is NEVER called on task.reject;
//   - the RPC response stays {task_id, status:"rejected"} (C2).
//
// RED today: undefined RejectFunc field; TestRejectDoesNotApprove also
// demonstrates the live defect (ApproveFunc IS called by handleReject).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRejectDoesNotApprove is the branch's load-bearing pin (W1): with
// distinct approve/reject spies wired, task.reject must call ONLY the
// reject seam, with the reason, and answer status=rejected.
func TestRejectDoesNotApprove(t *testing.T) {
	var approvedIDs []string
	var rejectedIDs []string
	var rejectedReasons []string

	h := NewTaskApprovalHandler(func(ctx context.Context, taskID string) error {
		approvedIDs = append(approvedIDs, taskID)
		return nil
	})
	h.RejectFunc = func(ctx context.Context, taskID, reason string) error {
		rejectedIDs = append(rejectedIDs, taskID)
		rejectedReasons = append(rejectedReasons, reason)
		return nil
	}

	res, err := dispatchTaskApproval(t, h, "task.reject", map[string]any{
		"task_id": "task-w1",
		"reason":  "plan touches production; do not execute",
	})
	if err != nil {
		t.Fatalf("task.reject: %v", err)
	}
	raw, merr := json.Marshal(res)
	if merr != nil {
		t.Fatalf("marshal response: %v", merr)
	}
	var body struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if uerr := json.Unmarshal(raw, &body); uerr != nil {
		t.Fatalf("decode response %s: %v", raw, uerr)
	}
	if body.TaskID != "task-w1" || body.Status != "rejected" {
		t.Errorf("response = %s, want task_id=task-w1 status=rejected (C2)", raw)
	}
	if len(approvedIDs) != 0 {
		t.Errorf("CURRENT DEFECT: task.reject called ApproveFunc with %v; a rejection must never approve/execute the plan (W1)", approvedIDs)
	}
	if len(rejectedIDs) != 1 || rejectedIDs[0] != "task-w1" {
		t.Errorf("RejectFunc calls = %v, want exactly [task-w1]", rejectedIDs)
	}
	if len(rejectedReasons) != 1 || rejectedReasons[0] != "plan touches production; do not execute" {
		t.Errorf("RejectFunc reasons = %v, want the full rejection reason forwarded", rejectedReasons)
	}
}

// TestRejectWithoutRejectFuncErrors pins honest degradation: a handler
// wired with ONLY an approver (the current daemon wiring shape) must
// refuse task.reject with an actionable error instead of silently
// approving through ApproveFunc.
func TestRejectWithoutRejectFuncErrors(t *testing.T) {
	var approvedIDs []string
	h := NewTaskApprovalHandler(func(ctx context.Context, taskID string) error {
		approvedIDs = append(approvedIDs, taskID)
		return nil
	})

	_, err := dispatchTaskApproval(t, h, "task.reject", map[string]any{
		"task_id": "task-w1-nilseam",
		"reason":  "no",
	})
	if err == nil {
		t.Error("task.reject without a RejectFunc seam must error, never fall back to approval")
	} else if !strings.Contains(err.Error(), "reject") {
		t.Errorf("error %q should name the unavailable rejection service", err)
	}
	if len(approvedIDs) != 0 {
		t.Errorf("CURRENT DEFECT: ApproveFunc called on reject with no reject seam: %v", approvedIDs)
	}
}

// TestRejectPropagatesSeamError pins error honesty: when the reject seam
// (StrategicPlanner.RejectPlan) refuses (task not awaiting approval, store
// error), the RPC surfaces the error and never reports rejected.
func TestRejectPropagatesSeamError(t *testing.T) {
	h := NewTaskApprovalHandler(func(ctx context.Context, taskID string) error {
		t.Errorf("ApproveFunc called during a failing reject for %s", taskID)
		return nil
	})
	h.RejectFunc = func(ctx context.Context, taskID, reason string) error {
		return context.DeadlineExceeded
	}

	if _, err := dispatchTaskApproval(t, h, "task.reject", map[string]any{
		"task_id": "task-w1-err",
		"reason":  "no",
	}); err == nil {
		t.Error("task.reject must surface the RejectFunc error, not report success")
	}
}
