//go:build e2e

// Proof 2 (master.md Integration Test Plan): archive + daemon-stop paths
// each fire EXACTLY ONE erase against the stub llama-server; a
// cloud-configured provider fires none.
//
// The proofs run for real: the resolver-snapshot key shape (host|credential)
// is accepted by LlamaServerTeardown.Handles through the runtime manager,
// the erase route strips the provider baseURL's /v1 prefix (the slot route
// lives at the server root), the coordinator resolves the primary session id
// to the accounting (conversation) id on a snapshot miss, and the shutdown
// sweep erases per session without a live-session reference count.
package sessionteardown

import (
	"testing"
	"time"
)

// TestArchiveFiresExactlyOneErase: a session that has actually SERVED on
// the llama endpoint (so the resolver's sticky map holds its endpoint
// key) is archived via sessions.archive; the teardown coordinator fans
// the archive event to the llama adapter; the stub records exactly one
// POST /slots/0?action=erase. Unarchive fires nothing; a second archive
// brings the total to exactly 2.
func TestArchiveFiresExactlyOneErase(t *testing.T) {
	stub := newLlamaStub(t)
	s := newSandbox(t, stub, withLlamaChatLane)

	sess := s.createSession("teardown-archive", s.ProjectDir)
	serveOnLlamaEndpoint(t, s, sess)

	s.archiveSession(sess, true)
	got := assertErasesExact(t, stub, 1, 3*time.Second)
	t.Logf("archive fired %d erase(s): %v", len(got), got)

	// Unarchive + re-archive: unarchive fires nothing, the second archive
	// fires exactly one more.
	s.archiveSession(sess, false)
	s.archiveSession(sess, true)
	got = assertErasesExact(t, stub, 2, 3*time.Second)
	t.Logf("second archive -> total %d erase(s)", len(got))
}

// TestStopSweepFiresOncePerActiveSession: two sessions served on the
// llama endpoint, then SIGTERM. The shutdown sweep tears down every
// session with a remembered endpoint — exactly one erase PER SESSION,
// synchronously, before runtimes stop.
func TestStopSweepFiresOncePerActiveSession(t *testing.T) {
	stub := newLlamaStub(t)
	s := newSandbox(t, stub, withLlamaChatLane)

	sessA := s.createSession("teardown-stop-a", s.ProjectDir)
	sessB := s.createSession("teardown-stop-b", s.ProjectDir)
	serveOnLlamaEndpoint(t, s, sessA)
	serveOnLlamaEndpoint(t, s, sessB)

	s.stopGraceful()

	got := assertErasesExact(t, stub, 2, 5*time.Second)
	t.Logf("stop sweep fired %d erase(s) for 2 active sessions: %v", len(got), got)
}

// TestCloudConfiguredFiresNone: a session whose serving endpoint is a
// cloud-shaped (openai-api) provider is archived and the daemon stopped;
// the llama stub records ZERO erases (the cloud adapter is the explicit
// no-op). Unlike the two proofs above this path is NOT blocked — the
// cloud no-op is key-shape-agnostic — but it runs inside the same suite
// and is kept skippable with it so the suite's status stays coherent.
func TestCloudConfiguredFiresNone(t *testing.T) {
	stub := newLlamaStub(t)
	s := newSandbox(t, stub) // chat lane on the FakeLLM (cloud-shaped)

	sess := s.createSession("teardown-cloud", s.ProjectDir)
	runCloudTurn(t, s, sess)

	s.archiveSession(sess, true)
	s.stopGraceful()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if n := stub.eraseCount(); n != 0 {
		t.Fatalf("cloud-configured session fired %d erase(s), want 0 (recorded: %v)",
			n, stub.erasesSnapshot())
	}
	t.Log("cloud session: zero erases across archive + stop sweep")
}
