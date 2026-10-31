package auditlog

import (
	"context"
	"database/sql"
	"fmt"
)

// List returns chain records in ascending seq order (read-only addition,
// activity-aggregator leaf 2026-10-30: the Store previously exposed only
// Head; the cross-store activity view needs the full chain read in the
// same package style). limit <= 0 means unbounded.
func (s *Store) List(ctx context.Context, limit int) ([]Record, error) {
	query := `
        SELECT seq, prev_hash, record_hash, type, employee_id, payload, at
        FROM audit_log_chain ORDER BY seq ASC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit log list: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var (
			rec     Record
			payload string
			at      string
		)
		if err := rows.Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash, &rec.Type,
			&rec.EmployeeID, &payload, &at); err != nil {
			return nil, fmt.Errorf("audit log list scan: %w", err)
		}
		if err := decodePayload(payload, &rec.Payload); err != nil {
			return nil, err
		}
		if err := rec.At.UnmarshalJSON([]byte(`"` + at + `"`)); err != nil {
			return nil, fmt.Errorf("audit log list: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit log list: %w", err)
	}
	return records, nil
}

// keep sql referenced for the null-scan symmetry note above
var _ = sql.ErrNoRows
