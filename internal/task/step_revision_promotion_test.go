package task

// Regression tests for the 2026-09-07 revision-promotion finding: a revision
// step depends on its rejected original (CreateRevision appends original.ID),
// but PromoteReadySteps required successfully-terminal deps — so revisions
// were structurally unschedulable (stranded pending rows).

import "testing"

func TestStepStore_PromoteReadySteps_RevisionPastRejectedOriginal(t *testing.T) {
	store := newTestStepStore(t)

	original := NewTaskStep("task-rev-promo", "original work", 0)
	original.ID = "step-orig-revpromo"
	if err := store.Create(original); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := store.SetState(original.ID, StepRejected); err != nil {
		t.Fatalf("set rejected: %v", err)
	}

	rev := NewTaskStep("task-rev-promo", "revision work", 1000)
	rev.ID = "step-task-rev-promo-rev-1001-abc123"
	rev.DependsOn = []string{original.ID}
	if err := store.Create(rev); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	promoted, err := store.PromoteReadySteps("task-rev-promo")
	if err != nil {
		t.Fatalf("PromoteReadySteps: %v", err)
	}
	if len(promoted) != 1 || promoted[0].ID != rev.ID {
		t.Fatalf("revision not promoted past rejected original: promoted=%v", promoted)
	}

	persisted, err := store.GetByID(rev.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != StepReady {
		t.Errorf("revision state = %q, want ready", persisted.State)
	}
}

func TestStepStore_PromoteReadySteps_NonRevisionStillBlockedByRejected(t *testing.T) {
	store := newTestStepStore(t)

	rejected := NewTaskStep("task-rev-block", "rejected work", 0)
	rejected.ID = "step-rej-block"
	if err := store.Create(rejected); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.SetState(rejected.ID, StepRejected); err != nil {
		t.Fatalf("set rejected: %v", err)
	}

	// A NORMAL step (not a revision) depending on the rejected step must
	// still be blocked — only revisions may proceed past a rejection.
	normal := NewTaskStep("task-rev-block", "downstream work", 1)
	normal.ID = "step-normal-block"
	normal.DependsOn = []string{rejected.ID}
	if err := store.Create(normal); err != nil {
		t.Fatalf("create normal: %v", err)
	}

	promoted, err := store.PromoteReadySteps("task-rev-block")
	if err != nil {
		t.Fatalf("PromoteReadySteps: %v", err)
	}
	if len(promoted) != 0 {
		t.Fatalf("non-revision step promoted past rejected dep: %v", promoted)
	}
}

func TestStepStore_PromoteReadySteps_RevisionStillBlockedByFailed(t *testing.T) {
	store := newTestStepStore(t)

	failed := NewTaskStep("task-rev-fail", "failed work", 0)
	failed.ID = "step-fail-revfail"
	if err := store.Create(failed); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.SetState(failed.ID, StepFailed); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	rev := NewTaskStep("task-rev-fail", "revision of failed", 1000)
	rev.ID = "step-task-rev-fail-rev-1001-def456"
	rev.DependsOn = []string{failed.ID}
	if err := store.Create(rev); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	promoted, err := store.PromoteReadySteps("task-rev-fail")
	if err != nil {
		t.Fatalf("PromoteReadySteps: %v", err)
	}
	if len(promoted) != 0 {
		t.Fatalf("revision promoted past FAILED dep (must stay blocked): %v", promoted)
	}
}

func TestIsRevisionStep(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"step-task-x-rev-1001-abc", true},
		{"step-task-x-0-abc", false},
		{"step-orig", false},
		// M5 regression: a task ID that itself contains "-rev-" must not
		// make its NORMAL steps revision steps. The old bare
		// strings.Contains(id, "-rev-") flagged every step of task
		// "task-rev-promo".
		{"step-task-rev-promo-0-abc", false},
		{"step-task-rev-promo-1000-def456", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsRevisionStep(c.id); got != c.want {
			t.Errorf("IsRevisionStep(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

// TestStepStore_PromoteReadySteps_RevisionBlockedByUnrelatedRejectedDep is
// the M5 scoping test: a revision may proceed past ITS OWN rejected original
// (the LAST DependsOn entry, per CreateRevision's construction), but a
// different, unrelated rejected dependency must still block it.
func TestStepStore_PromoteReadySteps_RevisionBlockedByUnrelatedRejectedDep(t *testing.T) {
	store := newTestStepStore(t)

	orig := NewTaskStep("task-unrel", "original work", 0)
	orig.ID = "step-unrel-orig"
	if err := store.Create(orig); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := store.SetState(orig.ID, StepRejected); err != nil {
		t.Fatalf("set rejected: %v", err)
	}

	unrelated := NewTaskStep("task-unrel", "unrelated rejected work", 1)
	unrelated.ID = "step-unrel-other"
	if err := store.Create(unrelated); err != nil {
		t.Fatalf("create unrelated: %v", err)
	}
	if err := store.SetState(unrelated.ID, StepRejected); err != nil {
		t.Fatalf("set unrelated rejected: %v", err)
	}

	// Revision inherits the unrelated dep in front (inherited deps precede
	// the own original, which CreateRevision appends last).
	rev := NewTaskStep("task-unrel", "revision work", 1000)
	rev.ID = "step-task-unrel-rev-1001-abc123"
	rev.DependsOn = []string{unrelated.ID, orig.ID}
	if err := store.Create(rev); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	promoted, err := store.PromoteReadySteps("task-unrel")
	if err != nil {
		t.Fatalf("PromoteReadySteps: %v", err)
	}
	if len(promoted) != 0 {
		t.Fatalf("revision promoted past UNRELATED rejected dep: %v", promoted)
	}
	persisted, err := store.GetByID(rev.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != StepPending {
		t.Errorf("revision state = %q, want pending (blocked)", persisted.State)
	}
}

// TestStepStore_PromoteReadySteps_RevisionPastOriginal_WithCompletedExtraDep
// guards against over-blocking: the M5 scoping must still promote a revision
// whose own rejected original is the last dep when its other deps succeeded.
func TestStepStore_PromoteReadySteps_RevisionPastOriginal_WithCompletedExtraDep(t *testing.T) {
	store := newTestStepStore(t)

	orig := NewTaskStep("task-extra", "original work", 0)
	orig.ID = "step-extra-orig"
	if err := store.Create(orig); err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := store.SetState(orig.ID, StepRejected); err != nil {
		t.Fatalf("set rejected: %v", err)
	}

	done := NewTaskStep("task-extra", "completed work", 1)
	done.ID = "step-extra-done"
	if err := store.Create(done); err != nil {
		t.Fatalf("create done: %v", err)
	}
	if err := store.SetState(done.ID, StepCompleted); err != nil {
		t.Fatalf("set completed: %v", err)
	}

	rev := NewTaskStep("task-extra", "revision work", 1000)
	rev.ID = "step-task-extra-rev-1002-def456"
	rev.DependsOn = []string{done.ID, orig.ID}
	if err := store.Create(rev); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	promoted, err := store.PromoteReadySteps("task-extra")
	if err != nil {
		t.Fatalf("PromoteReadySteps: %v", err)
	}
	if len(promoted) != 1 || promoted[0].ID != rev.ID {
		t.Fatalf("revision with own-original-last not promoted: %v", promoted)
	}
}
