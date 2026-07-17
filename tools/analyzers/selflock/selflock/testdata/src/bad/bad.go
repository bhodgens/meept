// Package bad contains test cases for the selflock analyzer.
// These test functions are intentionally unused - they are analyzed by the selflock analyzer.
//lint:file-ignore U1000 test data for selflock analyzer - unused functions are intentional test cases
package bad

import "context"

type Goal struct {
	mu interface{} // mock mutex
}

func (g *Goal) Lock()    {}
func (g *Goal) Unlock()  {}
func (g *Goal) AddActivePlan(id string) int { return 0 } // has internal locking
func (g *Goal) RemoveActivePlan(id string) int { return 0 }
func (g *Goal) Assess(health string) {}

type Store struct{}

func (s *Store) Update(ctx context.Context, g *Goal) error {
	g.snapshot() // callback acquires lock
	return nil
}

func (g *Goal) snapshot() {}

type GoalStore struct{}

func (gs *GoalStore) Update(ctx context.Context, g *Goal) error { return nil }

//lint:ignore U1000 test function for selflock analyzer
func testSelfDeadlock() {
	goal := &Goal{}

	// BAD: External lock + method with internal locking
	goal.Lock()
	goal.AddActivePlan("plan-1") // want "selflock: AddActivePlan called while holding lock"
	goal.Unlock()

	// BAD: Same pattern with different method
	goal.Lock()
	goal.Assess("healthy") // want "selflock: Assess called while holding lock"
	goal.Unlock()
}

//lint:ignore U1000 test function for selflock analyzer
func testCallbackDeadlock(ctx context.Context) {
	goal := &Goal{}
	store := &GoalStore{}

	// BAD: External lock + store callback that may access locked object
	goal.Lock()
	store.Update(ctx, goal) // want "selflock: Update may call back into locked object"
	goal.Unlock()
}
