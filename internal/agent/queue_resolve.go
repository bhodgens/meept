package agent

// Queue resolution seams for the steering queue lifecycle (C3, leaf
// 01-safety/02-queue-lifecycle). RunOnce registers a turn's queue under the
// THREAD-scoped conversation id minted by the thread router
// (conv-X-thread-work-ab12), while chat.steer / the TUI only know the
// session-level id (conv-X). Resolution order: exact key -> thread-unwrapped
// base -> session-owned base+"-thread-*" keys. An ambiguous multi-thread
// match is an EXPLICIT error, never a silent pick.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/session"
)

// ErrAmbiguousQueue is returned when a session-level conversation id matches
// more than one live thread-scoped queue. Steering must then target the full
// thread conversation id; silently picking one thread would inject into the
// wrong work stream.
var ErrAmbiguousQueue = errors.New("ambiguous active queue: multiple live thread queues match; steer with the full thread conversation id")

// threadKeyMarker separates the session-level conversation base from the
// thread-scoped suffix minted by the thread router
// (internal/session/thread_resolve.go).
const threadKeyMarker = "-thread-"

// unwrapThreadConversationID strips a thread suffix from a conversation key,
// returning the session-level base. Keys without a thread marker are
// returned unchanged.
func unwrapThreadConversationID(key string) string {
	if i := strings.Index(key, threadKeyMarker); i > 0 {
		return key[:i]
	}
	return key
}

// UnwrapThreadConversationID is the exported view of
// unwrapThreadConversationID for surfaces that key off the session-level
// conversation base - the task store's session_tasks join is written with
// session ids, so an RPC that receives a thread-scoped conversation id must
// strip the thread suffix before a session-scoped lookup
// (chat.queue_status task_states, leaf 05-navigation/03 T2).
func UnwrapThreadConversationID(key string) string {
	return unwrapThreadConversationID(key)
}

// isThreadKeyFor reports whether key is a thread-scoped registration owned
// by the session-level base conversation id.
func isThreadKeyFor(base, key string) bool {
	return strings.HasPrefix(key, base+threadKeyMarker)
}

// ResolveActiveQueue resolves the live queue for a conversation id using the
// C3 resolution order: exact key -> thread-unwrapped base -> session-owned
// base+"-thread-*" keys. A single thread match remaps; multiple live thread
// matches return ErrAmbiguousQueue (never a silent pick); no match returns
// ErrQueueNotFound (the HTTP layer maps it to 404).
//
// Identity canonicalization (leaf 01, 20261121-interactive-usability C-1):
// the registry cannot reach the session store (import direction - the
// session package does not know agent), so the SESSION->CONVERSATION half
// of the resolution lives in ResolveActiveQueueWithSession. The bare
// resolver keeps the queue-key-only order (exact -> unwrapped base ->
// thread keys).
func (r *AgentRegistry) ResolveActiveQueue(conversationID string) (*MessageQueue, error) {
	r.activeQueuesMu.RLock()
	defer r.activeQueuesMu.RUnlock()

	entry, err := r.resolveActiveQueueLocked(conversationID)
	if err != nil {
		return nil, err
	}
	return entry.Queue, nil
}

// ResolveActiveQueueWithSession resolves the live queue accepting ANY of
// the session's identity shapes: the primary session id (session-abc123 -
// the shape the Flutter GUI sends as conversation_id), the session's own
// conversation id, a thread-scoped conversation id, or a queue key already
// registered by the loop. Read-only: no registration or store mutation.
//
// Resolution order (matches the submit path's Get-then-GetByConversationID
// precedence, then the C3 thread keys):
//
//  1. exact queue key (a live registration under the id as given)
//  2. session->conversation resolution via session.ResolveThreadConversationID
//     (session id -> owning conversation id; thread-scoped id -> owning
//     session -> its conversation id), probing each candidate queue key
//  3. thread-unwrapped base + session-owned thread keys (C3)
//
// byConversationID/byID are the session store's GetByConversationID/Get.
// A nil store (either func nil) keeps the bare C3 behavior - test
// registries without a store are unaffected.
func (r *AgentRegistry) ResolveActiveQueueWithSession(
	conversationID string,
	byConversationID func(conversationID string) *session.Session,
	byID func(id string) *session.Session,
) (*MessageQueue, error) {
	r.activeQueuesMu.RLock()
	entry, err := r.resolveActiveQueueLocked(conversationID)
	r.activeQueuesMu.RUnlock()
	if err == nil {
		return entry.Queue, nil
	}

	// Session->conversation resolution: the id may be the session's
	// PRIMARY id or a thread-scoped conversation id; map it to the
	// session's OWN conversation id (the canonical submit identity) and
	// re-probe the queue map. Read-only: ResolveThreadConversationID
	// performs direct lookups + a thread-suffix unwrap only.
	if sess := session.ResolveThreadConversationID(conversationID, byConversationID, byID); sess != nil {
		if canonical := sess.ConversationID; canonical != "" {
			r.activeQueuesMu.RLock()
			entry, err = r.resolveActiveQueueLocked(canonical)
			r.activeQueuesMu.RUnlock()
			if err == nil {
				return entry.Queue, nil
			}
		}
	}

	return nil, err
}

// resolveActiveQueueLocked implements the C3 resolution order. Callers must
// hold activeQueuesMu (read or write).
func (r *AgentRegistry) resolveActiveQueueLocked(conversationID string) (*QueueEntry, error) {
	// Step 1: exact key.
	if entry, ok := r.activeQueues[conversationID]; ok {
		return entry, nil
	}

	// Step 2: thread-unwrapped base (a thread-scoped lookup whose exact
	// registration ended falls back to the session-level registration).
	base := unwrapThreadConversationID(conversationID)
	if base != conversationID {
		if entry, ok := r.activeQueues[base]; ok {
			return entry, nil
		}
	}

	// Step 3: session-owned thread keys. RunOnce registers under the
	// thread-scoped id the thread router minted; a session-level lookup
	// must find that single live thread queue.
	var match *QueueEntry
	matches := 0
	for key, entry := range r.activeQueues {
		if isThreadKeyFor(base, key) {
			matches++
			match = entry
		}
	}
	switch matches {
	case 0:
		return nil, fmt.Errorf("resolve active queue for %q: %w", conversationID, ErrQueueNotFound)
	case 1:
		return match, nil
	default:
		return nil, fmt.Errorf("resolve active queue for %q: %w", conversationID, ErrAmbiguousQueue)
	}
}
