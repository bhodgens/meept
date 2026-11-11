package agent

// R3 regression-repair pin tests (delegate resume claim, turn-scoped model
// override, fail-closed child route resolution):
//
//   - R3-1: the C11 resume claim (m.resuming) is SUPERSEDED by the
//     children-map entry on every Resume success path — one successful
//     resume + later cancel no longer permanently refuses all future
//     Resumes with a stale "already being resumed" claim.
//   - R3-2: the C10 delegation route override armed by
//     RunAgentWithModelRef is TURN-SCOPED — no override survives the run,
//     whether the child turn consumed it or errored before consumption.
//   - R3-3: runChildTurn resolves the child's recorded route BEFORE the
//     turn launches; an unresolvable ref FAILS the child turn (fail-closed)
//     instead of silently degrading to the spec model.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingRunFunc returns a runFn that blocks until release is closed (it
// ignores ctx so only the test's close releases the child), counting calls.
func blockingRunFunc(release chan struct{}, runs *atomic.Int64) func(context.Context, string, string, string, string) (string, error) {
	return func(_ context.Context, _, _, _, _ string) (string, error) {
		runs.Add(1)
		<-release
		return "done", nil
	}
}

// claimHeld reports whether the C11 reservation claim is currently held for
// childID.
func claimHeld(m *DelegateManager, childID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, held := m.resuming[childID]
	return held
}

// R3-1 pin (LIVE path): resume succeeds -> the claim is released (superseded
// by the children entry); an immediate second Resume is refused only for the
// LEGITIMATE running reason, never the stale "already being resumed" claim;
// and resume+cancel+resume cycles work repeatedly.
func TestResume_SuccessReleasesClaim_LiveCycleRepeats(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var runs atomic.Int64
	release := make(chan struct{})
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(blockingRunFunc(release, &runs))

	ctx := context.Background()
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	require.NoError(t, err)

	// Cycle twice: cancel the running child, resume it, verify the claim is
	// released after every success and a back-to-back resume is never
	// refused by a STALE claim.
	for cycle := 0; cycle < 2; cycle++ {
		require.NoError(t, mgr.Cancel(res.ChildID), "cycle %d: cancel", cycle)

		got, err := mgr.Resume(ctx, "sess-parent", res.ChildID)
		require.NoError(t, err, "cycle %d: resume after cancel must work (stale claim?)", cycle)
		require.Equal(t, "running", got.Status)

		if claimHeld(mgr, res.ChildID) {
			t.Fatalf("cycle %d: resume claim still held after a SUCCESSFUL resume (R3-1 stale claim)", cycle)
		}

		// Immediate second Resume: the child is RUNNING, so a refusal is
		// legitimate — but its reason must be the children-entry gate
		// ("already resuming/running"), NEVER the stale reservation
		// ("already being resumed").
		_, err2 := mgr.Resume(ctx, "sess-parent", res.ChildID)
		if err2 == nil {
			t.Fatalf("cycle %d: second concurrent resume admitted (double execution)", cycle)
		}
		if strings.Contains(err2.Error(), "already being resumed") {
			t.Fatalf("cycle %d: second resume refused by the STALE claim: %v", cycle, err2)
		}
		// A running child is refused for a LEGITIMATE reason: the live
		// state gate ("not resumable ... running") or the children-entry
		// gate ("already resuming/running") — never the reservation.
		if !strings.Contains(err2.Error(), "running") {
			t.Fatalf("cycle %d: second resume error = %v, want a legitimate running-state refusal", cycle, err2)
		}
	}
	close(release)
}

