package plan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// ErrPlanNotFound is returned when a plan cannot be found by ID.
var ErrPlanNotFound = errors.New("plan not found")

// SQLiteStore implements PlanStore backed by SQLite.
type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteStore creates a new SQLite-backed plan store.
func NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	logger.Info("Plan store initialized", "path", dbPath)
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	// Enable foreign key enforcement so ON DELETE CASCADE works.
	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS plans (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT,
		file_path TEXT NOT NULL,
		project_id TEXT,
		state TEXT NOT NULL DEFAULT 'planning',
		task_id TEXT,
		source_session TEXT,
		approved_at TEXT,
		confirmed_at TEXT,
		approved_by TEXT,
		confirmed_by TEXT,
		revision_count INTEGER DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_plans_state ON plans(state);
	CREATE INDEX IF NOT EXISTS idx_plans_project ON plans(project_id);
	CREATE INDEX IF NOT EXISTS idx_plans_updated_at ON plans(updated_at DESC);

	CREATE TABLE IF NOT EXISTS plan_phases (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		total_steps INTEGER NOT NULL DEFAULT 0,
		completed_steps INTEGER NOT NULL DEFAULT 0,
		failed_steps INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL DEFAULT 'pending'
	);
	CREATE INDEX IF NOT EXISTS idx_plan_phases_plan ON plan_phases(plan_id);

	CREATE TABLE IF NOT EXISTS plan_sessions (
		plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
		session_id TEXT NOT NULL,
		linked_at TEXT NOT NULL,
		PRIMARY KEY (plan_id, session_id)
	);
	CREATE INDEX IF NOT EXISTS idx_plan_sessions_session ON plan_sessions(session_id);

	CREATE TABLE IF NOT EXISTS plan_signoffs (
		id TEXT PRIMARY KEY,
		plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
		phase_id TEXT REFERENCES plan_phases(id),
		session_id TEXT NOT NULL,
		by TEXT NOT NULL,
		action TEXT NOT NULL,
		comment TEXT,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_plan_signoffs_plan ON plan_signoffs(plan_id);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// ---------- Plan CRUD ----------

const planColumns = `id, title, description, file_path, project_id, state, task_id,
	source_session, approved_at, confirmed_at, approved_by, confirmed_by,
	revision_count, created_at, updated_at`

func (s *SQLiteStore) CreatePlan(ctx context.Context, p *Plan) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plans (id, title, description, file_path, project_id, state, task_id,
		                   source_session, approved_at, confirmed_at, approved_by, confirmed_by,
		                   revision_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID,
		p.Title,
		nullableString(p.Description),
		p.FilePath,
		nullableString(p.ProjectID),
		string(p.State),
		nullableString(p.TaskID),
		nullableString(p.SourceSession),
		nullableTime(p.ApprovedAt),
		nullableTime(p.ConfirmedAt),
		nullableString(p.ApprovedBy),
		nullableString(p.ConfirmedBy),
		p.RevisionCount,
		p.CreatedAt.Format(time.RFC3339),
		p.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		s.logger.Error("Failed to create plan", "id", p.ID, "error", err)
		return fmt.Errorf("failed to create plan: %w", err)
	}
	s.logger.Debug("Plan created", "id", p.ID, "title", p.Title)
	return nil
}

func (s *SQLiteStore) GetPlan(ctx context.Context, id string) (*Plan, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+planColumns+" FROM plans WHERE id = ?", id)
	return s.scanPlan(row)
}

