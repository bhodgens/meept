package voice

// C6 supervisor gate: no fact reaches the caller unapproved. Every draft
// sentence is checked against a fast endpoint (an employee via leaf 14's
// wiring, or a direct model ask) and only ALLOWed sentences are spoken.
// Every failure mode is fail-closed: timeout, transport error, quota block,
// and an unparseable reply all deny the sentence (the talk loop then speaks
// SupervisorConfig.FallbackLine and the call continues). A voice call is
// NEVER quota-parked and a quota error is never retried here - see
// internal/llm/AGENTS.md ("never short-retry a quota error", "quota is not
// a failure").

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/llm"
)

// Verdict is the outcome of one sentence check. The talk loop speaks the
// sentence when Allowed is true; otherwise it speaks SupervisorConfig's
// fallback line. FallbackLine itself never lives in this package's prompt.
type Verdict struct {
	Sentence string
	Allowed  bool
	Reason   string
}

// Supervisor checks draft sentences one at a time. Check is stateless and
// per-sentence so the talk loop can pipeline (check sentence N+1 while
// Telnyx plays N).
type Supervisor interface {
	Check(ctx context.Context, callID, sentence string) (Verdict, error)
}

// SupervisorChatter is the narrow model seam injected at wiring (leaf 14
// decides employee-vs-direct). It is a single fixed-shot question/answer.
type SupervisorChatter interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

// SupervisorConfig carries the leaf-01 config values. Timeout is the hard
// per-Check budget (voice.supervisor_timeout_ms); a zero/negative Timeout
// falls back to defaultSupervisorTimeout.
type SupervisorConfig struct {
	Enabled       bool
	Timeout       time.Duration
	FallbackLine  string
	NeverPatterns []string
}

const (
	defaultSupervisorTimeout = 400 * time.Millisecond
	// maxSentenceRunes caps a single sentence's length before checking;
	// maxSplitSentences caps how many sentences one draft expands to. Both
	// bound pathological drafts so a runaway generator cannot turn into an
	// unbounded Ask loop.
	maxSentenceRunes   = 280
	maxSplitSentences  = 16
	defaultDenyReason  = "deny"
	unparseableReason  = "deny: unparseable supervisor reply"
	neverPatternReason = "deny: never pattern"
	timeoutReason      = "timeout"
	quotaReason        = "quota"
	errorReason        = "error"
)

// supervisor is the default Supervisor implementation.
type supervisor struct {
	cfg     SupervisorConfig
	chatter SupervisorChatter
	logger  *slog.Logger

	// sentenceCache memoizes ALLOW verdicts so a repeat sentence (the
	// same fact restated across turns of one call, or across calls
	// sharing the supervisor) costs no second model ask. DENIES are
	// NEVER cached — fail-closed means a denial gets re-examined. A
	// short TTL bounds staleness (policy context moves). cacheMu guards
	// ONLY the map + LRU list swap: never held across Ask (no I/O under
	// a mutex, repo rule).
	cacheMu       sync.Mutex
	sentenceCache map[string]*list.Element
	cacheOrder    *list.List // front = most recently used/inserted
	cacheNow      func() time.Time

	// counters ride their own mutex (statsMu) so a stats read never
	// contends with a cache hit path. Posture: aggregate counts only —
	// the gate never logs per sentence (a voice call must not spam the
	// log at sentence rate); this is the observability substitute that
	// a per-call summary getter (Stats) exposes for wiring to poll.
	statsMu   sync.Mutex
	checks    int64 // Check calls that reached a verdict source (not ctx-cancel, not parent-err)
	denied    int64 // deny verdicts (never-pattern, model deny, error, quota — every fail-closed path)
	timeouts  int64 // timeout-class denies specifically
	cacheHits int64 // checks answered from the allow cache (zero model spend)
}

// cacheEntry is one memoized ALLOW.
type cacheEntry struct {
	sentence string // normalized key (also stored for eviction re-lookup)
	expires  time.Time
}

// SupervisorStats is the snapshot returned by (*supervisor).Stats.
type SupervisorStats struct {
	Checks    int64
	Denied    int64
	Timeouts  int64
	CacheHits int64
}

// supervisorCacheTTL is the allow-cache entry lifetime. A var (not a
// const) is the ms-tunable seam for tests, same posture as
// silenceWindowUnit in the talk loop; only supervisor cache tests mutate
// it, and package tests run sequentially.
var supervisorCacheTTL = 5 * time.Second

// supervisorCacheCap bounds the memo map; the least-recently-used entry
// is evicted when a NEW key would exceed it. 256 comfortably covers one
// call's distinct sentences (maxSplitSentences x a few turns) without
// unbounded growth across long-lived shared supervisors.
const supervisorCacheCap = 256

// NewSupervisor builds the gate. Construction logs ONCE when the gate is
// disabled; Check never logs per sentence (a voice call must not spam the
// log at sentence rate).
func NewSupervisor(cfg SupervisorConfig, chatter SupervisorChatter, logger *slog.Logger) *supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultSupervisorTimeout
	}
	s := &supervisor{
		cfg:           cfg,
		chatter:       chatter,
		logger:        logger,
		sentenceCache: make(map[string]*list.Element),
		cacheOrder:    list.New(),
		cacheNow:      time.Now,
	}
	if !cfg.Enabled {
		logger.Info("voice supervisor disabled: every sentence is allowed without a check")
	}
	return s
}

