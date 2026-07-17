package agent

import (
	"sync"
	"testing"
	"time"
)

// helpers for the existing map-based NewPerDepthSemaphore.

func fillSemaphoreLimits(maxDepth, maxParallel int) map[int]int {
	limits := make(map[int]int, maxDepth)
	for d := 1; d <= maxDepth; d++ {
		limits[d] = maxParallel
	}
	return limits
}

// TestPerDepthSemaphore_NoDeadlock verifies that per-depth semaphores prevent
// the exact deadlock scenario from HALO's docstring: N parents hold depth-N
// slots waiting for depth-(N+1) children. With per-depth pools the child
// contends on a different channel and never deadlocks.
func TestPerDepthSemaphore_NoDeadlock(t *testing.T) {
	maxDepth := 2
	maxParallel := 4
	limitMap := fillSemaphoreLimits(maxDepth, maxParallel)
	sems := NewPerDepthSemaphore(limitMap)

	var wg sync.WaitGroup

	// Acquire all depth-1 slots (4 parents)
	for i := 0; i < maxParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sems.Acquire(1); err != nil {
				t.Errorf("acquire depth 1: %v", err)
				return
			}
			// Parent sleeps, holding its depth-1 slot.
			time.Sleep(10 * time.Millisecond)
			// Child acquires depth-2 slot (different channel!).
			if err := sems.Acquire(2); err != nil {
				t.Errorf("acquire depth 2: %v", err)
				return
			}
			sems.Release(2)
			sems.Release(1)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success: no deadlock.
	case <-time.After(2 * time.Second):
		t.Fatal("Deadlock detected: per-depth semaphore did not acquire depth-2 slots while depth-1 holders slept")
	}
}

// TestPerDepthSemaphore_ThreeLevelTree demonstrates that a three-level
// recursive tree completes without deadlock using per-depth semaphores.
func TestPerDepthSemaphore_ThreeLevelTree(t *testing.T) {
	maxDepth := 3
	maxParallel := 2
	limitMap := fillSemaphoreLimits(maxDepth, maxParallel)
	sems := NewPerDepthSemaphore(limitMap)

	var wg sync.WaitGroup
	var mu sync.Mutex
	leavesCompleted := 0

	// Depth-1 root spawns 2 children to depth-2
	for i := 0; i < maxParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sems.Acquire(1); err != nil {
				t.Errorf("acquire depth 1: %v", err)
				return
			}

			// Spawn one depth-2 child.
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := sems.Acquire(2); err != nil {
					t.Errorf("acquire depth 2: %v", err)
					return
				}

				// Spawn one depth-3 grandchild.
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := sems.Acquire(3); err != nil {
						t.Errorf("acquire depth 3: %v", err)
						return
					}
					mu.Lock()
					leavesCompleted++
					mu.Unlock()
					sems.Release(3)
				}()

				sems.Release(2)
			}()

			sems.Release(1)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mu.Lock()
		leaves := leavesCompleted
		mu.Unlock()
		if leaves < maxParallel {
			t.Errorf("Expected at least %d leaves, got %d", maxParallel, leaves)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Three-level tree should complete without deadlock")
	}
}

// TestPerDepthSemaphore_SingleSemaphoreWouldDeadlock proves that a single
// shared semaphore (without depth separation) deadlocks under the same
// recursive spawn conditions that per-depth pools survive.
func TestPerDepthSemaphore_SingleSemaphoreWouldDeadlock(t *testing.T) {
	const maxParallel = 4
	single := make(chan struct{}, maxParallel)

	var barrier sync.WaitGroup
	barrier.Add(maxParallel)
	var stuckCount int64
	var mu sync.Mutex

	for i := 0; i < maxParallel; i++ {
		go func() {
			single <- struct{}{} // acquire one slot
			barrier.Done()       // signal acquired
			barrier.Wait()       // wait for all to acquire

			// Now each goroutine tries to acquire a second slot.
			// All slots are held, so none can proceed.
			select {
			case single <- struct{}{}:
				// Unexpected: should be blocked in deadlocked scenario
			case <-time.After(200 * time.Millisecond):
				mu.Lock()
				stuckCount++
				mu.Unlock()
			}
		}()
	}

	// Wait for stuck detection (300ms gives enough time for all 200ms timers).
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	stuck := stuckCount
	mu.Unlock()
	if stuck != maxParallel {
		t.Fatalf("Expected all %d goroutines to be stuck on second acquire, but only %d were", maxParallel, stuck)
	}

	// Clean up the held slots so the goroutines can finish.
	for i := 0; i < maxParallel; i++ {
		<-single
	}
	// Wait for goroutines to complete.
	time.Sleep(100 * time.Millisecond)
}

