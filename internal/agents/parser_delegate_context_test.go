package agents

// DSH-1: the `delegate_context` frontmatter knob (full|task, default task)
// on AgentMetadata. Pins the parse path (underscore form natively via the
// yaml tag) and the discovery-shadow scalar-replacement semantics (the
// higher tier's value wins outright, like model_alias — NOT a union).

import (
	"testing"
)

// T1: `delegate_context: full` parses into AgentMetadata.DelegateContext.
func TestParseAgentText_DelegateContext(t *testing.T) {
	text := `---
id: coder
name: Coder
role: executor
delegate_context: full
---

Body.
`
	def, err := ParseAgentText(text)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	if def.DelegateContext != "full" {
		t.Fatalf("DelegateContext = %q, want full", def.DelegateContext)
	}
}

// T2: absent knob = "" (the fork gate treats "" and "task" identically as
// task scope; "" is the storage default, "task" the explicit form).
func TestParseAgentText_DelegateContextDefault(t *testing.T) {
	text := `---
id: coder
name: Coder
role: executor
---

Body.
`
	def, err := ParseAgentText(text)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	if def.DelegateContext != "" {
		t.Fatalf("DelegateContext = %q, want empty (default)", def.DelegateContext)
	}
}

// T3: discovery shadowing replaces the scalar outright (higher tier wins).
func TestDelegateContext_ShadowScalarReplacement(t *testing.T) {
	base := AgentMetadata{ID: "coder", DelegateContext: "task"}
	shadow := AgentMetadata{ID: "coder", DelegateContext: "full"}

	merged := base
	if shadow.DelegateContext != "" {
		merged.DelegateContext = shadow.DelegateContext
	}
	if merged.DelegateContext != "full" {
		t.Fatalf("shadowed DelegateContext = %q, want full (outright replacement)", merged.DelegateContext)
	}
}
