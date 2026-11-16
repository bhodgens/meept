//go:build e2e

// voice-tierA leaf 07 scenario partial-bargein (T2): the STT-partial
// barge-in trigger (D1) observed on a LIVE integrated call.
//
// The scenario rides the LOCAL leg (voice.local.* RPC, landed with the
// local-leg tree) because it is the leg where a final prompt runs a REAL
// bound employee turn (Binding.OnCallStart at Open) - so the contract
// "the fresh final starts a new turn whose reply reflects the
// interrupted prompt" is assertable end to end. On the phone leg the
// binding never binds (no OnCallStart call site), so the same turn
// reflection cannot be observed hermetically there; the phone-side
// barge-in legs stay pinned by the two existing tests in this suite.
//
// Given: scratch daemon, voice.enabled, local leg enabled, loopback
// fake qwen3 TTS endpoint; a fake child (plain C1/C2 mode) answering the
// open frame; a raw client dialed to the call's C2 pair sockets.
// When: a STT partial arrives at the child's transcript stage WHILE the
// daemon's say is being played (synthesized through the fake endpoint),
// then a fresh final lands after the clear.
// Then: exactly one C1 interrupt (playback stops: clear observed), and
// the second turn's FakeLLM scripting proves the daemon ran the
// interrupting utterance as a fresh prompt (the reply text is carried on
// the second say).
//
// The partial/final injection rides the child's control socket (the
// suite's established C1 driver): the frames enter the daemon's talk
// loop exactly as the real STT-pump path would emit them (leaf 05/06
// wiring is pinned at unit level by the voicechild gate/leaf tests;
// this scenario proves the integrated daemon behavior).

package voicebargein

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// fakeQwen3Bar is the voice-bargein suite's local copy of the loopback
// OpenAI-compatible streaming TTS endpoint (per-suite helper convention;
// see the fakeTelnyx header note). Streams s16le@24k mono PCM chunks.
type fakeQwen3Bar struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
}

