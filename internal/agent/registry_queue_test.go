package agent

import (
	"errors"
	"sync"
	"testing"
)

func newTestRegistry() *AgentRegistry {
	return NewAgentRegistry(RegistryConfig{})
}

func TestRegistry_RegisterActiveQueue(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()

	gen := r.RegisterActiveQueue("conv-1", q)
	if gen == 0 {
		t.Error("generation should be > 0")
	}

	got, gotGen := r.GetActiveQueue("conv-1")
	if got == nil {
		t.Fatal("GetActiveQueue returned nil queue")
	}
	if got != q {
		t.Error("GetActiveQueue returned wrong queue pointer")
	}
	if gotGen != gen {
		t.Errorf("generation = %d, want %d", gotGen, gen)
	}
}

func TestRegistry_RegisterActiveQueue_MultipleGenerations(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q1 := NewMessageQueue()
	q2 := NewMessageQueue()

	gen1 := r.RegisterActiveQueue("conv-1", q1)
	gen2 := r.RegisterActiveQueue("conv-2", q2)

	if gen1 == gen2 {
		t.Errorf("generations should differ: gen1=%d gen2=%d", gen1, gen2)
	}

	got1, gotGen1 := r.GetActiveQueue("conv-1")
	if got1 != q1 {
		t.Error("conv-1 returned wrong queue")
	}
	if gotGen1 != gen1 {
		t.Errorf("conv-1 generation = %d, want %d", gotGen1, gen1)
	}

	got2, gotGen2 := r.GetActiveQueue("conv-2")
	if got2 != q2 {
		t.Error("conv-2 returned wrong queue")
	}
	if gotGen2 != gen2 {
		t.Errorf("conv-2 generation = %d, want %d", gotGen2, gen2)
	}
}

func TestRegistry_UnregisterActiveQueue(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()
	r.RegisterActiveQueue("conv-1", q)

	r.UnregisterActiveQueue("conv-1")

	got, gotGen := r.GetActiveQueue("conv-1")
	if got != nil {
		t.Error("GetActiveQueue should return nil after unregister")
	}
	if gotGen != 0 {
		t.Errorf("generation = %d, want 0 after unregister", gotGen)
	}
}

func TestRegistry_UnregisterActiveQueue_ClosesQueue(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()
	r.RegisterActiveQueue("conv-1", q)

	r.UnregisterActiveQueue("conv-1")

	if !q.IsClosed() {
		t.Error("queue should be closed after unregister")
	}
}

func TestRegistry_UnregisterActiveQueue_Idempotent(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()

	// Should not panic on non-existent conversation.
	r.UnregisterActiveQueue("does-not-exist")
	r.UnregisterActiveQueue("also-does-not-exist")
}

func TestRegistry_GetActiveQueue_NotFound(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()

	got, gotGen := r.GetActiveQueue("nonexistent")
	if got != nil {
		t.Error("expected nil queue for missing conversation")
	}
	if gotGen != 0 {
		t.Errorf("generation = %d, want 0 for missing conversation", gotGen)
	}
}

func TestRegistry_GetQueueWithVersion_Success(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()
	gen := r.RegisterActiveQueue("conv-1", q)

	got, err := r.GetQueueWithVersion("conv-1", gen)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != q {
		t.Error("returned wrong queue pointer")
	}
}

func TestRegistry_GetQueueWithVersion_NotFound(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()

	_, err := r.GetQueueWithVersion("nonexistent", 1)
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("error = %v, want ErrQueueNotFound", err)
	}
}

func TestRegistry_GetQueueWithVersion_GenerationMismatch(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()
	gen := r.RegisterActiveQueue("conv-1", q)

	_, err := r.GetQueueWithVersion("conv-1", gen+1)
	if !errors.Is(err, ErrGenerationMismatch) {
		t.Errorf("error = %v, want ErrGenerationMismatch", err)
	}
}

func TestRegistry_GetQueueWithVersion_ClosedQueue(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	q := NewMessageQueue()
	gen := r.RegisterActiveQueue("conv-1", q)

	// Close the queue without unregistering (simulates external close).
	q.Close()

	_, err := r.GetQueueWithVersion("conv-1", gen)
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("error = %v, want ErrQueueClosed", err)
	}
}

func TestRegistry_DB(t *testing.T) {
	r := newTestRegistry()

	if db := r.DB(); db != nil {
		t.Errorf("DB() = %v, want nil when no DB configured", db)
	}
}

func TestRegistry_ConcurrentRegistration(t *testing.T) {
	r := newTestRegistry()

	const goroutines = 50
	var wg sync.WaitGroup

	// Phase 1: Concurrent registration for different conversations.
	regs := make([]struct {
		id  string
		gen uint64
	}, goroutines)
	for i := range goroutines {
		regs[i].id = "conv-" + string(rune('0'+i%10)) + "-" + string(rune('0'+i/10))
	}

	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			q := NewMessageQueue()
			regs[idx].gen = r.RegisterActiveQueue(regs[idx].id, q)
		}(i)
	}
	wg.Wait()

	// Phase 2: Concurrent reads for all registered conversations.
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			got, gotGen := r.GetActiveQueue(regs[idx].id)
			if got == nil {
				t.Errorf("GetActiveQueue(%q) returned nil", regs[idx].id)
				return
			}
			if gotGen != regs[idx].gen {
				t.Errorf("GetActiveQueue(%q) gen = %d, want %d", regs[idx].id, gotGen, regs[idx].gen)
			}
		}(i)
	}
	wg.Wait()

	// Phase 3: Concurrent unregistration.
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			r.UnregisterActiveQueue(regs[idx].id)
		}(i)
	}
	wg.Wait()

	// All should be gone.
	for i := range goroutines {
		got, _ := r.GetActiveQueue(regs[i].id)
		if got != nil {
			t.Errorf("GetActiveQueue(%q) should return nil after unregister", regs[i].id)
		}
	}
}
