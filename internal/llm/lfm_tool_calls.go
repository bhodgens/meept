package llm

import (
	"encoding/json"
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
var lfmToolCallMarker = regexp.MustCompile(`<\|tool_call_start\|>(.*?)<\|tool_call_end\|>`)

// lfmFuncCall matches a single bracketed function call inside the markers.
var lfmFuncCall = regexp.MustCompile(`^\[([A-Za-z_][A-Za-z0-9_]*)\((.*)\)\]$`)

// parseLFMToolCalls scans content for LFM2.5 tool-call markers and converts
// each to a ToolCall. Markers are stripped from the returned content. When
// no markers are present the content is returned unchanged with nil calls.
func parseLFMToolCalls(content string) (string, []ToolCall) {
	matches := lfmToolCallMarker.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	calls := make([]ToolCall, 0, len(matches))
	remainder := lfmToolCallMarker.ReplaceAllString(content, "")
	for _, m := range matches {
		body := strings.TrimSpace(m[1])
		_fc := lfmFuncCall.FindStringSubmatch(body)
		if _fc == nil {
			continue
		}
		name := _fc[1]
		args := parseLFMArgs(_fc[2])
		raw, _ := json.Marshal(args)
		calls = append(calls, ToolCall{
			ID:   "",
			Type: "function",
			Function: ToolCallFunction{
				Name:      name,
				Arguments: string(raw),
			},
		})
	}
	return strings.TrimSpace(remainder), calls
}

// parseLFMArgs parses LFM2.5's Python-style kwarg list: key="value", num=42,
// flag=true. Returns a map suitable for JSON encoding as tool arguments.
func parseLFMArgs(s string) map[string]any {
	out := make(map[string]any)
	if strings.TrimSpace(s) == "" {
		return out
	}
	// Split on commas not inside quotes.
	var parts []string
	var cur strings.Builder
	inStr := false
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
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
		case len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"'):
			out[key] = val[1 : len(val)-1]
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
