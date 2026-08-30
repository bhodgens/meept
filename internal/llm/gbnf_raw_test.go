package llm

import (
	"testing"
)

// TestWithRawGrammar_AttachesWhenConstrainedOn verifies the raw-grammar seam:
// WithRawGrammar + switch on ⇒ payload["grammar"] set verbatim, even with no
// tools on the request (the WithGrammar/attachToolGrammar path requires
// tools; this seam exists for tool-free structured output, e.g. SKILL.state).
func TestWithRawGrammar_AttachesWhenConstrainedOn(t *testing.T) {
	prev := GBNFConstrainedEnabled()
	SetGBNFConstrained(true)
	t.Cleanup(func() { SetGBNFConstrained(prev) })

	opts := &chatOptions{}
	WithRawGrammar("root ::= \"ok\"")(opts)
	if opts.rawGrammar == "" {
		t.Fatal("option did not set rawGrammar")
	}
	payload := map[string]any{}
	attachRawGrammar(payload, opts)
	g, ok := payload["grammar"].(string)
	if !ok || g != "root ::= \"ok\"" {
		t.Fatalf("grammar = %v, want verbatim body", payload["grammar"])
	}
}

// TestAttachRawGrammar_OffSwitchNoAttach: switch off ⇒ payload untouched.
func TestAttachRawGrammar_OffSwitchNoAttach(t *testing.T) {
	prev := GBNFConstrainedEnabled()
	SetGBNFConstrained(false)
	t.Cleanup(func() { SetGBNFConstrained(prev) })

	opts := &chatOptions{}
	WithRawGrammar("root ::= \"ok\"")(opts)
	payload := map[string]any{}
	attachRawGrammar(payload, opts)
	if _, ok := payload["grammar"]; ok {
		t.Fatal("grammar attached with GBNF switch off")
	}
}

// TestAttachRawGrammar_EmptyNoAttach: empty grammar ⇒ no-op.
func TestAttachRawGrammar_EmptyNoAttach(t *testing.T) {
	opts := &chatOptions{}
	WithRawGrammar("")(opts)
	payload := map[string]any{}
	attachRawGrammar(payload, opts)
	if _, ok := payload["grammar"]; ok {
		t.Fatal("empty grammar modified payload")
	}
}

// TestAttachRawGrammar_DoesNotRequireTools pins the reason this seam exists:
// no tools in chatOpts, yet the grammar still attaches with the switch on.
func TestAttachRawGrammar_DoesNotRequireTools(t *testing.T) {
	prev := GBNFConstrainedEnabled()
	SetGBNFConstrained(true)
	t.Cleanup(func() { SetGBNFConstrained(prev) })

	opts := &chatOptions{}
	WithRawGrammar("root ::= \"x\"")(opts)
	if len(opts.tools) != 0 {
		t.Fatal("test precondition broken: tools must be empty")
	}
	payload := map[string]any{}
	attachRawGrammar(payload, opts)
	if _, ok := payload["grammar"]; !ok {
		t.Fatal("raw grammar must attach on tool-free requests")
	}
}
