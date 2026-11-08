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

// BND-1 (agent-boundary-hardening): the pre-LLM masking half of the PII
// proxy. Provider-bound context carries only [PII:*] tokens; stored history
// keeps raw text; the same session's RestoreOutput un-masks the reply.

// MaskOutboundScoped masks detected PII in message content with
// session-scoped tokens.
func TestMaskOutboundScopedMasksContent(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	msgs := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "you are a helper"},
		{Role: llm.RoleUser, Content: "contact bob@example.com about the invoice"},
	}

	masked := hook.MaskOutboundScoped(msgs, "sess-mask-1")
	if len(masked) != len(msgs) {
		t.Fatalf("slice length changed: %d -> %d", len(msgs), len(masked))
	}
	if strings.Contains(masked[1].Content, "bob@example.com") {
		t.Fatalf("raw PII survived masking: %q", masked[1].Content)
	}
	if !strings.Contains(masked[1].Content, "[PII:") {
		t.Fatalf("no token minted in masked content: %q", masked[1].Content)
	}
	// System content without PII stays byte-identical.
	if masked[0].Content != msgs[0].Content {
		t.Fatalf("PII-free message mutated: %q -> %q", msgs[0].Content, masked[0].Content)
	}
}

// The masking/restore round trip: tokens minted by MaskOutboundScoped for a
// session resolve through RestoreOutput for the SAME session.
func TestMaskOutboundScopedRoundTrip(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	const original = "reply to carol@example.com by Friday"
	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: original}}

	masked := hook.MaskOutboundScoped(msgs, "sess-rt")
	if masked[0].Content == original {
		t.Fatal("masking made no change")
	}

	// The model parrots the masked text back; RestoreOutput (the loop's
	// ScanOutput choke point) must recover the original value.
	restored := hook.RestoreOutput(masked[0].Content, "sess-rt")
	if restored != original {
		t.Fatalf("round trip broke: %q -> %q -> %q", original, masked[0].Content, restored)
	}
}

// An empty session scope refuses to mask: sessionless tokens could never be
// restored (the store refuses empty-session resolution).
func TestMaskOutboundScopedEmptySessionRefuses(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "mail dan@example.com"}}
	masked := hook.MaskOutboundScoped(msgs, "")
	if masked[0].Content != msgs[0].Content {
		t.Fatalf("scope-less masking ran: %q -> %q", msgs[0].Content, masked[0].Content)
	}
}

// Disabled hook / nil hook: byte-identical passthrough.
func TestMaskOutboundScopedDisabledByteIdentical(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{})

	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "mail erin@example.com"}}
	masked := hook.MaskOutboundScoped(msgs, "sess-off")
	if masked[0].Content != msgs[0].Content {
		t.Fatalf("disabled hook masked: %q -> %q", msgs[0].Content, masked[0].Content)
	}

	var nilHook *PiiRestoreHook
	masked2 := nilHook.MaskOutboundScoped(msgs, "sess-off")
	if masked2[0].Content != msgs[0].Content {
		t.Fatalf("nil hook masked: %q -> %q", msgs[0].Content, masked2[0].Content)
	}
}

// Cross-session isolation of the masking lane: tokens minted for session A
// do NOT resolve in session B's restore.
func TestMaskOutboundScopedCrossSessionIsolation(t *testing.T) {
	hook, _ := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "ping fay@example.com"}}
	masked := hook.MaskOutboundScoped(msgs, "sess-a")

	restored := hook.RestoreOutput(masked[0].Content, "sess-b")
	if restored != masked[0].Content {
		t.Fatalf("other-session token resolved: %q -> %q", masked[0].Content, restored)
	}
}

// End-to-end through the loop: the provider-bound messages the chatter
// receives carry tokens, the returned reply is restored, and the STORED
// conversation history keeps the raw user text (masking touches only the
// per-iteration clone).
func TestLoopPiiMask_ProviderBoundOnly(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)

	const userTurn = "remind gina@example.com about the audit"
	captured := make(chan []llm.ChatMessage, 8)
	chatter := &capturingPiiChatter{captured: captured,
		resp: &llm.Response{Content: "done", FinishReason: "stop"}}

	loop := NewAgentLoop("pii-mask-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
	)
	loop.SetPiiRestoreHook(NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(),
		config.PiiConfig{Enabled: true}, slog.Default()))

	resp, err := loop.RunOnce(context.Background(), userTurn, "conv-pii-mask-1")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if strings.Contains(resp, "[PII:") {
		t.Fatalf("token leaked into the reply: %q", resp)
	}

	// Provider-bound: the LAST user message the chatter saw must carry a
	// token, not the raw address.
	var providerBound string
	for _, m := range chatter.lastMessages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "remind") {
			providerBound = m.Content
		}
	}
	if providerBound == "" {
		t.Fatal("chatter saw no matching user message")
	}
	if strings.Contains(providerBound, "gina@example.com") {
		t.Fatalf("raw PII crossed to the provider: %q", providerBound)
	}
	if !strings.Contains(providerBound, "[PII:") {
		t.Fatalf("provider-bound text carries no token: %q", providerBound)
	}

	// Stored history keeps the raw text: masking is provider-bound only.
	var historyUser string
	msgs := loop.conversations.Get("conv-pii-mask-1").GetMessages()
	for _, m := range msgs {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "remind") {
			historyUser = m.Content
		}
	}
	if !strings.Contains(historyUser, "gina@example.com") {
		t.Fatalf("stored history lost the raw user text: %q", historyUser)
	}
}

// capturingPiiChatter records the messages of the last Chat call.
type capturingPiiChatter struct {
	captured     chan []llm.ChatMessage
	lastMessages []llm.ChatMessage
	resp         *llm.Response
}

func (c *capturingPiiChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	cp := make([]llm.ChatMessage, len(messages))
	copy(cp, messages)
	c.lastMessages = cp
	select {
	case c.captured <- cp:
	default:
	}
	return c.resp, nil
}
func (c *capturingPiiChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}
func (c *capturingPiiChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "pii-mask-mock"}
}

// No-hook loop stays byte-identical: raw PII crosses to the provider as
// before (the pre-BND-1 behavior), nothing is minted.
func TestLoopPiiMask_NoHookRawPassthrough(t *testing.T) {
	chatter := &capturingPiiChatter{resp: &llm.Response{Content: "done", FinishReason: "stop"}}
	loop := NewAgentLoop("pii-raw-session", t.TempDir(),
		WithLLMChatter(chatter),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
	)

	if _, err := loop.RunOnce(context.Background(), "mail hank@example.com", "conv-pii-mask-2"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	found := false
	for _, m := range chatter.lastMessages {
		if strings.Contains(m.Content, "hank@example.com") {
			found = true
		}
	}
	if !found {
		t.Fatal("no-hook loop no longer passes raw PII (behavior changed)")
	}
}
