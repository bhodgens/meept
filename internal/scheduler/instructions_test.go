package scheduler

// bughunt 2026-10-30 H1: instructionToJob must carry ActionArgs["prompt"]
// into the scheduled agent job (the github_ci_wait re-arm resume prompt
// was discarded and replaced by "Execute instruction: cron:...").

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/preferences"
)

func newProbeScheduler(t *testing.T) *Scheduler {
	t.Helper()
	sched, err := NewScheduler(config.SchedulerConfig{Timezone: "UTC"}, bus.New(bus.DefaultConfig(), slog.Default()))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	return sched
}

func scheduledAgentPrompt(t *testing.T, sched *Scheduler, instrID string) *AgentJobConfig {
	t.Helper()
	job, ok := sched.jobs["instruction_"+instrID]
	if !ok {
		t.Fatalf("job instruction_%s not scheduled (jobs: %d)", instrID, len(sched.jobs))
	}
	cfg := job.Config()
	if cfg.AgentConfig == nil {
		t.Fatalf("job %s has no AgentConfig", instrID)
	}
	return cfg.AgentConfig
}

func TestInstructionToJob_PromptFromActionArgs(t *testing.T) {
	sched := newProbeScheduler(t)
	is := &InstructionScheduler{scheduler: sched, store: &preferences.Store{}, logger: slog.Default()}

	instr := &preferences.UserInstruction{
		ID:      "test-1",
		Trigger: "cron:0 3 * * *",
		Action:  "agent_trigger",
		ActionArgs: map[string]any{
			"agent_id": "agent-123",
			"prompt":   `resume github_ci_wait ciwait_abc: re-check CI for o/r#42; pass resume_wait_id="ciwait_abc"`,
		},
	}
	if err := is.instructionToJob(instr, "0 3 * * *"); err != nil {
		t.Fatalf("instructionToJob: %v", err)
	}
	ac := scheduledAgentPrompt(t, sched, "test-1")
	if ac.Prompt != instr.ActionArgs["prompt"] {
		t.Errorf("job prompt = %q, want the ActionArgs prompt verbatim", ac.Prompt)
	}
	if ac.AgentID != "agent-123" {
		t.Errorf("AgentID = %q, want agent-123", ac.AgentID)
	}
}

func TestInstructionToJob_NoPromptArg(t *testing.T) {
	sched := newProbeScheduler(t)
	is := &InstructionScheduler{scheduler: sched, store: &preferences.Store{}, logger: slog.Default()}

	instr := &preferences.UserInstruction{
		ID:         "test-2",
		Trigger:    "cron:0 3 * * *",
		Action:     "agent_trigger",
		ActionArgs: map[string]any{"agent_id": "agent-456"},
	}
	if err := is.instructionToJob(instr, "0 3 * * *"); err != nil {
		t.Fatalf("instructionToJob: %v", err)
	}
	ac := scheduledAgentPrompt(t, sched, "test-2")
	want := "Execute instruction: cron:0 3 * * *"
	if ac.Prompt != want {
		t.Errorf("job prompt = %q, want fallback %q", ac.Prompt, want)
	}
}

func TestInstructionToJob_EmptyPromptArg(t *testing.T) {
	sched := newProbeScheduler(t)
	is := &InstructionScheduler{scheduler: sched, store: &preferences.Store{}, logger: slog.Default()}

	instr := &preferences.UserInstruction{
		ID:         "test-3",
		Trigger:    "cron:0 3 * * *",
		Action:     "agent_trigger",
		ActionArgs: map[string]any{"agent_id": "agent-789", "prompt": ""},
	}
	if err := is.instructionToJob(instr, "0 3 * * *"); err != nil {
		t.Fatalf("instructionToJob: %v", err)
	}
	ac := scheduledAgentPrompt(t, sched, "test-3")
	want := "Execute instruction: cron:0 3 * * *"
	if ac.Prompt != want {
		t.Errorf("job prompt = %q, want fallback %q", ac.Prompt, want)
	}
}
