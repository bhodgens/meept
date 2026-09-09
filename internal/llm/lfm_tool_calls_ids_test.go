package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// C1 pin: recovered LFM tool calls MUST carry unique non-empty IDs.
// Empty IDs collapse the executor's idToIdx map on multi-call replies
// (results scatter to one slot, the rest stay nil → panic) and break
// assistant-tool_calls/tool-result pairing on the wire.
func TestParseLFMToolCalls_IDsUniqueAndNonEmpty(t *testing.T) {
	in := "<|tool_call_start|>[file_read(path=\"a\")]<|tool_call_end|>" +
		"<|tool_call_start|>[file_read(path=\"a\")]<|tool_call_end|>" + // identical body
		"<|tool_call_start|>[file_write(path=\"b\", content=\"c\")]<|tool_call_end|>"
	_, calls := parseLFMToolCalls(in)
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
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
}

// ID stability: re-parsing identical content yields identical IDs, but a
// different body at the same index yields a different ID.
func TestLFMToolCallID_StableAndBodySensitive(t *testing.T) {
	a1 := lfmToolCallID(0, "file_read(path=\"a\")")
	a2 := lfmToolCallID(0, "file_read(path=\"a\")")
	b := lfmToolCallID(0, "file_read(path=\"b\")")
	c := lfmToolCallID(1, "file_read(path=\"a\")")
	if a1 != a2 {
		t.Errorf("same body+index produced different IDs: %q vs %q", a1, a2)
	}
	if a1 == b {
		t.Errorf("different bodies at same index share ID %q", a1)
	}
	if a1 == c {
		t.Errorf("same body at different indexes share ID %q", a1)
	}
}

// M9 pin: escaped quotes must not terminate the string early, and the
// escape sequences unescape to their literal characters. Input is the
// ARGS STRING as the production path passes it (regex group 2, wrapper
// parens already stripped).
func TestParseLFMArgs_Escapes(t *testing.T) {
	in := "path=\"x\", content=\"line1\\nline2\\ttab\", quote=\"say \\\"hi\\\"\", bs=\"a\\\\b\""
	args := parseLFMArgs(in)
	if got := args["content"]; got != "line1\nline2\ttab" {
		t.Errorf("content = %q, want escaped backslash-n and backslash-t decoded", got)
	}
	if got := args["quote"]; got != `say "hi"` {
		t.Errorf("quote = %q, want escaped inner quotes decoded", got)
	}
	if got := args["bs"]; got != `a\b` {
		t.Errorf("bs = %q, want double backslash decoded to single", got)
	}
	if got := args["path"]; got != "x" {
		t.Errorf("path = %q, want x", got)
	}
}

// Truncated-input safety for the escape scanner: a lone trailing backslash
// must not read past end-of-input, and the malformed pair degrades to the
// raw string (leading quote + abc + backslash), not a panic or a missing
// key.
func TestParseLFMArgs_TrailingBackslashSafe(t *testing.T) {
	in := `x="abc\`
	args := parseLFMArgs(in) // must not panic
	if v, ok := args["x"]; !ok {
		t.Errorf("x missing from %v", args)
	} else if s, _ := v.(string); !strings.Contains(s, "abc") {
		t.Errorf("x = %q, want abc preserved", s)
	}
}

// M10 pin: a multi-line marker body (newline inside the brackets) must
// still be recovered now that the regexes carry (?s).
func TestParseLFMToolCalls_MultiLineBody(t *testing.T) {
	in := "<|tool_call_start|>[file_write(path=\"a.txt\", content=\"line1\nline2\")]<|tool_call_end|>"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (multi-line body must recover)", len(calls))
	}
	if strings.Contains(content, "tool_call_start") {
		t.Errorf("marker text leaked into content: %q", content)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid: %v", err)
	}
	if args["content"] != "line1\nline2" {
		t.Errorf("content arg = %v, want multi-line value", args["content"])
	}
}

// M10 pin: a non-conforming marker body is dropped (not recovered) but the
// markers are still stripped — and the drop is no longer silent.
func TestParseLFMToolCalls_NonConformingBodyDropped(t *testing.T) {
	in := "<|tool_call_start|>[no parens here]<|tool_call_end|>"
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0 for non-conforming body", len(calls))
	}
	if strings.Contains(content, "tool_call") {
		t.Errorf("marker text leaked into content: %q", content)
	}
}

// LOW pin: a stream truncated mid-marker leaves an unclosed start marker in
// content; parseLFMToolCalls must not panic and must not fabricate a call.
func TestParseLFMToolCalls_TruncatedMarkerNoPanic(t *testing.T) {
	in := "text <|tool_call_start|>[file_write(path="
	content, calls := parseLFMToolCalls(in)
	if len(calls) != 0 {
		t.Errorf("got %d calls from truncated marker, want 0", len(calls))
	}
	if !strings.Contains(content, "tool_call_start") {
		t.Errorf("truncated raw marker should stay in content for the caller to log: %q", content)
	}
}