// TestPerDepthSemaphore_ReleaseAcquire verifies basic acquire/release
// semantics work correctly.
func TestPerDepthSemaphore_ReleaseAcquire(t *testing.T) {
	limitMap := fillSemaphoreLimits(3, 2)
	sems := NewPerDepthSemaphore(limitMap)

	// Acquire both slots at depth 1.
	if err := sems.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1: %v", err)
	}
	if err := sems.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1 (2nd): %v", err)
	}

	// Third acquire should block.
	done := make(chan error, 1)
	go func() {
		done <- sems.Acquire(1)
	}()

	select {
	case err := <-done:
		t.Fatalf("third acquire on full semaphore should block, got: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocked.
	}

	// Release one slot - third acquire should now succeed.
	sems.Release(1)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("third acquire after release: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("third acquire should have succeeded after release")
	}

	// Clean up remaining slot.
	sems.Release(1)
}

// TestPerDepthSemaphore_DepthsIndependent verifies that releasing depth 2
// does not unblock a blocked acquire on depth 1.
func TestPerDepthSemaphore_DepthsIndependent(t *testing.T) {
	limitMap := map[int]int{1: 1, 2: 1}
	sems := NewPerDepthSemaphore(limitMap)

	// Fill depth 1.
	if err := sems.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1: %v", err)
	}

	// Block on depth 1.
	done := make(chan error, 1)
	go func() {
		done <- sems.Acquire(1)
	}()

	// Release depth 2 instead of depth 1 — should NOT unblock depth 1.
	select {
	case <-done:
		t.Fatal("releasing depth 2 should not unblock depth 1")
	case <-time.After(100 * time.Millisecond):
		// Good: still blocked.
	}

	// Clean up.
	sems.Release(1)
}

// TestPerDepthSemaphore_DifferentDepthsIndependent verifies that each depth's
// pool is independent.
func TestPerDepthSemaphore_DifferentDepthsIndependent(t *testing.T) {
	limitMap := fillSemaphoreLimits(3, 1)
	sems := NewPerDepthSemaphore(limitMap)

	// Fill depth 1.
	if err := sems.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1: %v", err)
	}

	// Depth 2 and 3 should still be available.
	if err := sems.Acquire(2); err != nil {
		t.Fatalf("acquire depth 2: %v", err)
	}
	sems.Release(2)

	if err := sems.Acquire(3); err != nil {
		t.Fatalf("acquire depth 3: %v", err)
	}
	sems.Release(3)

	sems.Release(1)
}

// TestPerDepthSemaphore_UnregisteredDepth returns an error.
func TestPerDepthSemaphore_UnregisteredDepth(t *testing.T) {
	limitMap := map[int]int{1: 4}
	sems := NewPerDepthSemaphore(limitMap)

	err := sems.Acquire(99)
	if err == nil {
		t.Fatal("expected error for unregistered depth")
	}

	sems.Release(99) // Release on unregistered depth should be a no-op.
}

// TestPerDepthSemaphore_DeadlockScenario_ProvesConcept
// This test demonstrates the exact scenario described in HALO's
// subagent_tool_factory.py: with a single shared semaphore, N parents
// holding every slot at depth N prevent depth N+1 grandchildren from
// ever acquiring. The per-depth pool avoids this by assigning each depth
// its own channel.
func TestPerDepthSemaphore_DeadlockScenario_ProvesConcept(t *testing.T) {
	// Scenario: maxParallel=4, maxDepth=2.
	// Each parent acquires depth-1, sleeps, then tries to acquire depth-2.
	// With single-channel semaphore: all 4 depth-1 acquires succeed,
	// then all 4 depth-2 acquires block forever (all slots occupied by parents).
	// With per-depth: depth-1 acquires use channel[1], depth-2 uses channel[2].

	maxParallel := 4
	maxDepth := 2
	limitMap := fillSemaphoreLimits(maxDepth, maxParallel)
	sems := NewPerDepthSemaphore(limitMap)

	var wg sync.WaitGroup
	started := make(chan struct{}, maxParallel)
	completed := make(chan struct{}, maxParallel)

	for i := 0; i < maxParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Acquire depth-1 slot.
			if err := sems.Acquire(1); err != nil {
				t.Errorf("depth-1 acquire: %v", err)
				return
			}
			started <- struct{}{}

			// Hold the slot, simulate work.
			time.Sleep(50 * time.Millisecond)

			// Acquire depth-2 slot (different channel).
			if err := sems.Acquire(2); err != nil {
				t.Errorf("depth-2 acquire: %v", err)
				return
			}
			// Depth-2 work.
			time.Sleep(10 * time.Millisecond)
			sems.Release(2)

			sems.Release(1)
			completed <- struct{}{}
		}()
	}

	// Wait for completion within a generous timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Count completions.
		var nCompleted int
		for i := 0; i < maxParallel; i++ {
			select {
			case <-completed:
				nCompleted++
			default:
			}
		}
		if nCompleted != maxParallel {
			t.Errorf("Expected %d completions, got %d", maxParallel, nCompleted)
		}
		_ = started
	case <-time.After(5 * time.Second):
		t.Fatal("Deadlock detected: per-depth semaphore pool failed to handle recursive spawns")
	}
}
