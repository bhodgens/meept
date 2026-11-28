package voice

// bughunt_1125_voice_pins_test.go — pin tests for the 2026-11-25
// bughunt-gaps voice wave (audits 04-voice.md / 07-voice-eval-gatecapture.md):
//
//   A7-1: Dialer.fin terminal-latch map size-stability (bounded by the
//         retention window, not by the daemon's dial history).
//   D4/A7-2: provider outages after a consumed cap unit are LOUD and
//         queryable (Warn + counters + the outage-suspected line); the cap
//         text names the failure contribution. Spend stays attempt-based
//         (owner ruling — no refund).
//   A7-3: the barge-gate touch stamps ride the gate's anchored monotonic
//         clock (same domain as the residue deadline), proven with a fake
//         wall step.
//   A4-3/A7-4: TextServer.Send/SendRaw serialize the WHOLE write (deadline
//         set + conn.Write + deadline clear) on writeMu — concurrent
//         senders never interleave partial JSON lines and never clear a
//         sibling's in-flight deadline.
//   A7-5: SplitSentences is NEVER-drop (the ConnectSentences fix ported):
//         >16 sentences and oversize sentences arrive whole in bounded
//         chunks.
//
// Run with -race: the A4-3 write lock also closed a lock-scope bug.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// ---------------------------------------------------------------------------
// A7-1: fin map size stability
// ---------------------------------------------------------------------------

// TestDialerFinLatchBounded pins the terminal-latch leak fix: after many
// dial/finish cycles the fin map stays bounded (entries age out through
// the retention sweep in finishCall), never one entry per dialed call for
// the daemon's whole life (the A7-1 shape).
func TestDialerFinLatchBounded(t *testing.T) {
	orig := finLatchRetention
	finLatchRetention = 10 * time.Millisecond
	t.Cleanup(func() { finLatchRetention = orig })

	tel := &countingController{}
	cfg := dialConfig()
	cfg.TimeoutSecs = 3600 // timer never fires: finishCall drives the latch
	reg := NewRegistry(0)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), nil, testLogger())
	target := cfg.AllowedNumbers[0]

	const calls = 40
	for i := range calls {
		res, err := d.Dial(context.Background(), DialRequest{
			EmployeeID: "emp-a7-1", Target: target,
			RequestID: "req-fin-" + itoaTest(i),
		})
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		d.finishCall(res.CallControlID)
		// Age this entry past the (test-scale) retention window; the NEXT
		// finishCall's sweep must prune it.
		d.mu.Lock()
		if ts, ok := d.finSeenNano[res.CallControlID]; ok {
			d.finSeenNano[res.CallControlID] = ts - int64(time.Second)
		}
		d.mu.Unlock()
	}

	d.mu.Lock()
	finLen := len(d.fin)
	ansLen := len(d.answered)
	d.mu.Unlock()
	// Bounded: 40 dials with every entry aged out leave at most the last
	// (un-swept) latch, never 40. The old shape failed this at finLen=40
	// (one permanent entry per dialed call — the A7-1 leak).
	if finLen > 2 || ansLen > 2 {
		t.Fatalf("per-call maps unbounded: fin=%d answered=%d after %d calls (want <= 2 each)", finLen, ansLen, calls)
	}
}

// TestDialerFinLatchExactlyOnceWithinWindow pins the guard the leak fix
// must NOT break: within the retention window a repeated finishCall is a
// no-op (Done closes once; a double close would panic).
func TestDialerFinLatchExactlyOnceWithinWindow(t *testing.T) {
	orig := finLatchRetention
	finLatchRetention = time.Hour
	t.Cleanup(func() { finLatchRetention = orig })

	tel := &countingController{}
	cfg := dialConfig()
	cfg.TimeoutSecs = 3600
	reg := NewRegistry(0)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), nil, testLogger())

	res, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-fin", Target: cfg.AllowedNumbers[0], RequestID: "req-fin-x",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	d.finishCall(res.CallControlID)
	d.finishCall(res.CallControlID) // inside the window: short-circuit
	d.finishCall(res.CallControlID)
	<-res.Done // closed exactly once (double close panics loudly)

	d.mu.Lock()
	latched := d.fin[res.CallControlID]
	d.mu.Unlock()
	if !latched {
		t.Fatal("fin latch missing inside the retention window — exactly-once guard broken")
	}
}

