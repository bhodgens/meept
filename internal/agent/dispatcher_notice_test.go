package agent

// 02-routing TEST-GATE T2 (W22; audit-routing R4/R6; master matrix F7/F6).
//
// Two pins:
//
//  1. TestNoticePopulated - ClassificationNotice (dispatcher.go:231) has
//     ZERO writers today and llm.ClassificationUserGuidance is unused.
//     Every degraded classification lane must carry the user-facing
//     guidance so C1's routing_notice key can surface it. RED today:
//     the field is never assigned on any fallback lane.
//
//  2. TestHintCapabilityFit - a research-shaped planner step carrying
//     tool_hint=code is accepted silently and routed to the coder, which
//     has NO web_fetch grant (config/agents/coder/AGENT.md), while the
//     researcher HAS web_fetch+file_write. The hint must be remapped via
//     the authoritative table (config.ToolHintAgent) or an explicit
//     routing.warning mismatch event emitted - never a silent
//     coder-without-web_fetch. RED today: hint passes through unchanged
//     with no warning.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// requireNotice pins the C1 notice contract on a degraded dispatch: the
// DispatchResult must carry ClassificationNotice (dispatcher.go:231, zero
// writers today) sourced from llm.ClassificationUserGuidance for the
// observed failure kind - brief, user-language, never a raw error dump.
// The handler already READS the field (handler.go:1049/1258); the gap is
// purely on the write side.
func requireNotice(t *testing.T, res *DispatchResult, failureErr error, lane string) {
	t.Helper()
	if res == nil {
		t.Fatalf("%s lane produced nil DispatchResult", lane)
	}
	if res.ClassificationNotice == "" {
		t.Errorf("%s lane: ClassificationNotice is empty; degraded classification must disclose the failure to the user (C1 routing_notice)", lane)
		return
	}
	want := llm.ClassificationUserGuidance(failureErr)
	if want != "" && res.ClassificationNotice != want {
		t.Errorf("%s lane: ClassificationNotice = %q, want the ClassificationUserGuidance text %q",
			lane, res.ClassificationNotice, want)
	}
}

// noticeGateInput is long enough to pass the short-message guard and
// carries no compound signal, so the full classifier chain runs.
const noticeGateInput = "Create a file named hello.txt in the project directory with the text 'hello world'."

// TestNoticePopulated_EmptyResponseLane drives the real ClassifyAndRoute
// with a classifier endpoint that answers empty content: the chain
// degrades to llm_empty_fallback_chat and the result must attach the
// empty-response guidance.
func TestNoticePopulated_EmptyResponseLane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "empty"}),
	})

	res, err := d.ClassifyAndRoute(context.Background(), noticeGateInput, "sess-notice-empty", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || res.Intent == nil || res.Intent.Method != "llm_empty_fallback_chat" {
		t.Fatalf("expected the empty-response fallback lane, got %+v", res)
	}
	requireNotice(t, res, llm.ErrEmptyResponse, "llm_empty_fallback_chat")
}

// TestNoticePopulated_UnavailableLane drives a classifier endpoint that
// hard-fails (HTTP 400, non-retryable): the chain degrades past the LLM and
// the result must attach the failure guidance on the lane that answers.
func TestNoticePopulated_UnavailableLane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"classifier offline"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "offline"}),
	})

	res, err := d.ClassifyAndRoute(context.Background(), noticeGateInput, "sess-notice-offline", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || res.Intent == nil {
		t.Fatal("nil result/intent from degraded chain")
	}
	if res.Intent.Method == "llm" {
		t.Fatalf("canned failure must not classify via llm; got %+v", res.Intent)
	}
	failureErr := &llm.APIError{StatusCode: http.StatusBadRequest, Detail: "classifier offline"}
	requireNotice(t, res, failureErr, res.Intent.Method)
}

// TestNoticePopulated_CooldownLane arms the classifier cooldown with one
// failure, then dispatches again: the cooldown skip is a degraded lane too
// and must carry the notice (llm_classifier.go cooldown arming).
func TestNoticePopulated_CooldownLane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"classifier offline"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "offline"}),
	})

	first, err := d.ClassifyAndRoute(context.Background(), noticeGateInput, "sess-notice-cooldown", nil, "", "")
	if err != nil {
		t.Fatalf("first ClassifyAndRoute: %v", err)
	}
	if first == nil {
		t.Fatal("nil first result")
	}

	// Second call inside the cooldown window: the classifier is skipped.
	second, err := d.ClassifyAndRoute(context.Background(), noticeGateInput, "sess-notice-cooldown", nil, "", "")
	if err != nil {
		t.Fatalf("cooldown ClassifyAndRoute: %v", err)
	}
	if second == nil {
		t.Fatal("nil cooldown result")
	}
	failureErr := &llm.APIError{StatusCode: http.StatusBadRequest, Detail: "classifier offline"}
	requireNotice(t, second, failureErr, "cooldown")
}

