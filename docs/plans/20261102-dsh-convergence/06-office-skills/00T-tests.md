# Office Skills - Test Gate Leaf (00T)

> **For the implementing agent:** You are the TEST IMPLEMENTER for this gate.
> Write tests ONLY - no implementation. Tests fail RED for
> missing-implementation reasons (the office tools do not exist yet). Run them
> once to confirm they fail for the RIGHT reason (missing tool / missing skill
> gate), not syntax errors. Do NOT commit. Do NOT git add. Do NOT use read_file
> on existing source - explore with search_files or terminal cat. Write once
> and stop.

## Meta

- **Parent:** ../../06-office-skills/orchestrator.md
- **Scope:** The hermetic e2e suite covering the four office features (F14a-d)
  for the 06-office-skills branch, written against the tool behavior contracts
  C11.2-C11.4.
- **Dependencies:** none (dispatch in Phase 0, parallel with 00-research)
- **Estimated Context:** 35K
- **Concurrency Group:** Phase 0 (test gate)

## Goal

Deliver the RED e2e tests that gate the three office implementation leaves and
the requires-tools gate. The tests assert the TOOL BEHAVIOR CONTRACT (JSON
params in orchestrator C11.2-C11.4) through the live daemon's RPC/tool surface
- they do NOT import the concrete docx/pptx library, so they are the SAME
regardless of what research leaf 00 resolves. That decoupling lets this gate
run in parallel with the research gate. The suite lives in
`e2e/suites/office-skills/` (the `e2e` build tag) and is wired into
`e2e/manifest.json` in the same commit, per root AGENTS.md e2e policy.

Root e2e policy: NO new unit test files for features - all feature tests are
hermetic e2e. (The orchestrator's Test Gate matrix records unit as the
sanctioned `n/a`.)

## Context

- Suffers the standard hermetic e2e harness: build meept-daemon/meept into
  `$TMPDIR/meept-e2e-bin*`, boot with a scratch `MEEPT_HOME` (`harness.WithPreBootHook`),
  assert through live RPC / the tool surface. Model your suite on an existing
  tool-driven suite (e.g. `e2e/suites/` with a table/`spreadsheet_write`-style
  precedent; grep the harness in `e2e/harness/` for daemon boot helpers).
- The RED end state: the daemon boots, but `office_docx` / `office_pptx` /
  `office_workbook` are NOT registered (no implementation leaf has run yet), and
  the `office-xlsx` skill is absent from config. Tests must FAIL for exactly
  that reason - "tool not registered", not a panic or harness syntax error.
- The requires-tools gate (`F14d`) validates the SKILL frontmatter gate: with
  `skills.validate_prerequisites: true`, executing a skill whose `requires-tools`
  lists an unregistered tool must be refused with
  `skill <name> requires unavailable tool(s): <list>`.

## Verified Anchors

- `/Users/caimlas/git/meept/e2e/manifest.json` - the suite manifest (path_map +
  suites + scenario inventory) to extend in the same commit.
- `/Users/caimlas/git/meept/internal/skills/executor.go:234-278` -
  `checkRequiredTools` (the gate the tests exercise).
- `/Users/caimlas/git/meept/docs/workflows/skills.md:84-89` - gate failure
  message contract.
- Tool contracts: orchestrator C11.2 (`office_docx`), C11.3 (`office_pptx`),
  C11.4 (`office_workbook`).

## Tasks

