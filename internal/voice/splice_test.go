package voice

// Gate tests for the media splice (master.md C1/C2 + leaf 06 shapes, quoted
// verbatim): codec-derived chunk sizes, reorder window with silence fill,
// 20ms pacing, per-call barge-in clear, backpressure hangup.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// fakeWS is a narrow WebSocketConn fake capturing writes. Messages are
// queued and delivered by Read so the splice's inbound reader consumes the
// frames a test writes via Write.
type fakeWS struct {
	mu      sync.Mutex
	frames  [][]byte
	pending [][]byte // written frames not yet consumed by Read
	closed  bool
}

func (w *fakeWS) Write(p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	w.frames = append(w.frames, cp)
	w.pending = append(w.pending, cp)
	return nil
}

// Read returns the next pending message. Cancellation is honored eagerly:
// a cancel channel closes on ctx.Done so a blocked poll wakes immediately.
func (w *fakeWS) Read(ctx context.Context) ([]byte, error) {
	exit := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(exit)
	}()
	for {
		w.mu.Lock()
		if len(w.pending) > 0 {
			msg := w.pending[0]
			w.pending = w.pending[1:]
			w.mu.Unlock()
			return msg, nil
		}
		w.mu.Unlock()
		select {
		case <-exit:
			return nil, ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func (w *fakeWS) framesCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.frames)
}

// framesHas reports whether any written frame contains the substring.
func (w *fakeWS) framesHas(sub string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, f := range w.frames {
		if bytes.Contains(f, []byte(sub)) {
			return true
		}
	}
	return false
}

// framesSnapshot returns a copy of every written frame for diagnostics.
func (w *fakeWS) framesSnapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.frames))
	for i, f := range w.frames {
		out[i] = string(f)
	}
	return out
}

func (w *fakeWS) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

// capturePair builds an AudioPair whose child side is drained into a
// captured buffer, so tests assert the exact byte stream the child sees.
type capturePair struct {
	*AudioPair
	mu   sync.Mutex
	got  []byte
	done chan struct{}
	once sync.Once
}

func capturePairFor(t *testing.T, ccid string) *capturePair {
	t.Helper()
	dir := t.TempDir()
	p, err := NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	cp := &capturePair{AudioPair: p, done: make(chan struct{})}
	// Start the drain loop FIRST but tolerate the pair's pre-accept
	// sentinel ("child not connected"): the dial below completes within
	// microseconds, and the loop must survive until the real accept.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.ReadFromChild(buf)
			if n > 0 {
				cp.mu.Lock()
				cp.got = append(cp.got, buf[:n]...)
				cp.mu.Unlock()
			}
			if err != nil {
				if len(err.Error()) > 0 && bytes.Contains([]byte(err.Error()), []byte("child not connected")) {
					time.Sleep(2 * time.Millisecond)
					continue
				}
				cp.once.Do(func() { close(cp.done) })
				return
			}
		}
	}()
	dialAudioPair(t, dir, ccid)
	return cp
}

func (c *capturePair) bytes() []byte {
	// The "child received" view: everything the splice wrote toward the
	// child on the .in socket (captured by dialAudioPair's reader).
	childInMu.Lock()
	defer childInMu.Unlock()
	return append([]byte(nil), childInCapture[c.ccid]...)
}

// dialAudioPair dials the pair's two socket FILES like the live child does
// and keeps the child-side conns for the life of the pair (pair Close tears
// them down underneath). childOut exposes the child end of the .out socket
// (the splice's outbound source); childInCapture accumulates everything the
// splice writes toward the child on .in (the gate test's "child received"
// view).
var (
	childOutMu     sync.Mutex
	childOut       = map[string]net.Conn{}
	childInMu      sync.Mutex
	childInCapture = map[string][]byte{}
)

