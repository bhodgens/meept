package daemon

// Leaf 07 daemon-glue tests (local-leg plan docs/plans/20261106-voice-
// local-leg/07-local-leg.md T5/T6): WireVoice constructs a LocalLeg when
// cfg.Local.Enabled (nil otherwise), and the leg's Open routes the day
// brief with direction "local" / number "local:<label>" through the
// binding's OnCallStart - the same call-record row shape a phone call
// writes.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/voice"
)

// TestWireVoiceLocalLegNilWhenDisabled pins T6: without voice.local.enabled
// the wiring carries NO LocalLeg (leaf 08 refuses opens on nil).
func TestWireVoiceLocalLegNilWhenDisabled(t *testing.T) {
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{Enabled: false}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer w.Employee.Close()
	if w.Local != nil {
		t.Fatal("Local leg constructed with voice.local.enabled=false; want nil")
	}
}

// TestWireVoiceLocalLegConstructedWhenEnabled pins T6: with
// voice.local.enabled the wiring exposes a usable LocalLeg whose Live()
// starts empty, wired onto the SHARED registry, manager, and child.
func TestWireVoiceLocalLegConstructedWhenEnabled(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{
		Enabled:     true,
		STTProvider: "whisper",
		TTSProvider: "piper",
		TTSModel:    "en_US-test-medium",
		TTSVoice:    "test",
		BargeInRms:  3000,
		Name:        "wirebox",
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer func() { w.Employee.Close() }()
	if w.Local == nil {
		t.Fatal("Local leg nil with voice.local.enabled=true")
	}
	if live := w.Local.Live(); len(live) != 0 {
		t.Errorf("fresh leg Live() = %v, want empty", live)
	}
}

// buildConnectChildStub compiles a minimal meept-voice stand-in: it reads
// the start token from stdin and dials the text socket, then holds until
// signalled. Enough for ChildProc's handshake probe to pass so a local
// Open's spawn-on-open guarantee succeeds in-process (the brief test
// needs the spawn, not the audio pipeline).
func buildConnectChildStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "connectchild.go")
	const stub = `package main

import (
	"bufio"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	args := os.Args[1:]
	textSocket := ""
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "--text-socket" {
			textSocket = args[i+1]
		}
	}
	token, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if textSocket == "" || strings.TrimSpace(token) == "" {
		os.Exit(2)
	}
	conn, err := net.Dial("unix", textSocket)
	if err != nil {
		os.Exit(4)
	}
	if _, wErr := conn.Write([]byte(token + "\n")); wErr != nil {
		os.Exit(5)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				sigs <- syscall.SIGTERM
				return
			}
		}
	}()
	<-sigs
	conn.Close()
}
`
	if wErr := os.WriteFile(src, []byte(stub), 0o600); wErr != nil {
		t.Fatalf("write stub source: %v", wErr)
	}
	out := filepath.Join(dir, "meept-voice-stub")
	cmd := exec.Command("go", "build", "-o", out, src)
	if bErr := cmd.Run(); bErr != nil {
		t.Fatalf("go build stub child: %v", bErr)
	}
	return out
}

