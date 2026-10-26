package githubsvc

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/pkg/id"
)

// CIRef identifies the CI wait a parked github_ci_wait targets, so a
// scheduler can re-arm exactly one owner/repo/PR combination.
type CIRef struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// ReArmSink is the schedule seam a parked CI wait hands back to the daemon:
// ScheduleReArm records that CIRef should be re-checked at the given time
// under the given waitID. The in-memory recording implementation
// (InstructionReArmSink, a real scheduler instruction row) is owned by a
// later leaf; this interface pins the contract the builtin tools program
// against.
type ReArmSink interface {
	ScheduleReArm(ref CIRef, at time.Time, waitID string) error
}

// ErrCIWaitParked is returned by github_ci_wait when the wait gives up at
// its deadline without reaching a terminal CI outcome: the tool has parked
// the wait (re-armed the sink) and the caller can resume from the returned
// waitID instead of blocking again.
var ErrCIWaitParked = errors.New("githubsvc: ci wait parked; re-armed via schedule")

// InstructionReArmSink is the production ReArmSink: it persists a parked
// CI wait as a user-instruction cron row in the preferences store, so the
// daemon's instruction scheduler re-checks the CI ref when the row fires.
// The row is:
//
//	Trigger:    "cron:<HH:MM>"          (the minute `at` lands on)
//	Action:     "agent_trigger"
//	ActionArgs: {"prompt": "resume github_ci_wait <waitID> ..."}
//
// SyncCronInstructions (internal/scheduler/instructions.go) picks up the
// row on the next sync; the agent-facing prompt carries the waitID and the
// CI ref so a resuming agent can re-run github_ci_wait for exactly that
// owner/repo/PR.
type InstructionReArmSink struct {
	store  *preferences.Store
	logger *slog.Logger
}

var _ ReArmSink = (*InstructionReArmSink)(nil)

// NewInstructionReArmSink builds the sink against the daemon's instruction
// store. store must be non-nil (the daemon builds it in wireInstructions);
// a nil logger falls back to slog.Default(). The sink writes to the store's
// highest-priority tier (DefaultTier: project-local `.meept/instructions`,
// then `~/.meept/instructions`, then the system tier).
func NewInstructionReArmSink(store *preferences.Store, logger *slog.Logger) *InstructionReArmSink {
	if logger == nil {
		logger = slog.Default()
	}
	return &InstructionReArmSink{store: store, logger: logger}
}

// ScheduleReArm persists the re-arm row. It never panics: store/row
// failures are logged (with ref/waitID, no token material) and returned.
func (s *InstructionReArmSink) ScheduleReArm(ref CIRef, at time.Time, waitID string) error {
	if s.store == nil {
		return fmt.Errorf("githubsvc: instruction re-arm sink has no preferences store")
	}
	prompt := fmt.Sprintf(
		"resume github_ci_wait %s: re-check CI for %s/%s#%d (parked %s, re-armed at %s)",
		waitID, ref.Owner, ref.Repo, ref.Number,
		time.Now().UTC().Format(time.RFC3339), at.Format(time.RFC3339),
	)
	instr := &preferences.UserInstruction{
		ID:      id.Generate("ciwait_rearm_"),
		Name:    "github_ci_wait-" + waitID,
		Trigger: "cron:" + cronMinuteExpr(at),
		Action:  "agent_trigger",
		ActionArgs: map[string]any{
			"prompt":  prompt,
			"owner":   ref.Owner,
			"repo":    ref.Repo,
			"number":  ref.Number,
			"wait_id": waitID,
		},
		Enabled:  true,
		Scope:    "project",
		Priority: "normal",
	}
	tier := s.store.DefaultTier()
	if tier == "" {
		tier = preferences.DefaultTiers[len(preferences.DefaultTiers)-1]
	}
	if err := s.store.Save(instr, tier); err != nil {
		s.logger.Error("failed to persist ci re-arm instruction",
			"ref", ref.String(), "wait_id", waitID, "tier", tier, "error", err)
		return fmt.Errorf("githubsvc: persist ci re-arm instruction for %s: %w", ref.String(), err)
	}
	s.logger.Info("ci wait parked; re-armed via instruction cron row",
		"ref", ref.String(), "wait_id", waitID, "at", at.UTC().Format(time.RFC3339), "instruction", instr.ID)
	return nil
}

// cronMinuteExpr renders the minute `at` lands on as a 5-field cron
// expression ("M H * * *"): the scheduler parses it and the job fires at
// that minute (a one-shot re-check; the agent's resume either terminates
// the wait or parks a fresh row).
func cronMinuteExpr(at time.Time) string {
	return fmt.Sprintf("%d %d * * *", at.Minute(), at.Hour())
}

// String gives CIRef a compact, log-safe representation (no tokens).
func (c CIRef) String() string {
	return fmt.Sprintf("%s/%s#%d", c.Owner, c.Repo, c.Number)
}
