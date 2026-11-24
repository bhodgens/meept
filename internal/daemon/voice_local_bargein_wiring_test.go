package daemon

// Voice turn-2 leaf 01 wiring pin: routeFrame's C1 interrupt frame must
// trip the live LOCAL SPLICE's audio-plane latch. Historically the frame
// reached only the talk loop (control plane): nothing ever called
// LocalSpliceFor(ccid).LatchBargeIn in production, so the splice kept
// pacing the old answer's pump backlog (up to 64 chunks = 1.28 s) toward
// the client after barge-in — the operator complaint on live
// local-4dcfb291c2094ec9. The pin here proves the seam is wired: the
// latch + drain fire through the splice's own logger, pacing stops, and
// a ccid with no local splice (phone leg) is a nil-safe no-op.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// recordingTextSender2 (unique name across the daemon test package)
// collects raw C1 lines.
type recordingTextSender2 struct {
	mu     sync.Mutex
	frames []string
}

func (s *recordingTextSender2) Send(raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, string(raw))
	return nil
}

func TestVoiceRouteFrameInterruptLatchesLocalSplice(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)

	dir, err := os.MkdirTemp("/tmp", "vbarge")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	const ccid = "local-dbarge-1"
	pair, err := voice.NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	t.Cleanup(func() { _ = pair.Close() })

	// Fake child: dial both pair sockets (the pair's accept goroutines
	// claim the first conn each).
	childIn, err := net.Dial("unix", filepath.Join(dir, ccid+".in"))
	if err != nil {
		t.Fatalf("dial .in: %v", err)
	}
	defer func() { _ = childIn.Close() }()
	childOut, err := net.Dial("unix", filepath.Join(dir, ccid+".out"))
	if err != nil {
		t.Fatalf("dial .out: %v", err)
	}
	defer func() { _ = childOut.Close() }()
	time.Sleep(50 * time.Millisecond) // let the accepts settle

	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()

	var logBuf bytes.Buffer
	logMu := &sync.Mutex{}
	lw := &lockedWriter{w: &logBuf, mu: logMu}
	spliceLog := slog.New(slog.NewTextHandler(lw, nil))
	logged := func() string {
		logMu.Lock()
		defer logMu.Unlock()
		return logBuf.String()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- voice.RunLocalSplice(ctx, ccid, voice.LocalSpliceConfig{}, voice.LocalSpliceDeps{
			Audio:       pair,
			Client:      daemonSide,
			Text:        &recordingTextSender2{},
			CancelDraft: func(string) {},
			Logger:      spliceLog,
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && voice.LocalSpliceFor(ccid) == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if voice.LocalSpliceFor(ccid) == nil {
		t.Fatal("local splice never registered")
	}

	// Client joins + playback goes live: 40 chunks land in the pump
	// queue far faster than the 20 ms pacer drains them.
	if _, err := micSide.Write(make([]byte, 640)); err != nil {
		t.Fatalf("mic write: %v", err)
	}
	var paced atomic.Int64
	readerStop := make(chan struct{})
	defer close(readerStop)
	go func() {
		b := make([]byte, 640)
		for {
			select {
			case <-readerStop:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, rerr := micSide.Read(b)
			paced.Add(int64(n))
			if rerr != nil {
				var ne net.Error
				if errors.As(rerr, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()
	for range 40 {
		if _, err := childOut.Write(make([]byte, 640)); err != nil {
			t.Fatalf("child PCM write: %v", err)
		}
	}

	// Synchronize the backlog INTO the splice's pump queue before the
	// latch. On Linux the unix socket buffer (~229 KB) holds the whole
	// 25.6 KB stream, so the read pump may not have queued a single
	// chunk when routeFrame lands: the latch-time drain then correctly
	// finds an empty queue (the post-latch reads are discarded by the
	// stale gate instead) and the "drained stale chunks" line never
	// appears - a platform artifact, not the queue-drain shape the test
	// pins. Evidence that chunks ARE in flight: two paced chunks
	// delivered to the client. The client leg is a net.Pipe (writes
	// block until the test's reader consumes), so a delivered chunk
	// proves the pump queued the backlog: two consumed, the remaining
	// ~38 still sitting in the queue for the drain. Bounded condition
	// poll, no fixed sleep, assertions unchanged on every platform.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && paced.Load() < 2*640 {
		time.Sleep(5 * time.Millisecond)
	}
	if paced.Load() < 2*640 {
		t.Fatalf("splice never paced the child backlog to the client before the latch")
	}

	// THE SEAM: the child's C1 interrupt frame through routeFrame. No
	// talk loop is armed for this ccid (the latch runs BEFORE the loop
	// lookup; the frame itself then rides the no-loop drop path, which
	// is fine — the audio-plane latch is the behavior under test).
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventInterrupt})

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logged(), "voice: local barge-in") {
		time.Sleep(5 * time.Millisecond)
	}
	logText := logged()
	if !strings.Contains(logText, "voice: local barge-in") {
		t.Fatalf("routeFrame(EventInterrupt) did NOT latch the live local splice; splice log:\n%s", logText)
	}
	if !strings.Contains(logText, "barge-in drained stale chunks") {
		t.Fatalf("routeFrame(EventInterrupt) latched without draining the backlog; splice log:\n%s", logText)
	}

	// Pacing must STOP: the backlog was drained at the latch, so after
	// a short window no further bytes reach the client.
	time.Sleep(150 * time.Millisecond)
	before := paced.Load()
	time.Sleep(400 * time.Millisecond)
	if grew := paced.Load() - before; grew > 640 {
		t.Fatalf("client paced %d more bytes after the latch (want <= 1 chunk: the stale queue was not drained)", grew)
	}

	// Phone-leg no-op: no local splice under this ccid -> nil receiver
	// latch, must not panic.
	w.routeFrame(voice.Frame{CallID: "cc-phone-no-splice", Event: voice.EventInterrupt})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit on cancel")
	}
}
