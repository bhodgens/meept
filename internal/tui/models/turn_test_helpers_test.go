package models

// Test-facing helpers for the async-turn router/await contract (leaf 04).
// Deadcode wave-3 moved this whole block OUT of production turn.go: none
// of it is called by production code (the chat view drives the router via
// register/get/hasPendingFor and pendingTurn's own methods), so it compiles
// only into the test build. Tests live in this package now (the await-level
// tests moved over from package tui), so everything here can stay
// unexported — the production binary ships none of it.

import (
	"context"
	"sync"
	"time"
)

// fireTimers exposes the manual timer source's FireTimers through the
// router for tests that hold only the router.
func (r *turnRouter) fireTimers() {
	if mts, ok := r.timers.(*manualTimerSource); ok {
		mts.FireTimers()
	}
}

// armedTimers exposes the manual timer source's armed count through the
// router for tests that must synchronize with awaitTurnCmd's arming.
func (r *turnRouter) armedTimers() int {
	if mts, ok := r.timers.(*manualTimerSource); ok {
		return mts.ArmedTimers()
	}
	return 0
}

// deliverTestTerminal routes a terminal result by turn id; returns false
// when no tracked turn matches (the turn_id filtering behavior).
func (r *turnRouter) deliverTestTerminal(turnID, reply, status string) bool {
	r.mu.Lock()
	pt, ok := r.turns[turnID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	pt.notify(turnTerminalMsg{
		TurnID:         turnID,
		ConversationID: pt.conversationID,
		Reply:          reply,
		Status:         status,
	})
	return true
}

// deliverTestProgress refreshes liveness for a conversation's turns.
func (r *turnRouter) deliverTestProgress(conversationID, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pt := range r.turns {
		if pt.conversationID == conversationID {
			pt.bumpActivity()
			pt.mu.Lock()
			if text != "" {
				pt.progressText = text
			}
			pt.mu.Unlock()
		}
	}
}

// noopTimerSource never fires — used with liveness=0 tests.
type noopTimerSource struct{}

// After implements timerSource by returning a channel that never fires.
func (noopTimerSource) After(ctx context.Context, _ time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		<-ctx.Done()
	}()
	return ch
}

// manualTimer is one armed timer the test can fire synchronously.
type manualTimer struct {
	fire chan struct{}
}

// manualTimerSource is a timerSource whose timers fire only when the test
// calls FireTimers — zero real sleeps in liveness tests.
type manualTimerSource struct {
	mu     sync.Mutex
	timers []*manualTimer
}

// NewManualTimerSource creates a manually-fired timer source.
func NewManualTimerSource() *manualTimerSource {
	return &manualTimerSource{}
}

// After implements timerSource by arming a manual timer.
func (m *manualTimerSource) After(ctx context.Context, _ time.Duration) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := &manualTimer{fire: make(chan struct{}, 1)}
	m.timers = append(m.timers, t)
	go func() {
		<-ctx.Done()
		// Cancel: drop the timer so FireTimers after ctx death is a no-op.
		m.mu.Lock()
		for i, existing := range m.timers {
			if existing == t {
				m.timers = append(m.timers[:i], m.timers[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
	}()
	return t.fire
}

// FireTimers fires every armed timer exactly once, synchronously.
func (m *manualTimerSource) FireTimers() {
	m.mu.Lock()
	firing := m.timers
	m.timers = nil
	m.mu.Unlock()
	for _, t := range firing {
		select {
		case t.fire <- struct{}{}:
		default:
		}
	}
}

// ArmedTimers returns how many manual timers are currently armed.
func (m *manualTimerSource) ArmedTimers() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.timers)
}