// Check runs the gate for one sentence. Contract:
//   - Enabled=false            -> Verdict{Allowed:true, Reason:"disabled"}, nil
//   - NeverPatterns match      -> deny, no model call
//   - timeout / error / quota  -> Verdict{Allowed:false, Reason:<class>}, nil
//   - unparseable reply        -> deny (fail-closed)
//   - parent ctx cancellation  -> the ctx error (transport-level cancel)
//
// The per-Check deadline is fresh each call (context.WithTimeout inside
// Check), never a shared construction-time deadline.
func (s *supervisor) Check(ctx context.Context, callID, sentence string) (Verdict, error) {
	if err := ctx.Err(); err != nil {
		return Verdict{Sentence: sentence}, err
	}
	if !s.cfg.Enabled {
		return Verdict{Sentence: sentence, Allowed: true, Reason: "disabled"}, nil
	}
	s.countCheck()
	// NeverPatterns pre-check: a machine-checked denial spends no model call
	// and bypasses the allow cache (policy beats memoization; and denies
	// are never cached, so a hit here only shadows a stale allow).
	lower := strings.ToLower(sentence)
	for _, p := range s.cfg.NeverPatterns {
		if p != "" && strings.Contains(lower, strings.ToLower(p)) {
			s.countDeny()
			return Verdict{Sentence: sentence, Allowed: false, Reason: neverPatternReason}, nil
		}
	}
	// Allow cache: a repeat sentence the model already approved within the
	// TTL costs zero model asks (repeated facts across turns are common —
	// confirmations, restatements). Only successful ALLOW verdicts land in
	// the cache (see countAllow), so a hit can never resurrect a denial.
	if key := s.cacheLookupKey(sentence); s.cacheHit(key) {
		s.countCacheHit()
		return Verdict{Sentence: sentence, Allowed: true, Reason: "cached"}, nil
	}

	// Hard per-Check budget. Ask runs on a child of the caller's ctx with
	// our deadline; a chatter that ignores its ctx (or a genuinely stuck
	// endpoint) cannot extend Check past the budget - the select abandons
	// the wait at deadline/cancel and leaves the in-flight Ask to finish
	// (or die with the ctx) on its own goroutine. Check stays bounded and
	// the call never hangs on a provider wait.
	askCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	type askResult struct {
		reply string
		err   error
	}
	ch := make(chan askResult, 1) // buffered: the abandoned Ask never blocks
	go func() {
		reply, err := s.chatter.Ask(askCtx, supervisorPrompt(sentence))
		ch <- askResult{reply: reply, err: err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			// Parent cancellation (call hung up / shut down) is
			// transport-level: surface it. Our OWN deadline firing is a
			// timeout verdict.
			if parentErr := ctx.Err(); parentErr != nil {
				return Verdict{Sentence: sentence}, parentErr
			}
			// Quota is a timeout-class outcome: fallback verdict, no park,
			// no retry (internal/llm/AGENTS.md). Classify via real sentinels.
			var qe *llm.QuotaResetError
			if errors.As(res.err, &qe) || errors.Is(res.err, llm.ErrAllModelsQuotaBlocked) {
				s.countDeny()
				return Verdict{Sentence: sentence, Allowed: false, Reason: quotaReason}, nil
			}
			s.countDeny()
			return Verdict{Sentence: sentence, Allowed: false, Reason: errorReason}, nil
		}
		allowed := parseAllowed(res.reply)
		if allowed {
			// Only a SUCCESSFUL model ALLOW is memoized (never an
			// "disabled" fast path, a timeout, or a deny).
			s.cacheStore(s.cacheLookupKey(sentence))
		} else {
			s.countDeny()
		}
		return Verdict{Sentence: sentence, Allowed: allowed, Reason: parseReason(res.reply)}, nil
	case <-askCtx.Done():
		// Our deadline (or the parent) expired first: fail closed as
		// timeout. The abandoned Ask goroutine exits on its own via
		// askCtx cancellation; the buffered channel means it never leaks
		// blocked on the send.
		s.countTimeout()
		return Verdict{Sentence: sentence, Allowed: false, Reason: timeoutReason}, nil
	}
}

// cacheLookupKey normalizes a sentence for cache identity: trimmed,
// inner whitespace collapsed, lower-cased. Whitespace/case variants of the
// same approved sentence hit the same entry (the ask prompt embeds the raw
// sentence, but its verdict is not sensitive to those differences).
func (s *supervisor) cacheLookupKey(sentence string) string {
	return strings.ToLower(strings.Join(strings.Fields(sentence), " "))
}

