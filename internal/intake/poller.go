package intake

// Package intake is the provider-agnostic ticket-intake seam for the
// github-intake-factory tree (#67): anything that can list "tickets
// changed since last time" can feed the ticket->goal factory.
//
// This file implements Contract 1 of the tree (docs/plans/
// github-intake-factory/master.md): the Ticket unit, the label-gate
// Filter, and the Poller interface. The first Poller implementation
// (GitHubPoller, on the githubsvc client pool) lives in github.go.

import (
	"context"
	"strings"
	"time"
)

// Ticket is the provider-agnostic intake unit.
type Ticket struct {
	ID        string // provider ticket id (GitHub issue number as string)
	Repo      string // "owner/repo"
	Title     string
	Body      string // becomes the goal mandate
	Labels    []string
	URL       string
	Author    string
	UpdatedAt time.Time
}

// Poller produces tickets matching the intake filter. Implementations:
// GitHubPoller (this tree); Jira/Linear are follow-ons — this interface
// is the seam.
type Poller interface {
	// Poll returns new-or-changed tickets since the last poll watermark.
	// Implementations track their own watermark; a first poll returns
	// all currently-matching tickets.
	Poll(ctx context.Context) ([]Ticket, error)
}

// Filter is the label gate: only tickets carrying ALL required labels
// (case-insensitive) are returned.
type Filter struct {
	RequiredLabels []string // e.g. {"factory"}; empty = no gate
}

// Matches reports whether the given label set satisfies the filter.
// An empty filter matches everything; every RequiredLabels entry must
// be present in labels, compared case-insensitively.
func (f Filter) Matches(labels []string) bool {
	if len(f.RequiredLabels) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		have[strings.ToLower(l)] = struct{}{}
	}
	for _, want := range f.RequiredLabels {
		if _, ok := have[strings.ToLower(want)]; !ok {
			return false
		}
	}
	return true
}