func (s *SQLiteStore) UpdatePlan(ctx context.Context, p *Plan) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plans
		SET title = ?, description = ?, file_path = ?, project_id = ?, state = ?,
		    task_id = ?, source_session = ?, approved_at = ?, confirmed_at = ?,
		    approved_by = ?, confirmed_by = ?, revision_count = ?, updated_at = ?
		WHERE id = ?`,
		p.Title,
		nullableString(p.Description),
		p.FilePath,
		nullableString(p.ProjectID),
		string(p.State),
		nullableString(p.TaskID),
		nullableString(p.SourceSession),
		nullableTime(p.ApprovedAt),
		nullableTime(p.ConfirmedAt),
		nullableString(p.ApprovedBy),
		nullableString(p.ConfirmedBy),
		p.RevisionCount,
		p.UpdatedAt.Format(time.RFC3339),
		p.ID,
	)
	if err != nil {
		s.logger.Error("Failed to update plan", "id", p.ID, "error", err)
		return fmt.Errorf("failed to update plan: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeletePlan(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM plans WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete plan: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrPlanNotFound
	}
	s.logger.Info("Plan deleted", "id", id)
	return nil
}

func (s *SQLiteStore) ListPlans(ctx context.Context, projectID string, limit int) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+planColumns+` FROM plans
		WHERE project_id = ?
		ORDER BY updated_at DESC
		LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list plans: %w", err)
	}
	defer rows.Close()
	return s.scanPlans(rows)
}

func (s *SQLiteStore) ListPlansBySession(ctx context.Context, sessionID string) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.`+planColumns+` FROM plans p
		JOIN plan_sessions ps ON p.id = ps.plan_id
		WHERE ps.session_id = ?
		ORDER BY p.updated_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list plans by session: %w", err)
	}
	defer rows.Close()
	return s.scanPlans(rows)
}

func (s *SQLiteStore) ListPlansByState(ctx context.Context, state PlanState, limit int) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+planColumns+` FROM plans
		WHERE state = ?
		ORDER BY updated_at DESC
		LIMIT ?`, string(state), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list plans by state: %w", err)
	}
	defer rows.Close()
	return s.scanPlans(rows)
}

func (s *SQLiteStore) SetPlanState(ctx context.Context, id string, state PlanState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE plans SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), now, id)
	if err != nil {
		return fmt.Errorf("failed to set plan state: %w", err)
	}
	return nil
}

// ---------- Phase operations ----------

const phaseColumns = `id, plan_id, name, sequence, total_steps, completed_steps, failed_steps, state`

func (s *SQLiteStore) CreatePhase(ctx context.Context, p *PlanPhase) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plan_phases (id, plan_id, name, sequence, total_steps, completed_steps, failed_steps, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.PlanID, p.Name, p.Sequence, p.TotalSteps, p.CompletedSteps, p.FailedSteps, string(p.State))
	if err != nil {
		s.logger.Error("Failed to create phase", "id", p.ID, "error", err)
		return fmt.Errorf("failed to create phase: %w", err)
	}
	s.logger.Debug("Phase created", "id", p.ID, "plan_id", p.PlanID)
	return nil
}

func (s *SQLiteStore) GetPhases(ctx context.Context, planID string) ([]*PlanPhase, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+phaseColumns+` FROM plan_phases
		WHERE plan_id = ?
		ORDER BY sequence`, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to get phases: %w", err)
	}
	defer rows.Close()

	var phases []*PlanPhase
	for rows.Next() {
		p, err := s.scanPhaseRow(rows)
		if err != nil {
			s.logger.Error("Failed to scan phase", "error", err)
			continue
		}
		phases = append(phases, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate phases: %w", err)
	}
	return phases, nil
}

func (s *SQLiteStore) UpdatePhase(ctx context.Context, p *PlanPhase) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plan_phases
		SET name = ?, sequence = ?, total_steps = ?, completed_steps = ?, failed_steps = ?, state = ?
		WHERE id = ?`,
		p.Name, p.Sequence, p.TotalSteps, p.CompletedSteps, p.FailedSteps, string(p.State), p.ID)
	if err != nil {
		s.logger.Error("Failed to update phase", "id", p.ID, "error", err)
		return fmt.Errorf("failed to update phase: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SetPhaseState(ctx context.Context, id string, state PhaseState) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE plan_phases SET state = ? WHERE id = ?`, string(state), id)
	if err != nil {
		return fmt.Errorf("failed to set phase state: %w", err)
	}
	return nil
}

func (s *SQLiteStore) IncrementPhaseProgress(ctx context.Context, phaseID string, field string, delta int) error {
	// Validate field to prevent SQL injection.
	if field != "completed_steps" && field != "failed_steps" {
		return fmt.Errorf("invalid progress field: %s", field)
	}
	// #nosec G202 -- field is whitelisted above against known column names
	_, err := s.db.ExecContext(ctx, `
		UPDATE plan_phases SET `+field+` = `+field+` + ? WHERE id = ?`, delta, phaseID)
	if err != nil {
		return fmt.Errorf("failed to increment phase progress: %w", err)
	}
	return nil
}

