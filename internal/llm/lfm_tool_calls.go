package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// lfmToolCallMarker matches LiquidAI LFM2.5's native tool-call syntax:
//
//	<|tool_call_start|>[funcName(key="value", ...)]<|tool_call_end|>
//
// mlx_lm server does not parse this format — it returns the markers as
// plain message content with tool_calls: null (unlike llama.cpp, which
// ships an LFM2.5 tool-call extractor). parseLFMToolCalls recovers the
// structured calls so MLX-served LFM2.5 models work in the agent tool loop.
//
// (?s) lets the body span newlines: without it, a multi-line marker body
// fails to match and the raw marker text leaks into message content.
var lfmToolCallMarker = regexp.MustCompile(`(?s)<\|tool_call_start\|>(.*?)<\|tool_call_end\|>`)

// lfmFuncCall matches a single bracketed function call inside the markers.
// (?s) allows multi-line argument bodies; the anchors keep the full-body
// conformance check (non-conforming bodies are dropped with a warning).
var lfmFuncCall = regexp.MustCompile(`(?s)^\[([A-Za-z_][A-Za-z0-9_]*)\((.*)\)\]$`)

// lfmFunctionCallsBlock matches the Anthropic-style prose shape that
// quantized LFM2.5 MLX models leak instead of their native markers:
//
//	<function_calls>
//	<invocation>
//	[{"id":"...","name":"file_write","arguments":{...}},]
//	...
//	</function_calls>
//
// The model was evidently exposed to an Anthropic-format chat template
// (template bleed), so the tool call ships as XML-tagged JSON in plain
// message content — mlx_lm returns it with tool_calls: null. The opening
// <invocation> tag is OPTIONAL (it may be dropped or doubled by the
// quantized template). (?i) tolerates case drift. (?s) lets the body span
// newlines.
var lfmFunctionCallsBlock = regexp.MustCompile(`(?is)<function_calls>\s*(?:<invocation>\s*)?(.*?)</function_calls>`)

// parseLFMToolCalls scans content for LFM2.5 tool calls in any known shape
// and converts each to a ToolCall. Matched blocks are stripped from the
// returned content. When no shape is present the content is returned
// unchanged with nil calls.
//
// Shape 1 — native markers: <|tool_call_start|>[fn(k=v, ...)]<|tool_call_end|>
// Shape 2 — Anthropic-style XML prose (quantized-model template bleed):
// <function_calls>[<invocation>]JSON-array-of-call-objects</function_calls>.
// Shape 3 — markdown json-fence bleed (e2e run 7, 2026-09-10): the call
// ships as a fenced JSON object in plain content —
//
//	```json
//	{"name":"file_write","args":{"path":"hello.txt","content":"hello"}}
//	```
//
// The XML shape carries calls as JSON objects {id,name,arguments}; real
// quantized output is often malformed, so parsing is deliberately tolerant:
// trailing commas, doubled bracket blocks, leading prose, and unclosed
// blocks at stream cutoff all still recover.
//
// Each recovered call is minted a unique deterministic ID: "lfm-<i>-<hash>"
// for markers, the model's own id when one survives inside an XML-shape
// object, else "lfm-<i>-<hash>" over the ("xml:"+object) body — mlx_lm
// returns no IDs, but the executor keys result slots by ToolCall.ID and the
// wire marshaller omits tool_call_id when empty — empty IDs on a multi-call
// reply collapse the executor's idToIdx map and break pairing.
func parseLFMToolCalls(content string) (string, []ToolCall) {
	return parseLFMToolCallsWithBare(content, true)
}

// parseLFMToolCallsWithBare is parseLFMToolCalls with the bare-JSON half made
// optional. allowBare must be TRUE only when the request actually offered
// tools.
//
// Why the switch exists: the bare-JSON half is the one ambiguous shape. Marker
// (<|tool_call_start|>), XML (<function_calls>) and fenced syntax are
// unambiguous tool-call language, but a JSON object in prose can equally be
// the model's ANSWER. Mining it on a tool-less request destroyed a real reply:
// the intent analyzer asked for analysis JSON, the reply carried a call-like
// object, the miner stripped it, and the classifier stage failed with
// "intent analysis: empty content" (fresh-rig run 5, 2026-09-12). A request
// with no tools cannot have a legitimate tool call, so the half is skipped.
func parseLFMToolCallsWithBare(content string, allowBare bool) (string, []ToolCall) {
	content, markerCalls := parseLFMMarkerCalls(content)
	content, xmlCalls := parseLFMXMLCalls(content)
	content, fenceCalls := parseLFMFenceCalls(content)
	var bareCalls []ToolCall
	if allowBare {
		content, bareCalls = parseLFMBareJSONCalls(content)
	}
	return content, append(append(append(markerCalls, xmlCalls...), fenceCalls...), bareCalls...)
}

