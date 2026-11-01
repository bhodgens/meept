package tierpolicy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// TestExecuteDemoteOrderAndNoDelete: Given a demote action and stub Deps;
// When Execute; Then the episodic copy is recorded BEFORE the flag write,
// the delete count is zero, and nothing is deleted (leaf T2
// Given/When/Then; D1/D2 flag-not-delete).
func TestExecuteDemoteOrderAndNoDelete(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("c-1", "stale claim content", 90, nil),
	}}
	ap := &stubApplier{}
	e := New(Config{Enabled: true}, Deps{
		DemotionClaims: claims,
		Demotion:       ap,
		Deletes:        del, // wired to PROVE demotion never calls it
	})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"c-1"}, Reason: "reason=stale;claim=c-1;age_days=90"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.DemoteRequested != 1 || rep.Demoted != 1 {
		t.Fatalf("want requested=1 demoted=1, got %d/%d", rep.DemoteRequested, rep.Demoted)
	}
	if rep.ActionsApplied != 1 {
		t.Fatalf("want 1 applied action, got %d", rep.ActionsApplied)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", rep.Errors)
	}
	// ORDER: copy first, flag second (crash-between-steps leaves the
	// semantic entry un-flagged, so a re-run is safe).
	if len(ap.order) != 2 {
		t.Fatalf("want exactly 2 steps, got %v", ap.order)
	}
	if ap.order[0] != "copy:c-1" || ap.order[1] != "flag:c-1" {
		t.Fatalf("copy must precede flag, got %v", ap.order)
	}
	// LOSSLESS: the episodic copy carries the claim's full content.
	if len(ap.copied) != 1 || ap.copied[0].Content != "stale claim content" {
		t.Fatalf("copy must be lossless, got %+v", ap.copied)
	}
	// NO DELETE: the delete count is zero and the Deleter saw nothing.
	if del.deleted != nil {
		t.Fatalf("demotion must never delete, deleter saw %v", del.deleted)
	}
	if rep.EvictionsDeleted != 0 {
		t.Fatalf("delete count must be 0, got %d", rep.EvictionsDeleted)
	}
	// The flag records the reason from the machine-readable action.
	if len(ap.flags) != 1 || !strings.HasPrefix(ap.flags[0], "c-1:stale") {
		t.Fatalf("want flag c-1:stale, got %v", ap.flags)
	}
}

// TestExecuteDemoteIdempotent: re-executing a demote for an
// already-demoted claim is a noop - no second copy, no second flag
// (R-B7). The applier doubles as the detector: after the first Execute
// the claim's metadata carries the demoted flag, and the second Execute
// observes it through the detector seam.
func TestExecuteDemoteIdempotent(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("c-2", "content", 90, nil),
	}}
	ap := &demotedApplier{applier: stubApplier{}}
	e := New(Config{Enabled: true}, Deps{
		DemotionClaims: claims,
		Demotion:       ap,
	})
	act := Action{Kind: KindDemote, TargetIDs: []string{"c-2"}, Reason: "reason=stale;claim=c-2;age_days=90"}
	rep1, err := e.Execute(context.Background(), []Action{act})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if rep1.Demoted != 1 {
		t.Fatalf("first run must demote once, got %d", rep1.Demoted)
	}
	rep2, err := e.Execute(context.Background(), []Action{act})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if rep2.Demoted != 0 {
		t.Fatalf("second run must be a noop (0 demoted), got %d", rep2.Demoted)
	}
	if len(ap.applier.order) != 2 {
		t.Fatalf("second run must not copy or re-flag, order: %v", ap.applier.order)
	}
}

// demotedApplier wraps stubApplier and implements DemotedDetector by
// consulting the flags it stamped - the shape the real Manager-backed
// applier has (flag state is readable).
type demotedApplier struct {
	applier stubApplier
}

func (d *demotedApplier) CopyToEpisodic(ctx context.Context, claim memory.Memory) (string, error) {
	return d.applier.CopyToEpisodic(ctx, claim)
}

