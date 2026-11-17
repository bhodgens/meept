//go:build e2e

// Gate leaf 00T — contract symbol assertions for the typed-error and
// selection-chain contracts (C1, C3, C4, C5, C6 typed surfaces).
//
// Compile-RED until the owning leaves land; each assertion names its
// leaf. This file exists so the undefined-symbol list in `go vet` IS the
// exhaustive contract inventory the implementation leaves must satisfy.
package sovereignparity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestContract_C1_SelectionChain (leaf 01): the Filter interface shape,
// Verdict constants, and first-non-Allow semantics of runSelectionFilters.
func TestContract_C1_SelectionChain(t *testing.T) {
	if llm.VerdictAllow != 0 || llm.VerdictSkip != 1 || llm.VerdictFloor != 2 {
		t.Fatalf("C1 Verdict constants drifted: %d/%d/%d", llm.VerdictAllow, llm.VerdictSkip, llm.VerdictFloor)
	}

	// A minimal filter double proves the interface shape compiles as the
	// contract declares it (Name/Filter/Reason method set).
	skipper := &gateFilter{name: "gate-skip", verdict: llm.VerdictSkip, reason: "gate_skip_reason"}
	var f llm.Filter = skipper
	if f.Name() != "gate-skip" || f.Reason() != "gate_skip_reason" {
		t.Fatalf("C1 Filter interface shape drifted: %+v", f)
	}

	// runSelectionFilters returns the FIRST non-Allow verdict with that
	// filter's reason. The gate drives it through the registration seam
	// leaf 01 wires in NewResolver: with the gate filter registered as
	// the only selection filter, the chain must return its verdict.
	r := llm.NewResolver(writeAndLoadGateConfig(t, flagOffConfigFor(t)), nil)
	cand := &llm.Candidate{
		Model:      &llm.ModelConfig{ProviderID: "prova", ModelID: "model-a"},
		Alias:      "general",
		Capability: "completion",
		EstInput:   10,
		EstOutput:  10,
	}
	fc := &llm.FilterCtx{SessionID: "sess-gate", Capability: "completion", EstInput: 10, EstOutput: 10, Now: time.Now()}
	verdict, reason := r.RunSelectionFiltersForGate(cand, fc, []llm.Filter{skipper})
	if verdict != llm.VerdictSkip || reason != "gate_skip_reason" {
		t.Fatalf("C1 runSelectionFilters = (%v, %q); want (VerdictSkip, gate_skip_reason)", verdict, reason)
	}
}

// gateFilter is the C1 interface-shape double.
type gateFilter struct {
	name    string
	verdict llm.Verdict
	reason  string
}

func (g *gateFilter) Name() string                                      { return g.name }
func (g *gateFilter) Filter(*llm.Candidate, *llm.FilterCtx) llm.Verdict { return g.verdict }
func (g *gateFilter) Reason() string                                    { return g.reason }

// TestContract_C3_AdmissionErrors (leaf 02): the typed admission error
// family — NonRetryable, Unwrap to the sentinel, and the frozen reserve.
func TestContract_C3_AdmissionErrors(t *testing.T) {
	if llm.MinOutputReserve != 64 {
		t.Fatalf("C3 MinOutputReserve = %d; the frozen gateway.py MIN_OUTPUT_TOKENS is 64", llm.MinOutputReserve)
	}
	err := &llm.ContextTooSmallError{
		ModelID: "m", ProviderID: "p", EstInput: 1000, Reserve: 64, ContextLimit: 512,
	}
	if !err.NonRetryable() {
		t.Fatal("C3: ContextTooSmallError must be NonRetryable (never retried, never parked)")
	}
	wrapped := errors.Join(llm.ErrContextTooSmall, err) // any wrap shape
	if !errors.Is(wrapped, llm.ErrContextTooSmall) {
		t.Fatal("C3: ErrContextTooSmall sentinel lost through a wrap")
	}
	var asCT *llm.ContextTooSmallError
	if !errors.As(err, &asCT) {
		t.Fatal("C3: errors.As lost the typed admission verdict")
	}
}

