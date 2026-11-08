package builtin

// voice_call tool tests (leaf 14 T5): argument validation, dial-seam
// invocation, fail-closed shapes, and the toolprov provider registration.
// The tool gate (BuiltinRules row + ToolActionMap) is pinned from the agent
// package side; here we pin the tool itself and the provider seam.

import (
	"context"
	"errors"
	"strings"
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

// ---- local-leg leaf 09 (tool leg) ----

// localLegRecorder captures local-leg invocations.
type localLegRecorder struct {
	mu     sync.Mutex
	calls  []localCall
	callID string
	sessID string
	err    error
}

type localCall struct {
	employeeID string
	label      string
	ttsVoice   string
}

func (l *localLegRecorder) open(ctx context.Context, employeeID, label, ttsVoice string) (string, string, error) {
	l.mu.Lock()
	l.calls = append(l.calls, localCall{employeeID, label, ttsVoice})
	l.mu.Unlock()
	return l.callID, l.sessID, l.err
}

func (l *localLegRecorder) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// TestVoiceCallSchemaLegAndVoice (leaf 09 T1): the schema exposes the
// optional leg + voice args, and target is NO LONGER schema-Required (the
// per-leg rule: phone needs it, local does not - the tool validates).
func TestVoiceCallSchemaLegAndVoice(t *testing.T) {
	tool := NewVoiceCallTool(nil, nil)
	params := tool.Parameters()

	legProp, ok := params.Properties["leg"]
	if !ok {
		t.Fatal("schema has no leg property")
	}
	if legProp.Type != "string" {
		t.Errorf("leg prop type = %q, want string", legProp.Type)
	}
	voiceProp, ok := params.Properties["voice"]
	if !ok {
		t.Fatal("schema has no voice property")
	}
	if voiceProp.Type != "string" {
		t.Errorf("voice prop type = %q, want string", voiceProp.Type)
	}
	for _, name := range params.Required {
		if name == "target" {
			t.Fatal("target is schema-Required; the per-leg target rule (phone yes, local no) is the tool's job")
		}
	}
}

// TestVoiceCallInvalidLegRefused (leaf 09 T1): a leg value other than
// phone|local refuses before any seam is reached.
func TestVoiceCallInvalidLegRefused(t *testing.T) {
	d := &recordingDialer{}
	l := &localLegRecorder{}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-1"))
	tool.SetLocalFunc(l.open)

	res, err := tool.Execute(context.Background(), map[string]any{
		"leg":    "satellite",
		"target": "+155****0001",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.(*tools.ToolResult).Success {
		t.Fatal("invalid leg succeeded")
	}
	if d.count() != 0 || l.count() != 0 {
		t.Fatalf("seams reached despite invalid leg (dial=%d local=%d)", d.count(), l.count())
	}
}

// TestVoiceCallPhoneDefaultUnchanged (leaf 09 T1): leg omitted with a
// phone default routes to the dialer exactly as before - same args, same
// result shape, request_id idempotency intact.
func TestVoiceCallPhoneDefaultUnchanged(t *testing.T) {
	d := &recordingDialer{result: "cc-loc-1"}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-7"))
	tool.SetLocalFunc((&localLegRecorder{}).open)
	tool.SetLegDefault(func(context.Context, string) string { return "phone" })

	res, err := tool.Execute(context.Background(), map[string]any{
		"target":     "+155****0009",
		"request_id": "req-9",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("phone default refused: %+v", tr)
	}
	if d.count() != 1 {
		t.Fatalf("dialer called %d times, want 1", d.count())
	}
	call := d.calls[0]
	if call.employeeID != "emp-7" || call.target != "+155****0009" || call.requestID != "req-9" {
		t.Errorf("dial args = %+v, want (emp-7, +155****0009, req-9)", call)
	}
}

// TestVoiceCallPhoneTargetStillRequired (leaf 09 T1): with the phone leg
// resolved, a missing target still refuses and never reaches the dialer.
func TestVoiceCallPhoneTargetStillRequired(t *testing.T) {
	d := &recordingDialer{}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-1"))
	res, _ := tool.Execute(context.Background(), map[string]any{"leg": "phone"})
	if res.(*tools.ToolResult).Success {
		t.Fatal("phone leg without target succeeded")
	}
	if d.count() != 0 {
		t.Fatal("dialer reached without target")
	}
}

// TestVoiceCallLocal (leaf 09 T2): leg=local routes to the local seam with
// the employee identity, the optional target as the label, and the voice
// override; the result carries the call + session ids. Target is NOT
// required on the local leg (empty label = the leg's own default).
func TestVoiceCallLocal(t *testing.T) {
	l := &localLegRecorder{callID: "local-abc", sessID: "sess-1"}
	tool := NewVoiceCallTool(nil, constAgent("emp-5"))
	tool.SetLocalFunc(l.open)

	res, err := tool.Execute(context.Background(), map[string]any{
		"leg":   "local",
		"voice": "piper:norman",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("local call refused: %+v", tr)
	}
	if l.count() != 1 {
		t.Fatalf("local seam called %d times, want 1", l.count())
	}
	got := l.calls[0]
	if got.employeeID != "emp-5" || got.label != "" || got.ttsVoice != "piper:norman" {
		t.Errorf("local args = %+v, want (emp-5, \"\", piper:norman)", got)
	}
	out, ok := tr.Result.(VoiceCallResult)
	if !ok {
		t.Fatalf("result data = %T, want VoiceCallResult", tr.Result)
	}
	if out.CallControlID != "local-abc" || out.SessionID != "sess-1" {
		t.Errorf("result ids = (%q, %q), want (local-abc, sess-1)", out.CallControlID, out.SessionID)
	}
}

// TestVoiceCallLocalExplicitLabel: a local target arg rides through as the
// session label (verbatim - the tool adds no prefix).
func TestVoiceCallLocalExplicitLabel(t *testing.T) {
	l := &localLegRecorder{callID: "local-1", sessID: "s"}
	tool := NewVoiceCallTool(nil, constAgent("emp-2"))
	tool.SetLocalFunc(l.open)

	if _, err := tool.Execute(context.Background(), map[string]any{
		"leg":    "local",
		"target": "desk-mac",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if l.calls[0].label != "desk-mac" {
		t.Errorf("label = %q, want desk-mac", l.calls[0].label)
	}
}

// TestVoiceCallLocalDisabled (leaf 09 T2): nil local seam fails closed
// with the named disabled error - never a dial, never a panic.
func TestVoiceCallLocalDisabled(t *testing.T) {
	d := &recordingDialer{}
	tool := NewVoiceCallTool(d.dial, constAgent("emp-3"))

	res, err := tool.Execute(context.Background(), map[string]any{"leg": "local"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatal("local leg succeeded while unwired")
	}
	if tr.Err == nil || !strings.Contains(tr.Err.Error(), "voice local leg disabled") {
		t.Errorf("disabled error = %v, want it to name the disabled leg", tr.Err)
	}
	if d.count() != 0 {
		t.Fatal("dialer reached on a local call")
	}
}

// TestVoiceCallLocalEmployeeRequired: the local call is attributable like
// the phone one - no employee identity, no open.
func TestVoiceCallLocalEmployeeRequired(t *testing.T) {
	l := &localLegRecorder{}
	tool := NewVoiceCallTool(nil, nil)
	tool.SetLocalFunc(l.open)

	res, _ := tool.Execute(context.Background(), map[string]any{"leg": "local"})
	if res.(*tools.ToolResult).Success {
		t.Fatal("local call succeeded without employee identity")
	}
	if l.count() != 0 {
		t.Fatal("local seam reached without employee identity")
	}
}

// TestVoiceCallLocalErrorSurfaces: a leg refusal (busy, duplicate target,
// disabled inside the wiring) surfaces as a failed ToolResult carrying the
// typed error - the model reads the Error string.
func TestVoiceCallLocalErrorSurfaces(t *testing.T) {
	sentinel := errors.New("voice: registry busy: 1/1 slots live")
	l := &localLegRecorder{err: sentinel}
	tool := NewVoiceCallTool(nil, constAgent("emp-4"))
	tool.SetLocalFunc(l.open)

	res, err := tool.Execute(context.Background(), map[string]any{"leg": "local"})
	if err != nil {
		t.Fatalf("Execute returned a Go error %v; refusals are ToolResults", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatal("refused local call reported success")
	}
	if tr.Err == nil || !errors.Is(tr.Err, sentinel) {
		t.Fatalf("typed error not carried: %+v", tr)
	}
}

// TestVoiceCallLegDefaultSeam (CL4 precedence): with the leg arg omitted,
// the injected employee/default-leg seam decides - a "local" default
// routes local without an arg, an empty resolution falls back to phone.
func TestVoiceCallLegDefaultSeam(t *testing.T) {
	l := &localLegRecorder{callID: "local-d", sessID: "s"}
	d := &recordingDialer{result: "cc-d"}

	localDefault := NewVoiceCallTool(d.dial, constAgent("emp-8"))
	localDefault.SetLocalFunc(l.open)
	localDefault.SetLegDefault(func(_ context.Context, emp string) string {
		if emp != "emp-8" {
			t.Errorf("legDefault employee = %q, want emp-8", emp)
		}
		return "local"
	})
	if res, err := localDefault.Execute(context.Background(), map[string]any{}); err != nil || !res.(*tools.ToolResult).Success {
		t.Fatalf("local default refused: %v %+v", err, res)
	}
	if l.count() != 1 || d.count() != 0 {
		t.Fatalf("local default routed wrong (local=%d dial=%d)", l.count(), d.count())
	}

	phoneDefault := NewVoiceCallTool(d.dial, constAgent("emp-8"))
	phoneDefault.SetLocalFunc(l.open)
	phoneDefault.SetLegDefault(func(context.Context, string) string { return "" })
	if res, err := phoneDefault.Execute(context.Background(), map[string]any{"target": "+155****0010"}); err != nil || !res.(*tools.ToolResult).Success {
		t.Fatalf("phone fallback refused: %v %+v", err, res)
	}
	if d.count() != 1 {
		t.Fatalf("empty default did not fall back to phone (dial=%d)", d.count())
	}
}

// TestVoiceProviderLocalSeam (leaf 09 T4): SetVoiceLocalFunc binds the
// local seam onto the provider without disturbing the phone wiring, and
// nil is ignored (repo Set* nil-guard rule).
func TestVoiceProviderLocalSeam(t *testing.T) {
	SetVoiceProvider(
		func(context.Context, string, string, string) (string, error) { return "cc-p", nil },
		constAgent("emp-1"),
		nil,
	)
	SetVoiceLocalFunc(nil) // must be a no-op, not a clobber
	SetVoiceLocalFunc(func(context.Context, string, string, string) (string, string, error) {
		return "local-x", "sess-x", nil
	})

	f, _ := toolprov.Factory("voice")
	p := f()
	if p == nil {
		t.Fatal("bound voice factory returned nil Provider")
	}
	discovered := p.Discover()
	if len(discovered) != 1 || discovered[0].Name() != "voice_call" {
		t.Fatalf("discovered = %+v, want the voice_call tool", discovered)
	}
	vc, ok := discovered[0].(*VoiceCallTool)
	if !ok {
		t.Fatalf("discovered tool = %T, want *VoiceCallTool", discovered[0])
	}
	res, err := vc.Execute(context.Background(), map[string]any{"leg": "local"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("provider-wired local seam refused: %+v", tr)
	}
	out := tr.Result.(VoiceCallResult)
	if out.CallControlID != "local-x" || out.SessionID != "sess-x" {
		t.Errorf("ids = (%q, %q), want (local-x, sess-x)", out.CallControlID, out.SessionID)
	}

	// Phone branch keeps working through the same provider instance.
	res, err = vc.Execute(context.Background(), map[string]any{"target": "+155****0011"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.(*tools.ToolResult).Success {
		t.Fatalf("phone branch broken after local wiring: %+v", res)
	}

	// Leave the package-level factory clean for the other tests.
	SetVoiceLocalFunc(nil)
	SetVoiceProvider(nil, nil, nil)
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
