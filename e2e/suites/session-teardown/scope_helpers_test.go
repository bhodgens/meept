//go:build e2e

// Session-endpoint helpers: the teardown coordinator keys on the
// resolver's session→endpoint snapshot, which the ProviderManager feeds
// on every successful SCOPED LLM call. The suite seeds that snapshot via
// the resolver's RememberSessionEndpoint RPC-free equivalent: a real
// scoped chat turn against the endpoint in question.
package sessionteardown

import (
	"strings"
	"testing"
	"time"
)

// waitIdle blocks briefly — used after seeding to let any async wiring
// settle (kept minimal; the suites assert on recorded counts, not timing).
func waitIdle(d time.Duration) { time.Sleep(d) }

// chatTurn submits one short (<10 chars — short_message_guard lane) turn
// and awaits the terminal reply.
func (s *sandbox) chatTurn(sessionID, message string, timeout time.Duration) string {
	t := s.t
	out, stderr := s.runCLI(timeout, true, "chat", "--session", sessionID, message)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("chat turn returned empty reply\nstderr: %s\ndaemon log tail:\n%s", stderr, s.logTail())
	}
	return strings.TrimSpace(out)
}

// serveOnLlamaEndpoint gives the session one successful scoped turn on
// the llama endpoint, feeding reportSessionEndpoint and the resolver
// snapshot the teardown coordinator reads.
func serveOnLlamaEndpoint(t *testing.T, s *sandbox, sessionID string) {
	t.Helper()
	_ = s.chatTurn(sessionID, "hi", 120*time.Second)
	waitIdle(500 * time.Millisecond)
}

// runCloudTurn gives the session one successful scoped turn on the
// default (cloud-shaped FakeLLM) lane.
func runCloudTurn(t *testing.T, s *sandbox, sessionID string) {
	t.Helper()
	_ = s.chatTurn(sessionID, "hi", 120*time.Second)
	waitIdle(500 * time.Millisecond)
}