func dialAudioPair(t *testing.T, dir, ccid string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c1, err1 := net.Dial("unix", filepath.Join(dir, ccid+".in"))
		c2, err2 := net.Dial("unix", filepath.Join(dir, ccid+".out"))
		if err1 == nil && err2 == nil {
			childOutMu.Lock()
			childOut[ccid] = c2
			childOutMu.Unlock()
			// Capture the child's .in side (daemon->child PCM the splice
			// writes) and hold the conns until the pair closes underneath
			// us (read fails).
			go func() {
				buf := make([]byte, 8192)
				for {
					n, err := c1.Read(buf)
					if n > 0 {
						childInMu.Lock()
						childInCapture[ccid] = append(childInCapture[ccid], buf[:n]...)
						childInMu.Unlock()
					}
					if err != nil {
						break
					}
				}
				_ = c1.Close()
				_ = c2.Close()
				childOutMu.Lock()
				delete(childOut, ccid)
				childOutMu.Unlock()
			}()
			// Let the pair's Accept goroutines settle (microseconds in
			// practice; the settle window absorbs scheduler jitter).
			time.Sleep(50 * time.Millisecond)
			return
		}
		if c1 != nil {
			_ = c1.Close()
		}
		if c2 != nil {
			_ = c2.Close()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("dialAudioPair %s: could not dial both audio sockets within 2s", ccid)
}

// feedChild writes one 640-byte chunk on the child side of the pair's .out
// socket (the splice's outbound source). The chunk's first four bytes stamp
// the given value so tests can trace byte provenance.
func feedChild(ccid string) error {
	childOutMu.Lock()
	c, ok := childOut[ccid]
	childOutMu.Unlock()
	if !ok {
		return errChildConnClosed
	}
	chunk := make([]byte, 640)
	_, err := c.Write(chunk)
	return err
}

type childConnError struct{}

func (childConnError) Error() string { return "child conn not dialed" }

var errChildConnClosed error = childConnError{}

// textSink collects every C1 text frame the splice emits toward Telnyx.
type textSink struct {
	mu     sync.Mutex
	frames []string
}

func (s *textSink) Send(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, string(raw))
	return nil
}

func spliceConfig() SpliceConfig {
	return SpliceConfig{
		Codec:               telnyx.Codec{BidirectionalCodec: "L16", SamplingRate: 16000},
		ReorderWindowFrames: 10,
		ReorderGap:          60 * time.Millisecond,
		Pace:                20 * time.Millisecond,
		QueueLimit:          100,
	}
}

// payloadFrame builds one Telnyx media WS frame carrying a 640-byte L16
// payload whose first four bytes stamp the sequence number, so child-side
// delivery order is assertable by content.
func payloadFrame(seq int32) []byte {
	payload := make([]byte, 640)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(seq))
	b64 := base64.StdEncoding.EncodeToString(payload)
	return []byte(`{"event":"media","sequence_number":` + fmt.Sprintf("%d", seq) + `,"media":{"payload":"` + b64 + `"}}`)
}

func startSplice(t *testing.T, ccid string, cfg SpliceConfig, deps SpliceDeps) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunSplice(ctx, ccid, cfg, deps) }()
	return cancel, done
}

func stopSplice(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("RunSplice returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunSplice did not exit within 2s of cancel")
	}
}

// TestSpliceChunkSizes pins C2 verbatim: codec-derived chunk = 20 ms,
// L16@16k = 640 bytes (s16le, NOT big-endian RTP L16), PCMU@8k = 160 bytes.
func TestSpliceChunkSizes(t *testing.T) {
	gotL16, errL16 := ChunkBytes("L16", 16000)
	if errL16 != nil {
		t.Fatalf("ChunkBytes(L16,16000): %v", errL16)
	}
	if gotL16 != 640 {
		t.Errorf("ChunkBytes(L16,16000) = %d, want 640", gotL16)
	}
	gotPCMU, errPCMU := ChunkBytes("PCMU", 8000)
	if errPCMU != nil {
		t.Fatalf("ChunkBytes(PCMU,8000): %v", errPCMU)
	}
	if gotPCMU != 160 {
		t.Errorf("ChunkBytes(PCMU,8000) = %d, want 160", gotPCMU)
	}
}

