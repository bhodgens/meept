package agent

// Pins for the orphan-task regression the 2026-09-12 C-0 wave introduced.
//
// The wave fixed the QUICKPLAN async dead-end by making the async gate open
// for every lane that creates a task. IntentSchedule creates a task
// (ShouldCreateTask()=true) but is synchronously dispatched
// (ShouldDispatchAsync(schedule, RequiresPlanning())=false, because
// RequiresPlanning() has no schedule case). The handler's async branch is the
// ONLY consumer of Result.Task (handler.go:770), so the row dispatcher.go
// wrote was orphaned the moment it landed and the scheduler never ran it —
// the same dead-end the wave closed for quickplan, inverted. Task creation is
// now gated on the dispatch actually consuming the task.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// TestDispatchConsumesTask pins the predicate directly: a schedule intent
// creates a task but nothing consumes it; pair/collaborate and the async
// lanes do.
func TestDispatchConsumesTask(t *testing.T) {
	d := &Dispatcher{}

	sched := &Intent{Type: string(IntentSchedule)}
	if !IntentType(sched.Type).ShouldCreateTask() {
		t.Fatal("precondition: schedule must still be a task-creating intent")
	}
	if IntentType(sched.Type).ShouldDispatchAsync(sched.RequiresPlanning) {
		t.Fatal("precondition: a simple schedule intent must be synchronously dispatched")
	}
	if d.dispatchConsumesTask(sched) {
		t.Fatal("dispatchConsumesTask(schedule) = true; the created task is never read by any handler branch (orphan)")
	}

	for _, tc := range []struct {
		intent string
		want   bool
	}{
		{string(IntentPair), true},
		{string(IntentCollaborate), true},
		{string(IntentCode), true},
		{string(IntentQuickPlan), true},
		{string(IntentChat), false},
	} {
		in := &Intent{Type: tc.intent, RequiresPlanning: IntentType(tc.intent).RequiresPlanning()}
		if got := d.dispatchConsumesTask(in); got != tc.want {
			t.Errorf("dispatchConsumesTask(%s) = %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// newScheduleAwareClassifierServer answers the analyzer, the multi-intent
// detector, and the single-intent classifier so one server drives a full
// ClassifyAndRoute turn ending on the schedule lane.
func newScheduleAwareClassifierServer(t *testing.T) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		var prompt strings.Builder
		for _, m := range req.Messages {
			prompt.WriteString(m.Content)
			prompt.WriteString("\n")
		}
		var content string
		switch {
		case strings.Contains(prompt.String(), "identify ALL distinct intents"):
			content = `[]` // no compound
		case strings.Contains(prompt.String(), "Classify this user input"):
			content = `{"intent":"schedule","confidence":0.92,"reasoning":"scheduling request"}`
		default:
			// Intent analyzer: unambiguous.
			content = `{"goal":"schedule the reminder","ambiguity":0.1,"scope":"narrow","category":"schedule","suggested_questions":[],"confidence":0.9}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "capture"})
}

// TestClassifyAndRoute_ScheduleCreatesNoOrphanTask is the end-to-end pin: a
// schedule request routed through the real chain must not leave a task row,
// because its dispatch is synchronous and no handler branch reads the task.
func TestClassifyAndRoute_ScheduleCreatesNoOrphanTask(t *testing.T) {
	logger := digestTestLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: newScheduleAwareClassifierServer(t),
		Logger:           logger,
	})

	// Carries an explicit time signal, so the schedule intent is never
	// recall-arbitrated; the single-intent classifier decides.
	const input = "remind me to review the deployment checklist next week on Friday before the release window opens"

	res, err := d.ClassifyAndRoute(context.Background(), input, "sess-orphan-sched", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent")
	}
	if res.Intent.Type != string(IntentSchedule) {
		t.Fatalf("intent = %q (method %q), want schedule", res.Intent.Type, res.Intent.Method)
	}
	// Precondition: the async gate is shut for this verdict, so a task here
	// is exactly the orphan.
	if d.ShouldDispatchAsync(res) {
		t.Fatal("precondition: a schedule verdict must not dispatch async")
	}
	if res.Task != nil {
		t.Fatalf("schedule dispatch created task %q that no handler branch consumes (orphan)", res.Task.ID)
	}
}

// TestResumeAfterClarification_ScheduleCreatesNoOrphanTask pins the SECOND
// task-creation site. ResumeAfterClarification (reached from ClassifyAndRoute
// when a clarification is pending) created its task on shouldCreateTask alone,
// so an ambiguous question answered with a schedule intent still wrote an
// orphaned row — the same shape the C-0 wave closed on the primary route.
func TestResumeAfterClarification_ScheduleCreatesNoOrphanTask(t *testing.T) {
	logger := digestTestLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: newScheduleAwareClassifierServer(t),
		Logger:           logger,
	})
	// Unambiguous analyzer verdict so the resume proceeds to classification
	// instead of asking another follow-up question.
	d.intentAnalyzer = ambiguityTestServer(t, 0.1)

	const sessionID = "sess-orphan-resume"
	// Seed the pending clarification exactly as the ambiguity gate leaves it.
	clarify := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.9,
		Summary:    "remind me about the deployment",
	}
	d.sessionTracker.RecordIntent(sessionID, clarify, "")

	// The answer carries an explicit time signal, so the schedule verdict is
	// never recall-arbitrated and the classifier decides.
	res, err := d.ResumeAfterClarification(context.Background(),
		"remind me about the deployment",
		"next week on Friday before the release window opens",
		sessionID)
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	if res == nil || res.Intent == nil {
		t.Fatal("nil result or intent")
	}
	if res.Intent.Type != string(IntentSchedule) {
		t.Fatalf("intent = %q (method %q), want schedule", res.Intent.Type, res.Intent.Method)
	}
	if d.ShouldDispatchAsync(res) {
		t.Fatal("precondition: a schedule verdict must not dispatch async")
	}
	if res.Task != nil {
		t.Fatalf("resume created task %q that no handler branch consumes (orphan)", res.Task.ID)
	}
	rows, err := reg.Store().List(nil, 100)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("resume wrote %d task row(s) for a synchronous schedule intent (orphan)", len(rows))
	}
}
