package daemon

// Context-warm integration unit tests (phase2-context-warm leaf 02,
// daemon wiring half). Fake deps throughout: the warmer's Warm lane is
// recorded via contextWarmSources.Dispatch, the prefix comes from a real
// agent.Manager + template loop over an override persona (the voice
// employee lane shape), and the talk loop is the established armLoop
// helper from voice_wiring_test.go.
//
// Pins:
//   - enabled=false => warmer absent (newContextWarmer nil) and the
//     Route* package seams no-op: zero WarmFn calls.
//   - enabled=true  => a PARTIAL prompt frame reaches WarmFn with the
//     builder's rendered prefix (system = override persona lane, last
//     user = partial) and the session scope (the turn's accounting id).
//   - a FINAL (last:true) prompt cancels the pending warm AND the turn
//     dispatch still reaches the loop unchanged (the R7 supersede +
//     "the turn path is NOT modified" pin; the final-enqueue shape is
//     additionally covered by TestRouteFrameHeardOnlyOnSuccessfulEnqueue
//     and TestRouteFrameHeardFallsBackToTextWhenFromEmpty in
//     voice_wiring_telemetry_pins_test.go).
//   - releaseCall tears the warm state down (no late fire after hangup).
//   - a prefix-builder miss (no persona lane) skips cleanly: zero
//     WarmFn calls, no panic, the warmer counts the skip.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

type cwRecordedWarm struct {
	alias    string
	scope    string
	messages []llm.ChatMessage
}

type cwWarmRecorder struct {
	mu    sync.Mutex
	calls []cwRecordedWarm
}

func (r *cwWarmRecorder) fn() func(context.Context, string, []llm.ChatMessage, string) {
	return func(_ context.Context, alias string, messages []llm.ChatMessage, scope string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, cwRecordedWarm{alias: alias, scope: scope, messages: messages})
	}
}

