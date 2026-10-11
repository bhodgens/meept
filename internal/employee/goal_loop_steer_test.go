package employee

// Leaf 04 (steer-of-episode, goal-completion tree) tests: GoalLoop.Steer
// and Manager.SteerEmployee per SHARED-CONVENTIONS C5.
//
// Coverage matrix:
//   - steer of a quiet employee  → nil no-op, no episode created
//   - steer of a parked episode  → resume trigger enqueued, Source="steer",
//     assess prompt carries the steering content
//   - steer prompt line only for Source="steer"
//   - SteerEmployee acquires the same invoke mutex as triggerViaGoalLoop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// steerTestPolicy mirrors parkTestPolicy (goal_loop_park_test.go) with a
// horizon big enough for the steer-schedule waits used here.
func steerTestPolicy() llm.FailurePolicyConfig {
	return llm.FailurePolicyConfig{
		Horizon:           2 * time.Hour,
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}
}

// TestGoalLoopSteerQuiet verifies that steering an employee with no active
// episode is a nil no-op: no episode is created, no trigger is enqueued, the
// reflector is never called.
func TestGoalLoopSteerQuiet(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-steer-quiet", testTier1Constitution(), nil, nil).
		WithReflector(reflector)

	queued, err := loop.Steer(context.Background(), "focus on flaky tests")
	if err != nil {
		t.Fatalf("Steer of quiet employee returned error, want nil: %v", err)
	}
	if queued {
		t.Error("Steer of quiet employee reported queued=true, want false")
	}
	if got := reflector.CallCount(); got != 0 {
		t.Errorf("reflector called %d times on quiet steer, want 0", got)
	}
	if l := loop.PendingSteerCount(); l != 0 {
		t.Errorf("pending steer count = %d, want 0", l)
	}
}

// TestGoalLoopSteerActive_ParksResumeTrigger verifies that steering an
// employee with a parked episode enqueues a resume trigger event with
// Source="steer" and the steering content in the payload.
func TestGoalLoopSteerActive_ParksResumeTrigger(t *testing.T) {
	// Park an episode the way the provider-wait path does.
	resetAt := time.Now().UTC().Add(45 * time.Minute)
	parker, captured := newCapturingParker(t, steerTestPolicy())
	turns := parker.turns
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-steer-active", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)
	reflector.queueError(&llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		ResetAt:    resetAt,
		MaxWait:    24 * time.Hour,
	})

	if _, err := loop.Assess(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Assess returned error on quota wait, want nil (parked): %v", err)
	}
	if got := turns.Pending(); got != 1 {
		t.Fatalf("precondition: parker pending = %d, want 1", got)
	}

	// Steer the parked episode.
	queued, err := loop.Steer(context.Background(), "focus on flaky tests")
	if err != nil {
		t.Fatalf("Steer of parked episode returned error: %v", err)
	}
	if !queued {
		t.Error("Steer of parked episode reported queued=false, want true")
	}

	if l := loop.PendingSteerCount(); l != 1 {
		t.Fatalf("pending steer count = %d, want 1", l)
	}
	// The manager path (SteerEmployee) drains the queue under the invoke
	// mutex; at loop level the drain is explicit.
	trigger, ok := loop.drainPendingSteer()
	if !ok {
		t.Fatal("expected a parked steer trigger")
	}
	if trigger.Source != "steer" {
		t.Errorf("trigger source = %q, want %q", trigger.Source, "steer")
	}
	var payload map[string]any
	if err := json.Unmarshal(trigger.Payload, &payload); err != nil {
		t.Fatalf("steer payload unmarshal: %v (raw %s)", err, trigger.Payload)
	}
	if content, ok := payload["content"].(string); !ok || content != "focus on flaky tests" {
		t.Errorf("payload content = %v, want the steering text", payload["content"])
	}
	if trigger.FiredAt.IsZero() {
		t.Error("steer trigger FiredAt is zero")
	}
	if len(*captured) != 0 {
		t.Error("steer resume must not fire the shared parker's resume callback")
	}
}

