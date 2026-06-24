// Package integration — ci_monitor_poc_test.go is the proof-of-concept
// smoke test described in spec lines 646-648: "simulates a GitHub
// webhook payload, runs the full tier-2 cycle on a fixture repo,
// asserts goal health transitions green→yellow→green."
//
// The test does NOT call out to GitHub. It synthesizes a webhook
// push-payload, runs GoalLoop.Decide (which dispatches to decideTier2 →
// Assess → Plan), and then drives ApproveAndExecute twice: once for a
// failing execution (yellow / at_risk) and once for a successful
// execution (green / healthy). This is the full tier-2 cycle minus the
// RPC/HTTP envelope.
//
// This file re-declares the BotExecutor + chatter stubs locally (same
// convention as mcp_toggle_test.go and the other employee_*_test.go
// files in this package).
package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// ciChatter is the chatter stub for the CI monitor POC. It is
// queue-driven so each subtest can program the ASSESS + REFLECT
// responses independently. Identical in shape to tier2Chatter but
// re-declared here to keep the POC test self-contained (per the
// convention used by mcp_toggle_test.go for stubServerScript).
type ciChatter struct {
	mu        sync.Mutex
	responses []*llm.Response
	calls     atomic.Int32
}

func (c *ciChatter) queue(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, &llm.Response{Content: content})
}

