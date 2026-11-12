package daemon

// Gate tests T2 (feature F2, master.md C4, leaf 03): VoiceWiring.routeFrame
// on EventSetup emits exactly one C1 keys frame through TextServer.Send,
// mapping VoiceConfig fields to the wire keys and resolving cloud keys
// from the secrets broker. Empty/missing cloud keys are sent as "" (the
// child factory fails the call), never skipped. A duplicate (idempotent)
// setup never re-sends.
//
// RED style: compiles against today's symbols; fails on the MISSING
// emission (verified: no producer of stt_key exists daemon-side today,
// master.md anchor note). Missing symbols named in comments:
//   leaf 03: sendKeysFrame on VoiceWiring + its routeFrame EventSetup
//     call (after loop registration, once per call)
//   leaf 01: VoiceConfig.TTSBaseURL (the other three fields already exist
//     via the voice-local-leg tree, orchestrator-verified cb8c8672)
//
// Seam deviation (documented per the leaf's Notes): TextServer is a
// concrete *voice.TextServer field on VoiceWiring (voice_wiring.go:301),
// not interface-injectable. We therefore run a REAL TextServer on a short
// temp-dir socket and connect a fake child over net.Dial (the established
// internal/voice/socket_test.go pattern); the fake child's raw received
// lines are the recording sender.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
)

// keysEmitFixture builds a VoiceWiring whose TextServer is a REAL socket
// server with a fake child attached: the child's received raw lines are
// the recording-sender seam for C4.
type keysEmitFixture struct {
	w     *VoiceWiring
	lines chan string
}

