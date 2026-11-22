package agent

// 03-context TEST-GATE T5 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md; findings F-ORCH-24, F-NEW-7, F-NEW-9; W10;
// 02-recall-fidelity T3).
//
// Fact questions route to the memory_search lane, never task creation /
// task_get decomposition:
//   - TestRecallFactToMemory: a planted session fact (codeword ZEBRA-4417,
//     frozen probe p2) asked back as "what was the codeword..." must be
//     answerable from the scoped memory lane with NO task row created.
//     Today the question classifies quickplan and decomposes into
//     task_get plumbing (F-ORCH-24).
//   - Scope control (I5): session B in a DIFFERENT project must not
//     recall session A's project-scoped fact through the dispatcher's
//     scope options (EffectiveMemoryScope / WithScopeFilter).
//   - TestStepPromptFidelity: an orchestrator step built from a user
//     prompt that names an explicit file path must carry that path into
//     the step description (F-NEW-9: the step lost the path and answered
//     with a clarification question for info already in the prompt).
//
// RED today for the gate arms. GREEN after 03-context/02. Controls pass
// today and prove the fixtures are not the failure reason.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	memcfg "github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// gateFactMemoryManager builds a real SQLite-backed memory manager.
func gateFactMemoryManager(t *testing.T) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: memcfg.MemoryConfig{
			Backend:  memcfg.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: memcfg.EpisodicConfig{Enabled: true},
		},
		Logger: testLogger(),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init memory manager: %v", err)
	}
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Errorf("close memory manager: %v", err)
		}
	})
	return mgr
}

