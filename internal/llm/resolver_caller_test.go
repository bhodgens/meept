package llm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// newStickyTestResolver builds a resolver with a 3-model sticky alias and an
// optional attached routing logger. Model refs resolve against the shared
// createTestConfig fixture.
func newStickyTestResolver(t *testing.T, routingLogger *RoutingLogger) *Resolver {
	t.Helper()
	cfg := createTestConfig()
	cfg.ModelAliases["sticky"] = ModelAliasEntry{
		Models: []string{
			"zai/glm-4.7",
			"ollama/llama3.2",
			"zai/glm-4.5-air",
		},
		Timeout:                30,
		MaxFails:               3,
		BalancedStickyRequests: true,
	}
	resolver := NewResolver(cfg, nil)
	if routingLogger != nil {
		resolver.SetRoutingLogger(routingLogger)
	}
	return resolver
}

// TestResolveForAlias_WithCallerKey verifies the callerKey parameter is
// accepted on a non-sticky alias without behavior change.
func TestResolveForAlias_WithCallerKey(t *testing.T) {
	cfg := createTestConfig()
	resolver := NewResolver(cfg, nil)

	mc, err := resolver.ResolveForAlias("coder", "session-1")
	assert.NoError(t, err)
	assert.NotNil(t, mc)
	assert.Equal(t, "glm-4.7", mc.ModelID)
}

// TestResolveForAlias_StickyRequests verifies per-caller pinning: the same
// caller keeps its model across calls and concurrent callers distribute
// across the alias models.
func TestResolveForAlias_StickyRequests(t *testing.T) {
	resolver := newStickyTestResolver(t, nil)

	// First call for session-1 pins model 0.
	m1, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.7", m1.ModelID)

	// Second call for session-1 returns the SAME model (sticky).
	m2, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	assert.Equal(t, m1.ModelID, m2.ModelID)

	// Callers for session-2 and session-3 get DIFFERENT models.
	m3, err := resolver.ResolveForAlias("sticky", "session-2")
	assert.NoError(t, err)
	assert.NotEqual(t, m1.ModelID, m3.ModelID)

	m4, err := resolver.ResolveForAlias("sticky", "session-3")
	assert.NoError(t, err)
	assert.NotEqual(t, m1.ModelID, m4.ModelID)
	assert.NotEqual(t, m3.ModelID, m4.ModelID)

	// All three models are pinned exactly once.
	health := resolver.health["sticky"]
	assert.Len(t, health.StickyPins, 3)
}

// TestResolveForAlias_StickyEmptyCallerKeyDisables verifies empty callerKey
// bypasses sticky pinning and returns the round-robin position.
func TestResolveForAlias_StickyEmptyCallerKeyDisables(t *testing.T) {
	resolver := newStickyTestResolver(t, nil)

	m1, err := resolver.ResolveForAlias("sticky", "")
	assert.NoError(t, err)
	m2, err := resolver.ResolveForAlias("sticky", "")
	assert.NoError(t, err)
	assert.Equal(t, m1.ModelID, m2.ModelID, "no pin should be recorded without a callerKey")

	health := resolver.health["sticky"]
	assert.Empty(t, health.StickyPins)
}

// TestResolveForAlias_StickyPinReleasedOnFailure verifies a caller pinned to
// the failing model is re-pinned to a healthy model, while callers pinned to
// other models keep their pins.
func TestResolveForAlias_StickyPinReleasedOnFailure(t *testing.T) {
	resolver := newStickyTestResolver(t, nil)

	// Three callers pin all three models (0, 1, 2).
	_, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	_, err = resolver.ResolveForAlias("sticky", "session-2")
	assert.NoError(t, err)
	mc3, err := resolver.ResolveForAlias("sticky", "session-3")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.5-air", mc3.ModelID, "session-3 should hold the third model")

	// The model session-3 was served fails: its pin is released, others stay.
	resolver.RecordAliasFailure("sticky", nil)
	health := resolver.health["sticky"]
	assert.NotContains(t, health.StickyPins, "session-3")
	assert.Contains(t, health.StickyPins, "session-1")
	assert.Contains(t, health.StickyPins, "session-2")

	// Unaffected callers keep their exact model.
	m1, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.7", m1.ModelID)

	// The failed caller re-pins to a healthy model.
	m3, err := resolver.ResolveForAlias("sticky", "session-3")
	assert.NoError(t, err)
	assert.NotEqual(t, "glm-4.5-air", m3.ModelID, "must not re-pin to the failed model")
}

// TestResolveForAlias_StickyReasonsLogged verifies the routing logger
// receives sticky_request for pin hits and sticky_request_new for new pins.
func TestResolveForAlias_StickyReasonsLogged(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing.db")
	rl, err := NewRoutingLogger(dbPath, nil)
	assert.NoError(t, err)
	defer func() { _ = rl.Close() }()

	resolver := newStickyTestResolver(t, rl)

	_, err = resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	_, err = resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)

	decisions, err := rl.Recent(context.Background(), 10)
	assert.NoError(t, err)
	assert.Len(t, decisions, 2)
	assert.Equal(t, "sticky_request_new", decisions[1].Reason, "oldest is the new pin")
	assert.Equal(t, "sticky_request", decisions[0].Reason, "newest is the pin hit")
}

