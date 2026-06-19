package llm

// Internal white-box tests for the shutdown flag in RuntimeManager.
// Lives in package llm (not llm_test) so it can reach the unexported
// fields/methods involved in the auto-restart race: m.shutdown,
// m.makeHealthCallback, m.attemptAutoRestart, endpointProcess, and
// restartState.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestShutdownBlocksAutoRestart_AfterStopAll verifies the core invariant: once
// StopAll has set m.shutdown=true, a health-check transition to unhealthy
// fired through makeHealthCallback must NOT trigger attemptAutoRestart (no
// rs.attempts increment, no RestartProvider call, no subprocess respawn).
//
// This exercises the code path at runtime_manager.go:668 where
// attemptAutoRestart checks m.shutdown under the mutex and returns early.
func TestShutdownBlocksAutoRestart_AfterStopAll(t *testing.T) {
	t.Parallel()

	mgr := NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "shutdown-blocks.pid")
	endpointKey := "llama-cpp:127.0.0.1:7801"
	cfg := &RuntimeConfig{
		Type:               RuntimeLlamaCpp,
		ModelPath:          tempModelFileInternal(t),
		ModelPaths:         map[string]string{"alpha": tempModelFileInternal(t)},
		ModelKeys:          []string{"alpha"},
		EndpointKey:        endpointKey,
		PIDFile:            pidFile,
		AutoStart:          false, // do not spawn anything during the test
		AutoStop:           true,
		SpawnCommand:       []string{"sleep", "30"},
		SpawnTimeout:       2 * time.Second,
		HealthEndpoint:     "/health",
		HealthInterval:     time.Second,
		HealthTimeout:      time.Second,
		HealthThreshold:    1,
		RestartEnabled:     true,
		RestartMaxAttempts: 3,
		RestartCooldown:    time.Millisecond, // tiny so cooldown never blocks
		RestartResetAfter:  time.Second,
	}

	if err := mgr.RegisterConfig("race-provider", cfg, "http://127.0.0.1:7801"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	// Sanity: restartState was created because RestartEnabled=true.
	mgr.mu.Lock()
	ep, ok := mgr.endpoints[endpointKey]
	mgr.mu.Unlock()
	if !ok {
		t.Fatalf("endpoint %s not registered", endpointKey)
	}
	if ep.rs == nil {
		t.Fatal("expected restartState to be non-nil when RestartEnabled=true")
	}

	// StopAll sets m.shutdown = true (with the mutex held). AutoStop=true
	// would normally try to stop the (never-started) subprocess; that's a
	// no-op because no PID exists. The critical effect is the shutdown flag.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	// Confirm the precondition: shutdown flag is now true.
	mgr.mu.Lock()
	shutdownSet := mgr.shutdown
	mgr.mu.Unlock()
	if !shutdownSet {
		t.Fatal("expected m.shutdown=true after StopAll; the test cannot verify the invariant without it")
	}

	// Trigger the unhealthy transition through the SAME path the health
	// checker uses: makeHealthCallback(endpointKey). The returned callback
	// spawns `go m.attemptAutoRestart(endpointKey)` when called with false.
	cb := mgr.makeHealthCallback(endpointKey)
	cb(false) // launches a goroutine running attemptAutoRestart

	// Give the goroutine time to either run (bug) or return early (fix).
	// attemptAutoRestart holds m.mu briefly; 100ms is generous for a no-op
	// return path even under heavy scheduling pressure.
	time.Sleep(100 * time.Millisecond)

	// Assertion 1: rs.attempts must still be 0. If attemptAutoRestart had
	// proceeded past the shutdown check, it would increment rs.attempts
	// before calling RestartProvider.
	mgr.mu.Lock()
	attempts := ep.rs.attempts
	lastRestart := ep.rs.lastRestart
	mgr.mu.Unlock()
	if attempts != 0 {
		t.Errorf("expected rs.attempts=0 after shutdown (restart was blocked); got %d", attempts)
	}
	if !lastRestart.IsZero() {
		t.Errorf("expected rs.lastRestart to be zero (no restart attempted); got %v", lastRestart)
	}

	// Assertion 2: no subprocess was spawned. RestartProvider ->
	// StartProvider -> ep.proc.Start writes the PID file. Its absence is
	// proof that RestartProvider was never reached.
	//
	// (The process was never started during this test because AutoStart=false
	// and we never called StartAll/StartProvider, so the PID file should not
	// exist regardless. But this guards against a regression where
	// attemptAutoRestart somehow bypasses the shutdown flag and reaches
	// RestartProvider.)
	if _, err := os.Stat(pidFile); err == nil {
		t.Errorf("PID file %s exists; RestartProvider was reached despite shutdown flag", pidFile)
	}
}