// TestGoalLoopSteerActive_AssessPromptContainsContent proves the
// re-entered Assess call sees the steering content: the resume trigger is
// drained by a Decide pass whose assess prompt contains the steer line.
func TestGoalLoopSteerActive_AssessPromptContainsContent(t *testing.T) {
	parker, _ := newCapturingParker(t, steerTestPolicy())
	reflector := &promptCapturingReflector{next: &llm.Response{Content: `{"candidates":[]}`}}
	loop := NewGoalLoop("emp-steer-prompt", testTier1Constitution(), nil, nil).
		WithReflector(reflector).
		WithEpisodeParker(parker)

	// Park an episode.
	resetAt := time.Now().UTC().Add(45 * time.Minute)
	reflector.err = &llm.QuotaResetError{
		ProviderID: "p1",
		ModelID:    "m1",
		Code:       "usage_limit_reached",
		ResetAt:    resetAt,
		MaxWait:    24 * time.Hour,
	}
	if _, err := loop.Assess(context.Background(), basicTrigger()); err != nil {
		t.Fatalf("Assess returned error on quota wait, want nil (parked): %v", err)
	}

	// Steer, then re-enter Decide with the parked resume trigger — this is
	// exactly what the steer-resume path does.
	if _, err := loop.Steer(context.Background(), "focus on flaky tests"); err != nil {
		t.Fatalf("Steer error: %v", err)
	}
	trigger, ok := loop.drainPendingSteer()
	if !ok {
		t.Fatal("expected a parked steer trigger")
	}
	reflector.err = nil
	if err := loop.Decide(context.Background(), trigger); err != nil {
		t.Fatalf("Decide with steer trigger: %v", err)
	}
	if !strings.Contains(reflector.lastUserPrompt, "User steering: focus on flaky tests") {
		t.Errorf("assess prompt missing steer line; got:\n%s", reflector.lastUserPrompt)
	}
	if !strings.Contains(reflector.lastUserPrompt, "steer") {
		t.Errorf("assess prompt missing steer source marker; got:\n%s", reflector.lastUserPrompt)
	}
}

// TestBuildAssessUserPrompt_SteerSourceOnly: a non-steer trigger gets no
// steer line; a steer trigger carries the content from the payload.
func TestBuildAssessUserPrompt_SteerSourceOnly(t *testing.T) {
	c := testTier1Constitution()

	plain := buildAssessUserPrompt(c, basicTrigger())
	if strings.Contains(plain, "User steering:") {
		t.Error("non-steer trigger produced a steer line")
	}

	steer := buildAssessUserPrompt(c, TriggerEvent{
		Source:  "steer",
		Payload: []byte(`{"content":"check the deploy"}`),
		FiredAt: time.Now().UTC(),
	})
	if !strings.Contains(steer, "User steering: check the deploy") {
		t.Errorf("steer trigger assess prompt missing steering line; got:\n%s", steer)
	}
}

// TestBuildAssessUserPrompt_SteerNoContent: an empty steer payload must not
// produce a dangling "User steering:" with nothing after it.
func TestBuildAssessUserPrompt_SteerNoContent(t *testing.T) {
	c := testTier1Constitution()
	out := buildAssessUserPrompt(c, TriggerEvent{
		Source:  "steer",
		Payload: []byte(`{}`),
		FiredAt: time.Now().UTC(),
	})
	if strings.Contains(out, "User steering:") {
		t.Errorf("empty steer payload produced a steer line; got:\n%s", out)
	}
}

// TestGoalLoopSteer_ParkedOnly_KeepsTrigger verifies the has-active-episode
// predicate shape used by Steer: with a parked steer record present, Steer
// still enqueues (dedup is not the steer path's job — the invoke mutex
// serializes user steering).
func TestGoalLoopSteer_ParkedOnly_KeepsTrigger(t *testing.T) {
	loop := NewGoalLoop("emp-steer-keep", testTier1Constitution(), nil, nil)
	loop.steerMu.Lock()
	loop.pendingSteer = append(loop.pendingSteer, TriggerEvent{Source: "steer"})
	loop.steerMu.Unlock()

	if _, err := loop.Steer(context.Background(), "second nudge"); err != nil {
		t.Fatalf("Steer error: %v", err)
	}
	if l := loop.PendingSteerCount(); l != 2 {
		t.Errorf("pending steer count = %d, want 2", l)
	}
}

