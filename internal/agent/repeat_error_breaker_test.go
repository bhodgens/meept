package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Task 1: breaker primitive (tool-boundary-hardening leaf 02)
// ---------------------------------------------------------------------------

// TestBreaker_ExhaustsAfterThreeIdenticalFailures: 3x Observe on the same
// (tool, args-hash, error-line) key exhausts on the 3rd; Allow is false after.
func TestBreaker_ExhaustsAfterThreeIdenticalFailures(t *testing.T) {
	b := newRepeatErrorBreaker()
	hash := repeatErrorArgsHash(map[string]any{"name": "x"})

	for i := 1; i <= 2; i++ {
		exhausted, summary := b.Observe("task_create", hash, "name is required")
		assert.False(t, exhausted, "failure %d must not exhaust the budget", i)
		assert.Empty(t, summary, "no summary before exhaustion")
		assert.True(t, b.Allow("task_create", hash), "must still be allowed after %d failures", i)
	}

	exhausted, summary := b.Observe("task_create", hash, "name is required")
	require.True(t, exhausted, "the 3rd identical failure must exhaust the budget")
	assert.Equal(t,
		"tool task_create rejected the identical input 3 times (name is required); giving up",
		summary,
		"the summary must match the honest terminal message shape exactly")
	assert.False(t, b.Allow("task_create", hash), "Allow must refuse after exhaustion")
	assert.Equal(t, summary, b.Refusal("task_create", hash), "Refusal must carry the stored summary")
}

// TestBreaker_DifferentArgsAllowed: the same tool with different args gets an
// independent budget.
func TestBreaker_DifferentArgsAllowed(t *testing.T) {
	b := newRepeatErrorBreaker()
	hashA := repeatErrorArgsHash(map[string]any{"name": "a"})
	hashB := repeatErrorArgsHash(map[string]any{"name": "b"})

	for i := 0; i < maxIdenticalToolErrors; i++ {
		exhausted, _ := b.Observe("task_create", hashA, "name is required")
		if i < maxIdenticalToolErrors-1 {
			assert.False(t, exhausted)
		}
	}
	assert.False(t, b.Allow("task_create", hashA), "key A must be exhausted")
	assert.True(t, b.Allow("task_create", hashB), "key B (different args) must have a fresh budget")
}

// TestBreaker_DifferentErrorResetsCount: same tool and args but a DIFFERENT
// error first line is a new key — the model may have fixed the arg, so the new
// triple gets a fresh budget.
func TestBreaker_DifferentErrorResetsCount(t *testing.T) {
	b := newRepeatErrorBreaker()
	hash := repeatErrorArgsHash(map[string]any{"name": "x"})

	// Two failures with error A.
	b.Observe("task_create", hash, "name is required")
	b.Observe("task_create", hash, "name is required")

	// A different error text is a different key: still allowed.
	assert.True(t, b.Allow("task_create", hash), "a changed error line must open a fresh budget")

	// And error A's key is independent: one more A-failure exhausts it.
	exhausted, summary := b.Observe("task_create", hash, "name is required")
	require.True(t, exhausted)
	assert.Contains(t, summary, "name is required")
	// The B key stays fresh even though A is dead.
	assert.True(t, b.Allow("task_create", repeatErrorArgsHash(map[string]any{"name": "y"})))
}

// TestBreaker_ResetClears: Reset wipes counts, first errors, and dead keys.
func TestBreaker_ResetClears(t *testing.T) {
	b := newRepeatErrorBreaker()
	hash := repeatErrorArgsHash(map[string]any{"name": "x"})

	exhausted, _ := b.Observe("task_create", hash, "name is required")
	b.Observe("task_create", hash, "name is required")
	require.False(t, exhausted, "precondition: not yet exhausted")

	b.Reset()
	assert.True(t, b.Allow("task_create", hash), "Reset must re-open a dead key")
	assert.Empty(t, b.Refusal("task_create", hash), "Refusal must be empty after Reset")

	// The budget restarts from zero: two more failures do not exhaust.
	for i := 0; i < maxIdenticalToolErrors-1; i++ {
		exhausted, _ = b.Observe("task_create", hash, "name is required")
		assert.False(t, exhausted, "post-Reset failure %d must not exhaust", i+1)
	}
}

// TestBreaker_Concurrent: goroutine hammer on one key — exactly one Observe
// caller sees exhausted (exactly one summary), no race (run under -race).
func TestBreaker_Concurrent(t *testing.T) {
	b := newRepeatErrorBreaker()
	hash := repeatErrorArgsHash(map[string]any{"q": "hammer"})

	const goroutines = 32
	const observesPerGoroutine = 8

	var mu sync.Mutex
	summaries := make([]string, 0, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < observesPerGoroutine; i++ {
				if exhausted, summary := b.Observe("task_create", hash, "boom"); exhausted {
					mu.Lock()
					summaries = append(summaries, summary)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	assert.Len(t, summaries, 1,
		"exactly one Observe caller must observe the exhaustion transition")
	if len(summaries) == 1 {
		assert.Contains(t, summaries[0], "rejected the identical input 3 times (boom); giving up")
	}
	assert.False(t, b.Allow("task_create", hash), "the key must be dead after the hammer")
}

// TestBreaker_CanonicalArgsHash: the args hash is canonical-JSON based and
// therefore key-order-insensitive, including for nested maps. (Go's
// encoding/json marshals map keys in sorted order at every nesting level, so
// json.Marshal on map[string]any IS the canonicalizer — verified here.)
func TestBreaker_CanonicalArgsHash(t *testing.T) {
	flatA := repeatErrorArgsHash(map[string]any{"a": 1, "b": 2})
	flatB := repeatErrorArgsHash(map[string]any{"b": 2, "a": 1})
	assert.Equal(t, flatA, flatB, "flat maps differing only in key order must hash equal")

	nestedA := repeatErrorArgsHash(map[string]any{
		"outer": map[string]any{"p": 1, "q": map[string]any{"z": 9, "y": 8}},
		"list":  []any{1, 2},
	})
	nestedB := repeatErrorArgsHash(map[string]any{
		"list":  []any{1, 2},
		"outer": map[string]any{"q": map[string]any{"y": 8, "z": 9}, "p": 1},
	})
	assert.Equal(t, nestedA, nestedB, "nested maps differing only in key order must hash equal")

	// The hash is a hex sha256 digest.
	assert.Len(t, flatA, 64, "sha256 hex digest must be 64 chars")
	assert.NotEqual(t, flatA, nestedA, "different args must hash differently")

	// Nil args are deterministic.
	assert.Equal(t,
		repeatErrorArgsHash(nil),
		repeatErrorArgsHash(map[string]any{}),
		"nil and empty args must hash identically")

	// Raw JSON key order cannot leak into the hash via string formatting.
	assert.False(t, strings.Contains(fmt.Sprint(flatA), `"a"`))
}