func (r *cwWarmRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *cwWarmRecorder) last() cwRecordedWarm {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

// cwStubResolver is the EmployeeDefinitionResolver double: persona +
// optional model ref, with a failure knob for the skip path.
type cwStubResolver struct {
	def *employee.Employee
	err error
}

func (r cwStubResolver) GetEmployee(_ context.Context, _ string) (*employee.Employee, error) {
	return r.def, r.err
}

// cwSetup builds a test wiring with context_warm ON (window 120 ms,
// floor 1 token so trivial prefixes fire), a registered warmer over
// recording Dispatch, plus a live registry row + session binding +
// armed talk loop for one ccid. The real loop manager carries a
// session clone with the employee persona override pre-stamped (the
// after-first-turn shape), so BuildPrefix needs no employee lookup.
func cwSetup(t *testing.T, enabled bool, resolver EmployeeDefinitionResolver) (*VoiceWiring, *cwWarmRecorder, string) {
	t.Helper()
	w, _ := newVoiceWiringForTest(t)
	w.cfg.ContextWarm = enabled
	w.cfg.ContextWarmMinTokens = 1
	w.cfg.TurnConfirmMs = 120

	const ccid = "cc-ctxwarm"
	inst := acquireFor(w, ccid)

	// A real meept session (the store owns the session_id/conversation_id
	// duality the warmer must resolve for the warm/turn scope match).
	sessStore := session.NewMemoryStore(testLogger(t))
	sess, err := sessStore.Create("voice call")
	if err != nil {
		t.Fatalf("session create: %v", err)
	}
	sessionID := sess.ID

	// Call row so sessionFor resolves (employee, number) -> session.
	if err := w.Employee.store.Start(context.Background(), voice.CallRecord{
		CallID:     ccid,
		Number:     inst.Target,
		EmployeeID: inst.EmployeeID,
		Direction:  "incoming",
		SessionID:  sessionID,
	}); err != nil {
		t.Fatalf("call store start: %v", err)
	}

	// Loop manager: template (persona override lane pre-stamped, the
	// same field shape applyEmployeeDefinition leaves behind) + a
	// session clone keyed by the plain session id (default template
	// agent => agent.Manager cacheKey collapses to sessionID).
	tmpl := agent.NewAgentLoop("default", t.TempDir())
	cfgTmpl := tmpl.GetConfig()
	cfgTmpl.SystemPromptOveride = "You are CW-TEST, the context warm probe employee."
	tmpl.SetConfig(cfgTmpl)
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	loop, err := loopMgr.GetOrCreateWired(sessionID, t.TempDir(), tmpl)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	cfgLoop := loop.GetConfig()
	cfgLoop.SystemPromptOveride = "You are CW-TEST, the context warm probe employee."
	loop.SetConfig(cfgLoop)

	rec := &cwWarmRecorder{}
	cw := w.newContextWarmer(context.Background(), contextWarmSources{
		Loops:     loopMgr,
		Template:  tmpl,
		Employees: resolver,
		Sessions:  sessStore,
		Dispatch:  rec.fn(),
	})
	if enabled {
		if cw == nil {
			t.Fatal("enabled=true must construct the warmer")
		}
		voice.SetContextWarmRouting(cw)
		w.contextWarmer = cw
	} else {
		if cw != nil {
			t.Fatal("disabled config must NOT construct the warmer (zero overhead)")
		}
		voice.SetContextWarmRouting(nil)
	}
	t.Cleanup(func() { voice.SetContextWarmRouting(nil) })

	armLoop(w, ccid, inst, &cwRecordingRunner{})
	return w, rec, ccid
}

// cwRecordingRunner keeps the talk loop's turn lane observable without
// touching a session store (armLoop's helper shape).
type cwRecordingRunner struct {
	mu    sync.Mutex
	calls int
}

func (r *cwRecordingRunner) Run(_ context.Context, _ voice.SessionHandle, _ string) (string, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return "cw test reply", nil
}

// TestContextWarmDisabledNoWarms pins the zero-overhead posture: with
// voice.context_warm false the warmer is not even constructed, the route
// seam stays unregistered, and partial + final frames produce no WarmFn
// traffic while the frames still reach the loop (byte-identical legacy
// dispatch).
func TestContextWarmDisabledNoWarms(t *testing.T) {
	w, rec, ccid := cwSetup(t, false, nil)

	last := false
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarm disabled partial weather today please", Last: &last})
	// Settle past any possible window.
	time.Sleep(400 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Errorf("disabled: WarmFn fired %d times, want 0", n)
	}
	if voice.ContextWarmRouting() != nil {
		t.Error("disabled: warmer registered on the global route seam")
	}
	// The frames still flow unchanged: a final prompt with the seam
	// unregistered must not error the router (the loop's turn lane is
	// covered by TestRouteFrameHeardOnlyOnSuccessfulEnqueue).
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarm disabled final", Last: new(true)})
	time.Sleep(100 * time.Millisecond)
	w.releaseCall(ccid, voice.OutcomeHangup)
}

// TestContextWarmPartialReachesWarmFn pins the core flow: one partial
// prompt frame, after the debounce window, fires exactly one Warm with
// the builder's rendered prefix (system persona row first, the partial
// as the final user row) and the SESSION scope (the turn's accounting
// id — the same value RunOnce stamps via WithTaskScope).
func TestContextWarmPartialReachesWarmFn(t *testing.T) {
	w, rec, ccid := cwSetup(t, true, nil)
	defer func() { w.releaseCall(ccid, voice.OutcomeHangup) }()

	w.routeFrame(voice.Frame{
		CallID: ccid, Event: voice.EventPrompt,
		Text: "ctxwarmnonce tell me about the weather outside today please",
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && rec.count() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if rec.count() == 0 {
		t.Fatal("partial never reached WarmFn (routeFrame -> RoutePartial -> warmer broken)")
	}
	got := rec.last()
	if !strings.HasPrefix(got.scope, "conv-") {
		t.Errorf("warm scope = %q, want the session's CONVERSATION id (the turn's WithTaskScope accounting lane; session/conv duality)", got.scope)
	}
	if got.scope == "" {
		t.Error("warm scope empty — prompt_cache_key would be omitted entirely")
	}
	if len(got.messages) < 2 {
		t.Fatalf("prefix = %+v, want >= 2 messages (system + partial)", got.messages)
	}
	if got.messages[0].Role != llm.RoleSystem || !strings.Contains(got.messages[0].Content, "CW-TEST") {
		t.Errorf("prefix[0] = %+v, want the employee persona system row", got.messages[0])
	}
	last := got.messages[len(got.messages)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "ctxwarmnonce") {
		t.Errorf("prefix tail = %+v, want the partial as the final user row", last)
	}
}

// TestContextWarmFinalCancelsPending pins the supersede: a partial
// armed inside the window, then a final (last:true) prompt, must cancel
// the pending warm — and the FINAL's turn dispatch must still reach the
// loop unchanged (the integration may never gate the real turn).
func TestContextWarmFinalCancelsPending(t *testing.T) {
	w, rec, ccid := cwSetup(t, true, nil)
	defer func() { w.releaseCall(ccid, voice.OutcomeHangup) }()

	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarmnonce partial that the final supersedes now"})
	time.Sleep(30 * time.Millisecond) // inside the 120 ms window
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarmnonce final turn please answer", Last: new(true)})

	// Wait well past the window: the abandoned warm must never fire.
	time.Sleep(500 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Errorf("final supersede failed: %d warm(s) fired after the final frame", n)
	}
}

// TestContextWarmReleaseCallCancels pins the hangup teardown: a partial
// inside the window followed by releaseCall must never fire a warm for
// the dead call.
func TestContextWarmReleaseCallCancels(t *testing.T) {
	w, rec, ccid := cwSetup(t, true, nil)

	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarmnonce partial then hangup happens now"})
	time.Sleep(20 * time.Millisecond)
	w.releaseCall(ccid, voice.OutcomeHangup)

	time.Sleep(500 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Errorf("releaseCall teardown failed: %d warm(s) fired after hangup", n)
	}
}

// TestContextWarmBuilderMissSkips pins the clean-skip posture: enabled
// warmer, NO loop source (unresolvable session clone) and a failing
// employee resolver -> BuildPrefix returns ok=false -> zero WarmFn
// calls, zero panics; the warmer's skip counter moves.
func TestContextWarmBuilderMissSkips(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg.ContextWarm = true
	w.cfg.ContextWarmMinTokens = 1
	w.cfg.TurnConfirmMs = 120

	const ccid = "cc-ctxwarm-miss"
	inst := acquireFor(w, ccid)
	if err := w.Employee.store.Start(context.Background(), voice.CallRecord{
		CallID: ccid, Number: inst.Target, EmployeeID: inst.EmployeeID,
		Direction: "incoming", SessionID: "sess-miss",
	}); err != nil {
		t.Fatalf("call store start: %v", err)
	}

	rec := &cwWarmRecorder{}
	// No Loops/Template -> the builder's cfg source is missing (miss);
	// with a Template carrying NO persona override + a failing resolver
	// it must also miss instead of rendering a diverged prefix.
	tmpl := agent.NewAgentLoop("default", t.TempDir())
	cw := w.newContextWarmer(context.Background(), contextWarmSources{
		Loops:     agent.NewManager(agent.ManagerConfig{}),
		Template:  tmpl,
		Employees: cwStubResolver{err: errors.New("resolver down")},
		Dispatch:  rec.fn(),
	})
	if cw == nil {
		t.Fatal("enabled=true must construct the warmer")
	}
	voice.SetContextWarmRouting(cw)
	t.Cleanup(func() { voice.SetContextWarmRouting(nil) })

	armLoop(w, ccid, inst, &cwRecordingRunner{})
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt, Text: "ctxwarmnonce partial whose builder misses entirely"})
	time.Sleep(500 * time.Millisecond)
	if n := rec.count(); n != 0 {
		t.Errorf("builder-miss fired %d warm(s), want 0 (skip, never a diverged prefix)", n)
	}
	warms, skipped := cw.Stats()
	if warms != 0 || skipped != 1 {
		t.Errorf("stats = (%d warms, %d skipped), want (0, 1)", warms, skipped)
	}
	w.releaseCall(ccid, voice.OutcomeHangup)
}

// TestContextWarmDispatcherResolution pins the client-resolution chain:
// a session loop whose chatter is a ContextFirewall over a warm-capable
// chatter resolves through the SAME unwrap the turn dispatches through;
// a chain whose terminal chatter has no Warm lane resolves nil (never a
// guessed endpoint).
func TestContextWarmDispatcherResolution(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	fake := &cwFakeWarmChatter{}
	// WithLLMChatter + a non-nil chatter => NewAgentLoop wraps the
	// chatter in the ContextFirewall (the production voice shape); the
	// resolver must unwrap Inner() back to the warm-capable terminal.
	tmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(fake))
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	loop, err := loopMgr.GetOrCreateWired("sess-disp", t.TempDir(), tmpl)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	if _, isFirewall := loop.Chatter().(*llm.ContextFirewall); !isFirewall {
		t.Skip("loop chatter is not firewall-wrapped in this build shape; unwrap chain trivially direct")
	}

	src := contextWarmSources{Loops: loopMgr, Template: tmpl}
	if d := w.resolveContextWarmDispatcher(src, "sess-disp"); d == nil {
		t.Error("firewall -> warm-capable chatter resolved nil")
	}
	// Unknown session + a template whose chatter has no Warm lane: the
	// chain has no warm-aware client -> nil, never a panic.
	bare := agent.NewAgentLoop("default", t.TempDir())
	bareSrc := contextWarmSources{Loops: agent.NewManager(agent.ManagerConfig{}), Template: bare}
	if d := w.resolveContextWarmDispatcher(bareSrc, "sess-none"); d != nil {
		t.Errorf("unresolvable chain returned %v, want nil", d)
	}
}

