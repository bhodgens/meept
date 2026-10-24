package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
)

// fakeACPSession implements ACPSession for hermetic tests: Send returns a
// canned reply and (optionally) streams canned events first.
type fakeACPSession struct {
	reply  string
	err    error
	events []acp.SessionEvent
	sent   []string // prompt texts Send received
}

func (s *fakeACPSession) Send(ctx context.Context, text string) (string, error) {
	s.sent = append(s.sent, text)
	if s.err != nil {
		return "", s.err
	}
	return s.reply, nil
}

func (s *fakeACPSession) Events() <-chan acp.SessionEvent {
	ch := make(chan acp.SessionEvent, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	close(ch)
	return ch
}

// fakeACPSessionProvider implements ACPSessionProvider, recording the
// PersonaOpts each GetOrCreatePersona call received. returnNil models the
// manager's documented nil-session/nil-error signal (a plain nil
// *fakeACPSession would box into a non-nil ACPSession interface).
type fakeACPSessionProvider struct {
	sess       *fakeACPSession
	err        error
	returnNil  bool
	calls      int
	gotAgent   string
	gotPersona string
	gotWorkdir string
	gotOpts    acp.PersonaOpts
}

func (p *fakeACPSessionProvider) GetOrCreatePersona(
	ctx context.Context,
	agentID, persona, workdir string,
	opts acp.PersonaOpts,
) (ACPSession, error) {
	p.calls++
	p.gotAgent = agentID
	p.gotPersona = persona
	p.gotWorkdir = workdir
	p.gotOpts = opts
	if p.err != nil {
		return nil, p.err
	}
	if p.returnNil {
		return nil, nil
	}
	return p.sess, nil
}

func acpTestSpec() *AgentSpec {
	return &AgentSpec{
		ID:       "bob",
		Name:     "Bob",
		Role:     RoleExecutor,
		Enabled:  true,
		Executor: ExecutorACP,
		ACP: &ACPSpec{
			Agent:             "hermes",
			Persona:           "bob",
			CommandArgs:       []string{"-p", "borg"},
			PermissionMode:    "human",
			DecisionThreshold: 0.7,
		},
	}
}

// TestACPExecutorRun_HappyPath proves Run maps the spec's acp block onto
// GetOrCreatePersona field-by-field, streams updates through emit, and
// returns the final agent message.
func TestACPExecutorRun_HappyPath(t *testing.T) {
	sess := &fakeACPSession{
		reply: "the final answer",
		events: []acp.SessionEvent{
			{Kind: "message_chunk", Text: "the final "},
			{Kind: "message_chunk", Text: "answer"},
			{Kind: "tool_call", Text: "read_file"},
			{Kind: "done", Text: "the final answer"},
			{Kind: "closed", Text: ""},
		},
	}
	provider := &fakeACPSessionProvider{sess: sess}
	exec := newACPExecutorFromProvider(provider)

	var emitted []string
	got, err := exec.Run(context.Background(), acpTestSpec(), "/work/dir", "check that issue", func(updateText string) {
		emitted = append(emitted, updateText)
	})
	if err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	if got != "the final answer" {
		t.Errorf("Run final text = %q, want %q", got, "the final answer")
	}
	if provider.calls != 1 {
		t.Fatalf("GetOrCreatePersona calls = %d, want 1", provider.calls)
	}
	// persona lane arguments mapped from the spec.
	if provider.gotAgent != "hermes" {
		t.Errorf("agent = %q, want %q", provider.gotAgent, "hermes")
	}
	if provider.gotPersona != "bob" {
		t.Errorf("persona = %q, want %q", provider.gotPersona, "bob")
	}
	if provider.gotWorkdir != "/work/dir" {
		t.Errorf("workdir = %q, want %q", provider.gotWorkdir, "/work/dir")
	}
	// PersonaOpts mapped field-by-field.
	if strings.Join(provider.gotOpts.CommandArgs, ",") != "-p,borg" {
		t.Errorf("opts.CommandArgs = %v, want [-p borg]", provider.gotOpts.CommandArgs)
	}
	if provider.gotOpts.PermissionMode != "human" {
		t.Errorf("opts.PermissionMode = %q, want %q", provider.gotOpts.PermissionMode, "human")
	}
	if provider.gotOpts.DecisionThreshold != 0.7 {
		t.Errorf("opts.DecisionThreshold = %v, want 0.7", provider.gotOpts.DecisionThreshold)
	}
	// prompt text reached session/prompt.
	if len(sess.sent) != 1 || sess.sent[0] != "check that issue" {
		t.Errorf("session prompts = %v, want [check that issue]", sess.sent)
	}
	// streamed updates reached emit; the "done" duplicate and the empty
	// "closed" event did not.
	want := []string{"the final ", "answer", "read_file"}
	if strings.Join(emitted, "|") != strings.Join(want, "|") {
		t.Errorf("emitted = %v, want %v", emitted, want)
	}
}

// TestACPExecutorRun_NilACPSpecErrors proves Run fails for a spec without
// an acp: block (executor: acp with no harness binding).
func TestACPExecutorRun_NilACPSpecErrors(t *testing.T) {
	provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "x"}}
	exec := newACPExecutorFromProvider(provider)

	spec := &AgentSpec{ID: "bob", Executor: ExecutorACP}
	if _, err := exec.Run(context.Background(), spec, "/w", "hi", nil); !errors.Is(err, ErrNoACPSpecBinding) {
		t.Fatalf("Run error = %v, want ErrNoACPSpecBinding", err)
	}
	if _, err := exec.Run(context.Background(), nil, "/w", "hi", nil); !errors.Is(err, ErrNoACPSpecBinding) {
		t.Fatalf("Run(nil spec) error = %v, want ErrNoACPSpecBinding", err)
	}
	if provider.calls != 0 {
		t.Errorf("provider calls = %d, want 0 (binding checked before session)", provider.calls)
	}
}

