// Bughunt 2026-11-27 fix wave F12 — audit S-7 (LOW).
//
// SwapStore.Put swept expired rows in two phases: collect under the lock,
// then DELETE on disk and prune the mirrors AFTER unlocking. In that
// unlock→DELETE window a concurrent re-Put of the same (session, token)
// could refresh the row — its INSERT upserts the SAME primary key — and
// the unconditional disk DELETE + mirror prune then erased the fresh
// mapping while the second Put had already returned nil. The fix captures
// each swept row's expiry and guards both phases with it: the DELETE
// carries `AND expiry = ?`, and the mirror prune re-checks the row under
// the lock before deleting.
//
// This pin drives the exact race deterministically: row A expires, a
// Put(B) sweep collects it, and BETWEEN the unlock and the disk DELETE we
// re-Put A with a fresh expiry. The re-Put's mapping must survive in
// st.mem, st.fwd, and on disk. Revert the guards (unconditional DELETE +
// blind mirror prune) and the deny assertions fail.
package pii

import (
	"testing"
	"time"
)

func TestSwapStore_RePutDuringSweepSurvives_S7(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	st.SetRestoreWindow(1 * time.Second)

	// Row C expires first (its own 1s window starts now); A's window
	// starts 250ms later. One 1.2s sleep puts BOTH past expiry, so the
	// sweep below collects more than just the key we re-Put.
	if err := st.Put("sess-c", "tok-c", "value-c", "ssn"); err != nil {
		t.Fatalf("put C: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	if err := st.Put("sess", "tok", "SSN-111-22-3333", "ssn"); err != nil {
		t.Fatalf("put A: %v", err)
	}
	time.Sleep(1200 * time.Millisecond) // A and C are both past their windows.

	// Re-Put A between the sweep's unlock and its disk DELETE: we
	// replicate only the FIRST sweep phase (collect under the lock, as
	// Put does), then re-Put A, then drive the REAL second phase via
	// SwapStore.sweepDeleteAndPrune — the same method Put calls — so a
	// revert of the TOCTOU guards in production code fails this pin.
	st.mu.Lock()
	now := time.Now().Unix()
	var expMem []string
	var expFwd []string
	var expDisk []expiredDiskRow
	for k, row := range st.mem {
		if expiredLocked(row, now) {
			expMem = append(expMem, k)
			if sid, _, ok := splitStoreKey(k); ok {
				expDisk = append(expDisk, expiredDiskRow{sessionID: sid, token: row.Token, expiry: row.Expiry})
			}
		}
	}
	for k, tok := range st.fwd {
		sid, _, ok := splitStoreKey(k)
		if !ok {
			continue
		}
		if row, has := st.mem[storeKey(sid, tok)]; !has || expiredLocked(row, now) {
			expFwd = append(expFwd, k)
		}
	}
	st.mu.Unlock()
	if len(expMem) == 0 {
		t.Fatal("pin setup: sweep collected zero expired rows (fixture broken)")
	}

	// The concurrent re-Put: refresh A with a FRESH expiry. This is what
	// another goroutine does inside the unlock→DELETE window.
	if err := st.Put("sess", "tok", "SSN-999-88-7777", "ssn"); err != nil {
		t.Fatalf("re-put A: %v", err)
	}

	// Finish the sweep phases via the REAL production method.
	st.sweepDeleteAndPrune(expMem, expFwd, expDisk)

	// THE PIN: the refreshed row A survived disk and mirror.
	st.mu.RLock()
	row, memOK := st.mem[storeKey("sess", "tok")]
	fwdOK := false
	for _, tok := range st.fwd {
		if tok == "tok" {
			fwdOK = true
		}
	}
	st.mu.RUnlock()
	if !memOK {
		t.Fatal("S-7: re-Put during the sweep window lost the mirror entry (blind prune deleted a refreshed row)")
	}
	if !fwdOK {
		t.Fatal("S-7: re-Put during the sweep window lost the forward index entry")
	}
	if row.Value != "SSN-999-88-7777" {
		t.Errorf("S-7: refreshed row value = %q, want the re-Put value", row.Value)
	}
	if got, ok := st.Resolve("sess", "tok"); !ok || got != "SSN-999-88-7777" {
		t.Errorf("S-7: Resolve after re-Put+resume = (%q, %v), want the fresh value", got, ok)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM pii_swaps WHERE session_id = 'sess' AND token = 'tok'`).Scan(&count); err != nil {
		t.Fatalf("disk count: %v", err)
	}
	if count != 1 {
		t.Errorf("S-7: refreshed disk row deleted by the sweep (%d rows), want 1", count)
	}

	// Still-expired row C did NOT survive: the guard keeps exactly the
	// refreshed rows, it must not turn the sweep into a no-op.
	var countC int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM pii_swaps WHERE session_id = 'sess-c'`).Scan(&countC); err != nil {
		t.Fatalf("disk count C: %v", err)
	}
	if countC != 0 {
		t.Errorf("S-7: still-expired row C survived the guarded sweep (%d rows), want 0", countC)
	}
}

// Non-vacuity: revert to the pre-fix shape (unconditional DELETE on
// session_id+token, blind mirror prune) and the refreshed row's disk
// count drops to 0 / the mirror entry disappears — the deny assertions
// above fail.
