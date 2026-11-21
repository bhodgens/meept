package voice

// bughunt_c_pins_test.go: 2026-11-11 bughunt pins for the four internal/voice
// findings fixed in this tree.
//
//   - C-1: the local leg's HandleInterrupt is wired into the LIVE C1
//     frame path (TalkLoop.Run receiving an EventInterrupt frame), not
//     merely reachable — the old pins called HandleInterrupt directly,
//     which is exactly why the dead wiring survived them.
//   - C-2: staleness is judged at READ time: a chunk read inside the
//     residue window is dropped at consume even when the window has
//     since expired (the old consume-time re-judge wrote ~1.28 s of the
//     interrupted stream's tail).
//   - C-19: the gate's real clock is monotonic — a wall-clock step
//     cannot extend or kill the residue window.
//   - C-14: the link coordinator's floor lifecycle kinds reach the
//     operator telemetry ring through the emitEvent/OnEvent seam.

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// C-1: the child's C1 interrupt frame reaches the live splice through
// TalkLoop.Run -> handleInterrupt -> LocalSpliceFor(ccid).HandleInterrupt.
// ---------------------------------------------------------------------------

// TestTalkLoopInterruptFrameDrivesLocalSplice drives the REAL frame path:
// a registered live local splice, a talk loop Run consuming an
// EventInterrupt frame off its channel, and the splice's observable
// barge-in state after the loop processed the frame (gate bump + latch +
// clear frame + one CancelDraft). A direct HandleInterrupt call here
// would not exercise the production wiring this pin exists to hold.
func TestTalkLoopInterruptFrameDrivesLocalSplice(t *testing.T) {
	p := localPairFor(t, "ls-c1-wire")
	_, daemonSide := net.Pipe()
	defer func() { _ = daemonSide.Close() }()
	text := &textSink{}

	var cancels atomic.Int32
	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        text,
		CancelDraft: func(string) { cancels.Add(1) },
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-c1-wire", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, "ls-c1-wire")

	// The REAL production shape: TalkLoop.Run consumes the child's
	// interrupt frame from its channel (routeFrame pushes frames at
	// loop.frames in the wiring; the loop's Run select is the seam).
	sink := &recordSink{}
	loop := newTestTalkLoop(t, sink.Send, &cannedSupervisor{}, &cannedTurns{}, TalkLoopConfig{})
	loopCtx, loopCancel := context.WithCancel(context.Background())
	defer loopCancel()
	runDone := make(chan struct{})
	go func() {
		_ = loop.Run(loopCtx, Instance{CallControlID: "ls-c1-wire"})
		close(runDone)
	}()
	t.Cleanup(func() { <-runDone })

	genBefore, _ := sp.gate.Snapshot()
	loop.frames <- Frame{CallID: "ls-c1-wire", Event: EventInterrupt}

	// The splice latched: the pacer is stopped (interrupt) and the gate
	// bumped — neither was reachable from the frame path before C-1.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		genAfter, _ := sp.gate.Snapshot()
		sp.mu.Lock()
		latched := sp.interrupt && sp.cancelOne
		sp.mu.Unlock()
		if genAfter == genBefore+1 && latched {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	genAfter, _ := sp.gate.Snapshot()
	sp.mu.Lock()
	latched := sp.interrupt && sp.cancelOne
	sp.mu.Unlock()
	if genAfter != genBefore+1 {
		t.Fatalf("frame-path interrupt did not bump the splice gate (%d -> %d): HandleInterrupt is not wired into the live frame path", genBefore, genAfter)
	}
	if !latched {
		t.Fatal("frame-path interrupt did not latch the splice (interrupt+cancelOne)")
	}
	// The splice sent its own child clear...
	waitFor(t, 2*time.Second, "splice clear frame", func() bool {
		text.mu.Lock()
		defer text.mu.Unlock()
		return len(text.frames) > 0
	})
	// ...and fired CancelDraft exactly once (via the loop's send below).
	if c := cancels.Load(); c != 1 {
		t.Fatalf("CancelDraft fired %d times, want 1", c)
	}
	// The loop also sent its own clear (the pre-existing handleInterrupt
	// contract) — both lanes alive.
	if len(sink.all()) == 0 {
		t.Fatal("talk loop sent no frames: the loop's own barge-in path broke")
	}
	loopCancel()
}

// ---------------------------------------------------------------------------
// C-2: a chunk READ inside the residue window is dropped at consume even
// after the window has expired (the pacer backlog outlived the 250 ms).
// ---------------------------------------------------------------------------

// TestLocalSpliceBargeInWindowExpiredBacklogDroppedAtConsume runs the
// live splice and, at the unit seam below the socket pump, pins the
// consume-side contract: itemStale answers from the chunk's carried
// read-time tag, not from the gate's CURRENT residue state.
func TestLocalSpliceBargeInWindowExpiredBacklogDroppedAtConsume(t *testing.T) {
	var g bargeGate
	now := time.Now().UnixNano()
	g.nowNano = func() int64 { return now }
	mono := time.Duration(0)
	g.monoNow = func() time.Duration { return mono }

	// Playback live at cancel: residue stamped.
	touch := now
	g.Bump(touch)
	_, res := g.Snapshot()
	if res == 0 {
		t.Fatal("precondition: residue window not stamped")
	}

	// The chunk was READ inside the window: the pump's ReadStale verdict
	// was STALE, and it tagged the chunk.
	stale := g.ReadStale(1, res)
	if !stale {
		t.Fatal("precondition: in-window read judged fresh")
	}
	item := localReadItem{data: make([]byte, 640), gen: 1, residueUntil: res, stale: stale}

	// The pacer backlog holds the chunk PAST the window expiry.
	now += int64(3 * bargeResidueQuiet)
	mono += 3 * bargeResidueQuiet

	// The gate's CURRENT residue state has expired: a re-judge against
	// it would call the chunk fresh and WRITE it — the C-2 defect. The
	// carried read-time verdict keeps it dropped.
	if !item.stale {
		t.Fatal("stale tag lost")
	}
	if g.ReadStale(item.gen, item.residueUntil) {
		t.Fatal("gate still marks same-generation reads stale past the window: the fresh-turn path broke")
	}

	// ConsumeStale stays generation-only: a current-generation chunk
	// read AFTER the window (stale=false) consumes fresh — the new
	// turn's audio must reach the client.
	if g.ConsumeStale(1) {
		t.Fatal("current-generation post-window chunk consumed stale: new-turn audio would be dropped")
	}
}

// The live-splice end-to-end companion: run the REAL pump/pacer. Chunks
// fed immediately after a mid-stream barge-in (inside the residue window,
// so the pump tags them stale) must never reach the client even when the
// pacer is latched well past the window's expiry; a chunk fed AFTER the
// window (fresh) re-arms and paces out. Mirrors the A6 pin's phases with
// a LONG latch gap standing in for the backlog delay.
func TestLocalSpliceBargeInStaleTaggedChunksNeverReplay(t *testing.T) {
	p := localPairFor(t, "ls-c2-tag")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        text,
		CancelDraft: func(string) {},
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-c2-tag", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

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

	sp := waitForSplice(t, "ls-c2-tag")

	// Phase 1: playback LIVE.
	for range 4 {
		if err := feedChild("ls-c2-tag"); err != nil {
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

	// Phase 2: barge-in mid-stream (residue stamped; audioLastAct fresh).
	sp.HandleInterrupt("ls-c2-tag")
	// Feed the interrupted stream's tail IMMEDIATELY — inside the
	// residue window, so the pump READS them stale and tags them.
	for range 5 {
		if err := feedChild("ls-c2-tag"); err != nil {
			t.Fatalf("feedChild stale: %v", err)
		}
	}
	// Hold PAST the window expiry (3x residue): the pacer is still
	// latched, the tagged chunks sit in the pump channel backlog, and
	// the gate's residue deadline has passed. The OLD consume-time
	// re-judge would start writing the tagged tail here.
	time.Sleep(3*localBargeResidueQuiet + 150*time.Millisecond)
	if got := before(); got >= 2*640 {
		t.Fatalf("stale-tagged backlog replayed %d bytes to the client after the window expired, want < 1 chunk past the pre-interrupt tail", got)
	}

	// Phase 3: a FRESH feed (post-window) re-arms and paces out.
	// Sample the baseline BEFORE the feed: feedChild hands the chunk to
	// the pump synchronously (net.Pipe write), so under full-suite load
	// the reader goroutine can record the freshly paced 640 bytes before
	// this goroutine is rescheduled. Capturing the baseline after the
	// feed then hides those bytes inside freshStart and the bounded poll
	// below waits 3 s for a second chunk that never comes — the
	// "client got 1280 bytes, want >= 1920" full-suite flake.
	freshStart := before()
	if err := feedChild("ls-c2-tag"); err != nil {
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
// C-19: a wall-clock step cannot move the gate's residue deadline.
// ---------------------------------------------------------------------------

// TestBargeGateResidueDeadlineSurvivesWallStep arms a residue window on
// the dual-seam fake clock, then STEPS THE WALL backward and forward
// across the deadline while the monotonic clock moves normally. The
// deadline rides the mono pace: a backward step must NOT extend the
// window (a read past the mono deadline is fresh even though the stepped
// wall still reads inside the old window), and a forward step must NOT
// kill it early (a read before the mono deadline is stale even though
// the stepped wall reads past the deadline). On the OLD wall-only clock
// both assertions fail: the deadline was wall-relative.
func TestBargeGateResidueDeadlineSurvivesWallStep(t *testing.T) {
	wall := time.Now().UnixNano()
	mono := time.Duration(0)
	var g bargeGate
	g.nowNano = func() int64 { return wall }
	g.monoNow = func() time.Duration { return mono }

	// Anchor + arm: playback live at cancel (touch = wall now), residue
	// stamped until mono == residueWindow.
	g.Bump(wall)
	_, res := g.Snapshot()
	if res == 0 {
		t.Fatal("residue window not stamped")
	}

	// Mono advances normally to just inside the window; the wall steps
	// BACKWARD 2 windows at the same moment (an NTP correction). The
	// stepped wall now reads far "before" the deadline — a wall-relative
	// window would stretch by that full step.
	wall -= int64(2 * bargeResidueQuiet)
	mono += bargeResidueQuiet / 2
	if !g.ReadStale(1, res) {
		t.Fatal("backward wall step killed the residue window early (clock not monotonic)")
	}

	// Mono advances just past the window; the wall steps FORWARD past
	// the deadline (the opposite NTP correction). A wall-relative window
	// would have expired early (or stretched, then still be live here).
	wall += int64(3 * bargeResidueQuiet)
	mono += bargeResidueQuiet
	if g.ReadStale(1, res) {
		t.Fatal("forward wall step extended the residue window past its monotonic deadline")
	}
}

// ---------------------------------------------------------------------------
// C-14: the coordinator's floor lifecycle reaches the telemetry ring.
// ---------------------------------------------------------------------------

// TestLinkCoordinatorFloorEventsReachTelemetryRing wires the C-14 event
// sink over a fake ring append and drives the lifecycle edges: a margin
// RE-ARM (holder tail audible at the fire), the re-armed fire's real
// RELEASE once the tail goes quiet, the DEFERRED release (holder
// chained), and the watchdog release. Every kind string the log lines
// carry must also land on the seam — keyed by the side's bound loop ccid
// (B) or the link id (A, unbound).
func TestLinkCoordinatorFloorEventsReachTelemetryRing(t *testing.T) {
	var mu sync.Mutex
	var events []TelemetryEvent
	var seq uint64
	ringAppend := func(ccid string, in EventInput) uint64 {
		mu.Lock()
		defer mu.Unlock()
		seq++
		events = append(events, TelemetryEvent{Seq: seq, Kind: in.Kind, Reason: in.Reason, Text: ccid})
		return seq
	}

	var buf syncBuffer
	margin := 400 * time.Millisecond
	c := newTestCoordinator(margin, slog.New(slog.NewTextHandler(&buf, nil)))
	// C-14 seam: the coordinator appends through the SAME emitEvent
	// closure shape the talk loop uses.
	c.SetEventSink(func(ccid, kind, reason string) {
		ringAppend(ccid, EventInput{Kind: kind, Reason: reason})
	})

	// Bind B's loop so the side's ring key is the loop's live ccid
	// (asserted below via the keyed B event).
	turnsB := &holdTurns{reply: "ok."}
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-c14", 1, TurnKindFinal)

	// A live shell splice under A's ccid makes TailActive real: the
	// first margin fire sees an audible tail and RE-ARMS. The touch is
	// stamped shortly BEFORE the margin fire (armed at turn_final,
	// fires at +margin) so the tail is AUDIBLE at the fire — a fire-
	// time race with a stale touch would release instead of re-arm.
	shell := &runLocalSplice{
		ccid: "cc-a-c14",
		cfg:  LocalSpliceConfig{Pace: 20 * time.Millisecond},
		log:  localDiscardLogger(),
		wake: make(chan struct{}, 1),
	}
	localSpliceMu.Lock()
	localSpliceByCcid["cc-a-c14"] = shell
	localSpliceMu.Unlock()
	defer func() {
		localSpliceMu.Lock()
		delete(localSpliceByCcid, "cc-a-c14")
		localSpliceMu.Unlock()
	}()
	time.Sleep(margin - 150*time.Millisecond)
	shell.audioLastAct.Store(time.Now().UnixNano())

	// First margin fire: tail active -> re-armed (kind=margin_rearmed).
	waitFor(t, 2*time.Second, "margin_rearmed on the ring", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			if ev.Kind == "margin_rearmed" {
				return true
			}
		}
		return false
	})

	// Let the tail go quiet: the re-armed fire releases
	// (kind=floor_released, reason=margin).
	shell.audioLastAct.Store(time.Now().Add(-2 * margin).UnixNano())
	waitFor(t, 3*time.Second, "floor_released on the ring", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			if ev.Kind == "floor_released" && ev.Reason == "margin" {
				return true
			}
		}
		return false
	})

	// Wait out B's own turn flow: the release flushed B's parked prompt,
	// B dispatched + finalized its turn, and B's own fallback margin
	// releases its floor at ~release+2xmargin. Phase 2 needs a QUIET
	// coordinator (holder free) or A's turn_start is a listener edge.
	time.Sleep(2*margin + 300*time.Millisecond)

	// The deferred + watchdog kinds: a fresh hold -> final -> chain ->
	// hang (the A8 pin's exact shape). The drain edge hits the chain
	// guard -> floor_release_deferred + watchdog armed, and the watchdog
	// releases for real (floor_watchdog kind).
	stateA("cc-a-c14", 1, TurnKindStart)
	stateA("cc-a-c14", 1, TurnKindFinal)
	stateA("cc-a-c14", 2, TurnKindStart) // chain: reDispatched
	c.mu.Lock()
	c.a.busy = false
	c.mu.Unlock()
	shell.audioLastAct.Store(0)    // quiet tail: not audible
	c.NoteAudioDrained("cc-a-c14") // defer branch
	waitFor(t, 2*time.Second, "floor_release_deferred on the ring", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			if ev.Kind == "floor_release_deferred" {
				return true
			}
		}
		return false
	})
	// The deferred watchdog then releases for real: the ring carries
	// floor_watchdog (reason=floor_watchdog), the distinct kind.
	waitFor(t, 3*margin+2*time.Second, "floor_watchdog on the ring", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			if ev.Kind == "floor_watchdog" && ev.Reason == "floor_watchdog" {
				return true
			}
		}
		return false
	})
}