func (c *ciChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responses) == 0 {
		return &llm.Response{Content: `{"health":"healthy","reasoning":"default"}`}, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

func (c *ciChatter) ChatWithProgress(_ context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(context.Background(), msgs, opts...)
}

func (c *ciChatter) Config() *llm.ModelConfig { return nil }

// ciExecutor is a BotExecutor stub whose output / error can be swapped
// between calls. The POC test flips it from "fail" to "success" to
// drive the health transition.
type ciExecutor struct {
	mu       sync.Mutex
	output   string
	tokens   int
	calls    atomic.Int32
	failNext bool
}

func (e *ciExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	e.calls.Add(1)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failNext {
		e.failNext = false
		return "", 0, errors.New("revert failed: conflict on main")
	}
	return e.output, e.tokens, nil
}

// githubPushPayload is a synthetic GitHub push event payload. It
// matches the minimum shape Manager.Trigger / webhook handlers expect:
// repository name + head_commit + ref. The exact field set mirrors the
// real GitHub webhook payload spec (just the fields the GoalLoop's
// ASSESS prompt inspects).
const githubPushPayload = `{
  "ref": "refs/heads/main",
  "repository": {"name": "meept", "full_name": "caimlas/meept"},
  "head_commit": {
    "id": "abc123def456",
    "message": "feat: introduce regression",
    "timestamp": "2026-06-23T10:00:00Z"
  },
  "sender": {"login": "ci-bot"}
}`

// TestCIMonitor_POC is the spec-mandated proof-of-concept smoke test
// (spec lines 646-648). It simulates a GitHub webhook push event,
// runs the tier-2 GoalLoop cycle end-to-end, and asserts goal health
// transitions: green (initial) → yellow (at_risk after a failed
// execution) → green (healthy after a successful execution).
//
// The test is structured as three sequential subtests so the goal
// health transitions are visible by name in test output.
func TestCIMonitor_POC(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CI monitor POC integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// --- fixture setup: hire the CI monitor employee + a goal ---
	employeeID := id.Generate("ci_monitor_")
	constitutionMap := validConstitutionMap()
	constitutionMap["autonomy_tier"] = "tier_2_propose"
	constitutionMap["purpose"] = "monitor CI for the main branch"
	constitutionMap["role"] = "CI Reliability Engineer"
	constitutionMap["charter"] = "investigate failures on main; never merge code directly"
	constitutionMap["constraints"] = map[string]any{
		"risk_ceiling":        "medium",
		"assessment_interval": "15m",
		"never":               []string{"merge to main", "force push"},
	}

	if _, err := env.empMgr.Hire(ctx, employee.HireRequest{
		ID:           employeeID,
		Name:         "ci-monitor",
		Description:  "CI reliability engineer",
		Prompt:       "investigate failing main-branch builds",
		Model:        "stub-model",
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Tools:        []string{"file_read", "web_fetch"},
		Enabled:      true,
		Constitution: constitutionMap,
	}); err != nil {
		t.Fatalf("Hire ci-monitor: %v", err)
	}

	emp, err := env.empMgr.GetEmployee(ctx, employeeID)
	if err != nil {
		t.Fatalf("GetEmployee: %v", err)
	}
	constitution := emp.Constitution

	goal := &employee.Goal{
		ID:         employee.NewGoalID(),
		EmployeeID: employeeID,
		Title:      "keep main builds green",
		Mandate:    "every push to main should leave CI in a passing state",
		State:      employee.GoalActive,
		Source:     employee.SourceUser,
		Health:     employee.GoalHealthy, // initial: green
	}
	if err := env.goalStore.Create(ctx, goal); err != nil {
		t.Fatalf("goalStore.Create: %v", err)
	}

	// --- GoalLoop wiring with stubs ---
	chatter := &ciChatter{}
	executor := &ciExecutor{
		output: "investigation complete",
		tokens: 200,
	}
	planner := newTier2Planner()

	loop := employee.NewGoalLoop(employeeID, &constitution, env.goalStore, nil).
		WithExecutor(executor).
		WithPlanner(planner).
		WithReflector(chatter).
		WithMaxConsecutiveFailures(2) // lower threshold so failures surface quickly

	// --- Step 1: webhook fires (GitHub push). Decide runs Assess + Plan ---
	t.Run("webhook triggers tier-2 assess and produces candidate plan", func(t *testing.T) {
		// ASSESS response: candidate that proposes a revert.
		chatter.queue(`{"candidates":[{"title":"investigate failing build","description":"identify the commit that broke main","prompt":"run git log + bisect"}]}`)

		// Drive Decide via a simulated webhook TriggerEvent.
		trigger := employee.TriggerEvent{
			Source:  "webhook",
			Topic:   "github.push",
			Payload: []byte(githubPushPayload),
			FiredAt: time.Now().UTC(),
		}
		if err := loop.Decide(ctx, trigger); err != nil {
			t.Fatalf("Decide: %v", err)
		}

		// tier-2 Decide should have produced exactly one plan via the
		// stubbed planner. The plan sits in pending_approval; the
		// executor should NOT have been called yet.
		if got := atomic.LoadInt32(&planner.created); got != 1 {
			t.Errorf("expected 1 plan created, got %d", got)
		}
		if executor.calls.Load() != 0 {
			t.Errorf("executor called %d times during Decide; want 0 (tier-2 requires approval)",
				executor.calls.Load())
		}
	})

	// --- Step 2: first execution fails → health should transition to
	// at_risk (yellow). The plan ID comes from the planner stub. ---
	t.Run("failed execution transitions goal to at_risk", func(t *testing.T) {
		// The planner created plan-t2-001 in the previous subtest.
		// Drive ApproveAndExecute with that ID. Mark the executor to
		// fail on the next call.
		executor.mu.Lock()
		executor.failNext = true
		executor.mu.Unlock()

		// Note: no LLM response is queued here because the failure path
		// in GoalLoop.Reflect does not call the LLM — it uses the
		// consecutive-failure counter to derive health (at_risk when
		// below threshold, broken when at/above).


		planRef := employee.PlanRef{
			ID:    "plan-t2-001",
			State: "approved",
		}
		result, health, err := loop.ApproveAndExecute(ctx, planRef)
		if err != nil {
			t.Fatalf("ApproveAndExecute (fail): %v", err)
		}
		if result.Success {
			t.Error("expected execution to fail; got success")
		}
		// After a single failure (below the threshold of 2), Reflect
		// should report at_risk. The failure counter is now 1.
		if health != employee.GoalAtRisk {
			t.Errorf("health after first failure = %q, want %q",
				health.String(), employee.GoalAtRisk.String())
		}

		// The persisted goal should reflect at_risk.
		updated, err := env.goalStore.Get(ctx, goal.ID)
		if err != nil {
			t.Fatalf("goalStore.Get: %v", err)
		}
		if updated.Health != employee.GoalAtRisk {
			t.Errorf("persisted health = %q, want %q",
				updated.Health.String(), employee.GoalAtRisk.String())
		}
		// Plan ID appended to history.
		history := updated.History()
		if len(history) == 0 || history[len(history)-1] != planRef.ID {
			t.Errorf("plan %q not in goal history %v", planRef.ID, history)
		}
	})

	// --- Step 3: second execution succeeds → health should return to
	// healthy (green). ---
	t.Run("successful execution transitions goal back to healthy", func(t *testing.T) {
		// Second plan from the planner (plan-t2-002).
		planRef := employee.PlanRef{
			ID:    "plan-t2-002",
			State: "approved",
		}
		// Queue a REFLECT response for the post-execute LLM call.
		chatter.queue(`{"health":"healthy","reasoning":"revert landed; main is green"}`)

		result, health, err := loop.ApproveAndExecute(ctx, planRef)
		if err != nil {
			t.Fatalf("ApproveAndExecute (success): %v", err)
		}
		if !result.Success {
			t.Errorf("expected execution to succeed; got error %q", result.Error)
		}
		if health != employee.GoalHealthy {
			t.Errorf("health after successful execution = %q, want %q",
				health.String(), employee.GoalHealthy.String())
		}

		// Final persisted goal state: healthy.
		final, err := env.goalStore.Get(ctx, goal.ID)
		if err != nil {
			t.Fatalf("goalStore.Get final: %v", err)
		}
		if final.Health != employee.GoalHealthy {
			t.Errorf("final persisted health = %q, want %q",
				final.Health.String(), employee.GoalHealthy.String())
		}
		// History should now contain both plans.
		history := final.History()
		if len(history) < 2 {
			t.Errorf("expected at least 2 plans in history; got %d (%v)", len(history), history)
		}
		// Consecutive failure counter should have reset on success.
		if loop.ConsecutiveFailures() != 0 {
			t.Errorf("ConsecutiveFailures after success = %d, want 0",
				loop.ConsecutiveFailures())
		}
	})
}
