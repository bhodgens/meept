//go:build e2e

// Suite voice-local (local-leg plan leaf 11, T1+T2): the whole LOCAL leg,
// hermetic end to end - RPC voice.local.open, the client goroutine talking
// raw PCM over the returned C2 pair paths, FAKE whisper-cli / piper
// subprocess shims on PATH (leaf-10 adapters run as real subprocesses
// against them), a scripted FakeLLM turn, paced PCM drained from OutPath,
// voice.local.end, and the post-conditions (registry empty + calls.db row
// with transcripts).
//
// The fake child is built in REAL-AUDIO mode (-ldflags -X
// main.realAudio=true): its C2 mic pump feeds client PCM through the REAL
// providers.NewSTT(whisper) adapter (segmented, one final per utterance),
// and its say mirror drives the REAL providers.NewTTS(piper) adapter so
// daemon says land as actual PCM on the .out socket - the same adapters
// cmd/meept-voice installs, fakes only at the subprocess boundary.
//
// No live vendor, no real mic, no network beyond loopback (fake LLM +
// black-hole Telnyx base URL), no model files on disk beyond the empty
// marker files the shims ignore.

package voicelocal

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
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

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon): calls.db reads

	"github.com/caimlas/meept/e2e/harness"
)

// ---- fake whisper-cli / piper subprocess shims (leaf-10 boundaries) ----

// writeFakeWhisper installs a whisper-cli shim in binDir: it prints a
// deterministic transcript per invocation (the -f WAV it received is
// drained to /dev/null; content identity is the whisper unit tests' job).
func writeFakeWhisper(t *testing.T, binDir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"cat \"$4\" > /dev/null 2>&1 || true\n" +
		"echo \"local utterance one\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "whisper-cli"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake whisper-cli: %v", err)
	}
}

