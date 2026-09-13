package main

// Tests for the daemon binary's supervisor-mode entry point: the hidden flag
// must be recognised before cobra runs, a normal daemon invocation must fall
// through untouched, and a malformed supervisor invocation must fail loudly
// rather than start a daemon.
//
// The supervisor loop itself is covered in internal/llm (supervisor_internal_test.go);
// these cases pin the wiring in this package.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRunSupervisorMode(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantHandled bool
		wantCode    int
	}{
		{
			name:        "normal daemon start is not supervisor mode",
			args:        []string{"-f"},
			wantHandled: false,
		},
		{
			name:        "version command is not supervisor mode",
			args:        []string{"version"},
			wantHandled: false,
		},
		{
			name:        "no arguments",
			args:        nil,
			wantHandled: false,
		},
		{
			name:        "malformed supervisor invocation fails",
			args:        []string{"--supervise-parent"},
			wantHandled: true,
			wantCode:    2,
		},
		{
			name:        "missing runtime argv fails",
			args:        []string{"--supervise-parent", "42", "--"},
			wantHandled: true,
			wantCode:    2,
		},
		{
			// The dispatch reached llm.RunSupervisor, which reported the
			// unspawnable runtime instead of starting a daemon.
			name:        "unspawnable runtime is reported",
			args:        []string{"--supervise-parent", strconv.Itoa(os.Getpid()), "--", filepath.Join(t.TempDir(), "no-such-runtime")},
			wantHandled: true,
			wantCode:    1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, handled := runSupervisorMode(tc.args)
			if handled != tc.wantHandled {
				t.Fatalf("handled = %v, want %v", handled, tc.wantHandled)
			}
			if handled && code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
		})
	}
}
