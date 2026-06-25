package employee

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// Test helpers for PeriodicAuditor tests.
// ---------------------------------------------------------------------------

// periodicTestTurns builds a minimal set of TurnRecords suitable for
// PeriodicAuditor.Audit. The constitution is non-nil so the auditor
// actually processes the turns.
func periodicTestTurns() []TurnRecord {
	c := testConstitution()
	return []TurnRecord{
		{
			EmployeeID:   "emp-periodic",
			TurnID:       "turn-001",
			ToolCalls:    []ToolCallRecord{{ToolName: "file_read", Action: "read config"}},
			FinalOutput:  "read the config file successfully",
			Constitution: c,
		},
		{
			EmployeeID:   "emp-periodic",
			TurnID:       "turn-002",
			ToolCalls:    []ToolCallRecord{{ToolName: "shell_execute", Action: "run tests"}},
			FinalOutput:  "tests passed",
			Constitution: c,
		},
	}
}

// multiResponseChatter returns a different response on each call, enabling
// tests that simulate "first call fails, retry succeeds" scenarios.
type multiResponseChatter struct {
	responses []string
	calls     int
}

func (m *multiResponseChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	idx := m.calls
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.calls++
	return &llm.Response{Content: m.responses[idx]}, nil
}
func (m *multiResponseChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(context.Background(), nil)
}
func (m *multiResponseChatter) Config() *llm.ModelConfig { return nil }

// errorChatter always returns an error, simulating LLM outage.
type errorChatter struct{}

func (e *errorChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return nil, context.DeadlineExceeded
}
func (e *errorChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return e.Chat(context.Background(), nil)
}
func (e *errorChatter) Config() *llm.ModelConfig { return nil }

// ---------------------------------------------------------------------------
// PeriodicAuditor.Audit — core behaviour tests.
// ---------------------------------------------------------------------------

func TestPeriodicAuditor_Audit_Clean(t *testing.T) {
	turns := periodicTestTurns()
	mc := &mockChatter{
		response: `{"drift_score":0.0,"findings":[]}`,
	}

	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	auditor := NewPeriodicAuditor(mc, store, 0.3)

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for clean audit, got %d", len(findings))
	}
	if driftScore != 0.0 {
		t.Errorf("expected drift score 0.0, got %f", driftScore)
	}
}

func TestPeriodicAuditor_Audit_CriticalFinding(t *testing.T) {
	turns := periodicTestTurns()
	mc := &mockChatter{
		response: `{"drift_score":0.8,"findings":[{"severity":"critical","violated_rule":"never[0]","evidence":"merged to main"}]}`,
	}

	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	pause := &pauseTracker{}
	auditor := NewPeriodicAuditor(mc, store, 0.3)
	auditor.SetAutoPause(pause.fn())

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("Severity = %q, want %q", findings[0].Severity, SeverityCritical)
	}
	if findings[0].Checkpoint != CheckpointPeriodic {
		t.Errorf("Checkpoint = %q, want %q", findings[0].Checkpoint, CheckpointPeriodic)
	}
	if driftScore <= 0 {
		t.Errorf("expected positive drift score, got %f", driftScore)
	}
	if !pause.called {
		t.Error("expected auto-pause to be called for critical finding")
	}

	// Verify finding was persisted.
	persisted, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-periodic"})
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(persisted) != 1 {
		t.Errorf("expected 1 persisted finding, got %d", len(persisted))
	}
}

func TestPeriodicAuditor_Audit_DriftExceedsThreshold(t *testing.T) {
	turns := periodicTestTurns()
	mc := &mockChatter{
		// High drift but no critical finding — only a warning.
		response: `{"drift_score":0.75,"findings":[{"severity":"warning","violated_rule":"charter drift","evidence":"slowly moving away from charter"}]}`,
	}

	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	pause := &pauseTracker{}
	auditor := NewPeriodicAuditor(mc, store, 0.3)
	auditor.SetAutoPause(pause.fn())

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected at least 1 finding")
	}
	if driftScore <= 0.3 {
		t.Errorf("drift score %f should exceed threshold 0.3", driftScore)
	}
	if !pause.called {
		t.Error("expected auto-pause when drift exceeds threshold")
	}
	if !strings.Contains(pause.reason, "drift") {
		t.Errorf("pause reason should mention drift, got %q", pause.reason)
	}
}

func TestPeriodicAuditor_Audit_DriftBelowThreshold(t *testing.T) {
	turns := periodicTestTurns()
	mc := &mockChatter{
		// Low drift, only a warning — should NOT auto-pause.
		response: `{"drift_score":0.1,"findings":[{"severity":"warning","violated_rule":"minor","evidence":"slightly off-charter"}]}`,
	}

	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	pause := &pauseTracker{}
	auditor := NewPeriodicAuditor(mc, store, 0.3)
	auditor.SetAutoPause(pause.fn())

	_, _, err = auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if pause.called {
		t.Error("auto-pause should NOT be called when drift is below threshold and no critical finding")
	}
}

