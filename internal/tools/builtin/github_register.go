package builtin

import (
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// github_register.go populates TWO init-side seams from the builtin
// package:
//
//  1. githubsvc.ToolFactories - the original factory seam broken over
//     the builtin <-> githubsvc import cycle (populated first; the
//     githubsvc.RegisterGitHubTools entry point that used to walk it was
//     removed as dead code, but the factory remains the construction
//     seam the provider mirrors).
//  2. toolprov.Register("github", ...) - the compiled-in provider seam
//     (contract C1, docs/plans/20261031-tool-provider-seam/). The
//     provider's Discover() constructs the SAME eight github_* tools
//     with the same arguments as the ToolFactories closure, so both
//     paths serve identical tool sets. The construction inputs (pool,
//     defaults, ledger, sink) are runtime values only the daemon can
//     supply (github_wiring.go), so this registers a provider FACTORY
//     whose opts are captured at Discover time via a SetGitHubProvider
//     call from the daemon call site rather than at init time.
//
// A binary that links this package gets both seams; one that does not
// gets the descriptive errors both seams document - never a panic.
// builtin already imports githubsvc (pool / OwnerRepo / ReArmSink
// types), so importing toolprov here is cycle-safe: toolprov imports
// only tools + secrets + stdlib.
func init() {
	githubsvc.ToolFactories = func(opts githubsvc.RegistrationOptions, ledger effects.Ledger, sink githubsvc.ReArmSink) []tools.Tool {
		defaults := githubsvc.OwnerRepo{Owner: opts.DefaultOwner, Repo: opts.DefaultRepo}
		return []tools.Tool{
			NewGitHubPRCreateTool(opts.Pool, ledger, defaults),
			NewGitHubPRStatusTool(opts.Pool, defaults),
			NewGitHubCIWaitTool(opts.Pool, defaults, sink),
			NewGitHubPRCommentTool(opts.Pool, ledger, defaults),
			NewGitHubPRReviewTool(opts.Pool, ledger, defaults),
			NewGitHubPRMergeTool(opts.Pool, ledger, defaults),
			NewGitHubIssueGetTool(opts.Pool, defaults),
			NewGitHubIssueCommentTool(opts.Pool, ledger, defaults),
		}
	}

	// Register the provider factory; a duplicate "github" registration
	// can only happen if this init runs twice (impossible in one
	// binary), so an error here is a programming failure surfaced at
	// startup - never silently swallowed.
	if err := toolprov.Register("github", githubProviderFactory); err != nil {
		panic("builtin: github provider registration: " + err.Error())
	}
}

// githubProviderFactoryFunc builds the github provider from the options
// a Discover-time caller captures. Declared as a variable so the daemon
// (github_wiring.go) can bind runtime opts before toolprov.RegisterAll
// constructs the provider.
var githubProviderFactoryFunc func() toolprov.Provider

// githubProviderFactory is the thunk handed to toolprov.Register. It
// defers to githubProviderFactoryFunc, which the daemon populates via
// SetGitHubProvider with the runtime opts (pool, ledger, sink) before
// walking toolprov.RegisterAll. An unbound factory fails closed with a
// descriptive error instead of constructing a provider that cannot
// authenticate.
func githubProviderFactory() toolprov.Provider {
	if githubProviderFactoryFunc == nil {
		return nil
	}
	return githubProviderFactoryFunc()
}

// SetGitHubProvider binds the github provider's construction inputs.
// The provider it builds constructs the same eight github_* tools as
// the githubsvc.ToolFactories closure above (same constructors, same
// argument shapes), so both registration paths are guaranteed to serve
// identical tool sets. The broker binding is the in-process secrets
// broker (token resolution happens in-process via Value; children only
// ever receive placeholders).
func SetGitHubProvider(opts githubsvc.RegistrationOptions, ledger effects.Ledger, sink githubsvc.ReArmSink, broker *secrets.Broker) {
	githubProviderFactoryFunc = func() toolprov.Provider {
		return &githubProvider{
			opts:   opts,
			ledger: ledger,
			sink:   sink,
			broker: broker,
		}
	}
}

// githubProvider is the toolprov.Provider for the compiled-in github
// tool group.
type githubProvider struct {
	opts   githubsvc.RegistrationOptions
	ledger effects.Ledger
	sink   githubsvc.ReArmSink
	broker *secrets.Broker
}

// Name implements toolprov.Provider.
func (p *githubProvider) Name() string { return "github" }

// Discover implements toolprov.Provider. It mirrors the
// githubsvc.ToolFactories closure exactly: same tools, same order, same
// constructor arguments.
func (p *githubProvider) Discover() []tools.Tool {
	defaults := githubsvc.OwnerRepo{Owner: p.opts.DefaultOwner, Repo: p.opts.DefaultRepo}
	return []tools.Tool{
		NewGitHubPRCreateTool(p.opts.Pool, p.ledger, defaults),
		NewGitHubPRStatusTool(p.opts.Pool, defaults),
		NewGitHubCIWaitTool(p.opts.Pool, defaults, p.sink),
		NewGitHubPRCommentTool(p.opts.Pool, p.ledger, defaults),
		NewGitHubPRReviewTool(p.opts.Pool, p.ledger, defaults),
		NewGitHubPRMergeTool(p.opts.Pool, p.ledger, defaults),
		NewGitHubIssueGetTool(p.opts.Pool, defaults),
		NewGitHubIssueCommentTool(p.opts.Pool, p.ledger, defaults),
	}
}

// PermissionClass implements toolprov.Provider. The github group spans
// read-only lookups AND mutating writes (PR merge, comments, issue
// comments), so its ceiling is mutating - it never spawns processes,
// so it is not privileged.
func (p *githubProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider.
func (p *githubProvider) Broker() *secrets.Broker {
	return p.broker
}
