package daemon

// Gate tests for Contract C2 (tree docs/plans/20261113-escalation-resume-and-hardening,
// leaf 02): the daemon subscribes bus topic "plan.approved" under subscriber
// id "employee-plan-resume" and invokes the employee resume seam with the
// payload's plan_id/by exactly once; malformed payloads are logged skips
// (no call, no panic).
//
// Compile-today rule (leaf 02 lands in parallel with leaf 01): this file
// depends on the SEAM only —
//   - Components.resumeApprovedPlan: the func field leaf 02's wiring
//     assigns (signature: func(ctx, planID, callerID string) error — the
//     frozen C1 shape), nil = wiring absent;
//   - startEmployeeResumeSubscriber: the leaf-02 constructor that
//     subscribes "employee-plan-resume" and starts the pump, returning the
//     subscriber for test-observable assertions.
// RED today: both symbols are undefined (compile error naming them).
// After leaf 02, the wiring satisfies the seam with
// EmployeeManager.ResumeApprovedPlan and these tests go green.

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// resumeSeamRecorder captures ResumeApprovedPlan seam invocations
// concurrently-safely.
type resumeSeamRecorder struct {
	mu    sync.Mutex
	calls []planResumeCall
	err   error // returned to the caller of the seam
}

type planResumeCall struct {
	planID   string
	callerID string
}

func (r *resumeSeamRecorder) record(planID, callerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, planResumeCall{planID: planID, callerID: callerID})
	return r.err
}

func (r *resumeSeamRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *resumeSeamRecorder) last() planResumeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return planResumeCall{}
	}
	return r.calls[len(r.calls)-1]
}

// newResumeWiringHarness builds components with a real bus and the seam
// armed with the recorder, then starts the leaf-02 subscriber pump.
func newResumeWiringHarness(t *testing.T, rec *resumeSeamRecorder) (*Components, *bus.MessageBus, *bus.Subscriber, func()) {
	t.Helper()
	logger := slog.Default()
	mb := bus.New(nil, logger)
	c := &Components{
		Logger:             logger,
		msgBus:             mb,
		resumeApprovedPlan: rec.record,
	}
	sub := c.startEmployeeResumeSubscriber(context.Background())
	if sub == nil {
		t.Fatalf("employee resume subscriber not wired (startEmployeeResumeSubscriber returned nil)")
	}
	if sub.ID != "employee-plan-resume" {
		t.Errorf("subscriber id = %q, want %q", sub.ID, "employee-plan-resume")
	}
	if sub.Topic != "plan.approved" {
		t.Errorf("subscriber topic = %q, want %q", sub.Topic, "plan.approved")
	}
	cleanup := func() { c.stopEmployeeResumeSubscriber() }
	return c, mb, sub, cleanup
}

// resumeBusMessage mirrors gateBusMessage: a plan.approved event carrying
// the payload keys the plan manager publishes.
func resumeBusMessage(topic string, payload map[string]any) *models.BusMessage {
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
	if err != nil {
		panic(err)
	}
	msg.Topic = topic
	return msg
}

// waitForResumeCalls polls until the seam recorder holds want calls.
func waitForResumeCalls(t *testing.T, rec *resumeSeamRecorder, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("resume seam calls did not reach %d within 2s (have %d)", want, rec.count())
}

// TestEmployeeResumeSubscriber_OnApproved_InvokesResume: publishing
// plan.approved {plan_id, by} reaches the seam exactly once with the exact
// (planID, by) arguments.
func TestEmployeeResumeSubscriber_OnApproved_InvokesResume(t *testing.T) {
	rec := &resumeSeamRecorder{}
	_, mb, _, cleanup := newResumeWiringHarness(t, rec)
	defer cleanup()

	mb.Publish("plan.approved", resumeBusMessage("plan.approved", map[string]any{
		"plan_id": "plan-w1",
		"by":      "user",
	}))
	waitForResumeCalls(t, rec, 1)
	time.Sleep(50 * time.Millisecond) // let stragglers (if any) surface

	if got := rec.count(); got != 1 {
		t.Fatalf("resume seam called %d times, want exactly 1", got)
	}
	call := rec.last()
	if call.planID != "plan-w1" {
		t.Errorf("plan_id = %q, want %q", call.planID, "plan-w1")
	}
	if call.callerID != "user" {
		t.Errorf("callerID = %q, want %q", call.callerID, "user")
	}
}

// TestEmployeeResumeSubscriber_MalformedPayloadSkips: a payload missing
// plan_id (or carrying a non-string) is a logged WARN skip — no seam call,
// no panic; the subscriber keeps draining afterwards.
func TestEmployeeResumeSubscriber_MalformedPayloadSkips(t *testing.T) {
	rec := &resumeSeamRecorder{}
	_, mb, _, cleanup := newResumeWiringHarness(t, rec)
	defer cleanup()

	// Missing plan_id.
	mb.Publish("plan.approved", resumeBusMessage("plan.approved", map[string]any{
		"by": "user",
	}))
	// Non-string plan_id.
	mb.Publish("plan.approved", resumeBusMessage("plan.approved", map[string]any{
		"plan_id": 42,
		"by":      "user",
	}))
	time.Sleep(200 * time.Millisecond)

	if got := rec.count(); got != 0 {
		t.Fatalf("malformed payloads invoked the resume seam %d times, want 0", got)
	}

	// The pump survived and still drains: a well-formed event after the
	// malformed ones flows through.
	mb.Publish("plan.approved", resumeBusMessage("plan.approved", map[string]any{
		"plan_id": "plan-after-malformed",
		"by":      "user",
	}))
	waitForResumeCalls(t, rec, 1)
	if call := rec.last(); call.planID != "plan-after-malformed" {
		t.Errorf("post-malformed resume plan_id = %q, want %q", call.planID, "plan-after-malformed")
	}
}

// TestEmployeeResumeSubscriber_NilSeamNoop: a nil seam (components without
// the employee manager) degrades silently — the subscriber must not be
// armed in a shape that panics; the wiring helper itself stays nil-safe.
func TestEmployeeResumeSubscriber_NilSeamNoop(t *testing.T) {
	logger := slog.Default()
	mb := bus.New(nil, logger)
	c := &Components{Logger: logger, msgBus: mb}
	// The nil-seam arm must either skip wiring entirely or tolerate events
	// without panicking; leaf 02 picks the posture, this pins no-panic.
	sub := c.startEmployeeResumeSubscriber(context.Background())
	if sub != nil {
		mb.Publish("plan.approved", resumeBusMessage("plan.approved", map[string]any{
			"plan_id": "plan-nil-seam",
			"by":      "user",
		}))
		time.Sleep(100 * time.Millisecond)
		c.stopEmployeeResumeSubscriber()
	}
}
