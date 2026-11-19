//go:build e2e

// Suite voice_clips (gate leaf 00T, plan 20261113-filler-clips-and-context-warm
// phase1-clips): the filler-clip cache end to end, hermetic. Helpers follow
// the voice-lifecycle suite's per-suite duplication convention (Ed25519
// signed webhooks, loopback fake Telnyx, fake child over a control socket),
// with the clipchild stand-in under this suite's testdata (raw-frame mirror
// + clip render/playback; see its header doc for contract provenance) and
// a configurable-delay fake qwen3 TTS endpoint.
//
// Frozen contracts quoted from the root plan:
//
//	C1 clipKey = hex(sha256("v1" \x1f text \x1f vendor \x1f model \x1f voice
//	   \x1f hex(sha256(refHash)))); unit-pinned in
//	   internal/voice/clipstore/clipkey_contract_test.go.
//	C2 store: dir config.MeeptHome()/voice_clips; <key>.pcm raw s16le;
//	   <key>.json meta {key,text,vendor,model,voice,sample_rate,bytes,
//	   created_at}; Has/Get never create the dir; corrupt/short pcm =
//	   delete + miss.
//	C3 frames: say gains "clip_id" omitempty; clip_render {"event":
//	   "clip_render","call_id",clips:[{key,text}] cap 32}; clip_ready
//	   {"event":"clip_ready","call_id","key","ok"}. All additive; a frame
//	   without them == today's exact JSON.
//	C5 lanes: greeting/filler/silence/fragment/farewell pools; farewell
//	   default ["Goodbye!","Bye!","Talk soon."].
package voiceclips

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
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

const webhookPath = "/api/v1/voice/webhook"

// ---- fake qwen3 TTS endpoint (configurable synthesis delay) ----

// fakeTTS is the hermetic OpenAI-compatible /v1/audio/speech stand-in.
// delay controls per-request synth latency (the 2 s slow lane proves
// cached filler fires from the store, not synth). Counts requests per
// input text.
type fakeTTS struct {
	t     *testing.T
	srv   *httptest.Server
	mu    sync.Mutex
	delay time.Duration
	seen  map[string]int
	total int
}

func newFakeTTS(t *testing.T) *fakeTTS {
	t.Helper()
	f := &fakeTTS{t: t, seen: map[string]int{}}
	mux := http.NewServeMux()
	// The qwen3 vendor speaks /v1/audio/speech/pcm-stream with a
	// length-prefixed frame stream; the legacy cloud vendor speaks
	// /v1/audio/speech with raw PCM. Serve both from one handler so the
	// fake keeps working after the pcm-stream switch (root cause of the
	// pre-existing voice_clips e2e red: the fake 404'd the pcm route).
	synth := func(w http.ResponseWriter, r *http.Request, pcmStream bool) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck // test sink
		input, _ := body["input"].(string)
		if input == "" {
			input, _ = body["text"].(string) // qwen3 pcm-stream body shape
		}
		f.mu.Lock()
		f.seen[input]++
		f.total++
		delay := f.delay
		f.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "audio/pcm")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 400 ms of constant-sample s16le in 20 flushes (streaming).
		for i := 0; i < 20; i++ {
			chunk := make([]byte, 1920)
			for s := 0; s < 960; s++ {
				binary.LittleEndian.PutUint16(chunk[s*2:], uint16(3000+i))
			}
			if pcmStream {
				// [4-byte BE uint32 length][s16le PCM] frames,
				// zero-length terminator (pcmstream.go C1 contract).
				var frame [4 + 1920]byte
				binary.BigEndian.PutUint32(frame[:4], uint32(len(chunk)))
				copy(frame[4:], chunk)
				chunk = frame[:]
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if pcmStream {
			_, _ = w.Write([]byte{0, 0, 0, 0}) //nolint:errcheck // test sink
		}
	}
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		synth(w, r, false)
	})
	mux.HandleFunc("/v1/audio/speech/pcm-stream", func(w http.ResponseWriter, r *http.Request) {
		synth(w, r, true)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTTS) baseURL() string { return f.srv.URL }

func (f *fakeTTS) setDelay(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delay = d
}

func (f *fakeTTS) totalCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

// ---- fake Telnyx + clipchild control mirror ----

// mirroredLine is one daemon->child C1 line the clipchild mirrored
// verbatim (raw key of its ctrl frame message).
type mirroredLine struct {
	CallID string
	Raw    string // verbatim wire line (keys values redacted by the child)
}

type ctrlMsg struct {
	Ctrl      string `json:"ctrl"`
	CallID    string `json:"call_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Last      *bool  `json:"last,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Direction string `json:"direction,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

type fakeTelnyx struct {
	t     *testing.T
	stack *harness.Stack
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	srv   *httptest.Server

	mu       sync.Mutex
	actions  []string
	recorded []recWebhook

	childMu   sync.Mutex
	ctrlConn  net.Conn
	frames    []mirroredLine // ordered daemon->child mirror
	childLogs []ctrlMsg      // ordered child->suite log msgs
	ctrlDiag  []string       // ctrl msgs that parsed but matched no case
}

type recWebhook struct {
	Event  string
	CCID   string
	Status int
}

func newFakeTelnyx(t *testing.T) *fakeTelnyx {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("fakeTelnyx: generate keypair: %v", err)
	}
	f := &fakeTelnyx{t: t, pub: pub, priv: priv}
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

func (f *fakeTelnyx) publicKeyB64() string {
	return base64.StdEncoding.EncodeToString(f.pub)
}

func (f *fakeTelnyx) signHeaders(msg []byte) http.Header {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	signed := append(append([]byte(ts), '|'), msg...)
	sig := ed25519.Sign(f.priv, signed)
	h := http.Header{}
	h.Set("Telnyx-Signature-Ed25519", base64.StdEncoding.EncodeToString(sig))
	h.Set("Telnyx-Timestamp", ts)
	return h
}

func (f *fakeTelnyx) extraEnv() map[string]string {
	return map[string]string{
		"MEEPT_TELNYX_TEST_BASE_URL": f.srv.URL,
		"MEEPT_E2E_TELNYX_API_KEY":   "fake-telnyx-key",
	}
}

func (f *fakeTelnyx) secretsOverlay() map[string]any {
	return map[string]any{
		"secrets.sources.telnyx_api_key": map[string]any{
			"kind": "env",
			"name": "MEEPT_E2E_TELNYX_API_KEY",
		},
	}
}

// overlayOptions: the shared [voice] block. Extra rides as top-level
// voice keys (same convention as the lifecycle suite).
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
		voice["from_number"] = "+155****0001"
		voice["idle_stop_seconds"] = 600 // keep the shared child warm across calls
		voice["supervisor_timeout_ms"] = 400
		voice["onnx_runtime_lib"] = ""
		voice["supervisor_enabled"] = false
		addr := ""
		if tr, ok := cfg["transport"].(map[string]any); ok {
			if h, ok := tr["http"].(map[string]any); ok {
				if a, ok := h["addr"].(string); ok {
					addr = a
				}
			}
		}
		if addr == "" {
			f.t.Fatal("fakeTelnyx: overlay could not resolve the daemon http addr")
		}
		voice["public_stream_url"] = "wss://" + addr + "/api/v1/voice/media"
		for k, v := range extra {
			voice[k] = v
		}
	})
}

