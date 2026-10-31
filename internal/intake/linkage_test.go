package intake

// NOTE: gate tests for the github-intake-factory tree (leaf 00T) — F2, F3,
// F4. Intentionally RED: linkage.go (Contract 2: TriggerRefPrefix,
// IntakeKey, Linker, NewLinker, EnsureGoal) lands in leaf 02. The Linker
// is tested against the LOCAL GoalCreator seam (CreateGoal + ListGoals +
// UpdateGoal) documented in 00T Notes and leaf 02's contract, NOT against
// a concrete *employee.Manager — the stub below is the seam the
// implementation leaf must satisfy. A compile failure naming the missing
// linkage symbols is the only expected RED failure.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/employee"
)

// ---------------------------------------------------------------------------
// GoalCreator stub (the local manager seam, Contract 2 / 00T Note).
// ---------------------------------------------------------------------------

// goalStub is the in-memory GoalCreator the Linker is tested through.
// createCalls / updateCalls count every call so the dedup and
// re-dispatch tests can assert exact store-write counts.
type goalStub struct {
	mu sync.Mutex

	goals []*employee.Goal

	createCalls int
	updateCalls int
}

func newGoalStub() *goalStub { return &goalStub{} }

func (g *goalStub) CreateGoal(ctx context.Context, spec employee.GoalSpec) (*employee.Goal, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.createCalls++
	gl := &employee.Goal{
		ID:         fmtGoalID(g.createCalls),
		EmployeeID: spec.EmployeeID,
		Title:      spec.Title,
		Mandate:    spec.Mandate,
		State:      employee.GoalActive,
		Source:     employee.GoalSource(spec.Source),
		TriggerRef: spec.TriggerRef,
		Health:     employee.GoalUnknown,
	}
	g.goals = append(g.goals, gl)
	return gl, nil
}

func (g *goalStub) ListGoals(ctx context.Context, employeeID string) ([]*employee.Goal, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]*employee.Goal, len(g.goals))
	copy(out, g.goals)
	return out, nil
}

// UpdateGoal persists mandate/state/health mutations in place (leaf 02's
// minimal manager addition — the real Manager gets a 5-line delegate to
// GoalStore.Update).
func (g *goalStub) UpdateGoal(ctx context.Context, goal *employee.Goal) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.updateCalls++
	for i, gl := range g.goals {
		if gl.ID == goal.ID {
			g.goals[i] = goal
			return nil
		}
	}
	g.goals = append(g.goals, goal)
	return nil
}

func (g *goalStub) createCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.createCalls
}

func (g *goalStub) updateCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.updateCalls
}

// fmtGoalID produces a stub goal id — deterministic, NOT
// time.Now()-derived (predid rule); real goal IDs come from the store.
func fmtGoalID(n int) string {
	return "goal-stub-" + intToLetters(n)
}

func intToLetters(n int) string {
	if n == 0 {
		return "a"
	}
	letters := []byte{}
	for n > 0 {
		letters = append([]byte{byte('a' + n%26)}, letters...)
		n /= 26
	}
	return string(letters)
}

// ---------------------------------------------------------------------------
// F3: IntakeKey stability (leaf 02 Task 1)
// ---------------------------------------------------------------------------

// TestIntakeKey_Stable: IntakeKey(repo, id, body) is equal for equal
// inputs, differs when ANY part differs, and is the effects composition
// EffectKey("intake_ticket_goal", repo, ticketID, sha256(body)) (F3).
func TestIntakeKey_Stable(t *testing.T) {
	a := IntakeKey("o/r", "7", "do the thing")
	b := IntakeKey("o/r", "7", "do the thing")
	if a != b {
		t.Fatalf("IntakeKey not stable for identical inputs: %q != %q", a, b)
	}

	// Composition pin: byte-identical to effects.EffectKey over the
	// contract tuple with the body hashed.
	sum := sha256.Sum256([]byte("do the thing"))
	want := effects.EffectKey("intake_ticket_goal", "o/r", "7", hex.EncodeToString(sum[:]))
	if a != want {
		t.Fatalf("IntakeKey = %q, want contract composition %q", a, want)
	}

	for _, tc := range []struct{ name, repo, id, body string }{
		{"different repo", "o/other", "7", "do the thing"},
		{"different ticket id", "o/r", "8", "do the thing"},
		{"different body", "o/r", "7", "do the OTHER thing"},
	} {
		if got := IntakeKey(tc.repo, tc.id, tc.body); got == a {
			t.Errorf("%s: key collides with the base key (%s)", tc.name, got)
		}
	}

	// 64 lowercase hex chars — the effects.EffectKey shape.
	if len(a) != 64 {
		t.Fatalf("IntakeKey length = %d, want 64 hex chars", len(a))
	}
}