// arrayElementOffsets marks every byte offset in content at which a '{' opens an
// object that is an ELEMENT of a JSON array: its immediately enclosing bracket
// is an '[' and the content's brackets balance. Offsets are the ones a caller
// would pass to findJSONObjectEnd (the object's opening brace).
//
// The array verdict is deliberately conservative (audit finding F78): brackets
// are tracked with quote awareness only INSIDE a bracket run, and a content
// whose brackets do not balance yields NO element offsets at all. Prose is not
// JSON — a contraction opens a string that never closes, an unmatched '[' never
// ends — and either mistake used to flip the verdict, mining an object out of a
// genuine array or silently dropping a real bare call.
func arrayElementOffsets(content string) map[int]bool {
	var stack []byte
	inStr := false
	var quote byte
	elem := make(map[int]bool)
	for i := 0; i < len(content); i++ {
		ch := content[i]
		switch {
		case inStr && ch == '\\':
			i++ // skip the escaped byte inside a string
		case inStr:
			if ch == quote {
				inStr = false
			}
		case (ch == '"' || ch == '\'') && len(stack) > 0:
			inStr = true
			quote = ch
		case ch == '[' || ch == '{':
			if ch == '{' && len(stack) > 0 && stack[len(stack)-1] == '[' {
				elem[i] = true
			}
			stack = append(stack, ch)
		case ch == ']' || ch == '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) != 0 {
		// Unbalanced brackets: the prose brackets never closed, so no offset
		// can be called an array element.
		return nil
	}
	return elem
}

// parseLFMBareJSONCalls mines tool calls that ship as a BARE JSON object in
// prose with no fence, marker, or XML wrapper — the shape LFM2.5-8B-A1B
// (MLX 4-bit) actually produces. Verified BY TEST 2026-09-12: asked to call
// json_extract, the model answered
//
//	\boxed{
//	  "name": "json_extract",
//	  "arguments": {"text": "..."}
//	}
//
// i.e. the call is correct and complete, but wrapped in a LaTeX box rather
// than any syntax the three earlier shapes recognize, so the turn ran with
// zero tool executions and the step auto-approved a prose answer. This half
// closes that gap.
//
// Safety: an object is minted as a call only when it carries a call shape
// (name/tool/function string + args/arguments/parameters payload) — the same
// structural test parseLFMFenceBody applies. A plain JSON answer (the
// extracted record itself, an evidence envelope) has no such key and is left
// untouched, so genuine data payloads survive verbatim.
func parseLFMBareJSONCalls(content string) (string, []ToolCall) {
	if !strings.Contains(content, "{") {
		return content, nil
	}
	// Markdown-fence spans are the fence half's territory: it deliberately
	// leaves an array body untouched (only single objects are mined there),
	// so re-mining inside a fence here would overturn that contract.
	type span struct{ lo, hi int }
	var fences []span
	for _, m := range lfmJSONFence.FindAllStringSubmatchIndex(content, -1) {
		fences = append(fences, span{m[0], m[1]})
	}
	inFence := func(pos int) bool {
		for _, f := range fences {
			if pos >= f.lo && pos < f.hi {
				return true
			}
		}
		return false
	}
	// An object that is an ELEMENT of a JSON array is likewise not a call
	// shape (same rule the fence half documents). This scans the ACTUAL bracket
	// nesting — a preceding comma is NOT evidence of an array: commas are
	// ordinary prose punctuation ("here is the config, {...}"), and treating
	// "after a comma" as "inside an array" silently dropped a bare call that
	// followed one (audit finding F76). Only an object whose immediately
	// enclosing bracket is an open '[' is skipped.
	//
	// Two rules keep the array verdict honest (audit finding F78):
	//   - quotes are tracked only INSIDE a bracket run. Prose contractions
	//     ("the user's request") otherwise opened a string that never closed,
	//     which hid every later bracket: the stack read empty, an object that
	//     really was an array element was mined as a call, and the call was
	//     stripped out of the model's answer.
	//   - the WHOLE content's brackets must balance. An unbalanced prose '['
	//     ("Steps: [1) read the file. {\"name\":...}") left a phantom array
	//     open across every later object, so a real bare call was dropped.
	// A truncated reply inside an array is the one case this now mines: no
	// closing bracket arrived, so there is no array to be an element of — and
	// the object is only minted when it carries a call shape anyway.
	arrayElems := arrayElementOffsets(content)
	inArray := func(pos int) bool { return arrayElems[pos] }

	var calls []ToolCall
	var kept strings.Builder
	i := 0
	for i < len(content) {
		rel := strings.IndexByte(content[i:], '{')
		if rel < 0 {
			break
		}
		start := i + rel
		end := findJSONObjectEnd(content[start:])
		if end < 0 {
			break // truncated tail object; keep the remainder verbatim
		}
		if inFence(start) || inArray(start) {
			kept.WriteString(content[i : start+1])
			i = start + 1
			continue
		}
		obj := content[start : start+end+1]
		mined := parseLFMFenceBody(obj)
		if len(mined) == 0 {
			// Not a call shape: keep this byte and look for the next '{'.
			kept.WriteString(content[i : start+1])
			i = start + 1
			continue
		}
		kept.WriteString(content[i:start])
		mined[0].ID = lfmToolCallID(len(calls), "bare:"+obj)
		calls = append(calls, mined...)
		i = start + end + 1
	}
	kept.WriteString(content[i:])
	if len(calls) == 0 {
		return content, nil
	}
	slog.Default().Warn("lfm tool-call recovered from bare JSON object in prose (model template bleed)",
		"calls", len(calls))
	// Drop a LaTeX \boxed{ } / \fbox{ } wrapper left dangling around the
	// stripped call so the remainder reads as prose, not stray syntax. Two
	// spellings occur:
	//   - the double-braced `\boxed{ {...} }` leaves the token AND the box's
	//     now-empty brace pair;
	//   - the single-braced `\boxed{ {...} }` whose opening brace was consumed
	//     WITH the mined object leaves only the bare token `\boxed`.
	// Only those two shapes are stripped, and only when a box token is actually
	// present, so ordinary prose ending in '}' is never touched (audit finding
	// F77) — and a legitimate JSON payload AFTER a boxed block keeps its final
	// '}' (audit finding F79: the brace strip used to fire on any remainder
	// containing a box token, truncating `... {"year": 2017}` to `... {"year":
	// 2017`).
	out := strings.TrimSpace(kept.String())
	if strings.Contains(out, `\boxed`) || strings.Contains(out, `\fbox`) {
		out = stripEmptyBoxWrappers(out)
		out = strings.ReplaceAll(out, `\boxed{`, "")
		out = strings.ReplaceAll(out, `\fbox{`, "")
		out = strings.TrimSpace(out)
		// The bare token (its opening brace consumed by the mined object) is
		// stripped only when it is the LAST non-space token.
		out = strings.TrimSpace(strings.TrimSuffix(out, `\boxed`))
		out = strings.TrimSpace(strings.TrimSuffix(out, `\fbox`))
	}
	return strings.TrimSpace(out), calls
}

// boxEmptyWrapper matches a box token whose brace pair is now empty — only
// whitespace between the token's '{' and its '}'. That is exactly the wrapper a
// mined call leaves behind, and matching it in a regexp RE2 handles the
// multi-line spelling (`\boxed{\n\n}`) the call's own newline padding leaves.
var boxEmptyWrapper = regexp.MustCompile(`(?s)\\(?:boxed|fbox)\{\s*\}`)

// stripEmptyBoxWrappers removes every empty box brace pair (`\boxed{ }`,
// `\fbox{\n}`) from out. It replaces the old "strip a trailing '}' whenever a
// box token appears anywhere" rule, which destroyed the final brace of a
// legitimate JSON payload that followed a boxed block (audit finding F79).
func stripEmptyBoxWrappers(out string) string {
	return boxEmptyWrapper.ReplaceAllString(out, "")
}

// lfmJSONFence matches a fenced JSON block whose body plausibly carries
// tool-call fields. The fence info string may carry a language tag (json)
// or none; the body is parsed and vetted structurally afterwards, so the
// regex only needs to find the fence boundaries, not validate the content.
// (Backticks are plain characters inside a double-quoted Go string, so the
// pattern is written quoted rather than raw.)
var lfmJSONFence = regexp.MustCompile("(?s)```[^\\n`]*\\n(.*?)```")

// parseLFMFenceCalls is the markdown json-fence half of parseLFMToolCalls;
// see there for the contract. A fence body counts as a leaked tool call
// only when it parses as a JSON object carrying a plausible tool-call
// shape — a "name"/"function" string plus "args"/"arguments"/"parameters"
// map, or (OpenAI function-call shape) a "function" object with name +
// arguments. Plain fenced code (shell snippets, JSON payloads without a
// call shape) is left untouched: the guarantee is that a fence is stripped
// ONLY when a call is minted from it, mirroring the marker/XML halves.
func parseLFMFenceCalls(content string) (string, []ToolCall) {
	matches := lfmJSONFence.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	var calls []ToolCall
	var remainder strings.Builder
	last := 0
	for _, m := range matches {
		body := content[m[2]:m[3]]
		mined := parseLFMFenceBody(body)
		if len(mined) == 0 {
			continue // not a tool-call fence; keep the text
		}
		remainder.WriteString(content[last:m[0]])
		// Re-mint with the running call index: two IDENTICAL fence bodies
		// in one reply must not share an ID (the executor keys result slots
		// by ToolCall.ID — duplicate IDs collapse pairing). Mirrors the
		// index-prefixed minting guarantee in lfmToolCallID.
		mined[0].ID = lfmToolCallID(len(calls), "fence:"+body)
		calls = append(calls, mined...)
		last = m[1]
	}
	remainder.WriteString(content[last:])
	if len(calls) > 0 {
		slog.Default().Warn("lfm tool-call recovered from markdown json-fence shape (model template bleed)",
			"calls", len(calls))
		return strings.TrimSpace(remainder.String()), calls
	}
	return content, nil
}

// parseLFMFenceBody mines ToolCalls out of one fence body. Accepted shapes:
//
//	{"name":"file_write","args":{...}}          (direct)
//	{"name":"file_write","arguments":{...}}     (direct, long key)
//	{"function":{"name":"file_write","arguments":"{\"path\":...}"}}  (OpenAI wire shape)
//
// arguments may be an object OR a JSON-encoded string (the OpenAI wire
// format stringifies it). Anything else returns nil so prose/code fences
// survive untouched.
func parseLFMFenceBody(body string) []ToolCall {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &obj); err != nil {
		return nil
	}

	name := ""
	var argsRaw json.RawMessage
	// OpenAI function-call shape: {"function":{"name":...,"arguments":...}}
	if fn, ok := obj["function"]; ok {
		var fnObj struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(fn, &fnObj); err == nil && fnObj.Name != "" {
			name = fnObj.Name
			argsRaw = fnObj.Arguments
		}
	}
	// Direct shape: {"name":...,"args"|"arguments"|"parameters":...}
	if name == "" {
		var nameStr string
		if raw, ok := obj["name"]; ok {
			if err := json.Unmarshal(raw, &nameStr); err != nil || nameStr == "" {
				return nil
			}
		} else if raw, ok := obj["tool"]; ok {
			if err := json.Unmarshal(raw, &nameStr); err != nil || nameStr == "" {
				return nil
			}
		} else {
			return nil
		}
		name = nameStr
		for _, key := range []string{"args", "arguments", "parameters"} {
			if raw, ok := obj[key]; ok {
				argsRaw = raw
				break
			}
		}
	}

	args := normalizeLFMFenceArgs(argsRaw)
	raw, _ := json.Marshal(args)
	return []ToolCall{{
		ID:   lfmToolCallID(0, "fence:"+body),
		Type: "function",
		Function: ToolCallFunction{
			Name:      name,
			Arguments: string(raw),
		},
	}}
}

