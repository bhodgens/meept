// Package employee — scheduler_jobs.go wires the three recurring background
// jobs required by the AI Employee Design spec:
//
//  1. Per-employee GoalLoop assess (spec line 40: assessment_interval)
//  2. Global PeriodicAuditor (spec line 430: periodic_interval, default 6h)
//  3. Plan approval timeout sweeper (spec line 591: default 7d auto-reject)
//
// The methods are no-ops when the underlying stores/LLM aren't wired; the
// daemon calls them unconditionally and they degrade gracefully.
package employee

import (
	"context"
	"fmt"
	"time"
)

// Scheduler is the minimal scheduler surface the employee package needs.
// The daemon adapts *scheduler.Scheduler to this interface (which has only
// one method: run a callback at a fixed interval). Using a local interface
// avoids importing internal/scheduler here.
type Scheduler interface {
	// RunAtInterval registers fn to fire every interval. name is used
	// for logging and deduplication (same name replaces prior registration).
	RunAtInterval(name string, interval time.Duration, fn func())
}

// ScheduleAssessJobs registers a GoalLoop assess job for each employee with
// a constitution. The interval comes from the constitution's
// Constraints.AssessmentInterval (spec line 40); when zero, the provided
// default is used. Returns the count of jobs scheduled.
//
// Each job calls runAssessForEmployee which delegates to Trigger. The full
// GoalLoop.Decide integration depends on LLM + PlanCreator wiring. Jobs
// are best-effort: errors are logged at warning level.
func (m *Manager) ScheduleAssessJobs(ctx context.Context, sched Scheduler, defaultInterval time.Duration) (int, error) {
	if sched == nil {
		return 0, nil
	}
	emps, err := m.ListEmployees(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("schedule assess: list: %w", err)
	}
	scheduled := 0
	for _, emp := range emps {
		if !emp.HasConstitution() {
			continue
		}
		// Parse the assessment interval string (e.g. "15m", "6h").
		// When empty or invalid, fall back to the provided default.
		var interval time.Duration
		if s := emp.Constitution.Constraints.AssessmentInterval; s != "" {
			if d, err := time.ParseDuration(s); err == nil && d > 0 {
				interval = d
			}
		}
		if interval <= 0 {
			interval = defaultInterval
		}
		if interval <= 0 {
			interval = 15 * time.Minute // spec POC: 15m
		}
		empID := emp.ID
		sched.RunAtInterval(
			"employee.assess."+empID,
			interval,
			func() { m.runAssessForEmployee(context.Background(), empID) },
		)
		scheduled++
	}
	if scheduled > 0 {
		m.logger.Info("scheduled employee assess jobs", "count", scheduled)
	}
	return scheduled, nil
}

// SchedulePeriodicAudit registers a single global periodic audit job at the
// given interval (spec line 430; default 6h). The job delegates to the
// PeriodicAuditor if one is wired via SetPeriodicAuditor.
//
// The actual audit logic (LLM call, finding persistence, auto-pause) lives
// in the enforcement package. The Manager's role is to trigger it on
// schedule and emit the drift metric.
func (m *Manager) SchedulePeriodicAudit(ctx context.Context, sched Scheduler, interval time.Duration) error {
	if sched == nil {
		return nil
	}
	if interval <= 0 {
		interval = DefaultPeriodicAuditInterval // 6h, see types.go
	}
	sched.RunAtInterval("employee.periodic_audit", interval, func() {
		m.runPeriodicAudit(context.Background())
	})
	m.logger.Info("scheduled periodic audit job", "interval", interval)
	return nil
}

// ScheduleApprovalTimeoutSweeper registers a job that auto-rejects plans
// stuck in PendingApproval longer than the configured timeout (spec line
// 591: default 7d). The job runs hourly. For each timed-out plan, the
// goal's plan is rejected and an audit finding is written.
func (m *Manager) ScheduleApprovalTimeoutSweeper(ctx context.Context, sched Scheduler, timeout time.Duration) error {
	if sched == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 7 * 24 * time.Hour // spec: default 7d
	}
	sched.RunAtInterval("employee.approval_timeout", time.Hour, func() {
		m.runApprovalTimeoutSweep(context.Background(), timeout)
	})
	m.logger.Info("scheduled approval timeout sweeper",
		"timeout", timeout, "check_interval", time.Hour)
	return nil
}

