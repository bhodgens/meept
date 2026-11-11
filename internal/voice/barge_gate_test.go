package voice

// barge_gate_test.go: pins for the SHARED fresh-audio barge-in gate
// (bughunt 2026-11-10 A6, generalized from the local leg to both media
// splices) plus the phone-leg mirrors of the local stale-backlog pin.
// The phone splice's defect shape was identical to the local leg's A6:
// resumeBargeIn fired on ANY pump read, so stale in-flight audio of the
// interrupted utterance re-armed the latch and replayed over the new
// turn (bounded only by QueueLimit and the backpressure grace hangup).

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shared gate unit pins (bargeGate).
// ---------------------------------------------------------------------------

func TestBargeGateBumpInvalidatesSnapshots(t *testing.T) {
	var g bargeGate
	gen0, res0 := g.Snapshot()
	if gen0 != 0 || res0 != 0 {
		t.Fatalf("fresh gate snapshot = (%d, %d), want (0, 0)", gen0, res0)
	}
	// Bump with NO recent touch: generation moves, NO residue stamps
	// (the silent-then-say shape — next PCM is unconditionally fresh).
	g.Bump(0)
	gen1, res1 := g.Snapshot()
	if gen1 != 1 || res1 != 0 {
		t.Fatalf("post-bump snapshot = (%d, %d), want (1, 0)", gen1, res1)
	}
	// Pre-bump snapshots are stale on both verdicts.
	if !g.ReadStale(gen0, res0) {
		t.Fatal("pre-bump snapshot read fresh: stale in-flight audio would re-arm the latch")
	}
	if !g.ConsumeStale(gen0) {
		t.Fatal("pre-bump generation consumed fresh")
	}
	if !g.FreshSnapshot(gen1) {
		t.Fatal("current-generation snapshot not fresh")
	}
	if g.FreshSnapshot(gen0) {
		t.Fatal("stale generation passed FreshSnapshot")
	}
}

func TestBargeGateResidueWindowExpiry(t *testing.T) {
	now := time.Now().UnixNano()
	var g bargeGate
	g.nowNano = func() int64 { return now }
	touch := now
	g.Bump(touch) // stream flowing at cancel: residue stamped
	_, res := g.Snapshot()
	if res == 0 {
		t.Fatal("bump with a recent touch did not stamp the residue deadline")
	}
	// Same-generation reads INSIDE the window are stale (socket-buffer
	// residue of the interrupted stream).
	if !g.ReadStale(1, res) {
		t.Fatal("same-generation read inside the live residue window read fresh")
	}
	if !g.ConsumeStale(1) {
		t.Fatal("same-generation consume inside the live residue window read fresh")
	}
	// Walk the clock PAST the deadline: the interrupted stream is long
	// dead — same-generation chunks are FRESH again.
	now += int64(2 * bargeResidueQuiet)
	if g.ReadStale(1, res) {
		t.Fatal("expired residue window still marks reads stale")
	}
	if g.ConsumeStale(1) {
		t.Fatal("expired residue window still marks consumes stale")
	}
	// ...and a NEW bump with a silent stream clears residue entirely.
	g.Bump(0)
	gen, res := g.Snapshot()
	if res != 0 {
		t.Fatalf("silent bump kept residue %d, want 0", res)
	}
	if !g.FreshSnapshot(gen) {
		t.Fatal("post-bump generation not fresh")
	}
}

func TestBargeGateNoResidueWhenStreamSilent(t *testing.T) {
	var g bargeGate
	// A stale touch (older than the window) is the silent-then-say
	// shape: bump moves the generation but stamps NO residue.
	old := time.Now().Add(-2 * bargeResidueQuiet).UnixNano()
	g.Bump(old)
	gen, res := g.Snapshot()
	if res != 0 {
		t.Fatalf("bump with a stale touch stamped residue %d, want 0 (immediate re-arm posture)", res)
	}
	if !g.FreshSnapshot(gen) {
		t.Fatal("post-bump generation not fresh")
	}
	if g.ReadStale(gen, 0) {
		t.Fatal("first post-cancel PCM marked stale on the no-residue path")
	}
}

func TestBargeGateZeroValueAndNilSafe(t *testing.T) {
	var g bargeGate // zero value ready
	if cur := g.Current(); cur != 0 {
		t.Fatalf("zero gate Current = %d, want 0", cur)
	}
	if g.ReadStale(0, 0) || g.ConsumeStale(0) || !g.FreshSnapshot(0) {
		t.Fatal("zero-value gate misjudged a same-generation chunk")
	}
	var nilGate *bargeGate
	nilGate.Bump(time.Now().UnixNano()) // must not panic
	if _, res := nilGate.Snapshot(); res != 0 {
		t.Fatal("nil gate snapshot returned a residue deadline")
	}
	if nilGate.ReadStale(0, 0) || nilGate.ConsumeStale(0) || !nilGate.FreshSnapshot(0) {
		t.Fatal("nil gate must keep pre-gate semantics (everything fresh)")
	}
}

