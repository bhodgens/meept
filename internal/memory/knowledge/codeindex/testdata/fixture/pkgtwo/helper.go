// Package pkgtwo is the callee side of the fixture's known call edge.
package pkgtwo

// Helper doubles its input.
func Helper(n int) int {
	return n * 2
}

// Counter is a method-receiver fixture.
type Counter struct {
	N int
}

// Next advances and returns the counter.
func (c *Counter) Next() int {
	c.N++
	return c.N
}