// R3-1 pin (COLD path): the same supersede semantics on the rehydrated
// resume path — resume succeeds -> claim released; resume+cancel+resume
// cycles work repeatedly against the durable store.
func TestResume_SuccessReleasesClaim_ColdCycleRepeats(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	store := session.NewMemoryStore(nil)
	spawnMgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	spawnMgr.SetStore(store)
	ctx := context.Background()
	res, err := spawnMgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	require.NoError(t, err)
	spawnMgr.SettleForTest(res.ChildID, ChildStateInterrupted, "", "cancelled")

	var runs atomic.Int64
	release := make(chan struct{})
	fresh := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	fresh.SetStore(store)
	fresh.SetRunFunc(blockingRunFunc(release, &runs))

	for cycle := 0; cycle < 2; cycle++ {
		if cycle > 0 {
			// The prior cycle's resume left the child running; cancel so
			// the next cold resume is admissible.
			require.NoError(t, fresh.Cancel(res.ChildID), "cycle %d: cancel", cycle)
		}
		got, err := fresh.Resume(ctx, "sess-parent", res.ChildID)
		require.NoError(t, err, "cycle %d: cold resume must work (stale claim?)", cycle)
		require.Equal(t, "running", got.Status)
		if claimHeld(fresh, res.ChildID) {
			t.Fatalf("cycle %d: cold resume left the claim held after SUCCESS (R3-1)", cycle)
		}
	}
	close(release)
}

// ---------------------------------------------------------------------------
// R3-2: the delegation route override is turn-scoped on the shared loop.
// ---------------------------------------------------------------------------

// newR3RouteRegistry builds a registry whose "probe" loop is RunOnce-capable
// against the capture server (mirrors the daemon wiring: concrete client +
// resolver, the same shape RunAgentWithModelRef arms overrides on).
func newR3RouteRegistry(t *testing.T, serverURL string) (*AgentRegistry, *llm.Resolver) {
	t.Helper()
	resolver := overrideResolver(t, serverURL)
	reg := NewAgentRegistry(RegistryConfig{
		Logger:          silentLogger(),
		Resolver:        resolver,
		LLMClient:       llm.NewClient(&llm.ModelConfig{BaseURL: serverURL, ProviderID: "local", ModelID: "manager-default"}),
		ToolRegistry:    NewPlaceholderToolRegistry(),
		SecurityChecker: pkgsecurity.NewPermissionChecker(pkgsecurity.Config{}),
		MessageBus:      bus.New(nil, slogDiscardLogger()),
	})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{
		ID: "probe", Enabled: true,
		Constraints: AgentConstraints{MaxIterations: 1},
	}
	reg.mu.Unlock()
	return reg, resolver
}

// newR3HarnessRegistry is newR3RouteRegistry with the client's short-retry
// budget lowered to 1 so a failing upstream fails the turn in one attempt
// (the default 3-attempt budget spends ~90s of backoff against a 500 in
// tests).
func newR3HarnessRegistry(t *testing.T, serverURL string) (*AgentRegistry, *llm.Resolver) {
	t.Helper()
	resolver := overrideResolver(t, serverURL)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL: serverURL, ProviderID: "local", ModelID: "manager-default",
	})
	client.SetFailurePolicyConfig(&llm.FailurePolicyConfig{ShortRetries: 1})
	reg := NewAgentRegistry(RegistryConfig{
		Logger:          silentLogger(),
		Resolver:        resolver,
		LLMClient:       client,
		ToolRegistry:    NewPlaceholderToolRegistry(),
		SecurityChecker: pkgsecurity.NewPermissionChecker(pkgsecurity.Config{}),
		MessageBus:      bus.New(nil, slogDiscardLogger()),
	})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{
		ID: "probe", Enabled: true,
		Constraints: AgentConstraints{MaxIterations: 1},
	}
	reg.mu.Unlock()
	return reg, resolver
}

// R3-2 pin (a): a consumed override leaves NO override behind — after the
// child run returns, loop inspection shows no armed override and no staged
// config, and the route reached the wire.
func TestRunAgentWithModelRef_ConsumedOverrideClearedAfterRun(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	reg, _ := newR3RouteRegistry(t, server.URL)
	loop, err := reg.Get("probe")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = reg.RunAgentWithModelRef(ctx, "probe", "do a thing", "conv-r3-consumed", "local/user-b")
	if err != nil && !errors.Is(err, ErrMaxIterationsReached) {
		t.Fatalf("child run failed unexpectedly: %v", err)
	}

	require.GreaterOrEqual(t, cap.count(), 1, "the LLM must have been reached")
	assert.Equal(t, "user-override-b", cap.models[0],
		"the delegation route must serve the child turn")
	assert.Empty(t, loop.GetModelOverride(),
		"no override may survive the run (next loop inspection shows no override)")
	assert.Nil(t, loop.pendingModelOverrideConfig, "staged override config must be consumed")
}

