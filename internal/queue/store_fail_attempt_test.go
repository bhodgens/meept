package queue

// Bughunt 2026-10-06 — H1 (failure-path claim token) and H2 (unreachable
// attempt-freshness guard) pins.
//
// H1: Store.Fail was the only job-state write with a bare `WHERE id = ?`.
// The worker presents its claim token on the SUCCESS path (CompleteAttempt)
// but dropped it on the FAILURE path, so a late failure from a SUPERSEDED
// attempt could move the LIVE attempt's job to `failed`.
//
// H2: `completionFreshChecker` is declared structurally in internal/agent.
// PersistentQueue held `store` as an unexported field WITHOUT embedding it, so
// Store.CompletionIsFresh was never promoted to the queue's method set and the
// agent's guard silently degraded to its pre-H5 state-only branch for every
// production queue. TestQueueExposesAttemptCapabilities below is the assertion
// that would have caught it at commit time.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// TestQueueExposesAttemptCapabilities pins that the REAL production types carry
// the optional attempt-aware capabilities their consumers probe for
// structurally. A missing method is invisible to a test that exercises a
// hand-rolled double, so this asserts the concrete types directly.
func TestQueueExposesAttemptCapabilities(t *testing.T) {
	// The agent's guard probe, mirrored. Structural typing means an identical
	// shape satisfies both declarations.
	type freshChecker interface {
		CompletionIsFresh(ctx context.Context, jobID, claimToken string) (fresh, ok bool)
	}
	type attemptCompleter interface {
		CompleteAttempt(ctx context.Context, jobID string, result any, claimToken string) error
	}
	type attemptFailer interface {
		FailAttempt(ctx context.Context, jobID string, err error, claimToken string) error
	}

	pq := (*PersistentQueue)(nil)
	if _, ok := any(pq).(freshChecker); !ok {
		t.Error("*PersistentQueue does not expose CompletionIsFresh: " +
			"internal/agent's stale-completion guard is DEAD for single-node queues")
	}
	if _, ok := any(pq).(attemptCompleter); !ok {
		t.Error("*PersistentQueue does not expose CompleteAttempt")
	}
	if _, ok := any(pq).(attemptFailer); !ok {
		t.Error("*PersistentQueue does not expose FailAttempt: " +
			"the worker's failure path would silently drop the claim token")
	}

	// ClusterQueue embeds Queue, so it needs its OWN forwarders — the embedded
	// interface hides capabilities declared only on the concrete type.
	cq := (*ClusterQueue)(nil)
	if _, ok := any(cq).(freshChecker); !ok {
		t.Error("*ClusterQueue does not expose CompletionIsFresh: " +
			"the guard would work single-node but silently degrade in cluster mode")
	}
	if _, ok := any(cq).(attemptFailer); !ok {
		t.Error("*ClusterQueue does not expose FailAttempt")
	}
}

// failableJob inserts a job and claims it, returning the live attempt.
func failableJob(t *testing.T, s *Store, workerID string) *Job {
	t.Helper()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "fail-path"})
	if err := s.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	claimed, err := s.ClaimNextForAgent(workerID, nil, "")
	if err != nil {
		t.Fatalf("Claim failed: %v", err)
	}
	if claimed.ClaimToken == "" {
		t.Fatal("claim returned an empty ClaimToken; the attempt predicates cannot work")
	}
	return claimed
}

// jobStateAndToken reads back the two fields the guards depend on.
func jobStateAndToken(t *testing.T, s *Store, jobID string) (state, token, errMsg string) {
	t.Helper()
	var (
		storedState string
		storedToken sql.NullString
		storedErr   sql.NullString
	)
	if err := s.db.QueryRow(`SELECT state, claim_token, error FROM jobs WHERE id = ?`, jobID).
		Scan(&storedState, &storedToken, &storedErr); err != nil {
		t.Fatalf("read back job %s: %v", jobID, err)
	}
	return storedState, storedToken.String, storedErr.String
}

// TestStoreFailAttempt_SupersededAttemptCannotFailLiveJob is the H1 core pin.
// A reclaimed-and-re-executed job's stale failure must be REFUSED, leaving the
// live attempt's row untouched.
func TestStoreFailAttempt_SupersededAttemptCannotFailLiveJob(t *testing.T) {
	store := newTestStore(t, "")
	ctx := context.Background()

	abandoned := failableJob(t, store, "worker-abandoned")
	abandonedToken := abandoned.ClaimToken

	// Cluster reclaim retires the token, then a second attempt claims it.
	if err := store.ResetToPending(ctx, abandoned.ID); err != nil {
		t.Fatalf("ResetToPending: %v", err)
	}
	live := failableJob(t, store, "worker-live")
	liveToken := live.ClaimToken
	if liveToken == abandonedToken {
		t.Fatalf("probe setup broken: reclaim did not retire the token (%q)", liveToken)
	}

	// The ABANDONED attempt reports a late failure. It must be refused.
	if _, err := store.FailAttempt(abandoned.ID, "late failure from a superseded attempt", abandonedToken); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("stale FailAttempt: err = %v, want ErrJobNotClaimable", err)
	}

	// The live attempt's row is untouched.
	state, token, errMsg := jobStateAndToken(t, store, abandoned.ID)
	if state != string(StateClaimed) {
		t.Errorf("state = %q, want %q — a superseded attempt killed the live job", state, StateClaimed)
	}
	if errMsg != "" {
		t.Errorf("error = %q, want empty — the stale failure must not write its message", errMsg)
	}
	if token != liveToken {
		t.Errorf("claim_token = %q, want the live attempt's %q", token, liveToken)
	}

	// The LIVE attempt can still fail its own job.
	if _, err := store.FailAttempt(abandoned.ID, "genuine failure", liveToken); err != nil {
		t.Fatalf("live FailAttempt failed: %v", err)
	}
	state, _, errMsg = jobStateAndToken(t, store, abandoned.ID)
	if state != string(StateFailed) {
		t.Errorf("state after the live failure = %q, want %q", state, StateFailed)
	}
	if errMsg != "genuine failure" {
		t.Errorf("error = %q, want the live attempt's message", errMsg)
	}
}