// TestWireVoiceLocalLegBrief pins T5: a local Open runs the day brief +
// binding with direction "local" and number "local:<label>" - the call
// row in the call store carries direction=local and the local: target,
// byte-shape identical to a phone call row.
func TestWireVoiceLocalLegBrief(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.ChildBinaryPath = buildConnectChildStub(t)
	deps.Config.Voice.Local = config.LocalConfig{
		Enabled:     true,
		STTProvider: "whisper",
		TTSProvider: "piper",
		BargeInRms:  3000,
		Name:        "briefbox",
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	defer func() { w.Employee.Close() }()
	// The text server must LISTEN before the local Open spawns the child:
	// the stub dials the socket and the handshake probe waits for a
	// connected child (a spawn without the listener is a guaranteed
	// handshake timeout). registerTool stays a no-op here.
	startCtx, stopStart := context.WithCancel(context.Background())
	defer stopStart()
	if sErr := w.Start(startCtx, func(dial builtin.DialerFunc, agentID builtin.AgentIDFunc) {}); sErr != nil {
		t.Fatalf("Start: %v", sErr)
	}

	call, err := w.Local.Open(context.Background(), "emp-local-brief", "briefbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if call.SessionID == "" {
		t.Fatal("Open returned an empty SessionID (binding.OnCallStart did not resolve a session)")
	}

	// The binding row: today's transcript for the local number exists and
	// the call store resolves the session for (local:briefbox, employee).
	sid, ok := w.Employee.store.SessionIDFor("local:briefbox", "emp-local-brief")
	if !ok || sid == "" {
		t.Fatal("call store has no session for (local:briefbox, emp-local-brief); OnCallStart did not record the row with the local number")
	}
	if sid != call.SessionID {
		t.Errorf("store session %q != LocalCall.SessionID %q", sid, call.SessionID)
	}

	// TodayTranscripts for the local target proves the row's number
	// shape: a phone-shaped number would NOT match "local:briefbox".
	transcripts, err := w.Employee.store.TodayTranscripts(context.Background(), "local:briefbox")
	if err != nil {
		t.Fatalf("TodayTranscripts: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts for local:briefbox = %d, want 1 (the call row carries the local: number)", len(transcripts))
	}
	if !strings.HasPrefix(transcripts[0].Number, "local:") {
		t.Errorf("transcript number = %q, want the local: prefix (direction local, number local:<label>)", transcripts[0].Number)
	}

	// Terminal fan-out: End closes the row (outcome recorded) and frees
	// the registry slot.
	if err := w.Local.End(call.CallID, "brief test done"); err != nil {
		t.Errorf("End: %v", err)
	}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == call.CallID {
			t.Errorf("registry still holds %s after End", call.CallID)
		}
	}
}

// TestWireVoiceLocalLegArmsWithoutTelnyxKey pins the phone-secret-gate
// bughunt: with voice.enabled + voice.local.enabled but NO broker-resolvable
// telnyx_api_key (the local-leg-only install: nil broker, or a broker error),
// the local leg must STILL arm (w.Local non-nil, the voice.local RPC seam
// routes an open) while the phone/telnyx seam stays nil. This is the
// local-leg invariant in internal/voice/AGENTS.md (local leg never imports
// telnyx or touches the broker key). Before the fix, WireVoice returned
// nil,nil here, disabling BOTH legs.
func TestWireVoiceLocalLegArmsWithoutTelnyxKey(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{Enabled: true, Name: "notoken"}
	deps.Broker = nil // broker-less install: telnyx_api_key cannot resolve

	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("local-enabled + broker-less install must NOT yield nil wiring (the phone-secret gate must not disable the local leg)")
	}
	defer w.Employee.Close()

	if w.Local == nil {
		t.Fatal("local leg failed to arm without a telnyx_api_key; voice.local.open would refuse with ErrVoiceLocalDisabled")
	}
	if w.tel != nil {
		t.Fatal("phone/telnyx seam must stay nil without a telnyx_api_key")
	}
	if w.Dialer != nil {
		t.Fatal("phone dialer must be nil without a telnyx_api_key")
	}
	if w.Routes != nil {
		t.Fatal("phone routes must be nil without a telnyx_api_key")
	}

	// voice.local.open routes normally (not the disabled refusal): the RPC
	// seam adapts a live leg, so the handler's open seam is non-empty.
	seams := voiceLocalSeams(w, nil, testLogger(t))
	if seams.Open == nil {
		t.Fatal("voice.local.open seam unwired without a telnyx_key; opens would refuse with ErrVoiceLocalDisabled")
	}
	if seams.End == nil || seams.Status == nil {
		t.Fatal("voice.local.end/.status seams unwired without a telnyx_api_key")
	}
}

// TestWireVoiceDisabledWhenNoTokenAndNoLocal pins the fail-closed bound of
// the same bughunt fix: when BOTH the phone token is unresolvable AND the
// local leg is not enabled, WireVoice must STILL return nil,nil (the fully
// disabled boot, byte-identical to the pre-fix posture) - the partial
// local-only wiring exists ONLY to serve a live local leg.
func TestWireVoiceDisabledWhenNoTokenAndNoLocal(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{Enabled: false}
	deps.Broker = nil

	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w != nil {
		t.Fatal("no phone token + local disabled must be the fully-disabled boot (nil wiring); got a partial wiring")
	}
}

