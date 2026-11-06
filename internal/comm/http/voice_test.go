package http

// Gate tests for the voice HTTP surface (leaf 10, T1-T7): webhook signature
// gating, streaming.* no-ops, media-socket signature gate before upgrade,
// start-frame reading (connected skip, bounds, PCMU flag), idempotent
// OnMediaStart handoff, fail-closed when the public key is unset, the
// plain-HTTP loopback listener, and the DTMF counted no-op.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// fakeVoiceManager is a VoiceCallManager fake counting calls.
type fakeVoiceManager struct {
	mu        sync.Mutex
	events    []telnyx.CallEvent
	starts    []string // ccids passed to OnMediaStart
	dialCalls int      // times dial actually ran (idempotency check)
	streamURL string

	// dial blocks until released so handoff tests can hold the call open.
	dialGate chan struct{}
}

func (m *fakeVoiceManager) HandleCallEvent(ctx context.Context, ev telnyx.CallEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

// OnMediaStart mirrors the manager's idempotency contract: first start per
// ccid runs dial, later starts are no-ops.
func (m *fakeVoiceManager) OnMediaStart(ccid string, dial func(context.Context) error) error {
	m.mu.Lock()
	for _, s := range m.starts {
		if s == ccid {
			m.mu.Unlock()
			return nil
		}
	}
	m.starts = append(m.starts, ccid)
	m.mu.Unlock()
	m.mu.Lock()
	m.dialCalls++
	gate := m.dialGate
	m.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return dial(context.Background())
}

func (m *fakeVoiceManager) StreamURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamURL
}

func (m *fakeVoiceManager) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

// recordingRunner is a SpliceRunner fake.
type recordingRunner struct {
	mu     sync.Mutex
	calls  []string
	starts []StartFrame
}

func (r *recordingRunner) call(ctx context.Context, ccid string, conn WebSocketConn, start StartFrame) error {
	r.mu.Lock()
	r.calls = append(r.calls, ccid)
	r.starts = append(r.starts, start)
	r.mu.Unlock()
	return nil
}

// signingVerify builds VerifyFuncs over a fresh ed25519 keypair. Signers:
// signWebhook over raw bodies, signMedia over stream URLs.
func signingVerify(t *testing.T) (VerifyFuncs, func([]byte) http.Header, func(string) http.Header) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sign := func(message []byte) http.Header {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := ed25519.Sign(priv, append([]byte(ts+"|"), message...))
		return http.Header{
			"Telnyx-Signature-Ed25519": {base64.StdEncoding.EncodeToString(sig)},
			"Telnyx-Timestamp":         {ts},
		}
	}
	verify := VerifyFuncs{
		Webhook: func(body []byte, h http.Header) error {
			return telnyx.VerifyWebhookSignature(body, h, pub)
		},
		Media: func(h http.Header, streamURL string) error {
			return telnyx.VerifyMediaSocket(h, streamURL, pub)
		},
	}
	return verify, func(body []byte) http.Header { return sign(body) },
		func(streamURL string) http.Header { return sign([]byte(streamURL)) }
}

