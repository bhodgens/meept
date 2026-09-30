# Leaf 03 — stray test + final lint zero

**Objective:** Resolve the untracked broken test file and run the final
full-repo lint verification.

**Note:** the stray test is now the TDD seed landed by leaf 02. This
leaf's remaining scope is the final verification sweep.

**Files:**
- Verify only: `internal/services/plan_service_sink_test.go` (tracked after leaf 02)

**Steps:**
1. `golangci-lint run ./...` — expect zero typecheck errors and zero
   findings. Fix any stragglers in place (annotation or real fix, same
   rules as leaf 01).
2. `gosec -quiet -include=G115,G123 ./internal/...` — expect zero.
3. `go build ./...` + `make e2e-fast` — full hermetic tier green.
4. `make graphs` if `git status docs/generated/` shows changes after the
   build; commit graphs separately.
5. `git status` — confirm no untracked *.go debris remains at repo root
   (backfill-evolver-plans, gendoc-openapi were the prior offenders).

**Commit:** `chore: final lint-zero verification for coverage-completion` (only if fixes were needed).

**Self-check:** paste the zero-finding golangci + gosec output into the
report.
