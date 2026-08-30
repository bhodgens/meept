package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ErrJudgmentNotFound is returned by DiskStore.LoadJudgment when no
// judgment exists for the given trajectory ID. Match with errors.Is.
var ErrJudgmentNotFound = errors.New("eval: judgment not found")

// failureMarkers are content prefixes/snippets that indicate a tool result
// represents a failed execution. IsToolError is the primary signal; these
// markers cover conversations serialized before the flag existed or rebuilt
// from wire formats that dropped it.
var failureMarkers = []string{
	"error:",
	"command failed",
	"exit status",
	"exit code",
	"traceback (most recent call last)",
}

// Step is the minimal per-tool shape the judge reasons over. Derived from
// tool-role messages; assistant/user rows are skipped.
type Step struct {
	Name   string `json:"name"`
	Err    string `json:"err,omitempty"`
	Failed bool   `json:"failed"`
}

// TrajectoryJudgment is the C7 contract: the verdict for one stored
// trajectory. FirstErrorStep is 0 when passed; 1-based when failed. When the
// oracle fails but no tool step failed, it is len(steps)+1 (outcome-only
// fail; the sentinel points past the tools at the final outcome).
type TrajectoryJudgment struct {
	TrajectoryID   string `json:"trajectory_id"`
	Passed         bool   `json:"passed"`
	FirstErrorStep int    `json:"first_error_step"` // 0 if passed; 1-based if failed
	Summary        string `json:"summary"`
	OracleName     string `json:"oracle_name"`
}

// StepsFromMessages derives judge steps from a conversation: tool-role
// messages become Steps in order; assistant and user rows are skipped. A
// step fails when IsToolError is set or the result content carries a
// recognizable failure marker. Unknown callers pass Name in ChatMessage.Name.
func StepsFromMessages(msgs []llm.ChatMessage) []Step {
	var steps []Step
	for _, m := range msgs {
		if m.Role != llm.RoleTool {
			continue
		}
		name := m.Name
		if name == "" {
			name = "tool"
		}
		step := Step{Name: name, Err: m.Content}
		switch {
		case m.IsToolError:
			step.Failed = true
		case contentLooksFailed(m.Content):
			step.Failed = true
		default:
			// Success: keep the raw content out of Err — it is a result,
			// not an error.
			step.Err = ""
		}
		steps = append(steps, step)
	}
	return steps
}

// contentLooksFailed reports whether tool-result content carries a failure
// marker. Line-leading prefixes only, case-insensitive, so prose mentioning
// "error" mid-sentence does not false-positive.
func contentLooksFailed(content string) bool {
	c := strings.ToLower(content)
	for _, marker := range failureMarkers {
		if strings.HasPrefix(c, marker) {
			return true
		}
	}
	// Multi-line results: any line may be the failure line.
	for _, line := range strings.Split(c, "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range failureMarkers {
			if strings.HasPrefix(line, marker) {
				return true
			}
		}
	}
	return false
}

