package voice

// Gate tests for contract C7 (master.md, quoted verbatim):
//
//	C7 Ledger commitment filing: brief commitments file as
//	ledger.Append(kind='commitment', source='voice', source_ref=ccid,
//	subject=<resolved subject>); exactly-once per (ccid,
//	commitment-index) — dedupe by querying existing source_ref rows
//	before append.
//
// POSTURE (leaf 04): internal/voice must not import internal/ledger; the
// CommitmentFiler seam interface lives in internal/voice (brief_seam.go)
// and the filing adapter in internal/daemon wiring. These tests pin the
// seam + the exactly-once rule at the voice layer using an in-memory
// fake filer; the daemon-side integration pins the real Append mapping.
// RED until leaf 04 (CommitmentFiler / FileCommitments undefined).

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// fakeCommitmentFiler records every Append-shaped call so tests pin the
// C7 field mapping and the exactly-once dedupe. Structurally satisfies
// the CommitmentFiler seam.
type fakeCommitmentFiler struct {
	mu    sync.Mutex
	filed []commitmentRow
	failN int // first N calls fail, to prove dedupe survives an error
}

// commitmentRow mirrors the C7 Append mapping the seam must carry.
type commitmentRow struct {
	Kind      string
	Source    string
	SourceRef string // ccid
	Subject   string // the commitment text as resolved subject
	Index     int    // commitment index within the brief
}

func (f *fakeCommitmentFiler) file(row commitmentRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failN > 0 {
		f.failN--
		return fmt.Errorf("ledger append failed (simulated)")
	}
	f.filed = append(f.filed, row)
	return nil
}

func (f *fakeCommitmentFiler) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.filed)
}

// TestCommitmentsFileTwoRows pins the base mapping: GIVEN a finalized
// brief with 2 commitments; WHEN filing runs; THEN exactly 2 rows are
// filed, each kind=commitment, source=voice, source_ref=ccid, subject=
// the commitment text.
func TestCommitmentsFileTwoRows(t *testing.T) {
	filer := &fakeCommitmentFiler{}
	ccid := "cc-commit-1"
	commitments := []string{"send the final invoice", "schedule the inspection"}
	// FileCommitments is the leaf-04 seam entry: it dedupes per
	// (ccid, commitment index) against the filer's existing rows.
	if err := FileCommitments(context.Background(), filer, ccid, commitments); err != nil {
		t.Fatalf("FileCommitments: %v", err)
	}
	if filer.count() != 2 {
		t.Fatalf("filed %d rows, want exactly 2", filer.count())
	}
	filer.mu.Lock()
	defer filer.mu.Unlock()
	for i, row := range filer.filed {
		if row.Kind != "commitment" {
			t.Errorf("row %d kind = %q, want commitment", i, row.Kind)
		}
		if row.Source != "voice" {
			t.Errorf("row %d source = %q, want voice", i, row.Source)
		}
		if row.SourceRef != ccid {
			t.Errorf("row %d source_ref = %q, want ccid %q", i, row.SourceRef, ccid)
		}
		if row.Subject != commitments[i] {
			t.Errorf("row %d subject = %q, want the commitment %q", i, row.Subject, commitments[i])
		}
	}
}

// TestCommitmentsRefinalizeStillTwo pins exactly-once per (ccid, index):
// re-running the filing for the SAME brief (replayed hangup, release
// fan-out race, restart) must file ZERO new rows — dedupe by querying
// existing (source=voice, source_ref=ccid) rows before append.
func TestCommitmentsRefinalizeStillTwo(t *testing.T) {
	filer := &fakeCommitmentFiler{}
	ccid := "cc-commit-2"
	commitments := []string{"call the customer back", "order materials"}
	if err := FileCommitments(context.Background(), filer, ccid, commitments); err != nil {
		t.Fatalf("first filing: %v", err)
	}
	if err := FileCommitments(context.Background(), filer, ccid, commitments); err != nil {
		t.Fatalf("re-finalize filing: %v", err)
	}
	if got := filer.count(); got != 2 {
		t.Fatalf("after re-finalize filed %d rows, want still exactly 2 (dedupe failed)", got)
	}
}

// TestCommitmentsZeroCommitmentsZeroRows pins the empty case: a brief
// with no commitments files zero ledger rows (no placeholder, no error).
func TestCommitmentsZeroCommitmentsZeroRows(t *testing.T) {
	filer := &fakeCommitmentFiler{}
	if err := FileCommitments(context.Background(), filer, "cc-commit-3", nil); err != nil {
		t.Fatalf("FileCommitments(nil): %v", err)
	}
	if err := FileCommitments(context.Background(), filer, "cc-commit-4", []string{}); err != nil {
		t.Fatalf("FileCommitments(empty): %v", err)
	}
	if got := filer.count(); got != 0 {
		t.Fatalf("filed %d rows for a commitment-free brief, want 0", got)
	}
}

// TestCommitmentsDedupePerCallID pins per-call scoping: commitments of a
// DIFFERENT ccid must not suppress filing for this ccid (dedupe key is
// (source_ref=ccid, index), not the commitment text alone).
func TestCommitmentsDedupePerCallID(t *testing.T) {
	filer := &fakeCommitmentFiler{}
	text := "email the quote"
	if err := FileCommitments(context.Background(), filer, "cc-a", []string{text}); err != nil {
		t.Fatalf("filing cc-a: %v", err)
	}
	if err := FileCommitments(context.Background(), filer, "cc-b", []string{text}); err != nil {
		t.Fatalf("filing cc-b: %v", err)
	}
	if got := filer.count(); got != 2 {
		t.Fatalf("filed %d rows across two calls, want 2 (one per ccid)", got)
	}
}

// TestCommitmentsFileOnlyOnCompleted pins the leaf-04 rule: filing runs
// for a finalized COMPLETED brief only — failed/abandoned/in_progress
// briefs never file commitments (a reaped or failed call promised
// nothing verifiable).
func TestCommitmentsFileOnlyOnCompleted(t *testing.T) {
	filer := &fakeCommitmentFiler{}
	for _, status := range []string{"failed", "abandoned", "in_progress"} {
		b := briefFixture
		b.Status = status
		if err := FileCommitmentsFromBrief(context.Background(), filer, "cc-gate", &b); err != nil {
			t.Fatalf("filing %s brief: %v", status, err)
		}
	}
	if got := filer.count(); got != 0 {
		t.Fatalf("non-completed briefs filed %d commitment rows, want 0", got)
	}
	// The completed variant files.
	b := briefFixture // status completed
	if err := FileCommitmentsFromBrief(context.Background(), filer, "cc-gate", &b); err != nil {
		t.Fatalf("filing completed brief: %v", err)
	}
	if got := filer.count(); got != len(briefFixture.Brief.Commitments) {
		t.Fatalf("completed brief filed %d rows, want %d", got, len(briefFixture.Brief.Commitments))
	}
}
