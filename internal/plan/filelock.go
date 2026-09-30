package plan

import (
	"hash/fnv"
	"sync"
)

// mdWriteLocks is a FIXED-SIZE pool of mutexes bucketed by FNV-1a hash of the
// plan file path. A per-path map would grow without bound (one mutex leaked
// per plan file ever locked — L12); a fixed pool keeps mutual exclusion for
// same-path writers while capping memory at mdWriteLockBuckets mutexes.
// Distinct paths sharing a bucket serialize harmlessly.
const mdWriteLockBuckets = 64

var mdWriteLocks [mdWriteLockBuckets]sync.Mutex

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
	h := fnv.New32a()
	_, _ = h.Write([]byte(filePath))
	mu := &mdWriteLocks[h.Sum32()%mdWriteLockBuckets]
	mu.Lock()
	return mu.Unlock
}
