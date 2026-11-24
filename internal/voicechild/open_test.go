package voicechild

// Tests for the C1 open frame (local-leg plan CL1): the daemon opens a
// call, the child builds the pipeline and answers with setup. This is the
// production trigger the protocol was missing - without it no pipeline is
// ever created, so no setup frame ever arrives, so the daemon never arms a
// talk loop.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// drainFrames pops every frame currently queued by the fake daemon (a
// quiet-wire assertion: the child must have sent nothing).
func drainFrames(d *fakeDaemon) []voiceframe.Frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.queue
	d.queue = nil
	return out
}

// TestOpenRegistersPipelineAndAnswersSetup drives the full answer chain:
// open frame -> pipeline registered (audio pair dialed) -> a setup line on
// the wire carrying the identity the daemon needs to arm its talk loop.
func TestOpenRegistersPipelineAndAnswersSetup(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-open")

	child := newTestChild(t, d, "tok-open")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID:     "cc-open",
		Event:      voiceframe.EventOpen,
		From:       "+155****0001",
		To:         "+155****0002",
		Direction:  "inbound",
		Codec:      "L16",
		SampleRate: 16000,
	})
	// newPipeline dials both audio sockets; accept both or the dial blocks.
	in, out := d.acceptAll("cc-open")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()

	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	if p, ok := child.getPipeline("cc-open"); !ok || p == nil {
		t.Fatalf("pipeline for cc-open not registered after open")
	}

	line := d.waitRawLine(t)
	want := `{"call_id":"cc-open","event":"setup","from":"+155****0001","to":"+155****0002","direction":"inbound","codec":"L16","sample_rate":16000}`
	if line != want {
		t.Fatalf("setup answer line wrong:\n got %s\nwant %s", line, want)
	}

	// Teardown still works (end frame drops the pipeline the open created).
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc-open", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 0 })

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error on close: %v", err)
		}
	case <-time.After(testPollDeadline):
		t.Fatalf("Run did not return after socket close")
	}
}

// TestOpenIdempotentOnLiveCall: a second open for a live ccid must not
// build a second pipeline or answer a second setup (the daemon's duplicate
// setup guard exists, but the child must also stay quiet - bughunt
// 2026-11-07 voice F-6 is the daemon-side mirror of this rule).
func TestOpenIdempotentOnLiveCall(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-dup")

	child := newTestChild(t, d, "tok-dup")
	go func() { _ = child.Run(context.Background()) }()
	childConn := d.serveText()

	open := voiceframe.Frame{
		CallID: "cc-dup", Event: voiceframe.EventOpen,
		Direction: "inbound", Codec: "L16", SampleRate: 16000,
	}
	d.sendLine(childConn, open)
	in, out := d.acceptAll("cc-dup")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()

	// First open: pipeline + one setup answer.
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	d.waitFrame(t)

	// Second open: no new pipeline, no second answer.
	d.sendLine(childConn, open)
	if n := child.pipelineCount(); n != 1 {
		t.Fatalf("pipeline count after duplicate open = %d, want 1", n)
	}
	if got := drainFrames(d); len(got) != 0 {
		t.Fatalf("duplicate open emitted %d frame(s), want 0: %v", len(got), got)
	}
}

// TestOpenCarriesProfileOntoPipeline: leg marker and RMS threshold from
// the open frame land on the pipeline (local leg's RMS barge-in reads
// them; providers still come later over the keys frame).
func TestOpenCarriesProfileOntoPipeline(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-prof")

	child := newTestChild(t, d, "tok-prof")
	go func() { _ = child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc-prof", Event: voiceframe.EventOpen,
		Direction: "local", Codec: "L16", SampleRate: 16000,
		Local: true, BargeInRms: 3000,
	})
	in, out := d.acceptAll("cc-prof")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()

	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline("cc-prof")
	if !ok || p == nil {
		t.Fatalf("pipeline for cc-prof not registered")
	}
	if !p.local || p.bargeInRms != 3000 {
		t.Fatalf("profile not stored: local=%v bargeInRms=%d, want true/3000", p.local, p.bargeInRms)
	}
}

