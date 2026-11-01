//go:build e2e

// Package codemode is the hermetic RED gate suite for the codemode sandbox
// tool (docs/plans/token-efficiency/02-codemode/00T-gate.md, branch 02
// leaf 00T). It does NOT compile until the implementation lands: the suite
// consumes the frozen C3 Result type from internal/codemode, which the
// spike-gated leaves 01/02 create. That compile-RED is the pinned gate
// state — the orchestrator records it; it is not a defect to fix here.
//
// Runtime note: the spike verdict (docs/research/codemode-spike-report.md)
// pins goja as the sandbox runtime. These suite-level cases are
// runtime-agnostic per the leaf: they pin contract C3's Result JSON shape
// and the [agent.tools] codemode_enabled gate, never goja internals — so
// nothing in this suite references or embeds goja. Only the C3 Result
// contract type is consumed.
//
// Hermetic: the fake LLM records every outbound request body; the scripts
// are fixed inputs; no live model, no network, no user config.
//
// Coverage map (manifest scenarios):
//
//	codemode-01  gate default off: tool absent from definitions  — TestCodemodeDisabledByDefault
//	codemode-02  gate on: tool registered                         — TestCodemodeToolRegisteredWhenEnabled
//	codemode-03  `return 1+1;` -> Result.Value "2"               — TestCodemodeScriptReturnValue
//	codemode-04  infinite loop + timeout_s=1 -> Err, wall < 5s   — TestCodemodeTimeoutKills
//	codemode-05  nested tool calls stay out of LLM context       — TestCodemodeNestedCallsStayOutOfContext
package codemode

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	// internal/codemode does not exist yet: this import is the compile-RED
	// pin for the gate. It resolves once leaves 01/02 land the C3 package.
	codemode "github.com/caimlas/meept/internal/codemode"
)

// jsonQuote renders a Go string literal — a valid JSON string encoding for
// the fixed (ASCII, newline-free) script inputs of this suite.
func jsonQuote(s string) string { return strconv.Quote(s) }

// itoa renders the small timeout_s literals.
func itoa(n int) string { return strconv.Itoa(n) }

// c3Fields pins the C3 Result JSON field names VERBATIM (the frozen contract
// from docs/plans/token-efficiency/master.md): the codemode tool serializes
// the C3 Result to JSON, so the model-facing tool envelope must carry an
// object with exactly these keys. They are asserted by name here (in
// addition to decoding into codemode.Result) so the pin survives any
// nesting the executor adds around the result.
var c3Fields = []string{`"OK"`, `"Output"`, `"Value"`, `"Err"`}

func containsAll(haystack string, needles []string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}

// startCodemodeStack boots the scratch stack and pins the tooluse lane (the
// bundled coder specialist holds the tool grants; when the gate lands, it
// also holds codemode).
func startCodemodeStack(t *testing.T, codemodeEnabled bool) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"agent.tools.codemode_enabled": codemodeEnabled,
	}))
	s.Fake.SetClassifierOutput(`{"intent":"tooluse","confidence":0.95,"reasoning":"pinned tooluse"}`)
	s.RegisterProject(t, "e2e-project")
	return s
}

// codemodeTurn runs one scripted codemode call through a real chat turn and
// returns the joined text of every role=tool message the daemon sent back
// to the fake LLM (the observable C3 envelopes).
func codemodeTurn(t *testing.T, s *harness.Stack, session, script string, timeoutS int) string {
	t.Helper()
	sessionID := s.CreateSession(t, session, s.ProjectDir)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "codemode",
		Arguments: `{"script":` + jsonQuote(script) +
			`, "timeout_s": ` + itoa(timeoutS) + `}`,
	})
	s.ChatTurn(t, sessionID, "Create a codemode script run named "+session, 120*time.Second)
	return toolResultTexts(s.Fake)
}

// echoJSON is the fixed nested-call script: two tools.echo calls whose
// results must stay sandbox-internal (pi parity — nested tool calls never
// enter LLM context; only the one codemode Result does). The echo bridge
// tool is chosen by the implementation leaf; this case pins the nested-call
// surface on the codemode Result, not on the bridge.
const echoJSON = `async () => { await tools.echo("first"); await tools.echo("second"); return "nested-done"; }`

