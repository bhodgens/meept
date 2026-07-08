package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

// TestWorkerPool_Creation tests pool initialization
func TestWorkerPool_Creation(t *testing.T) {
	cfg := DefaultWorkerPoolConfig()
	pool := NewWorkerPool(cfg)

	if pool == nil {
		t.Fatal("NewWorkerPool returned nil")
	}
	if pool.maxWorkers != 100 {
		t.Errorf("expected maxWorkers=100, got %d", pool.maxWorkers)
	}
	if pool.maxLoopsPerWorker != 5 {
		t.Errorf("expected maxLoopsPerWorker=5, got %d", pool.maxLoopsPerWorker)
	}
	if len(pool.workers) != 0 {
		t.Errorf("expected 0 workers initially, got %d", len(pool.workers))
	}

	pool.Stop()
}

// TestWorkerPool_StartStop tests lifecycle methods
func TestWorkerPool_StartStop(t *testing.T) {
	pool := NewWorkerPool(DefaultWorkerPoolConfig())

	// Should not panic
	pool.Start()
	pool.Stop()

	// Multiple stops should not panic
	pool.Stop()
}

// TestWorkerPool_Submit tests work submission creates workers lazily
func TestWorkerPool_Submit(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        10,
		MaxLoopsPerWorker: 2,
		IdleTimeout:       time.Second,
	}
	pool := NewWorkerPool(cfg)
	pool.Start()
	defer pool.Stop()

	// Create a minimal agent loop
	loop := agent.NewAgentLoop("test-session", "/tmp")

	// Submit work
	item := WorkItem{
		Loop:           loop,
		Trigger:        TriggerUserMessage,
		Message:        "test message",
		ConversationID: "conv-1",
	}

	err := pool.Submit(item)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Give worker time to start
	time.Sleep(50 * time.Millisecond)

	// Verify worker was created
	pool.mu.Lock()
	workerCount := len(pool.workers)
	pool.mu.Unlock()

	if workerCount != 1 {
		t.Errorf("expected 1 worker after submit, got %d", workerCount)
	}
}

// TestWorker_HasCapacity tests capacity checks
func TestWorker_HasCapacity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker := &Worker{
		id:            0,
		ctx:           ctx,
		cancel:        cancel,
		assignedLoops: make(map[*agent.AgentLoop]bool),
		workQueue:     make(chan WorkItem, 100),
		maxLoops:      2,
	}

	// Initially has capacity
	if !worker.hasCapacity() {
		t.Error("expected worker to have capacity initially")
	}

	// Assign max loops using real AgentLoop instances
	loop1 := agent.NewAgentLoop("session-1", "/tmp")
	loop2 := agent.NewAgentLoop("session-2", "/tmp")
	worker.assignedLoops[loop1] = true
	worker.assignedLoops[loop2] = true

	// Should be at capacity
	if worker.hasCapacity() {
		t.Error("expected worker to be at capacity after 2 assignments")
	}
}

// TestWorkerPool_MultipleSubmits tests that multiple workers are created when needed
func TestWorkerPool_MultipleSubmits(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        5,
		MaxLoopsPerWorker: 2,
		IdleTimeout:       time.Second,
	}
	pool := NewWorkerPool(cfg)
	pool.Start()
	defer pool.Stop()

	// Submit multiple work items
	// With maxLoopsPerWorker=2, we should need multiple workers for 5 loops
	for i := 0; i < 5; i++ {
		loop := agent.NewAgentLoop("test-session", "/tmp")
		item := WorkItem{
			Loop:           loop,
			Trigger:        TriggerUserMessage,
			Message:        "test",
			ConversationID: "conv",
		}
		if err := pool.Submit(item); err != nil {
			t.Fatalf("Submit %d failed: %v", i, err)
		}
	}

	// Give workers time to process
	time.Sleep(100 * time.Millisecond)

	pool.mu.Lock()
	workerCount := len(pool.workers)
	pool.mu.Unlock()

	// Should have created multiple workers (at least 3 for 5 loops with capacity of 2)
	if workerCount < 3 {
		t.Errorf("expected at least 3 workers for 5 loops, got %d", workerCount)
	}
}

