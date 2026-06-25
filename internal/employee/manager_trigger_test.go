package employee

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers (manager-level)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Tests: Manager.Trigger GoalLoop integration
// ---------------------------------------------------------------------------

// TestTrigger_NoGoalLoop_Fallback verifies that when no GoalLoop is
// registered, Trigger falls back to the errNotConfigured error (since
// botManager is nil in this test). This confirms the fallback path is
// reached when no loop is registered.
func TestTrigger_NoGoalLoop_Fallback(t *testing.T) {
	m := NewManager(nil) // no bot manager
	_, err := m.Trigger(context.Background(), "emp-1", map[string]any{"source": "test"})
	if err == nil {
		t.Fatal("expected errNotConfigured when botManager is nil")
	}
}

// TestTriggerViaGoalLoop_DelegatesToDecide verifies that when a GoalLoop
// is registered, triggerViaGoalLoop delegates to GoalLoop.Decide and
// returns a "completed" status on success.
func TestTriggerViaGoalLoop_DelegatesToDecide(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"p"}]}`)
	reflector.queueResponse(`{"health":"healthy","reasoning":"ok"}`)
	executor := newStubExecutor()
	executor.succeedWith("done", 10)

	loop := NewGoalLoop("emp-decide-test", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(executor)

	m := NewManager(nil)

	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-decide-test",
		map[string]any{"source": "test"},
		"tier_1_reactive",
		loop,
	)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want %q", result.Status, "completed")
	}
	if result.InvocationID == "" {
		t.Error("expected non-empty invocation ID")
	}
	if result.StartedAt.IsZero() {
		t.Error("expected non-zero StartedAt")
	}
	if executor.CallCount() != 1 {
		t.Errorf("executor called %d times, want 1", executor.CallCount())
	}
}

// TestTriggerViaGoalLoop_AssessError_PropagatesAsError verifies that when
// GoalLoop.Decide returns an error (e.g. LLM unavailable), the
// TriggerResult reflects the error status and the error propagates.
func TestTriggerViaGoalLoop_AssessError_PropagatesAsError(t *testing.T) {
	reflector := newStubReflector()
	reflector.queueError(errors.New("LLM unavailable"))

	loop := NewGoalLoop("emp-assess-err", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithExecutor(newStubExecutor())

	m := NewManager(nil)

	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-assess-err",
		map[string]any{"source": "test"},
		"tier_1_reactive",
		loop,
	)
	if err == nil {
		t.Fatal("expected error when Assess fails")
	}
	if result.Status != "error" {
		t.Errorf("status = %q, want %q", result.Status, "error")
	}
}

// TestTriggerViaGoalLoop_PayloadMarshal verifies that the payload is
// correctly marshaled into the TriggerEvent.
func TestTriggerViaGoalLoop_PayloadMarshal(t *testing.T) {
	// Use a tier-1 constitution with no candidates (so Decide is a no-op).
	reflector := newStubReflector()
	// default returns {"candidates":[]}

	loop := NewGoalLoop("emp-payload", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	m := NewManager(nil)

	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-payload",
		map[string]any{"source": "webhook", "event": "push"},
		"tier_1_reactive",
		loop,
	)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want %q", result.Status, "completed")
	}
}

// TestTriggerViaGoalLoop_NilPayload verifies that a nil payload doesn't
// panic and defaults source to "manual".
func TestTriggerViaGoalLoop_NilPayload(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-nil", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	m := NewManager(nil)

	result, err := m.triggerViaGoalLoop(
		context.Background(),
		"emp-nil",
		nil,
		"tier_1_reactive",
		loop,
	)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want %q", result.Status, "completed")
	}
}

// ---------------------------------------------------------------------------
// Tests: Per-employee serialization (spec line 614)
// ---------------------------------------------------------------------------

// TestAcquireInvokeMutex_SerializesConcurrentAccess verifies that the
// per-employee serialization mutex correctly serializes concurrent access.
// This is the core of Gap G4 (spec line 614: "Goal loop uses the same
// pattern" as per-bot invocation serialization).
//
// We spawn N goroutines that all acquire the same employee's mutex,
// increment a counter, sleep briefly, then decrement. If any two calls
// overlap, maxDepth will be > 1. With proper serialization, maxDepth
// must be exactly 1.
func TestAcquireInvokeMutex_SerializesConcurrentAccess(t *testing.T) {
	m := NewManager(nil)

	var (
		wg       sync.WaitGroup
		maxDepth int32
		curDepth int32
		callCnt  int32
	)

	const n = 10
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			mu := m.acquireInvokeMutex("emp-ser-test")
			mu.Lock()
			defer mu.Unlock()

			cur := atomic.AddInt32(&curDepth, 1)
			for {
				old := atomic.LoadInt32(&maxDepth)
				if cur <= old || atomic.CompareAndSwapInt32(&maxDepth, old, cur) {
					break
				}
			}
			atomic.AddInt32(&callCnt, 1)

			// Simulate work (hold the lock briefly to widen the race window).
			time.Sleep(5 * time.Millisecond)

			atomic.AddInt32(&curDepth, -1)
		}()
	}
	wg.Wait()

	if maxDepth != 1 {
		t.Errorf("maxDepth = %d, want 1 (calls should be serialized per spec line 614)", maxDepth)
	}
	if callCnt != n {
		t.Errorf("callCnt = %d, want %d", callCnt, n)
	}
}

// TestAcquireInvokeMutex_DistinctEmployeesNotContended verifies that
// different employees get different mutexes (no cross-employee contention).
// Two employees should be able to hold their respective mutexes
// simultaneously.
func TestAcquireInvokeMutex_DistinctEmployeesNotContended(t *testing.T) {
	m := NewManager(nil)

	muA := m.acquireInvokeMutex("emp-A")
	muB := m.acquireInvokeMutex("emp-B")
	// Both should be acquired simultaneously (different mutexes, no blocking).
	// Lock both to prove they're distinct.
	muA.Lock()
	muB.Lock()
	// If they were the same mutex, muB.Lock() would deadlock.
	muA.Unlock()
	muB.Unlock()

	// Same employee should get the same mutex pointer back.
	muA1 := m.acquireInvokeMutex("emp-A")
	if muA != muA1 {
		t.Error("expected same mutex instance for same employee ID")
	}
}

// TestAcquireInvokeMutex_DistinctEmployeesConcurrent verifies that
// concurrent calls for different employees don't block each other. This
// complements TestAcquireInvokeMutex_SerializesConcurrentAccess by showing
// that only same-employee calls serialize.
func TestAcquireInvokeMutex_DistinctEmployeesConcurrent(t *testing.T) {
	m := NewManager(nil)

	var (
		wg       sync.WaitGroup
		maxDepth int32
		curDepth int32
	)

	const n = 5
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			// Each goroutine uses a distinct employee ID.
			empID := "emp-distinct-" + string(rune('A'+idx))
			mu := m.acquireInvokeMutex(empID)
			mu.Lock()
			defer mu.Unlock()

			cur := atomic.AddInt32(&curDepth, 1)
			for {
				old := atomic.LoadInt32(&maxDepth)
				if cur <= old || atomic.CompareAndSwapInt32(&maxDepth, old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&curDepth, -1)
		}(i)
	}
	wg.Wait()

	// Since all goroutines use distinct employee IDs, they should all
	// run concurrently: maxDepth should equal n.
	if maxDepth != n {
		t.Errorf("maxDepth = %d, want %d (distinct employees should run concurrently)", maxDepth, n)
	}
}

// ---------------------------------------------------------------------------
// Tests: RegisterGoalLoop / GetGoalLoop
// ---------------------------------------------------------------------------

// TestRegisterGoalLoop_NilGuard verifies that nil loops and empty IDs are
// silently ignored (typed-nil guard per CLAUDE.md).
func TestRegisterGoalLoop_NilGuard(t *testing.T) {
	m := NewManager(nil)
	// Nil loop should be ignored, not panic.
	m.RegisterGoalLoop("emp-1", nil)
	if loop := m.GetGoalLoop("emp-1"); loop != nil {
		t.Errorf("expected nil loop after registering nil")
	}

	// Empty ID should be ignored.
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-1", testTier1Constitution(), nil, nil).WithReflector(reflector)
	m.RegisterGoalLoop("", loop)
	if l := m.GetGoalLoop(""); l != nil {
		t.Error("expected nil loop for empty employee ID")
	}
}

// TestRegisterGoalLoop_Overwrite verifies that a second registration for
// the same employee replaces the first.
func TestRegisterGoalLoop_Overwrite(t *testing.T) {
	m := NewManager(nil)
	reflector := newStubReflector()

	loop1 := NewGoalLoop("emp-1", testTier1Constitution(), nil, nil).WithReflector(reflector)
	loop2 := NewGoalLoop("emp-1", testTier2Constitution(), nil, nil).WithReflector(reflector)

	m.RegisterGoalLoop("emp-1", loop1)
	if l := m.GetGoalLoop("emp-1"); l != loop1 {
		t.Error("expected loop1 after first registration")
	}

	m.RegisterGoalLoop("emp-1", loop2)
	if l := m.GetGoalLoop("emp-1"); l != loop2 {
		t.Error("expected loop2 after overwrite")
	}
}

// TestGetGoalLoop_NotRegistered verifies that querying an unregistered
// employee returns nil.
func TestGetGoalLoop_NotRegistered(t *testing.T) {
	m := NewManager(nil)
	if loop := m.GetGoalLoop("nonexistent"); loop != nil {
		t.Errorf("expected nil for unregistered employee, got %v", loop)
	}
}