// TestHintCapabilityFit_ResearchStepCodeHint drives the REAL planner hint
// acceptance (parsePlanOutput -> validateToolHint): a research-shaped step
// with tool_hint=code must not silently stay on the coder lane. Either the
// hint is remapped to the researcher-capable vocabulary or an explicit
// routing.warning mismatch event fires. The shipped roster is the
// capability ground truth: researcher HAS web_fetch, coder does NOT.
func TestHintCapabilityFit_ResearchStepCodeHint(t *testing.T) {
	// Capability ground truth from the shipped AGENT.md files (ASCII grep
	// at gate authoring): researcher lists web_fetch; coder does not.
	if !agentRosterHasTool(t, config.AgentIDResearcher, "web_fetch") {
		t.Fatal("fixture drift: researcher AGENT.md must list web_fetch")
	}
	if agentRosterHasTool(t, config.AgentIDCoder, "web_fetch") {
		t.Fatal("fixture drift: coder AGENT.md must NOT list web_fetch (the mismatch this gate pins)")
	}
	// The authoritative hint table maps research -> researcher.
	if agent, ok := config.ToolHintAgent("research"); !ok || agent != config.AgentIDResearcher {
		t.Fatalf("hint table drift: research -> %q (ok=%v), want researcher", agent, ok)
	}

	msgBus := bus.New(nil, testLogger())
	t.Cleanup(msgBus.Close)
	warnSub := msgBus.Subscribe("gate-hint-warnings", "routing.warning")
	t.Cleanup(func() { msgBus.Unsubscribe(warnSub) })

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := taskStore.Close(); closeErr != nil {
			t.Errorf("close task store: %v", closeErr)
		}
	})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		Logger:         testLogger(),
		TemplateLoader: newPlannerTemplateLoader(t.TempDir()),
		MaxPlanSteps:   5,
		PlannerTimeout: 5 * time.Second,
	})

	planJSON := `{"steps":[{"description":"Research current Go 1.27 release notes on the web and summarize the changes","tool_hint":"code","depends_on":[]}]}`
	steps, err := sp.parsePlanOutput("task-hint-fit", planJSON)
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := steps[0]

	remapped := step.ToolHint != "code"
	var warned bool
	select {
	case msg := <-warnSub.Channel:
		if msg != nil {
			var payload map[string]any
			if json.Unmarshal(msg.Payload, &payload) == nil {
				warned = true
			}
		}
	case <-time.After(500 * time.Millisecond):
	}

	if !remapped && !warned {
		t.Errorf("research-shaped step kept tool_hint=%q with no routing.warning: the step routes to %s which has no web_fetch grant; remap via the hint table or emit an explicit mismatch event",
			step.ToolHint, config.AgentIDCoder)
	}
	if remapped {
		agent, ok := config.ToolHintAgent(step.ToolHint)
		if !ok || !agentRosterHasTool(t, agent, "web_fetch") {
			t.Errorf("remapped hint %q resolves to %q (ok=%v) which still lacks web_fetch", step.ToolHint, agent, ok)
		}
	}
}

// TestHintCapabilityFit_CodeStepUnchanged is the over-correction guard: a
// genuine code step keeps its code hint with no mismatch warning - the fit
// check must not remap work that already matches its executor.
func TestHintCapabilityFit_CodeStepUnchanged(t *testing.T) {
	msgBus := bus.New(nil, testLogger())
	t.Cleanup(msgBus.Close)
	warnSub := msgBus.Subscribe("gate-hint-warnings-ok", "routing.warning")
	t.Cleanup(func() { msgBus.Unsubscribe(warnSub) })

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := taskStore.Close(); closeErr != nil {
			t.Errorf("close task store: %v", closeErr)
		}
	})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		Logger:         testLogger(),
		TemplateLoader: newPlannerTemplateLoader(t.TempDir()),
		MaxPlanSteps:   5,
		PlannerTimeout: 5 * time.Second,
	})

	planJSON := `{"steps":[{"description":"Implement a Go function that parses the CSV export","tool_hint":"code","depends_on":[]}]}`
	steps, err := sp.parsePlanOutput("task-hint-fit-ok", planJSON)
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if len(steps) != 1 || steps[0].ToolHint != "code" {
		t.Fatalf("genuine code step hint changed: %+v", steps)
	}
	select {
	case msg := <-warnSub.Channel:
		if msg != nil {
			t.Errorf("unexpected routing.warning for a capability-matched code step: %s", msg.Payload)
		}
	default:
	}
}

// agentRosterHasTool reads the shipped AGENT.md tool list for a grant.
// The roster files are the capability ground truth the fit check must
// consult (config/agents/<id>/AGENT.md frontmatter tools list). The
// repo root is two levels above the package test cwd.
func agentRosterHasTool(t *testing.T, agentID, tool string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "agents", agentID, "AGENT.md"))
	if err != nil {
		t.Fatalf("read roster %s: %v", agentID, err)
	}
	// Frontmatter list-item form: "  - web_fetch". Whole-line match so
	// "web_fetch_extra" cannot false-match, and prose mentions of the
	// tool name do not count as a grant.
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "- "+tool {
			return true
		}
	}
	return false
}
