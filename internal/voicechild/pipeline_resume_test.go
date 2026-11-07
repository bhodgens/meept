package voicechild

// bughunt 2026-11-07 voice F-1 (HIGH) pin, child twin: `dropQueued` used to
// be a once-per-call latch (never reset anywhere), so every say after the
// first clear was silently dropped at enqueueSay — the caller never heard
// another word for the rest of the call. A NEW say is a new utterance: the
// drop applies to the interrupted stream only (playGen retirement aborts
// it), so the pipeline must play the next say.

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

func TestSayAfterClearPlaysAgain(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc7")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc7", Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cc7")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline("cc7")

	// Utterance 1: gated TTS, one chunk out, then clear mid-stream.
	gate1 := newGatedTTS()
	p.tts = gate1
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc7", Event: voiceframe.EventSay, Text: "first", Last: lastPtr(true)})
	buf := make([]byte, 640)
	_ = out.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := io.ReadFull(out, buf); n != 640 || err != nil {
		t.Fatalf("utterance 1 first chunk: n=%d err=%v", n, err)
	}
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc7", Event: voiceframe.EventClear})
	waitFor(t, p.isDropped)
	close(gate1.releaseSecond) // the stale stream's next read: gen mismatch -> abort

	// Utterance 2 (THE F-1 case): a fresh say after clear must play, not
	// be silently dropped.
	gate2 := newGatedTTS()
	p.tts = gate2
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc7", Event: voiceframe.EventSay, Text: "second", Last: lastPtr(true)})
	_ = out.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := io.ReadFull(out, buf)
	if n != 640 || err != nil {
		t.Fatalf("F-1 mute bug: say after clear was never heard: n=%d err=%v", n, err)
	}
	if buf[0] != 1 {
		t.Fatalf("utterance 2 chunk content unexpected: %d", buf[0])
	}

	_ = childConn.Close()
	<-runDone
}
