package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration
)

// Crash-safety design (loop-economics leaf 03):
//
// The scheduler wraps robfig/cron, whose tick loop is internal and not
// extensible with a wake/catch-up hook. Claim-before-deliver semantics are
// therefore implemented at the executeJob dispatch boundary:
//
//  1. Normal cron fires: wrapJob snapshots the wall-clock fire time as the
//     tick, and executeJob claims it atomically before delivery. A claim row
//     that survives a daemon crash marks the tick delivered — after restart,
//     the same tick is never re-dispatched.
//  2. Missed ticks: on wake (daemon startup or explicit ProcessMissedTicks),
//     due ticks since lastWake are enumerated per job. With coalesce_missed
//     enabled (default) only MAX(tick) is enqueued, once, with missed_count
//     metadata; disabled restores the accumulate-each behavior.
//
// SQLite driver: modernc.org/sqlite (pure Go), matching internal/queue,
// internal/session, internal/memory. The DSN uses the _pragma form that the
// driver actually honors (queue's _journal_mode query params are silently
// ignored by modernc; see modernc.org/sqlite applyQueryParams).

// DefaultClaimRetentionDays is the default retention for claimed ticks.
const DefaultClaimRetentionDays = 7

// claimDBFileName is the SQLite database holding claimed ticks, stored in the
// scheduler's data directory.
const claimDBFileName = "scheduler_claims.db"

// claimTickTimeFormat is a fixed-width RFC3339 variant (nanosecond precision)
// so tick_time TEXT values compare lexicographically in chronological order.
// RFC3339Nano drops trailing fractional zeros, which breaks string ordering.
const claimTickTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// claimStore persists claimed ticks in SQLite.
type claimStore struct {
	db *sql.DB
}

// newClaimStore opens (creating if needed) the claimed-ticks database and
// applies the schema migration.
func newClaimStore(dbPath string) (*claimStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open claim database: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite writes must be serialized

	if err := migrateClaimStore(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("migration failed: %w; close failed: %v", err, closeErr)
		}
		return nil, err
	}
	return &claimStore{db: db}, nil
}

