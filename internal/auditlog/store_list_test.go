package auditlog

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestStore_List pins the read-only chain listing added for the activity
// aggregator: ascending seq order, full Record shape, limit honored.
func TestStore_List(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := OpenStore(dir+"/audit.db", nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, Record{
			Type:       fmt.Sprintf("event.%d", i),
			EmployeeID: "emp-1",
			Payload:    map[string]any{"n": i, "action": "test.action"},
			At:         base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Unbounded: all three, ascending seq.
	got, err := store.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, rec := range got {
		if rec.Seq != uint64(i+1) {
			t.Errorf("got[%d].Seq = %d, want %d", i, rec.Seq, i+1)
		}
		if rec.Type != fmt.Sprintf("event.%d", i) {
			t.Errorf("got[%d].Type = %q", i, rec.Type)
		}
		if rec.Payload["action"] != "test.action" {
			t.Errorf("got[%d] payload not decoded: %v", i, rec.Payload)
		}
	}

	// Limit: first N only.
	limited, err := store.List(ctx, 2)
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 2 || limited[0].Seq != 1 || limited[1].Seq != 2 {
		t.Errorf("limited list wrong: %+v", limited)
	}

	// Empty chain: nil, no error.
	empty, err := OpenStore(dir+"/empty.db", nil)
	if err != nil {
		t.Fatalf("open empty store: %v", err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	recs, err := empty.List(ctx, 0)
	if err != nil || len(recs) != 0 {
		t.Errorf("empty chain: len/err = %d/%v, want 0/nil", len(recs), err)
	}
}
