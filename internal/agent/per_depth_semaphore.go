package agent

import (
	"fmt"
	"sync"
)

// PerDepthSemaphore manages one semaphore per spawnable depth to prevent
// deadlock while bounding parallelism. Each depth has an independent channel
// so that releasing depth 2 never unblocks depth 1.
//
// Modeled after HALO's per-depth semaphore pool (subagent_tool_factory.py).
//
// Single-semaphore deadlock scenario: with N parents holding every depth-N
// slot on a single shared semaphore, every depth-(N+1) grandchild blocks
// forever waiting for a slot its parent is holding. Per-depth pools avoid
// this because a parent at depth-N contends on semaphores[N] while its child
// at depth-(N+1) contends on semaphores[N+1].
type PerDepthSemaphore struct {
	mu       sync.Mutex
	channels map[int]chan struct{}
	limits   map[int]int
}

// NewPerDepthSemaphore creates a semaphore pool. The map maps depth -> max
// concurrent acquires. Depths not listed default to 0 (no spawning allowed).
func NewPerDepthSemaphore(limits map[int]int) *PerDepthSemaphore {
	p := &PerDepthSemaphore{
		channels: make(map[int]chan struct{}),
		limits:   limits,
	}
	for depth, limit := range limits {
		if limit <= 0 {
			// Use a closed channel to instantly block all acquires.
			ch := make(chan struct{})
			close(ch)
			p.channels[depth] = ch
		} else {
			ch := make(chan struct{}, limit)
			p.channels[depth] = ch
		}
	}
	return p
}

// Acquire blocks until a slot is available at the given depth.
// Returns an error if the depth is not registered.
func (p *PerDepthSemaphore) Acquire(depth int) error {
	p.mu.Lock()
	ch, ok := p.channels[depth]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("depth %d not registered in semaphore pool", depth)
	}

	ch <- struct{}{}
	return nil
}

// Release returns a slot to the semaphore at the given depth.
// Double-release is silently ignored.
func (p *PerDepthSemaphore) Release(depth int) {
	p.mu.Lock()
	ch, ok := p.channels[depth]
	p.mu.Unlock()

	if !ok {
		return
	}

	select {
	case <-ch:
	default:
		// Double-release protection.
	}
}
