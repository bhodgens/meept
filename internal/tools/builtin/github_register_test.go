package builtin

// NOTE: gate test for the github-pr-ci tree (leaf 00T), F8. This file is
// intentionally RED: RegisterGitHubTools, RegistrationOptions and the pool
// referenced below do not exist yet — they land in leaf 01 (RegistrationOptions
// type stub) and leaf 05 (RegisterGitHubTools). Written to compile once
// those land; a compile failure is the only expected failure in RED state.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/tools"
)

// registerTokenStub is a static TokenSource so the pool can be built for the
// enabled case.
type registerTokenStub struct{}

func (registerTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_testtoken", nil
}

// newRegisterPool builds a real *githubsvc.ClientPool for the registration
// tests. In the enabled case the pool is non-nil; in the disabled case we
// pass a zero-value RegistrationOptions (nil Pool) to model "not configured".
func newRegisterPool(t *testing.T, baseURL string) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  registerTokenStub{},
		BaseURL: baseURL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// expectedGitHubToolNames is the frozen C2 tool-name set. The master plan and
// leaf 00T both assert the full C2 set; note the C2 contract defines 8 names
// (the parent "seven tools" phrasing undercounts pr_status/ci_wait split).
var expectedGitHubToolNames = []string{
	"github_pr_create",
	"github_pr_status",
	"github_ci_wait",
	"github_pr_comment",
	"github_pr_review",
	"github_pr_merge",
	"github_issue_get",
	"github_issue_comment",
}

// TestGitHubToolRegistration verifies F8: when the github block is enabled
// (a populated RegistrationOptions with a real Pool), RegisterGitHubTools
// registers every C2 tool; when disabled (zero-value RegistrationOptions,
// nil Pool) it registers none.
//
// The [github].enabled flag lives in the config block (C5) that the daemon
// consults before calling RegisterGitHubTools; the gate test proxies that
// flag through RegistrationOptions: a non-nil Pool models "enabled", a
// zero-value (nil Pool) models "disabled / not configured". This keeps the
// test contract-shaped and compilable against the pinned signature:
//
//	githubsvc.RegisterGitHubTools(r *tools.Registry, opts githubsvc.RegistrationOptions) error
func TestGitHubToolRegistration(t *testing.T) {
	// ---- Enabled case: a real pool drives full registration. ----
	// No request is made in this test — only registry membership is
	// asserted — so the pool's BaseURL can be the default (empty).
	pool := newRegisterPool(t, "")
	enabledOpts := githubsvc.RegistrationOptions{
		Pool:         pool,
		DefaultOwner: "octo",
		DefaultRepo:  "cat",
		WorkingDir:   t.TempDir(),
	}

	enabledReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	if err := githubsvc.RegisterGitHubTools(enabledReg, enabledOpts); err != nil {
		t.Fatalf("RegisterGitHubTools(enabled): %v", err)
	}
	for _, name := range expectedGitHubToolNames {
		if got := enabledReg.Get(name); got == nil {
			t.Errorf("enabled: registry missing tool %q; registered=%v", name, enabledReg.Names())
		}
	}

	// ---- Disabled case: zero-value options must register nothing. ----
	disabledReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	if err := githubsvc.RegisterGitHubTools(disabledReg, githubsvc.RegistrationOptions{}); err != nil {
		t.Fatalf("RegisterGitHubTools(disabled): %v", err)
	}
	for _, name := range expectedGitHubToolNames {
		if got := disabledReg.Get(name); got != nil {
			t.Errorf("disabled: registry unexpectedly contains tool %q; registered=%v", name, disabledReg.Names())
		}
	}
}

// TestInstructionReArmSink verifies the production ReArmSink persists a
// parked CI wait as a user-instruction cron row in the preferences store:
// ScheduleReArm writes a row with Trigger "cron:<M H * * *>", Action
// "agent_trigger", and an ActionArgs prompt that names the waitID; the
// row is discoverable and retrievable by the instruction scheduler.
//
// This is the leaf-05 additive companion to TestGitHubToolRegistration
// above (same file, same package): the sink is what the daemon wires in
// as RegistrationOptions.Sink, and its rows are what
// InstructionScheduler.SyncCronInstructions picks up.
func TestInstructionReArmSink(t *testing.T) {
	tierDir := t.TempDir()
	store := preferences.NewUserInstructionStore([]string{tierDir})
	sink := githubsvc.NewInstructionReArmSink(store, slog.New(slog.DiscardHandler))

	ref := githubsvc.CIRef{Owner: "octo", Repo: "cat", Number: 7}
	at := time.Now().Add(2 * time.Minute)
	waitID := "ciwait_testwait"
	if err := sink.ScheduleReArm(ref, at, waitID); err != nil {
		t.Fatalf("ScheduleReArm: %v", err)
	}

	// Find the persisted row by its (deterministic) name and verify the
	// exact fields the scheduler and a resuming agent rely on.
	found := false
	for _, instr := range store.GetActive() {
		if instr.Name != "github_ci_wait-"+strings.ToLower(waitID) {
			continue
		}
		found = true
		wantTrigger := fmt.Sprintf("cron:%d %d * * *", at.Minute(), at.Hour())
		if instr.Trigger != wantTrigger {
			t.Errorf("Trigger = %q, want %q", instr.Trigger, wantTrigger)
		}
		if !strings.HasPrefix(instr.Trigger, "cron:") {
			t.Errorf("Trigger %q does not carry the cron: prefix the scheduler scans for", instr.Trigger)
		}
		if instr.Action != "agent_trigger" {
			t.Errorf("Action = %q, want agent_trigger", instr.Action)
		}
		prompt, _ := instr.ActionArgs["prompt"].(string)
		if !strings.Contains(prompt, waitID) {
			t.Errorf("ActionArgs prompt %q does not name the waitID %q", prompt, waitID)
		}
		if !strings.Contains(prompt, "resume_wait_id") {
			t.Errorf("ActionArgs prompt %q does not instruct the resuming agent to pass resume_wait_id", prompt)
		}
		if got, _ := instr.ActionArgs["wait_id"].(string); got != waitID {
			t.Errorf("ActionArgs wait_id = %q, want %q", got, waitID)
		}
		if !instr.Enabled {
			t.Errorf("row is not enabled; the scheduler only syncs active rows")
		}
		// Retrieve it back by ID: the resuming agent path uses Get.
		if got := store.Get(instr.ID); got == nil || got.ID != instr.ID {
			t.Errorf("store.Get(%q) did not return the persisted row", instr.ID)
		}
	}
	if !found {
		t.Fatalf("no persisted instruction named github_ci_wait-%s; active=%v", waitID, store.GetActive())
	}

	// Discovery (what SyncCronInstructions consumes) must surface the row
	// without error.
	if _, err := store.Discovery(); err != nil {
		t.Fatalf("Discovery: %v", err)
	}
	if got := store.GetActive(); len(got) != 1 {
		t.Errorf("expected exactly 1 active instruction after one ScheduleReArm, got %d", len(got))
	}
}
