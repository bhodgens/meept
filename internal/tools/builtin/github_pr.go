package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/google/go-github/v93/github"
)

// github_pr.go implements the four PR-lifecycle tools (create, comment,
// review, merge). Each tool is effects-wrapped: it claims a stable,
// content-derived effects key before issuing the irreversible GitHub API
// call, records the provider's response as the receipt, and completes the
// record. GitHub is NOT provider-idempotent, so EffectMeta declares
// ProviderIdempotent=false; reconciled stuck records are surfaced for
// human reconciliation rather than auto-retried.
//
// owner/repo are resolved via githubsvc.OwnerRepo.Resolve BEFORE any
// effects claim, so a blank target or an unresolvable token (ErrNoToken)
// fails closed without touching the ledger.

// ---------------------------------------------------------------------------
// Small arg-extraction helpers shared by the four tools.
// ---------------------------------------------------------------------------

// prStringArg returns args[key] as a string, or "" when absent/mistyped.
func prStringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// prBoolArg returns args[key] as a (value, present) pair for bool args.
func prBoolArg(args map[string]any, key string) (bool, bool) {
	b, ok := args[key].(bool)
	return b, ok
}

// prIntArg returns args[key] as an int. It accepts the int, int64, float64
// (JSON numbers) and decimal-string encodings so it is robust to however
// the LLM serialises a number.
func prIntArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}

// githubLedgerGuard returns the fail-closed error an effects-wrapped tool
// reports when constructed without an effects ledger (a nil-Ledger
// RegistrationOptions still registers the tool for membership, but its
// Execute cannot claim a record and must not run the irreversible call).
// One helper keeps the five effects-wrapped tools naming the missing
// dependency identically.
func githubLedgerGuard(toolName string) error {
	return fmt.Errorf("%s: no effects ledger is wired: the tool refuses to run its irreversible GitHub call without the effects idempotency guard", toolName)
}

// prBodySHA256 returns the lowercase hex SHA256 of s, the content half of
// the comment/review effects keys (contract C4: sha256(body)).
func prBodySHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// isClientAPIError reports whether err is a GitHub API client error with a
// 4xx status (including 409 "not mergeable"). Those are terminal provider
// outcomes — the attempt happened — so the merge tool records a receipt and
// completes rather than abandoning. 5xx/transport errors are NOT matched:
// those leave the record claimed for reconcile.
func isClientAPIError(err error) bool {
	var apiErr *github.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return false
	}
	code := apiErr.Response.StatusCode
	return code >= http.StatusBadRequest && code < http.StatusInternalServerError
}

// ---------------------------------------------------------------------------
// github_pr_create
// ---------------------------------------------------------------------------

// GitHubPRCreateTool opens a new pull request on the resolved owner/repo.
type GitHubPRCreateTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	ledger   effects.Ledger
	defaults githubsvc.OwnerRepo
}

// NewGitHubPRCreateTool constructs a create tool bound to a pool, the
// effects ledger, and the default owner/repo target.
func NewGitHubPRCreateTool(pool *githubsvc.ClientPool, ledger effects.Ledger, defaults githubsvc.OwnerRepo) *GitHubPRCreateTool {
	return &GitHubPRCreateTool{pool: pool, ledger: ledger, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubPRCreateTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubPRCreateTool) Name() string { return "github_pr_create" }

// Description implements tools.Tool.
func (t *GitHubPRCreateTool) Description() string {
	return "Create a new pull request on a GitHub repository. Optionally target a specific owner/repo (defaults to the session target). Returns the created PR's number and URL."
}

// Parameters implements tools.Tool.
func (t *GitHubPRCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner": {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":  {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"base":  {Type: schemaTypeString, Description: "Branch or commit the PR merges into (optional, defaults to the repo default branch)."},
			"head":  {Type: schemaTypeString, Description: "Branch containing the changes. Required."},
			"title": {Type: schemaTypeString, Description: "PR title. Required."},
			"body":  {Type: schemaTypeString, Description: "PR body/markdown (optional)."},
			"draft": {Type: schemaTypeBoolean, Description: "Open as a draft PR (optional, default false)."},
		},
		Required: []string{"head", "title"},
	}
}

