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
	"io"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// localDiscardLogger keeps splice logs out of test output.
func localDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
	for i := 0; i < 10; i++ {
		if err := feedChild("ls-car-1"); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	buf := make([]byte, 640)
	for i := 0; i < 10; i++ {
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
	for i := 0; i < 8; i++ {
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