// ---------- Session linking ----------

func (s *SQLiteStore) LinkSession(ctx context.Context, planID, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO plan_sessions (plan_id, session_id, linked_at)
		VALUES (?, ?, ?)`, planID, sessionID, now)
	if err != nil {
		return fmt.Errorf("failed to link session: %w", err)
	}
	s.logger.Debug("Session linked to plan", "session", sessionID, "plan", planID)
	return nil
}

func (s *SQLiteStore) UnlinkSession(ctx context.Context, planID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM plan_sessions WHERE plan_id = ? AND session_id = ?`, planID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to unlink session: %w", err)
	}
	s.logger.Debug("Session unlinked from plan", "session", sessionID, "plan", planID)
	return nil
}

func (s *SQLiteStore) GetPlansForSession(ctx context.Context, sessionID string) ([]*Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.`+planColumns+` FROM plans p
		JOIN plan_sessions ps ON p.id = ps.plan_id
		WHERE ps.session_id = ?
		ORDER BY p.updated_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plans for session: %w", err)
	}
	defer rows.Close()
	return s.scanPlans(rows)
}

// ---------- Signoff operations ----------

func (s *SQLiteStore) CreateSignoff(ctx context.Context, so *PlanSignoff) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plan_signoffs (id, plan_id, phase_id, session_id, by, action, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		so.ID, so.PlanID, nullableString(so.PhaseID), so.SessionID, so.By, so.Action,
		nullableString(so.Comment), so.CreatedAt.Format(time.RFC3339))
	if err != nil {
		s.logger.Error("Failed to create signoff", "id", so.ID, "error", err)
		return fmt.Errorf("failed to create signoff: %w", err)
	}
	s.logger.Debug("Signoff created", "id", so.ID, "plan_id", so.PlanID, "action", so.Action)
	return nil
}

func (s *SQLiteStore) GetSignoffs(ctx context.Context, planID string) ([]*PlanSignoff, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id, phase_id, session_id, by, action, comment, created_at
		FROM plan_signoffs
		WHERE plan_id = ?
		ORDER BY created_at`, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to get signoffs: %w", err)
	}
	defer rows.Close()

	var signoffs []*PlanSignoff
	for rows.Next() {
		var (
			id, planID, sessionID, by, action, createdAt string
			phaseID, comment                              sql.NullString
		)
		if err := rows.Scan(&id, &planID, &phaseID, &sessionID, &by, &action, &comment, &createdAt); err != nil {
			s.logger.Error("Failed to scan signoff", "error", err)
			continue
		}
		so := &PlanSignoff{
			ID:        id,
			PlanID:    planID,
			SessionID: sessionID,
			By:        by,
			Action:    action,
		}
		if phaseID.Valid {
			so.PhaseID = phaseID.String
		}
		if comment.Valid {
			so.Comment = comment.String
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			so.CreatedAt = t
		}
		signoffs = append(signoffs, so)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate signoffs: %w", err)
	}
	return signoffs, nil
}

func (s *SQLiteStore) GetRevisionCount(ctx context.Context, planID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM plan_signoffs
		WHERE plan_id = ? AND action = 'revision_requested'`, planID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get revision count: %w", err)
	}
	return count, nil
}

// ---------- Counts ----------

