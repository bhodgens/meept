package session

import (
	"strings"
)

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
	if idx := strings.LastIndex(id, "-thread-"); idx > 0 {
		base := id[:idx]
		if base != "" && byConversationID != nil {
			if sess := byConversationID(base); sess != nil {
				return sess
			}
		}
	}
	return nil
}