// R3-2 pin (b): a child run that ERRORS before consuming the override leaves
// no override behind — the next unrelated turn on the shared loop must not
// inherit the child's route.
func TestRunAgentWithModelRef_ErroredRunLeavesNoOverride(t *testing.T) {
	// An always-500 upstream with a NON-JSON body: every LLM call of the
	// turn fails, so the one-shot consumption site never completes a
	// served turn. (MinAttempts=1 in the harness config keeps the failure
	// fast; a real provider failover budget applies on daemon wiring.)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusInternalServerError)
	}))
	defer failing.Close()

	// The registry is built on the FAILING upstream (both the loop client's
	// config and the resolver's resolved configs point there, matching the
	// daemon wiring shape) — the child run's LLM calls fail and nothing is
	// served for this turn.
	reg, _ := newR3HarnessRegistry(t, failing.URL)
	loop, err := reg.Get("probe")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = reg.RunAgentWithModelRef(ctx, "probe", "doomed child turn", "conv-r3-errored", "local/user-b")
	require.Error(t, err, "the failing upstream must fail the child run")

	assert.Empty(t, loop.GetModelOverride(),
		"a run that errors before consumption must leave NO override behind (R3-2 stuck-override)")
	assert.Nil(t, loop.pendingModelOverrideConfig,
		"no staged override config may survive the errored run")

	// Stale-route probe on the ERRORED registry: no override may be lying
	// around after the failed run.
	assert.Empty(t, loop.GetModelOverride(),
		"the errored registry's loop must show no override after the failed run")
}

// R3-2 pin (b2): the STUCK-OVERRIDE shape — an override left armed by a run
// that parked before consumption (seeded directly, the state a pre-R3-2
// errored/parked child left behind) is torn down by the next
// RunAgentWithModelRef call; it never survives past that run.
func TestRunAgentWithModelRef_StuckOverrideTornDown(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	reg, _ := newR3RouteRegistry(t, server.URL)
	loop, err := reg.Get("probe")
	require.NoError(t, err)

	// Stuck-override state (park-before-consumption leftover).
	loop.ApplyRequestModel("local/user-b")
	require.NotEmpty(t, loop.GetModelOverride(), "pre-arm failed")

	ctx := context.Background()
	// The NEXT delegation-shaped run on the shared loop (empty modelRef
	// behaves like RunAgent: nothing new armed) must not INHERIT the
	// leftover: no override state survives that run.
	_, err = reg.RunAgentWithModelRef(ctx, "probe", "unrelated turn", "conv-r3-stuck", "")
	require.NoError(t, err)
	assert.Empty(t, loop.GetModelOverride(),
		"a leftover override from a parked child must be gone after the next run")
	assert.Nil(t, loop.pendingModelOverrideConfig, "no staged config may survive")
}

// ---------------------------------------------------------------------------
// R3-3: fail-closed route resolution before the child turn launches.
// ---------------------------------------------------------------------------

