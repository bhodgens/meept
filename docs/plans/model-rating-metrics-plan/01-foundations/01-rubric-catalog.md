# Rubric Catalog (internal/modelrating) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate
> tests from 00T-contracts.md ALREADY EXIST and are committed - make them pass; do
> NOT delete, skip, weaken, or rewrite any gate test. Work ONLY in
> `/Users/caimlas/git/meept/.hermes/worktrees/model-rating-metrics-plan`.
> Do NOT commit. Do NOT use read_file on existing source files - explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** New package `internal/modelrating`: rubric IDs, scopes, categories,
  severities, Verdict struct, validation helpers
- **Dependencies:** 00T-contracts (TESTS_VALIDATED)
- **Estimated Context:** 40K
- **Concurrency Group:** B

## Goal

The frozen vocabulary every later leaf imports. Pure data + tiny validation; no I/O,
no daemon deps, stdlib only.

## Context

Issue #168's corrected taxonomy. The catalog distinguishes two rubric scopes so
workflow defects (planner/orchestrator failures) never blame the reviewed model.
Categories/severity strings are wire values persisted in SQLite and returned by HTTP -
they are frozen; do not rename or reorder.

## Verified Anchors

- `internal/metrics/store.go:246-268` - house DDL style (text columns, sentinels)
- `pkg/id` - exists for ID minting (not needed here; catalog IDs are constants)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master.md C1:

```go
package modelrating
type RubricID string
type RubricScope string
const (ScopeModel RubricScope = "model"; ScopeWorkflow RubricScope = "workflow")
type Category string
// "pass", "wrong_language", "wrong_api_or_dependency", "logic_error",
// "security_defect", "incomplete_implementation", "unrequested_change",
// "build_or_test_failure", "style_bloat", "convention_violation"
type Severity string
// "cosmetic", "functional", "blocking", "critical"; SeverityNone = ""
type Verdict struct{ RubricID, Scope, Category, Severity, Detail string; Confidence float64 }
func (v Verdict) Validate() error   // scope in vocab; category in vocab; severity rules:
                                    // pass => severity ""; else severity in 4-level vocab
func Rubrics() []Rubric             // full catalog, stable order; Rubric{ID, Scope, Statement string}
```

### What This Leaf Consumes

- Nothing outside stdlib.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Package + vocab types + constants | Create: internal/modelrating/catalog.go | `go test ./internal/modelrating/ -run MR-CAT` | package missing | gate MR-CAT-1..3 GREEN | Given the package; When enumerating; Then frozen vocab matches master.md C1 verbatim |
| T2 | Catalog entries (MODEL + WORKFLOW scope lists) | Modify: internal/modelrating/catalog.go | `go test ./internal/modelrating/ -run MR-CAT-4` | Rubrics() undefined | MR-CAT-4 GREEN | Given Rubrics(); When filtering by scope; Then MODEL rubrics cover: line-number corruption, stray debug artifacts/TODOs/placeholders, heredocs in code assembly, grep -c/-q zero-match aborts, paraphrase drift, inconsistent conventions, provider rate limits ignored + the 9 defect categories; WORKFLOW rubrics cover: leaf sizing, interface contracts, test gate, orchestrator-review-skipped, status trickle/reset, circular dependencies, context budget, sequential dispatch, leaf-agent-committed-code, missing/stale plan sections, sibling design drift |
| T3 | Verdict.Validate | Modify: internal/modelrating/catalog.go | `go test ./internal/modelrating/ -run MR-CAT` | Validate undefined | all MR-* GREEN | Given pass verdict with severity ""; When Validate; Then nil. Given defect verdict with severity "critical"; Then nil. Given unknown scope/category/severity combos; Then error |

Rubric IDs: prefix `MR-` + two-letter scope code + digit, e.g. `MR-LC1` (line
corruption), `MR-WF1` (workflow: leaf sizing). Document each statement in the catalog
comment; IDs are frozen once committed.

## Self-Verification Checklist

- [ ] Gate tests MR-CAT-1..4 pass; no gate test modified (`git diff` on gate file empty)
- [ ] Package imports stdlib only
- [ ] gofmt clean; no TODOs/placeholders

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. gate-test output; 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] C1 satisfied verbatim (names, strings, signatures)
- [ ] Catalog covers every rubric from issue #168's two scope lists
- [ ] No gate-test diff; no artifacts; gofmt clean

Output: APPROVED or specific gaps.

## Notes

Keep statements one sentence each in the catalog; they become the model judge's
prompt material in branch 02 and the docs table in branch 04.
