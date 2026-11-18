package intake

// Outcome write-back (Contract 3 of the github-intake-factory tree,
// docs/plans/github-intake-factory/03-writeback.md): goal outcomes
// surface on the source ticket as comments, with a done label on
// healthy. Comments are effects-wrapped so a poll cycle can never storm
// a ticket with duplicates.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"

	"github.com/google/go-github/v93/github"
)

// Comment body template (plain text; Detail is truncated to 280 runes):
//
//	[meept factory] goal <GoalID>: <State>
//	<Detail>
const commentTemplate = "[meept factory] goal %s: %s\n%s"

// maxDetailRunes bounds the Detail section of a comment body. Cut on
// runes so a multi-byte character is never split.
const maxDetailRunes = 280

// defaultDoneLabel is the label added when an outcome reports healthy.
const defaultDoneLabel = "factory:done"

// Outcome is one goal-assessment result to surface on the source ticket.
type Outcome struct {
	GoalID string
	State  string // e.g. "healthy" | "at_risk" | "broken"
	Detail string // human-readable assessment summary; truncated to 280 runes
}

// WriteBack posts goal outcomes to tickets. The comment path is wrapped
// in the effects ledger: one comment per (goal, state) transition — a
// repeated poll with the same outcome collides on the completed key and
// reuses the stored receipt instead of re-posting (storm guard).
type WriteBack struct {
	pool   *githubsvc.ClientPool
	repo   githubsvc.OwnerRepo
	ledger effects.Ledger
	log    *slog.Logger

	doneLabel string
}

// NewWriteBack builds a WriteBack over the githubsvc client pool and the
// effects ledger. The ledger must be non-nil: the comment path claims
// the effect key before any API call (fail-closed).
func NewWriteBack(pool *githubsvc.ClientPool, ledger effects.Ledger,
	repo githubsvc.OwnerRepo, handler slog.Handler) *WriteBack {
	if handler == nil {
		handler = slog.DiscardHandler
	}
	return &WriteBack{
		pool:      pool,
		repo:      repo,
		ledger:    ledger,
		log:       slog.New(handler),
		doneLabel: defaultDoneLabel,
	}
}

// SetDoneLabel overrides the label added on healthy outcomes (default
// "factory:done"). Config wiring calls this. A nil-guarded setter.
func (w *WriteBack) SetDoneLabel(label string) {
	if label == "" {
		return
	}
	w.doneLabel = label
}

// isClientAPIError reports whether err is a GitHub API client error with
// a 4xx status. Those are terminal provider outcomes — the attempt
// happened — so the comment path records a receipt and completes rather
// than leaving the record claimed. 5xx/transport errors are NOT matched:
// those stay claimed for reconcile.
func isClientAPIError(err error) bool {
	var apiErr *github.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response == nil {
		return false
	}
	code := apiErr.Response.StatusCode
	return code >= http.StatusBadRequest && code < http.StatusInternalServerError
}

