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

// ---------------------------------------------------------------------------
// A6-12: the tool's Execute prefers the OPTIONAL ctx-aware seam. A runner
// that also implements ContactCtxChainRunner must be invoked through
// RunCtx with the CALLER's ctx (turn cancellation reaches the chain);
// a plain-only runner keeps the exact context-free Run call.
// ---------------------------------------------------------------------------

// fakeCtxChainRunner implements BOTH seams: it records which path
// Execute chose and whether the ctx was threaded (cancelled detection).
type fakeCtxChainRunner struct {
	fakeChainRunner
	gotCtxCancelled bool
	usedRunCtx      bool
	// cancelOnCall: the fake cancels the derived probe after seeing ctx
	probe func(ctx context.Context)
}

func (f *fakeCtxChainRunner) Run(person, message string, channels []string) string {
	f.usedRunCtx = false
	return f.fakeChainRunner.Run(person, message, channels)
}

func (f *fakeCtxChainRunner) RunCtx(ctx context.Context, person, message string, channels []string) string {
	f.usedRunCtx = true
	f.gotCtxCancelled = ctx.Err() != nil
	if f.probe != nil {
		f.probe(ctx)
	}
	return f.fakeChainRunner.Run(person, message, channels)
}

// TestContactToolPrefersCtxSeam pins the optional-capability dispatch:
// a runner implementing ContactCtxChainRunner is called via RunCtx with
// the Execute ctx (a cancelled Execute ctx reaches the runner as
// cancelled — the chain stops), and the plain seam is never used.
func TestContactToolPrefersCtxSeam(t *testing.T) {
	chain := &fakeCtxChainRunner{
		fakeChainRunner: fakeChainRunner{report: "telnyx_voice: no_answer"},
	}
	tool := NewContactTool(chain)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the turn was cancelled before the tool ran
	res, err := tool.Execute(ctx, map[string]any{"person": "c-ada", "message": "m"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, _ := res.(*tools.ToolResult)
	if tr == nil || !tr.Success {
		t.Fatalf("result = %+v, want success", res)
	}
	if !chain.usedRunCtx {
		t.Fatal("A6-12 regression: Execute used the plain Run seam despite a ctx-capable runner")
	}
	if !chain.gotCtxCancelled {
		t.Fatal("Execute did not thread the caller ctx into RunCtx")
	}
}

// TestContactToolCtxCapableButPlainOnlyFallback pins the other direction:
// a runner that ONLY implements ContactChainRunner keeps the old
// context-free call (byte-identical posture, no panic, no RunCtx).
func TestContactToolCtxCapableButPlainOnlyFallback(t *testing.T) {
	chain := &fakeChainRunner{report: "telnyx_voice: answered"}
	tool := NewContactTool(chain)
	if _, isCtx := any(chain).(ContactCtxChainRunner); isCtx {
		t.Fatal("test fixture must NOT implement the ctx seam (plain-runner case)")
	}
	res, err := tool.Execute(context.Background(), map[string]any{"person": "c-bob", "message": "m"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if chain.gotPerson != "c-bob" {
		t.Fatalf("plain seam not invoked: chain saw person %q", chain.gotPerson)
	}
	tr, _ := res.(*tools.ToolResult)
	if tr == nil || !tr.Success {
		t.Fatalf("result = %+v, want success via plain seam", res)
	}
}
