package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
	"github.com/google/go-github/v93/github"
)

// github_ci.go implements the two read-only CI observability tools:
//
//   - github_pr_status: a point-in-time PR + CI health snapshot
//   - github_ci_wait:   polls that snapshot until green, a terminal CI
//     failure, or the caller's timeout — at which point the wait is
//     PARKED (re-armed through the ReArmSink) instead of blocking
//     forever; the park carries a resume waitID.
//
// Both tools resolve owner/repo via githubsvc.OwnerRepo.Resolve and get
// their client from the pool BEFORE any GitHub call, so an unresolvable
// token (ErrNoToken) fails closed. Both are read-only (GET endpoints
// only) and therefore never touch the effects ledger.

// ciFailedConclusions is the fast-fail allow-list of terminal, non-green
// check/workflow conclusions: as soon as any check or workflow concludes
// one of these, github_ci_wait returns immediately instead of waiting out
// the full timeout.
var ciFailedConclusions = map[string]bool{
	"failure":         true,
	"cancelled":       true,
	"timed_out":       true,
	"action_required": true,
	"stop_failure":    true,
}

// ciWaitParkLeadTime is how far ahead of now the parked CI wait is
// re-armed by default: CI is rarely re-run in the seconds between a park
// and the re-check, so the re-arm gives the checks a head start.
const ciWaitParkLeadTime = 15 * time.Second

// ciWaitMinTimeout is the default github_ci_wait timeout in seconds when
// the argument is absent or non-positive.
const ciWaitMinTimeout = 600

// ciWaitMaxTimeout caps the github_ci_wait timeout in seconds; longer
// waits are parked and resumed instead of held.
const ciWaitMaxTimeout = 3600

// ciWaitPollIntervalStart is the first poll interval; it doubles each
// round, bounded by ciWaitPollIntervalMax.
const ciWaitPollIntervalStart = 2 * time.Second

// ciWaitPollIntervalMax caps the poll interval; the wait must never
// sleep more than this between CI observations.
const ciWaitPollIntervalMax = 15 * time.Second

// fetchPRStatus fetches the PR, its head-ref check runs, and the head-SHA
// workflow runs, and summarizes them into a githubsvc.PRStatus. This is
// the single observation primitive shared by github_pr_status and the
// github_ci_wait poll loop.
func fetchPRStatus(ctx context.Context, client *github.Client, owner, repo string, prNum int) (githubsvc.PRStatus, error) {
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNum)
	if err != nil {
		return githubsvc.PRStatus{}, fmt.Errorf("fetch pull request: %w", err)
	}
	headSHA := pr.GetHead().GetSHA()

	var checks []*github.CheckRun
	if headSHA != "" {
		res, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, headSHA, &github.ListCheckRunsOptions{})
		if err != nil {
			return githubsvc.PRStatus{}, fmt.Errorf("list check runs for ref %s: %w", headSHA, err)
		}
		checks = res.GetCheckRuns()
	}

	// One page, filtered to the PR's head SHA so unrelated default-branch
	// runs never influence this PR's green/fail verdict.
	var workflows []*github.WorkflowRun
	if headSHA != "" {
		res, _, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, &github.ListWorkflowRunsOptions{
			HeadSHA:     headSHA,
			ListOptions: github.ListOptions{PerPage: 100},
		})
		if err != nil {
			return githubsvc.PRStatus{}, fmt.Errorf("list workflow runs for ref %s: %w", headSHA, err)
		}
		workflows = res.GetWorkflowRuns()
	}

	return githubsvc.SummarizePR(pr, checks, workflows), nil
}

