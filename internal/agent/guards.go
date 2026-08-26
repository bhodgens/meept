package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
)

// This file implements loop-economics leaf 07 "loop guards": three additional
// loop-safety mechanisms that compose with the existing cycle and convergence
// detectors in loop.go rather than forking them:
//
//  1. NoProgressLadder — normalized (key-sorted, whitespace-collapsed) hashing
//     of tool name + args catches near-identical-but-not-byte-identical calls
//     that the byte-level cycleDetector misses. Escalates ok -> warn -> veto.
//  2. SearchRollback — ring window of web_search arg hashes enabling pre-exec
//     duplicate detection with a free re-sample (iteration budget untouched).
//  3. ReasoningWatchdog — tracks reasoning-only streaks (reasoning tokens but
//     empty text, no tool calls) and forces a textual/tool answer.

// GuardConfig configures the loop guards. Zero values normalize to the
// defaults documented per-field; use Normalized() before reading.
type GuardConfig struct {
	// NoProgressWarnAt: consecutive identical normalized calls before a
	// system nudge is injected. Default 3.
	NoProgressWarnAt int `json:"no_progress_warn_at"`
	// NoProgressVetoAt: consecutive identical normalized calls before the
	// call is vetoed. Default 5.
	NoProgressVetoAt int `json:"no_progress_veto_at"`
	// GracefulAfterVetoes: consecutive vetoes after which the turn
	// terminates gracefully with a partial-results summary. Default 3.
	GracefulAfterVetoes int `json:"graceful_after_vetoes"`
	// DuplicateSearchRollback enables rollback of duplicate web_search
	// calls within the rollback window (ship-on default true).
	DuplicateSearchRollback bool `json:"duplicate_search_rollback"`
	// RollbackWindow is the ring size in turns for search-arg hashes.
	// Default 10.
	RollbackWindow int `json:"rollback_window"`
	// ReasoningTokenCap caps cumulative reasoning tokens within a streak.
	// Default 16384.
	ReasoningTokenCap int `json:"reasoning_token_cap"`
	// ReasoningStreakTurns is the number of consecutive reasoning-only
	// turns tolerated before breach. Default 3.
	ReasoningStreakTurns int `json:"reasoning_streak_turns"`
}

// Defaults for GuardConfig zero values.
const (
	DefaultNoProgressWarnAt     = 3
	DefaultNoProgressVetoAt     = 5
	DefaultGracefulAfterVetoes  = 3
	DefaultRollbackWindow       = 10
	DefaultReasoningTokenCap    = 16384
	DefaultReasoningStreakTurns = 3
)

// DefaultGuardConfig returns the ship-on guard defaults (user directive:
// every guard ships enabled; if one misfires we see it and fix it).
func DefaultGuardConfig() GuardConfig {
	return GuardConfig{
		NoProgressWarnAt:        DefaultNoProgressWarnAt,
		NoProgressVetoAt:        DefaultNoProgressVetoAt,
		GracefulAfterVetoes:     DefaultGracefulAfterVetoes,
		DuplicateSearchRollback: true,
		RollbackWindow:          DefaultRollbackWindow,
		ReasoningTokenCap:       DefaultReasoningTokenCap,
		ReasoningStreakTurns:    DefaultReasoningStreakTurns,
	}
}

// Normalized returns cfg with zero-values replaced by defaults. NOTE on
// DuplicateSearchRollback: because Go zero-values a bool to false and the
// ship-on default is true, an all-zero GuardConfig is interpreted as
// "entirely unconfigured" and Normalized returns the ship-on defaults.
// To explicitly DISABLE rollback, start from DefaultGuardConfig() and set
// the flag false (see TestGuards_DuplicateSearchRollback_DisabledFlag).
func (c GuardConfig) Normalized() GuardConfig {
	n := c
	if n.NoProgressWarnAt <= 0 {
		n.NoProgressWarnAt = DefaultNoProgressWarnAt
	}
	if n.NoProgressVetoAt <= 0 {
		n.NoProgressVetoAt = DefaultNoProgressVetoAt
	}
	if n.GracefulAfterVetoes <= 0 {
		n.GracefulAfterVetoes = DefaultGracefulAfterVetoes
	}
	if n.RollbackWindow <= 0 {
		n.RollbackWindow = DefaultRollbackWindow
	}
	if n.ReasoningTokenCap <= 0 {
		n.ReasoningTokenCap = DefaultReasoningTokenCap
	}
	if n.ReasoningStreakTurns <= 0 {
		n.ReasoningStreakTurns = DefaultReasoningStreakTurns
	}
	// Ship-on: an unconfigured (zero-value) config defaults rollback ON.
	// Explicit disable requires starting from DefaultGuardConfig().
	n.DuplicateSearchRollback = n.DuplicateSearchRollback || c == GuardConfig{}
	return n
}

// HashToolCall returns a normalized hash of tool name + arguments JSON.
// JSON args are canonicalized (keys sorted, whitespace collapsed) so that
// reordered or differently-spaced but semantically identical calls hash equal.
// Non-JSON args fall back to whitespace-collapsed raw comparison.
func HashToolCall(tool string, argsJSON string) string {
	return hashString(tool + "\x00" + normalizeArgsJSON(argsJSON))
}

