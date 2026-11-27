package hitl

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestHarnessDriveFrozenClockBounded pins A7-11(i): with a clock that never
// advances (frozen), Drive must still terminate within a bounded number of
// iterations. The old code's only exit was the deadline check, which never
// fired on a frozen clock, so Drive looped forever. The new maxPolls cap
// (derived from budget/poll) bounds the loop; a frozen clock terminates in
// ~budget wall-clock time instead of forever.
func TestHarnessDriveFrozenClockBounded(t *testing.T) {
	// A clock that always returns the same instant: never advances.
	frozen := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return frozen }

	// A terminal that never completes, so the only exit is the cap.
	terminal := func() TerminalStatus { return TerminalStatus{} }

	// Small budget so the derived cap is small and the frozen clock
	// terminates quickly in the test.
	r := &Runner{PollInterval: time.Millisecond, DriveTimeout: 500 * time.Millisecond}
	done := make(chan *RunResult, 1)
	go func() {
		res := r.Drive(RunConfig{
			Scenario: Scenario{ID: "S"},
			Regime:   RegimePerAction,
			Perms:    nil,
			Terminal: terminal,
			Clock:    clock,
		})
		done <- res
	}()
	select {
	case res := <-done:
		// Must have returned. Verify no terminal event was fabricated.
		for _, e := range res.Events {
			if e.Kind == EventTerminal {
				t.Fatalf("frozen-clock run must not fabricate a terminal event: %+v", res.Events)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Drive did not return within 10s on a frozen clock (unbounded loop)")
	}
}

// fakePermsRetry is a PermissionSource that fails Resolve for a given id a
// fixed number of times before succeeding. It counts Resolve calls so the
// test can assert a retry happened.
type fakePermsRetry struct {
	pending      []PendingPermission
	taken        map[string]bool
	failFirst    map[string]int // id -> number of Resolve calls that must fail
	resolveCalls map[string]int
	decisions    map[string]string
	mu           sync.Mutex
}

func newFakePermsRetry() *fakePermsRetry {
	return &fakePermsRetry{
		taken:        map[string]bool{},
		failFirst:    map[string]int{},
		resolveCalls: map[string]int{},
		decisions:    map[string]string{},
	}
}

func (f *fakePermsRetry) Pending() ([]PendingPermission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []PendingPermission{}
	for _, p := range f.pending {
		if !f.taken[p.ID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePermsRetry) Resolve(id, decision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolveCalls[id]++
	if f.failFirst[id] > 0 && f.resolveCalls[id] <= f.failFirst[id] {
		return errors.New("resolve refused (transient)")
	}
	f.taken[id] = true
	f.decisions[id] = decision
	return nil
}

// TestHarnessResolveRetry pins A7-11(ii): a transient Resolve failure must
// NOT mark the permission as seen, so the next poll retries the same
// permission id. At HEAD, seen[p.ID] was set before Resolve, so a failed
// Resolve was never retried.
func TestHarnessResolveRetry(t *testing.T) {
	perms := newFakePermsRetry()
	perms.pending = []PendingPermission{
		{ID: "perm-1", AgentID: "a", SessionID: "s1", Summary: "HITL-STEP-ALLOW: run the report step"},
	}
	perms.failFirst["perm-1"] = 1 // first Resolve fails, second succeeds

	human := NewScriptedHuman(map[string]string{"HITL-STEP-ALLOW": "allow"}, "deny")

	// A frozen clock so the loop is bounded; the terminal never completes.
	frozen := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return frozen }
	terminal := func() TerminalStatus { return TerminalStatus{} }

	// Small budget so the derived cap is small and the test terminates
	// quickly (the frozen clock otherwise loops until the cap).
	r := &Runner{PollInterval: time.Millisecond, DriveTimeout: 500 * time.Millisecond}
	r.Drive(RunConfig{
		Scenario:  Scenario{ID: "S"},
		Regime:    RegimePerAction,
		SessionID: "s1",
		Perms:     perms,
		Human:     human,
		Terminal:  terminal,
		Clock:     clock,
	})

	perms.mu.Lock()
	calls := perms.resolveCalls["perm-1"]
	decision := perms.decisions["perm-1"]
	perms.mu.Unlock()
	if calls < 2 {
		t.Fatalf("Resolve called %d times for perm-1, want >= 2 (first failed, must retry)", calls)
	}
	if decision != "allow" {
		t.Fatalf("final decision for perm-1 = %q, want allow", decision)
	}
}