// TestRecallFactToMemory pins fact->memory routing: the codeword question
// creates no task and the scoped memory lane holds the answer.
func TestRecallFactToMemory(t *testing.T) {
	const codeword = "ZEBRA-4417"
	const question = "What was the codeword I told you earlier in this session?"
	const sessA = "conv-fact-session-a"
	const projA = "proj-fact-alpha"

	mgr := gateFactMemoryManager(t)
	ctx := context.Background()

	// Plant the session fact exactly as ambient extraction would: a
	// project-scoped memory row bound to session A's project.
	if _, err := mgr.Store(ctx, memory.Memory{
		Type:      memory.MemoryTypeEpisodic,
		Category:  "conversation",
		Content:   "The user said the secret codeword for this session is " + codeword + ". It is also written in alpha_notes.txt.",
		SessionID: sessA,
		Scope:     memory.ScopeProject,
		Metadata:  map[string]any{"project_id": projA},
	}); err != nil {
		t.Fatalf("plant fact: %v", err)
	}

	// The classifier verdict the live rig produced for this question
	// (F-ORCH-24: quickplan -> 2-step task_get decomposition).
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"quickplan","confidence":0.9}`),
		MemoryMgr:        mgr,
		Registry:         NewAgentRegistry(RegistryConfig{Logger: testLogger()}),
	})
	reg, err := task.NewRegistry(t.TempDir()+"/tasks.db", bus.New(nil, nil), testLogger())
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("close task registry: %v", err)
		}
	})
	d.taskStore = reg.Store()
	d.taskRegistry = reg
	d.sessionStore = &stubSessionStore{sessions: map[string]*session.Session{
		"sess-fact-a": {ID: "sess-fact-a", ConversationID: sessA, ProjectID: projA},
	}}

	// Precondition control (passes today): the scoped memory lane CAN
	// answer the question - the fact is reachable, so the routing defect
	// is not a storage gap.
	results, err := mgr.Search(ctx, memory.MemoryQuery{Query: question, Limit: 5, MatchAny: true},
		d.sessionScopeOptions(memcfg.AgentIDChat, sessA)...)
	if err != nil {
		t.Fatalf("scoped memory search: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Memory.Content, codeword) {
			found = true
		}
	}
	if !found {
		t.Fatalf("control failed: scoped memory lane cannot see the planted fact (results=%d); fixture problem, not the gate", len(results))
	}

	// Gate arm (RED today): the fact question must not create a task.
	result, err := d.ClassifyAndRoute(ctx, question, sessA, nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if result.Task != nil {
		t.Fatalf("RED (F-ORCH-24): fact question created task %q (%s) - it must route to the memory_search lane, never task_get decomposition",
			result.Task.ID, result.Task.Name)
	}
	if result.Intent != nil && result.Intent.Type == string(IntentQuickPlan) {
		t.Fatalf("RED (F-ORCH-24): fact question still classified quickplan (method %q); fact-shaped questions must take the memory lane",
			result.Intent.Method)
	}

	// I5 scope control (passes today, must keep passing): session B in a
	// DIFFERENT project must not recall A's project-scoped fact.
	optsB := d.sessionScopeOptions(memcfg.AgentIDChat, "conv-fact-session-b")
	resultsB, err := mgr.Search(ctx, memory.MemoryQuery{Query: question, Limit: 5, MatchAny: true}, optsB...)
	if err != nil {
		t.Fatalf("session B search: %v", err)
	}
	for _, r := range resultsB {
		if strings.Contains(r.Memory.Content, codeword) {
			t.Fatalf("I5 scope leak: session B (unbound/foreign project) recalled session A's scoped codeword fact")
		}
	}
}

// TestStepPromptFidelity pins F-NEW-9: the orchestrator step the executor
// receives must carry the FULL user prompt's explicit file path, even
// when the planner's decomposition omits it. Live defect: the step lost
// the path and the turn answered with a clarification question for info
// already in the prompt ("What would you like me to validate? Please
// provide a file path...").
//
// Gate arm drives the REAL quick_plan planning path with a planner whose
// scripted decomposition is generic (exactly what the live 8B emitted):
// the persisted step must still carry the explicit path. RED today -
// nothing re-injects the full prompt when the planner drops it. GREEN
// after 03-context/02 guarantees the step input carries the full prompt.
func TestStepPromptFidelity(t *testing.T) {
	const missingPath = "/tmp/usability-rig-p3/definitely-missing-9f3k2.txt"
	const fullPrompt = "Read the file " + missingPath + " and tell me plainly what it contains. If the file does not exist, say so plainly - do not ask me for the path, it is in this message."

	// Control (passes today): direct-mode fallback carries req.Input
	// verbatim, so the path survives when no planner LLM is involved.
	spDirect := NewStrategicPlanner(StrategicPlannerConfig{Logger: testLogger()})
	directSteps := spDirect.createFallbackSteps(PlanRequest{
		TaskID: "task-fidelity-direct",
		Input:  fullPrompt,
		Intent: string(IntentCode),
		Mode:   "direct",
	}, nil)
	if len(directSteps) != 1 || !strings.Contains(directSteps[0].Description, missingPath) {
		t.Fatalf("control failed: direct-mode fallback lost the path: %+v", directSteps)
	}

	// The loss seam (diagnostic): extractSummary - the ~100-char summary
	// every intent carries - truncates this prompt BEFORE the path. Any
	// step built from Intent.Summary instead of the full input loses the
	// path (the F-NEW-9 defect class).
	summary := extractSummary(fullPrompt)
	if strings.Contains(summary, missingPath) {
		t.Logf("note: extractSummary retains the path on this build; the truncation seam moved")
	} else {
		t.Logf("confirmed loss seam: extractSummary(%d chars) = %q (path absent)", len(fullPrompt), summary)
	}

	// Gate arm: the real quick_plan path. The planner's scripted reply is
	// a generic decomposition with NO path - the live F-NEW-9 shape. The
	// persisted step must still carry the explicit path (02-recall-
	// fidelity T3: "step receives the path").
	chatter := &repairCaptureChatter{
		resps: []string{`{"steps":[{"description":"validate the requested file and report its contents","tool_hint":"code"}]}`},
	}
	sp := newPlanRepairTestPlanner(t, chatter)
	tsk := newTestTask("read the missing file", fullPrompt)
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-fidelity",
		Input:     fullPrompt,
		Intent:    string(IntentCode),
		Mode:      "quick_plan",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Precondition control: the planner PROMPT carried the full path
	// (the decompose template renders req.Input). The loss happens
	// between the planner output and the persisted step.
	if len(chatter.prompts) == 0 {
		t.Fatal("planner never entered; fixture problem")
	}
	if !strings.Contains(chatter.prompts[0], missingPath) {
		t.Fatalf("control failed: planner prompt lost the path: %q", chatter.prompts[0])
	}

	steps, err := sp.stepStore.ListByTaskID(tsk.ID)
	if err != nil || len(steps) == 0 {
		t.Fatalf("no persisted steps: %v (n=%d)", err, len(steps))
	}
	for _, st := range steps {
		if strings.Contains(st.Description, missingPath) {
			return // GREEN: the step carries the explicit path.
		}
	}
	t.Fatalf("RED (F-NEW-9): orchestrator step lost the explicit file path %q; the executor would answer with a clarification question for info already in the prompt. Persisted steps: %+v",
		missingPath, steps)
}
