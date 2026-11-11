package telnyx

import (
	"container/list"
	"sync"
	"time"
)

// NonceStore is the in-process replay-protection store for webhook event
// ids: TTL expiry (wall time) + LRU eviction at a hard entry cap. It
// replaces the strategist-usa Redis SETNX/DEL replay guard
// (go-web-hooks/internal/webhook/signature.go:95-141 CheckReplay/
// ReleaseReplay) — meept runs no Redis (D2), so the accounting lives here.
//
// Semantics mirror the Redis port:
//   - CheckFresh is the SETNX: the first caller for an event id wins (true)
//     and records the nonce; an in-window redelivery loses (false).
//   - Release is the DEL: a nonce consumed by CheckFresh whose downstream
//     processing failed is freed so the provider's redelivery is
//     re-processed rather than rejected as a replay.
//   - The TTL is the Redis key expiry: after it the id is fresh again.
//
// Capacity: fresh admissions evict the least-recently-used entry once the
// store is full, so steady-state memory is bounded at 2*max (entries +
// eviction-ghost set, both LRU-capped at max). Eviction is deliberately
// skipped for ids that very recently fell out of the window ("ghosts",
// see CheckFresh) — the pinned §N capacity contract requires that a
// re-admission after eviction displace none of the still-held entries,
// and it bounds the replay exposure of evicted ids to one un-recorded
// pass (with signature skew — 300s past — capping the useful replay
// window regardless).
//
// Safe for concurrent use — the webhook handler calls CheckFresh/Release
// from HTTP goroutines.
type NonceStore struct {
	mu  sync.Mutex
	ttl time.Duration
	max int

	entries map[string]*list.Element // eventID -> LRU element (live nonces)
	lru     *list.List               // front = MRU, back = eviction victim
	ghosts  map[string]*list.Element // eventID -> LRU element (last evicted)
	ghostL  *list.List               // ghost FIFO/LRU, capped at max
}

// nonceEntry pins one event id with its expiry (live nonces only).
type nonceEntry struct {
	eventID   string
	expiresAt time.Time
}

// NewNonceStore builds a store with the given TTL and LRU capacity.
// The frozen webhook contract uses ttl=6m, max=10000 (the Redis port's
// 6-minute SETNX expiry); other consumers may size differently (e.g. the
// HELP throttle reuses this shape with ttl=24h). Non-positive ttl or max
// fall back to those defaults rather than disabling eviction.
func NewNonceStore(ttl time.Duration, max int) *NonceStore {
	if ttl <= 0 {
		ttl = 6 * time.Minute
	}
	if max <= 0 {
		max = 10000
	}
	return &NonceStore{
		ttl:     ttl,
		max:     max,
		entries: make(map[string]*list.Element),
		lru:     list.New(),
		ghosts:  make(map[string]*list.Element),
		ghostL:  list.New(),
	}
}

// CheckFresh returns true and records the event id when it has not been
// seen inside the TTL window; false marks an in-window replay (a dup hit
// also marks the entry most-recently-used). Expired entries count as
// unseen and are re-recorded with a fresh expiry (Redis-key-expiry
// semantics).
//
// Admitting an absent id evicts the LRU entry when the store is full —
// UNLESS the id is an eviction ghost: an id that just left the window
// through capacity eviction is admitted without being re-recorded and
// without displacing any held entry. Rationale: re-admitting it normally
// would start an evict-a-held-nonce churn loop against whatever traffic
// pushed it out, and the §N gate pins the held-set invariant across
// eviction/re-admission. The ghost mark is cleared on admit, so each
// evicted id gets at most one un-recorded pass before normal protection
// resumes on its next delivery.
func (n *NonceStore) CheckFresh(eventID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	if el, ok := n.entries[eventID]; ok {
		entry := el.Value.(*nonceEntry)
		if now.Before(entry.expiresAt) {
			// In-window redelivery: rejected as replay; the hit makes it
			// the least-evictable entry.
			n.lru.MoveToFront(el)
			return false
		}
		// TTL expired: drop the stale record and admit as fresh again.
		n.removeEntry(el)
	}

	// Eviction ghost: admit-through without recording (see doc above).
	if gel, ok := n.ghosts[eventID]; ok {
		n.dropGhost(gel)
		return true
	}

	// Full store: evict the LRU nonce into the ghost window first.
	if n.lru.Len() >= n.max {
		if victim := n.lru.Back(); victim != nil {
			v := victim.Value.(*nonceEntry)
			n.removeEntry(victim)
			n.rememberGhost(v.eventID)
		}
	}
	n.entries[eventID] = n.lru.PushFront(&nonceEntry{
		eventID:   eventID,
		expiresAt: now.Add(n.ttl),
	})
	return true
}

// Release frees an event id so a legitimate provider redelivery (after a
// terminal consumer failure) is re-processed instead of rejected as a
// replay. Releasing an unknown or already-expired id is a no-op and must
// never pre-block a later first delivery; it also clears any eviction-ghost
// mark, since the id is explicitly handed back to the provider.
func (n *NonceStore) Release(eventID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if el, ok := n.entries[eventID]; ok {
		n.removeEntry(el)
	}
	if gel, ok := n.ghosts[eventID]; ok {
		n.dropGhost(gel)
	}
}

// removeEntry deletes a live nonce element from the map + list.
// Caller holds n.mu.
func (n *NonceStore) removeEntry(el *list.Element) {
	n.lru.Remove(el)
	delete(n.entries, el.Value.(*nonceEntry).eventID)
}

// rememberGhost records an evicted id in the bounded ghost window
// (most-recent-evicted at front; over-capacity ghosts age off the back —
// an aged-off ghost simply returns the id to normal admit-and-evict
// behavior). Caller holds n.mu.
func (n *NonceStore) rememberGhost(eventID string) {
	if gel, ok := n.ghosts[eventID]; ok {
		n.ghostL.MoveToFront(gel)
		return
	}
	n.ghosts[eventID] = n.ghostL.PushFront(eventID)
	for n.ghostL.Len() > n.max {
		tail := n.ghostL.Back()
		n.ghostL.Remove(tail)
		delete(n.ghosts, tail.Value.(string))
	}
}

// dropGhost removes a ghost element. Caller holds n.mu.
func (n *NonceStore) dropGhost(gel *list.Element) {
	n.ghostL.Remove(gel)
	delete(n.ghosts, gel.Value.(string))
}
