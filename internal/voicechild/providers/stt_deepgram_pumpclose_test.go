package providers

// DEFECT 4 (high) regression: the Deepgram pump goroutine must NOT spin on a
// closed pcm channel.
//
// Mechanism of the bug: `case chunk, ok := <-pcm` on a CLOSED channel is
// always ready, and the old body did `continue` after sending Finalize, so
// the select re-took the same case and re-sent `{"type":"Finalize"}` as fast
// as the CPU allows - pinning a core and flooding the vendor with Finalize
// frames. It triggered whenever the child closed p.pcmCh (in-read error /
// teardown).
//
// Two assertions pin the fix:
//  1. exactly ONE Finalize frame reaches the vendor after the close, and
//     the pump goroutine exits (no spin, no leak), and
//  2. under the OLD code this test fails on the Finalize count (hundreds of
//     thousands of frames land in a few ms), so it is a real regression.

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// lineCountingWS is a minimal wsSession that records the payload of every
// text write and hands back a transcript-ish stream only when the test says
// so. It never blocks the pump's write path.
type lineCountingWS struct {
	mu      sync.Mutex
	texts   []string
	bins    int
	readErr chan struct{} // closed to make Read fail (session teardown)
}

func (w *lineCountingWS) Write(ctx context.Context, messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if messageType == 1 {
		w.texts = append(w.texts, string(data))
	} else {
		w.bins++
	}
	return nil
}

func (w *lineCountingWS) Read(ctx context.Context) (int, []byte, error) {
	<-w.readErr
	return 0, nil, context.Canceled
}

func (w *lineCountingWS) Close() error { return nil }

func (w *lineCountingWS) finalizeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, t := range w.texts {
		if strings.Contains(t, `"Finalize"`) {
			n++
		}
	}
	return n
}

func (w *lineCountingWS) binaryCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bins
}

// TestDeepgramPumpExitsOnceOnClosedPCM: close the pcm channel while the
// reader is still open, and assert the pump (a) sends exactly one Finalize,
// (b) forwards no binary after the close, and (c) exits promptly (the
// goroutine does not spin).
func TestDeepgramPumpExitsOnceOnClosedPCM(t *testing.T) {
	ws := &lineCountingWS{readErr: make(chan struct{})}
	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	s.setSTTDialer(func(ctx context.Context, base, path string, header wsHeader) (wsSession, error) {
		return ws, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm := make(chan []byte)
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	pcm <- pcmS16LE(160) // one chunk so the pump is demonstrably live
	close(pcm)           // <-- the trigger: closed channel is always ready

	// Give the pump ample time to spin if it is broken. The old code
	// re-sent Finalize in a tight loop, so a generous budget here only
	// makes the failure louder.
	time.Sleep(150 * time.Millisecond)

	if got := ws.finalizeCount(); got != 1 {
		t.Fatalf("Finalize frames after pcm close = %d, want exactly 1 (pump is spinning on the closed channel)", got)
	}
	if got := ws.binaryCount(); got != 1 {
		t.Fatalf("binary frames = %d, want 1 (only the chunk sent before close)", got)
	}

	// The pump must be gone: it may only still be parked on readCtx.Done()
	// because the reader owns the session. Tearing the reader down must
	// release it with no further writes.
	close(ws.readErr)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ws.finalizeCount() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := ws.finalizeCount(); got != 1 {
		t.Fatalf("Finalize frames after teardown = %d, want exactly 1", got)
	}

	// The transcript channel must close (run() returned) - a spun pump
	// would keep the reader loop alive and the channel open.
	select {
	case _, ok := <-out:
		if ok {
			t.Fatal("unexpected transcript with no vendor result")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream never closed: pump/reader did not unwind after pcm close")
	}
}

// TestDeepgramPumpServesPCMThenFlush: a normal stream (open channel) still
// forwards binary and never sends Finalize early, so the fix did not break
// the hot path.
func TestDeepgramPumpServesPCMThenFlush(t *testing.T) {
	ws := &lineCountingWS{readErr: make(chan struct{})}
	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	s.setSTTDialer(func(ctx context.Context, base, path string, header wsHeader) (wsSession, error) {
		return ws, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm := make(chan []byte)
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range 3 {
		pcm <- pcmS16LE(160)
	}
	time.Sleep(100 * time.Millisecond)
	if got := ws.finalizeCount(); got != 0 {
		t.Fatalf("Finalize sent while the channel is still open: %d", got)
	}
	if got := ws.binaryCount(); got != 3 {
		t.Fatalf("binary frames = %d, want 3", got)
	}
	close(pcm)
	deadline := time.Now().Add(2 * time.Second)
	for ws.finalizeCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := ws.finalizeCount(); got != 1 {
		t.Fatalf("Finalize frames after close = %d, want exactly 1", got)
	}
	close(ws.readErr)
	cancel()
	<-out
}

// keep bufio/net imported for the helper shapes used by sibling tests in
// this package (avoids churn if the fakes move).
var (
	_ = bufio.NewReader
	_ = net.Dial
)
