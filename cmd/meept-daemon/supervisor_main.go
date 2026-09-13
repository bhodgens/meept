package main

// Supervisor mode entry point.
//
// The daemon spawns local LLM runtimes (llama-server, mlx_lm) under a
// supervisor, so that a hard-killed daemon (SIGKILL, panic, terminal teardown)
// cannot leave the runtime running with its model loaded and its endpoint port
// held: macOS has no parent-death signal, so the runtime itself can never
// notice. The supervisor is this binary in a hidden mode:
//
//	meept-daemon --supervise-parent <daemon pid> --pid-report-fd <fd> -- <runtime argv>
//
// It is built by llm.RuntimeProcess.Start (internal/llm/supervisor.go), which
// never appears in `meept-daemon --help`: the flag exists for the daemon's own
// children, not for operators.
//
// The dispatch happens in main BEFORE cobra runs, for two reasons:
//   - the runtime's own argv follows the `--` terminator and must reach the
//     runtime byte-identically; cobra's root-command argument validation would
//     reject a first positional argument that is not a subcommand;
//   - supervisor mode must not touch config loading, logging setup, or any
//     daemon state, so a runtime spawn stays cheap and independent of the
//     daemon's configuration.

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/llm"
)

// runSupervisorMode handles a supervisor-mode invocation. handled is false for
// every other command line, so main falls through to the normal daemon start.
// A malformed supervisor invocation is reported and fails: silently starting
// the daemon instead would leave the runtime unsupervised.
func runSupervisorMode(args []string) (code int, handled bool) {
	opts, requested, err := llm.ParseSupervisorArgs(args)
	if !requested {
		return 0, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "meept-daemon: %v\n", err)
		return 2, true
	}
	return llm.RunSupervisor(opts), true
}
