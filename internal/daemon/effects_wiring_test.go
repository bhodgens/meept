package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
)

// effectsTestBuf returns a logger + its buffer for log-assertion tests.
func effectsTestBuf() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, &buf
}

// seedClaimedRecord claims a key directly in the ledger, returning its key.
func seedClaimedRecord(t *testing.T, l effects.Ledger, tool string, providerIdempotent bool, payload json.RawMessage) string {
	t.Helper()
	key := effects.EffectKey("reconcile-test", tool, payloadString(payload))
	_, _, err := l.Claim(context.Background(), key, effects.EffectMeta{
		Tool:               tool,
		ProviderIdempotent: providerIdempotent,
		Payload:            payload,
	})
	if err != nil {
		t.Fatalf("seed claim: %v", err)
	}
	return key
}

func payloadString(p json.RawMessage) string { return string(p) }

// seedReceiptedRecord claims and then records a receipt, simulating an
// effect that ran but whose completion marker was never written.
func seedReceiptedRecord(t *testing.T, l effects.Ledger, tool string, providerIdempotent bool) string {
	t.Helper()
	payload := json.RawMessage(`{"seeded":"receipted"}`)
	key := seedClaimedRecord(t, l, tool, providerIdempotent, payload)
	if err := l.RecordReceipt(context.Background(), key, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("seed receipt: %v", err)
	}
	return key
}

func TestEffectsReconciler(t *testing.T) {
	ctx := context.Background()

	t.Run("idempotent tool retried", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		logger, _ := effectsTestBuf()
		key := seedClaimedRecord(t, ledger, "backup.git_push", true, json.RawMessage(`{"retry":"me"}`))

		r := NewEffectsReconciler(ledger, logger)
		execRan := false
		r.RegisterExecutor("backup.git_push", func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error) {
			execRan = true
			if rec.Key != key {
				t.Errorf("executor got key %q, want %q", rec.Key, key)
			}
			return json.RawMessage(`{"retried":true}`), nil
		})

		counts := r.Reconcile(ctx)
		if !execRan {
			t.Error("executor did not run")
		}
		if counts.Retried != 1 || counts.Failed != 0 || counts.Surfaced != 0 {
			t.Errorf("counts = %+v, want Retried=1", counts)
		}
		rec, err := ledger.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec.State != effects.StateCompleted {
			t.Errorf("state = %q, want completed", rec.State)
		}
		if string(rec.Receipt) != `{"retried":true}` {
			t.Errorf("receipt = %s, want re-executor receipt", rec.Receipt)
		}
	})

	t.Run("idempotent tool executor fails", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		logger, buf := effectsTestBuf()
		key := seedClaimedRecord(t, ledger, "backup.git_push", true, json.RawMessage(`{"fail":"me"}`))

		r := NewEffectsReconciler(ledger, logger)
		r.RegisterExecutor("backup.git_push", func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		})

		counts := r.Reconcile(ctx)
		if counts.Retried != 0 || counts.Failed != 1 || counts.Surfaced != 0 {
			t.Errorf("counts = %+v, want Failed=1", counts)
		}
		rec, err := ledger.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec.State != effects.StateClaimed {
			t.Errorf("state = %q, want claimed (stays pending)", rec.State)
		}
		if !bytes.Contains(buf.Bytes(), []byte("effect reconcile retry failed")) {
			t.Errorf("log missing warn; got: %s", buf.String())
		}
	})

	t.Run("non-idempotent tool surfaced", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		logger, buf := effectsTestBuf()
		key := seedClaimedRecord(t, ledger, "push.notify", false, json.RawMessage(`{"surfaced":"me"}`))

		r := NewEffectsReconciler(ledger, logger)
		execRan := false
		// an executor EXISTS for this tool, but ProviderIdempotent=false
		// means it must NEVER be invoked
		r.RegisterExecutor("push.notify", func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error) {
			execRan = true
			return json.RawMessage(`{}`), nil
		})

		counts := r.Reconcile(ctx)
		if execRan {
			t.Error("non-idempotent effect must NOT be re-executed even with a registered executor")
		}
		if counts.Retried != 0 || counts.Surfaced != 1 || counts.Failed != 0 {
			t.Errorf("counts = %+v, want Surfaced=1", counts)
		}
		rec, err := ledger.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if rec.State != effects.StateClaimed {
			t.Errorf("state = %q, want claimed (surfaced, untouched)", rec.State)
		}
		if !bytes.Contains(buf.Bytes(), []byte("effect pending reconciliation")) {
			t.Errorf("log missing error; got: %s", buf.String())
		}
	})

	t.Run("no executor registered surfaces idempotent record", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		logger, _ := effectsTestBuf()
		seedClaimedRecord(t, ledger, "backup.git_push", true, json.RawMessage(`{"nobody":"home"}`))

		r := NewEffectsReconciler(ledger, logger)
		counts := r.Reconcile(ctx)
		if counts.Retried != 0 || counts.Surfaced != 1 {
			t.Errorf("counts = %+v, want Surfaced=1 (never retried blind)", counts)
		}
	})

	t.Run("receipted stuck record follows the same policy", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		logger, _ := effectsTestBuf()
		seedReceiptedRecord(t, ledger, "backup.git_push", true)

		r := NewEffectsReconciler(ledger, logger)
		execCalls := 0
		r.RegisterExecutor("backup.git_push", func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error) {
			execCalls++
			// The effect already ran (receipt exists): the re-executor
			// replays the stored receipt rather than re-firing the effect.
			return rec.Receipt, nil
		})

		counts := r.Reconcile(ctx)
		if execCalls != 1 {
			t.Errorf("executor calls = %d, want 1", execCalls)
		}
		if counts.Retried != 1 {
			t.Errorf("counts = %+v, want Retried=1", counts)
		}
	})

	t.Run("reconcile pending error is returned", func(t *testing.T) {
		r := NewEffectsReconciler(&erringLedger{}, effectsTestBufLogger())
		counts := r.Reconcile(ctx)
		if counts.Retried != 0 || counts.Surfaced != 0 || counts.Failed != 0 {
			t.Errorf("counts = %+v, want zeros on ledger error", counts)
		}
		if !failedReconcileLogged {
			// the error path logs; asserted via the shared flag below
			t.Error("expected ledger error to be logged")
		}
	})
}

