package agent

// Planner-output hardening pins (issue #58 capabilities 1 and 3).
//
// Capability 1 — plan-repair retry: a PARSE failure from the planner gets
// exactly ONE re-ask with the parse error appended to the prompt; transport
// failures from plannerLoop.RunOnce never retry; the second failure is final.
//
// Capability 3 — tool-hint validation: with SetValidToolNames wired, an
// unknown tool_hint is Warn-logged and dropped (empty) so the executor's
// tool-hint table picks; a known hint passes through; a nil/empty valid set
// skips validation entirely (legacy behavior).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
)

// repairCaptureChatter is a canned llm.Chatter stub that returns scripted
// responses in order and records every prompt it was asked, so tests can
// pin both the call count and the exact repair prompt wording.
type repairCaptureChatter struct {
	mu       sync.Mutex
	resps    []string
	errs     []error // per-call error, parallel to resps (nil entry = success)
	prompts  []string
	callIdx  int
	fallback string // returned when the script is exhausted
}

func (c *repairCaptureChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var prompt strings.Builder
	for _, m := range messages {
		prompt.WriteString(m.Content)
		prompt.WriteString("\n")
	}
	c.prompts = append(c.prompts, prompt.String())
	i := c.callIdx
	c.callIdx++
	if i < len(c.errs) && c.errs[i] != nil {
		return nil, c.errs[i]
	}
	if i < len(c.resps) {
		return &llm.Response{Content: c.resps[i], FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
	}
	return &llm.Response{Content: c.fallback, FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
}

func (c *repairCaptureChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *repairCaptureChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "plan-repair-test"}
}

func (c *repairCaptureChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callIdx
}

func (c *repairCaptureChatter) promptAt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < len(c.prompts) {
		return c.prompts[i]
	}
	return ""
}

// newPlanRepairTestPlanner builds a StrategicPlanner over the given chatter,
// mirroring newEmptyPlanTestPlanner's wiring.
func newPlanRepairTestPlanner(t *testing.T, chatter llm.Chatter) *StrategicPlanner {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	reg := newEmptyPlanTestRegistry(chatter)
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       reg,
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})
	return sp
}

func newPlanRepairTestTask(t *testing.T, sp *StrategicPlanner, id, input string) error {
	t.Helper()
	return sp.taskStore.Create(newTestTask(id, input))
}

// TestPlanRepairRetry_RecoversMalformedThenValid pins the recovery path: the
// planner emits prose (no JSON) once, then valid JSON on the repair re-ask.
// The plan must succeed with the repaired steps, the planner must be entered
// EXACTLY twice, and the second prompt must carry the repair section.
func TestPlanRepairRetry_RecoversMalformedThenValid(t *testing.T) {
	goodPlan := `{"steps": [{"description": "step one", "tool_hint": "code"}, {"description": "step two"}]}`
	chatter := &repairCaptureChatter{
		resps: []string{
			"I could not produce a plan in JSON format, sorry.",
			goodPlan,
		},
	}
	sp := newPlanRepairTestPlanner(t, chatter)

	tsk := newTestTask("task-repair-recover", "create a config file for the scheduler")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	steps, err := sp.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-repair-recover",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("planSinglePhase: %v", err)
	}

	if got := chatter.callCount(); got != 2 {
		t.Errorf("planner LLM calls = %d, want exactly 2 (initial + one repair retry)", got)
	}

	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 from the repaired plan", len(steps))
	}
	if steps[0].Description != "step one" || steps[1].Description != "step two" {
		t.Errorf("step descriptions = %q, %q; want the repaired plan's", steps[0].Description, steps[1].Description)
	}

	// The repair re-ask must carry the exact repair wording with the parse
	// failure inline.
	second := chatter.promptAt(1)
	want := "Your previous response failed to parse: planner output failed to parse: no JSON found in planner output. Respond again with ONLY the JSON plan object."
	if !strings.Contains(second, want) {
		t.Errorf("repair prompt missing exact repair section;\nwant substring: %q\ngot prompt: %.400s", want, second)
	}
	if !strings.Contains(second, "create a config file for the scheduler") {
		t.Errorf("repair prompt lost the original task input; got %.200s", second)
	}
}

// TestPlanRepairRetry_SecondParseFailureIsFinal pins the one-retry-maximum
// rule: bad JSON twice means the second failure is final (no third call) and
// the original parse error surfaces.
func TestPlanRepairRetry_SecondParseFailureIsFinal(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{
			"still no json here",
			"also still no json",
		},
	}
	sp := newPlanRepairTestPlanner(t, chatter)

	tsk := newTestTask("task-repair-final", "create a config file for the scheduler")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := sp.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-repair-final",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err == nil {
		t.Fatal("planSinglePhase must fail when the repair re-ask also fails to parse")
	}
	if !strings.Contains(err.Error(), "no JSON found in planner output") {
		t.Errorf("error = %q, want the original parse-failure text", err.Error())
	}
	if got := chatter.callCount(); got != 2 {
		t.Errorf("planner LLM calls = %d, want exactly 2 (no retry after the retry)", got)
	}
}

