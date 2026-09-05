package effects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// TestSQLiteLedgerPersistenceAcrossReopen pins the durable behavior: a
// record claimed (and one receipted) before Close is visible after the
// ledger is reopened, with identical payloads and receipts.
func TestSQLiteLedgerPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "effects.db")

	l1, err := NewSQLiteLedger(dbPath, testLogger())
	if err != nil {
		t.Fatalf("open ledger 1: %v", err)
	}

	keyClaimed := EffectKey("backup.git_push", "sess-1", "step-1")
	keyReceipted := EffectKey("backup.git_push", "sess-1", "step-2")
	payload := json.RawMessage(`{"repo":"/srv/backup.git","head":"abc123"}`)

	if _, _, err := l1.Claim(ctx, keyClaimed, EffectMeta{
		Tool: "backup.git_push", SessionID: "sess-1", ProviderIdempotent: true, Payload: payload,
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, _, err := l1.Claim(ctx, keyReceipted, EffectMeta{
		Tool: "backup.git_push", SessionID: "sess-1", ProviderIdempotent: true, Payload: payload,
	}); err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	receipt := json.RawMessage(`{"pushed":true,"ref":"refs/heads/main"}`)
	if err := l1.RecordReceipt(ctx, keyReceipted, receipt); err != nil {
		t.Fatalf("record receipt: %v", err)
	}
	if err := l1.Close(); err != nil {
		t.Fatalf("close ledger 1: %v", err)
	}

	l2, err := NewSQLiteLedger(dbPath, testLogger())
	if err != nil {
		t.Fatalf("reopen ledger: %v", err)
	}
	defer func() {
		if err := l2.Close(); err != nil {
			t.Errorf("close ledger 2: %v", err)
		}
	}()

	pending, err := l2.ReconcilePending(ctx)
	if err != nil {
		t.Fatalf("reconcile after reopen: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending records after reopen = %d, want 2", len(pending))
	}
	for _, rec := range pending {
		if !bytes.Equal(rec.Payload, payload) {
			t.Errorf("record %s: payload = %s, want byte-identical %s", rec.Key, rec.Payload, payload)
		}
		switch rec.Key {
		case keyClaimed:
			if rec.State != StateClaimed {
				t.Errorf("claimed record state = %q after reopen, want %q", rec.State, StateClaimed)
			}
		case keyReceipted:
			if rec.State != StateReceipted {
				t.Errorf("receipted record state = %q after reopen, want %q", rec.State, StateReceipted)
			}
			if !bytes.Equal(rec.Receipt, receipt) {
				t.Errorf("receipt = %s, want byte-identical %s", rec.Receipt, receipt)
			}
		default:
			t.Errorf("unexpected pending record %s", rec.Key)
		}
	}
}

// TestSQLiteLedgerReceiptByteForByte pins that receipts/payloads stored as
// TEXT come back verbatim — no unmarshal/re-marshal normalization.
func TestSQLiteLedgerReceiptByteForByte(t *testing.T) {
	ctx := context.Background()
	l, err := NewSQLiteLedger(filepath.Join(t.TempDir(), "effects.db"), testLogger())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Errorf("close ledger: %v", err)
		}
	}()

	key := EffectKey("tool", "sess", "bytes")
	raw := json.RawMessage(`{"z":1,"a":[1,2],"note":"  spaced  "}`)
	if _, _, err := l.Claim(ctx, key, EffectMeta{Tool: "tool", Payload: raw}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	out, err := l.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(out.Payload, raw) {
		t.Fatalf("payload round-trip = %s, want byte-identical %s", out.Payload, raw)
	}

	receipt := json.RawMessage(`{"weird_key_order":true,"x":null}`)
	if err := l.RecordReceipt(ctx, key, receipt); err != nil {
		t.Fatalf("record receipt: %v", err)
	}
	out, err = l.Get(ctx, key)
	if err != nil {
		t.Fatalf("get after receipt: %v", err)
	}
	if !bytes.Equal(out.Receipt, receipt) {
		t.Fatalf("receipt round-trip = %s, want byte-identical %s", out.Receipt, receipt)
	}
}

// TestSQLiteLedgerUniqueConstraintPath pins that a conflicting INSERT (not
// a pre-check) is what surfaces the prior record.
func TestSQLiteLedgerUniqueConstraintPath(t *testing.T) {
	ctx := context.Background()
	l, err := NewSQLiteLedger(filepath.Join(t.TempDir(), "effects.db"), testLogger())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Errorf("close ledger: %v", err)
		}
	}()

	key := EffectKey("tool", "sess", "dup")
	if _, _, err := l.Claim(ctx, key, EffectMeta{Tool: "tool"}); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	granted, prior, err := l.Claim(ctx, key, EffectMeta{Tool: "tool"})
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if granted {
		t.Fatal("second claim granted=true; PK conflict must deny it")
	}
	if prior == nil {
		t.Fatal("second claim prior=nil; PK conflict must surface the prior record")
	}
	if prior.State != StateClaimed {
		t.Fatalf("prior state = %q, want %q", prior.State, StateClaimed)
	}
	// The unknown-key error contract must hold over SQL too.
	if _, err := l.Get(ctx, EffectKey("tool", "sess", "absent")); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("get unknown: err=%v, want ErrUnknownKey", err)
	}
}