// erringLedger always fails ReconcilePending. Embeds only the interface —
// never a concrete mutex-bearing struct — to keep the vet lock-copy check clean.
type erringLedger struct {
	effects.Ledger
}

func (erringLedger) ReconcilePending(ctx context.Context) ([]effects.EffectRecord, error) {
	return nil, context.DeadlineExceeded
}

var failedReconcileLogged bool

func effectsTestBufLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&countingWriter{}, nil))
}

type countingWriter struct{}

func (countingWriter) Write(p []byte) (int, error) {
	failedReconcileLogged = true
	return len(p), nil
}

func TestEffectsStartupReconcile(t *testing.T) {
	// Task 4: the helper the daemon startup hook calls, exercised against a
	// tempdir SQLite ledger pre-seeded with one claimed idempotent record.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "effects.db")

	seedLedger, err := effects.NewSQLiteLedger(dbPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteLedger: %v", err)
	}
	key := seedClaimedRecord(t, seedLedger, "backup.git_push", true, json.RawMessage(`{"startup":"seed"}`))
	if err := seedLedger.Close(); err != nil {
		t.Fatalf("Close seed ledger: %v", err)
	}

	// Reopen (proves persistence across restart — the startup scenario).
	logger, buf := effectsTestBuf()
	startupLedger, err := effects.NewSQLiteLedger(dbPath, logger)
	if err != nil {
		t.Fatalf("reopen NewSQLiteLedger: %v", err)
	}
	defer func() {
		if cerr := startupLedger.Close(); cerr != nil {
			t.Errorf("Close startup ledger: %v", cerr)
		}
	}()

	r := NewEffectsReconciler(startupLedger, logger)
	r.RegisterExecutor("backup.git_push", func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error) {
		return json.RawMessage(`{"startup":"retried"}`), nil
	})

	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	counts := runEffectsStartupReconcile(rctx, r, logger)
	if counts.Retried != 1 {
		t.Fatalf("counts = %+v, want Retried=1", counts)
	}
	if !bytes.Contains(buf.Bytes(), []byte("effects startup reconcile")) {
		t.Errorf("log missing startup reconcile summary; got: %s", buf.String())
	}

	rec, err := startupLedger.Get(rctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.State != effects.StateCompleted {
		t.Errorf("state = %q, want completed after startup reconcile", rec.State)
	}
}
