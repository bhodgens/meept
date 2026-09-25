//go:build e2e

package steppipeline

import (
	"os"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// readWholeDaemonLog returns the FULL daemon log (LogTail caps at 4KB,
// which drops early lines under chatty runs).
func readWholeDaemonLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(s.Work + "/daemon.log")
	if err != nil {
		return ""
	}
	return string(data)
}