Tests-first (TDD): each feature row in the orchestrator Test Gate matrix maps
to e2e scenarios below. Write the suite files, run them, confirm they fail RED
with missing-implementation reasons, and report back.

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | office_docx round-trip e2e | Create: e2e/suites/office-skills/suite_test.go | `make e2e-fast-area AREA=office-skills` | FAIL: `office_docx` not in tool registry / skill absent | PASS: tool registered, create->reopen->validate round-trip returns final path + extracted text | Given a booted scratch daemon; When a client asks the daemon to run `office_docx` create+read against a temp .docx path in the session cwd; Then the tool returns the final absolute path and the reopened document contains the requested heading/paragraph text |
| T2 | office_pptx round-trip e2e | Included in suite_test.go | `make e2e-fast-area AREA=office-skills` | FAIL: `office_pptx` not registered | PASS: create->reopen->validate round-trip returns final path + slide count/content | Given a booted scratch daemon; When `office_pptx` create+read runs on a temp .pptx; Then the final path is returned and the reopened deck has the requested slide title/body |
| T3 | office_workbook round-trip e2e | Included in suite_test.go | `make e2e-fast-area AREA=office-skills` | FAIL: `office_workbook` not registered | PASS: create->reopen->validate round-trip returns path + sheet names + typed cell values | Given a booted scratch daemon; When `office_workbook` create+read runs on a temp .xlsx; Then the final path is returned and reopened sheet names/header cells match, formula cells preserved not evaluated |
| T4 | requires-tools gate e2e (F14d) | Included in suite_test.go | `make e2e-fast-area AREA=office-skills` | FAIL: skill exists OR gate not enforced | PASS: office-xlsx skill execution refused `requires unavailable tool(s)` when its tool is absent | Given `skills.validate_prerequisites: true` and the office-xlsx skill present; When a skill whose requires-tools lists an unregistered tool is executed; Then execution is refused with the exact `requires unavailable tool(s)` message |
| T5 | manifest + path_map wiring | Modify: e2e/manifest.json | `python3 -m json.tool e2e/manifest.json` ; `make e2e-affected-area AREA=office-skills` | FAIL: suite not discovered / unknown area | PASS: suite listed; internal/tools/builtin + config/skills map to the office-skills suite | Given the manifest edited; When `make e2e-affected-area AREA=office-skills` runs; Then the office-skills suite executes |

Task notes:
- Keep the suite in one or two Go files under `e2e/suites/office-skills/`
  with the `e2e` build tag (repo convention); T1-T4 may share one test process.
- Do NOT reference the concrete docx/pptx library anywhere - assert the tool
  contract surface only.
- The gate test (T4) must be written so it fails RED NOW (skill bundle not yet
  shipped -> the "present skill" precondition itself is the missing
  implementation) and passes only after the office-xlsx skill body + tool exist
  AND `skills.validate_prerequisites` is on.

## Self-Verification Checklist

- [ ] All five tasks implemented; suite present under e2e/suites/office-skills/
- [ ] `e2e/manifest.json` extended with the suite + path_map in the same edit
- [ ] Tests fail RED for missing-implementation reasons (tools unregistered /
      skill absent), NOT syntax errors
- [ ] No implementation written; no import of the docx/pptx library
- [ ] One language (Go); no git commit

**DO NOT COMMIT.** The orchestrator commits the validated gate tests at
TESTS_VALIDATED.

## Completion Report Format

1. **Files touched:** e2e/suites/office-skills/suite_test.go (+ any helper),
   e2e/manifest.json
2. **Verify command + output:** `make e2e-fast-area AREA=office-skills` - last
   20 lines showing the RED failures and their reasons (tool not registered /
   skill absent), confirming the tests fail for the RIGHT reason
3. **Deviations:** none / list with rationale

## Review Checklist (For Review Agent)

- [ ] Every F14a-d row in the Test Gate matrix maps to a scenario in the suite
- [ ] Tests assert the behavior contract (values, round-trips, gate message),
      not implementation details or the concrete library
- [ ] RED failures are missing-implementation (unregistered tool / absent
      skill), not harness/syntax
- [ ] `e2e/manifest.json` updated; suite uses the `e2e` build tag and scratch
      harness
- [ ] No unit test files added for the features (root e2e policy)
- [ ] No line-number corruption / debug artifacts

If the gate demands a library pick to compile, you DIVERGED from the contract
-surface design - the suite must be library-agnostic (report an OPEN QUESTION
instead of importing a document library).

## Notes

- Root AGENTS.md e2e policy and manifest shape are authoritative - read
  `e2e/manifest.json` + an existing suite before writing.
- The gate message must match the repo verbatim:
  `skill <name> requires unavailable tool(s): <list> - run 'meept doctor' to
  diagnose` (skills.md:84-89).
