package builtin

// Gate tests for contract C6 (master.md, quoted verbatim): the
// employee-facing "contact" tool. Written BEFORE implementation
// (00T-gate-tests.md, T6). RED until leaf 02 lands contact.go.
//
// Pinned contract:
//   - Tool name "contact".
//   - Args: {person: string (contact id / phone / signal uuid),
//     message: string, channels: []string (optional override; empty =
//     record policy or employee default)}.
//   - Behavior: resolve record -> chain -> attempts -> ChainReport
//     rendered as per-channel outcome lines. Voice connect does NOT
//     block the tool result: "answered" is terminal for the chain.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// fakeChainRunner lives in contact_fake_test.go (shared seam fake).

// mustReport extracts the tool's string report (the repo convention:
// NewSuccessResult(string); file_edit_test.go pattern). Zero-value
// string on any other shape.
func mustReport(tr *tools.ToolResult) string {
	if tr == nil {
		return ""
	}
	s, _ := tr.Result.(string)
	return s
}

// TestContactToolName pins the registered tool name.
func TestContactToolName(t *testing.T) {
	tool := NewContactTool(nil)
	if tool.Name() != "contact" {
		t.Errorf("Name() = %q, want %q (C6)", tool.Name(), "contact")
	}
}

// TestContactToolArgsAndReport pins the arg shape and the report
// rendering: {person, message, channels?} reach the chain seam and the
// result renders per-channel lines with outcomes.
func TestContactToolArgsAndReport(t *testing.T) {
	chain := &fakeChainRunner{
		report: "telnyx_voice: no_answer\nsignal_voice: busy\nsignal_msg: delivered",
	}
	tool := NewContactTool(chain)

	res, err := tool.Execute(context.Background(), map[string]any{
		"person":   "+15550000023",
		"message":  "dentist reminder",
		"channels": []string{"telnyx_voice", "signal_voice", "signal_msg"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("result = %+v, want success ToolResult", res)
	}

	if chain.gotPerson != "+15550000023" {
		t.Errorf("chain saw person %q, want the arg verbatim (id / phone / signal uuid resolution is the seam's job)", chain.gotPerson)
	}
	if chain.gotMessage != "dentist reminder" {
		t.Errorf("chain saw message %q, want %q", chain.gotMessage, "dentist reminder")
	}
	if len(chain.gotChannels) != 3 || chain.gotChannels[0] != "telnyx_voice" {
		t.Errorf("chain saw channels %v, want the override list", chain.gotChannels)
	}

	report, _ := tr.Result.(string)
	for _, want := range []string{"telnyx_voice", "signal_voice", "signal_msg", "no_answer", "busy", "delivered"} {
		if !strings.Contains(report, want) {
			t.Errorf("report output missing %q:\n%s", want, report)
		}
	}
	// Per-channel LINES: each channel outcome is its own line.
	if lines := strings.Split(strings.TrimSpace(report), "\n"); len(lines) < 3 {
		t.Errorf("report rendered %d lines, want >= 3 per-channel outcome lines:\n%s", len(lines), report)
	}
}

// TestContactToolEmptyChannelsOmitted pins the C6 rule: an omitted or
// empty channels list means "record policy or employee default" - the
// chain seam must receive NO override.
func TestContactToolEmptyChannelsOmitted(t *testing.T) {
	chain := &fakeChainRunner{report: "telnyx_voice: answered"}
	tool := NewContactTool(chain)

	if _, err := tool.Execute(context.Background(), map[string]any{
		"person":  "c-ada",
		"message": "m",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(chain.gotChannels) != 0 {
		t.Errorf("chain saw override %v with no channels arg, want none (empty = record policy)", chain.gotChannels)
	}
}

// TestContactToolAnsweredTerminal pins the voice-connect rule: an
// "answered" outcome is terminal for the chain and the tool result
// surfaces it WITHOUT blocking on the live call.
func TestContactToolAnsweredTerminal(t *testing.T) {
	chain := &fakeChainRunner{report: "telnyx_voice: answered (call continues on its own session)"}
	tool := NewContactTool(chain)

	res, err := tool.Execute(context.Background(), map[string]any{
		"person":  "c-bob",
		"message": "m",
	})
	if err != nil {
		t.Fatalf("Execute: %v (answered must not block the tool result, C6)", err)
	}
	tr, _ := res.(*tools.ToolResult)
	if tr == nil || !tr.Success || !strings.Contains(mustReport(tr), "answered") {
		t.Fatalf("result = %+v, want success output containing the answered outcome", res)
	}
}

// TestContactToolMissingPersonFails pins arg validation: no person arg is
// a failure result (never a chain run against an empty resolver key).
func TestContactToolMissingPersonFails(t *testing.T) {
	chain := &fakeChainRunner{}
	tool := NewContactTool(chain)

	res, err := tool.Execute(context.Background(), map[string]any{"message": "m"})
	if err != nil {
		t.Fatalf("Execute returned Go error %v, want failure ToolResult", err)
	}
	if tr, ok := res.(*tools.ToolResult); ok && tr.Success {
		t.Errorf("missing person arg succeeded; want failure result")
	}
	if chain.gotPerson != "" {
		t.Errorf("chain invoked with person %q despite missing arg", chain.gotPerson)
	}
}
