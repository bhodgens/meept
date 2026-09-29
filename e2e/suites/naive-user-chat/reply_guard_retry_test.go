//go:build e2e

// Reply-guard rewrite-retry scenario (manifest scenario naive-user-chat-05).
//
// The unit-level contract (internal/agent/reply_guard_retry_test.go) pins
// the loop mechanics with an in-process chatter; this scenario proves the
// same recovery END TO END through the daemon: a chat lane ("chat" intent —
// dispatcher RouteToAgent → AgentLoop.RunOnce, no task/planner/executor
// scaffolding) whose FIRST completion is a raw memory_store-style
// tool-result JSON dump — the exact shape the reply guard replaces (rule
// tool_result_json) — must NOT ship the canned apology ("raw data instead
// of an answer"). The loop must inject the rewrite nudge, re-run the
// reasoning cycle ONCE, and the user-facing reply must be the model's
// rewritten plain-language answer.
//
// The scripted model misbehaves exactly once (ScriptOnce, keyed on the
// rewrite nudge being ABSENT from the request): after the guard injects the
// nudge the marker appears in the transcript, so the retried completion is
// the genuine prose and the second guard pass clears.
//
// Assertions:
//
//	R1  the reply is the rewritten prose (names the memory marker), not
//	    the canned "raw data instead of an answer" line
//	R2  the fake LLM saw the rewrite nudge exactly once (the loop
//	    actually retried — bounded — not a pass-through)
//	R3  the reply passes the shared A0-A3 shape checks
package naiveuserchat

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

const (
	guardDumpReply = `{"remembered":"guard-retry-marker-e2e","memory_id":"guard-retry-e2e-0001","success":true,"type":"task"}`
	guardFixReply  = "your note is stored as a task memory carrying marker guard-retry-marker-e2e — ask me to recall it any time and i will pull it up."
	// The rewrite nudge the loop injects (internal/agent/reply_guard.go
	// replyGuardRewriteNudge); a distinctive substring is enough to
	// detect it in the requests the fake LLM receives.
	guardNudgeMarker = "rewrite your answer in plain language"
)

// TestReplyGuardRewriteRetry drives one chat-lane turn whose first
// completion is a raw tool-result dump and asserts the daemon recovers with
// a rewritten prose answer instead of the canned apology
// (naive-user-chat-05).
func TestReplyGuardRewriteRetry(t *testing.T) {
	s := harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-guard-retry", s.ProjectDir)

	// Pin the lane to chat: RouteToAgent → RunOnce — the loop seam the
	// rewrite retry lives on (a task lane would answer through the
	// executor's post-tool text and the task-completion relay instead).
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"guard-retry chat pin"}`)

	// First completion on this lane: the raw dump — consumed exactly
	// once, BEFORE the rewrite nudge exists in the transcript. The
	// predicate pins the CHAT loop's completion: the loop's LLM call
	// goes out with "stream": true (internal/llm client.go addStream),
	// which distinguishes it from the intent-analysis/classifier
	// requests (non-streaming) sharing this lane. Scripts run before
	// the fake's legacy overrides, so an unpinned predicate would
	// otherwise swallow the classifier's request. After the loop
	// injects the nudge, the marker appears and the second script
	// serves the rewritten prose.
	predStream := func(body map[string]any) bool {
		stream, _ := body["stream"].(bool)
		return stream
	}
	chatLane := harness.And(
		predStream,
		harness.Not(harness.MessageContains(guardNudgeMarker)),
	)
	s.Fake.ScriptOnce(chatLane, harness.TextResponse(guardDumpReply))
	s.Fake.Script(
		harness.And(
			predStream,
			harness.MessageContains(guardNudgeMarker),
		),
		harness.TextResponse(guardFixReply),
	)

	reply := s.ChatTurn(t, sessionID, "remember that the guard retry marker e2e is green", 180*time.Second)

	// R3: shared reply shape.
	assertReplyShape(t, "guard-retry", reply)

	// R2 first (diagnostics): count nudge sightings up front so an R1
	// failure can report whether the retry fired at all.
	sawNudge := 0
	for _, req := range s.Fake.Requests() {
		raw, _ := req["messages"].([]any)
		for _, m := range raw {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if text, _ := msg["content"].(string); strings.Contains(text, guardNudgeMarker) {
				sawNudge++
			}
		}
	}

	// R1: the rewritten answer shipped — the canned apology did not.
	if !strings.Contains(reply, "guard-retry-marker-e2e") {
		t.Fatalf("R1: reply does not carry the rewritten answer: %q (llm requests=%d, nudge sightings=%d)\ndaemon log tail:\n%s",
			reply, s.Fake.RequestCount(), sawNudge, s.Daemon.LogTail())
	}
	if strings.Contains(reply, "raw data instead of an answer") {
		t.Fatalf("R1: the canned apology shipped; the rewrite retry did not recover: %q", reply)
	}

	if sawNudge == 0 {
		t.Fatalf("R2: the rewrite nudge never reached the model (retry did not fire); requests: %d\ndaemon log tail:\n%s",
			s.Fake.RequestCount(), s.Daemon.LogTail())
	}
	if sawNudge > 1 {
		t.Fatalf("R2: the rewrite nudge fired %d times; the retry must be bounded to one per turn", sawNudge)
	}
}