// ---------------------------------------------------------------------------
// bughunt 2026-11-27 L14/T-3: the fin prune re-opened the F-2 answered leak
// for >1h-late call.answered webhooks. Pins: (1) a re-mark landed AFTER its
// fin latch was pruned (the straggler shape) is reaped by a later
// finishCall's sweep once IT ages past the window — the leak is closed and
// the map stays bounded; (2) a marker whose fin latch is still inside the
// window is never pruned early (the NotifyAnswered-vs-terminal race it
// guards is still open).
// ---------------------------------------------------------------------------

// TestDialerStragglerAnsweredMarkerPruned pins the leak fix: finishCall
// (prunes the latch via a following sweep) -> a late NotifyAnswered
// re-marks the ccid (passes the now-absent fin guard, exactly the
// >1h-late webhook shape) -> the re-mark ages past the window -> a later
// finishCall sweep reaps the straggler. The old shape left answered[ccid]
// set for the daemon's whole life.
func TestDialerStragglerAnsweredMarkerPruned(t *testing.T) {
	orig := finLatchRetention
	finLatchRetention = 10 * time.Millisecond
	t.Cleanup(func() { finLatchRetention = orig })

	tel := &countingController{}
	cfg := dialConfig()
	cfg.TimeoutSecs = 3600
	reg := NewRegistry(0)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), nil, testLogger())
	target := cfg.AllowedNumbers[0]

	dial := func(reqID string) string {
		res, err := d.Dial(context.Background(), DialRequest{
			EmployeeID: "emp-l14", Target: target, RequestID: reqID,
		})
		if err != nil {
			t.Fatalf("Dial %s: %v", reqID, err)
		}
		return res.CallControlID
	}

	ccid := dial("req-l14-1")
	d.finishCall(ccid) // terminal cleanup; latch stamped

	// Age the latch past the window and drive a sweep (unrelated call's
	// finishCall): the latch is now PRUNED, so a late webhook passes the
	// fin guard — the true straggler shape.
	d.mu.Lock()
	if ts, ok := d.finSeenNano[ccid]; ok {
		d.finSeenNano[ccid] = ts - int64(time.Second)
	}
	d.mu.Unlock()
	sweeper := dial("req-l14-sweep0")
	d.finishCall(sweeper)

	d.NotifyAnswered(ccid) // passes the pruned guard: re-marks

	d.mu.Lock()
	marked := d.answered[ccid]
	d.mu.Unlock()
	if !marked {
		t.Fatal("precondition failed: late NotifyAnswered did not re-mark (test no longer exercises the straggler shape)")
	}

	// FRESH sub-pin: an unrelated sweep immediately after the re-mark
	// must NOT reap it — a marker's bounded life runs from ITS OWN
	// set-time, not from some other call's terminal.
	sweeper = dial("req-l14-sweep1")
	d.finishCall(sweeper)
	d.mu.Lock()
	marked = d.answered[ccid]
	d.mu.Unlock()
	if !marked {
		t.Fatal("fresh straggler re-mark reaped by an unrelated sweep before its own window elapsed")
	}

	// Age the STRAGGLER MARKER (not a fin latch) past the window, then
	// drive one more sweep: the straggler must go.
	d.mu.Lock()
	if ts, ok := d.answeredSeenNano[ccid]; ok {
		d.answeredSeenNano[ccid] = ts - int64(time.Second)
	}
	d.mu.Unlock()
	sweeper = dial("req-l14-sweep2")
	d.finishCall(sweeper)

	d.mu.Lock()
	straggler := d.answered[ccid]
	_, stragglerStamp := d.answeredSeenNano[ccid]
	d.mu.Unlock()
	if straggler || stragglerStamp {
		t.Fatalf("straggler answered marker for %s survived the sweep (L14 leak: lives for the daemon's whole life)", ccid)
	}
}

// ---------------------------------------------------------------------------
// D4/A7-2: loud, queryable provider outages (attempt-based cap preserved)
// ---------------------------------------------------------------------------

