package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/caimlas/meept/internal/tools"
)

// ToolRetryBreaker is a circuit breaker for identical repeated tool failures
// (harness-eval leaf 05, book ch1 "Correct" layer).
//
// Key = tool name + canonical JSON of args (sorted keys). Identical
// consecutive failures increment the counter; a success resets it. At
// WarnAt consecutive failures the breaker logs once per key; at VetoAt
// Observe returns veto=true so the caller skips the retry and injects a
// system note instead of burning iterations.
//
// The zero value is usable: WarnAt<=0 becomes 3, VetoAt becomes 5.
// Thread-safe: tool calls may run concurrently.
type ToolRetryBreaker struct {
	WarnAt int
	VetoAt int

	mu     sync.Mutex
	counts map[string]int
	warned map[string]bool
}

// NewToolRetryBreaker returns a breaker with default thresholds (warn 3, veto 5).
func NewToolRetryBreaker() *ToolRetryBreaker {
	return &ToolRetryBreaker{WarnAt: 3, VetoAt: 5}
}

// thresholds applies zero-value defaults.
func (b *ToolRetryBreaker) thresholds() (warn, veto int) {
	warn, veto = b.WarnAt, b.VetoAt
	if warn <= 0 {
		warn = 3
	}
	if veto <= 0 {
		veto = 5
	}
	return warn, veto
}

// canonicalArgs renders args as canonical JSON with sorted keys. Nil/empty
// args map to "{}". Non-serializable values fall back to fmt.Sprint so key
// building never fails.
func canonicalArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		// json.Marshal on map[string]any sorts keys already; on the rare
		// unserializable value, fall back to a deterministic text form.
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var s string
		for _, k := range keys {
			s += k + "=" + fmt.Sprint(args[k]) + ";"
		}
		return s
	}
	return string(b)
}

// breakerKey is the identity of a distinct failing call: tool name + args.
func breakerKey(name string, args map[string]any) string {
	return name + "|" + canonicalArgs(args)
}

// Observe records one tool outcome. failed=true increments the key's
// consecutive-failure counter and returns veto=true once it reaches VetoAt
// (a one-time warn logs at WarnAt). failed=false resets the key.
func (b *ToolRetryBreaker) Observe(name string, args map[string]any, failed bool) bool {
	warnAt, vetoAt := b.thresholds()

	b.mu.Lock()
	if b.counts == nil {
		b.counts = make(map[string]int)
		b.warned = make(map[string]bool)
	}
	key := breakerKey(name, args)
	if !failed {
		delete(b.counts, key)
		delete(b.warned, key)
		b.mu.Unlock()
		return false
	}
	b.counts[key]++
	count := b.counts[key]
	shouldWarn := count >= warnAt && !b.warned[key]
	if shouldWarn {
		b.warned[key] = true
	}
	veto := count >= vetoAt
	b.mu.Unlock()

	if shouldWarn {
		slog.Warn("tool retry breaker: repeated identical tool failure",
			"tool", name, "consecutive_failures", count, "veto_at", vetoAt)
	}
	return veto
}

// VerifyFromToolResults decides task success from STRUCTURED tool data only
// (book ch1 "Verify" input-isolation principle): exit codes, success flags,
// and error fields. It never reads assistant-generated prose, so a model
// claiming "tests passed" cannot fabricate a pass.
//
// results must be non-empty; empty input fails closed.
func VerifyFromToolResults(results []tools.ToolResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if !r.Success {
			return false
		}
		if r.Error != "" {
			return false
		}
	}
	return true
}
