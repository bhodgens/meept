//go:build e2e

// Suite effects-ledger: the external-effect idempotency ledger over a
// real sandbox sqlite effects.db — exactly-once across a process
// "restart" (close the ledger, reopen the same file) and the pinned
// state machine's illegal-transition refusals.
package effectsledger

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
)

// sandboxEffectsDBPath returns a per-test sandbox effects.db path
// (never ~/.meept).
func sandboxEffectsDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "effects.db")
}

// int32Var is a tiny mutable counter closure targets can bump (closures
// over plain ints don't compile as loop-carried counters here).
type int32Var struct{ n int }

// openLedger opens (and migrates) a SQLiteLedger at path.
func openLedger(t *testing.T, path string) *effects.SQLiteLedger {
	t.Helper()
	l, err := effects.NewSQLiteLedger(path, nil)
	if err != nil {
		t.Fatalf("open effects ledger %s: %v", path, err)
	}
	return l
}

// TestEffectsLedger_ExactlyOnceAcrossRestart covers effects-ledger-01:
// the same external effect key, claimed by one process generation,
// completed, and then claimed again by a FRESH ledger handle over the
// SAME sqlite file (the restart), executes exactly once — the second
// generation receives the stored receipt as an idempotent no-op.
func TestEffectsLedger_ExactlyOnceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := sandboxEffectsDBPath(t)
	key := effects.EffectKey("git_push", "session-e2e", "step-1", "remote/backup")

	// Generation 1: claim + execute + complete, then CLOSE (the restart).
	l1 := openLedger(t, dbPath)
	var executions int32Var
	receipt, reused, err := effects.Run(ctx, l1, key, effects.EffectMeta{
		Tool: "git_backup", SessionID: "session-e2e",
		Payload: json.RawMessage(`{"remote":"backup"}`),
	}, func(_ context.Context) (json.RawMessage, error) {
		executions.n++
		return json.Marshal(map[string]any{"pushed": true})
	})
	if err != nil {
		t.Fatalf("generation 1 run: %v", err)
	}
	if reused {
		t.Fatal("first generation must execute, not reuse")
	}
	if executions.n != 1 {
		t.Fatalf("generation 1 executions = %d, want 1", executions.n)
	}
	if receipt == nil {
		t.Fatal("generation 1 returned nil receipt")
	}
	if err := l1.Complete(ctx, key); err != nil {
		t.Fatalf("generation 1 complete: %v", err)
	}
	if err := l1.Close(); err != nil {
		t.Fatalf("generation 1 close: %v", err)
	}

	// Generation 2 (the restart): fresh handle, same file, same key.
	l2 := openLedger(t, dbPath)
	defer l2.Close()
	var exec2 int32Var
	receipt2, reused2, err := effects.Run(ctx, l2, key, effects.EffectMeta{
		Tool: "git_backup", SessionID: "session-e2e",
	}, func(_ context.Context) (json.RawMessage, error) {
		exec2.n++
		return json.Marshal(map[string]any{"pushed": true, "again": true})
	})
	if err != nil {
		t.Fatalf("generation 2 run: %v", err)
	}
	if !reused2 {
		t.Fatal("post-restart claim of a completed key must be an idempotent reuse")
	}
	if exec2.n != 0 {
		t.Fatalf("effect EXECUTED AGAIN after restart (executions=%d) — idempotency broken", exec2.n)
	}
	if string(receipt2) != string(receipt) {
		t.Fatalf("reused receipt = %s, want the stored %s", receipt2, receipt)
	}

	// The pending-reconcile view is empty: nothing stuck from either
	// generation.
	pending, err := l2.ReconcilePending(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending records after completed effect: %+v", pending)
	}
}

// TestEffectsLedger_StateMachineRejectsIllegalTransitions covers
// effects-ledger-02: completed cannot be abandoned; claimed cannot be
// re-receipted; unknown keys are distinct errors.
func TestEffectsLedger_StateMachineRejectsIllegalTransitions(t *testing.T) {
	ctx := context.Background()
	l := openLedger(t, sandboxEffectsDBPath(t))
	defer l.Close()

	// Complete-then-Abandon is the pinned illegal transition.
	key := effects.EffectKey("push", "s", "1")
	if _, _, err := l.Claim(ctx, key, effects.EffectMeta{Tool: "t"}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := l.RecordReceipt(ctx, key, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if err := l.Complete(ctx, key); err != nil {
		t.Fatalf("complete: %v", err)
	}
	err := l.Abandon(ctx, key, "operator changed their mind")
	if !errors.Is(err, effects.ErrInvalidTransition) {
		t.Fatalf("abandon of completed = %v, want ErrInvalidTransition", err)
	}

	// Idempotent re-complete of a completed key is legal.
	if err := l.Complete(ctx, key); err != nil {
		t.Fatalf("idempotent re-complete: %v", err)
	}

	// A receipted record cannot be receipted again.
	key2 := effects.EffectKey("push", "s", "2")
	if _, _, err := l.Claim(ctx, key2, effects.EffectMeta{Tool: "t"}); err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if err := l.RecordReceipt(ctx, key2, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("receipt 2: %v", err)
	}
	if err := l.RecordReceipt(ctx, key2, json.RawMessage(`{"ok":true,"again":true}`)); !errors.Is(err, effects.ErrInvalidTransition) {
		t.Fatalf("double receipt = %v, want ErrInvalidTransition", err)
	}

	// Abandoned is dead: nothing more may transition it.
	if err := l.Abandon(ctx, key2, "done"); err != nil {
		t.Fatalf("abandon receipted: %v", err)
	}
	if err := l.Complete(ctx, key2); !errors.Is(err, effects.ErrInvalidTransition) {
		t.Fatalf("complete abandoned = %v, want ErrInvalidTransition", err)
	}

	// Unknown key: distinct sentinel.
	if _, err := l.Get(ctx, effects.EffectKey("push", "s", "never")); !errors.Is(err, effects.ErrUnknownKey) {
		t.Fatalf("get unknown = %v, want ErrUnknownKey", err)
	}

	// ReconcilePending sees nothing: completed and abandoned are both
	// terminal.
	pending, err := l.ReconcilePending(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("terminal records leaked into pending: %+v", pending)
	}

	// A crash-window record (claimed, never completed) IS reconcilable.
	stuckKey := effects.EffectKey("push", "s", "crash")
	if _, _, err := l.Claim(ctx, stuckKey, effects.EffectMeta{Tool: "t"}); err != nil {
		t.Fatalf("claim stuck: %v", err)
	}
	pending, err = l.ReconcilePending(ctx)
	if err != nil {
		t.Fatalf("reconcile stuck: %v", err)
	}
	if len(pending) != 1 || pending[0].Key != stuckKey {
		t.Fatalf("reconcile must surface the claimed-but-incomplete record, got %+v", pending)
	}
	if time.Now().Before(pending[0].ClaimedAt) {
		t.Fatal("claimed_at must be stamped at claim time")
	}
}