// TestACPExecutorRun_ProviderError wraps the manager error.
func TestACPExecutorRun_ProviderError(t *testing.T) {
	provider := &fakeACPSessionProvider{err: acp.ErrAgentNotFound}
	exec := newACPExecutorFromProvider(provider)
	if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); !errors.Is(err, acp.ErrAgentNotFound) {
		t.Fatalf("Run error = %v, want wrapped acp.ErrAgentNotFound", err)
	}
}

// TestNewACPExecutor_NilManagerReturnsNil pins the kill-switch contract:
// a nil manager yields no executor, so injection setters stay no-ops.
func TestNewACPExecutor_NilManagerReturnsNil(t *testing.T) {
	if exec := NewACPExecutor(nil); exec != nil {
		t.Errorf("NewACPExecutor(nil) = %v, want nil", exec)
	}
	if exec := newACPExecutorFromProvider(nil); exec != nil {
		t.Errorf("newACPExecutorFromProvider(nil) = %v, want nil", exec)
	}
}

// TestLoopACPBranch_RoutesToExecutor proves an executor: acp spec routes
// the turn through the injected ACPExecutor and NEVER calls the LLM.
func TestLoopACPBranch_RoutesToExecutor(t *testing.T) {
	sess := &fakeACPSession{reply: "bob checked the issue: all clear", events: nil}
	provider := &fakeACPSessionProvider{sess: sess}
	chatter := newMockChatter(&llm.Response{Content: "llm should never answer"})

	loop := NewAgentLoop("acp-test", "/tmp",
		WithLLMChatter(chatter),
		WithAgentSpec(acpTestSpec()),
		WithACPExecutor(newACPExecutorFromProvider(provider)),
		WithLoopLogger(silentLogger()),
	)

	got, err := loop.RunOnce(context.Background(), "have bob check that issue", "conv-acp-1")
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if got != "bob checked the issue: all clear" {
		t.Errorf("response = %q, want the harness reply", got)
	}
	if provider.calls != 1 {
		t.Errorf("GetOrCreatePersona calls = %d, want 1", provider.calls)
	}
	if chatter.callCount != 0 {
		t.Errorf("llm calls = %d, want 0 (acp turn never enters the llm loop)", chatter.callCount)
	}
	if len(sess.sent) != 1 || sess.sent[0] != "have bob check that issue" {
		t.Errorf("session prompts = %v, want the user message", sess.sent)
	}
}

