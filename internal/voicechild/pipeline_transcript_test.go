package voicechild

// Leaf 05 tests (docs/plans/20261106-voice-tierA-qwen3-tts/
// 05-stt-pipeline-wiring.md, tasks T3 + T4):
//
//	T3 (TestTranscriptStartOnce): keys-before-setup and setup-before-keys
//	    both yield exactly one Stream call + one consumer per pipeline.
//	T4 (TestTranscriptTeardown): close() cancels the ctx, the Stream
//	    session ends, the consumer exits (trStart closed) - no leaked
//	    goroutine.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// startOnceChild builds a child + one pipeline with a recording STT
// factory, WITHOUT sending keys (the caller controls the transcript
// start order). Returns the pieces the tests drive.
func startOnceChild(t *testing.T, ccid string) (*fakeDaemon, *Child, net.Conn, *pipeline, *recordingSTT) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok")
	stt := newRecordingSTT()
	child.SetProviderFactories(
		func(cfg ProviderConfig, key string) (STTProvider, error) { return stt, nil },
		func(cfg ProviderConfig, key string) (TTSProvider, error) { return stubTTS{}, nil },
	)
	go func() { _ = child.Run(context.Background()) }()
	childConn := d.serveText()
	t.Cleanup(func() { _ = childConn.Close() })
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() { _ = in.Close(); _ = out.Close() })
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing after setup", ccid)
	}
	return d, child, childConn, p, stt
}

// TestTranscriptStartOnce (leaf T3): whichever of setup/start and
// keys/constructProviders runs first, Stream + the consumer start
// exactly once per pipeline.
func TestTranscriptStartOnce(t *testing.T) {
	// Order 1: setup first (start finds no STT yet), keys arrive later
	// (handleKeys hook starts transcripts). A repeated keys frame must
	// not re-Stream.
	const ccid1 = "so1"
	d, _, childConn, p, stt := startOnceChild(t, ccid1)
	_ = d
	sendRaw(t, childConn, `{"call_id":"`+ccid1+`","event":"keys","stt_key":"k","tts_key":"k"}`)
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	sendRaw(t, childConn, `{"call_id":"`+ccid1+`","event":"keys","stt_key":"k","tts_key":"k"}`)
	waitFor(t, func() bool { return p.transStarted })
	time.Sleep(100 * time.Millisecond)
	if got := stt.streamCalls(); got != 1 {
		t.Fatalf("order setup-then-keys: Stream called %d times, want exactly 1", got)
	}

	// Order 2: keys materialize before start (reverse of the normal C4
	// order) - start's own startTranscripts call must be a no-op.
	const ccid2 = "so2"
	_, child2, _, p2, stt2 := startOnceChild(t, ccid2)
	p2.applyKeys(KeysFrame{CallID: ccid2, STTKey: "k", TTSKey: "k"})
	if err := child2.constructProviders(p2); err != nil {
		t.Fatalf("constructProviders: %v", err)
	}
	p2.startTranscripts() // handleKeys hook, keys-before-start order
	waitFor(t, func() bool { return stt2.streamCalls() == 1 })
	if err := p2.start(context.Background()); err != nil { // start runs after
		t.Fatalf("start: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := stt2.streamCalls(); got != 1 {
		t.Fatalf("order keys-then-start: Stream called %d times, want exactly 1", got)
	}
}

// TestTranscriptTeardown (leaf T4): end frame tears the pipeline down;
// the consumer exits (trStart closed) and the Stream session ends via
// the pcmCh close / ctx cancel - no goroutine leak.
func TestTranscriptTeardown(t *testing.T) {
	d, child, childConn, p, stt := newSTTWiredChild(t, "tt1")
	_ = d
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	waitFor(t, func() bool { return p.transStarted })

	d.sendLine(childConn, voiceframe.Frame{CallID: "tt1", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 0 })

	select {
	case <-p.trStart:
		// consumer exited: clean teardown
	case <-time.After(2 * time.Second):
		t.Fatalf("transcript consumer did not exit after teardown (goroutine leak)")
	}
	if got := stt.streamCalls(); got != 1 {
		t.Errorf("Stream called %d times after teardown, want exactly 1", got)
	}
}