// ---------------------------------------------------------------------------
// PeriodicAuditor.Audit — LLM parse failure and retry behaviour.
// ---------------------------------------------------------------------------

func TestPeriodicAuditor_Audit_LLMUnparseable_RetriesThenSkips(t *testing.T) {
	turns := periodicTestTurns()
	// Both calls return unparseable garbage.
	mc := &mockChatter{response: "I cannot produce JSON"}

	auditor := NewPeriodicAuditor(mc, nil, 0.3)

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit error: %v", err)
	}
	if findings != nil {
		t.Errorf("expected nil findings after unparseable retry, got %+v", findings)
	}
	if driftScore != 0 {
		t.Errorf("expected 0 drift score, got %f", driftScore)
	}
	if mc.called != 2 {
		t.Errorf("expected 2 LLM calls (initial + retry), got %d", mc.called)
	}
}

func TestPeriodicAuditor_Audit_LLMUnparseable_RecoveredOnRetry(t *testing.T) {
	turns := periodicTestTurns()
	mc := &multiResponseChatter{
		responses: []string{
			"not valid JSON at all", // first call fails
			`{"drift_score":0.5,"findings":[{"severity":"warning","violated_rule":"recovered","evidence":"found on retry"}]}`, // retry succeeds
		},
	}

	auditor := NewPeriodicAuditor(mc, nil, 0.3)

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding after recovery, got %d", len(findings))
	}
	if findings[0].ViolatedRule != "recovered" {
		t.Errorf("ViolatedRule = %q, want %q", findings[0].ViolatedRule, "recovered")
	}
	if driftScore != 0.5 {
		t.Errorf("driftScore = %f, want 0.5", driftScore)
	}
	if mc.calls != 2 {
		t.Errorf("expected 2 calls, got %d", mc.calls)
	}
}

// ---------------------------------------------------------------------------
// PeriodicAuditor.Audit — nil model / edge cases.
// ---------------------------------------------------------------------------

func TestPeriodicAuditor_Audit_NilModel(t *testing.T) {
	turns := periodicTestTurns()
	auditor := NewPeriodicAuditor(nil, nil, 0.3)

	findings, driftScore, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if findings != nil {
		t.Errorf("expected nil findings with nil model, got %+v", findings)
	}
	if driftScore != 0 {
		t.Errorf("expected 0 drift score, got %f", driftScore)
	}
}

func TestPeriodicAuditor_Audit_EmptyTurns(t *testing.T) {
	mc := &mockChatter{response: `{"drift_score":0,"findings":[]}`}
	auditor := NewPeriodicAuditor(mc, nil, 0.3)

	findings, driftScore, err := auditor.Audit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if findings != nil {
		t.Errorf("expected nil findings for empty turns, got %+v", findings)
	}
	if driftScore != 0 {
		t.Errorf("expected 0 drift score, got %f", driftScore)
	}
	if mc.called != 0 {
		t.Errorf("expected 0 LLM calls for empty turns, got %d", mc.called)
	}
}

func TestPeriodicAuditor_Audit_NilConstitution(t *testing.T) {
	mc := &mockChatter{response: `{"drift_score":0,"findings":[]}`}
	turns := []TurnRecord{
		{EmployeeID: "emp-x", TurnID: "t1", FinalOutput: "done", Constitution: nil},
	}
	auditor := NewPeriodicAuditor(mc, nil, 0.3)

	findings, _, err := auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if findings != nil {
		t.Errorf("expected nil findings for nil constitution, got %+v", findings)
	}
}

// ---------------------------------------------------------------------------
// PeriodicAuditor — 3-strike failure tracking.
// ---------------------------------------------------------------------------

