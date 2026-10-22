# Fix: Security Sanitizer C0 Control Character Handling

## Finding
The security sanitizer (`internal/security/sanitizer.go`) passes C0 control characters (NUL `\x00`, backspace `\x08`, escape `\x1b`, DEL `\x7f`) through unchanged, while the sibling FTS5 sanitizer (`pkg/sqlite/fts5.go:122-135`) strips them via `unicode.IsControl(r)`. This inconsistency allows control bytes to reach prompt assembly, creating a potential security risk.

## Changes
- **File**: `internal/security/sanitizer.go`
  - Added `"unicode"` import
  - Modified `sanitizeStructure()` to strip C0 control characters (U+0000-U+001F, U+007F) except tab (`\t`), newline (`\n`), and carriage return (`\r`)
  - Operates on runes to preserve UTF-8 correctness
  - Updates `modified` flag when control characters are removed

- **File**: `internal/security/sanitizer_test.go` (existing, verified)
  - Table-driven regression test covering:
    - NUL, backspace, escape, DEL characters are removed
    - Tab, newline, carriage return are preserved
    - Multi-byte UTF-8 characters remain intact after control character removal
    - Mixed control character strings

## Test Results
- `go build ./internal/security/...` -- PASS
- `go vet ./internal/security/...` -- PASS
- `go test -p 2 ./internal/security/...` -- PASS (1.658s)

## Observations
- None

gates_run: go build ./internal/security/... (PASS), go vet ./internal/security/... (PASS), go test -p 2 ./internal/security/... (PASS)