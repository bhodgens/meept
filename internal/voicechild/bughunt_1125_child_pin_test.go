package voicechild

// bughunt_1125_child_pin_test.go — A4-1 (bughunt 2026-11-25): the end
// frame's pipeline.close() (analyzer-init barrier + the 5 s
// transcript-consumer bound) must run OFF the single C1 dispatch loop.
// The synchronous shape froze every OTHER live call's say/clear/end
// frames in lineCh behind a hung teardown — the exact class 82400e5b
// removed for analyzer INIT. The fix (internal/voicechild/child.go
// closePipelineAsync) moves the call sites off serve()'s loop;
// dropPipeline's map-delete is the double-close once-guard.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// TestEndFrameCloseDoesNotBlockDispatchLoop: a slow call whose close()
// parks on a hung analyzer factory (the ONNX-dlopen shape) must NOT delay
// another live call's say frame. Old shape: no audio on the healthy call
// for the whole hang; new shape: audio flows while the teardown parks.
func TestEndFrameCloseDoesNotBlockDispatchLoop(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-slow")
	d.listenPair("cc-keep")

	child := newTestChild(t, d, "tok-hol")
	release := make(chan struct{})
	var releaseOnce sync.Once
	unhang := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unhang)
	// The hang shape: a VAD factory parked inside construction (a hung
	// dlopen). close()'s analyzerInit.Wait parks behind it until release.
	child.SetVADFactory(func(int) (VADAnalyzer, error) {
		<-release
		return nil, errors.New("late release")
	})
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()
	// The slow call opens LOCAL (open.go arms initAnalyzersAsync only for
	// local frames with a factory); the healthy call is a phone open.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc-slow", Event: voiceframe.EventOpen, Local: true,
		Codec: "L16", SampleRate: 16000,
	})
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc-keep", Event: voiceframe.EventOpen,
		Codec: "L16", SampleRate: 16000,
	})
	inS, outS := d.acceptAll("cc-slow")
	inK, outK := d.acceptAll("cc-keep")
	defer func() { _ = inS.Close() }()
	defer func() { _ = outS.Close() }()
	defer func() { _ = inK.Close() }()
	defer func() { _ = outK.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })
	// Let the parked factory goroutine enter its build (the Wait must see
	// the Add — Add is synchronous in handleOpen, the park is the hang).
	time.Sleep(100 * time.Millisecond)

	// Hang up the slow call: close() parks on the analyzer-init barrier.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc-slow", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	// The healthy call must keep moving WHILE the teardown is parked:
	// say -> stub TTS -> 640 bytes on cc-keep's .out within 400 ms. The
	// synchronous shape (pre-fix) sat the frame in lineCh for the whole
	// hang, so this read only completes once `release` fires — the
	// deadline below fails loudly instead.
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 640)
		_ = outK.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		n, err := outK.Read(buf)
		if err != nil {
			readErr <- err
			return
		}
		if n != 640 {
			readErr <- errors.New("partial first chunk")
			return
		}
		readErr <- nil
	}()
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc-keep", Event: voiceframe.EventSay, Text: "keep talking", Last: lastPtr(true)})

	select {
	case err := <-readErr:
		if err != nil {
			t.Fatalf("healthy call's audio blocked behind a hung end-frame teardown (A4-1 regression): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("say frame never dispatched while the end-frame teardown parked (dispatch loop head-of-line block, A4-1)")
	}

	// Release the hang: the parked close finishes, and Run still returns
	// cleanly on socket close (the exit sweep must not double-close the
	// already-dropped slow pipeline — dropPipeline is the once-guard).
	unhang()
	time.Sleep(100 * time.Millisecond)
	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error on close: %v", err)
		}
	case <-time.After(testPollDeadline + 2*time.Second):
		t.Fatal("Run did not return after socket close (exit teardown unbounded)")
	}
}
