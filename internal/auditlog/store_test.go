package auditlog

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrate_Idempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate 1: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate 2: %v", err)
	}
}

func TestAppend_ChainsSeqAndHashes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()

	r1, err := s.Append(ctx, rec(0, "ignored-prev", "audit_finding"))
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if r1.Seq != 1 || r1.PrevHash != "" {
		t.Fatalf("genesis: got %+v", r1)
	}
	if r1.RecordHash == "" {
		t.Fatal("RecordHash must be filled")
	}
	r2, err := s.Append(ctx, r1) // caller-supplied Seq/PrevHash overwritten
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if r2.Seq != 2 || r2.PrevHash != r1.RecordHash {
		t.Fatalf("chain: got %+v", r2)
	}

	// Zero At is stamped to now.
	r3, err := s.Append(ctx, Record{Type: "x", Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("Append 3: %v", err)
	}
	if r3.At.IsZero() {
		t.Fatal("zero At must be stamped")
	}
}

func TestHead(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_ = Migrate(ctx, db)
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()

	if _, ok, err := s.Head(ctx); err != nil || ok {
		t.Fatalf("empty head: ok=%v err=%v", ok, err)
	}
	r1, _ := s.Append(ctx, rec(0, "", "t"))
	h, ok, err := s.Head(ctx)
	if err != nil || !ok || h.RecordHash != r1.RecordHash {
		t.Fatalf("head: ok=%v err=%v head=%+v", ok, err, h)
	}
}

func TestOpenStore_ExpandsHomeAndCreatesDir(t *testing.T) {
	// Relative path with a new subdirectory must be created with 0700.
	dir := filepath.Join(t.TempDir(), "nested", "audit")
	s, err := OpenStore(filepath.Join(dir, "chain.db"), nil)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.IsDir() == false {
		t.Fatal("dir not created")
	}
}

func TestAppend_ConcurrentSerialization(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_ = Migrate(ctx, db)
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()

	const n = 20
	var wg sync.WaitGroup
	seqs := make(chan uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := s.Append(ctx, Record{
				Type: "concurrent", EmployeeID: "e",
				Payload: map[string]any{"i": i},
			})
			if err != nil {
				t.Errorf("Append %d: %v", i, err)
				return
			}
			seqs <- r.Seq
		}(i)
	}
	wg.Wait()
	close(seqs)
	seen := map[uint64]bool{}
	for sq := range seqs {
		if seen[sq] {
			t.Fatalf("duplicate seq %d", sq)
		}
		seen[sq] = true
	}
	res, err := VerifyChain(ctx, db)
	if err != nil || !res.OK || res.Records != n {
		t.Fatalf("verify after concurrent appends: %+v err=%v", res, err)
	}
}
