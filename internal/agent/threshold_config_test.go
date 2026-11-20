package agent

// 06-planning TEST-GATE T1 (F13a): the approval step threshold must be
// operator-configurable, and 0 must DISABLE the gate - not be clamped
// back to the default.
//
// RED today for missing-implementation reasons:
//   - internal/agent/strategic.go NewStrategicPlanner clamps
//     ApprovalStepThreshold <= 0 to 5, so a configured 0 (disable) is
//     resurrected into a 5-step gate. requiresApproval then trips on
//     plans the operator explicitly asked to run ungated.
//   - orchestrator.task_approval_step_threshold does not exist on
//     config.OrchestratorConfig (master Q1 ratified key; default 5,
//     0 disables), and internal/daemon/components.go never sets
//     ApprovalStepThreshold when constructing StrategicPlannerConfig.
//
// The config-field assertion uses reflection so this gate COMPILES today
// (a direct reference to the not-yet-existing field would break the
// whole package's test build) and flips GREEN the moment the field lands
// with the ratified name/tag/default. The behavioral pins call the
// existing requiresApproval seam directly.
//
// Gate tests: do not weaken. GREEN after 06-planning/01 T2.

import (
	"reflect"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// gateThresholdSteps builds n placeholder steps for the gate-count pin.
func gateThresholdSteps(n int) []*task.TaskStep {
	steps := make([]*task.TaskStep, 0, n)
	for i := 0; i < n; i++ {
		steps = append(steps, task.NewTaskStep("task-threshold-gate", "step", i))
	}
	return steps
}

// TestThresholdConfig pins the threshold semantics through the real
// requiresApproval seam: 0 (and negative) DISABLE the gate for any step
// count; an explicit N gates at >= N; below N stays ungated. The
// zero/negative rows are RED today (the <=0 -> 5 clamp resurrects the
// gate); the explicit-N rows pin behavior that must survive the fix.
func TestThresholdConfig(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		steps     int
		wantGate  bool
	}{
		{"zero disables gate even for large plans", 0, 21, false},
		{"negative disables gate", -1, 9, false},
		{"explicit N gates at N", 3, 3, true},
		{"explicit N gates above N", 3, 8, true},
		{"below explicit N stays ungated", 7, 6, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := NewStrategicPlanner(StrategicPlannerConfig{
				ApprovalStepThreshold: tt.threshold,
				Logger:                slogDiscardLogger(),
			})
			req := PlanRequest{TaskID: "task-threshold-gate", Mode: "plan"}
			got := sp.requiresApproval(req, gateThresholdSteps(tt.steps))
			if got != tt.wantGate {
				t.Fatalf("threshold=%d steps=%d: requiresApproval=%v, want %v (0 must disable the gate, never clamp back to the default 5)",
					tt.threshold, tt.steps, got, tt.wantGate)
			}
		})
	}
}

// TestThresholdConfigDefaultKey pins the ratified config surface
// (master Q1): orchestrator.task_approval_step_threshold exists on
// config.OrchestratorConfig as an int with json/toml tag
// task_approval_step_threshold and DefaultConfig() sets it to 5 (the
// historical default preserved through config, NOT through the planner
// clamp). Reflection keeps this compile-safe until the field lands.
// RED today: the field does not exist.
func TestThresholdConfigDefaultKey(t *testing.T) {
	cfg := config.DefaultConfig()
	v := reflect.ValueOf(cfg.Orchestrator)
	f := v.FieldByName("TaskApprovalStepThreshold")
	if !f.IsValid() {
		t.Fatalf("config.OrchestratorConfig has no TaskApprovalStepThreshold field; orchestrator.task_approval_step_threshold (default 5, 0 disables) is not configurable")
	}
	if f.Kind() != reflect.Int {
		t.Fatalf("TaskApprovalStepThreshold kind = %v, want int", f.Kind())
	}
	if got := f.Int(); got != 5 {
		t.Errorf("DefaultConfig orchestrator.task_approval_step_threshold = %d, want the preserved default 5", got)
	}
	sf, ok := v.Type().FieldByName("TaskApprovalStepThreshold")
	if !ok {
		t.Fatalf("TaskApprovalStepThreshold struct field missing")
	}
	if tag := sf.Tag.Get("json"); tag != "task_approval_step_threshold" {
		t.Errorf("json tag = %q, want task_approval_step_threshold (ratified key, master Q1)", tag)
	}
	if tag := sf.Tag.Get("toml"); tag != "task_approval_step_threshold" {
		t.Errorf("toml tag = %q, want task_approval_step_threshold", tag)
	}
}

// TestThresholdConfigZeroNotClamped is the field-level companion to the
// behavioral zero pin: after construction with 0 the planner's internal
// threshold must stay 0 (disabled). RED today: NewStrategicPlanner
// rewrites 0 to 5 (strategic.go clamp), so the gate can never be turned
// off through configuration.
func TestThresholdConfigZeroNotClamped(t *testing.T) {
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		ApprovalStepThreshold: 0,
		Logger:                slogDiscardLogger(),
	})
	if sp.approvalStepThreshold != 0 {
		t.Fatalf("ApprovalStepThreshold=0 was clamped to %d; 0 must mean disabled (the default 5 belongs in config.DefaultConfig, not in a clamp that resurrects the gate)", sp.approvalStepThreshold)
	}
}
