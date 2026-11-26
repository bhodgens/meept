package daemon

// toolgate_key_test.go — bughunt H5/A1-5 pin (owner ruling D2): the G3
// effects key must carry the claimant's SESSION id and AGENT id, so two
// concurrent loops dedupe on their own ledger rows. Pre-fix the key was
// (gate.tool, name, argsHash): session B running the same (tool, args) as
// session A collided on one row — false claimant-mismatch Warns, a second
// concurrent execution while A's claim was in flight, and stuck claimed
// rows from a crashed claimant poisoning the other session forever.
//
// Non-vacuity: every assertion below flips against the pre-fix key.
// Two-session separation fails (one row, B's success logged a mismatch);
// the same-session retry collapse fails (two rows).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

func gateCtx(sessionID, agentID string) context.Context {
	ctx := tools.ContextWithSessionID(context.Background(), sessionID)
	return tools.ContextWithAgentID(ctx, agentID)
}

func mutatingPosture(string) (toolprov.RiskClass, bool) {
	return toolprov.RiskMutating, true
}

// TestEffectsGateKeyCarriesSessionAndAgent pins the pure key function:
// identical (tool, args) in two sessions => two different keys; identical
// (tool, args) on a retry in the SAME session => one key; the same agent
// id across sessions still separates (session half), and the same session
// id under two agent ids separates too (agent half, ruling D2).
func TestEffectsGateKeyCarriesSessionAndAgent(t *testing.T) {
	args := map[string]any{"path": "/x/y.txt", "content": "z"}
	kA := effectsGateKey(gateCtx("session-A", "coder"), "file_write", args)
	kA2 := effectsGateKey(gateCtx("session-A", "coder"), "file_write", args)
	kB := effectsGateKey(gateCtx("session-B", "coder"), "file_write", args)
	kB2 := effectsGateKey(gateCtx("session-B", "coder"), "file_write", args)
	kAAg := effectsGateKey(gateCtx("session-A", "analyst"), "file_write", args)

	if kA != kA2 {
		t.Fatalf("same-session retry must collapse on one key: %s vs %s", kA, kA2)
	}
	if kB != kB2 {
		t.Fatalf("same-session retry (B) must collapse on one key")
	}
	if kA == kB {
		t.Fatal("H5/A1-5 regression: two sessions collide on one effects key")
	}
	if kA == kAAg {
		t.Fatal("same session under different agents must not collide (ruling D2: both ids in the key)")
	}
	// Determinism across restarts: content-derived, no clocks or counters.
	if again := effectsGateKey(gateCtx("session-A", "coder"), "file_write", args); again != kA {
		t.Fatal("key must be pure/content-derived")
	}
	// Different args still differ inside one session.
	if effectsGateKey(gateCtx("session-A", "coder"), "file_write", map[string]any{"path": "/other"}) == kA {
		t.Fatal("args no longer part of the key")
	}
	// Unattributed legacy context keys deterministically on (tool,args)
	// alone and stays distinct from attributed keys.
	kLegacy := effectsGateKey(context.Background(), "file_write", args)
	if kLegacy != effectsGateKey(context.Background(), "file_write", args) {
		t.Fatal("legacy key must be deterministic")
	}
	if kLegacy == kA {
		t.Fatal("unattributed legacy key must not collide with an attributed one")
	}
}

