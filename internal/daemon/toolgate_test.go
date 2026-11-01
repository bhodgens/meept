package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// gateStubTool is a minimal tools.Tool for daemon-gate tests.
type gateStubTool struct {
	name     string
	category string
	readOnly bool
	execErr  error
}

func (t *gateStubTool) Name() string        { return t.name }
func (t *gateStubTool) Description() string { return "stub " + t.name }
func (t *gateStubTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object"}
}
func (t *gateStubTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	if t.execErr != nil {
		return nil, t.execErr
	}
	return map[string]any{"ok": true}, nil
}
func (t *gateStubTool) IsReadOnly(map[string]any) bool        { return t.readOnly }
func (t *gateStubTool) IsConcurrencySafe(map[string]any) bool { return false }
func (t *gateStubTool) Category() string                      { return t.category }

// recordingEmitter records every audit chain emission.
type recordingEmitter struct {
	records []auditlog.Record
	err     error // when set, Emit fails
}

func (r *recordingEmitter) Emit(rec auditlog.Record) error {
	r.records = append(r.records, rec)
	return r.err
}

// recordingEngine substitutes the security engine tier; records Check calls.
// The gate reaches it through the engineSeam interface (see toolgate.go).
type recordingEngine struct {
	actions []string
	deny    bool
}

func (e *recordingEngine) Check(action, _ string, _ map[string]string, _ string) intsecurity.Decision {
	e.actions = append(e.actions, action)
	if e.deny {
		return intsecurity.Decision{Allowed: false, Reason: "engine deny"}
	}
	return intsecurity.Decision{Allowed: true, Reason: "Permitted", RiskLevel: intsecurity.RiskLow, RuleSource: "test"}
}

// TestExecGateAudit pins T2 (G2): AfterExecute emits one chain-shaped
// record per execution for success AND error outcomes, carrying tool name,
// args hash, outcome, and duration.
func TestExecGateAudit(t *testing.T) {
	emitter := &recordingEmitter{}
	gate := NewExecGate(GateConfig{Audit: emitter, Engine: &recordingEngine{}})

	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	reg.Register(&gateStubTool{name: "audit_ok", category: "custom"})
	reg.Register(&gateStubTool{name: "audit_err", category: "custom",
		execErr: errors.New("boom")})

	if _, err := reg.Execute(context.Background(), "audit_ok", map[string]any{}); err != nil {
		t.Fatalf("success-path Execute returned error: %v", err)
	}
	res, err := reg.Execute(context.Background(), "audit_err", map[string]any{})
	if err != nil {
		t.Fatalf("error-path Execute returned Go error: %v", err)
	}
	if res.Success {
		t.Fatal("error-path tool reported Success=true")
	}

	if len(emitter.records) != 2 {
		t.Fatalf("audit emissions = %d, want 2 (success + error); records=%+v",
			len(emitter.records), emitter.records)
	}
	for _, rec := range emitter.records {
		if rec.Type != "tool.execution" {
			t.Errorf("record type = %q, want tool.execution", rec.Type)
		}
		tool, _ := rec.Payload["tool"].(string)
		outcome, _ := rec.Payload["outcome"].(string)
		hash, _ := rec.Payload["args_hash"].(string)
		if tool == "" || outcome == "" || hash == "" {
			t.Errorf("record payload missing tool/outcome/args_hash: %+v", rec.Payload)
		}
		if _, ok := rec.Payload["duration_ms"]; !ok {
			t.Errorf("record payload missing duration_ms: %+v", rec.Payload)
		}
	}
	got := map[string]bool{}
	for _, rec := range emitter.records {
		outcome, _ := rec.Payload["outcome"].(string)
		got[outcome] = true
	}
	if !got["success"] || !got["error"] {
		t.Errorf("outcomes = %v, want {success,error}", got)
	}
}

