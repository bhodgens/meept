package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/pii"
)

// piiSideSendFakeLoop is a minimal chatScopeProvider + session source for
// the C21 side-send tests: it answers SessionID without constructing a
// full AgentLoop. (In-package test double — the interface seam is
// structural, satisfied by *AgentLoop in production.)
type piiSideSendFakeLoop struct{ session string }

func (f *piiSideSendFakeLoop) SessionID() string { return f.session }
func (f *piiSideSendFakeLoop) scopeOptions() []llm.ChatOption {
	return nil
}

// piiSideSendChatter is an llm.Chatter double that records the messages
// it was called with and replies with a fixed content.
type piiSideSendChatter struct {
	messages [][]llm.ChatMessage
	reply    string
}

func (c *piiSideSendChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.messages = append(c.messages, messages)
	return &llm.Response{Content: c.reply}, nil
}

// newPiiSideSendHook builds a real PiiRestoreHook over a temp swap store.
func newPiiSideSendHook(t *testing.T) *PiiRestoreHook {
	t.Helper()
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hook := NewPiiRestoreHook(pii.NewSwapper(store), intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true, SecretRestore: true}, slog.Default())
	if hook == nil {
		t.Fatal("NewPiiRestoreHook returned nil")
	}
	return hook
}

// seedSideSendMask warms the session swap store with the email secret so
// the masked form carries the [PII:*] token the detector minted.
func seedSideSendMask(t *testing.T, hook *PiiRestoreHook, session, raw string) string {
	t.Helper()
	masked := hook.MaskOutboundScoped([]llm.ChatMessage{{Role: llm.RoleUser, Content: raw}}, session)
	if !strings.Contains(masked[0].Content, "[PII:") {
		t.Fatalf("seed mask failed: content=%q", masked[0].Content)
	}
	return masked[0].Content
}

// TestMaskSideSendHelpers_NilHookByteIdentical pins the disabled path:
// nil hook returns inputs unchanged (both message and text forms).
func TestMaskSideSendHelpers_NilHookByteIdentical(t *testing.T) {
	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "raw SECRETMARKER stays"}}
	got := maskSideSendMessages(nil, msgs, "sess-1")
	if len(got) != 1 || got[0].Content != msgs[0].Content {
		t.Fatalf("nil hook mutated messages: %q", got[0].Content)
	}
	if txt := maskSideSendText(nil, "raw SECRETMARKER stays", "sess-1"); txt != "raw SECRETMARKER stays" {
		t.Fatalf("nil hook mutated text: %q", txt)
	}
}

// TestMaskSideSendHelpers_EmptySessionRefused pins the scope-less
// refusal: a hook with an empty sessionID must not mask (tokens minted
// without a session can never be restored).
func TestMaskSideSendHelpers_EmptySessionRefused(t *testing.T) {
	hook := newPiiSideSendHook(t)
	const raw = "reach me at bob@example.com about SECRETMARKER"
	if txt := maskSideSendText(hook, raw, ""); txt != raw {
		t.Fatalf("empty session masked anyway: %q", txt)
	}
}

// TestSideSendSessionID_TypedNilLoopGuard pins the typed-nil guard: a
// typed-nil *AgentLoop stored in the chatScopeProvider interface must
// resolve to "" (not panic).
func TestSideSendSessionID_TypedNilLoopGuard(t *testing.T) {
	var loop *AgentLoop
	var provider chatScopeProvider = loop
	if got := sideSendSessionID(provider); got != "" {
		t.Fatalf("typed-nil loop session = %q, want empty", got)
	}
	if got := sideSendSessionID(nil); got != "" {
		t.Fatalf("nil provider session = %q, want empty", got)
	}
	if got := sideSendSessionID(&piiSideSendFakeLoop{session: "sess-9"}); got != "sess-9" {
		t.Fatalf("fake loop session = %q, want sess-9", got)
	}
}

// TestIntentAnalyzer_PiiMaskHook_MasksPrompt pins finding C21 for the
// intent-analyzer side send: with a hook wired and a session scope
// resolved, the user message the analyzer sends carries the masked form
// and never the raw secret.
func TestIntentAnalyzer_PiiMaskHook_MasksPrompt(t *testing.T) {
	hook := newPiiSideSendHook(t)
	const session = "sess-ia"
	const raw = "please fix this, contact me at carol@example.com"
	seedSideSendMask(t, hook, session, raw)

	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ScopeProvider: &piiSideSendFakeLoop{session: session},
	}, nil, slog.Default())
	ia.SetPiiMaskHook(hook)
	if ia.piiMask == nil {
		t.Fatal("SetPiiMaskHook did not wire the hook")
	}

	// The exact transform AnalyzeTrueIntent applies to the built messages
	// before ia.client.Chat (finding C21 call site).
	maskedMessages := maskSideSendMessages(ia.piiMask, ia.buildAnalysisMessages(raw, nil), sideSendSessionID(ia.scopeProvider))
	if len(maskedMessages) != 2 {
		t.Fatalf("messages = %d, want 2", len(maskedMessages))
	}
	user := maskedMessages[1].Content
	if strings.Contains(user, "carol@example.com") {
		t.Fatalf("raw secret reached the intent-analysis prompt: %q", user)
	}
	if !strings.Contains(user, "[PII:") {
		t.Fatalf("masked token missing from intent-analysis prompt: %q", user)
	}
}