func newKeysEmitFixture(t *testing.T) *keysEmitFixture {
	t.Helper()
	w, _ := newVoiceWiringForTest(t)

	// Replace the placeholder TextServer with a started one whose socket
	// path lives in a short temp dir (sun_path limit).
	dir := shortKeysDir(t)
	path := filepath.Join(dir, "keys-test.sock")
	srv := voice.NewTextServer(path, testLogger(t))
	ctx, cancel := context.WithCancel(context.Background())
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Start(ctx) }()
	waitForSocket(t, path)
	w.TextServer = srv
	t.Cleanup(func() {
		cancel()
		srv.Close()
		if srvErr != nil {
			<-srvErr
		}
	})

	// Fake child: records every raw line the daemon sends. The dial
	// retries: os.Stat sees the socket file at bind() time, a beat
	// before listen() finishes accepting (ECONNREFUSED in that window
	// under suite load - the internal/voice/socket_test.go waitConn
	// pattern). A probe dial cannot be used to wait: the first
	// connection claims the single child slot.
	var conn net.Conn
	dialDeadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		conn, err = net.Dial("unix", path)
		if err == nil {
			break
		}
		if time.Now().After(dialDeadline) {
			t.Fatalf("fake child dial: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	lines := make(chan string, 16)
	go func() {
		br := bufio.NewScanner(conn)
		for br.Scan() {
			lines <- br.Text()
		}
		close(lines)
	}()
	t.Cleanup(func() { _ = conn.Close() })

	return &keysEmitFixture{w: w, lines: lines}
}

// TestVoiceKeysEmission (gate T2): setup frame -> exactly one keys line
// with the C4 config mapping + broker keys; duplicate setup -> still one.
func TestVoiceKeysEmission(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-1"
	acquireFor(f.w, ccid)

	// Setup #1 -> exactly one keys line.
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	if wire["event"] != "keys" {
		t.Errorf("event = %v, want \"keys\"", wire["event"])
	}
	if wire["call_id"] != ccid {
		t.Errorf("call_id = %v, want %s", wire["call_id"], ccid)
	}
	// C4 config->wire mapping. The wiring test config sets
	// DefaultEmployee only, so vendor fields read the "empty = default"
	// rule: stt_vendor deepgram, tts_vendor openai (leaf 03's defaults
	// when config empty).
	wantStr := map[string]string{
		"stt_vendor": "deepgram",
		"tts_vendor": "openai",
		"stt_model":  "",
		"tts_model":  "",
		"tts_voice":  "",
	}
	for key, want := range wantStr {
		if got, _ := wire[key].(string); got != want {
			t.Errorf("wire %s = %v, want %q (C4 mapping; empty config = vendor default)", key, wire[key], want)
		}
	}
	// tts_base_url must exist as a wire key (NEW leaf 01 config field
	// mapped through): empty config sends "".
	if got, present := wire["tts_base_url"]; !present || got != "" {
		t.Errorf("missing symbol/behavior: wire tts_base_url = %v (present=%v), want \"\" key (leaf 01 VoiceConfig.TTSBaseURL + C4 mapping)", got, present)
	}

	// Duplicate setup (idempotent) -> still exactly one keys line.
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	select {
	case extra := <-f.lines:
		t.Fatalf("duplicate setup emitted a second line %q; keys send once per call (C4)", extra)
	case <-time.After(300 * time.Millisecond):
	}

	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// TestVoiceKeysEmissionUsesProfileResolution (local-leg leaf 04 tail,
// CL4): the keys line must carry the per-call employee_leg row's TTS
// model/voice over the top-level [voice] config. RED today: sendKeysFrame
// reads w.cfg directly, so the employee row is invisible to the wire.
func TestVoiceKeysEmissionUsesProfileResolution(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-profile"
	// Employee row wins over top level per CL4 precedence; STT model has
	// no per-employee pin, so top level must still land.
	f.w.cfg.TTSModel = "global-model"
	f.w.cfg.TTSVoice = "global-voice"
	f.w.cfg.STTModel = "global-stt-model"
	f.w.cfg.EmployeeLeg = []config.EmployeeLegConfig{{
		Employee: f.w.cfg.DefaultEmployee,
		Leg:      voice.LegPhone,
		TTSModel: "employee-model",
		TTSVoice: "employee-voice",
	}}
	acquireFor(f.w, ccid)

	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	wantStr := map[string]string{
		"tts_model":  "employee-model",
		"tts_voice":  "employee-voice",
		"stt_model":  "global-stt-model",
		"stt_vendor": "deepgram",
		"tts_vendor": "openai",
	}
	for key, want := range wantStr {
		if got, _ := wire[key].(string); got != want {
			t.Errorf("wire %s = %v, want %q (CL4: employee_leg row over top-level config)", key, wire[key], want)
		}
	}

	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// TestVoiceKeysEmissionTurnConfirmSelector (voice-startguard C1): the
// keys line carries turn_confirm_ms from voice.turn_confirm_ms — a
// hand-built fixture config (unset) sends 0 (legacy per-final dispatch,
// the pre-C1 behavior), and a config-set value threads through to the
// wire. The key is present even at 0 (no omitempty: the child decodes
// ABSENT as its own default, 0 as explicit legacy).
func TestVoiceKeysEmissionTurnConfirmSelector(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-tc"
	acquireFor(f.w, ccid)

	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	if got, present := wire["turn_confirm_ms"]; !present {
		t.Fatal("keys wire lacks turn_confirm_ms (C1 selector must ride the frame)")
	} else if v, ok := got.(float64); !ok || v != 0 {
		t.Errorf("wire turn_confirm_ms = %v, want 0 for an unset fixture VoiceConfig (0 = legacy per-final dispatch)", got)
	}
	f.w.releaseCall(ccid, voice.OutcomeHangup)

	// Config-set value threads through.
	const ccid2 = "cc-keys-tc2"
	acquireFor(f.w, ccid2)
	f.w.cfg.TurnConfirmMs = 1234
	f.w.routeFrame(voice.Frame{CallID: ccid2, Event: voice.EventSetup})
	line2 := readKeysLine(t, f.lines)
	wire2 := map[string]any{}
	if err := json.Unmarshal([]byte(line2), &wire2); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line2)
	}
	if got, _ := wire2["turn_confirm_ms"].(float64); got != 1234 {
		t.Errorf("wire turn_confirm_ms = %v, want 1234 (voice.turn_confirm_ms threaded to the keys frame)", wire2["turn_confirm_ms"])
	}
	t.Cleanup(func() { f.w.releaseCall(ccid2, voice.OutcomeHangup) })
}

// TestVoiceKeysEmissionDefaultConfigSends900 (C1 default threading): a
// DefaultConfig-shaped VoiceConfig — what the daemon actually runs with
// an unmodified install — puts 900 on the wire, proving the load
// boundary default reaches the child (aggregation ON out of the box).
func TestVoiceKeysEmissionDefaultConfigSends900(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-tc3"
	acquireFor(f.w, ccid)
	f.w.cfg.TurnConfirmMs = config.DefaultConfig().Voice.TurnConfirmMs
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	if got, _ := wire["turn_confirm_ms"].(float64); got != 900 {
		t.Errorf("wire turn_confirm_ms = %v, want 900 (DefaultConfig.Voice.TurnConfirmMs)", wire["turn_confirm_ms"])
	}
	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// TestVoiceKeysEmissionEmptyCloudKeys sends "" for BOTH cloud keys when
// the broker lacks them: the line is still emitted (the child factory
// fails the call; the child and other calls stay up).
func TestVoiceKeysEmissionEmptyCloudKeys(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-2"
	acquireFor(f.w, ccid)

	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	for _, key := range []string{"stt_key", "tts_key"} {
		got, present := wire[key]
		if !present {
			t.Fatalf("missing behavior: wire key %s absent; empty cloud keys are SENT as \"\" not skipped (C4)", key)
		}
		if got != "" {
			t.Errorf("%s = %v, want \"\" (missing cloud key sent empty, not skipped)", key, got)
		}
	}
	if wire["event"] != "keys" {
		t.Errorf("event = %v, want \"keys\" (empty keys still emit the frame)", wire["event"])
	}
	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// readKeysLine returns the first keys-event line the fake child receives.
func readKeysLine(t *testing.T, lines <-chan string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("fake child line stream closed before any keys line")
			}
			if strings.Contains(line, `"keys"`) {
				return line
			}
			continue // a non-keys frame (e.g. say filler); keep scanning
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("missing behavior: no keys line emitted on EventSetup (leaf 03: routeFrame never sends keys)")
	return ""
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s never appeared", path)
}

func shortKeysDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "vkeys")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