// TestDialProviderOutageSurfaces pins the outage shape: every DialOut
// fails AFTER its cap unit was consumed. Required outcomes: one Warn per
// failing dial, the outage-suspected Warn at the streak threshold, query-
// able counters, a cap-refusal error text naming the failure contribution,
// AND (owner ruling) the consumed units are NOT refunded — the cap still
// exhausts after exactly cap-many attempts.
func TestDialProviderOutageSurfaces(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	tel := &countingController{failDV: true} // provider down: every POST errors
	cfg := dialConfig()
	cfg.DailyDialCap = 3
	reg := NewRegistry(4)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), nil, logger)
	target := cfg.AllowedNumbers[0]

	var refusal string
	for i := range 4 {
		_, err := d.Dial(context.Background(), DialRequest{
			EmployeeID: "emp-outage", Target: target,
			RequestID: "req-outage-" + itoaTest(i),
		})
		if err == nil {
			t.Fatalf("dial %d succeeded against a failing provider", i)
		}
		if errors.Is(err, ErrDailyCapReached) {
			refusal = err.Error()
		}
	}
	if refusal == "" {
		t.Fatal("4th dial did not hit the cap (attempt-based spend must hold: 3 consumed units, no refund)")
	}

	// (b) counters queryable through the public accessors.
	if got := d.ProviderFailureCount(); got != 3 {
		t.Errorf("ProviderFailureCount = %d, want 3 (three POST failures consumed three cap units)", got)
	}
	if got := d.ConsecutiveProviderFailures(); got < 3 {
		t.Errorf("ConsecutiveProviderFailures = %d, want >= 3", got)
	}
	if d.LastProviderFailure() == "" {
		t.Error("LastProviderFailure empty after a failing dial streak")
	}

	logs := logBuf.String()
	// (a) one Warn per failing dial.
	if n := strings.Count(logs, "provider failure after cap unit consumed"); n != 3 {
		t.Errorf("provider-failure Warns = %d, want 3 (one per dial), log:\n%s", n, logs)
	}
	// the outage-suspected line at threshold 3 consecutive failures.
	if !strings.Contains(logs, "Telnyx provider outage suspected") {
		t.Errorf("no outage-suspected Warn after 3 consecutive provider failures:\n%s", logs)
	}
	// (c) the refusal names the cause.
	if !strings.Contains(refusal, "provider failures contributed 3 today") {
		t.Errorf("cap refusal text %q must name the provider-failure contribution (D4: spend stays, silence must not)", refusal)
	}
	// The refusal itself Warns (loud).
	if !strings.Contains(logs, "outbound daily cap exhausted") {
		t.Errorf("cap refusal logged no Warn:\n%s", logs)
	}
}

// flappingController fails every other DialOut: proves the consecutive
// streak resets on a dial that reaches the registry while the TODAY cost
// count keeps accumulating.
type flappingController struct {
	mu    sync.Mutex
	calls int
}

func (c *flappingController) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls%2 == 1 {
		return "", errors.New("telnyx 503")
	}
	return "cc-flap-" + time.Now().Format("150405.000000000"), nil
}
func (c *flappingController) AnswerCall(_ context.Context, _ string) error { return nil }
func (c *flappingController) StartStreaming(_ context.Context, _, _ string, _ telnyx.Codec) error {
	return nil
}
func (c *flappingController) HangupCall(_ context.Context, _ string) error { return nil }

// TestDialOutageStreakResetsOnSuccess pins the streak half of the signal:
// a healthy dial between two failures is NOT an outage (no suspected
// Warn), while the today-count still records the outage's cost.
func TestDialOutageStreakResetsOnSuccess(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	tel := &flappingController{} // fail, succeed, fail
	cfg := dialConfig()
	// Distinct targets + employees per attempt so no registry gate
	// short-circuits before DialOut (each attempt must actually reach the
	// provider for the streak math).
	cfg.AllowedNumbers = []string{"A", "B", "C"} // normalized by dialConfig's own rule below
	cfg.DailyDialCap = 10
	cfg.TimeoutSecs = 3600
	reg := NewRegistry(4)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), nil, logger)

	targets := cfg.AllowedNumbers
	for i := range 3 {
		_, _ = d.Dial(context.Background(), DialRequest{
			EmployeeID: "emp-flap-" + itoaTest(i), Target: targets[i],
			RequestID: "req-flap-" + itoaTest(i),
		})
	}
	if got := d.ProviderFailureCount(); got != 2 {
		t.Errorf("ProviderFailureCount = %d, want 2 (two failed attempts consumed two cap units)", got)
	}
	if got := d.ConsecutiveProviderFailures(); got != 1 {
		t.Errorf("ConsecutiveProviderFailures = %d, want 1 (the success reset the streak)", got)
	}
	if strings.Contains(logBuf.String(), "outage suspected") {
		t.Errorf("outage Warn fired without a 3-failure streak:\n%s", logBuf.String())
	}
}

// ---------------------------------------------------------------------------
// A7-3: touch stamps share the gate's monotonic domain (fake wall step)
// ---------------------------------------------------------------------------