// ---------------------------------------------------------------------------
// Manager.SteerEmployee
// ---------------------------------------------------------------------------

// TestSteerEmployee_NoLoop_Nil verifies that steering an employee with no
// registered GoalLoop is a nil no-op (the chat-quiet counterpart).
func TestSteerEmployee_NoLoop_Nil(t *testing.T) {
	m := NewManager(nil)
	queued, err := m.SteerEmployee(context.Background(), "emp-noloop", "hello")
	if err != nil {
		t.Fatalf("SteerEmployee with no loop returned error, want nil: %v", err)
	}
	if queued {
		t.Error("SteerEmployee with no loop reported queued=true, want false")
	}
}

// TestSteerEmployee_DelegatesToLoopSteer verifies the manager delegates to
// the registered loop's Steer with the given content.
func TestSteerEmployee_DelegatesToLoopSteer(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-steer-mgr", testTier1Constitution(), nil, nil).
		WithReflector(reflector)
	m := NewManager(nil)
	m.RegisterGoalLoop("emp-steer-mgr", loop)

	if queued, err := m.SteerEmployee(context.Background(), "emp-steer-mgr", "nudge"); err != nil {
		t.Fatalf("SteerEmployee error: %v", err)
	} else if queued {
		t.Error("quiet steer reported queued=true, want false")
	}
	if l := loop.PendingSteerCount(); l != 0 {
		t.Errorf("quiet steer enqueued a trigger; pending = %d, want 0", l)
	}
	if got := reflector.CallCount(); got != 0 {
		t.Errorf("reflector called %d times, want 0", got)
	}
}