// TestWireVoiceLocalDisabledRefusalPreserved pins the disabled-leg named
// error contract: when voice.local.enabled is false (token present - the
// normal phone boot), the local RPC seams must be nil, so the handler keeps
// refusing opens with ErrVoiceLocalDisabled.
func TestWireVoiceLocalDisabledRefusalPreserved(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{Enabled: false}

	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("precondition: phone-token present must wire; want non-nil wiring")
	}
	defer w.Employee.Close()
	if w.Local != nil {
		t.Fatal("local leg constructed with voice.local.enabled=false; want nil (ErrVoiceLocalDisabled must still fire on opens)")
	}
	seams := voiceLocalSeams(w, nil, testLogger(t))
	if seams.Open != nil {
		t.Fatal("voice.local.open seam must be nil when the local leg is disabled, so ErrVoiceLocalDisabled keeps firing")
	}
}

// TestVoiceLinkBackchannelSoftenReloadLive pins the wave-3 reload fix
// (bughunt 2026-11-19): the link service's per-employee backchannel
// soften resolver reads the LIVE [voice] snapshot, never the boot one.
// The LinkManager is constructed lazily ONCE, so a boot-snapshot closure
// froze voice.backchannel_soften across daemon.reloaded republishes while
// the keys frame already read currentVoiceCfg. Drive the EXACT production
// state construction (newVoiceLinkState, the voiceLinkSeams helper) and
// swap cfgLive the way reloadVoiceConfig does.
func TestVoiceLinkBackchannelSoftenReloadLive(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	// A live local leg is required for the link state (the manager's
	// construction refuses nil legs); build one over the test wiring's
	// registry/manager/text server exactly as WireVoice does.
	leg, err := voice.NewLocalLeg(voice.LocalLegConfig{Enabled: true}, voice.LocalDeps{
		Registry:   w.Registry,
		TextServer: w.TextServer,
		Manager:    w.Manager,
		Logger:     testLogger(t),
	})
	if err != nil {
		t.Fatalf("NewLocalLeg: %v", err)
	}
	w.Local = leg
	off := false
	w.cfg.BackchannelSoften = &off

	state := newVoiceLinkState(w, nil, testLogger(t))
	// Boot read: global false, no rows.
	if state.backchannelSoftenFor("emp-a") {
		t.Fatal("boot resolve = true, want the boot global false")
	}
	// Construct the manager NOW (the lazy one-shot): the resolver closure
	// it captures must still follow later config swaps.
	if m := state.manager(); m == nil {
		t.Fatal("link manager construction failed on a live leg")
	}

	// Config swap #1 (reloadVoiceConfig's exact seam): global true plus a
	// per-employee explicit-false row - row > global must hold live.
	on := true
	live := w.cfg
	live.BackchannelSoften = &on
	rowOff := false
	live.EmployeeLeg = []config.EmployeeLegConfig{
		{Employee: "emp-b", BackchannelSoften: &rowOff},
	}
	w.cfgMu.Lock()
	w.cfgLive = &live
	w.cfgMu.Unlock()

	if !state.backchannelSoftenFor("emp-a") {
		t.Fatal("post-swap resolve(emp-a) = false, want the NEW global true (boot snapshot froze the toggle)")
	}
	if state.backchannelSoftenFor("emp-b") {
		t.Fatal("post-swap resolve(emp-b) = true, want the live row's explicit false (row > global)")
	}

	// Config swap #2: toggle back off live; the boot snapshot is untouched.
	live2 := live
	live2.BackchannelSoften = &off
	w.cfgMu.Lock()
	w.cfgLive = &live2
	w.cfgMu.Unlock()
	if state.backchannelSoftenFor("emp-a") {
		t.Fatal("second-swap resolve = true, want the live false")
	}
	if w.cfg.BackchannelSoften == nil || *w.cfg.BackchannelSoften {
		t.Fatal("test corrupted the boot snapshot")
	}

	// Hand-built state without the live seam: falls back to cfg, no panic.
	bare := &voiceLinkState{cfg: live}
	if !bare.backchannelSoftenFor("emp-a") {
		t.Fatal("nil-seam fallback must read the construction cfg snapshot")
	}
}
