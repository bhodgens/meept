// Package clean contains code that mutexio must NOT flag: closures with
// deferred unlocks followed by unrelated statements in the enclosing scope.
//
// Regression scenario (pre-2026-09-06 analyzer): the enclosing function's
// scan paired a closure's mu.Lock() with its deferred mu.Unlock() and set
// the effective unlock position to the ENCLOSING function's end, flagging
// every I/O-named call after the closure (e.g. t.Run in table-driven
// tests). A deferred unlock inside a function literal releases when the
// literal returns, so the pair must never escape the literal's scope.
package clean

import (
	"os"
	"sync"
	"testing"
)

// hookFixture mirrors the shape that triggered the false positive: a
// setter taking a callback whose body locks, followed by more statements
// in the enclosing test function.
type hookFixture struct {
	hook func(from, to string)
}

func (h *hookFixture) SetHook(fn func(from, to string)) { h.hook = fn }

func TestClosureLockThenSubsequentCalls(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	fx := &hookFixture{}
	fx.SetHook(func(from, to string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, from+"->"+to)
	})
	if len(calls) != 0 {
		t.Fatal("no calls yet")
	}
	// Previously flagged: t.Run matched the "Run" I/O method name while
	// the analyzer (wrongly) considered mu still held.
	t.Run("subtest after closure", func(t *testing.T) {
		t.Log("not under lock")
	})
}

func TestMultipleClosuresThenFileIO(t *testing.T) {
	var mu sync.Mutex
	apply := func(f func()) { f() }
	apply(func() {
		mu.Lock()
		defer mu.Unlock()
	})
	apply(func() {
		mu.Lock()
		defer mu.Unlock()
	})
	// Previously flagged: os.WriteFile after the closures ran.
	tmp := t.TempDir()
	if err := os.WriteFile(tmp+"/out", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDeferInsideMethodValueClosure(t *testing.T) {
	var mu sync.RWMutex
	read := func() int {
		mu.RLock()
		defer mu.RUnlock()
		return 1
	}
	_ = read
	// Previously flagged via RLock pairing leakage.
	t.Run("nested", func(t *testing.T) { t.Log("fine") })
}
