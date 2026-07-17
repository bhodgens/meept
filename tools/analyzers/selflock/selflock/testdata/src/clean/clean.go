// Package clean contains test cases for the selflock analyzer.
// These test functions are intentionally unused - they are analyzed by the selflock analyzer.
//lint:file-ignore U1000 test data for selflock analyzer - unused functions are intentional test cases
package clean

import "context"

type Goal struct {
	//lint:ignore U1000 mock mutex for testing
	mu interface{}
}

func (g *Goal) Lock()   {}
func (g *Goal) Unlock() {}
func (g *Goal) AddActivePlan(id string) int {
	g.Lock()
	defer g.Unlock()
	return 0
}

type Store struct{}

func (s *Store) Update(ctx context.Context, g *Goal) error { return nil }

//lint:ignore U1000 test function for selflock analyzer
// OK: No external locking - method handles its own
func testNoExternalLock() {
	goal := &Goal{}
	goal.AddActivePlan("plan-1") // OK: method has internal locking
}

//lint:ignore U1000 test function for selflock analyzer
// OK: Lock acquired and released before calling method
func testLockThenCall() {
	goal := &Goal{}
	goal.Lock()
	// do something
	//lint:ignore SA2001 intentional empty critical section for testing
	goal.Unlock()
	goal.AddActivePlan("plan-1") // OK: lock released before call
}

//lint:ignore U1000 test function for selflock analyzer
// OK: Store update without holding lock
func testStoreUpdate(ctx context.Context) {
	goal := &Goal{}
	store := &Store{}
	store.Update(ctx, goal) // OK: no external lock held
}

//lint:ignore U1000 test function for selflock analyzer
// OK: nolint suppression
func testSuppressed() {
	goal := &Goal{}
	goal.Lock()
	goal.AddActivePlan("plan-1") //nolint:selflock // explicit suppression
	goal.Unlock()
}
