package agent

// Named-agent routing pins (acp-employees follow-up). The live smoke defect
// (2026-10-23): "have bob introduce himself and list two files in this
// repo" was classified + planned; the planner prose said "hand the task to
// bob via request_handoff"; the capability table assigned committer; the
// handoff never fired; committer's canned chat line answered the turn.
//
// Pins below prove:
//   - a message addressing a roster agent by name classifies with
//     AgentID=bob and routes the turn to bob's loop;
//   - a name not in the roster falls through to the normal chain;
//   - the @bob / ask bob / let bob / bob should variants;
//   - a plan step whose description names a roster agent gets step.AgentID
//     stamped, beating the hint table;
//   - a handoff keeps the target's AgentID on the created step and the
//     session-scoped bob loop reaches the ACP seam (runTurnProducer's acp
//     branch, never reasoningCycle);
//   - an unknown handoff target errors, listing the roster names.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// chatTestSpec is the local chat agent spec (first-class roster member —
// the pins need both agent kinds present to prove routing is kind-agnostic).
func chatTestSpec() *AgentSpec {
	return &AgentSpec{
		ID:      config.AgentIDChat,
		Name:    "chat",
		Role:    RoleExecutor,
		Enabled: true,
		Purpose: "general chat",
		Constraints: AgentConstraints{
			MaxIterations: 3,
			Timeout:       defaultTestTimeout(),
		},
	}
}

func namedTestRegistry(t *testing.T, provider *fakeACPSessionProvider) *AgentRegistry {
	t.Helper()
	registry := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
	if err := registry.RegisterSpec(chatTestSpec()); err != nil {
		t.Fatalf("RegisterSpec chat: %v", err)
	}
	if err := registry.RegisterSpec(acpTestSpec()); err != nil {
		t.Fatalf("RegisterSpec bob: %v", err)
	}
	if provider != nil {
		registry.SetACPExecutor(newACPExecutorFromProvider(provider))
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

// namedRouteDispatcher wires the production chain the routing pins drive:
// registry (chat + bob), task store, loop manager + session store with a
// project path (so RouteToAgent resolves a SESSION-scoped loop through
// GetOrCreateWired — the exact clone path the ACP stamp must survive), and
// a mock chatter installed on the registry templates so a local-agent turn
// never panics on the llm-nil guard (the ACP lane must never call it).
func namedRouteDispatcher(t *testing.T, workdir string) (*Dispatcher, *AgentRegistry, *fakeACPSessionProvider, *mockChatter) {
	t.Helper()
	logger := slogDiscardLogger()

	provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "hi, I am bob — here are two files"}}
	registry := namedTestRegistry(t, provider)

	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	chatter := newMockChatter(&llm.Response{Content: "llm must never answer on the acp lane"})
	// The registry templates need an llm client so RunOnceWithParts passes
	// its entry guard; GetOrCreateWired copies it into the clone via
	// ConfigSnapshot. The ACP branch returns before reasoningCycle, so the
	// chatter's call count stays 0 — which is the assertion.
	for _, id := range []string{"bob", config.AgentIDChat} {
		if template, terr := registry.Get(id); terr == nil && template != nil {
			template.llm = chatter
		}
	}

	d := NewDispatcher(DispatcherConfig{
		Registry:     registry,
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		Logger:       logger,
	})
	d.SetAgentLoopManager(NewManager(ManagerConfig{}))
	d.sessionStore = &stubSessionStore{
		sessions: map[string]*session.Session{
			"sess-named": {ID: "sess-named", ConversationID: "sess-named", ProjectPath: workdir},
		},
	}
	return d, registry, provider, chatter
}

