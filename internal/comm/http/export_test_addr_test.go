package http

// TestAddr exposes the listener address to external-package tests
// (package http_test). Production Addr accessor was removed as dead code
// in the deadcode wave-1; this shim lives in a _test.go file so it never
// ships in the production build.
func (s *Server) TestAddr() string { return s.listenerAddr() }
