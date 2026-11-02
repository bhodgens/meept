package builtin

// NOTE: gate test for the github-pr-ci tree (leaf 00T), F8. Registration
// coverage is driven through the production path: the provider bound via
// SetGitHubProvider and walked via toolprov.Names/RegisterAll, exactly what
// the daemon's wireGitHubTools does (githubsvc.RegisterGitHubTools was
// removed as dead code - see docs/plans/20261101-security-gate-followups/
// 03-remove-registergithubtools.md).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// registerTokenStub is a static TokenSource so the pool can be built for the
// enabled case.
type registerTokenStub struct{}

func (registerTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_testtoken", nil
}

// newRegisterPool builds a real *githubsvc.ClientPool for the registration
// tests. In the enabled case the pool is non-nil; in the disabled case the
// provider is never bound, mirroring the daemon's "github not configured"
// posture (no SetGitHubProvider call, no seam walk).
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

// bindGitHubProviderForTest binds the github provider's construction inputs
// via SetGitHubProvider (the same binder the daemon uses) and restores the
// previous binding when the test ends, so package-global state cannot leak
// between tests.
func bindGitHubProviderForTest(t *testing.T, opts githubsvc.RegistrationOptions) {
	t.Helper()
	saved := githubProviderFactoryFunc
	t.Cleanup(func() { githubProviderFactoryFunc = saved })
	SetGitHubProvider(opts, nil, nil, nil)
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

// TestGitHubToolRegistration verifies F8 through the production registration
// path. Enabled case: a populated RegistrationOptions with a real Pool,
// provider bound via SetGitHubProvider, discovered via toolprov - every C2
// tool lands in the registry. Disabled case: the daemon models "github not
// configured" by never binding the provider and never walking the seam; the
// seam-level equivalent is an unbound provider factory, whose thunk yields a
// nil Provider and whose RegisterAll fails closed registering nothing.
func TestGitHubToolRegistration(t *testing.T) {
	// ---- Enabled case: a real pool drives full registration. ----
	// No request is made in this test - only registry membership is
	// asserted - so the pool's BaseURL can be the default (empty).
	pool := newRegisterPool(t, "")
	bindGitHubProviderForTest(t, githubsvc.RegistrationOptions{
		Pool:         pool,
		DefaultOwner: "octo",
		DefaultRepo:  "cat",
		WorkingDir:   t.TempDir(),
	})

	// The "github" provider must be discoverable by name; this is what
	// doctor surfacing and the [tool_providers] config filter consume.
	found := false
	for _, name := range toolprov.Names() {
		if name == "github" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("toolprov.Names() missing %q; names=%v", "github", toolprov.Names())
	}

	enabledReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	n, err := toolprov.RegisterAll(enabledReg, []string{"github"})
	if err != nil {
		t.Fatalf("RegisterAll(github): %v", err)
	}
	if n != len(expectedGitHubToolNames) {
		t.Errorf("RegisterAll(github) registered %d tools, want %d", n, len(expectedGitHubToolNames))
	}
	for _, name := range expectedGitHubToolNames {
		if got := enabledReg.Get(name); got == nil {
			t.Errorf("enabled: registry missing tool %q; registered=%v", name, enabledReg.Names())
		}
	}

	// ---- Disabled case: unbound provider fails closed, registers nothing. ----
	saved := githubProviderFactoryFunc
	githubProviderFactoryFunc = nil
	t.Cleanup(func() { githubProviderFactoryFunc = saved })

	disabledReg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	if _, err := toolprov.RegisterAll(disabledReg, []string{"github"}); err == nil {
		t.Fatal("RegisterAll with an unbound github provider succeeded; the seam must fail closed instead of silently registering nothing")
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
// as the provider's ReArmSink, and its rows are what
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
