package benchmark

import (
	"context"
	"os"
	"testing"
	"time"
)

// mockSolver returns a fixed diff for testing.
type mockSolver struct {
	diff string
	err  error
}

func (m *mockSolver) Solve(ctx context.Context, repoDir string, inst Instance) (string, error) {
	return m.diff, m.err
}

func TestRunner_Run_EmptyInstances(t *testing.T) {
	solver := &mockSolver{diff: "some diff"}
	runner := NewRunner(solver, t.TempDir())

	report, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Total != 0 {
		t.Errorf("expected 0 total, got %d", report.Total)
	}
	if report.Score != 0 {
		t.Errorf("expected 0 score, got %f", report.Score)
	}
}

func TestRunner_Run_ContextCancelled(t *testing.T) {
	solver := &mockSolver{diff: "diff"}
	runner := NewRunner(solver, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	instances := []Instance{
		{ID: "test-1", Repo: "foo/bar", BaseCommit: "abc", ProblemStatement: "fix it"},
	}

	report, err := runner.Run(ctx, instances)
	if err == nil {
		t.Fatal("expected context error")
	}
	if report.Total != 1 {
		t.Errorf("expected total=1, got %d", report.Total)
	}
}

func TestExecSolver_BuildArgs_NoPlaceholder(t *testing.T) {
	s := &ExecSolver{Command: "meept chat --oneshot"}
	args := s.buildArgs("/tmp/repo", Instance{ProblemStatement: "fix the bug"})

	expected := []string{"meept", "chat", "--oneshot", "fix the bug"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, a := range expected {
		if args[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, args[i])
		}
	}
}

func TestExecSolver_BuildArgs_WithPlaceholder(t *testing.T) {
	s := &ExecSolver{Command: "echo {problem}"}
	args := s.buildArgs("/tmp/repo", Instance{ProblemStatement: "hello world"})

	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[1] != "hello world" {
		t.Errorf("expected 'hello world', got %q", args[1])
	}
}

func TestPromptSolver(t *testing.T) {
	inner := &mockSolver{diff: "result"}
	ps := &PromptSolver{
		Inner:  inner,
		Prefix: "You are a code fixer.",
	}

	diff, err := ps.Solve(context.Background(), "/tmp", Instance{ProblemStatement: "fix bug"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff != "result" {
		t.Errorf("expected 'result', got %q", diff)
	}
}

func TestLoadInstances_InvalidJSON(t *testing.T) {
	path := t.TempDir() + "/bad.json"
	if err := writeTestFile(path, "not json"); err != nil {
		t.Fatal(err)
	}

	_, err := LoadInstances(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadInstances_MissingFields(t *testing.T) {
	path := t.TempDir() + "/missing.json"
	data := `[{"id": "x", "repo": "a/b"}]`
	if err := writeTestFile(path, data); err != nil {
		t.Fatal(err)
	}

	_, err := LoadInstances(path)
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestLoadInstances_Valid(t *testing.T) {
	path := t.TempDir() + "/valid.json"
	data := `[{"id":"x","repo":"a/b","base_commit":"abc","problem_statement":"fix"}]`
	if err := writeTestFile(path, data); err != nil {
		t.Fatal(err)
	}

	instances, err := LoadInstances(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	if instances[0].ID != "x" {
		t.Errorf("expected id 'x', got %q", instances[0].ID)
	}
}

func TestReport_Scoring(t *testing.T) {
	report := &Report{
		Total:     4,
		Resolved:  2,
		Plausible: 1,
		Failed:    1,
	}
	// Score is computed by the runner, but verify the formula.
	score := float64(report.Resolved) / float64(report.Total) * 100
	if score != 50.0 {
		t.Errorf("expected 50.0, got %f", score)
	}
}

func TestRunner_Timeout(t *testing.T) {
	// Verify WithTimeout option is applied.
	solver := &mockSolver{diff: "x"}
	runner := NewRunner(solver, t.TempDir(), WithTimeout(30*time.Second))
	if runner.timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", runner.timeout)
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
