package telnyx

// 01-webhook-hardening/00T §N — NonceStore gate tests.
//
// Pins the frozen contract (master.md §1 item 1):
//
//	type NonceStore struct{ ... }               // in-process TTL+LRU, NO Redis (D2)
//	func NewNonceStore(ttl time.Duration, max int) *NonceStore // ttl=6m, max=10000
//	func (n *NonceStore) CheckFresh(eventID string) bool       // true=fresh, records
//	func (n *NonceStore) Release(eventID string)               // free for provider redelivery
//
// Adapted from strategist-usa go-web-hooks internal/webhook/signature.go
// CheckReplay/ReleaseReplay (Redis SETNX + DEL; the release-makes-
// redelivery-re-process contract is telnyx_signature_matrix_test.go:118-142
// TestTelnyxReplayNonce_KeyFormatMatchesConsumerRelease). meept has no
// Redis, so the TTL+LRU accounting is in-process.
//
// No clock seam is required: the TTL test runs against wall time with a
// generous margin so the implementation needs no exported time hook.

import (
	"strconv"
	"testing"
	"time"
)

func TestNonceStore_FreshThenDuplicate(t *testing.T) {
	n := NewNonceStore(6*time.Minute, 10000)
	if !n.CheckFresh("evt-1") {
		t.Fatal("first delivery must be fresh (CheckFresh true)")
	}
	if n.CheckFresh("evt-1") {
		t.Fatal("second delivery of the same event id must be false (replay)")
	}
	// A different event id is independent.
	if !n.CheckFresh("evt-2") {
		t.Fatal("distinct event id must be fresh")
	}
}

func TestNonceStore_ReleaseFreesForRedelivery(t *testing.T) {
	// Ported release semantics: strategist matrix test :126-142 —
	// Release must remove exactly what CheckFresh recorded, so the
	// provider redelivery (after a terminal consumer failure) is
	// re-processed, not rejected.
	n := NewNonceStore(6*time.Minute, 10000)
	if !n.CheckFresh("evt-rel") {
		t.Fatal("first delivery must be fresh")
	}
	if n.CheckFresh("evt-rel") {
		t.Fatal("in-window redelivery must be rejected before release")
	}
	n.Release("evt-rel")
	if !n.CheckFresh("evt-rel") {
		t.Fatal("redelivery after Release must be re-processed (fresh)")
	}
	// And re-recorded: another dup is rejected again.
	if n.CheckFresh("evt-rel") {
		t.Fatal("after Release+re-check the nonce must be held again")
	}
}

func TestNonceStore_ReleaseUnknownIsNoop(t *testing.T) {
	n := NewNonceStore(6*time.Minute, 10000)
	// Must not panic and must not make a never-seen id "unfresh".
	n.Release("never-seen")
	if !n.CheckFresh("never-seen") {
		t.Fatal("Release of an unknown id must not pre-block a later first delivery")
	}
}

func TestNonceStore_TTLExpiry(t *testing.T) {
	// Wall-time TTL: the nonce expires after ttl, exactly like the Redis
	// key expiry the strategist port replaces. Margins are loose enough
	// to be CI-stable without a clock seam.
	const ttl = 60 * time.Millisecond
	n := NewNonceStore(ttl, 10000)
	if !n.CheckFresh("evt-ttl") {
		t.Fatal("first delivery must be fresh")
	}
	if n.CheckFresh("evt-ttl") {
		t.Fatal("immediate redelivery must be rejected")
	}
	time.Sleep(150 * time.Millisecond)
	if !n.CheckFresh("evt-ttl") {
		t.Fatal("delivery after TTL expiry must be fresh again (nonce expired)")
	}
}

func TestNonceStore_LRUEvictionBound(t *testing.T) {
	// The store caps at max entries with LRU eviction (the Redis-size
	// guard meept keeps in-process). max=4 keeps the test fast and
	// exact.
	const max = 4
	n := NewNonceStore(time.Hour, max)
	ids := []string{"e0", "e1", "e2", "e3"}
	for _, id := range ids {
		if !n.CheckFresh(id) {
			t.Fatalf("first delivery of %s must be fresh", id)
		}
	}
	// Touch e0 (a dup hit marks it recently used), then push e4 — the
	// LRU entry is now e1.
	if n.CheckFresh("e0") {
		t.Fatal("e0 dup within window must be rejected")
	}
	if !n.CheckFresh("e4") {
		t.Fatal("e4 must be admitted (evicting LRU e1), fresh")
	}
	if !n.CheckFresh("e1") {
		t.Fatal("LRU entry e1 must have been evicted (fresh again)")
	}
	for _, id := range []string{"e0", "e2", "e3", "e4"} {
		if n.CheckFresh(id) {
			t.Errorf("entry %s must still be held (only the LRU entry was evicted)", id)
		}
	}
}

func TestNonceStore_ConcurrentCheckFresh(t *testing.T) {
	// The webhook handler calls CheckFresh/Release from HTTP goroutines;
	// the store must be safe under -race with exactly one winner per id.
	n := NewNonceStore(time.Minute, 1000)
	const goroutines = 8
	results := make(chan bool, goroutines)
	for range goroutines {
		go func() { results <- n.CheckFresh("evt-race") }()
	}
	fresh := 0
	for range goroutines {
		if <-results {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("CheckFresh admitted %d of %d concurrent first deliveries, want exactly 1", fresh, goroutines)
	}
	// Release concurrent with checks must not panic either.
	done := make(chan struct{})
	go func() {
		for i := range 100 {
			n.Release("evt-race-" + strconv.Itoa(i))
		}
		close(done)
	}()
	for i := range 100 {
		n.CheckFresh("evt-race-" + strconv.Itoa(i))
	}
	<-done
}
