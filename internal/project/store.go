package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("project not found")

// Store provides SQLite-backed persistence for projects and worktrees.
type Store struct {
	pool   *sqlite.Pool
	logger *slog.Logger
}

// NewStore creates and initialises a project store backed by SQLite.
// dbPath is the path to the database file (parent directories are created
// automatically).
func NewStore(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create project db directory: %w", err)
	}

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 3,
		WALMode:  true,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create project db pool: %w", err)
	}

	s := &Store{pool: pool, logger: logger}
	if err := s.initSchema(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to init project schema: %w", err)
	}
	return s, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	return s.pool.WithConn(ctx, func(db *sql.DB) error {
		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS projects (
				id          TEXT PRIMARY KEY,
				name        TEXT NOT NULL,
				mode        TEXT NOT NULL,
				git_url     TEXT DEFAULT '',
				branch      TEXT DEFAULT '',
				local_path  TEXT NOT NULL,
				status      TEXT NOT NULL DEFAULT 'active',
				last_sync   TEXT DEFAULT '',
				created_at  TEXT NOT NULL,
				updated_at  TEXT NOT NULL
			)`); err != nil {
			return fmt.Errorf("create projects table: %w", err)
		}
		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS project_worktrees (
				id          TEXT PRIMARY KEY,
				project_id  TEXT NOT NULL,
				session_id  TEXT DEFAULT '',
				plan_id     TEXT DEFAULT '',
				path        TEXT NOT NULL,
				branch      TEXT NOT NULL,
				status      TEXT NOT NULL DEFAULT 'active',
				created_at  TEXT NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
			)`); err != nil {
			return fmt.Errorf("create project_worktrees table: %w", err)
		}
		if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_worktrees_project ON project_worktrees(project_id)`); err != nil {
			return fmt.Errorf("create worktrees index: %w", err)
		}
		if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_worktrees_session ON project_worktrees(session_id)`); err != nil {
			return fmt.Errorf("create worktrees session index: %w", err)
		}
		// project_recents table for /project typeahead recents
		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS project_recents (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				project_path TEXT UNIQUE NOT NULL,
				last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`); err != nil {
			return fmt.Errorf("create project_recents table: %w", err)
		}
		if _, err := db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_recents_last_used
			ON project_recents(last_used_at DESC)`); err != nil {
			return fmt.Errorf("create project_recents index: %w", err)
		}
		return nil
	})
}

// Close releases all database resources.
func (s *Store) Close() error {
	if s.pool != nil {
		return s.pool.Close()
	}
	return nil
}

// Pool returns the underlying SQLite connection pool.
// Callers should use pool.Get(ctx) to obtain a *sql.DB,
// pass it to the desired operation, then pool.Put(db) to return it.
func (s *Store) Pool() *sqlite.Pool {
	return s.pool
}

// ---------- Project CRUD ----------

