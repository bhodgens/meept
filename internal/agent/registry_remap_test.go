package agent

// 01-safety TEST GATE T4 (00T-gate.md): thread-remap queue lookup (C3).
//
// GetActiveQueue is an exact map lookup today. RunOnce registers the queue
// under the THREAD-scoped conversation id the thread router minted
// (conv-X-thread-work-ab12), but chat.steer / the TUI only know the
// session-level id (conv-X) -- steering an active turn returns "queue not
// found" on both ids (F-NEW-11).
//
// C3 resolution order: exact key -> thread-unwrapped base -> session-owned
// base+"-thread-*" keys; an ambiguous multi-thread match is an EXPLICIT
// error, never a silent pick.
//
// RED today: the remap seam does not exist (undefined:
// ResolveActiveQueue) and GetActiveQueue misses the thread registration.

import (
	"errors"
	"testing"
)

// TestThreadRemapLookup pins the single-thread remap: a queue registered
// under conv-X-thread-work-ab12 must resolve for a lookup of conv-X.
func TestThreadRemapLookup(t *testing.T) {
	r := newTestRegistry()
	q := NewMessageQueue()
	threadKey := "conv-X-thread-work-ab12"
	r.RegisterActiveQueue(threadKey, q, nil)

	// Exact lookup still works (resolution order step 1).
	exact, _ := r.GetActiveQueue(threadKey)
	if exact != q {
		t.Fatalf("exact-key lookup broke: got %v, want the registered queue", exact)
	}

	// The C3 remap seam the 02-queue-lifecycle leaf adds: resolution
	// order exact -> unwrapped base -> session-owned thread keys.
	got, err := r.ResolveActiveQueue("conv-X")
	if err != nil {
		t.Fatalf("ResolveActiveQueue(conv-X): %v", err)
	}
	if got != q {
		t.Errorf("remap resolved %v, want the queue registered under %s", got, threadKey)
	}

	// The existing exact-only lookup must learn the same remap (the
	// chat.steer handler and ChatService.Steer call GetActiveQueue).
	base, _ := r.GetActiveQueue("conv-X")
	if base == nil {
		t.Errorf("CURRENT DEFECT: GetActiveQueue(conv-X) misses the queue registered under %s (exact-only lookup, no thread remap)", threadKey)
	}
}

// TestThreadRemapAmbiguousErrors pins the ambiguity contract: two live
// thread queues under one session base must produce an EXPLICIT error,
// never a silent arbitrary pick (cross-thread steering would inject into
// the wrong work stream).
func TestThreadRemapAmbiguousErrors(t *testing.T) {
	r := newTestRegistry()
	qA := NewMessageQueue()
	qB := NewMessageQueue()
	r.RegisterActiveQueue("conv-Y-thread-work-aa01", qA, nil)
	r.RegisterActiveQueue("conv-Y-thread-work-bb02", qB, nil)

	got, err := r.ResolveActiveQueue("conv-Y")
	if err == nil {
		t.Fatalf("ambiguous two-thread match resolved silently to %v; C3 requires an explicit error", got)
	}
	if !errors.Is(err, ErrAmbiguousQueue) {
		t.Errorf("ambiguity error = %v, want ErrAmbiguousQueue (explicit, actionable)", err)
	}
}

// TestThreadRemapExactWins pins resolution order: an exact registration
// for the base id outranks any session-owned thread keys.
func TestThreadRemapExactWins(t *testing.T) {
	r := newTestRegistry()
	baseQueue := NewMessageQueue()
	threadQueue := NewMessageQueue()
	r.RegisterActiveQueue("conv-Z", baseQueue, nil)
	r.RegisterActiveQueue("conv-Z-thread-work-cc03", threadQueue, nil)

	got, err := r.ResolveActiveQueue("conv-Z")
	if err != nil {
		t.Fatalf("ResolveActiveQueue with exact registration: %v", err)
	}
	if got != baseQueue {
		t.Errorf("exact key must win over thread remap: got %v, want the base registration", got)
	}
}

// TestThreadRemapNotFound pins the miss contract: no registration at all
// resolves to ErrQueueNotFound (which the HTTP layer maps to 404).
func TestThreadRemapNotFound(t *testing.T) {
	r := newTestRegistry()
	got, err := r.ResolveActiveQueue("conv-missing")
	if got != nil || !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("ResolveActiveQueue(missing) = (%v, %v), want (nil, ErrQueueNotFound)", got, err)
	}
}
