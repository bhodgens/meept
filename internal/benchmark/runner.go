package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner executes benchmark instances against a Solver.
type Runner struct {
	solver   Solver
	cacheDir string // bare repo cache
	workDir  string // temp working directory for checkouts
	timeout  time.Duration
	logger   *slog.Logger
}

// RunnerOption configures the Runner.
type RunnerOption func(*Runner)

// WithTimeout sets the per-instance timeout.
func WithTimeout(d time.Duration) RunnerOption {
	return func(r *Runner) { r.timeout = d }
}

// WithCacheDir sets the bare-repo cache directory.
func WithCacheDir(dir string) RunnerOption {
	return func(r *Runner) { r.cacheDir = dir }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) RunnerOption {
	return func(r *Runner) {
		if l != nil {
			r.logger = l
		}
	}
}

// NewRunner creates a benchmark runner.
func NewRunner(solver Solver, workDir string, opts ...RunnerOption) *Runner {
	r := &Runner{
		solver:   solver,
		workDir:  workDir,
		cacheDir: filepath.Join(workDir, "repo-cache"),
		timeout:  10 * time.Minute,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	// Ensure working directory exists (MkdirTemp requires parent to exist).
	if err := os.MkdirAll(r.workDir, 0o755); err != nil {
		r.logger.Warn("failed to create work dir", "path", r.workDir, "error", err)
	}
	return r
}

// Run executes all instances and returns a report.
func (r *Runner) Run(ctx context.Context, instances []Instance) (*Report, error) {
	report := &Report{
		StartedAt: time.Now(),
		Total:     len(instances),
		Results:   make([]Result, 0, len(instances)),
	}

	for _, inst := range instances {
		select {
		case <-ctx.Done():
			report.CompletedAt = time.Now()
			return report, ctx.Err()
		default:
		}

		result := r.runInstance(ctx, inst)
		report.Results = append(report.Results, result)

		switch result.Status {
		case StatusResolved:
			report.Resolved++
		case StatusPlausible:
			report.Plausible++
		case StatusFailed:
			report.Failed++
		case StatusError:
			report.Errors++
		}

		r.logger.Info("instance complete",
			"id", inst.ID,
			"status", result.Status,
			"duration", result.Duration.Round(time.Millisecond),
		)
	}

	report.CompletedAt = time.Now()
	if report.Total > 0 {
		report.Score = float64(report.Resolved) / float64(report.Total) * 100
	}
	return report, nil
}

// runInstance handles a single benchmark instance end-to-end.
func (r *Runner) runInstance(ctx context.Context, inst Instance) Result {
	start := time.Now()
	result := Result{InstanceID: inst.ID}

	// Create instance timeout context.
	instCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Checkout the repo at the base commit.
	repoDir, err := r.checkout(instCtx, inst)
	if err != nil {
		result.Status = StatusError
		result.Error = fmt.Sprintf("checkout failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer os.RemoveAll(repoDir)

	// Run the solver.
	diff, err := r.solver.Solve(instCtx, repoDir, inst)
	if err != nil {
		result.Status = StatusFailed
		result.Error = fmt.Sprintf("solver failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	if diff == "" {
		result.Status = StatusFailed
		result.Error = "no changes produced"
		result.Duration = time.Since(start)
		return result
	}

	result.Diff = diff
	result.EditOutcome = true

	// Run pre-existing tests if a test command is specified.
	if inst.TestCmd != "" {
		testPassed, testErr := r.runTests(instCtx, repoDir, inst.TestCmd)
		result.TestOutcome = testPassed
		if testErr != nil {
			r.logger.Warn("test command failed",
				"id", inst.ID,
				"error", testErr,
			)
		}
		if testPassed {
			result.Status = StatusResolved
		} else {
			result.Status = StatusPlausible
		}
	} else {
		// No test command — plausible if edits applied cleanly.
		result.Status = StatusPlausible
	}

	result.Duration = time.Since(start)
	return result
}

// checkout clones (or uses cached bare repo) and checks out the base commit.
func (r *Runner) checkout(ctx context.Context, inst Instance) (string, error) {
	repoURL := "https://github.com/" + inst.Repo + ".git"
	repoName := strings.ReplaceAll(inst.Repo, "/", "_")
	barePath := filepath.Join(r.cacheDir, repoName+".git")

	// Clone bare repo if not cached.
	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		if err := os.MkdirAll(r.cacheDir, 0o755); err != nil {
			return "", fmt.Errorf("create cache dir: %w", err)
		}
		// Use -c fetch.fsck.badTimezone=ignore for repos with bad commit
		// timestamps (e.g., psf/requests — see psf/requests#2690).
		cmd := exec.CommandContext(ctx, "git", "-c", "fetch.fsck.badTimezone=ignore",
			"clone", "--bare", "--quiet", repoURL, barePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("bare clone: %w: %s", err, string(out))
		}
	}

	// Clone from bare cache into a temp working directory.
	workDir, err := os.MkdirTemp(r.workDir, "bench-"+repoName+"-*")
	if err != nil {
		return "", fmt.Errorf("create workdir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", barePath, workDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("clone from cache: %w: %s", err, string(out))
	}

	// Checkout the base commit.
	cmd = exec.CommandContext(ctx, "git", "-c", "advice.detachedHead=false",
		"checkout", "--quiet", inst.BaseCommit)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("checkout %s: %w: %s", inst.BaseCommit, err, string(out))
	}

	return workDir, nil
}

// runTests executes the test command in the repo directory.
func (r *Runner) runTests(ctx context.Context, repoDir, testCmd string) (bool, error) {
	// Split the test command into args.
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return true, nil
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "CI=true") // suppress interactive prompts

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Truncate output for logging.
		outStr := string(output)
		if len(outStr) > 2000 {
			outStr = outStr[:2000] + "...(truncated)"
		}
		return false, fmt.Errorf("tests failed: %w\n%s", err, outStr)
	}
	return true, nil
}

// GetDiff returns the git diff of the working directory against HEAD.
func GetDiff(repoDir string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
