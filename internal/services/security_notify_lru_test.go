package services

// P3 (bughunt 2026-11-24) pins: the channel-B dedup ledger (seenPush) is a
// bounded LRU, not an unbounded map. Alert-delivery semantics are otherwise
// byte-identical:
//
//   - same alert twice publishes exactly one security.alert event;
//   - cap+1 distinct alerts keep len(seenPush) <= seenPushCap and evict the
//     oldest entry, so re-firing the oldest AFTER eviction publishes again
//     (accepted LRU consequence — bounded memory replaces dedup-forever).

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// waitForAlert reads bus events until one decodes to the wanted message
// (or the deadline passes).
func waitForAlert(sub *bus.Subscriber, message string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-sub.Channel:
			var ev SecurityAlertEvent
			if err := json.Unmarshal(msg.Payload, &ev); err == nil && ev.Message == message {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// SEC-LRU-1: same alert twice publishes exactly one security.alert event
// (dedup semantics preserved by the LRU swap).
func TestSECLRU_1_DuplicateSuppressed(t *testing.T) {
	b := bus.New(nil, testLogger())
	t.Cleanup(b.Close)
	sub := b.Subscribe("p3-dup", TopicSecurityAlert.Name)
	sn := NewSecurityNotifier("push", b, testLogger())

	sn.Notify("dup-message", "mcp")
	if !waitForAlert(sub, "dup-message", 2*time.Second) {
		t.Fatal("first alert did not publish")
	}
	sn.Notify("dup-message", "mcp")
	if waitForAlert(sub, "dup-message", 300*time.Millisecond) {
		t.Error("duplicate alert published a second notification, want exactly 1")
	}
}

// SEC-LRU-2: fill cap+1 distinct alerts — the ledger stays bounded
// (len <= seenPushCap), map and list agree, the OLDEST entry is gone, and
// re-firing the evicted oldest DOES notify again. That re-notify is pinned
// as ACCEPTED behavior: bounded memory replaces dedup-forever.
func TestSECLRU_2_CapEvictsOldest(t *testing.T) {
	b := bus.New(nil, testLogger())
	t.Cleanup(b.Close)
	sn := NewSecurityNotifier("push", b, testLogger())

	for i := 0; i < seenPushCap+1; i++ {
		sn.Notify(fmt.Sprintf("storm-%d", i), "mcp")
	}

	sn.seenMu.Lock()
	size := len(sn.seenPush)
	orderLen := sn.seenOrder.Len()
	_, oldestPresent := sn.seenPush["mcp|storm-0"]
	sn.seenMu.Unlock()
	if size > seenPushCap {
		t.Errorf("ledger size = %d, want <= %d", size, seenPushCap)
	}
	if orderLen != size {
		t.Errorf("seenOrder len = %d, map len = %d, want equal (map/list drift leaks or mis-evicts)", orderLen, size)
	}
	if oldestPresent {
		t.Fatal("oldest key survived eviction; the accepted re-notify pin below would be vacuous")
	}

	// Fresh subscriber: only post-fill publishes can land here.
	post := b.Subscribe("p3-post", TopicSecurityAlert.Name)

	// Re-fire the evicted oldest: it notifies again (accepted behavior).
	sn.Notify("storm-0", "mcp")
	if !waitForAlert(post, "storm-0", 2*time.Second) {
		t.Error("re-fire of evicted oldest did not notify; accepted LRU re-notify behavior broken")
	}

	// A key still inside the window is still deduped. storm-500 is safe:
	// the fill evicted only storm-0, and the storm-0 re-fire insert evicted
	// the next-oldest (storm-1) — storm-500 was never touched.
	sn.Notify("storm-500", "mcp")
	if waitForAlert(post, "storm-500", 300*time.Millisecond) {
		t.Error("re-fire of in-window key notified; dedup broken")
	}
}

// SEC-LRU-3: a suppressed re-fire refreshes recency — the touched oldest key
// survives the next eviction; the untouched next-oldest is evicted instead.
func TestSECLRU_3_TouchRefreshesRecency(t *testing.T) {
	b := bus.New(nil, testLogger())
	t.Cleanup(b.Close)
	sn := NewSecurityNotifier("push", b, testLogger())

	for i := 0; i < seenPushCap; i++ {
		sn.Notify(fmt.Sprintf("k-%d", i), "s")
	}
	// Re-fire k-0: suppressed, but the LRU touch moves it to the front.
	sn.Notify("k-0", "s")
	// Overflow insert evicts the least-recently-seen key (k-1), not k-0.
	sn.Notify("new-key", "s")

	sn.seenMu.Lock()
	_, touchedPresent := sn.seenPush["s|k-0"]
	_, evictedPresent := sn.seenPush["s|k-1"]
	size := len(sn.seenPush)
	sn.seenMu.Unlock()
	if size != seenPushCap {
		t.Errorf("ledger size = %d, want exactly %d", size, seenPushCap)
	}
	if !touchedPresent {
		t.Error("touched key evicted; LRU recency touch not applied")
	}
	if evictedPresent {
		t.Error("untouched older key survived; eviction is not LRU-ordered")
	}
}
