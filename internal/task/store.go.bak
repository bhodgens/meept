package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3" //nolint:revive // blank import for side effects
)

// ErrTaskNotFound is returned when a task cannot be found by ID.
var ErrTaskNotFound = errors.New("task not found")

// Store provides SQLite persistence for tasks.
type Store struct {
	db        *sql.DB
	stepStore *StepStore
	logger    *slog.Logger
}

// NewStore creates a new SQLite-backed task store.
func NewStore(dbPath string, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	// Initialize step store using the same database connection
	stepStore, err := NewStepStore(db, logger.With("component", "step-store"))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize step store: %w", err)
	}
	store.stepStore = stepStore

	logger.Info("Task store initialized", "path", dbPath)
	return store, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id             TEXT PRIMARY KEY,
		name           TEXT NOT NULL,
		description    TEXT,
		project_dir    TEXT,
		workspace_dir  TEXT,
		state          TEXT DEFAULT 'pending',
		git_repo       TEXT,
		memvid_zone    TEXT,
		metadata       TEXT,
		total_jobs     INTEGER DEFAULT 0,
		completed_jobs INTEGER DEFAULT 0,
		failed_jobs    INTEGER DEFAULT 0,
		created_at     TEXT NOT NULL,
		updated_at     TEXT NOT NULL,
		memory_refs       TEXT,
		context_query     TEXT,
		inherited_from    TEXT,
		created_memories  TEXT,
		assigned_agent    TEXT,
		token_usage       INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_state ON tasks(state);
	CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_assigned_agent ON tasks(assigned_agent);

	CREATE TABLE IF NOT EXISTS session_tasks (
		session_id TEXT NOT NULL,
		task_id    TEXT NOT NULL,
		linked_at  TEXT NOT NULL,
		PRIMARY KEY (session_id, task_id),
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_session_tasks_session ON session_tasks(session_id);
	CREATE INDEX IF NOT EXISTS idx_session_tasks_task ON session_tasks(task_id);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Add columns if they don't exist (for migrations)
	migrations := []string{
		"ALTER TABLE tasks ADD COLUMN memory_refs TEXT",
		"ALTER TABLE tasks ADD COLUMN context_query TEXT",
		"ALTER TABLE tasks ADD COLUMN inherited_from TEXT",
		"ALTER TABLE tasks ADD COLUMN created_memories TEXT",
		"ALTER TABLE tasks ADD COLUMN assigned_agent TEXT",
		"ALTER TABLE tasks ADD COLUMN token_usage INTEGER DEFAULT 0",
	}

	for _, m := range migrations {
		// Ignore errors - column may already exist
		_, _ = s.db.Exec(m)
	}

	return nil
}

