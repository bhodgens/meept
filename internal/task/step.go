package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// ErrStepNotFound is returned when a step cannot be found by ID.
var ErrStepNotFound = errors.New("step not found")

// CategorizedRecommendation represents a recommendation from an agent.
type CategorizedRecommendation struct {
	Category    string  `json:"category"` // "security", "performance", "maintainability", "follow-up"
	Priority    string  `json:"priority"` // "critical", "high", "medium", "low"
	Description string  `json:"description"`
	AgentID     string  `json:"agent_id"`
	Confidence  float64 `json:"confidence"`
}

// ChecklistItem represents a single checkbox item in a checklist.
type ChecklistItem struct {
	Text      string     `json:"text"`
	Completed bool       `json:"completed"`
	CreatedAt time.Time  `json:"created_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Checklist tracks explicit checkbox items for a task step.
type Checklist struct {
	Items []ChecklistItem `json:"items"`
}

// AddItem adds a new checklist item.
func (c *Checklist) AddItem(text string) {
	if c == nil {
		return
	}
	c.Items = append(c.Items, ChecklistItem{
		Text:      text,
		Completed: false,
		CreatedAt: time.Now().UTC(),
	})
}

// CompleteItem marks an item as completed by its text.
func (c *Checklist) CompleteItem(text string) bool {
	if c == nil {
		return false
	}
	now := time.Now().UTC()
	for i := range c.Items {
		if c.Items[i].Text == text && !c.Items[i].Completed {
			c.Items[i].Completed = true
			c.Items[i].CompletedAt = &now
			return true
		}
	}
	return false
}

// Remaining returns the number of incomplete items.
func (c *Checklist) Remaining() int {
	if c == nil {
		return 0
	}
	count := 0
	for _, item := range c.Items {
		if !item.Completed {
			count++
		}
	}
	return count
}

// IsComplete returns true if all items are completed (or checklist is nil/empty).
func (c *Checklist) IsComplete() bool {
	if c == nil || len(c.Items) == 0 {
		return true
	}
	for _, item := range c.Items {
		if !item.Completed {
			return false
		}
	}
	return true
}

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
// Terminal states indicate no more direct work will be done on this step:
// - Completed/Approved: work succeeded
// - Failed/Skipped: work cannot proceed
// - Rejected: work finished but failed review; a revision step handles the redo
func (s StepState) IsTerminal() bool {
	return s == StepCompleted || s == StepApproved || s == StepFailed || s == StepSkipped || s == StepRejected
}

// IsSuccessfullyTerminal returns true if the step completed successfully.
// Used for task completion checks where rejected steps need revisions.
func (s StepState) IsSuccessfullyTerminal() bool {
	return s == StepCompleted || s == StepApproved
}

// TaskStep represents a single step within a task's execution plan.
//
//nolint:revive // stutter with package name is intentional for API clarity
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
	// Recommendations holds categorized recommendations from the agent
	Recommendations []CategorizedRecommendation `json:"recommendations,omitempty"`
	// Evidence collected during step execution (file hashes, exit codes, etc.)
	Evidence []models.Evidence `json:"evidence,omitempty"`
	// Claims made by the agent about what was accomplished
	Claims []string `json:"claims,omitempty"`
	// Validated indicates whether evidence has been verified
	Validated bool `json:"validated"`
	// ValidationError contains the reason validation failed
	ValidationError string `json:"validation_error,omitempty"`
	// TokenUsage tracks tokens consumed by this step.
	TokenUsage int `json:"token_usage,omitempty"`
	// MemoryRefs are memory IDs inherited from parent task or accumulated from prior steps.
	MemoryRefs []string `json:"memory_refs,omitempty"`
	// AccumulatedContext contains evidence/outputs from prior steps.
	AccumulatedContext string `json:"accumulated_context,omitempty"`
	// ValidationRetryCount tracks how many times this step has been re-queued for validation retry.
	ValidationRetryCount int       `json:"validation_retry_count,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	// ModelOverride specifies a model to use for this step (overrides agent default)
	ModelOverride string `json:"model_override,omitempty"`
	// Checklist tracks explicit checkbox items for this step
	Checklist *Checklist `json:"checklist,omitempty"`
	// Phase identifies which phase/milestone this step belongs to
	Phase string `json:"phase,omitempty"`
	// CheckpointGate indicates if this step is a phase gate requiring validation
	CheckpointGate bool `json:"checkpoint_gate"`
	// IsHandoff indicates this step was created by the agent handoff system
	IsHandoff bool `json:"is_handoff,omitempty"`
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

// AddTokenUsage adds tokens to the step's running total.
func (s *TaskStep) AddTokenUsage(tokens int) {
	s.TokenUsage += tokens
	s.UpdatedAt = time.Now().UTC()
}

// AddMemoryRef adds a memory reference to the step.
func (s *TaskStep) AddMemoryRef(ref string) {
	if slices.Contains(s.MemoryRefs, ref) {
		return // Already exists
	}
	s.MemoryRefs = append(s.MemoryRefs, ref)
	s.UpdatedAt = time.Now().UTC()
}

// AppendToContext appends content to the accumulated context.
func (s *TaskStep) AppendToContext(content string) {
	if s.AccumulatedContext == "" {
		s.AccumulatedContext = content
	} else {
		s.AccumulatedContext += "\n\n---\n\n" + content
	}
	s.UpdatedAt = time.Now().UTC()
}

// IncrementRevision increments the revision count.
func (s *TaskStep) IncrementRevision() {
	s.RevisionCount++
}

// CreateRevisionWithContext creates a revision step with additional context
// from the review feedback. The revisionContext is prepended to the step's
// AccumulatedContext so the coder agent sees prior rejection feedback.
func CreateRevisionWithContext(original *TaskStep, feedback string, revisionContext string) *TaskStep {
	revision := CreateRevision(original, feedback)
	if revisionContext != "" {
		revision.AccumulatedContext = revisionContext
	}
	return revision
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
		revision.DependsOn = append(revision.DependsOn, original.DependsOn...)
		revision.DependsOn = append(revision.DependsOn, original.ID)
	}
	return revision
}

