package daemon

// Wiring-seam tests for internal/daemon/voice_wiring.go (bughunt wave,
// audit ids H2-wiring / H4 / M4 / M1). These exercise the DAEMON wiring,
// not the voice feature itself: fake telnyx seam + real Manager +
// real Registry + real TalkLoop, driven through VoiceWiring's own seams
// (routeFrame, releaseCall, spliceHangup, spliceRunner, WireVoice). No
// live child process, phone leg, or LLM is involved.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// fakeWiringTelnyx is a telnyx.CallController + DialController fake that
// records every call-control POST.
type fakeWiringTelnyx struct {
	mu      sync.Mutex
	hangups []string
	answers []string
	streams []string
}

func (f *fakeWiringTelnyx) AnswerCall(_ context.Context, ccid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, ccid)
	return nil
}

func (f *fakeWiringTelnyx) StartStreaming(_ context.Context, ccid string, _ string, _ telnyx.Codec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streams = append(f.streams, ccid)
	return nil
}

func (f *fakeWiringTelnyx) HangupCall(_ context.Context, ccid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hangups = append(f.hangups, ccid)
	return nil
}

func (f *fakeWiringTelnyx) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	return "cc-dial-test", nil
}

func (f *fakeWiringTelnyx) hangupCount(ccid string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.hangups {
		if id == ccid {
			n++
		}
	}
	return n
}

func (f *fakeWiringTelnyx) hangupCountAny() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hangups)
}

// blockingTurnRunner blocks in Run until its ctx is cancelled, then
// records whether it observed context.Canceled (the H4 contract).
type blockingTurnRunner struct {
	mu        sync.Mutex
	sawCancel bool
	started   chan struct{}
}

func newBlockingTurnRunner() *blockingTurnRunner {
	return &blockingTurnRunner{started: make(chan struct{})}
}

func (b *blockingTurnRunner) Run(ctx context.Context, _ voice.SessionHandle, _ string) (string, error) {
	close(b.started)
	<-ctx.Done()
	b.mu.Lock()
	b.sawCancel = errors.Is(ctx.Err(), context.Canceled)
	b.mu.Unlock()
	return "", ctx.Err()
}

func (b *blockingTurnRunner) cancelled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sawCancel
}

// allowChatter is a SupervisorChatter that allows everything (never
// consulted by these tests; it just keeps the gate non-degraded).
type allowChatter struct{}

func (allowChatter) Ask(context.Context, string) (string, error) {
	return `{"allowed": true, "reason": "test"}`, nil
}

// --- test wiring constructor ---

// shortMeeptHomeForTest points MEEPT_HOME at a path short enough for
// unix socket binds (sun_path, 103 bytes on macOS): a fixed-depth dir
// under the temp root keyed by a test-unique counter, removed at
// cleanup. The e2e sandboxes use the same leaf-15 shape.
var shortHomeMu sync.Mutex
var shortHomeSeq int

