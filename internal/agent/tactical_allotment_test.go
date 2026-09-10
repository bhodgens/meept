package agent

// Allotment-wiring tests (allotment tree leaf 02): the tactical scheduler's
// context-window provider seam, AllotmentCfg defaulting, continuation-step
// creation at ScheduleReadySteps time, and PlanRequest.ExecutorModelRef
// plumbing.
//
// The batching tests drive the REAL scheduleStep path (real stores +
// capturing queue) so they pin the persistence contract too: continuation
// steps are real TaskSteps, rewritten in place and persisted before their
// jobs are built. Legacy behavior (nil provider / unknown window) is pinned
// as byte-identical: same job count, untouched descriptions, no new deps.
// (Legacy guard note: 7 steps x 512 tokens fit one 3584-token wave only in
// the legacy path; the batching test uses a window that forces 3 batches.)

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// allotmentDesc is 2048 chars = 512 tokens at the default 4 chars/token.
var allotmentDesc = strings.Repeat("a", 2048)

// newAllotmentTacticalFixture builds a scheduler over real stores with a
// capturing queue and a raised per-agent concurrency cap so a whole batch
// wave can enqueue in one ScheduleReadySteps call.
func newAllotmentTacticalFixture(t *testing.T) (*TacticalScheduler, *capturedQueue, *task.Store) {
	t.Helper()
	taskStore, stepStore := newTestTaskAndStepStore(t)

	cq := &capturedQueue{}
	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:             stepStore,
		TaskStore:             taskStore,
		Queue:                 cq,
		Bus:                   bus.New(nil, nil),
		Logger:                slogDiscardLogger(),
		MaxConcurrentPerAgent: 10,
	})
	return scheduler, cq, taskStore
}

// seedAllotmentSteps creates a task with n identical 512-token steps
// (2048 chars / 4 chars-per-token at MinStepTokens floor), all ready with
// no dependencies: GetReadySteps returns the whole wave in sequence order.
// NOTE: task.NewTask's first argument is the task NAME; the ID is generated
// (task-<timestamp>), so the generated ID is returned and must be used for
// all ScheduleReadySteps / ListByTaskID calls.
func seedAllotmentSteps(t *testing.T, taskStore *task.Store, name string, n int) string {
	t.Helper()
	tk := task.NewTask(name, "allotment batching test")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	desc := allotmentDesc
	for i := 0; i < n; i++ {
		s := task.NewTaskStep(tk.ID, desc, i)
		s.State = task.StepReady
		s.ToolHint = "code" // executor hint -> allotmentAgentID resolves "coder"
		if err := taskStore.StepStore().Create(s); err != nil {
			t.Fatalf("create step %d: %v", i, err)
		}
	}
	return tk.ID
}

func TestTacticalScheduler_ContextWindowProvider_Nil(t *testing.T) {
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		Logger: slogDiscardLogger(),
	})
	if got := ts.contextWindowFor("coder"); got != 0 {
		t.Errorf("contextWindowFor with nil provider = %d, want 0", got)
	}
	if ts.allotmentCfg != DefaultAllotmentConfig() {
		t.Errorf("zero AllotmentCfg not defaulted in NewTacticalScheduler: %+v", ts.allotmentCfg)
	}
}

func TestTacticalScheduler_SetContextWindowProvider(t *testing.T) {
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		Logger: slogDiscardLogger(),
	})
	// nil is ignored (mirrors SetHandoffPropagator / SetSessionStore).
	ts.SetContextWindowProvider(nil)
	if ts.contextWindowProvider != nil {
		t.Fatal("SetContextWindowProvider(nil) installed a provider")
	}
	ts.SetContextWindowProvider(func(string) int { return 8192 })
	if got := ts.contextWindowFor("coder"); got != 8192 {
		t.Errorf("contextWindowFor = %d, want 8192", got)
	}
}