func (s *SQLiteStore) CountPlansBySessionAndState(ctx context.Context, sessionID string) (map[PlanState]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.state, COUNT(*) as cnt
		FROM plans p
		JOIN plan_sessions ps ON p.id = ps.plan_id
		WHERE ps.session_id = ?
		GROUP BY p.state`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to count plans by session and state: %w", err)
	}
	defer rows.Close()

	result := make(map[PlanState]int)
	for rows.Next() {
		var state string
		var cnt int
		if err := rows.Scan(&state, &cnt); err != nil {
			s.logger.Error("Failed to scan plan count", "error", err)
			continue
		}
		result[PlanState(state)] = cnt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate plan counts: %w", err)
	}
	return result, nil
}

// ---------- Scan helpers ----------

func (s *SQLiteStore) scanPlan(row *sql.Row) (*Plan, error) {
	var (
		id, title, filePath, state   string
		createdAt, updatedAt         string
		description, projectID       sql.NullString
		taskID, sourceSession        sql.NullString
		approvedAt, confirmedAt      sql.NullString
		approvedBy, confirmedBy      sql.NullString
		revisionCount                int
	)

	err := row.Scan(&id, &title, &description, &filePath, &projectID, &state, &taskID,
		&sourceSession, &approvedAt, &confirmedAt, &approvedBy, &confirmedBy,
		&revisionCount, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrPlanNotFound
		}
		return nil, err
	}

	return buildPlan(id, title, filePath, state, createdAt, updatedAt,
		description, projectID, taskID, sourceSession,
		approvedAt, confirmedAt, approvedBy, confirmedBy, revisionCount)
}

func (s *SQLiteStore) scanPlans(rows *sql.Rows) ([]*Plan, error) {
	var plans []*Plan
	for rows.Next() {
		var (
			id, title, filePath, state   string
			createdAt, updatedAt         string
			description, projectID       sql.NullString
			taskID, sourceSession        sql.NullString
			approvedAt, confirmedAt      sql.NullString
			approvedBy, confirmedBy      sql.NullString
			revisionCount                int
		)

		err := rows.Scan(&id, &title, &description, &filePath, &projectID, &state, &taskID,
			&sourceSession, &approvedAt, &confirmedAt, &approvedBy, &confirmedBy,
			&revisionCount, &createdAt, &updatedAt)
		if err != nil {
			s.logger.Error("Failed to scan plan row", "error", err)
			continue
		}

		p, err := buildPlan(id, title, filePath, state, createdAt, updatedAt,
			description, projectID, taskID, sourceSession,
			approvedAt, confirmedAt, approvedBy, confirmedBy, revisionCount)
		if err != nil {
			s.logger.Error("Failed to build plan", "error", err)
			continue
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate plans: %w", err)
	}
	return plans, nil
}

func buildPlan(id, title, filePath, state, createdAt, updatedAt string,
	description, projectID, taskID, sourceSession sql.NullString,
	approvedAt, confirmedAt, approvedBy, confirmedBy sql.NullString,
	revisionCount int) (*Plan, error) {

	p := &Plan{
		ID:            id,
		Title:         title,
		FilePath:      filePath,
		State:         PlanState(state),
		RevisionCount: revisionCount,
	}

	if description.Valid {
		p.Description = description.String
	}
	if projectID.Valid {
		p.ProjectID = projectID.String
	}
	if taskID.Valid {
		p.TaskID = taskID.String
	}
	if sourceSession.Valid {
		p.SourceSession = sourceSession.String
	}
	if approvedAt.Valid {
		if t, err := time.Parse(time.RFC3339, approvedAt.String); err == nil {
			p.ApprovedAt = &t
		}
	}
	if confirmedAt.Valid {
		if t, err := time.Parse(time.RFC3339, confirmedAt.String); err == nil {
			p.ConfirmedAt = &t
		}
	}
	if approvedBy.Valid {
		p.ApprovedBy = approvedBy.String
	}
	if confirmedBy.Valid {
		p.ConfirmedBy = confirmedBy.String
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		p.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}

	return p, nil
}

func (s *SQLiteStore) scanPhaseRow(rows *sql.Rows) (*PlanPhase, error) {
	var (
		id, planID, name, state string
		sequence, totalSteps    int
		completedSteps          int
		failedSteps             int
	)
	err := rows.Scan(&id, &planID, &name, &sequence, &totalSteps, &completedSteps, &failedSteps, &state)
	if err != nil {
		return nil, err
	}
	return &PlanPhase{
		ID:             id,
		PlanID:         planID,
		Name:           name,
		Sequence:       sequence,
		TotalSteps:     totalSteps,
		CompletedSteps: completedSteps,
		FailedSteps:    failedSteps,
		State:          PhaseState(state),
	}, nil
}

// ---------- Nullable helpers ----------

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}
