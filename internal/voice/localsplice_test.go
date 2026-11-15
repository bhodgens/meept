package voice

// Gate tests for the carrier-free local splice (leaf 05, contract CL7):
// 640B chunked mic->child, 20ms paced child->client, per-barge-in latch
// with a byte-exact clear frame and a single CancelDraft, JoinTimeout ->
// ErrNoClient, L16-only chunk refusal, and no goroutine leak on ctx
// cancel. Client side is net.Pipe here; leaf 07 hands unix conns (the
// splice only needs io.ReadWriter plus optional read deadlines).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// localDiscardLogger keeps splice logs out of test output.
func localDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func startLocalSplice(t *testing.T, ccid string, cfg LocalSpliceConfig, deps LocalSpliceDeps) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunLocalSplice(ctx, ccid, cfg, deps) }()
	return cancel, done
}

func stopLocalSplice(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("RunLocalSplice returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunLocalSplice did not exit within 2s of cancel")
	}
}

// localChildInBytes snapshots everything the child side received on .in
// (captured by dialAudioPair's reader, shared with the phone splice tests).
func localChildInBytes(ccid string) []byte {
	childInMu.Lock()
	defer childInMu.Unlock()
	return append([]byte(nil), childInCapture[ccid]...)
}

// localReadFull reads exactly len(buf) bytes with a bounded deadline so a
// broken pacer fails the test instead of hanging it.
func localReadFull(c net.Conn, buf []byte) error {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return err
		}
	}
	return nil
}

