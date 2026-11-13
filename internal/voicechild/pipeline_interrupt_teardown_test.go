package voicechild

// D2 regression (defect 2026-11-13): awaitPendingInterrupt parked on the
// RMS-trip interrupt handshake with no ctx escape. The done channel is
// closed only by the emit goroutine; when the pipeline was torn down while
// that goroutine was still wedged in emitInterrupt, the transcript
// consumer rode the worst-case 2 s socket-write deadline (or, if the done
// never closed, blocked forever). Contract: teardown releases a parked
// consumer via p.ctx — close() cancels the ctx BEFORE it clears the
// handshake field under playMu, so the select arms the escape and later
// callers take the nil no-op path.

import (
	"context"
	"testing"
	"time"
)

func TestAwaitPendingInterruptTeardownEscape(t *testing.T) {
	const ccid = "intctreq1"
	_, _, p, gate := tripLocal(t, ccid, 500*time.Millisecond)
	close(gate.park) // release the parked playback before teardown

	// Hand the pipeline an interrupt handshake that NEVER closes — the
	// emit goroutine is by hypothesis wedged in the text-socket write.
	neverClosed := make(chan struct{})
	p.playMu.Lock()
	p.pendingInterrupt = neverClosed
	p.playMu.Unlock()

	returned := make(chan struct{})
	go func() {
		p.awaitPendingInterrupt()
		close(returned)
	}()

	// Give the consumer a moment to park on the never-closing done.
	select {
	case <-returned:
		t.Fatal("awaitPendingInterrupt returned before teardown (it did not park on the handshake)")
	case <-time.After(30 * time.Millisecond):
	}

	start := time.Now()
	p.close(context.Background())
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatalf("awaitPendingInterrupt never released on teardown (ctx escape missing)")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("teardown release took %s: the consumer rode the write deadline instead of the ctx escape", el)
	}

	// close() must have retired the handshake field (under playMu), so a
	// fresh consumer takes the no-op path immediately.
	p.playMu.Lock()
	stale := p.pendingInterrupt != nil
	p.playMu.Unlock()
	if stale {
		t.Fatal("close() left a stale pendingInterrupt handshake (post-teardown callers must see the nil no-op)")
	}
	done := make(chan struct{})
	go func() {
		p.awaitPendingInterrupt()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("awaitPendingInterrupt blocked on a cleared handshake after teardown")
	}
}
