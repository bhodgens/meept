package main

// Bounded transcript read pins (defect 2026-11-14): printLocalCallTranscript
// previously called client.Call RAW, so a wedged unix socket held the CLI's
// exit open for the RPC client's full multi-minute retry ladder — violating
// the bounded-round-trip contract (C-11) the events tracker already
// implements. Each transcript attempt is now bounded by
// transcriptReadTimeout; the flush-race retry loop stays.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// blockingTranscriptClient blocks forever in Call — the wedged-socket
// shape. An abandoned Call's return is discarded (buffered channel).
type blockingTranscriptClient struct{}

func (blockingTranscriptClient) Call(_ string, _ any) (json.RawMessage, error) {
	select {} // never returns: the wedged socket
}

// TestPrintLocalCallTranscriptBoundedOnWedgedSocket: a wedged socket must
// NOT hold call end for the client's retry ladder. The whole bounded retry
// loop (attempts x timeout + sleeps) completes well inside a generous
// wall-clock budget and prints the honest degrade.
func TestPrintLocalCallTranscriptBoundedOnWedgedSocket(t *testing.T) {
	oldTimeout := transcriptReadTimeout
	transcriptReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { transcriptReadTimeout = oldTimeout })

	start := time.Now()
	out := captureStdout(t, func() {
		printLocalCallTranscript(blockingTranscriptClient{}, "local-wedge", "emp")
	})
	elapsed := time.Since(start)

	// 4 attempts x 50 ms + 3 x 200 ms sleeps = 800 ms worst case; the
	// pre-fix code hung on the unbounded client.Call instead. Generous
	// 5 s budget proves boundedness, not a timing race.
	if elapsed > 5*time.Second {
		t.Fatalf("transcript read hung on a wedged socket: %v (must be bounded)", elapsed)
	}
	if !strings.Contains(out, "(no transcript lines)") {
		t.Fatalf("wedged-socket path must print the honest degrade:\n%s", out)
	}
}

// slowThenHeldClient: the first Call wedges, but the bounded read must
// return at its deadline — the child goroutine is abandoned, not joined.
type wedgeCountClient struct {
	calls int
}

func (c *wedgeCountClient) Call(_ string, _ any) (json.RawMessage, error) {
	c.calls++
	select {}
}

// TestCallTranscriptBoundedReturnsAtDeadline: the helper itself returns
// (ok=false) at the timeout rather than blocking on the client.
func TestCallTranscriptBoundedReturnsAtDeadline(t *testing.T) {
	oldTimeout := transcriptReadTimeout
	transcriptReadTimeout = 40 * time.Millisecond
	t.Cleanup(func() { transcriptReadTimeout = oldTimeout })

	c := &wedgeCountClient{}
	start := time.Now()
	_, ok := callTranscriptBounded(c, "local-x")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("callTranscriptBounded exceeded its deadline: %v", elapsed)
	}
	if ok {
		t.Fatal("wedged call must report ok=false")
	}
	if c.calls != 1 {
		t.Fatalf("expected exactly one underlying Call, got %d", c.calls)
	}
}
