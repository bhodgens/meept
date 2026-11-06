// intake_wiring.go wires the github-intake-factory poll loop ([intake]
// block) into the daemon. It mirrors wireGitHubTools' gating posture
// (enabled-gated, fail-closed, degrade-to-no-op) and schedules the poll
// through the proven employeeSchedulerAdapter.RunAtInterval path
// (idempotent per job name), so no new goroutine is introduced: the
// scheduler owns the job's lifecycle.
//
// Token coupling: intake has NO separate token config. It runs on the
// same githubsvc client pool as the github_* tools — c.GitHubPool when
// wireGitHubTools already built one (either block enabled first; the
// call site below runs wireGitHubTools first), else a dedicated pool
// built from [github].token_secret via the same precedence. Documented
// in docs/workflows/intake.md.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/intake"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// intakePollJobName is the scheduler job ID for the intake poll. The
// gate tests pin it (TestIntakeWiring_*) and RunAtInterval is idempotent
// per name.
const intakePollJobName = "intake_poller"

// defaultIntakePollInterval is the poll cadence when intake.poll_interval
// is empty or unparseable ("5m"; IntakeConfig documents the same default).
const defaultIntakePollInterval = 5 * time.Minute

// wireIntakePoller registers the intake poll job when [intake].enabled.
// Disabled (the default) -> Debug log and return: no job, no error.
// Enabled-but-invalid (ValidateEnabled error, empty/invalid poll
// interval, missing token source, no employee manager) -> Warn naming
// the problem and return: fail-closed, no partial wiring. Enabled and
// valid -> build GitHubPoller + Linker + WriteBack on the shared
// githubsvc pool and schedule "intake_poller" at the configured
// interval. Poll errors log Warn and wait for the next tick; a panic in
// a tick is recovered so the scheduler goroutine survives (the same
// recover posture as startGitHubReArmTicker).
//
// Free function (not a method) per the committed gate tests
// (intake_wiring_gate_test.go), which call wireIntakePoller(c). The
// poll context is the Components lifecycle context when set, else
// context.Background() (the gate-test fixture has no lifecycle).
func wireIntakePoller(c *Components) {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := c.Config.Intake
	if !cfg.Enabled {
		logger.Debug("intake poller not scheduled", "reason", "intake block disabled")
		return
	}
	if err := cfg.ValidateEnabled(); err != nil {
		logger.Warn("intake block enabled but invalid; no intake poller scheduled", "error", err)
		return
	}

	interval := defaultIntakePollInterval
	if cfg.PollInterval != "" {
		parsed, err := time.ParseDuration(cfg.PollInterval)
		if err != nil || parsed <= 0 {
			logger.Warn("intake.poll_interval is not a positive duration; using the default",
				"configured", cfg.PollInterval, "default", defaultIntakePollInterval.String())
		} else {
			interval = parsed
		}
	}

	owner, repo, ok := strings.Cut(cfg.WatchRepo, "/")
	if !ok || owner == "" || repo == "" {
		logger.Warn("intake.watch_repo is not owner/repo; no intake poller scheduled",
			"watch_repo", cfg.WatchRepo)
		return
	}

	pool := c.GitHubPool
	dedicated := false
	if pool == nil {
		ghCfg := c.Config.GitHub
		var src githubsvc.TokenSource
		if ghCfg.TokenSecret == "" {
			// No token material at all: schedule anyway — every poll
			// fails closed at request time with a nameable error from
			// the pool's ErrTokenSource, which keeps the operator's
			// misconfiguration visible in the logs on every tick.
			logger.Warn("intake enabled but [github].token_secret is empty; scheduling with an erroring token source (every poll will fail until a token is configured)")
			src = githubsvc.ErrTokenSource{Err: fmt.Errorf("githubsvc: no GitHub token resolved: [github].token_secret is empty (intake has no separate token)")}
		} else {
			src, _ = c.resolveGitHubTokenSource(ghCfg.TokenSecret, logger)
		}
		var err error
		pool, err = githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
			Tokens:  src,
			BaseURL: config.ExpandMeeptPath(ghCfg.BaseURL),
		})
		if err != nil {
			logger.Error("intake poller wiring failed: pool construction", "error", err)
			return
		}
		dedicated = true
	}

	filter := intake.Filter{RequiredLabels: cfg.RequiredLabels}
	poller := intake.NewGitHubPoller(pool, githubsvc.OwnerRepo{Owner: owner, Repo: repo}, filter, logger.Handler())

	linker := intake.NewLinker(c.EmployeeManager, c.EffectsLedger, logger.Handler())

	// Schedule the poll. RunAtInterval is idempotent per name: a
	// re-registration replaces any prior "intake_poller" job.
	adapter := employeeSchedulerAdapter{sched: c.Scheduler}
	adapter.RunAtInterval(intakePollJobName, interval, func() {
		pollIntakeTickets(ctx, c, poller, linker, cfg)
	})

	// Outcome write-back: built at wiring time with the configured done
	// label and retained on Components so the goal-health outcome seam
	// can reach it once the goals-assess subscriber lands (leaf 06+).
	// A nil ledger degrades to no write-back: every Report path is
	// fail-closed on a nil ledger (writeback.go), so a missing ledger
	// means the seam can never fire — logged, never a panic.
	if c.EffectsLedger != nil {
		c.IntakeWriteBack = intake.NewWriteBack(pool, c.EffectsLedger,
			githubsvc.OwnerRepo{Owner: owner, Repo: repo}, logger.Handler())
		c.IntakeWriteBack.SetDoneLabel(cfg.DoneLabel)
	} else {
		logger.Warn("effects ledger not wired; intake outcome write-back disabled (goal outcomes will not comment back)")
	}

	// Plan->GitHub-Projects projection (leaf 06, Contract 5): built only
	// when project_number > 0 AND the effects ledger is wired — receipts
	// are the correlation store, so a nil ledger means no projection.
	// project_number 0 (the shipped default) skips silently.
	if cfg.ProjectNumber > 0 {
		if c.EffectsLedger != nil {
			c.IntakeProjector = intake.NewProjector(pool, c.EffectsLedger,
				cfg.ProjectNumber, githubsvc.OwnerRepo{Owner: owner, Repo: repo}, logger.Handler())
			wireIntakeProjectionSubscriptions(c, cfg, logger)
			logger.Info("intake projection armed", "project_number", cfg.ProjectNumber, "watch_repo", cfg.WatchRepo)
		} else {
			logger.Warn("intake.project_number is set but the effects ledger is not wired; plan projection disabled")
		}
	}

	// FK anchor: the default_employee may never have been hired — a
	// trigger-source goal factory legitimately routes work to a
	// configured id, not a managed employee. Without the anchor every
	// CreateGoal fails the employee_goals FK. Idempotent, best-effort.
	// Nil manager (the gate-test fixture) skips: the fixture has no
	// employee stores to anchor into.
	if c.EmployeeManager != nil {
		c.EmployeeManager.EnsureEmployeeRecord(ctx, cfg.DefaultEmployee)
	}

	logger.Info("intake poller scheduled",
		"job", intakePollJobName,
		"interval", interval.String(),
		"watch_repo", cfg.WatchRepo,
		"default_employee", cfg.DefaultEmployee,
		"required_labels", strings.Join(cfg.RequiredLabels, ","),
		"gate_command", cfg.GateCommand,
		"pool", map[bool]string{true: "dedicated", false: "shared"}[dedicated],
	)
}