func (f *fakeTelnyx) postWebhook(s *harness.Stack, eventType, ccid, direction string) int {
	f.t.Helper()
	body := callEventBody(eventType, ccid, direction)
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+webhookPath, strings.NewReader(string(body)))
	if err != nil {
		f.t.Fatalf("webhook request: %v", err)
	}
	for k, vs := range f.signHeaders(body) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		f.t.Fatalf("webhook post %s for %s: %v", eventType, ccid, err)
	}
	defer resp.Body.Close() //nolint:errcheck // bounded
	status := resp.StatusCode
	f.mu.Lock()
	f.recorded = append(f.recorded, recWebhook{Event: eventType, CCID: ccid, Status: status})
	f.mu.Unlock()
	return status
}

func (f *fakeTelnyx) deliverCallInitiated(s *harness.Stack, ccid, direction string) {
	f.t.Helper()
	if status := f.postWebhook(s, "call.initiated", ccid, direction); status != http.StatusOK {
		f.t.Fatalf("call.initiated for %s answered %d, want 200; daemon tail:\n%s", ccid, status, s.Daemon.LogTail())
	}
}

func (f *fakeTelnyx) deliverHangup(s *harness.Stack, ccid string) {
	f.t.Helper()
	if status := f.postWebhook(s, "call.hangup", ccid, "incoming"); status != http.StatusOK {
		f.t.Fatalf("call.hangup for %s answered %d, want 200", ccid, status)
	}
}

func callEventBody(eventType, ccid, direction string) []byte {
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"event_type": eventType,
			"payload": map[string]any{
				"call_control_id": ccid,
				"call_session_id": "sess-" + ccid,
				"from":            "+155****0009",
				"to":              "+155****0001",
				"direction":       direction,
			},
		},
	})
	if err != nil {
		return []byte("{}")
	}
	return body
}

