// Package benchmark provides SWE-bench-style regression testing for meept's
// agent harness. It clones problem repositories at specific commits, feeds
// issue descriptions to the agent, and scores whether the agent produces a
// plausible patch (edits apply cleanly, pre-existing tests still pass).
package benchmark

import (
	"context"
	"time"
)

// Instance represents a single SWE-bench-style problem.
type Instance struct {
	// ID is a unique identifier (e.g., "django__django-11333").
	ID string `json:"id"`
	// Repo is the GitHub repository path (e.g., "django/django").
	Repo string `json:"repo"`
	// BaseCommit is the commit to checkout before solving.
	BaseCommit string `json:"base_commit"`
	// ProblemStatement is the issue description fed to the agent.
	ProblemStatement string `json:"problem_statement"`
	// TestCmd is the command to run pre-existing tests (e.g., "python -m pytest tests/").
	// If empty, the instance is scored on edit-only plausibility.
	TestCmd string `json:"test_cmd,omitempty"`
	// Language is the primary language of the repo (for reporting).
	Language string `json:"language,omitempty"`
}

// ResultStatus represents the outcome of a benchmark instance.
type ResultStatus string

const (
	// StatusResolved means the agent produced a plausible patch that passes tests.
	StatusResolved ResultStatus = "resolved"
	// StatusPlausible means the agent edited files cleanly but tests were not run or inconclusive.
	StatusPlausible ResultStatus = "plausible"
	// StatusFailed means the agent did not produce a usable patch.
	StatusFailed ResultStatus = "failed"
	// StatusError means the harness itself errored (checkout failure, timeout, etc.).
	StatusError ResultStatus = "error"
)

// Result holds the outcome of running one benchmark instance.
type Result struct {
	InstanceID string        `json:"instance_id"`
	Status     ResultStatus  `json:"status"`
	Diff       string        `json:"diff,omitempty"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	// EditOutcome is true if the agent reported successful edits.
	EditOutcome bool `json:"edit_outcome"`
	// TestOutcome is true if pre-existing tests passed after the edit.
	TestOutcome bool `json:"test_outcome"`
}

// Report summarizes a benchmark run.
type Report struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Total       int       `json:"total"`
	Resolved    int       `json:"resolved"`
	Plausible   int       `json:"plausible"`
	Failed      int       `json:"failed"`
	Errors      int       `json:"errors"`
	// Score is Resolved/Total as a percentage (0-100).
	Score   float64  `json:"score"`
	Results []Result `json:"results"`
}

// Solver is the interface for anything that can attempt to solve a benchmark
// instance. The real implementation wraps meept's AgentLoop; tests use a mock.
type Solver interface {
	// Solve attempts to resolve the problem statement in the given repo directory.
	// It returns the unified diff of changes made (empty string if no changes).
	Solve(ctx context.Context, repoDir string, instance Instance) (diff string, err error)
}
