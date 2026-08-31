# Plan: Skill evolution: improve go-sqlite-null-scan-error

## Meta

- plan_id: plan-20260831231950-0005
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.00 with 5 negative and 0 positive outcomes across 7 injections — the current skill is actively harming performance and needs significant revision.

Candidate content:
# go-sqlite-null-scan-error

Specialist skill for diagnosing and fixing Go SQLite scan errors involving NULL values.

## When to use

Invoke when code exhibits any of the following:
- `sql: Scan error on column index N` during `rows.Scan()`
- Panic from dereferencing a nil pointer after querying nullable SQLite columns
- Type mismatch errors scanning into `string`, `int`, `bool` for columns that may contain NULL
- Need to safely read optional fields from SQLite results

## Core Principles

1. **SQLite NULL maps to Go pointers/Null types** — you cannot scan a NULL SQLite value directly into a non-pointer Go variable.
2. **Always use `sql.Null*` structs** for columns that may be NULL, or scan into `*T` pointers.
3. **Check `.Valid`** before accessing the underlying value of a `sql.Null*` struct.

## Common Patterns

### Pattern 1: Using sql.NullString / sql.NullInt64

```go
import "database/sql"

var name sql.NullString
var age sql.NullInt64
err := row.Scan(&name, &age)
if err != nil {
    return err
}
if name.Valid {
    // use name.String
} else {
    // column was NULL
}
```

### Pattern 2: Scanning into pointers

```go
var name *string
var age *int
err := row.Scan(&name, &age)
if err != nil {
    return err
}
if name != nil {
    // use *name
}
```

### Pattern 3: Helper for default values

```go
func nullString(s sql.NullString, def string) string {
    if s.Valid {
        return s.String
    }
    return def
}

func nullInt64(i sql.NullInt64, def int64) int64 {
    if i.Valid {
        return i.Int64
    }
    return def
}
```

## Anti-patterns to Avoid

- **Never** scan a nullable column directly into `string`, `int`, `bool` — this panics or errors on NULL.
- **Never** assume `.String` or `.Int64` is safe without checking `.Valid` first.
- **Avoid** custom `Scan` implementations unless you have a specific reason — standard library `Null*` types handle edge cases.

## Debugging Checklist

1. Identify which column index is failing in the error message.
2. Check the SQLite schema for that column — is it defined as NOT NULL?
3. If nullable, verify the Go destination type is a pointer or `sql.Null*`.
4. Verify `row.Scan()` arguments match column count and types exactly.
5. For TEXT columns that may be NULL, use `sql.NullString` (not `*string` unless preferred).
6. For INTEGER columns that may be NULL, use `sql.NullInt64`.
7. For REAL/BLOB columns that may be NULL, use `sql.NullFloat64` / `sql.NullString`.

## Tool Hints

- Use `analyst` to inspect schema and trace error paths.
- Use `coder` to refactor scan calls with correct Null types.
- Use `debugger` for runtime panic investigation.


## Notes

