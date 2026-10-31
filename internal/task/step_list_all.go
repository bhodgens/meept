package task

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ListAllSteps returns every task step across all tasks, oldest updated
// first. Read-only addition (activity-aggregator leaf, 2026-10-30): the
// step store only exposed per-task reads (ListByTaskID) before; the
// cross-store activity view needs a full listing without importing sqlite
// or reaching into the DB file. Same style as ListByTaskID.
func (s *StepStore) ListAllSteps() ([]*TaskStep, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, description, depends_on, tool_hint, agent_id,
		       job_id, state, result, sequence, revision_count,
		       recommendations, evidence, claims, validated, validation_error,
		       filter_retry_count, filter_error,
		       token_usage, memory_refs, accumulated_context, model_override,
		       checklist, phase, checkpoint_gate, is_handoff, conversation_id,
		       session_id, suggested_next_hint, created_at, updated_at
		       FROM task_steps
		ORDER BY updated_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all steps: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate all steps: %w", err)
	}
	return steps, nil
}

// ensure ctx/sql/time stay referenced if the file grows later
var (
	_ = context.Background
	_ = sql.ErrNoRows
	_ = time.Now
)
