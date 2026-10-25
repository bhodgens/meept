package employee

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// mockBusPublisher captures PublishEmployeePaused and PublishCriticalFinding
// and PublishConstitutionValidationError calls for testing.
type mockBusPublisher struct {
	mu           sync.Mutex
	events       []EmployeePausedEvent
	criticalEvts []CriticalFindingEvent
	constErrors  []ConstitutionValidationErrorEvent
}

func (m *mockBusPublisher) PublishEmployeePaused(employeeID, reason, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, EmployeePausedEvent{
		EmployeeID: employeeID,
		Reason:     reason,
		Source:     source,
	})
}

func (m *mockBusPublisher) PublishCriticalFinding(employeeID, findingID, violatedRule, evidence string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.criticalEvts = append(m.criticalEvts, CriticalFindingEvent{
		EmployeeID:   employeeID,
		FindingID:    findingID,
		ViolatedRule: violatedRule,
		Evidence:     evidence,
	})
}

func (m *mockBusPublisher) PublishConstitutionValidationError(employeeID, validationError, constitutionSummary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.constErrors = append(m.constErrors, ConstitutionValidationErrorEvent{
		EmployeeID:          employeeID,
		ValidationError:     validationError,
		ConstitutionSummary: constitutionSummary,
	})
}

func (m *mockBusPublisher) getEvents() []EmployeePausedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]EmployeePausedEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

func (m *mockBusPublisher) getCriticalEvents() []CriticalFindingEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]CriticalFindingEvent, len(m.criticalEvts))
	copy(cp, m.criticalEvts)
	return cp
}

// newTestManagerWithBot creates a Manager backed by a temp SQLite bot store
// with a single bot registered, so Pause/Resume can be tested end-to-end.
func newTestManagerWithBot(t *testing.T, botID string) (*Manager, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	bm := bot.NewManager(store, nil)
	mgr := NewManager(bm)

	// Create a bot definition in the store so PauseBot has something to pause.
	ctx := context.Background()
	def := bot.BotDefinition{
		ID:      botID,
		Name:    "test-bot",
		Enabled: true,
	}
	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("create bot: %v", err)
	}

	cleanup := func() {
		store.Close()
	}
	return mgr, cleanup
}

// TestManager_Pause_PublishesBusEvent verifies that Manager.Pause publishes
// an employee.paused bus event when a BusPublisher is wired (spec line 383).
func TestManager_Pause_PublishesBusEvent(t *testing.T) {
	t.Parallel()

	const botID = "emp-test-1"
	mgr, cleanup := newTestManagerWithBot(t, botID)
	defer cleanup()

	pub := &mockBusPublisher{}
	mgr.SetBusPublisher(pub)

	if err := mgr.Pause(context.Background(), botID); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	events := pub.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 bus event, got %d", len(events))
	}
	ev := events[0]
	if ev.EmployeeID != botID {
		t.Errorf("employee_id: want %q, got %q", botID, ev.EmployeeID)
	}
	if ev.Source != "operator" {
		t.Errorf("source: want %q, got %q", "operator", ev.Source)
	}
	if ev.Reason == "" {
		t.Error("reason should not be empty")
	}
}

// TestManager_PauseWithReason_PublishesBusEvent verifies that the source and
// reason from PauseWithReason propagate to the bus event.
func TestManager_PauseWithReason_PublishesBusEvent(t *testing.T) {
	t.Parallel()

	const botID = "emp-test-2"
	mgr, cleanup := newTestManagerWithBot(t, botID)
	defer cleanup()

	pub := &mockBusPublisher{}
	mgr.SetBusPublisher(pub)

	if err := mgr.PauseWithReason(context.Background(), botID,
		"critical audit finding: never[2]", "auto_pause"); err != nil {
		t.Fatalf("PauseWithReason failed: %v", err)
	}

	events := pub.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 bus event, got %d", len(events))
	}
	ev := events[0]
	if ev.EmployeeID != botID {
		t.Errorf("employee_id: want %q, got %q", botID, ev.EmployeeID)
	}
	if ev.Source != "auto_pause" {
		t.Errorf("source: want %q, got %q", "auto_pause", ev.Source)
	}
	if ev.Reason != "critical audit finding: never[2]" {
		t.Errorf("reason: want %q, got %q", "critical audit finding: never[2]", ev.Reason)
	}
}

