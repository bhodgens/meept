package providers

// Leaf 04 (link-72a04aebac963cdb): TTS connection hygiene.
//
// Defect pinned here: the uvicorn TTS server drops idle keep-alives and a
// stale pooled connection silently killed the greeting POST ("http:
// server closed idle connection"). The shared transport disables
// keep-alives outright and Speak retries exactly once, narrowly, on
// dial/keep-alive-class transport errors — never on HTTP status.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// captureDefaultSlog points slog.Default at an in-memory text handler for
// the duration of the test (the retry WARN goes through the default
// logger; the child's production logger is slog.Default-backed too).
func captureDefaultSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// staleKeepaliveFake is a real-TCP http.Server whose FIRST accepted
// connection is hijacked and killed before any response is written — the
// transport-level failure the stale keep-alive produces — while every
// later connection serves 200 streaming PCM. It records the number of
// handler invocations (== attempts that reached the server).
func staleKeepaliveFake(t *testing.T, pcm []byte) (net.Listener, *atomic.Int64) {
	t.Helper()
	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// Kill the connection mid-response-less: the client's
			// Do fails with a transport-level EOF before any status.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("ResponseWriter is not a Hijacker")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close() //nolint:mutexio // error path, no lock held
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher := w.(http.Flusher)
		for len(pcm) > 0 {
			n := min(960, len(pcm))
			if _, err := w.Write(pcm[:n]); err != nil {
				return
			}
			flusher.Flush()
			pcm = pcm[n:]
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			t.Errorf("fake server: %v", serr)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
	return ln, &attempts
}

// speakTTSWithTransport builds an OpenAITTS pointed at a plain http
// base URL with an explicit transport (fresh-dial, no keep-alives, like
// the production ttsTransport).
func speakTTSWithTransport(baseURL string) *OpenAITTS {
	return &OpenAITTS{ //nolint:gosec // G101: TEST-only fake vendor key, not a real credential
		cfg:       ProviderConfig{Vendor: TTSVendorOpenAI, BaseURL: baseURL, Model: "gpt-4o-mini-tts", SampleRate: 24000},
		apiKey:    "test-tts-key",
		transport: &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 10 * time.Second},
	}
}

// hostPort trims the scheme off a net.Listener address.
func hostPort(lnAddr net.Addr) string { return lnAddr.String() }

// H1: a transport-layer kill of the first connection is retried exactly
// once; Speak still returns the audio and logs one WARN naming the call.
func TestTTSSpeakRetriesOnceOnStaleTransportError(t *testing.T) {
	logs := captureDefaultSlog(t)
	pcm := pcm24k(2400) // 100 ms at the vendor rate
	ln, attempts := staleKeepaliveFake(t, pcm)
	tts := speakTTSWithTransport("http://" + hostPort(ln.Addr()))

	ctx := WithCallID(context.Background(), "local-testcall")
	rc, err := tts.Speak(ctx, "greeting that hit a stale keep-alive")
	if err != nil {
		t.Fatalf("Speak after transport kill: %v (want one narrow retry to succeed)", err)
	}
	defer func() { _ = rc.Close() }() //nolint:mutexio // deferred close, no lock held

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read streamed audio: %v", err)
	}
	if len(got) != len(pcm) {
		t.Fatalf("audio size %d, want %d", len(got), len(pcm))
	}
	if n := attempts.Load(); n != 2 {
		t.Fatalf("server attempts = %d, want exactly 2 (one retry, never more)", n)
	}
	line := logs.String()
	if !strings.Contains(line, "level=WARN") ||
		!strings.Contains(line, "openai tts: retrying failed speech request") {
		t.Fatalf("want one WARN retry line, got: %q", line)
	}
	if !strings.Contains(line, "call_id=local-testcall") {
		t.Fatalf("WARN must name the call_id, got: %q", line)
	}
	// Exactly one WARN line.
	if strings.Count(line, "retrying failed speech request") != 1 {
		t.Fatalf("expected exactly one WARN line, got: %q", line)
	}
}

// H1 negative: the retry budget is ONE attempt. A fake that kills the
// first TWO connections makes Speak fail with the wrapped request error,
// exactly two attempts, and one WARN line.
func TestTTSSpeakDoesNotRetryMoreThanOnce(t *testing.T) {
	logs := captureDefaultSlog(t)
	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("ResponseWriter is not a Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close() //nolint:mutexio // error path, no lock held
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			t.Errorf("kill-everything server: %v", serr)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})

	tts := speakTTSWithTransport("http://" + hostPort(ln.Addr()))
	_, err = tts.Speak(context.Background(), "hi")
	if err == nil {
		t.Fatal("Speak succeeded against a server that kills every connection")
	}
	if !strings.Contains(err.Error(), "openai tts: request:") {
		t.Fatalf("final error shape %q, want wrapped \"openai tts: request:\"", err)
	}
	if n := attempts.Load(); n != 2 {
		t.Fatalf("server attempts = %d, want exactly 2 (one WARN retry, then give up)", n)
	}
	if strings.Count(logs.String(), "retrying failed speech request") != 1 {
		t.Fatalf("want exactly one WARN line, got: %q", logs.String())
	}
}

