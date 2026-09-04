package agent

// Endpoint-timeout parking tests (endpoint-wait overhaul): a turn that hits
// a transport timeout — or whose FIRST resolve finds every alias candidate
// endpoint-blocked — PARKS on the existing throttle machinery (StateQuotaWait
// / "throttle_wait", TurnParker class=throttle) instead of erroring, while
// the resolver's endpoint block + lazy-clear provides the single prober.
// Non-timeout failures keep surfacing through rotation as before (control).

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// timeoutError builds a transport-timeout error the way the HTTP layer does
// (wrapped context.DeadlineExceeded) — the shape VerdictForFailure classifies
// as FailureThrottle/"transport_timeout" when arming the endpoint cooldown.
func timeoutError(host string) error {
	return fmt.Errorf("post %q: %w", host, context.DeadlineExceeded)
}

// timeoutScriptChatter is throttleScriptChatter with an EXPLICIT
// ChatWithProgress: the loop prefers the progress path when offered, and
// throttleScriptChatter's delegation would silently route the scripted
// errors through it. Both entries share one script and one call count.
type timeoutScriptChatter struct {
	throttleScriptChatter
}

func (m *timeoutScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

// newEndpointParkLoop is newThrottleParkLoop plus a resolver: a loop wired
// to a real TurnParker sharing the injected clock. The parker is NOT
// started: park-side tests never drain.
func newEndpointParkLoop(t *testing.T, chatter llm.Chatter, resolver *llm.Resolver, maxWait time.Duration) (*AgentLoop, *TurnParker, *fakeNowFunc) {
	t.Helper()
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-endpoint-park", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, maxWait)
	parker.nowFunc = clock.nowFn
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker)
	return loop, parker, clock
}

// armAliasEndpointBlocks arms the endpoint cooldown for EVERY member of the
// test alias via timeout-class RecordAliasFailure calls (the same way a real
// timeout does), returning the resolver-stamped model configs for identity
// assertions. ResolveForAlias afterwards must report ErrAllEndpointsBlocked.
func armAliasEndpointBlocks(t *testing.T, resolver *llm.Resolver) []*llm.ModelConfig {
	t.Helper()
	var armed []*llm.ModelConfig
	for range 2 {
		m, err := resolver.ResolveForAlias(testClassifierAlias, "")
		if err != nil {
			t.Fatalf("setup resolve: %v", err)
		}
		resolver.RecordAliasFailure(testClassifierAlias, timeoutError(m.BaseURL), m)
		armed = append(armed, m)
		// Step to the next member for the second iteration.
		if _, err := resolver.RotateToNextModel(testClassifierAlias); err != nil {
			t.Fatalf("setup rotate: %v", err)
		}
	}
	if _, err := resolver.ResolveForAlias(testClassifierAlias, ""); !errors.Is(err, llm.ErrAllEndpointsBlocked) {
		t.Fatalf("setup: resolve = %v, want ErrAllEndpointsBlocked", err)
	}
	return armed
}

// TestAgentLoop_TransportTimeoutParksTurn: mid-turn transport timeouts arm
// the endpoint cooldown per member (RecordAliasFailure → FailureThrottle
// verdict). A timeout on ONE member with a healthy peer rotates and
// succeeds — parking would be wrong there. When the rotation target's
// endpoint is ALSO timeout-blocked, the resolve pre-check reports
// ErrAllEndpointsBlocked and the turn PARKS on the throttle machinery
// instead of dialing the blocked endpoint a third time: no error to the
// caller, StateQuotaWait with reason "throttle_wait", ResumeAt at the
// endpoint-block expiry (the default 30s alias timeout).
func TestAgentLoop_TransportTimeoutParksTurn(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	chatter := &timeoutScriptChatter{throttleScriptChatter{
		errs: []error{
			timeoutError("https://p1.invalid"),
			timeoutError("https://p2.invalid"),
		},
		resp: &llm.Response{Content: "recovered", FinishReason: "stop"},
	}}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", ProviderID: "p2"},
	)
	loop, parker, clock := newEndpointParkLoop(t, chatter, resolver, time.Hour)
	_ = clock

	before := runtime.NumGoroutine()
	reply, err := loop.RunOnce(context.Background(), "hello", "conv-timeout-park")
	if err != nil {
		t.Fatalf("parked turn must not surface an error, got: %v", err)
	}
	if reply != "" {
		t.Errorf("reply = %q, want empty (turn parked)", reply)
	}
	if got := parker.Pending(); got != 1 {
		t.Fatalf("parker.Pending() = %d, want 1", got)
	}
	rec := parkedThrottleRecord(parker)
	if rec.Class != llm.FailureThrottle {
		t.Errorf("record class = %v, want FailureThrottle (throttle machinery reused)", rec.Class)
	}
	if rec.ResumeAt.Before(time.Now().Add(29 * time.Second)) {
		t.Errorf("ResumeAt = %v, want at the endpoint-block expiry (~30s alias timeout)", rec.ResumeAt)
	}
	if rec.TurnPayload == nil {
		t.Error("TurnPayload = nil, want the frozen chat-dispatch encoding")
	}
	if state := loop.GetState(); state != StateQuotaWait {
		t.Errorf("state = %v, want StateQuotaWait", state)
	}
	history := loop.GetStateHistory()
	if len(history) == 0 || history[len(history)-1].Reason != "throttle_wait" {
		t.Errorf("last transition reason = %q, want \"throttle_wait\"", history[len(history)-1].Reason)
	}
	// Two calls: p1 timed out, p2 (rotation target) also timed out — the
	// third dial (the blocked endpoint again) must never happen.
	if got := chatter.callCount(); got != 2 {
		t.Errorf("chatter calls = %d, want 2 (park after both members timed out)", got)
	}
	// The timed-out endpoint is under a live block; the loop's parked
	// wait and the resolver's lazy-clear describe the same deadline.
	m1 := &llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", ProviderID: "p1"}
	if !resolver.EndpointBlocked(m1) {
		t.Error("resolver.EndpointBlocked(m1) = false, want true after the timeout")
	}
	waitGoroutinesSettle(t, before)
}

