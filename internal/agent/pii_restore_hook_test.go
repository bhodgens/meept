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

// newPiiRestoreTestHook builds a PiiRestoreHook over a fresh in-memory
// store with the given config. The store dir is a temp dir (durable file,
// empty session set).
func newPiiRestoreTestHook(t *testing.T, cfg config.PiiConfig) (*PiiRestoreHook, *pii.Swapper) {
	t.Helper()
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)
	hook := NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(), cfg, slog.Default())
	if hook == nil {
		t.Fatal("NewPiiRestoreHook returned nil for non-nil deps")
	}
	return hook, swapper
}

// T1: same-session masked text restores; the disabled config returns the
// input byte-identical.
func TestPiiRestoreHookRestoresSameSessionTokens(t *testing.T) {
	hook, swapper := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	const session = "sess-1"
	masked, n := swapper.Mask("reach me at bob@example.com today", session)
	if n == 0 || !strings.Contains(masked, "[PII:") {
		t.Fatalf("Mask produced no tokens: %q (count=%d)", masked, n)
	}

	restored := hook.RestoreOutput(masked, session)
	if restored != "reach me at bob@example.com today" {
		t.Fatalf("RestoreOutput = %q, want original text", restored)
	}
}

// T1: a token from ANOTHER session must never resolve here (cross-session
// restore refusal pinned end-to-end through the hook).
func TestPiiRestoreHookRefusesCrossSessionTokens(t *testing.T) {
	hook, swapper := newPiiRestoreTestHook(t, config.PiiConfig{Enabled: true})

	masked, _ := swapper.Mask("mail me at carol@example.com", "sess-origin")

	restored := hook.RestoreOutput(masked, "sess-other")
	if restored != masked {
		t.Fatalf("cross-session token resolved: got %q, want token %q untouched", restored, masked)
	}
}

// T1: disabled config -> byte-identical passthrough, no store touch.
func TestPiiRestoreHookDisabledIsByteIdentical(t *testing.T) {
	hook, swapper := newPiiRestoreTestHook(t, config.PiiConfig{})

	masked, _ := swapper.Mask("mail me at dave@example.com", "sess-d")

	restored := hook.RestoreOutput(masked, "sess-d")
	if restored != masked {
		t.Fatalf("disabled hook mutated text: %q -> %q", masked, restored)
	}
}

// T1 typed-nil guard: nil swapper -> constructor nil -> nothing registered.
// A nil obfuscator is legal (the secret lane is gated separately and
// RestoreOutput skips Deobfuscate when the pointer is nil), so only the
// swapper nil-guards the constructor.
func TestNewPiiRestoreHookNilSwapper(t *testing.T) {
	if h := NewPiiRestoreHook(nil, intsecurity.NewSecretObfuscator(), config.PiiConfig{Enabled: true}, slog.Default()); h != nil {
		t.Fatal("nil swapper must produce a nil hook")
	}
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h := NewPiiRestoreHook(pii.NewSwapper(store), nil, config.PiiConfig{Enabled: true}, slog.Default())
	if h == nil {
		t.Fatal("nil obfuscator is legal (secret lane off); hook must not be nil")
	}
	// With the secret lane gated on but the obfuscator missing, restore
	// still runs the token lane and skips Deobfuscate safely.
	masked, _ := pii.NewSwapper(store).Mask("mail bob@example.com", "sess-nil-obf")
	out := h.RestoreOutput(masked, "sess-nil-obf")
	if !strings.Contains(out, "bob@example.com") {
		t.Fatalf("token lane broken with nil obfuscator: %q", out)
	}
}

// T4: the previously dead Deobfuscate lane - a secret that entered the LLM
// context obfuscated (#XXXX#) comes back restored at output when the config
// gates the lane on.
func TestSecretPlaceholderRestore(t *testing.T) {
	const secret = "sk-restore-lane-secret-99"

	t.Setenv("PII_RESTORE_TEST_SECRET", secret)
	obfuscator := intsecurity.NewSecretObfuscator()

	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	hook := NewPiiRestoreHook(pii.NewSwapper(store), obfuscator, config.PiiConfig{Enabled: true, SecretRestore: true}, slog.Default())

	// Model echoes the placeholder it was given.
	obfuscated := obfuscator.Obfuscate("use the credential " + secret + " please")
	if obfuscated == "use the credential "+secret+" please" || !strings.Contains(obfuscated, "#") {
		t.Fatalf("obfuscator did not swap the secret: %q", obfuscated)
	}

	restored := hook.RestoreOutput("use the credential "+extractPlaceholder(t, obfuscated, secret)+" please", "sess-x")
	if strings.Contains(restored, secret) == false {
		t.Fatalf("secret placeholder not restored at output: %q", restored)
	}
	if !strings.Contains(restored, secret) {
		t.Fatalf("restored output lost the secret: %q", restored)
	}
}

