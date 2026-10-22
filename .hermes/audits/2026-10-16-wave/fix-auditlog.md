# Fix: VerifyChain returns OK=true for an EMPTY chain

## Problem
`VerifyChain` in `internal/auditlog/chain.go` unconditionally sets `res.OK = true` after the row loop, even when zero records were scanned. This means a truncated-to-zero audit log "verifies" successfully -- a tamper-evidence gap.

## Fix
When zero records are seen, `VerifyChain` now returns `OK=false` with `Reason: "empty chain"`. This closes the gap without breaking any callers, since:
- The CLI `agents audit verify` RPC (`internal/employee/handler.go:813`) uses the result to report chain integrity -- an empty chain failing verification is the correct security behavior.
- The test `TestVerifyChain_EmptyAndGood` was updated to expect the new empty-chain verdict.

## Files Changed
- `internal/auditlog/chain.go:123-133` -- Added empty-chain check; `res.OK = false` with `Reason: "empty chain"` when `res.Records == 0`
- `internal/auditlog/chain_test.go:68-74` -- Updated test to assert empty chain returns `OK=false` with `Reason: "empty chain"`

## Regression Test
The existing test `TestVerifyChain_EmptyAndGood` in `internal/auditlog/chain_test.go` serves as the regression test. It creates a fresh scratch DB (via `t.TempDir()`), runs `Migrate` to create the schema, then calls `VerifyChain` before any `Append` -- asserting the new empty-chain verdict.

## Gates
- `go build ./internal/auditlog/...` -- PASSED
- `go vet ./internal/auditlog/...` -- PASSED
- `go test -p 2 ./internal/auditlog/...` -- PASSED (all tests green)

gates_run: go build ./internal/auditlog/... (ok), go vet ./internal/auditlog/... (ok), go test -p 2 ./internal/auditlog/... (ok)
