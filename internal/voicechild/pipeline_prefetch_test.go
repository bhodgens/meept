package voicechild

// TTS pipeline serialization fix (2026-11-09): synthesis of sentence N+1
// must START while sentence N's audio is still being paced to the .out
// socket, never wait for N to fully play. Pre-fix the worker called
// tts.Speak synchronously inside the drain loop, so every sentence break
// cost the provider's full generation latency (qwen3 does not stream;
// 0.7-4.8 s) at the speakers — the daemon logged it as
// "localsplice pacer stall gap_ms=4620/6963/1238" while replies were
// mid-flight.
//
// The fake TTS makes both halves observable with wall-clock timestamps:
// Speak blocks for a configured "synthesis delay" (mirroring the real
// full-generation server), records its call time and cancellation, and
// the returned reader timestamps every chunk hand-off toward the socket.
// The tests drain .out like the daemon pacer (one 640 B chunk per
// interval); with the unix sndbuf full, the worker blocks on Write, so
// the provider-side read timestamps spread over the real pacing window.
//
//   TestPrefetchOverlapsPacing               — overlap contract + FIFO bytes.
//   TestPrefetchDepthBound                   — at most TWO Speak in flight.
//   TestPrefetchSecondSlotLaunchesDuringPlay — depth-2 lookahead overlap.
//   TestClearDuringPrefetchDiscardsLookahead — prefetched PCM discarded.
//   TestClearDuringSynthAbortsSpeak          — synth aborts on clear, next turn plays.
//   TestPlaybackCadenceReportsSynthWriteMs   — synth_ms/write_ms telemetry.

import (
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// pacedTTS: Speak blocks `delay` (recording start + cancellation), then
// hands out `chunks` 640-byte chunks filled with the call ordinal byte
// (1-based), timestamping every read toward the socket and the close.
type pacedTTS struct {
	mu        sync.Mutex
	delay     time.Duration
	chunks    int
	calls     []*pacedCall
	callsMade int // total Speak calls started (call-time ordinals for untagged texts)
	inFlight  int // Speak calls generating right now
	maxFlight int // high-water mark: the prefetch depth bound must keep this <=2
}

type pacedCall struct {
	ordinal   byte
	start     time.Time
	cancelled bool
	done      time.Time // when Speak returned (ok or cancelled)
	reads     []time.Time
	closed    time.Time
	tts       *pacedTTS
}

func newPacedTTS(delay time.Duration, chunks int) *pacedTTS {
	return &pacedTTS{delay: delay, chunks: chunks}
}

func (t *pacedTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	// Frame-derived ordinal when the text carries one ("sentence %d"):
	// with the bounded depth-2 prefetch, two Speak CALLS may enter the
	// provider in either launch order (separate goroutines), so a
	// call-time stamp could mislabel legal ordering. Text-stamped tests
	// pin the FRAME identity; untagged texts keep the call-time ordinal
	// (single-in-flight shapes where entry order is deterministic).
	t.mu.Lock()
	t.inFlight++
	if t.inFlight > t.maxFlight {
		t.maxFlight = t.inFlight
	}
	t.callsMade++
	ordinal := t.callsMade
	var n int
	if _, err := fmt.Sscanf(text, "sentence %d", &n); err == nil {
		ordinal = n + 1 // frame 0 -> ordinal 1 (runs stay 1-based for the FIFO decode)
	}
	call := &pacedCall{ordinal: byte(ordinal), start: time.Now(), tts: t} //nolint:gosec // G115: test ordinal is a small counter, fits a byte
	t.calls = append(t.calls, call)
	delay := t.delay
	chunks := t.chunks
	t.mu.Unlock()

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.mu.Lock()
		t.inFlight--
		call.cancelled = true
		call.done = time.Now()
		t.mu.Unlock()
		return nil, ctx.Err()
	}
	t.mu.Lock()
	t.inFlight--
	call.done = time.Now()
	t.mu.Unlock()
	return &pacedReader{call: call, left: chunks}, nil
}

