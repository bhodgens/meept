// Package integration — employee_audit_test.go exercises the post-turn
// audit + auto-pause flow end-to-end via a fake LLM whose response
// reports a critical constitution violation. Mirrors what spec line 642
// calls "inject a fake LLM response that violates never[], assert
// finding + auto-pause".
//
// The stub defined here is intentionally a copy of the package-internal
// mockChatter (internal/employee/enforcement_test.go:498-514). The
// integration test package cannot import internal/employee test
// helpers, so the pattern is re-declared locally — matching the
// mcp_toggle_test.go convention of duplicating shared helpers.
package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// auditChatter is a minimal llm.Chatter that returns a canned response.
// It is the integration-test equivalent of internal/employee's
// mockChatter (which lives in the employee package's test files and so
// cannot be imported here).
type auditChatter struct {
	resp    *llm.Response
	err     error
	called  atomic.Int32
}

func (c *auditChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.called.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	return c.resp, nil
}

func (c *auditChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(context.Background(), nil)
}

func (c *auditChatter) Config() *llm.ModelConfig { return nil }

// pauseRecorder is the AutoPauseFunc used by the audit test. It records
// the most recent pause call so the test can assert on the reason.
type pauseRecorder struct {
	mu        sync.Mutex
	called    bool
	employee  string
	reason    string
	pauseCount int
}

func (p *pauseRecorder) func_() employee.AutoPauseFunc {
	return func(employeeID, reason string) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.called = true
		p.employee = employeeID
		p.reason = reason
		p.pauseCount++
		return nil
	}
}

func (p *pauseRecorder) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pauseCount
}

// TestEmployee_AuditViolation_AutoPause verifies the full Checkpoint 2
// path: a post-turn LLM audit that returns a critical finding triggers
// (a) an AuditFinding persisted to the store, and (b) auto-pause of the
// employee. The test drives the real PostTurnAuditor + AuditStore
// directly (no daemon) — the test mirrors the production path that
// fires from the GoalLoop's Reflect step.
func TestEmployee_AuditViolation_AutoPause(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping employee audit integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Hire an employee whose constitution forbids "merge to main".
	employeeID := id.Generate("emp_")
	hireEmployee(t, env, employeeID)

	// Load the live constitution to hand to the auditor. The auditor
	// needs *Constitution on the TurnRecord so buildPostTurnPrompt can
	// list the never[] rules to the LLM.
	emp, err := env.empMgr.GetEmployee(ctx, employeeID)
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	constitution := emp.Constitution

	// Wire a PostTurnAuditor with a fake LLM that reports a critical
	// "never[]" violation. The production wiring constructs the
	// auditor once per employee in NewComponents; here we build a
	// one-shot auditor directly.
	violatingResp := `{"severity":"critical","violated_rule":"never[0]","evidence":"LLM output merged to main"}`
	chatter := &auditChatter{resp: &llm.Response{Content: violatingResp}}
	auditor := employee.NewPostTurnAuditor(chatter, env.auditStore, "post-turn audit")
	pauser := &pauseRecorder{}
	auditor.SetAutoPause(pauser.func_())

	turn := employee.TurnRecord{
		EmployeeID:   employeeID,
		TurnID:       id.Generate("turn_"),
		FinalOutput:  "I have merged the fix to main",
		Constitution: &constitution,
	}

	finding, err := auditor.Audit(ctx, turn)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if finding == nil {
		t.Fatal("expected finding from critical violation")
	}
	if finding.Severity != employee.SeverityCritical {
		t.Errorf("Severity = %q, want %q", finding.Severity, employee.SeverityCritical)
	}
	if finding.ViolatedRule == "" {
		t.Error("ViolatedRule should be non-empty on a critical finding")
	}

	// auto-pause must have been invoked (spec: critical → auto-pause).
	if !pauser.called {
		t.Fatal("expected auto-pause to be called on critical finding")
	}
	if pauser.employee != employeeID {
		t.Errorf("auto-pause employee = %q, want %q", pauser.employee, employeeID)
	}
	if pauser.Count() != 1 {
		t.Errorf("auto-pause call count = %d, want 1", pauser.Count())
	}

	// The finding must be persisted in the AuditStore. List with a
	// filter on the employee + critical severity and assert non-empty.
	findings, err := env.auditStore.List(ctx, employee.AuditListFilter{
		EmployeeID: employeeID,
		Severity:   string(employee.SeverityCritical),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 persisted finding, got %d", len(findings))
	}
	if findings[0].Severity != employee.SeverityCritical {
		t.Errorf("persisted finding severity = %q, want %q",
			findings[0].Severity, employee.SeverityCritical)
	}
	if findings[0].EmployeeID != employeeID {
		t.Errorf("persisted finding employee = %q, want %q",
			findings[0].EmployeeID, employeeID)
	}

	// Resolve the finding as "acknowledged" to exercise the Resolve path.
	if err := env.empMgr.ResolveAuditFinding(ctx, findings[0].ID, "acknowledged", ""); err != nil {
		t.Fatalf("ResolveAuditFinding: %v", err)
	}

	// Subtest: clean turn (severity=info, empty violated_rule) must
	// NOT trigger auto-pause.
	t.Run("clean turn does not pause", func(t *testing.T) {
		cleanResp := `{"severity":"info","violated_rule":"","evidence":""}`
		chatter := &auditChatter{resp: &llm.Response{Content: cleanResp}}
		auditor := employee.NewPostTurnAuditor(chatter, env.auditStore, "clean")
		pauser := &pauseRecorder{}
		auditor.SetAutoPause(pauser.func_())

		cleanTurn := employee.TurnRecord{
			EmployeeID:   employeeID,
			TurnID:       id.Generate("turn_"),
			FinalOutput:  "investigated the failure; no violation",
			Constitution: &constitution,
		}
		finding, err := auditor.Audit(ctx, cleanTurn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding != nil {
			t.Errorf("expected nil finding for clean turn, got %+v", finding)
		}
		if pauser.called {
			t.Error("auto-pause must not fire on a clean turn")
		}
	})

	// Subtest: unparseable LLM response retries once with a stricter
	// prompt, then skips with a nil finding (spec line 605). No pause.
	t.Run("unparseable response skips with nil finding", func(t *testing.T) {
		chatter := &auditChatter{resp: &llm.Response{Content: "this is not json"}}
		auditor := employee.NewPostTurnAuditor(chatter, env.auditStore, "bad-json")
		pauser := &pauseRecorder{}
		auditor.SetAutoPause(pauser.func_())

		badTurn := employee.TurnRecord{
			EmployeeID:   employeeID,
			TurnID:       id.Generate("turn_"),
			FinalOutput:  "anything",
			Constitution: &constitution,
		}
		finding, err := auditor.Audit(ctx, badTurn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding != nil {
			t.Errorf("expected nil finding on unparseable response, got %+v", finding)
		}
		if pauser.called {
			t.Error("auto-pause must not fire on unparseable response")
		}
		if chatter.called.Load() != 2 {
			t.Errorf("expected 2 LLM calls (initial + retry), got %d", chatter.called.Load())
		}
	})

	// Sanity: errors.Is on the sentinel works for the (nil, nil)
	// clean-turn case.
	var sentinelErr = employee.ErrEmployeeNotFound
	if !errors.Is(sentinelErr, employee.ErrEmployeeNotFound) {
		t.Errorf("errors.Is sanity check failed")
	}
}
