package session

import (
	"strings"
)

// ThreadIDSeparator is the infix that separates a base conversation id from
// the thread topic in a thread-scoped conversation id
// ("<base conv id>-thread-<topic>-<n>"). ResolveThreadConversationID unwraps
// on it, so no minted thread id may contain it.
const ThreadIDSeparator = "-thread-"

// maxThreadSlugRunes bounds the sanitized slug. Thread ids are
// "<prefix><slug>-<random hex>" and end up in conversation ids, CLI args and
// the session_threads primary key — an unbounded user label would put an
// arbitrary-length, path-unsafe segment into all three.
const maxThreadSlugRunes = 32

// ThreadSlug sanitizes a topic label into the slug segment of a thread id.
//
// This is the structural guard for ResolveThreadConversationID's unwrap: that
// function finds the LAST "-thread-" in an id and treats everything before it
// as the base conversation id. A topic label containing "-thread-" would
// therefore mint an id whose base is NOT a real conversation id.
//
// The unwrap is fail-safe even without this guard (a false trigger resolves to
// nil, never to the wrong session — see ResolveThreadConversationID), but
// fail-safe means "the turn loses its session binding", not "this is fine".
// Sanitizing at mint time removes the shape entirely: '-', '.', '/', '\' and
// every other non-alphanumeric rune collapse to '_', so no separator, dot
// segment or path separator can survive. An empty result becomes "thread", so
// the id is always "thread-<slug>-<random hex>".
//
// Display code must render thread.TopicLabel, never the slug — the slug is
// lossy by design ("my/topic" and "my.topic" both mint "my_topic").
func ThreadSlug(topicLabel string) string {
	var b strings.Builder
	b.Grow(len(topicLabel))
	n := 0
	for _, r := range topicLabel {
		if n >= maxThreadSlugRunes {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		n++
	}
	if b.Len() == 0 {
		return "thread"
	}
	return b.String()
}

// ResolveThreadConversationID maps any conversation-ish key to the SESSION
// that owns it.
//
// Thread-scoped conversation ids ("conv-<hex>-thread-<topic>-<n>", minted by
// Session.GetOrCreateThread) are NOT session keys: the session store indexes
// sessions by their own conversation id and primary id, so a lookup with a
// thread id misses and callers degrade (step jobs fell back to the daemon CWD
// — task-state-04 e2e, verified live). This helper unwraps a thread id to its
// owning session by trimming the "-thread-..." suffix and re-looking the base
// conversation id.
//
// Fail-safe contract (bughunt L11) — read this before adding a thread-id mint
// path:
//
//   - The unwrap runs ONLY after two DIRECT lookups (by conversation id, then
//     by primary id) have already missed. A thread id is therefore never
//     unwrapped to a session that the direct lookups would have found, so an
//     id that merely CONTAINS "-thread-" cannot shadow a real session.
//   - It trims at the LAST occurrence and re-looks the base. A false trigger
//     (a base that is not a real conversation id) resolves to nil — a clean
//     miss, NEVER a wrong-session binding.
//   - The residual risk is a LOST BINDING, not a mis-binding: an id crafted so
//     its last "-thread-" sits mid-string yields a base that no session owns,
//     and callers degrade (e.g. a step job with no session working dir). That
//     is the same fail-safe direction as any unknown id.
//
// The structural mitigation is ThreadSlug: every mint path
// (CreateThreadInSession, services.CreateThread, and the agent's
// TopicDetector.GenerateThreadID) slug-sanitizes the topic label so
// "-thread-" — and any '.' or path separator — cannot appear in a thread id.
// Keep that guarantee if you add another mint site: an unsanitized label
// re-opens the lost-binding path even though it cannot cause a mis-binding.
//
// The caller supplies two lookups so no store interface has to change:
// byConversationID is GetByConversationID, byID is Get (legacy callers pass
// the session primary id). Returns nil when the id is neither a session
// conversation id, a session primary id, nor derivable from a thread id.
func ResolveThreadConversationID(
	id string,
	byConversationID func(conversationID string) *Session,
	byID func(id string) *Session,
) *Session {
	if id == "" {
		return nil
	}
	if byConversationID != nil {
		if sess := byConversationID(id); sess != nil {
			return sess
		}
	}
	if byID != nil {
		if sess := byID(id); sess != nil {
			return sess
		}
	}
	// Thread-scoped id: "<base conversation id>-thread-<topic>-<n>".
	// The base itself is a conversation id, so re-look it that way.
	if idx := strings.LastIndex(id, ThreadIDSeparator); idx > 0 {
		base := id[:idx]
		if base != "" && byConversationID != nil {
			if sess := byConversationID(base); sess != nil {
				return sess
			}
		}
	}
	return nil
}