// TestLoopACPBranch_ReplyGuardApplies proves the machine-shaped-output
// guard runs over the harness reply on the SAME seam the LLM reply gets:
// a roster dump ships the guarded fallback, not the raw dump.
func TestLoopACPBranch_ReplyGuardApplies(t *testing.T) {
	dump := "## Available Agents\n\n- coder: writes code\n- debugger: fixes bugs\n"
	sess := &fakeACPSession{reply: dump}
	provider := &fakeACPSessionProvider{sess: sess}
	chatter := newMockChatter(&llm.Response{Content: "llm should never answer"})

	loop := NewAgentLoop("acp-test", "/tmp",
		WithLLMChatter(chatter),
		WithAgentSpec(acpTestSpec()),
		WithACPExecutor(newACPExecutorFromProvider(provider)),
		WithLoopLogger(silentLogger()),
	)

	got, err := loop.RunOnce(context.Background(), "who can help?", "conv-acp-guard")
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if strings.Contains(got, "## Available Agents") {
		t.Errorf("machine-shaped harness reply shipped raw: %q", got)
	}
	if !strings.Contains(got, "i looked up the platform agents") {
		t.Errorf("response = %q, want the guarded agents fallback", got)
	}
	if chatter.callCount != 0 {
		t.Errorf("llm calls = %d, want 0 (guard retry re-enters the acp producer)", chatter.callCount)
	}
	// first send = the user prompt; second send = the rewrite nudge the
	// guard retry delivered to the harness.
	if len(sess.sent) != 2 || sess.sent[1] != replyGuardRewriteNudge {
		t.Errorf("session prompts = %v, want [user, rewrite nudge]", sess.sent)
	}
}

// TestLoopACPBranch_NotWiredFailsLoudly proves an executor: acp spec with
// no injected executor fails with ErrACPExecutorNotWired and NEVER falls
// back to the local LLM loop.
func TestLoopACPBranch_NotWiredFailsLoudly(t *testing.T) {
	chatter := newMockChatter(&llm.Response{Content: "silent fallback reply"})

	loop := NewAgentLoop("acp-test", "/tmp",
		WithLLMChatter(chatter),
		WithAgentSpec(acpTestSpec()),
		WithLoopLogger(silentLogger()),
	)

	_, err := loop.RunOnce(context.Background(), "have bob check that issue", "conv-acp-unwired")
	if !errors.Is(err, ErrACPExecutorNotWired) {
		t.Fatalf("RunOnce error = %v, want ErrACPExecutorNotWired", err)
	}
	if !strings.Contains(err.Error(), "acp executor not wired") {
		t.Errorf("error text = %q, want it to name 'acp executor not wired'", err.Error())
	}
	if chatter.callCount != 0 {
		t.Errorf("llm calls = %d, want 0 (no silent fallback)", chatter.callCount)
	}
}

// TestLoopACPBranch_LocalSpecUnaffected proves a local-loop spec keeps the
// LLM reasoning cycle even when an ACPExecutor is injected.
func TestLoopACPBranch_LocalSpecUnaffected(t *testing.T) {
	sess := &fakeACPSession{reply: "harness reply"}
	provider := &fakeACPSessionProvider{sess: sess}
	chatter := newMockChatter(&llm.Response{Content: "the local llm answer"})

	loop := NewAgentLoop("local-test", "/tmp",
		WithLLMChatter(chatter),
		WithAgentSpec(&AgentSpec{ID: "chat", Role: RoleExecutor, Enabled: true}),
		WithACPExecutor(newACPExecutorFromProvider(provider)),
		WithLoopLogger(silentLogger()),
	)

	got, err := loop.RunOnce(context.Background(), "hello", "conv-local")
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if got != "the local llm answer" {
		t.Errorf("response = %q, want the llm reply", got)
	}
	if provider.calls != 0 {
		t.Errorf("GetOrCreatePersona calls = %d, want 0 for a local spec", provider.calls)
	}
}