// Judge evaluates a trajectory's tool steps plus an oracle check against the
// workdir, producing the C7 verdict. No LLM involvement: the judgment is
// derived purely from structured step data (exit/error status) and the
// oracle.
//
// FirstErrorStep semantics (1-based):
//   - first failing tool step's index, when any tool failed (regardless of
//     oracle outcome — the tool failure is the earlier signal)
//   - len(steps)+1 when the oracle failed but all tools succeeded
//     (outcome-only fail)
//   - 0 when the trajectory passed
//
// Oracle launch errors are not fatal: they fail the judgment closed with
// FirstErrorStep at the outcome sentinel, so a broken oracle can never mark
// a trajectory passed.
func Judge(ctx context.Context, steps []Step, oracle Oracle, workdir string) (TrajectoryJudgment, error) {
	if oracle == nil {
		return TrajectoryJudgment{}, errors.New("eval: judge requires an oracle")
	}
	if err := ctx.Err(); err != nil {
		return TrajectoryJudgment{}, fmt.Errorf("eval: judge: %w", err)
	}

	j := TrajectoryJudgment{OracleName: oracle.Name()}

	firstFailed := 0
	var firstFailedErr string
	for i, s := range steps {
		if s.Failed {
			firstFailed = i + 1
			firstFailedErr = s.Err
			break
		}
	}

	res, err := oracle.Check(ctx, workdir)
	if err != nil {
		// Fail closed, same shape as a failed oracle check.
		res = OracleResult{Passed: false, Err: err.Error()}
	}
	j.OracleName = oracle.Name()
	j.Passed = res.Passed && firstFailed == 0

	switch {
	case j.Passed:
		j.FirstErrorStep = 0
		j.Summary = fmt.Sprintf("passed: %d tool steps, oracle %q passed", len(steps), j.OracleName)
	case firstFailed > 0:
		j.FirstErrorStep = firstFailed
		j.Summary = fmt.Sprintf("step %d (%s) failed: %s", firstFailed, steps[firstFailed-1].Name, truncSummary(firstFailedErr))
	default:
		j.FirstErrorStep = len(steps) + 1
		j.Summary = fmt.Sprintf("oracle %q failed after %d clean tool steps: %s", j.OracleName, len(steps), truncSummary(res.Output))
	}
	return j, nil
}

// truncSummary bounds error text embedded in judgment summaries.
func truncSummary(s string) string {
	const max = 300
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// JudgedPassed reports whether the judgment marks its trajectory as passed.
// Satisfies the judgment-source constraint of selfimprove.OnlyJudged, which
// cannot import this package (eval → agent → selfimprove dependency chain).
func (j TrajectoryJudgment) JudgedPassed() bool {
	return j.Passed
}

// SaveJudgment persists a judgment as JSON at <Dir>/<trajectory_id>.judgment.json,
// beside the run record of the same ID. Overwrites an existing judgment with
// the same ID. The ID must be a safe path segment.
func (s *DiskStore) SaveJudgment(ctx context.Context, j TrajectoryJudgment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.TrajectoryID == "" {
		return errors.New("eval: judgment trajectory_id is required")
	}
	path, err := s.judgmentPath(j.TrajectoryID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("eval: create store dir: %w", err)
	}
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: encode judgment: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("eval: write judgment: %w", err)
	}
	return nil
}

// LoadJudgment reads the judgment stored for a trajectory ID. Unknown IDs
// return ErrJudgmentNotFound.
func (s *DiskStore) LoadJudgment(ctx context.Context, trajectoryID string) (TrajectoryJudgment, error) {
	if err := ctx.Err(); err != nil {
		return TrajectoryJudgment{}, err
	}
	path, err := s.judgmentPath(trajectoryID)
	if err != nil {
		return TrajectoryJudgment{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TrajectoryJudgment{}, fmt.Errorf("%w: %s", ErrJudgmentNotFound, trajectoryID)
		}
		return TrajectoryJudgment{}, fmt.Errorf("eval: read judgment: %w", err)
	}
	var j TrajectoryJudgment
	if err := json.Unmarshal(b, &j); err != nil {
		return TrajectoryJudgment{}, fmt.Errorf("eval: parse judgment %s: %w", trajectoryID, err)
	}
	return j, nil
}

// judgmentPath returns the file path for a trajectory judgment, refusing
// anything that smells like path traversal regardless of where the ID came
// from. Mirrors DiskStore.recordPath.
func (s *DiskStore) judgmentPath(trajectoryID string) (string, error) {
	if trajectoryID == "" || trajectoryID == "." || trajectoryID == ".." ||
		strings.Contains(trajectoryID, "/") ||
		strings.Contains(trajectoryID, "\\") ||
		strings.Contains(trajectoryID, string(filepath.Separator)) {
		return "", fmt.Errorf("eval: invalid trajectory id %q", trajectoryID)
	}
	return filepath.Join(s.Dir, trajectoryID+".judgment.json"), nil
}
