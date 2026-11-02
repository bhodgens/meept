package githubsvc

import (
	"fmt"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/tools"
)

// ToolFactories is the init-populated factory seam that breaks the
// builtin <-> githubsvc import cycle. builtin imports githubsvc for these
// types (pool, OwnerRepo, ReArmSink), so githubsvc cannot itself construct
// the builtin tools. Instead this package-level variable is populated by
// builtin's init() (internal/tools/builtin/github_register.go) with a
// factory that builds all eight github_* tools from these options.
//
// Tool construction is deliberately NOT defined in a package that imports
// builtin: any binary that links builtin gets a non-nil ToolFactories (and,
// via builtin's init, the toolprov provider seam the daemon walks with
// RegisterAll); a binary that does not link builtin gets a descriptive
// error in the enabled case only (the disabled case never touches the
// factory and registers nothing).
// Never panic.
var ToolFactories func(opts RegistrationOptions, ledger effects.Ledger, sink ReArmSink) []tools.Tool

// RegistrationOptions is the configuration surface for the GitHub PR/CI
// tool set. It is the seam the builtin tool package consumes: the provider
// factory closure (githubsvc.ToolFactories) and SetGitHubProvider both take
// it, and the daemon's github_wiring.go builds it from the [github] config
// block.
type RegistrationOptions struct {
	// Pool is the per-token GitHub client pool the registered tools call.
	// A nil Pool models "github not enabled / not configured": registration
	// is a no-op, never an error.
	Pool *ClientPool
	// DefaultOwner is the owner fallback when a tool invocation omits one.
	DefaultOwner string
	// DefaultRepo is the repo fallback when a tool invocation omits one.
	DefaultRepo string
	// WorkingDir is the session working directory the tools operate in.
	WorkingDir string
	// Ledger backs the effects-wrapped tools (pr create/comment/review/
	// merge, issue comment). Nil is legal: the tools still construct and
	// register (membership), and their Execute paths fail closed with a
	// clear error naming the missing effects ledger.
	Ledger effects.Ledger
	// Sink is the schedule seam a parked github_ci_wait hands back: on
	// timeout the wait is parked and re-armed through this sink. Nil is
	// legal: parking still returns the parked result (with its waitID);
	// the ScheduleReArm call is skipped.
	Sink ReArmSink
}

// OwnerRepo holds a default owner/repo target and resolves per-call
// arguments against it.
type OwnerRepo struct {
	Owner string
	Repo  string
}

// Resolve returns the owner and repo to target. An explicit argument takes
// precedence; an empty argument falls back to the default held on this
// OwnerRepo. A position whose argument and default are both empty is an
// error, so the caller never targets a blank owner or repo.
func (o OwnerRepo) Resolve(ownerArg, repoArg string) (string, string, error) {
	owner := ownerArg
	if owner == "" {
		owner = o.Owner
	}
	repo := repoArg
	if repo == "" {
		repo = o.Repo
	}
	if owner == "" {
		return "", "", fmt.Errorf("githubsvc: no owner to resolve (argument and default both empty)")
	}
	if repo == "" {
		return "", "", fmt.Errorf("githubsvc: no repo to resolve (argument and default both empty)")
	}
	return owner, repo, nil
}

// RegisterGitHubTools was the registration entry point; it was removed as
// dead code (zero production callers since the provider seam landed) - see
// docs/plans/20261101-security-gate-followups/03-remove-registergithubtools.md.
// The daemon now registers the github_* tools through the provider:
// builtin.SetGitHubProvider binds the construction inputs, then
// toolprov.RegisterAll walks the "github" provider into the registry.