// R3-3 pin: an unresolvable route fails the child turn with a clear route
// error and the run seam is never invoked (no silent spec-model fallback).
func TestRunChildTurn_UnresolvableRefFailsClosed(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var runs atomic.Int64
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(func(context.Context, string, string, string, string) (string, error) {
		runs.Add(1)
		return "should not run", nil
	})
	mgr.SetModelRefResolver(func(ref string) (string, error) {
		if ref == "ghost/missing" {
			return "", errors.New("escalation ref not found: ghost/missing")
		}
		return ref, nil
	})

	rec := ChildRecord{
		ChildID: "delegate-r3-bad", AgentID: "probe", ParentSessionID: "s",
		Mode: "spawn", ConversationID: "conv-r3-bad",
		State: ChildStateRunning,
		// A spec model that would silently serve if the ref were dropped.
		Provider: "ghost", Model: "missing",
	}
	_, err := mgr.runChildTurn(context.Background(), rec, "brief")
	require.Error(t, err, "an unresolvable route must fail the child turn (fail-closed)")
	assert.Contains(t, err.Error(), "does not resolve")
	assert.Contains(t, err.Error(), "ghost/missing")
	assert.Zero(t, runs.Load(), "the run seam must never fire for an unresolvable route")
}

// R3-3 pin: a resolvable route passes resolution and reaches the run seam;
// the empty route stays a no-op override (identity default).
func TestRunChildTurn_ResolvableRefReachesRunSeam(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var gotRef atomic.Value
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(func(_ context.Context, _, _, _ string, modelRef string) (string, error) {
		gotRef.Store(modelRef)
		return "ok", nil
	})
	mgr.SetModelRefResolver(func(ref string) (string, error) {
		// Alias names resolve through the same seam as direct refs.
		if ref == "classifier" {
			return "local/a", nil
		}
		return ref, nil
	})

	rec := ChildRecord{
		ChildID: "delegate-r3-ok", AgentID: "probe", ParentSessionID: "s",
		Mode: "spawn", ConversationID: "conv-r3-ok",
		State: ChildStateRunning, Provider: "local", Model: "user-b",
	}
	_, err := mgr.runChildTurn(context.Background(), rec, "brief")
	require.NoError(t, err)
	assert.Equal(t, "local/user-b", gotRef.Load())

	// Bare alias-name route resolves too.
	rec2 := rec
	rec2.ChildID = "delegate-r3-alias"
	rec2.Provider = ""
	rec2.Model = "classifier"
	rec2.ConversationID = "conv-r3-alias"
	_, err = mgr.runChildTurn(context.Background(), rec2, "brief")
	require.NoError(t, err)
	assert.Equal(t, "local/a", gotRef.Load())

	// Empty route: no resolution, no override (identity default).
	rec3 := rec
	rec3.ChildID = "delegate-r3-empty"
	rec3.Provider = ""
	rec3.Model = ""
	rec3.ConversationID = "conv-r3-empty"
	_, err = mgr.runChildTurn(context.Background(), rec3, "brief")
	require.NoError(t, err)
	assert.Equal(t, "", gotRef.Load())
}

// R3-3 pin (end-to-end through the resume execution path): an unresolvable
// route settles the child as FAILED with the route error — surfaced, not
// degraded.
func TestResumeChild_UnresolvableRouteSettlesFailed(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var runs atomic.Int64
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(func(context.Context, string, string, string, string) (string, error) {
		runs.Add(1)
		return "ok", nil
	})
	mgr.SetModelRefResolver(func(string) (string, error) {
		return "", errors.New("no such model")
	})

	rec := ChildRecord{
		ChildID: "delegate-r3-settle", AgentID: "probe", ParentSessionID: "s",
		Mode: "spawn", ConversationID: "conv-r3-settle",
		State: ChildStateRunning, Provider: "p", Model: "gone",
	}
	mgr.children[rec.ChildID] = &childEntry{rec: rec, settle: make(chan struct{})}
	mgr.resumeChild(context.Background(), rec.ChildID, rec, "do the thing")

	mgr.mu.RLock()
	e := mgr.children[rec.ChildID]
	state := e.rec.State
	errStr := e.rec.Err
	settled := e.settled
	mgr.mu.RUnlock()

	assert.Equal(t, ChildStateFailed, state, "an unresolvable route must FAIL the child")
	assert.Contains(t, errStr, "does not resolve")
	assert.True(t, settled, "the failed child must be settled")
	assert.Zero(t, runs.Load(), "no silent spec-model execution for a dead route")
}