// TestManager_Pause_NoPublisher verifies that Pause works correctly when
// no BusPublisher is wired (backward compatibility — no event, no panic).
func TestManager_Pause_NoPublisher(t *testing.T) {
	t.Parallel()

	const botID = "emp-test-3"
	mgr, cleanup := newTestManagerWithBot(t, botID)
	defer cleanup()

	// No SetBusPublisher — should be a no-op for the bus path.
	if err := mgr.Pause(context.Background(), botID); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}
}

// TestManager_Pause_NilBusPublisherGuard verifies the typed-nil guard on
// SetBusPublisher: passing nil does not set the publisher and subsequent
// Pause calls do not panic.
func TestManager_Pause_NilBusPublisherGuard(t *testing.T) {
	t.Parallel()

	const botID = "emp-test-4"
	mgr, cleanup := newTestManagerWithBot(t, botID)
	defer cleanup()

	// Nil should be ignored (typed-nil guard per CLAUDE.md).
	mgr.SetBusPublisher(nil)

	if err := mgr.Pause(context.Background(), botID); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}
}

// TestManager_SetPostTurnAuditor_NilGuard verifies the typed-nil guard on
// SetPostTurnAuditor (CLAUDE.md requirement: all Set* methods nil-check).
func TestManager_SetPostTurnAuditor_NilGuard(t *testing.T) {
	t.Parallel()
	mgr := NewManager(nil)
	mgr.SetPostTurnAuditor(nil) // should be a no-op
	if mgr.PostTurnAuditor() != nil {
		t.Error("PostTurnAuditor should be nil after nil SetPostTurnAuditor")
	}
}

// ---------------------------------------------------------------------------
// Persistence-orphan regressions (bughunt 2026-10-25 acp-employee smoke).
// ---------------------------------------------------------------------------

// newHirePersistenceManager builds a Manager with real bot/constitution/
// goal/audit stores over one shared SQLite file in a temp dir, plus a raw
// handle on the same file for row counting.
func newHirePersistenceManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bots.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { _ = constStore.Close() })
	goalStore, err := NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { _ = goalStore.Close() })
	auditStore, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })
	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, goalStore, auditStore, nil)
	return m, dbPath
}

