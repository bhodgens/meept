package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

func TestSelectAgent(t *testing.T) {
	ts := &TacticalScheduler{}

	tests := []struct {
		toolHint  string
		wantAgent string
	}{
		{"code", "coder"},
		{"refactor", "coder"},
		{"debug", "debugger"},
		{"fix", "debugger"},
		{"analyze", "analyst"},
		{"research", "analyst"},
		{"git", "committer"},
		{"commit", "committer"},
		{"schedule", "scheduler"},
		{"plan", "planner"},
		{"chat", "chat"},
		{"", "chat"},
		{"unknown", "chat"},
	}

	for _, tt := range tests {
		step := &task.TaskStep{ToolHint: tt.toolHint}
		got := ts.selectAgent(step)
		if got != tt.wantAgent {
			t.Errorf("selectAgent(%q) = %q, want %q", tt.toolHint, got, tt.wantAgent)
		}
	}
}

func TestTacticalScheduler_IsRateLimitError(t *testing.T) {
	ts := &TacticalScheduler{}

	cases := []struct {
		msg  string
		want bool
	}{
		{"HTTP 429: Too Many Requests", true},
		{"anthropic: rate limit exceeded", true},
		{"quota exceeded for model", true},
		{"rate_limit_error on provider X", true},
		{"", false},
		{"context deadline exceeded", false},
		{"permission denied", false},
	}
	for _, tc := range cases {
		if got := ts.isRateLimitError(tc.msg); got != tc.want {
			t.Errorf("isRateLimitError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestTacticalScheduler_Semaphore(t *testing.T) {
	// Test that semaphores are initialized correctly
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		MaxConcurrentJobs:     5,
		MaxConcurrentPerAgent: 2,
	})

	if ts.globalSemaphore == nil {
		t.Fatal("globalSemaphore should be initialized")
	}
	if cap(ts.globalSemaphore) != 5 {
		t.Errorf("globalSemaphore cap = %d, want 5", cap(ts.globalSemaphore))
	}

	if ts.agentSemaphore == nil {
		t.Fatal("agentSemaphore should be initialized")
	}

	// Test acquireSlots and releaseSlots
	t.Run("AcquireAndRelease", func(t *testing.T) {
		// Should be able to acquire slots for known agents
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire slots for coder")
		}

		// Acquire again (up to limit)
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire second slot for coder")
		}

		// Third acquire should fail (limit is 2)
		if ts.acquireSlots("coder") {
			t.Error("should not be able to acquire third slot for coder")
		}

		// Release one slot
		ts.releaseSlots("coder")

		// Should be able to acquire again
		if !ts.acquireSlots("coder") {
			t.Error("should be able to acquire slot after release")
		}
	})

	t.Run("GlobalSemaphoreLimit", func(t *testing.T) {
		// Create a new scheduler with small limits for testing
		ts2 := NewTacticalScheduler(TacticalSchedulerConfig{
			MaxConcurrentJobs:     3,
			MaxConcurrentPerAgent: 10,
		})

		// Acquire all global slots
		for i := 0; i < 3; i++ {
			if !ts2.acquireSlots("coder") {
				t.Errorf("should acquire global slot %d", i)
			}
		}

		// Next acquire should fail
		if ts2.acquireSlots("coder") {
			t.Error("should not acquire when global semaphore full")
		}

		// Release all
		for i := 0; i < 3; i++ {
			ts2.releaseSlots("coder")
		}
	})

	t.Run("ReleaseUnknownAgent", func(t *testing.T) {
		// Should not panic when releasing unknown agent
		ts.releaseSlots("unknown-agent")
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		ts3 := NewTacticalScheduler(TacticalSchedulerConfig{
			MaxConcurrentJobs:     10,
			MaxConcurrentPerAgent: 5,
		})

		var wg sync.WaitGroup
		acquired := make(chan bool, 20)
		done := make(chan struct{})

		// Try to acquire from multiple goroutines, holding slots until done
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ts3.acquireSlots("coder") {
					acquired <- true
					<-done // Hold slot until signaled
					ts3.releaseSlots("coder")
				} else {
					acquired <- false
				}
			}()
		}

		// Give goroutines time to acquire
		time.Sleep(100 * time.Millisecond)
		close(done) // Release all goroutines

		wg.Wait()
		close(acquired)

		// Count successful acquisitions
		count := 0
		for ok := range acquired {
			if ok {
				count++
			}
		}

		// Should have at most 10 successful (global limit)
		if count > 10 {
			t.Errorf("too many successful acquisitions: got %d, want <= 10", count)
		}
	})
}