// TestSteerEmployee_SerializesWithTrigger proves SteerEmployee acquires the
// SAME per-employee invoke mutex as triggerViaGoalLoop: a trigger holding
// the mutex blocks the steer until released.
func TestSteerEmployee_SerializesWithTrigger(t *testing.T) {
	reflector := newStubReflector()
	loop := NewGoalLoop("emp-steer-serial", testTier1Constitution(), nil, nil).
		WithReflector(reflector)
	m := NewManager(nil)
	m.RegisterGoalLoop("emp-steer-serial", loop)

	release := make(chan struct{})
	mu := m.acquireInvokeMutex("emp-steer-serial")
	mu.Lock()
	go func() {
		<-release
		mu.Unlock()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := m.SteerEmployee(context.Background(), "emp-steer-serial", "nudge")
		done <- err
	}()

	select {
	case err := <-done:
		close(release)
		t.Fatalf("SteerEmployee ran while the invoke mutex was held — wrong mutex or no lock (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
		// Still blocked: the steer is waiting on the invoke mutex. Good.
		close(release)
	}
	if err := <-done; err != nil {
		t.Fatalf("SteerEmployee error after release: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RPC surface
// ---------------------------------------------------------------------------

// TestRPCHandler_Steer_NilManager verifies agents.steer returns
// errNotConfigured with a nil manager.
func TestRPCHandler_Steer_NilManager(t *testing.T) {
	h := NewRPCHandler(nil)
	fn, ok := h.Handlers()["agents.steer"]
	if !ok {
		t.Fatal("agents.steer not registered")
	}
	if _, err := fn(context.Background(), json.RawMessage(`{"id":"x","content":"c"}`)); !errors.Is(err, errNotConfigured) {
		t.Fatalf("want errNotConfigured, got %v", err)
	}
}

// TestRPCHandler_Steer_RoundTrip drives agents.steer through the handler
// against a real Manager and checks validation + delegation.
func TestRPCHandler_Steer_RoundTrip(t *testing.T) {
	loop := NewGoalLoop("emp-steer-rpc", testTier1Constitution(), nil, nil)
	m := NewManager(nil)
	m.RegisterGoalLoop("emp-steer-rpc", loop)
	h := NewRPCHandler(m)

	// Missing employee_id.
	if _, err := h.handleSteer(context.Background(), json.RawMessage(`{"content":"c"}`)); err == nil {
		t.Error("missing employee_id: want error")
	}
	// Missing content.
	if _, err := h.handleSteer(context.Background(), json.RawMessage(`{"employee_id":"x"}`)); err == nil {
		t.Error("missing content: want error")
	}
	// Valid call (quiet employee → no-op success; handler still answers
	// status "ok" with steer_queued=false).
	out, err := h.handleSteer(context.Background(), json.RawMessage(`{"employee_id":"emp-steer-rpc","content":"nudge"}`))
	if err != nil {
		t.Fatalf("handleSteer: %v", err)
	}
	resp, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("response shape = %T, want map", out)
	}
	if queued, ok := resp["steer_queued"].(bool); !ok || queued {
		t.Errorf("steer_queued = %v, want false (quiet employee)", resp["steer_queued"])
	}
	if id, ok := resp["id"].(string); !ok || id != "emp-steer-rpc" {
		t.Errorf("id = %v, want emp-steer-rpc", resp["id"])
	}
}

// ---------------------------------------------------------------------------
// HTTP surface
// ---------------------------------------------------------------------------

// TestSteerRouteRegistered: POST /api/v1/agents/{id}/steer dispatches to
// agents.steer (route-exists probe with a nil rpc → 503).
func TestSteerRouteRegistered(t *testing.T) {
	h := NewAgentAPIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/emp1/steer", strings.NewReader(`{"content":"nudge"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("POST .../steer is not registered")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (nil rpc)", rec.Code)
	}
}

// TestSteerHandler_DispatchesToRPC verifies the HTTP handler forwards the
// path id and body content to agents.steer.
func TestSteerHandler_DispatchesToRPC(t *testing.T) {
	var gotMethod string
	var gotParams map[string]any
	h := NewAgentAPIHandler(func(_ context.Context, method string, params json.RawMessage) (any, error) {
		gotMethod = method
		if err := json.Unmarshal(params, &gotParams); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "steer_queued": false}, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/emp42/steer", strings.NewReader(`{"content":"nudge"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if gotMethod != "agents.steer" {
		t.Errorf("dispatched method = %q, want agents.steer", gotMethod)
	}
	if id, ok := gotParams["employee_id"].(string); !ok || id != "emp42" {
		t.Errorf("params employee_id = %v, want emp42", gotParams["employee_id"])
	}
	if content, ok := gotParams["content"].(string); !ok || content != "nudge" {
		t.Errorf("params content = %v, want nudge", gotParams["content"])
	}
}

// TestSteerRoute_MissingID verifies the path id gate.
func TestSteerRoute_MissingID(t *testing.T) {
	called := false
	h := NewAgentAPIHandler(func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
		called = true
		return map[string]any{}, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	// Go 1.22 ServeMux {id} wildcard does not match the empty segment, so
	// probe the validation indirectly: a request to the steer route with a
	// valid id MUST reach the RPC (the negative case is unrepresentable on
	// this mux; the check in handleSteerHTTP guards direct callers).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/emp9/steer", strings.NewReader(`{"content":"x"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Error("rpc callback not invoked")
	}
}

// ---------------------------------------------------------------------------
// helpers (scoped to this file; do not reuse names from other test files)
// ---------------------------------------------------------------------------

// promptCapturingReflector records the last user prompt passed to Chat.
type promptCapturingReflector struct {
	mu             sync.Mutex
	err            error
	next           *llm.Response
	lastUserPrompt string
}

func (r *promptCapturingReflector) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(messages) > 0 {
		r.lastUserPrompt = messages[len(messages)-1].Content
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.next != nil {
		return r.next, nil
	}
	return &llm.Response{Content: `{"candidates":[]}`}, nil
}

func (r *promptCapturingReflector) Config() *llm.ModelConfig { return nil }

func (r *promptCapturingReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return r.Chat(ctx, msgs, opts...)
}