// TestSetupWithoutOpenStillWorks: an old daemon sends no open frame. The
// setup path must still build a pipeline (byte-identical legacy behavior)
// and must NOT answer setup to itself.
func TestSetupWithoutOpenStillWorks(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-legacy")

	child := newTestChild(t, d, "tok-legacy")
	go func() { _ = child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc-legacy", Event: voiceframe.EventSetup,
		Direction: "inbound", Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cc-legacy")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()

	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	if got := drainFrames(d); len(got) != 0 {
		t.Fatalf("setup path answered %d frame(s) to itself, want 0: %v", len(got), got)
	}
}

// TestOpenEmptyCallIDFailsCallNotChild guards the frame contract: an open
// with no call_id can never key a pipeline. handleOpen still refuses it
// with the named error; handleLine isolates the failure per-frame (audit
// fix 4): the error is logged, no pipeline exists, and the call to
// handleLine must NOT panic or stop the reader loop.
func TestOpenEmptyCallIDFailsCallNotChild(t *testing.T) {
	logger := newCaptureLogger()
	child := New(ChildConfig{AudioDir: t.TempDir(), Logger: logger.logger()})
	child.handleLine(context.Background(), `{"call_id":"","event":"open"}`)
	if got := child.pipelineCount(); got != 0 {
		t.Fatalf("pipeline count after empty-call_id open = %d, want 0", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if logger.contains("empty call_id") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("handleLine did not log the empty call_id failure; log so far:\n%s", logger.everything())
}

// slowVAD blocks inside the factory until released, then returns a fake
// analyzer. It records whether Close ran, for the teardown-race pin below.
type slowVAD struct {
	enter   chan struct{}
	release chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (s *slowVAD) AnalyzeAudio([]byte) bool { return false }
func (s *slowVAD) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}
func (s *slowVAD) factory(int) (VADAnalyzer, error) {
	close(s.enter)
	<-s.release
	return s, nil
}

// TestOpenSetupNotBlockedBySlowAnalyzer pins the head-of-line fix: with a
// local VAD factory that blocks for seconds (dlopen/model load), the child
// must still answer 'setup' immediately. Before the fix, handleOpen invoked
// the factory synchronously on the single C1 dispatch loop, delaying setup
// and every other call's frames behind the model load.
func TestOpenSetupNotBlockedBySlowAnalyzer(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc-slow")
	child := newTestChild(t, d, "tok-slow")
	sv := &slowVAD{enter: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	child.SetVADFactory(sv.factory)

	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc-slow", Event: voiceframe.EventOpen, Local: true,
		Codec: "L16", SampleRate: 16000, BargeInRms: 3000,
	})
	in, out := d.acceptAll("cc-slow")
	defer func() { _ = in.Close(); _ = out.Close() }()

	// The factory must actually be entered (async init started).
	select {
	case <-sv.enter:
	case <-time.After(testPollDeadline):
		t.Fatal("slow VAD factory never invoked")
	}

	// setup must arrive WITHOUT releasing the factory.
	line := d.waitRawLine(t)
	if !strings.Contains(line, `"event":"setup"`) {
		t.Fatalf("expected setup while analyzer still loading, got %s", line)
	}

	// Fast teardown while the factory is blocked: close() must wait on the
	// barrier, so release the factory and prove the session is closed, not
	// leaked or installed after teardown.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc-slow", Event: voiceframe.EventEnd, Reason: "hangup"})
	close(sv.release)
	select {
	case <-sv.closed:
	case <-time.After(testPollDeadline):
		t.Fatal("slow VAD session not closed after teardown (leak)")
	}
	_ = childConn.Close()
	<-runDone
}