// dialMedia dials the daemon's media websocket (the child's .out pair only
// exists once media is armed, exactly like the lifecycle suites).
func (f *fakeTelnyx) dialMedia(s *harness.Stack, ccid string) {
	f.t.Helper()
	host := strings.TrimPrefix(s.HTTPBaseURL(), "https://")
	streamURL := "wss://" + host + "/api/v1/voice/media"
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, streamURL, &websocket.DialOptions{
		HTTPClient: insecureClient(),
		HTTPHeader: f.signHeaders([]byte(streamURL)),
	})
	dialCancel()
	if err != nil {
		f.t.Fatalf("dial media ws for %s: %v", ccid, err)
	}
	f.t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") }) //nolint:errcheck // teardown
	writeWS(conn, `{"event":"connected"}`)
	start := fmt.Sprintf(`{"event":"start","start":{"call_control_id":%q,"media_format":{"encoding":"PCMU","sample_rate":8000}}}`, ccid)
	writeWS(conn, start)
	go func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer rcancel()
		for {
			if _, _, err := conn.Read(rctx); err != nil {
				return
			}
		}
	}()
}

func writeWS(conn *websocket.Conn, payload string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		panic("writeWS: " + err.Error()) //nolint:forbidigo // test helper
	}
}

// connectChild attaches the suite observer to the clipchild's control
// socket.
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
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		f.t.Fatalf("clipchild control socket %s never appeared within %s", socketPath, within)
	}
	f.childMu.Lock()
	f.ctrlConn = conn
	f.childMu.Unlock()
	go f.readCtrl(conn)
}

func (f *fakeTelnyx) readCtrl(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var m ctrlMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		f.childMu.Lock()
		switch m.Ctrl {
		case "frame":
			f.frames = append(f.frames, mirroredLine{CallID: m.CallID, Raw: m.Raw})
		case "log":
			f.childLogs = append(f.childLogs, m)
		default:
			f.ctrlDiag = append(f.ctrlDiag, string(sc.Bytes()))
		}
		f.childMu.Unlock()
	}
}

func (f *fakeTelnyx) childCommand(m ctrlMsg) {
	f.t.Helper()
	f.childMu.Lock()
	conn := f.ctrlConn
	f.childMu.Unlock()
	if conn == nil {
		f.t.Fatalf("clipchild control connection not established")
	}
	b, err := json.Marshal(m)
	if err != nil {
		f.t.Fatalf("encode ctrl: %v", err)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		f.t.Fatalf("child ctrl write: %v", err)
	}
}

func (f *fakeTelnyx) armCall(ccid string) {
	f.t.Helper()
	f.childCommand(ctrlMsg{Ctrl: "setup", CallID: ccid, From: "+155****0009", To: "+155****0001", Direction: "inbound"})
	f.childCommand(ctrlMsg{Ctrl: "dial-audio", CallID: ccid})
}

// childPrompt sends a final prompt frame (last:true) through the child.
func (f *fakeTelnyx) childPrompt(ccid, text string) {
	last := true
	f.childCommand(ctrlMsg{Ctrl: "prompt", CallID: ccid, Text: text, Last: &last})
}

// awaitMirrored polls the ordered mirror until one line for ccid contains
// substr; returns the mirror index.
func (f *fakeTelnyx) awaitMirrored(ccid, substr string, within time.Duration) int {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		for i, ln := range f.frames {
			if ln.CallID == ccid && strings.Contains(ln.Raw, substr) {
				f.childMu.Unlock()
				return i
			}
		}
		f.childMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	dump := f.dumpFrames(ccid)
	f.childMu.Lock()
	var logs strings.Builder
	for _, m := range f.childLogs {
		logs.WriteString(m.Text)
		logs.WriteString("\n")
	}
	f.childMu.Unlock()
	f.t.Fatalf("mirrored %s lines never carried %q within %s; frames:\n%s\nALL frames:\n%s\nchild logs:\n%s\ndaemon tail:\n%s", ccid, substr, within, dump, f.dumpFrames(""), logs.String(), f.tail())
	return -1
}

func (f *fakeTelnyx) dumpFrames(ccid string) string {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	var b strings.Builder
	for _, ln := range f.frames {
		if ccid == "" || ln.CallID == ccid {
			b.WriteString(ln.Raw)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// awaitChildLog polls the child's log lines until one for ccid contains
// substr; returns the log text.
func (f *fakeTelnyx) awaitChildLog(ccid, substr string, within time.Duration) string {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		for _, m := range f.childLogs {
			if m.CallID == ccid && strings.Contains(m.Text, substr) {
				txt := m.Text
				f.childMu.Unlock()
				return txt
			}
		}
		f.childMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("clipchild logs for %s never showed %q within %s", ccid, substr, within)
	return ""
}

// tail returns the daemon log tail for failure diagnostics.
func (f *fakeTelnyx) tail() string {
	if f.stack == nil {
		return "(no stack)"
	}
	data, err := os.ReadFile(f.stack.Daemon.LogPath())
	if err != nil {
		return f.stack.Daemon.LogTail()
	}
	return string(data)
}

// ---- suite boot plumbing ----

// buildClipChild compiles this suite's testdata/clipchild.
func buildClipChild(t *testing.T, dir string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("clipchild cwd: %v", err)
	}
	src := filepath.Join(cwd, "testdata", "clipchild", "main.go")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("clipchild source: %v", err)
	}
	out := filepath.Join(dir, "clipchild")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build clipchild: %v", err)
	}
	return out
}

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

