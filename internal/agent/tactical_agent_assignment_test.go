package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// LOW-fix pin (2026-09-08 audit): the schedule-time agent-assignment rule
// must not clobber an explicitly assigned step.AgentID. Pair-session
// actor/reviewer steps are stamped by the strategist (strategic.go stamps
// AgentID before the scheduler runs); the tool-hint table is a FALLBACK,
// not an override. assignStepAgent is the extracted rule scheduleStep uses.
func TestScheduleStep_PreservesAssignedAgentID(t *testing.T) {
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		MaxConcurrentJobs:     5,
		MaxConcurrentPerAgent: 2,
	})

	step := &task.TaskStep{ID: "step-preserve", ToolHint: "review", AgentID: "reviewer-agent"}
	ts.assignStepAgent(step)

	if step.AgentID != "reviewer-agent" {
		t.Errorf("step.AgentID = %q, want preserved reviewer-agent (hint %q must not override)",
			step.AgentID, step.ToolHint)
	}
}

func TestScheduleStep_FallsBackToHintWhenUnassigned(t *testing.T) {
	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		MaxConcurrentJobs:     5,
		MaxConcurrentPerAgent: 2,
	})

	step := &task.TaskStep{ID: "step-fallback", ToolHint: "write", AgentID: ""}
	ts.assignStepAgent(step)

	if step.AgentID == "" {
		t.Fatalf("step.AgentID still empty; hint fallback should have populated it")
	}
	if step.AgentID == "chat" {
		t.Errorf("write hint resolved to %q; table regression (write→writer missing?)", step.AgentID)
	}
}
