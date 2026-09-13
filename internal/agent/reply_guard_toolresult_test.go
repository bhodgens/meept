package agent

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// observedToolResultDump is the EXACT reply the daemon produced on
// 2026-09-13 (local-e2e-1): two concatenated memory_store result objects,
// forwarded to the user verbatim instead of an answer. The guard had no rule
// for this shape, so the user saw machine output.
const observedToolResultDump = `{
  "category": "",
  "memory_id": "65d6862a-04c3-4a76-9666-1eb573673ca9",
  "success": true,
  "type": "task"
}
{
  "category": "",
  "memory_id": "c6ebc9d7-aa0f-4016-9427-a5a53310c4fa",
  "success": true,
  "type": "episodic"
}`

// TestReplyGuard_ReplacesRawToolResultDump is the regression for the
// machine-shaped reply the guard missed.
func TestReplyGuard_ReplacesRawToolResultDump(t *testing.T) {
	out, match, replaced := replaceCatalogReply(observedToolResultDump)
	if !replaced {
		t.Fatal("a raw tool-result dump must be replaced")
	}
	if match.Rule != "tool_result_json" {
		t.Fatalf("rule = %q, want tool_result_json", match.Rule)
	}
	if !strings.Contains(strings.ToLower(out), "ask me to do something specific") {
		t.Fatalf("replacement is not the user-language fallback: %q", out)
	}
	if strings.Contains(out, "memory_id") {
		t.Fatal("the raw payload must not survive the replacement")
	}
}

// TestReplyGuard_PassesLegitimateJSONAnswers guards the other direction: the
// guard REPLACES what it matches, so a JSON answer a user asked for must pass.
func TestReplyGuard_PassesLegitimateJSONAnswers(t *testing.T) {
	cases := map[string]string{
		"fenced json":             "here is the record you asked for:\n```json\n{\"title\": \"Attention Is All You Need\", \"authors\": [\"Vaswani\"], \"year\": 2017}\n```",
		"json with prose":         `The extracted record is {"title": "Attention Is All You Need", "year": 2017}, which matches the paper.`,
		"plain answer":            "The paper is \"Attention Is All You Need\" by Vaswani et al., published in 2017.",
		"empty":                   "",
		"json without a tool key": "{\n  \"title\": \"Attention Is All You Need\",\n  \"year\": 2017\n}",
	}
	for name, in := range cases {
		if _, _, replaced := replaceCatalogReply(in); replaced {
			t.Errorf("%s: must NOT be replaced", name)
		}
	}
}

// TestReplyGuard_LogsToolResultRule checks the observability half: the WARN
// names the new rule, so a future misroute of this shape is diagnosable.
func TestReplyGuard_LogsToolResultRule(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	applyReplyGuardLogged(observedToolResultDump, logger, replyGuardContext{
		Agent:          "coder",
		Intent:         "tooluse",
		SessionID:      "session-abc",
		ConversationID: "conv-abc",
	})

	logged := buf.String()
	if !strings.Contains(logged, "rule=tool_result_json") {
		t.Fatalf("WARN must name the rule, got: %s", logged)
	}
	if !strings.Contains(logged, "memory_id") {
		t.Fatalf("WARN must name the matched key, got: %s", logged)
	}
}