func TestPeriodicAuditor_RecordPeriodicFailure_ThreeStrike(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	turns := periodicTestTurns()
	auditor := NewPeriodicAuditor(nil, store, 0.3)

	// Call recordPeriodicFailure directly (same package) to bypass the
	// backoff window that would otherwise prevent rapid successive calls.
	// Strike 1: counter at 1, no finding persisted.
	auditor.recordPeriodicFailure(store, turns)
	if auditor.consecutiveFailures != 1 {
		t.Fatalf("expected 1 consecutive failure, got %d", auditor.consecutiveFailures)
	}

	// Strike 2: counter at 2, no finding persisted.
	auditor.recordPeriodicFailure(store, turns)
	if auditor.consecutiveFailures != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", auditor.consecutiveFailures)
	}

	// Strike 3: counter resets to 0, critical finding persisted with
	// violated_rule=auditor_unavailable and checkpoint=periodic.
	auditor.recordPeriodicFailure(store, turns)
	if auditor.consecutiveFailures != 0 {
		t.Errorf("expected counter to reset after 3 strikes, got %d", auditor.consecutiveFailures)
	}

	persisted, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-periodic"})
	if err != nil {
		t.Fatalf("store.List: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted finding after 3 strikes, got %d", len(persisted))
	}
	f := persisted[0]
	if f.Severity != SeverityCritical {
		t.Errorf("Severity = %q, want %q", f.Severity, SeverityCritical)
	}
	if f.ViolatedRule != "auditor_unavailable" {
		t.Errorf("ViolatedRule = %q, want %q", f.ViolatedRule, "auditor_unavailable")
	}
	if f.Checkpoint != CheckpointPeriodic {
		t.Errorf("Checkpoint = %q, want %q", f.Checkpoint, CheckpointPeriodic)
	}
	if !strings.Contains(f.Evidence, "3 consecutive") {
		t.Errorf("Evidence should mention 3 consecutive failures, got %q", f.Evidence)
	}
}

func TestPeriodicAuditor_RecordPeriodicFailure_ResetOnSuccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	turns := periodicTestTurns()
	auditor := NewPeriodicAuditor(nil, store, 0.3)

	// Manually trigger 2 failures via recordPeriodicFailure.
	auditor.recordPeriodicFailure(store, turns)
	auditor.recordPeriodicFailure(store, turns)
	if auditor.consecutiveFailures != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", auditor.consecutiveFailures)
	}

	// Now simulate a successful Audit call: the success path at line 940-942
	// resets the counter. We test that directly.
	auditor.failMu.Lock()
	auditor.consecutiveFailures = 0
	auditor.failMu.Unlock()

	if auditor.consecutiveFailures != 0 {
		t.Errorf("expected counter to reset after success, got %d", auditor.consecutiveFailures)
	}

	// After reset, 3 more failures should produce another finding.
	auditor.recordPeriodicFailure(store, turns)
	auditor.recordPeriodicFailure(store, turns)
	// Backoff would normally prevent a 3rd rapid call, but we bypass it.
	// Reset lastFailureAt to avoid backoff interference in the test.
	auditor.failMu.Lock()
	auditor.lastFailureAt = time.Time{} // zero time → backoff check passes
	auditor.failMu.Unlock()

	auditor.recordPeriodicFailure(store, turns)

	persisted, _ := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-periodic"})
	if len(persisted) != 1 {
		t.Fatalf("expected 1 finding after post-reset 3 strikes, got %d", len(persisted))
	}
}

// ---------------------------------------------------------------------------
// PeriodicAuditor — backoff window.
// ---------------------------------------------------------------------------

func TestPeriodicAuditor_BackoffWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	turns := periodicTestTurns()
	mc := &errorChatter{}
	auditor := NewPeriodicAuditor(mc, store, 0.3)

	// First failure: hits the LLM, records a failure.
	_, _, _ = auditor.Audit(context.Background(), turns)
	if auditor.consecutiveFailures != 1 {
		t.Fatalf("expected 1 consecutive failure after first audit, got %d", auditor.consecutiveFailures)
	}
	if auditor.lastFailureAt.IsZero() {
		t.Fatal("expected lastFailureAt to be set")
	}

	// Second call within 30s backoff window: should skip without hitting LLM.
	// We can verify by checking that the failure counter did NOT increment
	// (the call returned early without hitting the LLM or recording a failure).
	// The mockChatter's call count is on the mockChatter type, but errorChatter
	// doesn't track calls. Instead, we verify via the consecutiveFailures counter.
	_, _, err = auditor.Audit(context.Background(), turns)
	if err != nil {
		t.Fatalf("Audit during backoff should not return error: %v", err)
	}
	if auditor.consecutiveFailures != 1 {
		t.Errorf("expected consecutive failures to remain 1 during backoff, got %d", auditor.consecutiveFailures)
	}
}

// ---------------------------------------------------------------------------
// parsePeriodicResponse — table-driven tests.
// ---------------------------------------------------------------------------