// callEventBody marshals a canonical Telnyx call webhook.
func callEventBody(t *testing.T, eventType, ccid string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"event_type": eventType,
			"payload": map[string]any{
				"call_control_id": ccid,
				"direction":       "incoming",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return body
}

// enabledRoutes wires a VoiceRoutes with a recording runner.
func enabledRoutes(t *testing.T) (*VoiceRoutes, *fakeVoiceManager, *recordingRunner, func([]byte) http.Header, func(string) http.Header) {
	t.Helper()
	verify, signWebhook, signMedia := signingVerify(t)
	mgr := &fakeVoiceManager{streamURL: "wss://example.ngrok.dev/api/v1/voice/media"}
	runner := &recordingRunner{}
	v, err := NewVoiceRoutes(mgr, verify, VoiceRoutesConfig{
		Enabled:      true,
		MediaPath:    "/api/v1/voice/media",
		SpliceRunner: runner.call,
	}, nil)
	if err != nil {
		t.Fatalf("NewVoiceRoutes: %v", err)
	}
	return v, mgr, runner, signWebhook, signMedia
}

// doVoice builds a server whose middleware chain wraps only the voice
// routes (matching the production wiring: the routes are reached through
// Server.middleware, bypassing auth via HandlesPath).
func doVoice(t *testing.T, v *VoiceRoutes, method, path string, body []byte, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
	handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request fell through to the general mux: %s %s", r.Method, r.URL.Path)
	}))
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, vs := range headers {
		for _, val := range vs {
			req.Header.Add(k, val)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// --- T1: webhook ---

func TestVoiceWebhookSignatureFail(t *testing.T) {
	v, mgr, _, signWebhook, _ := enabledRoutes(t)

	// No signature headers at all.
	rec := doVoice(t, v, http.MethodPost, v.WebhookPath(), callEventBody(t, "call.initiated", "cc1"), nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook: status = %d, want 401", rec.Code)
	}
	// Tampered body: signed payload differs from the delivered body.
	headers := signWebhook(callEventBody(t, "call.initiated", "cc1"))
	rec = doVoice(t, v, http.MethodPost, v.WebhookPath(), callEventBody(t, "call.initiated", "OTHER"), headers)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered webhook: status = %d, want 401", rec.Code)
	}
	if n := mgr.eventCount(); n != 0 {
		t.Fatalf("manager saw %d events after signature failures, want 0", n)
	}
}

func TestVoiceWebhookStreamingNoop(t *testing.T) {
	v, mgr, _, signWebhook, _ := enabledRoutes(t)
	for _, ev := range []string{"streaming.started", "streaming.stopped", "streaming.failed"} {
		body := callEventBody(t, ev, "cc1")
		rec := doVoice(t, v, http.MethodPost, v.WebhookPath(), body, signWebhook(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", ev, rec.Code)
		}
	}
	if n := mgr.eventCount(); n != 0 {
		t.Fatalf("streaming.* events reached the manager %d times, want 0", n)
	}
}

func TestVoiceWebhookCallEventsReachManager(t *testing.T) {
	v, mgr, _, signWebhook, _ := enabledRoutes(t)
	for _, ev := range []string{"call.initiated", "call.answered", "call.hangup"} {
		body := callEventBody(t, ev, "cc-"+ev)
		rec := doVoice(t, v, http.MethodPost, v.WebhookPath(), body, signWebhook(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", ev, rec.Code)
		}
	}
	if n := mgr.eventCount(); n != 3 {
		t.Fatalf("manager saw %d events, want 3", n)
	}
}

func TestVoiceWebhookManagerError500(t *testing.T) {
	verify, signWebhook, _ := signingVerify(t)
	mgr := &errorManager{}
	v, err := NewVoiceRoutes(mgr, verify, VoiceRoutesConfig{
		Enabled:      true,
		SpliceRunner: func(context.Context, string, WebSocketConn, StartFrame) error { return nil },
	}, nil)
	if err != nil {
		t.Fatalf("NewVoiceRoutes: %v", err)
	}
	body := callEventBody(t, "call.initiated", "cc1")
	rec := doVoice(t, v, http.MethodPost, v.WebhookPath(), body, signWebhook(body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("manager error: status = %d, want 500", rec.Code)
	}
}

type errorManager struct{ fakeVoiceManager }

func (m *errorManager) HandleCallEvent(ctx context.Context, ev telnyx.CallEvent) error {
	return errors.New("telnyx api unreachable")
}

// --- T2: media signature gate ---

func TestVoiceMediaAuthTampered(t *testing.T) {
	v, mgr, runner, _, signMedia := enabledRoutes(t)

	// Signed for a DIFFERENT url than StreamURL() reports.
	headers := signMedia("wss://evil.example/api/v1/voice/media")
	rec := doVoice(t, v, http.MethodGet, v.MediaPath(), nil, headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tampered media sig: status = %d, want 403", rec.Code)
	}
	// No headers at all.
	rec = doVoice(t, v, http.MethodGet, v.MediaPath(), nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unsigned media: status = %d, want 403", rec.Code)
	}
	// Refusal happened BEFORE the upgrade: response is a plain HTTP error,
	// not a 101.
	if rec.Code == http.StatusSwitchingProtocols {
		t.Fatal("upgrade must not happen on signature failure")
	}
	if len(runner.calls) != 0 || mgr.StreamURL() == "" {
		t.Fatalf("runner invoked or manager unset: %v %q", runner.calls, mgr.StreamURL())
	}
	// A valid signature over the EXACT StreamURL upgrades. The request must
	// carry the RFC 6455 upgrade headers (coder/websocket enforces them
	// even server-side).
	headers = signMedia(mgr.StreamURL())
	rec = doVoiceUpgrade(t, v, v.MediaPath(), headers)
	if rec.Code != http.StatusSwitchingProtocols {
		t.Fatalf("valid media sig: status = %d, want 101, body %s", rec.Code, rec.Body.String())
	}
}

// doVoice issues a request through the middleware chain like doVoice, with
// proper WebSocket upgrade headers so websocket.Accept can complete.
func doVoiceUpgrade(t *testing.T, v *VoiceRoutes, path string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
	handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request fell through to the general mux: %s %s", r.Method, r.URL.Path)
	}))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	for k, vs := range headers {
		for _, val := range vs {
			req.Header.Add(k, val)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// --- T3: start frame ---

func TestVoiceStartFrameSkipAndParse(t *testing.T) {
	v, _, runner, _, signMedia := enabledRoutes(t)

	// Real websocket client over httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v.serveVoiceRoute(w, r)
		})).ServeHTTP(w, r)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	headers := signMedia(v.mgrStreamURL())
	conn, resp, err := dialMediaWS(t, wsURL, headers)
	if err != nil {
		t.Fatalf("dial media ws: %v (resp %v)", err, resp)
	}
	defer conn.Close(websocketStatusNormal, "")

	// connected frame first (must be skipped), then the start frame.
	mustWriteWS(t, conn, []byte(`{"event":"connected"}`))
	mustWriteWS(t, conn, []byte(`{"event":"start","start":{"call_control_id":"cc77","media_format":{"encoding":"PCMU","sample_rate":8000}}}`))

	deadline := time.Now().Add(3 * time.Second)
	for {
		if runner.callCount() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("splice runner never invoked after start frame")
		}
		time.Sleep(2 * time.Millisecond)
	}
	start := runner.lastStart()
	if start.CallControlID != "cc77" || start.Encoding != "PCMU" || start.SampleRate != 8000 {
		t.Fatalf("start frame parse: %+v", start)
	}
	if !start.TranscodeInbound {
		t.Fatal("PCMU encoding must set TranscodeInbound")
	}
}