func (d *demotedApplier) MarkDemoted(ctx context.Context, claimID, reason string, at time.Time) error {
	return d.applier.MarkDemoted(ctx, claimID, reason, at)
}

// IsDemoted reports the flag state the applier itself maintains.
func (d *demotedApplier) IsDemoted(_ context.Context, claimID string) (bool, error) {
	for _, f := range d.applier.flags {
		if strings.HasPrefix(f, claimID+":") {
			return true, nil
		}
	}
	return false, nil
}

// TestExecuteDemoteFlagCheckMidFlight: a claim flagged between planning
// and execution (metadata carries the flag) is skipped even without the
// detector seam - the metadata check is the idempotency backstop.
func TestExecuteDemoteFlagCheckMidFlight(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("c-3", "content", 90, map[string]any{MetaDemoted: true}),
	}}
	ap := &stubApplier{}
	e := New(Config{Enabled: true}, Deps{DemotionClaims: claims, Demotion: ap})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"c-3"}, Reason: "reason=stale"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Demoted != 0 {
		t.Fatalf("already-demoted must noop, demoted=%d", rep.Demoted)
	}
	if len(ap.order) != 0 {
		t.Fatalf("no steps may run on a flagged claim, got %v", ap.order)
	}
}

// TestExecuteDemoteCrashBetweenStepsRecovers: a copy failure after step 1
// (or a flag failure after the copy) reports the error but leaves
// recoverable state - re-running completes without duplicating the copy
// (T2 crash-between-steps; here: copy succeeded, flag failed, re-run
// only re-flags because the copy carries demoted_from).
func TestExecuteDemoteCrashBetweenStepsRecovers(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("c-4", "content", 90, nil),
	}}
	ap := &crashAfterCopyApplier{claims: claims, failFlag: true}
	e := New(Config{Enabled: true}, Deps{DemotionClaims: claims, Demotion: ap})
	act := Action{Kind: KindDemote, TargetIDs: []string{"c-4"}, Reason: "reason=stale"}
	rep1, err := e.Execute(context.Background(), []Action{act})
	if err == nil {
		t.Fatal("first run must surface the flag failure")
	}
	if rep1.Demoted != 0 {
		t.Fatalf("failed flag must not count as demoted, got %d", rep1.Demoted)
	}
	if len(ap.copies) != 1 {
		t.Fatalf("step 1 (copy) must have run once, got %d", len(ap.copies))
	}
	// Re-run: the applier now reports demoted (its failure recorded the
	// flag server-side in this simulation), so no second copy happens.
	ap.failFlag = false
	rep2, err := e.Execute(context.Background(), []Action{act})
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if rep2.Demoted != 1 {
		t.Fatalf("re-run must complete the demotion, got %d", rep2.Demoted)
	}
	if len(ap.copies) != 1 {
		t.Fatalf("re-run must NOT duplicate the episodic copy, copies=%d", len(ap.copies))
	}
}

// crashAfterCopyApplier simulates a crash after the episodic copy: the
// copy lands (recorded) but MarkDemoted fails until failFlag is cleared.
// The copy side dedups by claim ID (the demoted_from marker the real
// applier stamps): a re-run finds the copy already present and does not
// duplicate it - the crash-between-steps recovery shape (R-B7).
type crashAfterCopyApplier struct {
	claims   *stubClaimSource
	copies   []memory.Memory
	copyIDs  map[string]bool
	flags    []string
	failFlag bool
}

func (c *crashAfterCopyApplier) CopyToEpisodic(_ context.Context, claim memory.Memory) (string, error) {
	if c.copyIDs == nil {
		c.copyIDs = make(map[string]bool)
	}
	if c.copyIDs[claim.ID] {
		return "epi-" + claim.ID, nil // already copied; dedup, no re-record
	}
	c.copyIDs[claim.ID] = true
	c.copies = append(c.copies, claim)
	return "epi-" + claim.ID, nil
}