// Execute implements tools.Tool.
func (t *GitHubPRCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	head := prStringArg(args, "head")
	title := prStringArg(args, "title")
	if head == "" || title == "" {
		return nil, fmt.Errorf("github_pr_create: head and title are required")
	}
	req := github.CreatePullRequest{
		Base:  prStringArg(args, "base"),
		Head:  head,
		Title: github.String(title),
		Body:  github.String(prStringArg(args, "body")),
	}
	if d, ok := prBoolArg(args, "draft"); ok {
		req.Draft = github.Bool(d)
	}

	key := effects.EffectKey("github_pr_create", owner, repo, head, title)
	meta := effects.EffectMeta{Tool: "github_pr_create", ProviderIdempotent: false}
	if t.ledger == nil {
		return nil, githubLedgerGuard("github_pr_create")
	}

	var resNumber int
	var resURL string
	receipt, reused, runErr := effects.Run(ctx, t.ledger, key, meta, func(ctx context.Context) (json.RawMessage, error) {
		pr, _, apiErr := client.PullRequests.Create(ctx, owner, repo, req)
		if apiErr != nil {
			return nil, apiErr
		}
		resNumber = pr.GetNumber()
		resURL = pr.GetHTMLURL()
		return json.Marshal(map[string]any{"number": resNumber, "html_url": resURL})
	})
	if runErr != nil {
		return nil, fmt.Errorf("github_pr_create: %w", runErr)
	}
	if !reused {
		if err := t.ledger.Complete(ctx, key); err != nil {
			return nil, fmt.Errorf("github_pr_create: complete: %w", err)
		}
	} else {
		var rc map[string]any
		if json.Unmarshal(receipt, &rc) == nil {
			if n, ok := rc["number"].(float64); ok {
				resNumber = int(n)
			}
			resURL, _ = rc["html_url"].(string)
		}
	}

	return map[string]any{"number": resNumber, "url": resURL}, nil
}

// Ensure GitHubPRCreateTool implements tools.Tool.
var _ tools.Tool = (*GitHubPRCreateTool)(nil)

// ---------------------------------------------------------------------------
// github_pr_comment
// ---------------------------------------------------------------------------

// GitHubPRCommentTool posts a single review comment on a pull request.
type GitHubPRCommentTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	ledger   effects.Ledger
	defaults githubsvc.OwnerRepo
}

// NewGitHubPRCommentTool constructs a comment tool bound to a pool, the
// effects ledger, and the default owner/repo target.
func NewGitHubPRCommentTool(pool *githubsvc.ClientPool, ledger effects.Ledger, defaults githubsvc.OwnerRepo) *GitHubPRCommentTool {
	return &GitHubPRCommentTool{pool: pool, ledger: ledger, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubPRCommentTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubPRCommentTool) Name() string { return "github_pr_comment" }

// Description implements tools.Tool.
func (t *GitHubPRCommentTool) Description() string {
	return "Post a review comment on a pull request. Returns the comment's id and URL."
}

// Parameters implements tools.Tool.
func (t *GitHubPRCommentTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner": {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":  {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"pr":    {Type: schemaTypeInteger, Description: "Pull request number. Required."},
			"body":  {Type: schemaTypeString, Description: "Comment body. Required."},
		},
		Required: []string{"pr", "body"},
	}
}

// Execute implements tools.Tool.
func (t *GitHubPRCommentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	prNum, ok := prIntArg(args, "pr")
	if !ok {
		return nil, fmt.Errorf("github_pr_comment: pr is required")
	}
	body := prStringArg(args, "body")
	if body == "" {
		return nil, fmt.Errorf("github_pr_comment: body is required")
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	key := effects.EffectKey("github_pr_comment", owner, repo, fmt.Sprint(prNum), prBodySHA256(body))
	meta := effects.EffectMeta{Tool: "github_pr_comment", ProviderIdempotent: false}
	if t.ledger == nil {
		return nil, githubLedgerGuard("github_pr_comment")
	}

	var resID int64
	var resURL string
	receipt, reused, runErr := effects.Run(ctx, t.ledger, key, meta, func(ctx context.Context) (json.RawMessage, error) {
		c, _, apiErr := client.PullRequests.CreateComment(ctx, owner, repo, prNum,
			github.CreatePullRequestCommentRequest{Body: body})
		if apiErr != nil {
			return nil, apiErr
		}
		resID = c.GetID()
		resURL = c.GetHTMLURL()
		return json.Marshal(map[string]any{"id": resID, "html_url": resURL})
	})
	if runErr != nil {
		return nil, fmt.Errorf("github_pr_comment: %w", runErr)
	}
	if !reused {
		if err := t.ledger.Complete(ctx, key); err != nil {
			return nil, fmt.Errorf("github_pr_comment: complete: %w", err)
		}
	} else {
		var rc map[string]any
		if json.Unmarshal(receipt, &rc) == nil {
			if n, ok := rc["id"].(float64); ok {
				resID = int64(n)
			}
			resURL, _ = rc["html_url"].(string)
		}
	}

	return map[string]any{"id": resID, "url": resURL}, nil
}

// Ensure GitHubPRCommentTool implements tools.Tool.
var _ tools.Tool = (*GitHubPRCommentTool)(nil)

// ---------------------------------------------------------------------------
// github_pr_review
// ---------------------------------------------------------------------------

// prReviewEvents is the allow-list of review events the tool accepts.
var prReviewEvents = map[string]bool{"APPROVE": true, "REQUEST_CHANGES": true, "COMMENT": true}

// GitHubPRReviewTool submits a structured review on a pull request.
type GitHubPRReviewTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	ledger   effects.Ledger
	defaults githubsvc.OwnerRepo
}

