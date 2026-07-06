package guarded

import "sync"

type GuardedStruct struct {
	mu      sync.Mutex  // guarded by: mu
 guarded int
}

func NewGuardedStruct() *GuardedStruct {
	return &GuardedStruct{
		guarded: 0,
	}
}

// Correct access - under lock
func (g *GuardedStruct) GetGuardedCorrect() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.guarded
}

// WRONG: unguarded read of guarded field
func (g *GuardedStruct) GetGuardedWrong() int {
	return g.guarded // want "fieldguard: unguarded read of guarded field"
}

// WRONG: unguarded write of guarded field
func (g *GuardedStruct) SetGuardedWrong(v int) {
	g.guarded = v // want "fieldguard: unguarded write of guarded field"
}

// Correct write - under lock
func (g *GuardedStruct) SetGuardedCorrect(v int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.guarded = v
}