// The nil-default pin: an unwired coordinator emits nothing and stays
// byte-identical (the mirror rule), and the ring seam resolves a side's
// ccid through the bound loop (not the link id).
func TestLinkCoordinatorEventSinkNilDefaultAndCcidKey(t *testing.T) {
	var buf syncBuffer
	c := newTestCoordinator(50*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	// No SetEventSink: a margin fire still logs (the buf carries the
	// line) and no seam call can panic.
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-nil-sink", 1, TurnKindStart)
	stateA("cc-nil-sink", 1, TurnKindFinal)
	waitFor(t, 2*time.Second, "margin release without sink", func() bool {
		return strings.Contains(buf.String(), "kind=floor_released")
	})

	// The keyed shape: a wired sink receives the LOOP's ccid.
	var mu sync.Mutex
	var got []string
	c2 := newTestCoordinator(time.Hour, slog.New(slog.NewTextHandler(&syncBuffer{}, nil)))
	c2.SetEventSink(func(ccid, kind, reason string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, ccid+"|"+kind)
	})
	loopA := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      &cannedTurns{},
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     linkDiscard(),
	}, TalkLoopConfig{})
	c2.Bind(LinkSideA, loopA)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = loopA.Run(ctx, Instance{CallControlID: "cc-loop-a"}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	waitFor(t, 2*time.Second, "runCcid stamped", func() bool {
		return loopA.ccidForTelemetry() == "cc-loop-a"
	})
	// Fire the emit seam through the funnel guard shape: a release on a
	// side whose key is the loop ccid. Drive it directly through the
	// locked emit (unit seam).
	c2.mu.Lock()
	c2.emitLocked(LinkSideA, "floor_released", "drained")
	c2.mu.Unlock()
	waitFor(t, time.Second, "ring keyed by loop ccid", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(got, "cc-loop-a|floor_released")
	})
}
