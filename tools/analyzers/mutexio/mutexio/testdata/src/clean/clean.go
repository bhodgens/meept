// Package clean contains negative test fixtures for the mutexio analyzer.
// None of these functions should produce a diagnostic.
package clean

import (
	"sync"
	"sync/atomic"
)

// fakeClient is an I/O-like type whose method names overlap with atomic ops
// and map operations. Calling its methods under a lock SHOULD still be flagged
// because fakeClient is not sync/atomic, sync.Map, or a project-local struct
// guarded by an internal mutex. This fixture exists to verify the analyzer
// doesn't over-suppress — it's a regression check, not a clean pattern.
//
// (We don't call fakeClient under a lock in this file. The bad/ package
// covers the must-flag case.)

// cleanAtomicBoolLoad: atomic.Bool.Load/Store under a mutex is NOT I/O.
func cleanAtomicBoolLoad() {
	var mu sync.Mutex
	var a atomic.Bool
	mu.Lock()
	a.Store(true)
	if a.Load() {
		_ = true
	}
	mu.Unlock()
}

// cleanAtomicInt64Ops: atomic.Int64.Add/Swap/CompareAndSwap under a mutex
// is NOT I/O.
func cleanAtomicInt64Ops() {
	var mu sync.Mutex
	var n atomic.Int64
	mu.Lock()
	n.Add(1)
	n.Swap(42)
	n.CompareAndSwap(0, 1)
	_ = n.Load()
	mu.Unlock()
}

// cleanAtomicPointerLoad: atomic.Pointer.Load under a mutex is NOT I/O.
func cleanAtomicPointerLoad() {
	var mu sync.Mutex
	var p atomic.Pointer[int]
	mu.Lock()
	if p.Load() != nil {
		// in-memory read
	}
	mu.Unlock()
}

// cleanAtomicValueLoad: atomic.Value.Load/Store under a mutex is NOT I/O.
func cleanAtomicValueLoad() {
	var mu sync.Mutex
	var v atomic.Value
	mu.Lock()
	_ = v.Load()
	v.Store("hello")
	mu.Unlock()
}

// cleanSyncMapOps: sync.Map.Load/Store/Delete/Range under a mutex is NOT I/O.
func cleanSyncMapOps() {
	var mu sync.Mutex
	var m sync.Map
	mu.Lock()
	m.Store("k", "v")
	if _, ok := m.Load("k"); ok {
		// in-memory read
	}
	m.Delete("k")
	m.Range(func(_, _ any) bool { return true })
	mu.Unlock()
}

// concurrentMap is a project-local map-like type guarded by an internal
// mutex. Its Get/Put/Len methods should NOT be flagged as I/O even when
// the caller holds an unrelated outer mutex, because the operations are
// pure in-memory map reads under the inner lock.
type concurrentMap struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *concurrentMap) Get(key string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *concurrentMap) Put(key string, v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = v
}

func (c *concurrentMap) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// cleanProjectLocalMap: a project-local map-like type's Get/Put/Len under
// an outer mutex is NOT I/O.
func cleanProjectLocalMap() {
	var outerMu sync.Mutex
	cm := &concurrentMap{m: map[string]int{}}
	outerMu.Lock()
	cm.Put("a", 1)
	if _, ok := cm.Get("a"); ok {
		// in-memory read on the inner map
	}
	_ = cm.Len()
	outerMu.Unlock()
}

// realIORegressionCheck: a genuine I/O call under a lock is STILL flagged.
// (Lives in the bad package — see bad/violationDirectUnlock etc.)
// This comment exists to remind reviewers that this fixture file tests
// only the negative (non-flagging) path.