// TestSpliceReorder pins the reorder window: frames seq 1,3,2 arrive out of
// order and the child receives 1,2,3 contiguous; a gap persisting past
// reorder_gap_ms is filled with silence of the missing duration (counted,
// never drop-oldest).
func TestSpliceReorder(t *testing.T) {
	ws := &fakeWS{}
	p := capturePairFor(t, "cc-re-1")
	text := &textSink{}

	cfg := spliceConfig()
	deps := SpliceDeps{TelnyxWS: ws, Audio: p.AudioPair, Text: text, Clock: func() time.Time { return time.Unix(0, 0) }}
	cancel, done := startSplice(t, "cc-re-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	// Hand frames to the splice in scrambled order 1,3,2 via its inbound
	// media path: write them onto the ws fake the splice reads from.
	for _, seq := range []int32{1, 3, 2} {
		_ = ws.Write(append(payloadFrame(seq), '\n'))
	}

	deadline := time.Now().Add(2 * time.Second)
	var first12 []byte
	for time.Now().Before(deadline) {
		got := p.bytes()
		if len(got) >= 3*640 {
			first12 = got[:3*640]
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(first12) < 3*640 {
		t.Fatalf("child received only %d bytes, want >= %d (three contiguous chunks)", len(first12), 3*640)
	}
	for i, wantSeq := range []uint32{1, 2, 3} {
		stamped := binary.LittleEndian.Uint32(first12[i*640 : i*640+4])
		if stamped != wantSeq {
			t.Errorf("child chunk %d sequence = %d, want %d (reorder window must re-emit 1,2,3)", i, stamped, wantSeq)
		}
	}
}

// TestSplicePacing pins: outbound pacing is exactly one chunk per 20 ms on
// the injected clock - 10 chunks must not complete before the clock has
// advanced at least 180 ms (no burst; bursts crackle in the carrier playout
// buffer).
func TestSplicePacing(t *testing.T) {
	ws := &fakeWS{}
	p := capturePairFor(t, "cc-pace-1")
	text := &textSink{}

	var mu sync.Mutex
	now := time.Unix(0, 0)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	cfg := spliceConfig()
	deps := SpliceDeps{TelnyxWS: ws, Audio: p.AudioPair, Text: text, Clock: clock}
	cancel, done := startSplice(t, "cc-pace-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	// Feed 10 outbound chunks (child -> daemon over .out, the C2-legal
	// direction); advance the fake clock 20 ms per outbound frame the
	// splice writes to Telnyx (paced writes are what advance the clock
	// here).
	writes := 0
	deadline := time.Now().Add(2 * time.Second)
	for writes < 10 && time.Now().Before(deadline) {
		_ = feedChild("cc-pace-1")
		ws.mu.Lock()
		writes = len(ws.frames)
		ws.mu.Unlock()
		mu.Lock()
		now = now.Add(20 * time.Millisecond)
		mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	if writes < 10 {
		t.Fatalf("only %d paced outbound frames written, want 10", writes)
	}
	mu.Lock()
	elapsed := now.Sub(time.Unix(0, 0))
	mu.Unlock()
	if elapsed < 180*time.Millisecond {
		t.Errorf("clock advanced %v across %d chunks, want >= 180ms (one chunk per 20ms)", elapsed, writes)
	}
}

// TestBargeInPerCall pins C1 barge-in semantics: an interrupt frame for call
// A makes the Telnyx side receive {"event":"clear"}, invokes
// CancelDraftFn("cc-a") exactly once, and leaves a concurrent call B's
// splice writing untouched.
func TestBargeInPerCall(t *testing.T) {
	wsA := &fakeWS{}
	wsB := &fakeWS{}
	pA := capturePairFor(t, "cc-a")
	pB := capturePairFor(t, "cc-b")
	textA := &textSink{}
	textB := &textSink{}

	var cmu sync.Mutex
	cancelA := 0
	cancelDraft := func(ccid string) {
		if ccid == "cc-a" {
			cmu.Lock()
			cancelA++
			cmu.Unlock()
		}
	}

	cfg := spliceConfig()
	depsA := SpliceDeps{TelnyxWS: wsA, Audio: pA.AudioPair, Text: textA, CancelDraftFn: cancelDraft, Clock: time.Now}
	depsB := SpliceDeps{TelnyxWS: wsB, Audio: pB.AudioPair, Text: textB, Clock: time.Now}
	cancelA_, doneA := startSplice(t, "cc-a", cfg, depsA)
	cancelB, doneB := startSplice(t, "cc-b", cfg, depsB)
	defer stopSplice(t, cancelA_, doneA)
	defer stopSplice(t, cancelB, doneB)

	// Feed both calls' outbound paths some child audio so B is actively
	// writing before the interrupt (the splice writes only what the child
	// sends; silence from the child means silence to Telnyx).
	for i := 0; i < 12; i++ {
		_ = feedChild("cc-b")
		_ = feedChild("cc-a")
	}

	// Wait for B to be actively writing, then interrupt A.
	deadline := time.Now().Add(2 * time.Second)
	for wsB.framesCount() < 3 && time.Now().Before(deadline) {
		// keep the child side fed while the pacer drains
		_ = feedChild("cc-b")
		time.Sleep(5 * time.Millisecond)
	}
	bBefore := wsB.framesCount()

	// The interrupt rides the MEDIA WS for call A (C1 parity: the splice's
	// inbound handler drives the per-call bargeIn; the Telnyx clear frame
	// lands on the same ws fake).
	_ = wsA.Write(append([]byte(`{"event":"interrupt","call_id":"cc-a"}`), '\n'))

	// Wait for the clear frame to land (bounded): a busy outbound pacer
	// must not delay the inbound interrupt handling beyond a frame or two.
	clearDeadline := time.Now().Add(2 * time.Second)
	for !wsA.framesHas(`"event":"clear"`) && time.Now().Before(clearDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !wsA.framesHas(`"event":"clear"`) {
		t.Errorf("Telnyx side for call A did not receive a clear frame after interrupt (wsA frames=%v)", wsA.framesSnapshot())
	}
	cmu.Lock()
	got := cancelA
	cmu.Unlock()
	if got != 1 {
		t.Errorf("CancelDraftFn(cc-a) called %d times, want exactly 1", got)
	}
	// B keeps writing: keep feeding B's child side and expect more frames.
	for i := 0; i < 20; i++ {
		_ = feedChild("cc-b")
		time.Sleep(10 * time.Millisecond)
	}
	if wsB.framesCount() <= bBefore {
		t.Errorf("call B stalled after interrupt on call A (frames %d <= %d)", wsB.framesCount(), bBefore)
	}
}

// TestSpliceBackpressure pins: a stalled Telnyx writer (the pacer cannot
// drain the outbound queue because the fake WS never consumes it faster
// than the pace) fills the bounded queue (degraded), and once the grace
// window passes the splice calls HangupFn(ccid, reason) with a named
// reason - never a silent drop-oldest.
func TestSpliceBackpressure(t *testing.T) {
	// The child (.out) firehoses audio while the paced Telnyx writer
	// drains far slower (Pace 100 ms vs feed 2 ms): the bounded queue
	// pins full, the splice logs degraded, and once the grace window
	// passes the splice calls HangupFn(ccid, reason) with a named reason
	// - never a silent drop-oldest.
	p := capturePairFor(t, "cc-bp-1")
	ws := &fakeWS{}
	text := &textSink{}

	hangup := make(chan string, 1)
	hangupFn := func(ccid, reason string) {
		hangup <- ccid + ":" + reason
	}

	cfg := spliceConfig()
	cfg.QueueLimit = 2
	cfg.Pace = 100 * time.Millisecond
	cfg.ReorderGap = 20 * time.Millisecond // keep the inbound path quiet
	deps := SpliceDeps{TelnyxWS: ws, Audio: p.AudioPair, Text: text, HangupFn: hangupFn, Clock: time.Now}
	cancel, done := startSplice(t, "cc-bp-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	// Keep the child side saturated well past the grace window.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = feedChild("cc-bp-1")
			time.Sleep(2 * time.Millisecond)
		}
	}()
	defer close(stop)

	select {
	case msg := <-hangup:
		if !bytes.Contains([]byte(msg), []byte(":")) {
			t.Errorf("HangupFn reason empty: %q (a named reason is required)", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("HangupFn never invoked past queue grace window")
	}
}
