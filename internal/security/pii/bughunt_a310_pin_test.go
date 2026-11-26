// bughunt 2026-11-25 gap A3-10 pin test.
package pii

import (
	"path/filepath"
	"testing"
	"time"
)

// A3-10: expired swap rows must be EVICTED from the in-memory mirrors and
// the disk on subsequent Put (lazy sweep). Before the fix, the restore
// window was honoured by Resolve only — raw PII values accumulated in
// st.mem/st.fwd and pii_swaps for the process lifetime.
func TestSwapStore_PutEvictsExpiredRows(t *testing.T) {
	st, err := NewSwapStore(filepath.Join(t.TempDir(), "pii"))
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	st.SetRestoreWindow(1 * time.Second)

	// Row A: expires in ~1s.
	if err := st.Put("sess-a", "tok-a", "SSN-111-22-3333", "ssn"); err != nil {
		t.Fatalf("put A: %v", err)
	}
	// Let the restore window pass.
	time.Sleep(1100 * time.Millisecond)

	// Row B: fresh — its Put must sweep row A out of mem, fwd, and disk.
	if err := st.Put("sess-b", "tok-b", "4111-1111-1111-1111", "card"); err != nil {
		t.Fatalf("put B: %v", err)
	}

	st.mu.RLock()
	_, memA := st.mem[storeKey("sess-a", "tok-a")]
	_, fwdA := st.fwd[forwardKey("sess-a", "ssn", "SSN-111-22-3333")]
	_, memB := st.mem[storeKey("sess-b", "tok-b")]
	st.mu.RUnlock()

	if memA {
		t.Error("A3-10 regression: expired row still in st.mem after a subsequent Put")
	}
	if fwdA {
		t.Error("A3-10 regression: expired row's forward index still in st.fwd after a subsequent Put")
	}
	if !memB {
		t.Fatal("fresh row must stay in st.mem")
	}

	// Disk row pruned too.
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM pii_swaps WHERE session_id = 'sess-a'`).Scan(&count); err != nil {
		t.Fatalf("disk count: %v", err)
	}
	if count != 0 {
		t.Errorf("expired row still on disk (%d rows); the disk sweep never ran", count)
	}

	// Resolve for the evicted row must fail (already did via TTL; keep it
	// true after eviction).
	if _, ok := st.Resolve("sess-a", "tok-a"); ok {
		t.Error("expired row must not resolve")
	}
}

// A3-10 (handle capture): a Put racing Close must never panic — the write
// handle is captured under the lock and nil-guarded — and a Put after
// Close reports ErrStoreClosed, not an opaque sql error or a nil deref.
func TestSwapStore_PutAfterCloseIsClosedError(t *testing.T) {
	st, err := NewSwapStore(filepath.Join(t.TempDir(), "pii"))
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// st.db and st.ins are nil past this point: a Put that ignored the
	// closed flag (the pre-fix shape could Exec a captured handle) must
	// still land on the documented ErrStoreClosed contract.
	if err := st.Put("sess-x", "tok-x", "secret", "ssn"); err != ErrStoreClosed {
		t.Fatalf("post-close Put error = %v, want ErrStoreClosed", err)
	}
	// Concurrent Put/Close hammer: no panic, no data race (run with -race
	// in CI gates).
	st2, err := NewSwapStore(filepath.Join(t.TempDir(), "pii2"))
	if err != nil {
		t.Fatalf("NewSwapStore 2: %v", err)
	}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			for j := 0; j < 50; j++ {
				_ = st2.Put("s", tokenFor(i, j), "value-keep-out-of-logs", "class")
			}
			done <- struct{}{}
		}(i)
	}
	go func() {
		time.Sleep(2 * time.Millisecond)
		_ = st2.Close()
	}()
	for i := 0; i < 8; i++ {
		<-done
	}
}

func tokenFor(i, j int) string {
	return string(rune('a'+i)) + string(rune('a'+j%26)) + string(rune('0'+j/26))
}