// pollIntakeTickets is one poll tick: list tickets, map each to exactly
// one goal. A poll failure (or a per-ticket failure) logs Warn and waits
// for the next tick — never panics the scheduler goroutine, never
// aborts the remaining tickets on one bad entry.
func pollIntakeTickets(ctx context.Context, c *Components, poller *intake.GitHubPoller,
	linker *intake.Linker, cfg config.IntakeConfig) {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// recover guard: a panic in one tick must not kill the scheduler's
	// job goroutine (same posture as the github re-arm ticker).
	defer func() {
		if r := recover(); r != nil {
			logger.Error("intake poll tick panic recovered", "recovered", r)
		}
	}()

	tickets, err := poller.Poll(ctx)
	if err != nil {
		logger.Warn("intake poll failed; waiting for the next tick", "error", err)
		return
	}
	var gate *employee.GoalGateSpec
	if cfg.GateCommand != "" {
		gate = &employee.GoalGateSpec{Command: cfg.GateCommand}
	}
	for _, t := range tickets {
		if _, err := linker.EnsureGoal(ctx, t, cfg.DefaultEmployee, gate); err != nil {
			logger.Warn("intake ticket mapping failed; continuing with the remaining tickets",
				"repo", t.Repo, "ticket", t.ID, "error", err)
		}
	}
	if len(tickets) > 0 {
		logger.Debug("intake poll mapped tickets to goals",
			"tickets", len(tickets), "default_employee", cfg.DefaultEmployee)
	}
}

