package daemon

// 00T §3 + §4 gate tests (caller-authz plan, branch 01-identity):
//
//	§3 resolver fail-soft   - daemonCallerResolver over the contact store:
//	    known+approved / known+unapproved / unknown -> Info{Number} only /
//	    store error -> WARN + NO upward error (the call always proceeds,
//	    O1) / nil store -> fail-soft.
//	§4 stamping             - bindingTurnRunner.Run must stamp
//	    callerid.With(turnCtx, inst.Caller) alongside the existing
//	    employeeIDContextKey stamp (assert at the RIGHT layer: the ctx the
//	    onward Turns seam receives, i.e. the turn ctx), and the voice-built
//	    loop must carry agentID == employee id (O5) so the PreExecChecker
//	    routes the employee constitution. Details-capture posture mirrors
//	    internal/agent: the executor injects details["agent_id"] from the
//	    loop/executor agentID (loop.go:2710 WithExecutorAgentID(loop.agentID)
//	    -> executor.checkPermission details injection); the loop stamp is
//	    the observable the daemon fix is graded on.
//
// Frozen shapes this file pins as the executable spec for leaf 02:
//   - voice.CallerResolver interface (ResolveCaller(ctx, e164) ->
//     (callerid.Info, error)), Instance.Caller (callerid.Info, zero legal).
//   - daemonCallerResolver(store, logger) constructor returning something
//     satisfying voice.CallerResolver; store is the *contact.SQLiteStore
//     handle (nil tolerated: fail-soft).
//
// RED style: undefined symbols (callerid package, voice.CallerResolver /
// Instance.Caller field, daemonCallerResolver) until leaves 01+02 land;
// the agentID-fix assertion fails (loop keeps the template's "default"
// id) until leaf 02 stamps voice clones.
//
// Phone literals are built via callerGateE164 (fmt.Sprintf) on purpose:
// agent tooling redacts long digit runs on display, which corrupts
// source re-emitted from tool output.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/callerid"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// callerGateE164 builds a valid test E.164 (+1555 NNNNNNN).
func callerGateE164(n int) string { return fmt.Sprintf("+1555%07d", n) }

// The resolver constructor must satisfy the frozen voice.CallerResolver
// seam (compile-time pin - the daemon's resolver is what the voice
// package consumes at the answer site).
var _ voice.CallerResolver = daemonCallerResolver(nil, slog.Default())

// callerGateFakeCalls is a no-op voice.CallStoreT so the binding never
// touches sqlite (mirrors internal/voice binding_test's fakeCalls).
type callerGateFakeCalls struct{}

func (callerGateFakeCalls) Start(_ context.Context, _ voice.CallRecord) error { return nil }
func (callerGateFakeCalls) End(_ context.Context, _ string, _ string) error   { return nil }
func (callerGateFakeCalls) AppendTranscript(_ context.Context, _ string, _ voice.TranscriptLine) error {
	return nil
}
func (callerGateFakeCalls) TodayTranscripts(_ context.Context, _ string) ([]voice.CallTranscript, error) {
	return nil, nil
}
func (callerGateFakeCalls) SessionIDFor(_, _ string) (string, bool) { return "", false }

// callerGateFakeSessions is the voice.SessionStore fake (binding_test shape).
type callerGateFakeSessions struct {
	mu      sync.Mutex
	created int
}

func (f *callerGateFakeSessions) Create(name string) (voice.SessionHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created++
	return voice.SessionHandle{ID: "sess-1", ConversationID: "conv-1"}, nil
}
func (f *callerGateFakeSessions) List() ([]voice.NamedSession, error)              { return nil, nil }
func (f *callerGateFakeSessions) UpdateName(sessionID, name string) error          { return nil }
func (f *callerGateFakeSessions) SetEmployeeID(sessionID, employeeID string) error { return nil }

// callerGateCaptureTurns captures the FULL onward context of every turn -
// the §4 review checklist's "right layer": whatever bindingTurnRunner.Run
// handed to Binding.RunTurn is what this fake sees.
type callerGateCaptureTurns struct {
	mu  sync.Mutex
	ctx []context.Context
}

func (c *callerGateCaptureTurns) Run(ctx context.Context, _ voice.SessionHandle, _ string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = append(c.ctx, ctx)
	return "ok", nil
}

func (c *callerGateCaptureTurns) captured() []context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]context.Context, len(c.ctx))
	copy(out, c.ctx)
	return out
}

