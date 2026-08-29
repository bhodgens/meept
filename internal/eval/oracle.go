package eval

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// maxOracleOutput bounds captured oracle output (4KB).
const maxOracleOutput = 4096

// OracleResult is the verdict produced by an Oracle check.
type OracleResult struct {
	Passed bool   `json:"passed"`
	Output string `json:"output"`
	Err    string `json:"error,omitempty"`
}

// Oracle checks whether a task's success criteria were met inside a workdir.
// The workdir is always an explicit argument; implementations must never
// call os.Getwd().
type Oracle interface {
	Name() string
	Check(ctx context.Context, workdir string) (OracleResult, error)
}

// ShellOracle runs a single shell command as an oracle. Exit status 0 means
// the check passed; any non-zero exit, launch failure, or timeout means it
// failed. This is intentionally a standalone exec helper — it does not import
// internal/employee, avoiding any import cycle.
//
// Field-naming note: Go forbids a struct field colliding with the Name()
// method, so the oracle's name lives in the OracleName field and Name()
// returns it.
type ShellOracle struct {
	OracleName string        // identifier reported by Name()
	Command    string        // shell command to run in the workdir
	Timeout    time.Duration // execution timeout; <=0 means no timeout
}

// Name implements Oracle.
func (s ShellOracle) Name() string { return s.OracleName }

// Check runs Command in workdir, capturing combined output truncated to
// maxOracleOutput bytes. An empty Command fails closed with an error and no
// exec. Timeout kills the process.
func (s ShellOracle) Check(ctx context.Context, workdir string) (OracleResult, error) {
	if s.Command == "" {
		return OracleResult{}, fmt.Errorf("eval: shell oracle %q has empty command", s.OracleName)
	}

	var cancel context.CancelFunc
	if s.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", s.Command)
	cmd.Dir = workdir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := buf.String()
	if len(out) > maxOracleOutput {
		out = out[:maxOracleOutput]
	}

	if ctx.Err() == context.DeadlineExceeded {
		return OracleResult{
			Passed: false,
			Output: out,
			Err:    fmt.Sprintf("oracle command timed out after %s", s.Timeout),
		}, nil
	}

	res := OracleResult{Output: out}
	if runErr != nil {
		res.Err = runErr.Error()
	}
	res.Passed = runErr == nil
	return res, nil
}