// Create inserts a new task.
func (s *Store) Create(task *Task) error {
	metadataJSON := "{}"
	if len(task.Metadata) > 0 {
		metadataJSON = string(task.Metadata)
	}

	memoryRefsJSON := encodeStringSlice(task.MemoryRefs)
	createdMemoriesJSON := encodeStringSlice(task.CreatedMemories)

	_, err := s.db.Exec(`
		INSERT INTO tasks (id, name, description, project_dir, workspace_dir, state,
		                   git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
		                   failed_jobs, created_at, updated_at, memory_refs, context_query,
		                   inherited_from, created_memories, assigned_agent, token_usage)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.Name,
		nullableString(task.Description),
		nullableString(task.ProjectDir),
		nullableString(task.WorkspaceDir),
		string(task.State),
		nullableString(task.GitRepo),
		nullableString(task.MemvidZone),
		metadataJSON,
		task.TotalJobs,
		task.CompletedJobs,
		task.FailedJobs,
		task.CreatedAt.Format(time.RFC3339),
		task.UpdatedAt.Format(time.RFC3339),
		nullableString(memoryRefsJSON),
		nullableString(task.ContextQuery),
		nullableString(task.InheritedFrom),
		nullableString(createdMemoriesJSON),
		nullableString(task.AssignedAgent),
		task.TokenUsage,
	)

	if err != nil {
		s.logger.Error("Failed to create task", "id", task.ID, "error", err)
		return fmt.Errorf("failed to create task: %w", err)
	}

	s.logger.Debug("Task created", "id", task.ID, "name", task.Name)
	return nil
}

// GetByID retrieves a task by ID.
func (s *Store) GetByID(id string) (*Task, error) {
	row := s.db.QueryRow(`
		SELECT id, name, description, project_dir, workspace_dir, state,
		       git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
		       failed_jobs, created_at, updated_at, memory_refs, context_query,
		       inherited_from, created_memories, assigned_agent, token_usage
		FROM tasks WHERE id = ?`, id)

	task, err := s.scanTask(row)
	if err != nil {
		return nil, err
	}

	// Load linked sessions
	sessions, err := s.GetLinkedSessions(id)
	if err == nil {
		task.LinkedSessions = sessions
	}

	return task, nil
}

// Update updates an existing task.
func (s *Store) Update(task *Task) error {
	metadataJSON := "{}"
	if len(task.Metadata) > 0 {
		metadataJSON = string(task.Metadata)
	}

	memoryRefsJSON := encodeStringSlice(task.MemoryRefs)
	createdMemoriesJSON := encodeStringSlice(task.CreatedMemories)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE tasks
		SET name = ?, description = ?, project_dir = ?, workspace_dir = ?, state = ?,
		    git_repo = ?, memvid_zone = ?, metadata = ?, total_jobs = ?,
		    completed_jobs = ?, failed_jobs = ?, updated_at = ?,
		    memory_refs = ?, context_query = ?, inherited_from = ?,
		    created_memories = ?, assigned_agent = ?, token_usage = ?
		WHERE id = ?`,
		task.Name,
		nullableString(task.Description),
		nullableString(task.ProjectDir),
		nullableString(task.WorkspaceDir),
		string(task.State),
		nullableString(task.GitRepo),
		nullableString(task.MemvidZone),
		metadataJSON,
		task.TotalJobs,
		task.CompletedJobs,
		task.FailedJobs,
		now,
		nullableString(memoryRefsJSON),
		nullableString(task.ContextQuery),
		nullableString(task.InheritedFrom),
		nullableString(createdMemoriesJSON),
		nullableString(task.AssignedAgent),
		task.TokenUsage,
		task.ID,
	)

	if err != nil {
		s.logger.Error("Failed to update task", "id", task.ID, "error", err)
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// Delete removes a task by ID.
func (s *Store) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}

	s.logger.Info("Task deleted", "id", id)
	return nil
}

// List returns all tasks, optionally filtered by state.
func (s *Store) List(state *TaskState, limit int) ([]*Task, error) {
	var rows *sql.Rows
	var err error

	if state != nil {
		rows, err = s.db.Query(`
			SELECT id, name, description, project_dir, workspace_dir, state,
			       git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
			       failed_jobs, created_at, updated_at, memory_refs, context_query,
			       inherited_from, created_memories, assigned_agent, token_usage
			FROM tasks
			WHERE state = ?
			ORDER BY updated_at DESC
			LIMIT ?`, string(*state), limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, name, description, project_dir, workspace_dir, state,
			       git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
			       failed_jobs, created_at, updated_at, memory_refs, context_query,
			       inherited_from, created_memories, assigned_agent, token_usage
			FROM tasks
			ORDER BY updated_at DESC
			LIMIT ?`, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task, err := s.scanTaskRows(rows)
		if err != nil {
			s.logger.Error("Failed to scan task", "error", err)
			continue
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tasks: %w", err)
	}

	return tasks, nil
}

// ListActive returns all active (non-terminal) tasks.
func (s *Store) ListActive() ([]*Task, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, project_dir, workspace_dir, state,
		       git_repo, memvid_zone, metadata, total_jobs, completed_jobs,
		       failed_jobs, created_at, updated_at, memory_refs, context_query,
		       inherited_from, created_memories, assigned_agent, token_usage
		FROM tasks
		WHERE state IN ('pending', 'planning', 'executing', 'testing')
		ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task, err := s.scanTaskRows(rows)
		if err != nil {
			s.logger.Error("Failed to scan active task row", "error", err)
			continue
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate active tasks: %w", err)
	}

	return tasks, nil
}

// LinkSession links a session to a task.
func (s *Store) LinkSession(taskID, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO session_tasks (session_id, task_id, linked_at)
		VALUES (?, ?, ?)`,
		sessionID, taskID, now)

	if err != nil {
		return fmt.Errorf("failed to link session: %w", err)
	}

	s.logger.Debug("Session linked to task", "session", sessionID, "task", taskID)
	return nil
}

// UnlinkSession removes a session-task link.
func (s *Store) UnlinkSession(taskID, sessionID string) error {
	_, err := s.db.Exec(`
		DELETE FROM session_tasks WHERE session_id = ? AND task_id = ?`,
		sessionID, taskID)

	if err != nil {
		return fmt.Errorf("failed to unlink session: %w", err)
	}

	s.logger.Debug("Session unlinked from task", "session", sessionID, "task", taskID)
	return nil
}

// GetLinkedSessions returns all sessions linked to a task.
func (s *Store) GetLinkedSessions(taskID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT session_id FROM session_tasks WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get linked sessions: %w", err)
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			continue
		}
		sessions = append(sessions, sessionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to get linked sessions: %w", err)
	}

	return sessions, nil
}

