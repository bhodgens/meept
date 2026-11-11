package agent

// Pin tests for the bughunt fix-wave F4 findings (C1, C2, C3, C4, C18, C19,
// C22, C23). Each test names its finding and pins the exact regression
// behavior the fix restores.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/pii"
)

// --- C1: TierFloorError never records an alias failure / rotates ------------

// TestAgentLoop_TierFloorErrorNoRotationRetry (C1): a *llm.TierFloorError is
// a classified terminal verdict — it must surface WITHOUT a rotation retry
// (one chatter call) and WITHOUT RecordAliasFailure (ConsecutiveFails == 0).
// Pre-fix it fell into the generic branch: cooldown-damaged healthy members
// and burned the retry budget spraying the degraded tier.
func TestAgentLoop_TierFloorErrorNoRotationRetry(t *testing.T) {
	floorErr := &llm.TierFloorError{
		ProviderID: "p1",
		ModelID:    "m1",
		Depth:      3,
		Limit:      2,
	}
	chatter := &errChatter{err: floorErr}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "k1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "k2", ProviderID: "p2"},
	)
	loop := newThrottleTestLoop(t, chatter, resolver)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-tier-floor")
	if err == nil {
		t.Fatal("RunOnce = nil error, want the tier floor surfaced")
	}
	if !llm.IsTierFloorError(err) {
		t.Fatalf("surfaced error = %T (%v), want a *llm.TierFloorError", err, err)
	}
	// No alias health damage: the floor is a policy verdict, not a failure.
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); ok && fails != 0 {
		t.Errorf("alias ConsecutiveFails = %d, want 0 (floor error must never RecordAliasFailure)", fails)
	}
	// No rotation retry: exactly one dispatch attempt.
	if got := chatter.chatterCalls(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (no rotation retry on a tier floor)", got)
	}
}

// --- C2: throttle resume re-parks on a QuotaResetError ----------------------

// TestThrottleResumeQuotaErrorReparks (C2): a QuotaResetError surfacing
// during a throttle resume must RE-PARK the turn (D9 park-not-fail), not
// terminalize it. Pre-fix resumeThrottledTurn failed the resume on ANY error.
func TestThrottleResumeQuotaErrorReparks(t *testing.T) {
	quotaErr := &llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		ResetAt:    time.Now().Add(time.Hour),
		MaxWait:    24 * time.Hour,
	}
	chatter := &errChatter{err: quotaErr}
	loop, _, parker, _ := newThrottleResumeLoop(t, chatter)

	// Build a parked throttle record the resume path can decode, and run the
	// resume callback directly (the drain machinery is pinned elsewhere).
	rec := ParkedTurnRecord{
		ConversationID: "conv-quota-resume",
		SessionID:      "sess-throttle-resume",
		Class:          llm.FailureThrottle,
		Attempt:        1,
		TurnPayload:    []byte(`{"message":"hello","conversation_id":"conv-quota-resume"}`),
	}
	before := parker.Pending()
	loop.resumeThrottledTurn(context.Background(), rec)

	// The turn must be re-parked (pending grew by one) at the quota reset
	// time, with the grown attempt — NOT terminalized.
	if got := parker.Pending(); got != before+1 {
		t.Fatalf("pending after resume = %d, want %d (re-parked, not failed)", got, before+1)
	}
	reparked := parkedThrottleRecord(parker)
	if reparked.Attempt != rec.Attempt+1 {
		t.Errorf("re-park attempt = %d, want %d (grown across generations)", reparked.Attempt, rec.Attempt+1)
	}
	if reparked.ResumeAt.Before(time.Now()) {
		t.Errorf("re-park resume time %v is in the past, want the quota reset time", reparked.ResumeAt)
	}
	// The resume made the LLM call exactly once (the failing resume attempt).
	if got := chatter.chatterCalls(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (resume attempt only)", got)
	}
}

// --- C3: quota/budget resume defers delivery on the re-parked shape --------