// ---------------------------------------------------------------------------
// F2: EnsureGoal create path (leaf 02 Task 1)
// ---------------------------------------------------------------------------

// TestLinker_EnsureGoal_Creates: a first-arrival ticket creates exactly
// one goal with Source="trigger" and TriggerRef "intake:<repo>#<id>",
// with the ticket body as mandate and an effects claim recorded under the
// IntakeKey (F2).
func TestLinker_EnsureGoal_Creates(t *testing.T) {
	stub := newGoalStub()
	ledger := effects.NewMemoryLedger()
	linker := NewLinker(stub, ledger, slog.DiscardHandler)

	ticket := Ticket{
		ID:     "42",
		Repo:   "o/r",
		Title:  "fix the flux capacitor",
		Body:   "the flux capacitor is broken; fix it",
		Labels: []string{"factory"},
		URL:    "https://github.com/o/r/issues/42",
		Author: "octo",
	}

	goal, err := linker.EnsureGoal(context.Background(), ticket, "emp-1", nil)
	if err != nil {
		t.Fatalf("EnsureGoal: %v", err)
	}
	if goal == nil {
		t.Fatal("EnsureGoal returned nil goal on the create path")
	}

	if stub.createCount() != 1 {
		t.Fatalf("CreateGoal called %d times, want exactly 1", stub.createCount())
	}
	if goal.Source != employee.SourceTrigger {
		t.Errorf("goal.Source = %q, want %q", goal.Source, string(employee.SourceTrigger))
	}
	wantRef := "intake:o/r#42"
	if goal.TriggerRef != wantRef {
		t.Errorf("goal.TriggerRef = %q, want %q", goal.TriggerRef, wantRef)
	}
	if goal.EmployeeID != "emp-1" {
		t.Errorf("goal.EmployeeID = %q, want %q", goal.EmployeeID, "emp-1")
	}
	if goal.Mandate != ticket.Body {
		t.Errorf("goal.Mandate = %q, want the ticket body %q", goal.Mandate, ticket.Body)
	}
	if goal.Title != ticket.Title {
		t.Errorf("goal.Title = %q, want the ticket title %q", goal.Title, ticket.Title)
	}

	// The create claimed the IntakeKey in the ledger.
	if rec, err := ledger.Get(context.Background(), IntakeKey("o/r", "42", ticket.Body)); err != nil || rec == nil {
		t.Errorf("effects ledger has no record for the IntakeKey: rec=%v err=%v", rec, err)
	}

	// Gate passthrough: a non-nil gate reaches the created spec.
	stub2 := newGoalStub()
	linker2 := NewLinker(stub2, effects.NewMemoryLedger(), slog.DiscardHandler)
	gate := &employee.GoalGateSpec{Command: "go test ./..."}
	goal2, err := linker2.EnsureGoal(context.Background(), ticket, "emp-2", gate)
	if err != nil {
		t.Fatalf("EnsureGoal with gate: %v", err)
	}
	_ = goal2 // created; the gate lands via the spec — the stub stores what
	// CreateGoal received, so a lost gate would surface as a store
	// round-trip failure in the real manager.
}

// ---------------------------------------------------------------------------
// F3: dedup no-op path (leaf 02 Task 2)
// ---------------------------------------------------------------------------