// TestPlanRepairRetry_NoRetryOnTransportFailure pins the sentinel scoping: a
// RunOnce transport/agent failure (wrapped as "planner failed: ...") is NOT a
// parse error, so no repair re-ask happens — exactly one planner call, and
// the transport error surfaces.
func TestPlanRepairRetry_NoRetryOnTransportFailure(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{"irrelevant"},
		errs:  []error{context.DeadlineExceeded},
	}
	sp := newPlanRepairTestPlanner(t, chatter)

	tsk := newTestTask("task-repair-transport", "create a config file for the scheduler")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := sp.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-repair-transport",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err == nil {
		t.Fatal("planSinglePhase must fail on a transport error")
	}
	if !strings.Contains(err.Error(), "planner failed") {
		t.Errorf("error = %q, want the RunOnce transport failure text", err.Error())
	}
	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want exactly 1 (transport failures never retry)", got)
	}
}

// TestPlanRepairRetry_NoRetryOnEmptyPlan pins that the empty-plan sentinel
// (which parsed CLEANLY) never takes the repair path — the issue #53
// degradation paths keep exclusive ownership of it.
func TestPlanRepairRetry_NoRetryOnEmptyPlan(t *testing.T) {
	chatter := &repairCaptureChatter{
		resps: []string{`{"steps": []}`},
	}
	sp := newPlanRepairTestPlanner(t, chatter)

	tsk := newTestTask("task-repair-emptyplan", "create a config file for the scheduler")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	steps, err := sp.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-repair-emptyplan",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("planSinglePhase: %v", err)
	}
	if got := chatter.callCount(); got != 1 {
		t.Errorf("planner LLM calls = %d, want exactly 1 (empty plan never retries)", got)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want the deterministic single step", len(steps))
	}
}

// TestPlanRepairRetry_MetricsRecorded pins the outcome metric: a recovered
// repair records outcome=recovered; a failed repair records outcome=failed.
func TestPlanRepairRetry_MetricsRecorded(t *testing.T) {
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	newSP := func(chatter llm.Chatter) *StrategicPlanner {
		sp := newPlanRepairTestPlanner(t, chatter)
		sp.metricsStore = store
		return sp
	}

	countMetric := func(outcome string) int {
		type row struct {
			Name  string `db:"metric_name"`
			Tags  string `db:"tags"`
			Value int    `db:"value"`
		}
		var rows []row
		if err := store.DB().Select(&rows,
			`SELECT metric_name, tags, value FROM metrics_live WHERE metric_name = 'strategic_planner.plan_repair_retry'`); err != nil {
			t.Fatalf("select metric rows: %v", err)
		}
		n := 0
		for _, r := range rows {
			var tags map[string]string
			if err := json.Unmarshal([]byte(r.Tags), &tags); err != nil {
				t.Fatalf("unmarshal tags %q: %v", r.Tags, err)
			}
			if tags["outcome"] == outcome {
				n += r.Value
			}
		}
		return n
	}

	// Recovered path.
	recovered := &repairCaptureChatter{
		resps: []string{
			"no json",
			`{"steps": [{"description": "repaired step"}]}`,
		},
	}
	spRec := newSP(recovered)
	tskRec := newTestTask("task-repair-metric-ok", "create a config file for the scheduler")
	if err := spRec.taskStore.Create(tskRec); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := spRec.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tskRec.ID,
		SessionID: "sess-repair-metric-ok",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	}); err != nil {
		t.Fatalf("planSinglePhase (recovered): %v", err)
	}
	if got := countMetric("recovered"); got != 1 {
		t.Errorf("plan_repair_retry{outcome=recovered} = %d, want 1", got)
	}

	// Failed path.
	failed := &repairCaptureChatter{
		resps: []string{"no json", "still no json"},
	}
	spFail := newSP(failed)
	tskFail := newTestTask("task-repair-metric-fail", "create a config file for the scheduler")
	if err := spFail.taskStore.Create(tskFail); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := spFail.planSinglePhase(context.Background(), PlanRequest{
		TaskID:    tskFail.ID,
		SessionID: "sess-repair-metric-fail",
		Input:     "create a config file for the scheduler",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	}); err == nil {
		t.Fatal("planSinglePhase (failed repair) must fail")
	}
	if got := countMetric("failed"); got != 1 {
		t.Errorf("plan_repair_retry{outcome=failed} = %d, want 1", got)
	}
}