// H2: an HTTP 500 is NOT a transport error — single attempt, no retry,
// no WARN. (Status failures already return their own named error after
// the response; the retry path must never swallow them.)
func TestTTSSpeakDoesNotRetryOnHTTPStatus(t *testing.T) {
	logs := captureDefaultSlog(t)
	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			t.Errorf("500 server: %v", serr)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})

	tts := speakTTSWithTransport("http://" + hostPort(ln.Addr()))
	_, err = tts.Speak(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("error shape %q, want \"openai tts: unexpected status 500\"", err)
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("500 must not be retried: attempts = %d, want 1", n)
	}
	if strings.Contains(logs.String(), "retrying failed speech request") {
		t.Fatalf("no WARN expected on a status failure, got: %q", logs.String())
	}
}

// H3: the shared transport pins DisableKeepAlives (flag test — the
// greeting-killer is stale pooled connections; the fix is no pooling).
func TestTTSTransportDisablesKeepAlives(t *testing.T) {
	tr, ok := ttsTransport.(*http.Transport)
	if !ok {
		t.Fatalf("ttsTransport is %T, want *http.Transport", ttsTransport)
	}
	if !tr.DisableKeepAlives {
		t.Fatal("ttsTransport must set DisableKeepAlives: true (leaf 04 stale keep-alive fix)")
	}
	// Sane-timeout mirror of DefaultTransport (no IdleConnTimeout needed).
	if tr.TLSHandshakeTimeout != 10*time.Second ||
		tr.ExpectContinueTimeout != 1*time.Second ||
		tr.ResponseHeaderTimeout != 30*time.Second {
		t.Fatalf("transport timeouts drifted from the pinned set: %+v", tr)
	}
}

// H3: the constructors land the shared transport on the provider (the
// seam must not silently drop it).
func TestNewTTSUsesSharedTransport(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ProviderConfig
		key  string
	}{
		{"openai", ProviderConfig{Vendor: "openai", Model: "gpt-4o-mini-tts"}, "k"}, //nolint:gosec // G101: test-only fake key
		{"qwen3", ProviderConfig{Vendor: "qwen3", BaseURL: "http://127.0.0.1:1"}, ""},
	} {
		p, err := NewTTS(tc.cfg, tc.key)
		if err != nil {
			t.Fatalf("%s: NewTTS: %v", tc.name, err)
		}
		oai, ok := p.(*OpenAITTS)
		if !ok {
			t.Fatalf("%s: got %T, want *OpenAITTS", tc.name, p)
		}
		if oai.transport != ttsTransport {
			t.Fatalf("%s: provider transport is not the shared ttsTransport", tc.name)
		}
	}
}

// H1 classifier unit: narrow dial/keep-alive classes retry; status
// strings, cancellations, and nil never do.
func TestIsTTSTransportErrorClassification(t *testing.T) {
	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	retry := []error{
		&net.OpError{Op: "read", Net: "tcp", Err: errors.New("server closed idle connection")},
		errors.New("Post \"http://x/y\": EOF"),
		io.ErrUnexpectedEOF,
		errors.New("read tcp 127.0.0.1:1: connection reset by peer"),
		&urlWrapErr{},
	}
	for _, err := range retry {
		if !isTTSTransportError(ctx, err) {
			t.Errorf("want retryable: %v", err)
		}
	}
	noretry := []error{
		nil,
		errors.New("openai tts: unexpected status 503"),
		errors.New("context canceled"),
		&net.DNSError{Err: "no such host", IsNotFound: true},
	}
	for _, err := range noretry {
		if isTTSTransportError(ctx, err) {
			t.Errorf("must NOT retry: %v", err)
		}
	}
	// A transport-class error on a cancelled context is not retried.
	if isTTSTransportError(canceled, io.EOF) {
		t.Error("cancelled ctx must never retry")
	}
}

// urlWrapErr mimics url.Error's shape (wraps an *net.OpError) — the exact
// wrapper http.Client.Do returns for dial-class failures.
type urlWrapErr struct{}

func (e *urlWrapErr) Error() string {
	return "Post \"http://socrates:8001/v1/audio/speech\": http: server closed idle connection"
}
func (e *urlWrapErr) Unwrap() error {
	return &net.OpError{Op: "read", Net: "tcp", Err: errors.New("http: server closed idle connection")}
}
