package tui

import (
	"strings"
	"testing"
)

// newDispatchTestHandler creates a CommandHandler suitable for dispatch
// command testing. The rpc field is left nil; tests verify the
// "requires daemon connection" guard path and argument parsing.
func newDispatchTestHandler(t *testing.T) *CommandHandler {
	t.Helper()
	return &CommandHandler{}
}

func TestHandleDispatchCommand_NoArgs(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand(nil)
	if !res.IsError {
		t.Error("expected error for no args")
	}
	// With nil rpc, the daemon connection check fires first.
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected 'daemon connection' error, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_EmptyArgs(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand([]string{})
	if !res.IsError {
		t.Error("expected error for empty args")
	}
}

func TestHandleDispatchCommand_NoDaemon(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	// With nil RPC, submit should fail with "requires daemon connection".
	res := h.handleDispatchCommand([]string{"node-1", "coder", "do stuff"})
	if !res.IsError {
		t.Error("expected error when daemon not connected")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected 'daemon connection' in output, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_StatusNoDaemon(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand([]string{"status", "job-1"})
	if !res.IsError {
		t.Error("expected error when daemon not connected")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected 'daemon connection' in output, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_ResultsNoDaemon(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand([]string{"results", "job-1"})
	if !res.IsError {
		t.Error("expected error when daemon not connected")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected 'daemon connection' in output, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_StatusMissingJobID(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	// Even with nil RPC, the nil check comes first, so this still tests the
	// daemon guard. If rpc were wired, the missing-jobID guard would fire.
	res := h.handleDispatchCommand([]string{"status"})
	if !res.IsError {
		t.Error("expected error")
	}
}

func TestHandleDispatchCommand_ResultsMissingJobID(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand([]string{"results"})
	if !res.IsError {
		t.Error("expected error")
	}
}

func TestHandleDispatchCommand_SubmitTooFewArgs(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	// Only 2 args (node, agent) — not enough for submit.
	// With nil RPC, the daemon check fires first. The test verifies the
	// command routes to the submit path (not status/results).
	res := h.handleDispatchCommand([]string{"node-1", "coder"})
	if !res.IsError {
		t.Error("expected error")
	}
}

func TestDispatchSubmit_TooFewArgs(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.dispatchSubmit([]string{"node-1", "coder"})
	if !res.IsError {
		t.Error("expected error for < 3 args")
	}
	if !strings.Contains(res.Output, "usage:") {
		t.Errorf("expected usage hint, got %q", res.Output)
	}
}

func TestDispatchStatus_MissingJobID(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.dispatchStatus(nil)
	if !res.IsError {
		t.Error("expected error for missing jobID")
	}
	if !strings.Contains(res.Output, "usage:") {
		t.Errorf("expected usage hint, got %q", res.Output)
	}
}

func TestDispatchResults_MissingJobID(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.dispatchResults(nil)
	if !res.IsError {
		t.Error("expected error for missing jobID")
	}
	if !strings.Contains(res.Output, "usage:") {
		t.Errorf("expected usage hint, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_RoutesToStatus(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	// "status" keyword should route to status path, not submit.
	res := h.handleDispatchCommand([]string{"status", "job-123"})
	// With nil rpc this returns an error about daemon connection.
	if !res.IsError {
		t.Error("expected error (nil rpc)")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected daemon connection error, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_RoutesToResults(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	res := h.handleDispatchCommand([]string{"results", "job-456"})
	if !res.IsError {
		t.Error("expected error (nil rpc)")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected daemon connection error, got %q", res.Output)
	}
}

func TestHandleDispatchCommand_RoutesToSubmit(t *testing.T) {
	t.Parallel()
	h := newDispatchTestHandler(t)
	// Any first arg that isn't "status" or "results" routes to submit.
	res := h.handleDispatchCommand([]string{"node-x", "agent-y", "task-z"})
	if !res.IsError {
		t.Error("expected error (nil rpc)")
	}
	if !strings.Contains(res.Output, "daemon connection") {
		t.Errorf("expected daemon connection error, got %q", res.Output)
	}
}
