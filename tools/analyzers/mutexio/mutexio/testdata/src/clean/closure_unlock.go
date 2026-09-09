// Package clean contains code that mutexio must NOT flag.
//
// KNOWN GAP FIXTURE (deliberately in the clean corpus): a DIRECT
// mu.Unlock() inside a function literal does not pair with a Lock in the
// enclosing scope, because checkBody skips FuncLit subtrees (commit
// 6cee7f9c). In `mu.Lock(); doIO(); f(func(){ mu.Unlock() })` the enclosing
// Lock stays unmatched and the held-past-body diagnostic never fires, even
// though the I/O between Lock and the closure's Unlock genuinely runs under
// the lock at runtime.
//
// The gap is accepted rather than fixed: recording FuncLit-contained
// unlocks as "escapes scope" and holding the enclosing lock to body-end
// would re-flag the deferred-closure shapes that 6cee7f9c fixed (a deferred
// unlock in a closure releases at the literal's end, not the enclosing
// function's), and AGENTS.md's IIFE collect-then-operate pattern depends on
// that scope boundary. If you hit a real bug of this shape, either restructure
// so the unlock is not inside a literal, or annotate the enclosing I/O call
// with //nolint:mutexio.
package clean

import (
	"os"
	"sync"
)

// doIO is an I/O-named call the analyzer would flag if it believed the lock
// was held.
func doIO() error {
	return os.WriteFile("/dev/null", []byte("x"), 0o600)
}

// ClosureUnlockDoesNotPairWithEnclosingLock pins the documented false
// negative: no diagnostic is expected here today.
func ClosureUnlockDoesNotPairWithEnclosingLock() {
	var mu sync.Mutex
	unlockViaCallback := func(release func()) {
		release()
	}
	mu.Lock()
	if err := doIO(); err != nil { // NOT flagged (known gap: unlock lives in the closure below)
		mu.Unlock()
		return
	}
	unlockViaCallback(func() {
		mu.Unlock()
	})
}