type pacedSnap struct {
	starts    []time.Time
	cancelled []bool
	firstRead []time.Time
	lastRead  []time.Time
	closed    []time.Time
	speaks    int
	maxFlight int
}

// snapshot returns a race-safe copy of the per-call timeline.
func (t *pacedTTS) snapshot() pacedSnap {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := pacedSnap{speaks: len(t.calls), maxFlight: t.maxFlight}
	for _, c := range t.calls {
		s.starts = append(s.starts, c.start)
		s.cancelled = append(s.cancelled, c.cancelled)
		s.closed = append(s.closed, c.closed)
		if len(c.reads) > 0 {
			s.firstRead = append(s.firstRead, c.reads[0])
			s.lastRead = append(s.lastRead, c.reads[len(c.reads)-1])
		} else {
			s.firstRead = append(s.firstRead, time.Time{})
			s.lastRead = append(s.lastRead, time.Time{})
		}
	}
	return s
}

type pacedReader struct {
	call *pacedCall
	left int
}

func (r *pacedReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	r.left--
	n := min(len(p), 640)
	b := r.call.tts.chunkBytes(r.call.ordinal, n)
	copy(p[:n], b)
	return n, nil
}

func (r *pacedReader) Close() error {
	r.call.tts.mu.Lock()
	if r.call.closed.IsZero() {
		r.call.closed = time.Now()
	}
	r.call.tts.mu.Unlock()
	return nil
}

// chunkBytes hands out one filled chunk and timestamps the provider-side
// read (the moment those bytes pass toward the .out socket write).
func (t *pacedTTS) chunkBytes(ordinal byte, n int) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.calls {
		if c.ordinal == ordinal {
			c.reads = append(c.reads, time.Now())
			break
		}
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ordinal
	}
	return b
}

// paceDrain drains `reads` chunks of 640 B from the .out socket, one per
// interval (daemon pacer twin), verifying each chunk carries a single
// ordinal and recording the per-read ordinal pattern for FIFO asserts.
func paceDrain(t *testing.T, out net.Conn, reads int, interval time.Duration) []byte {
	t.Helper()
	var ordinals []byte
	buf := make([]byte, 640)
	for i := range reads {
		_ = out.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(out, buf); err != nil {
			t.Fatalf("pacer read %d: %v", i, err)
		}
		ord := buf[0]
		for _, b := range buf {
			if b != ord {
				t.Fatalf("pacer read %d: chunk carries mixed ordinals (interleaved streams)", i)
			}
		}
		ordinals = append(ordinals, ord)
		time.Sleep(interval)
	}
	return ordinals
}

// newPaceChild wires a child + pipeline + audio pair and returns the
// fake daemon, the child, the daemon-side text conn, the pipeline, and
// the .out socket.
func newPaceChild(t *testing.T, ccid string) (*fakeDaemon, *Child, net.Conn, *pipeline, net.Conn) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if err := <-runDone; err != nil {
			t.Errorf("Run returned error on close: %v", err)
		}
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing", ccid)
	}
	return d, child, childConn, p, out
}

func sayLine(t *testing.T, d *fakeDaemon, childConn net.Conn, ccid, text string) {
	t.Helper()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: text, Last: lastPtr(true)}) //nolint:modernize // newexpr would emit new(bool)=false; true is intended
}

func clearLine(t *testing.T, d *fakeDaemon, childConn net.Conn, ccid string) {
	t.Helper()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventClear})
}