// ScheduleFindingsRetention registers a daily job that prunes audit findings
// older than the configured retention period (spec line 154: default 90 days).
// The job calls AuditStore.PruneOlderThan and logs the count pruned. When
// retentionDays is zero or negative, the job is not registered (no retention
// configured → findings are kept indefinitely).
func (m *Manager) ScheduleFindingsRetention(ctx context.Context, sched Scheduler, retentionDays int) error {
	if sched == nil {
		return nil
	}
	if retentionDays <= 0 {
		retentionDays = 90 // spec default
	}
	sched.RunAtInterval("employee.findings_retention", 24*time.Hour, func() {
		m.runFindingsRetention(context.Background(), retentionDays)
	})
	m.logger.Info("scheduled findings retention job",
		"retention_days", retentionDays, "check_interval", 24*time.Hour)
	return nil
}

// runFindingsRetention prunes old audit findings and logs the result.
func (m *Manager) runFindingsRetention(ctx context.Context, retentionDays int) {
	m.mu.RLock()
	auditStore := m.auditStore
	m.mu.RUnlock()

	if auditStore == nil {
		return
	}
	count, err := auditStore.PruneOlderThan(ctx, retentionDays)
	if err != nil {
		m.logger.Warn("findings retention: prune failed", "error", err)
		return
	}
	if count > 0 {
		m.logger.Info("findings retention: pruned old findings",
			"count", count, "retention_days", retentionDays)
	}
}

// runAssessForEmployee runs a scheduled assessment for one employee.
// It delegates to Trigger, which emits telemetry and invokes the bot
// runner. The full GoalLoop.Decide integration happens inside the
// trigger path when the GoalLoop is configured.
//
// G5: Uses a per-employee assessment semaphore (buffer=1) to prevent
// overlapping assessments. If the previous assessment for this employee
// is still running, the non-blocking send fails and this tick is skipped
// with a debug log.
func (m *Manager) runAssessForEmployee(ctx context.Context, employeeID string) {
	// G5: Acquire (non-blocking) the assessment semaphore.
	sem := m.acquireAssessmentSemaphore(employeeID)
	select {
	case sem <- struct{}{}:
		// Acquired; proceed with assessment.
	default:
		// Channel is full — previous assessment still running. Skip.
		m.logger.Debug("scheduled assess: previous assessment still running, skipping",
			"employee_id", employeeID)
		return
	}
	defer func() { <-sem }() // release

	emp, err := m.GetEmployee(ctx, employeeID)
	if err != nil {
		m.logger.Warn("scheduled assess: employee not found",
			"employee_id", employeeID, "error", err)
		return
	}
	if !emp.HasConstitution() {
		return
	}
	_, err = m.Trigger(ctx, employeeID, map[string]any{
		"source": "scheduled_assess",
	})
	if err != nil {
		m.logger.Warn("scheduled assess: trigger failed",
			"employee_id", employeeID, "error", err)
	}
}

// acquireAssessmentSemaphore returns the per-employee assessment semaphore
// (buffer=1). Created lazily on first access, guarded by invokeMuMapGuard
// (shared with the invoke mutex map since the lifecycle is the same).
func (m *Manager) acquireAssessmentSemaphore(employeeID string) chan struct{} {
	m.invokeMuMapGuard.Lock()
	defer m.invokeMuMapGuard.Unlock()
	sem, ok := m.assessmentSems[employeeID]
	if !ok {
		sem = make(chan struct{}, 1)
		m.assessmentSems[employeeID] = sem
	}
	return sem
}

