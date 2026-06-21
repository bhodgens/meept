package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/preferences"
)

// InstructionScheduler syncs user instructions with the scheduler.
type InstructionScheduler struct {
	scheduler *Scheduler
	store     *preferences.Store
	logger    *slog.Logger
}

// NewInstructionScheduler creates a new instruction scheduler.
func NewInstructionScheduler(s *Scheduler, store *preferences.Store, logger *slog.Logger) *InstructionScheduler {
	return &InstructionScheduler{
		scheduler: s,
		store:     store,
		logger:    logger,
	}
}

// SyncCronInstructions loads cron-type instructions and creates/removes jobs.
func (s *InstructionScheduler) SyncCronInstructions() error {
	instructions := s.store.GetActive()
	var synced int

	for _, instr := range instructions {
		if !strings.HasPrefix(instr.Trigger, "cron:") {
			continue
		}

		cronExpr := strings.TrimPrefix(instr.Trigger, "cron:")
		if err := s.instructionToJob(instr, cronExpr); err != nil {
			s.logger.Warn("failed to sync instruction", "id", instr.ID, "error", err)
			continue
		}
		synced++
	}

	s.logger.Debug("cron instructions synced", "count", synced)
	return nil
}

// instructionToJob converts an instruction to a scheduled job.
func (s *InstructionScheduler) instructionToJob(instr *preferences.UserInstruction, cronExpr string) error {
	jobID := fmt.Sprintf("instruction_%s", instr.ID)

	// Remove existing job if any
	s.scheduler.RemoveJob(jobID)

	if instr.Action == "agent_trigger" {
		agentID, _ := instr.ActionArgs["agent_id"].(string)
		prompt := fmt.Sprintf("Execute instruction: %s", instr.Trigger)

		return s.scheduler.CreateJobWithDeps(JobConfig{
			ID:       jobID,
			Name:     fmt.Sprintf("Instruction: %s", instr.ID),
			Schedule: cronExpr,
			Type:     JobTypeAgent,
			AgentConfig: &AgentJobConfig{
				Prompt:  prompt,
				AgentID: agentID,
			},
		}, []string{})
	} else if instr.Action == "shell_execute" {
		command, _ := instr.ActionArgs["command"].(string)
		if command == "" {
			return fmt.Errorf("shell_execute has no command")
		}

		return s.scheduler.CreateJobWithDeps(JobConfig{
			ID:       jobID,
			Name:     fmt.Sprintf("Instruction: %s", instr.ID),
			Schedule: cronExpr,
			Type:     JobTypeShell,
			ShellConfig: &ShellJobConfig{
				Command: command,
			},
		}, []string{})
	}

	return fmt.Errorf("unsupported action type: %s", instr.Action)
}

// Start begins syncing cron instructions.
func (s *InstructionScheduler) Start(ctx context.Context) error {
	if err := s.SyncCronInstructions(); err != nil {
		s.logger.Warn("failed to sync cron instructions on startup", "error", err)
	}
	return nil
}
