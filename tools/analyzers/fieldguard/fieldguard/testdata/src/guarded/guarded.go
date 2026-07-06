package guarded

import "sync"

type GuardedStruct struct {
	mu      sync.Mutex  // guard for: guarded
 guarded int           // guarded by mu
}

func NewGuardedStruct() *GuardedStruct {
	return &GuardedStruct{
		guarded: 0,
	}
}

// Correct access - under lock with defer
func (g *GuardedStruct) GetGuardedCorrect() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.guarded
}

// WRONG: unguarded read of guarded field
func (g *GuardedStruct) GetGuardedWrong() int {
	return g.guarded // want "fieldguard: unguarded access to field"
}

// WRONG: unguarded write of guarded field
func (g *GuardedStruct) SetGuardedWrong(v int) {
	g.guarded = v // want "fieldguard: unguarded access to field"
}

// Correct write - under lock
func (g *GuardedStruct) SetGuardedCorrect(v int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.guarded = v
}

// Correct access - explicit unlock before return
func (g *GuardedStruct) GetGuardedExplicit() int {
	g.mu.Lock()
	v := g.guarded
	g.mu.Unlock()
	return v
}
