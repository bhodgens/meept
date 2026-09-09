package llm

import (
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

// newIdentityTestResolver builds a resolver for the success-identity tests:
// the "coder" alias (zai/glm-4.7 + ollama/llama3.2) with quota blocking
// enabled, so both the alias-level fields and the block maps are exercised.
func newIdentityTestResolver() *Resolver {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(createTestConfig(), logger)
	r.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}
	return r
}

// coderAliases returns the two member models of the "coder" alias —
// modelA (zai/glm-4.7) and modelB (ollama/llama3.2) — resolved the same way
// NewResolver builds alias members.
func coderAliases(r *Resolver) (modelA, modelB *ModelConfig) {
	alias := r.aliases["coder"]
	return alias.Models[0], alias.Models[1]
}

// TestRecordAliasSuccessModel_StragglerDoesNotClearOtherModelBlock is the
// regression for bughunt 2026-09-08 item 14: a success on model B must NOT
// clear the cooldown / failure streak that model A's failure earned.
func TestRecordAliasSuccessModel_StragglerDoesNotClearOtherModelBlock(t *testing.T) {
	r := newIdentityTestResolver()
	modelA, modelB := coderAliases(r)

	// Model A fails: streak=1, cooldown armed.
	r.RecordAliasFailure("coder", errors.New("boom"), modelA)
	_, fails, cooldown, _ := r.GetAliasHealth("coder")
	if fails != 1 {
		t.Fatalf("precondition: consecutive_fails = %d, want 1", fails)
	}
	if cooldown.IsZero() {
		t.Fatal("precondition: cooldown not armed after model A failure")
	}

	// Straggler success on model B: must NOT clear A's earned state.
	r.RecordAliasSuccessModel("coder", modelB)
	_, fails, cooldown, _ = r.GetAliasHealth("coder")
	if fails != 1 {
		t.Errorf("after model B success, consecutive_fails = %d, want 1 (B success must not clear A's streak)", fails)
	}
	if cooldown.IsZero() {
		t.Error("after model B success, cooldown was cleared; want A's cooldown retained")
	}
	if r.health["coder"].FailedProviderID != modelA.ProviderID || r.health["coder"].FailedModelID != modelA.ModelID {
		t.Errorf("failed identity = %s/%s, want %s/%s",
			r.health["coder"].FailedProviderID, r.health["coder"].FailedModelID,
			modelA.ProviderID, modelA.ModelID)
	}

	// A success from the FAILING model itself still clears (control).
	r.RecordAliasSuccessModel("coder", modelA)
	_, fails, cooldown, _ = r.GetAliasHealth("coder")
	if fails != 0 {
		t.Errorf("after model A success, consecutive_fails = %d, want 0", fails)
	}
	if !cooldown.IsZero() {
		t.Errorf("after model A success, cooldown = %v, want zero", cooldown)
	}
}

// TestRecordAliasSuccessModel_AliasTimeoutBlockIdentityGated verifies the
// armed alias-level explicit-timeout block (tree 02 leaf 04) is released
// only by a success from the model whose consistent failures armed it —
// the alias-wide clear in RecordAliasSuccess must still do so (legacy
// callers), but the identity-attributed form must not clear it on a
// straggler success from the OTHER model.
func TestRecordAliasSuccessModel_AliasTimeoutBlockIdentityGated(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// The endpoint test config's "soft" alias has an explicit timeout: 60s,
	// max_fails 2 — and exactly ONE model, so arm the block via the
	// alias-wide failure identity, then send a straggler success from a
	// DIFFERENT identity (constructed ad hoc, not an alias member, to
	// model a misattributed success without changing the alias).
	r := NewResolver(endpointTestConfig(), logger)
	soft := r.aliases["soft"]
	gptA := soft.Models[0]

	// Two consistent same-member failures arm the 60s alias block.
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	if r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Fatal("precondition: alias timeout block not armed")
	}

	// Straggler success attributed to a DIFFERENT model: block must stay.
	straggler := &ModelConfig{ProviderID: "other", ModelID: "model-b", ConfiguredTimeout: true}
	r.RecordAliasSuccessModel("soft", straggler)
	if r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Error("straggler success cleared the alias timeout block; want it retained")
	}
	if r.health["soft"].TimeoutStreak != 2 {
		t.Errorf("TimeoutStreak = %d, want 2 (straggler must not reset the streak)", r.health["soft"].TimeoutStreak)
	}

	// Success from the model that armed it: block released.
	r.RecordAliasSuccessModel("soft", gptA)
	if !r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Error("own-model success did not release the alias timeout block")
	}
	if r.health["soft"].TimeoutStreak != 0 || r.health["soft"].TimeoutBlocks != 0 {
		t.Errorf("own-model success left streak=%d blocks=%d, want 0/0",
			r.health["soft"].TimeoutStreak, r.health["soft"].TimeoutBlocks)
	}
}