// TestPrefetchOverlapsPacing pins the contract: Speak(N+1) STARTS before
// N's audio has finished leaving the provider reader, and the
// provider-side gap between stream N's reader close and stream N+1's
// first chunk collapses far below the synthesis delay — a serialized
// chain pays the FULL delay between every pair of sentences (the dead
// speaker gap this fix removes). Byte order stays strict FIFO per
// ordinal on the socket.
func TestPrefetchOverlapsPacing(t *testing.T) {
	const (
		ccid     = "pf1"
		delay    = 300 * time.Millisecond
		chunks   = 120 // 76.8 KB per stream: far over the unix sndbuf -> pacing binds the provider reads
		interval = 8 * time.Millisecond
	)
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(delay, chunks)
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "first sentence")
	sayLine(t, d, childConn, ccid, "second sentence")

	ordinals := paceDrain(t, out, 2*chunks, interval)

	// Byte order: exactly one contiguous run per ordinal, ascending.
	for i, ord := range ordinals {
		if want := byte(i/chunks + 1); ord != want {
			t.Fatalf("FIFO VIOLATION: chunk %d carries ordinal %d, want %d", i, ord, want)
		}
	}

	snap := tts.snapshot()
	if snap.speaks != 2 {
		t.Fatalf("Speak calls = %d, want 2", snap.speaks)
	}
	for i, c := range snap.cancelled {
		if c {
			t.Fatalf("Speak %d cancelled without a clear", i)
		}
	}
	if snap.maxFlight > 1 {
		t.Fatalf("depth bound violated mid-overlap: max %d Speak in flight", snap.maxFlight)
	}

	// THE OVERLAP: Speak(N+1) started while N's chunks were still being
	// paced out. Under the old serial chain starts[1] could not precede
	// stream N's last provider read.
	if snap.lastRead[0].IsZero() || !snap.starts[1].Before(snap.lastRead[0]) {
		t.Fatalf("NO OVERLAP: Speak#2 started at %v, stream#1's last chunk left the provider at %v",
			snap.starts[1].Format(time.StampMilli), snap.lastRead[0].Format(time.StampMilli))
	}
	// The audible-gap metric: time from stream N's reader close to
	// stream N+1's first chunk. Serialized = the full synthesis delay;
	// overlapped = near zero (generation hid inside N's pacing).
	gap := snap.firstRead[1].Sub(snap.closed[0])
	if gap >= delay/2 {
		t.Fatalf("pipeline still serialized: inter-stream provider gap %s vs synth delay %s (overlap must hide generation inside N's pacing)",
			gap, delay)
	}
	t.Logf("overlap proven: Speak#2 began %v before stream#1 paced its last byte; inter-stream gap %v vs synth delay %v",
		snap.lastRead[0].Sub(snap.starts[1]), gap, delay)
}

// TestPrefetchDepthBound: five rapid says. The bounded synth queue means
// at most TWO Speak calls are ever in flight (the prefetchDepth bound),
// while PCM still leaves the socket in strict dequeue order.
func TestPrefetchDepthBound(t *testing.T) {
	const ccid = "pf2"
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(20*time.Millisecond, 5)
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "sentence 0")
	sayLine(t, d, childConn, ccid, "sentence 1")
	sayLine(t, d, childConn, ccid, "sentence 2")
	sayLine(t, d, childConn, ccid, "sentence 3")
	sayLine(t, d, childConn, ccid, "sentence 4")

	buf := make([]byte, 5*5*640)
	_ = out.SetReadDeadline(time.Now().Add(testPollDeadline))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("burst never fully drained: %v", err)
	}
	const run = 5 * 640
	for i, b := range buf {
		if b != byte(i/run+1) {
			t.Fatalf("chunk %d = %d, want ordinal %d (depth bound must keep dequeue order)", i, b, i/run+1)
		}
	}
	snap := tts.snapshot()
	if snap.speaks != 5 {
		t.Fatalf("Speak calls = %d, want 5", snap.speaks)
	}
	if snap.maxFlight > 2 {
		t.Fatalf("prefetch depth violated: %d Speak in flight at once (prefetchDepth=2 bound broken)", snap.maxFlight)
	}
	if snap.maxFlight < 2 {
		t.Fatalf("depth-2 prefetch never engaged: max %d in flight — the second slot stays idle (regression to depth 1)", snap.maxFlight)
	}
	// The synth queue never exceeded its bound at any observable point.
	if p.synthQueueDepth() > prefetchDepth {
		t.Fatalf("synth queue depth %d > prefetchDepth %d", p.synthQueueDepth(), prefetchDepth)
	}
}