// countRows opens the shared DB read-side and counts rows for the given
// query (must have exactly one ? placeholder bound to id).
func countRows(t *testing.T, dbPath, query, id string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open for count: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(query, id).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// assertNoEmployeeRows fails unless definition/state/constitution/goal all
// have zero rows for id — the "hire wrote nothing / retire swept
// everything" invariant.
func assertNoEmployeeRows(t *testing.T, dbPath, id string) {
	t.Helper()
	queries := map[string]string{
		"bot_definitions":        `SELECT COUNT(*) FROM bot_definitions WHERE id = ?`,
		"bot_states":             `SELECT COUNT(*) FROM bot_states WHERE definition_id = ?`,
		"employee_constitutions": `SELECT COUNT(*) FROM employee_constitutions WHERE employee_id = ?`,
		"employee_goals":         `SELECT COUNT(*) FROM employee_goals WHERE employee_id = ?`,
	}
	for table, q := range queries {
		if n := countRows(t, dbPath, q, id); n != 0 {
			t.Errorf("%s: %d rows for %q, want 0", table, n, id)
		}
	}
}

// TestHireInvalidConstitutionLeavesNoRows covers fix 2: a constitution that
// fails decode (type error — live smoke failure) or Validate
// (amendment_policy.requires_approval false — live smoke failure) must write
// ZERO bot rows, and the id must stay free for a corrected re-hire.
func TestHireInvalidConstitutionLeavesNoRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"decode type error", func(cm map[string]any) { cm["constraints"] = "not-an-object" }},
		{"requires_approval false", func(cm map[string]any) {
			cm["amendment_policy"].(map[string]any)["requires_approval"] = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, dbPath := newHirePersistenceManager(t)
			c := validConstitution()
			c.EscalatesTo = []string{"user"}
			cm := constitutionToMap(t, c)
			tc.mutate(cm)

			_, err := m.Hire(ctx, HireRequest{
				ID:           "emp-invalid",
				Name:         "invalid bot",
				Prompt:       "test prompt",
				Enabled:      true,
				Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
				Constitution: cm,
			})
			if err == nil {
				t.Fatal("expected Hire to fail on invalid constitution")
			}
			assertNoEmployeeRows(t, dbPath, "emp-invalid")

			// The id must be reusable: re-hire with a valid constitution.
			if _, err := m.Hire(ctx, HireRequest{
				ID:           "emp-invalid",
				Name:         "fixed bot",
				Prompt:       "test prompt",
				Enabled:      true,
				Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
				Constitution: constitutionToMap(t, c),
			}); err != nil {
				t.Fatalf("re-Hire after failed hire: %v", err)
			}
		})
	}
}

// TestHirePostInsertFailureRollsBack covers the rollback leg of fix 2: when
// a step AFTER the bot-definition insert fails (goal creation), Hire must
// remove the definition, state, and constitution rows so the id is free.
func TestHirePostInsertFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, dbPath := newHirePersistenceManager(t)

	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	_, err := m.Hire(ctx, HireRequest{
		ID:           "emp-rollback",
		Name:         "rollback bot",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
		Goals:        []GoalSpec{{Title: "", Mandate: "no title"}}, // invalid: fails after insert
	})
	if err == nil {
		t.Fatal("expected Hire to fail on invalid goal")
	}
	assertNoEmployeeRows(t, dbPath, "emp-rollback")

	// Idempotent re-hire with a valid goal proves the id is free.
	if _, err := m.Hire(ctx, HireRequest{
		ID:           "emp-rollback",
		Name:         "rollback bot v2",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
		Goals:        []GoalSpec{{Title: "ok", Mandate: "do", Source: "user"}},
	}); err != nil {
		t.Fatalf("re-Hire after rollback: %v", err)
	}
}

// TestRetireLeavesNoRows covers the DeleteEmployee path end-to-end: after a
// successful hire (with goals) and Retire, NOTHING keyed by the id remains
// in the bot store or the employee stores (fix 1 + explicit goal sweep).
func TestRetireLeavesNoRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, dbPath := newHirePersistenceManager(t)

	c := validConstitution()
	c.EscalatesTo = []string{"user"}
	if _, err := m.Hire(ctx, HireRequest{
		ID:           "emp-retire",
		Name:         "retire bot",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
		Goals:        []GoalSpec{{Title: "g1", Mandate: "m1", Source: "user"}},
	}); err != nil {
		t.Fatalf("Hire: %v", err)
	}
	if n := countRows(t, dbPath, `SELECT COUNT(*) FROM bot_definitions WHERE id = ?`, "emp-retire"); n != 1 {
		t.Fatalf("post-hire bot_definitions rows = %d, want 1", n)
	}

	if err := m.Retire(ctx, "emp-retire"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	assertNoEmployeeRows(t, dbPath, "emp-retire")

	// Re-hire of the retired id must succeed (covers fix 1 via the full
	// employee path: no orphan bot_states row blocks re-creation).
	if _, err := m.Hire(ctx, HireRequest{
		ID:           "emp-retire",
		Name:         "retired then rehired",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
	}); err != nil {
		t.Fatalf("re-Hire retired id: %v", err)
	}
}
