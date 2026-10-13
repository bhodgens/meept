// Package pkgone is the calling side of the fixture's known call edge.
package pkgone

import "fixture.example/repotest/pkgtwo"

// MaxDepth is a fixture const.
const MaxDepth = 3

// Alpha calls pkgtwo.Helper and pkgtwo.Counter.Next — the fixture's known
// call edges.
func Alpha(depth int) int {
	if depth <= 0 {
		return 0
	}
	c := pkgtwo.Counter{}
	c.Next()
	return pkgtwo.Helper(depth) + c.Next()
}

// Beta is a method fixture: Server.Start style receiver.
type Server struct {
	Name string
}

// Start calls Alpha (intra-package edge).
func (s Server) Start() int {
	return Alpha(MaxDepth)
}

// unusedVar exercises var collection.
var defaultName = "alpha"