// terminalRequestMessages returns the messages array of the LAST recorded
// executor request (the one carrying the tools array — the terminal payload
// the LLM would see after the tool round-trip).
func terminalRequestMessages(t *testing.T, f *harness.FakeLLM) []map[string]any {
	t.Helper()
	var last []map[string]any
	for _, body := range f.Requests() {
		if _, ok := body["tools"].([]any); !ok {
			continue
		}
		msgs, _ := body["messages"].([]any)
		var m []map[string]any
		for _, raw := range msgs {
			if msg, ok := raw.(map[string]any); ok {
				m = append(m, msg)
			}
		}
		last = m
	}
	if last == nil {
		t.Fatalf("no executor request (tools array) recorded in the fake LLM traffic")
	}
	return last
}

// toolMessages returns the role=tool messages of a messages slice.
func toolMessages(msgs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "tool" {
			out = append(out, m)
		}
	}
	return out
}

// toolResultTexts collects the content of every role=tool message the
// daemon has ever sent back to the fake LLM — the tool envelopes the model
// actually saw (the C3 Result JSON for codemode calls).
func toolResultTexts(f *harness.FakeLLM) string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

// requestsDeclareTool reports whether ANY completion request the fake LLM
// received declared the given tool name in its tools array — the observable
// of tool-definition exposure (same probe the tools-mcp suite uses).
func requestsDeclareTool(f *harness.FakeLLM, tool string) bool {
	for _, body := range f.Requests() {
		raw, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, entry := range raw {
			fn, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n == tool {
				return true
			}
		}
	}
	return false
}

// isC3Object reports whether the raw JSON object carries every verbatim C3
// field key.
func isC3Object(raw string) bool { return containsAll(raw, c3Fields) }

// c3Result finds the first C3 Result object in the recorded tool text. It
// walks each recorded envelope, recursively descends into any JSON object it
// can parse, and returns the first object whose keys are exactly the C3
// fields, decoded into codemode.Result. The recursive walk is what keeps the
// assertion independent of how the executor nests the tool result (string,
// ToolResult envelope, ExecutionResult wrapper).
func c3Result(res string) (codemode.Result, bool) {
	var found codemode.Result
	foundIt := false
	for _, line := range strings.Split(res, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var root any
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			continue
		}
		if hit, ok := findC3(root); ok {
			b, _ := json.Marshal(hit)
			_ = json.Unmarshal(b, &found)
			foundIt = true
			break
		}
	}
	return found, foundIt
}

// findC3 recursively locates the first JSON object carrying every verbatim
// C3 field key and returns it as a raw object.
func findC3(node any) (map[string]any, bool) {
	switch v := node.(type) {
	case map[string]any:
		if isC3Object(verbatimKeys(v)) {
			return v, true
		}
		// Search this object's own values, then siblings.
		for _, child := range v {
			if hit, ok := findC3(child); ok {
				return hit, true
			}
		}
		return nil, false
	case []any:
		for _, child := range v {
			if hit, ok := findC3(child); ok {
				return hit, true
			}
		}
		return nil, false
	case string:
		// A nested JSON-encoded result (the executor stringifies some
		// results); re-parse and descend.
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
			return nil, false
		}
		var inner any
		if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
			return nil, false
		}
		return findC3(inner)
	default:
		return nil, false
	}
}

// verbatimKeys reports the JSON keys present on an object, rendered the way
// the C3 field check wants to see them ("OK", "Output", ...).
func verbatimKeys(obj map[string]any) string {
	var b strings.Builder
	for k := range obj {
		b.WriteString(`"` + k + `"`)
		b.WriteString(" ")
	}
	return b.String()
}

// codemode-01: [agent.tools] codemode_enabled is default false — the
// codemode tool must be absent from every LLM request's definitions array.
// One real turn under the default (flag off) proves the listing surface:
// no codemode entry anywhere the model could see.
func TestCodemodeDisabledByDefault(t *testing.T) {
	s := startCodemodeStack(t, false)
	codemodeTurn(t, s, "cm-default", `return "x";`, 5)
	if requestsDeclareTool(s.Fake, "codemode") {
		t.Fatalf("codemode present in LLM tool definitions with the config gate OFF (default must be absent)")
	}
}