// normalizeLFMFenceArgs coerces a fence call's arguments payload into a
// map[string]any. The payload may be a JSON object or a JSON-encoded
// STRING of an object (OpenAI wire format); absent/malformed degrades to
// an empty map — name+args-empty is strictly more recoverable than
// dropping the call, mirroring normalizeLFMXMLArgs.
func normalizeLFMFenceArgs(raw json.RawMessage) map[string]any {
	out := make(map[string]any)
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err == nil {
		return out
	}
	// Stringified-JSON shape: "{\"path\":\"hello.txt\"}"
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out
		}
	}
	return make(map[string]any)
}

// parseLFMMarkerCalls is the <|tool_call_start|>/<|tool_call_end|> half of
// parseLFMToolCalls; see there for the contract.
func parseLFMMarkerCalls(content string) (string, []ToolCall) {
	matches := lfmToolCallMarker.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	calls := make([]ToolCall, 0, len(matches))
	remainder := lfmToolCallMarker.ReplaceAllString(content, "")
	for i, m := range matches {
		body := strings.TrimSpace(m[1])
		_fc := lfmFuncCall.FindStringSubmatch(body)
		if _fc == nil {
			slog.Default().Warn("lfm tool-call marker body not in [fn(args)] form; dropping",
				"body", body)
			continue
		}
		name := _fc[1]
		args := parseLFMArgs(_fc[2])
		raw, _ := json.Marshal(args)
		calls = append(calls, ToolCall{
			ID:   lfmToolCallID(i, body),
			Type: "function",
			Function: ToolCallFunction{
				Name:      name,
				Arguments: string(raw),
			},
		})
	}
	return strings.TrimSpace(remainder), calls
}