// NewGitHubPRReviewTool constructs a review tool bound to a pool, the
// effects ledger, and the default owner/repo target.
func NewGitHubPRReviewTool(pool *githubsvc.ClientPool, ledger effects.Ledger, defaults githubsvc.OwnerRepo) *GitHubPRReviewTool {
	return &GitHubPRReviewTool{pool: pool, ledger: ledger, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubPRReviewTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubPRReviewTool) Name() string { return "github_pr_review" }

// Description implements tools.Tool.
func (t *GitHubPRReviewTool) Description() string {
	return "Submit a review on a pull request with event APPROVE, REQUEST_CHANGES, or COMMENT (default COMMENT). Returns the review's id."
}

// Parameters implements tools.Tool.
func (t *GitHubPRReviewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner": {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":  {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"pr":    {Type: schemaTypeInteger, Description: "Pull request number. Required."},
			"body":  {Type: schemaTypeString, Description: "Review body. Required."},
			"event": {Type: schemaTypeString, Description: "Review event. One of APPROVE, REQUEST_CHANGES, COMMENT. Default COMMENT.",
				Enum: []string{"APPROVE", "REQUEST_CHANGES", "COMMENT"}},
		},
		Required: []string{"pr", "body"},
	}
}

// Execute implements tools.Tool.
func (t *GitHubPRReviewTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	prNum, ok := prIntArg(args, "pr")
	if !ok {
		return nil, fmt.Errorf("github_pr_review: pr is required")
	}
	body := prStringArg(args, "body")
	if body == "" {
		return nil, fmt.Errorf("github_pr_review: body is required")
	}
	event := prStringArg(args, "event")
	if event == "" {
		event = "COMMENT"
	}
	if !prReviewEvents[event] {
		return nil, fmt.Errorf("github_pr_review: invalid event %q (want APPROVE, REQUEST_CHANGES, or COMMENT)", event)
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	key := effects.EffectKey("github_pr_review", owner, repo, fmt.Sprint(prNum), event, prBodySHA256(body))
	meta := effects.EffectMeta{Tool: "github_pr_review", ProviderIdempotent: false}
	if t.ledger == nil {
		return nil, githubLedgerGuard("github_pr_review")
	}

	var resID int64
	receipt, reused, runErr := effects.Run(ctx, t.ledger, key, meta, func(ctx context.Context) (json.RawMessage, error) {
		r, _, apiErr := client.PullRequests.CreateReview(ctx, owner, repo, prNum, &github.PullRequestReviewRequest{
			Body:  github.String(body),
			Event: github.String(event),
		})
		if apiErr != nil {
			return nil, apiErr
		}
		resID = r.GetID()
		return json.Marshal(map[string]any{"id": resID})
	})
	if runErr != nil {
		return nil, fmt.Errorf("github_pr_review: %w", runErr)
	}
	if !reused {
		if err := t.ledger.Complete(ctx, key); err != nil {
			return nil, fmt.Errorf("github_pr_review: complete: %w", err)
		}
	} else {
		var rc map[string]any
		if json.Unmarshal(receipt, &rc) == nil {
			if n, ok := rc["id"].(float64); ok {
				resID = int64(n)
			}
		}
	}

	return map[string]any{"id": resID}, nil
}

// Ensure GitHubPRReviewTool implements tools.Tool.
var _ tools.Tool = (*GitHubPRReviewTool)(nil)

// ---------------------------------------------------------------------------
// github_pr_merge
// ---------------------------------------------------------------------------

// GitHubPRMergeTool merges a pull request.
type GitHubPRMergeTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	ledger   effects.Ledger
	defaults githubsvc.OwnerRepo
}