// newReversionTestResolver builds a resolver whose alias puts the default
// model LAST, so a post-cooldown revert is distinguishable from a rotation
// advance (rotation lands on index 1; reversion lands on index 2).
func newReversionTestResolver(t *testing.T) *Resolver {
	t.Helper()
	cfg := createTestConfig()
	cfg.ModelAliases["with-default"] = ModelAliasEntry{
		Models: []string{
			"zai/glm-4.5-air",
			"ollama/llama3.2",
			"zai/glm-4.7",
		},
		Timeout:      30,
		MaxFails:     3,
		DefaultModel: "zai/glm-4.7",
	}
	return NewResolver(cfg, nil)
}

// TestResolveForAlias_DefaultModelReversion verifies that after the
// reversion deadline passes, resolution snaps back to default_model instead
// of continuing round-robin from the fallback position.
func TestResolveForAlias_DefaultModelReversion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "routing.db")
	rl, err := NewRoutingLogger(dbPath, nil)
	assert.NoError(t, err)
	defer func() { _ = rl.Close() }()

	cfg := createTestConfig()
	cfg.ModelAliases["with-default"] = ModelAliasEntry{
		Models: []string{
			"zai/glm-4.5-air",
			"ollama/llama3.2",
			"zai/glm-4.7",
		},
		Timeout:      30,
		MaxFails:     3,
		DefaultModel: "zai/glm-4.7",
	}
	resolver := NewResolver(cfg, nil)
	resolver.SetRoutingLogger(rl)

	// Baseline resolve: round-robin position 0 (not the default model).
	mc, err := resolver.ResolveForAlias("with-default", "")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.5-air", mc.ModelID)

	// Failure arms the cooldown and the reversion deadline.
	resolver.RecordAliasFailure("with-default", nil)
	health := resolver.health["with-default"]
	assert.False(t, health.RevertAt.IsZero(), "RecordAliasFailure must arm RevertAt when default_model is set")

	// Simulate the cooldown window fully elapsing (deterministic, no sleep).
	health.CooldownUntil = time.Now().Add(-time.Second)
	health.RevertAt = time.Now().Add(-time.Second)

	// Next resolve reverts to the default model (index 2), NOT the rotation
	// successor (index 1), and logs the default_reversion reason.
	mc, err = resolver.ResolveForAlias("with-default", "")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.7", mc.ModelID)

	decisions, err := rl.Recent(context.Background(), 10)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(decisions), 2)
	assert.Equal(t, "default_reversion", decisions[0].Reason, "newest decision is the reversion")

	// Reversion consumed: deadline disarmed, failure state reset.
	assert.True(t, health.RevertAt.IsZero())
	assert.Equal(t, 0, health.ConsecutiveFails)
	assert.True(t, health.CooldownUntil.IsZero())
}

// TestResolveForAlias_DefaultModelReversion_WaitsForCooldown verifies the
// revert does not fire while the alias is still cooling down.
func TestResolveForAlias_DefaultModelReversion_WaitsForCooldown(t *testing.T) {
	resolver := newReversionTestResolver(t)

	mc, err := resolver.ResolveForAlias("with-default", "")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.5-air", mc.ModelID)

	resolver.RecordAliasFailure("with-default", nil)
	assert.False(t, resolver.health["with-default"].RevertAt.IsZero())

	// Deadline not yet reached: rotation advances but does not revert.
	mc, err = resolver.ResolveForAlias("with-default", "")
	assert.NoError(t, err)
	assert.Equal(t, "llama3.2", mc.ModelID)
}

// TestRecordAliasFailure_StalePinBeyondModelList verifies a pin pointing past
// the configured model list (config shrank) is dropped, not served.
func TestRecordAliasFailure_StalePinBeyondModelList(t *testing.T) {
	resolver := newStickyTestResolver(t, nil)

	// Pin all three models, then shrink the alias to one model in place.
	_, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	_, err = resolver.ResolveForAlias("sticky", "session-2")
	assert.NoError(t, err)
	_, err = resolver.ResolveForAlias("sticky", "session-3")
	assert.NoError(t, err)

	alias := resolver.aliases["sticky"]
	alias.Models = alias.Models[:1]

	// Session-1's pin (index 0) still fits; the others are stale. Resolution
	// must not panic or index out of range.
	mc, err := resolver.ResolveForAlias("sticky", "session-1")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.7", mc.ModelID)

	mc, err = resolver.ResolveForAlias("sticky", "session-2")
	assert.NoError(t, err)
	assert.Equal(t, "glm-4.7", mc.ModelID, "stale pin must be re-assigned in bounds")
}
