package voice

// TestMain hook for the ChildProc parent-death test: routes the fake-daemon
// re-execution role before the flag package parses anything. Mirrors
// internal/llm/supervisor_internal_test.go's pattern. No sibling test file
// defines TestMain (verified: grep across internal/voice).

import (
	"os"
	"os/signal"
	"testing"
)

// voiceFakeDaemonFlag marks a test binary invocation standing in for the
// daemon process in TestChildProcParentDeath.
const voiceFakeDaemonFlag = "--voice-fake-daemon"

// voiceFakeDaemonDeathPipe keeps the fake parent's death-pipe write end
// alive for the process's whole life: a package-level reference cannot be
// finalized, so the descriptor stays open until the process dies, which is
// exactly the signal the supervisor relies on.
var voiceFakeDaemonDeathPipe *os.File

func init() {
	signalNotifyReal = func(c chan<- os.Signal, sig ...os.Signal) {
		signal.Notify(c, sig...)
	}
}

// TestMain routes re-executions into the fake-daemon role.
func TestMain(m *testing.M) {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == voiceFakeDaemonFlag {
		os.Exit(runVoiceFakeDaemon(args[1:]))
	}
	os.Exit(m.Run())
}
