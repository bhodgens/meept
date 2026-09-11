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
	content, markerCalls := parseLFMMarkerCalls(content)
	content, xmlCalls := parseLFMXMLCalls(content)
	content, fenceCalls := parseLFMFenceCalls(content)
	return content, append(append(markerCalls, xmlCalls...), fenceCalls...)
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