// ---------------------------------------------------------------------------
// Phone splice, A6 pin 1: mid-stream barge-in with queued backlog — the
// backlog is discarded, and the latch STAYS latched until post-window
// PCM (mirror of the local leg's
// TestLocalSpliceBargeInStaleBacklogDoesNotReplay).
// ---------------------------------------------------------------------------

func TestSpliceBargeInStaleBacklogDoesNotReplay(t *testing.T) {
	ws := &fakeWS{}
	// rawPairFor, not capturePairFor: the splice read pump must be the
	// sole .out reader (a second unix reader steals chunks). The
	// sun_path-safe dir variant (long test names overflow t.TempDir's
	// prefixed socket path on macOS).
	p := localPairFor(t, "cc-stale-1")
	text := &textSink{}

	var cancels atomic.Int32
	cfg := spliceConfig()
	deps := SpliceDeps{
		TelnyxWS:      ws,
		Audio:         p,
		Text:          text,
		CancelDraftFn: func(string) { cancels.Add(1) },
		Clock:         time.Now,
	}
	cancel, done := startSplice(t, "cc-stale-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	mediaCount := func() int { return ws.framesMediaCount() }

	// Phase 1: get playback LIVE (media frames pacing toward Telnyx) —
	// the interrupted stream's natural state, and the precondition for
	// the residue stamp.
	for range 12 {
		_ = feedChild("cc-stale-1")
	}
	deadline := time.Now().Add(3 * time.Second)
	for mediaCount() < 3 && time.Now().Before(deadline) {
		_ = feedChild("cc-stale-1")
		time.Sleep(5 * time.Millisecond)
	}
	if mediaCount() < 3 {
		t.Fatalf("precondition: pacer never wrote media frames (got %d)", mediaCount())
	}

	// Phase 2: barge-in MID-STREAM. bargeIn bumps the gate and ( paced
	// writes just happened) stamps the residue window; the child's abort
	// takes effect on its side while several chunks of the interrupted
	// stream are still queued (socket buffer + pump carry).
	_ = ws.Write([]byte(`{"event":"interrupt","call_id":"cc-stale-1"}` + "\n"))
	clearDeadline := time.Now().Add(2 * time.Second)
	for !ws.framesHas(`"event":"clear"`) && time.Now().Before(clearDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ws.framesHas(`"event":"clear"`) {
		t.Fatal("no clear frame after interrupt")
	}

	// Queue the in-flight tail IMMEDIATELY (inside the residue window):
	// these are the interrupted stream's buffered bytes. NONE of them
	// may pace out, and none may re-arm the latch.
	for range 8 {
		if err := feedChild("cc-stale-1"); err != nil {
			t.Fatalf("feedChild stale: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	// Generous window for the pump to consume them; at most the
	// pre-interrupt tail may already have been paced (nothing NEW).
	beforeResume := mediaCount()
	time.Sleep(400 * time.Millisecond)
	if grew := mediaCount() - beforeResume; grew > 1 {
		t.Fatalf("stale backlog replayed %d media frames toward Telnyx, want <= 1 (interrupted audio is dead)", grew)
	}

	// The latch must STILL be latched (no stale re-arm): a duplicate
	// interrupt must be refused (cancelSent stays true -> no second
	// CancelDraft, no second clear).
	clearsBefore := ws.framesCountOf(`"event":"clear"`)
	_ = ws.Write([]byte(`{"event":"interrupt","call_id":"cc-stale-1"}` + "\n"))
	time.Sleep(150 * time.Millisecond)
	if c := cancels.Load(); c != 1 {
		t.Fatalf("CancelDraftFn fired %d times, want 1: a stale chunk re-armed the latch early", c)
	}
	if got := ws.framesCountOf(`"event":"clear"`); got != clearsBefore {
		t.Fatalf("duplicate interrupt wrote clear frame %d -> %d: the latch re-armed on stale audio", clearsBefore, got)
	}

	// Phase 3: past the residue window, a FRESH stream (the new turn's
	// say) re-arms and paces out again.
	time.Sleep(bargeResidueQuiet + 100*time.Millisecond)
	for range 12 {
		_ = feedChild("cc-stale-1")
		time.Sleep(5 * time.Millisecond)
	}
	deadline = time.Now().Add(3 * time.Second)
	for mediaCount() < beforeResume+3 && time.Now().Before(deadline) {
		_ = feedChild("cc-stale-1")
		time.Sleep(5 * time.Millisecond)
	}
	if mediaCount() < beforeResume+3 {
		t.Fatalf("fresh child PCM did not re-arm the latch (media frames %d, want >= %d)",
			mediaCount(), beforeResume+3)
	}
}

// ---------------------------------------------------------------------------
// Phone splice, A6 pin 2: silent stream at cancel — the documented
// immediate re-arm on the FIRST fresh byte is preserved (no residue
// window is stamped when nothing was flowing).
// ---------------------------------------------------------------------------

func TestSpliceBargeInSilentStreamImmediateRearm(t *testing.T) {
	ws := &fakeWS{}
	p := localPairFor(t, "cc-silent-1")
	text := &textSink{}

	cfg := spliceConfig()
	deps := SpliceDeps{
		TelnyxWS:      ws,
		Audio:         p,
		Text:          text,
		CancelDraftFn: func(string) {},
		Clock:         time.Now,
	}
	cancel, done := startSplice(t, "cc-silent-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	// Barge-in with NO child audio flowing at all: no paced write ever
	// happened, so the gate must stamp NO residue window.
	_ = ws.Write([]byte(`{"event":"interrupt","call_id":"cc-silent-1"}` + "\n"))
	clearDeadline := time.Now().Add(2 * time.Second)
	for !ws.framesHas(`"event":"clear"`) && time.Now().Before(clearDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ws.framesHas(`"event":"clear"`) {
		t.Fatal("no clear frame after interrupt")
	}

	// The VERY FIRST post-cancel byte is fresh and must re-arm
	// immediately — no residue-window wait. The full pre-barge backlog
	// shape cannot exist here (nothing was flowing), so the first fed
	// chunk IS the new turn.
	for range 12 {
		_ = feedChild("cc-silent-1")
		time.Sleep(2 * time.Millisecond)
	}
	deadline := time.Now().Add(3 * time.Second)
	for ws.framesMediaCount() < 3 && time.Now().Before(deadline) {
		_ = feedChild("cc-silent-1")
		time.Sleep(5 * time.Millisecond)
	}
	if ws.framesMediaCount() < 3 {
		t.Fatalf("silent-then-say immediate re-arm lost: pacer stayed stopped after a no-residue barge-in (media frames %d)", ws.framesMediaCount())
	}
}

// Companion (local leg, gate refactor parity): the shared-gate residue
// stamp must still key off the local splice's paced-write touch — a
// live-stream cancel keeps residue semantics identical after the
// refactor onto bargeGate. This rides the existing A6 pin's phases at a
// smaller scale; the full shape is pinned by
// TestLocalSpliceBargeInStaleBacklogDoesNotReplay (kept green below).

// ---------------------------------------------------------------------------
// Local splice regression after the refactor onto the shared gate: the
// existing A6 pin (TestLocalSpliceBargeInStaleBacklogDoesNotReplay,
// bughunt_f2_pins_test.go) must stay green — asserted by running the
// package suite; the small invariant below pins the wiring seam the
// refactor could silently break (HandleInterrupt bumps the SHARED gate,
// not a localsplice-local field).
// ---------------------------------------------------------------------------

func TestLocalSpliceHandleInterruptBumpsSharedGate(t *testing.T) {
	p := localPairFor(t, "ls-gate-1")
	_, daemonSide := net.Pipe()
	defer func() { _ = daemonSide.Close() }()

	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        &textSink{},
		CancelDraft: func(string) {},
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-gate-1", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, "ls-gate-1")
	genBefore, _ := sp.gate.Snapshot()
	if genBefore != 0 {
		t.Fatalf("fresh splice gate generation = %d, want 0", genBefore)
	}
	sp.HandleInterrupt("ls-gate-1")
	genAfter, residue := sp.gate.Snapshot()
	if genAfter != genBefore+1 {
		t.Fatalf("HandleInterrupt did not bump the shared gate (%d -> %d)", genBefore, genAfter)
	}
	// No audio ever flowed (client side never wrote, no paced touch) ->
	// NO residue stamp: the immediate re-arm posture is preserved.
	if residue != 0 {
		t.Fatalf("silent-callback residue stamped %d, want 0", residue)
	}
}

// Compile-time shape guards for the test helpers used above.
var (
	_ = errors.New
	_ = sync.Mutex{}
)