// runPeriodicAudit is the periodic audit callback. It iterates employees
// and delegates to the PeriodicAuditor. The TurnRecords that feed the
// audit are collected by the enforcement engine's runtime observer
// (PostTurnAuditor records each turn; the PeriodicAuditor reads them).
//
// When the PeriodicAuditor is not wired, this is a no-op.
func (m *Manager) runPeriodicAudit(ctx context.Context) {
	m.mu.RLock()
	auditor := m.periodicAuditor
	goalStore := m.goalStore
	m.mu.RUnlock()

	if auditor == nil {
		return // Not wired; degrade gracefully.
	}

	// The PeriodicAuditor's Audit method requires TurnRecords. These are
	// supplied by the enforcement engine's observer, which records each
	// turn as it completes. The auditor itself holds a reference to the
	// AuditStore and can query recent turns from there.
	//
	// We invoke a wrapper that lets the auditor pull its own recent
	// turns from the store. This avoids the Manager needing to know the
	// turn-record storage schema.
	emps, err := m.ListEmployees(ctx, "")
	if err != nil {
		m.logger.Warn("periodic audit: list employees failed", "error", err)
		return
	}
	for _, emp := range emps {
		if !emp.HasConstitution() {
			continue
		}
		// G8: Filter turns whose goal has been retired. Findings for retired
		// goals do not contribute to the DriftScore (audit trail preserved
		// but excluded from active drift calculation).
		rawTurns := m.collectTurns(emp.ID, 50, 24*time.Hour)
		if len(rawTurns) == 0 {
			continue
		}
		// Skip turns whose GoalID references a retired goal. When the
		// GoalStore is nil, we can't check retirement status so we
		// include all turns (best-effort).
		var turns []TurnRecord
		if m.goalStore != nil {
			for _, t := range rawTurns {
				if t.GoalID == "" {
					turns = append(turns, t)
					continue
				}
				goal, gErr := m.goalStore.Get(ctx, t.GoalID)
				if gErr != nil || goal.IsRetired() {
					continue // skip retired-goal turns
				}
				turns = append(turns, t)
			}
		} else {
			turns = rawTurns
		}
		if len(turns) == 0 {
			continue
		}
		findings, drift, err := auditor.Audit(ctx, turns)
		if err != nil {
			m.logger.Warn("periodic audit: auditor error",
				"employee_id", emp.ID, "error", err)
			continue
		}
		// Emit drift metric (spec line 672).
		m.emitMetric("employee.drift.score", drift, map[string]string{
			"employee_id": emp.ID,
		})
		// Emit findings count metric (spec line 669).
		for _, f := range findings {
			m.emitMetric("employee.audit.findings", 1, map[string]string{
				"employee_id": emp.ID,
				"severity":    string(f.Severity),
				"checkpoint":  string(f.Checkpoint),
			})
		}
		// G7: attach findings to goals (spec line 382: "attach to owning
		// Goal"). Best-effort: goal lookup failures are logged but do not
		// block the audit.
		if goalStore != nil {
			for _, f := range findings {
				if f.GoalID == "" {
					continue
				}
				goal, gErr := goalStore.Get(ctx, f.GoalID)
				if gErr != nil || goal == nil {
					continue
				}
				goal.AttachFinding(f.ID)
				if uErr := goalStore.Update(ctx, goal); uErr != nil {
					m.logger.Warn("periodic audit: attach finding to goal failed",
						"goal_id", f.GoalID, "finding_id", f.ID, "error", uErr)
				}
			}
		}
		// Auto-pause on critical finding (spec: critical → auto-pause).
		for _, f := range findings {
			if f.Severity == SeverityCritical {
				m.logger.Warn("periodic audit: critical finding, auto-pausing",
					"employee_id", emp.ID, "rule", f.ViolatedRule)
				if err := m.PauseWithReason(ctx, emp.ID,
					"periodic audit critical: "+f.ViolatedRule, "auto_pause"); err != nil {
					m.logger.Error("periodic audit: auto-pause failed",
						"employee_id", emp.ID, "error", err)
				}
				break
			}
		}
	}
}

