package voicechild

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// T4: keys frame with both keys -> both factories run, adapters installed.
func TestKeysWiringConstructsProviders(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("kw1")

	child := newTestChild(t, d, "tok")
	var sttCalls, ttsCalls int
	var mu sync.Mutex
	child.SetProviderFactories(
		func(key string, rate int) (STTProvider, error) {
			mu.Lock()
			sttCalls++
			mu.Unlock()
			if key != "stt-secret" {
				t.Errorf("stt factory key = %q, want the keys-frame key", key)
			}
			if rate != 16000 {
				t.Errorf("stt factory rate = %d, want setup-frame 16000", rate)
			}
			return stubSTT{}, nil
		},
		func(key string, rate int) (TTSProvider, error) {
			mu.Lock()
			ttsCalls++
			mu.Unlock()
			if key != "tts-secret" {
				t.Errorf("tts factory key = %q", key)
			}
			return stubTTS{}, nil
		},
	)

	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "kw1", Event: voiceframe.EventSetup,
		Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("kw1")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	// Keys arrive at call setup, after the daemon validated the token.
	// The keys event is not voiceframe.Frame-shaped (the shared codec has
	// no key fields by design), so the wire line is hand-crafted.
	sendRaw(t, childConn, `{"call_id":"kw1","event":"keys","stt_key":"stt-secret","tts_key":"tts-secret"}`)

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return sttCalls == 1 && ttsCalls == 1
	})

	p, ok := child.getPipeline("kw1")
	if !ok {
		t.Fatalf("pipeline gone after keys")
	}
	p.playMu.Lock()
	_, isSTT := p.stt.(stubSTT)
	_, isTTS := p.tts.(stubTTS)
	p.playMu.Unlock()
	if !isSTT || !isTTS {
		t.Fatalf("adapters not installed on the pipeline")
	}

	// A missing key fails the call with a named error: the pipeline is
	// torn down, the child and other calls are unaffected.
	d.listenPair("kw2")
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "kw2", Event: voiceframe.EventSetup,
		Codec: "L16", SampleRate: 16000,
	})
	in2, out2 := d.acceptAll("kw2")
	defer func() { _ = in2.Close() }()
	defer func() { _ = out2.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })

	// Swap the STT factory to a missing-key failure (mirroring
	// providers.NewSTT's ErrMissingSTTKey; the real constructor is
	// exercised in the providers package, here we pin the child's
	// routing behavior on failure).
	child.SetProviderFactories(func(key string, rate int) (STTProvider, error) {
		return nil, ErrMissingSTTKeyFactory
	}, nil)

	sendRaw(t, childConn, `{"call_id":"kw2","event":"keys","stt_key":"","tts_key":"tts2"}`)
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	// The surviving pipeline is still functional: a say does not error
	// the child and kw1 stays registered.
	d.sendLine(childConn, voiceframe.Frame{CallID: "kw1", Event: voiceframe.EventSay, Text: "still here"})
	time.Sleep(50 * time.Millisecond)
	if _, ok := child.getPipeline("kw1"); !ok {
		t.Fatalf("surviving pipeline gone after a say")
	}
	if child.pipelineCount() != 1 {
		t.Fatalf("pipeline count = %d, want 1", child.pipelineCount())
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

// ErrMissingSTTKeyFactory mirrors providers.ErrMissingSTTKey without the
// import (providers imports voicechild; the cycle must not close).
var ErrMissingSTTKeyFactory = errors.New("voicechild/providers: missing STT key for call")

// sendRaw writes one raw JSON line to the child connection (keys frames
// are not voiceframe.Frame-shaped by design: no key fields there).
func sendRaw(t *testing.T, conn interface{ Write([]byte) (int, error) }, line string) {
	t.Helper()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("write raw: %v", err)
	}
}

// T5: adapter construction logs vendor+model only - no key substring in
// the captured log output. Pins both the child wiring path (construct-
// failure logging) and the providers' constructors.
func TestNoKeyLogs(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("nk1")

	logger := newCaptureLogger()
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(dir, "t.sock"),
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok\n"),
		Logger:         logger.logger(),
	})
	child.SetProviderFactories(
		func(key string, rate int) (STTProvider, error) {
			return nil, errMissingKeyForLogTest
		},
		func(key string, rate int) (TTSProvider, error) {
			return stubTTS{}, nil
		},
	)

	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "nk1", Event: voiceframe.EventSetup,
		Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("nk1")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	const sttKey = "sk-super-secret-stt-value"
	const ttsKey = "sk-super-secret-tts-value"
	sendRaw(t, childConn, `{"call_id":"nk1","event":"keys","stt_key":"`+sttKey+`","tts_key":"`+ttsKey+`"}`)

	// The missing-key construction error tears the pipeline down; the log
	// must name the failure without key material.
	waitFor(t, func() bool { return child.pipelineCount() == 0 })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if logger.contains("provider construction failed") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	all := logger.everything()
	for _, secret := range []string{sttKey, ttsKey, "stt-secret", "tts-secret"} {
		if strings.Contains(all, secret) {
			t.Fatalf("log leaked key material %q:\n%s", secret, all)
		}
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

var errMissingKeyForLogTest = errors.New("missing STT key for call")