// TestSetValidToolNames_UnknownHintDroppedValidKept pins capability 3: with a
// valid-name set wired, an unknown tool_hint is dropped to empty (Warn) while
// a known hint passes through verbatim.
func TestSetValidToolNames_UnknownHintDroppedValidKept(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })
	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	chatter := &repairCaptureChatter{
		resps: []string{`{"steps": [
			{"description": "known hint step", "tool_hint": "code"},
			{"description": "unknown hint step", "tool_hint": "quantum_frobnicator"},
			{"description": "empty hint step"}
		]}`},
	}
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:       newEmptyPlanTestRegistry(chatter),
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         logger,
	})
	sp.SetValidToolNames(map[string]bool{"code": true, "research": true, "bash": true})

	steps, err := sp.parsePlanOutput("task-hint-validation", chatter.resps[0])
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	if steps[0].ToolHint != "code" {
		t.Errorf("known hint = %q, want %q (valid hints pass through)", steps[0].ToolHint, "code")
	}
	if steps[1].ToolHint != "" {
		t.Errorf("unknown hint = %q, want empty (dropped so the tool-hint table picks)", steps[1].ToolHint)
	}
	if steps[2].ToolHint != "" {
		t.Errorf("empty hint = %q, want empty", steps[2].ToolHint)
	}
	if !strings.Contains(logBuf.String(), "planner emitted unknown tool_hint") {
		t.Errorf("log = %s, want the unknown-hint Warn", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "quantum_frobnicator") {
		t.Errorf("log = %s, want the dropped hint name", logBuf.String())
	}
}

// TestSetValidToolNames_NilSetSkipsValidation pins the legacy behavior: with
// no valid-name set wired (or an empty one), hints pass through unchecked.
func TestSetValidToolNames_NilSetSkipsValidation(t *testing.T) {
	input := `{"steps": [{"description": "s1", "tool_hint": "anything_goes"}]}`

	// No setter called at all.
	sp1 := newPlanRepairTestPlanner(t, &repairCaptureChatter{})
	steps, err := sp1.parsePlanOutput("task-hint-nil", input)
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if steps[0].ToolHint != "anything_goes" {
		t.Errorf("unset valid-set: hint = %q, want verbatim passthrough", steps[0].ToolHint)
	}

	// Setter called with an empty map.
	sp2 := newPlanRepairTestPlanner(t, &repairCaptureChatter{})
	sp2.SetValidToolNames(map[string]bool{})
	steps, err = sp2.parsePlanOutput("task-hint-empty", input)
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if steps[0].ToolHint != "anything_goes" {
		t.Errorf("empty valid-set: hint = %q, want verbatim passthrough", steps[0].ToolHint)
	}

	// Nil-guarded setter: nil map must be a no-op, not a panic or a wipe.
	sp3 := newPlanRepairTestPlanner(t, &repairCaptureChatter{})
	sp3.SetValidToolNames(nil)
	steps, err = sp3.parsePlanOutput("task-hint-nilmap", input)
	if err != nil {
		t.Fatalf("parsePlanOutput: %v", err)
	}
	if steps[0].ToolHint != "anything_goes" {
		t.Errorf("nil-map setter: hint = %q, want verbatim passthrough", steps[0].ToolHint)
	}
}

// TestParsePlanOutput_SentinelWrapping pins the error taxonomy the retry
// depends on: both parse-failure kinds match errors.Is(ErrPlannerParse), the
// empty-plan sentinel does NOT, and the legacy message texts are preserved.
func TestParsePlanOutput_SentinelWrapping(t *testing.T) {
	sp := newPlanRepairTestPlanner(t, &repairCaptureChatter{})

	_, err := sp.parsePlanOutput("task-sentinel", "prose only, no json")
	if err == nil || !strings.Contains(err.Error(), "no JSON found in planner output") {
		t.Fatalf("error = %v, want the legacy no-JSON message", err)
	}
	if !errors.Is(err, ErrPlannerParse) {
		t.Errorf("no-JSON error must match ErrPlannerParse")
	}

	_, err = sp.parsePlanOutput("task-sentinel", "```json\n{\"steps\": \"not an array\"}\n```")
	if err == nil || !strings.Contains(err.Error(), "failed to parse plan JSON") {
		t.Fatalf("error = %v, want the legacy unmarshal message", err)
	}
	if !errors.Is(err, ErrPlannerParse) {
		t.Errorf("unmarshal error must match ErrPlannerParse")
	}

	_, err = sp.parsePlanOutput("task-sentinel", `{"steps": []}`)
	if err == nil || !errors.Is(err, ErrPlannerEmptyPlan) {
		t.Fatalf("error = %v, want ErrPlannerEmptyPlan", err)
	}
	if errors.Is(err, ErrPlannerParse) {
		t.Errorf("ErrPlannerEmptyPlan must NOT match ErrPlannerParse (it parsed cleanly)")
	}
}