func shortMeeptHomeForTest(t *testing.T) string {
	t.Helper()
	shortHomeMu.Lock()
	shortHomeSeq++
	n := shortHomeSeq
	shortHomeMu.Unlock()
	base := os.TempDir()
	home := filepath.Join(base, fmt.Sprintf("mvt%d", n))
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("short MEEPT_HOME: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

// newVoiceWiringForTest builds a VoiceWiring over a fake telnyx seam.
// The wiring is NOT Started (no text server socket): the tests drive
// routeFrame/releaseCall/spliceHangup/spliceRunner directly. MEEPT_HOME
// is pointed at a SHORT temp root (the AudioPair paths must stay under
// the kernel sun_path limit, 103 bytes on macOS - t.TempDir() alone
// overruns it on this platform).
func newVoiceWiringForTest(t *testing.T) (*VoiceWiring, *fakeWiringTelnyx) {
	t.Helper()
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))

	tel := &fakeWiringTelnyx{}
	registry := voice.NewRegistry(2)
	// No spawn seam: HandleCallEvent tracks the call without a child.
	manager := voice.NewManager(voice.ManagerConfig{
		StreamURL: "wss://example.test/media",
		Codec:     telnyx.DefaultCodec,
	}, tel, nil)

	vlog := testLogger(t).With("component", "voice-wiring-test")
	// The release fan-out calls Employee.ReleaseHook; build the real
	// employee stack over throwaway stores (same shape as the hold tests
	// in voice_employee_test.go).
	employee, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      session.NewMemoryStore(testLogger(t)),
		LoopManager:   agent.NewManager(agent.ManagerConfig{}),
		TemplateLoop:  agent.NewAgentLoop("template-voice-wiring-test", t.TempDir()),
		WorkingDir:    filepath.Join(os.TempDir(), "mvt-work"),
		CallStorePath: filepath.Join(t.TempDir(), "calls.db"),
		Logger:        vlog.With("sub", "employee"),
	})
	if err != nil {
		t.Fatalf("WireVoiceEmployee: %v", err)
	}
	t.Cleanup(func() { _ = employee.Close() })
	w := &VoiceWiring{
		Manager:     manager,
		Registry:    registry,
		ChildProc:   voice.NewChildProc(voice.ChildProcConfig{}, vlog.With("sub", "child"), nil),
		TextServer:  voice.NewTextServer(filepath.Join(voice.SocketDir(), "test-text.sock"), vlog),
		Employee:    employee,
		supervisor:  voice.NewSupervisor(voice.SupervisorConfig{Enabled: false}, allowChatter{}, vlog),
		tel:         tel,
		loops:       make(map[string]*voice.TalkLoop),
		loopCancels: make(map[string]context.CancelFunc),
		bus:         nil,
		cfg: config.VoiceConfig{
			DefaultEmployee: "phone-employee",
		},
		logger: vlog,
	}
	return w, tel
}

// acquireFor registers a registry instance for ccid (the answer/dial-time
// Acquire the wiring's spawn seam normally performs).
func acquireFor(w *VoiceWiring, ccid string) voice.Instance {
	inst, err := w.Registry.Acquire(ccid, w.cfg.DefaultEmployee, "+15550000001")
	if err != nil {
		panic(fmt.Sprintf("registry acquire: %v", err))
	}
	return inst
}

// armLoop arms a talk loop + per-call context for ccid exactly as
// routeFrame does (the loop+ctx wiring under test lives in that code
// path; loop frames are fed through routeFrame itself).
func armLoop(w *VoiceWiring, ccid string, inst voice.Instance, turns voice.TurnRunner) {
	loop := voice.NewTalkLoop(voice.TalkDeps{
		Supervisor: w.supervisor,
		Turns:      turns,
		SessionFor: func(voice.Instance) voice.SessionHandle { return voice.SessionHandle{} },
		Send:       func(voice.Frame) error { return nil },
		OnEnd:      func(endCcid, reason string) { w.managerHangup(endCcid, reason) },
		Logger:     w.logger,
	}, voice.TalkLoopConfig{
		FillerLines: []string{"one moment"},
	})
	w.loopsMu.Lock()
	w.loops[ccid] = loop
	loopCtx, loopCancel := context.WithCancel(w.loopContext(ccid))
	w.loopCancels[ccid] = loopCancel
	w.loopsMu.Unlock()
	go func() {
		if runErr := loop.Run(loopCtx, inst); runErr != nil {
			w.logger.Debug("test talk loop ended", "ccid", ccid, "err", runErr)
		}
		loopCancel()
		w.loopsMu.Lock()
		delete(w.loops, ccid)
		delete(w.loopCancels, ccid)
		w.loopsMu.Unlock()
	}()
}

