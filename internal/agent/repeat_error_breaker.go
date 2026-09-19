package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// maxIdenticalToolErrors is the per-key failure budget (tool-boundary-hardening
// leaf 02): once the same (tool, canonical-args, error-first-line) triple has
// failed this many times in one logical work scope, further identical calls
// are refused WITHOUT executing the tool and the step/plan turn terminalizes
// with the honest summary. 3 mirrors the cycle detector's corrective threshold.
const maxIdenticalToolErrors = 3

// repeatErrorBreaker is a turn-scoped circuit breaker for the
// repeat-identical-error failure mode (2026-09-18 e2e: 45+ identical
// task_create{} calls across 11+ abort→replan→fresh-turn rounds; the cycle
// detector reset each generation so every fresh generation re-ran the doomed
// call).
//
// Key = tool name + "|" + sha256(canonicalJSON(args)) + "|" + firstLine(error).
// The error first line is part of the key deliberately: a CHANGED error text
// means the model (or the world) changed something, so the new triple earns a
// fresh budget and only the dead triple is refused.
//
// SCOPE / LIFETIME (the load-bearing design decision, verified — see
// repeat_error_breaker_loop_test.go TestBreaker_ScopeSurvivesReplanCycle):
// the breaker lives on the AgentLoop as `repeatErr` and is constructed in
// NewAgentLoop (loop.go, next to loop.toolBreaker). It is deliberately NOT in
// resetTurnGuards — the escalation→replan path (orchestrator.go
// handlePlanRequest → strategic.ReplanFailedTask → Plan → planSinglePhase, and
// the executor job path re-entering the SAME task-scoped loop via
// registry.GetForTask(agentID, taskID), daemon/components.go:8018) reuses the
// same loop instance across replan attempts, while resetTurnGuards wipes the
// within-turn guards (cycle detector, no-progress ladder) at every turn
// boundary. Keeping the breaker out of that reset is exactly what makes its
// memory survive the abort→escalation→replan cycle: only a NEW loop instance
// (new session via Manager.GetOrCreateWired, or a fresh GetForTask loop after
// ReleaseTaskLoops) starts with a clean breaker.
//
// Thread-safe: tool calls may run concurrently (ExecuteAll parallel groups).
type repeatErrorBreaker struct {
	mu sync.Mutex
	// counts maps full key (tool|argsHash|errLine) -> failure count.
	counts map[string]int
	// dead maps pair key (tool|argsHash) -> the terminal summary of the
	// first exhausted error variant. Allow/Refusal consult the pair key so
	// the pre-call check (which cannot know the error line) refuses any
	// error variant whose triple already burned its budget.
	dead map[string]string
}

func newRepeatErrorBreaker() *repeatErrorBreaker {
	return &repeatErrorBreaker{
		counts: make(map[string]int),
		dead:   make(map[string]string),
	}
}

// repeatErrorArgsHash hashes args canonically: json.Marshal on map[string]any
// sorts keys at every nesting level (verified by
// TestBreaker_CanonicalArgsHash), so it IS the order-insensitive
// canonicalizer; sha256 fixes the digest. Non-serializable args fall back to
// the deterministic fmt form so key building never fails.
func repeatErrorArgsHash(args map[string]any) string {
	if len(args) == 0 {
		args = map[string]any{} // nil and empty hash identically
	}
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte(fmt.Sprint(args))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// repeatErrKey is the identity of a distinct failing triple.
func repeatErrKey(tool, argsHash, errFirstLine string) string {
	return tool + "|" + argsHash + "|" + errFirstLine
}

// firstErrorLine returns the first non-empty line of the error text (same
// semantics as progress_synthesizer.firstLine; kept local so the breaker is
// self-contained).
func firstErrorLine(errMsg string) string {
	for _, line := range strings.Split(errMsg, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// repeatErrPairKey is the pre-call identity: the loop's Allow check runs
// before execution and therefore before any error exists, so it can only
// match on (tool, args). A pair is dead once ANY of its error variants
// exhausted its budget.
func repeatErrPairKey(tool, argsHash string) string {
	return tool + "|" + argsHash
}

// Allow is the pre-call check: false when the (tool, args) pair is dead —
// i.e. some error variant of this exact input already failed
// maxIdenticalToolErrors times (the call must NOT reach the tool).
func (b *repeatErrorBreaker) Allow(tool, argsHash string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, dead := b.dead[repeatErrPairKey(tool, argsHash)]
	return !dead
}

// Observe records one failed call. Returns exhausted=true when this exact
// key has now failed maxIdenticalToolErrors times; summary is the honest
// terminal message for the step/plan turn:
//
//	"tool <name> rejected the identical input N times (<firstErr>); giving up"
//
// Once exhausted, the pair is pinned dead (Allow refuses it) until Reset.
func (b *repeatErrorBreaker) Observe(tool, argsHash, errMsg string) (exhausted bool, summary string) {
	errLine := firstErrorLine(errMsg)
	key := repeatErrKey(tool, argsHash, errLine)
	pair := repeatErrPairKey(tool, argsHash)

	b.mu.Lock()
	if b.counts == nil {
		b.counts = make(map[string]int)
		b.dead = make(map[string]string)
	}
	// Already dead: exhausted is a one-time TRANSITION signal (the caller
	// terminalizes on it); the pre-call Allow check owns refusal afterwards.
	if existing, dead := b.dead[pair]; dead {
		b.mu.Unlock()
		return false, existing
	}
	b.counts[key]++
	count := b.counts[key]
	if count >= maxIdenticalToolErrors {
		summary = fmt.Sprintf(
			"tool %s rejected the identical input %d times (%s); giving up",
			tool, count, errLine)
		b.dead[pair] = summary
	}
	b.mu.Unlock()
	return count >= maxIdenticalToolErrors, summary
}

// Refusal returns the terminal summary stored for the dead (tool, args) pair,
// or "" when the pair is not dead. Used by the loop's refusal path to surface
// the same honest message the exhaustion produced.
func (b *repeatErrorBreaker) Refusal(tool, argsHash string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dead[repeatErrPairKey(tool, argsHash)]
}

// Reset clears all state (new step / new plan conversation). Deliberately NOT
// called by resetTurnGuards — see the struct comment for the lifetime proof.
func (b *repeatErrorBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts = make(map[string]int)
	b.dead = make(map[string]string)
}
