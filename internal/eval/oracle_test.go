package eval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellOraclePassFail(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		wantPass  bool
		wantEmpty bool
	}{
		{name: "true passes", command: "true", wantPass: true, wantEmpty: true},
		{name: "false fails", command: "false", wantPass: false, wantEmpty: true},
		{name: "failing output captured", command: "echo boom >&2; exit 1", wantPass: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := ShellOracle{OracleName: "t", Command: tt.command, Timeout: 10 * time.Second}
			res, err := o.Check(context.Background(), t.TempDir())
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if res.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (output %q err %q)", res.Passed, tt.wantPass, res.Output, res.Err)
			}
			if tt.wantEmpty && res.Output != "" {
				t.Errorf("expected empty output, got %q", res.Output)
			}
			if !tt.wantPass && res.Err == "" {
				t.Error("expected Err set on failure")
			}
		})
	}
}

func TestShellOracleTimeout(t *testing.T) {
	o := ShellOracle{OracleName: "t", Command: "sleep 5", Timeout: 100 * time.Millisecond}
	start := time.Now()
	res, err := o.Check(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout did not kill process promptly: %v", elapsed)
	}
	if res.Passed {
		t.Error("timeout must not pass")
	}
	if !strings.Contains(res.Err, "timed out") {
		t.Errorf("Err should mention timeout, got %q", res.Err)
	}
}

func TestShellOracleWorkdir(t *testing.T) {
	dir := t.TempDir()
	o := ShellOracle{OracleName: "t", Command: "pwd", Timeout: 10 * time.Second}
	res, err := o.Check(context.Background(), dir)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if got := strings.TrimSpace(res.Output); got != dir {
		t.Errorf("pwd = %q, want %q", got, dir)
	}
	// Confirm t.TempDir resolves consistently (macOS symlink).
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		if got := strings.TrimSpace(res.Output); got != dir && got != resolved {
			t.Errorf("pwd = %q, want %q or %q", got, dir, resolved)
		}
	}
}

func TestShellOracleEmptyCommand(t *testing.T) {
	o := ShellOracle{OracleName: "t", Command: "", Timeout: time.Second}
	if _, err := o.Check(context.Background(), t.TempDir()); err == nil {
		t.Fatal("empty command must return an error (fail closed)")
	}
}

func TestShellOracleOutputTruncated(t *testing.T) {
	o := ShellOracle{OracleName: "t", Command: "yes | head -c 10000", Timeout: 30 * time.Second}
	res, err := o.Check(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(res.Output) != maxOracleOutput {
		t.Errorf("output len = %d, want %d", len(res.Output), maxOracleOutput)
	}
}

func TestShellOracleImplementsOracle(t *testing.T) {
	var _ Oracle = ShellOracle{}
}
