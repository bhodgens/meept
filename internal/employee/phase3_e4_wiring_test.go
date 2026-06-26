package employee

import (
	"context"
	"testing"
)

// TestE4_PostTurnAuditor_PublishesCriticalFinding verifies that when the
// PostTurnAuditor detects a critical finding, it calls
// PublishCriticalFinding on the wired BusPublisher (in addition to the
// existing autoPause callback).
func TestE4_PostTurnAuditor_PublishesCriticalFinding(t *testing.T) {
	c := testConstitution()
	turn := TurnRecord{
		EmployeeID:   "emp-e4-001",
		TurnID:       "turn-e4-001",
		ToolCalls:    []ToolCallRecord{{ToolName: "git_push", Action: "push"}},
		FinalOutput:  "pushed to main",
		Constitution: c,
	}

	mc := &mockChatter{response: `{"severity":"critical","violated_rule":"never[0]","evidence":"merged to main"}`}
	pause := &pauseTracker{}
	busPub := &mockBusPublisher{}

	auditor := &PostTurnAuditor{
		model:             mc,
		prompt:            "audit",
		retryWithStricter: false,
	}
	auditor.SetAutoPause(pause.fn())
	auditor.SetBusPublisher(busPub)

	finding, err := auditor.Audit(context.Background(), turn)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if finding == nil || finding.Severity != SeverityCritical {
		t.Fatalf("expected critical finding, got %+v", finding)
	}

	// Both the autoPause callback AND the bus publisher should fire.
	if !pause.called {
		t.Error("expected autoPause callback to fire")
	}

	critEvts := busPub.getCriticalEvents()
	if len(critEvts) != 1 {
		t.Fatalf("expected 1 critical finding event, got %d", len(critEvts))
	}
	ev := critEvts[0]
	if ev.EmployeeID != "emp-e4-001" {
		t.Errorf("employeeID = %q, want emp-e4-001", ev.EmployeeID)
	}
	if ev.ViolatedRule == "" {
		t.Error("expected non-empty violatedRule")
	}
	if ev.Evidence == "" {
		t.Error("expected non-empty evidence")
	}
}

// TestE4_PostTurnAuditor_NoBusPublisher_StillAutoPauses verifies that when
// no BusPublisher is wired, the existing autoPause callback still fires.
// This confirms belt-and-suspenders: bus publication is additive, not a
// replacement.
func TestE4_PostTurnAuditor_NoBusPublisher_StillAutoPauses(t *testing.T) {
	c := testConstitution()
	turn := TurnRecord{
		EmployeeID:   "emp-e4-002",
		TurnID:       "turn-e4-002",
		ToolCalls:    []ToolCallRecord{{ToolName: "git_push", Action: "push"}},
		FinalOutput:  "pushed to main",
		Constitution: c,
	}

	mc := &mockChatter{response: `{"severity":"critical","violated_rule":"never[0]","evidence":"merged to main"}`}
	pause := &pauseTracker{}

	auditor := &PostTurnAuditor{
		model:             mc,
		prompt:            "audit",
		retryWithStricter: false,
	}
	auditor.SetAutoPause(pause.fn())
	// Deliberately NOT calling SetBusPublisher

	finding, err := auditor.Audit(context.Background(), turn)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if finding == nil || finding.Severity != SeverityCritical {
		t.Fatalf("expected critical finding, got %+v", finding)
	}
	if !pause.called {
		t.Error("expected autoPause to fire even without bus publisher")
	}
}

// TestE4_PostTurnAuditor_SetBusPublisher_NilGuard verifies the setter
// follows the CLAUDE.md nil-guard convention.
func TestE4_PostTurnAuditor_SetBusPublisher_NilGuard(t *testing.T) {
	auditor := &PostTurnAuditor{}
	auditor.SetBusPublisher(nil) // should not panic, should not set

	auditor.mu.Lock()
	busPub := auditor.busPublisher
	auditor.mu.Unlock()

	if busPub != nil {
		t.Error("expected nil busPublisher after SetBusPublisher(nil)")
	}
}

// TestE4_PostTurnAuditor_NonCriticalDoesNotPublish verifies that
// non-critical findings (warning, info) do NOT trigger
// PublishCriticalFinding.
func TestE4_PostTurnAuditor_NonCriticalDoesNotPublish(t *testing.T) {
	c := testConstitution()
	turn := TurnRecord{
		EmployeeID:   "emp-e4-003",
		TurnID:       "turn-e4-003",
		ToolCalls:    []ToolCallRecord{{ToolName: "file_read", Action: "read"}},
		FinalOutput:  "done",
		Constitution: c,
	}

	mc := &mockChatter{response: `{"severity":"warning","violated_rule":"charter deviation","evidence":"output tone off"}`}
	busPub := &mockBusPublisher{}

	auditor := &PostTurnAuditor{
		model:             mc,
		prompt:            "audit",
		retryWithStricter: false,
	}
	auditor.SetBusPublisher(busPub)

	finding, err := auditor.Audit(context.Background(), turn)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if finding == nil {
		t.Fatal("expected finding, got nil")
	}
	if finding.Severity != SeverityWarning {
		t.Fatalf("expected warning, got %s", finding.Severity)
	}
	critEvts := busPub.getCriticalEvents()
	if len(critEvts) != 0 {
		t.Errorf("PublishCriticalFinding should not be called for warning findings, got %d events", len(critEvts))
	}
}

// TestE4_Manager_HandleCriticalFinding_PausesEmployee verifies that
// Manager.HandleCriticalFinding calls PauseWithReason which pauses the
// employee and publishes the employee.paused bus event. This exercises
// the subscriber-side handler that the daemon wiring invokes.
func TestE4_Manager_HandleCriticalFinding_PausesEmployee(t *testing.T) {
	mgr, cleanup := newTestManagerWithBot(t, "emp-e4-handle")
	defer cleanup()

	busPub := &mockBusPublisher{}
	mgr.SetBusPublisher(busPub)

	ev := CriticalFindingEvent{
		EmployeeID:   "emp-e4-handle",
		FindingID:    "finding-001",
		ViolatedRule: "never[0]: no git push to main",
		Evidence:     "pushed to main at 3am",
	}

	mgr.HandleCriticalFinding(context.Background(), ev)

	// The employee should be paused (employee.paused event published).
	pausedEvts := busPub.getEvents()
	if len(pausedEvts) != 1 {
		t.Fatalf("expected 1 employee.paused event, got %d", len(pausedEvts))
	}
	pe := pausedEvts[0]
	if pe.EmployeeID != "emp-e4-handle" {
		t.Errorf("employee_id = %q, want emp-e4-handle", pe.EmployeeID)
	}
	if pe.Source != "auto_pause" {
		t.Errorf("source = %q, want auto_pause", pe.Source)
	}
	if pe.Reason == "" {
		t.Error("expected non-empty reason")
	}
}