// TestStoreFailAttempt_SameTokenRetryIsIdempotent pins that a duplicate
// failure request from the SAME attempt is success, not a second state change.
func TestStoreFailAttempt_SameTokenRetryIsIdempotent(t *testing.T) {
	store := newTestStore(t, "")
	claimed := failableJob(t, store, "worker-retry")

	first, err := store.FailAttempt(claimed.ID, "boom", claimed.ClaimToken)
	if err != nil {
		t.Fatalf("first FailAttempt: %v", err)
	}
	if first != CompletionApplied {
		t.Fatalf("first disposition = %v, want CompletionApplied", first)
	}

	second, err := store.FailAttempt(claimed.ID, "boom", claimed.ClaimToken)
	if err != nil {
		t.Fatalf("idempotent retry: err = %v, want nil", err)
	}
	if second != CompletionIdempotent {
		t.Errorf("retry disposition = %v, want CompletionIdempotent", second)
	}
}

// TestStoreFail_TokenlessLegacyStillWorks pins the pre-H1 contract: callers
// that never claimed keep their behaviour, so the migration is not breaking.
func TestStoreFail_TokenlessLegacyStillWorks(t *testing.T) {
	store := newTestStore(t, "")
	claimed := failableJob(t, store, "worker-legacy")

	if err := store.Fail(claimed.ID, "legacy failure"); err != nil {
		t.Fatalf("token-less Fail: %v", err)
	}
	state, _, errMsg := jobStateAndToken(t, store, claimed.ID)
	if state != string(StateFailed) {
		t.Errorf("state = %q, want %q — the token-less path must be unchanged", state, StateFailed)
	}
	if errMsg != "legacy failure" {
		t.Errorf("error = %q, want the token-less caller's message", errMsg)
	}
}

// TestPersistentQueue_FailAttempt_UnpublishedOnSupersededAttempt pins the
// downstream half of H1: a refused failure publishes NO queue.job.failed, so a
// subscriber cannot process a stale failure as current.
func TestPersistentQueue_FailAttempt_UnpublishedOnSupersededAttempt(t *testing.T) {
	ctx := context.Background()
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "failattempt.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })

	sub := msgBus.Subscribe("test-fail-attempt", "queue.job.failed")
	defer msgBus.Unsubscribe(sub)

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "fail-publish"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	abandoned, err := q.Claim(ctx, "worker-a", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := q.store.ResetToPending(ctx, job.ID); err != nil {
		t.Fatalf("ResetToPending: %v", err)
	}
	if _, err := q.Claim(ctx, "worker-b", nil, ""); err != nil {
		t.Fatalf("second Claim: %v", err)
	}

	// The abandoned attempt's late failure is refused and publishes nothing.
	if err := q.FailAttempt(ctx, job.ID, errors.New("stale"), abandoned.ClaimToken); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("stale FailAttempt: err = %v, want ErrJobNotClaimable", err)
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("a superseded attempt published a failure event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: the stale failure publishes nothing
	}

	// The LIVE attempt's failure DOES publish. worker-b holds the job now, so
	// ITS token is the one that must be presented, not the abandoned attempt's.
	// (worker-b's Claim returned the job; re-read the row because the abandon
	// path above already consumed the returned struct.)
	_ = abandoned

	// Present the token the LIVE attempt actually holds.
	var liveStored sql.NullString
	if err := q.store.db.QueryRow(`SELECT claim_token FROM jobs WHERE id = ?`, job.ID).
		Scan(&liveStored); err != nil {
		t.Fatalf("read live token: %v", err)
	}
	if !liveStored.Valid {
		t.Fatal("probe setup broken: the live attempt has no token")
	}
	if err := q.FailAttempt(ctx, job.ID, errors.New("live failure"), liveStored.String); err != nil {
		t.Fatalf("live FailAttempt: %v", err)
	}
	select {
	case msg := <-sub.Channel:
		var payload struct {
			ClaimToken string `json:"claim_token"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal failure payload: %v", err)
		}
		if payload.ClaimToken != liveStored.String {
			t.Errorf("failure event claim_token = %q, want the live attempt's %q",
				payload.ClaimToken, liveStored.String)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the live attempt's failure event")
	}
}

// TestPersistentQueue_CompletionEventCarriesClaimToken pins H2's second leg: the
// attempt token must reach the event, or the agent's guard can only ever ask the
// state-only question.
func TestPersistentQueue_CompletionEventCarriesClaimToken(t *testing.T) {
	ctx := context.Background()
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "tok.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })

	sub := msgBus.Subscribe("test-token-carry", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "carry-token"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	claimed, err := q.Claim(ctx, "worker-tok", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ClaimToken == "" {
		t.Fatal("claim returned an empty token")
	}

	if err := q.CompleteAttempt(ctx, job.ID, map[string]string{"result": "done"}, claimed.ClaimToken); err != nil {
		t.Fatalf("CompleteAttempt: %v", err)
	}

	select {
	case msg := <-sub.Channel:
		var payload struct {
			JobID      string `json:"job_id"`
			ClaimToken string `json:"claim_token"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal completion payload: %v", err)
		}
		if payload.ClaimToken != claimed.ClaimToken {
			t.Errorf("completion event claim_token = %q, want the attempt's %q — "+
				"without it the agent guard degrades to the state-only predicate",
				payload.ClaimToken, claimed.ClaimToken)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the completion event")
	}
}