// TestRecordAliasSuccess_AliasWideStillClearsTimeoutBlock pins the legacy
// behavior kept for identity-less callers (loop.go stream path): the
// alias-wide RecordAliasSuccess still resets the streak and releases the
// armed alias timeout block.
func TestRecordAliasSuccess_AliasWideStillClearsTimeoutBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(endpointTestConfig(), logger)
	gptA := r.aliases["soft"].Models[0]

	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	if r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Fatal("precondition: alias timeout block not armed")
	}

	// Identity-less success: legacy alias-wide clear.
	r.RecordAliasSuccess("soft")
	if !r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Error("alias-wide RecordAliasSuccess no longer releases the armed timeout block; legacy identity-less callers rely on this")
	}
	if r.health["soft"].TimeoutStreak != 0 {
		t.Errorf("alias-wide success left TimeoutStreak=%d, want 0", r.health["soft"].TimeoutStreak)
	}
}

// TestRecordAliasSuccessModel_NilModelDegradesToAliasWide covers the
// nil-model contract: unresolvable serving identity keeps the legacy
// alias-wide clear (documented escape hatch for identity-less callers).
func TestRecordAliasSuccessModel_NilModelDegradesToAliasWide(t *testing.T) {
	r := newIdentityTestResolver()
	modelA, _ := coderAliases(r)

	r.RecordAliasFailure("coder", errors.New("boom"), modelA)
	r.RecordAliasSuccessModel("coder", nil)

	_, fails, cooldown, _ := r.GetAliasHealth("coder")
	if fails != 0 {
		t.Errorf("nil-model success left consecutive_fails=%d, want 0 (alias-wide clear)", fails)
	}
	if !cooldown.IsZero() {
		t.Errorf("nil-model success left cooldown=%v, want zero", cooldown)
	}
}

// TestRecordAliasSuccessModel_NoKnownFailureStillSweepsExpiredBlocks pins
// the sweep part of the contract: with no failure recorded (identity gate
// vacuously passes) and quota blocks present, a success must still lazily
// delete EXPIRED blocks and keep UNEXPIRED ones — expiry+success remains
// the only path that removes a block (AGENTS.md quota-resilience).
func TestRecordAliasSuccessModel_NoKnownFailureStillSweepsExpiredBlocks(t *testing.T) {
	r := newIdentityTestResolver()
	modelA, modelB := coderAliases(r)

	r.BlockQuotaEntry("coder", modelA.ProviderID, modelA.ModelID, time.Now().Add(-time.Minute)) // expired
	r.BlockQuotaEntry("coder", modelB.ProviderID, modelB.ModelID, time.Now().Add(time.Hour))    // unexpired
	r.BlockQuotaCredential("coder", "zai:key:expired", time.Now().Add(-time.Minute))            // expired
	r.BlockQuotaCredential("coder", "zai:key:live", time.Now().Add(time.Hour))                  // unexpired

	// Success from model B (identity with no recorded failure — gate passes).
	r.RecordAliasSuccessModel("coder", modelB)

	blocks := r.ActiveQuotaBlocks()
	if len(blocks) != 2 {
		t.Fatalf("expected exactly the 2 unexpired blocks to survive, got %d: %+v", len(blocks), blocks)
	}
	for _, b := range blocks {
		if b.CredentialKey == "zai|"+modelA.ProviderID+"|"+modelA.ModelID {
			t.Error("expired entry block was not swept")
		}
	}
}

