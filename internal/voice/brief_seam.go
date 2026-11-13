package voice

// CommitmentFiler seam (leaf 04, contract C7). internal/voice must not
// import internal/ledger (existing repo posture): the seam interface lives
// here, the real ledger-append adapter lives in internal/daemon wiring.
// FileCommitments / FileCommitmentsFromBrief implement the C7
// exactly-once rule per (ccid, commitment index) against the filer's own
// dedupe query.

import (
	"context"
	"fmt"
)

// CommitmentRow mirrors the C7 ledger row the adapter must produce: one
// ledger.Append(kind=commitment, source=voice, source_ref=ccid) per
// commitment, subject = the commitment text, payload carrying the
// commitment index (exactly-once per (ccid, index)).
type CommitmentRow struct {
	Kind      string
	Source    string
	SourceRef string // ccid
	Subject   string // the commitment text as resolved subject
	Index     int    // commitment index within the brief
}

// CommitmentFiler files commitments into the employee ledger. The
// internal/daemon adapter maps this onto ledger.Store.Append with the
// frozen C7 field mapping (CommitmentRow); HasCommitment is the dedupe
// query (existing source_ref rows for this ccid before append).
type CommitmentFiler interface {
	// FileCommitment appends one row for (ccid, index, commitment).
	FileCommitment(ctx context.Context, row CommitmentRow) error
	// HasCommitment reports whether a row for (ccid, index) already exists.
	HasCommitment(ctx context.Context, ccid string, index int) (bool, error)
}

// FileCommitments files each commitment of a finalized brief as one row,
// deduped exactly-once per (ccid, commitment index): the filer is queried
// for existing rows before each append, so a re-finalize (replayed hangup,
// release fan-out race, restart) files zero new rows.
func FileCommitments(ctx context.Context, filer CommitmentFiler, ccid string, commitments []string) error {
	if filer == nil || ccid == "" || len(commitments) == 0 {
		return nil
	}
	for i, c := range commitments {
		if err := fileCommitmentOnce(ctx, filer, ccid, i, c); err != nil {
			return err
		}
	}
	return nil
}

// FileCommitmentsFromBrief is the C7 entry point on a finalized brief:
// only a COMPLETED brief files commitments (a failed, abandoned, or
// in_progress call promised nothing verifiable — frozen leaf rule).
func FileCommitmentsFromBrief(ctx context.Context, filer CommitmentFiler, ccid string, b *CallBrief) error {
	if b == nil || b.Status != BriefStatusCompleted {
		return nil
	}
	return FileCommitments(ctx, filer, ccid, b.Brief.Commitments)
}

// fileCommitmentOnce runs the dedupe check then the append for one index.
func fileCommitmentOnce(ctx context.Context, filer CommitmentFiler, ccid string, index int, commitment string) error {
	exists, err := filer.HasCommitment(ctx, ccid, index)
	if err != nil {
		return fmt.Errorf("voice commitments: dedupe query for %s idx %d: %w", ccid, index, err)
	}
	if exists {
		return nil
	}
	row := CommitmentRow{
		Kind:      "commitment",
		Source:    "voice",
		SourceRef: ccid,
		Subject:   commitment,
		Index:     index,
	}
	if err := filer.FileCommitment(ctx, row); err != nil {
		return fmt.Errorf("voice commitments: file %s idx %d: %w", ccid, index, err)
	}
	return nil
}
