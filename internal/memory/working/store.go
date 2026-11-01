// Package working implements the WORKING memory tier: a bounded, in-memory
// ring of recent conversation turns (contract C4 of the
// 20261031-context-memory-engine plan).
//
// Design rules pinned by the parent plan:
//   - Append does NO I/O (memory + eviction only); Flush is the only I/O.
//   - Recall is concurrency-safe (RWMutex) and never runs consolidation.
//   - Token accounting goes through an injected TokenCounter; tests use a
//     deterministic chars/4 stub (see store_test.go).
//   - Eviction order: TTL-expired first, then oldest (approximate LRU by
//     Turn.At). Evicted turns are queued in-memory for Flush, never
//     silently dropped (never-delete eviction, findings-memory-tiers.md
//     P4/R-B1); the queue itself is bounded with drop-oldest + counter.
//
// Ring sizing defaults follow R-B1: MaxTokens primary (8192 tokens, about
// 10% of a 64K-128K window), MaxItems hard backstop (200 turns), TTL for
// session scoping (24h, Memini uses 72h at conversation scale).
//
// All of this ships opt-in: the store does nothing unless wired. Config
// plumbing (policy knobs, Enabled flag) lands in leaf 02; this package
// owns only the plain Config struct and its defaults.
//
// ASCII only. No config package imports; no internal/agent imports; no
// second Consolidator.
package working

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Ring sizing defaults (findings-memory-tiers.md R-B1).
const (
	// DefaultMaxTokens is the primary token bound for the working ring
	// (~10% of a 64K-128K context window; Letta caps core blocks at
	// 2000-5000 characters each, 5-15 blocks).
	DefaultMaxTokens = 8192
	// DefaultMaxItems is the hard item-count backstop for the ring.
	DefaultMaxItems = 200
	// DefaultTTL session-scopes the ring (Memini working TTL is 72h at
	// conversation scale; meept picks the tighter 24h session shape).
	DefaultTTL = 24 * time.Hour
)

// Config configures the working-tier ring store. It is a plain struct on
// purpose: leaf 02 owns the config-schema plumbing; this package never
// imports internal/config.
type Config struct {
	// MaxTokens is the primary token budget for the live ring. Must be > 0.
	MaxTokens int
	// MaxItems is the hard backstop on live turn count. Must be > 0.
	MaxItems int
	// TTL is the age at which a turn expires from the live ring and is
	// queued for Flush. 0 (or negative) disables TTL expiry. Must be >= 0.
	TTL time.Duration
}

// Turn is one conversation turn held by the working tier. Turns are NOT
// memory.Memory records (contract C4): they are a distinct struct and are
// never forced into a MemoryType.
type Turn struct {
	// ID is the caller-provided unique turn identifier. This package never
	// generates IDs (pkg/id or caller-owned IDs only).
	ID string
	// SessionID scopes the turn to its session; recorded for the Flush
	// adapter, not used as a Recall/Snapshot filter (whole-store semantics).
	SessionID string
	// Role is the speaker role (user/assistant/tool/...).
	Role string
	// Content is the turn text.
	Content string
	// At is the turn timestamp; eviction order is approximate LRU by At.
	At time.Time
}

// TokenCounter counts tokens deterministically for budget accounting.
// Matches the shape of internal/llm.Tokenizer.
type TokenCounter interface {
	CountTokens(string) int
}

// EpisodicSink receives evicted turns so nothing is silently dropped
// (never-delete eviction, P4). Flush drains the internal queue into it.
type EpisodicSink interface {
	AppendTurns(ctx context.Context, turns []Turn) error
}

// Store is the working-tier conversation buffer: a token-bounded ring of
// recent turns with eviction-to-episodic flush and concurrency-safe recall.
// Safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	cfg     Config
	tk      TokenCounter
	now     func() time.Time
	turns   []Turn // live ring, append-ordered
	tokens  int    // total CountTokens(Content) over live turns
	queue   []Turn // evicted turns awaiting Flush (eviction order)
	dropped int    // turns dropped from a full queue (sink never flushed)
	flushMu sync.Mutex
}

// New creates a working-tier store. Validation: MaxTokens > 0, MaxItems > 0,
// TTL >= 0. tk must be non-nil (typed-nil guarded).
func New(cfg Config, tk TokenCounter) (*Store, error) {
	if cfg.MaxTokens <= 0 {
		return nil, fmt.Errorf("working: MaxTokens must be > 0, got %d", cfg.MaxTokens)
	}
	if cfg.MaxItems <= 0 {
		return nil, fmt.Errorf("working: MaxItems must be > 0, got %d", cfg.MaxItems)
	}
	if cfg.TTL < 0 {
		return nil, fmt.Errorf("working: TTL must be >= 0, got %s", cfg.TTL)
	}
	if isNilValue(tk) {
		return nil, errors.New("working: TokenCounter must not be nil")
	}
	return &Store{
		cfg:   cfg,
		tk:    tk,
		now:   time.Now,
		turns: make([]Turn, 0, 16),
		queue: make([]Turn, 0, 16),
	}, nil
}

