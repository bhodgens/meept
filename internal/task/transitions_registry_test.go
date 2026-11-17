package task

// Issue #135 pins: every task-state WRITE surface must go through the
// taskTransitions guard (ValidTaskState + SetStateChecked), with exactly one
// explicit, audited operator escape hatch (UpdateStateUnchecked).
//
// The three surfaces under test:
//  1. Registry.UpdateState        (internal callers)
//  2. Handler.handleUpdate        (task.update bus contract, RPC/HTTP proxied)
//  3. TaskService.Update          (internal/services — pinned there)

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// newTestRegistryWithLogger is newTestRegistry with an explicit logger, so a
// test can capture the registry's own log output (the override audit line).
func newTestRegistryWithLogger(t *testing.T, logger *slog.Logger) *Registry {
	t.Helper()
	reg, err := NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	return reg
}

// mustCreateTask creates a task and fails the test on error.
func mustCreateTask(t *testing.T, reg *Registry, name string) *Task {
	t.Helper()
	task, err := reg.Create(context.Background(), name, "issue #135 pin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return task
}

// mustSetState drives a task to a source state through the GUARDED surface, so
// the staging itself proves the checked path accepts these edges.
func mustSetState(t *testing.T, reg *Registry, taskID string, state TaskState) {
	t.Helper()
	if err := reg.UpdateState(context.Background(), taskID, state); err != nil {
		t.Fatalf("UpdateState(%s): %v", state, err)
	}
}

// --- Surface 1: Registry.UpdateState ---------------------------------------

// TestUpdateState_IllegalEdgeRejected pins that the checked default refuses an
// edge that is not a member of taskTransitions (completed -> pending, the exact
// resurrection the issue describes) and leaves the persisted row untouched.
func TestUpdateState_IllegalEdgeRejected(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "illegal-edge")

	// pending -> executing -> testing -> completed (legal chain).
	mustSetState(t, reg, task.ID, StateExecuting)
	mustSetState(t, reg, task.ID, StateTesting)
	mustSetState(t, reg, task.ID, StateCompleted)

	err := reg.UpdateState(ctx, task.ID, StatePending)
	if err == nil {
		t.Fatal("UpdateState(completed->pending) = nil, want an illegal-transition error")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want ErrInvalidTransition", err)
	}

	got, getErr := reg.Get(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != StateCompleted {
		t.Errorf("state = %q, want completed — a rejected transition must not persist", got.State)
	}
}

// TestUpdateState_UnknownStateRejected pins state-STRING validation on the
// registry surface: an arbitrary string is refused even though it is not a
// legal transition FROM anywhere.
func TestUpdateState_UnknownStateRejected(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "unknown-state")

	err := reg.UpdateState(ctx, task.ID, TaskState("banana"))
	if err == nil {
		t.Fatal("UpdateState(pending->banana) = nil, want an unknown-state error")
	}
	if !errors.Is(err, ErrInvalidTaskState) {
		t.Errorf("error = %v, want ErrInvalidTaskState", err)
	}
	if errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want an unknown-STATE error, not a transition error", err)
	}

	got, getErr := reg.Get(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != StatePending {
		t.Errorf("state = %q, want pending — an unknown state must never persist", got.State)
	}
}

// TestUpdateState_SameStateIsNoOp keeps the established "same state is not a
// transition" contract: a same-state write succeeds and persists the row.
func TestUpdateState_SameStateIsNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "same-state")

	if err := reg.UpdateState(ctx, task.ID, StatePending); err != nil {
		t.Fatalf("UpdateState(pending->pending) = %v, want nil (same-state no-op)", err)
	}
}

// handleUpdatePayload builds a BusMessage the way the bus proxy/RPC path does.
func handleUpdatePayload(t *testing.T, params map[string]any) *models.BusMessage {
	t.Helper()
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &models.BusMessage{ID: "req-135", Payload: payload}
}

// --- Surface 2: Handler.handleUpdate (task.update bus contract) -------------

// TestHandleUpdate_IllegalEdgeRejected pins the bus-contract surface: the same
// completed -> pending write is refused, with no partial persistence.
func TestHandleUpdate_IllegalEdgeRejected(t *testing.T) {
	reg := newTestRegistry(t)
	h := NewHandler(reg, reg.bus, nil)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "bus-illegal-edge")

	// pending -> executing -> completed (legal chain) to reach terminal.
	mustSetState(t, reg, task.ID, StateExecuting)
	mustSetState(t, reg, task.ID, StateCompleted)

	_, err := h.handleUpdate(ctx, handleUpdatePayload(t, map[string]any{
		"id":    task.ID,
		"state": string(StatePending),
	}))
	if err == nil {
		t.Fatal("handleUpdate(completed->pending) = nil, want an illegal-transition error")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want ErrInvalidTransition", err)
	}

	got, getErr := reg.Get(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != StateCompleted {
		t.Errorf("state = %q, want completed — the bus path must not persist an illegal edge", got.State)
	}
}

