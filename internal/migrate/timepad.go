package migrate

import (
	"context"
	"database/sql"
	"fmt"
)

// FixedWidthTimeLen is the character width of the fixed-width RFC3339 UTC
// timestamp format that the memory stores write (layout
// "2006-01-02T15:04:05.000000000Z07:00", always 9 fraction digits, 'Z'
// suffix): 30. Kept in this package because the legacy-shape predicates
// below assume exactly this wall-clock layout (positions 11/20).
const FixedWidthTimeLen = 30

// LegacyTimestampWhere returns the shape predicate selecting only legacy,
// variable-width RFC3339Nano timestamp values (trailing fraction zeros
// trimmed at write time, e.g. ".1833Z" for .183300000). Deliberately
// narrow: NULLs, empty strings (nullable columns keep their sentinel
// untouched), full-width rows, and anything not shaped
// 'YYYY-MM-DDTHH:MM:SS(.frac)?Z' are left alone, so padding is idempotent
// and never rewrites an already-correct value.
func LegacyTimestampWhere(col string) string {
	return fmt.Sprintf(
		`%[1]s IS NOT NULL AND %[1]s != '' AND length(%[1]s) < %[2]d`+
			` AND substr(%[1]s, 11, 1) = 'T'`+
			` AND substr(%[1]s, length(%[1]s), 1) = 'Z'`+
			` AND (substr(%[1]s, 20, 1) = '.' OR substr(%[1]s, 20, 1) = 'Z')`,
		col, FixedWidthTimeLen)
}

// LegacyTimestampPadExpr rewrites a legacy-shape timestamp to fixed width
// purely in SQL: fraction digits (position 21..before the final 'Z') get
// zero-padded to 9; a second-precision timestamp ('...SSZ', no fraction)
// gets '.000000000' inserted. Padding a decimal fraction with trailing
// zeros preserves the exact instant — the migration rewrites
// representation, never value.
func LegacyTimestampPadExpr(col string) string {
	return fmt.Sprintf(
		`CASE WHEN substr(%[1]s, 20, 1) = '.' THEN`+
			` substr(%[1]s, 1, 20)`+
			` || substr(substr(%[1]s, 21, length(%[1]s) - 21) || '000000000', 1, 9) || 'Z'`+
			` ELSE substr(%[1]s, 1, 19) || '.000000000Z' END`,
		col)
}

// PadLegacyTimestampSQL builds the idempotent padding UPDATE for one
// timestamp column of one table. Table/column names must be store
// constants, never user input (#nosec G201/G202).
func PadLegacyTimestampSQL(table, col string) string {
	// #nosec G201 -- table/col are compile-time store constants, not user input
	//nolint:gosec // identifier interpolation from store config
	return fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s",
		table, col, LegacyTimestampPadExpr(col), LegacyTimestampWhere(col))
}

// PadLegacyTimestampsSQL joins the padding UPDATEs for every timestamp
// column of a table into one multi-statement migration SQL (modernc.org
// /sqlite's Exec runs all statements, and Apply wraps them with the
// user_version bump in a single transaction).
func PadLegacyTimestampsSQL(table string, cols ...string) string {
	sql := ""
	for _, col := range cols {
		sql += PadLegacyTimestampSQL(table, col) + ";"
	}
	return sql
}

// LegacyPaddingPending returns a Guard predicate for a padding migration
// step: does any row still carry a variable-width legacy timestamp in one
// of the columns? Databases that never had legacy rows (fresh create) or
// were already padded record the version without re-running the DML.
func LegacyPaddingPending(table string, cols ...string) func(*sql.DB) (bool, error) {
	return func(db *sql.DB) (bool, error) {
		exists, err := TableExists(db, table)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
		for _, col := range cols {
			// #nosec G201 -- table/col are compile-time store constants
			//nolint:gosec // identifier interpolation from store config
			q := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s)", table, LegacyTimestampWhere(col))
			var pending int
			if err := db.QueryRow(q).Scan(&pending); err != nil {
				// Column absent (a pre-column legacy db): nothing pending
				// for it — the column-adding steps run before this one.
				continue
			}
			if pending == 1 {
				return true, nil
			}
		}
		return false, nil
	}
}

// BackfillFixedWidthTimestamps pads legacy variable-width timestamps in
// place and reports how many column values were rewritten. It is
// idempotent by construction: the WHERE clause only matches short legacy
// shapes, so re-running affects zero rows. Missing columns are skipped
// (the caller's schema may predate them); other failures surface.
func BackfillFixedWidthTimestamps(ctx context.Context, db *sql.DB, table string, cols ...string) (int, error) {
	changed := 0
	for _, col := range cols {
		exists, err := TableExists(db, table)
		if err != nil {
			return changed, err
		}
		if !exists {
			return changed, nil
		}
		has, cerr := ColumnExists(db, table, col)
		if cerr != nil {
			return changed, fmt.Errorf("migrate: column probe %s.%s: %w", table, col, cerr)
		}
		if !has {
			continue
		}
		res, err := db.ExecContext(ctx, PadLegacyTimestampSQL(table, col))
		if err != nil {
			return changed, fmt.Errorf("migrate: pad legacy timestamps %s.%s: %w", table, col, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			changed += int(n)
		}
	}
	return changed, nil
}
