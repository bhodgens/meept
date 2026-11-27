package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// Bughunt 2026-11-24 M4: the model-blame join raced the async llm_calls
// insert. internal/llm/client.go records the call on a fire-and-forget
// goroutine while the C3 join in enrichedReviewPayload runs synchronously
// from the review-completion path, so on the loss race
// Store.ResolveReviewedModel answered ("", nil) (row not committed yet) and
// reviewed_model_id silently fell back to the CONFIGURED alias — the rating
// panel blamed the wrong model for that step. The suite never caught it
// because tests insert rows synchronously.
//
// Pin posture (acceptance: "pin with a slow-insert fake store that proves
// the join WAITS, not just falls back"):
//  1. late insert → the join re-resolves and stamps the REAL model id;
//  2. row never arrives → the fallback is unchanged and the wait is bounded;
//  3. a real store error is NOT retried (bounded-cost rule).

// slowInsertResolver fakes the async write side: the first emptyAnswers
// resolves answer ("", nil) — the row has not landed — then it lands model.
type slowInsertResolver struct {
	emptyAnswers int
	model        string
	err          error
	calls        int
}

func (f *slowInsertResolver) ResolveReviewedModel(_, _ string) (string, error) {
	f.calls++
	if f.calls <= f.emptyAnswers {
		return "", nil
	}
	if f.err != nil {
		return "", f.err
	}
	return f.model, nil
}

func newBlameRaceManager(t *testing.T, fake reviewedModelResolver) *ReviewManager {
	t.Helper()
	sstore := newTestStepStore(t)
	if err := sstore.Create(&task.TaskStep{
		ID:      "step-race-1",
		TaskID:  "task-blame-race",
		AgentID: "coder",
	}); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: sstore,
		Bus:       bus.New(nil, slogDiscardLogger()),
		Logger:    slogDiscardLogger(),
	})
	rm.metricsStore = fake
	rm.blameJoinSleeper = func(time.Duration) {} // no real time in the pin
	return rm
}

// Pin 1 (RED at the pre-fix single-resolve posture): the row lands after the
// first two clean empties; the join must wait and stamp the real model id.
func TestBlameJoinRetriesLateLLMCallsInsert(t *testing.T) {
	fake := &slowInsertResolver{emptyAnswers: 2, model: "m-real"}
	rm := newBlameRaceManager(t, fake)

	payload := rm.enrichedReviewPayload("step-race-1", "task-blame-race", &ReviewResult{
		Status:     ReviewApproved,
		ReviewerID: "code-reviewer",
	}, 0)

	if got, _ := payload["reviewed_model_id"].(string); got != "m-real" {
		t.Errorf("reviewed_model_id = %q, want m-real: the join did NOT wait for the late insert (resolves=%d)", got, fake.calls)
	}
	if fake.calls != 3 {
		t.Errorf("resolves = %d, want 3 (two empties + the landed row)", fake.calls)
	}
}

// Pin 2: a row that never arrives falls back to the configured ref and the
// wait stays bounded (blameJoinBudget), never hanging the completion path.
func TestBlameJoinAbsentRowFallsBackWithinBudget(t *testing.T) {
	fake := &slowInsertResolver{emptyAnswers: 1_000_000}
	rm := newBlameRaceManager(t, fake)

	start := time.Now()
	resolved, attempts, err := rm.resolveBlameModel("stp-step-race-1", "coder")
	elapsed := time.Since(start)

	if err != nil || resolved != "" {
		t.Errorf("resolved=%q err=%v, want clean empty fallback", resolved, err)
	}
	if attempts != blameJoinAttempts {
		t.Errorf("attempts = %d, want %d", attempts, blameJoinAttempts)
	}
	if elapsed > 2*blameJoinBudget {
		t.Errorf("waited %v, want <= %v (bounded-cost rule)", elapsed, 2*blameJoinBudget)
	}

	// The payload path must carry the fallback, never an empty blame.
	rm2 := newBlameRaceManager(t, &slowInsertResolver{emptyAnswers: 1_000_000})
	payload := rm2.enrichedReviewPayload("step-race-1", "task-blame-race", &ReviewResult{
		Status:     ReviewApproved,
		ReviewerID: "code-reviewer",
	}, 0)
	if got, _ := payload["reviewed_model_id"].(string); got == "m-real" {
		t.Errorf("fallback path stamped a resolved model %q", got)
	}
}

// Pin 3: a real store error stops after ONE resolve — it is not a race, and
// the completion path must not hammer a broken store.
func TestBlameJoinDoesNotRetryRealStoreError(t *testing.T) {
	boom := errors.New("store on fire")
	fake := &slowInsertResolver{emptyAnswers: 0, err: boom}
	rm := newBlameRaceManager(t, fake)

	_, attempts, err := rm.resolveBlameModel("stp-step-race-1", "coder")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the store error", err)
	}
	if attempts != 1 || fake.calls != 1 {
		t.Errorf("attempts=%d calls=%d, want 1 (real errors are not retried)", attempts, fake.calls)
	}
}
