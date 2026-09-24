//go:build e2e

// Shared helpers for the refusal suite.
package refusal

import (
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// waitAnyTask waits until at least one task row exists and returns the latest.
func waitAnyTask(t *testing.T, s *harness.Stack) harness.TaskRow {
	t.Helper()
	harness.WaitFor(t, 20*time.Second, "a task row to appear", func() bool {
		return len(harness.Tasks(t, s.TasksDBPath())) > 0
	})
	tasks := harness.Tasks(t, s.TasksDBPath())
	return tasks[len(tasks)-1]
}
