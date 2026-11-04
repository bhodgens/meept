//go:build e2e

// fakeTelnyx is the hermetic Telnyx stand-in for the voice suites (leaf
// 15): Ed25519-signed call-control webhook deliveries INTO the daemon's
// voice route (a test keypair whose public half the daemon is configured
// with), a media websocket CLIENT per call (Telnyx dials IN to the
// daemon's media route, so the fake is the dialing side), and a control
// socket bridge to the fake child (testdata/fakechild). No live vendor,
// no network beyond loopback, no ONNX.
//
// Shared-helper choice: duplicated per suite package. The repo's e2e tier
// keeps helpers in e2e/harness only (shared across ALL suites, Go-test
// files only) and the parked leaf-00T files were already per-suite, so a
// cross-suite shared Go package would need a new import path inside the
// e2e tier for three consumers; per-suite duplication follows the tier's
// existing convention.
package voicelifecycle

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
)

// c1Frame mirrors the C1 text-socket frame the suite asserts on (wire
// keys per contract C1).
type c1Frame struct {
	CallID string `json:"call_id"`
	Event  string `json:"event"`
	Text   string `json:"text,omitempty"`
	Last   *bool  `json:"last,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ctrlMsg is the fake-child control-socket message shape (see
// testdata/fakechild/main.go).
type ctrlMsg struct {
	Ctrl       string   `json:"ctrl"`
	CallID     string   `json:"call_id,omitempty"`
	Text       string   `json:"text,omitempty"`
	Last       *bool    `json:"last,omitempty"`
	From       string   `json:"from,omitempty"`
	To         string   `json:"to,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Codec      string   `json:"codec,omitempty"`
	SampleRate int      `json:"sample_rate,omitempty"`
	Frame      *c1Frame `json:"frame,omitempty"`
}

type fakeTelnyx struct {
	t    *testing.T
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey

	// srv is the fake call-control API the daemon's telnyx client POSTs
	// against (MEEPT_TELNYX_TEST_BASE_URL): every /calls/... action is
	// recorded and answered 200, no live vendor.
	srv *httptest.Server

	mu sync.Mutex
	// recorded carries every webhook delivery (event + status) AND every
	// call-control action path the daemon POSTed, the observable the
	// answer/hangup/refusal assertions poll.
	recorded []recWebhook
	actions  []string
	// media carries the frames each call's media websocket received and
	// the live conns the fake writes interrupts to (Telnyx side).
	media     map[string][]string
	mediaConn map[string]*websocket.Conn

	childMu   sync.Mutex
	ctrlConn  net.Conn
	frames    map[string][]c1Frame
	sayCount  map[string]int
	clearedAt map[string]int // index of first clear frame per call (-1 none)
}

type recWebhook struct {
	Event  string
	CCID   string
	Status int
}

// webhookPath is the daemon's fixed voice webhook route.
const webhookPath = "/api/v1/voice/webhook"

// mediaPath is the daemon's default voice media route.
const mediaPath = "/api/v1/voice/media"

