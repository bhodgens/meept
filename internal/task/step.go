package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// StepState represents the state of a task step.
type StepState string

const (
	StepPending   StepState = "pending"
	StepReady     StepState = "ready"
	StepScheduled StepState = "scheduled"
	StepRunning   StepState = "running"
	StepReviewing StepState = "reviewing"
	StepApproved  StepState = "approved"
	StepRejected  StepState = "rejected"
	StepCompleted StepState = "completed"
	StepFailed    StepState = "failed"
	StepSkipped   StepState = "skipped"
)

func (s StepState) String() string {
	return string(s)
}

// IsTerminal returns true if the step is in a terminal state.
// Approved steps are also terminal - they're ready to be treated as completed.
func (s StepState) IsTerminal() bool {
	return s == StepCompleted || s == StepApproved || s == StepFailed || s == StepSkipped
}

// TaskStep represents a single step within a task's execution plan.
type TaskStep struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	Description   string    `json:"description"`
	DependsOn     []string  `json:"depends_on,omitempty"`
	ToolHint      string    `json:"tool_hint,omitempty"`
	AgentID       string    `json:"agent_id,omitempty"`
	JobID         string    `json:"job_id,omitempty"`
	State         StepState `json:"state"`
	Result        string    `json:"result,omitempty"`
	Sequence      int       `json:"sequence"`
	RevisionCount int       `json:"revision_count"` // Number of revision cycles
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NewTaskStep creates a new task step.
func NewTaskStep(taskID, description string, sequence int) *TaskStep {
	now := time.Now().UTC()
	return &TaskStep{
		ID:          fmt.Sprintf("step-%s-%d-%d", taskID, sequence, now.UnixNano()),
		TaskID:      taskID,
		Description: description,
		State:       StepPending,
		Sequence:    sequence,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// WithDependsOn sets step dependencies.
func (s *TaskStep) WithDependsOn(deps []string) *TaskStep {
	s.DependsOn = deps
	return s
}

// WithToolHint sets the tool hint for agent selection.
func (s *TaskStep) WithToolHint(hint string) *TaskStep {
	s.ToolHint = hint
	return s
}

// WithAgentID sets the assigned agent.
func (s *TaskStep) WithAgentID(agentID string) *TaskStep {
	s.AgentID = agentID
	return s
}

// IncrementRevision increments the revision count.
func (s *TaskStep) IncrementRevision() {
	s.RevisionCount++
}

// CreateRevision creates a new revision step based on this step.
func CreateRevision(original *TaskStep, feedback string) *TaskStep {
	now := time.Now().UTC()
	revision := &TaskStep{
		ID:            fmt.Sprintf("step-%s-rev-%d-%d", original.TaskID, original.Sequence+1000+original.RevisionCount, now.UnixNano()),
		TaskID:        original.TaskID,
		Description:   fmt.Sprintf("[REVISION] %s\n\nFeedback: %s", original.Description, feedback),
		ToolHint:      original.ToolHint,
		State:         StepPending,
		Sequence:      original.Sequence + 1000 + original.RevisionCount,
		RevisionCount: original.RevisionCount + 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// The revision depends on the original step
	if original.DependsOn == nil {
		revision.DependsOn = []string{original.ID}
	} else {
		revision.DependsOn = append(original.DependsOn, original.ID)
	}
	return revision
}

// StepStore provides SQLite persistence for task steps.
type StepStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewStepStore creates a new step store using an existing database connection.
func NewStepStore(db *sql.DB, logger *slog.Logger) (*StepStore, error) {
	if logger == nil {
		logger = slog.Default()
	}

	store := &StepStore{
		db:     db,
		logger: logger,
	}

	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate step store: %w", err)
	}

	logger.Info("Step store initialized")
	return store, nil
}

func (s *StepStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS task_steps (
		id          TEXT PRIMARY KEY,
		task_id     TEXT NOT NULL,
		description TEXT NOT NULL,
		depends_on  TEXT,
		tool_hint   TEXT,
		agent_id    TEXT,
		job_id      TEXT,
		state       TEXT DEFAULT 'pending',
		result      TEXT,
		sequence    INTEGER DEFAULT 0,
		created_at  TEXT NOT NULL,
		updated_at  TEXT NOT NULL,
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_task_steps_task_id ON task_steps(task_id);
	CREATE INDEX IF NOT EXISTS idx_task_steps_state ON task_steps(state);
	CREATE INDEX IF NOT EXISTS idx_task_steps_job_id ON task_steps(job_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Create inserts a new task step.
func (s *StepStore) Create(step *TaskStep) error {
	depsJSON := encodeStringSlice(step.DependsOn)

	_, err := s.db.Exec(`
		INSERT INTO task_steps (id, task_id, description, depends_on, tool_hint, agent_id,
		                        job_id, state, result, sequence, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID,
		step.TaskID,
		step.Description,
		nullableString(depsJSON),
		nullableString(step.ToolHint),
		nullableString(step.AgentID),
		nullableString(step.JobID),
		string(step.State),
		nullableString(step.Result),
		step.Sequence,
		step.CreatedAt.Format(time.RFC3339),
		step.UpdatedAt.Format(time.RFC3339),
	)

	if err != nil {
		s.logger.Error("Failed to create step", "id", step.ID, "error", err)
		return fmt.Errorf("failed to create step: %w", err)
	}

	s.logger.Debug("Step created", "id", step.ID, "task_id", step.TaskID, "sequence", step.Sequence)
	return nil
}

// Update updates an existing task step.
func (s *StepStore) Update(step *TaskStep) error {
	depsJSON := encodeStringSlice(step.DependsOn)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.Exec(`
		UPDATE task_steps
		SET description = ?, depends_on = ?, tool_hint = ?, agent_id = ?,
		    job_id = ?, state = ?, result = ?, sequence = ?, updated_at = ?
		WHERE id = ?`,
		step.Description,
		nullableString(depsJSON),
		nullableString(step.ToolHint),
		nullableString(step.AgentID),
		nullableString(step.JobID),
		string(step.State),
		nullableString(step.Result),
		step.Sequence,
		now,
		step.ID,
	)

	if err != nil {
		s.logger.Error("Failed to update step", "id", step.ID, "error", err)
		return fmt.Errorf("failed to update step: %w", err)
	}

	return nil
}

// GetByID retrieves a step by ID.
func (s *StepStore) GetByID(id string) (*TaskStep, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, created_at, updated_at
		FROM task_steps WHERE id = ?`, id)

	return s.scanStep(row)
}

// GetByJobID retrieves a step by its associated job ID.
func (s *StepStore) GetByJobID(jobID string) (*TaskStep, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, created_at, updated_at
		FROM task_steps WHERE job_id = ?`, jobID)

	return s.scanStep(row)
}

// ListByTaskID returns all steps for a task, ordered by sequence.
func (s *StepStore) ListByTaskID(taskID string) ([]*TaskStep, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, created_at, updated_at
		FROM task_steps
		WHERE task_id = ?
		ORDER BY sequence ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list steps: %w", err)
	}
	defer rows.Close()

	var steps []*TaskStep
	for rows.Next() {
		step, err := s.scanStepRows(rows)
		if err != nil {
			s.logger.Error("Failed to scan step", "error", err)
			continue
		}
		steps = append(steps, step)
	}

	return steps, nil
}

// GetReadySteps returns steps that are ready to execute (all dependencies completed).
func (s *StepStore) GetReadySteps(taskID string) ([]*TaskStep, error) {
	// Load all steps for the task
	allSteps, err := s.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	// Build state map
	stateMap := make(map[string]StepState)
	for _, step := range allSteps {
		stateMap[step.ID] = step.State
	}

	// Find steps where state is "ready"
	var ready []*TaskStep
	for _, step := range allSteps {
		if step.State == StepReady {
			ready = append(ready, step)
		}
	}

	return ready, nil
}

// PromoteReadySteps moves pending steps to ready if all their dependencies are completed.
// Returns the list of newly promoted steps.
func (s *StepStore) PromoteReadySteps(taskID string) ([]*TaskStep, error) {
	allSteps, err := s.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	// Build state map
	stateMap := make(map[string]StepState)
	for _, step := range allSteps {
		stateMap[step.ID] = step.State
	}

	var promoted []*TaskStep
	for _, step := range allSteps {
		if step.State != StepPending {
			continue
		}

		// Check if all dependencies are completed
		allDepsCompleted := true
		for _, depID := range step.DependsOn {
			depState, ok := stateMap[depID]
			if !ok || !depState.IsTerminal() || depState == StepFailed {
				allDepsCompleted = false
				break
			}
		}

		if allDepsCompleted {
			step.State = StepReady
			step.UpdatedAt = time.Now().UTC()
			if err := s.SetState(step.ID, StepReady); err != nil {
				s.logger.Error("Failed to promote step to ready", "id", step.ID, "error", err)
				continue
			}
			promoted = append(promoted, step)
		}
	}

	return promoted, nil
}

// SetState updates a step's state.
func (s *StepStore) SetState(id string, state StepState) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), now, id)
	if err != nil {
		return fmt.Errorf("failed to set step state: %w", err)
	}
	return nil
}

// SetJobID sets the job ID for a step.
func (s *StepStore) SetJobID(id, jobID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET job_id = ?, updated_at = ? WHERE id = ?`,
		jobID, now, id)
	if err != nil {
		return fmt.Errorf("failed to set step job_id: %w", err)
	}
	return nil
}

// SetAgentID sets the assigned agent ID for a step.
func (s *StepStore) SetAgentID(id, agentID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET agent_id = ?, updated_at = ? WHERE id = ?`,
		agentID, now, id)
	if err != nil {
		return fmt.Errorf("failed to set step agent_id: %w", err)
	}
	return nil
}

// SetResult sets the result for a step.
func (s *StepStore) SetResult(id, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET result = ?, updated_at = ? WHERE id = ?`,
		result, now, id)
	if err != nil {
		return fmt.Errorf("failed to set step result: %w", err)
	}
	return nil
}

// AreAllCompleted returns true if all steps for a task are in a terminal state
// and none have failed.
func (s *StepStore) AreAllCompleted(taskID string) (bool, error) {
	steps, err := s.ListByTaskID(taskID)
	if err != nil {
		return false, err
	}

	if len(steps) == 0 {
		return false, nil
	}

	for _, step := range steps {
		if !step.State.IsTerminal() {
			return false, nil
		}
		if step.State == StepFailed {
			return false, nil
		}
	}

	return true, nil
}

// HasFailures returns true if any step for a task has failed.
func (s *StepStore) HasFailures(taskID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM task_steps WHERE task_id = ? AND state = ?`,
		taskID, string(StepFailed)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check failures: %w", err)
	}
	return count > 0, nil
}

// CountByState returns a count of steps grouped by state for a task.
func (s *StepStore) CountByState(taskID string) (map[StepState]int, error) {
	rows, err := s.db.Query(`
		SELECT state, COUNT(*) FROM task_steps WHERE task_id = ? GROUP BY state`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to count steps by state: %w", err)
	}
	defer rows.Close()

	counts := make(map[StepState]int)
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			continue
		}
		counts[StepState(state)] = count
	}

	return counts, nil
}

// DeleteByTaskID removes all steps for a task.
func (s *StepStore) DeleteByTaskID(taskID string) error {
	_, err := s.db.Exec("DELETE FROM task_steps WHERE task_id = ?", taskID)
	if err != nil {
		return fmt.Errorf("failed to delete steps: %w", err)
	}
	return nil
}

func (s *StepStore) scanStep(row *sql.Row) (*TaskStep, error) {
	var (
		id, taskID, description, state string
		dependsOn, toolHint            sql.NullString
		agentID, jobID, result         sql.NullString
		sequence                       int
		createdAt, updatedAt           string
	)

	err := row.Scan(&id, &taskID, &description, &dependsOn, &toolHint, &agentID,
		&jobID, &state, &result, &sequence, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return buildStep(id, taskID, description, state, dependsOn, toolHint,
		agentID, jobID, result, sequence, createdAt, updatedAt), nil
}

func (s *StepStore) scanStepRows(rows *sql.Rows) (*TaskStep, error) {
	var (
		id, taskID, description, state string
		dependsOn, toolHint            sql.NullString
		agentID, jobID, result         sql.NullString
		sequence                       int
		createdAt, updatedAt           string
	)

	err := rows.Scan(&id, &taskID, &description, &dependsOn, &toolHint, &agentID,
		&jobID, &state, &result, &sequence, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	return buildStep(id, taskID, description, state, dependsOn, toolHint,
		agentID, jobID, result, sequence, createdAt, updatedAt), nil
}

func buildStep(id, taskID, description, state string,
	dependsOn, toolHint, agentID, jobID, result sql.NullString,
	sequence int, createdAt, updatedAt string) *TaskStep {

	step := &TaskStep{
		ID:          id,
		TaskID:      taskID,
		Description: description,
		State:       StepState(state),
		Sequence:    sequence,
	}

	if dependsOn.Valid {
		step.DependsOn = decodeStringSlice(dependsOn.String)
	}
	if toolHint.Valid {
		step.ToolHint = toolHint.String
	}
	if agentID.Valid {
		step.AgentID = agentID.String
	}
	if jobID.Valid {
		step.JobID = jobID.String
	}
	if result.Valid {
		step.Result = result.String
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		step.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		step.UpdatedAt = t
	}

	return step
}

// encodeStepDependsOn encodes step dependencies as JSON.
func encodeStepDependsOn(deps []string) string {
	if len(deps) == 0 {
		return ""
	}
	data, _ := json.Marshal(deps)
	return string(data)
}
