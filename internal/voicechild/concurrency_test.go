package voicechild

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// serialTTS is a TTS whose Speak calls must never overlap AND whose audio
// carries a per-stream ordinal byte: every 640-byte chunk its reader
// hands out is filled with the Speak call number (1-based). The test
// decodes the .out socket stream and asserts the chunks form exactly one
// contiguous run of N chunks per ordinal, in ascending order — the FIFO
// drain signature. With the old per-say goroutines, two streams wrote PCM
// to the same audioOut socket unsynchronized and the daemon heard
// interleaved chunks; overlapping Speak calls are also recorded directly.
type serialTTS struct {
	mu       sync.Mutex
	active   bool
	overlaps int
	speaks   int
}

func (s *serialTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	s.mu.Lock()
	s.speaks++
	ordinal := s.speaks
	if s.active {
		s.overlaps++
	}
	s.mu.Unlock()
	return &serialReader{s: s, chunks: 3, ordinal: byte(ordinal)}, nil
}

func (s *serialTTS) counts() (speaks, overlaps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speaks, s.overlaps
}

type serialReader struct {
	s       *serialTTS
	chunks  int
	ordinal byte
	started bool
}

func (r *serialReader) Read(p []byte) (int, error) {
	if r.chunks <= 0 {
		return 0, io.EOF
	}
	if !r.started {
		r.started = true
		r.s.mu.Lock()
		r.s.active = true
		r.s.mu.Unlock()
	}
	r.chunks--
	n := len(p)
	if n > 640 {
		n = 640
	}
	for i := 0; i < n; i++ {
		p[i] = r.ordinal
	}
	return n, nil
}

func (r *serialReader) Close() error {
	if r.started {
		r.s.mu.Lock()
		r.s.active = false
		r.s.mu.Unlock()
	}
	return nil
}