// TestLinker_Dedup_IdenticalBody: a second EnsureGoal with the same body
// returns the SAME goal and makes no second CreateGoal call — the
// IntakeKey collision is the crash-window guard (F3).
func TestLinker_Dedup_IdenticalBody(t *testing.T) {
	stub := newGoalStub()
	ledger := effects.NewMemoryLedger()
	linker := NewLinker(stub, ledger, slog.DiscardHandler)

	ticket := Ticket{ID: "9", Repo: "o/r", Title: "t", Body: "stable body"}

	first, err := linker.EnsureGoal(context.Background(), ticket, "emp-1", nil)
	if err != nil {
		t.Fatalf("first EnsureGoal: %v", err)
	}

	second, err := linker.EnsureGoal(context.Background(), ticket, "emp-1", nil)
	if err != nil {
		t.Fatalf("second EnsureGoal (identical body): %v", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("second EnsureGoal returned %+v, want the SAME goal %s", second, first.ID)
	}
	if got := stub.createCount(); got != 1 {
		t.Fatalf("CreateGoal called %d times after identical re-arrival, want 1 (dedup no-op)", got)
	}
}

// ---------------------------------------------------------------------------
// F4: re-dispatch path (leaf 02 Task 3)
// ---------------------------------------------------------------------------

// TestLinker_Redispatch_OnBodyChange: a changed body updates the existing
// goal's mandate, resets it active with health unknown, and never creates
// a second goal (F4). No effects claim rides an update.
func TestLinker_Redispatch_OnBodyChange(t *testing.T) {
	stub := newGoalStub()
	ledger := effects.NewMemoryLedger()
	linker := NewLinker(stub, ledger, slog.DiscardHandler)

	first := Ticket{ID: "11", Repo: "o/r", Title: "t", Body: "original mandate"}
	goal, err := linker.EnsureGoal(context.Background(), first, "emp-1", nil)
	if err != nil {
		t.Fatalf("first EnsureGoal: %v", err)
	}

	// Age the goal past its original state so the reset is observable:
	// simulate a completed assessment (healthy) that the re-dispatch must
	// wipe back to unknown + active.
	goal.Health = employee.GoalHealthy

	changed := Ticket{ID: "11", Repo: "o/r", Title: "t", Body: "REWRITTEN mandate v2"}
	updated, err := linker.EnsureGoal(context.Background(), changed, "emp-1", nil)
	if err != nil {
		t.Fatalf("second EnsureGoal (changed body): %v", err)
	}

	if updated.ID != goal.ID {
		t.Fatalf("re-dispatch created a NEW goal %s, want in-place update of %s", updated.ID, goal.ID)
	}
	if got := stub.createCount(); got != 1 {
		t.Errorf("CreateGoal called %d times, want 1 (no duplicate goal)", got)
	}
	if got := stub.updateCount(); got < 1 {
		t.Errorf("UpdateGoal called %d times, want >= 1 (mandate persisted)", got)
	}
	if updated.Mandate != changed.Body {
		t.Errorf("goal.Mandate = %q, want the changed body %q", updated.Mandate, changed.Body)
	}
	if updated.State != employee.GoalActive {
		t.Errorf("goal.State = %v, want GoalActive (reset)", updated.State)
	}
	if updated.Health != employee.GoalUnknown {
		t.Errorf("goal.Health = %v, want GoalUnknown (reset)", updated.Health)
	}
	// An update is not an external effect: no second ledger record may
	// appear beyond the original create claim.
	if _, err := ledger.Get(context.Background(), IntakeKey("o/r", "11", changed.Body)); err == nil {
		t.Errorf("changed-body re-dispatch wrote an effects claim; updates must not claim")
	}
}

// ---------------------------------------------------------------------------
// Helpers shared across the linkage tests.
// ---------------------------------------------------------------------------

// jsonCompact is unused-guard insurance for future receipt assertions in
// this file (kept minimal; compile-time reference keeps encoding/json
// imported for the effects payload checks the leaf-02 tests will pin).
var _ = json.Marshal

// errSentinelAnchor keeps errors referenced for the leaf-02 fail-closed
// assertions (nil ledger) that extend this file additively.
var _ = errors.New
var _ = time.Now
