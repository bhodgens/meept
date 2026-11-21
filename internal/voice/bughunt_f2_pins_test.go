package voice

// Pin tests for the bughunt 2026-11-10 fixer F2 wave (internal/voice):
// A3 manager spawn-race unwind, A4 text-server write deadline, A6
// localsplice fresh-audio barge-in re-arm, A7 RawMicCapture Read/Close
// mutex scope, A8 link deferred-release watchdog, A9 childproc stale
// sweep client-pair exclusion. Each test names its finding id; the fix
// is the code under test, the pin is the regression it must never allow.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// ---------------------------------------------------------------------------
// A3 (HIGH): hangup racing an in-flight inbound spawn must release the
// registry slot and leave NO leaked m.calls entry.
// ---------------------------------------------------------------------------

// blockingSpawn simulates the real spawn seam: slow enough that a hangup
// event can be processed while the spawn is in flight.
func blockingSpawn(release <-chan struct{}) func(context.Context, telnyx.CallEvent) (context.CancelFunc, error) {
	return func(_ context.Context, _ telnyx.CallEvent) (context.CancelFunc, error) {
		<-release
		return func() {}, nil
	}
}

func TestManagerHangupDuringSpawnReleasesSlot(t *testing.T) {
	tel := &fakeController{}
	spawnGate := make(chan struct{})
	m := NewManager(ManagerConfig{
		StreamURL: "wss://tunnel.example/media",
		Codec:     telnyx.Codec{BidirectionalCodec: "L16", SamplingRate: 16000},
	}, tel, blockingSpawn(spawnGate))

	released := make(chan Outcome, 4)
	m.SetReleaseHook(func(ccid string, outcome Outcome) { released <- outcome })

	// Start the inbound spawn in the background (as the webhook handler
	// does); the spawn blocks on spawnGate.
	spawnDone := make(chan error, 1)
	go func() {
		spawnDone <- m.HandleCallEvent(context.Background(), inboundInitiated("cc-race-1"))
	}()
	// Wait until the handler registered the done entry (the pre-spawn
	// step): from here the hangup deterministically races the SPAWN, not
	// the map insert.
	waitFor(t, 2*time.Second, "done registered", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.done["cc-race-1"]
		return ok
	})

	// While the spawn is in flight, the hangup arrives: done is
	// registered, so releaseOnce fires the fan-out and DELETES the done
	// entry before the spawn returns.
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-race-1",
	}); err != nil {
		t.Fatalf("HandleCallEvent(hangup during spawn): %v", err)
	}
	select {
	case o := <-released:
		if o != OutcomeHangup {
			t.Fatalf("hangup-time release outcome = %v, want hangup", o)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("hangup during spawn did not fire the release fan-out")
	}

	// Spawn completes AFTER the release: the fixed path must unwind
	// (cancel the session, fire the funnel) instead of leaking m.calls.
	close(spawnGate)
	select {
	case err := <-spawnDone:
		if err != nil {
			t.Fatalf("HandleCallEvent(initiated) returned %v after a raced hangup; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("initiated handler never returned after spawn completion")
	}

	// The hangup already fired the fan-out exactly once (the funnel's
	// once-guard); the fixed spawn-completion path must NOT fire it again
	// (no double release of the registry slot).
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case o := <-released:
			t.Fatalf("extra release outcome %v after the hangup's fan-out: double release", o)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	// No leaked m.calls entry: the manager must have forgotten the ccid.
	m.mu.Lock()
	_, leakedCalls := m.calls["cc-race-1"]
	_, leakedDone := m.done["cc-race-1"]
	m.mu.Unlock()
	if leakedCalls {
		t.Fatalf("m.calls entry leaked past a hangup-during-spawn: nothing would ever release it")
	}
	if leakedDone {
		t.Fatalf("m.done entry resurrected after the hangup")
	}
}

// ---------------------------------------------------------------------------
// A4 (MED): a child conn that never reads must not wedge Send/SendRaw —
// both return within the write deadline so teardown can proceed.
// ---------------------------------------------------------------------------

func TestTextServerSendBoundsWedgedChild(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "wedged.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})
	// Claim the conn but NEVER read from it: the kernel buffer fills and
	// a deadline-less write would block forever.
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "cc-wedge", Event: EventSay, Text: strings.Repeat("x", 4096)}) == nil
	})
	_ = child

	// Payload big enough to exceed the unix socket buffer so the next
	// write genuinely blocks on the wedged reader.
	big := strings.Repeat("y", 1<<20)
	sendDone := make(chan error, 1)
	go func() {
		sendDone <- srv.Send(Frame{CallID: "cc-wedge", Event: EventSay, Text: big})
	}()
	select {
	case err := <-sendDone:
		if err == nil {
			t.Fatalf("Send to a wedged child unexpectedly succeeded; deadline must bound it")
		}
	case <-time.After(textWriteTimeout + 3*time.Second):
		t.Fatalf("Send still blocked %v after the %v write deadline: teardown would hang forever", textWriteTimeout+3*time.Second, textWriteTimeout)
	}

	// SendRaw (the keys seam; releaseCall's end frame rides the same
	// writeChild path) is bounded too.
	rawDone := make(chan error, 1)
	go func() {
		sendDone <- nil // reuse guard
		rawDone <- srv.SendRaw([]byte(`{"call_id":"cc-wedge","event":"say","text":"` + big + `"}` + "\n"))
	}()
	select {
	case err := <-rawDone:
		if err == nil {
			t.Fatalf("SendRaw to a wedged child unexpectedly succeeded")
		}
	case <-time.After(textWriteTimeout + 3*time.Second):
		t.Fatalf("SendRaw still blocked past the deadline: the end-frame send would block teardown")
	}

	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// A6 (MED): stale in-flight audio of the interrupted stream must not
