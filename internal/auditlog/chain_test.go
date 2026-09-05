package auditlog

import (
	"context"
	"testing"
	"time"
)

func fixedAt() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 123456789, time.UTC) }

func rec(seq uint64, prev string, typ string) Record {
	return Record{Seq: seq, PrevHash: prev, Type: typ, EmployeeID: "e1",
		Payload: map[string]any{"k": "v"}, At: fixedAt()}
}

func TestHashRecord_DeterministicAndKeyOrderIndependent(t *testing.T) {
	a := rec(1, "", "audit_finding")
	b := rec(1, "", "audit_finding")
	b.Payload = map[string]any{"k2": 1, "k1": "v"} // different key order/set → different hash
	h1, err := HashRecord(a)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if len(h1) != 64 {
		t.Fatalf("want 64-char hex, got %q", h1)
	}
	again, _ := HashRecord(a)
	if h1 != again {
		t.Fatal("hash must be deterministic")
	}
	h2, _ := HashRecord(b)
	if h1 == h2 {
		t.Fatal("different payloads must hash differently")
	}
	// RecordHash itself must NOT affect the hash (chicken-and-egg guard).
	a.RecordHash = "any-old-value"
	h3, _ := HashRecord(a)
	if h1 != h3 {
		t.Fatal("RecordHash must be excluded from hash input")
	}
}

func TestHashRecord_TimeNormalization(t *testing.T) {
	a := rec(1, "", "x")
	b := rec(1, "", "x")
	b.At = fixedAt().In(time.FixedZone("EST", -5*3600))
	h1, _ := HashRecord(a)
	h2, _ := HashRecord(b)
	// Same instant expressed in a different zone must hash identically:
	// HashRecord normalizes At to RFC3339Nano UTC (master.md C1).
	if h1 != h2 {
		t.Fatal("same instant in a different zone must hash identically (UTC normalization)")
	}
	c := rec(1, "", "x")
	c.At = fixedAt().Truncate(time.Second) // truncated instant differs
	h3, _ := HashRecord(c)
	if h1 == h3 {
		t.Fatal("truncation must change the hash (RFC3339Nano in canonical form)")
	}
}

func TestVerifyChain_EmptyAndGood(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	res, err := VerifyChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyChain empty: %v", err)
	}
	if !res.OK || res.Records != 0 {
		t.Fatalf("empty chain must verify: %+v", res)
	}

	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()
	prev := ""
	for i := uint64(1); i <= 5; i++ {
		got, err := s.Append(ctx, rec(i, prev, "audit_finding"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if got.Seq != i || got.PrevHash != prev {
			t.Fatalf("Append %d: got %+v want prev=%q", i, got, prev)
		}
		prev = got.RecordHash
	}
	res, err = VerifyChain(ctx, db)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !res.OK || res.Records != 5 {
		t.Fatalf("want ok 5 records: %+v", res)
	}
}

func TestVerifyChain_DetectsTamperAndLinkBreak(t *testing.T) {
	t.Run("payload tampered", func(t *testing.T) {
		db := chainOfThree(t)
		_, err := db.Exec(`UPDATE audit_log_chain SET payload='{"evil":1}' WHERE seq=2`)
		if err != nil {
			t.Fatalf("tamper exec: %v", err)
		}
		res, err := VerifyChain(context.Background(), db)
		if err != nil {
			t.Fatalf("VerifyChain: %v", err)
		}
		if res.OK || res.BrokenAt != 2 {
			t.Fatalf("want broken at 2: %+v", res)
		}
	})
	t.Run("prev_hash link broken", func(t *testing.T) {
		db := chainOfThree(t)
		_, err := db.Exec(`UPDATE audit_log_chain SET prev_hash='deadbeef' WHERE seq=3`)
		if err != nil {
			t.Fatalf("tamper exec: %v", err)
		}
		res, _ := VerifyChain(context.Background(), db)
		if res.OK || res.BrokenAt != 3 {
			t.Fatalf("want broken at 3: %+v", res)
		}
	})
	t.Run("genesis prev_hash must be empty", func(t *testing.T) {
		db := chainOfThree(t)
		_, _ = db.Exec(`UPDATE audit_log_chain SET prev_hash='x' WHERE seq=1`)
		res, _ := VerifyChain(context.Background(), db)
		if res.OK || res.BrokenAt != 1 {
			t.Fatalf("want broken at 1: %+v", res)
		}
	})
}