// ciTerminalReason reports whether the observation reached a terminal
// non-green outcome (any check or workflow concluded in the fast-fail
// allow-list) and, if so, a human-readable reason.
func ciTerminalReason(status githubsvc.PRStatus) (string, bool) {
	for _, c := range status.Checks {
		if ciFailedConclusions[c.Conclusion] {
			return fmt.Sprintf("check %q concluded %s", c.Name, c.Conclusion), true
		}
	}
	for _, w := range status.Workflows {
		if ciFailedConclusions[w.Conclusion] {
			return fmt.Sprintf("workflow %q concluded %s", w.Name, w.Conclusion), true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// github_pr_status
// ---------------------------------------------------------------------------

// GitHubPRStatusTool returns a point-in-time snapshot of a PR and its CI
// health (check runs + workflow runs), summarized as a
// githubsvc.PRStatus.
type GitHubPRStatusTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	defaults githubsvc.OwnerRepo
}

// NewGitHubPRStatusTool constructs a status tool bound to a client pool
// and the default owner/repo target.
func NewGitHubPRStatusTool(pool *githubsvc.ClientPool, defaults githubsvc.OwnerRepo) *GitHubPRStatusTool {
	return &GitHubPRStatusTool{pool: pool, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubPRStatusTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubPRStatusTool) Name() string { return "github_pr_status" }

// Description implements tools.Tool.
func (t *GitHubPRStatusTool) Description() string {
	return "Get the current CI health of a pull request: PR state plus all check runs and workflow runs for its head SHA. Returns the summarized PR status including an all_green verdict."
}

// Parameters implements tools.Tool.
func (t *GitHubPRStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner": {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":  {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"pr":    {Type: schemaTypeInteger, Description: "Pull request number. Required."},
		},
		Required: []string{"pr"},
	}
}

// IsReadOnly implements tools.Tool: the tool only issues GET requests.
func (t *GitHubPRStatusTool) IsReadOnly(map[string]any) bool { return true }

// Execute implements tools.Tool.
func (t *GitHubPRStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	prNum, ok := prIntArg(args, "pr")
	if !ok {
		return nil, fmt.Errorf("github_pr_status: pr is required")
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	status, err := fetchPRStatus(ctx, client, owner, repo, prNum)
	if err != nil {
		return nil, fmt.Errorf("github_pr_status: %w", err)
	}
	return status, nil
}

// Ensure GitHubPRStatusTool implements tools.Tool.
var _ tools.Tool = (*GitHubPRStatusTool)(nil)

// ---------------------------------------------------------------------------
// github_ci_wait
// ---------------------------------------------------------------------------

// GitHubCIWaitTool polls a PR's CI until it is all green or a check or
// workflow concludes a terminal failure. When the caller's timeout
// expires without a verdict, the wait is PARKED: the ReArmSink is
// scheduled to re-check the CI ref in ciWaitParkLeadTime and the result
// carries a resume waitID.
type GitHubCIWaitTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	defaults githubsvc.OwnerRepo
	sink     githubsvc.ReArmSink
}

// NewGitHubCIWaitTool constructs a CI-wait tool bound to a client pool,
// the default owner/repo target, and the ReArmSink that parks expired
// waits. A nil sink still parks: the result carries the waitID and the
// park error is returned, so the outcome is never lost.
func NewGitHubCIWaitTool(pool *githubsvc.ClientPool, defaults githubsvc.OwnerRepo, sink githubsvc.ReArmSink) *GitHubCIWaitTool {
	return &GitHubCIWaitTool{pool: pool, defaults: defaults, sink: sink}
}

// Category implements tools.Categorizer.
func (t *GitHubCIWaitTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubCIWaitTool) Name() string { return "github_ci_wait" }

// Description implements tools.Tool.
func (t *GitHubCIWaitTool) Description() string {
	return "Wait for a pull request's CI to finish. Polls check runs and workflow runs until all green, or until a check fails/cancels (fast-fail, no full timeout). On timeout the wait is parked and re-armed; the result carries a wait_id to resume later. timeout_seconds is capped at 3600."
}

// Parameters implements tools.Tool.
func (t *GitHubCIWaitTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner":           {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":            {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"pr":              {Type: schemaTypeInteger, Description: "Pull request number. Required."},
			"timeout_seconds": {Type: schemaTypeInteger, Description: "How long to wait before parking (optional, default 600, capped at 3600)."},
		},
		Required: []string{"pr"},
	}
}

// IsReadOnly implements tools.Tool: the tool only issues GET requests
// (polling) — the park itself is a local schedule record, not a GitHub
// write.
func (t *GitHubCIWaitTool) IsReadOnly(map[string]any) bool { return true }

// park records the CI ref on the ReArmSink and returns the parked result
// plus an error wrapping githubsvc.ErrCIWaitParked. A sink error does not
// suppress the park: the result is still returned and the error is
// folded into the wrap. A nil sink (registration without a scheduler
// wiring) still parks and returns the parked result; only the ScheduleReArm
// call is skipped and the skip is logged, so the operator sees why the
// re-arm will not fire.
func (t *GitHubCIWaitTool) park(owner, repo string, prNum int, status githubsvc.PRStatus) (any, error) {
	waitID := id.Generate("ciwait_")
	at := time.Now().Add(ciWaitParkLeadTime)
	ref := githubsvc.CIRef{Owner: owner, Repo: repo, Number: prNum}

	var sinkErr error
	if t.sink != nil {
		sinkErr = t.sink.ScheduleReArm(ref, at, waitID)
	} else {
		slog.Warn("github_ci_wait: parked without re-arm — no ReArmSink wired (instruction scheduler absent); resume manually",
			"owner", owner, "repo", repo, "pr", prNum, "wait_id", waitID)
	}

	result := map[string]any{
		"parked":   true,
		"resume":   true,
		"wait_id":  waitID,
		"rearm_at": at.Format(time.RFC3339),
		"status":   status,
	}
	if sinkErr != nil {
		return result, fmt.Errorf("%w: schedule re-arm for %s/%s#%d: %w", githubsvc.ErrCIWaitParked, owner, repo, prNum, sinkErr)
	}
	if t.sink == nil {
		return result, fmt.Errorf("%w: parked %s/%s#%d as %s (no re-arm sink wired; resume manually)", githubsvc.ErrCIWaitParked, owner, repo, prNum, waitID)
	}
	return result, fmt.Errorf("%w: re-armed %s/%s#%d as %s for %s", githubsvc.ErrCIWaitParked, owner, repo, prNum, waitID, at.Format(time.RFC3339))
}

// Execute implements tools.Tool.
func (t *GitHubCIWaitTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	prNum, ok := prIntArg(args, "pr")
	if !ok {
		return nil, fmt.Errorf("github_ci_wait: pr is required")
	}
	timeout, _ := prIntArg(args, "timeout_seconds") // ok=false or <=0 → default below
	if timeout <= 0 {
		timeout = ciWaitMinTimeout
	}
	if timeout > ciWaitMaxTimeout {
		timeout = ciWaitMaxTimeout
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	// An earlier caller deadline wins: a cancelled ctx also parks, so a
	// busy agent loop can never hold a stuck turn.
	if cd, has := ctx.Deadline(); has && cd.Before(deadline) {
		deadline = cd
	}

	interval := ciWaitPollIntervalStart
	for {
		// Deadline checks run BEFORE every fetch and before every sleep so
		// a timeout_seconds=1 wait never sleeps a second past its deadline.
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return t.park(owner, repo, prNum, githubsvc.PRStatus{})
		}

		status, ferr := fetchPRStatus(ctx, client, owner, repo, prNum)
		if ferr != nil {
			return nil, fmt.Errorf("github_ci_wait: %w", ferr)
		}

		if status.AllGreen {
			return map[string]any{"status": status, "terminal": "green"}, nil
		}
		if reason, failed := ciTerminalReason(status); failed {
			// Fast-fail: a concluded failure is terminal; do not wait out
			// the remaining timeout on red CI.
			return map[string]any{"status": status, "terminal": "failed", "reason": reason}, nil
		}

		// Not terminal: back off before the next observation. The sleep is
		// the smaller of the doubling interval and the time left, so the
		// loop re-enters the deadline check instead of oversleeping past
		// it.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return t.park(owner, repo, prNum, status)
		}
		sleep := interval
		if remaining < sleep {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
		case <-time.After(sleep):
		}
		interval *= 2
		if interval > ciWaitPollIntervalMax {
			interval = ciWaitPollIntervalMax
		}
	}
}

// Ensure GitHubCIWaitTool implements tools.Tool.
var _ tools.Tool = (*GitHubCIWaitTool)(nil)