// TestQuotaResumeReparkedShapeDefersDelivery (C3): when RunOnceWithParts
// returns the re-parked shape ("", nil) — the turn parked again mid-flight
// on the throttle parker — the quota resume must NOT persist an empty reply,
// NOT send chat.response, and NOT emit a completed turn.terminal.
func TestQuotaResumeReparkedShapeDefersDelivery(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	// First chatter call throttles: the loop parks the turn mid-flight
	// (parked-turn guard) and returns the parked shape ("", nil).
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	loop := NewAgentLoop("c3-quota-session", "/tmp",
		WithLLMChatter(&errChatter{err: throttleErr}))
	loop.security = nil
	throttleFailurePolicyForTests(t, time.Second)
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	loop.SetTurnParker(parker)
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

	respSub := msgBus.Subscribe("c3-quota-resp", "chat.response")
	defer msgBus.Unsubscribe(respSub)
	termSub := msgBus.Subscribe("c3-quota-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{
		SessionID:      "sess-c3-quota",
		ConversationID: "conv-c3-quota",
		Message:        "finish it",
		TurnID:         "turn-c3-quota",
	})

	// The turn is parked again on the throttle parker (that is what the
	// parked shape means), and NOTHING was delivered.
	if got := parker.Pending(); got != 1 {
		t.Fatalf("pending on the throttle parker = %d, want 1 (turn re-parked mid-flight)", got)
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case msg := <-respSub.Channel:
			t.Fatalf("re-parked quota resume sent a chat.response: %+v", msg.Payload)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	// And no terminal event: the turn is still parked.
	select {
	case msg := <-termSub.Channel:
		t.Fatalf("re-parked quota resume emitted a turn.terminal: %+v", msg.Payload)
	default:
	}
}

// TestBudgetResumeReparkedShapeDefersDelivery (C3, budget twin): same parked
// shape through resumeParkedTurn — no empty reply, no terminal event.
func TestBudgetResumeReparkedShapeDefersDelivery(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	throttleErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1"}
	loop := NewAgentLoop("c3-budget-session", "/tmp",
		WithLLMChatter(&errChatter{err: throttleErr}))
	loop.security = nil
	throttleFailurePolicyForTests(t, time.Second)
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	loop.SetTurnParker(parker)
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())

	respSub := msgBus.Subscribe("c3-budget-resp", "chat.response")
	defer msgBus.Unsubscribe(respSub)
	termSub := msgBus.Subscribe("c3-budget-term", "turn.terminal")
	defer msgBus.Unsubscribe(termSub)

	h.resumeParkedTurn(context.Background(), ParkedTurn{
		SessionID:      "sess-c3-budget",
		ConversationID: "conv-c3-budget",
		TurnID:         "turn-c3-budget",
		Message:        "finish it",
	})

	if got := parker.Pending(); got != 1 {
		t.Fatalf("pending on the throttle parker = %d, want 1 (turn re-parked mid-flight)", got)
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case msg := <-respSub.Channel:
			t.Fatalf("re-parked budget resume sent a chat.response: %+v", msg.Payload)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case msg := <-termSub.Channel:
		t.Fatalf("re-parked budget resume emitted a turn.terminal: %+v", msg.Payload)
	default:
	}
}

// --- C4: attempt-cap split enforced without bus/give-up wiring --------------

// TestTurnParkerAttemptCapWithoutBusOrGiveUp (C4): a bare parker (no
// parkEventBus, no giveUpFunc) must still give up at the record's attempt
// cap — the record never reaches the resume callback. Pre-fix the cap split
// was gated on the callback wiring and capped records replayed forever.
func TestTurnParkerAttemptCapWithoutBusOrGiveUp(t *testing.T) {
	var resumed atomic.Int32
	p, clock := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, rec ParkedTurnRecord) {
		resumed.Add(1)
	})
	// Bare parker: no SetParkEventBus, no SetGiveUpFunc.
	ctx := context.Background()
	p.Start(ctx)
	defer p.Stop()

	if !p.Park(ParkedTurnRecord{
		SessionID:   "s-c4",
		Class:       llm.FailureQuota,
		ResumeAt:    clock.nowFn().Add(20 * time.Millisecond),
		Attempt:     3,
		MaxAttempts: 3,
	}) {
		t.Fatal("park refused")
	}
	clock.advance(time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool { return p.Pending() == 0 })

	if got := resumed.Load(); got != 0 {
		t.Errorf("resume ran %d times at the attempt cap, want 0 (cap enforced without bus/give-up wiring)", got)
	}
}