// parseLFMXMLCalls is the <function_calls> half of parseLFMToolCalls; see
// there for the contract. Returns (content, nil) when no block is present.
func parseLFMXMLCalls(content string) (string, []ToolCall) {
	lower := strings.ToLower(content)
	// Unclosed block (stream cutoff): the closing tag never arrived, so the
	// regex can't see it — check this BEFORE the matches path, since an
	// unclosed block produces zero regex matches. Recover everything after
	// the last opening tag; parseLFMXMLCallObjects is tolerant enough to
	// mine calls out of the truncated body.
	if !strings.Contains(lower, "</function_calls>") &&
		strings.Contains(lower, "<function_calls>") {
		idx := strings.LastIndex(lower, "<function_calls>")
		unclosed := content[idx+len("<function_calls>"):]

		calls := parseLFMXMLCallObjects(unclosed)
		if len(calls) > 0 {
			slog.Default().Warn("lfm tool-call recovered from unclosed <function_calls> block (stream cutoff)",
				"calls", len(calls))
			return strings.TrimSpace(content[:idx]), calls
		}
		// Nothing recoverable — keep the raw text so callers can log it.
		return content, nil
	}

	matches := lfmFunctionCallsBlock.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	var calls []ToolCall
	var remainder strings.Builder
	last := 0
	for _, m := range matches {
		remainder.WriteString(content[last:m[0]])
		calls = append(calls, parseLFMXMLCallObjects(content[m[2]:m[3]])...)
		last = m[1]
	}
	remainder.WriteString(content[last:])
	if len(calls) > 0 {
		slog.Default().Warn("lfm tool-call recovered from <function_calls> XML shape (model template bleed)",
			"calls", len(calls))
	}
	return strings.TrimSpace(remainder.String()), calls
}