// TestRecordAliasSuccessModel_QuotaLazyClearStillExpiryGated verifies that
// even a matching-identity success cannot lift an UNEXPIRED quota block:
// lazy clearing requires expiry AND success (AGENTS.md invariant), so a
// model that just succeeded but whose block window has not elapsed stays
// blocked until the window passes.
func TestRecordAliasSuccessModel_QuotaLazyClearStillExpiryGated(t *testing.T) {
	r := newIdentityTestResolver()
	modelA, _ := coderAliases(r)
	until := time.Now().Add(time.Hour)
	r.BlockQuotaEntry("coder", modelA.ProviderID, modelA.ModelID, until)

	// Matching-identity success while the block is live: block stays.
	r.RecordAliasSuccessModel("coder", modelA)
	blocks := r.ActiveQuotaBlocks()
	if len(blocks) != 1 {
		t.Fatalf("unexpired block must survive a success; got %d blocks: %+v", len(blocks), blocks)
	}
	if got := blocks[0].ResetAt; !got.Equal(until) {
		t.Errorf("block ResetAt = %v, want the original %v (block must not be truncated)", got, until)
	}

	// After expiry, the next success sweeps it.
	r.BlockQuotaEntry("coder", modelA.ProviderID, modelA.ModelID, time.Now().Add(-time.Second))
	r.RecordAliasSuccessModel("coder", modelA)
	if blocks := r.ActiveQuotaBlocks(); len(blocks) != 0 {
		t.Errorf("expired block must be swept by a success; got %d blocks", len(blocks))
	}
}

// TestRecordAliasSuccessModel_NonMemberIdentityDoesNotClear pins that the
// identity comparison is on the recorded FAILED model, not alias membership:
// any success attributed to an identity other than the failing model's
// leaves the earned cooldown in place.
func TestRecordAliasSuccessModel_NonMemberIdentityDoesNotClear(t *testing.T) {
	r := newIdentityTestResolver()
	modelA, _ := coderAliases(r)

	r.RecordAliasFailure("coder", errors.New("boom"), modelA)
	other := &ModelConfig{ProviderID: "xai", ModelID: "grok"}
	r.RecordAliasSuccessModel("coder", other)

	_, fails, cooldown, _ := r.GetAliasHealth("coder")
	if fails != 1 || cooldown.IsZero() {
		t.Errorf("non-member success cleared state: fails=%d cooldown=%v; want fails=1, cooldown armed", fails, cooldown)
	}
}

// TestResolver_RotateToNextModel_ConcurrentStress is the -race stress test
// for the shared per-alias rotation cursor (bughunt 2026-09-08 item 14,
// DEBT 1): N goroutines concurrently rotate and resolve the same alias.
// Correctness bar is the documented one — no panic, no data race, and the
// cursor is ALWAYS a valid index into the alias (interleaving order is
// deliberately not asserted: the cursor is per-alias by design).
func TestResolver_RotateToNextModel_ConcurrentStress(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(createTestConfig(), logger)

	const (
		workers  = 8
		perWork  = 200
		aliasLen = 2 // "coder" alias: zai/glm-4.7 + ollama/llama3.2
	)

	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := range workers {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := range perWork {
				var mc *ModelConfig
				var err error
				if (seed+j)%2 == 0 {
					mc, err = r.RotateToNextModel("coder")
				} else {
					mc, err = r.ResolveForAlias("coder", "")
				}
				if err != nil {
					errs <- err
					return
				}
				if mc == nil {
					errs <- errors.New("RotateToNextModel returned nil model with nil error")
					return
				}
				// Cursor validity: the returned model must be a member of
				// the alias (any valid index) — read under r.mu to keep
				// the -race build honest about the invariant itself.
				r.mu.Lock()
				idx := r.health["coder"].CurrentIndex
				r.mu.Unlock()
				if idx < 0 || idx >= aliasLen {
					errs <- errors.New("cursor out of range")
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// Post-storm invariant: cursor still valid and resolves still succeed.
	r.mu.Lock()
	idx := r.health["coder"].CurrentIndex
	r.mu.Unlock()
	if idx < 0 || idx >= aliasLen {
		t.Fatalf("post-storm cursor = %d, want in [0,%d)", idx, aliasLen)
	}
	if _, err := r.ResolveForAlias("coder", ""); err != nil {
		t.Fatalf("post-storm resolve failed: %v", err)
	}
}
