package voice

// barge_gate_touch_pins_test.go: per-leg pins that the OUTBOUND-TOUCH
// feed into bargeGate.Bump is actually wired (regression-review follow-up
// to the A6 generalization, 2026-11-11).
//
// The gate stamps its post-cancel residue window ONLY when Bump receives
// a touch timestamp that is recent at cancel time. Each leg feeds its own
// timestamp (phone: lastPacedWriteNano via lastPacedTouchNano; local:
// audioLastAct). If a future leg (or a refactor) forgets to keep that
// feed live, residue stamping silently dies: post-cancel reads of the
// interrupted stream's socket-buffer tail then re-arm the latch and
// replay over the new turn — the exact defect A6 fixed. These pins fail
// loudly on that break: with playback LIVE at cancel, the FIRST
// post-cancel read (current generation, inside the residue window) must
// be stale. A broken touch feed leaves residueUntilNano == 0, that read
// counts as fresh, and the assertion fires.

import (
	"net"
	"sync"
	"testing"
	"time"
)

// TestSpliceBargeInLiveStreamStampsResidue (PHONE): playback live at
// cancel -> the gate's residue deadline is stamped, so a chunk fed
// IMMEDIATELY after the interrupt (current generation, inside the
// window) is verdict-stale and must NOT re-arm the latch. The local pin
// (TestLocalSpliceHandleInterruptBumpsSharedGate) covers the local leg's
// silent shape; this is the phone leg's live-stream counterpart.
func TestSpliceBargeInLiveStreamStampsResidue(t *testing.T) {
	ws := &fakeWS{}
	p := localPairFor(t, "cc-touch-1")
	text := &textSink{}

	cfg := spliceConfig()
	deps := SpliceDeps{
		TelnyxWS:      ws,
		Audio:         p,
		Text:          text,
		CancelDraftFn: func(string) {},
		Clock:         time.Now,
	}
	cancel, done := startSplice(t, "cc-touch-1", cfg, deps)
	defer stopSplice(t, cancel, done)

	// Precondition: playback LIVE — paced Telnyx writes inside the
	// residue window, which is what makes the touch recent.
	for range 12 {
		_ = feedChild("cc-touch-1")
	}
	deadline := time.Now().Add(3 * time.Second)
	for ws.framesMediaCount() < 3 && time.Now().Before(deadline) {
		_ = feedChild("cc-touch-1")
		time.Sleep(5 * time.Millisecond)
	}
	if ws.framesMediaCount() < 3 {
		t.Fatalf("precondition: pacer never wrote media frames (got %d)", ws.framesMediaCount())
	}

	// Barge-in mid-stream: bargeIn bumps the gate and, because the
	// pacer wrote moments ago, stamps the residue window.
	_ = ws.Write([]byte(`{"event":"interrupt","call_id":"cc-touch-1"}` + "\n"))
	clearDeadline := time.Now().Add(2 * time.Second)
	for !ws.framesHas(`"event":"clear"`) && time.Now().Before(clearDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !ws.framesHas(`"event":"clear"`) {
		t.Fatal("no clear frame after interrupt")
	}

	// The TOUCH-FEED assertion: feed one chunk IMMEDIATELY (inside the
	// 250ms residue window). It carries the CURRENT generation, so it is
	// fresh UNLESS the residue window was stamped. The stale-backlog pin
	// proves a stamped window holds the latch; here the mirror asserts
	// stamping actually happened — a broken touch feed (residue 0) would
	// re-arm on this very chunk, so a SECOND clear frame appears.
	for range 3 {
		_ = feedChild("cc-touch-1")
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // well inside bargeResidueQuiet
	clearsBefore := ws.framesCountOf(`"event":"clear"`)
	_ = ws.Write([]byte(`{"event":"interrupt","call_id":"cc-touch-1"}` + "\n"))
	time.Sleep(150 * time.Millisecond)
	if got := ws.framesCountOf(`"event":"clear"`); got != clearsBefore {
		t.Fatalf("clear frames %d -> %d: the latch re-armed on a post-cancel in-window chunk — residue was NOT stamped (touch feed into bargeGate.Bump is broken)",
			clearsBefore, got)
	}
}

// TestLocalSpliceBargeInLiveStreamStampsResidue (LOCAL): the local leg's
// mirror — playback live at cancel (audioLastAct fresh), a post-cancel
// in-window chunk must stay stale (latch NOT re-armed).
func TestLocalSpliceBargeInLiveStreamStampsResidue(t *testing.T) {
	p := localPairFor(t, "ls-touch-1")
	micSide, daemonSide := net.Pipe()
	defer func() { _ = micSide.Close(); _ = daemonSide.Close() }()
	text := &textSink{}

	var cancels int
	deps := LocalSpliceDeps{
		Audio:       p,
		Client:      daemonSide,
		Text:        text,
		CancelDraft: func(string) { cancels++ },
		Logger:      localDiscardLogger(),
	}
	cancel, done := startLocalSplice(t, "ls-touch-1", LocalSpliceConfig{}, deps)
	defer stopLocalSplice(t, cancel, done)

	sp := waitForSplice(t, "ls-touch-1")

	// Speaker-side counter: bytes paced to the client (the local pin's
	// live-playback evidence — the pacer's write stamps audioLastAct).
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
			n, err := micSide.Read(buf)
			if err != nil {
				return
			}
			bmu.Lock()
			read += int64(n)
			bmu.Unlock()
		}
	}()
	defer close(stopRead)

	// Precondition: playback LIVE (chunks flowing, paced to the client
	// — the pacer's writes stamp audioLastAct inside the residue
	// window).
	for range 6 {
		if err := feedChild("ls-touch-1"); err != nil {
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
		t.Fatalf("precondition: playback never went live (client got %d bytes)", before())
	}

	// Barge-in mid-stream via the manager seam.
	sp.HandleInterrupt("ls-touch-1")
	time.Sleep(50 * time.Millisecond)

	// Touch-feed assertion: an in-window post-cancel chunk is stale ONLY
	// if the residue window was stamped from the live audioLastAct. If
	// the feed broke (residue 0), HandleInterrupt again would find the
	// latch re-armed and fire a second CancelDraft.
	if _, residue := sp.gate.Snapshot(); residue == 0 {
		t.Fatal("residue window NOT stamped with playback live: audioLastAct feed into bargeGate.Bump is broken")
	}
	_ = cancels
}
