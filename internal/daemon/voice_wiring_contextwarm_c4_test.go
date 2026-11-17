package daemon

// C4 hard-rule pins at the WARMER layer (issue #142, opts-shaped Warm).
//
//   - Rule 1: the shipped llm.Client.Warm carries NO alias parameter; the
//     warmer layer resolves the real turn's alias via the read-only
//     Resolver.PeekAliasForWarm and hands the serving candidate to Warm on
//     the opts (WithResolvedModel) + the accounting scope (WithTaskScope).
//   - Dispatcher shape: a *llm.ProviderManager chatter is itself the warm
//     dispatcher (it re-derives routing from the opts INSIDE the manager);
//     the unwrap chain must NOT collapse it to its primary entry, which
//     would warm the wrong endpoint whenever the alias serves a non-primary
//     provider.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// c4OptsRecordingChatter is a warm-capable chatter double that captures the
// opts the warmer layer passed to Warm.
type c4OptsRecordingChatter struct {
	mu      sync.Mutex
	records [][]llm.ChatOption
}

func (c *c4OptsRecordingChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "ok"}, nil
}

func (c *c4OptsRecordingChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "ok"}, nil
}

func (c *c4OptsRecordingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "c4-fake-model", ContextLimit: 32768}
}

func (c *c4OptsRecordingChatter) Warm(_ context.Context, _ []llm.ChatMessage, opts ...llm.ChatOption) error {
	c.mu.Lock()
	c.records = append(c.records, opts)
	c.mu.Unlock()
	return nil
}

func (c *c4OptsRecordingChatter) lastOpts() []llm.ChatOption {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.records) == 0 {
		return nil
	}
	return c.records[len(c.records)-1]
}

func (c *c4OptsRecordingChatter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

func c4ProvidersConfig() *llm.ProvidersConfig {
	return &llm.ProvidersConfig{
		Model: "testprov/default-model",
		Providers: map[string]llm.ProviderConfig{
			"testprov": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:1"},
				Models: map[string]llm.ModelDef{
					"default-model": {ContextLimit: 8192},
					"coder-model":   {ContextLimit: 16384},
				},
			},
		},
		ModelAliases: map[string]llm.ModelAliasEntry{
			"coder": {Models: []string{"testprov/coder-model"}, Timeout: 30, MaxFails: 1},
		},
	}
}

// TestContextWarmAliasResolvedAtWarmerLayer pins C4 rule 1 at the wiring
// seam: with a Resolver wired, a fired warm reaches the dispatcher with the
// alias RESOLVED on the opts (WithResolvedModel naming the alias's serving
// candidate) and the conversation scope stamped (WithTaskScope) — the
// shipped Warm itself takes no alias parameter.
func TestContextWarmAliasResolvedAtWarmerLayer(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg.ContextWarm = true
	w.cfg.ContextWarmMinTokens = 1
	w.cfg.TurnConfirmMs = 120

	const ccid = "cc-c4alias"
	inst := acquireFor(w, ccid)

	sessStore := session.NewMemoryStore(testLogger(t))
	sess, err := sessStore.Create("voice call")
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

	// Persona lane + the alias the real turn rides (modelRef precedence
	// in buildContextWarmPrefix: loop override -> loop model ref).
	fake := &c4OptsRecordingChatter{}
	tmpl := agent.NewAgentLoop("default", t.TempDir(),
		agent.WithLLMChatter(fake),
		agent.WithModelRef("coder"))
	cfgTmpl := tmpl.GetConfig()
	cfgTmpl.SystemPromptOveride = "You are C4-TEST, the alias-resolution warm employee."
	tmpl.SetConfig(cfgTmpl)
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	loop, err := loopMgr.GetOrCreateWired(sess.ID, t.TempDir(), tmpl)
	if err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}
	cfgLoop := loop.GetConfig()
	cfgLoop.SystemPromptOveride = "You are C4-TEST, the alias-resolution warm employee."
	loop.SetConfig(cfgLoop)

	resolver := llm.NewResolver(c4ProvidersConfig(), slog.New(slog.DiscardHandler))

	cw := w.newContextWarmer(context.Background(), contextWarmSources{
		Loops:    loopMgr,
		Template: tmpl,
		// Failing employee lookup + a live loop: the prefix renders from
		// the loop's persona override lane (the turn's degraded shape),
		// alias from the loop's model ref ("coder").
		Employees: cwStubResolver{err: errors.New("no definition needed")},
		Resolver:  resolver,
		Sessions:  sessStore,
	})
	if cw == nil {
		t.Fatal("enabled=true must construct the warmer")
	}
	voice.SetContextWarmRouting(cw)
	t.Cleanup(func() { voice.SetContextWarmRouting(nil) })

	armLoop(w, ccid, inst, &cwRecordingRunner{})
	w.routeFrame(voice.Frame{
		CallID: ccid, Event: voice.EventPrompt,
		Text: "c4aliasres probe partial whose weather report lands shortly please",
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fake.count() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	w.releaseCall(ccid, voice.OutcomeHangup)

	if fake.count() == 0 {
		t.Fatal("warm never reached the dispatcher")
	}
	opts := fake.lastOpts()
	if len(opts) == 0 {
		t.Fatal("warmer dispatched Warm with NO opts (scope + resolved alias must travel on the opts lane)")
	}
	mc := llm.ResolvedModelOf(opts)
	if mc == nil {
		t.Fatal("WithResolvedModel absent: the warmer layer did not resolve the alias before calling Warm (C4 rule 1)")
	}
	if mc.ProviderID != "testprov" || mc.ModelID != "coder-model" {
		t.Errorf("resolved candidate = %s/%s, want testprov/coder-model (the alias's serving member)",
			mc.ProviderID, mc.ModelID)
	}
}

// TestContextWarmProviderManagerIsDispatcher pins the unwrap shape: a
// session loop whose chatter is a ContextFirewall over a *llm.ProviderManager
// resolves the manager ITSELF as the warm dispatcher — the manager
// re-derives the alias routing from the opts; collapsing it to the primary
// entry would warm the wrong endpoint for a non-primary alias member.
func TestContextWarmProviderManagerIsDispatcher(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)

	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "primary", BaseURL: "http://127.0.0.1:1", ModelID: "m1"},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	tmpl := agent.NewAgentLoop("default", t.TempDir(), agent.WithLLMChatter(pm))
	loopMgr := agent.NewManager(agent.ManagerConfig{})
	if _, err := loopMgr.GetOrCreateWired("sess-pmdisp", t.TempDir(), tmpl); err != nil {
		t.Fatalf("GetOrCreateWired: %v", err)
	}

	src := contextWarmSources{Loops: loopMgr, Template: tmpl}
	d := w.resolveContextWarmDispatcher(src, "sess-pmdisp")
	if d == nil {
		t.Fatal("ProviderManager chain resolved nil")
	}
	if _, ok := d.(*llm.ProviderManager); !ok {
		t.Errorf("dispatcher = %T, want *llm.ProviderManager (the manager must own alias routing, not be unwrapped to its primary entry)", d)
	}
}
