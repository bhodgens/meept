package daemon

// Bughunt 2026-11-25 A5-4 pin: the commitment dedupe query (HasCommitment)
// used to scan the employee's OLDEST 1000 commitment rows (QueryLog is
// ORDER BY id ASC LIMIT n), so past 1000 rows every NEW (ccid, index) fell
// outside the window, dedupe answered false for genuine duplicates, and a
// replayed finalize appended duplicate commitment rows — the C7 exactly-once
// contract silently void. The fix scopes the query by the exact subject the
// filer writes ("call:<ccid>", covered by idx_ledger_emp_subject), so the
// window is bounded by ONE call's commitment count and can never be pushed
// out by unrelated history.

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/voice"
)

func TestHasCommitment_FindsRecentDuplicatePast1000OldRows(t *testing.T) {
	store, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	filer := &ledgerCommitmentFiler{store: store, employeeID: "emp-dedup"}

	// Seed >1000 OLD commitment rows for OTHER calls (each its own subject,
	// so the employee's commitment ROW count passes the old 1000 window).
	for i := range 1050 {
		payload, _ := json.Marshal(commitmentPayload{Idx: 0, Text: "old"})
		if _, err := store.Append(ctx, ledger.DraftRecord{
			EmployeeID: "emp-dedup",
			Kind:       ledger.KindCommitment,
			Subject:    "call:ccid-old",
			Payload:    payload,
			Source:     ledger.SourceVoice,
			SourceRef:  "ccid-old",
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// The RECENT duplicate: file once, then the dedupe probe must find it —
	// pre-fix the ASC/LIMIT-1000 window ended inside the old rows and the
	// probe answered false (fails OPEN, duplicate appended).
	row := voice.CommitmentRow{Kind: "commitment", Source: "voice", SourceRef: "ccid-new", Subject: "send the deck", Index: 3}
	if err := filer.FileCommitment(ctx, row); err != nil {
		t.Fatalf("file: %v", err)
	}
	found, err := filer.HasCommitment(ctx, "ccid-new", 3)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !found {
		t.Fatal("HasCommitment missed a RECENT duplicate beyond 1000 older rows (A5-4 fail-open)")
	}

	// Non-vacuity on the index axis: a different index for the same call is
	// NOT a duplicate.
	foundOther, err := filer.HasCommitment(ctx, "ccid-new", 4)
	if err != nil {
		t.Fatal(err)
	}
	if foundOther {
		t.Fatal("(ccid, idx=4) must not match the idx=3 row")
	}
	// And the old-rows call still probes true at its own index (window is
	// per-subject, never blind to any call).
	foundOld, err := filer.HasCommitment(ctx, "ccid-old", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !foundOld {
		t.Fatal("old call's own row must remain probeable")
	}
	// Empty ccid guard: never claims, never errors.
	if foundEmpty, err := filer.HasCommitment(ctx, "", 0); err != nil || foundEmpty {
		t.Fatalf("empty ccid = (%v, %v), want (false, nil)", foundEmpty, err)
	}
}