// --- C18: Parts-bearing messages masked in BOTH representations -------------

// TestMaskOutboundScopedMasksTextParts (C18): a parts-bearing user message
// (AddUserMessageWithParts shape) must have its text parts masked — every
// serializer PREFERS Parts — and the flat Content masked consistently.
func TestMaskOutboundScopedMasksTextParts(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	msgs := []llm.ChatMessage{{
		Role:    llm.RoleUser,
		Content: "contact bob@example.com about the invoice",
		Parts: []llm.ContentPart{
			{Type: "text", Text: "also cc carol@example.com"},
			{Type: "image_url", ImageURL: &llm.ImageRef{URL: "file://img.png"}},
		},
	}}

	masked := hook.MaskOutboundScoped(msgs, "sess-parts-1")
	// Parts representation: text part masked, image part untouched.
	if strings.Contains(masked[0].Parts[0].Text, "carol@example.com") {
		t.Fatalf("raw PII survived in the text part: %q", masked[0].Parts[0].Text)
	}
	if !strings.Contains(masked[0].Parts[0].Text, "[PII:") {
		t.Fatalf("text part carries no token: %q", masked[0].Parts[0].Text)
	}
	if masked[0].Parts[1].ImageURL == nil || masked[0].Parts[1].ImageURL.URL != "file://img.png" {
		t.Fatalf("non-text part was mutated: %+v", masked[0].Parts[1])
	}
	// Content representation: masked too (consistency), token-bearing.
	if strings.Contains(masked[0].Content, "bob@example.com") {
		t.Fatalf("raw PII survived in Content while parts were masked: %q", masked[0].Content)
	}
	if !strings.Contains(masked[0].Content, "[PII:") {
		t.Fatalf("Content carries no token: %q", masked[0].Content)
	}
	// Round trip: both representations restore for the same session.
	if got := hook.RestoreOutput(masked[0].Parts[0].Text, "sess-parts-1"); got != "also cc carol@example.com" {
		t.Errorf("parts round trip broke: %q", got)
	}
	if got := hook.RestoreOutput(masked[0].Content, "sess-parts-1"); got != "contact bob@example.com about the invoice" {
		t.Errorf("content round trip broke: %q", got)
	}
}

// TestLoopPiiMask_ProviderBoundPartsMasked (C18, loop-level): a turn entered
// with parts must reach the provider with tokens in BOTH the parts text and
// the flat content of the cloned provider-bound message.
func TestLoopPiiMask_ProviderBoundPartsMasked(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)

	chatter := &capturingPiiChatter{
		resp: &llm.Response{Content: "done", FinishReason: "stop"},
	}
	loop := NewAgentLoop("pii-parts-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
	)
	loop.SetPiiRestoreHook(NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true}, slog.Default()))

	_, err = loop.RunOnceWithParts(context.Background(),
		"remind gina@example.com about the audit",
		[]llm.ContentPart{{Type: "text", Text: "cc heidi@example.com on the thread"}},
		"conv-pii-parts")
	if err != nil {
		t.Fatalf("RunOnceWithParts: %v", err)
	}

	var partsMsg *llm.ChatMessage
	for i := range chatter.lastMessages {
		m := &chatter.lastMessages[i]
		if m.Role == llm.RoleUser && len(m.Parts) > 0 {
			partsMsg = m
		}
	}
	if partsMsg == nil {
		t.Fatal("chatter saw no parts-bearing user message")
	}
	if strings.Contains(partsMsg.Parts[0].Text, "heidi@example.com") {
		t.Fatalf("raw PII crossed to the provider in Parts: %q", partsMsg.Parts[0].Text)
	}
	if !strings.Contains(partsMsg.Parts[0].Text, "[PII:") {
		t.Fatalf("provider-bound parts carry no token: %q", partsMsg.Parts[0].Text)
	}
	if strings.Contains(partsMsg.Content, "gina@example.com") {
		t.Fatalf("raw PII crossed to the provider in Content: %q", partsMsg.Content)
	}
}

// --- C19: registry-built loops receive the PII hook -------------------------