// ---------------------------------------------------------------------------
// Plan -> GitHub Project projection (leaf 06, Contract 5 wiring seam)
// ---------------------------------------------------------------------------

// wireIntakeProjectionSubscriptions arms the two bus subscribers that drive
// the Projector:
//
//   - "plan.executing"      -> EnsureProjection (the plan just synthesized;
//     its phase tree becomes sub-issues of the intake ticket).
//   - "plan.phase_state"    -> MirrorState (phase state transitions the
//     PlanManager publishes: in_progress on synthesize, completed on
//     phase/task completion).
//
// A nil msgBus or nil lifecycle ctx (the wiring gate fixture) skips
// silently: the projector stays constructed but inert, never a panic.
// Subscribers exit on ctx.Done and unsubscribe on the way out, the same
// posture as the employee.critical_finding subscriber. Every handler is
// fail-soft: a projection error logs Warn and never touches the plan
// lifecycle that published the event.
func wireIntakeProjectionSubscriptions(c *Components, cfg config.IntakeConfig, logger *slog.Logger) {
	if c.msgBus == nil || c.ctx == nil {
		logger.Debug("intake projection subscriptions skipped: no bus or lifecycle context on Components")
		return
	}
	if c.PlanManager == nil || c.EmployeeManager == nil {
		logger.Warn("intake projection subscriptions skipped: PlanManager or EmployeeManager not wired")
		return
	}

	execSub := c.msgBus.Subscribe("intake-projection-executing", "plan.executing")
	go func() {
		defer c.msgBus.Unsubscribe(execSub)
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg, ok := <-execSub.Channel:
				if !ok {
					return
				}
				projectPlanFromEvent(c.ctx, c, cfg, logger, msg)
			}
		}
	}()

	stateSub := c.msgBus.Subscribe("intake-projection-phase-state", "plan.phase_state")
	go func() {
		defer c.msgBus.Unsubscribe(stateSub)
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg, ok := <-stateSub.Channel:
				if !ok {
					return
				}
				var ev struct {
					PlanID  string `json:"plan_id"`
					PhaseID string `json:"phase_id"`
					State   string `json:"state"`
				}
				if err := json.Unmarshal(msg.Payload, &ev); err != nil {
					logger.Warn("plan.phase_state: unmarshal failed", "error", err)
					continue
				}
				if ev.PlanID == "" || ev.PhaseID == "" || ev.State == "" {
					continue
				}
				// MirrorState is fail-soft by contract: it logs its own
				// Warns and returns nil. The projection only exists for
				// intake-linked plans anyway — an unprojected phase gets
				// ErrUnknownKey inside and is skipped there.
				if err := c.IntakeProjector.MirrorState(c.ctx, ev.PlanID, ev.PhaseID, ev.State); err != nil {
					logger.Warn("intake projection: MirrorState returned an error",
						"plan_id", ev.PlanID, "phase_id", ev.PhaseID, "state", ev.State, "error", err)
				}
			}
		}
	}()
}

