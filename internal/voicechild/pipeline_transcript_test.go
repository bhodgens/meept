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
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
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
	waitFor(t, p.isTransStarted)
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

// TestSTTOpenRetryRecovers (audit fix 5): an STT Stream open that FAILS
// (transient adapter/transport error at keys time) must not leave the
// call permanently deaf — the keys frame arrives exactly once per call,
// so no external hook ever retries. The pipeline's own bounded retry
// chain re-opens the session while the pipeline is live. The stub fails
// the first open and succeeds on a retry: exactly one consumer must end
// up running, and trStart must NOT be closed while the retry is pending.
type flakySTT struct {
	mu       sync.Mutex
	fails    int
	calls    int
	accepted chan struct{}
	trans    chan Transcript
}

func (f *flakySTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	if n <= f.fails {
		f.mu.Unlock()
		return nil, errors.New("test: transient stt open failure")
	}
	f.mu.Unlock()
	go func() {
		for range pcm {
		}
	}()
	close(f.accepted)
	return f.trans, nil
}

func TestSTTOpenRetryRecovers(t *testing.T) {
	restore := pushFastRetryDelay(t)
	defer restore()

	const ccid = "rt1"
	d, child, childConn, p, _ := startOnceChild(t, ccid)
	f := &flakySTT{fails: 1, accepted: make(chan struct{}), trans: make(chan Transcript, 4)}
	child.SetProviderFactories(
		func(cfg ProviderConfig, key string) (STTProvider, error) { return f, nil },
		func(cfg ProviderConfig, key string) (TTSProvider, error) { return stubTTS{}, nil },
	)
	// Keys arrive (the ONLY keys line this call will ever see): the hook
	// claims the transcript slot and the retry chain starts.
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"k","tts_key":"k"}`)

	// The chain must reach Stream at least twice (first fails, retry
	// succeeds) without the pipeline ever being torn down.
	select {
	case <-f.accepted:
	case <-time.After(testPollDeadline):
		t.Fatalf("FIX 5 VIOLATION: STT session never opened after a failed first attempt (call stayed deaf)")
	}
	if n := func() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }(); n < 2 {
		t.Fatalf("Stream attempted %d times, want >= 2 (bounded retry never ran)", n)
	}
	if !p.isTransStarted() {
		t.Fatalf("transcript claim lost after retry")
	}
	// The session really runs: a transcript reaches the daemon as a prompt.
	f.trans <- Transcript{Text: "recovered", Final: true}
	frame := d.waitFrame(t)
	if frame.Event != voiceframe.EventPrompt || frame.Text != "recovered" {
		t.Fatalf("post-retry transcript produced %q %q, want prompt \"recovered\"", frame.Event, frame.Text)
	}
	_ = child
}

// TestSTTOpenRetryBounded (audit fix 5, other half): a provider that
// ALWAYS fails must give up after sttOpenAttempts tries, log permanently-
// deaf, close trStart (so teardown never hangs), and keep the child
// alive.
type alwaysFailSTT struct {
	mu    sync.Mutex
	calls int
}

func (a *alwaysFailSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	return nil, errors.New("test: permanent stt open failure")
}

func TestSTTOpenRetryBounded(t *testing.T) {
	restore := pushFastRetryDelay(t)
	defer restore()

	const ccid = "rt2"
	logger := newCaptureLogger()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(dir, "t.sock"),
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok\n"),
		Logger:         logger.logger(),
	})
	f := &alwaysFailSTT{}
	child.SetProviderFactories(
		func(cfg ProviderConfig, key string) (STTProvider, error) { return f, nil },
		func(cfg ProviderConfig, key string) (TTSProvider, error) { return stubTTS{}, nil },
	)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in, out := d.acceptAll(ccid)
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline(ccid)
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"k","tts_key":"k"}`)

	// Bounded: exactly sttOpenAttempts Stream calls, then the deaf log.
	waitFor(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.calls >= sttOpenAttempts
	})
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	n := f.calls
	f.mu.Unlock()
	if n != sttOpenAttempts {
		t.Fatalf("Stream attempted %d times, want exactly %d (retry must be bounded)", n, sttOpenAttempts)
	}
	waitFor(t, func() bool { return logger.contains("permanently failed") })

	// trStart closed by the failed chain: teardown must not wait 5s.
	select {
	case <-p.trStart:
	case <-time.After(2 * time.Second):
		t.Fatalf("failed retry chain never closed trStart (teardown would hang)")
	}

	// Child still alive: another call registers and serves fine.
	d.listenPair("rt3")
	d.sendLine(childConn, voiceframe.Frame{CallID: "rt3", Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in3, out3 := d.acceptAll("rt3")
	defer func() { _ = in3.Close() }()
	defer func() { _ = out3.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })

	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	if err := childConn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run returned error on close: %v", err)
	}
}

// pushFastRetryDelay shrinks the retry backoff for tests and restores it
// afterwards.
func pushFastRetryDelay(t *testing.T) func() {
	t.Helper()
	oldDelay, oldAttempts := trRetryDelay, sttOpenAttempts
	trRetryDelay = 20 * time.Millisecond
	sttOpenAttempts = 3
	return func() { trRetryDelay, sttOpenAttempts = oldDelay, oldAttempts }
}

// TestTranscriptTeardown (leaf T4): end frame tears the pipeline down;
// the consumer exits (trStart closed) and the Stream session ends via
// the pcmCh close / ctx cancel - no goroutine leak.
func TestTranscriptTeardown(t *testing.T) {
	d, child, childConn, p, stt := newSTTWiredChild(t, "tt1")
	_ = d
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	waitFor(t, p.isTransStarted)

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