// TestRegistryGetForTaskCarriesPiiRestoreHook (C19): a registry built with
// RegistryConfig.PiiRestore (or via SetPiiRestoreHook) must stamp the hook
// onto every loop GetForTask creates — pre-fix registry-built loops shipped
// unmasked.
func TestRegistryGetForTaskCarriesPiiRestoreHook(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hook := NewPiiRestoreHook(pii.NewSwapper(store), intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true}, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	if hook == nil {
		t.Fatal("test setup produced a nil hook")
	}

	t.Run("constructor config", func(t *testing.T) {
		reg := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger(), PiiRestore: hook})
		spec := &AgentSpec{ID: "spec-specialist", Name: "Specialist"}
		if err := reg.RegisterSpec(spec); err != nil {
			t.Fatalf("RegisterSpec: %v", err)
		}
		loop, err := reg.GetForTask("spec-specialist", "task-c19a")
		if err != nil {
			t.Fatalf("GetForTask: %v", err)
		}
		if loop.PiiRestoreHook() == nil {
			t.Fatal("registry-built loop has a nil PiiRestoreHook (C19 regression)")
		}
	})

	t.Run("setter wiring", func(t *testing.T) {
		reg := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
		reg.SetPiiRestoreHook(hook)
		spec := &AgentSpec{ID: "spec-specialist-2", Name: "Specialist Two"}
		if err := reg.RegisterSpec(spec); err != nil {
			t.Fatalf("RegisterSpec: %v", err)
		}
		loop, err := reg.GetForTask("spec-specialist-2", "task-c19b")
		if err != nil {
			t.Fatalf("GetForTask: %v", err)
		}
		if loop.PiiRestoreHook() == nil {
			t.Fatal("registry-built loop (setter wiring) has a nil PiiRestoreHook")
		}
	})

	t.Run("no hook keeps legacy nil", func(t *testing.T) {
		reg := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
		spec := &AgentSpec{ID: "spec-specialist-3", Name: "Specialist Three"}
		if err := reg.RegisterSpec(spec); err != nil {
			t.Fatalf("RegisterSpec: %v", err)
		}
		loop, err := reg.GetForTask("spec-specialist-3", "task-c19c")
		if err != nil {
			t.Fatalf("GetForTask: %v", err)
		}
		if loop.PiiRestoreHook() != nil {
			t.Fatal("hook-less registry stamped a PiiRestoreHook (must stay nil)")
		}
	})
}

// --- C22: credential scan reads the POST-restore finalResponse --------------

// TestLoopReplyRestoredAndCredentialSanitized (C22): a reply carrying BOTH a
// credential pattern and a session PII token must ship restored (the token
// resolved) AND credential-sanitized — the scan runs on finalResponse, not
// the stale pre-restore variable, so the restore lane's work is preserved.
func TestLoopReplyRestoredAndCredentialSanitized(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)
	sessionID := "c22-session"

	// Mint the token the way the masking lane would: mask the secret value
	// in this session, then build a reply that parrots the token.
	const secretValue = "vera.lambert@northbeam.example"
	masked, n := swapper.Mask("the contact is "+secretValue, sessionID)
	if n == 0 {
		t.Fatal("detector found no PII in the fixture; test setup invalid")
	}
	// The token is the [PII:*] placeholder Mask minted; extract it.
	tokenStart := strings.Index(masked, "[PII:")
	tokenEnd := strings.Index(masked, "]")
	if tokenStart < 0 || tokenEnd < 0 || tokenEnd <= tokenStart {
		t.Fatalf("mask produced no [PII:*] token: %q", masked)
	}
	token := masked[tokenStart : tokenEnd+1]

	//nolint:gosec // test fixture, not a real credential
	//nolint:gosec // test fixture, not a real credential
	reply := "your key is: sk-" + strings.Repeat("a9", 20) + " and your stored value is " + token

	chatter := &capturingPiiChatter{resp: &llm.Response{Content: reply, FinishReason: "stop"}}
	loop := NewAgentLoop(sessionID, t.TempDir(),
		WithLLMChatter(chatter),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
		WithSecurityOrchestrator(func() *intsecurity.Orchestrator {
			cfg := intsecurity.DefaultOrchestratorConfig()
			cfg.RedactOutput = true
			return intsecurity.NewOrchestrator(cfg, slogDiscardLogger())
		}()),
	)
	loop.SetPiiRestoreHook(NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true}, slog.Default()))

	shipped, err := loop.RunOnce(context.Background(), "hello", "conv-c22")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// Restored: the [PII:*] token resolved back to the original value.
	if strings.Contains(shipped, "[PII:") {
		t.Fatalf("token leaked into the shipped reply: %q", shipped)
	}
	if !strings.Contains(shipped, secretValue) {
		t.Fatalf("restore lane lost: shipped %q does not carry the restored value", shipped)
	}
	// Credential-sanitized: the redaction removed the API key shape.
	if strings.Contains(shipped, "sk-123") {
		t.Fatalf("credential pattern shipped unredacted: %q", shipped)
	}
	if !strings.Contains(shipped, "sk-") || !strings.Contains(shipped, "***") {
		t.Errorf("credential redaction did not run: %q", shipped)
	}
}

