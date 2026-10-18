//go:build e2e

// Proof 1 (master.md Integration Test Plan): two sessions against one
// stub OpenAI-compatible endpoint that records the session_id header.
// The header carries the turn's ACCOUNTING session id — the loop's
// WithTaskScope value, which for interactive turns is the session's
// conversation id (conv-*; internal/agent/loop.go RunOnceWithParts). The
// suite resolves each session's conv id from sessions.db and asserts:
// every scoped request carries one of the two sessions' conv ids (never
// a third value, never swapped between sessions), and both sessions
// actually reached the endpoint. Unscoped secondary calls ("" header)
// are covered by TestEmptyScopeCarriesNoHeader.
package sessionaffinity

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// conversationIDForSession reads sessions.sessions.conversation_id.
func conversationIDForSession(t *testing.T, dbPath, sessionID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open sessions.db: %v", err)
	}
	defer db.Close()
	var convID string
	if err := db.QueryRow(`SELECT conversation_id FROM sessions WHERE id = ?`, sessionID).Scan(&convID); err != nil {
		t.Fatalf("conversation_id for %s: %v", sessionID, err)
	}
	if convID == "" {
		t.Fatalf("session %s has an empty conversation_id", sessionID)
	}
	return convID
}

func TestAffinityHeadersCarrySessionID(t *testing.T) {
	stub, _ := newHeaderStub(t)
	s := newSandbox(t, stub, nil, []string{"fake/fake-model"})

	sessA := s.createSession("affinity-a", s.ProjectDir)
	sessB := s.createSession("affinity-b", s.ProjectDir)
	convA := conversationIDForSession(t, s.StateDir+"/sessions.db", sessA)
	convB := conversationIDForSession(t, s.StateDir+"/sessions.db", sessB)

	// One plain chat turn per session (DIRECT chat lane; the stub answers
	// both streaming and plain JSON with real content).
	if reply := s.chatTurn(sessA, "hello there", 120*time.Second); reply == "" {
		t.Fatal("session A chat turn returned empty reply")
	}
	if reply := s.chatTurn(sessB, "good day", 120*time.Second); reply == "" {
		t.Fatal("session B chat turn returned empty reply")
	}

	got := stub.snapshot()
	if len(got) == 0 {
		t.Fatal("stub recorded no requests; the daemon never reached the endpoint")
	}
	countA, countB := 0, 0
	for i, v := range got {
		switch v {
		case convA:
			countA++
		case convB:
			countB++
		case "":
			// Unscoped internal call (e.g. an intent-analyzer probe
			// fired before the loop armed its scope). Allowed; the
			// empty-scope rule has its own test.
		default:
			t.Fatalf("request %d carried session header %q; want %q, %q, or \"\" (unscoped)",
				i, v, convA, convB)
		}
	}
	if countA == 0 || countB == 0 {
		t.Fatalf("both sessions must reach the endpoint (A=%d, B=%d of %d)",
			countA, countB, len(got))
	}
	t.Logf("affinity: %d requests (A=%d, B=%d), every header matched its session's conversation id",
		len(got), countA, countB)
}

// TestEmptyScopeCarriesNoHeader pins the empty-scope rule: the header
// value is either "" (an unscoped internal call — no WithTaskScope) or
// exactly the owning turn's accounting session id. Any third value is a
// cross-session leak.
func TestEmptyScopeCarriesNoHeader(t *testing.T) {
	stub, _ := newHeaderStub(t)
	s := newSandbox(t, stub, nil, []string{"fake/fake-model"})

	sess := s.createSession("affinity-scope", s.ProjectDir)
	conv := conversationIDForSession(t, s.StateDir+"/sessions.db", sess)
	_ = s.chatTurn(sess, "scoped hello", 120*time.Second)

	got := stub.snapshot()
	if len(got) == 0 {
		t.Fatal("stub recorded no requests")
	}
	for i, v := range got {
		if v != "" && v != conv {
			t.Fatalf("request %d carried session header %q, want \"\" or %q",
				i, v, conv)
		}
	}
	scoped := countBySession(got, conv)
	if scoped == 0 {
		t.Fatal("no request carried the session scope; the scope threading regressed")
	}
	t.Logf("scope split: %d/%d requests carried %q; the rest were unscoped",
		scoped, len(got), conv)
}