// re-arm the latch nor replay over the new turn.
// ---------------------------------------------------------------------------

func TestLocalSpliceBargeInStaleBacklogDoesNotReplay(t *testing.T) {
	p := localPairFor(t, "ls-stale-1")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	var cancels atomic.Int32
	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        text,
		CancelDraft: func(string) { cancels.Add(1) },
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-stale-1", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	// Speaker-side counter.
	var read int64
	var bmu sync.Mutex
	before := func() int64 { bmu.Lock(); defer bmu.Unlock(); return read }
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
				read += int64(n)
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

	sp := waitForSplice(t, "ls-stale-1")

	// Phase 1: get playback LIVE (chunks flowing, paced to the client) —
	// the interrupted stream's natural state.
	for range 4 {
		if err := feedChild("ls-stale-1"); err != nil {
			t.Fatalf("feedChild live: %v", err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if before() >= 640 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if before() < 640 {
		t.Fatalf("playback never went live (client got %d bytes)", before())
	}

	// Phase 2: barge-in MID-STREAM. HandleInterrupt bumps the generation;
	// the child's abort takes effect on its side while several chunks of
	// the interrupted stream are still queued (socket buffer + pump
	// channel backlog).
	sp.HandleInterrupt("ls-stale-1")
	clearCount := func() int {
		text.mu.Lock()
		defer text.mu.Unlock()
		return len(text.frames)
	}
	deadline = time.Now().Add(2 * time.Second)
	for clearCount() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// Queue the in-flight tail IMMEDIATELY (inside the residue window):
	// these are the interrupted stream's buffered bytes.
	for range 5 {
		if err := feedChild("ls-stale-1"); err != nil {
			t.Fatalf("feedChild stale: %v", err)
		}
	}
	// Generous window for the pump to consume them; none may pace out.
	time.Sleep(400 * time.Millisecond)
	if got := before(); got >= 2*640 {
		t.Fatalf("stale backlog replayed %d bytes to the client, want < 1 chunk past the pre-interrupt tail (interrupted audio is dead)", got)
	}

	// The latch must STILL be latched (no stale re-arm): a duplicate
	// interrupt must be refused (cancelOne stays true -> no second
	// CancelDraft).
	sp.HandleInterrupt("ls-stale-1")
	time.Sleep(100 * time.Millisecond)
	if c := cancels.Load(); c != 1 {
		t.Fatalf("CancelDraft fired %d times, want 1: a stale chunk re-armed the latch early", c)
	}

	// Phase 3: past the residue window, a FRESH write (the new turn's
	// stream) re-arms and paces out.
	time.Sleep(localBargeResidueQuiet + 100*time.Millisecond)
	// Sample the baseline BEFORE the synchronous feed: under full-suite load
	// the reader can record the fresh chunk before this goroutine resumes,
	// which would hide it inside freshStart and make the poll wait in vain.
	freshStart := before()
	if err := feedChild("ls-stale-1"); err != nil {
		t.Fatalf("feedChild fresh: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if before() >= freshStart+640 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := before(); got < freshStart+640 {
		t.Fatalf("fresh child PCM did not re-arm the latch (client got %d bytes, want >= %d)", got, freshStart+640)
	}
}

// ---------------------------------------------------------------------------
// A7 (MED): Read must not hold c.mu across the blocking pipe read —
// Close (which needs the mutex) completes even while Read is blocked.
// ---------------------------------------------------------------------------

func TestRawMicCaptureCloseCompletesWhileReadBlocked(t *testing.T) {
	// Stand in for the ffmpeg stdout pipe: a reader that never produces.
	pr, pw := io.Pipe()
	c := &RawMicCapture{pipe: pr, cancel: func() {}, stderr: &boundedTail{max: 64}}

	readReturned := make(chan struct{})
	go func() {
		defer close(readReturned)
		buf := make([]byte, 640)
		_, _ = c.Read(buf) // blocks on the empty pipe
	}()
	// Give the reader goroutine time to enter the blocking read.
	time.Sleep(100 * time.Millisecond)

	closeDone := make(chan error, 1)
	go func() { closeDone <- c.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close returned %v while Read was blocked; want clean teardown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Close hung while Read was blocked on the pipe: a stalled ffmpeg would wedge teardown forever")
	}
	_ = pw.Close()

	// The blocked Read must also unprompted-return once Close killed the
	// source (io.Pipe's Write-side close unblocks the reader; the real
	// pipe errors on the dead ffmpeg).
	select {
	case <-readReturned:
	case <-time.After(2 * time.Second):
		t.Fatalf("blocked Read never returned after Close")
	}
}

// A7 companion: a live-mode capture (real exec.Cmd shape) still tears
// down — Close kills the process and Wait reaps while a Read is parked.
// Skipped without ffmpeg (CI/Linux file-mode-only hosts).
func TestRawMicCaptureLiveCloseCompletesWhileReadBlocked(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH; live-mode pin needs it")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("live capture is darwin-only (avfoundation)")
	}
	c, err := NewRawMicCapture("", "")
	if err != nil {
		t.Fatalf("NewRawMicCapture: %v", err)
	}
	go func() {
		buf := make([]byte, 640)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("live Close hung while Read was parked on the ffmpeg pipe")
	}
}

// ---------------------------------------------------------------------------
// A8 (LOW): a holder that chains then goes silent must lose the floor
// within ~2x margin (the deferred watchdog backstop).
// ---------------------------------------------------------------------------

func TestLinkCoordinatorDeferredWatchdogBoundedRelease(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	margin := 300 * time.Millisecond
	c := newTestCoordinator(margin, slog.New(slog.NewTextHandler(&buf, nil)))
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	// Holder A: turn 1 starts + finals (floor held, drain pending),
	// chains sub-turn 2 (turn_start after final -> reDispatched).
	stateA("cc-a-hang", 1, TurnKindStart)
	stateA("cc-a-hang", 1, TurnKindFinal)
	stateA("cc-a-hang", 2, TurnKindStart)

	// Sub-turn 2's text flushes early (its LLM hangs BEFORE the final
	// edge): busy clears, floor + chain marker stay. This is exactly the
	// hung-holder shape — no further edges will EVER arrive.
	c.mu.Lock()
	c.a.busy = false
	c.mu.Unlock()

	// The drained edge fires and defers (chain guard): the watchdog arms.
	c.NoteAudioDrained("cc-a-hang")
	waitFor(t, 2*time.Second, "deferred telemetry", func() bool {
		return strings.Contains(buf.String(), "kind=floor_release_deferred")
	})

	// The holder's LLM hangs forever: NO further edges. The watchdog must
	// release within ~2x margin (defer consume + watchdog fire), flushing
	// the peer park.
	start := time.Now()
	waitFor(t, 5*margin+2*time.Second, "watchdog release flushes B", func() bool {
		return turnsB.count() == 1
	})
	elapsed := time.Since(start)
	if elapsed > 3*margin {
		t.Fatalf("deferred watchdog released after %v, want within ~2x margin (%v)", elapsed, 2*margin)
	}
	c.mu.Lock()
	held := c.a.floorHeld || c.holder == LinkSideA
	c.mu.Unlock()
	if held {
		t.Fatalf("floor still held after the watchdog released: the hold is unbounded again")
	}
	if !strings.Contains(buf.String(), fmt.Sprintf("reason=%s", linkWatchdogReason)) {
		t.Fatalf("telemetry missing the watchdog reason (%s):\n%s", linkWatchdogReason, buf.String())
	}
	assertFloorInvariant(t, c, "after deferred watchdog release")
}

// A8 companion: a LIVE chain (sub-turn finalizes normally) must NOT
// release via the watchdog — the live edges invalidate it. The hold
// behaves exactly as before for live chains.
func TestLinkCoordinatorDeferredWatchdogDoesNotCutLiveChain(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	margin := time.Hour // watchdog must never fire in this test
	c := newTestCoordinator(margin, slog.New(slog.NewTextHandler(&buf, nil)))
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-live", 1, TurnKindStart)
	stateA("cc-a-live", 1, TurnKindFinal)
	stateA("cc-a-live", 2, TurnKindStart) // chains
	// Sub-turn 2's text flushes early: busy clears, floor + chain stay
	// (same shape as T-HR2b's guard-defer pin).
	c.mu.Lock()
	c.a.busy = false
	c.mu.Unlock()
	c.NoteAudioDrained("cc-a-live") // defer + watchdog arm (1h: never fires)

	waitFor(t, 2*time.Second, "deferred telemetry", func() bool {
		return strings.Contains(buf.String(), "kind=floor_release_deferred")
	})
	// Live chain: sub-turn 2 finalizes with no new dispatch; its own
	// margin would arm — but the margin is 1h here, so nothing fires.
	// The point: the watchdog does not cut a live chain early.
	time.Sleep(300 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatalf("watchdog cut a LIVE chain: B dispatched early")
	}
	c.mu.Lock()
	held := c.a.floorHeld && c.holder == LinkSideA
	c.mu.Unlock()
	if !held {
		t.Fatalf("live chain lost the floor to the watchdog")
	}
}

// ---------------------------------------------------------------------------
// A9 (LOW): the spawn stale sweep must never delete the LIVE client pair
// namespace (<ccid>.c.in/.c.out) while still sweeping child-pair stale.
// ---------------------------------------------------------------------------

func TestSweepChildPairStaleSparesLiveClientPair(t *testing.T) {
	dir := t.TempDir()
	liveClientIn := filepath.Join(dir, "cc-live.c.in")
	liveClientOut := filepath.Join(dir, "cc-live.c.out")
	staleChildIn := filepath.Join(dir, "cc-dead.in")
	staleChildOut := filepath.Join(dir, "cc-dead.out")
	for _, p := range []string{liveClientIn, liveClientOut, staleChildIn, staleChildOut} {
		if err := os.WriteFile(p, []byte("junk"), 0o600); err != nil {
			t.Fatalf("create %s: %v", p, err)
		}
	}
	sweepChildPairStale(slog.New(slog.DiscardHandler), dir)

	for _, p := range []string{staleChildIn, staleChildOut} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("stale child-pair file survived the sweep: %s (%v)", p, err)
		}
	}
	for _, p := range []string{liveClientIn, liveClientOut} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("LIVE client-pair file deleted by the sweep: %s (%v) — a concurrent local call just lost its transport", p, err)
		}
	}
}

// bufio-based helpers used above.
var _ = bufio.NewReader
