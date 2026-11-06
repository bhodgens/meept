package builtin

// voice_call tool tests (leaf 14 T5): argument validation, dial-seam
// invocation, fail-closed shapes, and the toolprov provider registration.
// The tool gate (BuiltinRules row + ToolActionMap) is pinned from the agent
// package side; here we pin the tool itself and the provider seam.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
)

// recordingDialer captures dial invocations.
type recordingDialer struct {
	mu     sync.Mutex
	calls  []dialCall
	result string
	err    error
}

type dialCall struct {
	employeeID string
	target     string
	requestID  string
}

func (d *recordingDialer) dial(ctx context.Context, employeeID, target, requestID string) (string, error) {
	d.mu.Lock()
	d.calls = append(d.calls, dialCall{employeeID, target, requestID})
	d.mu.Unlock()
	return d.result, d.err
}

func (d *recordingDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func constAgent(id string) AgentIDFunc {
	return func(context.Context) string { return id }
}

// TestVoiceCallToolDials pins the happy path: grant + valid args reach the
// injected dialer with the calling employee's identity.
func TestVoiceCallToolDials(t *testing.T) {
	d := &recordingDialer{result: "cc-123"}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-9"))

	res, err := tool.Execute(context.Background(), map[string]any{
		"target":     "+15551230001",
		"request_id": "req-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("result = %+v, want success", res)
	}
	if d.count() != 1 {
		t.Fatalf("dialer called %d times, want 1", d.count())
	}
	call := d.calls[0]
	if call.employeeID != "emp-9" || call.target != "+15551230001" || call.requestID != "req-1" {
		t.Errorf("dial args = %+v, want (emp-9, +15551230001, req-1)", call)
	}
}

// TestVoiceCallToolGeneratedRequestID: an omitted request_id gets a
// generated one (never an empty effects key component).
func TestVoiceCallToolGeneratedRequestID(t *testing.T) {
	d := &recordingDialer{result: "cc-1"}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-1"))

	if _, err := tool.Execute(context.Background(), map[string]any{"target": "+15551230002"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if d.calls[0].requestID == "" {
		t.Fatal("empty request_id reached the dialer")
	}
}

// TestVoiceCallToolRefusals pins the fail-closed shapes: missing target,
// missing dialer wiring, and missing employee identity all refuse without
// reaching the dialer.
func TestVoiceCallToolRefusals(t *testing.T) {
	d := &recordingDialer{}

	// Missing target.
	tool := NewVoiceCallTool(d.dial, constAgent("emp-1"))
	if res, _ := tool.Execute(context.Background(), map[string]any{}); res.(*tools.ToolResult).Success {
		t.Fatal("missing target succeeded")
	}
	if d.count() != 0 {
		t.Fatalf("dialer reached despite missing target")
	}

	// Unwired dialer.
	unwired := NewVoiceCallTool(nil, constAgent("emp-1"))
	if res, _ := unwired.Execute(context.Background(), map[string]any{"target": "+15551230003"}); res.(*tools.ToolResult).Success {
		t.Fatal("nil dialer succeeded")
	}

	// Missing employee identity (nil agentID func).
	noAgent := NewVoiceCallTool(d.dial, nil)
	if res, _ := noAgent.Execute(context.Background(), map[string]any{"target": "+15551230004"}); res.(*tools.ToolResult).Success {
		t.Fatal("missing employee identity succeeded")
	}
	if d.count() != 0 {
		t.Fatalf("dialer reached without employee identity")
	}
}

// TestVoiceCallToolDialErrorSurfaces: a dial refusal (allowlist, busy,
// duplicate) surfaces as a failed result carrying the typed error - never a
// Go error (the model reads the Error string).
func TestVoiceCallToolDialErrorSurfaces(t *testing.T) {
	sentinel := errors.New("voice: target number not in voice.allowed_numbers")
	d := &recordingDialer{err: sentinel}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-1"))

	res, err := tool.Execute(context.Background(), map[string]any{"target": "+15551230005"})
	if err != nil {
		t.Fatalf("Execute returned a Go error %v; refusals are ToolResults", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatal("refused dial reported success")
	}
	if tr.Err == nil || !errors.Is(tr.Err, sentinel) {
		t.Fatalf("typed error not carried: %+v", tr)
	}
}

// TestVoiceProviderRegistered pins the toolprov seam: the "voice" provider
// is registered at init, and Discover (after SetVoiceProvider) contributes
// exactly the voice_call tool under its canonical name.
func TestVoiceProviderRegistered(t *testing.T) {
	if _, ok := toolprov.Factory("voice"); !ok {
		t.Fatal("voice provider factory not registered")
	}

	// Unbound factory fails closed (nil Provider).
	if f, _ := toolprov.Factory("voice"); f != nil {
		if p := f(); p != nil {
			t.Fatal("unbound voice factory returned a Provider; must fail closed")
		}
	}

	// Bound factory constructs the provider and discovers the tool.
	SetVoiceProvider(
		func(context.Context, string, string, string) (string, error) { return "cc-x", nil },
		constAgent("emp-1"),
		nil,
	)
	f, _ := toolprov.Factory("voice")
	p := f()
	if p == nil {
		t.Fatal("bound voice factory returned nil Provider")
	}
	if p.Name() != "voice" {
		t.Errorf("provider name = %q, want voice", p.Name())
	}
	discovered := p.Discover()
	if len(discovered) != 1 {
		t.Fatalf("Discover returned %d tools, want 1 (voice_call)", len(discovered))
	}
	if discovered[0].Name() != "voice_call" {
		t.Errorf("discovered tool = %q, want voice_call", discovered[0].Name())
	}
	if p.PermissionClass() != toolprov.RiskMutating {
		t.Errorf("PermissionClass = %q, want mutating", p.PermissionClass())
	}
	if p.Broker() != nil {
		// nil broker is legal (no secret material); just exercising the
		// accessor for the coverage shape.
		t.Skip("broker non-nil; shape assertion only")
	}
}

// TestVoiceCallPermissionActionRow pins the security surface from here so a
// row removal fails a builtin-package test, not just the agent-package one.
// pkg/security (imported via internal/security engine re-export) exposes
// BuiltinRules through the engine seed; the production checker reads
// pkg/security.BuiltinRules directly, which the agent-package completeness
// test asserts. Here we assert the pkg package (importable without a cycle
// from builtin) still knows the action.
func TestVoiceCallPermissionActionRow(t *testing.T) {
	rule, ok := pkgsecurity.BuiltinRules["voice_call"]
	if !ok {
		t.Fatal("BuiltinRules has no voice_call row (unknown actions DENY by default: the row is mandatory)")
	}
	if rule.Action != "voice_call" {
		t.Errorf("rule action = %q, want voice_call", rule.Action)
	}
}
