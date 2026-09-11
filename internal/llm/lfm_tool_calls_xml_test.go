package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// Run-6 evidence pin: the exact malformed output the LFM2.5-MLX-4bit coder
// produced on the e2e write task (state/tasks.db, step ...-0001). The model
// leaked an Anthropic-style <function_calls>/<invocation> block as plain
// content instead of native markers, with a trailing comma before the first
// ']' and a doubled bracket block after it. Every call must still recover.
func TestParseLFMToolCalls_XMLShapeRun6Evidence(t *testing.T) {
	// Verbatim reconstruction of the run-6 step result body (minus the
	// "job ... completed by agent coder: " prefix, which is not part of the
	// model content).
	in := "job done\n" +
		"<function_calls>\n" +
		"<invocation>\n" +
		`[{"id":"a3b4c5d6-1e-4f-5b-7c3a-3b5c-3b5c-3b5c-3b5c-3b5c-3b5c","name":"file_write","arguments":{"path":"hello.txt","content":"hello"}},]` + "\n" +
		`[{"id":"b8c6d5e7-2a-4c5-8b-9c3a-3b5c-3b5c-3b5c-3b5c-3b5c-3b5c","name":"file_read","arguments":{"path":"hello.txt"}}]` + "\n" +
		"</function_calls>"

	content, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 from run-6 evidence shape", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("call 0 name = %q, want file_write", calls[0].Function.Name)
	}
	if calls[1].Function.Name != "file_read" {
		t.Errorf("call 1 name = %q, want file_read", calls[1].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("file_write args invalid JSON: %v", err)
	}
	if args["path"] != "hello.txt" || args["content"] != "hello" {
		t.Errorf("file_write args = %v, want path/content preserved", args)
	}
	if !strings.Contains(content, "job done") {
		t.Errorf("surrounding prose lost: %q", content)
	}
	if strings.Contains(content, "<function_calls>") || strings.Contains(content, "<invocation>") {
		t.Errorf("XML residue leaked into content: %q", content)
	}
	// The model supplied its own ids — they must survive so the executor
	// can pair results (native-marker recovery mints instead).
	if calls[0].ID != "a3b4c5d6-1e-4f-5b-7c3a-3b5c-3b5c-3b5c-3b5c-3b5c-3b5c" {
		t.Errorf("call 0 ID = %q, want the model-supplied id", calls[0].ID)
	}
}

// Clean-shape pin: a single well-formed XML-shape call parses without any
// tolerance machinery firing.
func TestParseLFMToolCalls_XMLShapeSingleCall(t *testing.T) {
	in := `<function_calls>[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}}]</function_calls>`
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	if strings.Contains(content, "function_calls") {
		t.Errorf("XML residue leaked into content: %q", content)
	}
}

// Multi-call array in ONE bracket pair — the intended shape — must recover
// in order.
func TestParseLFMToolCalls_XMLShapeMultiCallArray(t *testing.T) {
	in := `<function_calls><invocation>` +
		`[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}},` +
		`{"name":"file_read","arguments":{"path":"a.txt"}}]` +
		`</function_calls>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "file_write" || calls[1].Function.Name != "file_read" {
		t.Errorf("call order = [%s %s], want [file_write file_read]",
			calls[0].Function.Name, calls[1].Function.Name)
	}
}

// Malformed trailing comma inside a single call array must not prevent
// recovery.
func TestParseLFMToolCalls_XMLShapeTrailingComma(t *testing.T) {
	in := `<function_calls>[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}},]</function_calls>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (trailing comma tolerated)", len(calls))
	}
}

// XML shape wrapped in surrounding prose: the block is stripped and the
// prose stays intact.
func TestParseLFMToolCalls_XMLShapeWithSurroundingProse(t *testing.T) {
	in := "I'll write the file for you.\n" +
		`<function_calls>[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}}]</function_calls>` + "\n" +
		"Let me know if you need anything else."
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if !strings.Contains(content, "I'll write the file for you.") ||
		!strings.Contains(content, "Let me know if you need anything else.") {
		t.Errorf("prose mangled: %q", content)
	}
	if strings.Contains(content, "function_calls") {
		t.Errorf("XML residue leaked into content: %q", content)
	}
}