// cacheHit reports whether key holds a live ALLOW entry, refreshing its
// recency on a hit and dropping it when the TTL expired.
func (s *supervisor) cacheHit(key string) bool {
	if key == "" {
		return false
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	el, ok := s.sentenceCache[key]
	if !ok {
		return false
	}
	entry := el.Value.(*cacheEntry)
	if s.cacheNow().After(entry.expires) {
		s.cacheRemoveLocked(el)
		return false
	}
	s.cacheOrder.MoveToFront(el)
	return true
}

// cacheStore memoizes an ALLOW under key, evicting the least-recently-used
// (or already-expired) entries when the cache is at capacity. An expired
// sweep runs only at the cap, keeping the hot path O(1).
func (s *supervisor) cacheStore(key string) {
	if key == "" {
		return
	}
	now := s.cacheNow()
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if el, ok := s.sentenceCache[key]; ok {
		el.Value.(*cacheEntry).expires = now.Add(supervisorCacheTTL)
		s.cacheOrder.MoveToFront(el)
		return
	}
	for s.cacheOrder.Len() >= supervisorCacheCap {
		// Evict LRU (back). The entry itself may be expired — same
		// eviction shape either way.
		back := s.cacheOrder.Back()
		if back == nil {
			break
		}
		s.cacheRemoveLocked(back)
	}
	el := s.cacheOrder.PushFront(&cacheEntry{sentence: key, expires: now.Add(supervisorCacheTTL)})
	s.sentenceCache[key] = el
}

// cacheRemoveLocked drops one entry (caller holds cacheMu).
func (s *supervisor) cacheRemoveLocked(el *list.Element) {
	s.cacheOrder.Remove(el)
	delete(s.sentenceCache, el.Value.(*cacheEntry).sentence)
}

// countCheck records one gate-reaching Check (excludes the disabled fast
// path and a parent-cancel early return — those never cost or classify).
func (s *supervisor) countCheck() {
	s.statsMu.Lock()
	s.checks++
	s.statsMu.Unlock()
}

func (s *supervisor) countDeny() {
	s.statsMu.Lock()
	s.denied++
	s.statsMu.Unlock()
}

func (s *supervisor) countTimeout() {
	s.statsMu.Lock()
	s.timeouts++
	s.denied++
	s.statsMu.Unlock()
}

func (s *supervisor) countCacheHit() {
	s.statsMu.Lock()
	s.cacheHits++
	s.statsMu.Unlock()
}

// Stats returns a snapshot of the gate counters. Wiring/report surfaces
// read this instead of per-sentence logs (the never-log-at-sentence-rate
// posture stays intact). Intentionally NOT on the Supervisor interface —
// the talk loop treats the gate as just a Supervisor.
func (s *supervisor) Stats() SupervisorStats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return SupervisorStats{
		Checks:    s.checks,
		Denied:    s.denied,
		Timeouts:  s.timeouts,
		CacheHits: s.cacheHits,
	}
}

// cacheLen reports the live entry count (tests only).
func (s *supervisor) cacheLen() int {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return len(s.sentenceCache)
}

// supervisorPrompt is the fixed, short ask: one verdict word plus a reason
// capped at 100 runes. The fallback line is deliberately NOT in the prompt
// (leaf note: the model must not learn to trigger it).
func supervisorPrompt(sentence string) string {
	return fmt.Sprintf(
		"You are a voice-call safety supervisor. Policy is in force for this call. "+
			"Decide whether the assistant may SPEAK the sentence below. "+
			"Answer exactly one line: \"ALLOW\" or \"DENY: <reason up to 100 runes>\". "+
			"Sentence: %q", sentence)
}

// parseAllowed tolerantly reads ALLOW/DENY prefixes; anything else denies
// (fail-closed).
func parseAllowed(reply string) bool {
	switch {
	case strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reply)), "ALLOW"):
		return true
	default:
		return false
	}
}

// parseReason extracts the deny detail for the verdict. Deny and unparseable
// reasons both start with "deny" so logs filter on one class.
func parseReason(reply string) string {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return unparseableReason
	}
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "ALLOW") {
		return "allow"
	}
	if rest, ok := strings.CutPrefix(trimmed, "DENY:"); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return defaultDenyReason
		}
		if n := utf8.RuneCountInString(rest); n > 100 {
			r := []rune(rest)
			rest = string(r[:100])
		}
		return "deny: " + rest
	}
	if strings.HasPrefix(upper, "DENY") {
		return defaultDenyReason
	}
	return unparseableReason
}

// SplitSentences splits a draft on sentence boundaries (. ! ? newline),
// keeping the terminator with its sentence and dropping empties. Each
// sentence is capped at maxSentenceRunes and a draft yields at most
// maxSplitSentences sentences, in order.
func SplitSentences(draft string) []string {
	out := make([]string, 0, 4)
	var current strings.Builder
	flush := func() {
		s := strings.TrimSpace(current.String())
		current.Reset()
		if s == "" {
			return
		}
		if utf8.RuneCountInString(s) > maxSentenceRunes {
			r := []rune(s)
			s = string(r[:maxSentenceRunes])
		}
		out = append(out, s)
	}
	for _, r := range draft {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			current.WriteRune(r)
			flush()
			if len(out) >= maxSplitSentences {
				return out
			}
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return out
}
