# Leaf 01 — gosec G115/G123 zero

**Objective:** Fix all 7 scoped gosec findings so
`gosec -quiet -include=G115,G123 ./internal/...` returns zero.

**Files (from live scan 2026-09-29):**
- `internal/memory/vector/store.go` — 3 findings (G115)
- `internal/tui/vim/mode.go` — 1 finding (G115)
- `internal/tui/thread_indicator.go` — 1 finding (G115)
- `internal/code/ast/parser.go` — 1 finding (G115/G123)
- (re-scan after fixing — counts may shift by ±1 across these files)

**Method (per finding):**
1. Run `gosec -quiet -include=G115,G123 ./internal/... 2>&1 | grep -E '\.go:[0-9]+'`
   to list exact sites.
2. For each: read the surrounding code. Prefer a real fix (explicit bound
   check before conversion, or `min()`/`max()` clamp like
   `cmd/meept/doctor.go:339-352` post-fix shape) over `//nolint:gosec`.
   Use `//nolint:gosec // G115: <reason>` ONLY when the value is provably
   bounded by construction — and state the bound in the comment.
3. DO NOT change behavior. These are conversion-safety annotations, not
   refactors.

**Verify:**
```
gosec -quiet -include=G115,G123 ./internal/...   # expect: no findings
go build ./...                                    # expect: clean
go test -p 2 -count=1 ./internal/memory/vector/ ./internal/tui/... ./internal/code/...   # expect: ok
```

**Commit:** one commit, message `fix(lint): resolve gosec G115/G123 findings (7 sites)`.

**Self-check before reporting:** re-run the gosec command; paste the zero-
finding result into the report.