func newFakeTelnyx(t *testing.T) *fakeTelnyx {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("fakeTelnyx: generate keypair: %v", err)
	}
	f := &fakeTelnyx{
		t:         t,
		pub:       pub,
		priv:      priv,
		frames:    map[string][]c1Frame{},
		sayCount:  map[string]int{},
		clearedAt: map[string]int{},
		media:     map[string][]string{},
		mediaConn: map[string]*websocket.Conn{},
	}
	// Call-control API stand-in: record every action path, answer 200
	// with a minimal Telnyx-shaped body (DialOut parses data.call_control_id).
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20)) //nolint:errcheck // test sink drain
		f.mu.Lock()
		f.actions = append(f.actions, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"call_control_id":"cc-fake-out"}}`)) //nolint:errcheck // test sink
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// publicKeyB64 is the base64 Ed25519 public key the daemon verifies with.
func (f *fakeTelnyx) publicKeyB64() string {
	return base64.StdEncoding.EncodeToString(f.pub)
}

// baseURL is the fake call-control API origin the daemon's telnyx client
// POSTs against (wired through MEEPT_TELNYX_TEST_BASE_URL).
func (f *fakeTelnyx) baseURL() string { return f.srv.URL }

// streamURL is the media URL Telnyx is told to dial: the scratch daemon's
// own listener over the ws scheme (the signature check is scheme-agnostic:
// the signed message is the exact URL string). The path must stay the
// default media route the daemon registers.
func (f *fakeTelnyx) streamURL(s *harness.Stack) string {
	host := strings.TrimPrefix(s.HTTPBaseURL(), "https://")
	return "wss://" + host + mediaPath
}

// overlayOptions builds the [voice] config overlay every voice test
// shares. It runs as a harness config hook because the daemon http addr
// the stream URL needs is pinned inside writeConfigs, where the hook
// executes (cfg.transport.http.addr is already resolved there).
func (f *fakeTelnyx) overlayOptions(extra map[string]any) harness.StartOption {
	return harness.WithConfigHook(func(cfg map[string]any) {
		voice := map[string]any{}
		if v, ok := cfg["voice"].(map[string]any); ok {
			voice = v
		} else {
			cfg["voice"] = voice
		}
		voice["enabled"] = true
		voice["telnyx_public_key"] = f.publicKeyB64()
		voice["connection_id"] = "conn-e2e"
		voice["from_number"] = "+15550000001"
		voice["filler"] = []string{"One moment."}
		voice["idle_stop_seconds"] = 2
		voice["supervisor_timeout_ms"] = 400
		voice["onnx_runtime_lib"] = ""
		for k, v := range extra {
			voice[k] = v
		}
		addr := ""
		if t, ok := cfg["transport"].(map[string]any); ok {
			if h, ok := t["http"].(map[string]any); ok {
				if a, ok := h["addr"].(string); ok {
					addr = a
				}
			}
		}
		if addr == "" {
			f.t.Fatal("fakeTelnyx: overlay could not resolve the daemon http addr from the harness config")
		}
		voice["public_stream_url"] = "wss://" + addr + mediaPath
	})
}

// apiKeyEnv is the env var carrying the fake telnyx API key (declared to
// the secrets broker via secretsOverlay; resolved in-process at wiring).
func apiKeyEnv() string { return "MEEPT_E2E_TELNYX_API_KEY" }

// extraEnv carries the hermetic telnyx API base override (the daemon reads
// it from the process environment at boot, never from config) and the API
// key env the secrets source names. The base URL is a loopback black hole:
// these suites never exercise outbound dial POSTs against it.
func (f *fakeTelnyx) extraEnv() map[string]string {
	return map[string]string{
		"MEEPT_TELNYX_TEST_BASE_URL": f.baseURL(),
		apiKeyEnv():                  "fake-telnyx-key",
	}
}

// secretsOverlay declares the telnyx_api_key secret source the voice
// wiring resolves through the broker (missing secret degrades the whole
// voice block to disabled, so this fragment is load-bearing).
func (f *fakeTelnyx) secretsOverlay() map[string]any {
	return map[string]any{
		"secrets.sources.telnyx_api_key": map[string]any{
			"kind": "env",
			"name": apiKeyEnv(),
		},
	}
}

// signHeaders builds the Telnyx Ed25519 signature headers over msg
// ("{timestamp}|{msg}"), the exact scheme internal/voice/telnyx verifies.
func (f *fakeTelnyx) signHeaders(msg []byte) http.Header {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	signed := append(append([]byte(ts), '|'), msg...)
	sig := ed25519.Sign(f.priv, signed)
	h := http.Header{}
	h.Set("Telnyx-Signature-Ed25519", base64.StdEncoding.EncodeToString(sig))
	h.Set("Telnyx-Timestamp", ts)
	return h
}

// insecureClient tolerates the scratch daemon's self-signed certificate
// (loopback only, same posture as the harness itself).
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
}

// callEventBody builds one Telnyx call-control webhook body.
func callEventBody(eventType, ccid, direction string) []byte {
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"event_type": eventType,
			"payload": map[string]any{
				"call_control_id": ccid,
				"call_session_id": "sess-" + ccid,
				"from":            "+15550000009",
				"to":              "+15550000001",
				"direction":       direction,
			},
		},
	})
	if err != nil {
		return []byte("{}")
	}
	return body
}

// postWebhook delivers one signed webhook into the daemon and records +
// returns the HTTP status.
func (f *fakeTelnyx) postWebhook(s *harness.Stack, eventType, ccid, direction string) int {
	f.t.Helper()
	body := callEventBody(eventType, ccid, direction)
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+webhookPath, bytes.NewReader(body))
	if err != nil {
		f.t.Fatalf("fakeTelnyx: webhook request: %v", err)
	}
	for k, vs := range f.signHeaders(body) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		f.t.Fatalf("fakeTelnyx: webhook post %s for %s: %v", eventType, ccid, err)
	}
	defer resp.Body.Close() //nolint:errcheck // bounded test response
	status := resp.StatusCode
	if status >= 400 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		f.t.Logf("fakeTelnyx: %s webhook refused %d; body: %s", eventType, status, string(bodyBytes))
	}
	f.mu.Lock()
	f.recorded = append(f.recorded, recWebhook{Event: eventType, CCID: ccid, Status: status})
	f.mu.Unlock()
	return status
}

// deliverCallInitiated posts a signed incoming call.initiated webhook and
// requires 200 (the manager accepted the call).
func (f *fakeTelnyx) deliverCallInitiated(s *harness.Stack, ccid, direction string) {
	f.t.Helper()
	if status := f.postWebhook(s, "call.initiated", ccid, direction); status != http.StatusOK {
		f.t.Fatalf("fakeTelnyx: call.initiated for %s answered %d, want 200; daemon log tail:\n%s",
			ccid, status, s.Daemon.LogTail())
	}
}

// deliverCallInitiatedExpectStatus posts the webhook and returns the raw
// status (refusal paths assert >= 400).
func (f *fakeTelnyx) deliverCallInitiatedExpectStatus(s *harness.Stack, ccid, direction string) int {
	f.t.Helper()
	return f.postWebhook(s, "call.initiated", ccid, direction)
}

// deliverHangup posts a signed call.hangup webhook and requires 200.
func (f *fakeTelnyx) deliverHangup(s *harness.Stack, ccid string) {
	f.t.Helper()
	if status := f.postWebhook(s, "call.hangup", ccid, "incoming"); status != http.StatusOK {
		f.t.Fatalf("fakeTelnyx: call.hangup for %s answered %d, want 200", ccid, status)
	}
}

// recordedCount reports how many recorded webhooks match the predicate.
func (f *fakeTelnyx) recordedCount(match func(recWebhook) bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.recorded {
		if match(r) {
			n++
		}
	}
	return n
}

// awaitAnswered polls until the daemon answered ccid. The observable is
// OUR OWN delivery log: the manager's refusal mapped to a 500 (or a
// successful 200) shows up in the webhook status we recorded. Answering
// itself happens via a POST to the FAKE base URL (a black hole), so the
// real answer action is observed through the call answering SUCCESSFULLY
// downstream: media start frame + talk loop. This helper asserts the
// webhook was ACCEPTED (200) without triggering redelivery.
func (f *fakeTelnyx) awaitAnswered(ccid string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if f.recordedCount(func(r recWebhook) bool {
			return r.CCID == ccid && r.Event == "call.initiated" && r.Status == http.StatusOK
		}) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: call.initiated for %s never accepted within %s", ccid, within)
}

// assertNoAnswer asserts every delivery for ccid was refused (>= 400):
// a refused call must never see an accepted webhook.
func (f *fakeTelnyx) assertNoAnswer(ccid string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		bad := f.recordedCount(func(r recWebhook) bool {
			return r.CCID == ccid && r.Status >= 200 && r.Status < 300
		})
		if bad > 0 {
			f.t.Fatalf("fakeTelnyx: %d accepted webhook(s) for refused call %s", bad, ccid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---- media websocket (Telnyx dials IN) ----

// dialMedia dials the daemon's media websocket as Telnyx would: Ed25519
// headers over the exact stream URL, then the connected + start frames
// (PCMU@8k; the daemon transcodes). A reader goroutine logs inbound
// frames; the outbound media writes are asserted via the child mirror
// (say/clear), not via audio bytes.
func (f *fakeTelnyx) dialMedia(s *harness.Stack, ccid string) {
	f.t.Helper()
	streamURL := f.streamURL(s)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, streamURL, &websocket.DialOptions{
		HTTPClient: insecureClient(),
		HTTPHeader: f.signHeaders([]byte(streamURL)),
	})
	dialCancel()
	if err != nil {
		f.t.Fatalf("fakeTelnyx: dial media ws for %s: %v", ccid, err)
	}
	f.t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") }) //nolint:errcheck // teardown
	f.mu.Lock()
	f.mediaConn[ccid] = conn
	f.mu.Unlock()

	writeWS(conn, `{"event":"connected"}`)
	start := fmt.Sprintf(`{"event":"start","start":{"call_control_id":%q,"media_format":{"encoding":"PCMU","sample_rate":8000}}}`, ccid)
	writeWS(conn, start)

	go func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer rcancel()
		for {
			_, data, err := conn.Read(rctx)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.media[ccid] = append(f.media[ccid], string(data))
			f.mu.Unlock()
		}
	}()
}

// sendMediaFrame writes one raw JSON line onto the call's media websocket
// (the fake Telnyx's inbound direction: media frames and interrupts).
func (f *fakeTelnyx) sendMediaFrame(ccid, payload string) {
	f.t.Helper()
	f.mu.Lock()
	conn := f.mediaConn[ccid]
	f.mu.Unlock()
	if conn == nil {
		f.t.Fatalf("fakeTelnyx: no media websocket for %s", ccid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The splice's inbound reader is line-delimited (bufio ReadBytes) -
	// frames need a trailing newline to process.
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload+"\n")); err != nil {
		f.t.Fatalf("fakeTelnyx: media write for %s: %v", ccid, err)
	}
}

// mediaHasFrame reports whether the call's media websocket received a
// frame containing substr.
func (f *fakeTelnyx) mediaHasFrame(ccid, substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fr := range f.media[ccid] {
		if strings.Contains(fr, substr) {
			return true
		}
	}
	return false
}

// awaitMediaFrame polls until the call's media websocket received a frame
// containing substr (bounded; the clear-frame observable).
func (f *fakeTelnyx) awaitMediaFrame(ccid, substr string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if f.mediaHasFrame(ccid, substr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: no %q frame on %s media socket within %s", substr, ccid, within)
}

// writeWS sends one text frame, bounded.
func writeWS(conn *websocket.Conn, payload string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		// The reader path asserts the consequences; a failed pre-start
		// write is fatal to the test's premise.
		panic("writeWS: " + err.Error()) //nolint:forbidigo // test helper
	}
}

// ---- fake-child control bridge ----

// buildFakeChild compiles testdata/fakechild into dir and returns the
// binary path (per-test temp dir; the process is bounded by the daemon's
// ChildProc lifetime and the llm supervisor's parent-death rule).
func buildFakeChild(t *testing.T, dir string) string {
	t.Helper()
	// The fake child source lives ONCE under voice-lifecycle/testdata;
	// sibling suites build it from there (go test cwd is the package dir:
	// walk up two levels to e2e/suites, then across).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("fake child cwd: %v", err)
	}
	src := filepath.Join(filepath.Dir(cwd), "voice-lifecycle", "testdata", "fakechild", "main.go")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fake child source: %v", err)
	}
	out := filepath.Join(dir, "fakechild")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build fakechild: %v", err)
	}
	return out
}

// connectChild dials the running fake child's control socket (the path is
// derived by the child from its --audio-dir: <sock dir>/../ctrl.sock).
func (f *fakeTelnyx) connectChild(socketPath string, within time.Duration) {
	f.t.Helper()
	var conn net.Conn
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", socketPath)
		if err == nil {
			conn = c
			break
		}
		if time.Now().After(deadline.Add(-time.Millisecond)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		f.t.Fatalf("fakeTelnyx: control socket %s never appeared within %s", socketPath, within)
	}
	f.childMu.Lock()
	f.ctrlConn = conn
	f.childMu.Unlock()
	go f.readChildFrames(conn)
}

// readChildFrames mirrors control-socket messages into the frame log.
func (f *fakeTelnyx) readChildFrames(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for sc.Scan() {
		var m ctrlMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Frame == nil {
			continue
		}
		f.childMu.Lock()
		f.frames[m.Frame.CallID] = append(f.frames[m.Frame.CallID], *m.Frame)
		if m.Frame.Event == "say" {
			f.sayCount[m.Frame.CallID]++
		}
		if m.Frame.Event == "clear" {
			if _, seen := f.clearedAt[m.Frame.CallID]; !seen {
				f.clearedAt[m.Frame.CallID] = len(f.frames[m.Frame.CallID]) - 1
			}
		}
		f.childMu.Unlock()
	}
}

// childCommand sends one control message to the fake child.
func (f *fakeTelnyx) childCommand(m ctrlMsg) {
	f.t.Helper()
	f.childMu.Lock()
	conn := f.ctrlConn
	f.childMu.Unlock()
	if conn == nil {
		f.t.Fatalf("fakeTelnyx: child control connection not established")
	}
	b, err := json.Marshal(m)
	if err != nil {
		f.t.Fatalf("fakeTelnyx: encode ctrl: %v", err)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		f.t.Fatalf("fakeTelnyx: child ctrl write: %v", err)
	}
}

// childSetup tells the fake child to announce the call over C1. Per the
// C1 contract the setup frame is a CHILD -> daemon production; the daemon
// consumes it in the wiring's frame router.
func (f *fakeTelnyx) childSetup(ccid, direction string) {
	f.t.Helper()
	f.childCommand(ctrlMsg{Ctrl: "setup", CallID: ccid, From: "+15550000009", To: "+15550000001", Direction: direction, Codec: "L16", SampleRate: 16000})
}

// childDialAudio tells the fake child to connect the per-call C2 pair.
func (f *fakeTelnyx) childDialAudio(ccid string) {
	f.t.Helper()
	f.childCommand(ctrlMsg{Ctrl: "dial-audio", CallID: ccid})
}

// childSendsPrompt tells the fake child to speak a prompt over C1.
func (f *fakeTelnyx) childSendsPrompt(ccid, text string, last bool) {
	f.t.Helper()
	m := ctrlMsg{Ctrl: "prompt", CallID: ccid, Text: text}
	if last {
		t := true
		m.Last = &t
	}
	f.childCommand(m)
}

// childSendsInterrupt tells the fake child to barge in over C1.
func (f *fakeTelnyx) childSendsInterrupt(ccid string) {
	f.t.Helper()
	f.childCommand(ctrlMsg{Ctrl: "interrupt", CallID: ccid})
}

// awaitChildFrame polls the mirrored frames until one with the event
// lands for ccid (handshake-probe frames, call id prefixed "_", are
// ignored) and returns it.
func (f *fakeTelnyx) awaitChildFrame(ccid, event string, within time.Duration) c1Frame {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fr, ok := f.findFrame(ccid, event); ok {
			return fr
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: no %s frame for %s within %s", event, ccid, within)
	return c1Frame{}
}

// findFrame scans the log for the first frame with the event (skipping
// the handshake probe id).
func (f *fakeTelnyx) findFrame(ccid, event string) (c1Frame, bool) {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	for _, fr := range f.frames[ccid] {
		if fr.Event == event && !strings.HasPrefix(fr.CallID, "_") {
			return fr, true
		}
	}
	return c1Frame{}, false
}

// awaitSayContaining polls until a say frame for ccid carries non-empty
// text (greeting, filler, or the supervisor-allowed reply).
func (f *fakeTelnyx) awaitSayContaining(ccid string, within time.Duration) c1Frame {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		var got *c1Frame
		for i := range f.frames[ccid] {
			fr := f.frames[ccid][i]
			if fr.Event == "say" && strings.TrimSpace(fr.Text) != "" && !strings.HasPrefix(fr.CallID, "_") {
				got = &f.frames[ccid][i]
				break
			}
		}
		f.childMu.Unlock()
		if got != nil {
			return *got
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: no say frame with text for %s within %s", ccid, within)
	return c1Frame{}
}

// awaitClearFrame polls until a clear frame lands for ccid.
func (f *fakeTelnyx) awaitClearFrame(ccid string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		_, ok := f.clearedAt[ccid]
		f.childMu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: no clear frame for %s within %s", ccid, within)
}

// awaitChildStop polls until the fake child's control connection is gone
// (the child process exits on idle stop or daemon shutdown, closing the
// socket). Bounded poll on the real observable.
func (f *fakeTelnyx) awaitChildStop(within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		conn := f.ctrlConn
		f.childMu.Unlock()
		if conn == nil {
			return
		}
		// A dead peer surfaces on the next write; probe with a zero-length
		// write (never a fixed sleep for readiness).
		if _, err := conn.Write(nil); err != nil {
			f.childMu.Lock()
			f.ctrlConn = nil
			f.childMu.Unlock()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: fake child still alive after %s (idle stop did not fire)", within)
}

// sayFrameCount reports how many say frames were mirrored for ccid.
func (f *fakeTelnyx) sayFrameCount(ccid string) int {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	return f.sayCount[ccid]
}

// awaitSayAdvance polls until the say count for ccid exceeds baseline
// (interleaved-progress observable; never a sleep-sized window).
func (f *fakeTelnyx) awaitSayAdvance(ccid string, baseline int, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if f.sayFrameCount(ccid) > baseline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: say stream for %s never advanced past %d within %s", ccid, baseline, within)
}

// sayFrameCountAfterClear reports say frames mirrored for ccid AFTER the
// first clear frame (queued speech dropped for THAT call only).
func (f *fakeTelnyx) sayFrameCountAfterClear(ccid string) int {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	idx, ok := f.clearedAt[ccid]
	if !ok {
		return 0
	}
	n := 0
	for i, fr := range f.frames[ccid] {
		if i > idx && fr.Event == "say" {
			n++
		}
	}
	return n
}

// ---- suite boot plumbing (shared by the suite's tests) ----

// bootVoiceStack starts a scratch daemon with the voice block armed: fake
// Telnyx keypair, fake child binary (child_binary_path), and the secrets
// source the wiring resolves. Returns the stack and the fake child's
// control socket path (the child derives it from --audio-dir as the
// sibling of the sock dir under the sandbox MEEPT_HOME).
func bootVoiceStack(t *testing.T, fake *fakeTelnyx, extra map[string]any) (*harness.Stack, string) {
	t.Helper()
	childBin := buildFakeChild(t, t.TempDir())
	merged := map[string]any{
		"child_binary_path": childBin,
		// The inbound spawner Acquires the C8 slot under the owning
		// employee; empty = spawn refuses with a named error (500).
		"default_employee": "voice-fixture-employee",
	}
	for k, v := range extra {
		merged[k] = v
	}
	opts := []harness.StartOption{
		fake.overlayOptions(merged),
		harness.WithExtraEnv(fake.extraEnv()),
	}
	for k, v := range fake.secretsOverlay() {
		opts = append(opts, overlayOne(k, v))
	}
	s := harness.Start(t, opts...)

	// Boot armed: the wiring log names the voice routes armed. Bounded
	// poll on the log file (readiness observable, no fixed sleep).
	awaitLogLine(t, s, "voice routes armed", 30*time.Second)

	ctrlPath := filepath.Join(s.MeeptHome, "voice", "ctrl.sock")
	return s, ctrlPath
}

// awaitLogLine polls the daemon log for a substring, bounded.
func awaitLogLine(t *testing.T, s *harness.Stack, substr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(s.Daemon.LogPath())
		if err == nil && strings.Contains(string(data), substr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon log never showed %q within %s; tail:\n%s", substr, within, s.Daemon.LogTail())
}

// overlayOne returns a StartOption setting one dotted config key.
func overlayOne(key string, value any) harness.StartOption {
	return harness.WithConfigOverlay(map[string]any{key: value})
}

// Long unix-socket paths fail to bind on macOS (sun_path <= 103 bytes):
// the voice text socket lives under $MEEPT_HOME/voice/sock/, and the
// default macOS TMPDIR (per-user launchd dir) pushes the sandbox path
// past the limit. Shorten the TEST PROCESS's TMPDIR once when needed so
// the harness sandbox roots stay under the bind limit. Per-process env:
// sibling suites in concurrent -p 2 processes are unaffected.
func shortenTmpdirForSockets() {
	if runtime.GOOS != "darwin" {
		return
	}
	dir := os.TempDir()
	const maxSunPath = 103
	probe := filepath.Join(dir, "meept-e2e-1234567890", "home", ".meept", "voice", "sock", "text.sock")
	if len(probe) < 100 {
		return
	}
	short := "/tmp/meept-e2e-voice"
	if err := os.MkdirAll(short, 0o755); err != nil {
		return
	}
	_ = os.Setenv("TMPDIR", short+string(os.PathSeparator)) //nolint:errcheck // best-effort test-env shaping
}

func init() {
	shortenTmpdirForSockets()
}

// assertNoMediaFrame asserts NO frame containing substr lands on the
// call's media websocket within the window (isolation observable).
func (f *fakeTelnyx) assertNoMediaFrame(ccid, substr string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if f.mediaHasFrame(ccid, substr) {
			f.t.Fatalf("fakeTelnyx: unexpected %q frame on %s media socket", substr, ccid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// awaitAction polls the daemon's call-control POSTS recorded at the fake
// API until one for ccid contains substr (bounded; never a fixed sleep).
func (f *fakeTelnyx) awaitAction(ccid, substr string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		found := false
		for _, p := range f.actions {
			if strings.Contains(p, ccid) && strings.Contains(p, substr) {
				found = true
				break
			}
		}
		f.mu.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fakeTelnyx: no %s action for %s within %s", substr, ccid, within)
}