// callerGateLogs is a minimal slog handler collecting records so the
// fail-soft WARN (master: "nil/failing resolver -> fail-soft Info{Number}
// with WARN") is assertable.
type callerGateLogs struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *callerGateLogs) Enabled(context.Context, slog.Level) bool { return true }
func (l *callerGateLogs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r.Clone())
	return nil
}
func (l *callerGateLogs) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *callerGateLogs) WithGroup(string) slog.Handler      { return l }

// countAt counts records at or above a level (WARN-and-up posture).
func (l *callerGateLogs) countAt(atLeast slog.Level) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.records {
		if r.Level >= atLeast {
			n++
		}
	}
	return n
}

// callerGateLogger returns a logger whose records the returned handler
// captures.
func callerGateLogger() (*slog.Logger, *callerGateLogs) {
	h := &callerGateLogs{}
	return slog.New(h), h
}

// callerGateFakeProgress is the no-op voice.ProgressPublisher the
// binding requires (binding_test's captureProgress shape).
type callerGateFakeProgress struct{}

func (callerGateFakeProgress) PublishProgress(voice.ProgressEvent) {}

// putCaller stores one contact; t.Fatal on any store error (fixture).
func putCaller(t *testing.T, s *contact.SQLiteStore, id, name, e164 string, approved bool) {
	t.Helper()
	if err := s.Put(context.Background(), contact.Record{
		ID: id, DisplayName: name, Phone: e164, VoiceApproved: approved,
	}); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
}

// --- §3 resolver fail-soft ---

// TestCallerResolverKnownApproved pins the happy identity branch: an
// approved contact resolves to the full Info (Number, Name, Approved,
// Known) with no error.
func TestCallerResolverKnownApproved(t *testing.T) {
	logger, _ := callerGateLogger()
	store, err := contact.NewStore(t.TempDir() + "/contacts.db")
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	number := callerGateE164(1)
	putCaller(t, store, "c-ada", "Ada", number, true)

	r := daemonCallerResolver(store, logger)
	info, err := r.ResolveCaller(context.Background(), number)
	if err != nil {
		t.Fatalf("ResolveCaller known+approved: %v (resolver errors must fail-soft, never propagate)", err)
	}
	want := callerid.Info{Number: number, Name: "Ada", Approved: true, Known: true}
	if info != want {
		t.Errorf("ResolveCaller = %+v, want %+v", info, want)
	}
}

// TestCallerResolverKnownUnapproved pins the known-but-not-approved
// branch: Known=true, Approved=false (step 1c gates the ACTIONS).
func TestCallerResolverKnownUnapproved(t *testing.T) {
	logger, _ := callerGateLogger()
	store, err := contact.NewStore(t.TempDir() + "/contacts.db")
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	number := callerGateE164(2)
	putCaller(t, store, "c-ben", "Ben", number, false)

	r := daemonCallerResolver(store, logger)
	info, err := r.ResolveCaller(context.Background(), number)
	if err != nil {
		t.Fatalf("ResolveCaller known+unapproved: %v", err)
	}
	want := callerid.Info{Number: number, Name: "Ben", Approved: false, Known: true}
	if info != want {
		t.Errorf("ResolveCaller = %+v, want %+v", info, want)
	}
}

// TestCallerResolverUnknownFailSoft pins the unknown-number branch:
// fail-soft Info with ONLY the number set (O2: unknown = unapproved,
// Known=false), no error.
func TestCallerResolverUnknownFailSoft(t *testing.T) {
	logger, _ := callerGateLogger()
	store, err := contact.NewStore(t.TempDir() + "/contacts.db")
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	number := callerGateE164(9999)
	r := daemonCallerResolver(store, logger)
	info, err := r.ResolveCaller(context.Background(), number)
	if err != nil {
		t.Fatalf("ResolveCaller unknown: %v, want fail-soft nil error", err)
	}
	if info != (callerid.Info{Number: number}) {
		t.Errorf("ResolveCaller unknown = %+v, want Info{Number: %s} (Number-only fail-soft)", info, number)
	}
}

