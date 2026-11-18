package voice

// Test-support adapter (leaf 04): the 00T gate fake (fakeCommitmentFiler
// in brief_commitments_test.go) predates the production CommitmentFiler
// seam — it records rows through its own unexported file/count methods.
// This file adapts the fake ONTO the seam so the gate suite compiles and
// pins the exactly-once rule through the real production entry points
// (FileCommitments / FileCommitmentsFromBrief). No gate test is edited;
// prod builds never see this file, so the seam stays clean.

import (
	"context"
)

// Compile-time proof the adapted fake satisfies the production seam.
var _ CommitmentFiler = (*fakeCommitmentFiler)(nil)

// FileCommitment adapts a seam row onto the gate fake's recorder.
func (f *fakeCommitmentFiler) FileCommitment(_ context.Context, row CommitmentRow) error {
	return f.file(commitmentRow(row))
}

// HasCommitment is the dedupe query: a row for (ccid, index) already
// recorded by this filer counts as filed (the exactly-once check the
// production adapter runs against the ledger).
func (f *fakeCommitmentFiler) HasCommitment(_ context.Context, ccid string, index int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.filed {
		if r.SourceRef == ccid && r.Index == index {
			return true, nil
		}
	}
	return false, nil
}