// cwFakeWarmChatter is a warm-capable llm.Chatter double.
type cwFakeWarmChatter struct {
	mu    sync.Mutex
	warms int
}

func (c *cwFakeWarmChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "ok"}, nil
}

func (c *cwFakeWarmChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "ok"}, nil
}

func (c *cwFakeWarmChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "cw-fake-model", ContextLimit: 32768}
}

func (c *cwFakeWarmChatter) Warm(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) error {
	c.mu.Lock()
	c.warms++
	c.mu.Unlock()
	return nil
}

func (c *cwFakeWarmChatter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.warms
}

// TestContextWarmWindowReloadLive pins bughunt 2026-11-19 M6 at the daemon
// seam: resolveContextWarmWindow reads the LIVE snapshot (currentVoiceCfg),
// not the boot w.cfg, and reloadVoiceConfig pushes the resolved window into
// the constructed-once warmer via SetWindow. The disabled default stays a
// nil-warmer no-op (byte-identical).
func TestContextWarmWindowReloadLive(t *testing.T) {
	w, rec, ccid := cwSetup(t, true, nil)
	defer func() { w.releaseCall(ccid, voice.OutcomeHangup) }()

	// Boot snapshot: TurnConfirmMs=120 (cwSetup), no override.
	if got := w.resolveContextWarmWindow(); got != 120*time.Millisecond {
		t.Fatalf("boot window = %v, want 120ms", got)
	}

	// Simulate the daemon.reloaded republish: the LIVE snapshot carries a
	// new override while the boot w.cfg stays untouched (the keys frame
	// reads the same live value — the warm window must not diverge).
	live := w.cfg
	live.ContextWarmWindowMs = 45
	w.cfgMu.Lock()
	w.cfgLive = &live
	w.cfgMu.Unlock()
	if got := w.resolveContextWarmWindow(); got != 45*time.Millisecond {
		t.Fatalf("live window = %v, want the 45ms override from cfgLive (boot w.cfg must not win)", got)
	}
	if w.cfg.ContextWarmWindowMs != 0 {
		t.Fatal("test corrupted the boot snapshot")
	}

	// Push the resolved window into the warmer exactly as reloadVoiceConfig
	// does, then prove a partial debounces at the NEW window: fire inside
	// 45ms+slack, well before the boot 120ms window would have fired.
	w.contextWarmer.SetWindow(w.resolveContextWarmWindow())
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventPrompt,
		Text: "ctxwarmnonce reload window probe partial text for the live override"})
	deadline := time.Now().Add(110 * time.Millisecond)
	for time.Now().Before(deadline) && rec.count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if rec.count() == 0 {
		t.Fatal("warm did not fire inside the reloaded 45ms window (SetWindow reload-live broken)")
	}

	// Override cleared on a later reload: the window falls back to the
	// live turn_confirm_ms, never the boot value.
	live2 := live
	live2.ContextWarmWindowMs = 0
	live2.TurnConfirmMs = 300
	w.cfgMu.Lock()
	w.cfgLive = &live2
	w.cfgMu.Unlock()
	if got := w.resolveContextWarmWindow(); got != 300*time.Millisecond {
		t.Fatalf("fallback window = %v, want the live 300ms turn_confirm_ms", got)
	}
}