// TestPrefetchSecondSlotLaunchesDuringPlay pins the depth-2 overlap the
// bound exists for: while stream N paces out, BOTH N+1 and N+2 may be
// synthesizing at once, so a long generation for N+1 (0.7-5 s, no
// stream) no longer delays N+2's start until N+1's write phase — with
// depth 1 it did, and the third sentence still paid a synth-delay gap.
// Test shape: the test-side pacer applies backpressure so stream 1 parks
// inside its write loop; then a slow synth for 2 and a slow synth for 3
// must be IN FLIGHT simultaneously (speak#3 starts while speak#2 is
// still generating), and the queue holds exactly two jobs.
func TestPrefetchSecondSlotLaunchesDuringPlay(t *testing.T) {
	const (
		ccid     = "pf2b"
		chunks   = 30
		interval = 8 * time.Millisecond
		delay    = 500 * time.Millisecond // long enough that #3 starts while #2 generates
	)
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(delay, chunks)
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "sentence 0")
	// Head-of-burst: worker registers Speak#1 and blocks awaiting it.
	waitFor(t, func() bool { return p.synthQueueDepth() == 1 })

	// Drafts 2 and 3 arrive while stream 1 paces out (backpressure via
	// the bounded test pacer). Both must prefetch concurrently.
	sayLine(t, d, childConn, ccid, "sentence 1")
	sayLine(t, d, childConn, ccid, "sentence 2")

	// Drain stream 1's chunks with the pacer so writingNow stays true.
	paceDrain(t, out, chunks, interval)

	waitFor(t, func() bool { return p.synthQueueDepth() == 2 || tts.snapshot().speaks >= 3 })
	// THE depth-2 evidence: Speak#3 started while Speak#2 was still
	// generating (both in flight) — impossible under depth 1.
	snap := tts.snapshot()
	if snap.speaks < 3 {
		t.Fatalf("Speak calls = %d, want >=3 (second lookahead must launch during stream 1's pacing)", snap.speaks)
	}
	if snap.maxFlight < 2 {
		t.Fatalf("depth-2 prefetch never engaged: max %d Speak in flight at once", snap.maxFlight)
	}
	if snap.maxFlight > 2 {
		t.Fatalf("prefetchDepth bound broken: %d Speak in flight", snap.maxFlight)
	}
	// Strict dequeue order on the socket: ordinal runs ascend even
	// though Speak#2 and #3 overlapped.
	ordinals := paceDrain(t, out, 2*chunks, interval)
	for i, ord := range ordinals {
		if want := byte(i/chunks + 2); ord != want {
			t.Fatalf("FIFO VIOLATION: chunk %d carries ordinal %d, want %d", i, ord, want)
		}
	}
}

// TestClearDuringPrefetchDiscardsLookahead: while stream 1 paces out
// (test-side backpressure), streams 2 AND 3's syntheses are prefetched
// into the bounded queue (depth TWO). A clear must retire ALL of it:
// no byte of ordinal >= 2 ever reaches the socket (both prefetched
// readers are cancelled/closed, the queued drafts never dequeued), the
// pending set drains, and the next say still plays (F-1 re-arm through
// the prefetch machinery — the pipeline is not muted by a cancelled
// lookahead).
func TestClearDuringPrefetchDiscardsLookahead(t *testing.T) {
	const (
		ccid     = "pf3"
		chunks   = 30
		interval = 8 * time.Millisecond
	)
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(40*time.Millisecond, chunks)
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "current")
	// Pacer drains 5 chunks; the remaining writes fill the sndbuf and
	// park the worker inside stream 1's write loop (writingNow).
	paceDrain(t, out, 5, interval)
	sayLine(t, d, childConn, ccid, "prefetched")
	sayLine(t, d, childConn, ccid, "queued")
	waitFor(t, func() bool { return p.synthActive() })
	waitFor(t, func() bool { return p.pendingSayCount() == 2 })

	clearLine(t, d, childConn, ccid)
	waitFor(t, p.isDropped)

	// The worker is parked inside stream 1's Write (the test-side pacer
	// left the sndbuf full). Drain like the real daemon always does:
	// the write loop unwinds on its gen check, retires the prefetched
	// slot, drops the queued draft, and exits. Then assert the pending
	// set is empty and NOTHING from the retired generations reached the
	// socket (no byte with ordinal >= 2 anywhere).
	leaked := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = out.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		buf := make([]byte, 8192)
		n, _ := out.Read(buf)
		if n == 0 {
			break
		}
		for i := 0; i < n; i++ {
			if buf[i] >= 2 {
				leaked = true
			}
		}
	}
	if leaked {
		t.Fatalf("clear leaked queued/prefetched PCM to the socket (byte with ordinal >= 2)")
	}
	waitFor(t, func() bool { return p.pendingSayCount() == 0 })
	assertNoBytesWithin(t, out, 150*time.Millisecond, "stale bytes kept arriving after the retired generation drained")

	// Re-arm: a fresh say after the clear plays.
	gate2 := newGatedTTS()
	swapTTS(p, gate2)
	sayLine(t, d, childConn, ccid, "after clear")
	select {
	case <-gate2.enteredPlay:
	case <-time.After(testPollDeadline):
		t.Fatalf("say after clear never started playback (mute regression)")
	}
	buf := make([]byte, 640)
	_ = out.SetReadDeadline(time.Now().Add(testPollDeadline))
	if n, err := io.ReadFull(out, buf); n != 640 || err != nil {
		t.Fatalf("say after clear inaudible: n=%d err=%v", n, err)
	}
	close(gate2.releaseSecond)
}

