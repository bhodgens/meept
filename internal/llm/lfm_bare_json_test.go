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
	_, calls := parseLFMToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("an array element behind a prose contraction must not be mined, got %+v (F78)", calls)
	}
	// "rest == content" here is implied by parseLFMBareJSONCalls' len(calls)==0
	// early return — it cannot fail. The assert that CAN fail is the helper's
	// own verdict: the element must be marked, otherwise the skip is an
	// accident of the return path rather than the array rule.
	objStart := strings.Index(content, `{"name"`)
	if !arrayElementOffsets(content)[objStart] {
		t.Errorf("arrayElementOffsets must mark the array element at %d, got %v (F78)",
			objStart, arrayElementOffsets(content))
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
	// The call's name and its absence from the remainder are both implied by
	// len(calls)==1, so asserting them proves nothing. These two asserts can
	// fail: a mangled arguments payload, or a strip that eats the prose that
	// surrounds the mined call.
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["path"] != "a.txt" {
		t.Errorf("args = %v, want the call's own path argument recovered intact", args)
	}
	if !strings.Contains(rest, "1) read the file") || !strings.Contains(rest, "Call:") {
		t.Errorf("the prose around the mined call must survive, got %q", rest)
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
	// The boxed-wrapper cleanup rewrites the REMAINDER only. Its asserts live
	// on `rest` below; this one guards the other half of the seam — a mined
	// call's arguments must never be reached by the display-text cleanup.
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not JSON: %v", err)
	}
	if args["text"] != "x" {
		t.Errorf("boxed-wrapper cleanup altered the mined call's arguments: %v", args)
	}
	if !strings.Contains(rest, `{"title": "x", "year": 2017}`) {
		t.Errorf("the JSON payload after the boxed call must keep its final brace, got %q (F79)", rest)
	}
	if strings.HasPrefix(rest, "}") {
		t.Errorf("the box's orphan brace must not survive ahead of the payload, got %q (F79)", rest)
	}
}

// TestParseLFMToolCalls_ArrayElementAfterUnbalancedTail pins the wave-3
// finding (group B, item 1): the whole-content bracket veto ("an unbalanced
// stack yields no array verdict") voided EVERY offset for the whole content,
// so an unbalanced bracket anywhere — or an apostrophe quoted inside a LATER
// bracket run (quotes are tracked only inside a run, and an unterminated quote
// hides the closing bracket) — re-armed the F78 bug: a genuine array element
// after a balanced "[...]" was mined as a bare call and stripped out of the
// model's answer. Element-ness is decided by PAIRING: the object's own
// enclosing '[' must close after it.
func TestParseLFMToolCalls_ArrayElementAfterUnbalancedTail(t *testing.T) {
	cases := []struct{ name, content string }{
		{
			"unbalanced bracket tail",
			`[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}}] Steps: [1, 2, 3`,
		},
		{
			"apostrophe inside a later bracket run",
			`[{"name":"file_write","arguments":{"path":"a.txt","content":"x"}}] note [it's here`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := parseLFMToolCalls(tc.content)
			if len(calls) != 0 {
				t.Fatalf("a genuine array element was mined after an unbalanced tail: %d calls (%+v)",
					len(calls), calls)
			}
			// The verdict must come from per-object pairing, not from the
			// whole content happening to balance: assert the helper itself
			// marks the element (this is the assert that fails at the
			// whole-content-veto revision).
			objStart := strings.Index(tc.content, `{"name"`)
			if !arrayElementOffsets(tc.content)[objStart] {
				t.Errorf("arrayElementOffsets must mark the element at %d (pairing, not whole-content balance)",
					objStart)
			}
		})
	}
}

// TestParseLFMToolCalls_TruncatedArrayNotMined pins the wave-3 finding (group
// B, item 2): the truncation carve-out ("A truncated reply inside an array is
// the one case this now mines") minted a call from a prose TEMPLATE — the same
// text as a legitimate example array minus its closing ']' — so a stream cutoff
// on an example array executed the example. A cutoff on an example array is
// indistinguishable from a truncated real array, and a truncated response is a
// decode/retry problem, not a tool call, so the element must stay un-mined.
func TestParseLFMToolCalls_TruncatedArrayNotMined(t *testing.T) {
	cases := []struct{ name, content, objMark string }{
		{
			"truncated example array (opening element)",
			`For example, call it like so: [{"name":"json_extract","arguments":{"text":"x"}}`,
			`{"name":"json_extract"`,
		},
		{
			"truncated array, later element after a comma",
			`[{"name":"file_write","args":{"path":"a"}},{"name":"json_extract","arguments":{"text":"b"}}`,
			`{"name":"json_extract"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, calls := parseLFMToolCalls(tc.content)
			if len(calls) != 0 {
				t.Fatalf("a truncated array must not mint a call: recovered %d (%+v)", len(calls), calls)
			}
			objStart := strings.Index(tc.content, tc.objMark)
			if !arrayElementOffsets(tc.content)[objStart] {
				t.Errorf("the truncated array's element at %d must be marked as an element, not a call",
					objStart)
			}
		})
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