// TestVoiceStartFrameL16NoTranscode pins the flag on the default codec.
func TestVoiceStartFrameL16NoTranscode(t *testing.T) {
	v, _, runner, _, signMedia := enabledRoutes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(v.serveVoiceRoute)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	conn, _, err := dialMediaWS(t, wsURL, signMedia(v.mgrStreamURL()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocketStatusNormal, "")
	mustWriteWS(t, conn, []byte(`{"event":"start","start":{"call_control_id":"cc88","media_format":{"encoding":"L16","sample_rate":16000}}}`))
	deadline := time.Now().Add(3 * time.Second)
	for runner.callCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never invoked")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if start := runner.lastStart(); start.TranscodeInbound {
		t.Fatal("L16 must not set TranscodeInbound")
	}
}

// --- T4: idempotent handoff ---

func TestVoiceMediaHandoffIdempotent(t *testing.T) {
	v, mgr, _, _, signMedia := enabledRoutes(t)
	// Two START frames for the same ccid on one socket: the manager's
	// OnMediaStart runs dial exactly once.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(v.serveVoiceRoute)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	conn, _, err := dialMediaWS(t, wsURL, signMedia(mgr.StreamURL()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocketStatusNormal, "")
	start := []byte(`{"event":"start","start":{"call_control_id":"cc9","media_format":{"encoding":"L16","sample_rate":16000}}}`)
	mustWriteWS(t, conn, start)
	mustWriteWS(t, conn, []byte(`{"event":"connected"}`))
	mustWriteWS(t, conn, start) // duplicate start for the same ccid

	deadline := time.Now().Add(3 * time.Second)
	for mgr.dialRuns() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("dial never ran")
		}
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if runs := mgr.dialRuns(); runs != 1 {
		t.Fatalf("dial ran %d times for duplicate start, want 1", runs)
	}
}

// --- T5: fail-closed without key ---