// CreateProject inserts a new project record.
func (s *Store) CreateProject(ctx context.Context, p *Project) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.Status == "" {
		p.Status = "active"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO projects (id, name, mode, git_url, branch, local_path, status, last_sync, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, string(p.Mode), p.GitURL, p.Branch, p.LocalPath, p.Status,
		formatTime(p.LastSync), formatTime(p.CreatedAt), formatTime(p.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

// GetProject retrieves a project by ID.
func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	row, err := s.pool.QueryRow(ctx,
		`SELECT id, name, mode, git_url, branch, local_path, status, last_sync, created_at, updated_at
		 FROM projects WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return scanProject(row)
}

// ListProjects returns all projects.
func (s *Store) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, mode, git_url, branch, local_path, status, last_sync, created_at, updated_at
		 FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p, err := scanProjectFromRows(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// UpdateProject updates an existing project.
func (s *Store) UpdateProject(ctx context.Context, p *Project) error {
	p.UpdatedAt = time.Now().UTC()
	res, err := s.pool.Exec(ctx,
		`UPDATE projects SET name=?, mode=?, git_url=?, branch=?, local_path=?, status=?, last_sync=?, updated_at=?
		 WHERE id=?`,
		p.Name, string(p.Mode), p.GitURL, p.Branch, p.LocalPath, p.Status,
		formatTime(p.LastSync), formatTime(p.UpdatedAt), p.ID,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project by ID.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetProjectByPath looks up a project by its local_path.
func (s *Store) GetProjectByPath(ctx context.Context, localPath string) (*Project, error) {
	row, err := s.pool.QueryRow(ctx,
		`SELECT id, name, mode, git_url, branch, local_path, status, last_sync, created_at, updated_at
		 FROM projects WHERE local_path = ?`, localPath)
	if err != nil {
		return nil, err
	}
	return scanProject(row)
}

// ---------- Worktree CRUD ----------

// CreateWorktree inserts a new worktree record.
func (s *Store) CreateWorktree(ctx context.Context, w *Worktree) error {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	if w.Status == "" {
		w.Status = "active"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO project_worktrees (id, project_id, session_id, plan_id, path, branch, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ProjectID, w.SessionID, w.PlanID, w.Path, w.Branch, w.Status,
		formatTime(w.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	return nil
}

// GetWorktree retrieves a worktree by ID.
func (s *Store) GetWorktree(ctx context.Context, id string) (*Worktree, error) {
	row, err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, session_id, plan_id, path, branch, status, created_at
		 FROM project_worktrees WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return scanWorktree(row)
}

// GetActiveWorktreeBySession returns the active worktree for a session, if any.
func (s *Store) GetActiveWorktreeBySession(ctx context.Context, sessionID string) (*Worktree, error) {
	row, err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, session_id, plan_id, path, branch, status, created_at
		 FROM project_worktrees WHERE session_id = ? AND status = 'active'`, sessionID)
	if err != nil {
		return nil, err
	}
	return scanWorktree(row)
}

// ListWorktreesByProject returns all worktrees for a project.
func (s *Store) ListWorktreesByProject(ctx context.Context, projectID string) ([]*Worktree, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, session_id, plan_id, path, branch, status, created_at
		 FROM project_worktrees WHERE project_id = ? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var worktrees []*Worktree
	for rows.Next() {
		w, err := scanWorktreeFromRows(rows)
		if err != nil {
			return nil, err
		}
		worktrees = append(worktrees, w)
	}
	return worktrees, rows.Err()
}

// UpdateWorktree updates an existing worktree record.
func (s *Store) UpdateWorktree(ctx context.Context, w *Worktree) error {
	res, err := s.pool.Exec(ctx,
		`UPDATE project_worktrees SET session_id=?, plan_id=?, path=?, branch=?, status=?
		 WHERE id=?`,
		w.SessionID, w.PlanID, w.Path, w.Branch, w.Status, w.ID,
	)
	if err != nil {
		return fmt.Errorf("update worktree: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWorktree removes a worktree by ID.
func (s *Store) DeleteWorktree(ctx context.Context, id string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM project_worktrees WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupOrphanedWorktrees marks worktrees with status "active" that have no
// associated session as "cleaned".
func (s *Store) CleanupOrphanedWorktrees(ctx context.Context) (int, error) {
	res, err := s.pool.Exec(ctx,
		`UPDATE project_worktrees SET status = 'cleaned' WHERE status = 'active' AND (session_id = '' OR session_id IS NULL) AND (plan_id = '' OR plan_id IS NULL)`)
	if err != nil {
		return 0, fmt.Errorf("cleanup orphaned worktrees: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// ---------- helpers ----------

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func scanProject(row *sql.Row) (*Project, error) {
	var p Project
	var lastSync, createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.Mode, &p.GitURL, &p.Branch, &p.LocalPath,
		&p.Status, &lastSync, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.LastSync = parseTime(lastSync)
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

func scanProjectFromRows(rows sqlite.Rows) (*Project, error) {
	var p Project
	var lastSync, createdAt, updatedAt string
	err := rows.Scan(&p.ID, &p.Name, &p.Mode, &p.GitURL, &p.Branch, &p.LocalPath,
		&p.Status, &lastSync, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	p.LastSync = parseTime(lastSync)
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

func scanWorktree(row *sql.Row) (*Worktree, error) {
	var w Worktree
	var createdAt string
	err := row.Scan(&w.ID, &w.ProjectID, &w.SessionID, &w.PlanID, &w.Path,
		&w.Branch, &w.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.CreatedAt = parseTime(createdAt)
	return &w, nil
}

func scanWorktreeFromRows(rows sqlite.Rows) (*Worktree, error) {
	var w Worktree
	var createdAt string
	err := rows.Scan(&w.ID, &w.ProjectID, &w.SessionID, &w.PlanID, &w.Path,
		&w.Branch, &w.Status, &createdAt)
	if err != nil {
		return nil, err
	}
	w.CreatedAt = parseTime(createdAt)
	return &w, nil
}
