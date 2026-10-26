package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/google/go-github/v93/github"
)

// github_issue.go implements the two issue tools: github_issue_get (read-only
// fetch of a single issue) and github_issue_comment (post an issue comment,
// effects-wrapped). Both reuse the owner/repo resolution, the shared arg
// helpers, and the effects-wrapped effect protocol established by
// github_pr.go / github_ci.go.
//
// owner/repo are resolved via githubsvc.OwnerRepo.Resolve BEFORE any pool.For
// call and, for the comment tool, before any effects claim, so a blank target
// or an unresolvable token (ErrNoToken) fails closed without touching the
// ledger.

// ---------------------------------------------------------------------------
// github_issue_get
// ---------------------------------------------------------------------------

// GitHubIssueGetTool returns a single issue's core fields (number, title,
// state, html_url) for the resolved owner/repo. Read-only: it issues a GET
// only.
type GitHubIssueGetTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	defaults githubsvc.OwnerRepo
}

// NewGitHubIssueGetTool constructs a get tool bound to a client pool and the
// default owner/repo target.
func NewGitHubIssueGetTool(pool *githubsvc.ClientPool, defaults githubsvc.OwnerRepo) *GitHubIssueGetTool {
	return &GitHubIssueGetTool{pool: pool, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubIssueGetTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubIssueGetTool) Name() string { return "github_issue_get" }

// Description implements tools.Tool.
func (t *GitHubIssueGetTool) Description() string {
	return "Get a single issue by number on a GitHub repository. Optionally target a specific owner/repo (defaults to the session target). Returns the issue's number, title, state, and URL."
}

// Parameters implements tools.Tool.
func (t *GitHubIssueGetTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner":  {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":   {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"number": {Type: schemaTypeInteger, Description: "Issue number. Required."},
		},
		Required: []string{"number"},
	}
}

// IsReadOnly implements tools.Tool: the tool only issues GET requests.
func (t *GitHubIssueGetTool) IsReadOnly(map[string]any) bool { return true }

// Execute implements tools.Tool.
func (t *GitHubIssueGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	number, ok := prIntArg(args, "number")
	if !ok {
		return nil, fmt.Errorf("github_issue_get: number is required")
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	issue, _, getErr := client.Issues.Get(ctx, owner, repo, number)
	if getErr != nil {
		return nil, fmt.Errorf("github_issue_get: %w", getErr)
	}
	return map[string]any{
		"number":   issue.GetNumber(),
		"title":    issue.GetTitle(),
		"state":    issue.GetState(),
		"html_url": issue.GetHTMLURL(),
	}, nil
}

// Ensure GitHubIssueGetTool implements tools.Tool.
var _ tools.Tool = (*GitHubIssueGetTool)(nil)

// ---------------------------------------------------------------------------
// github_issue_comment
// ---------------------------------------------------------------------------

// GitHubIssueCommentTool posts a single comment on an issue. Effects-wrapped:
// it claims a stable, content-derived effects key before issuing the
// irreversible GitHub API call, records the provider's response as the
// receipt, and completes the record. GitHub is NOT provider-idempotent, so
// EffectMeta declares ProviderIdempotent=false; a reconciled stuck record is
// surfaced for human reconciliation rather than auto-retried.
type GitHubIssueCommentTool struct {
	tools.ToolDefaults
	pool     *githubsvc.ClientPool
	ledger   effects.Ledger
	defaults githubsvc.OwnerRepo
}

// NewGitHubIssueCommentTool constructs a comment tool bound to a pool, the
// effects ledger, and the default owner/repo target.
func NewGitHubIssueCommentTool(pool *githubsvc.ClientPool, ledger effects.Ledger, defaults githubsvc.OwnerRepo) *GitHubIssueCommentTool {
	return &GitHubIssueCommentTool{pool: pool, ledger: ledger, defaults: defaults}
}

// Category implements tools.Categorizer.
func (t *GitHubIssueCommentTool) Category() string { return "github" }

// Name implements tools.Tool.
func (t *GitHubIssueCommentTool) Name() string { return "github_issue_comment" }

// Description implements tools.Tool.
func (t *GitHubIssueCommentTool) Description() string {
	return "Post a comment on a GitHub issue. Optionally target a specific owner/repo (defaults to the session target). Returns the comment's id and URL."
}

// Parameters implements tools.Tool.
func (t *GitHubIssueCommentTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"owner":  {Type: schemaTypeString, Description: "Repository owner (optional, defaults to the session target)."},
			"repo":   {Type: schemaTypeString, Description: "Repository name (optional, defaults to the session target)."},
			"number": {Type: schemaTypeInteger, Description: "Issue number. Required."},
			"body":   {Type: schemaTypeString, Description: "Comment body. Required."},
		},
		Required: []string{"number", "body"},
	}
}

// Execute implements tools.Tool. The effects key is content-derived:
// EffectKey("github_issue_comment", owner, repo, number, sha256(body)).
func (t *GitHubIssueCommentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	owner, repo, err := t.defaults.Resolve(prStringArg(args, "owner"), prStringArg(args, "repo"))
	if err != nil {
		return nil, err
	}
	number, ok := prIntArg(args, "number")
	if !ok {
		return nil, fmt.Errorf("github_issue_comment: number is required")
	}
	body := prStringArg(args, "body")
	if body == "" {
		return nil, fmt.Errorf("github_issue_comment: body is required")
	}
	client, err := t.pool.For(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	key := effects.EffectKey("github_issue_comment", owner, repo, fmt.Sprint(number), prBodySHA256(body))
	meta := effects.EffectMeta{Tool: "github_issue_comment", ProviderIdempotent: false}

	var resID int64
	var resURL string
	receipt, reused, runErr := effects.Run(ctx, t.ledger, key, meta, func(ctx context.Context) (json.RawMessage, error) {
		c, _, apiErr := client.Issues.CreateComment(ctx, owner, repo, number,
			github.IssueCommentRequest{Body: body})
		if apiErr != nil {
			return nil, apiErr
		}
		resID = c.GetID()
		resURL = c.GetHTMLURL()
		return json.Marshal(map[string]any{"id": resID, "html_url": resURL})
	})
	if runErr != nil {
		return nil, fmt.Errorf("github_issue_comment: %w", runErr)
	}
	if !reused {
		if err := t.ledger.Complete(ctx, key); err != nil {
			return nil, fmt.Errorf("github_issue_comment: complete: %w", err)
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

// Ensure GitHubIssueCommentTool implements tools.Tool.
var _ tools.Tool = (*GitHubIssueCommentTool)(nil)