// TestContextWarmReloadNilWarmerNoOp pins the disabled-default posture:
// with context_warm false the warmer is nil, and the reload path's
// SetWindow call is a safe no-op (byte-identical boot, zero overhead).
func TestContextWarmReloadNilWarmerNoOp(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg.ContextWarm = false
	if cw := w.newContextWarmer(context.Background(), contextWarmSources{}); cw != nil {
		t.Fatal("disabled config constructed a warmer")
	}
	// The exact call reloadVoiceConfig makes when the feature is off.
	w.contextWarmer.SetWindow(w.resolveContextWarmWindow())
	if voice.ContextWarmRouting() != nil {
		t.Fatal("disabled reload registered a warmer")
	}
}

// TestContextWarmDispatcherResolvesConversationScope pins the wave-3
// identity fix (bughunt 2026-11-19): warmFn receives the warm SCOPE -
// BuildPrefix resolves the calls.db session id to the session's
// CONVERSATION id - while the loop manager keys on the SESSION id.
// resolveContextWarmDispatcher must resolve the duality through
// src.Sessions BEFORE the loop lookup so the warm rides the SESSION
// loop's chatter, never the template's. The employee model override
// travels with that loop identity (BuildPrefix reads GetModelOverride
// off the same loop).
func TestContextWarmDispatcherResolvesConversationScope(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	sessStore := session.NewMemoryStore(testLogger(t))
	sess, err := sessStore.Create("voice link warm scope")
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	sessionFake := &cwFakeWarmChatter{}
	templateFake := &cwFakeWarmChatter{}
	tmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(templateFake))
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	// Session loop cloned from a DIFFERENT chatter than the template:
	// the two fakes are distinguishable dispatcher identities.
	sessTmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(sessionFake))
	loop, err := loopMgr.GetOrCreateWired(sess.ID, t.TempDir(), sessTmpl)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	// The employee's persistent model override rides the session loop.
	loop.SetPersistentModelOverride("emp-override-model")
	// The prefix fn only warms a MIRRORABLE persona: give the session
	// loop a system prompt override (the template keeps none, so a
	// template-fallback resolution would refuse or diverge).
	sessCfg := loop.GetConfig()
	sessCfg.SystemPromptOveride = "session persona"
	loop.SetConfig(sessCfg)

	src := contextWarmSources{Loops: loopMgr, Template: tmpl, Sessions: sessStore}

	// The scope the warmer actually hands the resolver: the session's
	// CONVERSATION id (BuildPrefix output shape).
	d := w.resolveContextWarmDispatcher(src, sess.ConversationID)
	if d == nil {
		t.Fatal("conversation-id scope resolved nil; the session loop must win")
	}
	if d != warmDispatcher(sessionFake) {
		t.Fatalf("dispatcher = %T %p, want the SESSION loop chatter %p (template %p means the duality was skipped)", d, d, sessionFake, templateFake)
	}

	// A raw SESSION id still resolves (legacy direct-lookup shape).
	if d2 := w.resolveContextWarmDispatcher(src, sess.ID); d2 != warmDispatcher(sessionFake) {
		t.Fatalf("session-id scope dispatcher = %v, want the session loop chatter", d2)
	}

	// Prefix half parity (bughunt 2026-11-19 wave 4): buildContextWarmPrefix
	// must resolve the SAME duality - a conversation-id scope reads the
	// SESSION loop's cfg (employee override), not the template's, or the
	// prefix diverges from the dispatcher's client and the cache hit dies.
	// The prefix Alias carries the loop's persistent model override, so
	// it distinguishes the session loop from the template (ok=true alone
	// would pass on the template fallback too).
	prefixFn := w.buildContextWarmPrefix(src)
	prefix, ok := prefixFn("", sess.ConversationID, "partial speech")
	if !ok {
		t.Fatal("prefix fn refused a conversation-id scope with a live session loop")
	}
	if prefix.Alias != "emp-override-model" {
		t.Fatalf("prefix alias = %q, want the session loop's employee override (template cfg means the duality was skipped)", prefix.Alias)
	}
}