// TestIntentAnalyzer_PiiMaskHook_NilHookPassthrough pins the no-regression
// path: with no hook wired the raw content passes through byte-identically.
func TestIntentAnalyzer_PiiMaskHook_NilHookPassthrough(t *testing.T) {
	const raw = "please fix this, contact me at carol@example.com"
	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ScopeProvider: &piiSideSendFakeLoop{session: "sess-ia"},
	}, nil, slog.Default())
	maskedMessages := maskSideSendMessages(ia.piiMask, ia.buildAnalysisMessages(raw, nil), sideSendSessionID(ia.scopeProvider))
	if maskedMessages[1].Content != raw {
		t.Fatalf("nil hook mutated content: %q", maskedMessages[1].Content)
	}
}

// TestLLMClassifier_PiiMaskHook_MasksPrompt pins finding C21 for the LLM
// classifier's Classify side send: with a hook wired, the user message
// carries the masked form and never the raw secret; the ClassifyMulti
// site (input masked before interpolation) is pinned through the same
// lane.
func TestLLMClassifier_PiiMaskHook_MasksPrompt(t *testing.T) {
	hook := newPiiSideSendHook(t)
	const session = "sess-lc"
	const raw = "please fix this, contact me at dan@example.com"
	seedSideSendMask(t, hook, session, raw)

	c := &LLMClassifier{logger: testLogger(), scopeProvider: &piiSideSendFakeLoop{session: session}}
	c.SetPiiMaskHook(hook)
	if c.piiMask == nil {
		t.Fatal("SetPiiMaskHook did not wire the hook")
	}

	prompt := c.buildClassificationPrompt(raw)
	masked := maskSideSendText(c.piiMask, prompt, sideSendSessionID(c.scopeProvider))
	if strings.Contains(masked, "dan@example.com") {
		t.Fatalf("raw secret reached the classification prompt: %q", masked)
	}
	if !strings.Contains(masked, "[PII:") {
		t.Fatalf("masked token missing from classification prompt: %q", masked)
	}
	// The multi-intent prompt builder is a pure function: ClassifyMulti
	// masks its INPUT before interpolation — pin the same lane there.
	maskedMulti := buildClassifyMultiPrompt(maskSideSendText(c.piiMask, raw, sideSendSessionID(c.scopeProvider)))
	if strings.Contains(maskedMulti, "dan@example.com") || !strings.Contains(maskedMulti, "[PII:") {
		t.Fatalf("multi-intent prompt not masked: %q", maskedMulti)
	}
}

// TestLLMClassifier_PiiMaskHook_NilHookPassthrough pins the no-regression
// path for the classifier: nil hook leaves the prompt byte-identical.
func TestLLMClassifier_PiiMaskHook_NilHookPassthrough(t *testing.T) {
	const raw = "please fix this, contact me at dan@example.com"
	c := &LLMClassifier{logger: testLogger(), scopeProvider: &piiSideSendFakeLoop{session: "sess-lc"}}
	prompt := c.buildClassificationPrompt(raw)
	masked := maskSideSendText(c.piiMask, prompt, sideSendSessionID(c.scopeProvider))
	if masked != prompt {
		t.Fatalf("nil hook mutated prompt: %q", masked)
	}
}

// TestReflectionEngine_PiiMaskHook_NilSetterGuard pins the setter
// nil-guard rule on ReflectionEngine.SetPiiMaskHook (C21).
func TestReflectionEngine_PiiMaskHook_NilSetterGuard(t *testing.T) {
	re := &ReflectionEngine{}
	re.SetPiiMaskHook(nil) // must not panic nor set
	if re.piiMask != nil {
		t.Fatal("nil setter wired a hook")
	}
	var nilEngine *ReflectionEngine
	nilEngine.SetPiiMaskHook(newPiiSideSendHook(t)) // typed-nil receiver must not panic
}

// TestMaskSideSendChatterRoundTrip proves the mask lane through a real
// Chatter-shaped call: the messages a side send hands to Chat carry the
// token and never the raw secret — exactly what crosses to the provider.
func TestMaskSideSendChatterRoundTrip(t *testing.T) {
	hook := newPiiSideSendHook(t)
	const session = "sess-rt"
	const raw = "mail me at erin@example.com"
	seedSideSendMask(t, hook, session, raw)

	chatter := &piiSideSendChatter{reply: "ok"}
	msgs := maskSideSendMessages(hook, []llm.ChatMessage{{Role: llm.RoleUser, Content: raw}}, session)
	resp, err := chatter.Chat(context.Background(), msgs)
	if err != nil {
		t.Fatalf("chatter.Chat: %v", err)
	}
	if resp == nil || resp.Content == "" {
		t.Fatal("expected non-empty reply")
	}
	sent := chatter.messages[0][0].Content
	if strings.Contains(sent, "erin@example.com") {
		t.Fatalf("raw secret crossed the provider boundary: %q", sent)
	}
	if !strings.Contains(sent, "[PII:") {
		t.Fatalf("masked token missing from sent messages: %q", sent)
	}
}
