package voicechild

// Tests for the C1 open frame (local-leg plan CL1): the daemon opens a
// call, the child builds the pipeline and answers with setup. This is the
// production trigger the protocol was missing - without it no pipeline is
// ever created, so no setup frame ever arrives, so the daemon never arms a
// talk loop.

import (
	"context"
	"strings"
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
	case <-time.After(5 * time.Second):
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

// TestOpenEmptyCallIDErrors guards the frame contract: an open with no
// call_id can never key a pipeline.
func TestOpenEmptyCallIDErrors(t *testing.T) {
	child := New(ChildConfig{AudioDir: t.TempDir()})
	err := child.handleLine(context.Background(), `{"call_id":"","event":"open"}`)
	if err == nil || !strings.Contains(err.Error(), "empty call_id") {
		t.Fatalf("open with empty call_id: err = %v, want mention of empty call_id", err)
	}
}
