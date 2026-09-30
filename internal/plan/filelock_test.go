package plan

import (
	"sync"
	"testing"
	"time"
)

// Pins the L12 fix: the lock pool is a fixed-size bucket array (never grows
// with the number of plan files) and still provides mutual exclusion.
func TestLockMarkdownWrite_MutualExclusion(t *testing.T) {
	const path = "/tmp/fake-plan-dir/plan-lock-test.md"

	entered := make(chan struct{}, 1)
	unlock1 := LockMarkdownWrite(path)
	go func() {
		unlock2 := LockMarkdownWrite(path)
		entered <- struct{}{}
		unlock2()
	}()

	select {
	case <-entered:
		t.Fatal("second LockMarkdownWrite acquired the lock while held")
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}
	unlock1()

	select {
	case <-entered:
		// expected after release
	case <-time.After(time.Second):
		t.Fatal("second LockMarkdownWrite never acquired the lock after release")
	}
}

// Distinct paths hashing to the same bucket must not deadlock: both
// serialize through the shared bucket mutex.
func TestLockMarkdownWrite_ConcurrentDistinctPaths(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for range 50 {
				unlock := LockMarkdownWrite("/tmp/plans/plan-lock-concurrent-test.md")
				unlock()
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// no deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock in lock pool")
	}
}