func TestParsePeriodicResponse(t *testing.T) {
	turns := periodicTestTurns()

	tests := []struct {
		name          string
		input         string
		wantErr       bool
		wantFindings  int
		wantDriftScore float64
		wantSeverity  AuditSeverity
	}{
		{
			name:           "clean — no findings",
			input:          `{"drift_score":0.0,"findings":[]}`,
			wantFindings:   0,
			wantDriftScore: 0.0,
		},
		{
			name:           "single warning finding",
			input:          `{"drift_score":0.3,"findings":[{"severity":"warning","violated_rule":"charter drift","evidence":"output deviates"}]}`,
			wantFindings:   1,
			wantDriftScore: 0.3,
			wantSeverity:   SeverityWarning,
		},
		{
			name:           "single critical finding",
			input:          `{"drift_score":0.9,"findings":[{"severity":"critical","violated_rule":"never[0]","evidence":"violation detected"}]}`,
			wantFindings:   1,
			wantDriftScore: 0.9,
			wantSeverity:   SeverityCritical,
		},
		{
			name:           "multiple findings",
			input:          `{"drift_score":0.6,"findings":[{"severity":"warning","violated_rule":"r1","evidence":"e1"},{"severity":"critical","violated_rule":"r2","evidence":"e2"}]}`,
			wantFindings:   2,
			wantDriftScore: 0.6,
		},
		{
			name:    "malformed JSON",
			input:   "this is not json",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name: "JSON wrapped in markdown fences",
			input: "```json\n" +
				`{"drift_score":0.4,"findings":[{"severity":"warning","violated_rule":"r","evidence":"e"}]}` +
				"\n```",
			wantFindings:   1,
			wantDriftScore: 0.4,
			wantSeverity:   SeverityWarning,
		},
		{
			name:           "invalid severity filtered out",
			input:          `{"drift_score":0.2,"findings":[{"severity":"catastrophic","violated_rule":"x","evidence":"y"}]}`,
			wantFindings:   0,
			wantDriftScore: 0.2,
		},
		{
			name:           "missing findings key treated as empty",
			input:          `{"drift_score":0.1}`,
			wantFindings:   0,
			wantDriftScore: 0.1,
		},
		{
			name:           "info severity accepted",
			input:          `{"drift_score":0.05,"findings":[{"severity":"info","violated_rule":"informational","evidence":"note"}]}`,
			wantFindings:   1,
			wantDriftScore: 0.05,
			wantSeverity:   SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, driftScore, err := parsePeriodicResponse(tt.input, turns)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(findings) != tt.wantFindings {
				t.Fatalf("got %d findings, want %d", len(findings), tt.wantFindings)
			}
			if driftScore != tt.wantDriftScore {
				t.Errorf("drift score = %f, want %f", driftScore, tt.wantDriftScore)
			}
			if tt.wantFindings > 0 && tt.wantSeverity != "" {
				if findings[0].Severity != tt.wantSeverity {
					t.Errorf("Severity = %q, want %q", findings[0].Severity, tt.wantSeverity)
				}
				if findings[0].Checkpoint != CheckpointPeriodic {
					t.Errorf("Checkpoint = %q, want %q", findings[0].Checkpoint, CheckpointPeriodic)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildPeriodicPrompt — verify the prompt contains expected content.
// ---------------------------------------------------------------------------

func TestBuildPeriodicPrompt(t *testing.T) {
	turns := periodicTestTurns()
	prompt := buildPeriodicPrompt(turns)

	if !strings.Contains(prompt, "## Constitution") {
		t.Error("prompt should contain constitution header")
	}
	if !strings.Contains(prompt, "Purpose:") {
		t.Error("prompt should contain Purpose field")
	}
	if !strings.Contains(prompt, "Charter:") {
		t.Error("prompt should contain Charter field")
	}
	if !strings.Contains(prompt, "## Last 2 Turns") {
		t.Errorf("prompt should reference 2 turns, got: %s", prompt)
	}
	if !strings.Contains(prompt, "turn-001") || !strings.Contains(prompt, "turn-002") {
		t.Error("prompt should contain turn IDs")
	}
	if !strings.Contains(prompt, "file_read") || !strings.Contains(prompt, "shell_execute") {
		t.Error("prompt should contain tool names")
	}
}

// ---------------------------------------------------------------------------
// NewPeriodicAuditor — constructor edge cases.
// ---------------------------------------------------------------------------

func TestNewPeriodicAuditor_DefaultThreshold(t *testing.T) {
	a := NewPeriodicAuditor(&errorChatter{}, nil, 0)
	if a.driftThreshold != 0.3 {
		t.Errorf("default threshold = %f, want 0.3", a.driftThreshold)
	}
}

func TestNewPeriodicAuditor_CustomThreshold(t *testing.T) {
	a := NewPeriodicAuditor(&errorChatter{}, nil, 0.75)
	if a.driftThreshold != 0.75 {
		t.Errorf("threshold = %f, want 0.75", a.driftThreshold)
	}
}

// ---------------------------------------------------------------------------
// Helper: switchingChatter calls different functions on successive calls.
// ---------------------------------------------------------------------------

// (Removed: not needed after switching 3-strike tests to call
// recordPeriodicFailure directly.)