// Mixed shapes in one reply: native markers AND an XML block both recover,
// markers first.
func TestParseLFMToolCalls_MixedMarkerAndXMLShapes(t *testing.T) {
	in := `<|tool_call_start|>[file_read(path="native.txt")]<|tool_call_end|>` +
		`<function_calls>[{"name":"file_write","arguments":{"path":"xml.txt","content":"y"}}]</function_calls>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (one per shape)", len(calls))
	}
	if calls[0].Function.Name != "file_read" || calls[1].Function.Name != "file_write" {
		t.Errorf("order = [%s %s], want [file_read file_write]",
			calls[0].Function.Name, calls[1].Function.Name)
	}
}

// C1 pin extended to the XML shape: absent/empty model ids must be minted
// unique and deterministic.
func TestParseLFMToolCalls_XMLShapeMintsAbsentIDs(t *testing.T) {
	in := `<function_calls>` +
		`[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}},` +
		`{"id":"","name":"file_write","arguments":{"path":"a.txt","content":"x"}}]` +
		`</function_calls>`
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	seen := map[string]bool{}
	for i, c := range calls {
		if c.ID == "" {
			t.Errorf("call %d has empty ID", i)
		}
		if seen[c.ID] {
			t.Errorf("call %d ID %q duplicates an earlier call", i, c.ID)
		}
		seen[c.ID] = true
	}
	// Deterministic: re-parse yields the same minted IDs.
	_, again := parseLFMToolCalls(in)
	for i := range calls {
		if calls[i].ID != again[i].ID {
			t.Errorf("minted ID not stable on re-parse: %q vs %q", calls[i].ID, again[i].ID)
		}
	}
}

// Argument values containing braces/brackets must not confuse the object
// scanner.
func TestParseLFMToolCalls_XMLShapeBracesInArgValue(t *testing.T) {
	in := `<function_calls>[{"name":"file_write","arguments":{"path":"a.txt","content":"if (x) { return [y]; }"}}]</function_calls>`
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["content"] != "if (x) { return [y]; }" {
		t.Errorf("content arg = %v, want braces preserved", args["content"])
	}
	if strings.Contains(content, "function_calls") {
		t.Errorf("XML residue leaked into content: %q", content)
	}
}

// LOW pin mirroring TestParseLFMToolCalls_TruncatedMarkerNoPanic: a stream
// cut off mid-<function_calls> block (no closing tag) still recovers the
// complete call objects that arrived, mirroring the unclosed-marker policy.
func TestParseLFMToolCalls_UnclosedXMLBlockRecovers(t *testing.T) {
	// Cut off mid-block AFTER a complete call object: recovery must fire.
	in := `<function_calls>
<invocation>
[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}},{"name":"file_re`
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 from unclosed block", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	if strings.Contains(content, "<function_calls>") {
		t.Errorf("unclosed opening tag should be stripped on successful recovery: %q", content)
	}

	// Cut off BEFORE any complete object: nothing to recover, raw text kept.
	in2 := `<function_calls>
<invocation>
[{"name":"file_wr`
	content2, calls2 := parseLFMToolCalls(in2)
	if len(calls2) != 0 {
		t.Errorf("got %d calls from pre-object cutoff, want 0", len(calls2))
	}
	if !strings.Contains(content2, "<function_calls>") {
		t.Errorf("unrecoverable truncated block should stay in content for the caller to log: %q", content2)
	}
}

// Plain prose that happens to mention function_calls (no tags) must not be
// touched.
func TestParseLFMToolCalls_NoFalsePositiveOnProse(t *testing.T) {
	in := `The system prompt mentions function_calls but this is just prose about the <function_calls> API.`
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 {
		t.Errorf("got %d calls from prose, want 0", len(calls))
	}
	if content != in {
		t.Errorf("prose mangled: %q", content)
	}
}

// Native-marker pins must still pass with the XML parser in the pipeline:
// marker-free content is returned unchanged.
func TestParseLFMToolCalls_PlainContentUnchanged(t *testing.T) {
	in := "Just a normal reply with no tool calls whatsoever."
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
	if content != in {
		t.Errorf("plain content mangled: %q", content)
	}
}
