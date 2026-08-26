package employee

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/runtime"
)

// stubBackend is a scripted runtime.ExecutionBackend for gate tests.
type stubBackend struct {
	results []*runtime.CommandResult
	errs    []error
	calls   []string
}

func (s *stubBackend) Execute(_ context.Context, cmd runtime.Command) (*runtime.CommandResult, error) {
	i := len(s.calls)
	s.calls = append(s.calls, cmd.Cmd)
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i < len(s.results) {
		return s.results[i], nil
	}
	return &runtime.CommandResult{}, nil
}

func (s *stubBackend) Name() string { return "stub" }
func (s *stubBackend) Close() error { return nil }

func TestGate_PassFailMatrix(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		output     string
		wantPassed bool
	}{
		{"pass", 0, "all good\n", true},
		{"fail", 1, "FAIL: tests broke\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			be := &stubBackend{results: []*runtime.CommandResult{
				{Output: "", ExitCode: 0},                  // git status
				{Output: "abc123\n", ExitCode: 0},          // HEAD
				{Output: tc.output, ExitCode: tc.exitCode}, // gate
			}}
			res, state, err := RunGate(context.Background(), GateConfig{Command: "make check"}, be, "/ws", nil)
			if err != nil {
				t.Fatalf("RunGate error: %v", err)
			}
			if res.Passed != tc.wantPassed {
				t.Errorf("passed = %v, want %v", res.Passed, tc.wantPassed)
			}
			if res.Skipped {
				t.Error("unexpected skip")
			}
			if res.Output != tc.output {
				t.Errorf("output = %q, want %q", res.Output, tc.output)
			}
			if !tc.wantPassed && state.LastFailedOutput == "" {
				t.Error("failed run must record LastFailedOutput")
			}
			if tc.wantPassed && state.LastFailedOutput != "" {
				t.Error("passing run must not record LastFailedOutput")
			}
			if state.WorkspaceHash == "" {
				t.Error("workspace hash must be non-empty")
			}
			if len(be.calls) != 3 || be.calls[2] != "make check" {
				t.Errorf("calls = %v; want hash cmds + gate command", be.calls)
			}
		})
	}
}

// TestGate_HashStabilityAcrossNoOpGitState verifies the workspace hash is
// stable across no-op rounds (same porcelain output + same HEAD).
func TestGate_HashStabilityAcrossNoOpGitState(t *testing.T) {
	mk := func() runtime.ExecutionBackend {
		return &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "deadbeef\n", ExitCode: 0},
			{Output: "ok\n", ExitCode: 0},
		}}
	}
	_, s1, err := RunGate(context.Background(), GateConfig{Command: "true"}, mk(), "/ws", nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	_, s2, err := RunGate(context.Background(), GateConfig{Command: "true"}, mk(), "/ws", nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if s1.WorkspaceHash != s2.WorkspaceHash {
		t.Errorf("hash changed across no-op round: %q vs %q", s1.WorkspaceHash, s2.WorkspaceHash)
	}
}

// TestGate_SkipOnlyWhenPrevFailureAndUnchanged verifies the skip matrix.
func TestGate_SkipOnlyWhenPrevFailureAndUnchanged(t *testing.T) {
	// Derive a realistic prev failure state by running a failing gate once.
	hashBE := func() *stubBackend {
		return &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "", ExitCode: 0},
			{Output: "boom\n", ExitCode: 1},
		}}
	}
	_, prevState, err := RunGate(context.Background(), GateConfig{Command: "make check"}, hashBE(), "/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prevState.LastFailedOutput == "" {
		t.Fatal("setup: expected failed state")
	}

	gateCalls := func(be *stubBackend) int {
		n := 0
		for _, c := range be.calls {
			if c == "make check" {
				n++
			}
		}
		return n
	}

	t.Run("skips on prev failure + unchanged", func(t *testing.T) {
		be := &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0}, // git status
			{Output: "", ExitCode: 0}, // HEAD
		}}
		prev := &GateState{WorkspaceHash: prevState.WorkspaceHash, LastFailedOutput: "boom"}
		res, _, err := RunGate(context.Background(), GateConfig{Command: "make check", SkipWhenUnchanged: true}, be, "/ws", prev)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Skipped || res.Passed {
			t.Errorf("want skipped result, got %+v", res)
		}
		if res.Reason == "" {
			t.Error("skip must carry a reason")
		}
		if gateCalls(be) != 0 {
			t.Error("gate command must not run when skipped")
		}
	})

	t.Run("runs when workspace changed despite prev failure", func(t *testing.T) {
		be := &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "", ExitCode: 0},
			{Output: "ok\n", ExitCode: 0},
		}}
		prev := &GateState{WorkspaceHash: "different-hash", LastFailedOutput: "boom"}
		res, _, err := RunGate(context.Background(), GateConfig{Command: "make check", SkipWhenUnchanged: true}, be, "/ws", prev)
		if err != nil {
			t.Fatal(err)
		}
		if res.Skipped {
			t.Error("must re-run when workspace changed")
		}
		if gateCalls(be) != 1 {
			t.Errorf("gate ran %d times, want 1", gateCalls(be))
		}
	})

	t.Run("runs when prev passed even if unchanged", func(t *testing.T) {
		be := &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "", ExitCode: 0},
			{Output: "ok\n", ExitCode: 0},
		}}
		prev := &GateState{WorkspaceHash: prevState.WorkspaceHash + "-x", LastFailedOutput: ""}
		res, _, err := RunGate(context.Background(), GateConfig{Command: "make check", SkipWhenUnchanged: true}, be, "/ws", prev)
		if err != nil {
			t.Fatal(err)
		}
		if res.Skipped {
			t.Error("skip only applies to previous FAILURES, not passes")
		}
	})

	t.Run("runs when SkipWhenUnchanged disabled", func(t *testing.T) {
		be := &stubBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},
			{Output: "", ExitCode: 0},
			{Output: "ok\n", ExitCode: 0},
		}}
		prev := &GateState{WorkspaceHash: prevState.WorkspaceHash, LastFailedOutput: "boom"}
		res, _, err := RunGate(context.Background(), GateConfig{Command: "make check"}, be, "/ws", prev)
		if err != nil {
			t.Fatal(err)
		}
		if res.Skipped {
			t.Error("must not skip when SkipWhenUnchanged=false")
		}
	})
}

