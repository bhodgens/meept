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

// parseLFMToolCalls scans content for LFM2.5 tool-call markers and converts
// each to a ToolCall. Markers are stripped from the returned content. When
// no markers are present the content is returned unchanged with nil calls.
//
// Each recovered call is minted a unique deterministic ID ("lfm-<i>-<hash>"):
// mlx_lm returns no IDs, but the executor keys result slots by ToolCall.ID
// and the wire marshaller omits tool_call_id when empty — empty IDs on a
// multi-call reply collapse the executor's idToIdx map and break pairing.
func parseLFMToolCalls(content string) (string, []ToolCall) {
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