// 1. "have bob X" classifies with AgentID=bob, method name_match, and the
// handler's default branch (ShouldDispatchAsync false, no Task) routes the
// FULL turn through RouteToAgent to bob's loop.
func TestClassifyAndRoute_NamedAgent_RoutesToBob(t *testing.T) {
	d, _, provider, chatter := namedRouteDispatcher(t, t.TempDir())

	res, err := d.ClassifyAndRoute(context.Background(),
		"have bob introduce himself and list two files in this repo",
		"sess-named", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.AgentID != "bob" {
		t.Errorf("AgentID = %q, want bob", res.AgentID)
	}
	if res.Intent == nil || res.Intent.Method != "name_match" {
		t.Fatalf("expected name_match classification, got %+v", res.Intent)
	}
	if res.Intent.RequiresPlanning {
		t.Error("name-matched turn must not demand planning")
	}
	if d.ShouldDispatchAsync(res) {
		t.Error("name-matched chat turn must dispatch synchronously (no orchestrator)")
	}
	if res.Task != nil {
		t.Error("name-matched turn must not create a tracked task")
	}

	reply, err := d.RouteToAgent(context.Background(), res, "sess-named")
	if err != nil {
		t.Fatalf("RouteToAgent: %v", err)
	}
	if !strings.Contains(reply, "bob") {
		t.Errorf("reply = %q, want the harness introduction", reply)
	}
	// The turn reached the ACP seam, not the local reasoning cycle.
	if provider.calls != 1 {
		t.Errorf("GetOrCreatePersona calls = %d, want 1 (acp producer must run)", provider.calls)
	}
	if chatter.callCount != 0 {
		t.Errorf("llm calls = %d, want 0 — reasoningCycle ran instead of the acp producer", chatter.callCount)
	}
	if len(provider.sess.sent) != 1 || !strings.Contains(provider.sess.sent[0], "introduce himself") {
		t.Errorf("session prompts = %v, want the full user turn", provider.sess.sent)
	}
	// The session-scoped loop the turn ran on is bob's, not chat's.
	clone, ok := d.loopManager.loops[namedSessionLoopKey("sess-named", "bob")]
	if !ok {
		t.Fatal("expected a per-agent session-scoped loop for bob")
	}
	if clone.GetAgentID() != "bob" {
		t.Errorf("session loop agent = %q, want bob", clone.GetAgentID())
	}
}

func namedSessionLoopKey(sessionID, agentID string) string {
	return sessionID + "\x00" + agentID
}

// 2. Name not in the roster falls through to the normal chain (no
// name_match method, no route to a phantom agent).
func TestClassifyAndRoute_NamedAgent_UnknownNameFallsThrough(t *testing.T) {
	d, _, _, _ := namedRouteDispatcher(t, t.TempDir())

	res, err := d.ClassifyAndRoute(context.Background(),
		"have notrealagent do the thing", "sess-named", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent != nil && res.Intent.Method == "name_match" {
		t.Errorf("unknown roster name must not name_match, got %+v", res.Intent)
	}
	if res.AgentID == "notrealagent" {
		t.Error("dispatched to a non-existent agent")
	}
}

// 3. Variants: @bob, ask bob, let bob, bob should.
func TestClassifyAndRoute_NamedAgent_Variants(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"mention", "@bob run the tests"},
		{"mention mid", "first summarize, then @bob run the tests"},
		{"ask", "ask bob to introduce himself"},
		{"let", "let bob check that issue"},
		{"should", "bob should list two files"},
		{"colon", "bob: introduce yourself"},
		{"comma", "bob, run the tests"},
		{"have", "have bob introduce himself"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, _, _ := namedRouteDispatcher(t, t.TempDir())
			res, err := d.ClassifyAndRoute(context.Background(), tc.input, "sess-named", nil, "", "")
			if err != nil {
				t.Fatalf("ClassifyAndRoute: %v", err)
			}
			if res.AgentID != "bob" || res.Intent.Method != "name_match" {
				t.Errorf("input %q → agent %q method %q, want bob/name_match",
					tc.input, res.AgentID, res.Intent.Method)
			}
		})
	}
}

// A periphrastic question must NOT hijack the roster name ("do you have
// coder available?" is a status question, not an assignment to coder).
func TestNamedAgentCandidate_QuestionsDoNotRoute(t *testing.T) {
	specs := []*AgentSpec{chatTestSpec(), acpTestSpec()}
	resolve := resolveNamedRosterAgent(specs)
	for _, q := range []string{
		"do you have coder available for this?",
		"we have coder running since boot",
		"is bob still busy?",
		"write a function that handles bob's request",
		"the planner should be faster than coder",
	} {
		if id, ok := namedAgentCandidate(q, resolve); ok {
			t.Errorf("question %q name-routed to %q, want fallthrough", q, id)
		}
	}
}