// TestGate_TimeoutKillsCommand uses a real local backend and a sleeping
// shell command to verify the timeout actually kills the process.
func TestGate_TimeoutKillsCommand(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git unavailable or init failed: %v", err)
	}

	cfg := GateConfig{
		Command:        "sleep 30",
		TimeoutSeconds: 1,
	}
	start := time.Now()
	res, _, err := RunGate(context.Background(), cfg, nil, dir, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Logf("RunGate returned without error (result=%+v)", res)
	}
	if elapsed > 10*time.Second {
		t.Errorf("timeout did not kill the command; took %s", elapsed)
	}
	if elapsed < time.Second {
		t.Errorf("command finished before the timeout could fire (%s)", elapsed)
	}
}

// TestGate_RealLocalPassFail exercises RunGate end-to-end against a real
// local backend and real git repository.
func TestGate_RealLocalPassFail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, cmdStr := range []string{"git init -q .", "git config user.email t@t", "git config user.name t"} {
		c := exec.Command("sh", "-c", cmdStr)
		c.Dir = dir
		if err := c.Run(); err != nil {
			t.Logf("setup %q: %v", cmdStr, err)
		}
	}

	be := runtime.NewLocalBackend(runtime.Config{}, os.Environ())
	defer be.Close()

	t.Run("passing script completes goal eligibility", func(t *testing.T) {
		script := filepath.Join(dir, "gate_ok.sh")
		os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
		cfg := GateConfig{Command: "./gate_ok.sh"}
		res, state, err := RunGate(context.Background(), cfg, be, dir, nil)
		if err != nil {
			t.Fatalf("RunGate: %v", err)
		}
		if !res.Passed || res.Skipped {
			t.Errorf("want pass, got %+v", res)
		}
		if state.LastFailedOutput != "" {
			t.Error("no failure output expected on pass")
		}
		if len(state.WorkspaceHash) != 64 {
			t.Errorf("hash length = %d, want 64 (sha256 hex)", len(state.WorkspaceHash))
		}
	})

	t.Run("failing script records output and skips when unchanged", func(t *testing.T) {
		script := filepath.Join(dir, "gate_bad.sh")
		os.WriteFile(script, []byte("#!/bin/sh\necho 'compile error'\nexit 1\n"), 0o755)
		cfg := GateConfig{Command: "./gate_bad.sh", SkipWhenUnchanged: true}

		res1, state1, err := RunGate(context.Background(), cfg, be, dir, nil)
		if err != nil {
			t.Fatalf("run 1: %v", err)
		}
		if res1.Passed {
			t.Fatal("expected failure")
		}
		if !strings.Contains(state1.LastFailedOutput, "compile error") {
			t.Errorf("LastFailedOutput missing output: %q", state1.LastFailedOutput)
		}

		res2, _, err := RunGate(context.Background(), cfg, be, dir, &state1)
		if err != nil {
			t.Fatalf("run 2: %v", err)
		}
		if !res2.Skipped {
			t.Errorf("second run should skip unchanged workspace; got %+v", res2)
		}
		if !strings.Contains(res2.Reason, "unchanged") {
			t.Errorf("reason = %q", res2.Reason)
		}
	})
}