// TestContextWarmDispatcherUnknownScopeFallsBackToTemplate pins the
// graceful miss: a scope the session store does not know (and a nil
// store) never guesses a session - the lookup keeps the raw id, misses
// the manager, and falls through to the template chatter. An
// unresolvable chain stays nil (the existing nil-skip contract).
func TestContextWarmDispatcherUnknownScopeFallsBackToTemplate(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	sessStore := session.NewMemoryStore(testLogger(t))
	templateFake := &cwFakeWarmChatter{}
	tmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(templateFake))
	src := contextWarmSources{
		Loops:    agent.NewManager(agent.ManagerConfig{}),
		Template: tmpl,
		Sessions: sessStore,
	}
	d := w.resolveContextWarmDispatcher(src, "conv-unknown-scope")
	if d != warmDispatcher(templateFake) {
		t.Fatalf("unknown scope dispatcher = %v, want the template chatter fallback", d)
	}
	// nil store (hand-built wiring): same fallback, never a panic.
	src.Sessions = nil
	if d2 := w.resolveContextWarmDispatcher(src, "conv-unknown-scope"); d2 != warmDispatcher(templateFake) {
		t.Fatalf("nil-store scope dispatcher = %v, want the template chatter fallback", d2)
	}
	// No template, no loop: nil-skip contract holds.
	bare := contextWarmSources{Loops: agent.NewManager(agent.ManagerConfig{})}
	if d3 := w.resolveContextWarmDispatcher(bare, "conv-unknown-scope"); d3 != nil {
		t.Fatalf("unresolvable chain returned %v, want nil", d3)
	}
}

