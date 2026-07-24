package benchmark

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ExecSolver runs an external command to solve benchmark instances.
// This mirrors Aider's harness design: the benchmark runner is separate
// from the agent, invoking it as a subprocess in the problem repo.
//
// The command template supports these placeholders:
//
//	{problem}  — the problem statement (passed as a single argument)
//	{repo_dir} — the repository working directory
//
// If no placeholders are present, the problem statement is appended as
// the final argument and the command runs with Dir set to repo_dir.
type ExecSolver struct {
	// Command is the shell command to run (e.g., "meept chat --oneshot").
	Command string
	// Timeout per instance. Zero means use the runner's timeout.
	Timeout time.Duration
	// Env is additional environment variables for the subprocess.
	Env []string
}

// Solve runs the configured command against the problem instance.
func (s *ExecSolver) Solve(ctx context.Context, repoDir string, inst Instance) (string, error) {
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	args := s.buildArgs(repoDir, inst)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), s.Env...)

	// Capture combined output for debugging.
	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(output)
		if len(outStr) > 3000 {
			outStr = outStr[:3000] + "...(truncated)"
		}
		return "", fmt.Errorf("solver command failed: %w\noutput: %s", err, outStr)
	}

	// After the solver runs, extract the diff.
	diff, err := GetDiff(repoDir)
	if err != nil {
		return "", fmt.Errorf("get diff after solve: %w", err)
	}

	return diff, nil
}

// buildArgs constructs the command arguments.
func (s *ExecSolver) buildArgs(repoDir string, inst Instance) []string {
	parts := strings.Fields(s.Command)
	if len(parts) == 0 {
		return []string{"meept", "chat", "--oneshot", inst.ProblemStatement}
	}

	// Check for placeholders.
	hasPlaceholder := false
	result := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		switch {
		case strings.Contains(p, "{problem}"):
			result = append(result, strings.ReplaceAll(p, "{problem}", inst.ProblemStatement))
			hasPlaceholder = true
		case strings.Contains(p, "{repo_dir}"):
			result = append(result, strings.ReplaceAll(p, "{repo_dir}", repoDir))
			hasPlaceholder = true
		default:
			result = append(result, p)
		}
	}

	// If no placeholders, append problem statement as final arg.
	if !hasPlaceholder {
		result = append(result, inst.ProblemStatement)
	}

	return result
}

// PromptSolver wraps a prompt template around the problem statement.
// Useful for injecting SWE-bench-style instructions before the issue text.
type PromptSolver struct {
	Inner   Solver
	Prompt  string // Template with {problem} placeholder.
	Prefix  string // Static prefix prepended to the problem statement.
}

// Solve delegates to the inner solver after transforming the problem statement.
func (s *PromptSolver) Solve(ctx context.Context, repoDir string, inst Instance) (string, error) {
	modified := inst
	if s.Prompt != "" {
		modified.ProblemStatement = strings.ReplaceAll(s.Prompt, "{problem}", inst.ProblemStatement)
	} else if s.Prefix != "" {
		modified.ProblemStatement = s.Prefix + "\n\n" + inst.ProblemStatement
	}
	return s.Inner.Solve(ctx, repoDir, modified)
}
