package employee

// Gate tests for branch 03-plans-goals, leaf 00T (goal fence + retire
// consistency + paused-write fence, M-10/M-11/F16/C-P3). TESTS ONLY —
// written against the NOT-YET-EXISTING symbols of leaf 05
// (ErrGoalTransition, goalTransitions/validFrom, UpdateWithTransition).
// Expected RED reasons are marked per test.
//
// Rig note: reuses testGoalStore / mustCreateGoal / fillDefaults
// (goal_test.go) and newHirePersistenceManager / countRows
// (manager_test.go) — all same-package test helpers.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// setGoalStateRaw persists a goal row's state directly (bypassing the store
// API) so a test can seed a starting state that the future fence would
// otherwise police — e.g. seeding `blocked` without depending on the
// active->blocked legality the fence itself will assert.
func setGoalStateRaw(t *testing.T, store *GoalStore, goalID, state string) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(),
		`UPDATE employee_goals SET state = ? WHERE id = ?`, state, goalID); err != nil {
		t.Fatalf("seed state %q: %v", state, err)
	}
}

// TestGoalTransitionFence pins contract C-P3 (leaf 05): the GoalStore
// validates every state change against a validFrom table, so the "durable
// blocked phase" is enforced by the store, not convention:
//
//   - blocked -> active via the PLAIN store write is refused with
//     ErrGoalTransition (unblock is human-gated only);
//   - the human-gated tool path (UpdateWithTransition(..., allowHumanBypass
//     = true) — the only legitimate bypass per the leaf's grep-provable
//     caller set) succeeds;
//   - retired -> anything is refused (terminal, even with the bypass);
//   - a normal legal edge (active -> paused) still passes the plain Update;
//   - an UNKNOWN persisted state surfaces as a load error, never a silent
//     active.
//
// RED: ErrGoalTransition + GoalStore.UpdateWithTransition undefined
// (compile failure — both land in leaf 05).
func TestGoalTransitionFence(t *testing.T) {
	ctx := context.Background()
	store := testGoalStore(t)

	// --- blocked -> active refused on the plain write path.
	blocked := mustCreateGoal(t, store, &Goal{Title: "blocked fence", Mandate: "m"})
	if err := store.Update(ctx, blocked); err != nil {
		t.Fatalf("baseline update: %v", err)
	}
	setGoalStateRaw(t, store, blocked.ID, "blocked")
	g, err := store.Get(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("Get seeded blocked goal: %v", err)
	}
	g.State = GoalActive
	if err := store.Update(ctx, g); !errors.Is(err, ErrGoalTransition) {
		t.Errorf("plain store write blocked->active must fail with ErrGoalTransition, got: %v", err)
	}
	// Row must be untouched by the refused write.
	again, againErr := store.Get(ctx, blocked.ID)
	if againErr != nil {
		t.Fatalf("Get after refused write: %v", againErr)
	}
	if again.State != GoalBlocked {
		t.Errorf("refused blocked->active changed the row: state = %s, want blocked (unchanged)", again.State)
	}

	// --- human-gated resume (the goal_update tool's bypass) succeeds.
	g2, err := store.Get(ctx, blocked.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	g2.State = GoalActive
	if err := store.UpdateWithTransition(ctx, g2, true); err != nil {
		t.Errorf("human-gated resume blocked->active must succeed via UpdateWithTransition(bypass=true): %v", err)
	}
	if again, err := store.Get(ctx, blocked.ID); err != nil {
		t.Fatalf("Get after resume: %v", err)
	} else if again.State != GoalActive {
		t.Errorf("after tool resume: state = %s, want active", again.State)
	}

	// --- retired is terminal: nothing may leave it, not even the bypass.
	retired := mustCreateGoal(t, store, &Goal{Title: "retired fence", Mandate: "m"})
	if err := store.Retire(ctx, retired.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	rg, err := store.Get(ctx, retired.ID)
	if err != nil {
		t.Fatalf("Get retired goal: %v", err)
	}
	rg.State = GoalActive
	if err := store.Update(ctx, rg); !errors.Is(err, ErrGoalTransition) {
		t.Errorf("retired->active must be refused with ErrGoalTransition, got: %v", err)
	}
	if err := store.UpdateWithTransition(ctx, rg, true); !errors.Is(err, ErrGoalTransition) {
		t.Errorf("retired->active must be refused even with the human bypass, got: %v", err)
	}

	// --- a legal plain edge (active -> paused) still passes.
	active := mustCreateGoal(t, store, &Goal{Title: "legal edge", Mandate: "m"})
	ag, err := store.Get(ctx, active.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	ag.State = GoalPaused
	if err := store.Update(ctx, ag); err != nil {
		t.Errorf("active->paused is a legal transition, plain Update must pass: %v", err)
	}
	// paused -> blocked legal; blocked -> paused must be refused (the table
	// says blocked leaves only to active(bypass)/retired).
	pg, err := store.Get(ctx, active.ID)
	if err != nil {
		t.Fatalf("Get paused: %v", err)
	}
	pg.State = GoalBlocked
	if err := store.Update(ctx, pg); err != nil {
		t.Errorf("paused->blocked is legal, got: %v", err)
	}
	bg, err := store.Get(ctx, active.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	bg.State = GoalPaused
	if err := store.Update(ctx, bg); !errors.Is(err, ErrGoalTransition) {
		t.Errorf("blocked->paused must be refused (blocked leaves only to active-via-human or retired), got: %v", err)
	}

	// --- unknown persisted state surfaces as a LOAD error, not silent active.
	// (Today ParseGoalState returns (active, error) and scanGoal wraps it, so
	// this sub-assertion already passes; it is a pin against the fence work
	// "tightening unknown->reject" not regressing the error surfacing.)
	setGoalStateRaw(t, store, active.ID, "quantum")
	_, err = store.Get(ctx, active.ID)
	if err == nil {
		t.Error("loading a goal with an unknown persisted state must error, not decode silently to active")
	} else if !strings.Contains(err.Error(), "quantum") {
		t.Logf("load error shape (informational): %v", err)
	}
}

// TestRetireSetsRetiredAt pins M-11 (leaf 05 T3/T4): retired_at consistency +
// soft-delete retirement.
//
// Part A (store-level tool equivalence — the goal_update tool's "complete"
// action is a GoalStore state write): ANY write that lands a goal in the
// retired state must stamp retired_at, so IsRetired() is true for every
// retired row. Today GoalStore.Update persists g.RetiredAt verbatim: a
// writer that flips State=GoalRetired with a zero RetiredAt produces a
// retired row with retired_at NULL — exactly the inconsistency M-11 records.
//
// Part B pins Manager.Retire (employee delete) against the CURRENT
// hard-DELETE: the Q3 default is soft-delete — goal rows REMAIN, state
// becomes retired, retired_at is set (audit trail preserved).
//
// Part A/B are behavioral RED at HEAD (both compile-clean today); the whole
// package is additionally compile-RED via TestGoalTransitionFence's new
// symbols.
func TestRetireSetsRetiredAt(t *testing.T) {
	ctx := context.Background()

	// --- Part A: retired-state write stamps retired_at.
	store := testGoalStore(t)
	g := mustCreateGoal(t, store, &Goal{Title: "complete via tool", Mandate: "m"})
	loaded, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	loaded.State = GoalRetired
	loaded.RetiredAt = time.Time{} // the tool writer forgets the timestamp
	if err := store.Update(ctx, loaded); err != nil {
		t.Fatalf("retire write: %v", err)
	}
	stored, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get after retire write: %v", err)
	}
	if stored.State != GoalRetired {
		t.Fatalf("state = %s, want retired", stored.State)
	}
	if stored.RetiredAt.IsZero() || !stored.IsRetired() {
		t.Errorf("retired row carries no retired_at (IsRetired=%v): every retired-state write "+
			"must stamp retired_at (M-11)", stored.IsRetired())
	}

	// --- Part B: employee delete soft-deletes goals (rows survive).
	m, dbPath := newHirePersistenceManager(t)
	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	if _, err := m.Hire(ctx, HireRequest{
		ID:           "emp-soft-retire",
		Name:         "soft retire bot",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
		Goals: []GoalSpec{
			{Title: "g1", Mandate: "m1", Source: "user"},
			{Title: "g2", Mandate: "m2", Source: "user"},
		},
	}); err != nil {
		t.Fatalf("Hire: %v", err)
	}
	if n := countRows(t, dbPath, `SELECT COUNT(*) FROM employee_goals WHERE employee_id = ?`, "emp-soft-retire"); n != 2 {
		t.Fatalf("post-hire goal rows = %d, want 2", n)
	}

	if err := m.Retire(ctx, "emp-soft-retire"); err != nil {
		t.Fatalf("Manager.Retire: %v", err)
	}

	// Q3 default: the goal rows must SURVIVE as retired. Today the retire
	// path hard-DELETEs them (manager.go goal sweep) -> behavioral RED.
	remaining := countRows(t, dbPath, `SELECT COUNT(*) FROM employee_goals WHERE employee_id = ?`, "emp-soft-retire")
	if remaining != 2 {
		t.Errorf("Manager.Retire hard-deleted goals: %d rows remain, want 2 "+
			"(M-11 Q3: employee delete must SOFT-delete goals — state=retired, retired_at set, rows preserved)",
			remaining)
		return
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open for check: %v", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT state, retired_at FROM employee_goals WHERE employee_id = ?`, "emp-soft-retire")
	if err != nil {
		t.Fatalf("query goals: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var retiredAt sql.NullString
		if err := rows.Scan(&state, &retiredAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if state != "retired" {
			t.Errorf("surviving goal row state = %q, want retired", state)
		}
		if !retiredAt.Valid || retiredAt.String == "" {
			t.Errorf("surviving goal row has no retired_at: %v", retiredAt)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
}

// TestPausedGoalSkipsAssessmentWrites pins F16 (leaf 05 T5): a paused or
// blocked goal must receive NO assessment writes — health, last_assessed,
// recent_findings, round counters are untouched while the goal is not active.
// The goal_loop driver's reflect path is out of reach for a package gate (it
// needs a full agent loop), so this uses the STORE-LEVEL equivalent the leaf
// names: the write-back goes through GoalStore.Update carrying a health +
// findings change while the persisted row is paused.
//
// Today Update blindly persists every column including health/findings —
// behavioral RED: the paused row gets mutated. GREEN contract (leaf 05):
// Update with an unchanged State refuses assessment-column deltas for
// non-active goals (a legit state-change write is NOT blocked — see the
// review checklist: only assessment/health writes).
func TestPausedGoalSkipsAssessmentWrites(t *testing.T) {
	ctx := context.Background()
	store := testGoalStore(t)

	for _, state := range []GoalState{GoalPaused, GoalBlocked} {
		g := mustCreateGoal(t, store, &Goal{Title: "frozen " + state.String(), Mandate: "m"})
		loaded, err := store.Get(ctx, g.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		// Park the goal through the store (same-state write, health untouched).
		loaded.State = state
		if err := store.Update(ctx, loaded); err != nil {
			t.Fatalf("set state %s: %v", state, err)
		}

		// Snapshot the durable assessment columns via raw read (bypasses any
		// future decode hardening; the pin is on the ROW).
		var health, findings, lastAssessed sql.NullString
		var rounds int
		if err := store.db.QueryRowContext(ctx,
			`SELECT health, recent_findings, last_assessed, rounds_started FROM employee_goals WHERE id = ?`,
			g.ID).Scan(&health, &findings, &lastAssessed, &rounds); err != nil {
			t.Fatalf("raw snapshot: %v", err)
		}

		// An assessment write-back attempt on the non-active goal: health +
		// last_assessed + findings move, state does NOT. The leaf may implement
		// the fence either as a silent skip or as a refusal error — both keep
		// the row unchanged, which is what this pin asserts.
		cur, err := store.Get(ctx, g.ID)
		if err != nil {
			t.Fatalf("Get current: %v", err)
		}
		cur.Assess(GoalBroken, time.Now().UTC())
		cur.AttachFinding("finding-f16")
		if err := store.Update(ctx, cur); err != nil {
			t.Logf("assessment write-back on %s goal refused with an error (acceptable fence shape): %v", state, err)
		}

		var health2, findings2, lastAssessed2 sql.NullString
		var rounds2 int
		if err := store.db.QueryRowContext(ctx,
			`SELECT health, recent_findings, last_assessed, rounds_started FROM employee_goals WHERE id = ?`,
			g.ID).Scan(&health2, &findings2, &lastAssessed2, &rounds2); err != nil {
			t.Fatalf("raw re-read: %v", err)
		}
		if health2.String != health.String || lastAssessed2.String != lastAssessed.String ||
			findings2.String != findings.String || rounds2 != rounds {
			t.Errorf("%s goal received assessment writes: health %q->%q, last_assessed %q->%q, findings %q->%q, rounds %d->%d "+
				"(F16: non-active goals must be fenced from assessment write-backs; state stays %s, no column delta)",
				state,
				health.String, health2.String, lastAssessed.String, lastAssessed2.String,
				findings.String, findings2.String, rounds, rounds2, state)
		}
	}
}

// TestMarkReadRequiresDelivered pins the messages nit (leaf 06 T5):
// MarkRead may only move DELIVERED messages to read. Today's guard is
// `state != 'read'`, so a queued message jumps straight to read, skipping —
// and erasing — the delivery receipt. The queued row must stay untouched.
//
// Behavioral RED at HEAD: the queued->read write succeeds.
func TestMarkReadRequiresDelivered(t *testing.T) {
	ctx := context.Background()
	s := newTestMessageStore(t)

	// queued message — never drained, so it stays in state queued. Its
	// recipient is deliberately never drained (separate inbox), so the row
	// is provably queued when MarkRead runs.
	queued := &AgentMessage{From: "pm", To: "sleeper", Body: "not yet delivered"}
	if err := s.Enqueue(queued); err != nil {
		t.Fatalf("Enqueue queued: %v", err)
	}
	// delivered control — drained through the real queued->delivered path.
	delivered := &AgentMessage{From: "pm", To: "coder", Body: "delivered then read"}
	if err := s.Enqueue(delivered); err != nil {
		t.Fatalf("Enqueue delivered: %v", err)
	}
	if _, err := s.DrainInbox("coder", 10); err != nil {
		t.Fatalf("DrainInbox: %v", err)
	}

	// Today MarkRead never errors; after leaf 06 it may either skip the
	// queued id silently or refuse it with an error. Both are acceptable —
	// the hard pin is the ROW STATE below.
	if err := s.MarkRead([]string{queued.ID, delivered.ID}); err != nil {
		t.Logf("MarkRead returned an error (acceptable refusal shape if the queued row stays queued): %v", err)
	}

	stateOf := func(id string) string {
		t.Helper()
		var st string
		if err := s.db.QueryRowContext(ctx, `SELECT state FROM agent_messages WHERE id = ?`, id).Scan(&st); err != nil {
			t.Fatalf("read state of %s: %v", id, err)
		}
		return st
	}

	if got := stateOf(queued.ID); got != MessageStateQueued {
		t.Errorf("MarkRead moved a QUEUED message to %q: queued->read must be refused — "+
			"a message may only pass through delivered (delivery receipt erased by the skip)", got)
	}
	if got := stateOf(delivered.ID); got != MessageStateRead {
		t.Errorf("delivered message after MarkRead: state = %q, want read", got)
	}
}