// GetTasksForSession returns all tasks linked to a session.
func (s *Store) GetTasksForSession(sessionID string) ([]*Task, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.name, t.description, t.project_dir, t.workspace_dir, t.state,
		       t.git_repo, t.memvid_zone, t.metadata, t.total_jobs, t.completed_jobs,
		       t.failed_jobs, t.created_at, t.updated_at, t.memory_refs, t.context_query,
		       t.inherited_from, t.created_memories, t.assigned_agent, t.token_usage
		FROM tasks t
		JOIN session_tasks st ON t.id = st.task_id
		WHERE st.session_id = ?
		ORDER BY t.updated_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks for session: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task, err := s.scanTaskRows(rows)
		if err != nil {
			continue
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// StepStore returns the underlying step store.
func (s *Store) StepStore() *StepStore {
	return s.stepStore
}

// DB returns the underlying database connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) scanTask(row *sql.Row) (*Task, error) {
	var (
		id, name, state          string
		description, projectDir  sql.NullString
		workspaceDir, gitRepo    sql.NullString
		memvidZone, metadata     sql.NullString
		totalJobs, completedJobs int
		failedJobs, tokenUsage   int
		createdAt, updatedAt     string
		memoryRefs, contextQuery sql.NullString
		inheritedFrom            sql.NullString
		createdMemories          sql.NullString
		assignedAgent            sql.NullString
	)

	err := row.Scan(&id, &name, &description, &projectDir, &workspaceDir, &state,
		&gitRepo, &memvidZone, &metadata, &totalJobs, &completedJobs,
		&failedJobs, &createdAt, &updatedAt, &memoryRefs, &contextQuery,
		&inheritedFrom, &createdMemories, &assignedAgent, &tokenUsage)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	return s.buildTask(id, name, state, description, projectDir, workspaceDir,
		gitRepo, memvidZone, metadata, totalJobs, completedJobs, failedJobs, tokenUsage,
		createdAt, updatedAt, memoryRefs, contextQuery, inheritedFrom,
		createdMemories, assignedAgent)
}

func (s *Store) scanTaskRows(rows *sql.Rows) (*Task, error) {
	var (
		id, name, state          string
		description, projectDir  sql.NullString
		workspaceDir, gitRepo    sql.NullString
		memvidZone, metadata     sql.NullString
		totalJobs, completedJobs int
		failedJobs, tokenUsage   int
		createdAt, updatedAt     string
		memoryRefs, contextQuery sql.NullString
		inheritedFrom            sql.NullString
		createdMemories          sql.NullString
		assignedAgent            sql.NullString
	)

	err := rows.Scan(&id, &name, &description, &projectDir, &workspaceDir, &state,
		&gitRepo, &memvidZone, &metadata, &totalJobs, &completedJobs,
		&failedJobs, &createdAt, &updatedAt, &memoryRefs, &contextQuery,
		&inheritedFrom, &createdMemories, &assignedAgent, &tokenUsage)
	if err != nil {
		return nil, err
	}

	return s.buildTask(id, name, state, description, projectDir, workspaceDir,
		gitRepo, memvidZone, metadata, totalJobs, completedJobs, failedJobs, tokenUsage,
		createdAt, updatedAt, memoryRefs, contextQuery, inheritedFrom,
		createdMemories, assignedAgent)
}

func (s *Store) buildTask(id, name, state string,
	description, projectDir, workspaceDir, gitRepo, memvidZone, metadata sql.NullString,
	totalJobs, completedJobs, failedJobs, tokenUsage int,
	createdAt, updatedAt string,
	memoryRefs, contextQuery, inheritedFrom, createdMemories, assignedAgent sql.NullString) (*Task, error) {

	task := &Task{
		ID:            id,
		Name:          name,
		State:         TaskState(state),
		TotalJobs:     totalJobs,
		CompletedJobs: completedJobs,
		FailedJobs:    failedJobs,
		TokenUsage:    tokenUsage,
	}

	if description.Valid {
		task.Description = description.String
	}
	if projectDir.Valid {
		task.ProjectDir = projectDir.String
	}
	if workspaceDir.Valid {
		task.WorkspaceDir = workspaceDir.String
	}
	if gitRepo.Valid {
		task.GitRepo = gitRepo.String
	}
	if memvidZone.Valid {
		task.MemvidZone = memvidZone.String
	}
	if metadata.Valid {
		task.Metadata = json.RawMessage(metadata.String)
	}
	if memoryRefs.Valid {
		task.MemoryRefs = decodeStringSlice(memoryRefs.String)
	}
	if contextQuery.Valid {
		task.ContextQuery = contextQuery.String
	}
	if inheritedFrom.Valid {
		task.InheritedFrom = inheritedFrom.String
	}
	if createdMemories.Valid {
		task.CreatedMemories = decodeStringSlice(createdMemories.String)
	}
	if assignedAgent.Valid {
		task.AssignedAgent = assignedAgent.String
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		task.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		task.UpdatedAt = t
	}

	return task, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// encodeStringSlice encodes a string slice as JSON.
func encodeStringSlice(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	data, _ := json.Marshal(slice)
	return string(data)
}

// decodeStringSlice decodes a JSON string slice.
func decodeStringSlice(s string) []string {
	if s == "" {
		return nil
	}
	var slice []string
	if err := json.Unmarshal([]byte(s), &slice); err != nil {
		return nil
	}
	return slice
}

// RecoverStaleTasks finds all tasks in non-terminal states (pending, planning,
// executing, testing) and marks them as failed with reason "daemon_shutdown".
// It also marks any non-terminal steps belonging to those tasks as failed.
// Returns the number of tasks that were recovered.
func (s *Store) RecoverStaleTasks() (int, error) {
	// Collect IDs of stale tasks in non-terminal states.
	rows, err := s.db.Query(`
		SELECT id FROM tasks
		WHERE state IN ('pending', 'planning', 'executing', 'testing')`)
	if err != nil {
		return 0, fmt.Errorf("failed to query stale tasks: %w", err)
	}
	defer rows.Close()

	var taskIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			s.logger.Error("Failed to scan stale task ID", "error", err)
			continue
		}
		taskIDs = append(taskIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("failed to iterate stale tasks: %w", err)
	}

	if len(taskIDs) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Mark stale tasks as failed.
	_, err = s.db.Exec(`
		UPDATE tasks
		SET state = 'failed', updated_at = ?
		WHERE state IN ('pending', 'planning', 'executing', 'testing')`,
		now)
	if err != nil {
		return 0, fmt.Errorf("failed to mark stale tasks as failed: %w", err)
	}

	// Mark non-terminal steps belonging to stale tasks as failed.
	for _, taskID := range taskIDs {
		_, err := s.db.Exec(`
			UPDATE task_steps
			SET state = 'failed', result = 'daemon_shutdown', updated_at = ?
			WHERE task_id = ? AND state NOT IN ('completed', 'approved', 'failed', 'skipped', 'rejected')`,
			now, taskID)
		if err != nil {
			s.logger.Error("Failed to mark stale steps as failed", "task_id", taskID, "error", err)
			// Continue processing remaining tasks.
		}
	}

	s.logger.Info("Recovered stale tasks", "count", len(taskIDs))
	return len(taskIDs), nil
}

// Ensure TaskStore implements io.Closer
var _ io.Closer = (*Store)(nil)