// TestTacticalScheduler_ContinuationBatches pins the leaf-02 core: a known
// 6144-token window -> allotment (6144-4096)*0.75 = 1536 -> three 512-token
// steps per batch -> 7 ready steps batch 3/3/1 (same fixture as the leaf-01
// SplitStepsByAllotment test). Wave 1 schedules exactly batch 0 (3 jobs);
// batches 1 and 2 become real continuation steps - [continuation k/3]
// prefixes, DependsOn chained to the previous batch's last step - persisted
// in the store. The dependency gate in scheduleStep (deps must be terminal)
// is what holds each continuation batch until the previous batch drains.
func TestTacticalScheduler_ContinuationBatches(t *testing.T) {
	ts, cq, taskStore := newAllotmentTacticalFixture(t)
	ts.SetContextWindowProvider(func(string) int { return 6144 })

	taskID := seedAllotmentSteps(t, taskStore, "allot-cont-1", 7)
	stepStore := taskStore.StepStore()

	if err := ts.ScheduleReadySteps(context.Background(), taskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}

	// Only batch 0 fits this wave: continuation batches are chained to
	// not-yet-terminal deps, so scheduleStep defers them.
	if len(cq.jobs) != 3 {
		t.Fatalf("expected 3 enqueued jobs (batch 0 only), got %d", len(cq.jobs))
	}
	var payload StepJobPayload
	if err := json.Unmarshal(cq.jobs[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if strings.Contains(payload.Description, "[continuation") {
		t.Errorf("batch-0 job carries continuation prefix: %q", payload.Description)
	}

	persisted, err := stepStore.ListByTaskID(taskID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(persisted) != 7 {
		t.Fatalf("expected 7 persisted steps, got %d", len(persisted))
	}
	bySeq := map[int]*task.TaskStep{}
	for _, s := range persisted {
		bySeq[s.Sequence] = s
	}

	desc := allotmentDesc
	for _, tc := range []struct {
		seq         int
		wantDesc    string
		wantDepends []string
	}{
		{0, desc, nil},
		{1, desc, nil},
		{2, desc, nil},
		{3, ContinuationDescription(desc, 1, 3), []string{bySeq[2].ID}},
		{4, ContinuationDescription(desc, 1, 3), []string{bySeq[2].ID}},
		{5, ContinuationDescription(desc, 1, 3), []string{bySeq[2].ID}},
		{6, ContinuationDescription(desc, 2, 3), []string{bySeq[5].ID}},
	} {
		s := bySeq[tc.seq]
		if s == nil {
			t.Fatalf("step with sequence %d missing", tc.seq)
		}
		if s.Description != tc.wantDesc {
			t.Errorf("seq %d description = %q, want %q", tc.seq, s.Description, tc.wantDesc)
		}
		if tc.wantDepends == nil {
			if len(s.DependsOn) != 0 {
				t.Errorf("seq %d DependsOn = %v, want none", tc.seq, s.DependsOn)
			}
		} else if len(s.DependsOn) != len(tc.wantDepends) {
			t.Errorf("seq %d DependsOn = %v, want %v", tc.seq, s.DependsOn, tc.wantDepends)
		} else {
			for i, dep := range tc.wantDepends {
				if s.DependsOn[i] != dep {
					t.Errorf("seq %d DependsOn[%d] = %q, want %q", tc.seq, i, s.DependsOn[i], dep)
				}
			}
		}
	}
}

// TestTacticalScheduler_ZeroWindowKeepsLegacyBehavior pins the regression
// guard: provider installed but window unknown (0) means AllotmentTokens
// returns 0: no batching, no continuation rewriting, whole wave schedules.
func TestTacticalScheduler_ZeroWindowKeepsLegacyBehavior(t *testing.T) {
	ts, cq, taskStore := newAllotmentTacticalFixture(t)
	ts.SetContextWindowProvider(func(string) int { return 0 })

	taskID := seedAllotmentSteps(t, taskStore, "allot-legacy-1", 7)

	if err := ts.ScheduleReadySteps(context.Background(), taskID); err != nil {
		t.Fatalf("ScheduleReadySteps: %v", err)
	}
	if len(cq.jobs) != 7 {
		t.Fatalf("expected 7 enqueued jobs (legacy whole-wave), got %d", len(cq.jobs))
	}

	persisted, err := taskStore.StepStore().ListByTaskID(taskID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	for _, s := range persisted {
		if strings.Contains(s.Description, "[continuation") {
			t.Errorf("seq %d description rewritten without a known window: %q", s.Sequence, s.Description)
		}
		if len(s.DependsOn) != 0 {
			t.Errorf("seq %d grew dependencies without a known window: %v", s.Sequence, s.DependsOn)
		}
	}
}

// TestPlanRequest_CarriesExecutorModelRef pins the PlanRequest field and
// its wire shape: present when set, omitted when empty (omitempty keeps
// legacy payloads byte-compatible).
func TestPlanRequest_CarriesExecutorModelRef(t *testing.T) {
	req := PlanRequest{
		TaskID:           "t1",
		Intent:           string(IntentPlan),
		ExecutorModelRef: "zai/glm-4.7",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire["executor_model_ref"] != "zai/glm-4.7" {
		t.Errorf("executor_model_ref = %v, want zai/glm-4.7", wire["executor_model_ref"])
	}

	empty, err := json.Marshal(PlanRequest{TaskID: "t1"})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(empty), "executor_model_ref") {
		t.Errorf("empty ExecutorModelRef should be omitted, got %s", empty)
	}
}