// TestAgentLoop_ResolveAllEndpointsBlockedParksFreshTurn: a FRESH turn whose
// first resolve hits ErrAllEndpointsBlocked (a peer armed the block moments
// ago) parks WITHOUT a single LLM call — the parked agent does not become
// another 429-generating prober. ResumeAt = the earliest block expiry.
func TestAgentLoop_ResolveAllEndpointsBlockedParksFreshTurn(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", ProviderID: "p2"},
	)
	armed := armAliasEndpointBlocks(t, resolver)
	chatter := &throttleScriptChatter{
		resp: &llm.Response{Content: "should never be reached"},
	}
	loop, parker, _ := newEndpointParkLoop(t, chatter, resolver, time.Hour)

	reply, err := loop.RunOnce(context.Background(), "hello", "conv-resolve-park")
	if err != nil {
		t.Fatalf("parked turn must not surface an error, got: %v", err)
	}
	if reply != "" {
		t.Errorf("reply = %q, want empty (turn parked)", reply)
	}
	if got := parker.Pending(); got != 1 {
		t.Fatalf("parker.Pending() = %d, want 1", got)
	}
	rec := parkedThrottleRecord(parker)
	if rec.Class != llm.FailureThrottle {
		t.Errorf("record class = %v, want FailureThrottle", rec.Class)
	}
	if rec.ResumeAt.Before(time.Now().Add(29 * time.Second)) {
		t.Errorf("ResumeAt = %v, want at the earliest block expiry (~30s)", rec.ResumeAt)
	}
	// Identity: one of the alias members (the earliest-expiring block).
	// The record carries no model fields — decode the frozen payload.
	turn, err := recordToThrottleTurn(rec)
	if err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	identityKnown := false
	for _, m := range armed {
		if turn.ProviderID == m.ProviderID && turn.ModelID == m.ModelID {
			identityKnown = true
			break
		}
	}
	if !identityKnown {
		t.Errorf("parked identity = %s/%s, want one of the armed alias members", turn.ProviderID, turn.ModelID)
	}
	// Zero LLM calls: a blocked fresh turn must not add prober traffic.
	if got := chatter.callCount(); got != 0 {
		t.Errorf("chatter calls = %d, want 0 (park at resolve, never dial)", got)
	}
}

// TestAgentLoop_TimeoutParkGivesUpBeyondMaxWait: when the endpoint block
// outlasts the parker's MaxWait, the D8 ThrottleGiveUpError surfaces and
// nothing is parked (mirrors the throttle give-up contract).
func TestAgentLoop_TimeoutParkGivesUpBeyondMaxWait(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", ProviderID: "p2"},
	)
	_ = armAliasEndpointBlocks(t, resolver)
	chatter := &throttleScriptChatter{}
	loop, parker, _ := newEndpointParkLoop(t, chatter, resolver, 10*time.Millisecond)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-timeout-giveup")
	if err == nil {
		t.Fatal("expected the D8 give-up error to surface")
	}
	var giveUp *llm.ThrottleGiveUpError
	if !errors.As(err, &giveUp) {
		t.Fatalf("got %T (%v), want *llm.ThrottleGiveUpError", err, err)
	}
	if parker.Pending() != 0 {
		t.Errorf("parker.Pending() = %d, want 0 (nothing parked on give-up)", parker.Pending())
	}
	if got := chatter.callCount(); got != 0 {
		t.Errorf("chatter calls = %d, want 0", got)
	}
}

// TestAgentLoop_NonTimeoutFailureStillSurfaces is the control: a NON-timeout
// failure (connection refused) keeps the historical behavior — rotation
// within budget, then the error surfaces. No parking, no throttle state.
func TestAgentLoop_NonTimeoutFailureStillSurfaces(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	// The script exhausts BEFORE MaxAttempts so the mid-loop
	// "budget exhausted" surface fires (two members, rotation allowed
	// once at attempt 1; attempt 2 hits the exhausted script → surface).
	chatter := &timeoutScriptChatter{throttleScriptChatter{
		errs: []error{
			errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
			errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
			errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
		},
	}}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", ProviderID: "p2"},
	)
	loop, parker, _ := newEndpointParkLoop(t, chatter, resolver, time.Hour)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-non-timeout")
	if err == nil {
		t.Fatal("expected the non-timeout failure to surface (control)")
	}
	var giveUp *llm.ThrottleGiveUpError
	if errors.As(err, &giveUp) {
		t.Fatalf("non-timeout failure must not become a give-up: %v", err)
	}
	if parker.Pending() != 0 {
		t.Errorf("parker.Pending() = %d, want 0 (no park on non-timeout)", parker.Pending())
	}
	if state := loop.GetState(); state == StateQuotaWait {
		t.Error("state = StateQuotaWait, want the failure surfaced without parking")
	}
}
