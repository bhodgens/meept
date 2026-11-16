package daemon

// C1 played-edge routing (VoiceWiring.routeFrame): the child's end-of-turn
// audio marker must reach the live local splice's drain watch (MarkFinalPlayed),
// which then fires the AudioDrained edge once the paced tail quiets. This
// pins the wiring half of the seam that releases the link floor only on the
// TRUE end of turn (link-2fdc3f5fd8d81805: the blind turn_final arm fired
// mid-reply on inter-sentence synth gaps; 10.94 s both-audible overlap).

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// nilTextSender satisfies voice.TextSender for splice tests that never barge.
type nilTextSender struct{}

func (nilTextSender) Send([]byte) error { return nil }

func TestRouteFramePlayedArmsLiveSplice(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-played-route"

	// Real splice over a real audio pair (same construction LocalLeg.Open
	// performs): dir from the short MEEPT_HOME the wiring fixture pins.
	dir := voice.SocketDir()
	pair, err := voice.NewAudioPair(dir, ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	t.Cleanup(func() { _ = pair.Close() })

	// Child side dials both sockets (the child's C2 handshake shape).
	inConn, err := net.Dial("unix", filepath.Join(dir, ccid+".in"))
	if err != nil {
		t.Fatalf("dial .in: %v", err)
	}
	defer func() { _ = inConn.Close() }()
	outConn, err := net.Dial("unix", filepath.Join(dir, ccid+".out"))
	if err != nil {
		t.Fatalf("dial .out: %v", err)
	}
	defer func() { _ = outConn.Close() }()
	// Pair accepts conns asynchronously; give the accept goroutines a beat
	// (same settle the voice package's own dial helper waits for).
	time.Sleep(100 * time.Millisecond)

	micSide, spliceSide := net.Pipe()
	t.Cleanup(func() { _ = micSide.Close(); _ = spliceSide.Close() })

	var fired atomic.Bool
	drain := func(string) { fired.Store(true) }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- voice.RunLocalSplice(ctx, ccid, voice.LocalSpliceConfig{}, voice.LocalSpliceDeps{
			Audio:        pair,
			Client:       spliceSide,
			Text:         nilTextSender{},
			CancelDraft:  func(string) {},
			AudioDrained: drain,
			Logger:       testLogger(t),
		})
	}()

	// Join: the client's first mic byte + continuous draining so the
	// pacer never blocks on net.Pipe.
	go func() {
		_, _ = micSide.Write([]byte{1, 2, 3, 4})
		buf := make([]byte, 640)
		for {
			_ = micSide.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.ReadFull(micSide, buf); err != nil {
				return
			}
		}
	}()

	// Wait for registration, then feed this turn's audio (child side).
	deadline := time.Now().Add(2 * time.Second)
	for voice.LocalSpliceFor(ccid) == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if voice.LocalSpliceFor(ccid) == nil {
		t.Fatal("splice never registered")
	}
	for range 4 {
		if _, err := outConn.Write(make([]byte, 640)); err != nil {
			t.Fatalf("child feed: %v", err)
		}
	}

	// BEFORE the played edge: no fire may land (no arm exists — the
	// router is the only armer for this seam shape).
	time.Sleep(200 * time.Millisecond)
	if fired.Load() {
		t.Fatal("drain fired before the played edge (unarmed watch?)")
	}

	// Route the C1 played frame exactly as the text server handler does.
	last := true
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPlayed, Last: &last})

	// The played-armed watch fires at pending==0 + the final quiet window
	// (500 ms default + pacing of the 4 fed chunks).
	waitEnd := time.Now().Add(4 * time.Second)
	for !fired.Load() && time.Now().Before(waitEnd) {
		time.Sleep(10 * time.Millisecond)
	}
	if !fired.Load() {
		t.Fatal("played edge routed through routeFrame never fired the drain watch")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice did not exit after cancel")
	}
}

// A played frame for a ccid WITHOUT a live local splice (phone leg, torn-
// down call, plain local without the seam) must be a safe no-op — Debug-
// logged, never routed into any talk loop, never a panic through the nil
// LocalSpliceFor receiver.
func TestRouteFramePlayedWithoutSpliceIsNoop(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-played-dead"
	last := true
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPlayed, Last: &last})
	// And with a talk loop armed: the frame must NOT enter the loop's
	// queue (the router consumes it), so a loop fed nothing else stays
	// silently alive.
	inst := acquireFor(w, ccid)
	trackCall(t, w, ccid)
	turns := newBlockingTurnRunner()
	armLoop(w, ccid, inst, turns)
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPlayed, Last: &last})
	time.Sleep(150 * time.Millisecond)
	select {
	case <-turns.started:
		t.Fatal("played frame reached the talk loop and started a turn")
	default:
	}
	w.releaseCall(ccid, voice.OutcomeHangup)
}