// --- C23: voice spoken rewrite runs restore + credential scan ---------------

// TestVoiceSpokenRewriteRestoredAndSanitized (C23): a rewrite that parrots a
// session PII token must come back restored; a credential pattern in the
// rewrite must be sanitized before the adapter persists/speaks it.
func TestVoiceSpokenRewriteRestoredAndSanitized(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)
	sessionID := "c23-session"

	const secretValue = "vera.lambert@northbeam.example"
	masked, n := swapper.Mask("the contact is "+secretValue, sessionID)
	if n == 0 {
		t.Fatal("detector found no PII in the fixture; test setup invalid")
	}
	tokenStart := strings.Index(masked, "[PII:")
	tokenEnd := strings.Index(masked, "]")
	if tokenStart < 0 || tokenEnd < 0 || tokenEnd <= tokenStart {
		t.Fatalf("mask produced no [PII:*] token: %q", masked)
	}
	token := masked[tokenStart : tokenEnd+1]

	//nolint:gosec // test fixture, not a real credential
	rewrite := "sure — the key is: sk-" + strings.Repeat("a9", 20) + " and the value is " + token

	// RunVoiceSpokenRewriteRetry re-enters the turn producer ONCE: the
	// scripted chatter's first (and only) call serves the contaminated
	// rewrite text.
	scv := &spokenScriptChatter{script: []string{
		rewrite,
	}}

	loop := NewAgentLoop(sessionID, t.TempDir(),
		WithLLMChatter(scv),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
		WithSecurityOrchestrator(func() *intsecurity.Orchestrator {
			cfg := intsecurity.DefaultOrchestratorConfig()
			cfg.RedactOutput = true
			return intsecurity.NewOrchestrator(cfg, slogDiscardLogger())
		}()),
	)
	loop.SetPiiRestoreHook(NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true}, slog.Default()))

	conv, _ := loop.conversations.GetOrRestore("conv-c23", func() ([]llm.ChatMessage, error) {
		return nil, nil
	})
	if conv == nil {
		t.Fatal("GetOrRestore returned nil conversation")
	}
	conv.AddAssistantMessage("the answer is ```code``` tool_call_id x")

	got, err := loop.RunVoiceSpokenRewriteRetry(context.Background(), "conv-c23")
	if err != nil {
		t.Fatalf("RunVoiceSpokenRewriteRetry: %v", err)
	}
	if strings.Contains(got, "[PII:") {
		t.Fatalf("rewrite shipped a raw [PII:*] token: %q", got)
	}
	if !strings.Contains(got, secretValue) {
		t.Fatalf("rewrite restore lost: %q does not carry the restored value", got)
	}
	if strings.Contains(got, "sk-123") {
		t.Fatalf("rewrite shipped an unredacted credential: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("rewrite credential redaction did not run: %q", got)
	}
}

// spokenScriptChatter serves scripted replies in order, then fails loudly.
type spokenScriptChatter struct {
	script []string
	calls  atomic.Int32
}

func (s *spokenScriptChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	i := int(s.calls.Add(1)) - 1
	if i >= len(s.script) {
		return nil, errors.New("script exhausted: unexpected extra chatter call")
	}
	return &llm.Response{Content: s.script[i], FinishReason: "stop"}, nil
}

func (s *spokenScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *spokenScriptChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "spoken-script-chatter"}
}