// truncateRunes cuts s to at most max runes, never splitting a
// multi-byte character.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// Report posts the outcome comment on the ticket (effects-wrapped) and,
// when the outcome state is "healthy", adds the done label.
//
// The done label rides the same effects record as the comment: it is
// idempotent upstream (GitHub dedups labels) and is part of the same
// logical effect. A crash between comment and label leaves the comment
// recorded; the next poll with the same outcome collides on the
// completed key and re-adds the label harmlessly.
//
// 4xx provider rejections follow the recorded-outcome pattern
// (github_pr.go): receipt + Complete + surface the error. 5xx/transport
// errors leave the record claimed for reconcile.
func (w *WriteBack) Report(ctx context.Context, t Ticket, o Outcome) (int64, string, error) {
	if w.ledger == nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: nil effects ledger (fail-closed)")
	}
	if w.pool == nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: nil client pool")
	}

	client, err := w.pool.For(ctx, w.repo.Owner, w.repo.Repo)
	if err != nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: pool client for %s: %w", w.repo.Owner+"/"+w.repo.Repo, err)
	}

	num, err := strconv.Atoi(t.ID)
	if err != nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: ticket id %q is not an issue number: %w", t.ID, err)
	}

	key := effects.EffectKey("intake_writeback", t.Repo, t.ID, o.State+"-"+o.GoalID)
	payload, err := json.Marshal(o)
	if err != nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: marshal outcome: %w", err)
	}
	meta := effects.EffectMeta{
		Tool:               "intake_writeback",
		Payload:            payload,
		ProviderIdempotent: false,
	}

	who := effects.Claimant{AgentID: o.GoalID}
	var attemptErr error
	receipt, reused, runErr := effects.Run(ctx, w.ledger, key, meta, who, func(ctx context.Context) (json.RawMessage, error) {
		body := fmt.Sprintf(commentTemplate, o.GoalID, o.State, truncateRunes(o.Detail, maxDetailRunes))
		c, resp, apiErr := client.Issues.CreateComment(ctx, w.repo.Owner, w.repo.Repo, num, github.IssueCommentRequest{Body: body})
		if apiErr != nil {
			if isClientAPIError(apiErr) {
				// 4xx: the comment attempt happened (e.g. issue
				// locked). Record a failure receipt and complete so
				// reconcile never re-tries it; surface the provider
				// error to the caller afterwards.
				attemptErr = fmt.Errorf("intake: writeback: provider rejected comment on %s#%s: %w", t.Repo, t.ID, apiErr)
				return json.Marshal(map[string]any{"id": int64(0), "html_url": "", "message": apiErr.Error()})
			}
			// Transport/5xx: leave the record claimed for reconcile.
			return nil, apiErr
		}
		_ = resp
		return json.Marshal(map[string]any{"id": c.GetID(), "html_url": c.GetHTMLURL()})
	})
	if runErr != nil {
		// bughunt D7 retry lane: a RECEIPTED prior from OUR OWN claimant
		// (the label-failure window above — comment landed, label failed,
		// record left receipted so the failure stays retryable) refuses
		// re-execution by design. Adopt it: re-run ONLY the label, then
		// complete. Anything else (claimed by another caller, unknown
		// state) still fails the call.
		if prior, gerr := w.ledger.Get(ctx, key); gerr == nil && prior != nil &&
			prior.State == effects.StateReceipted && who.Matches(effects.Claimant{
			AgentID: prior.ClaimantAgentID, SessionID: prior.ClaimantSessionID,
		}) {
			var adoptErr error
			receipt, reused, adoptErr = w.retryLabelOnly(ctx, client, num, o, key, who, prior.Receipt)
			if adoptErr != nil {
				return 0, "", fmt.Errorf("intake: WriteBack.Report: %w", adoptErr)
			}
		} else {
			return 0, "", fmt.Errorf("intake: WriteBack.Report: %w", runErr)
		}
	}

	if reused {
		// Idempotent no-op path: the receipt (and the healthy label,
		// which rides the same effect) came from the first execution —
		// never re-post. On the crash-window case the label re-add is
		// idempotent upstream.
		w.log.DebugContext(ctx, "intake writeback reused",
			"repo", t.Repo, "ticket", t.ID, "goal", o.GoalID, "state", o.State)
		var rc struct {
			ID      int64  `json:"id"`
			HTMLURL string `json:"html_url"`
		}
		if err := json.Unmarshal(receipt, &rc); err != nil {
			return 0, "", fmt.Errorf("intake: WriteBack.Report: decode reused receipt %s: %w", string(receipt), err)
		}
		return rc.ID, rc.HTMLURL, nil
	}

	var rc struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(receipt, &rc); err != nil {
		return 0, "", fmt.Errorf("intake: WriteBack.Report: decode receipt %s: %w", string(receipt), err)
	}

	// Fresh execution. Healthy adds the done label BEFORE the effects
	// record completes (bughunt D7): Complete is the dedupe gate — the
	// reused-receipt path NEVER re-runs anything — so completing first
	// made a failed label call permanent while the old comment claimed
	// "the next poll re-adds the label harmlessly". The label call is
	// idempotent upstream, so a retry after a label failure (record left
	// receipted, not completed) is safe: the comment is NOT re-posted
	// because the effects Run short-circuits only on COMPLETED priors —
	// a receipted prior is refused as owned-by-another... which means
	// the poller's own next attempt must be the completing one. Leave
	// the record receipted on label failure: Complete below runs only
	// after the label succeeded (or the state is non-healthy, where no
	// label is part of the effect).
	if o.State == "healthy" {
		if _, _, apiErr := client.Issues.AddLabelsToIssue(ctx, w.repo.Owner, w.repo.Repo, num, []string{w.doneLabel}); apiErr != nil {
			// The comment landed and its receipt is stored (receipted,
			// not completed). Surface the label failure WITHOUT
			// completing: the next poll with the same outcome retries
			// the label (and only the label — the receipted prior
			// refuses re-execution, and the reconcile path can adopt
			// + complete it once the label lands).
			err := fmt.Errorf("intake: WriteBack.Report: done label %q on %s#%s: %w", w.doneLabel, t.Repo, t.ID, apiErr)
			if attemptErr != nil {
				return rc.ID, rc.HTMLURL, errors.Join(attemptErr, err)
			}
			return rc.ID, rc.HTMLURL, err
		}
	}

	// Complete the effect so the next poll with the same outcome collides
	// on the completed key and reuses this receipt (storm guard). Runs
	// AFTER the label (bughunt D7): completion is the dedupe gate, so
	// everything inside the effect's logical scope must succeed first.
	if err := w.ledger.Complete(ctx, key, who); err != nil {
		return rc.ID, rc.HTMLURL, fmt.Errorf("intake: WriteBack.Report: complete %s: %w", key, err)
	}

	if attemptErr != nil {
		// 4xx recorded-outcome: receipt stored + completed; surface the
		// provider rejection to the caller.
		return rc.ID, rc.HTMLURL, attemptErr
	}
	return rc.ID, rc.HTMLURL, nil
}

// retryLabelOnly adopts a RECEIPTED prior written by the same claimant
// (the D7 label-failure window: the comment landed and its receipt was
// recorded, but the done label failed and the record was deliberately
// left uncompleted so the failure stays retryable). The comment is NOT
// re-posted — the stored receipt is returned verbatim; ONLY the label
// re-runs (idempotent upstream), after which the caller completes the
// record. On a second label failure the error wraps the receipted-prior
// refusal so the record stays receipted for the next poll.
func (w *WriteBack) retryLabelOnly(ctx context.Context, client *github.Client, num int, o Outcome, key string, who effects.Claimant, priorReceipt json.RawMessage) (json.RawMessage, bool, error) {
	if o.State == "healthy" {
		if _, _, apiErr := client.Issues.AddLabelsToIssue(ctx, w.repo.Owner, w.repo.Repo, num, []string{w.doneLabel}); apiErr != nil {
			return nil, false, fmt.Errorf("intake: writeback: retry done label %q on %s: %w", w.doneLabel, key, apiErr)
		}
	}
	return priorReceipt, false, nil
}
