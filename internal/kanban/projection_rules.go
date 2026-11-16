package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/caimlas/meept/pkg/id"
)

// ProjectionRule is one row of the kanban_projections table (C3): a
// (kind, from_state) -> column mapping scoped to a board. The projector
// (03-board-projection/01) consults rules on every StateEvent and sends
// the canonical card for the work item to the column the rule names.
//
// The key column is from_state — the state the transition LEAVES — exactly
// as C3 names the table's column and as the leaf's mapping instruction
// pins ("map (Kind, FromState)->column via rules"). Rule authors therefore
// write one row per outgoing transition, e.g. (task, pending) -> "doing"
// means "when a task leaves pending, its card goes to doing".
type ProjectionRule struct {
	ID        string
	BoardID   string
	Kind      string // WorkRef machine name: task|job|plan|step|goal
	FromState string // the state the transition leaves
	ColumnID  string
}

// ProjectionRules is an immutable in-memory snapshot of rule rows. The
// projector holds one and never re-reads the table per event; reloading a
// snapshot (rebuild, 03-board-projection/02) is a construction-time
// concern, not a hot-path one.
type ProjectionRules struct {
	rows []ProjectionRule
}

// NewProjectionRules copies rows into an immutable snapshot.
func NewProjectionRules(rows []ProjectionRule) ProjectionRules {
	return ProjectionRules{rows: append([]ProjectionRule(nil), rows...)}
}

// Len reports the number of rules in the snapshot.
func (r ProjectionRules) Len() int { return len(r.rows) }

// Rows returns a copy of the rule rows (safe for callers to retain).
func (r ProjectionRules) Rows() []ProjectionRule {
	return append([]ProjectionRule(nil), r.rows...)
}

// Matches returns every rule keyed on (kind, state) — normally zero or
// one; multiple boards on one ruleset may legitimately return several.
func (r ProjectionRules) Matches(kind, state string) []ProjectionRule {
	var out []ProjectionRule
	for _, row := range r.rows {
		if row.Kind == kind && row.FromState == state {
			out = append(out, row)
		}
	}
	return out
}

// SetProjectionRule upserts the rule for (board, kind, from_state). The
// table shipped in 01-foundation/01 without Go accessors; these are the
// missing ones (C3). Re-setting the same key replaces the target column
// instead of stacking a second mapping — a projector that saw two rules
// for one key would ping-pong the card between columns.
//
// Concurrency note: the store is single-connection (SetMaxOpenConns(1)),
// so the read-then-write upsert below cannot interleave with another
// writer in-process; cross-process writers are excluded by the daemon's
// sole-owner model.
func (s *Store) SetProjectionRule(ctx context.Context, rule ProjectionRule) error {
	if rule.BoardID == "" {
		return fmt.Errorf("projection rule board_id is required")
	}
	if rule.Kind == "" || rule.FromState == "" {
		return fmt.Errorf("projection rule kind and from_state are required")
	}
	if rule.ColumnID == "" {
		return fmt.Errorf("projection rule column_id is required")
	}

	var existingID string
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM kanban_projections WHERE board_id = ? AND kind = ? AND from_state = ?",
		rule.BoardID, rule.Kind, rule.FromState).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to look up projection rule: %w", err)
	}
	if err == nil {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE kanban_projections SET column_id = ? WHERE id = ?", rule.ColumnID, existingID); err != nil {
			return fmt.Errorf("failed to update projection rule: %w", err)
		}
		return nil
	}

	if rule.ID == "" {
		rule.ID = id.Generate("proj-")
	}
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO kanban_projections (id, board_id, kind, from_state, column_id) VALUES (?, ?, ?, ?, ?)",
		rule.ID, rule.BoardID, rule.Kind, rule.FromState, rule.ColumnID); err != nil {
		return fmt.Errorf("failed to insert projection rule: %w", err)
	}
	return nil
}

// LoadProjectionRules reads the rules for boardID into a snapshot. An
// empty boardID loads every board's rules (reconcile/rebuild, 03/02).
func (s *Store) LoadProjectionRules(ctx context.Context, boardID string) (ProjectionRules, error) {
	query := "SELECT id, board_id, kind, from_state, column_id FROM kanban_projections"
	var args []any
	if boardID != "" {
		query += " WHERE board_id = ?"
		args = append(args, boardID)
	}
	query += " ORDER BY kind ASC, from_state ASC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ProjectionRules{}, fmt.Errorf("failed to list projection rules: %w", err)
	}
	defer rows.Close()

	var out []ProjectionRule
	for rows.Next() {
		var r ProjectionRule
		if err := rows.Scan(&r.ID, &r.BoardID, &r.Kind, &r.FromState, &r.ColumnID); err != nil {
			return ProjectionRules{}, fmt.Errorf("failed to scan projection rule: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return ProjectionRules{}, fmt.Errorf("failed to iterate projection rules: %w", err)
	}
	return NewProjectionRules(out), nil
}
