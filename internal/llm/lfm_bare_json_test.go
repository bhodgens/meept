package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// LFM2.5-8B-A1B (MLX 4-bit) ships its tool call as a bare JSON object in
// prose — no markers, no fence, no <function_calls> wrapper. Verified BY
// TEST 2026-09-12; without recovery the turn runs zero tools and the step
// auto-approves a prose answer.

func TestParseLFMToolCalls_BareBoxedJSONCall(t *testing.T) {
	content := "\\boxed{\n  \"name\": \"json_extract\",\n  \"arguments\": {\"text\": \"Attention Is All You Need.\"}\n}"
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1 (content=%q)", len(calls), content)
	}
	if calls[0].Function.Name != "json_extract" {
		t.Errorf("name = %q, want json_extract", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["text"] != "Attention Is All You Need." {
		t.Errorf("text arg = %v, want the extracted passage", args["text"])
	}
	if strings.Contains(rest, "json_extract") {
		t.Errorf("call text should be stripped from the remainder, got %q", rest)
	}
}

func TestParseLFMToolCalls_BareArgsAlias(t *testing.T) {
	content := `{"name": "file_write", "args": {"path": "hello.txt", "content": "hello"}}`
	_, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
}

func TestParseLFMToolCalls_PlainJSONAnswerSurvives(t *testing.T) {
	// The extracted record itself has no call shape — it must NOT be mined
	// and MUST survive verbatim.
	content := `{"title": "Attention Is All You Need", "authors": ["Vaswani"], "year": 2017}`
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("plain JSON data was mined as a tool call: %+v", calls)
	}
	if rest != content {
		t.Errorf("plain JSON data was modified: %q", rest)
	}
}

func TestParseLFMToolCalls_EvidenceEnvelopeSurvives(t *testing.T) {
	// The validation envelope (claims/evidence) is prose output, not a call.
	content := `{"claims": ["Extracted metadata"], "evidence": {"type": "json", "content": "{\"title\": \"x\"}"}}`
	_, calls := parseLFMToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("evidence envelope was mined as a tool call: %+v", calls)
	}
}

func TestParseLFMToolCalls_BareCallBetweenProse(t *testing.T) {
	content := "I will call the tool now.\n{\"name\": \"json_extract\", \"arguments\": {\"text\": \"abc\"}}\nDone."
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1", len(calls))
	}
	if !strings.Contains(rest, "I will call the tool now.") || !strings.Contains(rest, "Done.") {
		t.Errorf("surrounding prose should survive, got %q", rest)
	}
}
