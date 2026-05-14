package task

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

func TestAmendmentManager_RegisterHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)
	mgr := NewAmendmentManager(msgBus, logger)
	defer mgr.Close()

	called := false
	handler := func(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
		called = true
		return &AmendmentReply{Success: true}, nil
	}

	mgr.RegisterHandler(AmendmentInjectContext, handler)

	req := NewAmendmentRequest("task-1", AmendmentInjectContext, "test content")
	if err := mgr.Submit(context.Background(), req); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	reply, err := mgr.Process(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !reply.Success {
		t.Error("Expected success")
	}
	if !called {
		t.Error("Handler should have been called")
	}
}

func TestAmendmentManager_GetPending(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)
	mgr := NewAmendmentManager(msgBus, logger)
	defer mgr.Close()

	req := NewAmendmentRequest("task-1", AmendmentInjectContext, "test")
	if err := mgr.Submit(context.Background(), req); err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	found, ok := mgr.GetPending(req.ID)
	if !ok {
		t.Fatal("Should find pending request")
	}
	if found.ID != req.ID {
		t.Errorf("Wrong request ID: %s", found.ID)
	}
}

func TestAmendmentManager_GetPendingForTask(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)
	mgr := NewAmendmentManager(msgBus, logger)
	defer mgr.Close()

	req1 := NewAmendmentRequest("task-1", AmendmentInjectContext, "test1")
	req2 := NewAmendmentRequest("task-1", AmendmentSkipStep, "test2")
	req3 := NewAmendmentRequest("task-2", AmendmentAddStep, "test3")

	_ = mgr.Submit(context.Background(), req1)
	_ = mgr.Submit(context.Background(), req2)
	_ = mgr.Submit(context.Background(), req3)

	pending := mgr.GetPendingForTask("task-1")
	if len(pending) != 2 {
		t.Fatalf("Expected 2 pending for task-1, got %d", len(pending))
	}
}

func TestAmendmentManager_CancelPendingForTask(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)
	mgr := NewAmendmentManager(msgBus, logger)
	defer mgr.Close()

	req1 := NewAmendmentRequest("task-1", AmendmentInjectContext, "test1")
	req2 := NewAmendmentRequest("task-1", AmendmentSkipStep, "test2")

	_ = mgr.Submit(context.Background(), req1)
	_ = mgr.Submit(context.Background(), req2)

	mgr.CancelPendingForTask("task-1")

	pending := mgr.GetPendingForTask("task-1")
	if len(pending) != 0 {
		t.Fatalf("Expected 0 pending after cancel, got %d", len(pending))
	}

	r1, ok := mgr.GetPending(req1.ID)
	if !ok {
		t.Fatal("Should find request")
	}
	if r1.Status != AmendmentIgnored {
		t.Errorf("Expected Ignored status, got %v", r1.Status)
	}
}

func TestAmendmentManager_NoHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	msgBus := bus.New(nil, logger)
	mgr := NewAmendmentManager(msgBus, logger)
	defer mgr.Close()

	req := NewAmendmentRequest("task-1", AmendmentInjectContext, "test")
	_ = mgr.Submit(context.Background(), req)

	reply, err := mgr.Process(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if reply.Success {
		t.Error("Expected failure when no handler registered")
	}
	if req.Status != AmendmentRejected {
		t.Errorf("Expected Rejected status, got %v", req.Status)
	}
}

func TestAmendmentReply_Marshal(t *testing.T) {
	reply := &AmendmentReply{
		RequestID: "req-1",
		Success:   true,
		Message:   "Success message",
		Metadata:  json.RawMessage(`{"key":"value"}`),
	}

	data, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var unmarshaled AmendmentReply
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if unmarshaled.RequestID != reply.RequestID {
		t.Errorf("RequestID mismatch: %s vs %s", unmarshaled.RequestID, reply.RequestID)
	}
	if !unmarshaled.Success {
		t.Error("Success should be true")
	}
}