// writeFakePiper installs a piper shim: raw s16le PCM on stdout (the
// --output-raw contract), deterministic non-zero samples, sized so the
// splice paces several 20 ms chunks out of one synthesis.
func writeFakePiper(t *testing.T, binDir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		// 80 ms of tone: 1280 samples s16le = 2560 bytes @16k. Reads its
		// stdin (the text) so the writer goroutine never blocks forever.
		"cat > /dev/null\n" +
		"awk 'BEGIN{for(i=0;i<1280;i++){printf \"%c%c\", i%251, (i*7)%251}}' | head -c 2560\n"
	if err := os.WriteFile(filepath.Join(binDir, "piper"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake piper: %v", err)
	}
}

// writePiperModel lays down a piper model.onnx + sidecar config declaring
// audio.sample_rate=16000 (the negotiated rate; no resample in play).
func writePiperModel(t *testing.T, dir string) string {
	t.Helper()
	onnx := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(onnx, []byte("fake-onnx"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	conf := `{"audio": {"sample_rate": 16000}}`
	if err := os.WriteFile(filepath.Join(dir, "model.json"), []byte(conf), 0o600); err != nil {
		t.Fatalf("write model config: %v", err)
	}
	return onnx
}

// shimPath prepends a shim dir to the TEST process PATH (inherited by the
// daemon -> child chain: the daemon spawns the child with the inherited
// environment because voice.onnx_runtime_lib is empty in these stacks).
func shimPath(t *testing.T) (binDir, modelDir string) {
	t.Helper()
	binDir = t.TempDir()
	modelDir = t.TempDir()
	writeFakeWhisper(t, binDir)
	writeFakePiper(t, binDir)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return binDir, modelDir
}

// ---- fake Qwen3-free loopback: nothing cloud here; the local suite's
// boot reuses the fakeTelnyx helper shape (per-suite copy, tier rule) but
// never speaks to Telnyx beyond the warm-call arm that spawns the child. ----

// c1Frame mirrors the C1 text-socket frame the suite asserts on (wire
// keys per contract C1).
type c1Frame struct {
	CallID string `json:"call_id"`
	Event  string `json:"event"`
	Text   string `json:"text,omitempty"`
	Last   *bool  `json:"last,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ctrlMsg is the fake-child control-socket message shape.
type ctrlMsg struct {
	Ctrl   string   `json:"ctrl"`
	CallID string   `json:"call_id,omitempty"`
	Text   string   `json:"text,omitempty"`
	Note   string   `json:"note,omitempty"` // speak-mic: base64 PCM payload
	Last   *bool    `json:"last,omitempty"`
	Frame  *c1Frame `json:"frame,omitempty"`
}

type fakeTelnyx struct {
	t *testing.T

	pub  ed25519.PublicKey
	priv ed25519.PrivateKey

	// srv is the fake call-control API the daemon's telnyx client POSTs
	// against (MEEPT_TELNYX_TEST_BASE_URL): every /calls/... action is
	// recorded and answered 200, no live vendor. The warm call's answer
	// needs a real endpoint; the local leg itself never touches it.
	srv *httptest.Server

	mu       sync.Mutex
	logs     []c1Frame // mirrored control frames (log + prompt + say + ...)
	actions  []string
	childMu  sync.Mutex
	ctrlConn net.Conn
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

// publicKeyB64 is the base64 Ed25519 public key the daemon verifies with.
func (f *fakeTelnyx) publicKeyB64() string {
	return base64.StdEncoding.EncodeToString(f.pub)
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

// baseURL is the fake call-control API origin the daemon's telnyx client
// POSTs against (wired through MEEPT_TELNYX_TEST_BASE_URL).
func (f *fakeTelnyx) baseURL() string { return f.srv.URL }

// extraEnv carries the hermetic telnyx API base override + a fake key (the
// wiring refuses to arm without a broker-resolvable telnyx_api_key).
func (f *fakeTelnyx) extraEnv() map[string]string {
	return map[string]string{
		"MEEPT_TELNYX_TEST_BASE_URL": f.baseURL(),
		"MEEPT_E2E_TELNYX_API_KEY":   "fake-telnyx-key",
	}
}

// overlayOptions merges extra into the [voice] block (config hook; see
// the lifecycle suite's overlayOptions for the dotted-overlay caveat).
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
		voice["filler"] = []string{"One moment."}
		voice["idle_stop_seconds"] = 3
		voice["supervisor_timeout_ms"] = 400
		voice["onnx_runtime_lib"] = ""
		voice["supervisor_enabled"] = false
		// The warm call's answer needs a stream URL; the local suite never
		// dials media, so any daemon-reachable wss origin satisfies the
		// manager's "public media origin configured" gate. Resolved from
		// the pinned http addr like the lifecycle suite's overlay does.
		addr := ""
		if tr, ok := cfg["transport"].(map[string]any); ok {
			if h, ok := tr["http"].(map[string]any); ok {
				if a, ok := h["addr"].(string); ok {
					addr = a
				}
			}
		}
		if addr == "" {
			f.t.Fatal("fakeTelnyx: overlay could not resolve the daemon http addr from the harness config")
		}
		voice["public_stream_url"] = "wss://" + addr + "/api/v1/voice/media"
		for k, v := range extra {
			voice[k] = v
		}
	})
}

// secretsOverlay declares the telnyx_api_key env secret source.
func (f *fakeTelnyx) secretsOverlay() map[string]any {
	return map[string]any{
		"secrets.sources.telnyx_api_key": map[string]any{
			"kind": "env",
			"name": "MEEPT_E2E_TELNYX_API_KEY",
		},
	}
}

// connectChild dials the fake child's control socket and mirrors frames.
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
		f.t.Fatalf("control socket %s never appeared within %s", socketPath, within)
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
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m.Frame == nil {
			continue
		}
		f.mu.Lock()
		f.logs = append(f.logs, *m.Frame)
		f.mu.Unlock()
	}
}

// awaitLog polls the mirrored control logs until one contains substr.
func (f *fakeTelnyx) awaitLog(ccid, substr string, within time.Duration) string {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, fr := range f.controlLogs(ccid) {
			if strings.Contains(fr.Text, substr) {
				return fr.Text
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("child control logs for %s never showed %q within %s; logs: %v",
		ccid, substr, within, f.controlLogs(ccid))
	return ""
}

// controlLogs returns every mirrored log frame for ccid.
func (f *fakeTelnyx) controlLogs(ccid string) []c1Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []c1Frame
	for _, fr := range f.logs {
		if fr.CallID == ccid && fr.Event == "log" && fr.Text != "" {
			out = append(out, fr)
		}
	}
	return out
}

// childCommand sends one control message to the fake child.
func (f *fakeTelnyx) childCommand(m ctrlMsg) {
	f.t.Helper()
	f.childMu.Lock()
	conn := f.ctrlConn
	f.childMu.Unlock()
	if conn == nil {
		f.t.Fatalf("child control connection not established")
	}
	b, err := json.Marshal(m)
	if err != nil {
		f.t.Fatalf("encode ctrl: %v", err)
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		f.t.Fatalf("child ctrl write: %v", err)
	}
}

// ---- suite boot plumbing ----

// buildRealAudioChild compiles testdata/fakechild in REAL-AUDIO mode.
func buildRealAudioChild(t *testing.T, dir string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("fake child cwd: %v", err)
	}
	src := filepath.Join(filepath.Dir(cwd), "voice-lifecycle", "testdata", "fakechild", "main.go")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fake child source: %v", err)
	}
	out := filepath.Join(dir, "fakechild-real-audio")
	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.realAudio=true -X main.realProviders=true",
		"-o", out, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build fakechild (real audio): %v", err)
	}
	return out
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

// shortenTmpdirForSockets keeps harness sandbox roots under the macOS
// sun_path limit (same per-suite helper as the sibling voice suites).
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

func init() {
	shortenTmpdirForSockets()
}

// bootLocalStack starts a scratch daemon with the local leg enabled, the
// real-audio fake child, and the subprocess shims on the daemon's PATH.
func bootLocalStack(t *testing.T, fake *fakeTelnyx, modelPath string) (*harness.Stack, string) {
	t.Helper()
	childBin := buildRealAudioChild(t, t.TempDir())
	voiceExtras := map[string]any{
		"child_binary_path": childBin,
		"default_employee":  "voice-fixture-employee",
		// CL4 per-leg config: the local leg resolves whisper/piper with
		// the shim model paths; TTS base URL stays empty (piper needs no
		// endpoint). The local block rides as a NESTED object (dotted
		// overlays would replace [voice] whole).
		"stt_provider": "whisper",
		"stt_model":    "/tmp/fake-whisper-model.bin",
		"tts_provider": "piper",
		"tts_model":    modelPath,
		"local": map[string]any{
			"enabled":      true,
			"barge_in_rms": 3000,
		},
	}
	opts := []harness.StartOption{
		fake.overlayOptions(voiceExtras),
		harness.WithExtraEnv(fake.extraEnv()),
	}
	for k, v := range fake.secretsOverlay() {
		opts = append(opts, harness.WithConfigOverlay(map[string]any{k: v}))
	}
	s := harness.Start(t, opts...)
	awaitLogLine(t, s, "voice routes armed", 30*time.Second)
	awaitLogLine(t, s, "voice wiring: local leg armed", 15*time.Second)
	ctrlPath := filepath.Join(s.MeeptHome, "voice", "ctrl.sock")
	return s, ctrlPath
}

// rpcCreateEmployee registers the fixture employee through agents.create
// (the real voice.local.open gate checks the employee store).
func rpcCreateEmployee(t *testing.T, s *harness.Stack, employee string) {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	created := client.CallResult("agents.create", map[string]any{
		"id":      employee,
		"name":    employee,
		"prompt":  "You are " + employee + ", an e2e local voice probe.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       "e2e local voice probe",
			"role":          "e2e local voice probe",
			"charter":       "Answer the caller briefly.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "medium",
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{},
			},
			"version":     1,
			"authored_by": "user",
		},
	})
	if created["id"] != employee {
		t.Fatalf("agents.create id = %v, want %s", created["id"], employee)
	}
}

// rpcOpenLocalCall opens a local call over the REAL voice.local.open RPC.
func rpcOpenLocalCall(t *testing.T, s *harness.Stack, employee, label string) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	open := client.CallResult("voice.local.open", map[string]any{
		"employee_id": employee,
		"label":       label,
	})
	for _, k := range []string{"call_id", "in_path", "out_path", "session_id"} {
		if v, _ := open[k].(string); v == "" {
			t.Fatalf("voice.local.open returned empty %s: %v", k, open)
		}
	}
	return open
}

// rpcErrorMessage extracts the message from an error envelope.
func rpcErrorMessage(t *testing.T, method string, resp map[string]any) string {
	t.Helper()
	e, ok := resp["error"]
	if !ok || e == nil {
		t.Fatalf("%s: expected an error envelope, got result: %v", method, resp["result"])
	}
	em, ok := e.(map[string]any)
	if !ok {
		return fmt.Sprint(e)
	}
	if m, ok := em["message"].(string); ok && m != "" {
		return m
	}
	return fmt.Sprint(em)
}

// localClient is the raw local-leg audio client: it dials the call's C2
// pair sockets exactly like the CLI's file mode.
type localClient struct {
	mic     net.Conn
	speaker net.Conn
}

func dialLocalClient(t *testing.T, open map[string]any) *localClient {
	t.Helper()
	inPath, _ := open["in_path"].(string)
	outPath, _ := open["out_path"].(string)
	var mic, speaker net.Conn
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c1, e1 := net.Dial("unix", inPath)
		c2, e2 := net.Dial("unix", outPath)
		if e1 == nil && e2 == nil {
			mic, speaker = c1, c2
			break
		}
		if c1 != nil {
			_ = c1.Close() //nolint:errcheck // retry path
		}
		if c2 != nil {
			_ = c2.Close() //nolint:errcheck // retry path
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mic == nil || speaker == nil {
		t.Fatalf("local client could not dial the pair (%s / %s) within 10s", inPath, outPath)
	}
	t.Cleanup(func() {
		_ = mic.Close()     //nolint:errcheck // teardown
		_ = speaker.Close() //nolint:errcheck // teardown
	})
	return &localClient{mic: mic, speaker: speaker}
}

// scriptedUtteranceB64 builds one scripted utterance for the speak-mic
// control: loud s16le PCM (well over the whisper shim's silence gate)
// followed by >500 ms of true silence (the leaf-10 trailing-silence
// flush), base64-encoded for the ctrl envelope. At 16k: 320 ms of tone +
// 600 ms of silence.
func scriptedUtteranceB64() string {
	const rate = 16000
	tone := make([]byte, rate*320/1000*2)
	for i := 0; i < len(tone)/2; i++ {
		binary.LittleEndian.PutUint16(tone[i*2:], uint16(2000+i%400))
	}
	silence := make([]byte, rate*600/1000*2)
	return base64.StdEncoding.EncodeToString(append(tone, silence...))
}

// speakMic drains the client's speaker dial in the background so the pair
// stays joined (the splice's join gate needs first-byte flow on the
// client-facing surface; see the SCOPE NOTE on the delegation topology).
func (c *localClient) speakMic() {
	buf := make([]byte, 8*1024)
	for {
		if _, err := c.speaker.Read(buf); err != nil {
			return
		}
	}
}

// TestVoiceLocalEndToEnd runs the whole local leg hermetically (T1):
// voice.local.open -> pair paths -> scripted client PCM through the REAL
// whisper adapter (fake subprocess shim) -> fake-runner employee turn ->
// say -> the REAL piper adapter synthesizes -> PCM onto C2 .out -> the
// daemon splice relays it -> voice.local.end -> registry empty + calls.db
// row with transcripts.
//
// SCOPE NOTE (honest): the suite's "client writes scripted PCM" hop rides
// the fake child's speak-mic control. The landed AudioPair client surface
// delegates BOTH client directions to the .out accept (internal/voice/
// socket.go ReadFromClient -> ReadFromChild -> p.out), so a client-side
// .in dial cannot reach the daemon's mic relay while the child owns the
// .in accept - the production topology answerOpen pins. Everything else is
// the REAL path: the whisper adapter runs as a real subprocess (fake
// whisper-cli shim), the piper adapter synthesizes for real (fake piper
// shim), the daemon's splice and talk loop relay and gate the audio, and
// the call store row lands through the real binding. The client-surface
// delegation gap is a cross-leaf finding for the local-leg tree owner.
func TestVoiceLocalEndToEnd(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootLocalStack(t, fake, modelPath)

	const (
		employee = "emp-local-e2e"
		label    = "e2ebox"
	)
	rpcCreateEmployee(t, s, employee)

	// Script the employee turn by prompt content: the whisper shim's
	// fixed transcript ("local utterance one") becomes the caller text;
	// the turn reply must be exactly this marker so the say path proves
	// the reply (not the greeting) was synthesized.
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("local utterance one")),
		harness.TextResponse("PIPERMARKER the shipment is on Friday."),
	)

	// Arm the shared child ONCE with a throwaway phone call (the manager's
	// spawn seam is the only EnsureRunning caller), so the control mirror
	// can attach before the local call's frames flow.
	const warmCcid = "cc-local-warm"
	spawnChildViaPhoneWarmCall(t, s, fake, warmCcid)
	fake.connectChild(ctrlPath, 15*time.Second)

	// 1. RPC open: the pair paths come back and the session is bound.
	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	// The child answered the open frame with setup and the daemon emitted
	// the keys line: the local vendors construct from it (keyless D2).
	fake.awaitLog(ccid, "fakechild keys received stt_vendor=whisper tts_vendor=piper", 15*time.Second)
	fake.awaitLog(ccid, "fakechild tts constructed", 15*time.Second)
	fake.awaitLog(ccid, "fakechild stt armed vendor=whisper", 15*time.Second)

	// 2. Client writes scripted PCM (see the SCOPE NOTE): loud s16le tone
	// plus >500 ms of silence - the leaf-10 trailing-silence flush. The
	// REAL whisper adapter segments it and emits ONE final.
	client := dialLocalClient(t, open)
	go client.speakMic()
	fake.childCommand(ctrlMsg{Ctrl: "speak-mic", CallID: ccid, Note: scriptedUtteranceB64()})
	// The final prompt rode C1 into the talk loop (mirrored by the fake
	// child's transcript consumer): a last:true prompt with the shim's
	// fixed transcript.
	awaitPromptFrame(t, fake, ccid, "local utterance one", true, 20*time.Second)
	// And the employee turn RAN on it (fake-runner request log).
	awaitExecutorPrompt(t, s, "local utterance one", 30*time.Second)

	// 3. Say path: the reply flows back as a say; realAudio mode drives
	// the REAL piper adapter; its PCM lands on the C2 .out socket and the
	// daemon splice relays it.
	awaitSayFrame(t, fake, ccid, "PIPERMARKER", 20*time.Second)
	fake.awaitLog(ccid, "fakechild say drive wrote", 15*time.Second)

	// 4. voice.local.end through the same RPC surface.
	endClient := harness.DialRPC(t, s.SocketPath)
	end := endClient.CallResult("voice.local.end", map[string]any{"call_id": ccid})
	if end["ok"] != true {
		t.Fatalf("voice.local.end result = %v, want ok:true", end)
	}
	awaitLogLine(t, s, "voice call ended", 15*time.Second)

	// 5. Post-conditions: registry empty; status lists no calls.
	statusClient := harness.DialRPC(t, s.SocketPath)
	status := statusClient.CallResult("voice.local.status", map[string]any{})
	rows, _ := status["calls"].([]any)
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if id, _ := row["call_id"].(string); id == ccid {
			t.Fatalf("voice.local.status still lists %s after end: %v", ccid, status)
		}
	}

	// 6. calls.db row for ccid with both transcript lines (caller +
	// assistant), direction "local", target "local:e2ebox".
	assertCallRow(t, s, ccid, employee, "local:e2ebox",
		"local utterance one", "PIPERMARKER the shipment is on Friday.")
}

// spawnChildViaPhoneWarmCall runs one throwaway inbound call so the child
// spawns (EnsureRunning's only production caller), then hangs it up. The
// observable is the child's control socket (the child derives it from its
// --audio-dir), the same bounded poll the bargein suite uses - the talk
// loop never arms on a media-less warm call (no open frame, no setup).
func spawnChildViaPhoneWarmCall(t *testing.T, s *harness.Stack, fake *fakeTelnyx, warmCcid string) string {
	t.Helper()
	deliverCallInitiatedLocal(t, s, fake, warmCcid)
	ctrlPath := filepath.Join(s.MeeptHome, "voice", "ctrl.sock")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(ctrlPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake child control socket %s never appeared after the warm call", ctrlPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
	deliverHangupLocal(t, s, fake, warmCcid)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)
	return ctrlPath
}

// deliverCallInitiatedLocal posts a signed incoming call.initiated webhook
// into the daemon's voice route (Ed25519 over "{ts}|{body}").
func deliverCallInitiatedLocal(t *testing.T, s *harness.Stack, fake *fakeTelnyx, ccid string) {
	t.Helper()
	body := callEventBody("call.initiated", ccid, "incoming")
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+"/api/v1/voice/webhook", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("webhook request: %v", err)
	}
	for k, vs := range fake.signHeaders(body) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := insecureLocalClient().Do(req)
	if err != nil {
		t.Fatalf("webhook post: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // bounded test response
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("call.initiated for %s answered %d, want 200", ccid, resp.StatusCode)
	}
}

// deliverHangupLocal posts the signed call.hangup webhook.
func deliverHangupLocal(t *testing.T, s *harness.Stack, fake *fakeTelnyx, ccid string) {
	t.Helper()
	body := callEventBody("call.hangup", ccid, "incoming")
	req, err := http.NewRequest(http.MethodPost, s.HTTPBaseURL()+"/api/v1/voice/webhook", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("webhook request: %v", err)
	}
	for k, vs := range fake.signHeaders(body) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := insecureLocalClient().Do(req)
	if err != nil {
		t.Fatalf("webhook post: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // bounded test response
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("call.hangup for %s answered %d, want 200", ccid, resp.StatusCode)
	}
}

// awaitPromptFrame polls the mirrored frames until ccid carries a prompt
// with text and the expected last flag.
func awaitPromptFrame(t *testing.T, fake *fakeTelnyx, ccid, substr string, last bool, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		found := false
		for _, fr := range fake.logs {
			if fr.CallID == ccid && fr.Event == "prompt" && strings.Contains(fr.Text, substr) {
				isLast := fr.Last != nil && *fr.Last
				if isLast == last {
					found = true
				}
			}
		}
		fake.mu.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no prompt frame with %q (last=%v) for %s within %s", substr, last, ccid, within)
}

// awaitSayFrame polls the mirrored frames until ccid carries a say
// containing substr.
func awaitSayFrame(t *testing.T, fake *fakeTelnyx, ccid, substr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		found := false
		for _, fr := range fake.logs {
			if fr.CallID == ccid && fr.Event == "say" && strings.Contains(fr.Text, substr) {
				found = true
			}
		}
		fake.mu.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no say frame with %q for %s within %s", substr, ccid, within)
}

// awaitExecutorPrompt polls the FakeLLM request log until an executor
// request's last user message contains substr (the observable that a REAL
// employee turn ran on that prompt).
func awaitExecutorPrompt(t *testing.T, s *harness.Stack, substr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, req := range s.Fake.Requests() {
			msgs, _ := req["messages"].([]any)
			var lastUser string
			for _, m := range msgs {
				msg, ok := m.(map[string]any)
				if !ok {
					continue
				}
				if role, _ := msg["role"].(string); role == "user" {
					lastUser, _ = msg["content"].(string)
				}
			}
			if strings.Contains(lastUser, substr) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no executor request carried %q within %s (employee turn never ran)", substr, within)
}

// assertCallRow opens calls.db and pins the local call's row: one row for
// ccid, ended, direction "local", target verbatim, and both transcript
// lines in order. The ended state + transcript lines are written by the
// release fan-out (ReleaseHook -> OnCallEnd) and the turn appends, so the
// read polls until the row settles (bounded).
func assertCallRow(t *testing.T, s *harness.Stack, ccid, employee, target, userText, assistantText string) {
	t.Helper()
	dbPath := filepath.Join(s.MeeptHome, "voice", "calls.db")
	deadline := time.Now().Add(20 * time.Second)
	var (
		direction  string
		number     string
		state      string
		sessionID  string
		transcript string
		settled    bool
	)
	for time.Now().Before(deadline) {
		rows, err := queryCallsDB(t, dbPath, ccid)
		if err == nil && len(rows) == 1 {
			direction = rows[0].direction
			number = rows[0].number
			state = rows[0].state
			sessionID = rows[0].sessionID
			transcript = rows[0].transcript
			if strings.Contains(transcript, userText) &&
				strings.Contains(transcript, assistantText) &&
				state == "ended" {
				settled = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !settled && state == "" {
		t.Fatalf("calls.db never carried a row for %s within 20s (db at %s)", ccid, dbPath)
	}
	if direction != "local" {
		t.Errorf("call row direction = %q, want \"local\" (CL5: the local leg rides the same store)", direction)
	}
	if number != target {
		t.Errorf("call row number = %q, want %q (NormalizeE164 must never mangle local targets)", number, target)
	}
	// HARD ended-state assertion (gap closed): the local call row MUST
	// transition to 'ended' through ReleaseHook -> OnCallEnd, which the
	// goal hold (LocalLeg.Open -> AcquireCallHold) now feeds. The loop
	// above also gates on ended, so reaching this line means the row
	// settled only once it ended. A row stuck at 'live' fails here.
	if state != "ended" {
		t.Errorf("call row state = %q, want \"ended\" (local call must reach the ended state via ReleaseHook -> OnCallEnd; heldCalls filled by LocalLeg.Open -> AcquireCallHold)", state)
	}
	if sessionID == "" {
		t.Errorf("call row session_id is empty; OnCallStart never bound the session")
	}
	var lines []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(transcript), &lines); err != nil {
		t.Fatalf("decode transcript %q: %v", transcript, err)
	}
	var gotUser, gotAssistant bool
	for _, l := range lines {
		if l.Role == "user" && strings.Contains(l.Text, userText) {
			gotUser = true
		}
		if l.Role == "assistant" && strings.Contains(l.Text, assistantText) {
			gotAssistant = true
		}
	}
	if !gotUser || !gotAssistant {
		t.Errorf("transcript missing lines (user=%v assistant=%v): %s", gotUser, gotAssistant, transcript)
	}
}

// callRow is one voice_calls row (the columns this suite pins).
type callRow struct {
	direction  string
	number     string
	state      string
	sessionID  string
	transcript string
}

// queryCallsDB reads the call row for ccid via the sqlite3 CLI.
func queryCallsDB(t *testing.T, dbPath, ccid string) ([]callRow, error) {
	t.Helper()
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	out, err := exec.Command("sqlite3", dbPath,
		"SELECT direction, number, state, session_id, transcript FROM voice_calls WHERE call_id='"+ccid+"';").Output()
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil, fmt.Errorf("no row for %s", ccid)
	}
	parts := strings.Split(line, "|")
	if len(parts) != 5 {
		return nil, fmt.Errorf("row shape: %q", line)
	}
	return []callRow{{
		direction:  parts[0],
		number:     parts[1],
		state:      parts[2],
		sessionID:  parts[3],
		transcript: parts[4],
	}}, nil
}

// insecureLocalClient tolerates the scratch daemon's self-signed cert.
func insecureLocalClient() *http.Client {
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

// TestVoiceLocalRefusals pins the busy / duplicate-target refusals
// surfacing through the RPC error shapes (T2): the registry's shared
// cross-leg rules mapped onto the named RPC errors (CL6/CL8).
func TestVoiceLocalRefusals(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, _ := bootLocalStack(t, fake, modelPath)

	const (
		employee = "emp-local-refuse"
		label    = "refusebox"
	)
	rpcCreateEmployee(t, s, employee)

	// First open: succeeds and holds the registry slot.
	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)

	// Duplicate target (same label, second employee): refused with the
	// named duplicate error shape.
	rpcCreateEmployee(t, s, "emp-local-other")
	dupClient := harness.DialRPC(t, s.SocketPath)
	dupResp := dupClient.CallError("voice.local.open", map[string]any{
		"employee_id": "emp-local-other",
		"label":       label,
	})
	dupMsg := rpcErrorMessage(t, "voice.local.open(dup)", dupResp)
	if !strings.Contains(dupMsg, "voice: a live call to this target already exists") {
		t.Errorf("duplicate-target refusal = %q, want the named ErrVoiceLocalDuplicate shape", dupMsg)
	}

	// Busy: the SAME employee's second call (different label) hits the
	// one-live-call-per-employee rule -> the named busy shape.
	busyClient := harness.DialRPC(t, s.SocketPath)
	busyResp := busyClient.CallError("voice.local.open", map[string]any{
		"employee_id": employee,
		"label":       "otherbox",
	})
	busyMsg := rpcErrorMessage(t, "voice.local.open(busy)", busyResp)
	if !strings.Contains(busyMsg, "voice: call registry busy") {
		t.Errorf("busy refusal = %q, want the named ErrVoiceLocalBusy shape", busyMsg)
	}

	// Unknown employee: refused before any side effect.
	unkClient := harness.DialRPC(t, s.SocketPath)
	unkResp := unkClient.CallError("voice.local.open", map[string]any{
		"employee_id": "no-such-employee",
		"label":       label,
	})
	unkMsg := rpcErrorMessage(t, "voice.local.open(unknown)", unkResp)
	if !strings.Contains(unkMsg, "voice: unknown employee") {
		t.Errorf("unknown-employee refusal = %q, want the named ErrVoiceLocalUnknownEmployee shape", unkMsg)
	}

	// Teardown: end the held call so the suite leaves nothing live.
	endClient := harness.DialRPC(t, s.SocketPath)
	end := endClient.CallResult("voice.local.end", map[string]any{"call_id": ccid})
	if end["ok"] != true {
		t.Fatalf("voice.local.end result = %v, want ok:true", end)
	}
	awaitLogLine(t, s, "voice call ended", 15*time.Second)
}