// parseLFMXMLCallObjects mines ToolCalls out of the body of a
// <function_calls> block. The body should be a JSON array of
// {id,name,arguments} objects, but quantized models mangle it: trailing
// commas, doubled bracket blocks, stray prose, missing ids. Every stage is
// tolerant — an unparsable call is skipped, never fatal, and the body is
// treated as a flat stream of objects so doubled/misnested brackets can't
// hide a recoverable call.
func parseLFMXMLCallObjects(body string) []ToolCall {
	var calls []ToolCall
	s := body
	for {
		start := strings.Index(s, "{")
		if start < 0 {
			break
		}
		end := findJSONObjectEnd(s[start:])
		if end < 0 {
			break // truncated tail object; nothing more to mine
		}
		obj := s[start : start+end+1]
		s = s[start+end+1:]

		var callObj struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(obj), &callObj); err != nil || callObj.Name == "" {
			continue // not a call object; skip
		}
		args := normalizeLFMXMLArgs(callObj.Arguments)
		raw, _ := json.Marshal(args)
		id := callObj.ID
		if strings.TrimSpace(id) == "" {
			id = lfmToolCallID(len(calls), "xml:"+obj)
		}
		calls = append(calls, ToolCall{
			ID:   id,
			Type: "function",
			Function: ToolCallFunction{
				Name:      callObj.Name,
				Arguments: string(raw),
			},
		})
	}
	return calls
}

