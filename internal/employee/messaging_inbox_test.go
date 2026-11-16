package employee

import (
	"encoding/json"
	"strings"
	"testing"
)

// Messaging inbox leaf 03 (F6): agent_messages.body carries either legacy
// raw text OR an AgentInboxBody JSON envelope {"body":...,"structured":{...}}.
// These tests pin the typed-body contract (round-trip + legacy compat) and
// stay RED until AgentInboxBody / InboxStructured / ParseAgentInboxBody
// exist in this package.
func TestAgentInboxBody_Roundtrip(t *testing.T) {
	t.Run("structured body marshals to body+structured envelope", func(t *testing.T) {
		in := AgentInboxBody{
			Body: "please review the parser change",
			Structured: &InboxStructured{
				ToolTargets: []string{"shell", "github"},
				FilePaths:   []string{"internal/employee/messaging.go"},
				PlanEntries: []string{"03-01"},
			},
		}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		// The wire shape is contractual: top-level "body" + "structured",
		// structured keys tool_targets/file_paths/plan_entries.
		var probe map[string]any
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("marshaled body is not a JSON object: %v", err)
		}
		structured, ok := probe["structured"].(map[string]any)
		if !ok {
			t.Fatalf("envelope must carry a %q object; got %s", "structured", raw)
		}
		for _, key := range []string{"tool_targets", "file_paths", "plan_entries"} {
			if _, ok := structured[key]; !ok {
				t.Fatalf("structured payload missing key %q; got %s", key, raw)
			}
		}
		if body, _ := probe["body"].(string); body != in.Body {
			t.Fatalf("envelope body = %q; want %q", body, in.Body)
		}

		var out AgentInboxBody
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if out.Body != in.Body {
			t.Fatalf("round-trip Body = %q; want %q", out.Body, in.Body)
		}
		if out.Structured == nil {
			t.Fatal("round-trip lost Structured")
		}
		want := in.Structured
		got := out.Structured
		if strings.Join(got.ToolTargets, ",") != strings.Join(want.ToolTargets, ",") {
			t.Fatalf("ToolTargets = %v; want %v", got.ToolTargets, want.ToolTargets)
		}
		if strings.Join(got.FilePaths, ",") != strings.Join(want.FilePaths, ",") {
			t.Fatalf("FilePaths = %v; want %v", got.FilePaths, want.FilePaths)
		}
		if strings.Join(got.PlanEntries, ",") != strings.Join(want.PlanEntries, ",") {
			t.Fatalf("PlanEntries = %v; want %v", got.PlanEntries, want.PlanEntries)
		}
	})

	t.Run("structured omitted stays omitempty", func(t *testing.T) {
		raw, err := json.Marshal(AgentInboxBody{Body: "plain note"})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(raw), "structured") {
			t.Fatalf("nil Structured must be omitted from the envelope; got %s", raw)
		}
	})

	t.Run("legacy raw text decodes as body", func(t *testing.T) {
		got := ParseAgentInboxBody("hello")
		if got.Body != "hello" {
			t.Fatalf("legacy Body = %q; want %q", got.Body, "hello")
		}
		if got.Structured != nil {
			t.Fatalf("legacy Structured = %+v; want nil", got.Structured)
		}
	})

	t.Run("typed envelope parses with structured", func(t *testing.T) {
		envelope := `{"body":"run the gate","structured":{"tool_targets":["shell"],"file_paths":[],"plan_entries":["03-02"]}}`
		got := ParseAgentInboxBody(envelope)
		if got.Body != "run the gate" {
			t.Fatalf("Body = %q; want %q", got.Body, "run the gate")
		}
		if got.Structured == nil {
			t.Fatal("structured envelope must parse with Structured set")
		}
		if len(got.Structured.ToolTargets) != 1 || got.Structured.ToolTargets[0] != "shell" {
			t.Fatalf("ToolTargets = %v; want [shell]", got.Structured.ToolTargets)
		}
		if len(got.Structured.PlanEntries) != 1 || got.Structured.PlanEntries[0] != "03-02" {
			t.Fatalf("PlanEntries = %v; want [03-02]", got.Structured.PlanEntries)
		}
	})

	t.Run("invalid json falls back to raw body", func(t *testing.T) {
		raw := `{"body": nope`
		got := ParseAgentInboxBody(raw)
		if got.Body != raw {
			t.Fatalf("unparseable body must fall back verbatim; got %q", got.Body)
		}
		if got.Structured != nil {
			t.Fatalf("fallback Structured = %+v; want nil", got.Structured)
		}
	})
}