// NewGitHubPRMergeTool constructs a merge tool bound to a pool, the
// effects ledger, and the default owner/repo target.
func NewGitHubPRMergeTool(pool *githubsvc.ClientPool, ledger effects.Ledger, defaults githubsvc.OwnerRepo) *GitHubPRMergeTool {
	return &GitHubPRMergeTool{pool: pool, ledger: ledger, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubPRMergeTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubPRMergeTool) Name() string { return "github_pr_merge" }

// Description implements tools.Tool.
func (t *GitHubPRMergeTool) Description() string {
	return "Merge a pull request using the merge, squash, or rebase strategy (default merge). Returns the merge result."
}

// Parameters implements tools.Tool.
func (t *GitHubPRMergeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner": {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":  {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"pr":    {Type: schemaTypeInteger, Description: "Pull request number. Required."},
			"strategy": {Type: schemaTypeString, Description: "Merge strategy: merge, squash, or rebase. Default merge.",
				Enum: []string{"merge", "squash", "rebase"}},
		},
		Required: []string{"pr"},
	}
}

// Execute implements tools.Tool. The head SHA is fetched before the effects
// claim so the key is stable across resume. A 4xx/409 provider rejection is
// a recorded outcome: the receipt is stored and the record completed (the
// attempt happened), then the provider error is surfaced to the caller.
func (t *GitHubPRMergeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	prNum, ok := prIntArg(args, "pr")
	if !ok {
		return nil, fmt.Errorf("github_pr_merge: pr is required")
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	// Fetch the PR head SHA before the claim so the effects key is stable.
	pr, _, getErr := client.PullRequests.Get(ctx, owner, repo, prNum)
	if getErr != nil {
		return nil, fmt.Errorf("github_pr_merge: fetch pull request: %w", getErr)
	}
	headSHA := pr.GetHead().GetSHA()

	strategy := prStringArg(args, "strategy")
	if strategy == "" {
		strategy = "merge"
	}

	key := effects.EffectKey("github_pr_merge", owner, repo, fmt.Sprint(prNum), headSHA)
	meta := effects.EffectMeta{Tool: "github_pr_merge", ProviderIdempotent: false}
	if t.ledger == nil {
		return nil, githubLedgerGuard("github_pr_merge")
	}

	var resMerged bool
	var resSHA, resMsg string
	// attemptErr carries the 4xx provider outcome so it can be surfaced to
	// the caller after the receipt has been recorded and the record completed.
	var attemptErr error
	receipt, reused, runErr := effects.Run(ctx, t.ledger, key, meta, func(ctx context.Context) (json.RawMessage, error) {
		m, _, apiErr := client.PullRequests.Merge(ctx, owner, repo, prNum, "",
			&github.PullRequestOptions{MergeMethod: strategy})
		if apiErr != nil {
			if isClientAPIError(apiErr) {
				// 4xx/409: the merge attempt happened. Record a failure
				// receipt and complete so reconcile never re-tries it;
				// surface the provider error to the caller afterwards.
				attemptErr = fmt.Errorf("github_pr_merge: provider rejected merge: %w", apiErr)
				resMerged = false
				resMsg = apiErr.Error()
				return json.Marshal(map[string]any{"merged": resMerged, "message": resMsg})
			}
			// Transport/5xx: leave the record claimed for reconcile.
			return nil, apiErr
		}
		resMerged = m.GetMerged()
		resSHA = m.GetSHA()
		resMsg = m.GetMessage()
		return json.Marshal(map[string]any{"merged": resMerged, "sha": resSHA, "message": resMsg})
	})
	if runErr != nil {
		return nil, fmt.Errorf("github_pr_merge: %w", runErr)
	}
	if !reused {
		if err := t.ledger.Complete(ctx, key); err != nil {
			return nil, fmt.Errorf("github_pr_merge: complete: %w", err)
		}
	} else {
		var rc map[string]any
		if json.Unmarshal(receipt, &rc) == nil {
			if v, ok := rc["merged"].(bool); ok {
				resMerged = v
			}
			if v, ok := rc["sha"].(string); ok {
				resSHA = v
			}
			if v, ok := rc["message"].(string); ok {
				resMsg = v
			}
		}
	}

	return map[string]any{"merged": resMerged, "sha": resSHA, "message": resMsg}, attemptErr
}

// Ensure GitHubPRMergeTool implements tools.Tool.
var _ tools.Tool = (*GitHubPRMergeTool)(nil)