// TestExecGateLedgerTwoSessionsIsolated is the behavior pin through the
// REAL registry + memory ledger: session A's completed (tool,args) must
// NOT idempotent-suppress or claimant-mismatch session B's identical call;
// each lane keeps its own row; and A's retry collapses onto A's row.
// Pre-fix: A completes the shared row, B's Claim sees a completed prior
// (treated as an idempotent no-op allow), B's Complete hits a
// claimant-mismatch Warn, and a crashed A strand blocks B forever.
func TestExecGateLedgerTwoSessionsIsolated(t *testing.T) {
	ledger := effects.NewMemoryLedger()
	gate := NewExecGate(GateConfig{
		Audit:         &recordingEmitter{},
		Ledger:        ledger,
		Engine:        &recordingEngine{},
		PostureLookup: mutatingPosture,
	})
	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	reg.Register(&gateStubTool{name: "mut_tool", category: "custom"})

	ctxA := gateCtx("sess-A", "coder")
	ctxB := gateCtx("sess-B", "coder")

	if _, err := reg.Execute(ctxA, "mut_tool", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("A Execute: %v", err)
	}
	if _, err := reg.Execute(ctxB, "mut_tool", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("B Execute: %v", err)
	}
	// Same-session retry runs AFTER its own completion: still allowed
	// (the idempotent no-op path stays intact), and lands on ONE row.
	if _, err := reg.Execute(ctxA, "mut_tool", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("A retry Execute: %v", err)
	}

	keyA := effectsGateKey(ctxA, "mut_tool", map[string]any{"k": "v"})
	keyB := effectsGateKey(ctxB, "mut_tool", map[string]any{"k": "v"})
	if keyA == keyB {
		t.Fatal("ledger keys collided across sessions")
	}
	recA, err := ledger.Get(context.Background(), keyA)
	if err != nil {
		t.Fatalf("A row missing: %v", err)
	}
	if recA.State != effects.StateCompleted {
		t.Errorf("A row state = %s, want completed", recA.State)
	}
	if recA.ClaimantSessionID != "sess-A" || recA.ClaimantAgentID != "coder" {
		t.Errorf("A row claimant = %+v, want sess-A/coder", recA)
	}
	recB, err := ledger.Get(context.Background(), keyB)
	if err != nil {
		t.Fatalf("B row missing (B collided onto A's row): %v", err)
	}
	if recB.State != effects.StateCompleted {
		t.Errorf("B row state = %s, want completed (false claimant-mismatch class)", recB.State)
	}

	// The crash case: A abandons mid-flight; B's identical call must not
	// inherit the stuck row. Build fresh state for determinism.
	ledger2 := effects.NewMemoryLedger()
	gate2 := NewExecGate(GateConfig{
		Audit:         &recordingEmitter{},
		Ledger:        ledger2,
		Engine:        &recordingEngine{},
		PostureLookup: mutatingPosture,
	})
	reg2 := tools.NewRegistry(nil)
	reg2.SetGate(gate2)
	failing := &gateStubTool{name: "mut_tool", category: "custom", execErr: errors.New("boom")}
	reg2.Register(failing)

	if _, err := reg2.Execute(gateCtx("sess-A", "coder"), "mut_tool", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("A failing Execute: %v", err)
	}
	// A's row is abandoned (not completed) — and it is A's row only.
	recA2, err := ledger2.Get(context.Background(), effectsGateKey(gateCtx("sess-A", "coder"), "mut_tool", map[string]any{"k": "v"}))
	if err != nil {
		t.Fatalf("A2 row missing: %v", err)
	}
	if recA2.State == effects.StateCompleted {
		t.Errorf("failed execution left a completed row: %+v", recA2)
	}
	if _, err := ledger2.Get(context.Background(), effectsGateKey(gateCtx("sess-B", "coder"), "mut_tool", map[string]any{"k": "v"})); !errors.Is(err, effects.ErrUnknownKey) {
		t.Errorf("B's key touched A's crashed row: err=%v", err)
	}
}

// TestExecGateEmptyClaimantStillBlocks pins the unchanged fail-closed
// path from the ruling: an unattributed (empty session AND empty agent)
// mutating call must still error at Claim and BLOCK the tool body — the
// key-scope fix must not quietly wave legacy unattributed callers
// through. (Before the fix this same shape was the ONLY shape that
// reached Claim with a zero claimant; keep it byte-identical.)
func TestExecGateEmptyClaimantStillBlocks(t *testing.T) {
	ledger := effects.NewMemoryLedger()
	gate := NewExecGate(GateConfig{
		Audit:         &recordingEmitter{},
		Ledger:        ledger,
		Engine:        &recordingEngine{},
		PostureLookup: mutatingPosture,
	})
	reg := tools.NewRegistry(nil)
	reg.SetGate(gate)
	ran := false
	reg.Register(&spyTool{name: "mut_tool", category: "custom", ran: &ran})

	_, err := reg.Execute(context.Background(), "mut_tool", map[string]any{"k": "v"})
	// The registry surfaces a blocked execution as an error RESULT; the
	// claim error must have produced a block, not a pass.
	if ran {
		t.Fatal("empty-claimant execution ran the tool body — fail-open regression")
	}
	_ = err
}

// spyTool records whether its body executed.
type spyTool struct {
	name     string
	category string
	ran      *bool
}

func (t *spyTool) Name() string        { return t.name }
func (t *spyTool) Description() string { return "spy" }
func (t *spyTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object"}
}
func (t *spyTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	*t.ran = true
	return map[string]any{"ok": true}, nil
}
func (t *spyTool) IsReadOnly(map[string]any) bool        { return false }
func (t *spyTool) IsConcurrencySafe(map[string]any) bool { return false }
func (t *spyTool) TimeoutMs() time.Duration              { return 0 }
func (t *spyTool) Category() string                      { return t.category }
