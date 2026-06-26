package employee

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// mockBusPublisher captures PublishEmployeePaused and PublishCriticalFinding
// and PublishConstitutionValidationError calls for testing.
type mockBusPublisher struct {
	mu            sync.Mutex
	events        []EmployeePausedEvent
	criticalEvts  []CriticalFindingEvent
	constErrors   []ConstitutionValidationErrorEvent
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
