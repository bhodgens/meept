# Plan: Skill evolution: improve go-sqlite-null-scan-error

## Meta

- plan_id: plan-20260831224700-0039
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.00 with 5 negative ratings, indicating the skill produces poor or irrelevant responses for Go SQLite NULL scan errors. The content needs restructuring with clearer diagnostics, concrete fix patterns, and better edge-case coverage.

Candidate content:
# Go SQLite NULL Scan Error Resolution

## Description
Diagnose and fix SQLite `sql: Scan error on column index N` NULL-related errors in Go applications using `database/sql` with `modernc.org/sqlite` or `github.com/mattn/go-sqlite3`.

## When to Use
- Application panics or returns errors like `sql: Scan error on column index N` during SQLite reads
- `driver.Value` type mismatches when scanning NULL values into Go variables
- Unexpected behavior when SQLite NULL columns don't map cleanly to Go types

## Root Causes

1. **Scanning NULL into non-pointer type** — Go's `int`, `string`, `bool` cannot hold NULL; they must use `*int`, `*string`, `*bool`
2. **Type mismatch** — scanning `TEXT` into `int`, or `FLOAT` into `string`
3. **Missing NULL handling in `.Scan()` calls** — using `sql.NullString`, `sql.NullInt64`, etc.
4. **Column index errors** — `Scan` argument order doesn't match query column order

## Diagnostic Steps

```bash
# 1. Identify the failing query and column index from the error message
grep -rn "Scan error on column index" . --include="*.go"

# 2. Find the source of the error
grep -rn "\.Scan(" . --include="*.go" | head -20

# 3. Check the SQLite schema for the relevant table
sqlite3 your_db.db ".schema your_table"
```

## Fix Patterns

### Pattern 1: Use Pointer Types for Nullable Columns
```go
// BEFORE (will panic on NULL):
var name string
err := row.Scan(&id, &name)

// AFTER:
var name *string
err := row.Scan(&id, &name)
if name != nil {
    // use *name
}
```

### Pattern 2: Use sql.Null* Types
```go
import "database/sql"

// BEFORE:
var active bool
err := row.Scan(&id, &active)

// AFTER:
var active sql.NullBool
err := row.Scan(&id, &active)
if active.Valid {
    // use active.Bool
}
```

### Pattern 3: Handle Type Mismatches with Casting
```go
// SQLite stores numbers as TEXT; cast explicitly:
var val float64
err := row.Scan(`CAST(my_col AS FLOAT)`)
```

### Pattern 4: Safe Scan Helper
```go
func scanPtr[T any](dest *T) interface{} {
    return dest
}

// Or use a generic wrapper:
type NullSafe struct {
    Valid bool
    Value interface{}
}
```

### Pattern 5: Using `ColumnTypeScanType()` to Inspect
```go
cols, _ := row.Columns()
for i, col := range cols {
    if ct, err := row.ColumnTypeScanType(i); err == nil {
        fmt.Printf("Column %d (%s): Go type = %v\n", i, col, ct)
    }
}
```

## Prevention Checklist
- [ ] All nullable SQLite columns use `*Type` or `sql.Null*` in Scan
- [ ] Column order in `Scan()` matches query SELECT order exactly
- [ ] Use `COALESCE(col, default_value)` in queries when appropriate
- [ ] Add unit tests with NULL-present rows for all scan paths

## Common Pitfalls
- `go-sqlite3` vs `modernc.org/sqlite` have different NULL handling behavior — verify which driver you're using
- `SCAN` into `[]byte` from `TEXT` column works, but `SCAN` into `string` from `NULL TEXT` fails
- `defer row.Close()` is not needed for single-row scans but is required for `rows.Next()` loops

## References
- [Go database/sql docs](https://pkg.go.dev/database/sql)
- [sql.Null* types docs](https://pkg.go.dev/database/sql#NullString)


## Notes

