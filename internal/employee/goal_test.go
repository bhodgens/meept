package employee

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testGoalStore returns a fresh GoalStore backed by a temp-file SQLite DB,
// with foreign-key enforcement ON so the ON DELETE CASCADE spec is
// exercised. The store (and its DB handle) are closed on test cleanup.
func testGoalStore(t *testing.T) *GoalStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "goals.db")
	store, err := NewGoalStore(path, nil)
	if err != nil {
		t.Fatalf("NewGoalStore: %v", err)
	}
	// bot_definitions is created by the bot package; for unit tests we need
	// just enough schema for FK references to resolve.
	if _, err := store.db.Exec(`
CREATE TABLE IF NOT EXISTS bot_definitions (
    id TEXT PRIMARY KEY,
    data TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`); err != nil {
		t.Fatalf("create bot_definitions stub: %v", err)
	}
	// Seed one bot row so employee_id FK can resolve.
	if _, err := store.db.Exec(
		`INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"bot-test-1", "{}", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed bot_definitions: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustCreateGoal(t *testing.T, store *GoalStore, g *Goal) *Goal {
	t.Helper()
	ctx := context.Background()
	if g == nil {
		g = &Goal{}
	}
	fillDefaults(g)
	if err := store.Create(ctx, g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return g
}

// fillDefaults populates required fields if empty, so tests can construct a
// minimal Goal and let the helper fill the rest.
func fillDefaults(g *Goal) {
	if g.ID == "" {
		g.ID = NewGoalID()
	}
	if g.EmployeeID == "" {
		g.EmployeeID = "bot-test-1"
	}
	if g.Title == "" {
		g.Title = "keep CI green for main"
	}
	if g.Mandate == "" {
		g.Mandate = "main branch CI run must stay green"
	}
	if g.Source == "" {
		g.Source = SourceUser
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	}
}

// --------------------------------------------------------------------------
// State / health enum tests
// --------------------------------------------------------------------------

func TestGoalState_String(t *testing.T) {
	tests := []struct {
		state GoalState
		want  string
	}{
		{GoalActive, "active"},
		{GoalPaused, "paused"},
		{GoalRetired, "retired"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("GoalState(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestParseGoalState_RoundTrip(t *testing.T) {
	for _, s := range []GoalState{GoalActive, GoalPaused, GoalRetired} {
		got, err := ParseGoalState(s.String())
		if err != nil {
			t.Errorf("ParseGoalState(%q): %v", s.String(), err)
			continue
		}
		if got != s {
			t.Errorf("ParseGoalState(%q) = %v, want %v", s.String(), got, s)
		}
	}
}

func TestParseGoalState_Unknown(t *testing.T) {
	if _, err := ParseGoalState("dormant"); err == nil {
		t.Fatal("expected error for unknown state")
	}
}

func TestGoalHealth_String(t *testing.T) {
	tests := []struct {
		health GoalHealth
		want   string
	}{
		{GoalHealthy, "healthy"},
		{GoalAtRisk, "at_risk"},
		{GoalBroken, "broken"},
		{GoalUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.health.String(); got != tt.want {
			t.Errorf("GoalHealth(%d).String() = %q, want %q", int(tt.health), got, tt.want)
		}
	}
}

func TestParseGoalHealth_RoundTrip(t *testing.T) {
	for _, h := range []GoalHealth{GoalHealthy, GoalAtRisk, GoalBroken, GoalUnknown} {
		got, err := ParseGoalHealth(h.String())
		if err != nil {
			t.Errorf("ParseGoalHealth(%q): %v", h.String(), err)
			continue
		}
		if got != h {
			t.Errorf("ParseGoalHealth(%q) = %v, want %v", h.String(), got, h)
		}
	}
}

func TestParseGoalHealth_Unknown(t *testing.T) {
	if _, err := ParseGoalHealth("on_fire"); err == nil {
		t.Fatal("expected error for unknown health")
	}
}

// --------------------------------------------------------------------------
// GoalSource tests
// --------------------------------------------------------------------------

func TestGoalSource_Values(t *testing.T) {
	got := []GoalSource{SourceUser, SourceTrigger, SourceSelfProposed, SourceAuditFinding}
	want := []string{"user", "trigger", "self_proposed", "audit_finding"}
	for i := range got {
		if string(got[i]) != want[i] {
			t.Errorf("source[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --------------------------------------------------------------------------
// Goal in-memory helper tests (ActivePlan / History / Assess)
// --------------------------------------------------------------------------

func TestGoal_ActivePlan(t *testing.T) {
	g := &Goal{ActivePlanID: "plan-1"}
	if got := g.ActivePlan(); got != "plan-1" {
		t.Errorf("ActivePlan = %q, want plan-1", got)
	}
	g.SetActivePlan("plan-2")
	if got := g.ActivePlan(); got != "plan-2" {
		t.Errorf("ActivePlan = %q, want plan-2", got)
	}
}

func TestGoal_History_NilSafe(t *testing.T) {
	g := &Goal{}
	if h := g.History(); h != nil {
		t.Errorf("History of empty goal = %v, want nil", h)
	}
}

func TestGoal_AppendHistory(t *testing.T) {
	g := &Goal{}
	if n := g.AppendHistory("p1"); n != 1 {
		t.Fatalf("AppendHistory returned %d, want 1", n)
	}
	if n := g.AppendHistory("p2"); n != 2 {
		t.Fatalf("AppendHistory returned %d, want 2", n)
	}
	h := g.History()
	if len(h) != 2 || h[0] != "p1" || h[1] != "p2" {
		t.Errorf("History = %v, want [p1 p2]", h)
	}
}

func TestGoal_History_DefensiveCopy(t *testing.T) {
	g := &Goal{PlanHistory: []string{"p1"}}
	h := g.History()
	h[0] = "MUTATED"
	if g.PlanHistory[0] != "p1" {
		t.Fatalf("History() did not return a copy; underlying slice mutated")
	}
}

func TestGoal_Assess(t *testing.T) {
	g := &Goal{Health: GoalUnknown}
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	g.Assess(GoalAtRisk, now)
	if g.Health != GoalAtRisk {
		t.Errorf("Health = %v, want GoalAtRisk", g.Health)
	}
	if !g.LastAssessed.Equal(now) {
		t.Errorf("LastAssessed = %v, want %v", g.LastAssessed, now)
	}
}

func TestGoal_IsRetired(t *testing.T) {
	g := &Goal{}
	if g.IsRetired() {
		t.Fatal("fresh goal should not be retired")
	}
	g.RetiredAt = time.Now().UTC()
	if !g.IsRetired() {
		t.Fatal("goal with RetiredAt should report retired")
	}
}

func TestGoal_LockUnlock(t *testing.T) {
	// Verify the embedded RWMutex doesn't deadlock on basic usage.
	g := &Goal{Title: "test"}
	g.Lock()
	g.Title = "modified"
	g.Unlock()
	if g.Title != "modified" {
		t.Error("Lock/Unlock did not protect the field write")
	}
}

// --------------------------------------------------------------------------
// GoalStore CRUD tests
// --------------------------------------------------------------------------

func TestGoalStore_CreateAndGet(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	original := &Goal{
		EmployeeID:   "bot-test-1",
		Title:        "ship v2",
		Mandate:      "deliver v2 by Q3",
		Source:       SourceUser,
		Health:       GoalUnknown,
		TriggerRef:   "schedule:nightly",
		ActivePlanID: "plan-0",
		PlanHistory:  []string{"plan-old"},
		CreatedAt:    time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Create(ctx, original); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if original.ID == "" {
		t.Fatal("Create did not assign an ID")
	}

	got, err := store.Get(ctx, original.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
	if got.Title != "ship v2" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.State != GoalActive {
		t.Errorf("State = %v, want GoalActive", got.State)
	}
	if got.Source != SourceUser {
		t.Errorf("Source = %q, want %q", got.Source, SourceUser)
	}
	if got.TriggerRef != "schedule:nightly" {
		t.Errorf("TriggerRef = %q", got.TriggerRef)
	}
	if got.Health != GoalUnknown {
		t.Errorf("Health = %v, want GoalUnknown", got.Health)
	}
	if got.ActivePlanID != "plan-0" {
		t.Errorf("ActivePlanID = %q", got.ActivePlanID)
	}
	if len(got.PlanHistory) != 1 || got.PlanHistory[0] != "plan-old" {
		t.Errorf("PlanHistory = %v, want [plan-old]", got.PlanHistory)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, original.CreatedAt)
	}
	if !got.LastAssessed.IsZero() {
		t.Errorf("LastAssessed = %v, want zero", got.LastAssessed)
	}
	if !got.RetiredAt.IsZero() {
		t.Errorf("RetiredAt = %v, want zero", got.RetiredAt)
	}
}

func TestGoalStore_Get_NotFound(t *testing.T) {
	store := testGoalStore(t)
	_, err := store.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("Get(unknown) err = %v, want ErrGoalNotFound", err)
	}
}

func TestGoalStore_Create_NilGoal(t *testing.T) {
	store := testGoalStore(t)
	if err := store.Create(context.Background(), nil); err == nil {
		t.Fatal("Create(nil) should error")
	}
}

func TestGoalStore_Create_InvalidState(t *testing.T) {
	store := testGoalStore(t)
	g := &Goal{
		EmployeeID: "bot-test-1",
		Title:      "bad",
		Mandate:    "bad",
		State:      GoalState(99),
		Source:     SourceUser,
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.Create(context.Background(), g); err == nil {
		t.Fatal("Create with invalid state should error")
	}
}

func TestGoalStore_ListByEmployee(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()

	// Use a distinct employee for this test.
	const emp = "bot-list-emp"
	seedBot(t, store, emp)

	for _, title := range []string{"g1", "g2", "g3"} {
		g := &Goal{
			EmployeeID: emp,
			Title:      title,
			Mandate:    "m-" + title,
			Source:     SourceTrigger,
			CreatedAt:  time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
		}
		if err := store.Create(ctx, g); err != nil {
			t.Fatalf("Create(%s): %v", title, err)
		}
	}

	// Add one for a different employee to ensure filtering works.
	other := &Goal{
		EmployeeID: "bot-test-1",
		Title:      "other",
		Mandate:    "m",
		Source:     SourceUser,
		CreatedAt:  time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Create(ctx, other); err != nil {
		t.Fatalf("Create(other): %v", err)
	}

	got, err := store.ListByEmployee(ctx, emp)
	if err != nil {
		t.Fatalf("ListByEmployee: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, g := range got {
		if g.EmployeeID != emp {
			t.Errorf("row %d EmployeeID = %q, want %q", i, g.EmployeeID, emp)
		}
	}
}

func TestGoalStore_ListActive(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	const emp = "bot-active-emp"
	seedBot(t, store, emp)

	// Two active, one retired, one for a different employee.
	active1 := mustCreateGoal(t, store, &Goal{EmployeeID: emp, Title: "a1", Mandate: "m", Source: SourceUser})
	active2 := mustCreateGoal(t, store, &Goal{EmployeeID: emp, Title: "a2", Mandate: "m", Source: SourceUser})
	retired := mustCreateGoal(t, store, &Goal{EmployeeID: emp, Title: "r1", Mandate: "m", Source: SourceUser})
	other := mustCreateGoal(t, store, &Goal{EmployeeID: "bot-test-1", Title: "o1", Mandate: "m", Source: SourceUser})

	if err := store.Retire(ctx, retired.ID, time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	// Without filter: all non-retired goals (active1, active2, other, plus any seeded).
	gotAll, err := store.ListActive(ctx, "")
	if err != nil {
		t.Fatalf("ListActive all: %v", err)
	}
	ids := goalIDs(gotAll)
	mustContain(t, ids, active1.ID)
	mustContain(t, ids, active2.ID)
	mustContain(t, ids, other.ID)
	mustNotContain(t, ids, retired.ID)

	// With employee filter: only emp's active goals.
	gotEmp, err := store.ListActive(ctx, emp)
	if err != nil {
		t.Fatalf("ListActive(emp): %v", err)
	}
	empIDs := goalIDs(gotEmp)
	mustContain(t, empIDs, active1.ID)
	mustContain(t, empIDs, active2.ID)
	mustNotContain(t, empIDs, other.ID)
	mustNotContain(t, empIDs, retired.ID)
}

// --------------------------------------------------------------------------
// Update tests
// --------------------------------------------------------------------------

func TestGoalStore_Update_StateTransition(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()

	type transition struct {
		name  string
		from  GoalState
		to    GoalState
		valid bool
	}
	transitions := []transition{
		{"active→paused", GoalActive, GoalPaused, true},
		{"paused→active", GoalPaused, GoalActive, true},
		{"active→retired", GoalActive, GoalRetired, true},
		{"paused→retired", GoalPaused, GoalRetired, true},
		// retired is terminal in our model; the store layer itself does not
		// enforce this, but the test verifies state survives the round-trip
		// regardless.
	}

	for _, tr := range transitions {
		t.Run(tr.name, func(t *testing.T) {
			g := mustCreateGoal(t, store, &Goal{
				EmployeeID: "bot-test-1",
				Title:      tr.name,
				Mandate:    "m",
				Source:     SourceUser,
				State:      tr.from,
			})
			g.State = tr.to
			if err := store.Update(ctx, g); err != nil {
				t.Fatalf("Update: %v", err)
			}
			got, err := store.Get(ctx, g.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.State != tr.to {
				t.Errorf("State = %v, want %v", got.State, tr.to)
			}
		})
	}
}

func TestGoalStore_Update_NotFound(t *testing.T) {
	store := testGoalStore(t)
	g := &Goal{
		ID:         "nonexistent",
		EmployeeID: "bot-test-1",
		Title:      "t",
		Mandate:    "m",
		Source:     SourceUser,
		State:      GoalActive,
	}
	if err := store.Update(context.Background(), g); !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("Update(unknown) err = %v, want ErrGoalNotFound", err)
	}
}

func TestGoalStore_Update_NilGoal(t *testing.T) {
	store := testGoalStore(t)
	if err := store.Update(context.Background(), nil); err == nil {
		t.Fatal("Update(nil) should error")
	}
}

func TestGoalStore_Update_PlanHistory(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	g := mustCreateGoal(t, store, &Goal{
		EmployeeID:  "bot-test-1",
		Title:       "ph",
		Mandate:     "m",
		Source:      SourceUser,
		PlanHistory: []string{"p1"},
	})

	g.AppendHistory("p2")
	g.AppendHistory("p3")
	g.SetActivePlan("p3-active")

	if err := store.Update(ctx, g); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	h := got.History()
	if len(h) != 3 {
		t.Fatalf("PlanHistory len = %d, want 3", len(h))
	}
	want := []string{"p1", "p2", "p3"}
	for i, w := range want {
		if h[i] != w {
			t.Errorf("PlanHistory[%d] = %q, want %q", i, h[i], w)
		}
	}
	if got.ActivePlan() != "p3-active" {
		t.Errorf("ActivePlanID = %q, want p3-active", got.ActivePlan())
	}
}

func TestGoalStore_Update_HealthComputation(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()

	steps := []struct {
		health GoalHealth
		time   string
	}{
		{GoalHealthy, "2026-06-23T12:00:00Z"},
		{GoalAtRisk, "2026-06-23T13:00:00Z"},
		{GoalBroken, "2026-06-23T14:00:00Z"},
		{GoalUnknown, "2026-06-23T15:00:00Z"},
	}

	g := mustCreateGoal(t, store, &Goal{
		EmployeeID: "bot-test-1",
		Title:      "hc",
		Mandate:    "m",
		Source:     SourceUser,
		Health:     GoalUnknown,
	})

	for i, step := range steps {
		ts, _ := time.Parse(time.RFC3339, step.time)
		g.Health = step.health
		g.LastAssessed = ts
		if err := store.Update(ctx, g); err != nil {
			t.Fatalf("Update[%d]: %v", i, err)
		}
		got, err := store.Get(ctx, g.ID)
		if err != nil {
			t.Fatalf("Get[%d]: %v", i, err)
		}
		if got.Health != step.health {
			t.Errorf("step %d Health = %v, want %v", i, got.Health, step.health)
		}
		if !got.LastAssessed.Equal(ts) {
			t.Errorf("step %d LastAssessed = %v, want %v", i, got.LastAssessed, ts)
		}
	}
}

// --------------------------------------------------------------------------
// Retire tests
// --------------------------------------------------------------------------

func TestGoalStore_Retire_SoftDelete(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	g := mustCreateGoal(t, store, &Goal{
		EmployeeID: "bot-test-1",
		Title:      "retire-me",
		Mandate:    "m",
		Source:     SourceUser,
	})
	now := time.Date(2026, 6, 23, 16, 0, 0, 0, time.UTC)

	if err := store.Retire(ctx, g.ID, now); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	got, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get after retire: %v", err)
	}
	if got.State != GoalRetired {
		t.Errorf("State = %v, want GoalRetired", got.State)
	}
	if !got.RetiredAt.Equal(now) {
		t.Errorf("RetiredAt = %v, want %v", got.RetiredAt, now)
	}

	// ListActive should exclude retired goals.
	active, err := store.ListActive(ctx, "bot-test-1")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, a := range active {
		if a.ID == g.ID {
			t.Fatal("retired goal appeared in ListActive")
		}
	}

	// ListByEmployee should still include it (it returns all goals).
	all, err := store.ListByEmployee(ctx, "bot-test-1")
	if err != nil {
		t.Fatalf("ListByEmployee: %v", err)
	}
	found := false
	for _, a := range all {
		if a.ID == g.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("retired goal missing from ListByEmployee")
	}
}

func TestGoalStore_Retire_NotFound(t *testing.T) {
	store := testGoalStore(t)
	err := store.Retire(context.Background(), "missing", time.Now().UTC())
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("Retire(missing) = %v, want ErrGoalNotFound", err)
	}
}

func TestGoalStore_Retire_DefaultsNow(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	g := mustCreateGoal(t, store, nil)
	before := time.Now().Add(-time.Second)
	if err := store.Retire(ctx, g.ID, time.Time{}); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	got, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetiredAt.Before(before) {
		t.Errorf("RetiredAt = %v, expected ~now", got.RetiredAt)
	}
}

// --------------------------------------------------------------------------
// Concurrency test: snapshot under lock pattern
// --------------------------------------------------------------------------

// TestGoalStore_Update_ConcurrentSafe verifies that concurrent goroutines
// can mutate the in-memory Goal via the lock-protected helpers and then
// serialize their persistence through the store. SQLite with WAL +
// busy_timeout handles the write serialization; the Goal mutex handles the
// in-memory snapshot.
//
// We do NOT slam the store with 20 parallel Exec calls because even with
// busy_timeout that produces flaky tests on slow CI. Instead each goroutine
// mutates the goal, then takes a turn at a serialization mutex before
// calling Update. This proves (a) the in-memory helpers are race-free and
// (b) sequential Updates preserve the snapshot-under-lock contract.
func TestGoalStore_Update_ConcurrentSafe(t *testing.T) {
	store := testGoalStore(t)
	ctx := context.Background()
	g := mustCreateGoal(t, store, &Goal{
		EmployeeID:  "bot-test-1",
		Title:       "concurrent",
		Mandate:     "m",
		Source:      SourceUser,
		PlanHistory: []string{},
	})

	var (
		wg       sync.WaitGroup
		writeMu  sync.Mutex // serializes Update calls
		errCount int
		errMu    sync.Mutex
	)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// In-memory mutation under the goal's own lock.
			g.AppendHistory("p-" + string(rune('a'+n)))

			// Serialize persistence.
			writeMu.Lock()
			defer writeMu.Unlock()
			if err := store.Update(ctx, g); err != nil {
				errMu.Lock()
				errCount++
				errMu.Unlock()
				t.Errorf("Update: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if errCount != 0 {
		t.Fatalf("%d concurrent Updates failed", errCount)
	}

	// Final state: 20 history entries, all persisted.
	got, err := store.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.History()) != 20 {
		t.Errorf("History len = %d, want 20", len(got.History()))
	}
}

// --------------------------------------------------------------------------
// Marshal edge cases
// --------------------------------------------------------------------------

func TestMarshalHistory_NilReturnsBrackets(t *testing.T) {
	got, err := marshalHistory(nil)
	if err != nil {
		t.Fatalf("marshalHistory(nil): %v", err)
	}
	if got != "[]" {
		t.Errorf("marshalHistory(nil) = %q, want []", got)
	}
}

func TestMarshalHistory_RoundTrip(t *testing.T) {
	in := []string{"a", "b", "c"}
	s, err := marshalHistory(in)
	if err != nil {
		t.Fatalf("marshalHistory: %v", err)
	}
	var out []string
	if err := jsonUnmarshal(s, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len = %d, want %d", len(out), len(in))
	}
	for i, v := range in {
		if out[i] != v {
			t.Errorf("[%d] = %q, want %q", i, out[i], v)
		}
	}
}

// --------------------------------------------------------------------------
// NewGoalID
// --------------------------------------------------------------------------

func TestNewGoalID_HasPrefixAndUniqueness(t *testing.T) {
	id1 := NewGoalID()
	id2 := NewGoalID()
	if len(id1) <= len(GoalIDPrefix) {
		t.Fatalf("ID too short: %q", id1)
	}
	if id1[:len(GoalIDPrefix)] != GoalIDPrefix {
		t.Errorf("ID prefix = %q, want %q", id1[:len(GoalIDPrefix)], GoalIDPrefix)
	}
	if id1 == id2 {
		t.Errorf("NewGoalID not unique: %q == %q", id1, id2)
	}
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// seedBot inserts a stub bot row so employee FK references resolve.
func seedBot(t *testing.T, store *GoalStore, botID string) {
	t.Helper()
	_, err := store.db.Exec(
		`INSERT OR IGNORE INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		botID, "{}", time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seed bot %q: %v", botID, err)
	}
}

func goalIDs(gs []*Goal) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.ID
	}
	return out
}

func mustContain(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			return
		}
	}
	t.Errorf("IDs %v do not contain %q", ids, want)
}

func mustNotContain(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			t.Errorf("IDs %v contain %q (should not)", ids, want)
			return
		}
	}
}

// jsonUnmarshal is a tiny wrapper so tests don't import encoding/json
// directly.
func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

// sql.ErrNoRows guard: testStore relies on it being covered by the database/sql
// import.
var _ = sql.ErrNoRows