// TestConcurrentSaysSerialize (audit fix 1): many say frames arriving at
// once (the normal supervisor path — sentence N+1 checked while N plays)
// must be drained by ONE playback worker per pipeline. With the old
// per-say goroutines, two streams wrote PCM to the same audioOut socket
// unsynchronized and the daemon heard interleaved chunks. The serial TTS
// pins the invariant structurally: Speak must never be live while another
// Speak's reader is live, for the whole burst.
func TestConcurrentSaysSerialize(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cs1")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cs1", Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cs1")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline("cs1")
	tts := &serialTTS{}
	swapTTS(p, tts)

	const burst = 8
	// Fire the whole burst through the REAL handler concurrently, exactly
	// as the daemon read loop would deliver back-to-back say frames.
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			frame := voiceframe.Frame{CallID: "cs1", Event: voiceframe.EventSay, Text: fmt.Sprintf("sentence %d", i), Last: lastPtr(true)}
			if err := child.handleSay(frame); err != nil {
				t.Errorf("handleSay %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Drain everything the worker writes: 3 chunks x burst frames x 640 B.
	total := burst * 3 * 640
	got := make(chan []byte, 1)
	go func() {
		_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, total)
		n, _ := io.ReadFull(out, buf)
		got <- buf[:n]
	}()
	var stream []byte
	select {
	case stream = <-got:
	case <-time.After(12 * time.Second):
		t.Fatalf("FIFO drain never produced all %d bytes", total)
	}
	if len(stream) != total {
		t.Fatalf("drained %d bytes, want %d (whole burst must play serially, none lost)", len(stream), total)
	}
	// FIFO signature: exactly `burst` runs of 3*640 bytes, run k all of
	// ordinal byte k+1. Interleaving (the pre-fix defect) shows up as a
	// run pattern that is not [1^n, 2^n, ...]. Socket coalescing can only
	// MERGE adjacent same-ordinal bytes, never reorder them.
	const run = 3 * 640
	for i, b := range stream {
		wantRun := i/run + 1
		if b != byte(wantRun) {
			t.Fatalf("FIX 1 VIOLATION: byte %d = %d, want ordinal %d — playback streams interleaved on audioOut (not FIFO-serialized)", i, b, wantRun)
		}
	}
	speaks, overlaps := tts.counts()
	if overlaps != 0 {
		t.Fatalf("FIX 1 VIOLATION: %d Speak overlaps — two playback streams hit audioOut concurrently", overlaps)
	}
	if speaks != burst {
		t.Fatalf("Speak called %d times, want %d (every queued frame played exactly once)", speaks, burst)
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

// TestClearDropsQueuedDrafts (audit fix 1 + F-1 interaction): a clear
// retires the CURRENT stream and every draft queued BEFORE it; a say
// after the clear (new generation) still plays — the F-1 re-arm must keep
// working through the queue.
func TestClearDropsQueuedDrafts(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cd1")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cd1", Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cd1")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline("cd1")

	// Stream 1 parks the worker mid-write; drafts 2+3 queue behind it.
	gate := newGatedTTS()
	swapTTS(p, gate)
	d.sendLine(childConn, voiceframe.Frame{CallID: "cd1", Event: voiceframe.EventSay, Text: "current", Last: lastPtr(true)})
	select {
	case <-gate.enteredPlay:
	case <-time.After(5 * time.Second):
		t.Fatalf("playback never started")
	}
	d.sendLine(childConn, voiceframe.Frame{CallID: "cd1", Event: voiceframe.EventSay, Text: "queued-1", Last: lastPtr(true)})
	d.sendLine(childConn, voiceframe.Frame{CallID: "cd1", Event: voiceframe.EventSay, Text: "queued-2", Last: lastPtr(true)})
	waitFor(t, func() bool { return p.queuedSayCount() == 2 })

	// Clear: retire stream 1 and the two queued drafts of the old gen.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cd1", Event: voiceframe.EventClear})
	waitFor(t, p.isDropped)
	close(gate.releaseSecond) // stale stream aborts on gen mismatch

	// The two queued drafts must be dropped, not played after the current
	// stream ends: wait for the queue to drain (worker skips retired drafts).
	waitFor(t, func() bool { return p.queuedSayCount() == 0 })

	// F-1 re-arm through the FIFO: a fresh say after the clear plays.
	stub := newChunkedTTS(quietSample, 2)
	swapTTS(p, stub)
	defer close(stub.park)
	d.sendLine(childConn, voiceframe.Frame{CallID: "cd1", Event: voiceframe.EventSay, Text: "after-clear", Last: lastPtr(true)})
	_ = out.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1280)
	if n, err := io.ReadFull(out, buf); n != 1280 || err != nil {
		t.Fatalf("say after clear never played (n=%d err=%v): F-1 re-arm broke in the FIFO rewrite", n, err)
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	<-runDone
}

// Self-verification checklist: two ccids in one child, -race clean. Both
// pipelines register from setup frames, both receive say -> first PCM chunk
// on their own .out socket, and end tears down exactly that call.
func TestConcurrentPipelines(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("ca")
	d.listenPair("cb")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()

	for _, ccid := range []string{"ca", "cb"} {
		d.sendLine(childConn, voiceframe.Frame{
			CallID: ccid, Event: voiceframe.EventSetup,
			Codec: "L16", SampleRate: 16000,
		})
	}
	inA, outA := d.acceptAll("ca")
	inB, outB := d.acceptAll("cb")
	defer func() { _ = inA.Close() }()
	defer func() { _ = outA.Close() }()
	defer func() { _ = inB.Close() }()
	defer func() { _ = outB.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })

	// say to both calls; each .out must get its own stub audio.
	d.sendLine(childConn, voiceframe.Frame{CallID: "ca", Event: voiceframe.EventSay, Text: "a speech", Last: lastPtr(true)})
	d.sendLine(childConn, voiceframe.Frame{CallID: "cb", Event: voiceframe.EventSay, Text: "b speech", Last: lastPtr(true)})

	type read struct {
		ccid string
		n    int
		buf  []byte
	}
	got := make(chan read, 2)
	readOne := func(ccid string, conn interface {
		Read([]byte) (int, error)
	}) {
		buf := make([]byte, 640)
		go func() {
			n, _ := conn.Read(buf)
			got <- read{ccid: ccid, n: n, buf: buf[:n]}
		}()
	}
	readOne("ca", outA)
	readOne("cb", outB)

	seen := make(map[string]int, 2)
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case r := <-got:
			seen[r.ccid] = r.n
		case <-deadline:
			t.Fatalf("timed out waiting for audio on both .out sockets; seen=%v", seen)
		}
	}
	for ccid, n := range seen {
		if n != 640 {
			t.Fatalf("call %s first chunk = %d bytes, want 640", ccid, n)
		}
	}

	// end one call; the other must stay live.
	d.sendLine(childConn, voiceframe.Frame{CallID: "ca", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	if _, ok := child.getPipeline("cb"); !ok {
		t.Fatalf("call cb pipeline must survive call ca end")
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
	_ = filepath.Join // keep path/filepath linked
}
