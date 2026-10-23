package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
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
// PersonaOpts each GetOrCreatePersona call received.
type fakeACPSessionProvider struct {
	sess       *fakeACPSession
	err        error
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