// TestCallerResolverStoreErrorFailSoft pins the broken-store branch: the
// lookup error becomes a WARN and NO upward error - the call ALWAYS
// proceeds (O1: answering is never gated). A cancelled ctx exercises the
// same posture (the 2s answer-time budget expiring must not block).
func TestCallerResolverStoreErrorFailSoft(t *testing.T) {
	logger, logs := callerGateLogger()
	store, err := contact.NewStore(t.TempDir() + "/contacts.db")
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close (forced lookup error): %v", err)
	}

	number := callerGateE164(3)
	r := daemonCallerResolver(store, logger)
	info, err := r.ResolveCaller(context.Background(), number)
	if err != nil {
		t.Fatalf("ResolveCaller with a failing store returned %v; master contract: fail-soft, the call proceeds", err)
	}
	if info != (callerid.Info{Number: number}) {
		t.Errorf("fail-soft Info = %+v, want Info{Number: %s}", info, number)
	}
	if logs.countAt(slog.LevelWarn) == 0 {
		t.Error("store-error fail-soft logged no WARN (master: fail-soft Info{Number} with WARN)")
	}

	// Cancelled ctx (2s answer-time budget expiring): same fail-soft.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	number2 := callerGateE164(4)
	info2, err2 := daemonCallerResolver(store, logger).ResolveCaller(ctx, number2)
	if err2 != nil {
		t.Fatalf("ResolveCaller with a cancelled ctx returned %v; a slow lookup must never block the answer", err2)
	}
	if info2 != (callerid.Info{Number: number2}) {
		t.Errorf("cancelled-ctx fail-soft Info = %+v, want Number-only", info2)
	}
}

// TestCallerResolverNilStoreFailSoft pins the never-wired posture: a nil
// contact store degrades to fail-soft Info{Number}, never a panic and
// never an upward error.
func TestCallerResolverNilStoreFailSoft(t *testing.T) {
	logger, logs := callerGateLogger()
	number := callerGateE164(5)
	r := daemonCallerResolver(nil, logger)
	info, err := r.ResolveCaller(context.Background(), number)
	if err != nil {
		t.Fatalf("ResolveCaller nil store: %v, want fail-soft nil", err)
	}
	if info != (callerid.Info{Number: number}) {
		t.Errorf("nil-store Info = %+v, want Number-only", info)
	}
	if logs.countAt(slog.LevelWarn) == 0 {
		t.Error("nil-store fail-soft logged no WARN")
	}
}

// --- §4 turn-ctx stamping ---

// newCallerGateBinding builds a voice.Binding over the package fakes.
func newCallerGateBinding(t *testing.T, turns *callerGateCaptureTurns) *voice.Binding {
	t.Helper()
	b, err := voice.NewBinding(voice.BindingDeps{
		Calls:    callerGateFakeCalls{},
		Sessions: &callerGateFakeSessions{},
		Turns:    turns,
		Progress: callerGateFakeProgress{},
	})
	if err != nil {
		t.Fatalf("voice.NewBinding: %v", err)
	}
	return b
}

// TestBindingTurnRunnerStampsCallerOnTurnCtx drives the REAL stamp site:
// turnRunnerFor(b, inst).Run -> Binding.RunTurn -> fake Turns seam. The
// captured onward ctx (the turn ctx, not the call ctx) must carry
// inst.Caller via callerid.From, AND the existing employee stamp must
// coexist (review checklist: stamps coexist on one ctx).
func TestBindingTurnRunnerStampsCallerOnTurnCtx(t *testing.T) {
	turns := &callerGateCaptureTurns{}
	b := newCallerGateBinding(t, turns)
	const ccid = "cc-caller-gate"
	number := callerGateE164(1)
	if _, err := b.OnCallStart(context.Background(), ccid, number, "emp-voice-1", "incoming", voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}

	caller := callerid.Info{Number: number, Name: "Ada", Approved: true, Known: true}
	inst := voice.Instance{
		CallControlID: ccid,
		EmployeeID:    "emp-voice-1",
		Target:        number,
		Caller:        caller,
	}

	gates := readyGatesFor(inst.CallControlID)
	runner := turnRunnerFor(b, gates, inst)
	if _, err := runner.Run(context.Background(), voice.SessionHandle{}, "hello"); err != nil {
		t.Fatalf("bindingTurnRunner.Run: %v", err)
	}

	captured := turns.captured()
	if len(captured) != 1 {
		t.Fatalf("captured %d turn ctxs, want exactly 1", len(captured))
	}
	turnCtx := captured[0]

	got, ok := callerid.From(turnCtx)
	if !ok {
		t.Fatal("turn ctx carries no callerid stamp (bindingTurnRunner.Run must callerid.With(turnCtx, inst.Caller))")
	}
	if got != caller {
		t.Errorf("turn ctx caller = %+v, want %+v", got, caller)
	}
	// The employee stamp coexists on the same ctx (keep-both contract).
	if aid := agentIDFromContext(turnCtx); aid != "emp-voice-1" {
		t.Errorf("employee stamp = %q, want emp-voice-1 (callerid stamp must ride ALONGSIDE it)", aid)
	}
	// The call ctx we passed in was never mutated (ctx values ride the
	// derived turn ctx only).
	if _, leaked := callerid.From(context.Background()); leaked {
		t.Error("callerid stamp leaked onto a fresh background ctx")
	}
}