// shortenTmpdirForSockets keeps sandbox roots under the macOS sun_path
// limit (same per-suite helper as the sibling voice suites).
func shortenTmpdirForSockets() {
	if runtime.GOOS != "darwin" {
		return
	}
	dir := os.TempDir()
	probe := filepath.Join(dir, "meept-e2e-1234567890", "home", ".meept", "voice", "sock", "text.sock")
	if len(probe) < 100 {
		return
	}
	short := "/tmp/mv"
	if err := os.MkdirAll(short, 0o755); err != nil {
		return
	}
	_ = os.Setenv("TMPDIR", short+string(os.PathSeparator)) //nolint:errcheck // best-effort test-env shaping
}

func init() { shortenTmpdirForSockets() }

// bootClipStack starts a scratch daemon with the clip-era voice block:
// clipchild binary, qwen3 pointed at the fake TTS endpoint, and the
// pool overlay the tests pass.
func bootClipStack(t *testing.T, fake *fakeTelnyx, tts *fakeTTS, pool map[string]any) (*harness.Stack, string) {
	t.Helper()
	childBin := buildClipChild(t, t.TempDir())
	voiceExtras := map[string]any{
		"child_binary_path": childBin,
		"default_employee":  "voice-fixture-employee",
		"tts_provider":      "qwen3",
		"tts_model":         "Qwen/Qwen3-TTS-test",
		"tts_base_url":      tts.baseURL(),
		"tts_voice":         "vivian",
		"stt_provider":      "deepgram",
		"stt_model":         "nova-3",
	}
	for k, v := range pool {
		voiceExtras[k] = v
	}
	opts := []harness.StartOption{
		fake.overlayOptions(voiceExtras),
		harness.WithExtraEnv(fake.extraEnv()),
	}
	for k, v := range fake.secretsOverlay() {
		opts = append(opts, harness.WithConfigOverlay(map[string]any{k: v}))
	}
	s := harness.Start(t, opts...)
	fake.mu.Lock()
	fake.stack = s
	fake.mu.Unlock()
	awaitLogLine(t, s, "voice routes armed", 30*time.Second)
	ctrlPath := filepath.Join(s.MeeptHome, "voice", "ctrl.sock")
	return s, ctrlPath
}

// prewarmCall runs a scenario's spawn + attach + arm sequence in the
// PROVEN voice-lifecycle order: a throwaway warm call spawns the shared
// child, hangup tears it down to idle (child survives: idle_stop_seconds
// is long in this suite), the ctrl mirror attaches to that child, and
// then the scenario's real call (ccid) arms. Attaching the mirror after
// the spawn (vs. racing the spawn on the first call) is what makes the
// daemon->child frame stream observable from the very first keys line.
func prewarmCall(t *testing.T, s *harness.Stack, fake *fakeTelnyx, ctrlPath, warmCcid, ccid string) {
	t.Helper()
	fake.deliverCallInitiated(s, warmCcid, "incoming")
	fake.dialMedia(s, warmCcid)
	awaitLogLine(t, s, "voice media: start frame", 20*time.Second)
	fake.deliverHangup(s, warmCcid)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)
	fake.connectChild(ctrlPath, 20*time.Second)
	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 20*time.Second)
	fake.armCall(ccid)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 20*time.Second)
}

// clipsDir is the contract C2 store path under the stack's MEEPT_HOME.
func clipsDir(s *harness.Stack) string { return filepath.Join(s.MeeptHome, "voice_clips") }

// storedClipKeys lists the .pcm keys currently in the store dir (nil, no
// error if the dir does not exist).
func storedClipKeys(s *harness.Stack) (map[string]bool, error) {
	dir := clipsDir(s)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".pcm") {
			out[strings.TrimSuffix(name, ".pcm")] = true
		}
	}
	return out, nil
}

func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
}

// clipGateKeyViaStdlib computes the FROZEN C1 clipKey with stdlib only,
// independent of the clipstore package:
//
//	clipKey = hex(sha256("v1" \x1f text \x1f vendor \x1f model \x1f voice
//	                       \x1f hex(sha256(refHash))))
//
// refHash = nil (preset voices => hash of zero bytes).
func clipGateKeyViaStdlib(text, vendor, model, voice string) string {
	inner := sha256.Sum256(nil)
	preimage := "v1" + "\x1f" + text + "\x1f" + vendor + "\x1f" + model + "\x1f" + voice + "\x1f" + hex.EncodeToString(inner[:])
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])
}