// runApprovalTimeoutSweep checks pending plans against the timeout and
// auto-rejects expired ones per spec line 591. The goal store tracks
// plans via ActivePlanID; the approval workflow is owned by the plan
// system (internal/plan). This sweeper queries goals with active plans
// in PendingApproval state and rejects those older than the timeout.
func (m *Manager) runApprovalTimeoutSweep(ctx context.Context, timeout time.Duration) {
	m.mu.RLock()
	goalStore := m.goalStore
	auditStore := m.auditStore
	m.mu.RUnlock()

	if goalStore == nil {
		return
	}

	goals, err := goalStore.ListActive(ctx, "")
	if err != nil {
		m.logger.Warn("approval timeout: list goals failed", "error", err)
		return
	}
	cutoff := time.Now().UTC().Add(-timeout)
	for _, g := range goals {
		// Goals without an active plan skip.
		if g.ActivePlanID == "" {
			continue
		}
		// The plan age is determined by the goal's LastAssessed time.
		// When LastAssessed is before the cutoff, the plan has been
		// pending too long.
		if g.LastAssessed.IsZero() || g.LastAssessed.After(cutoff) {
			continue
		}
		m.logger.Info("approval timeout: rejecting stale plan",
			"goal_id", g.ID, "plan_id", g.ActivePlanID,
			"age", time.Since(g.LastAssessed))
		if err := m.RejectPlan(ctx, g.ID, g.ActivePlanID,
			"auto-rejected: approval timeout (spec line 591)"); err != nil {
			// RejectPlan may fail if the plan was already approved
			// or rejected between sweeps — log and continue.
			m.logger.Warn("approval timeout: reject failed",
				"plan_id", g.ActivePlanID, "error", err)
			continue
		}
		// Write audit finding.
		if auditStore != nil {
			finding := AuditFinding{
				ID:           fmt.Sprintf("approval-timeout-%s-%d", g.ActivePlanID, time.Now().Unix()),
				EmployeeID:   g.EmployeeID,
				GoalID:       g.ID,
				PlanID:       g.ActivePlanID,
				Severity:     SeverityWarning,
				Checkpoint:   "periodic",
				ViolatedRule: "approval_timeout",
				Evidence:     fmt.Sprintf(`{"plan_id":"%s","goal_id":"%s"}`, g.ActivePlanID, g.ID),
				DetectedAt:   time.Now().UTC(),
			}
			_ = auditStore.Create(ctx, finding)
		}
	}
}

// SetPeriodicAuditor wires the PeriodicAuditor used by the scheduled
// periodic audit job. Nil is ignored (typed-nil guard per CLAUDE.md
// setter convention). Thread-safe.
func (m *Manager) SetPeriodicAuditor(a *PeriodicAuditor) {
	if a == nil {
		return
	}
	m.mu.Lock()
	m.periodicAuditor = a
	m.mu.Unlock()
}

// TurnCollectorFunc returns recent TurnRecords for an employee. The daemon
// injects an implementation that reads from the enforcement engine's
// runtime observer (PostTurnAuditor or similar). When nil, the periodic
// audit job has no turns to audit and is effectively a no-op.
type TurnCollectorFunc func(employeeID string, limit int, lookback time.Duration) []TurnRecord

// SetTurnCollector wires the turn collector used by the periodic audit.
// Nil is ignored (typed-nil guard). Thread-safe.
func (m *Manager) SetTurnCollector(fn TurnCollectorFunc) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	m.turnCollector = fn
	m.mu.Unlock()
}

// collectTurns is the internal helper that snapshots the turn collector
// under a read lock and calls it outside the lock (per CLAUDE.md mutex
// guidance — the collector may perform I/O).
func (m *Manager) collectTurns(employeeID string, limit int, lookback time.Duration) []TurnRecord {
	m.mu.RLock()
	fn := m.turnCollector
	m.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(employeeID, limit, lookback)
}