func newFakeQwen3Bar(t *testing.T) *fakeQwen3Bar {
	t.Helper()
	f := &fakeQwen3Bar{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck // test sink
		input, _ := body["input"].(string)
		f.mu.Lock()
		f.reqs = append(f.reqs, input)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "audio/pcm")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 20; i++ {
			chunk := make([]byte, 1920) // 480 samples s16le = 20 ms @24k
			for s := 0; s < 480; s++ {
				binary.LittleEndian.PutUint16(chunk[s*2:], uint16(2000+i))
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeQwen3Bar) baseURL() string { return f.srv.URL }

// spokeTexts returns every input the fake endpoint synthesized.
func (f *fakeQwen3Bar) spokeTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

// awaitSpoke polls until the endpoint has synthesized at least one
// utterance containing substr.
func (f *fakeQwen3Bar) awaitSpoke(substr string, within time.Duration) {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, s := range f.spokeTexts() {
			if strings.Contains(s, substr) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fake qwen3 endpoint never synthesized %q within %s; spoke: %v", substr, within, f.spokeTexts())
}

// localClient is the raw local-leg audio client: it dials the call's C2
// pair sockets exactly like the CLI's file mode (cmd/meept/voice.go
// runVoiceLocalFileMode). Mic writes flow client -> daemon -> child;
// speaker reads drain child -> daemon -> client.
type localClient struct {
	mic     net.Conn
	speaker net.Conn
}

func dialLocalClient(t *testing.T, open map[string]any) *localClient {
	t.Helper()
	inPath, _ := open["in_path"].(string)
	outPath, _ := open["out_path"].(string)
	if inPath == "" || outPath == "" {
		t.Fatalf("voice.local.open returned empty pair paths: %v", open)
	}
	// Short dials with retries: the pair listeners exist from Open, the
	// accept completes as soon as the splice goroutine starts.
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

// speakMic pushes non-silent L16@16k PCM onto the mic socket (the local
// splice chunks it to the child; the fake STT ignores the audio but the
// join gate requires first-byte flow within its grace).
func (c *localClient) speakMic(durationMs int) {
	samples := 16000 * durationMs / 1000
	buf := make([]byte, samples*2)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	_, _ = c.mic.Write(buf) //nolint:errcheck // mic loss never fails the scenario
}

// bootLocalVoiceStack boots a scratch daemon with voice + the local leg
// enabled and the plain fake child armed, returning the stack and the
// fake child's control socket path (same boot shape as the lifecycle
// suite's bootVoiceStack: overlays + env + secrets in one Start call).
func bootLocalVoiceStack(t *testing.T, fake *fakeTelnyx, q *fakeQwen3Bar) (*harness.Stack, string) {
	t.Helper()
	childBin := buildFakeChild(t, t.TempDir())
	voiceExtras := map[string]any{
		"child_binary_path": childBin,
		// The local leg's Open takes the employee explicitly (RPC arg),
		// but the shared registry/wiring still requires a default.
		"default_employee": "voice-fixture-employee",
		"tts_provider":     "qwen3",
		"tts_base_url":     q.baseURL(),
		"tts_model":        "Qwen/Qwen3-TTS-test",
		"tts_voice":        "vivian",
		// The local-leg keys ride as a NESTED object: overlayOptions
		// copies these verbatim into the [voice] map, and a literal
		// "local.enabled" KEY would not decode into
		// VoiceConfig.Local.Enabled (json5 unmarshal maps struct fields,
		// not dotted paths).
		"local": map[string]any{"enabled": true, "barge_in_rms": 3000},
	}
	opts := []harness.StartOption{
		// Merge INTO the existing [voice] map (dotted overlays would
		// replace it whole and lose enabled/telnyx keys - the lifecycle
		// suite's overlayOptions hook is the established pattern).
		fake.overlayOptions(voiceExtras),
		harness.WithExtraEnv(fake.extraEnv()),
	}
	for k, v := range fake.secretsOverlay() {
		opts = append(opts, harness.WithConfigOverlay(map[string]any{k: v}))
	}
	s := harness.Start(t, opts...)
	awaitLogLine(t, s, "voice routes armed", 30*time.Second)
	ctrlPath := filepath.Join(s.MeeptHome, "voice", "ctrl.sock")
	return s, ctrlPath
}

// interruptCount counts interrupt frames the child mirrored for ccid
// (the leaf-06 emitted frames, read off the control mirror).
func (f *fakeTelnyx) interruptCount(ccid string) int {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	n := 0
	for _, fr := range f.frames[ccid] {
		if fr.Event == "interrupt" && !strings.HasPrefix(fr.CallID, "_") {
			n++
		}
	}
	return n
}

// rpcOpenLocalCall opens a local call over the RPC surface (the real
// voice.local.open handler; employee existence is gate-checked there -
// the fixture employee is created through the REAL agents.create RPC).
func rpcOpenLocalCall(t *testing.T, s *harness.Stack, employee, label string) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	created := client.CallResult("agents.create", map[string]any{
		"id":      employee,
		"name":    employee,
		"prompt":  "You are " + employee + ", an e2e voice probe.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       "e2e local voice probe",
			"role":          "e2e voice probe",
			"charter":       "Answer the caller briefly.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				// "*" = inherit the template tool list (a declared-EMPTY
				// list is a deny-all constitution policy, which starves
				// the harness's IsExecutorRequest script predicate).
				"tools_allowed": []string{"*"},
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
	open := client.CallResult("voice.local.open", map[string]any{
		"employee_id": employee,
		"label":       label,
	})
	if open["call_id"] == "" {
		t.Fatalf("voice.local.open returned no call_id: %v", open)
	}
	return open
}

// TestVoicePartialBargein interrupts a playing say with a scripted STT
// partial, then completes the interrupting utterance with a final: the
// daemon barge-ins once (clear), and the fresh turn's reply reflects the
// interrupting prompt (asserted through what the fake TTS endpoint is
// asked to synthesize for the second turn).
func TestVoicePartialBargein(t *testing.T) {
	q := newFakeQwen3Bar(t)
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootLocalVoiceStack(t, fake, q)

	const (
		employee = "emp-barge-local"
		label    = "bargebox"
	)

	// Script the employee turns by prompt content: the FIRST draft (the
	// greeting is filler-class, no turn) answers the seeded opener; the
	// SECOND draft must be the INTERRUPTING utterance's answer, proved by
	// a marker only that reply carries.
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("shipping still on friday")),
		harness.TextResponse("The shipment is on Friday."),
	)
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains("actually move it to monday")),
		harness.TextResponse("BARGEMARKER shipment moved to Monday."),
	)

	// The phone leg ARMS the shared child first: the manager's spawn seam
	// runs child.EnsureRunning on call.initiated, which is the only
	// production caller of EnsureRunning today (the local leg's Open
	// notes the call but does not spawn - local-leg tree owns that gap).
	// One throwaway call, immediately hung up: the child stays warm for
	// the local call below (idle stop is 2s, so we must not dawdle).
	const warmCcid = "cc-barge-warm"
	fake.deliverCallInitiated(s, warmCcid, "incoming")
	fake.awaitAnswered(warmCcid, 10*time.Second)
	fake.dialMedia(s, warmCcid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.deliverHangup(s, warmCcid)
	awaitLogLine(t, s, "voice call ended", 15*time.Second)

	// The child's control socket exists only once a child has spawned and
	// connected; bounded poll instead of a fixed sleep.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ctrlPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake child control socket %s never appeared after the warm call", ctrlPath)
		}
		time.Sleep(50 * time.Millisecond)
	}

	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	// Connect the child's control socket (it spawned at Open) and join
	// the audio pair as the local client.
	fake.connectChild(ctrlPath, 15*time.Second)
	fake.childDialAudio(ccid)
	client := dialLocalClient(t, open)

	// Mic flow first: the local splice's join gate needs client bytes.
	go client.speakMic(2000)

	// Turn 1: the caller's first utterance completes (final). The talk
	// loop spawns the employee draft on the bound session; its reply is
	// spoken to the child as say frame(s) (observable on the control
	// mirror).
	fake.childSendsPrompt(ccid, "is the shipment still on friday", true)
	fake.awaitSayContaining(ccid, 20*time.Second)
	awaitExecutorPrompt(t, s, "is the shipment still on friday", 20*time.Second)

	// Playback of the reply is now LIVE on the child side (stub TTS
	// streams the say straight onto C2 .out; the splice paces it to the
	// client). Give the first chunk time to be spoken before barging in.
	time.Sleep(150 * time.Millisecond)

	// The partial lands WHILE speaking: the fake child replays the
	// leaf-06 transcript-branch frame sequence (interrupt BEFORE the
	// partial's prompt, C5 order - the exact frames the real pipeline's
	// consumeTranscripts emits). The talk loop drops the draft and sends
	// clear.
	fake.childCommand(ctrlMsg{Ctrl: "bargein", CallID: ccid, Text: "actually move it to"})
	fake.awaitClearFrame(ccid, 10*time.Second)
	awaitLogLine(t, s, "voice talk loop: barge-in", 10*time.Second)
	if got := fake.interruptCount(ccid); got != 1 {
		t.Fatalf("interrupt frames for %s = %d, want exactly 1 (D1 latch: once per barge-in)", ccid, got)
	}

	// The final completes the interrupting utterance: a fresh employee
	// turn runs on the INTERRUPTING prompt - proved by the FakeLLM
	// request log carrying it as a turn input after the barge-in.
	fake.childSendsPrompt(ccid, "actually move it to monday please", true)
	awaitExecutorPrompt(t, s, "actually move it to monday please", 30*time.Second)

	// The fresh draft's reply reaches the child as a NEW say after the
	// clear (playback restarted post-barge-in: F-1 re-arm on the live
	// call), and exactly one interrupt was emitted for the whole barge-in.
	if after := fake.sayFrameCountAfterClear(ccid); after == 0 {
		t.Fatalf("no say frame after clear for %s; the fresh turn's reply never restarted playback", ccid)
	}
	if got := fake.interruptCount(ccid); got != 1 {
		t.Fatalf("interrupt frames for %s = %d after the full cycle, want exactly 1", ccid, got)
	}
}

// awaitExecutorPrompt polls the FakeLLM request log until an executor
// request's last user message contains substr (the observable that a
// real employee turn RAN on that prompt).
func awaitExecutorPrompt(t *testing.T, s *harness.Stack, substr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, req := range s.Fake.Requests() {
			msgs, _ := req["messages"].([]any)
			var last string
			for _, m := range msgs {
				msg, ok := m.(map[string]any)
				if !ok {
					continue
				}
				if role, _ := msg["role"].(string); role == "user" {
					last, _ = msg["content"].(string)
				}
			}
			if strings.Contains(last, substr) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no executor request carried %q within %s (employee turn never ran)", substr, within)
}