// TestClearDuringSynthAbortsSpeak: a clear while the HEAD frame's Speak
// is generating (before any audio exists) must cancel that Speak's
// context — the interrupted turn stops burning provider latency, the
// synth read loop aborts — keep the socket silent, and let the next
// generation play.
func TestClearDuringSynthAbortsSpeak(t *testing.T) {
	const ccid = "pf4"
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(30*time.Second, 4) // Speak parks until its ctx dies
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "still generating")
	waitFor(t, func() bool { return tts.snapshot().speaks == 1 })
	if !p.synthActive() {
		t.Fatalf("head-of-burst synth slot not registered")
	}

	clearLine(t, d, childConn, ccid)

	waitFor(t, func() bool { return tts.snapshot().cancelled[0] })
	waitFor(t, func() bool { return !p.synthActive() })
	assertNoBytesWithin(t, out, 200*time.Millisecond, "clear during synth let audio through")

	// The pipeline re-arms for the next generation.
	quick := newPacedTTS(5*time.Millisecond, 2)
	swapTTS(p, quick)
	sayLine(t, d, childConn, ccid, "next turn")
	buf := make([]byte, 1280)
	_ = out.SetReadDeadline(time.Now().Add(testPollDeadline))
	if n, err := io.ReadFull(out, buf); n != 1280 || err != nil {
		t.Fatalf("next say after clear-during-synth inaudible: n=%d err=%v", n, err)
	}
}

// TestPlaybackCadenceReportsSynthWriteMs: the extended telemetry — each
// stream's cadence line carries synth_ms (generation latency) and
// write_ms (pacing wall time) so a live daemon run proves the overlap
// from the log alone.
func TestPlaybackCadenceReportsSynthWriteMs(t *testing.T) {
	const ccid = "pf5"
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	logger := newCaptureLogger()
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(dir, "t.sock"),
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok\n"),
		Logger:         logger.logger(),
	})
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll(ccid)
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline(ccid)
	tts := newPacedTTS(60*time.Millisecond, 3)
	swapTTS(p, tts)

	sayLine(t, d, childConn, ccid, "telemetry check")
	buf := make([]byte, 3*640)
	_ = out.SetReadDeadline(time.Now().Add(testPollDeadline))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("telemetry say never fully drained: %v", err)
	}
	waitFor(t, func() bool { return logger.contains("voicechild: playback cadence") })
	line := logger.everything()
	for _, want := range []string{"synth_ms=", "write_ms="} {
		if !strings.Contains(line, want) {
			t.Fatalf("cadence report missing %s field:\n%s", want, line)
		}
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	<-runDone
}