// migrateClaimStore creates the claimed-ticks table. Runs on every open so it
// is the migration path for this store (the scheduler has no separate
// migration framework).
func migrateClaimStore(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS scheduler_claimed_ticks (
	job_id     TEXT NOT NULL,
	tick_time  TEXT NOT NULL,
	claimed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
	UNIQUE(job_id, tick_time)
);
CREATE INDEX IF NOT EXISTS idx_scheduler_claimed_ticks_tick_time
	ON scheduler_claimed_ticks(tick_time);`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create scheduler_claimed_ticks table: %w", err)
	}
	return nil
}

// ClaimTick atomically claims delivery of a tick for a job. It returns false
// (without error) when the tick was already claimed — e.g. by a previous
// daemon instance before it crashed.
func (cs *claimStore) ClaimTick(jobID string, tick time.Time) (bool, error) {
	if jobID == "" {
		return false, errors.New("claim tick: job ID is required")
	}
	if tick.IsZero() {
		return false, errors.New("claim tick: tick time is required")
	}
	tickUTC := tick.UTC().Format(claimTickTimeFormat)

	res, err := cs.db.Exec(
		"INSERT INTO scheduler_claimed_ticks(job_id, tick_time) VALUES(?,?)",
		jobID, tickUTC,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to claim tick for job %s: %w", jobID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to check claim result for job %s: %w", jobID, err)
	}
	return n > 0, nil
}

// PruneClaimsOlderThan deletes claims with tick_time strictly older than
// cutoff and returns the number of rows removed.
func (cs *claimStore) PruneClaimsOlderThan(cutoff time.Time) (int64, error) {
	res, err := cs.db.Exec(
		"DELETE FROM scheduler_claimed_ticks WHERE tick_time < ?",
		cutoff.UTC().Format(claimTickTimeFormat),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to prune claimed ticks: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count pruned ticks: %w", err)
	}
	return n, nil
}

// ClaimCount returns the total number of claimed ticks (test/inspection aid).
func (cs *claimStore) ClaimCount() (int64, error) {
	var n int64
	if err := cs.db.QueryRow("SELECT COUNT(*) FROM scheduler_claimed_ticks").Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count claimed ticks: %w", err)
	}
	return n, nil
}

func (cs *claimStore) close() error {
	if err := cs.db.Close(); err != nil {
		return fmt.Errorf("failed to close claim database: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE-constraint
// violation.
func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "SQLITE_CONSTRAINT_UNIQUE")
}

// lastWakeFileName stores the last wake timestamp in the scheduler's data
// directory. File-based to match the existing jobs.json persistence pattern;
// it is rewritten atomically via temp-file rename.
const lastWakeFileName = "last_wake.json"

// lastWakeData is the persisted last-wake envelope.
type lastWakeData struct {
	LastWake string `json:"last_wake"`
}

// readLastWake reads the persisted last wake time. Returns the zero time when
// no file exists yet (fresh install).
func readLastWake(dataDir string) (time.Time, error) {
	if dataDir == "" {
		return time.Time{}, nil
	}
	path := filepath.Join(dataDir, lastWakeFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to read last wake file: %w", err)
	}
	var envelope lastWakeData
	if err := json.Unmarshal(data, &envelope); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse last wake file: %w", err)
	}
	if envelope.LastWake == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, envelope.LastWake)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid last_wake %q: %w", envelope.LastWake, err)
	}
	return parsed.UTC(), nil
}

// writeLastWake persists the last wake time atomically.
func writeLastWake(dataDir string, wake time.Time) error {
	if dataDir == "" {
		return errors.New("write last wake: data directory not configured")
	}
	envelope := lastWakeData{LastWake: wake.UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal last wake: %w", err)
	}
	path := filepath.Join(dataDir, lastWakeFileName)
	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write last wake temp file: %w", err)
	}
	if err := os.Rename(tempFile, path); err != nil {
		if removeErr := os.Remove(tempFile); removeErr != nil {
			return fmt.Errorf("failed to rename last wake file: %w; cleanup failed: %v", err, removeErr)
		}
		return fmt.Errorf("failed to rename last wake file: %w", err)
	}
	return nil
}

// ExecutionOptions carries per-delivery context from the dispatch boundary
// into executeJob.
type ExecutionOptions struct {
	// Tick is the scheduled tick being delivered. Zero means unknown
	// (e.g. manual RunNow), in which case executeJob falls back to the
	// execution start time for claiming.
	Tick time.Time
	// MissedCount is the number of ticks coalesced into this delivery
	// (0 for normal fires and accumulate mode).
	MissedCount int
	// PreClaimed marks that the caller already claimed Tick atomically
	// (the missed-tick sweep does claim-then-dispatch); executeJob must
	// not re-claim it, which would read as a duplicate and skip delivery.
	PreClaimed bool
}

// MissedTickRun describes a catch-up delivery produced by ProcessMissedTicks.
type MissedTickRun struct {
	JobID       string
	Tick        time.Time // MAX(tick) among due ticks when coalescing
	MissedCount int       // ticks skipped by coalescing (due-1); 0 in accumulate mode
}

// ProcessMissedTicks computes due ticks per job since lastWake and delivers
// them. With coalesce_missed enabled (default) only the latest tick per job
// is delivered, once, with missed_count metadata logged at info level;
// disabled, every due tick is delivered individually. lastWake is advanced to
// now after the sweep.
//
// The scheduler must be running. Call this once at startup (Start does this
// automatically) or after known downtime; cron fires continue to use the
// claim-before-deliver path, so double delivery cannot occur either way.
func (s *Scheduler) ProcessMissedTicks(now time.Time) ([]MissedTickRun, error) {
	if !s.running.Load() {
		return nil, errors.New("scheduler not running")
	}
	s.mu.RLock()
	coalesce := s.coalesceMissed
	s.mu.RUnlock()

	lastWake, err := s.GetLastWake()
	if err != nil {
		return nil, err
	}
	if lastWake.IsZero() {
		// Fresh install: nothing to catch up on; anchor lastWake.
		if setErr := s.SetLastWake(now.UTC()); setErr != nil {
			s.logger.Warn("scheduler: failed to anchor last wake", "error", setErr)
		}
		return nil, nil
	}
	now = now.UTC()
	if !now.After(lastWake) {
		return nil, nil // nothing due; also avoids a busy loop on rapid re-calls
	}

	// Snapshot jobs under the lock; all heavy work happens outside it.
	s.mu.RLock()
	type dueJob struct {
		job   Job
		spec  string
		ticks []time.Time
	}
	jobs := make([]dueJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		spec := job.Schedule()
		sched, parseErr := s.parseSchedule(spec)
		if parseErr != nil {
			s.logger.Warn("scheduler: skipping catch-up for job with invalid schedule",
				"job_id", job.ID(), "schedule", spec, "error", parseErr)
			continue
		}
		var ticks []time.Time
		for tick := sched.Next(lastWake); !tick.After(now); tick = sched.Next(tick) {
			ticks = append(ticks, tick)
		}
		if len(ticks) > 0 {
			jobs = append(jobs, dueJob{job: job, spec: spec, ticks: ticks})
		}
	}
	s.mu.RUnlock()

	var runs []MissedTickRun
	for _, dj := range jobs {
		if coalesce {
			latest := dj.ticks[len(dj.ticks)-1] // sched.Next is monotonic
			missed := len(dj.ticks) - 1
			claimed, claimErr := s.ClaimTick(dj.job.ID(), latest)
			if claimErr != nil {
				s.logger.Warn("scheduler: failed to claim coalesced tick",
					"job_id", dj.job.ID(), "tick", latest, "error", claimErr)
				continue
			}
			if !claimed {
				s.logger.Info("scheduler: coalesced tick already claimed, skipping",
					"job_id", dj.job.ID(), "tick", latest, SchedulerKeyMissedCount, missed)
				continue
			}
			s.logger.Info("scheduler: coalescing missed ticks",
				"job_id", dj.job.ID(),
				"schedule", dj.spec,
				"tick", latest,
				SchedulerKeyMissedCount, missed,
				"due_ticks", len(dj.ticks),
			)
			runs = append(runs, MissedTickRun{JobID: dj.job.ID(), Tick: latest, MissedCount: missed})
			s.dispatchMissed(dj.job, latest, missed)
		} else {
			for _, tick := range dj.ticks {
				claimed, claimErr := s.ClaimTick(dj.job.ID(), tick)
				if claimErr != nil {
					s.logger.Warn("scheduler: failed to claim missed tick",
						"job_id", dj.job.ID(), "tick", tick, "error", claimErr)
					continue
				}
				if !claimed {
					continue // already delivered (e.g. pre-crash)
				}
				s.logger.Info("scheduler: delivering missed tick",
					"job_id", dj.job.ID(), "tick", tick)
				runs = append(runs, MissedTickRun{JobID: dj.job.ID(), Tick: tick})
				s.dispatchMissed(dj.job, tick, 0)
			}
		}
	}

	if err := s.SetLastWake(now); err != nil {
		s.logger.Warn("scheduler: failed to persist last wake", "error", err)
	}
	return runs, nil
}

// dispatchMissed runs a claimed catch-up tick with missed_count metadata
// attached to the job's execution events.
func (s *Scheduler) dispatchMissed(job Job, tick time.Time, missedCount int) {
	s.mu.RLock()
	baseCtx := s.runNowCtx
	s.mu.RUnlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, 30*time.Minute)
	go func() {
		defer cancel()
		s.executeJob(ctx, job, ExecutionOptions{Tick: tick, MissedCount: missedCount, PreClaimed: true})
	}()
}

// ClaimTick atomically claims a tick for a job before delivery. It returns
// false without error when the tick was already claimed (constraint
// violation), which marks it delivered — callers must skip dispatch.
func (s *Scheduler) ClaimTick(jobID string, tick time.Time) (claimed bool, err error) {
	if s.claims == nil {
		return false, errors.New("claim store not initialized")
	}
	return s.claims.ClaimTick(jobID, tick)
}

// ClaimCount returns the number of claimed tick rows (inspection/test aid).
func (s *Scheduler) ClaimCount() (int64, error) {
	if s.claims == nil {
		return 0, errors.New("claim store not initialized")
	}
	return s.claims.ClaimCount()
}

// GetLastWake returns the persisted last wake time (zero if never set).
func (s *Scheduler) GetLastWake() (time.Time, error) {
	wake, err := readLastWake(s.dataDir)
	if err != nil {
		return time.Time{}, err
	}
	return wake, nil
}

// SetLastWake persists the last wake time.
func (s *Scheduler) SetLastWake(wake time.Time) error {
	return writeLastWake(s.dataDir, wake.UTC())
}

// advanceLastWake moves the persisted last-wake timestamp forward to at
// least wake. It is monotonically forward-only: earlier values never move
// the watermark backwards.
func (s *Scheduler) advanceLastWake(wake time.Time) error {
	current, err := s.GetLastWake()
	if err != nil {
		return err
	}
	wake = wake.UTC()
	if !current.IsZero() && !wake.After(current) {
		return nil
	}
	return s.SetLastWake(wake)
}

// pruneExpiredClaims removes claims older than the configured retention and
// logs the result. Called at startup.
func (s *Scheduler) pruneExpiredClaims() {
	cutoff := time.Now().UTC().AddDate(0, 0, -s.claimRetentionDays)
	pruned, err := s.claims.PruneClaimsOlderThan(cutoff)
	if err != nil {
		s.logger.Warn("scheduler: failed to prune expired claims", "error", err)
		return
	}
	if pruned > 0 {
		s.logger.Info("scheduler: pruned expired tick claims",
			"pruned", pruned, "retention_days", s.claimRetentionDays)
	}
}