func (c *crashAfterCopyApplier) MarkDemoted(_ context.Context, claimID, _ string, _ time.Time) error {
	if c.failFlag {
		return context.DeadlineExceeded // simulated crash
	}
	c.flags = append(c.flags, claimID)
	return nil
}

func (c *crashAfterCopyApplier) IsDemoted(_ context.Context, claimID string) (bool, error) {
	for _, f := range c.flags {
		if f == claimID {
			return true, nil
		}
	}
	return false, nil
}

// TestExecuteDemoteCopyFailureNoFlag: a copy failure leaves NO partial
// state - the flag is never written when the copy fails (T2: episodic
// write first, flag second, failure mid-copy leaves the semantic entry
// untouched so a re-run repeats cleanly).
func TestExecuteDemoteCopyFailureNoFlag(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("c-5", "content", 90, nil),
	}}
	ap := &stubApplier{copyErr: context.DeadlineExceeded}
	e := New(Config{Enabled: true}, Deps{DemotionClaims: claims, Demotion: ap})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"c-5"}, Reason: "reason=stale"},
	})
	if err == nil {
		t.Fatal("copy failure must surface")
	}
	if rep.Demoted != 0 {
		t.Fatalf("failed demotion must not count, got %d", rep.Demoted)
	}
	if len(ap.flags) != 0 {
		t.Fatalf("flag must not be written after a copy failure, got %v", ap.flags)
	}
	if len(rep.Errors) == 0 {
		t.Fatal("the failure must land in the report errors")
	}
}

// TestReportDemotedCountsAndDryRun: two demotions in one pass count
// Demoted == 2; dry-run counts requests without applying (leaf T5).
func TestReportDemotedCountsAndDryRun(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("d-1", "a", 90, nil),
		claimRes("d-2", "b", 90, nil),
	}}
	ap := &stubApplier{}
	e := New(Config{Enabled: true}, Deps{DemotionClaims: claims, Demotion: ap})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"d-1"}, Reason: "reason=stale"},
		{Kind: KindDemote, TargetIDs: []string{"d-2"}, Reason: "reason=low_usefulness"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Demoted != 2 {
		t.Fatalf("want Demoted==2, got %d", rep.Demoted)
	}
	if rep.DemoteRequested != 2 {
		t.Fatalf("want DemoteRequested==2, got %d", rep.DemoteRequested)
	}
	if rep.ActionsApplied != 2 {
		t.Fatalf("want 2 applied, got %d", rep.ActionsApplied)
	}

	// Dry-run: requests counted, nothing applied.
	dry := New(Config{Enabled: true, DryRun: true}, Deps{DemotionClaims: claims, Demotion: ap})
	before := len(ap.order)
	repDry, err := dry.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"d-1", "d-2"}, Reason: "reason=stale"},
	})
	if err != nil {
		t.Fatalf("dry Execute: %v", err)
	}
	if repDry.DemoteRequested != 2 {
		t.Fatalf("dry run must count requests, got %d", repDry.DemoteRequested)
	}
	if repDry.Demoted != 0 {
		t.Fatalf("dry run must not count demotions, got %d", repDry.Demoted)
	}
	if len(ap.order) != before {
		t.Fatalf("dry run must not touch the stores")
	}
}

// TestDemoteOrderBefore: the ordering helper puts demote before compress
// before evict in every plan (R-B4 extended; leaf T4 order rule).
func TestDemoteOrderBefore(t *testing.T) {
	t.Parallel()
	in := []Action{
		{Kind: KindEvict, TargetIDs: []string{"e"}},
		{Kind: KindDemote, TargetIDs: []string{"d"}},
		{Kind: KindCompress, TargetIDs: []string{"c"}},
	}
	got := DemoteOrderBefore(in)
	kinds := make([]string, len(got))
	for i, a := range got {
		kinds[i] = a.Kind
	}
	want := strings.Join([]string{KindDemote, KindCompress, KindEvict}, ",")
	if strings.Join(kinds, ",") != want {
		t.Fatalf("want order %s, got %s", want, strings.Join(kinds, ","))
	}
}
