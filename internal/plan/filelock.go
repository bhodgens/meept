package plan

import (
	"sync"
)

// LockMarkdownWrite serializes read-modify-write rewrites of a single plan
// markdown file within the process. ApprovePlan runs Synthesize's
// UpdatePlanStatus rewrite and — via the evolver approval bridge — the
// actuator's markPlanApplied (internal/skills/lifecycle) CONCURRENTLY on the
// same plan.md; both do read→modify→rename, and without this lock the second
// rename silently erases the first writer's change (lost update: the applied
// marker vanished while both writers reported success).
//
// The lock covers only the read-modify-write span; plain readers stay
// lock-free because every write is atomic (tmp + rename), so they always see
// one complete version.
//
// Usage:
//
//	unlock := plan.LockMarkdownWrite(path)
//	defer unlock()
func LockMarkdownWrite(filePath string) func() {
	m, _ := mdWriteLocks.LoadOrStore(filePath, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

var mdWriteLocks sync.Map // plan file path -> *sync.Mutex