// TestWorkerPool_Exhaustion tests error when pool is full
func TestWorkerPool_Exhaustion(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        2,
		MaxLoopsPerWorker: 1,
		IdleTimeout:       time.Second,
	}
	pool := NewWorkerPool(cfg)
	pool.Start()
	defer pool.Stop()

	// Fill the pool
	for i := 0; i < 2; i++ {
		loop := agent.NewAgentLoop("test-session", "/tmp")
		item := WorkItem{
			Loop:           loop,
			Trigger:        TriggerUserMessage,
			Message:        "test",
			ConversationID: "conv",
		}
		if err := pool.Submit(item); err != nil {
			t.Fatalf("Submit %d failed: %v", i, err)
		}
	}

	// Next submit should fail (pool exhausted)
	loop := agent.NewAgentLoop("overflow-session", "/tmp")
	item := WorkItem{
		Loop:           loop,
		Trigger:        TriggerUserMessage,
		Message:        "overflow",
		ConversationID: "conv",
	}
	err := pool.Submit(item)
	if err == nil {
		t.Error("expected pool exhaustion error, got nil")
	}
}

// TestWorkerPool_ContextCancellation tests graceful shutdown
func TestWorkerPool_ContextCancellation(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		cfg := DefaultWorkerPoolConfig()
		pool := NewWorkerPool(cfg)
		pool.Start()

		// Create a worker
		loop := agent.NewAgentLoop("test-session", "/tmp")
		item := WorkItem{
			Loop:           loop,
			Trigger:        TriggerUserMessage,
			Message:        "test",
			ConversationID: "conv",
		}
		_ = pool.Submit(item)

		// Stop should cancel all workers
		pool.Stop()

		done <- true
	}()

	select {
	case <-done:
		// Success - didn't hang
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out - worker goroutines did not exit")
	}
}

// TestWorkerPoolConfig_Defaults tests default configuration values
func TestWorkerPoolConfig_Defaults(t *testing.T) {
	cfg := DefaultWorkerPoolConfig()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"MaxWorkers", cfg.MaxWorkers, 100},
		{"MaxLoopsPerWorker", cfg.MaxLoopsPerWorker, 5},
		{"IdleTimeout", cfg.IdleTimeout, 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, tt.got)
			}
		})
	}
}

// TestWorkerPool_FindWorkerWithCapacity tests round-robin selection
func TestWorkerPool_FindWorkerWithCapacity(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        5,
		MaxLoopsPerWorker: 3,
		IdleTimeout:       time.Second,
	}
	pool := NewWorkerPool(cfg)

	// Initially no workers, should return nil
	worker := pool.findWorkerWithCapacity()
	if worker != nil {
		t.Error("expected nil when no workers exist")
	}

	// Create workers manually and test capacity
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w1 := &Worker{
		id:            0,
		ctx:           ctx,
		cancel:        cancel,
		assignedLoops: make(map[*agent.AgentLoop]bool),
		maxLoops:      3,
	}

	// Fill w1 to capacity
	for i := 0; i < 3; i++ {
		w1.assignedLoops[agent.NewAgentLoop("test", "/tmp")] = true
	}

	pool.workers = append(pool.workers, w1)

	// w1 is at capacity, should return nil
	worker = pool.findWorkerWithCapacity()
	if worker != nil {
		t.Error("expected nil when all workers at capacity")
	}

	// Add w2 with capacity
	w2 := &Worker{
		id:            1,
		ctx:           ctx,
		cancel:        cancel,
		assignedLoops: make(map[*agent.AgentLoop]bool),
		maxLoops:      3,
	}
	w2.assignedLoops[agent.NewAgentLoop("test", "/tmp")] = true // 1 assignment
	pool.workers = append(pool.workers, w2)

	// Should find w2
	worker = pool.findWorkerWithCapacity()
	if worker != w2 {
		t.Error("expected to find worker with capacity")
	}
}