// TestLoopACPBranch_RegistrySpecFallback proves a registry-built loop
// (constructed without a spec option) still resolves the executor branch
// through AgentRegistry.GetSpec.
func TestLoopACPBranch_RegistrySpecFallback(t *testing.T) {
	registry := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	if err := registry.RegisterSpec(acpTestSpec()); err != nil {
		t.Fatalf("RegisterSpec: %v", err)
	}
	sess := &fakeACPSession{reply: "registry-routed harness reply"}
	provider := &fakeACPSessionProvider{sess: sess}
	registry.SetACPExecutor(newACPExecutorFromProvider(provider))

	loop, err := registry.GetForTask("bob", "task-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	// the loop needs an llm client to pass RunOnceWithParts' entry guard;
	// the acp branch must never call it.
	chatter := newMockChatter(&llm.Response{Content: "llm should never answer"})
	WithLLMChatter(chatter)(loop)

	got, err := loop.RunOnce(context.Background(), "have bob check that issue", "conv-registry-acp")
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if got != "registry-routed harness reply" {
		t.Errorf("response = %q, want the harness reply", got)
	}
	if provider.calls != 1 {
		t.Errorf("GetOrCreatePersona calls = %d, want 1", provider.calls)
	}
	if chatter.callCount != 0 {
		t.Errorf("llm calls = %d, want 0", chatter.callCount)
	}
}

// TestSetACPExecutor_NilSafe pins the repo setter rule: nil receiver and
// nil executor never panic and never clobber a wired executor.
func TestSetACPExecutor_NilSafe(t *testing.T) {
	var nilLoop *AgentLoop
	nilLoop.SetACPExecutor(nil)
	nilLoop.SetACPExecutor(&ACPExecutor{})
	if got := nilLoop.ACPExecutor(); got != nil {
		t.Errorf("nil loop ACPExecutor = %v, want nil", got)
	}

	exec := newACPExecutorFromProvider(&fakeACPSessionProvider{})
	loop := NewAgentLoop("setter-test", "/tmp", WithLoopLogger(silentLogger()))
	loop.SetACPExecutor(exec)
	if loop.ACPExecutor() != exec {
		t.Error("SetACPExecutor did not wire the executor")
	}
	// nil executor is a no-op: the wired executor survives.
	loop.SetACPExecutor(nil)
	if loop.ACPExecutor() != exec {
		t.Error("SetACPExecutor(nil) clobbered the wired executor")
	}

	var nilRegistry *AgentRegistry
	nilRegistry.SetACPExecutor(nil)
	nilRegistry.SetACPExecutor(exec)
	if got := nilRegistry.ACPExecutor(); got != nil {
		t.Errorf("nil registry ACPExecutor = %v, want nil", got)
	}

	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.SetACPExecutor(nil)
	if got := reg.ACPExecutor(); got != nil {
		t.Errorf("registry ACPExecutor after nil set = %v, want nil", got)
	}
	reg.SetACPExecutor(exec)
	if got := reg.ACPExecutor(); got != exec {
		t.Error("registry SetACPExecutor did not wire the executor")
	}
}

// --- acp.persona.turns metrics (Phase 2) ---

// recordedMetric captures one TurnMetricRecorder.Record call.
type recordedMetric struct {
	name  string
	value float64
	tags  map[string]string
}

// fakeTurnMetricRecorder implements TurnMetricRecorder for hermetic tests.
type fakeTurnMetricRecorder struct {
	records []recordedMetric
}

func (r *fakeTurnMetricRecorder) Record(name string, value float64, tags map[string]string) {
	cp := make(map[string]string, len(tags))
	for k, v := range tags {
		cp[k] = v
	}
	r.records = append(r.records, recordedMetric{name: name, value: value, tags: cp})
}

func (r *fakeTurnMetricRecorder) only(t *testing.T) recordedMetric {
	t.Helper()
	if len(r.records) != 1 {
		t.Fatalf("acp.persona.turns records = %d, want 1 (%+v)", len(r.records), r.records)
	}
	m := r.records[0]
	if m.name != ACPTurnsMetric {
		t.Errorf("metric name = %q, want %q", m.name, ACPTurnsMetric)
	}
	if m.value != 1 {
		t.Errorf("metric value = %v, want 1 (counter)", m.value)
	}
	return m
}

// TestACPTurnMetrics_HappyPath proves one success datapoint per completed
// turn with exact tags {agent, persona, outcome}.
func TestACPTurnMetrics_HappyPath(t *testing.T) {
	provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "all clear"}}
	exec := newACPExecutorFromProvider(provider)
	rec := &fakeTurnMetricRecorder{}
	exec.SetTurnMetricRecorder(rec)

	if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "check", nil); err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	m := rec.only(t)
	wantTags := map[string]string{"agent": "hermes", "persona": "bob", "outcome": "success"}
	if len(m.tags) != len(wantTags) {
		t.Fatalf("tags = %v, want exactly %v", m.tags, wantTags)
	}
	for k, v := range wantTags {
		if m.tags[k] != v {
			t.Errorf("tag %q = %q, want %q", k, m.tags[k], v)
		}
	}
}

