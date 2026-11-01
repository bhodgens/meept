package agent

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/pii"
)

// piiStubTransform is the leaf T4 stub: a TransformContextHook backed by
// the real pii.Detector that replaces detected email addresses with a
// fixed token and RECORDS every text it observes, so the test can prove
// the pii pass ran AFTER secret obfuscation.
type piiStubTransform struct {
	detector *pii.Detector
	observed []string
}

func (s *piiStubTransform) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	modified := false
	for i, msg := range messages {
		s.observed = append(s.observed, msg.Content)
		spans := s.detector.Detect(msg.Content)
		if len(spans) == 0 {
			continue
		}
		var b strings.Builder
		pos := 0
		for _, sp := range spans {
			b.WriteString(msg.Content[pos:sp.Start])
			b.WriteString("[PII:stub-fixed-token]")
			pos = sp.End
		}
		b.WriteString(msg.Content[pos:])
		messages[i].Content = b.String()
		modified = true
	}
	if !modified {
		return ContextTransform{}
	}
	return ContextTransform{Messages: messages, ToolDefs: toolDefs, Modified: true, Reason: "pii stub transform"}
}

// TestPiiHookOrderPinned is the sanitizer-ordering pin (leaf T4): with
// SecurityTransformContext, SecretObfuscationHook, and a pii stub all
// registered at HookPriorityCritical in the prescribed order, a
// transforming pass obfuscates the secret BEFORE the pii stub observes
// the text, and pii registers AFTER secret-obfuscation among
// HookPriorityCritical peers (equal priority -> registration order).
func TestPiiHookOrderPinned(t *testing.T) {
	// The secret-obfuscation hook loads env secrets at construction; plant
	// a unique one this test controls.
	const secret = "zqxwv-supersecret-value-42"
	t.Setenv("PII_ORDER_TEST_SECRET", secret)

	orch := intsecurity.NewOrchestrator(intsecurity.OrchestratorConfig{}, slog.Default())
	obfuscator := intsecurity.NewSecretObfuscator()

	reg := NewHookRegistry(nil)
	stub := &piiStubTransform{detector: pii.NewDetector(pii.Config{Enabled: true})}

	// Prescribed registration order: security-transform -> secret-obfuscation -> pii.
	if h := NewSecurityTransformContext(orch, slog.Default()); h != nil {
		reg.RegisterTransformContext("security-transform", HookPriorityCritical, h)
	}
	if h := NewSecretObfuscationHook(obfuscator, slog.Default()); h != nil {
		reg.RegisterTransformContext("secret-obfuscation", HookPriorityCritical, h)
	}
	reg.RegisterTransformContext("pii-restore-stub", HookPriorityCritical, stub)

	// Registration-order semantics of HookPriorityCritical peers: equal
	// priorities keep insertion order, so pii sits after
	// secret-obfuscation in the registry's pass list.
	reg.mu.RLock()
	names := make([]string, 0, len(reg.transformContexts))
	for _, h := range reg.transformContexts {
		names = append(names, h.Name)
	}
	reg.mu.RUnlock()
	want := []string{"security-transform", "secret-obfuscation", "pii-restore-stub"}
	if len(names) != len(want) {
		t.Fatalf("registry pass list = %v; want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("registry pass order = %v; want %v", names, want)
		}
	}

	// Run the transform pass to fixpoint: RunTransformContext short-circuits
	// on the first Modified=true hook, so composition requires repeated
	// passes until nothing modifies.
	msgs := []llm.ChatMessage{{
		Role:    llm.RoleUser,
		Content: "the credential is " + secret + " and my mail is bob@example.com",
	}}
	for i := 0; i < 5; i++ {
		tr := reg.RunTransformContext(context.Background(), msgs, nil)
		if !tr.Modified {
			break
		}
		msgs = tr.Messages
	}

	// The pii stub must never have observed the RAW secret: secret
	// obfuscation ran strictly before the pii pass. Its FIRST observation
	// must already carry the secret placeholder while the PII span is
	// still raw (the stub is the first text pass to see the PII).
	if len(stub.observed) == 0 {
		t.Fatal("pii stub never ran")
	}
	secretReplaced := regexp.MustCompile(`#[0-9A-F]{4}#`)
	for i, seen := range stub.observed {
		if strings.Contains(seen, secret) {
			t.Fatalf("pii stub observed raw secret (pass %d): %q", i, seen)
		}
		if !secretReplaced.MatchString(seen) {
			t.Fatalf("pii stub observed text with no secret placeholder (pass %d): %q", i, seen)
		}
	}
	if !strings.Contains(stub.observed[0], "bob@example.com") {
		t.Fatalf("pii stub's first observation lost the raw PII span: %q", stub.observed[0])
	}

	// Final content: secret obfuscated, PII swapped by the stub.
	final := msgs[0].Content
	if strings.Contains(final, secret) {
		t.Fatalf("raw secret survived the passes: %q", final)
	}
	if strings.Contains(final, "bob@example.com") {
		t.Fatalf("raw PII survived the stub: %q", final)
	}
	if !strings.Contains(final, "[PII:stub-fixed-token]") {
		t.Fatalf("pii stub token missing from final content: %q", final)
	}

	// Env hygiene for anything that reads os.Environ indirectly later.
	_ = os.Getenv("PII_ORDER_TEST_SECRET")
}