// TestWorkerPool_MultipleStartStop tests that Start/Stop can be called
// multiple times without panicking.
func TestWorkerPool_MultipleStartStop(t *testing.T) {
	pool := NewWorkerPool(DefaultWorkerPoolConfig())

	for i := 0; i < 5; i++ {
		pool.Start()
		pool.Stop()
	}
}

// TestWorkerPool_Multiplexing verifies the 1:5 lazy multiplexing ratio:
// a single worker may accept up to MaxLoopsPerWorker agent loops;
// adding more forces a new worker or returns an error.
func TestWorkerPool_Multiplexing(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        10,
		MaxLoopsPerWorker: 5,
	}
	pool := NewWorkerPool(cfg)
	pool.Start()
	defer pool.Stop()

	wait := make(chan struct{})
	go func() {
		<-wait // never consume -- keep workers busy
	}()

	// Submit 5 distinct loops. All should fit on one worker.
	for i := 0; i < 5; i++ {
		loop := agent.NewAgentLoop("mp-session", "/tmp")
		item := WorkItem{
			Loop:           loop,
			Trigger:        TriggerUserMessage,
			Message:        "test",
			ConversationID: "conv",
		}
		if err := pool.Submit(item); err != nil {
			t.Fatalf("Submit loop %d: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	pool.mu.RLock()
	workersAfterFive := len(pool.workers)
	pool.mu.RUnlock()

	if workersAfterFive != 1 {
		t.Errorf("expected 1 worker for 5 loops, got %d", workersAfterFive)
	}

	// 6th loop must trigger a new worker.
	loop6 := agent.NewAgentLoop("mp-session-2", "/tmp")
	if err := pool.Submit(WorkItem{
		Loop:           loop6,
		Trigger:        TriggerUserMessage,
		Message:        "test",
		ConversationID: "conv-2",
	}); err != nil {
		t.Fatalf("Submit loop 6: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	pool.mu.RLock()
	workersAfterSix := len(pool.workers)
	pool.mu.RUnlock()

	if workersAfterSix != 2 {
		t.Errorf("expected 2 workers for 6 loops, got %d", workersAfterSix)
	}
}

// TestWorkerPool_ConcurrentSubmit tests thread safety of Submit when
// called from many goroutines simultaneously.  Run with -race to
// verify no data races.
func TestWorkerPool_ConcurrentSubmit(t *testing.T) {
	cfg := WorkerPoolConfig{
		MaxWorkers:        20,
		MaxLoopsPerWorker: 10,
	}
	pool := NewWorkerPool(cfg)
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	var successCount, errorCount int64

	numGoroutines := 50
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop := agent.NewAgentLoop("conc-session", "/tmp")
			err := pool.Submit(WorkItem{
				Loop:           loop,
				Trigger:        TriggerUserMessage,
				Message:        "hello",
				ConversationID: "conv",
			})
			if err != nil {
				atomic.AddInt64(&errorCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	// With 20 workers x 10 loops = 200 capacity, all 50 should succeed.
	if successCount != int64(numGoroutines) {
		t.Errorf("expected %d successes, got %d", numGoroutines, successCount)
	}

	pool.mu.RLock()
	actualWorkers := len(pool.workers)
	pool.mu.RUnlock()

	// Multiplexing means fewer workers than goroutines.
	if actualWorkers >= numGoroutines {
		t.Errorf("expected fewer than %d workers (multiplexing), got %d",
			numGoroutines, actualWorkers)
	}
}

// TestWorkerPool_ConcurrentStop verifies that Stop is safe while
// goroutines are concurrently submitting work.
func TestWorkerPool_ConcurrentStop(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig{
		MaxWorkers:        5,
		MaxLoopsPerWorker: 10,
	})
	pool.Start()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.Submit(WorkItem{
				Loop:           agent.NewAgentLoop("stop-conc", "/tmp"),
				Trigger:        TriggerUserMessage,
				ConversationID: "conv",
			})
		}()
	}

	// Stop concurrently with submits.
	pool.Stop()
	wg.Wait()
}