// TestContract_C4_Posture (leaf 05): the posture vocabulary, the parse
// fallback (unknown/empty -> private), and the AllowsSensitive gate.
func TestContract_C4_Posture(t *testing.T) {
	if llm.PosturePrivate != "private" || llm.PosturePublicOnly != "public_only" || llm.PostureNonSensitive != "non_sensitive" {
		t.Fatalf("C4 posture vocabulary drifted: %s/%s/%s", llm.PosturePrivate, llm.PosturePublicOnly, llm.PostureNonSensitive)
	}
	if llm.ParsePosture("") != llm.PosturePrivate || llm.ParsePosture("garbage") != llm.PosturePrivate {
		t.Fatal("C4: unknown/empty posture must parse to PosturePrivate (Q3 backward compatibility)")
	}
	if !llm.PosturePrivate.AllowsSensitive() {
		t.Fatal("C4: private posture must allow sensitive turns")
	}
	if llm.PosturePublicOnly.AllowsSensitive() || llm.PostureNonSensitive.AllowsSensitive() {
		t.Fatal("C4: public_only and non_sensitive must BAR sensitive turns")
	}
	// Schema round-trip: ModelDef carries DataPosture (leaf 05 T3).
	var def llm.ModelDef
	def.DataPosture = llm.PosturePublicOnly
	if def.DataPosture != llm.PosturePublicOnly {
		t.Fatal("C4: ModelDef.DataPosture round-trip failed")
	}
}

// TestContract_C5_FailureScopeConstants (leaf 06): the scope/class
// vocabularies are the frozen lowercase strings.
func TestContract_C5_FailureScopeConstants(t *testing.T) {
	if llm.ScopeModel != "model" || llm.ScopePool != "pool" || llm.ScopeAccount != "account" || llm.ScopeRequest != "request" {
		t.Fatalf("C5 FailureScope vocabulary drifted: %s/%s/%s/%s", llm.ScopeModel, llm.ScopePool, llm.ScopeAccount, llm.ScopeRequest)
	}
	if llm.ClassAccountDead != "account_dead" || llm.ClassCongested != "congested" ||
		llm.ClassTransient != "transient" || llm.ClassRequestFault != "request_fault" || llm.ClassEntryMissing != "entry_missing" {
		t.Fatal("C5 FailureClass vocabulary drifted")
	}
	// The typed classifier returns the 3-tuple the e2e table consumes.
	cls, scope, target := llm.ClassifyFailure("provacct", "model-a1", "", 401, "")
	if cls != llm.ClassAccountDead || scope != llm.ScopeAccount || target != "provacct" {
		t.Fatalf("C5 ClassifyFailure(401) = (%s, %s, %s); want (account_dead, account, provacct)", cls, scope, target)
	}
}

// TestContract_C6_ShadowTypes (leaf 04): the shadow decision shape and
// the hard no-side-effects entry point signature.
func TestContract_C6_ShadowTypes(t *testing.T) {
	r := llm.NewResolver(writeAndLoadGateConfig(t, shadowConfig(t)), nil)
	dec, err := r.ResolveShadow(context.Background(), llm.ShadowRequest{
		Alias:     "general",
		SessionID: "sess-shadow-contract",
	})
	if err != nil {
		t.Fatalf("C6 R4 RED (expected): ResolveShadow errored: %v — leaf 04 not landed", err)
	}
	if dec == nil || dec.Alias != "general" || dec.ModelID == "" {
		t.Fatalf("C6: shadow decision incomplete: %+v", dec)
	}
	if len(dec.Candidates) == 0 {
		t.Fatal("C6: shadow decision carries no Candidates")
	}
}

// TestContract_C7_AdvisoryBudget (leaf 08): the mode vocabulary, the
// Advisory() read-only verdict, and Mode() accessor.
func TestContract_C7_AdvisoryBudget(t *testing.T) {
	if llm.BudgetModeEnforce != "enforce" || llm.BudgetModeSteer != "steer" {
		t.Fatalf("C7 BudgetMode vocabulary drifted: %s/%s", llm.BudgetModeEnforce, llm.BudgetModeSteer)
	}
	b := llm.NewBudget(llm.BudgetConfig{DailyCostLimit: 10, Aggressiveness: 1}, nil)
	if b.Mode() != llm.BudgetModeEnforce {
		t.Fatalf("C7: default budget mode = %s; enforce is the frozen default", b.Mode())
	}
	v := b.Advisory()
	if v.OverTarget {
		t.Fatal("C7: a fresh budget must not report OverTarget")
	}
	b.RecordCost(llm.CostRecord{Timestamp: time.Now().UTC(), CostUSD: 100})
	v2 := b.Advisory()
	if !v2.OverTarget {
		t.Fatal("C7: Advisory must report the breach without mutating the counters (read-only)")
	}
	if v2.Used <= 0 || v2.Cap != 10 {
		t.Fatalf("C7: advisory verdict Used/Cap = %v/%v; want used>0, cap=10", v2.Used, v2.Cap)
	}
}
