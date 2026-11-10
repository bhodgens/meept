// Package daemon — ledger_integrity.go implements plan 20261109-employee-ledger
// leaf 04-ops/01: the scheduled `ledger-integrity` job. Each run folds the
// append-only log and diffs it against ledger_state via ledger.VerifyState;
// a clean report logs INFO, a divergent report logs ERROR with per-employee
// diff counts (keys only — never payload content), and RepairState runs only
// when [ledger].auto_repair is true (default false: divergence is reported,
// not silently rewritten).
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/scheduler"
)

// ledgerIntegrityJobID is the scheduler job id for the periodic integrity
// pass (the name the plan's Ops contract pins: 'ledger-integrity').
const ledgerIntegrityJobID = "ledger-integrity"

// LedgerIntegrityJob runs one VerifyState/RepairState pass over a ledger
// store. Constructed by the wiring below; tests call RunOnce directly.
type LedgerIntegrityJob struct {
	store  *ledger.Store
	cfg    config.LedgerConfig
	logger *slog.Logger
}

// NewLedgerIntegrityJob builds the integrity pass. A nil store fails closed
// at registration (see Components.registerLedgerIntegrityJob), so this
// constructor simply guards for direct/test use.
func NewLedgerIntegrityJob(store *ledger.Store, cfg config.LedgerConfig, logger *slog.Logger) *LedgerIntegrityJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &LedgerIntegrityJob{store: store, cfg: cfg, logger: logger}
}

// RunOnce executes one integrity pass:
//   - VerifyState clean (0 divergent) -> one INFO "ledger integrity clean"
//     with the checked-row count.
//   - divergent -> ERROR with per-employee diff counts (employee keys only,
//     never payload content) + per-diff (employee, subject) keys.
//   - cfg.AutoRepair true -> RepairState after a divergent report, INFO with
//     the repaired count. AutoRepair false (default) -> no repair at all.
//
// Returns the report (nil on verify error) so callers/tests can assert
// counts without scraping logs.
func (j *LedgerIntegrityJob) RunOnce(ctx context.Context) (*ledger.VerifyReport, error) {
	if j.store == nil {
		return nil, fmt.Errorf("ledger integrity: no store")
	}

	report, err := j.store.VerifyState(ctx)
	if err != nil {
		j.logger.Error("ledger integrity verify failed", "error", err)
		return nil, err
	}

	if report.Divergent == 0 {
		j.logger.Info("ledger integrity clean", "checked", report.Checked)
		return report, nil
	}

	// ERROR with per-employee diff counts — identity keys only; the
	// payload content never reaches the log (contract: keys, not data).
	perEmployee := make(map[string]int)
	for _, d := range report.Diffs {
		perEmployee[d.EmployeeID]++
	}
	employees := make([]string, 0, len(perEmployee))
	for e := range perEmployee {
		employees = append(employees, e)
	}
	sort.Strings(employees)
	counts := make([]any, 0, len(employees))
	for _, e := range employees {
		counts = append(counts, e+"="+fmt.Sprint(perEmployee[e]))
	}
	j.logger.Error("ledger integrity divergent",
		"checked", report.Checked,
		"divergent", report.Divergent,
		"per_employee", counts,
	)
	for _, d := range report.Diffs {
		j.logger.Error("ledger integrity diff",
			"employee_id", d.EmployeeID,
			"subject", d.Subject,
			"have_status", string(d.Have.Status),
			"want_status", string(d.Want.Status),
		)
	}

	if j.cfg.AutoRepair {
		repaired, err := j.store.RepairState(ctx)
		if err != nil {
			j.logger.Error("ledger integrity repair failed", "error", err)
			return report, err
		}
		j.logger.Info("ledger integrity repaired", "repaired", repaired)
	}
	return report, nil
}

// registerLedgerIntegrityJob schedules the periodic integrity pass on the
// daemon scheduler, copying the interval-job registration pattern used by
// the backup/trace-analysis schedulers. Guarded: a nil store (ledger
// disabled — the pinned buildLedger degrade posture) or a nil scheduler
// registers NOTHING; the daemon boots ledger-less unchanged.
func (c *Components) registerLedgerIntegrityJob(cfg *config.Config, logger *slog.Logger) {
	if c.EmployeeLedger == nil {
		// nil store -> no job (self-verification checklist guard).
		return
	}
	if c.Scheduler == nil {
		if logger != nil {
			logger.Warn("ledger integrity: scheduler unavailable; job not registered")
		}
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	lcfg := cfg.Ledger
	config.NormalizeLedgerDefaults(&lcfg)
	interval := time.Duration(lcfg.IntervalHours) * time.Hour

	job := &ledgerIntegrityJob{
		id:       ledgerIntegrityJobID,
		name:     "employee ledger integrity (verify" + repairSuffix(lcfg) + ")",
		interval: interval,
		runner:   NewLedgerIntegrityJob(c.EmployeeLedger, lcfg, logger.With("component", "ledger-integrity")),
		logger:   logger.With("component", "ledger-integrity"),
	}
	if _, err := c.Scheduler.Schedule(job); err != nil {
		logger.Warn("ledger integrity: schedule failed", "job_id", ledgerIntegrityJobID, "error", err)
		return
	}
	logger.Info("Ledger integrity job scheduled",
		"job_id", ledgerIntegrityJobID,
		"interval", interval.String(),
		"auto_repair", lcfg.AutoRepair,
	)
}

func repairSuffix(lcfg config.LedgerConfig) string {
	if lcfg.AutoRepair {
		return "+auto-repair"
	}
	return ""
}

// ledgerIntegrityJob is the minimal scheduler.Job wrapper (mirrors
// simpleIntervalJob / traceAnalysisJob): an "@every <interval>" cron
// expression firing RunOnce with panic isolation, so a corrupt state row
// never takes down the scheduler's cron goroutine.
type ledgerIntegrityJob struct {
	id       string
	name     string
	interval time.Duration
	runner   *LedgerIntegrityJob
	logger   *slog.Logger
}

// ID implements scheduler.Job.
func (j *ledgerIntegrityJob) ID() string { return j.id }

// Name implements scheduler.Job.
func (j *ledgerIntegrityJob) Name() string { return j.name }

// Schedule implements scheduler.Job: cron "@every <interval>".
func (j *ledgerIntegrityJob) Schedule() string { return fmt.Sprintf("@every %s", j.interval) }

// Type implements scheduler.Job.
func (j *ledgerIntegrityJob) Type() scheduler.JobType { return scheduler.JobTypeInterval }

// Execute implements scheduler.Job: one integrity pass with a bounded
// context and panic recovery.
func (j *ledgerIntegrityJob) Execute(_ context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			j.logger.Error("ledger integrity job panic recovered", "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err := j.runner.RunOnce(ctx)
	return err
}

// Config implements scheduler.Job.
func (j *ledgerIntegrityJob) Config() scheduler.JobConfig {
	return scheduler.JobConfig{
		ID:        j.id,
		Name:      j.name,
		Type:      scheduler.JobTypeInterval,
		Schedule:  j.Schedule(),
		Enabled:   true,
		CreatedAt: timeNowUTC(),
		Tags:      []string{"ledger", "integrity"},
	}
}
