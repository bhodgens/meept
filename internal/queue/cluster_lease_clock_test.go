package queue

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"log/slog"
)

// A3-12 pins: cluster lease expiry must compare PARSED times, never
// the raw strings. timeout_at is written RFC3339Nano ("...:05.123Z")
// while the sweeper's now was RFC3339 ("...:05Z"): '.'(0x2E) < 'Z'
// (0x5A), so a LIVE sub-second lease in the current second sorted
// lexically BEFORE now and got reclaimed up to a second early. The
// fix parses both sides; reads stay backward-compatible with
// seconds-granularity rows written by the old code.

func seedClaimedJob(t *testing.T, store *Store, id, node, timeoutAt string) {
	t.Helper()
	if err := store.Insert(&Job{
		ID: id, Type: JobTypeOneOff, State: StatePending, Priority: PriorityNormal,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, err := store.db.Exec(`
		UPDATE jobs SET state = 'claimed', claimed_by = 'w', claimed_by_node = ?, timeout_at = ?
		WHERE id = ?`, node, timeoutAt, id)
	if err != nil {
		t.Fatalf("seed claim: %v", err)
	}
}

func TestReclaimRemoteStaleSameSecondSubSecondLeaseNotStolen(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	store, err := NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	base := time.Now().UTC().Truncate(time.Second)
	// LIVE lease: expiry 950ms into the CURRENT second. String compare
	// (the bug): "...:SS.95Z" < "...:SSZ" -> "expired" -> reclaimed
	// while the owning node is still working. Parsed compare: 950ms
	// short of expiry -> left alone. NON-VACUITY: revert
	// ReclaimRemoteStale to the SQL string predicate and this test
	// fails with reclaimed=[live-frac] ~999 of 1000 runs (the only
	// escape is the sweep crossing the second boundary mid-test).
	live := base.Add(950 * time.Millisecond).Format(time.RFC3339Nano)
	seedClaimedJob(t, store, "live-frac", "node-b", live)

	reclaimed, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	for _, id := range reclaimed {
		if id == "live-frac" {
			t.Fatalf("sub-second live lease reclaimed (%q vs wall now) - string-compare bug (A3-12)", live)
		}
	}

	// Backward-compatible reads + honest expiry: a genuinely expired
	// lease with a fraction (fractional shape from the nano writer)
	// IS reclaimed...
	expFrac := base.Add(-5*time.Second + 123*time.Millisecond).Format(time.RFC3339Nano)
	seedClaimedJob(t, store, "dead-frac", "node-b", expFrac)
	// ...and so is a legacy seconds-granularity expired row (the old
	// RFC3339 writer's shape).
	expLegacy := base.Add(-30 * time.Second).Format(time.RFC3339)
	seedClaimedJob(t, store, "dead-legacy", "node-b", expLegacy)
	// A legacy-FUTURE lease must also survive.
	legacyFuture := base.Add(30 * time.Second).Format(time.RFC3339)
	seedClaimedJob(t, store, "live-legacy", "node-b", legacyFuture)

	reclaimed2, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("reclaim 2: %v", err)
	}
	got := map[string]bool{}
	for _, id := range reclaimed2 {
		got[id] = true
	}
	if !got["dead-frac"] || !got["dead-legacy"] {
		t.Fatalf("expired leases not reclaimed: %v", reclaimed2)
	}
	if got["live-legacy"] || got["live-frac"] {
		t.Fatalf("live leases reclaimed: %v", reclaimed2)
	}

	// Local node's own claims are never touched (ReclaimIfStale owns them).
	reclaimed3, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(reclaimed3) != 0 {
		t.Fatalf("second sweep reclaimed already-reset or local rows: %v", reclaimed3)
	}
}

// TestReclaimRemoteStaleUnparseableFailsClosed: a lease timestamp in
// an unreadable vocabulary must NEVER be treated as expired - failing
// closed protects live work (matches Flag-3 posture elsewhere).
func TestReclaimRemoteStaleUnparseableFailsClosed(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	store, err := NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedClaimedJob(t, store, "weird", "node-b", "not-a-timestamp")
	reclaimed, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	for _, id := range reclaimed {
		if id == "weird" {
			t.Fatal("unparseable lease reclaimed - must fail closed")
		}
	}
}

func TestParseLeaseStampVocabularies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"2026-11-25T12:00:05Z", true},           // RFC3339 (legacy writes)
		{"2026-11-25T12:00:05.123Z", true},       // RFC3339Nano (current writes)
		{"2026-11-25T12:00:05.000000000Z", true}, // explicit zero fraction
		{"2026-11-25T12:00:05.123+02:00", true},  // offset form
		{"2026-11-25 12:00:05.123+00:00", true},  // spaced sqlite-ish form
		{"", false},
		{"0001-01-01T00:00:00Zjunk", false},
	}
	for _, tc := range cases {
		_, ok := parseLeaseStamp(tc.in)
		if ok != tc.want {
			t.Errorf("parseLeaseStamp(%q) ok = %v, want %v", tc.in, ok, tc.want)
		}
	}
}
