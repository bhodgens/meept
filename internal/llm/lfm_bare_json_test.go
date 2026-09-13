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

// TestParseLFMToolCalls_BareCallAfterComma pins audit finding F76: the miner
// used to treat "the previous non-space byte is a comma" as "inside a JSON
// array", so a call object that follows a comma in ordinary prose was silently
// dropped. A comma is not evidence of an array; only real bracket nesting is.
func TestParseLFMToolCalls_BareCallAfterComma(t *testing.T) {
	content := "Here is the config, {\"name\": \"json_extract\", \"arguments\": {\"text\": \"abc\"}}"
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("a bare call preceded by a comma must be mined, recovered %d calls (F76)", len(calls))
	}
	if calls[0].Function.Name != "json_extract" {
		t.Errorf("name = %q, want json_extract", calls[0].Function.Name)
	}
	if strings.Contains(rest, "json_extract") {
		t.Errorf("the call must be stripped from the remainder, got %q", rest)
	}
}

// TestParseLFMToolCalls_TwoCallsSeparatedByComma covers the same hole with two
// call objects in one reply: the second follows a comma, not an array.
func TestParseLFMToolCalls_TwoCallsSeparatedByComma(t *testing.T) {
	content := `{"name":"file_write","args":{"path":"a.txt"}}, {"name":"json_extract","arguments":{"text":"b"}}`
	_, calls := parseLFMToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("comma-separated call objects must both be mined, recovered %d (F76)", len(calls))
	}
}

// TestParseLFMToolCalls_ArrayElementStillSkipped is the guard the F76 fix must
// NOT weaken: an object that really is an element of a JSON array stays
// un-mined (that shape is the fence/XML halves' territory).
func TestParseLFMToolCalls_ArrayElementStillSkipped(t *testing.T) {
	content := `[{"name": "json_extract", "arguments": {"text": "abc"}}]`
	_, calls := parseLFMToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("an object inside a JSON array must not be mined as a bare call, got %+v", calls)
	}
}

// TestParseLFMToolCalls_BoxedWrapperLeavesNoStrayToken pins audit finding F77:
// the documented `\boxed{\n {...}\n}` shape consumes the box's '{' with the
// mined object, so the remainder is the bare token `\boxed` — it (and the
// double-braced variant's stray '}') must not survive into the reply content.
func TestParseLFMToolCalls_BoxedWrapperLeavesNoStrayToken(t *testing.T) {
	content := "\\boxed{\n  \"name\": \"json_extract\",\n  \"arguments\": {\"text\": \"x\"}\n}"
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1", len(calls))
	}
	if rest != "" {
		t.Errorf("remainder = %q, want empty (the \\boxed token must be stripped, F77)", rest)
	}

	content2 := "\\boxed{ {\"name\": \"json_extract\", \"arguments\": {\"text\": \"y\"}} }"
	rest2, calls2 := parseLFMToolCalls(content2)
	if len(calls2) != 1 {
		t.Fatalf("double-brace: recovered %d calls, want 1", len(calls2))
	}
	if rest2 != "" {
		t.Errorf("double-brace remainder = %q, want empty (F77)", rest2)
	}
}

// TestParseLFMToolCalls_ArrayElementAfterProseApostrophe pins audit finding F78
// (direction 1): the F76 array check tracked quotes from byte 0, so a prose
// contraction ("here's") opened a string that never closed, every later bracket
// became invisible, the stack read empty — and an object that really WAS an
// array element was mined as a call and stripped out of the model's answer.
func TestParseLFMToolCalls_ArrayElementAfterProseApostrophe(t *testing.T) {
	content := "Sure — here's the plan.\n[{\"name\": \"json_extract\", \"arguments\": {\"text\": \"abc\"}}]"
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("an array element behind a prose contraction must not be mined, got %+v (F78)", calls)
	}
	if rest != content {
		t.Errorf("the array element was modified while not mined:\n got %q\nwant %q", rest, content)
	}
}

// TestParseLFMToolCalls_BareCallAfterUnbalancedProseBracket pins audit finding
// F78 (direction 2): an unbalanced prose '[' left a phantom array open across
// every later object, so a real bare call was silently dropped.
func TestParseLFMToolCalls_BareCallAfterUnbalancedProseBracket(t *testing.T) {
	content := "Steps: [1) read the file. Call: {\"name\": \"file_write\", \"args\": {\"path\": \"a.txt\"}}"
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("a bare call after an unbalanced prose '[' must still be mined, recovered %d (F78)", len(calls))
	}
	if calls[0].Function.Name != "file_write" {
		t.Errorf("name = %q, want file_write", calls[0].Function.Name)
	}
	if strings.Contains(rest, "file_write") {
		t.Errorf("the call must be stripped from the remainder, got %q", rest)
	}
}

// TestParseLFMToolCalls_BoxedWrapperKeepsFollowingJSONBrace pins audit finding
// F79: the boxed-wrapper cleanup stripped a trailing '}' whenever a box token
// appeared ANYWHERE in the remainder, so a legitimate JSON payload that
// followed the boxed call lost its final brace (and the box's real orphan brace
// survived in its place).
func TestParseLFMToolCalls_BoxedWrapperKeepsFollowingJSONBrace(t *testing.T) {
	content := `\boxed{{"name": "json_extract", "arguments": {"text": "x"}}} the record is {"title": "x", "year": 2017}`
	rest, calls := parseLFMToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("recovered %d calls, want 1", len(calls))
	}
	if !strings.Contains(rest, `{"title": "x", "year": 2017}`) {
		t.Errorf("the JSON payload after the boxed call must keep its final brace, got %q (F79)", rest)
	}
	if strings.HasPrefix(rest, "}") {
		t.Errorf("the box's orphan brace must not survive ahead of the payload, got %q (F79)", rest)
	}
}

// A tool-less request must pass a call-shaped JSON object through untouched.
// The intent analyzer asks for analysis JSON ({"goal": ..., "category": ...});
// mining a call-like object out of that reply stripped it to empty content and
// the classifier stage failed with "intent analysis: empty content"
// (fresh-rig run 5, 2026-09-12). Without tools offered, there is no legitimate
// tool call to recover.
func TestParseLFMBareJSON_NotMinedWithoutTools(t *testing.T) {
	answer := `{"name": "get_intent", "arguments": {"input": "do some research"}}`
	content, calls := parseLFMToolCallsWithBare(answer, false)
	if len(calls) != 0 {
		t.Fatalf("recovered %d calls from a tool-less reply, want 0", len(calls))
	}
	if content != answer {
		t.Errorf("content was modified:\n got %q\nwant %q", content, answer)
	}

	// The same reply WITH tools offered is a call, and is still recoverable.
	_, withTools := parseLFMToolCallsWithBare(answer, true)
	if len(withTools) != 1 {
		t.Fatalf("recovered %d calls with tools offered, want 1", len(withTools))
	}
	if withTools[0].Function.Name != "get_intent" {
		t.Errorf("recovered call name = %q, want get_intent", withTools[0].Function.Name)
	}
}

// Unambiguous marker syntax is recovered either way: it cannot be prose.
func TestParseLFMMarkers_RecoveredWithoutTools(t *testing.T) {
	reply := `<|tool_call_start|>[json_extract(text="x")]<|tool_call_end|>`
	content, calls := parseLFMToolCallsWithBare(reply, false)
	if len(calls) != 1 {
		t.Fatalf("recovered %d marker calls without tools, want 1 (markers are unambiguous)", len(calls))
	}
	if strings.TrimSpace(content) != "" {
		t.Errorf("content = %q, want the marker text stripped", content)
	}
}