// codemode-02: codemode_enabled=true registers the codemode tool — its
// definition appears in the executor requests the daemon sends the LLM.
func TestCodemodeToolRegisteredWhenEnabled(t *testing.T) {
	s := startCodemodeStack(t, true)
	codemodeTurn(t, s, "cm-enabled", `return "x";`, 5)
	if !requestsDeclareTool(s.Fake, "codemode") {
		t.Fatalf("codemode absent from LLM tool definitions with codemode_enabled=true")
	}
}

// codemode-03: `return 1+1;` serializes to C3 Result.Value "2" — the Result
// JSON (field names verbatim) is asserted from the recorded tool envelope.
func TestCodemodeScriptReturnValue(t *testing.T) {
	s := startCodemodeStack(t, true)
	res := codemodeTurn(t, s, "cm-return", `return 1+1;`, 5)

	c3, ok := c3Result(res)
	if !ok {
		t.Fatalf("no C3 Result JSON in the recorded tool results; results:\n%s", res)
	}
	if !c3.OK {
		t.Fatalf("C3 Result.OK = false for `return 1+1;`: %+v", c3)
	}
	if c3.Err != "" {
		t.Fatalf("C3 Result.Err = %q, want empty: %+v", c3.Err, c3)
	}
	if c3.Value != "2" {
		t.Fatalf("C3 Result.Value = %q, want \"2\" (JSON-serialized return): %+v", c3.Value, c3)
	}
}

// codemode-04: an infinite loop under timeout_s=1 is a hard kill — Err is
// set in the C3 Result and the whole turn wall time stays under 5s (the
// kill must not hang; the goja verdict measured ~2.002s for a 2s deadline,
// so a 1s deadline lands well inside the bound).
func TestCodemodeTimeoutKills(t *testing.T) {
	s := startCodemodeStack(t, true)
	sessionID := s.CreateSession(t, "cm-kill", s.ProjectDir)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "codemode",
		Arguments: `{"script":"for(;;){}", "timeout_s":1}`,
	})
	start := time.Now()
	s.ChatTurn(t, sessionID, "Create a codemode infinite loop run named cm-kill", 120*time.Second)
	elapsed := time.Since(start)

	res := toolResultTexts(s.Fake)
	c3, ok := c3Result(res)
	if !ok {
		t.Fatalf("no C3 Result JSON in the recorded tool results; results:\n%s", res)
	}
	if c3.OK {
		t.Fatalf("C3 Result.OK = true for a killed infinite loop: %+v", c3)
	}
	if c3.Err == "" {
		t.Fatalf("C3 Result.Err empty after the timeout kill: %+v", c3)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("turn took %s for a 1s timeout kill (hard kill must return promptly; bound 5s)", elapsed)
	}
}

// codemode-05: nested tool calls stay out of LLM context. The script calls
// tools.echo twice; the terminal executor request (real recorded payload,
// not logs) must contain exactly ONE role=tool message carrying the C3
// Result (the one codemode envelope) and ZERO tool messages for the nested
// echo calls — the pi-parity surface pin.
func TestCodemodeNestedCallsStayOutOfContext(t *testing.T) {
	s := startCodemodeStack(t, true)
	codemodeTurn(t, s, "cm-nested", echoJSON, 30)

	msgs := terminalRequestMessages(t, s.Fake)
	tools := toolMessages(msgs)
	codemodeResults, echoResults := 0, 0
	for _, m := range tools {
		content, _ := m["content"].(string)
		switch {
		case isC3Object(content):
			codemodeResults++
		case strings.Contains(content, "first") && strings.Contains(content, "second"):
			echoResults++
		}
	}
	if codemodeResults != 1 {
		t.Fatalf("terminal LLM request carries %d C3 codemode results, want exactly 1; tool messages:\n%+v",
			codemodeResults, tools)
	}
	if echoResults != 0 {
		t.Fatalf("nested echo tool results entered the LLM-visible context (%d messages) — only the one codemode Result may; tool messages:\n%+v",
			echoResults, tools)
	}
}
