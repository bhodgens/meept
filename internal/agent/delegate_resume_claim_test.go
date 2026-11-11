package agent

// F5 bughunt pin tests (C11 resume claim). C11: concurrent post-restart
// Resumes must not double-execute the child — the in-map claim under the
// manager lock admits exactly one execution; the second caller gets a clear
// refusal.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/session"
)

// TestResume_ConcurrentColdResumeSingleExecution pins the C11 claim: two
// concurrent Resumes of the same cold child produce exactly one execution
// (one runFn invocation); the second caller is refused with a clear error.
func TestResume_ConcurrentColdResumeSingleExecution(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	store := session.NewMemoryStore(nil)
	// The SPAWN manager runs with the default run seam (its own child
	// execution is irrelevant to this pin); the RESUME manager counts runs —
	// so the count isolates exactly the resume executions.
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetStore(store)
	var runs atomic.Int64
	count := func(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
		runs.Add(1)
		time.Sleep(50 * time.Millisecond) // widen the race window
		return "done", nil
	}

	ctx := context.Background()
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mgr.SettleForTest(res.ChildID, ChildStateInterrupted, "", "cancelled")

	// Two concurrent cold resumes (fresh manager over the same store).
	fresh := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	fresh.SetStore(store)
	fresh.SetRunFunc(count)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, errs[n] = fresh.Resume(ctx, "sess-parent", res.ChildID)
		}(i)
	}
	wg.Wait()

	refused := 0
	admitted := 0
	for _, err := range errs {
		if err == nil {
			admitted++
		} else {
			refused++
		}
	}
	if admitted != 1 || refused != 1 {
		t.Fatalf("concurrent resumes: admitted=%d refused=%d, want exactly 1 execution + 1 clear refusal (errs=%v)", admitted, refused, errs)
	}
	// Wait for the single execution to settle, then pin the run count.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && runs.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond) // let any (wrong) second run land too
	if got := runs.Load(); got != 1 {
		t.Fatalf("runFn invoked %d times, want exactly 1 (C11 double-execution)", got)
	}
}