// TestContextWarmScopeRidesSessionChatterNoTurns is the end-to-end shape
// of the identity fix through the REAL warmer (no Dispatch override): a
// partial frame fired on a call whose session loop carries its OWN
// warm-capable chatter (employee override lane) lands the warm on THAT
// chatter - not the template's - with the conversation scope, and starts
// ZERO turns (R8: the warm never touches the turn lane).
func TestContextWarmScopeRidesSessionChatterNoTurns(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg.ContextWarm = true
	w.cfg.ContextWarmMinTokens = 1
	w.cfg.TurnConfirmMs = 120

	const ccid = "cc-ctxwarm-scope"
	inst := acquireFor(w, ccid)

	sessStore := session.NewMemoryStore(testLogger(t))
	sess, err := sessStore.Create("voice call scope pin")
	if err != nil {
		t.Fatalf("session create: %v", err)
	}
	if err := w.Employee.store.Start(context.Background(), voice.CallRecord{
		CallID:     ccid,
		Number:     inst.Target,
		EmployeeID: inst.EmployeeID,
		Direction:  "incoming",
		SessionID:  sess.ID,
	}); err != nil {
		t.Fatalf("call store start: %v", err)
	}

	sessionFake := &cwFakeWarmChatter{}
	templateFake := &cwFakeWarmChatter{}
	persona := "You are CW-SCOPE, the session-chatter warm probe employee."
	tmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(templateFake))
	cfgTmpl := tmpl.GetConfig()
	cfgTmpl.SystemPromptOveride = persona
	tmpl.SetConfig(cfgTmpl)
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	sessTmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(sessionFake))
	loop, err := loopMgr.GetOrCreateWired(sess.ID, t.TempDir(), sessTmpl)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	cfgLoop := loop.GetConfig()
	cfgLoop.SystemPromptOveride = persona
	loop.SetConfig(cfgLoop)
	loop.SetPersistentModelOverride("emp-override-model")

	cw := w.newContextWarmer(context.Background(), contextWarmSources{
		Loops:     loopMgr,
		Template:  tmpl,
		Employees: cwStubResolver{err: errors.New("no definition needed")},
		Sessions:  sessStore,
	})
	if cw == nil {
		t.Fatal("enabled=true must construct the warmer")
	}
	voice.SetContextWarmRouting(cw)
	w.contextWarmer = cw
	t.Cleanup(func() { voice.SetContextWarmRouting(nil) })

	turns := &cwRecordingRunner{}
	armLoop(w, ccid, inst, turns)
	w.routeFrame(voice.Frame{
		CallID: ccid, Event: voice.EventPrompt,
		Text: "ctxwarmnonce scope probe partial whose warm must ride the session chatter",
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && sessionFake.count() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	w.releaseCall(ccid, voice.OutcomeHangup)

	if sessionFake.count() == 0 {
		t.Fatal("warm never reached the SESSION loop chatter (conversation-id scope missed the loop lookup)")
	}
	if n := templateFake.count(); n != 0 {
		t.Errorf("template chatter warmed %d times; the session loop must own the warm", n)
	}
	turns.mu.Lock()
	n := turns.calls
	turns.mu.Unlock()
	if n != 0 {
		t.Errorf("warm started %d turns; R8 forbids any turn from the warm lane", n)
	}
}
