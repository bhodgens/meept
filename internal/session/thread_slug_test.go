package session

// L11 of the 2026-10-05 bughunt: a thread id containing "-thread-" makes
// ResolveThreadConversationID's unwrap find a base that is not a real
// conversation id. The unwrap was already fail-safe (nil, never a wrong-session
// binding) but the risk was UNDOCUMENTED, and services.CreateThread minted
// ids straight from a user-supplied topic label with no sanitization.
//
// The structural fix is ThreadSlug at every mint site. These pins hold both
// halves: the slug removes the shape at mint time, AND the unwrap keeps its
// fail-safe direction (a crafted id resolves to nil, never to the wrong
// session).

import (
	"strings"
	"testing"
)

// TestThreadSlug_NeverMintsTheUnwrapSeparator is the structural pin: no topic
// label — including one crafted to smuggle the separator, a dot, or a path
// separator — can produce a slug containing "-thread-", ".", "/" or "\".
func TestThreadSlug_NeverMintsTheUnwrapSeparator(t *testing.T) {
	for _, label := range []string{
		"work",
		"my topic",
		"a-thread-b",
		"-thread-",
		"x--thread--y",
		"../../etc/passwd",
		`a\b`,
		"a/b",
		"ünïcödé-topic",
		"   ",
		"",
		".",
		strings.Repeat("x", 500),
	} {
		got := ThreadSlug(label)
		for _, bad := range []string{ThreadIDSeparator, ".", "/", "\\"} {
			if strings.Contains(got, bad) {
				t.Errorf("ThreadSlug(%q) = %q, must not contain %q", label, got, bad)
			}
		}
		if got == "" {
			t.Errorf("ThreadSlug(%q) = %q, want a non-empty fallback", label, got)
		}
	}
}

// TestThreadSlug_Bounded pins the length cap: a thread id carries the slug
// into the session_threads primary key, the conversation id and CLI args.
func TestThreadSlug_Bounded(t *testing.T) {
	got := ThreadSlug(strings.Repeat("abc", 100))
	if len([]rune(got)) != maxThreadSlugRunes {
		t.Errorf("slug length = %d runes, want %d", len([]rune(got)), maxThreadSlugRunes)
	}
}

// TestCreateThreadInSession_MintedIDCannotTriggerAWrongUnwrap is the end-to-end
// pin for the mint path: a hostile topic label must not produce a thread
// conversation id that the unwrap mis-resolves.
func TestCreateThreadInSession_MintedIDCannotTriggerAWrongUnwrap(t *testing.T) {
	sess := &Session{ID: "session-l11", ConversationID: "conv-l11"}
	thread := CreateThreadInSession(sess, "evil-thread-conv-other")

	// The thread's conversation id is "<conv>-<thread id>".
	if idx := strings.LastIndex(thread.ConversationID, ThreadIDSeparator); idx > 0 {
		base := thread.ConversationID[:idx]
		if base != sess.ConversationID {
			t.Errorf("minted conv id %q unwraps to %q, want the real base %q",
				thread.ConversationID, base, sess.ConversationID)
		}
	}
	// And the owning session is still resolvable through the public helper.
	got := ResolveThreadConversationID(
		thread.ConversationID,
		func(id string) *Session {
			if id == sess.ConversationID {
				return sess
			}
			return nil
		},
		func(string) *Session { return nil },
	)
	if got != sess {
		t.Errorf("ResolveThreadConversationID(minted conv id) = %v, want the owning session", got)
	}
}

// TestResolveThreadConversationID_FailSafeOnCraftedIDs is the fail-safe pin.
// The unwrap runs only AFTER both direct lookups miss, so an id containing the
// separator can never shadow a real session, and a false trigger resolves to
// nil rather than to some other session.
func TestResolveThreadConversationID_FailSafeOnCraftedIDs(t *testing.T) {
	real := &Session{ID: "session-real", ConversationID: "conv-real"}
	other := &Session{ID: "session-other", ConversationID: "conv-other"}

	byConv := map[string]*Session{"conv-real": real, "conv-other": other}
	convLookup := func(id string) *Session { return byConv[id] }
	idLookup := func(string) *Session { return nil }

	cases := []struct {
		name string
		in   string
		want *Session
	}{
		// A crafted id whose LAST separator sits mid-string: the base is
		// "conv-also" (or similar), which owns no session -> clean miss.
		{"crafted separator base not a session", "conv-also-thread-x", nil},
		// An id whose LAST separator yields a REAL base: that base wins,
		// which is exactly the thread-id contract.
		{"real base after separator", "conv-real-thread-work-1", real},
		// Separator present but the base resolves to a DIFFERENT real
		// session: that session wins, deterministically, by the base —
		// never by guessing. Documented behaviour, not a mis-binding.
		{"another real base", "conv-other-thread-y-1", other},
		// Direct lookups take precedence over any unwrap: a real
		// conversation id that merely CONTAINS the separator resolves to
		// itself, not to a base session.
		{"direct lookup wins over unwrap", "conv-real", real},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveThreadConversationID(tc.in, convLookup, idLookup)
			if got != tc.want {
				t.Fatalf("ResolveThreadConversationID(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	// The shadowing case, stated directly: an id that equals a real
	// conversation id BUT contains the separator must resolve to that
	// session via the DIRECT lookup — the unwrap must not preempt it.
	if got := ResolveThreadConversationID("conv-thread-weird", func(id string) *Session {
		if id == "conv-thread-weird" {
			return real
		}
		return nil
	}, idLookup); got != real {
		t.Errorf("direct lookup was preempted by the unwrap: got %v, want the owning session", got)
	}
}