// StepStore provides SQLite persistence for task steps.
type StepStore struct {
	db             *sql.DB
	logger         *slog.Logger
	logTransitions bool // Enable transition logging
}

// StateTransition represents a state transition for a task step.
type StateTransition struct {
	ID        int64     `json:"id"`
	StepID    string    `json:"step_id"`
	FromState StepState `json:"from_state"`
	ToState   StepState `json:"to_state"`
	Reason    string    `json:"reason"`
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
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
		id             TEXT PRIMARY KEY,
		task_id        TEXT NOT NULL,
		description    TEXT NOT NULL,
		depends_on     TEXT,
		tool_hint      TEXT,
		agent_id       TEXT,
		job_id         TEXT,
		state          TEXT DEFAULT 'pending',
		result         TEXT,
		sequence       INTEGER DEFAULT 0,
		revision_count INTEGER DEFAULT 0,
		recommendations TEXT,
		evidence       TEXT,
		claims         TEXT,
		validated      BOOLEAN DEFAULT FALSE,
		validation_error TEXT,
		token_usage    INTEGER DEFAULT 0,
		memory_refs    TEXT,
		accumulated_context TEXT,
		model_override TEXT,
		checklist      TEXT,
		phase          TEXT,
		checkpoint_gate BOOLEAN DEFAULT FALSE,
		is_handoff     BOOLEAN DEFAULT FALSE,
		created_at     TEXT NOT NULL,
		updated_at     TEXT NOT NULL,
		FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS task_state_transitions (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		step_id     TEXT NOT NULL,
		from_state  TEXT NOT NULL,
		to_state    TEXT NOT NULL,
		reason      TEXT,
		agent_id    TEXT,
		timestamp   DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (step_id) REFERENCES task_steps(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_task_steps_task_id ON task_steps(task_id);
	CREATE INDEX IF NOT EXISTS idx_task_steps_state ON task_steps(state);
	CREATE INDEX IF NOT EXISTS idx_task_steps_job_id ON task_steps(job_id);
	CREATE INDEX IF NOT EXISTS idx_task_state_transitions_step_id ON task_state_transitions(step_id);
	CREATE INDEX IF NOT EXISTS idx_task_state_transitions_timestamp ON task_state_transitions(timestamp DESC);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Post-migration: add columns for existing databases (ignore errors if column exists)
	for _, col := range []string{
		"ALTER TABLE task_steps ADD COLUMN revision_count INTEGER DEFAULT 0",
		"ALTER TABLE task_steps ADD COLUMN recommendations TEXT",
		"ALTER TABLE task_steps ADD COLUMN evidence TEXT",
		"ALTER TABLE task_steps ADD COLUMN claims TEXT",
		"ALTER TABLE task_steps ADD COLUMN validated BOOLEAN DEFAULT FALSE",
		"ALTER TABLE task_steps ADD COLUMN validation_error TEXT",
		"ALTER TABLE task_steps ADD COLUMN token_usage INTEGER DEFAULT 0",
		"ALTER TABLE task_steps ADD COLUMN memory_refs TEXT",
		"ALTER TABLE task_steps ADD COLUMN accumulated_context TEXT",
		"ALTER TABLE task_steps ADD COLUMN model_override TEXT",
		"ALTER TABLE task_steps ADD COLUMN checklist TEXT",
		"ALTER TABLE task_steps ADD COLUMN phase TEXT",
		"ALTER TABLE task_steps ADD COLUMN checkpoint_gate BOOLEAN DEFAULT FALSE",
		"ALTER TABLE task_steps ADD COLUMN is_handoff BOOLEAN DEFAULT FALSE",
	} {
		_, _ = s.db.Exec(col)
	}

	return nil
}

// Create inserts a new task step.
func (s *StepStore) Create(step *TaskStep) error {
	depsJSON := encodeStringSlice(step.DependsOn)
	recsJSON := encodeRecommendations(step.Recommendations)
	evidenceJSON := encodeEvidenceSlice(step.Evidence)
	claimsJSON := encodeStringSlice(step.Claims)
	memoryRefsJSON := encodeStringSlice(step.MemoryRefs)
	checklistJSON := encodeChecklist(step.Checklist)

	_, err := s.db.Exec(`
		INSERT INTO task_steps (id, task_id, description, depends_on, tool_hint, agent_id,
		                        job_id, state, result, sequence, revision_count,
		                        recommendations, evidence, claims, validated, validation_error,
		                        token_usage, memory_refs, accumulated_context, model_override,
		                        checklist, phase, checkpoint_gate, is_handoff,
		                        created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		step.RevisionCount,
		nullableString(recsJSON),
		nullableString(evidenceJSON),
		nullableString(claimsJSON),
		step.Validated,
		nullableString(step.ValidationError),
		step.TokenUsage,
		nullableString(memoryRefsJSON),
		nullableString(step.AccumulatedContext),
		nullableString(step.ModelOverride),
		nullableString(checklistJSON),
		nullableString(step.Phase),
		step.CheckpointGate,
		step.IsHandoff,
		step.CreatedAt.Format(time.RFC3339),
		step.UpdatedAt.Format(time.RFC3339),
	)

	if err != nil {
		s.logger.Error("Failed to create step", "id", step.ID, "error", err)
		return fmt.Errorf("failed to create step: %w", err)
	}

	s.logger.Debug("Step created", "id", step.ID, KeyTaskID, step.TaskID, "sequence", step.Sequence)
	return nil
}

// Update updates an existing task step and records state transitions.
// The SELECT, UPDATE, and state-transition INSERT are wrapped in a single
// transaction (BEGIN IMMEDIATE) so that concurrent updates serialize and the
// recorded FromState is always correct.
func (s *StepStore) Update(step *TaskStep) error {
	ctx := context.Background()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction for step update %s: %w", step.ID, err)
	}
	// Safe to call after Commit; a no-op on a committed transaction.
	defer tx.Rollback()

	// Fetch current state for transition recording (inside the transaction).
	var oldState StepState
	row := tx.QueryRowContext(ctx, "SELECT state FROM task_steps WHERE id = ?", step.ID)
	if err := row.Scan(&oldState); err != nil {
		return fmt.Errorf("failed to get current state for step %s: %w", step.ID, err)
	}

	depsJSON := encodeStringSlice(step.DependsOn)
	recsJSON := encodeRecommendations(step.Recommendations)
	evidenceJSON := encodeEvidenceSlice(step.Evidence)
	claimsJSON := encodeStringSlice(step.Claims)
	memoryRefsJSON := encodeStringSlice(step.MemoryRefs)
	checklistJSON := encodeChecklist(step.Checklist)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = tx.ExecContext(ctx, `
		UPDATE task_steps
		SET description = ?, depends_on = ?, tool_hint = ?, agent_id = ?,
		    job_id = ?, state = ?, result = ?, sequence = ?, revision_count = ?,
		    recommendations = ?, evidence = ?, claims = ?, validated = ?,
		    validation_error = ?, token_usage = ?, memory_refs = ?, accumulated_context = ?,
		    model_override = ?, checklist = ?, phase = ?, checkpoint_gate = ?, is_handoff = ?, updated_at = ?
		WHERE id = ?`,
		step.Description,
		nullableString(depsJSON),
		nullableString(step.ToolHint),
		nullableString(step.AgentID),
		nullableString(step.JobID),
		string(step.State),
		nullableString(step.Result),
		step.Sequence,
		step.RevisionCount,
		nullableString(recsJSON),
		nullableString(evidenceJSON),
		nullableString(claimsJSON),
		step.Validated,
		nullableString(step.ValidationError),
		step.TokenUsage,
		nullableString(memoryRefsJSON),
		nullableString(step.AccumulatedContext),
		nullableString(step.ModelOverride),
		nullableString(checklistJSON),
		nullableString(step.Phase),
		step.CheckpointGate,
		step.IsHandoff,
		now,
		step.ID,
	)

	if err != nil {
		s.logger.Error("Failed to update step", "id", step.ID, "error", err)
		return fmt.Errorf("failed to update step: %w", err)
	}

	// Record transition when state actually changed (inside the same transaction).
	if oldState != step.State {
		_, tErr := tx.ExecContext(ctx, `
			INSERT INTO task_state_transitions (step_id, from_state, to_state, reason, agent_id, timestamp)
			VALUES (?, ?, ?, ?, ?, ?)`,
			step.ID,
			string(oldState),
			string(step.State),
			nullableString("update"),
			nullableString(""),
			time.Now().UTC().Format(time.RFC3339),
		)
		if tErr != nil {
			s.logger.Warn("Failed to record state transition", "step_id", step.ID, "error", tErr)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit step update %s: %w", step.ID, err)
	}
	return nil
}

// GetByID retrieves a step by ID.
func (s *StepStore) GetByID(id string) (*TaskStep, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, revision_count,
		       recommendations, evidence, claims, validated, validation_error,
		       token_usage, memory_refs, accumulated_context, model_override,
		       checklist, phase, checkpoint_gate, is_handoff,
		       created_at, updated_at
		FROM task_steps WHERE id = ?`, id)

	return s.scanStep(row)
}

// GetByJobID retrieves a step by its associated job ID.
func (s *StepStore) GetByJobID(jobID string) (*TaskStep, error) {
	row := s.db.QueryRow(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, revision_count,
		       recommendations, evidence, claims, validated, validation_error,
		       token_usage, memory_refs, accumulated_context, model_override,
		       checklist, phase, checkpoint_gate, is_handoff,
		       created_at, updated_at
		FROM task_steps WHERE job_id = ?`, jobID)

	return s.scanStep(row)
}

// ListByTaskID returns all steps for a task, ordered by sequence.
func (s *StepStore) ListByTaskID(taskID string) ([]*TaskStep, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, revision_count,
		       recommendations, evidence, claims, validated, validation_error,
		       token_usage, memory_refs, accumulated_context, model_override,
		       checklist, phase, checkpoint_gate, is_handoff,
		       created_at, updated_at
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

// SetState updates a step's state and records the transition.
func (s *StepStore) SetState(id string, state StepState) error {
	// Fetch current state for transition recording
	var oldState StepState
	row := s.db.QueryRow("SELECT state FROM task_steps WHERE id = ?", id)
	if err := row.Scan(&oldState); err != nil {
		return fmt.Errorf("failed to get current state for step %s: %w", id, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), now, id)
	if err != nil {
		return fmt.Errorf("failed to set step state: %w", err)
	}

	// Record transition when state actually changed
	if oldState != state {
		if err := s.RecordTransition(&StateTransition{
			StepID:    id,
			FromState: oldState,
			ToState:   state,
			Reason:    "state_change",
			Timestamp: time.Now().UTC(),
		}); err != nil {
			s.logger.Warn("Failed to record state transition", "step_id", id, "error", err)
		}
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
// and the task can be considered successfully completed.
//
// A task is complete when:
// - All steps are in a terminal state
// - No steps have failed (StepFailed)
// - All rejected steps have a successful revision (completed/approved)
func (s *StepStore) AreAllCompleted(taskID string) (bool, error) {
	steps, err := s.ListByTaskID(taskID)
	if err != nil {
		return false, err
	}

	if len(steps) == 0 {
		return false, nil
	}

	// Build a map of step states and track which rejected steps have successful revisions
	stepStates := make(map[string]StepState)
	rejectedSteps := make(map[string]bool) // stepID -> has successful revision

	for _, step := range steps {
		stepStates[step.ID] = step.State
		if step.State == StepRejected {
			rejectedSteps[step.ID] = false // initially no successful revision
		}
	}

	// Check for successful revisions of rejected steps
	// A revision depends on the original step, so check DependsOn
	for _, step := range steps {
		if step.State.IsSuccessfullyTerminal() {
			for _, depID := range step.DependsOn {
				if _, isRejected := rejectedSteps[depID]; isRejected {
					rejectedSteps[depID] = true // this rejected step has a successful revision
				}
			}
		}
	}

	for _, step := range steps {
		if !step.State.IsTerminal() {
			return false, nil
		}
		if step.State == StepFailed {
			return false, nil
		}
		// Check rejected steps have successful revisions
		if step.State == StepRejected {
			if !rejectedSteps[step.ID] {
				return false, nil // rejected without successful revision
			}
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to count steps by state: %w", err)
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
		id, taskID, description, state      string
		dependsOn, toolHint                 sql.NullString
		agentID, jobID, result              sql.NullString
		sequence, revisionCount, tokenUsage int
		recommendations, evidence, claims   sql.NullString
		validated                           bool
		validationError                     sql.NullString
		memoryRefs, accumulatedContext      sql.NullString
		modelOverride, checklist, phase     sql.NullString
		checkpointGate                      bool
		isHandoff                           bool
		createdAt, updatedAt                string
	)

	err := row.Scan(&id, &taskID, &description, &dependsOn, &toolHint, &agentID,
		&jobID, &state, &result, &sequence, &revisionCount,
		&recommendations, &evidence, &claims, &validated, &validationError,
		&tokenUsage, &memoryRefs, &accumulatedContext, &modelOverride,
		&checklist, &phase, &checkpointGate, &isHandoff, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrStepNotFound
		}
		return nil, err
	}

	return buildStep(id, taskID, description, state, dependsOn, toolHint,
		agentID, jobID, result, sequence, revisionCount, tokenUsage,
		recommendations, evidence, claims, validated, validationError,
		memoryRefs, accumulatedContext, modelOverride, checklist, phase,
		checkpointGate, isHandoff, createdAt, updatedAt), nil
}

func (s *StepStore) scanStepRows(rows *sql.Rows) (*TaskStep, error) {
	var (
		id, taskID, description, state      string
		dependsOn, toolHint                 sql.NullString
		agentID, jobID, result              sql.NullString
		sequence, revisionCount, tokenUsage int
		recommendations, evidence, claims   sql.NullString
		validated                           bool
		validationError                     sql.NullString
		memoryRefs, accumulatedContext      sql.NullString
		modelOverride, checklist, phase     sql.NullString
		checkpointGate                      bool
		isHandoff                           bool
		createdAt, updatedAt                string
	)

	err := rows.Scan(&id, &taskID, &description, &dependsOn, &toolHint, &agentID,
		&jobID, &state, &result, &sequence, &revisionCount,
		&recommendations, &evidence, &claims, &validated, &validationError,
		&tokenUsage, &memoryRefs, &accumulatedContext, &modelOverride,
		&checklist, &phase, &checkpointGate, &isHandoff, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	return buildStep(id, taskID, description, state, dependsOn, toolHint,
		agentID, jobID, result, sequence, revisionCount, tokenUsage,
		recommendations, evidence, claims, validated, validationError,
		memoryRefs, accumulatedContext, modelOverride, checklist, phase,
		checkpointGate, isHandoff, createdAt, updatedAt), nil
}

func buildStep(id, taskID, description, state string,
	dependsOn, toolHint, agentID, jobID, result sql.NullString,
	sequence, revisionCount, tokenUsage int,
	recommendations, evidence, claims sql.NullString,
	validated bool, validationError sql.NullString,
	memoryRefs, accumulatedContext, modelOverride, checklist, phase sql.NullString,
	checkpointGate bool,
	isHandoff bool,
	createdAt, updatedAt string) *TaskStep {

	step := &TaskStep{
		ID:                 id,
		TaskID:             taskID,
		Description:        description,
		State:              StepState(state),
		Sequence:           sequence,
		RevisionCount:      revisionCount,
		TokenUsage:         tokenUsage,
		Validated:          validated,
		MemoryRefs:         decodeStringSlice(memoryRefs.String),
		AccumulatedContext: accumulatedContext.String,
		IsHandoff:          isHandoff,
	}

	if modelOverride.Valid {
		step.ModelOverride = modelOverride.String
	}
	if checklist.Valid {
		step.Checklist = decodeChecklist(checklist.String)
	}
	if phase.Valid {
		step.Phase = phase.String
	}
	step.CheckpointGate = checkpointGate

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
	if recommendations.Valid {
		_ = json.Unmarshal([]byte(recommendations.String), &step.Recommendations)
	}
	if evidence.Valid {
		_ = json.Unmarshal([]byte(evidence.String), &step.Evidence)
	}
	if claims.Valid {
		step.Claims = decodeStringSlice(claims.String)
	}
	if validationError.Valid {
		step.ValidationError = validationError.String
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		step.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		step.UpdatedAt = t
	}

	return step
}

// encodeRecommendations encodes recommendations as JSON.
func encodeRecommendations(recs []CategorizedRecommendation) string {
	if len(recs) == 0 {
		return ""
	}
	data, _ := json.Marshal(recs)
	return string(data)
}

// encodeEvidenceSlice encodes an evidence slice as JSON.
func encodeEvidenceSlice(evs []models.Evidence) string {
	if len(evs) == 0 {
		return ""
	}
	data, _ := json.Marshal(evs)
	return string(data)
}

// encodeChecklist encodes a checklist as JSON.
func encodeChecklist(c *Checklist) string {
	if c == nil || len(c.Items) == 0 {
		return ""
	}
	data, _ := json.Marshal(c)
	return string(data)
}

// decodeChecklist decodes a JSON checklist.
func decodeChecklist(s string) *Checklist {
	if s == "" {
		return nil
	}
	var c Checklist
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil
	}
	return &c
}

// SetStateWithReason updates a step's state and records the transition with a reason.
func (s *StepStore) SetStateWithReason(id string, state StepState, reason string) error {
	// Get current state for transition logging
	var currentStepState StepState
	row := s.db.QueryRow("SELECT state FROM task_steps WHERE id = ?", id)
	if err := row.Scan(&currentStepState); err != nil {
		// Step might not exist yet, continue without transition logging
		currentStepState = StepPending
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE task_steps SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), now, id)
	if err != nil {
		return fmt.Errorf("failed to set step state: %w", err)
	}

	// Record transition when state actually changed
	if currentStepState != state {
		if err := s.RecordTransition(&StateTransition{
			StepID:    id,
			FromState: currentStepState,
			ToState:   state,
			Reason:    reason,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			s.logger.Warn("Failed to record state transition", "step_id", id, "error", err)
		}
	}

	return nil
}

// RecordTransition records a state transition in the database.
func (s *StepStore) RecordTransition(transition *StateTransition) error {
	_, err := s.db.Exec(`
		INSERT INTO task_state_transitions (step_id, from_state, to_state, reason, agent_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`,
		transition.StepID,
		string(transition.FromState),
		string(transition.ToState),
		nullableString(transition.Reason),
		nullableString(transition.AgentID),
		transition.Timestamp.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to record transition: %w", err)
	}
	return nil
}

// GetTransitions returns all state transitions for a step, ordered by timestamp.
func (s *StepStore) GetTransitions(stepID string) ([]*StateTransition, error) {
	rows, err := s.db.Query(`
		SELECT id, step_id, from_state, to_state, reason, agent_id, timestamp
		FROM task_state_transitions
		WHERE step_id = ?
		ORDER BY timestamp ASC`, stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}
	defer rows.Close()

	var transitions []*StateTransition
	for rows.Next() {
		var t StateTransition
		var reason, agentID sql.NullString
		var timestamp string

		if err := rows.Scan(&t.ID, &t.StepID, &t.FromState, &t.ToState, &reason, &agentID, &timestamp); err != nil {
			continue
		}

		if reason.Valid {
			t.Reason = reason.String
		}
		if agentID.Valid {
			t.AgentID = agentID.String
		}
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			t.Timestamp = ts
		}

		transitions = append(transitions, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	return transitions, nil
}

// GetTransitionsByTask returns all state transitions for all steps in a task.
func (s *StepStore) GetTransitionsByTask(taskID string) ([]*StateTransition, error) {
	rows, err := s.db.Query(`
		SELECT st.id, st.step_id, st.from_state, st.to_state, st.reason, st.agent_id, st.timestamp
		FROM task_state_transitions st
		JOIN task_steps ts ON st.step_id = ts.id
		WHERE ts.task_id = ?
		ORDER BY st.timestamp ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task transitions: %w", err)
	}
	defer rows.Close()

	var transitions []*StateTransition
	for rows.Next() {
		var t StateTransition
		var reason, agentID sql.NullString
		var timestamp string

		if err := rows.Scan(&t.ID, &t.StepID, &t.FromState, &t.ToState, &reason, &agentID, &timestamp); err != nil {
			continue
		}

		if reason.Valid {
			t.Reason = reason.String
		}
		if agentID.Valid {
			t.AgentID = agentID.String
		}
		if ts, err := time.Parse(time.RFC3339, timestamp); err == nil {
			t.Timestamp = ts
		}

		transitions = append(transitions, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to get task transitions: %w", err)
	}

	return transitions, nil
}

// SetTransitionLogging enables or disables transition logging.
func (s *StepStore) SetTransitionLogging(enabled bool) {
	s.logTransitions = enabled
}

// TransitionLoggingEnabled returns whether transition logging is enabled.
func (s *StepStore) TransitionLoggingEnabled() bool {
	return s.logTransitions
}

// GetCheckpointGates returns all steps that are checkpoint gates for a task.
func (s *StepStore) GetCheckpointGates(taskID string) ([]*TaskStep, error) {
	steps, err := s.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	var gates []*TaskStep
	for _, step := range steps {
		if step.CheckpointGate {
			gates = append(gates, step)
		}
	}
	return gates, nil
}

// AreCheckpointGatesPassed returns true if all checkpoint gates for a task are completed.
func (s *StepStore) AreCheckpointGatesPassed(taskID string) (bool, error) {
	gates, err := s.GetCheckpointGates(taskID)
	if err != nil {
		return false, err
	}

	if len(gates) == 0 {
		return true, nil // No gates, all passed
	}

	for _, gate := range gates {
		if !gate.State.IsSuccessfullyTerminal() {
			return false, nil
		}
	}
	return true, nil
}

// GetPhaseSteps returns all steps for a specific phase.
func (s *StepStore) GetPhaseSteps(taskID, phase string) ([]*TaskStep, error) {
	steps, err := s.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	var phaseSteps []*TaskStep
	for _, step := range steps {
		if step.Phase == phase {
			phaseSteps = append(phaseSteps, step)
		}
	}
	return phaseSteps, nil
}

// IsPhaseComplete returns true if all steps in a phase are completed.
func (s *StepStore) IsPhaseComplete(taskID, phase string) (bool, error) {
	phaseSteps, err := s.GetPhaseSteps(taskID, phase)
	if err != nil {
		return false, err
	}

	if len(phaseSteps) == 0 {
		return true, nil // No steps in phase, considered complete
	}

	for _, step := range phaseSteps {
		if !step.State.IsSuccessfullyTerminal() {
			return false, nil
		}
	}
	return true, nil
}