// TestGateTouchWallStepKeepsResidue pins both sides of the residue
// comparison in ONE domain: with the C-19 fake wall+mono seams, a RAW
// wall-clock step must not change what the gate sees as touch age — the
// monotonic pace decides. Backward step (the tail-replay shape): a
// 100 ms-old (monotonic) touch with the wall 10 s behind must still stamp
// the residue window. Forward step (the clipped-reply shape): a monotonic-
// fresh touch must stay inside-window no matter how far the wall jumped.
func TestGateTouchWallStepKeepsResidue(t *testing.T) {
	var mu sync.Mutex
	wall := int64(1_000_000_000_000_000) // arbitrary unix-nano base
	mono := time.Duration(0)
	w := func() int64 { mu.Lock(); defer mu.Unlock(); return wall }
	m := func() time.Duration { mu.Lock(); defer mu.Unlock(); return mono }
	stepWall := func(d time.Duration) { mu.Lock(); wall += int64(d); mu.Unlock() }
	stepMono := func(d time.Duration) { mu.Lock(); mono += d; mu.Unlock() }

	// --- backward wall step, touch still monotonic-fresh ---
	g := &bargeGate{nowNano: w, monoNow: m}
	touch := g.TouchNano() // anchors the gate here
	stepMono(100 * time.Millisecond)
	stepWall(-10 * time.Second) // NTP correction backward
	g.Bump(touch)
	if _, residue := g.Snapshot(); residue == 0 {
		t.Fatal("residue window NOT stamped despite a 100 ms (monotonic) old touch: the touch crossed into the raw wall domain (A7-3 regression — tail replay)")
	}
	// The stamped deadline is gate-clock based: a fresh-gen read inside
	// the window verdicts stale.
	_, residue := g.Snapshot()
	if !g.ReadStale(g.Current(), residue) {
		t.Error("fresh-generation read inside the residue window verdicted fresh — the deadline left the monotonic domain")
	}
	// After the window elapses MONOTONICALLY (wall stays stepped), the
	// read turns fresh again — the window length was never wall-derived.
	stepMono(300 * time.Millisecond)
	if g.ReadStale(g.Current(), residue) {
		t.Error("residue window outlived its 250 ms monotonic bound")
	}

	// --- forward wall step: a monotonic-fresh touch must NOT expire ---
	g2 := &bargeGate{nowNano: w, monoNow: m}
	touch2 := g2.TouchNano()
	stepWall(30 * time.Second) // forward jump: the old raw-wall touch would
	// now read 30 s FUTURE from clockNano's anchored base... i.e. a
	// negative delta, so a raw stamp wrongly NEVER stamps (silent-then-
	// say preserved by accident) while stamping in the gate domain keeps
	// the decision right for genuinely old touches:
	stepMono(1 * time.Millisecond)
	g2.Bump(touch2)
	if _, r := g2.Snapshot(); r == 0 {
		t.Fatal("gate sees a 1 ms-old monotonic touch as wall-fresh: residue not stamped (forward-step defect)")
	}
}