// 4. Step-level name preservation: a plan step whose description names a
// roster agent gets step.AgentID = that agent, beating the hint table
// (the live defect: hint "git" → committer while the prose said bob).
func TestAssignStepAgent_RosterNameBeatsHintTable(t *testing.T) {
	logger := slogDiscardLogger()
	registry := namedTestRegistry(t, nil)
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		Logger:   logger,
		Registry: registry,
	})

	step := task.NewTaskStep("task-named-step", "Hand the task to bob via request_handoff", 0)
	step.ToolHint = "git" // capability table would say committer
	ts.assignStepAgent(step)
	if step.AgentID != "bob" {
		t.Fatalf("step.AgentID = %q, want bob (name beats hint table)", step.AgentID)
	}

	// No roster name in the description → hint table path untouched.
	plain := task.NewTaskStep("task-named-step", "Commit the pending changes", 1)
	plain.ToolHint = "git"
	ts.assignStepAgent(plain)
	if plain.AgentID != config.AgentIDCommitter {
		t.Errorf("hint-only step AgentID = %q, want committer", plain.AgentID)
	}

	// Explicit assignment still wins over both.
	explicit := task.NewTaskStep("task-named-step", "ask bob to review", 2)
	explicit.AgentID = "reviewer"
	ts.assignStepAgent(explicit)
	if explicit.AgentID != "reviewer" {
		t.Errorf("explicit AgentID overwritten: %q", explicit.AgentID)
	}
}

// 5a. Handoff keeps the target: the direct-step path must stamp the handoff
// step with req.ToAgentID, including executor: acp agents — otherwise
// assignStepAgent re-derives the executor from the hint round-trip
// (agentIDToToolHint("bob")="chat") and the handoff silently runs in the
// chat agent.
func TestHandleHandoff_StepCarriesTargetAgentID(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	registry := namedTestRegistry(t, nil)

	tk := task.NewTask("task-handoff-bob", "handoff to acp agent")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	fromStep := task.NewTaskStep(tk.ID, "initial step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("create from step: %v", err)
	}

	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
		Registry:  registry,
	})

	req := HandoffRequest{
		TaskID:      tk.ID,
		FromStepID:  fromStep.ID,
		FromAgentID: config.AgentIDCoder,
		ToAgentID:   "bob",
		Description: "Introduce yourself and list two files",
		InjectAfter: false,
	}
	msg := handoffBusMsg(req)
	if err := ts.HandleHandoff(context.Background(), msg); err != nil {
		t.Fatalf("HandleHandoff: %v", err)
	}

	steps, err := stepStore.ListByTaskID(tk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	var newStep *task.TaskStep
	for _, s := range steps {
		if s.ID != fromStep.ID {
			newStep = s
		}
	}
	if newStep == nil {
		t.Fatal("handoff step not created")
	}
	if newStep.AgentID != "bob" {
		t.Errorf("handoff step AgentID = %q, want bob (direct path dropped the target before)", newStep.AgentID)
	}
}

// 5b. A handoff step's job runs on the target's loop through the registry's
// task-scoped bucket (components.go: GetForTask(job.AgentID, job.TaskID)).
// The seam proof: a loop built for an executor: acp target enters the ACP
// producer, never reasoningCycle.
func TestTaskScopedACPLoop_ReachesACPSame(t *testing.T) {
	provider := &fakeACPSessionProvider{sess: &fakeACPSession{reply: "bob via acp seam"}}
	registry := namedTestRegistry(t, provider)
	chatter := newMockChatter(&llm.Response{Content: "must never answer"})

	loop, err := registry.GetForTask("bob", "task-handoff-seam")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}
	WithLLMChatter(chatter)(loop)
	loop.SetWorkingDir(t.TempDir())

	if _, err := loop.RunOnce(context.Background(), "Introduce yourself", "conv-handoff-seam"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("acp producer calls = %d, want 1", provider.calls)
	}
	if chatter.callCount != 0 {
		t.Errorf("reasoningCycle ran (llm calls = %d) on an acp-targeted handoff", chatter.callCount)
	}
}