// projectPlanFromEvent handles one plan.executing bus event: resolve the
// plan's intake ticket (plan -> goal -> TriggerRef intake:<repo>#<ticket>),
// then EnsureProjection the phase tree under it. Every step is fail-soft —
// a non-intake plan (no goal, no TriggerRef) is the common case and skips
// quietly at Debug.
func projectPlanFromEvent(ctx context.Context, c *Components, cfg config.IntakeConfig,
	logger *slog.Logger, msg *models.BusMessage) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("intake projection handler panic recovered", "recovered", r)
		}
	}()

	var ev struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(msg.Payload, &ev); err != nil || ev.PlanID == "" {
		return
	}

	p, err := c.PlanManager.GetPlan(ctx, ev.PlanID)
	if err != nil {
		logger.Warn("intake projection: plan lookup failed", "plan_id", ev.PlanID, "error", err)
		return
	}
	if p.GoalID == "" {
		return // not a goal-driven plan: nothing to project
	}
	goal, err := c.EmployeeManager.GetGoal(ctx, p.GoalID)
	if err != nil || goal == nil || goal.TriggerRef == "" {
		return // not an intake-linked goal
	}
	if !strings.HasPrefix(goal.TriggerRef, intake.TriggerRefPrefix) {
		return // trigger source other than intake (cron, manual)
	}
	repoTicket := strings.TrimPrefix(goal.TriggerRef, intake.TriggerRefPrefix)
	repo, ticketID, ok := strings.Cut(repoTicket, "#")
	if !ok || repo == "" || ticketID == "" {
		return
	}
	if repo != cfg.WatchRepo {
		// The projection board is scoped to the watch repo; a goal from a
		// different repo's intake (future multi-repo) is out of scope.
		logger.Debug("intake projection: goal TriggerRef repo differs from watch_repo",
			"plan_id", ev.PlanID, "goal_repo", repo, "watch_repo", cfg.WatchRepo)
		return
	}

	phases, err := c.PlanManager.GetPhases(ctx, ev.PlanID)
	if err != nil {
		logger.Warn("intake projection: phase lookup failed", "plan_id", ev.PlanID, "error", err)
		return
	}
	if len(phases) == 0 {
		return
	}
	phaseVals := make([]plan.PlanPhase, 0, len(phases))
	for _, ph := range phases {
		if ph != nil {
			phaseVals = append(phaseVals, *ph)
		}
	}

	// Real step lists when the step store is reachable: the phase child
	// task IDs come from the manager's task map via GetPhasesByTask on the
	// parent task. Best-effort — a nil/failed lookup falls back to
	// synthesized steps from each phase's TotalSteps counter.
	var stepsByPhase map[string][]task.TaskStep
	if c.StepStore != nil && p.TaskID != "" {
		if phaseTasks, ptErr := c.PlanManager.GetPhasesByTask(ctx, p.TaskID); ptErr == nil && len(phaseTasks) > 0 {
			stepsByPhase = map[string][]task.TaskStep{}
			for _, ph := range phaseTasks {
				if ph.TaskID == "" {
					continue
				}
				if steps, sErr := c.StepStore.ListByTaskID(ph.TaskID); sErr == nil {
					for _, st := range steps {
						if st != nil {
							stepsByPhase[ph.ID] = append(stepsByPhase[ph.ID], *st)
						}
					}
				}
			}
		}
	}

	ticket := intake.Ticket{ID: ticketID, Repo: repo}
	if err := c.IntakeProjector.EnsureProjectionWithSteps(ctx, ticket, ev.PlanID, phaseVals, stepsByPhase); err != nil {
		logger.Warn("intake projection failed (fail-soft; the plan keeps running)",
			"plan_id", ev.PlanID, "repo", repo, "ticket", ticketID, "error", err)
		return
	}
	logger.Info("intake projection complete",
		"plan_id", ev.PlanID, "repo", repo, "ticket", ticketID, "phases", len(phaseVals))
}