// TestSpliceTouchStampUsesGateDomain pins the phone splice producer
// wiring (the audit's exact defect site) by EXECUTING the pacer's stamp
// line on a shell splice: paceOne queues + writes one chunk, storing the
// touch. With the C-19 fake wall/mono seams armed, a raw wall step must
// not change the gate's view of the touch age: 10 ms of monotonic travel
// + a 10 s backward wall step must still stamp residue (a raw time.Now
// stamp would read ~10 s stale and skip stamping — the tail-replay
// defect), while 400 ms of monotonic travel must NOT stamp (the window
// expired, wall step irrelevant).
func TestSpliceTouchStampUsesGateDomain(t *testing.T) {
	ws := &fakeWS{}
	s := &runSplice{
		ccid:     "cc-domain",
		cfg:      SpliceConfig{Pace: time.Millisecond},
		deps:     SpliceDeps{TelnyxWS: ws},
		log:      testLogger(),
		wake:     make(chan struct{}, 1),
		overflow: make(chan []byte, 1),
	}
	var mu sync.Mutex
	wall := int64(1_000_000_000_000_000)
	var mono time.Duration
	s.gate.nowNano = func() int64 { mu.Lock(); defer mu.Unlock(); return wall }
	s.gate.monoNow = func() time.Duration { mu.Lock(); defer mu.Unlock(); return mono }

	// Production stamp site: paceOne writes the chunk and stores the
	// touch through the gate clock.
	s.outbound = [][]byte{make([]byte, 640)}
	if err := s.paceOne(context.Background()); err != nil {
		t.Fatalf("paceOne: %v", err)
	}
	if ws.framesMediaCount() == 0 {
		t.Fatal("paceOne wrote no media frame")
	}
	touch := s.lastPacedWriteNano.Load()
	if touch == 0 {
		t.Fatal("pacer stored no touch")
	}
	// The touch must live in the gate domain: it equals the gate's clock
	// at the stamp moment (fake domain value), NOT a raw wall reading.
	mu.Lock()
	gateNow := wall + int64(mono) - int64(mono) // anchor base at stamp time
	mu.Unlock()
	if diff := touch - gateNow; diff > int64(time.Second) || diff < -int64(time.Second) {
		t.Fatalf("pacer touch %d is outside the gate clock domain (base %d, raw wall leaked back in)", touch, gateNow)
	}

	// 10 ms monotonic travel + a 10 s BACKWARD wall step: the touch is
	// still recent in the monotonic domain -> residue must be stamped.
	mu.Lock()
	mono += 10 * time.Millisecond
	wall -= int64(10 * time.Second)
	mu.Unlock()
	s.gate.Bump(touch)
	if _, residue := s.gate.Snapshot(); residue == 0 {
		t.Fatal("residue NOT stamped despite a 10 ms-old (monotonic) touch and a backward wall step — A7-3 regression: raw-wall touch vs anchored-clock comparison")
	}

	// 400 ms of monotonic travel past the window (wall keeps drifting
	// forward): the touch is genuinely old -> no residue.
	mu.Lock()
	mono += 390 * time.Millisecond
	wall += int64(30 * time.Second)
	mu.Unlock()
	s.gate.Bump(touch)
	if _, residue := s.gate.Snapshot(); residue != 0 {
		t.Fatal("residue stamped for a 400 ms-old monotonic touch — the window followed the wall step instead of monotonic pace")
	}
}

// ---------------------------------------------------------------------------
// A4-3/A7-4: whole-write serialization on the child text socket
// ---------------------------------------------------------------------------

// chunkedConn models a real socket's partial-write behavior that net.Pipe
// CANNOT model (the pipe serializes each whole Write internally, which made
// the first interleave pin vacuous against old code): a small kernel-buffer
// capacity, writes copied chunk-by-chunk with NO lock across the chunks,
// parking (cond wait) when the buffer is full until a reader drains. Two
// unlocked writers therefore interleave mid-frame exactly like two goroutines
// on a saturated unix socket; a writeMu-serialized server never lets a second
// writer touch the stream while the first is parked.
type chunkedConn struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte // accepted bytes awaiting the reader
	cap    int
	closed bool
	rpos   int
}

func newChunkedConn(cap int) *chunkedConn {
	c := &chunkedConn{cap: cap}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// chunkSize is the per-step copy (the kernel segment an unlocked writer can
// be preempted between).
const chunkSize = 1024

func (c *chunkedConn) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > chunkSize {
			n = chunkSize
		}
		c.mu.Lock()
		for len(c.buf)-c.rpos >= c.cap && !c.closed {
			// Buffer full: PARK between chunks (preemption point). A
			// concurrent unlocked writer takes this window.
			c.cond.Wait() //nolint:mutexio // sync.Cond.Wait atomically releases c.mu; test-local buffer
		}
		if c.closed {
			c.mu.Unlock()
			return written, net.ErrClosed
		}
		if c.rpos > 0 {
			c.buf = append(c.buf[:0], c.buf[c.rpos:]...)
			c.rpos = 0
		}
		c.buf = append(c.buf, p[:n]...)
		c.cond.Broadcast() // data landed: wake a parked reader
		c.mu.Unlock()
		written += n
		p = p[n:]
	}
	return written, nil
}

// Read returns up to want accepted bytes (reader side).
func (c *chunkedConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	for c.rpos >= len(c.buf) && !c.closed {
		c.cond.Wait() //nolint:mutexio // sync.Cond.Wait atomically releases c.mu; test-local buffer
	}
	if c.rpos >= len(c.buf) {
		c.mu.Unlock()
		return 0, io.EOF
	}
	n := copy(p, c.buf[c.rpos:])
	c.rpos += n
	c.cond.Broadcast() // drain room for the next writer chunk
	c.mu.Unlock()
	return n, nil
}