// TestHandleUpdate_UnknownStateRejected pins state-string validation on the
// bus surface: `{"state":"done"}` is a typo, not a state.
func TestHandleUpdate_UnknownStateRejected(t *testing.T) {
	reg := newTestRegistry(t)
	h := NewHandler(reg, reg.bus, nil)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "bus-unknown-state")

	_, err := h.handleUpdate(ctx, handleUpdatePayload(t, map[string]any{
		"id":    task.ID,
		"state": "done",
	}))
	if err == nil {
		t.Fatal(`handleUpdate(state="done") = nil, want an unknown-state error`)
	}
	if !errors.Is(err, ErrInvalidTaskState) {
		t.Errorf("error = %v, want ErrInvalidTaskState", err)
	}

	got, getErr := reg.Get(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != StatePending {
		t.Errorf("state = %q, want pending — an unknown state string must never persist", got.State)
	}
}

// TestHandleUpdate_LegalEdgeStillWorks pins the positive direction: a legal
// edge through the bus contract still lands (gating must not break the
// ordinary update path).
func TestHandleUpdate_LegalEdgeStillWorks(t *testing.T) {
	reg := newTestRegistry(t)
	h := NewHandler(reg, reg.bus, nil)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "bus-legal-edge")

	res, err := h.handleUpdate(ctx, handleUpdatePayload(t, map[string]any{
		"id":    task.ID,
		"state": string(StateExecuting),
		"name":  "renamed",
	}))
	if err != nil {
		t.Fatalf("handleUpdate(pending->executing) = %v, want nil", err)
	}
	got, ok := res.(*Task)
	if !ok {
		t.Fatalf("result type = %T, want *Task", res)
	}
	if got.State != StateExecuting {
		t.Errorf("state = %q, want executing", got.State)
	}
	if got.Name != "renamed" {
		t.Errorf("name = %q, want renamed (non-state fields still apply)", got.Name)
	}
}

// --- The audited escape hatch ----------------------------------------------

// TestUpdateStateUnchecked_IsTheAuditedOverride pins the escape hatch the issue
// asks for: admin recovery may force an otherwise-illegal edge, and the override
// is logged at Warn naming the reason.
func TestUpdateStateUnchecked_IsTheAuditedOverride(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "override")

	// pending -> executing -> completed (legal chain) to reach terminal.
	mustSetState(t, reg, task.ID, StateExecuting)
	mustSetState(t, reg, task.ID, StateCompleted)

	if err := reg.UpdateStateUnchecked(ctx, task.ID, StatePending, "admin recovery: stuck task"); err != nil {
		t.Fatalf("UpdateStateUnchecked(completed->pending) = %v, want nil (explicit override)", err)
	}

	got, getErr := reg.Get(ctx, task.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != StatePending {
		t.Errorf("state = %q, want pending — the override must land", got.State)
	}
}

// TestUpdateStateUnchecked_LogsTheOverrideAtWarn pins the AUDIT half of the
// escape hatch: the override must leave a Warn record naming the edge and the
// reason. That log line is the only audit trail for an illegal edge, so it is
// asserted directly through a capturing slog handler.
func TestUpdateStateUnchecked_LogsTheOverrideAtWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	reg := newTestRegistryWithLogger(t, logger)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "override-audit")

	// pending -> executing -> completed (legal chain) to reach terminal.
	mustSetState(t, reg, task.ID, StateExecuting)
	mustSetState(t, reg, task.ID, StateCompleted)
	buf.Reset()

	if err := reg.UpdateStateUnchecked(ctx, task.ID, StatePending, "admin recovery: stuck task"); err != nil {
		t.Fatalf("UpdateStateUnchecked: %v", err)
	}

	var rec struct {
		Level  string `json:"level"`
		Msg    string `json:"msg"`
		TaskID string `json:"task_id"`
		From   string `json:"from"`
		To     string `json:"to"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("decode audit log %q: %v", buf.String(), err)
	}
	if rec.Level != "WARN" {
		t.Errorf("level = %q, want WARN", rec.Level)
	}
	if rec.From != string(StateCompleted) || rec.To != string(StatePending) {
		t.Errorf("logged edge = %s->%s, want completed->pending", rec.From, rec.To)
	}
	if rec.Reason != "admin recovery: stuck task" {
		t.Errorf("reason = %q, want the caller-supplied reason", rec.Reason)
	}
	if rec.TaskID != task.ID {
		t.Errorf("task_id = %q, want %q", rec.TaskID, task.ID)
	}
}

// TestUpdateStateUnchecked_RejectsUnknownState keeps the override honest: it
// bypasses the TRANSITION table, never the state enum.
func TestUpdateStateUnchecked_RejectsUnknownState(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()
	task := mustCreateTask(t, reg, "override-unknown")

	err := reg.UpdateStateUnchecked(ctx, task.ID, TaskState("nonsense"), "typo")
	if err == nil {
		t.Fatal("UpdateStateUnchecked(pending->nonsense) = nil, want an unknown-state error")
	}
	if !errors.Is(err, ErrInvalidTaskState) {
		t.Errorf("error = %v, want ErrInvalidTaskState", err)
	}
}
