package agent

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func newTestDispatcherWithRegistry(t *testing.T) (*Dispatcher, *AgentRegistry) {
	t.Helper()

	reg := NewAgentRegistry(RegistryConfig{})
	d := NewDispatcher(DispatcherConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})
	return d, reg
}

// --- SteerActiveAgent tests ---

func TestSteerActiveAgent_NoRegistry(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(DispatcherConfig{Logger: slog.Default()})
	err := d.SteerActiveAgent(context.Background(), "conv-1", "redirect", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("SteerActiveAgent with nil registry: got %v, want ErrQueueNotFound", err)
	}
}

func TestSteerActiveAgent_NoActiveQueue(t *testing.T) {
	t.Parallel()

	d, _ := newTestDispatcherWithRegistry(t)
	err := d.SteerActiveAgent(context.Background(), "conv-1", "redirect", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("SteerActiveAgent with no active queue: got %v, want ErrQueueNotFound", err)
	}
}

func TestSteerActiveAgent_ClosedQueue(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)
	q.Close()

	err := d.SteerActiveAgent(context.Background(), "conv-1", "redirect", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("SteerActiveAgent with closed queue: got %v, want ErrQueueNotFound", err)
	}
}

func TestSteerActiveAgent_Success(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)

	err := d.SteerActiveAgent(context.Background(), "conv-1", "redirect now", "test")
	if err != nil {
		t.Fatalf("SteerActiveAgent: got %v, want nil", err)
	}
	if !q.HasSteering() {
		t.Error("SteerActiveAgent: expected steering message in queue")
	}
}

// --- FollowUpActiveAgent tests ---

func TestFollowUpActiveAgent_NoRegistry(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(DispatcherConfig{Logger: slog.Default()})
	err := d.FollowUpActiveAgent(context.Background(), "conv-1", "also do X", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("FollowUpActiveAgent with nil registry: got %v, want ErrQueueNotFound", err)
	}
}

func TestFollowUpActiveAgent_NoActiveQueue(t *testing.T) {
	t.Parallel()

	d, _ := newTestDispatcherWithRegistry(t)
	err := d.FollowUpActiveAgent(context.Background(), "conv-1", "also do X", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("FollowUpActiveAgent with no active queue: got %v, want ErrQueueNotFound", err)
	}
}

func TestFollowUpActiveAgent_ClosedQueue(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)
	q.Close()

	err := d.FollowUpActiveAgent(context.Background(), "conv-1", "also do X", "test")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("FollowUpActiveAgent with closed queue: got %v, want ErrQueueNotFound", err)
	}
}

func TestFollowUpActiveAgent_Success(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)

	err := d.FollowUpActiveAgent(context.Background(), "conv-1", "also do X", "test")
	if err != nil {
		t.Fatalf("FollowUpActiveAgent: got %v, want nil", err)
	}
	if !q.HasFollowUp() {
		t.Error("FollowUpActiveAgent: expected follow-up message in queue")
	}
}

// --- RouteToAgent queue injection path tests ---

func TestRouteToAgent_SteerViaActiveQueue(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)

	result := &DispatchResult{
		AgentID: "coder",
		Intent:  &Intent{Type: string(IntentCode), Summary: "fix the bug"},
	}

	resp, err := d.RouteToAgent(context.Background(), result, "conv-1")
	if err != nil {
		t.Fatalf("RouteToAgent (steer): got err %v", err)
	}
	if resp != "message queued (steer)" {
		t.Errorf("RouteToAgent (steer): got %q, want %q", resp, "message queued (steer)")
	}
	if !q.HasSteering() {
		t.Error("RouteToAgent (steer): expected steering message in queue")
	}
}

func TestRouteToAgent_FollowUpViaActiveQueue(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)

	result := &DispatchResult{
		AgentID: "chat",
		Intent:  &Intent{Type: string(IntentChat), Summary: "tell me more"},
	}

	resp, err := d.RouteToAgent(context.Background(), result, "conv-1")
	if err != nil {
		t.Fatalf("RouteToAgent (follow-up): got err %v", err)
	}
	if resp != "message queued (follow-up)" {
		t.Errorf("RouteToAgent (follow-up): got %q, want %q", resp, "message queued (follow-up)")
	}
	if !q.HasFollowUp() {
		t.Error("RouteToAgent (follow-up): expected follow-up message in queue")
	}
}

func TestRouteToAgent_ExplicitSteerOverride(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)

	// IntentChat would normally be follow-up, but ExplicitSteerMode forces steering
	result := &DispatchResult{
		AgentID:           "chat",
		Intent:            &Intent{Type: string(IntentChat), Summary: "override to steer"},
		ExplicitSteerMode: true,
	}

	resp, err := d.RouteToAgent(context.Background(), result, "conv-1")
	if err != nil {
		t.Fatalf("RouteToAgent (explicit steer): got err %v", err)
	}
	if resp != "message queued (steer)" {
		t.Errorf("RouteToAgent (explicit steer): got %q, want %q", resp, "message queued (steer)")
	}
	if !q.HasSteering() {
		t.Error("RouteToAgent (explicit steer): expected steering message in queue")
	}
}

func TestRouteToAgent_ClosedQueueFallsThrough(t *testing.T) {
	t.Parallel()

	d, reg := newTestDispatcherWithRegistry(t)
	q := NewMessageQueue()
	reg.RegisterActiveQueue("conv-1", q)
	q.Close()

	result := &DispatchResult{
		AgentID: "coder",
		Intent:  &Intent{Type: string(IntentCode), Summary: "fix something"},
	}

	// With closed queue, should fall through to normal agent execution,
	// which requires an actual registered agent. Since there's no agent
	// registered, we expect an error about agent not found.
	_, err := d.RouteToAgent(context.Background(), result, "conv-1")
	if err == nil {
		t.Error("RouteToAgent with closed queue and no agent: expected error, got nil")
	}
}

func TestRouteToAgent_NilRegistry(t *testing.T) {
	t.Parallel()

	d := NewDispatcher(DispatcherConfig{Logger: slog.Default()})
	result := &DispatchResult{
		AgentID: "chat",
		Intent:  &Intent{Type: string(IntentChat), Summary: "hello"},
	}

	_, err := d.RouteToAgent(context.Background(), result, "conv-1")
	if err == nil {
		t.Error("RouteToAgent with nil registry: expected error, got nil")
	}
}