// normalizeArgsJSON canonicalizes an arguments JSON string: parse, re-marshal
// with sorted keys (encoding/json sorts map keys), no extra whitespace.
// Falls back to strings.Fields collapse for non-JSON payloads.
func normalizeArgsJSON(argsJSON string) string {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" || trimmed == "{}" || trimmed == "null" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		// Not JSON: collapse whitespace so trivial formatting diffs match.
		return strings.Join(strings.Fields(trimmed), " ")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return strings.Join(strings.Fields(trimmed), " ")
	}
	return strings.TrimSpace(buf.String())
}

// GuardTrackResult is the ladder outcome for a tracked tool call.
type GuardTrackResult string

const (
	GuardOK   GuardTrackResult = "ok"
	GuardWarn GuardTrackResult = "warn"
	GuardVeto GuardTrackResult = "veto"
)

// NoProgressLadder tracks normalized hashes of recent tool calls and
// escalates through ok/warn/veto as identical calls repeat consecutively.
type NoProgressLadder struct {
	mu                sync.Mutex
	lastHash          string
	streak            int
	consecutiveVetoes int
}

// NewNoProgressLadder creates an empty ladder.
func NewNoProgressLadder() *NoProgressLadder {
	return &NoProgressLadder{}
}

// Track records a tool call (name + args JSON) and returns the ladder state
// after this call relative to warnAt/vetoAt thresholds.
func (l *NoProgressLadder) Track(tool string, argsJSON string, warnAt, vetoAt int) GuardTrackResult {
	h := HashToolCall(tool, argsJSON)
	l.mu.Lock()
	defer l.mu.Unlock()

	if h == l.lastHash {
		l.streak++
	} else {
		l.lastHash = h
		l.streak = 1
		l.consecutiveVetoes = 0
	}

	switch {
	case l.streak >= vetoAt:
		l.consecutiveVetoes++
		return GuardVeto
	case l.streak >= warnAt:
		return GuardWarn
	default:
		return GuardOK
	}
}

// ConsecutiveVetoes reports how many consecutive tracked calls have been
// vetoed. Any non-vetoed distinct call resets the count.
func (l *NoProgressLadder) ConsecutiveVetoes() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.consecutiveVetoes
}

// Reset clears ladder state (e.g. between turns).
func (l *NoProgressLadder) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastHash = ""
	l.streak = 0
	l.consecutiveVetoes = 0
}

// SearchRollback maintains a bounded ring of recent web_search argument
// hashes to support free re-sample on exact duplicates.
type SearchRollback struct {
	mu     sync.Mutex
	window int
	ring   []string // ordered oldest -> newest, len <= window
	inRing map[string]struct{}
}

// NewSearchRollback creates a rollback ring; a non-positive window normalizes
// to the default of 10.
func NewSearchRollback(window int) *SearchRollback {
	if window <= 0 {
		window = DefaultRollbackWindow
	}
	return &SearchRollback{
		window: window,
		ring:   make([]string, 0, window),
		inRing: make(map[string]struct{}, window),
	}
}

// Observe records a completed web_search call's arg hash. Observation is
// post-execution only: ShouldRollback must be consulted BEFORE Observe for
// the current candidate so the first-ever call never rolls back.
func (r *SearchRollback) Observe(hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.inRing[hash]; !dup {
		r.ring = append(r.ring, hash)
		r.inRing[hash] = struct{}{}
	}
	for len(r.ring) > r.window {
		old := r.ring[0]
		r.ring = r.ring[1:]
		delete(r.inRing, old)
	}
}

// ShouldRollback reports whether a pending web_search with this arg hash was
// already executed within the observation window.
func (r *SearchRollback) ShouldRollback(hash string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.inRing[hash]
	return ok
}

// ReasoningWatchdog counts consecutive assistant turns that produced
// reasoning tokens but neither visible text nor tool calls.
type ReasoningWatchdog struct {
	mu           sync.Mutex
	streak       int
	streakTokens int
}

// NewReasoningWatchdog creates an idle watchdog.
func NewReasoningWatchdog() *ReasoningWatchdog {
	return &ReasoningWatchdog{}
}

// RecordTurn records one assistant turn outcome. hasText means non-empty
// visible content; hasToolCalls means at least one tool call. Both reset the
// streak. reasoningTokens is the reasoning-token count for this turn.
func (w *ReasoningWatchdog) RecordTurn(hasText, hasToolCalls bool, reasoningTokens int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if hasText || hasToolCalls {
		w.streak = 0
		w.streakTokens = 0
		return
	}
	w.streak++
	w.streakTokens += reasoningTokens
}

// Streak returns the current consecutive reasoning-only turn count.
func (w *ReasoningWatchdog) Streak() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.streak
}

// Breach reports whether the watchdog limits are exceeded: either the
// consecutive-turn streak reached streakTurns, or cumulative reasoning
// tokens in the current streak reached tokenCap.
func (w *ReasoningWatchdog) Breach(tokenCap, streakTurns int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tokenCap > 0 && w.streakTokens >= tokenCap {
		return true
	}
	if streakTurns > 0 && w.streak >= streakTurns {
		return true
	}
	return false
}

// Reset clears watchdog state.
func (w *ReasoningWatchdog) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.streak = 0
	w.streakTokens = 0
}