// readyGatesFor builds a holdTracker with ccid's gate already ready.
// (2026-11-10 merge fixup: turnRunnerFor gained the hold-gate param
// from the sibling webhook-hold audit; tests that drive a single turn
// arm the gate ready before running.)
func readyGatesFor(ccid string) *holdTracker {
	ht := newHoldTracker()
	ht.arm(ccid).markReady()
	return ht
}

// TestBindingTurnRunnerStampsZeroCaller pins the zero-Info posture:
// master says Instance.Caller zero is legal, and the stamp is
// unconditional - enforcement must be able to distinguish "unknown
// caller stamped" (ok=true, zero) from "non-voice turn" (ok=false).
func TestBindingTurnRunnerStampsZeroCaller(t *testing.T) {
	turns := &callerGateCaptureTurns{}
	b := newCallerGateBinding(t, turns)
	const ccid = "cc-caller-zero"
	if _, err := b.OnCallStart(context.Background(), ccid, callerGateE164(9), "emp-voice-1", "incoming", voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	inst := voice.Instance{CallControlID: ccid, EmployeeID: "emp-voice-1", Caller: callerid.Info{}}
	if _, err := turnRunnerFor(b, readyGatesFor(ccid), inst).Run(context.Background(), voice.SessionHandle{}, "hi"); err != nil {
		t.Fatalf("run: %v", err)
	}
	captured := turns.captured()
	if len(captured) != 1 {
		t.Fatalf("captured %d ctxs, want 1", len(captured))
	}
	if _, ok := callerid.From(captured[0]); !ok {
		t.Error("zero Caller must still stamp (ok=true, zero Info != absent)")
	}
}

// --- §4 agentID fix (O5) ---

// TestVoiceTurnRunnerStampsEmployeeAgentID proves the constitution-gate
// routing fix at the loop level: the voice-built clone that reaches
// RunOnce must carry agentID == the session's employee id, NOT the
// template's "default". This is what the executor then injects as
// details["agent_id"] (loop.go WithExecutorAgentID(loop.agentID) ->
// executor.checkPermission details injection), i.e. the observable the
// PreExecChecker routes on. RED today: the clone inherits "default".
func TestVoiceTurnRunnerStampsEmployeeAgentID(t *testing.T) {
	logger, _ := callerGateLogger()
	sessions := session.NewMemoryStore(logger)
	// Template stamped "default" (the production daemon template's id,
	// components.go WithAgentID("default")) so the manager caches the
	// clone under the plain session id (agent.cacheKey special-cases
	// "default" -> Get(sess.ID) finds it).
	template := agent.NewAgentLoop("template-voice-gate", t.TempDir(), agent.WithAgentID("default"))
	if template == nil {
		t.Fatal("template loop nil")
	}
	const tmplAgentID = "default"
	if got := template.GetAgentID(); got != tmplAgentID {
		t.Fatalf("template agentID = %q, want %q (fixture precondition)", got, tmplAgentID)
	}

	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	runner := &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		logger:     logger,
		// employees stays nil: the documented degrade posture (template
		// persona). The agentID stamp must NOT depend on the persona
		// resolver - voice loops route enforcement by employee id even
		// when the definition lookup is off.
	}

	sess, err := sessions.Create("voice caller gate")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	const employeeID = "emp-voice-1"

	// Drive the real turn path. The cloned loop has no LLM client, so
	// RunOnce refuses fast with ErrNoLLMClient - the stamp must already
	// have landed on the cached loop BEFORE execution (the executor reads
	// agentID at execution time, and answering never depends on this).
	_, err = runner.Run(context.Background(), voice.SessionHandle{
		ID:             sess.ID,
		ConversationID: sess.ConversationID,
		EmployeeID:     employeeID,
	}, "hello")
	if err == nil {
		t.Fatal("expected the nil-LLM loop to refuse the turn (fixture precondition)")
	}

	loop, ok := mgr.Get(sess.ID)
	if !ok {
		t.Fatal("voice turn never built the session loop")
	}
	if got := loop.GetAgentID(); got != employeeID {
		t.Errorf("voice-built loop agentID = %q, want %q (O5: executor details must carry agent_id = employee id so the constitution gate routes)", got, employeeID)
	}
	// Global posture guard: the shared template stays "default" - the fix
	// is VOICE clones only.
	if got := template.GetAgentID(); got != tmplAgentID {
		t.Errorf("template loop agentID mutated to %q, want %q (voice-only fix)", got, tmplAgentID)
	}
}
