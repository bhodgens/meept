package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardTestWriter{}, nil))
}

type discardTestWriter struct{}

func (discardTestWriter) Write(p []byte) (int, error) { return len(p), nil }

// newTestLedgers returns one constructor per Ledger implementation. The
// conformance suite runs identically over both stores.
func newTestLedgers(t *testing.T) map[string]func() Ledger {
	t.Helper()
	dir := t.TempDir()
	return map[string]func() Ledger{
		"memory": func() Ledger { return NewMemoryLedger() },
		"sqlite": func() Ledger {
			l, err := NewSQLiteLedger(filepath.Join(dir, "effects.db"), testLogger())
			if err != nil {
				t.Fatalf("open sqlite ledger: %v", err)
			}
			return l
		},
	}
}

func metaFor(tool string) EffectMeta {
	return EffectMeta{
		TaskID:    "task-1",
		StepID:    "step-2",
		SessionID: "sess-3",
		Tool:      tool,
	}
}

func testWho() Claimant {
	return Claimant{AgentID: "agent-1", SessionID: "sess-3"}
}

func TestLedgerConformance(t *testing.T) {
	for name, newLedger := range newTestLedgers(t) {
		t.Run(name, func(t *testing.T) {
			l := newLedger()
			defer func() {
				if err := l.Close(); err != nil {
					t.Errorf("close ledger: %v", err)
				}
			}()

			t.Run("claim grants once", func(t *testing.T) {
				key := EffectKey("tool.a", "sess", "step")
				granted, prior, err := l.Claim(context.Background(), key, metaFor("tool.a"), testWho())
				if err != nil {
					t.Fatalf("claim: %v", err)
				}
				if !granted || prior != nil {
					t.Fatalf("first claim: granted=%v prior=%v, want true nil", granted, prior)
				}
				granted, prior, err = l.Claim(context.Background(), key, metaFor("tool.a"), testWho())
				if err != nil {
					t.Fatalf("second claim: %v", err)
				}
				if granted {
					t.Fatal("second claim granted=true, want false")
				}
				if prior == nil {
					t.Fatal("second claim prior=nil, want the existing record")
				}
				if prior.State != StateClaimed {
					t.Fatalf("prior state = %q, want %q", prior.State, StateClaimed)
				}
				if prior.Tool != "tool.a" {
					t.Fatalf("prior tool = %q, want tool.a", prior.Tool)
				}
				if prior.ClaimedAt.IsZero() {
					t.Fatal("prior ClaimedAt is zero")
				}
			})

			t.Run("receipt transition", func(t *testing.T) {
				key := EffectKey("tool.b", "sess", "step")
				claimMeta := metaFor("tool.b")
				claimMeta.Payload = json.RawMessage(`{"k":"v"}`)
				if _, _, err := l.Claim(context.Background(), key, claimMeta, testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				receipt := json.RawMessage(`{"status":"ok","id":42}`)
				if err := l.RecordReceipt(context.Background(), key, testWho(), receipt); err != nil {
					t.Fatalf("record receipt: %v", err)
				}
				rec, err := l.Get(context.Background(), key)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if rec.State != StateReceipted {
					t.Fatalf("state = %q, want %q", rec.State, StateReceipted)
				}
				if rec.ExecutedAt == nil {
					t.Fatal("ExecutedAt not stamped")
				}
				if !bytesEqualJSON(rec.Receipt, receipt) {
					t.Fatalf("receipt round-trip mismatch: got %s want %s", rec.Receipt, receipt)
				}
				if !bytesEqualJSON(rec.Payload, json.RawMessage(`{"k":"v"}`)) {
					t.Fatalf("payload mismatch: %s", rec.Payload)
				}
			})

			t.Run("complete from receipted", func(t *testing.T) {
				key := EffectKey("tool.c", "sess", "step")
				if _, _, err := l.Claim(context.Background(), key, metaFor("tool.c"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.RecordReceipt(context.Background(), key, testWho(), json.RawMessage(`{"done":true}`)); err != nil {
					t.Fatalf("record receipt: %v", err)
				}
				if err := l.Complete(context.Background(), key, testWho()); err != nil {
					t.Fatalf("complete: %v", err)
				}
				rec, err := l.Get(context.Background(), key)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if rec.State != StateCompleted {
					t.Fatalf("state = %q, want %q", rec.State, StateCompleted)
				}
				if rec.CompletedAt == nil {
					t.Fatal("CompletedAt not stamped")
				}
			})

			t.Run("complete from claimed is allowed", func(t *testing.T) {
				key := EffectKey("tool.d", "sess", "step")
				if _, _, err := l.Claim(context.Background(), key, metaFor("tool.d"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.Complete(context.Background(), key, testWho()); err != nil {
					t.Fatalf("complete from claimed: %v", err)
				}
				rec, err := l.Get(context.Background(), key)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if rec.State != StateCompleted {
					t.Fatalf("state = %q, want %q", rec.State, StateCompleted)
				}
			})

			t.Run("abandon from claimed and receipted", func(t *testing.T) {
				keyC := EffectKey("tool.e", "sess", "claimed")
				if _, _, err := l.Claim(context.Background(), keyC, metaFor("tool.e"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.Abandon(context.Background(), keyC, testWho(), "test abandon claimed"); err != nil {
					t.Fatalf("abandon claimed: %v", err)
				}
				recC, err := l.Get(context.Background(), keyC)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if recC.State != StateAbandoned {
					t.Fatalf("state = %q, want %q", recC.State, StateAbandoned)
				}

				keyR := EffectKey("tool.e", "sess", "receipted")
				if _, _, err := l.Claim(context.Background(), keyR, metaFor("tool.e"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.RecordReceipt(context.Background(), keyR, testWho(), json.RawMessage(`{}`)); err != nil {
					t.Fatalf("record receipt: %v", err)
				}
				if err := l.Abandon(context.Background(), keyR, testWho(), "test abandon receipted"); err != nil {
					t.Fatalf("abandon receipted: %v", err)
				}
				recR, err := l.Get(context.Background(), keyR)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if recR.State != StateAbandoned {
					t.Fatalf("state = %q, want %q", recR.State, StateAbandoned)
				}
			})

			t.Run("complete is idempotent on completed", func(t *testing.T) {
				key := EffectKey("tool.f", "sess", "step")
				if _, _, err := l.Claim(context.Background(), key, metaFor("tool.f"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.Complete(context.Background(), key, testWho()); err != nil {
					t.Fatalf("first complete: %v", err)
				}
				if err := l.Complete(context.Background(), key, testWho()); err != nil {
					t.Fatalf("re-complete of completed: %v", err)
				}
			})

			t.Run("unknown key errors", func(t *testing.T) {
				key := EffectKey("tool.g", "sess", "missing")
				ctx := context.Background()
				if _, err := l.Get(ctx, key); !errors.Is(err, ErrUnknownKey) {
					t.Fatalf("get unknown: err=%v, want ErrUnknownKey", err)
				}
				if err := l.RecordReceipt(ctx, key, testWho(), json.RawMessage(`{}`)); !errors.Is(err, ErrUnknownKey) {
					t.Fatalf("record receipt unknown: err=%v, want ErrUnknownKey", err)
				}
				if err := l.Complete(ctx, key, testWho()); !errors.Is(err, ErrUnknownKey) {
					t.Fatalf("complete unknown: err=%v, want ErrUnknownKey", err)
				}
				if err := l.Abandon(ctx, key, testWho(), "nope"); !errors.Is(err, ErrUnknownKey) {
					t.Fatalf("abandon unknown: err=%v, want ErrUnknownKey", err)
				}
			})

			t.Run("invalid transitions", func(t *testing.T) {
				ctx := context.Background()
				// completed rejects RecordReceipt and Abandon.
				keyDone := EffectKey("tool.h", "sess", "done")
				if _, _, err := l.Claim(ctx, keyDone, metaFor("tool.h"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.Complete(ctx, keyDone, testWho()); err != nil {
					t.Fatalf("complete: %v", err)
				}
				if err := l.RecordReceipt(ctx, keyDone, testWho(), json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("record receipt on completed: err=%v, want ErrInvalidTransition", err)
				}
				if err := l.Abandon(ctx, keyDone, testWho(), "nope"); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("abandon completed: err=%v, want ErrInvalidTransition", err)
				}
				// abandoned rejects everything.
				keyDead := EffectKey("tool.h", "sess", "dead")
				if _, _, err := l.Claim(ctx, keyDead, metaFor("tool.h"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.Abandon(ctx, keyDead, testWho(), "dead"); err != nil {
					t.Fatalf("abandon: %v", err)
				}
				if err := l.RecordReceipt(ctx, keyDead, testWho(), json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("record receipt on abandoned: err=%v, want ErrInvalidTransition", err)
				}
				if err := l.Complete(ctx, keyDead, testWho()); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("complete abandoned: err=%v, want ErrInvalidTransition", err)
				}
				if err := l.Abandon(ctx, keyDead, testWho(), "again"); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("abandon abandoned: err=%v, want ErrInvalidTransition", err)
				}
				// receipted rejects RecordReceipt (already receipted).
				keyRec := EffectKey("tool.h", "sess", "twice")
				if _, _, err := l.Claim(ctx, keyRec, metaFor("tool.h"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.RecordReceipt(ctx, keyRec, testWho(), json.RawMessage(`{}`)); err != nil {
					t.Fatalf("first record receipt: %v", err)
				}
				if err := l.RecordReceipt(ctx, keyRec, testWho(), json.RawMessage(`{}`)); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("second record receipt: err=%v, want ErrInvalidTransition", err)
				}
			})

			t.Run("reconcile filters and orders", func(t *testing.T) {
				ctx := context.Background()
				pendingKeys := []string{
					EffectKey("tool.i", "sess", "p1"),
					EffectKey("tool.i", "sess", "p2"),
					EffectKey("tool.i", "sess", "p3"),
				}
				for i, key := range pendingKeys {
					if _, _, err := l.Claim(ctx, key, metaFor("tool.i"), testWho()); err != nil {
						t.Fatalf("claim %d: %v", i, err)
					}
					// Ensure strictly increasing claimed_at ordering.
					time.Sleep(2 * time.Millisecond)
				}
				// p1: leave claimed. p2: receipt it. p3: complete it.
				if err := l.RecordReceipt(ctx, pendingKeys[1], testWho(), json.RawMessage(`{"n":2}`)); err != nil {
					t.Fatalf("receipt p2: %v", err)
				}
				if err := l.Complete(ctx, pendingKeys[2], testWho()); err != nil {
					t.Fatalf("complete p3: %v", err)
				}

				pending, err := l.ReconcilePending(ctx)
				if err != nil {
					t.Fatalf("reconcile: %v", err)
				}
				var found []string
				for _, rec := range pending {
					switch rec.Key {
					case pendingKeys[0]:
						if rec.State != StateClaimed {
							t.Errorf("p1 state = %q, want %q", rec.State, StateClaimed)
						}
						found = append(found, rec.Key)
					case pendingKeys[1]:
						if rec.State != StateReceipted {
							t.Errorf("p2 state = %q, want %q", rec.State, StateReceipted)
						}
						found = append(found, rec.Key)
					case pendingKeys[2]:
						t.Errorf("p3 (completed) must not appear in ReconcilePending")
					}
				}
				if len(found) != 2 {
					t.Fatalf("pending records found = %d (%v), want 2", len(found), found)
				}
				// Oldest claimed_at first: p1 claimed before p2.
				if found[0] != pendingKeys[0] || found[1] != pendingKeys[1] {
					t.Fatalf("pending order = [%s, %s], want p1 then p2 (oldest first)", found[0], found[1])
				}
			})

			t.Run("returned records are defensive copies", func(t *testing.T) {
				key := EffectKey("tool.j", "sess", "step")
				if _, _, err := l.Claim(ctx2(), key, metaFor("tool.j"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				first, err := l.Get(ctx2(), key)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				// Mutate everything the caller can reach.
				first.State = StateCompleted
				first.Tool = "mutated"
				first.ClaimedAt = time.Time{}
				if first.Payload != nil {
					first.Payload[0] = 'X'
				}
				second, err := l.Get(ctx2(), key)
				if err != nil {
					t.Fatalf("second get: %v", err)
				}
				if second.State != StateClaimed {
					t.Fatalf("stored state corrupted via returned record: %q", second.State)
				}
				if second.Tool != "tool.j" {
					t.Fatalf("stored tool corrupted via returned record: %q", second.Tool)
				}
				if second.ClaimedAt.IsZero() {
					t.Fatal("stored ClaimedAt corrupted via returned record")
				}
				if second.Payload != nil && second.Payload[0] == 'X' {
					t.Fatal("stored payload corrupted via returned record")
				}
			})

			t.Run("double-claim race", func(t *testing.T) {
				const goroutines = 16
				key := EffectKey("tool.race", "sess", "step")
				var wg sync.WaitGroup
				granted := make(chan struct{}, goroutines)
				for range goroutines {
					wg.Go(func() {
						ok, _, err := l.Claim(ctx2(), key, EffectMeta{Tool: "tool.race"}, testWho())
						if err != nil {
							t.Errorf("claim: %v", err)
							return
						}
						if ok {
							granted <- struct{}{}
						}
					})
				}
				wg.Wait()
				close(granted)
				if n := len(granted); n != 1 {
					t.Fatalf("granted count = %d, want exactly 1", n)
				}
			})
		})
	}
}

func ctx2() context.Context { return context.Background() }

func TestRun(t *testing.T) {
	for name, newLedger := range newTestLedgers(t) {
		t.Run(name, func(t *testing.T) {
			l := newLedger()
			defer func() {
				if err := l.Close(); err != nil {
					t.Errorf("close ledger: %v", err)
				}
			}()
			ctx := context.Background()

			t.Run("fresh key executes and receipts", func(t *testing.T) {
				key := EffectKey("run.a", "sess", "step")
				receipt := json.RawMessage(`{"pushed":true}`)
				got, reused, err := Run(ctx, l, key, metaFor("run.a"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) { return receipt, nil })
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if reused {
					t.Fatal("reused=true on fresh key, want false")
				}
				if !bytesEqualJSON(got, receipt) {
					t.Fatalf("receipt = %s, want %s", got, receipt)
				}
				rec, err := l.Get(ctx, key)
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if rec.State != StateReceipted {
					t.Fatalf("state = %q, want %q (Run does not Complete; callers do)", rec.State, StateReceipted)
				}
				if !bytesEqualJSON(rec.Receipt, receipt) {
					t.Fatalf("stored receipt = %s, want %s", rec.Receipt, receipt)
				}
			})

			t.Run("completed prior reuses receipt without executing", func(t *testing.T) {
				key := EffectKey("run.b", "sess", "step")
				priorReceipt := json.RawMessage(`{"first":true}`)
				if _, _, err := l.Claim(ctx, key, metaFor("run.b"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.RecordReceipt(ctx, key, testWho(), priorReceipt); err != nil {
					t.Fatalf("receipt: %v", err)
				}
				if err := l.Complete(ctx, key, testWho()); err != nil {
					t.Fatalf("complete: %v", err)
				}
				executed := false
				got, reused, err := Run(ctx, l, key, metaFor("run.b"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) {
						executed = true
						return json.RawMessage(`{"second":true}`), nil
					})
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if executed {
					t.Fatal("execute ran despite completed prior; must be an idempotent no-op")
				}
				if !reused {
					t.Fatal("reused=false with completed prior, want true")
				}
				if !bytesEqualJSON(got, priorReceipt) {
					t.Fatalf("receipt = %s, want prior receipt %s", got, priorReceipt)
				}
			})

			t.Run("claimed prior owned by someone else does not execute", func(t *testing.T) {
				key := EffectKey("run.c", "sess", "step")
				if _, _, err := l.Claim(ctx, key, metaFor("run.c"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				executed := false
				_, _, err := Run(ctx, l, key, metaFor("run.c"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) {
						executed = true
						return json.RawMessage(`{}`), nil
					})
				if err == nil {
					t.Fatal("run on claimed prior: err=nil, want ErrInvalidTransition-wrapped error")
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("err=%v, want ErrInvalidTransition in chain", err)
				}
				if !contains(err.Error(), string(StateClaimed)) {
					t.Fatalf("error %q does not name the conflicting state %q", err, StateClaimed)
				}
				if executed {
					t.Fatal("execute ran despite claimed prior owned by another caller")
				}
			})

			t.Run("receipted prior owned by someone else does not execute", func(t *testing.T) {
				key := EffectKey("run.d", "sess", "step")
				if _, _, err := l.Claim(ctx, key, metaFor("run.d"), testWho()); err != nil {
					t.Fatalf("claim: %v", err)
				}
				if err := l.RecordReceipt(ctx, key, testWho(), json.RawMessage(`{}`)); err != nil {
					t.Fatalf("receipt: %v", err)
				}
				executed := false
				_, _, err := Run(ctx, l, key, metaFor("run.d"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) {
						executed = true
						return json.RawMessage(`{}`), nil
					})
				if err == nil || !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("err=%v, want ErrInvalidTransition in chain", err)
				}
				if executed {
					t.Fatal("execute ran despite receipted prior owned by another caller")
				}
			})

			t.Run("execute error propagates and record stays claimed", func(t *testing.T) {
				key := EffectKey("run.e", "sess", "step")
				boom := errors.New("provider exploded")
				_, _, runErr := Run(ctx, l, key, metaFor("run.e"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) { return nil, boom })
				if !errors.Is(runErr, boom) {
					t.Fatalf("run err=%v, want the execute error in the chain", runErr)
				}
				pending, err := l.ReconcilePending(ctx)
				if err != nil {
					t.Fatalf("reconcile: %v", err)
				}
				var found bool
				for _, rec := range pending {
					if rec.Key == key && rec.State == StateClaimed {
						found = true
					}
				}
				if !found {
					t.Fatal("failed effect not visible as claimed in ReconcilePending")
				}
			})

			t.Run("nil receipt is an error", func(t *testing.T) {
				key := EffectKey("run.f", "sess", "step")
				_, _, err := Run(ctx, l, key, metaFor("run.f"), testWho(),
					func(ctx context.Context) (json.RawMessage, error) { return nil, nil })
				if err == nil {
					t.Fatal("run with nil receipt: err=nil, want an error")
				}
				if !contains(err.Error(), "execute returned nil receipt") {
					t.Fatalf("err=%q, want it to name the mandatory receipt", err)
				}
			})
		})
	}
}

func bytesEqualJSON(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

var _ = fmt.Sprintf // keep fmt import if unused by future edits

// TestSecondClaimantCannotComplete pins issue 73: a resolve must match
// the claimant that claimed the key. A second principal who knows the
// key cannot Complete, Abandon, or RecordReceipt the first claimant's
// pending effect. The owner still can.
func TestSecondClaimantCannotComplete(t *testing.T) {
	owner := Claimant{AgentID: "agent-a", SessionID: "sess-a"}
	other := Claimant{AgentID: "agent-b", SessionID: "sess-b"}
	ctx := context.Background()
	for name, newLedger := range newTestLedgers(t) {
		t.Run(name, func(t *testing.T) {
			l := newLedger()
			defer func() {
				if err := l.Close(); err != nil {
					t.Errorf("close ledger: %v", err)
				}
			}()
			key := EffectKey("tool.claimant", "pin", name)
			if _, _, err := l.Claim(ctx, key, metaFor("tool.claimant"), owner); err != nil {
				t.Fatalf("claim: %v", err)
			}
			if err := l.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
				t.Fatalf("second claimant Complete: err=%v, want ErrClaimantMismatch", err)
			}
			if err := l.Abandon(ctx, key, other, "stolen"); !errors.Is(err, ErrClaimantMismatch) {
				t.Fatalf("second claimant Abandon: err=%v, want ErrClaimantMismatch", err)
			}
			if err := l.RecordReceipt(ctx, key, other, json.RawMessage(`{"stolen":true}`)); !errors.Is(err, ErrClaimantMismatch) {
				t.Fatalf("second claimant RecordReceipt: err=%v, want ErrClaimantMismatch", err)
			}
			rec, err := l.Get(ctx, key)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if rec.State != StateClaimed {
				t.Fatalf("state = %q after mismatched resolves, want still claimed", rec.State)
			}
			if got, ok := rec.ClaimantOf(); !ok || !got.Matches(owner) {
				t.Fatalf("stored claimant = %+v ok=%v, want owner", got, ok)
			}
			if err := l.Complete(ctx, key, owner); err != nil {
				t.Fatalf("owner Complete: %v", err)
			}
			rec, err = l.Get(ctx, key)
			if err != nil {
				t.Fatalf("get after owner complete: %v", err)
			}
			if rec.State != StateCompleted {
				t.Fatalf("state = %q, want completed by owner", rec.State)
			}
			// A later Complete by the other principal is still a mismatch,
			// not the idempotent success the owner gets.
			if err := l.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
				t.Fatalf("second claimant re-Complete: err=%v, want ErrClaimantMismatch", err)
			}
			if _, _, err := l.Claim(ctx, EffectKey("tool.claimant", "empty", name), metaFor("tool.claimant"), Claimant{}); !errors.Is(err, ErrClaimantRequired) {
				t.Fatalf("zero claimant claim: err=%v, want ErrClaimantRequired", err)
			}
		})
	}
}

// TestLegacyUnclaimedFirstClaimWins pins the migration rule: a row with
// no claimant cannot be resolved by a guess, and the first Claim adopts
// it (granted stays false — the effect must not be re-executed).
func TestLegacyUnclaimedFirstClaimWins(t *testing.T) {
	ctx := context.Background()
	owner := Claimant{AgentID: "adopter", SessionID: "sess-legacy"}
	other := Claimant{AgentID: "intruder", SessionID: "sess-other"}
	key := EffectKey("legacy", "row", "1")

	t.Run("memory", func(t *testing.T) {
		m := NewMemoryLedger()
		m.records[key] = EffectRecord{
			Key: key, State: StateClaimed, Tool: "legacy",
			ClaimedAt: time.Now().UTC(),
		}
		if err := m.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
			t.Fatalf("complete legacy before adopt: %v, want ErrClaimantMismatch", err)
		}
		granted, prior, err := m.Claim(ctx, key, EffectMeta{Tool: "legacy"}, owner)
		if err != nil {
			t.Fatalf("adopt claim: %v", err)
		}
		if granted {
			t.Fatal("legacy adopt granted=true, want false (do not re-execute)")
		}
		got, ok := Claimant{}, false
		if prior != nil {
			got, ok = prior.ClaimantOf()
		}
		if prior == nil || !prior.ClaimantSet || !ok || !got.Matches(owner) {
			t.Fatalf("adopted prior claimant = %+v ok=%v, want owner", got, ok)
		}
		if err := m.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
			t.Fatalf("intruder complete after adopt: %v", err)
		}
		if err := m.Complete(ctx, key, owner); err != nil {
			t.Fatalf("adopter complete: %v", err)
		}
	})

	t.Run("sqlite", func(t *testing.T) {
		sl, err := NewSQLiteLedger(filepath.Join(t.TempDir(), "effects.db"), testLogger())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() {
			if err := sl.Close(); err != nil {
				t.Errorf("close: %v", err)
			}
		}()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := sl.db.ExecContext(ctx,
			`INSERT INTO effects_ledger (key, state, claimed_at, tool) VALUES (?, 'claimed', ?, 'legacy')`,
			key, now); err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
		if err := sl.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
			t.Fatalf("complete legacy before adopt: %v, want ErrClaimantMismatch", err)
		}
		granted, prior, err := sl.Claim(ctx, key, EffectMeta{Tool: "legacy"}, owner)
		if err != nil {
			t.Fatalf("adopt claim: %v", err)
		}
		if granted {
			t.Fatal("legacy adopt granted=true, want false")
		}
		if prior == nil || !prior.ClaimantSet {
			t.Fatalf("adopted prior = %+v, want claimant set", prior)
		}
		got, ok := prior.ClaimantOf()
		if !ok || !got.Matches(owner) {
			t.Fatalf("adopted claimant = %+v ok=%v, want owner", got, ok)
		}
		if err := sl.Complete(ctx, key, other); !errors.Is(err, ErrClaimantMismatch) {
			t.Fatalf("intruder complete after adopt: %v", err)
		}
		if err := sl.Complete(ctx, key, owner); err != nil {
			t.Fatalf("adopter complete: %v", err)
		}
	})
}