func TestVoiceRoutesDisabled(t *testing.T) {
	// Disabled config: routes registered, but both answer 503.
	v, err := NewVoiceRoutes(nil, VerifyFuncs{}, VoiceRoutesConfig{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("NewVoiceRoutes disabled: %v", err)
	}
	rec := doVoice(t, v, http.MethodPost, v.WebhookPath(), []byte("{}"), nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled webhook: status = %d, want 503", rec.Code)
	}
	rec = doVoice(t, v, http.MethodGet, v.MediaPath(), nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled media: status = %d, want 503", rec.Code)
	}
	// The disabled routes still claim their paths so the middleware
	// exemption stays surgical (503, not fall-through to auth 401).
	if !v.HandlesPath(v.WebhookPath()) || !v.HandlesPath(v.MediaPath()) {
		t.Fatal("HandlesPath must cover both routes even when disabled")
	}
}

// --- T6: plain loopback listener ---

func TestVoicePlainListener(t *testing.T) {
	v, mgr, _, signWebhook, _ := enabledRoutes(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- StartVoicePlainListenerWith(ctx, ln, v, "") }()

	base := "http://" + ln.Addr().String()

	// Signed webhook over cleartext loopback: verified + handled.
	body := callEventBody(t, "call.initiated", "cc-plain")
	req, err := http.NewRequest(http.MethodPost, base+v.WebhookPath(), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for k, vs := range signWebhook(body) {
		for _, val := range vs {
			req.Header.Add(k, val)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("plain webhook post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plain webhook: status = %d, want 200", resp.StatusCode)
	}
	if n := mgr.eventCount(); n != 1 {
		t.Fatalf("plain listener manager events = %d, want 1", n)
	}

	// Unsigned webhook: 401 (signature still mandatory over plain HTTP).
	resp, err = client.Post(base+v.WebhookPath(), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("plain unsigned post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("plain unsigned webhook: status = %d, want 401", resp.StatusCode)
	}

	// Non-voice path: 404 - ONLY the two voice routes exist on the plain
	// listener.
	resp, err = client.Get(base + "/api/v1/health")
	if err != nil {
		t.Fatalf("plain health probe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("plain listener non-voice path: status = %d, want 404", resp.StatusCode)
	}

	// The media route answers on the plain listener too (403 unsigned, not
	// 404): signature headers absent -> refused before upgrade.
	resp, err = client.Get(base + v.MediaPath())
	if err != nil {
		t.Fatalf("plain media probe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("plain unsigned media: status = %d, want 403", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("plain listener: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("plain listener did not shut down")
	}
}

func TestVoicePlainListenerRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9090", "192.168.1.5:9090", "example.com:9090", "9090"} {
		err := StartVoicePlainListenerWith(context.Background(), nil, nil, addr)
		if !errors.Is(err, ErrNonLoopbackVoiceAddr) {
			t.Fatalf("addr %q: err = %v, want ErrNonLoopbackVoiceAddr", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9090", "127.9.9.9:9090", "[::1]:9090", "localhost:9090"} {
		if err := StartVoicePlainListenerWith(context.Background(), nil, nil, addr); err != nil {
			t.Fatalf("loopback addr %q refused: %v", addr, err)
		}
	}
}

// --- T7: DTMF ---

func TestVoiceDTMFCountedNoop(t *testing.T) {
	v, mgr, runner, _, signMedia := enabledRoutes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(v.serveVoiceRoute)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	conn, _, err := dialMediaWS(t, wsURL, signMedia(mgr.StreamURL()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocketStatusNormal, "")

	mustWriteWS(t, conn, []byte(`{"event":"connected"}`))
	mustWriteWS(t, conn, []byte(`{"event":"dtmf","digit":"1"}`))
	mustWriteWS(t, conn, []byte(`{"event":"dtmf","digit":"2"}`))
	// Start AFTER the dtmf frames: they must have been consumed as counted
	// no-ops, never as errors and never forwarded to the splice.
	mustWriteWS(t, conn, []byte(`{"event":"start","start":{"call_control_id":"cc-dtmf","media_format":{"encoding":"L16","sample_rate":16000}}}`))

	deadline := time.Now().Add(3 * time.Second)
	for runner.callCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("start frame after dtmf frames never parsed (dtmf broke the reader)")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if start := runner.lastStart(); start.CallControlID != "cc-dtmf" {
		t.Fatalf("start ccid = %q, want cc-dtmf", start.CallControlID)
	}
}

// readStartFrameUnits exercises the reader directly (bounds + skip count).
func TestVoiceStartFrameBounded(t *testing.T) {
	v, _, _, _, _ := enabledRoutes(t)
	// A socket that NEVER sends start: bounded by maxStartFrames, not
	// forever. fakePreStartWS feeds only connected frames.
	conn := &fakePreStartWS{n: maxStartFrames + 5}
	_, _, err := v.readStartFrame(context.Background(), conn)
	if err == nil {
		t.Fatal("expected bound error when no start frame arrives")
	}
	if !strings.Contains(err.Error(), "start frame") {
		t.Fatalf("error should name the start-frame bound: %v", err)
	}
	if conn.reads != maxStartFrames {
		t.Fatalf("reads = %d, want the %d-frame bound", conn.reads, maxStartFrames)
	}
}

// fakePreStartWS returns n connected-shaped frames then blocks.
type fakePreStartWS struct {
	n     int
	reads int
}

func (f *fakePreStartWS) Read(ctx context.Context) ([]byte, error) {
	f.reads++
	if f.reads <= f.n {
		return []byte(`{"event":"connected"}`), nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakePreStartWS) Write(p []byte) error { return nil }
func (f *fakePreStartWS) Close() error         { return nil }

// --- helpers ---

func (m *fakeVoiceManager) dialRuns() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dialCalls
}

func (r *recordingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *recordingRunner) lastStart() StartFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts[len(r.starts)-1]
}

// mgrStreamURL reaches the manager's StreamURL through the routes seam.
func (v *VoiceRoutes) mgrStreamURL() string { return v.mgr.StreamURL() }

// dialMediaWS dials the media websocket with signature headers using
// nhooyr-style coder/websocket client.
func dialMediaWS(t *testing.T, url string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: headers,
	})
}

func mustWriteWS(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write ws frame: %v", err)
	}
}

const websocketStatusNormal = websocket.StatusNormalClosure

// --- bughunt H2-wiring: start frame carries the negotiated wire codec ---
//
// The splice runner builds SpliceConfig.CodecWire from StartFrame.Encoding/
// SampleRate while TranscodeInbound keeps its PCMU-flag semantics. These
// tests pin the StartFrame side of that contract (the SpliceConfig fill is
// daemon wiring, pinned in internal/daemon/voice_wiring_test.go).

func TestVoiceStartFrameCarriesWireCodec(t *testing.T) {
	v, _, runner, _, signMedia := enabledRoutes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(v.serveVoiceRoute)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	conn, _, err := dialMediaWS(t, wsURL, signMedia(v.mgrStreamURL()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocketStatusNormal, "")
	mustWriteWS(t, conn, []byte(`{"event":"connected"}`))
	mustWriteWS(t, conn, []byte(`{"event":"start","start":{"call_control_id":"cc-h2","media_format":{"encoding":"PCMU","sample_rate":8000}}}`))

	deadline := time.Now().Add(3 * time.Second)
	for runner.callCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never invoked")
		}
		time.Sleep(2 * time.Millisecond)
	}
	start := runner.lastStart()
	// Wire codec fields populated verbatim from media_format...
	if start.Encoding != "PCMU" || start.SampleRate != 8000 {
		t.Fatalf("start frame wire codec: encoding=%q sample_rate=%d, want PCMU/8000", start.Encoding, start.SampleRate)
	}
	// ...while TranscodeInbound keeps its PCMU-flag semantics.
	if !start.TranscodeInbound {
		t.Fatal("PCMU encoding must keep TranscodeInbound true")
	}
}

func TestVoiceStartFrameL16EmptyTranscodeKeepsCodecFields(t *testing.T) {
	v, _, runner, _, signMedia := enabledRoutes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithVoiceRoutes(v))
		s.middleware(http.HandlerFunc(v.serveVoiceRoute)).ServeHTTP(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + v.MediaPath()
	conn, _, err := dialMediaWS(t, wsURL, signMedia(v.mgrStreamURL()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocketStatusNormal, "")
	mustWriteWS(t, conn, []byte(`{"event":"start","start":{"call_control_id":"cc-h2b","media_format":{"encoding":"L16","sample_rate":16000}}}`))

	deadline := time.Now().Add(3 * time.Second)
	for runner.callCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never invoked")
		}
		time.Sleep(2 * time.Millisecond)
	}
	start := runner.lastStart()
	if start.Encoding != "L16" || start.SampleRate != 16000 {
		t.Fatalf("start frame wire codec: encoding=%q sample_rate=%d, want L16/16000", start.Encoding, start.SampleRate)
	}
	if start.TranscodeInbound {
		t.Fatal("L16 must not set TranscodeInbound")
	}
}