// TestExecGateAuditFiresOnErrorResult pins the error-RESULT shape: a tool
// returning a failure envelope (not a Go error) still emits.
func TestExecGateAuditFiresOnErrorResult(t *testing.T) {
	emitter := &recordingEmitter{}
	gate := NewExecGate(GateConfig{Audit: emitter, Engine: &recordingEngine{}})
	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	reg.Register(&gateStubTool{name: "env_err", category: "custom"})

	// Force the error-result envelope through the tool's own error path.
	reg2 := tools.NewRegistry(nil)
	reg2.SetGate(gate)
	reg2.Register(&gateStubTool{name: "env_err", category: "custom",
		execErr: errors.New("envelope")})
	if _, err := reg2.Execute(context.Background(), "env_err", map[string]any{}); err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if len(emitter.records) != 1 {
		t.Fatalf("emissions = %d, want 1", len(emitter.records))
	}
	if outcome, _ := emitter.records[0].Payload["outcome"].(string); outcome != "error" {
		t.Errorf("outcome = %q, want error", outcome)
	}
}

// TestExecGateLedger pins T3 (G3): mutating executions reserve exactly one
// Claim+Complete pair; read-only treatment skips the ledger.
func TestExecGateLedger(t *testing.T) {
	ledger := effects.NewMemoryLedger()
	emitter := &recordingEmitter{}
	// Declared posture per tool: ro_tool read-only, mut_tool mutating.
	gate := NewExecGate(GateConfig{
		Audit:  emitter,
		Ledger: ledger,
		Engine: &recordingEngine{},
		PostureLookup: func(name string) (toolprov.RiskClass, bool) {
			if name == "ro_tool" {
				return toolprov.RiskReadOnly, true
			}
			return toolprov.RiskMutating, true
		},
	})

	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	reg.Register(&gateStubTool{name: "ro_tool", category: "custom", readOnly: true})
	reg.Register(&gateStubTool{name: "mut_tool", category: "custom", readOnly: false})

	if _, err := reg.Execute(context.Background(), "ro_tool", map[string]any{}); err != nil {
		t.Fatalf("read-only Execute returned error: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "mut_tool", map[string]any{}); err != nil {
		t.Fatalf("mutating Execute returned error: %v", err)
	}

	var pending int
	recs, err := ledger.ReconcilePending(context.Background())
	if err != nil {
		t.Fatalf("ReconcilePending: %v", err)
	}
	for _, rec := range recs {
		pending++
		t.Errorf("unexpected pending record after successful run: %+v (state=%s)", rec, rec.State)
	}
	_ = pending

	// The mutating run must have left its claim COMPLETED (claim -> receipt
	// -> complete); verify one record exists for it in the completed state.
	found := false
	for _, key := range []string{} {
		_ = key
	}
	rec, gerr := ledger.Get(context.Background(), effects.EffectKey("gate.tool", "mut_tool", argsHashForTest(map[string]any{})))
	if gerr != nil {
		t.Fatalf("mutating tool left no ledger record: %v", gerr)
	}
	if rec.State != effects.StateCompleted {
		t.Errorf("mutating record state = %s, want completed", rec.State)
	}
	found = true
	if !found {
		t.Fatal("unreachable")
	}

	if _, err := ledger.Get(context.Background(), effects.EffectKey("gate.tool", "ro_tool", argsHashForTest(map[string]any{}))); !errors.Is(err, effects.ErrUnknownKey) {
		t.Errorf("read-only tool reserved a ledger record (want none): rec=%+v err=%v", mustRecord(rec), err)
	}
}

func mustRecord(r any) any { return r }

// argsHashForTest mirrors gateArgsHash via the same json canonicalization.
func argsHashForTest(args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return "err"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestUndeclaredPostureMutating pins T4's daemon half (G4): a posture miss
// forces the mutating treatment (ledger reservation) even when the tool
// claims read-only. (The interface half - IsReadOnly reporting false to the
// gate - is pinned tools-side by TestUndeclaredPostureFailClosed.)
func TestUndeclaredPostureMutating(t *testing.T) {
	ledger := effects.NewMemoryLedger()
	// The custom category runs G1 through the (test) engine, which allows.
	// PostureLookup is nil: NOTHING is declared, so the read-only claim
	// must be overridden to mutating treatment (G4).
	eng := &recordingEngine{}
	gate := NewExecGate(GateConfig{
		Ledger: ledger,
		Audit:  &recordingEmitter{},
		Engine: nil,
	})
	gate.engineOverride(eng)

	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	reg.Register(&gateStubTool{name: "claim_ro", category: "custom", readOnly: true})

	if _, err := reg.Execute(context.Background(), "claim_ro", map[string]any{}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if _, err := ledger.Get(context.Background(), effects.EffectKey("gate.tool", "claim_ro", argsHashForTest(map[string]any{}))); err != nil {
		t.Fatalf("posture miss did not reserve the ledger (mutating treatment missing): %v", err)
	}
}

// TestExecGateFailClosed pins the fail-closed defaults: nil orchestrator
// blocks, a denying engine blocks, and a ledger Claim error blocks - each
// surfacing as a typed gate_blocked error result with the tool body never
// running.
func TestExecGateFailClosed(t *testing.T) {
	t.Run("nil orchestrator blocks shell", func(t *testing.T) {
		gate := NewExecGate(GateConfig{})
		reg := tools.NewRegistry(nil)
		reg.SetGate(gate)
		reg.Register(&gateStubTool{name: "sh", category: "shell"})
		res, err := reg.Execute(context.Background(), "sh", map[string]any{"command": "echo hi"})
		if err != nil {
			t.Fatalf("block must surface as a result, not a Go error: %v", err)
		}
		if res.Success || !strings.Contains(res.Error, "security scan blocked") {
			t.Errorf("want a gate_blocked failure naming the scan, got success=%v err=%q", res.Success, res.Error)
		}
	})

	t.Run("engine deny blocks custom category", func(t *testing.T) {
		gate := NewExecGate(GateConfig{Engine: &recordingEngine{deny: true},
			Audit: &recordingEmitter{}})
		reg := tools.NewRegistry(nil)
		reg.SetGate(gate)
		reg.Register(&gateStubTool{name: "custom_tool", category: "custom", readOnly: true})
		res, err := reg.Execute(context.Background(), "custom_tool", map[string]any{})
		if err != nil {
			t.Fatalf("block must surface as a result: %v", err)
		}
		if res.Success || !strings.Contains(res.Error, "engine deny") {
			t.Errorf("want the engine's reason in the result: %+v", res)
		}
	})

	t.Run("claim error blocks mutating", func(t *testing.T) {
		gate := NewExecGate(GateConfig{Ledger: failingLedger{},
			Engine: &recordingEngine{}})
		reg := tools.NewRegistry(nil)
		reg.SetGate(gate)
		reg.Register(&gateStubTool{name: "mut2", category: "custom", readOnly: false})
		res, err := reg.Execute(context.Background(), "mut2", map[string]any{})
		if err != nil {
			t.Fatalf("block must surface as a result: %v", err)
		}
		if res.Success || !strings.Contains(res.Error, "effects reservation blocked") {
			t.Errorf("want effects reservation block, got %+v", res)
		}
	})
}

// failingLedger always errors on Claim.
type failingLedger struct{}

func (failingLedger) Claim(context.Context, string, effects.EffectMeta) (bool, *effects.EffectRecord, error) {
	return false, nil, errors.New("ledger disk failure")
}
func (failingLedger) RecordReceipt(context.Context, string, json.RawMessage) error { return nil }
func (failingLedger) Complete(context.Context, string) error                       { return nil }
func (failingLedger) Abandon(context.Context, string, string) error                { return nil }
func (failingLedger) ReconcilePending(context.Context) ([]effects.EffectRecord, error) {
	return nil, nil
}
func (failingLedger) Get(context.Context, string) (*effects.EffectRecord, error) {
	return nil, effects.ErrUnknownKey
}
func (failingLedger) Close() error { return nil }
