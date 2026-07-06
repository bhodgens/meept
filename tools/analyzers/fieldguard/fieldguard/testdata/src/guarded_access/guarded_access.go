package guarded_access

import "sync"

type Container struct {
	mu    sync.Mutex  // guarded by: mu
	items []string
	count int  // guarded by: mu
}

func NewContainer() *Container {
	return &Container{
		items: make([]string, 0),
		count: 0,
	}
}

// Correct: all guarded fields accessed under lock
func (c *Container) AddCorrect(item string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, item)
	c.count++
}

// WRONG: accessing count without lock
func (c *Container) GetCountWrong() int {
	return c.count // want "fieldguard: unguarded read of guarded field"
}

// WRONG: accessing items without lock
func (c *Container) GetItemsWrong() []string {
	return c.items // want "fieldguard: unguarded read of guarded field"
}

// Correct snapshot pattern
func (c *Container) GetCountCorrect() int {
	c.mu.Lock()
	count := c.count
	c.mu.Unlock()
	return count
}
