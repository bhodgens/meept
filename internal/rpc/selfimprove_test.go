package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/selfimprove"
)

func newTestSelfImproveHandler(t *testing.T) (*SelfImproveHandler, *bus.MessageBus) {
	t.Helper()
	logger := slog.Default()
	msgBus := bus.New(nil, logger)
	cfg := selfimprove.DefaultConfig()
	cfg.DataPath = t.TempDir()
	ctrl := selfimprove.NewController(cfg, msgBus, nil, t.TempDir(), logger)
	_ = ctrl.Initialize(context.Background())
	return NewSelfImproveHandler(ctrl), msgBus
}

func newNilSelfImproveHandler() *SelfImproveHandler {
	return NewSelfImproveHandler(nil)
}

func TestSelfImproveHandler_NilController(t *testing.T) {
	h := newNilSelfImproveHandler()
	server := New(&Config{}, nil, slog.Default())
	h.RegisterSelfImproveMethods(server)

	methods := []string{
		"selfimprove.detect",
		"selfimprove.analyze",
		"selfimprove.generate",
		"selfimprove.validate",
		"selfimprove.apply",
		"selfimprove.reject",
		"selfimprove.status",
		"selfimprove.cycle",
	}
	for _, method := range methods {
		_, err := server.handlers[method](context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Errorf("expected error for %s with nil controller", method)
		}
	}
}

func TestSelfImproveHandler_Status(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	result, err := h.handleStatus(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleStatus: %v", err)
	}
	status := result.(*selfimprove.ControllerStatus)
	if status.CircuitBreakerTripped {
		t.Error("circuit breaker should not be tripped on fresh controller")
	}
}

func TestSelfImproveHandler_Detect(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	result, err := h.handleDetect(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleDetect: %v", err)
	}
	resp := result.(map[string]any)
	count, _ := resp["count"].(int)
	if count != 0 {
		t.Logf("detect returned %d issues (expected 0 for empty dir)", count)
	}
}

func TestSelfImproveHandler_Apply_MissingFixID(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	_, err := h.handleApply(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing fix_id")
	}
}

func TestSelfImproveHandler_Apply_UnknownFixID(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	_, err := h.handleApply(context.Background(), json.RawMessage(`{"fix_id":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for unknown fix_id")
	}
}

func TestSelfImproveHandler_Reject_MissingFixID(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	_, err := h.handleReject(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing fix_id")
	}
}

func TestSelfImproveHandler_Reject_UnknownFixID(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	_, err := h.handleReject(context.Background(), json.RawMessage(`{"fix_id":"nonexistent","reason":"test"}`))
	if err == nil {
		t.Fatal("expected error for unknown fix_id")
	}
}

func TestSelfImproveHandler_Cycle(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	result, err := h.handleCycle(context.Background(), json.RawMessage(`{"interactive":false}`))
	if err != nil {
		t.Fatalf("handleCycle: %v", err)
	}
	cycle := result.(*selfimprove.ImprovementCycle)
	if cycle.Status != selfimprove.CycleStatusCompleted {
		t.Errorf("expected completed, got %s", cycle.Status)
	}
}

func TestSelfImproveHandler_Generate(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	result, err := h.handleGenerate(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleGenerate: %v", err)
	}
	resp := result.(map[string]any)
	if resp["fixes_count"] != 0 {
		t.Errorf("expected 0 fixes, got %v", resp["fixes_count"])
	}
}

func TestSelfImproveHandler_Validate(t *testing.T) {
	h, _ := newTestSelfImproveHandler(t)

	result, err := h.handleValidate(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleValidate: %v", err)
	}
	resp := result.(map[string]any)
	if resp["validations_count"] != 0 {
		t.Errorf("expected 0 validations, got %v", resp["validations_count"])
	}
}

func TestSelfImproveHandler_ServerRegistration(t *testing.T) {
	// Verify all methods are registered and accessible on the server.
	h, _ := newTestSelfImproveHandler(t)
	server := New(&Config{}, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	h.RegisterSelfImproveMethods(server)

	expected := []string{
		"selfimprove.detect",
		"selfimprove.analyze",
		"selfimprove.generate",
		"selfimprove.validate",
		"selfimprove.apply",
		"selfimprove.reject",
		"selfimprove.status",
		"selfimprove.cycle",
	}
	for _, method := range expected {
		handler, ok := server.handlers[method]
		if !ok {
			t.Errorf("handler for %s not registered", method)
		}
		if handler == nil {
			t.Errorf("handler for %s is nil", method)
		}
	}
}
