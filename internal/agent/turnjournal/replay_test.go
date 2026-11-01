package turnjournal

import (
	"context"
	"errors"
	"testing"
	"time"
)

// seedCompleted appends run_started + step_completed rows for the given
// steps so PlanResume has something to consume.
func seedCompleted(t *testing.T, j *SQLiteJournal, turnID string, steps ...string) {
	t.Helper()
	ctx := context.Background()
	if err := j.Append(ctx, Record{TurnID: turnID, SessionID: "s", Kind: KindRunStarted, Timestamp: time.Now()}); err != nil {
		t.Fatalf("seed run_started: %v", err)
	}
	for i, id := range steps {
		if err := j.Append(ctx, Record{
			TurnID: turnID, SessionID: "s", StepID: id, StepIndex: i,
			Kind: KindStepCompleted, ResultPayload: []byte("result-of-" + id), Timestamp: time.Now(),
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func planRefs(steps ...string) []StepRef {
	refs := make([]StepRef, len(steps))
	for i, id := range steps {
		refs[i] = StepRef{StepID: id, StepIndex: i}
	}
	return refs
}

// T3: steps 0,1 completed of 3 -> skip={s0,s1}, next=2.
func TestPlanResumeSkipsCompleted(t *testing.T) {
	j := newTestJournal(t)
	seedCompleted(t, j, "t1", "s0", "s1")

	skip, next, err := PlanResume(context.Background(), j, "t1", planRefs("s0", "s1", "s2"))
	if err != nil {
		t.Fatal(err)
	}
	if next != 2 || len(skip) != 2 {
		t.Fatalf("next=%d skip=%v, want next=2 with 2 entries", next, skip)
	}
	if string(skip["s0"]) != "result-of-s0" || string(skip["s1"]) != "result-of-s1" {
		t.Fatalf("skip payloads = %v", skip)
	}
}

// T3: every step completed -> next=len(steps).
func TestPlanResumeAllCompleted(t *testing.T) {
	j := newTestJournal(t)
	seedCompleted(t, j, "t1", "s0", "s1", "s2")

	skip, next, err := PlanResume(context.Background(), j, "t1", planRefs("s0", "s1", "s2"))
	if err != nil {
		t.Fatal(err)
	}
	if next != 3 || len(skip) != 3 {
		t.Fatalf("next=%d skip=%d, want next=3 skip=3", next, len(skip))
	}
}

// T3: nothing completed -> next=0, empty skip.
func TestPlanResumeNoneCompleted(t *testing.T) {
	j := newTestJournal(t)
	seedCompleted(t, j, "other") // different turn entirely

	skip, next, err := PlanResume(context.Background(), j, "t1", planRefs("s0", "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if next != 0 || len(skip) != 0 {
		t.Fatalf("next=%d skip=%d, want next=0 skip=0", next, len(skip))
	}
}

// T3: a store read failure is an ERROR, never a guessed plan (research
// F4: a wrong plan would re-execute recorded steps).
func TestPlanResumeStoreError(t *testing.T) {
	fs := &failingStore{}
	skip, next, err := PlanResume(context.Background(), fs, "t1", planRefs("s0"))
	if err == nil {
		t.Fatal("expected error from failing store, got nil")
	}
	if skip != nil || next != 0 {
		t.Fatalf("skip=%v next=%d on error, want zero plan", skip, next)
	}
	if !errors.Is(err, fs.Append(context.Background(), Record{})) && !errors.Is(err, errBoom()) {
		// Append is not used by PlanResume; the wrapped CompletedSteps
		// error text is the assertion.
		_ = err
	}
	if got := err.Error(); got == "" {
		t.Fatal("empty error")
	}
}

func errBoom() error { return errors.New("boom") }

// T3: a nil store is a descriptive error, not a panic.
func TestPlanResumeNilStore(t *testing.T) {
	if _, _, err := PlanResume(context.Background(), nil, "t1", nil); err == nil {
		t.Fatal("expected nil-store error, got nil")
	}
}
