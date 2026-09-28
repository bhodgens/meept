package session

import "testing"

// TestResolveThreadConversationID pins the thread-id unwrap used by the
// daemon's resolveStepWorkingDir link chain and ChatHandler.sessionLoop: a
// thread-scoped conversation id ("conv-…-thread-…") must resolve to the
// OWNING session even though no store lookup knows it directly. Regression
// for the task-state-04 e2e skip (step jobs fell back to the daemon CWD
// because the thread-scoped id in Task.LinkedSessions resolved no session).
func TestResolveThreadConversationID(t *testing.T) {
	sess := &Session{ID: "session-abc", ConversationID: "conv-123"}
	byConv := map[string]*Session{"conv-123": sess}
	byID := map[string]*Session{"session-abc": sess}

	convLookup := func(id string) *Session { return byConv[id] }
	idLookup := func(id string) *Session { return byID[id] }

	cases := []struct {
		name string
		in   string
		want *Session
	}{
		{"conversation id", "conv-123", sess},
		{"primary id", "session-abc", sess},
		{"thread-scoped id", "conv-123-thread-work-001", sess},
		{"unknown", "conv-nope", nil},
		{"empty", "", nil},
		{"thread prefix only", "-thread-work-001", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveThreadConversationID(tc.in, convLookup, idLookup)
			if got != tc.want {
				t.Fatalf("ResolveThreadConversationID(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
