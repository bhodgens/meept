//go:build e2e

// Sticky rotation (leaf 07 task 4, master Contract 4) — RUNNING. The
// ping-pong gap this suite originally documented is FIXED (integration
// commit b1af5ce6: the PM failure path forgets the session's remembered
// endpoint). The sandbox
package sessionaffinity

import (
	"testing"
	"time"
)

// TestStickyRotationFollowsHealth is the Contract 4 end-to-end proof:
// one alias ("general", pinned onto the chat agent via pinChatModelAlias)
// with TWO provider members behind distinct stub endpoints. After a
// successful turn the session's endpoint is remembered; failing provider
// A's stub with 503s moves the session to B (the endpoint block armed by
// A's failure disqualifies A from the sticky hook AND the rotation);
// failing B afterwards moves the session BACK to A — the memory follows
// the serving endpoint on every success and never pins past a failure
// (Contract 4: quota blocks > endpoint cooldowns > alias blocks >
// stickiness; the memory is overwritten on EVERY success).
func TestStickyRotationFollowsHealth(t *testing.T) {
	stubA, _ := newHeaderStub(t)
	stubB, _ := newHeaderStub(t)

	s := newSandbox(t, stubA, stubB,
		[]string{"fake/fake-model", "fakeb/fake-model"})

	sess := s.createSession("sticky", s.ProjectDir)
	conv := conversationIDForSession(t, s.StateDir+"/sessions.db", sess)

	// Turn messages stay under 10 chars so isShortSimpleMessage routes
	// them straight to chat with zero LLM classifier calls: the CHAT
	// lane's own requests are what this test counts.
	// Turn 1 succeeds on A (first alias member): the session's endpoint
	// preference is remembered via the PM success seam.
	_ = s.chatTurn(sess, "hi", 120*time.Second)
	a1 := countBySession(stubA.snapshot(), conv)
	b1 := countBySession(stubB.snapshot(), conv)
	if a1 == 0 {
		t.Fatalf("turn 1 never reached endpoint A (A=%d, B=%d)", a1, b1)
	}
	t.Logf("turn 1: A=%d B=%d", a1, b1)

	// Fail A: the alias rotates to B mid-session. The sticky map must NOT
	// pin the session to the now-failing endpoint; B serves the turn and
	// becomes the new remembered preference.
	stubA.setFail(true)
	_ = s.chatTurn(sess, "ok next", 420*time.Second)
	b2 := countBySession(stubB.snapshot(), conv)
	if b2 == b1 {
		t.Fatalf("endpoint B never served the session after A failed (B stuck at %d)", b2)
	}
	t.Logf("turn 2 (A failing): B grew %d -> %d", b1, b2)

	// Recover A while B fails. Contract 4: the preference follows the
	// serving endpoint — turn 2's success over B moved the memory to B, so
	// the session sticks to B until B fails. B's 503 arms B's endpoint
	// block and drops the memory; rotation lands on A (A's block from
	// turn 2 expired — the sandbox's general-alias timeout is
	// seconds-scale) and A re-arms the memory. This is the directional
	// no-permanent-pin proof: the map never keeps the session on a failing
	// endpoint, in either direction.
	stubB.setFail(true)
	stubA.setFail(false)
	_ = s.chatTurn(sess, "and back", 420*time.Second)
	a3 := countBySession(stubA.snapshot(), conv)
	if a3 == a1 {
		t.Fatalf("endpoint A never received traffic after B failed post-recovery (A stuck at %d; the sticky map must not pin past a failure in EITHER direction)", a1)
	}
	t.Logf("turn 3 (A recovered, B failing): A grew %d -> %d", a1, a3)
}