// extractPlaceholder finds the #XXXX# placeholder the obfuscator minted for
// secret in obfuscated.
func extractPlaceholder(t *testing.T, obfuscated, secret string) string {
	t.Helper()
	// The first #...# span in the obfuscated text is the placeholder
	// (obfuscation replaced exactly this secret occurrence).
	i := strings.Index(obfuscated, "#")
	if i < 0 {
		t.Fatalf("no placeholder in %q", obfuscated)
	}
	j := strings.Index(obfuscated[i+1:], "#")
	if j < 0 {
		t.Fatalf("unterminated placeholder in %q", obfuscated)
	}
	return obfuscated[i : i+j+2]
}

// T4 (negative): with the secret lane gated OFF, placeholders survive
// verbatim (the operator explicitly disabled restore).
func TestSecretPlaceholderRestoreDisabled(t *testing.T) {
	const secret = "sk-disabled-lane-secret-77"
	t.Setenv("PII_RESTORE_DISABLED_TEST_SECRET", secret)
	obfuscator := intsecurity.NewSecretObfuscator()

	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	hook := NewPiiRestoreHook(pii.NewSwapper(store), obfuscator, config.PiiConfig{}, slog.Default())

	obfuscated := obfuscator.Obfuscate("credential " + secret + " end")
	ph := extractPlaceholder(t, obfuscated, secret)
	restored := hook.RestoreOutput("credential "+ph+" end", "sess-y")
	if restored != "credential "+ph+" end" {
		t.Fatalf("disabled hook restored the secret anyway: %q", restored)
	}
}

// T5: the hook logs counts/classes only - never values. Assert by capturing
// slog output while restoring and grepping for the raw value.
func TestPiiLeakageClasses_HookLogsCountsOnly(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)

	hook := NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(), config.PiiConfig{Enabled: true}, logger)

	const email = "leak-check@example.com"
	masked, _ := swapper.Mask("write to "+email+" now", "sess-log")
	hook.RestoreOutput(masked, "sess-log")

	logs := buf.String()
	if strings.Contains(logs, email) || strings.Contains(logs, "[PII:") {
		t.Fatalf("hook leaked value material into logs: %q", logs)
	}
}

// T2/T5 end-to-end through the loop: a response carrying [PII:...] from the
// same session is restored BEFORE history/trace capture; a loop without the
// hook is byte-identical to today. Uses the real loop pipeline
// (RunOnceWithParts) with a stub chatter, mirroring loop_test.go patterns.
type piiLoopChatter struct{ resp *llm.Response }

func (c *piiLoopChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return c.resp, nil
}
func (c *piiLoopChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}
func (c *piiLoopChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "pii-loop-mock"} }

func newPiiLoopWithHook(t *testing.T, withHook bool) (*AgentLoop, *pii.Swapper, string) {
	t.Helper()

	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	swapper := pii.NewSwapper(store)

	loop := NewAgentLoop("pii-loop-session", t.TempDir(),
		WithLLMChatter(&piiLoopChatter{resp: &llm.Response{Content: "done", FinishReason: "stop"}}),
		WithLoopLogger(slog.New(slog.NewTextHandler(&strings.Builder{}, nil))),
	)
	if withHook {
		loop.SetPiiRestoreHook(NewPiiRestoreHook(swapper, intsecurity.NewSecretObfuscator(), config.PiiConfig{Enabled: true}, slog.Default()))
	}
	return loop, swapper, "pii-loop-session"
}

func TestLoopPiiRestore_RestoredBeforePersistence(t *testing.T) {
	loop, swapper, session := newPiiLoopWithHook(t, true)

	masked, _ := swapper.Mask("the contact is eve@example.com", session)
	// The model's reply echoes the masked text it saw in context.
	loop.llm = &piiLoopChatter{resp: &llm.Response{Content: "replying about " + masked, FinishReason: "stop"}}

	resp, err := loop.RunOnce(context.Background(), "summarize contacts", "conv-pii-1")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if strings.Contains(resp, "[PII:") {
		t.Fatalf("token reached the returned response: %q", resp)
	}
	if !strings.Contains(resp, "eve@example.com") {
		t.Fatalf("restored value missing from response: %q", resp)
	}

	// History (the conversation the loop appended) must see restored text.
	msgs := loop.conversations.Get("conv-pii-1").GetMessages()
	var last string
	for _, m := range msgs {
		if m.Role == llm.RoleAssistant && m.Content != "" {
			last = m.Content
		}
	}
	if last == "" {
		t.Fatal("no assistant message in history")
	}
	if strings.Contains(last, "[PII:") {
		t.Fatalf("token reached conversation history: %q", last)
	}
	if !strings.Contains(last, "eve@example.com") {
		t.Fatalf("history lost the restored value: %q", last)
	}
}