// 6. Handoff to an unknown agent errors, listing available names.
func TestHandleHandoff_UnknownTargetErrorsWithRoster(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	registry := namedTestRegistry(t, nil)

	tk := task.NewTask("task-handoff-unknown", "unknown target")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	fromStep := task.NewTaskStep(tk.ID, "origin step", 1)
	fromStep.State = task.StepCompleted
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("create from step: %v", err)
	}

	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Queue:     &mockQueue{},
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
		Registry:  registry,
	})

	req := HandoffRequest{
		TaskID:      tk.ID,
		FromStepID:  fromStep.ID,
		ToAgentID:   "notrealagent",
		Description: "do the thing",
	}
	msg := handoffBusMsg(req)
	err := ts.HandleHandoff(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for unknown handoff target")
	}
	if !strings.Contains(err.Error(), "notrealagent") {
		t.Errorf("error %q must name the unknown target", err.Error())
	}
	for _, want := range []string{"bob", "chat"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must list available agent %q", err.Error(), want)
		}
	}
}

// Cache identity (manager fix): the session-loop cache is keyed per
// (session, agent). A conversation that first built a chat loop must NOT
// serve that cached chat clone when the next turn routes to bob — the
// cached chat-spec clone answered named-agent turns from the local LLM
// loop, skipping the ACP branch entirely. The two per-agent loops must
// coexist without evicting each other.
func TestRouteToAgent_PerAgentSessionLoopIdentity(t *testing.T) {
	workdir := t.TempDir()
	d, registry, provider, chatter := namedRouteDispatcher(t, workdir)

	// Turn 1: plain chat route builds the chat session loop (local LLM).
	chatResult := &DispatchResult{
		AgentID: config.AgentIDChat,
		Intent:  &Intent{Type: string(IntentChat), Summary: "hello there", Confidence: 0.9},
	}
	if _, err := d.RouteToAgent(context.Background(), chatResult, "sess-named"); err != nil {
		t.Fatalf("chat RouteToAgent: %v", err)
	}
	chatCalls := chatter.callCount
	if chatCalls == 0 {
		t.Fatal("chat turn should have entered the local reasoning cycle")
	}

	// Turn 2: "have bob ..." on the SAME conversation must run on bob's
	// loop through the ACP seam — not the cached chat clone.
	res, err := d.ClassifyAndRoute(context.Background(), "have bob introduce himself", "sess-named", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.AgentID != "bob" {
		t.Fatalf("AgentID = %q, want bob", res.AgentID)
	}
	reply, err := d.RouteToAgent(context.Background(), res, "sess-named")
	if err != nil {
		t.Fatalf("bob RouteToAgent: %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("acp producer calls = %d, want 1 (the chat clone must not swallow bob's turn)", provider.calls)
	}
	if !strings.Contains(reply, "bob") {
		t.Errorf("reply = %q, want the harness reply", reply)
	}
	if chatter.callCount != chatCalls {
		t.Errorf("local LLM calls grew on the bob turn (%d -> %d): bob ran the reasoning cycle", chatCalls, chatter.callCount)
	}

	// Both loops coexist under per-agent keys.
	_ = registry
	bobLoop, ok := d.loopManager.loops[namedSessionLoopKey("sess-named", "bob")]
	if !ok {
		t.Fatal("expected a per-agent session loop for bob")
	}
	if bobLoop.GetAgentID() != "bob" {
		t.Errorf("bob session loop agent = %q, want bob", bobLoop.GetAgentID())
	}
	chatLoop, ok := d.loopManager.loops[namedSessionLoopKey("sess-named", config.AgentIDChat)]
	if !ok || chatLoop.GetAgentID() != config.AgentIDChat {
		t.Error("the chat session loop must still exist under its own per-agent key")
	}
}