func (c *chunkedConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}
func (c *chunkedConn) LocalAddr() net.Addr                { return nil }
func (c *chunkedConn) RemoteAddr() net.Addr               { return nil }
func (c *chunkedConn) SetDeadline(time.Time) error        { return nil }
func (c *chunkedConn) SetReadDeadline(time.Time) error    { return nil }
func (c *chunkedConn) SetWriteDeadline(t time.Time) error { return nil }

// TestTextServerConcurrentSendersKeepFrameIntegrity is the write-lock pin:
// two concurrent senders (Send frame codec + SendRaw keys-style line) each
// push distinct 64 KB JSON lines over a chunkedConn whose tiny "kernel
// buffer" forces every Write to park between chunks. The OLD shape (conn
// snapshot under mu, write unlocked) lets the two parked writers interleave
// chunks mid-line: a reassembled line carries BOTH senders' run markers or
// fails to parse as one JSON object. The fixed shape (writeMu across the
// whole write) makes each line atomic on the stream. Non-vacuity: red on
// HEAD's socket.go (verified), green with the lock. Run with -race.
func TestTextServerConcurrentSendersKeepFrameIntegrity(t *testing.T) {
	cc := newChunkedConn(4096) // tiny buffer: constant preemption windows
	srv := NewTextServer(filepath.Join(shortDir(t), "unused.sock"), testLogger())
	srv.mu.Lock()
	srv.child = cc
	srv.mu.Unlock()
	t.Cleanup(func() { _ = cc.Close() })

	const rounds = 8
	markA := strings.Repeat("A", 64*1024)
	markB := strings.Repeat("B", 64*1024)

	var wg sync.WaitGroup
	sendErr := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range rounds {
			if err := srv.Send(Frame{CallID: "cc-A", Event: EventSay, Text: markA}); err != nil {
				sendErr <- fmt.Errorf("A sender: %w", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range rounds {
			line := []byte(`{"call_id":"cc-B","event":"say","text":"` + markB + `"}` + "\n")
			if err := srv.SendRaw(line); err != nil {
				sendErr <- fmt.Errorf("B sender: %w", err)
				return
			}
		}
	}()

	// Reassembling reader: split on newline; every line must be exactly
	// one whole frame from ONE sender.
	reader := make(chan error, 1)
	go func() {
		var acc []byte
		buf := make([]byte, 2048) // slow drain: keep the writers parked
		seen := 0
		for seen < 2*rounds {
			n, err := cc.Read(buf)
			if err != nil {
				reader <- fmt.Errorf("reader at %d/%d lines: %w", seen, 2*rounds, err)
				return
			}
			acc = append(acc, buf[:n]...)
			for {
				i := bytes.IndexByte(acc, '\n')
				if i < 0 {
					break
				}
				line := string(acc[:i])
				acc = acc[i+1:]
				seen++
				if !json.Valid([]byte(line)) {
					reader <- fmt.Errorf("interleaved/torn frame: line %d (%d bytes) is not one JSON object", seen, len(line))
					return
				}
				hasA := strings.Contains(line, "AAAA")
				hasB := strings.Contains(line, "BBBB")
				if hasA && hasB {
					reader <- fmt.Errorf("interleaved frame: line %d carries BOTH senders' payloads (A4-3 regression: writers unlocked on the single child conn)", seen)
					return
				}
				if !hasA && !hasB {
					reader <- fmt.Errorf("frame lost: line %d carries neither sender's payload", seen)
					return
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
		reader <- nil
	}()

	select {
	case rerr := <-reader:
		// Fail fast WITHOUT waiting on wg: on the interleave shape the
		// parked writers may never finish once the reader stops.
		if rerr != nil {
			_ = cc.Close() // unstick parked writers, then fail
			wg.Wait()
			t.Fatalf("frame integrity violated: %v", rerr)
		}
	case <-time.After(30 * time.Second):
		_ = cc.Close()
		t.Fatal("reader stalled")
	}
	wg.Wait()
	select {
	case serr := <-sendErr:
		t.Fatalf("sender error: %v", serr)
	default:
	}
}

// wedgeConn is a net.Conn stub simulating a full kernel buffer: a Write
// of an "A"-prefixed frame parks until released OR its own deadline
// expires (it enforces SetWriteDeadline faithfully, returning a
// net.Error timeout), while "B" writes complete instantly. This makes the
// A7-4 TOCTOU deterministic: writer B SUCCEEDS while writer A is still
// blocked mid-Write. In the old unlocked shape, B's success-clear strips
// A's deadline (A can then hang past its own bound); with the fix, B
// never reaches the conn until A's write (and its deadline) is done, so
// A always dies at exactly its deadline with the stream holding at most
// one writer's bytes per line.
type wedgeConn struct {
	mu       sync.Mutex
	deadline time.Time
	release  chan struct{}
	stream   bytes.Buffer // bytes accepted by completed/failed writes, in arrival order
}

func (c *wedgeConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	return nil
}

type deadlineExceeded struct{}

func (deadlineExceeded) Error() string   { return "test: write deadline exceeded" }
func (deadlineExceeded) Timeout() bool   { return true }
func (deadlineExceeded) Temporary() bool { return false }

func (c *wedgeConn) Write(p []byte) (int, error) {
	if len(p) == 0 || p[0] != 'A' {
		// B-family write: completes instantly (its success is the
		// sibling that used to clear the shared deadline).
		c.mu.Lock()
		c.stream.Write(p)
		c.mu.Unlock()
		return len(p), nil
	}
	// A-family write: parks (the full-buffer shape). Faithful to Go's
	// netFD semantics (changeWriteDeadline wakes a pending op): the write
	// observes the CURRENTLY armed deadline continuously — a sibling's
	// success-time clear to the zero value makes the blocked write
	// unbounded again, exactly the A7-4 hazard. The write ends ONLY on
	// release or by crossing a still-armed deadline.
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		c.mu.Lock()
		dl := c.deadline
		c.mu.Unlock()
		if !dl.IsZero() && !time.Now().Before(dl) {
			return 0, deadlineExceeded{}
		}
		select {
		case <-c.release:
			c.mu.Lock()
			c.stream.Write(p)
			c.mu.Unlock()
			return len(p), nil
		case <-tick.C:
		}
	}
}

func (c *wedgeConn) Read([]byte) (int, error)        { return 0, io.EOF }
func (c *wedgeConn) Close() error                    { return nil }
func (c *wedgeConn) LocalAddr() net.Addr             { return nil }
func (c *wedgeConn) RemoteAddr() net.Addr            { return nil }
func (c *wedgeConn) SetDeadline(time.Time) error     { return nil }
func (c *wedgeConn) SetReadDeadline(time.Time) error { return nil }

// TestTextServerSiblingCannotClearInFlightDeadline pins the TOCTOU half
// (A7-4): writer A (say frame, parked mid-Write against a full buffer)
// MUST die on its own write deadline even though writer B (keys/end
// frame) completes and clears during A's flight. Old shape: B's
// success-time clear strips A's deadline — A runs past its bound (the
// teardown hang the 2 s deadline exists to prevent, resurrected under
// concurrency). Fixed shape: B waits on writeMu; A times out exactly
// within the bound; every sender returns bounded, never hanging.
func TestTextServerSiblingCannotClearInFlightDeadline(t *testing.T) {
	orig := textWriteTimeout
	textWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { textWriteTimeout = orig })

	w := &wedgeConn{release: make(chan struct{})}
	t.Cleanup(func() { close(w.release) }) // never strand a parked stub write
	srv := NewTextServer(filepath.Join(shortDir(t), "unused2.sock"), testLogger())
	srv.mu.Lock()
	srv.child = w
	srv.mu.Unlock()

	aDone := make(chan error, 1)
	go func() {
		// SendRaw keeps its own line bytes; 'A' prefix marks the A family.
		aDone <- srv.SendRaw([]byte("AAAA" + strings.Repeat("x", 4096) + "\n"))
	}()
	// Let A arm its deadline and park mid-Write.
	time.Sleep(60 * time.Millisecond)
	// B lands while A is in flight: it must NOT disturb A's deadline.
	if err := srv.SendRaw([]byte("BBBB-frame\n")); err != nil {
		t.Fatalf("B write failed: %v", err)
	}

	select {
	case aerr := <-aDone:
		// Fixed shape: A died on ITS deadline (bounded), never a silent
		// success after a sibling cleared the bound, never a hang.
		if aerr == nil {
			t.Fatal("parked writer A completed despite a sibling clearing its write deadline (A7-4 TOCTOU)")
		}
		if !strings.Contains(aerr.Error(), "write to child") || !strings.Contains(aerr.Error(), "deadline exceeded") {
			t.Fatalf("A failed with an unexpected error: %v", aerr)
		}
	case <-time.After(2 * time.Second): // 10x the write bound
		t.Fatal("writer A hung past its write deadline after a sibling's success (deadline cleared out from under it — A7-4 regression)")
	}

	// Stream integrity: A's partial never shipped (it timed out before
	// the stub accepted bytes), so only B's whole frame rode the stream.
	w.mu.Lock()
	got := w.stream.String()
	w.mu.Unlock()
	if !strings.Contains(got, "BBBB-frame\n") {
		t.Fatalf("B frame missing from the stream: %q", got)
	}
	if strings.Contains(got, "AAAA") {
		t.Errorf("partial A bytes reached the stream before a newline (torn frame): %q", got)
	}
}

// ---------------------------------------------------------------------------
// A7-5: SplitSentences never-drop (+ the talk-loop split path end-to-end)
// ---------------------------------------------------------------------------

// TestSplitSentencesNeverDrop pins the splitter: a 20-sentence draft (over
// the old 16-sentence ceiling) and an oversize sentence (over the old
// 280-rune cut) arrive WHOLE, split into bounded pieces, in order.
func TestSplitSentencesNeverDrop(t *testing.T) {
	var b strings.Builder
	for i := range 20 {
		b.WriteString("Sentence number " + itoaTest(i) + " carries some words.")
	}
	long := b.String()
	got := SplitSentences(long)
	if len(got) != 20 {
		t.Fatalf("20-sentence draft split into %d units, want 20 (the old 16-cap dropped the tail)", len(got))
	}
	if !strings.Contains(got[19], "19") {
		t.Errorf("last unit = %q, want the 20th sentence (order kept, tail intact)", got[19])
	}

	// Oversize single sentence: 3 x 280 = 840 runes of terminator-free
	// text -> bounded chunks, every rune preserved.
	oversize := strings.Repeat("z", maxSentenceRunes*3)
	units := SplitSentences(oversize)
	joined := strings.Join(units, "")
	if joined != oversize {
		t.Errorf("oversize sentence lost text: got %d runes across %d units, want %d", len([]rune(joined)), len(units), len([]rune(oversize)))
	}
	for i, u := range units {
		if r := utf8Len(u); r > maxSentenceRunes {
			t.Errorf("unit %d is %d runes, over the %d bound (each CHECK unit must stay bounded)", i, r, maxSentenceRunes)
		}
	}
}

// TestTalkLoopSupervisorOnLongReplyNotTruncated is the A7-5 end-to-end
// pin via talkloop.go:1726: with the supervisor gate ON (the default
// supervised posture, ConnectSentences false), a reply of 20 sentences
// must arrive at the say frames WHOLE — no silent 16-sentence cut, no
// 280-rune mid-sentence truncation.
func TestTalkLoopSupervisorOnLongReplyNotTruncated(t *testing.T) {
	var b strings.Builder
	for i := range 20 {
		b.WriteString("Line " + itoaTest(i) + " of the long answer. ")
	}
	long := strings.TrimSpace(b.String())

	sink := &recordSink{}
	sup := &cannedSupervisor{allowed: map[string]bool{}}
	turns := &cannedTurns{reply: long}
	loop := newTestTalkLoop(t, sink.Send, sup, turns, TalkLoopConfig{
		FillerLines:  []string{"One moment."},
		GreetingLine: "Hello.",
		// ConnectSentences false (zero value): the SUPERVISOR-ON split path
	})

	cancel, done := runLoop(t, loop, "cc-split")
	defer func() {
		cancel()
		<-done
	}()

	loop.frames <- finalPrompt("cc-split", "question")

	// Reconstruct the reply from say frames that carry reply content (the
	// greeting/filler lines are not part of the reply).
	reconstruct := func() string {
		var rb strings.Builder
		for _, tx := range sink.sayTexts() {
			if strings.Contains(tx, "Line ") {
				rb.WriteString(tx)
				rb.WriteString(" ")
			}
		}
		return rb.String()
	}
	normalize := func(s string) string {
		return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), ".", "")
	}
	want := normalize(long)
	waitFor(t, 5*time.Second, "long supervised reply not fully spoken", func() bool {
		return normalize(reconstruct()) == want
	})
	if got := normalize(reconstruct()); got != want {
		t.Fatalf("supervised reply truncated: got %d runes, want %d", len(got), len(want))
	}
	// Every emitted say stays bounded (the per-check size bound survived
	// the never-drop port).
	for _, s := range sink.sayTexts() {
		if utf8Len(s) > maxSentenceRunes {
			t.Errorf("say frame %q exceeds the %d-rune check bound", truncateForTest(s, 40), maxSentenceRunes)
		}
	}
}

func utf8Len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
