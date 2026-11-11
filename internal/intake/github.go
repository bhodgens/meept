package intake

// GitHubPoller is the first Poller implementation: a label-gated GitHub
// Issues watch on the #64 githubsvc client pool. It NEVER imports
// internal/secrets — the pool resolves tokens itself.
//
// v1 trade: one page of PerPage=50 issues per poll; a repo with more
// than 50 updated-since matches loses rows (same accepted paging cap as
// the #64 workflow queries).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/caimlas/meept/internal/githubsvc"
)

// githubPollerPageSize is the single-page cap for list-issues calls.
const githubPollerPageSize = 50

// GitHubPoller lists repo issues updated since its watermark, gated by
// the intake Filter, and maps them to Tickets. The watermark is
// persisted in-memory on the struct; daemon restarts re-scan once —
// acceptable for v1, the dedup layer makes it a no-op.
//
// bughunt D8: overlapping Poll calls used to race the unsynchronized
// watermark (the cron fires each tick on a fresh goroutine) AND duplicate
// the API paging. In-flight now single-flights: a concurrent Poll
// returns (nil, ErrPollInProgress) instead of paging the same window a
// second time; the watermark read/write is guarded by the same mutex.
type GitHubPoller struct {
	pool   *githubsvc.ClientPool
	repo   githubsvc.OwnerRepo
	filter Filter
	logger *slog.Logger

	// pollMu guards watermark/hasWatermark AND the single-flight flag:
	// the mutex scope is memory-only (watermark bookkeeping), never held
	// across the HTTP call (mutexio rule) — the in-flight flag, checked
	// + set under the lock and cleared after the page, is what
	// serializes polls.
	pollMu       sync.Mutex
	watermark    time.Time
	hasWatermark bool
	pollRunning  bool
}

// ErrPollInProgress is returned to a Poll call that overlaps an
// in-flight one (single-flight skip). The cron's fresh-goroutine-per-tick
// model treats it as "nothing new this tick" — the NEXT tick re-reads
// the watermark the in-flight poll will have advanced.
var ErrPollInProgress = errors.New("intake: poll already in flight; skipped")

// NewGitHubPoller builds a GitHub Issues poller for the given repo.
// The logger parameter takes a Handler (the committed gate tests pass
// slog.DiscardHandler directly); it is wrapped in a *slog.Logger
// internally. Nil means discard.
func NewGitHubPoller(pool *githubsvc.ClientPool, repo githubsvc.OwnerRepo,
	filter Filter, logger slog.Handler) *GitHubPoller {
	if logger == nil {
		logger = slog.DiscardHandler
	}
	return &GitHubPoller{
		pool:   pool,
		repo:   repo,
		filter: filter,
		logger: slog.New(logger),
	}
}

// Poll implements Poller. It lists repo issues updated since the
// watermark (state=all, sorted=updated, direction=desc), passes each
// through the label gate, and maps survivors to Tickets. PR entries
// are skipped — the issues API returns PRs too and intake tickets are
// issues only. The watermark advances to the max UpdatedAt seen across
// a successful poll. Overlapping calls (bughunt D8): a Poll that
// overlaps an in-flight one returns ErrPollInProgress without paging.
func (p *GitHubPoller) Poll(ctx context.Context) ([]Ticket, error) {
	// Single-flight gate + watermark snapshot, one critical section.
	// The mutex is released BEFORE any I/O (mutexio rule); the in-flight
	// flag makes overlapping ticks no-ops instead of duplicate pages.
	p.pollMu.Lock()
	if p.pollRunning {
		p.pollMu.Unlock()
		return nil, ErrPollInProgress
	}
	p.pollRunning = true
	watermark := p.watermark
	hasWatermark := p.hasWatermark
	p.pollMu.Unlock()

	defer func() {
		p.pollMu.Lock()
		p.pollRunning = false
		p.pollMu.Unlock()
	}()

	client, err := p.pool.For(ctx, p.repo.Owner, p.repo.Repo)
	if err != nil {
		return nil, fmt.Errorf("intake: client pool: %w", err)
	}

	opts := &github.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			Page:    1,
			PerPage: githubPollerPageSize,
		},
	}
	if hasWatermark {
		opts.Since = watermark
	}

	issues, resp, err := client.Issues.ListByRepo(ctx, p.repo.Owner, p.repo.Repo, opts)
	if err != nil {
		return nil, fmt.Errorf("intake: list issues %s/%s: %w", p.repo.Owner, p.repo.Repo, err)
	}
	defer func() { _ = resp.Body.Close() }()

	tickets := make([]Ticket, 0, len(issues))
	var maxUpdated time.Time
	sawAny := false
	for _, issue := range issues {
		if issue == nil || issue.IsPullRequest() {
			continue
		}
		labels := make([]string, 0, len(issue.Labels))
		for _, l := range issue.Labels {
			labels = append(labels, l.GetName())
		}
		if !p.filter.Matches(labels) {
			continue
		}
		updatedAt := issue.GetUpdatedAt().Time
		// Client-side watermark enforcement on top of the API `since`
		// param: go-querystring truncates Since to whole seconds, so a
		// sub-second watermark could otherwise re-surface an issue whose
		// UpdatedAt sits inside the same second. Issue 1 of the fixture
		// sits exactly ON the watermark (t0) and must not re-surface.
		if hasWatermark && !updatedAt.After(watermark) {
			continue
		}
		if !sawAny || updatedAt.After(maxUpdated) {
			maxUpdated = updatedAt
			sawAny = true
		}
		tickets = append(tickets, Ticket{
			ID:        strconv.Itoa(issue.GetNumber()),
			Repo:      p.repo.Owner + "/" + p.repo.Repo,
			Title:     issue.GetTitle(),
			Body:      issue.GetBody(),
			Labels:    labels,
			URL:       issue.GetHTMLURL(),
			Author:    issue.GetUser().GetLogin(),
			UpdatedAt: updatedAt,
		})
	}

	if sawAny {
		p.pollMu.Lock()
		// Monotonic guard: a skipped overlapping poll can never rewind a
		// watermark a concurrent (post-single-flight) writer advanced.
		if !p.hasWatermark || maxUpdated.After(p.watermark) {
			p.watermark = maxUpdated
			p.hasWatermark = true
		}
		p.pollMu.Unlock()
	}
	p.logger.Debug("intake: github poll",
		slog.String("repo", p.repo.Owner+"/"+p.repo.Repo),
		slog.Int("issues", len(issues)),
		slog.Int("tickets", len(tickets)),
	)
	return tickets, nil
}