// TestACPTurnMetrics_EmptyPersonaTagged proves a personaless spec emits
// the counter with persona: "" (documented: empty is a valid tag value).
func TestACPTurnMetrics_EmptyPersonaTagged(t *testing.T) {
	spec := acpTestSpec()
	spec.ACP.Persona = ""
	provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "ok"}}
	exec := newACPExecutorFromProvider(provider)
	rec := &fakeTurnMetricRecorder{}
	exec.SetTurnMetricRecorder(rec)

	if _, err := exec.Run(context.Background(), spec, "/w", "hi", nil); err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	m := rec.only(t)
	if m.tags["persona"] != "" {
		t.Errorf("persona tag = %q, want empty string", m.tags["persona"])
	}
	if m.tags["agent"] != "hermes" || m.tags["outcome"] != "success" {
		t.Errorf("tags = %v, want agent=hermes outcome=success", m.tags)
	}
}

// TestACPTurnMetrics_ErrorPaths proves provider/session-resolution/Send
// failures each emit exactly one datapoint with outcome=error, and a
// caller-cancelled Send emits outcome=cancelled.
func TestACPTurnMetrics_ErrorPaths(t *testing.T) {
	t.Run("provider error", func(t *testing.T) {
		exec := newACPExecutorFromProvider(&fakeACPSessionProvider{err: acp.ErrAgentNotFound})
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); err == nil {
			t.Fatal("Run error = nil, want wrapped provider error")
		}
		m := rec.only(t)
		if m.tags["outcome"] != "error" {
			t.Errorf("outcome = %q, want error", m.tags["outcome"])
		}
	})
	t.Run("nil session", func(t *testing.T) {
		exec := newACPExecutorFromProvider(&fakeACPSessionProvider{returnNil: true})
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); !errors.Is(err, ErrNoACPSession) {
			t.Fatalf("Run error = %v, want ErrNoACPSession", err)
		}
		m := rec.only(t)
		if m.tags["outcome"] != "error" {
			t.Errorf("outcome = %q, want error", m.tags["outcome"])
		}
	})
	t.Run("send error", func(t *testing.T) {
		exec := newACPExecutorFromProvider(&fakeACPSessionProvider{sess: &fakeACPSession{err: errors.New("prompt failed")}})
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); err == nil {
			t.Fatal("Run error = nil, want wrapped send error")
		}
		m := rec.only(t)
		if m.tags["outcome"] != "error" {
			t.Errorf("outcome = %q, want error", m.tags["outcome"])
		}
	})
	t.Run("cancelled send", func(t *testing.T) {
		exec := newACPExecutorFromProvider(&fakeACPSessionProvider{sess: &fakeACPSession{err: context.Canceled}})
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := exec.Run(ctx, acpTestSpec(), "/w", "hi", nil); err == nil {
			t.Fatal("Run error = nil, want send error")
		}
		m := rec.only(t)
		if m.tags["outcome"] != "cancelled" {
			t.Errorf("outcome = %q, want cancelled", m.tags["outcome"])
		}
	})
}

// TestACPTurnMetrics_WiringFailuresNotCounted proves the counter is NOT
// emitted on the wiring-failure paths: Run on an executor without a
// provider (ErrACPExecutorNotWired), a spec without an acp binding
// (ErrNoACPSpecBinding), and the loop's not-wired branch (which never
// reaches an executor to record on at all).
func TestACPTurnMetrics_WiringFailuresNotCounted(t *testing.T) {
	t.Run("executor not wired", func(t *testing.T) {
		exec := &ACPExecutor{} // no provider: Run must hit the wiring guard
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); !errors.Is(err, ErrACPExecutorNotWired) {
			t.Fatalf("Run error = %v, want ErrACPExecutorNotWired", err)
		}
		if len(rec.records) != 0 {
			t.Errorf("records = %d, want 0 (wiring failure is not a turn)", len(rec.records))
		}
	})
	t.Run("no acp binding", func(t *testing.T) {
		exec := newACPExecutorFromProvider(&fakeACPSessionProvider{sess: &fakeACPSession{reply: "x"}})
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		spec := &AgentSpec{ID: "bob", Executor: ExecutorACP}
		if _, err := exec.Run(context.Background(), spec, "/w", "hi", nil); !errors.Is(err, ErrNoACPSpecBinding) {
			t.Fatalf("Run error = %v, want ErrNoACPSpecBinding", err)
		}
		if len(rec.records) != 0 {
			t.Errorf("records = %d, want 0 (binding failure is not a turn)", len(rec.records))
		}
	})
	t.Run("loop not-wired branch", func(t *testing.T) {
		rec := &fakeTurnMetricRecorder{}
		// an executor WITH a recorder, but the loop gets none at all.
		wired := newACPExecutorFromProvider(&fakeACPSessionProvider{sess: &fakeACPSession{reply: "x"}})
		wired.SetTurnMetricRecorder(rec)
		chatter := newMockChatter(&llm.Response{Content: "fallback"})
		loop := NewAgentLoop("acp-metrics-unwired", "/tmp",
			WithLLMChatter(chatter),
			WithAgentSpec(acpTestSpec()),
			WithLoopLogger(silentLogger()),
		)
		if _, err := loop.RunOnce(context.Background(), "have bob check that", "conv-acp-m-unwired"); !errors.Is(err, ErrACPExecutorNotWired) {
			t.Fatalf("RunOnce error = %v, want ErrACPExecutorNotWired", err)
		}
		if len(rec.records) != 0 {
			t.Errorf("records = %d, want 0", len(rec.records))
		}
	})
}