// trackCall drives the manager's inbound tracking for ccid (the manager
// owns release-hook firing exactly once per ccid).
func trackCall(t *testing.T, w *VoiceWiring, ccid string) {
	t.Helper()
	if err := w.Manager.HandleCallEvent(context.Background(), telnyx.CallEvent{
		EventType:     "call.initiated",
		CallControlID: ccid,
		Direction:     "incoming",
	}); err != nil {
		t.Fatalf("HandleCallEvent(initiated) %s: %v", ccid, err)
	}
}

// --- H4: releaseCall cancels the per-call talk-loop context ---

func TestReleaseCallCancelsTalkLoopContext(t *testing.T) {
	w, tel := newVoiceWiringForTest(t)
	const ccid = "cc-h4"
	inst := acquireFor(w, ccid)
	trackCall(t, w, ccid)

	// Arm the loop and start a turn: the turn runner blocks until its ctx
	// is cancelled.
	turns := newBlockingTurnRunner()
	armLoop(w, ccid, inst, turns)
	last := true
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "hello", Last: &last})
	select {
	case <-turns.started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn runner never started")
	}

	// Release: the loop must exit PROMPTLY (not after silence timers).
	releaseDone := make(chan struct{})
	go func() {
		w.releaseCall(ccid, voice.OutcomeHangup)
		close(releaseDone)
	}()
	start := time.Now()
	deadline := start.Add(2 * time.Second)
	for {
		w.loopsMu.Lock()
		_, stillArmed := w.loopCancels[ccid]
		w.loopsMu.Unlock()
		if !stillArmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("talk loop context not released promptly on releaseCall")
		}
		time.Sleep(2 * time.Millisecond)
	}
	<-releaseDone
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("release took %s; the loop must exit on ctx cancel, not silence timers", elapsed)
	}
	// Give the loop goroutine a beat to record the cancellation it
	// observed in the turn runner.
	turnDeadline := time.Now().Add(2 * time.Second)
	for !turns.cancelled() {
		if time.Now().After(turnDeadline) {
			t.Fatal("in-flight turn runner did not observe context.Canceled")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// releaseCall itself never issues carrier hangups (the manager/loop
	// own the hangup decision); a second release is a clean no-op.
	w.releaseCall(ccid, voice.OutcomeHangup)
	// The loop's OnEnd (managerHangup) fired the carrier POST once during
	// the ctx-cancel teardown; the second release must not add another.
	if n := tel.hangupCountAny(); n > 1 {
		t.Fatalf("release paths issued %d carrier hangups, want <= 1 (OnEnd only; releaseCall itself never hangs up)", n)
	}
}

func TestReleaseCallUnknownCCIDNoop(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	// No loop, no cancel armed: must not panic and must not leak.
	w.releaseCall("cc-never-armed", voice.OutcomeHangup)
	w.loopsMu.Lock()
	n := len(w.loopCancels)
	w.loopsMu.Unlock()
	if n != 0 {
		t.Fatalf("loopCancels leaked %d entries", n)
	}
}

// --- H2-wiring: spliceRunner populates SpliceConfig.CodecWire ---

// fakeMediaWS is a commhttp.WebSocketConn fake delivering canned frames
// then blocking until ctx cancel.
type fakeMediaWS struct {
	frames []string
	i      int
}

func (f *fakeMediaWS) Read(ctx context.Context) ([]byte, error) {
	if f.i < len(f.frames) {
		f.i++
		return []byte(f.frames[f.i-1]), nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeMediaWS) Write([]byte) error { return nil }
func (f *fakeMediaWS) Close() error       { return nil }

// TestSpliceRunnerCodecWireTranscodesPCMU drives w.spliceRunner with a
// PCMU-negotiated start frame: inbound mulaw frames must reach the child
// as 640-byte-aligned L16 chunks (transcode selected through CodecWire,
// child chunk ALWAYS the child-native 640 bytes). A wiring that drops
// CodecWire leaves wireCodecOf on the L16 fallback and passes raw mu-law
// through: the child stream is then a 160-byte multiple, never 640.
func TestSpliceRunnerCodecWireTranscodesPCMU(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	runner := w.spliceRunner()

	// Four sequenced PCMU wire frames (160 bytes each) - after mulaw
	// decode + upsample they fill ~2552 L16 bytes, i.e. >= 3 full
	// 640-byte child chunks.
	var frames []string
	for seq := 1; seq <= 4; seq++ {
		payload := make([]byte, 160)
		b64 := base64.StdEncoding.EncodeToString(payload)
		// Newline-terminated: the splice's inbound reader is a bufio
		// ReadBytes('\n') scanner; unterminated frames stay buffered.
		frames = append(frames, `{"event":"media","sequence_number":`+fmt.Sprintf("%d", seq)+`,"media":{"payload":"`+b64+`"}}`+"\n")
	}
	conn := &fakeMediaWS{frames: frames}

	// The splice runner creates its OWN AudioPair under SocketDir; the
	// test only dials the child side of those sockets once they appear
	// (creating a second pair over the same ccid would rebind/unlink the
	// runner's socket files).
	dir := voice.SocketDir()
	ccid := "cc-h2w"

	var childMu sync.Mutex
	var childGot []byte
	childDialDone := make(chan struct{})
	childDialStart := make(chan struct{})
	go func() {
		defer close(childDialDone)
		close(childDialStart)
		var c1, c2 net.Conn
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var e1, e2 error
			c1, e1 = net.Dial("unix", filepath.Join(dir, ccid+".in"))
			c2, e2 = net.Dial("unix", filepath.Join(dir, ccid+".out"))
			if e1 == nil && e2 == nil {
				break
			}
			if c1 != nil {
				_ = c1.Close()
				c1 = nil
			}
			if c2 != nil {
				_ = c2.Close()
				c2 = nil
			}
			time.Sleep(5 * time.Millisecond)
		}
		if c1 == nil || c2 == nil {
			return
		}
		defer func() { _ = c1.Close(); _ = c2.Close() }()
		buf := make([]byte, 8192)
		for {
			n, readErr := c1.Read(buf)
			if n > 0 {
				childMu.Lock()
				childGot = append(childGot, buf[:n]...)
				childMu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Start the splice on a cancellable ctx; the fake WS blocks after the
	// canned frames, so cancel ends it.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	<-childDialStart
	go func() {
		done <- runner(ctx, ccid, conn, commhttp.StartFrame{
			CallControlID:    ccid,
			Encoding:         "PCMU",
			SampleRate:       8000,
			TranscodeInbound: true,
		})
	}()

	// Wait for >= 2 full child chunks (4 PCMU frames upsample past 1280
	// bytes), then assert the child stream is 640-byte aligned.
	waitDeadline := time.Now().Add(3 * time.Second)
	total := 0
	for time.Now().Before(waitDeadline) {
		childMu.Lock()
		total = len(childGot)
		childMu.Unlock()
		if total >= 2*640 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("splice runner did not exit on cancel")
	}
	childMu.Lock()
	total = len(childGot)
	childMu.Unlock()
	if total < 2*640 {
		t.Fatalf("child received %d bytes, want >= %d (4 PCMU frames upsample past %d L16 bytes)", total, 2*640, 2*640)
	}
	if total%640 != 0 {
		t.Fatalf("child stream length %d is not a multiple of the 640-byte child chunk; CodecWire was not honored (raw mu-law passes through at 160-byte multiples)", total)
	}
	<-childDialDone
}

// --- M4: spliceHangup kills the live phone leg + fails the outcome ---

func TestSpliceHangupMirrorsCarrierHangup(t *testing.T) {
	w, tel := newVoiceWiringForTest(t)
	const ccid = "cc-m4"
	trackCall(t, w, ccid)

	released := make(chan struct{})
	w.Manager.SetReleaseHook(func(hookCcid string, outcome voice.Outcome) {
		if hookCcid == ccid {
			// M4 contract: the carrier POST happened BEFORE the outcome
			// fired (the leg is killed, not just the bookkeeping).
			if n := tel.hangupCount(ccid); n != 1 {
				t.Errorf("carrier hangups at release = %d, want 1", n)
			}
			if outcome != voice.OutcomeFailed {
				t.Errorf("outcome = %q, want failed", outcome)
			}
			close(released)
		}
	})

	w.spliceHangup(ccid, "voice: outbound queue saturated past grace window")
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("release hook never fired for the splice hangup")
	}
	if n := tel.hangupCount(ccid); n != 1 {
		t.Fatalf("spliceHangup issued %d carrier hangups, want exactly 1", n)
	}
}

func TestSpliceHangupUntrackedStillPostsCarrier(t *testing.T) {
	// FailOutcome on an untracked id is a no-op, but the M4 contract is
	// that the carrier POST always goes out (the live leg must die even
	// if daemon-side bookkeeping already ran).
	w, tel := newVoiceWiringForTest(t)
	w.spliceHangup("cc-untracked-m4", "test reason")
	if n := tel.hangupCount("cc-untracked-m4"); n != 1 {
		t.Fatalf("spliceHangup on untracked ccid issued %d carrier hangups, want 1", n)
	}
}

// --- M1: WireVoice honors voice.supervisor_model ---

// probeChatter identifies which chatter the supervisor gate rides.
type probeChatter struct{ id string }

func (p *probeChatter) Ask(context.Context, string) (string, error) {
	return "ALLOW", nil
}
func validWiringTestPubKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(ed25519.PublicKey(priv.Public().(ed25519.PublicKey)))
}

// testSecretsBroker builds a broker resolving telnyx_api_key from a temp
// file (the file kind avoids polluting the process environment).
func testSecretsBroker(t *testing.T) *secrets.Broker {
	t.Helper()
	tokenFile := filepath.Join(t.TempDir(), "telnyx-token")
	if err := os.WriteFile(tokenFile, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	broker, err := secrets.NewBroker(secrets.Config{
		"telnyx_api_key": {Kind: "file", Name: tokenFile},
	}, testLogger(t))
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	return broker
}

// wireVoiceTestDeps builds the full WireVoiceDeps for a supervisor-chatter
// test: real (in-memory/throwaway) employee stack deps, a temp MEEPT_HOME,
// a real telnyx token through the secrets broker, and the overrides the
// caller passes (chatter + factory).
func wireVoiceTestDeps(t *testing.T, supervisorModel string) WireVoiceDeps {
	t.Helper()
	t.Setenv("MEEPT_HOME", t.TempDir())
	sessions := session.NewMemoryStore(testLogger(t))
	template := agent.NewAgentLoop("template-voice-wiring-test", t.TempDir())
	loopMgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	return WireVoiceDeps{
		Config: &config.Config{Voice: config.VoiceConfig{
			Enabled:           true,
			TelnyxPublicKey:   validWiringTestPubKey(t),
			SupervisorModel:   supervisorModel,
			SupervisorEnabled: true,
			DefaultEmployee:   "phone-employee",
			PublicStreamURL:   "wss://example.test/media",
			Filler:            []string{"one moment"},
		}},
		Sessions: sessions,
		Loops:    loopMgr,
		Template: template,
		Broker:   testSecretsBroker(t),
		Logger:   testLogger(t),
	}
}

func TestWireVoiceSupervisorModelResolvesDedicatedChatter(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())

	dedicated := llm.NewClient(&llm.ModelConfig{BaseURL: "http://127.0.0.1:9", ModelID: "dedicated-fast"})
	deps := wireVoiceTestDeps(t, "fast-alias")
	deps.SupervisorChatterClient = func(modelRef string) *llm.Client {
		if modelRef == "fast-alias" {
			return dedicated
		}
		return nil
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer w.Employee.Close()
	if w.supervisor == nil {
		t.Fatal("supervisor not constructed")
	}

	// The gate rides the DEDICATED client: Check against the dedicated
	// (unreachable) endpoint lands in the transport-error fail-open path
	// (Reason: error/timeout), never an ALLOW - an ALLOW here would mean
	// a fallback chatter served the check instead of the dedicated
	// supervisor_model client.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	verdict, err := w.supervisor.Check(ctx, "cc-m1", "confirm the meeting at noon")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if verdict.Allowed || verdict.Reason == "allow" {
		t.Fatalf("verdict = %+v; the dedicated supervisor_model client must be in the loop (an unreachable endpoint fails open as error/timeout, never ALLOW)", verdict)
	}
}

func TestWireVoiceEmptySupervisorModelFallsBackToChatter(t *testing.T) {
	deps := wireVoiceTestDeps(t, "") // empty: legacy behavior
	factoryCalled := false
	deps.SupervisorChatter = &probeChatter{id: "classifier-fallback"}
	deps.SupervisorChatterClient = func(string) *llm.Client {
		factoryCalled = true
		return nil
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer w.Employee.Close()
	if w.supervisor == nil {
		t.Fatal("supervisor not constructed")
	}
	if factoryCalled {
		t.Fatal("empty supervisor_model must not invoke the dedicated-client factory")
	}
	// The fallback chatter is reachable: a Check goes to probeChatter and
	// its allow-all reply passes.
	verdict, err := w.supervisor.Check(context.Background(), "cc-m1b", "any sentence")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !verdict.Allowed {
		t.Fatalf("verdict = %+v, want allowed (fallback chatter in the loop)", verdict)
	}
}

func TestWireVoiceUnresolvableSupervisorModelFallsBackWithLog(t *testing.T) {
	deps := wireVoiceTestDeps(t, "no-such-model-anywhere")
	deps.SupervisorChatter = &probeChatter{id: "classifier-fallback"}
	deps.SupervisorChatterClient = func(string) *llm.Client {
		return nil // resolution failed
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer w.Employee.Close()
	// Unresolvable ref degrades to the classifier chatter, not a nil
	// gate: a Check still runs through the fallback.
	verdict, err := w.supervisor.Check(context.Background(), "cc-m1c", "any sentence")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !verdict.Allowed {
		t.Fatalf("verdict = %+v, want allowed (fallback chatter in the loop)", verdict)
	}
}

// bughunt 2026-11-07 voice F-6 pin: a duplicate setup frame for a live
// ccid must be IGNORED (the child-side handleSetup twin already is).
// Before the guard, routeFrame overwrote w.loops[ccid] and
// w.loopCancels[ccid]: the replaced loop kept running but received no
// further frames (the router looks up by map), and its silence timer
// eventually fired t.end -> managerHangup on the CURRENT live call; the
// lost cancel func leaked the context.
func TestRouteFrameDuplicateSetupIgnored(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-dup-setup"
	acquireFor(w, ccid)

	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	w.loopsMu.Lock()
	first := w.loops[ccid]
	firstCancel := w.loopCancels[ccid]
	w.loopsMu.Unlock()
	if first == nil {
		t.Fatal("precondition: first setup armed a talk loop")
	}

	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	w.loopsMu.Lock()
	second := w.loops[ccid]
	secondCancel := w.loopCancels[ccid]
	w.loopsMu.Unlock()

	if second != first {
		t.Fatal("F-6: duplicate setup REPLACED the live talk loop (orphaned loop + silence-timer hangup on the live call)")
	}
	if secondCancel == nil || firstCancel == nil {
		t.Fatal("precondition: per-call cancel armed on both reads")
	}
	t.Cleanup(func() { w.releaseCall(ccid, voice.OutcomeHangup) })
}