// TestShutdownBlocksAutoRestart_RaceWithStopAll verifies the TOCTOU fix in
// StartProvider: even if attemptAutoRestart's goroutine acquires m.mu before
// StopAll and passes the shutdown check (still false at that moment), the
// re-check inside StartProvider (added after the spawn-resources setup,
// right before ep.proc.Start) catches the shutdown flag and aborts the spawn.
//
// Before the fix, a restart goroutine could pass the top-of-function
// m.shutdown check, release m.mu, then call RestartProvider → StartProvider
// → ep.proc.Start which spawns a subprocess that survives daemon shutdown.
// The re-check at StartProvider closes that window.
func TestShutdownBlocksAutoRestart_RaceWithStopAll(t *testing.T) {

	t.Parallel()

	mgr := NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "race.pid")
	endpointKey := "llama-cpp:127.0.0.1:7802"
	cfg := &RuntimeConfig{
		Type:               RuntimeLlamaCpp,
		ModelPath:          tempModelFileInternal(t),
		ModelPaths:         map[string]string{"beta": tempModelFileInternal(t)},
		ModelKeys:          []string{"beta"},
		EndpointKey:        endpointKey,
		PIDFile:            pidFile,
		AutoStart:          false,
		AutoStop:           true,
		SpawnCommand:       []string{"sleep", "30"},
		SpawnTimeout:       2 * time.Second,
		HealthEndpoint:     "/health",
		HealthInterval:     time.Second,
		HealthTimeout:      time.Second,
		HealthThreshold:    1,
		RestartEnabled:     true,
		RestartMaxAttempts: 3,
		RestartCooldown:    time.Millisecond,
		RestartResetAfter:  time.Second,
	}
	if err := mgr.RegisterConfig("race-provider", cfg, "http://127.0.0.1:7802"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	mgr.mu.Lock()
	ep, ok := mgr.endpoints[endpointKey]
	mgr.mu.Unlock()
	if !ok || ep.rs == nil {
		t.Fatalf("endpoint/restartState not wired up for %s", endpointKey)
	}

	// Fire the unhealthy callback many times concurrently with StopAll.
	// Each cb(false) launches `go m.attemptAutoRestart(endpointKey)`. Whether
	// the goroutine runs before or after StopAll sets the flag, it must NOT
	// proceed to RestartProvider.
	cb := mgr.makeHealthCallback(endpointKey)

	const callbacks = 20
	var firedCount int32
	var wg sync.WaitGroup
	wg.Add(callbacks + 1)

	// Launcher: fires unhealthy transitions as fast as possible.
	for i := 0; i < callbacks; i++ {
		go func() {
			defer wg.Done()
			cb(false)
			atomic.AddInt32(&firedCount, 1)
		}()
	}

	// Stopper: runs StopAll concurrently with the launchers.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mgr.StopAll(ctx); err != nil {
			t.Errorf("StopAll: %v", err)
		}
	}()

	wg.Wait()

	// Allow any in-flight attemptAutoRestart goroutines to either complete
	// the (blocked) path or return from the cooldown check.
	time.Sleep(150 * time.Millisecond)

	// After the storm: no subprocess should have been spawned, even though
	// some attemptAutoRestart goroutines may have incremented rs.attempts
	// before StopAll set m.shutdown. The TOCTOU fix is in StartProvider:
	// it re-checks m.shutdown under the lock right before ep.proc.Start,
	// so even if attempts was incremented, the spawn is aborted.
	if _, err := os.Stat(pidFile); err == nil {
		t.Errorf("PID file %s exists; a restart spawned a subprocess despite shutdown (fired=%d callbacks, attempts=%d)",
			pidFile, atomic.LoadInt32(&firedCount), func() int {
				mgr.mu.Lock()
				defer mgr.mu.Unlock()
				return ep.rs.attempts
			}())
	}

	// Cleanup any in-flight restart attempts that might race with test exit.
	// If a restart goroutine is stuck in RestartProvider's StartProvider path,
	// it will have returned "runtime manager is shutting down" and not spawned.
}
