package builtin

import (
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/tools"
)

// github_register.go populates the githubsvc.ToolFactories seam from the
// builtin side. builtin imports githubsvc (for the pool, OwnerRepo, and
// ReArmSink types), so githubsvc can never construct these tools itself;
// instead its package-level ToolFactories variable is filled by this init
// and invoked by githubsvc.RegisterGitHubTools at registration time. A
// binary that links this package gets full registration; one that does
// not gets the descriptive error RegisterGitHubTools documents — never a
// panic.
//
// The ledger and sink are passed as explicit factory parameters (not read
// from opts) so the seam stays dependency-free: RegisterGitHubTools
// forwards opts.Ledger/opts.Sink here. A nil ledger is legal (the
// effects-wrapped tools construct fine and fail closed at Execute); a nil
// sink is legal (github_ci_wait parks without scheduling the re-arm).
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
}