func TestLoopPiiRestore_NoHookByteIdentical(t *testing.T) {
	loop, swapper, session := newPiiLoopWithHook(t, false)

	masked, _ := swapper.Mask("the contact is frank@example.com", session)
	loop.llm = &piiLoopChatter{resp: &llm.Response{Content: "replying about " + masked, FinishReason: "stop"}}

	resp, err := loop.RunOnce(context.Background(), "summarize contacts", "conv-pii-2")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// Without the hook today's behavior is preserved verbatim: the token
	// flows through untouched.
	if !strings.Contains(resp, "[PII:") {
		t.Fatalf("no-hook loop should be byte-identical to today (token preserved), got %q", resp)
	}
}

// TestSecretPlaceholderCrossSessionNeverRestores pins bughunt 2026-11-06
// security F-3: a placeholder TAGGED to session A that arrives in session
// B's output (handoff, delegate result, memory replay) must never resolve
// for B. The old "scoped restore made no progress -> full Deobfuscate"
// fallback fired in exactly this case and leaked A's secret.
func TestSecretPlaceholderCrossSessionNeverRestores(t *testing.T) {
	const secretA = "sk-alpha...cret-1"
	const secretB = "sk-bravo...cret-2"
	t.Setenv("PII_RESTORE_TEST_SECRET_A", secretA)
	t.Setenv("PII_RESTORE_TEST_SECRET_B", secretB)

	obfuscator := intsecurity.NewSecretObfuscator()
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hook := NewPiiRestoreHook(pii.NewSwapper(store), obfuscator, config.PiiConfig{Enabled: true, SecretRestore: true}, slog.Default())

	// A mints a tagged placeholder for its secret.
	phA := obfuscator.ObfuscateForSession("A holds "+secretA, "sess-alpha")
	if phA == "A holds "+secretA {
		t.Fatalf("no placeholder minted for session A: %q", phA)
	}
	carrier := extractPlaceholder(t, phA, secretA) // "#XXXX#" tagged to sess-alpha
	// B has its own tagged entry so the map is non-trivial.
	phB := obfuscator.ObfuscateForSession("B holds "+secretB, "sess-bravo")
	_ = phB

	// Cross-session transfer: A's placeholder text lands in B's output.
	leakProbe := "forwarded from alpha: " + carrier

	restored := hook.RestoreOutput(leakProbe, "sess-bravo")
	if strings.Contains(restored, secretA) {
		t.Fatalf("CROSS-SESSION LEAK: session A's secret restored into session B output: %q", restored)
	}
	if restored != leakProbe {
		t.Fatalf("B's restore changed A's placeholder unexpectedly: %q", restored)
	}

	// Sessionless caller must not sweep the process-wide map either.
	anon := hook.RestoreOutput(leakProbe, "")
	if strings.Contains(anon, secretA) {
		t.Fatalf("sessionless restore leaked tagged secret: %q", anon)
	}
}

// TestUntaggedPlaceholderStillRestores guards the other half of F-3: the
// legacy lane (untagged = pre-deploy placeholders) must keep restoring,
// else the fix would be a regression of T4's contract.
func TestUntaggedPlaceholderStillRestores(t *testing.T) {
	const secret = "sk-untag...cret"
	t.Setenv("PII_RESTORE_TEST_SECRET", secret)

	obfuscator := intsecurity.NewSecretObfuscator()
	store, err := pii.NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hook := NewPiiRestoreHook(pii.NewSwapper(store), obfuscator, config.PiiConfig{Enabled: true, SecretRestore: true}, slog.Default())

	obfuscated := obfuscator.Obfuscate("legacy secret " + secret) // untagged lane
	ph := extractPlaceholder(t, obfuscated, secret)

	restored := hook.RestoreOutput("echo "+ph+" back", "sess-any")
	if !strings.Contains(restored, secret) {
		t.Fatalf("untagged placeholder not restored via the untagged fallback: %q", restored)
	}
	// And in a sessionless call too (RestoreOutput refuses, untagged lane serves).
	restoredAnon := hook.RestoreOutput("echo "+ph+" back", "")
	if !strings.Contains(restoredAnon, secret) {
		t.Fatalf("sessionless untagged restore broke: %q", restoredAnon)
	}
}
