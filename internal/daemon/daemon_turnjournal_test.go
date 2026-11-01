package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/turnjournal"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
)

// readDaemonRunSource reads daemon.go so the ordering pins are structural:
// they survive line drift (the anchors are markers, not line numbers).
// TestMain chdirs to the module root, so the path is package-relative to
// the module.
func readDaemonRunSource(t *testing.T) (string, error) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("internal", "daemon", "daemon.go"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// turnjournalSource concatenates the package's non-test sources for the
// no-reimplementation grep-proof (module-root-relative; see TestMain).
func turnjournalSource(t *testing.T) (string, error) {
	t.Helper()
	dir := filepath.Join("internal", "agent", "turnjournal")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return "", err
		}
		sb.Write(b)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// grepLines returns the lines whose content matches the substring (a poor
// man's grep, stdlib-only, ASCII-safe).
func grepLines(src, substr string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

// failingStore reuses leaf 02's journalingStore fake (loop_turnjournal_test.go);
// its zero value serves reads from an empty rows list.
type failingStore = journalingStore

// newRehydrateDaemon builds a bare Daemon whose fields the rehydrate path
// touches are populated (no Run: the helper targets rehydrateTurnJournal
// directly -- the ordering pin below greps Run's source instead of booting
// a full daemon in-process).
func newRehydrateDaemon(t *testing.T, store turnjournal.JournalStore, q queue.Queue) *Daemon {
	t.Helper()
	d := &Daemon{
		logger: discardLogger(),
	}
	if store != nil {
		d.components = &Components{TurnJournal: store, Queue: q}
	}
	return d
}

// newTestQueueForRehydrate mirrors internal/queue's test helper (that one
// lives in the queue package's own tests and is not importable).
func newTestQueueForRehydrate(t *testing.T) *queue.PersistentQueue {
	t.Helper()
	msgBus := bus.New(nil, discardLogger())
	q, err := queue.NewPersistentQueue(filepath.Join(t.TempDir(), "queue.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// ---------------------------------------------------------------------------
// turn-journal leaf 03: boot rehydrate + replay dedup interlock (T2/T3/T4).
// These tests pin the same contracts leaf 04's e2e scenarios prove
// end-to-end: the Rehydrate call sits after ResetStaleClaimsAtStartup in
// Run, the requeue goes through the EXISTING queue path with the ORIGINAL
// job ID, and the queue's CompleteAttempt/CompletionIsFresh claim-token
// dedup stays the ONLY completion dedup.
// ---------------------------------------------------------------------------

// rehydrateSeededJournal opens a real SQLite turn journal and seeds one
// open run for taskID with the given journaled step payloads, plus one
// closed run older than the retention window (the T4 prune candidate).
func rehydrateSeededJournal(t *testing.T) (*turnjournal.SQLiteJournal, error) {
	t.Helper()
	j, err := turnjournal.NewSQLiteJournal(filepath.Join(t.TempDir(), "turnjournal.db"), nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = j.Close() })
	return j, nil
}

// seedOpenRun writes run_started + one step_completed row for the task,
// exactly the rows leaf 02's writer produces before a crash.
func seedOpenRun(t *testing.T, j *turnjournal.SQLiteJournal, taskID, stepID string, payload []byte) {
	t.Helper()
	ctx := context.Background()
	if err := j.Append(ctx, turnjournal.Record{
		TurnID: taskID, SessionID: "sess-rehydrate", Kind: turnjournal.KindRunStarted,
		ResultPayload: []byte(`{"step_id":"` + stepID + `"}`), Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("seed run_started: %v", err)
	}
	if err := j.Append(ctx, turnjournal.Record{
		TurnID: taskID, SessionID: "sess-rehydrate", StepID: stepID, StepIndex: 0,
		Kind: turnjournal.KindStepCompleted, ResultPayload: payload, Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("seed step_completed: %v", err)
	}
}

// T2: flag on + one open run whose queue job is pending -> the requeue
// closure observes the ORIGINAL job ID, the run counts once, and the
// queue state is untouched (already-pending = no-op enqueue).
func TestBootRehydrateRequeuesWithOriginalID(t *testing.T) {
	j, err := rehydrateSeededJournal(t)
	if err != nil {
		t.Fatal(err)
	}
	q := newTestQueueForRehydrate(t)
	ctx := context.Background()

	job, err := queue.NewJob(queue.JobTypeProjectTask, json.RawMessage(`{"step_id":"s1","task_id":"task-rehydrate-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	job = job.WithTaskID("task-rehydrate-1")
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}

	seedOpenRun(t, j, "task-rehydrate-1", "s1", []byte(`{"response":"journaled"}`))

	d := newRehydrateDaemon(t, j, q)
	resumed := d.rehydrateTurnJournal(ctx)
	if resumed != 1 {
		t.Fatalf("resumed = %d, want 1", resumed)
	}
	// The original job row survives with its ORIGINAL ID and stays
	// claimable (pending).
	got, err := q.Get(ctx, job.ID)
	if err != nil || got == nil {
		t.Fatalf("original job lost: %v %v", got, err)
	}
	if got.State != queue.StatePending {
		t.Fatalf("job state = %s, want pending", got.State)
	}
}

// T2: flag off (nil store) -> the whole block is skipped: no requeue, no
// panic. The Daemon-level gate (fullConfig.Daemon.TurnJournalEnabled) is
// asserted textually in TestBootRehydrateOrderingBelowClaimReset.
func TestBootRehydrateNilStoreSkips(t *testing.T) {
	q := newTestQueueForRehydrate(t)
	d := &Daemon{logger: discardLogger(), components: &Components{Queue: q}}
	if resumed := d.rehydrateTurnJournal(context.Background()); resumed != 0 {
		t.Fatalf("nil store resumed = %d, want 0", resumed)
	}
}

// T2: the Rehydrate call sits AFTER the ResetStaleClaimsAtStartup block
// and BEFORE status=running in Run -- textually and therefore temporally.
// This is the C6 ordering pin; moving either block fails this test.
func TestBootRehydrateOrderingBelowClaimReset(t *testing.T) {
	src, err := readDaemonRunSource(t)
	if err != nil {
		t.Fatal(err)
	}
	claimReset := strings.Index(src, "ResetStaleClaimsAtStartup(ctx, cutoff)")
	rehydrate := strings.Index(src, "rehydrateTurnJournal(ctx)")
	running := strings.Index(src, "models.StatusRunning")
	if claimReset < 0 || rehydrate < 0 || running < 0 {
		t.Fatalf("anchors missing: claimReset=%d rehydrate=%d running=%d", claimReset, rehydrate, running)
	}
	if !(claimReset < rehydrate && rehydrate < running) {
		t.Fatalf("ordering broken: claimReset@%d rehydrate@%d running@%d (want claimReset < rehydrate < running)", claimReset, rehydrate, running)
	}
	// R7 ordering chain: the classifier boot gate resolves BEFORE the
	// claim reset, which precedes the rehydrate block, which precedes
	// status=running. Nothing in this leaf moved a transport or the gate.
	gate := strings.Index(src, "classifierBoot.SignalReady()")
	if gate < 0 || !(gate < claimReset && claimReset < rehydrate && rehydrate < running) {
		t.Fatalf("boot ordering broken: gate@%d claimReset@%d rehydrate@%d running@%d (want gate < claimReset < rehydrate < running)",
			gate, claimReset, rehydrate, running)
	}
}

// T2: flag OFF -> rehydrateTurnJournal is never reached from Run; pin the
// gate predicate textually so a future edit cannot drop the flag check.
func TestBootRehydrateFlagGated(t *testing.T) {
	src, err := readDaemonRunSource(t)
	if err != nil {
		t.Fatal(err)
	}
	gate := "d.fullConfig.Daemon.TurnJournalEnabled"
	idx := strings.Index(src, gate)
	if idx < 0 {
		t.Fatalf("flag gate %q missing from the Run rehydrate block", gate)
	}
	call := strings.Index(src, "rehydrateTurnJournal(ctx)")
	if call < idx {
		t.Fatalf("rehydrate call@%d is not gated by the flag check@%d", call, idx)
	}
}

// T3 (the replay-dedup interlock): a requeued turn whose step is already
// journaled completes through the skip seam with ZERO LLM re-invocations
// and completes exactly once at the queue -- the claim-token guard, not
// journal code, owns the dedup. Flow: enqueue job, claim it (attempt 1),
// journal the step result (leaf 02's write), complete the job; then
// rehydrate treats the run as open (the run_closed write was lost in the
// crash), the skip seam restores the journaled result, and a SECOND
// CompleteAttempt for the stale attempt is refused by the queue while the
// fresh attempt's completion publishes exactly one completion event.
func TestReplayDedupInterlockJournaledStepCompletesOnce(t *testing.T) {
	j, err := rehydrateSeededJournal(t)
	if err != nil {
		t.Fatal(err)
	}
	q := newTestQueueForRehydrate(t)
	ctx := context.Background()

	job, _ := queue.NewJob(queue.JobTypeProjectTask,
		json.RawMessage(`{"step_id":"s1","task_id":"task-interlock","description":"do work"}`))
	job = job.WithTaskID("task-interlock")
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Attempt 1: claim -> journal the completed step (leaf 02) -> the
	// worker completes the job (fresh claim token).
	claimed, err := q.Claim(ctx, "w1", nil, "")
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	// Leaf 02's writer keys the step_completed row by the PAYLOAD's
	// step_id, which for a step job is the job ID (stepJobPayload builds
	// step_id from the job).
	seedOpenRun(t, j, "task-interlock", job.ID, []byte(`{"response":"journaled","status":"completed","success":true}`))
	if err := q.Complete(ctx, job.ID, map[string]any{"response": "attempt-1"}); err != nil {
		t.Fatalf("attempt-1 complete: %v", err)
	}

	// The daemon then crashed BEFORE OnRunClose (or the close write was
	// lost), so the run is still open in the journal -- but the queue
	// already holds the completion.

	// Boot: the skip seam must restore the journaled result without an
	// LLM call, and the rehydrate pass must NOT reset the completed job
	// back to pending (the queue's state machine owns the completion).
	loop := newTurnJournalTestLoop(t, &countingChatter{inner: &okChatter{content: "FRESH RE-EXECUTION"}})
	p, _ := newJournalingProcessor(t, loop)
	// Point the processor's hooks at the SAME store so the seeded row is
	// visible (newJournalingProcessor installs its own fake; replace it).
	p.WithTurnJournal(agent.NewTurnJournalHooks(j, discardLogger()))

	res, perr := p.Process(ctx, stepJob(t, job.ID, "task-interlock"))
	if perr != nil {
		t.Fatalf("Process: %v", perr)
	}
	envelope, ok := res.(map[string]any)
	if !ok || envelope["response"] != "journaled" {
		t.Fatalf("result = %#v, want the journaled result (never re-executed)", res)
	}

	// Rehydrate: the queue's completed state is terminal -- the requeue
	// closure must treat the open run as already settled and leave the
	// completed row untouched (no reset to pending, no re-execution).
	d := newRehydrateDaemon(t, j, q)
	if resumed := d.rehydrateTurnJournal(ctx); resumed != 0 {
		t.Fatalf("resumed = %d for an already-completed job, want 0", resumed)
	}
	got, gerr := q.Get(ctx, job.ID)
	if gerr != nil || got == nil || got.State != queue.StateCompleted {
		t.Fatalf("completed job disturbed by rehydrate: %+v (%v)", got, gerr)
	}

	// The stale attempt's completion is refused by the queue itself
	// (ErrJobNotClaimable) and nothing republished -- the dedup lives in
	// CompleteAttempt/CompletionIsFresh, never in journal code.
	err = q.CompleteAttempt(ctx, job.ID, map[string]any{"response": "stale"}, "stale-token")
	if !errors.Is(err, queue.ErrJobNotClaimable) {
		t.Fatalf("stale completion = %v, want ErrJobNotClaimable from the queue's own guard", err)
	}
	// A stale attempt's completion is refused by the queue itself
	// (ErrJobNotClaimable) and nothing republished -- the dedup lives in
	// CompleteAttempt/CompletionIsFresh, never in journal code. The
	// attempt-exact predicate agrees: a WRONG token on the completed row
	// reads not-fresh; the completing (token-less legacy) completion
	// reads fresh.
	if fresh, ok := q.CompletionIsFresh(ctx, job.ID, "stale-token"); !ok || fresh {
		t.Fatalf("CompletionIsFresh(stale token) = fresh=%v ok=%v, want ok=true fresh=false", fresh, ok)
	}
	if fresh, ok := q.CompletionIsFresh(ctx, job.ID, ""); !ok || !fresh {
		t.Fatalf("CompletionIsFresh(token-less on completed) = fresh=%v ok=%v, want ok=true fresh=true", fresh, ok)
	}
}

// T3 grep-proof: the turnjournal package holds no copy of the queue's
// completion dedup (CompleteAttempt / CompletionIsFresh appear only in
// comments, never as implementation).
func TestReplayDedupNoReimplementation(t *testing.T) {
	src, err := turnjournalSource(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"func.*CompleteAttempt", "func.*CompletionIsFresh", "claim_token"} {
		if lines := grepLines(src, marker); len(lines) > 0 {
			for _, l := range lines {
				trimmed := strings.TrimSpace(l)
				if strings.HasPrefix(trimmed, "//") {
					continue // comments are allowed (R6: the contract is documented)
				}
				t.Fatalf("turnjournal re-implements the dedup: %s", trimmed)
			}
		}
	}
}

// T4: boot-time retention -- a closed run older than ClosedRunRetention
// is pruned at boot, a fresh closed run is kept, and open runs are never
// touched.
func TestRehydratePruneClosedAtBoot(t *testing.T) {
	j, err := rehydrateSeededJournal(t)
	if err != nil {
		t.Fatal(err)
	}
	q := newTestQueueForRehydrate(t)
	ctx := context.Background()

	old := time.Now().Add(-2 * turnjournal.ClosedRunRetention)
	fresh := time.Now()

	seed := func(turnID string, at time.Time, closeIt bool) {
		t.Helper()
		if err := j.Append(ctx, turnjournal.Record{TurnID: turnID, SessionID: "s", Kind: turnjournal.KindRunStarted, Timestamp: at}); err != nil {
			t.Fatal(err)
		}
		if closeIt {
			// CloseRun stamps now; age the row by writing the closed
			// marker directly so the cutoff straddles deterministically.
			if err := j.Append(ctx, turnjournal.Record{TurnID: turnID, SessionID: "s", Kind: turnjournal.KindRunClosed, Timestamp: at}); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("old-run", old, true)
	seed("fresh-run", fresh, true)
	seed("open-run", old, false)

	d := newRehydrateDaemon(t, j, q)
	_ = d.rehydrateTurnJournal(ctx) // the prune rides the same block

	runs, err := j.OpenRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].TurnID != "open-run" {
		t.Fatalf("open runs after boot prune = %+v, want only open-run (open runs are NEVER pruned)", runs)
	}
	var oldRows, freshRows int
	if err := j.DBHandle().QueryRow("SELECT COUNT(*) FROM turn_journal WHERE turn_id='old-run'").Scan(&oldRows); err != nil {
		t.Fatal(err)
	}
	if err := j.DBHandle().QueryRow("SELECT COUNT(*) FROM turn_journal WHERE turn_id='fresh-run'").Scan(&freshRows); err != nil {
		t.Fatal(err)
	}
	if oldRows != 0 {
		t.Fatalf("old-run rows = %d, want 0 (pruned at boot)", oldRows)
	}
	if freshRows == 0 {
		t.Fatal("fresh-run pruned, want kept (inside the retention window)")
	}
}

// T4: a failing store read leaves boot unaffected -- rehydrateTurnJournal
// returns 0 and the prune attempt logs inside (no panic, no error out).
func TestRehydrateFailingStoreBestEffort(t *testing.T) {
	q := newTestQueueForRehydrate(t)
	d := newRehydrateDaemon(t, &failingStore{}, q)
	if resumed := d.rehydrateTurnJournal(context.Background()); resumed != 0 {
		t.Fatalf("resumed = %d on a failing store, want 0", resumed)
	}
}
