package http

// listenerAddr returns the address the server is listening on, for tests that
// must dial a :0-bound server. The production Server.Addr accessor was removed
// as dead code (deadcode wave-1); tests reach into the listener directly.
func (s *Server) listenerAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.config.Addr
}