// SetClock injects the wall-clock source (for deterministic TTL tests).
// Nil and typed-nil functions are ignored (guard per repo setter rule).
func (s *Store) SetClock(now func() time.Time) {
	if isNilValue(now) {
		return
	}
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

// Append adds a turn to the live ring. It performs NO I/O: over-budget
// overflow is handled by in-memory eviction only, and evicted turns are
// queued for Flush (the episodic demotion path, not deletion).
//
// Eviction order: TTL-expired turns first, then the oldest turn by Turn.At
// (approximate LRU), until the live ring fits MaxTokens and MaxItems. A
// single turn that alone exceeds MaxTokens is kept as the whole live set
// rather than dropped (the newest turn is never evicted into the void; it
// is still offered to the sink by the next overflow).
func (s *Store) Append(_ context.Context, turn Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowFn := s.now
	now := nowFn()
	s.turns = append(s.turns, turn)
	s.tokens += s.tk.CountTokens(turn.Content)

	// TTL expiry first (R-B1: TTL session-scopes the ring; it is not the
	// eviction policy for budget pressure).
	if s.cfg.TTL > 0 {
		cutoff := now.Add(-s.cfg.TTL)
		kept := make([]Turn, 0, len(s.turns))
		for _, t := range s.turns {
			if t.At.Before(cutoff) {
				s.enqueueEvicted(t)
				s.tokens -= s.tk.CountTokens(t.Content)
			} else {
				kept = append(kept, t)
			}
		}
		s.turns = kept
	}

	// Token budget is primary (P3: token triggers for prompt-reaching
	// tiers); MaxItems is the hard backstop. Evict oldest by At. The last
	// remaining turn is never evicted here (see Append doc comment).
	for (s.tokens > s.cfg.MaxTokens || len(s.turns) > s.cfg.MaxItems) && len(s.turns) > 1 {
		idx := oldestIndex(s.turns)
		evicted := s.turns[idx]
		s.turns = append(s.turns[:idx], s.turns[idx+1:]...)
		s.tokens -= s.tk.CountTokens(evicted.Content)
		s.enqueueEvicted(evicted)
	}
	return nil
}

// Recall answers a recall query over the live ring: term-overlap scoring,
// newest-first tiebreak, top `limit` contents. An empty (or term-less)
// query returns the newest `limit` turns. The signature is
// RecallFunc-compatible (C1 seam): usable directly as a TierBudget Recall.
func (s *Store) Recall(_ context.Context, query string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	s.mu.RLock()
	turns := make([]Turn, len(s.turns))
	copy(turns, s.turns)
	s.mu.RUnlock()

	if len(turns) == 0 {
		return nil
	}

	terms := tokenize(query)
	if len(terms) == 0 {
		// Empty query: newest first.
		sort.SliceStable(turns, func(i, j int) bool { return turns[i].At.After(turns[j].At) })
		out := make([]string, 0, min(limit, len(turns)))
		for _, t := range turns {
			if len(out) == limit {
				break
			}
			out = append(out, t.Content)
		}
		return out
	}

	type scored struct {
		content string
		at      time.Time
		score   int
	}
	cands := make([]scored, 0, len(turns))
	for _, t := range turns {
		counts := make(map[string]int)
		for _, w := range tokenize(t.Content) {
			counts[w]++
		}
		sc := 0
		for _, q := range terms {
			sc += counts[q]
		}
		cands = append(cands, scored{content: t.Content, at: t.At, score: sc})
	}
	// Relevant turns first; recency breaks ties (approximate LRU spirit).
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].at.After(cands[j].at)
	})
	out := make([]string, 0, min(limit, len(cands)))
	for _, c := range cands {
		if len(out) == limit {
			break
		}
		out = append(out, c.content)
	}
	return out
}

// Snapshot returns a read-only copy of the live ring in append order.
// Mutating the returned slice or its turns does not affect the store.
func (s *Store) Snapshot() []Turn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Turn, len(s.turns))
	copy(out, s.turns)
	return out
}

// Flush drains the eviction queue into dst in eviction order. It is the
// only I/O path in this package. Idempotent when the queue is empty (zero
// turns sent, nil error). On a sink error the batch is restored to the
// front of the queue, so a later retry re-sends the same turns in order.
// Concurrent Flush calls are serialized.
func (s *Store) Flush(ctx context.Context, dst EpisodicSink) error {
	if isNilValue(dst) {
		return errors.New("working: Flush sink must not be nil")
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := make([]Turn, len(s.queue))
	copy(batch, s.queue)
	s.queue = s.queue[:0]
	s.mu.Unlock()

	if err := dst.AppendTurns(ctx, batch); err != nil {
		s.mu.Lock()
		s.queue = append(batch, s.queue...)
		s.mu.Unlock()
		return fmt.Errorf("working: flush %d evicted turns: %w", len(batch), err)
	}
	return nil
}

// LiveTokens reports the total token load of the live ring.
func (s *Store) LiveTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokens
}

// QueuedTurns reports how many evicted turns await Flush.
func (s *Store) QueuedTurns() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.queue)
}

// Dropped reports how many evicted turns were dropped from a full queue
// (drop-oldest counter; the sink never flushed fast enough). Dropped turns
// are still present in the episodic store only if a previous Flush carried
// them; this counter exists to make silent loss visible, not silent.
func (s *Store) Dropped() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dropped
}

// enqueueEvicted queues an evicted turn for Flush. Caller holds s.mu.
// The queue is bounded at MaxItems with drop-oldest; drops are counted
// (never-delete eviction P4: the queue is a pressure valve, the counter
// makes any loss observable).
func (s *Store) enqueueEvicted(t Turn) {
	s.queue = append(s.queue, t)
	for len(s.queue) > s.cfg.MaxItems {
		s.queue = s.queue[1:]
		s.dropped++
	}
}

// oldestIndex returns the index of the turn with the smallest At
// (approximate LRU by timestamp). Caller holds s.mu.
func oldestIndex(turns []Turn) int {
	idx := 0
	for i := 1; i < len(turns); i++ {
		if turns[i].At.Before(turns[idx].At) {
			idx = i
		}
	}
	return idx
}

// tokenize lowercases and splits text into alphanumeric terms (ASCII
// semantics; ASCII-only source per plan rules).
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// isNilValue guards both untyped nil and typed-nil interface values
// (repo typed-nil guard rule).
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}