// TestACPTurnMetrics_LoopE2E proves a full loop turn (runTurnProducer ->
// runACPTurn -> Run) emits exactly one datapoint on the happy path and
// one outcome=error datapoint when the harness Send fails.
func TestACPTurnMetrics_LoopE2E(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "checked."}}
		exec := newACPExecutorFromProvider(provider)
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		chatter := newMockChatter(&llm.Response{Content: "never"})
		loop := NewAgentLoop("acp-metrics-e2e", "/tmp",
			WithLLMChatter(chatter),
			WithAgentSpec(acpTestSpec()),
			WithACPExecutor(exec),
			WithLoopLogger(silentLogger()),
		)
		if _, err := loop.RunOnce(context.Background(), "have bob check that", "conv-acp-m-ok"); err != nil {
			t.Fatalf("RunOnce error = %v", err)
		}
		m := rec.only(t)
		if m.tags["outcome"] != "success" || m.tags["agent"] != "hermes" || m.tags["persona"] != "bob" {
			t.Errorf("tags = %v, want {hermes bob success}", m.tags)
		}
	})
	t.Run("error", func(t *testing.T) {
		provider := &fakeACPSessionProvider{sess: &fakeACPSession{err: errors.New("harness down")}}
		exec := newACPExecutorFromProvider(provider)
		rec := &fakeTurnMetricRecorder{}
		exec.SetTurnMetricRecorder(rec)
		chatter := newMockChatter(&llm.Response{Content: "never"})
		loop := NewAgentLoop("acp-metrics-e2e-err", "/tmp",
			WithLLMChatter(chatter),
			WithAgentSpec(acpTestSpec()),
			WithACPExecutor(exec),
			WithLoopLogger(silentLogger()),
		)
		if _, err := loop.RunOnce(context.Background(), "have bob check that", "conv-acp-m-err"); err == nil {
			t.Fatal("RunOnce error = nil, want harness error")
		}
		m := rec.only(t)
		if m.tags["outcome"] != "error" {
			t.Errorf("outcome = %q, want error", m.tags["outcome"])
		}
	})
}

// TestSetTurnMetricRecorder_NilSafe pins the repo setter rule: nil
// receiver and nil recorder never panic, and a nil recorder leaves a
// wired recorder intact.
func TestSetTurnMetricRecorder_NilSafe(t *testing.T) {
	var nilExec *ACPExecutor
	nilExec.SetTurnMetricRecorder(nil) // must not panic

	exec := newACPExecutorFromProvider(&fakeACPSessionProvider{sess: &fakeACPSession{reply: "x"}})
	rec := &fakeTurnMetricRecorder{}
	exec.SetTurnMetricRecorder(rec)
	exec.SetTurnMetricRecorder(nil)
	if _, err := exec.Run(context.Background(), acpTestSpec(), "/w", "hi", nil); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(rec.records) != 1 {
		t.Errorf("records = %d, want 1 (nil recorder must not clobber the wired one)", len(rec.records))
	}
}

// TestMetricsStoreSatisfiesTurnRecorder is the seam-shape guard: the
// compile-time assertion in acp_executor.go already pins it; this keeps a
// readable test name in the suite.
func TestMetricsStoreSatisfiesTurnRecorder(t *testing.T) {
	var _ TurnMetricRecorder = (*metrics.Store)(nil)
}