// findJSONObjectEnd returns the index within s of the closing '}' that
// balances the opening '{' at s[0], accounting for JSON strings and
// backslash escapes inside them. Returns -1 if the object never closes
// (truncated input). Unlike a naive strings.Index("}"), this survives
// argument values containing braces — e.g. code content like "if (x) {y}".
func findJSONObjectEnd(s string) int {
	if len(s) == 0 || s[0] != '{' {
		return -1
	}
	depth := 0
	inStr := false
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inStr && ch == '\\':
			i++ // skip escaped byte inside string
		case inStr:
			if ch == quote {
				inStr = false
			}
		case ch == '"' || ch == '\'':
			inStr = true
			quote = ch
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// normalizeLFMXMLArgs coerces an XML-shape call's arguments into a
// map[string]any suitable for JSON encoding. The shape is {k:v,...} JSON;
// a malformed arguments payload (truncated or wrong type) deliberately
// degrades to an empty map rather than dropping the whole call —
// name+args-empty is strictly more recoverable than nothing.
func normalizeLFMXMLArgs(raw json.RawMessage) map[string]any {
	out := make(map[string]any)
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return make(map[string]any)
	}
	return out
}

// lfmToolCallID mints a deterministic, reply-unique tool-call ID for a
// recovered LFM marker call. The index prefix guarantees uniqueness within
// a reply even when two calls have identical bodies (or the hash degrades);
// the body hash keeps the ID stable across re-parses of the same content.
func lfmToolCallID(index int, body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("lfm-%d-%s", index, hex.EncodeToString(sum[:4]))
}

// parseLFMArgs parses LFM2.5's Python-style kwarg list: key="value", num=42,
// flag=true. Returns a map suitable for JSON encoding as tool arguments.
// Backslash escapes inside quoted values are honored both when splitting on
// commas (so \" does not end the string early) and when dequoting (so \n,
// \t, \\, \" and \' become their literal characters).
func parseLFMArgs(s string) map[string]any {
	out := make(map[string]any)
	if strings.TrimSpace(s) == "" {
		return out
	}
	// Split on commas not inside quotes. A backslash inside a quoted string
	// escapes the next byte — both are copied verbatim so an escaped quote
	// cannot terminate the string or hide a comma. The i+1 < len(s) bound
	// prevents advancing past end-of-input on a trailing backslash.
	var parts []string
	var cur strings.Builder
	inStr := false
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inStr && ch == '\\':
			cur.WriteByte(ch)
			if i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			}
		case inStr:
			cur.WriteByte(ch)
			if ch == quote {
				inStr = false
			}
		case ch == '"' || ch == '\'':
			inStr = true
			quote = ch
			cur.WriteByte(ch)
		case ch == ',':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, cur.String())
	}

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"':
			out[key] = unescapeLFMString(val[1 : len(val)-1])
		case len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			out[key] = unescapeLFMString(val[1 : len(val)-1])
		case val == "true":
			out[key] = true
		case val == "false":
			out[key] = false
		case val == "null":
			out[key] = nil
		default:
			var num float64
			if json.Unmarshal([]byte(val), &num) == nil {
				out[key] = num
			} else {
				out[key] = val
			}
		}
	}
	return out
}

// unescapeLFMString converts common backslash escape sequences in a
// dequoted LFM argument value to their literal characters. Unknown
// escapes are preserved verbatim (backslash + byte). A trailing lone
// backslash is kept as-is; the i+1 < len(s) bound prevents reading past
// end-of-input.
func unescapeLFMString(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case '\'':
			b.WriteByte('\'')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