// T1: bytes written to Client appear on child .in chunk-aligned (full
// 640-byte chunks only, no partial flush); bytes pushed onto child .out
// reach Client paced (assert >= 9*20ms wall for 10 chunks, not exact, so
// CI jitter never flakes).
func TestLocalSpliceCarries(t *testing.T) {
	p := localPairFor(t, "ls-car-1")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: text, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	cancel, done := startLocalSplice(t, "ls-car-1", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	// Inbound: 2.5 chunks of mic bytes; the child must receive exactly the
	// first two full 640B chunks, byte-identical (L16 passthrough).
	mic := make([]byte, 640*2+320)
	for i := range mic {
		mic[i] = byte(i)
	}
	if _, err := micSide.Write(mic); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(localChildInBytes("ls-car-1")) >= 640*2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // quiesce: prove no partial-chunk flush
	got := localChildInBytes("ls-car-1")
	if len(got) != 640*2 {
		t.Fatalf("child stream settled at %d bytes, want exactly %d (chunk-aligned, no partial flush)", len(got), 640*2)
	}
	for i := range got {
		if got[i] != mic[i] {
			t.Fatalf("child byte %d: got %d want %d", i, got[i], mic[i])
		}
	}

	// Outbound: 10 child chunks paced to the client at 20ms per chunk.
	start := time.Now()
	for range 10 {
		if err := feedChild("ls-car-1"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	buf := make([]byte, 640)
	for i := range 10 {
		if err := localReadFull(micSide, buf); err != nil {
			t.Fatalf("speaker read %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 180*time.Millisecond {
		t.Errorf("10 paced chunks took %v, want >= 180ms (one chunk per 20ms)", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("10 paced chunks took %v, pacer stalled", elapsed)
	}
}

// localPairFor is rawPairFor with a sun_path-safe dir (the 103-byte bind
// limit; longer test names overflow t.TempDir()'s prefixed path).
func localPairFor(t *testing.T, ccid string) *AudioPair {
	t.Helper()
	dir := shortDir(t)
	p, err := NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	dialAudioPair(t, dir, ccid)
	return p
}

// T2: interrupt -> clear frame sent once (byte-exact), CancelDraft once,
// duplicate interrupts latch while no fresh PCM flows, and the latch
// re-arms when fresh child PCM arrives. Other calls are isolated by
// construction (one RunLocalSplice per ccid, and HandleInterrupt refuses
// a foreign ccid).
func TestLocalSpliceBargeIn(t *testing.T) {
	p := localPairFor(t, "ls-barge-1")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	var cmu sync.Mutex
	cancels := 0
	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        text,
		CancelDraft: func(string) { cmu.Lock(); cancels++; cmu.Unlock() },
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-barge-1", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	clearFrames := func() []string {
		text.mu.Lock()
		defer text.mu.Unlock()
		return append([]string(nil), text.frames...)
	}

	// Phase 1 (no audio flowing yet): two interrupts for the same barge-in
	// must latch - one clear, one cancel. With no child PCM in flight the
	// re-arm path cannot fire between them, so this is deterministic.
	// (Registration happens inside the splice goroutine: poll briefly.)
	sp := LocalSpliceFor("ls-barge-1")
	deadline := time.Now().Add(2 * time.Second)
	for sp == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		sp = LocalSpliceFor("ls-barge-1")
	}
	if sp == nil {
		t.Fatalf("local splice not registered under its ccid")
	}
	sp.HandleInterrupt("ls-barge-1")
	sp.HandleInterrupt("ls-barge-1")
	sp.HandleInterrupt("ls-other-call") // foreign ccid: must be a no-op

	deadline = time.Now().Add(2 * time.Second)
	for len(clearFrames()) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	frames := clearFrames()
	if len(frames) != 1 {
		t.Errorf("clear frames sent %d times, want exactly 1 per barge-in: %q", len(frames), frames)
	}
	// Byte-exact INCLUDING the trailing '\n': EncodeFrame's newline is
	// part of the C1 wire contract (the child scans '\n'-delimited
	// lines), so the seam must carry a complete frame.
	if len(frames) > 0 && frames[0] != "{\"call_id\":\"ls-barge-1\",\"event\":\"clear\"}\n" {
		t.Errorf("clear frame = %q, want byte-exact {\"call_id\":...,\"event\":\"clear\"}+\\n", frames[0])
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cmu.Lock()
		c := cancels
		cmu.Unlock()
		if c == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cmu.Lock()
	c := cancels
	cmu.Unlock()
	if c != 1 {
		t.Errorf("CancelDraft called %d times, want exactly 1", c)
	}

	// Speaker-side reader: counts paced bytes reaching the client.
	var bmu sync.Mutex
	read := 0
	stopRead := make(chan struct{})
	go func() {
		buf := make([]byte, 640)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, err := micSide.Read(buf)
			if n > 0 {
				bmu.Lock()
				read += n
				bmu.Unlock()
			}
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()
	defer close(stopRead)

	// Phase 2: fresh child PCM re-arms the latch - paced writes resume.
	for range 8 {
		if err := feedChild("ls-barge-1"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bmu.Lock()
		r := read
		bmu.Unlock()
		if r >= 640 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	bmu.Lock()
	if read < 640 {
		t.Fatalf("writes did not re-arm on fresh child PCM (client got %d bytes)", read)
	}
	bmu.Unlock()

	// Phase 3: a second, distinct barge-in while audio flows. Stop the
	// feed first so the socket drains to quiescence and the post-interrupt
	// silence assertion is bounded (only finite stale bytes can remain).
	sp.HandleInterrupt("ls-barge-1")
	deadline = time.Now().Add(2 * time.Second)
	for len(clearFrames()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if frames = clearFrames(); len(frames) != 2 {
		t.Errorf("after the second barge-in, clear frames = %d (%q), want 2 (one per barge-in)", len(frames), frames)
	}
	cmu.Lock()
	c = cancels
	cmu.Unlock()
	if c != 2 {
		t.Errorf("CancelDraft called %d times total, want 2 (once per barge-in)", c)
	}

	// Writes stop: once the finite stale backlog drains and the stream
	// goes quiet, the client must see zero new bytes for a full window.
	last := -1
	lastChange := time.Now()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bmu.Lock()
		r := read
		bmu.Unlock()
		if r != last {
			last = r
			lastChange = time.Now()
		} else if time.Since(lastChange) >= 300*time.Millisecond {
			break // quiescent: the stale backlog has fully drained
		}
		time.Sleep(20 * time.Millisecond)
	}
	bmu.Lock()
	before := read
	bmu.Unlock()
	time.Sleep(300 * time.Millisecond)
	bmu.Lock()
	after := read
	bmu.Unlock()
	if after != before {
		t.Errorf("client received %d new bytes in the 300ms post-quiescence window, want 0 (writes must stop after barge-in)", after-before)
	}
}

// T3: JoinTimeout -> ErrNoClient when the client never connects/never
// writes, within the configured window (not the 10s default).
func TestLocalSpliceJoinTimeout(t *testing.T) {
	p := rawPairFor(t, "ls-join-1")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: &textSink{}, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	start := time.Now()
	err := RunLocalSplice(context.Background(), "ls-join-1", LocalSpliceConfig{JoinTimeout: 150 * time.Millisecond}, deps)
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNoClient) {
		t.Fatalf("RunLocalSplice err = %v, want ErrNoClient", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("join took %v, want the configured 150ms window (not the default 10s)", elapsed)
	}
}

// T2b (link-72a04aebac963cdb root-cause repro): the splice starts while
// the child has NOT yet dialed the pair's .out socket — the exact live
// ordering inside LocalLeg.Open (splice goroutine vs the child's
// setup-triggered dial). The outbound pump must treat the pre-accept
// "child not connected" shape as a WAIT (the phone splice polls it at
// splice.go:906), not a terminal error. Before the fix, whichever leg
// lost the dial race had its outboundLoop return at +0 ms (cadence
// summary logged with pump_chunks=0 first_child_pcm_set=false while the
// call stayed "alive" for 90 s): the client-side pacer died, the OTHER
// leg never received a first mic byte, and its join grace reaped it.
func TestLocalSpliceLateChildDialCarries(t *testing.T) {
	dir := shortDir(t)
	const ccid = "ls-late-1"
	p, err := NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: &textSink{}, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	cancel, done := startLocalSplice(t, ccid, LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	// Join the client side (inbound gate) — the dial RACE we are testing
	// is the outbound child read, so the client must be live to prove the
	// splice itself stays up.
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}

	// Let the outbound pump hit the pre-accept window several times over
	// (a terminal 'child read' fires within microseconds of the splice
	// goroutine starting; 200 ms is generous).
	time.Sleep(200 * time.Millisecond)

	// NOW the child dials, like the live child does after answering setup.
	childIn, err := net.Dial("unix", filepath.Join(dir, ccid+".in"))
	if err != nil {
		t.Fatalf("dial child .in: %v", err)
	}
	defer func() { _ = childIn.Close() }()
	childOut, err := net.Dial("unix", filepath.Join(dir, ccid+".out"))
	if err != nil {
		t.Fatalf("dial child .out: %v", err)
	}
	defer func() { _ = childOut.Close() }()
	// The pair's Accept goroutines claim the first conn per socket; give
	// them a settle window (same shape as dialAudioPair).
	time.Sleep(100 * time.Millisecond)

	// The splice must still be running — the pre-accept window is NOT a
	// call end.
	select {
	case err := <-done:
		t.Fatalf("splice ended on the pre-accept child read (the link-72a04aebac963cdb half-death): err=%v", err)
	default:
	}

	// Feed 5 child chunks; they must reach the client paced.
	payload := make([]byte, 0, 5*640)
	for i := range 5 {
		chunk := make([]byte, 640)
		for j := range chunk {
			chunk[j] = byte((i*640 + j) % 251)
		}
		if _, err := childOut.Write(chunk); err != nil {
			t.Fatalf("child .out write: %v", err)
		}
		payload = append(payload, chunk...)
	}

	got := make([]byte, len(payload))
	_ = micSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(micSide, got); err != nil {
		t.Fatalf("paced bytes never reached the client after the late child dial (outbound loop was dead): %v", err)
	}
	for i := range got {
		if got[i] != payload[i] {
			t.Fatalf("client byte %d: got %d want %d", i, got[i], payload[i])
		}
	}
}

// T2c (half-death observability regression): once the child HAS dialed,
// a terminal child-read failure (child conn breaks mid-call) must end the
// WHOLE splice promptly — not leave a zombie with a dead outbound while
// the inbound blocks forever on the alive client (link-72a04aebac963cdb:
// B's splice never returned; local.go's ERROR branch never logged, so the
// run post-mortem had to infer the death from the +0 ms cadence summary).
func TestLocalSpliceOutboundFailureEndsSplice(t *testing.T) {
	dir := shortDir(t)
	const ccid = "ls-outfail-1"
	p, err := NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	// Child dialed BEFORE the splice starts (isolates this test from the
	// pre-accept race that T2b pins).
	childIn, err := net.Dial("unix", filepath.Join(dir, ccid+".in"))
	if err != nil {
		t.Fatalf("dial child .in: %v", err)
	}
	defer func() { _ = childIn.Close() }()
	childOut, err := net.Dial("unix", filepath.Join(dir, ccid+".out"))
	if err != nil {
		t.Fatalf("dial child .out: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let the pair's Accept goroutines claim

	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: &textSink{}, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunLocalSplice(context.Background(), ccid, LocalSpliceConfig{}, deps)
	}()

	// Join the client side, then break the child's .out: the accepted
	// daemon-side read sees EOF and the outbound pump dies terminally.
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := childOut.Close(); err != nil {
		t.Fatalf("close child .out: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("RunLocalSplice returned nil after a terminal child EOF, want the child-read error")
		}
		if !errors.Is(err, io.EOF) {
			t.Fatalf("RunLocalSplice err = %v, want a child-read failure wrapping io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("splice stayed half-dead 5 s after the child .out broke (terminal outbound error must end the splice)")
	}
}

// The local leg negotiates L16 s16le@16000 only: any other configured
// chunk size is a named error at start, before any goroutine starts.
func TestLocalSpliceRefusesNonL16Chunk(t *testing.T) {
	p := localPairFor(t, "ls-l16-1")
	_, daemonSide := net.Pipe()
	defer func() { _ = daemonSide.Close() }()

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: &textSink{}, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	err := RunLocalSplice(context.Background(), "ls-l16-1", LocalSpliceConfig{ChunkBytes: 320}, deps)
	if !errors.Is(err, ErrL16Only) {
		t.Fatalf("ChunkBytes=320 err = %v, want ErrL16Only", err)
	}
}

// No goroutine leak on ctx cancel: with live traffic on both directions,
// cancelling ctx must settle the goroutine count back to baseline.
func TestLocalSpliceNoLeakOnCancel(t *testing.T) {
	baseline := runtime.NumGoroutine()
	p := localPairFor(t, "ls-leak-1")
	micSide, daemonSide := net.Pipe()

	deps := LocalSpliceDeps{Audio: p, Client: daemonSide, Text: &textSink{}, CancelDraft: func(string) {}, Logger: localDiscardLogger()}
	cancel, done := startLocalSplice(t, "ls-leak-1", LocalSpliceConfig{}, deps)

	// Live traffic while the splice runs.
	stopFeed := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopFeed:
				return
			default:
			}
			if feedChild("ls-leak-1") != nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	stopRead := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			_, err := micSide.Read(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("RunLocalSplice did not exit within 2s of cancel")
	}
	close(stopFeed)
	close(stopRead)
	_ = micSide.Close()
	_ = daemonSide.Close()
	_ = p.Close() // release the pair's own goroutines (t.Cleanup Close is idempotent)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("goroutines did not settle after ctx cancel: baseline %d, now %d", baseline, runtime.NumGoroutine())
}

// ---------------------------------------------------------------------------
// Audio-drain watch (turn-taking leaf 02: drain-driven floor release).
// The splice fires deps.AudioDrained(ccid) once per ArmDrainWatch when the
// outbound path has WRITTEN every child chunk of the turn and gone quiet
// for cfg.DrainQuiet, with the early-fire guard: no touch since the arm =
// no fire (the final sentence may still be synthesizing).
// ---------------------------------------------------------------------------

func startDrainSplice(t *testing.T, ccid string, cfg LocalSpliceConfig, drained func(string)) (*AudioPair, net.Conn, net.Conn, *textSink, context.CancelFunc, <-chan error) {
	t.Helper()
	p := localPairFor(t, ccid)
	micSide, daemonSide := net.Pipe()
	t.Cleanup(func() { _ = micSide.Close(); _ = daemonSide.Close() })
	text := &textSink{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunLocalSplice(ctx, ccid, cfg, LocalSpliceDeps{
			Audio:        p,
			Client:       daemonSide,
			Text:         text,
			CancelDraft:  func(string) {},
			AudioDrained: drained,
			Logger:       localDiscardLogger(),
		})
	}()
	return p, micSide, daemonSide, text, cancel, done
}

// waitForSplice polls the package registry until the splice registers
// (registration happens inside the splice goroutine).
func waitForSplice(t *testing.T, ccid string) *runLocalSplice {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sp := LocalSpliceFor(ccid); sp != nil {
			return sp
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("splice for %s never registered", ccid)
	return nil
}

func waitForFire(t *testing.T, deadline time.Duration, what string, fired *atomic.Bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if fired.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: drain signal never fired", what)
}

// T-D1: fed audio paced to the client + quiescence -> exactly one fire,
// AFTER the last paced write (never early), and re-arming for a second
// turn fires again. The watch goroutine must not outlive the splice.
func TestLocalSpliceDrainWatchFiresAfterQuiescence(t *testing.T) {
	var fired atomic.Bool
	var fireCount atomic.Int64
	drain := func(ccid string) {
		fireCount.Add(1)
		fired.Store(true)
	}
	cfg := LocalSpliceConfig{DrainQuiet: 120 * time.Millisecond}
	p, micSide, _, _, cancel, done := startDrainSplice(t, "ls-drain-1", cfg, drain)
	_ = p
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("splice did not exit after cancel")
		}
	}()
	sp := waitForSplice(t, "ls-drain-1")

	// Turn 1: 5 chunks paced (100 ms), then arm at the text-side
	// turn_final. The watch must hold until the backlog drains AND the
	// quiet window elapses after the LAST write.
	for range 5 {
		if err := feedChild("ls-drain-1"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	arm := time.Now()
	sp.ArmDrainWatch()

	// Read the paced bytes so the pacer is not blocked on net.Pipe (the
	// client side must consume; net.Pipe is synchronous).
	go func() {
		buf := make([]byte, 640)
		for range 5 {
			_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.ReadFull(micSide, buf)
		}
	}()

	waitForFire(t, 3*time.Second, "turn-1 drain", &fired)
	elapsed := time.Since(arm)
	if elapsed < cfg.DrainQuiet {
		t.Fatalf("drain fired after %v < quiet window %v (early release is the bug)", elapsed, cfg.DrainQuiet)
	}
	if n := fireCount.Load(); n != 1 {
		t.Fatalf("drain fired %d times for one arm, want exactly 1", n)
	}

	// Re-arm with nothing flowing: the post-arm-touch guard means a
	// silent stream NEVER fires - the second arm must NOT emit.
	fired.Store(false)
	sp.ArmDrainWatch()
	time.Sleep(3 * cfg.DrainQuiet)
	if fired.Load() {
		t.Fatal("drain fired on a silent post-arm stream (early-fire guard broken)")
	}
	// Fresh audio after the guard-held arm -> the watch fires again
	// (the guard lifts on the first post-arm touch + quiet window).
	if err := feedChild("ls-drain-1"); err != nil {
		t.Fatalf("feedChild 2: %v", err)
	}
	go func() {
		buf := make([]byte, 640)
		_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.ReadFull(micSide, buf)
	}()
	waitForFire(t, 3*time.Second, "post-guard drain", &fired)
	if n := fireCount.Load(); n != 2 {
		t.Fatalf("drain fired %d times total, want 2 (one per armed turn with audio)", n)
	}
}

// T-D5 (measured production defect, link-6eb490abd8349958): a real
// child's TTS stream length is NEVER a multiple of 640 — the last write
// leaves sub-chunk residue in the pump's carry, permanently non-zero
// mid-call (every playback cadence line in the daemon log had
// bytes % 640 != 0). The drain watch must still fire on that turn: the
// old `carry==0` predicate term made reason=drained structurally
// unreachable (0/5 drained releases, every floor release rode the
// margin). Pins: feed whole chunks PLUS a partial tail, arm, and the
// fire lands after pending drains + the quiet window even though carry
// stays non-zero.
func TestLocalSpliceDrainFiresWithNonAlignedTail(t *testing.T) {
	var fired atomic.Bool
	var fireCount atomic.Int64
	drain := func(string) {
		fireCount.Add(1)
		fired.Store(true)
	}
	cfg := LocalSpliceConfig{DrainQuiet: 150 * time.Millisecond}
	_, micSide, _, _, cancel, done := startDrainSplice(t, "ls-drain-5", cfg, drain)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("splice did not exit after cancel")
		}
	}()
	sp := waitForSplice(t, "ls-drain-5")

	// Three whole chunks, then arm (turn_final), THEN the 300-byte
	// partial tail arrives — the real shape: the final sentence's PCM
	// lands after the text edge. The tail forms no frame, so carry
	// stays at 300 for the rest of the call (the exact residue every
	// real turn leaves behind) and its pump read is the guaranteed
	// post-arm touch.
	for range 3 {
		if err := feedChild("ls-drain-5"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}

	go func() {
		buf := make([]byte, 640)
		for range 3 {
			_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.ReadFull(micSide, buf)
		}
	}()

	arm := time.Now()
	if !sp.ArmDrainWatch() {
		t.Fatal("ArmDrainWatch must report the drain signal as live")
	}
	if err := feedChildBytes("ls-drain-5", 300); err != nil {
		t.Fatalf("partial tail write: %v", err)
	}
	// The carry gate is gone: with residue stuck at 300, the fire must
	// still land on pending==0 + quiet.
	waitForFire(t, 3*time.Second, "non-aligned-tail drain", &fired)
	if elapsed := time.Since(arm); elapsed < cfg.DrainQuiet {
		t.Fatalf("drain fired after %v < quiet window %v (early)", elapsed, cfg.DrainQuiet)
	}
	sp.mu.Lock()
	carry := sp.audioCarry.Load()
	sp.mu.Unlock()
	if carry == 0 {
		t.Fatal("test shape broken: expected non-zero carry residue with a partial tail")
	}
	if n := fireCount.Load(); n != 1 {
		t.Fatalf("drain fired %d times, want 1", n)
	}
}

// T-D6: ArmDrainWatch's bool reports seam liveness for the coordinator's
// WARN instrumentation — nil dep -> false (plain local/phone calls),
// wired dep -> true (fresh arm AND a join onto an armed watch), ended
// splice -> false.
func TestLocalSpliceArmDrainWatchReportsLiveness(t *testing.T) {
	p := localPairFor(t, "ls-drain-6")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunLocalSplice(ctx, "ls-drain-6", LocalSpliceConfig{DrainQuiet: 80 * time.Millisecond}, LocalSpliceDeps{
			Audio: p, Client: daemonSide, Text: &textSink{},
			CancelDraft: func(string) {},
			Logger:      localDiscardLogger(),
		})
	}()
	sp := waitForSplice(t, "ls-drain-6")
	if sp.ArmDrainWatch() {
		t.Fatal("nil AudioDrained dep must report the drain signal as DEAD")
	}
	sp.mu.Lock()
	armed, running := sp.drainArmed, sp.watchRunning
	sp.mu.Unlock()
	if armed || running {
		t.Fatalf("nil dep armed a watch (armed=%v running=%v)", armed, running)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D2: arm BEFORE any audio (the final sentence still synthesizing at
// turn_final) - no fire while silent; when the audio finally flows, the
// fire lands after THAT audio is written + quiet, never before.
func TestLocalSpliceDrainWatchNoEarlyFireDuringSynthGap(t *testing.T) {
	var fired atomic.Bool
	drain := func(string) { fired.Store(true) }
	cfg := LocalSpliceConfig{DrainQuiet: 120 * time.Millisecond}
	_, micSide, _, _, cancel, done := startDrainSplice(t, "ls-drain-2", cfg, drain)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("splice did not exit after cancel")
		}
	}()
	sp := waitForSplice(t, "ls-drain-2")

	// Arm with the channel EMPTY and no post-arm touch yet (mirrors
	// turn_final racing the child's last TTS request).
	sp.ArmDrainWatch()
	time.Sleep(4 * cfg.DrainQuiet)
	if fired.Load() {
		t.Fatal("drain fired before any post-arm audio existed (the measured talk-over bug)")
	}

	// The late sentence's PCM arrives now; reader drains the paced chunk.
	go func() {
		buf := make([]byte, 640)
		_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.ReadFull(micSide, buf)
	}()
	start := time.Now()
	if err := feedChild("ls-drain-2"); err != nil {
		t.Fatalf("feedChild: %v", err)
	}
	waitForFire(t, 3*time.Second, "late-audio drain", &fired)
	if elapsed := time.Since(start); elapsed < cfg.DrainQuiet {
		t.Fatalf("drain fired %v after the late PCM, want >= quiet window %v", elapsed, cfg.DrainQuiet)
	}
}

// T-D3: nil AudioDrained dep (every plain local / phone call) - arming is
// a silent no-op, no goroutine, no fire, splice unaffected.
func TestLocalSpliceDrainWatchNilDepIsNoop(t *testing.T) {
	p := localPairFor(t, "ls-drain-3")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunLocalSplice(ctx, "ls-drain-3", LocalSpliceConfig{DrainQuiet: 80 * time.Millisecond}, LocalSpliceDeps{
			Audio: p, Client: daemonSide, Text: &textSink{},
			CancelDraft: func(string) {},
			Logger:      localDiscardLogger(),
		})
	}()
	sp := waitForSplice(t, "ls-drain-3")
	sp.ArmDrainWatch() // nil dep: must no-op without starting a watch
	sp.mu.Lock()
	armed, running := sp.drainArmed, sp.watchRunning
	sp.mu.Unlock()
	if armed || running {
		t.Fatalf("nil AudioDrained dep armed a watch (armed=%v running=%v)", armed, running)
	}
	// The splice still carries audio normally.
	if err := feedChild("ls-drain-3"); err != nil {
		t.Fatalf("feedChild: %v", err)
	}
	buf := make([]byte, 640)
	_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(micSide, buf); err != nil {
		t.Fatalf("paced write lost: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D4: watch goroutine does not outlive RunLocalSplice (ctx-cancel exit).
func TestLocalSpliceDrainWatchNoGoroutineLeak(t *testing.T) {
	var fired atomic.Bool
	drain := func(string) { fired.Store(true) }
	p, _, _, _, cancel, done := startDrainSplice(t, "ls-drain-4", LocalSpliceConfig{DrainQuiet: 200 * time.Millisecond}, drain)
	sp := waitForSplice(t, "ls-drain-4")
	sp.ArmDrainWatch() // armed, never fires (silent) - the goroutine parks on ticks
	time.Sleep(100 * time.Millisecond)
	// Baseline AFTER every setup goroutine (pair listeners, dial capture,
	// splice loops, the watch itself) is live: the assertion is that the
	// count never EXCEEDS the warm steady state after exit.
	baseline := runtime.NumGoroutine()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
	_ = p
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("drain watch leaked past splice exit: baseline %d now %d", baseline, runtime.NumGoroutine())
}

// ---------------------------------------------------------------------------
// Played-armed drain (C1 played edge = true end-of-turn marker).
// The link floor no longer blind-arms at turn_final (the child's inter-
// sentence synth gaps of 1.4-3.8 s exceed the quiet window and fake a
// mid-reply drain - link-2fdc3f5fd8d81805). ExpectFinalDrain records the
// turn_final edge WITHOUT arming; the child's played edge arms through
// MarkFinalPlayed, whose fire predicate is pending==0 + the SHORT
// DrainQuietFinal window (after the edge nothing more CAN arrive).
// ---------------------------------------------------------------------------

// T-D7: expect-final set + a mid-stream synth gap must NOT release. This
// is the exact regression the played edge kills: chunks pace out, the
// stream goes quiet for longer than EVERY window while more sentences are
// still to come - the old blind arm fired here (proven by the sibling
// shape below), the expect-only state must stay silent until the edge.
func TestLocalSpliceExpectFinalDoesNotFireOnSynthGap(t *testing.T) {
	var fired atomic.Bool
	drain := func(string) { fired.Store(true) }
	cfg := LocalSpliceConfig{DrainQuiet: 80 * time.Millisecond, DrainQuietFinal: 40 * time.Millisecond}
	_, micSide, _, _, cancel, done := startDrainSplice(t, "ls-played-1", cfg, drain)
	sp := waitForSplice(t, "ls-played-1")

	// Read side so the pacer never blocks (net.Pipe is synchronous).
	stopRead := make(chan struct{})
	go func() {
		buf := make([]byte, 640)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, _ = micSide.Read(buf)
		}
	}()
	defer close(stopRead)

	// Sentence 1's audio paced fully, then the coordinator's turn_final
	// text edge (the employee's last say FRAME left the daemon while the
	// child still has sentence 2 synthesizing).
	for range 3 {
		if err := feedChild("ls-played-1"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	time.Sleep(60 * time.Millisecond)
	if !sp.ExpectFinalDrain() {
		t.Fatal("ExpectFinalDrain must report the drain signal live (dep wired)")
	}
	// Wait far past BOTH quiet windows with the stream silent - the
	// expect-only state has nothing armed to fire.
	time.Sleep(400 * time.Millisecond)
	sp.mu.Lock()
	armed := sp.drainArmed
	expect := sp.drainExpectFinal
	sp.mu.Unlock()
	if armed {
		t.Fatal("ExpectFinalDrain must NOT arm the watch (blind arm is the early-fire bug)")
	}
	if !expect {
		t.Fatal("expect-final flag must stay set until the played edge or a fire")
	}
	if fired.Load() {
		t.Fatal("floor drain fired mid-reply on a synth gap (the 10.94 s overlap bug)")
	}

	// Now the played edge lands (sentence 2's final byte left the child's
	// socket - here sentence 1 WAS actually the whole turn): arms in
	// final mode, fires at pending==0 + the short window.
	if !sp.MarkFinalPlayed() {
		t.Fatal("MarkFinalPlayed must arm the drain watch on a live splice")
	}
	waitForFire(t, 3*time.Second, "played-armed drain", &fired)
	sp.mu.Lock()
	consumed := !sp.drainArmed && !sp.drainExpectFinal
	sp.mu.Unlock()
	if !consumed {
		t.Fatal("fire must consume both the arm and the expect flag")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D8: played-armed watch fires on a SILENT post-arm stream - the shape
// the blind arm deliberately refuses (there the silent stream could still
// be synthesizing). After the played edge nothing more can arrive, so
// pending==0 + quiet is terminal even with zero post-arm audio touches.
func TestLocalSpliceMarkFinalPlayedFiresOnSilentStream(t *testing.T) {
	var fired atomic.Bool
	var fireCount atomic.Int64
	drain := func(string) { fireCount.Add(1); fired.Store(true) }
	cfg := LocalSpliceConfig{DrainQuiet: time.Second, DrainQuietFinal: 60 * time.Millisecond}
	_, _, _, _, cancel, done := startDrainSplice(t, "ls-played-2", cfg, drain)
	sp := waitForSplice(t, "ls-played-2")

	// Nothing ever fed (denied-empty turn that STILL played: the child
	// drained a zero-chunk final stream and sent the edge).
	if !sp.ExpectFinalDrain() {
		t.Fatal("expect must be live with the dep wired")
	}
	if !sp.MarkFinalPlayed() {
		t.Fatal("played edge must arm")
	}
	waitForFire(t, 3*time.Second, "silent played-armed drain", &fired)
	if n := fireCount.Load(); n != 1 {
		t.Fatalf("played-armed drain fired %d times, want exactly 1", n)
	}
	// A second played edge after the fire re-arms (next turn's edge;
	// stale fires are covered by the coordinator's generation guards).
	fired.Store(false)
	sp.MarkFinalPlayed()
	waitForFire(t, 3*time.Second, "second played edge re-arm", &fired)
	if n := fireCount.Load(); n != 2 {
		t.Fatalf("played-armed drain fired %d times after two edges, want 2", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D9: queued final chunks + a played edge mid-drain: the fire waits for
// pending==0 even though the edge already landed - the short final window
// only replaces the post-arm-touch guard, never the backlog gate.
func TestLocalSplicePlayedArmedWaitsForPendingDrain(t *testing.T) {
	var fired atomic.Bool
	drain := func(string) { fired.Store(true) }
	cfg := LocalSpliceConfig{DrainQuiet: time.Second, DrainQuietFinal: 60 * time.Millisecond}
	_, micSide, _, _, cancel, done := startDrainSplice(t, "ls-played-3", cfg, drain)
	sp := waitForSplice(t, "ls-played-3")

	// 10 chunks (~200 ms of pacing at 20 ms each) are in the pacer when
	// the played edge lands (the child wrote its last byte; the daemon
	// still has the backlog to pace).
	for range 10 {
		if err := feedChild("ls-played-3"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	if !sp.MarkFinalPlayed() {
		t.Fatal("played edge must arm")
	}
	go func() {
		buf := make([]byte, 640)
		for range 10 {
			_ = micSide.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.ReadFull(micSide, buf)
		}
	}()
	arm := time.Now()
	waitForFire(t, 3*time.Second, "played-armed backlog drain", &fired)
	if elapsed := time.Since(arm); elapsed < 10*20*time.Millisecond {
		t.Fatalf("drain fired after %v while 10 paced chunks (~200ms) were still pending", elapsed)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D10: nil-dep splice (every plain local / phone call): both new seam
// methods are no-ops reporting liveness false - no goroutine, no flags.
func TestLocalSplicePlayedSeamsNilDepIsNoop(t *testing.T) {
	p := localPairFor(t, "ls-played-4")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunLocalSplice(ctx, "ls-played-4", LocalSpliceConfig{DrainQuiet: 80 * time.Millisecond}, LocalSpliceDeps{
			Audio: p, Client: daemonSide, Text: &textSink{},
			CancelDraft: func(string) {},
			Logger:      localDiscardLogger(),
		})
	}()
	sp := waitForSplice(t, "ls-played-4")
	if sp.ExpectFinalDrain() {
		t.Fatal("nil dep must report expect as dead")
	}
	if sp.MarkFinalPlayed() {
		t.Fatal("nil dep must report the played arm as dead")
	}
	sp.mu.Lock()
	armed, expect, running := sp.drainArmed, sp.drainExpectFinal, sp.watchRunning
	sp.mu.Unlock()
	if armed || expect || running {
		t.Fatalf("nil dep set drain state (armed=%v expect=%v running=%v)", armed, expect, running)
	}
	// Nil receiver too (link.go resolves LocalSpliceFor -> may be nil).
	var nilSp *runLocalSplice
	if nilSp.ExpectFinalDrain() || nilSp.MarkFinalPlayed() {
		t.Fatal("nil receiver must report dead")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// T-D11: the expect-mode parked watcher does not outlive the splice (a
// turn whose played edge never arrives must not leak the goroutine the
// coordinator's margin-release path left parked).
func TestLocalSpliceExpectFinalParkNoGoroutineLeak(t *testing.T) {
	drain := func(string) {}
	_, _, _, _, cancel, done := startDrainSplice(t, "ls-played-5", LocalSpliceConfig{DrainQuiet: 200 * time.Millisecond}, drain)
	sp := waitForSplice(t, "ls-played-5")
	sp.ExpectFinalDrain() // parked watcher, never armed, never fires
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("expect-mode watcher leaked past splice exit: baseline %d now %d", baseline, runtime.NumGoroutine())
}

// ---------------------------------------------------------------------------
// Turn-2 leaf 01: barge-in drains the stale pump backlog AT THE LATCH.
// The A6 gate dropped stale chunks only at CONSUME time — one paced select
// per 20 ms — so the resume on the NEXT say paced the up-to-64-chunk
// (1.28 s) stale queue out before the new answer started (live
// local-4dcfb291c2094ec9: the operator heard ~1.3 s of the old answer
// after barge-in; the 968 ms pacer-stall WARN at 17:15:16.021 is that
// backlog pacing its way out). HandleInterrupt now drains the pump
// channel + resets the carry, logs the drop, and the client hears at most
// the ~1 chunk already in paceOne.
// ---------------------------------------------------------------------------

// feedChildStamped writes one 640-byte child chunk whose every byte is
// `stamp`, so a client-side reader can attribute every received chunk to
// its generation (1 = interrupted stream, 2 = new say).
func feedChildStamped(t *testing.T, ccid string, stamp byte) {
	t.Helper()
	childOutMu.Lock()
	defer childOutMu.Unlock()
	c, ok := childOut[ccid]
	if !ok {
		t.Fatalf("feedChildStamped %s: child .out not dialed", ccid)
	}
	buf := make([]byte, 640)
	for i := range buf {
		buf[i] = stamp
	}
	if _, err := c.Write(buf); err != nil {
		t.Fatalf("feedChildStamped %s: %v", ccid, err)
	}
}

// localSpliceLogger builds a splice logger over a syncBuffer for the
// drain-telemetry pins (pattern copied from the link coordinator tests).
func localSpliceLogger(buf *syncBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

// startStampedReader reads 640-byte chunks off the client side and
// records each chunk's stamp byte under mu. Returns a snapshot func.
func startStampedReader(t *testing.T, micSide net.Conn) func() ([]byte, int) {
	t.Helper()
	var mu sync.Mutex
	var stamps []byte
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		buf := make([]byte, 640)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := localReadFull(micSide, buf); err != nil {
				return
			}
			mu.Lock()
			stamps = append(stamps, buf[0])
			mu.Unlock()
		}
	}()
	return func() ([]byte, int) {
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), stamps...), len(stamps)
	}
}

// TestLocalSpliceBargeInDrainsStalePump: (a) the latch drains the queued
// stale backlog with a counted INFO line, the client sees at most ONE
// more chunk (the in-flight frame), and after the resume via the next
// say the client receives ONLY the new-generation chunks.
func TestLocalSpliceBargeInDrainsStalePump(t *testing.T) {
	const ccid = "ls-drain-1"
	p := localPairFor(t, ccid)
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	var logBuf syncBuffer
	deps := LocalSpliceDeps{
		Audio: p, Client: daemonSide, Text: text,
		CancelDraft: func(string) {},
		Logger:      localSpliceLogger(&logBuf),
	}
	cancel, done := startLocalSplice(t, ccid, LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, ccid)
	snapshot := startStampedReader(t, micSide)

	// Join the client (the join gate needs one mic byte).
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}

	// Old say: 50 stamped chunks land in the 64-slot pump queue far
	// faster than the 20 ms pacer drains them.
	for range 50 {
		feedChildStamped(t, ccid, 1)
	}
	// Playback live: at least one chunk paced out.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, n := snapshot(); n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	stampsBefore, nBefore := snapshot()
	if nBefore < 1 || stampsBefore[0] != 1 {
		t.Fatalf("precondition: playback never went live (chunks=%d)", nBefore)
	}

	sp.HandleInterrupt(ccid)

	// Telemetry: the drain counted the backlog drop.
	drainLine := ""
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(logBuf.String(), "\n") {
			if strings.Contains(line, "barge-in drained stale chunks") {
				drainLine = line
			}
		}
		if drainLine != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if drainLine == "" {
		t.Fatalf("no drain telemetry line; log:\n%s", logBuf.String())
	}
	// The backlog at latch time is 50 fed minus ~nBefore paced minus at
	// most 2 still arriving; the drain line counted it. Require a real
	// queue drop (>= 10 chunks — the defect's dominant term was seconds
	// of backlog, not one chunk).
	if !strings.Contains(drainLine, "chunks=") {
		t.Fatalf("drain telemetry missing chunks=N field: %q", drainLine)
	}
	chunksStr := drainLine[strings.Index(drainLine, "chunks=")+len("chunks="):]
	if space := strings.IndexAny(chunksStr, " 	"); space >= 0 {
		chunksStr = chunksStr[:space]
	}
	var drained int
	if _, err := fmt.Sscanf(chunksStr, "%d", &drained); err != nil || drained < 10 {
		t.Fatalf("drain counted %d chunks (%q), want >= 10 — the backlog was not drained", drained, drainLine)
	}

	// Post-latch tail: at most the ONE chunk already committed inside
	// paceOne may still land; then total silence until resume.
	_, tailStart := snapshot()
	time.Sleep(300 * time.Millisecond)
	tailStamps, tailTotal := snapshot()
	if grew := tailTotal - tailStart; grew > 1 {
		t.Fatalf("post-latch tail paced %d chunks, want <= 1 (in-flight only): stale queue was NOT drained", grew)
	}
	for i := tailStart; i < tailTotal; i++ {
		if tailStamps[i] != 1 {
			t.Fatalf("post-latch chunk %d is not the in-flight gen-1 frame", i)
		}
	}

	// Fresh say PAST the residue window: only gen-2 audio may reach the
	// client from here (nothing stale was left in the queue or carry).
	time.Sleep(localBargeResidueQuiet + 100*time.Millisecond)
	for range 4 {
		feedChildStamped(t, ccid, 2)
	}
	_, preResume := snapshot()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, n := snapshot(); n >= preResume+3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	finalStamps, finalTotal := snapshot()
	if finalTotal < preResume+3 {
		t.Fatalf("resume did not pace the new say (chunks %d, want >= %d)", finalTotal, preResume+3)
	}
	for i := preResume; i < finalTotal; i++ {
		if finalStamps[i] != 2 {
			t.Fatalf("chunk %d after the fresh say carries stamp %d, want 2: stale audio replayed under the new turn", i, finalStamps[i])
		}
	}
}

// TestLocalSpliceBargeInDrainPreservesTerminalError: a terminal child-read
// error that lands while the barge-in latch parks the pacer must still end
// the leg with the named error (CL7), never get swallowed behind the
// drained queue.
func TestLocalSpliceBargeInDrainPreservesTerminalError(t *testing.T) {
	const ccid = "ls-drain-err-1"
	p := localPairFor(t, ccid)
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunLocalSplice(ctx, ccid, LocalSpliceConfig{}, LocalSpliceDeps{
			Audio: p, Client: daemonSide, Text: &textSink{},
			CancelDraft: func(string) {}, Logger: localDiscardLogger(),
		})
	}()

	sp := waitForSplice(t, ccid)
	// Client joins and playback goes live (net.Pipe needs a reader).
	readStop := make(chan struct{})
	t.Cleanup(func() { close(readStop) })
	go func() {
		buf := make([]byte, 640)
		for {
			select {
			case <-readStop:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			if _, err := micSide.Read(buf); err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	for range 40 {
		feedChild(ccid)
	}
	time.Sleep(100 * time.Millisecond) // backlog builds in the pump queue

	sp.HandleInterrupt(ccid) // latch + drain

	// NOW break the child .out: the pump read returns EOF, stashes it,
	// closes pumpDead — the parked pacer must wake and END the leg with
	// the named error, not sit latched until ctx cancel.
	childOutMu.Lock()
	c := childOut[ccid]
	childOutMu.Unlock()
	if c == nil {
		t.Fatal("child .out conn missing")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close child .out: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("RunLocalSplice returned nil after the drained-then-terminal child EOF, want the child-read error")
		}
		if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the wrapped io.EOF child-read failure", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v: the terminal EOF was swallowed and only ctx cancel ended the leg", err)
		}
		if !errors.Is(err, io.EOF) {
			t.Fatalf("err = %v, want child-read failure wrapping io.EOF", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("leg stayed latched 4 s after the child .out broke past the drain: terminal error swallowed")
	}
}

// TestLocalSpliceBargeInDrainSkipsEmptyQueue: an interrupt with nothing
// queued emits NO drain line (telemetry stays quiet for the common empty
// case) — pinned per the leaf's "(c) no drain when queue empty — your
// call, pin it".
func TestLocalSpliceBargeInDrainSkipsEmptyQueue(t *testing.T) {
	const ccid = "ls-drain-empty-1"
	p := localPairFor(t, ccid)
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	var logBuf syncBuffer
	deps := LocalSpliceDeps{
		Audio: p, Client: daemonSide, Text: &textSink{},
		CancelDraft: func(string) {}, Logger: localSpliceLogger(&logBuf),
	}
	cancel, done := startLocalSplice(t, ccid, LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, ccid)
	// Join the client, feed nothing: the pump queue is empty.
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	sp.HandleInterrupt(ccid)
	// The latch itself still logs.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logBuf.String(), "voice: local barge-in") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logBuf.String(), "voice: local barge-in") {
		t.Fatalf("barge-in latch line missing; log:\n%s", logBuf.String())
	}
	if strings.Contains(logBuf.String(), "drained stale chunks") {
		t.Fatalf("empty queue emitted a drain line (telemetry must stay quiet):\n%s", logBuf.String())
	}
}

// TestLocalSpliceLatchBargeInAudioPlaneOnly: LatchBargeIn — the
// production trip — latches + drains WITHOUT emitting the control-plane
// frames (no clear, no CancelDraft: the talk loop owns those, and the
// frame that trips this latch is the SAME frame the loop handles).
func TestLocalSpliceLatchBargeInAudioPlaneOnly(t *testing.T) {
	const ccid = "ls-latch-audio-1"
	p := localPairFor(t, ccid)
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}
	var cancels atomic.Int32
	var logBuf syncBuffer

	deps := LocalSpliceDeps{
		Audio: p, Client: daemonSide, Text: text,
		CancelDraft: func(string) { cancels.Add(1) },
		Logger:      localSpliceLogger(&logBuf),
	}
	cancel, done := startLocalSplice(t, ccid, LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, ccid)
	// Drain the client side so paceOne's net.Pipe writes never block.
	startStampedReader(t, micSide)
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	for range 30 {
		feedChild(ccid)
	}
	time.Sleep(80 * time.Millisecond)

	// Trip + drain. A nil receiver is the phone-leg no-op too.
	(*runLocalSplice)(nil).LatchBargeIn()
	sp.LatchBargeIn()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logBuf.String(), "voice: local barge-in") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logBuf.String(), "voice: local barge-in") {
		t.Fatalf("LatchBargeIn did not latch; log:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "drained stale chunks") {
		t.Fatalf("LatchBargeIn did not drain the backlog; log:\n%s", logBuf.String())
	}
	// No control-plane emits.
	text.mu.Lock()
	nFrames := len(text.frames)
	text.mu.Unlock()
	if nFrames != 0 {
		t.Fatalf("LatchBargeIn sent %d text frames, want 0 (clear belongs to HandleInterrupt/talk loop)", nFrames)
	}
	if c := cancels.Load(); c != 0 {
		t.Fatalf("LatchBargeIn fired CancelDraft %d times, want 0", c)
	}
	// Duplicate trips while latched are no-ops (no second latch line).
	sp.LatchBargeIn()
	sp.LatchBargeIn()
	time.Sleep(100 * time.Millisecond)
	if got := strings.Count(logBuf.String(), "voice: local barge-in"); got != 1 {
		t.Fatalf("latch line fired %d times, want 1 (per-barge-in guard)", got)
	}
}

func TestS16leRMSOver(t *testing.T) {
	quiet := make([]byte, 4)
	if s16leRMSOver(quiet, 100) {
		t.Fatal("silence must not clear the floor")
	}
	loud := []byte{0x00, 0x40, 0x00, 0x40} // two samples of 16384
	if !s16leRMSOver(loud, 1000) {
		t.Fatal("loud samples must clear a 1000 floor")
	}
	if s16leRMSOver(loud, 0) {
		t.Fatal("a zero floor must never report loud")
	}
}
