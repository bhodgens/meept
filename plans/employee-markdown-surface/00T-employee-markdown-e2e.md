# 00T — Test Gate: Hermetic E2E for the Employee Markdown Surface

## Dispatch Instruction

Do NOT write implementation code. Do NOT commit. Do NOT run git add. Write the hermetic e2e suite ONLY. The suite must fail RED for missing-implementation reasons (the `internal/employeemd` package and the CLI `.md` dispatch do not exist yet). Report back: files written, and `make e2e-fast-area AREA=employee-markdown` results.

## Objective

Write the hermetic e2e suite that proves an `EMPLOYEE.md` file flows through `meept agents validate` and `meept agents create` to a persisted employee, against the e2e harness daemon. This is the gate for leaves 01 and 02.

## Files

- Create: `e2e/suites/employee-markdown/employee_markdown_test.go`
- Create: `e2e/suites/employee-markdown/fixtures/companion.md` (test fixture EMPLOYEE.md)
- Create: `e2e/suites/employee-markdown/proc_test.go` (minimal process helpers, copied from cli-surface if needed)
- Modify: `e2e/manifest.json` (add suite + scenarios + path_map entry)

## Required knowledge (read these first)

- Suite structure: `e2e/suites/cli-surface/cli_surface_test.go` (harness.Start, s.RunCLI).
- RPC employee-create: `e2e/suites/goals-plans/goals_plans_test.go:98` `rpcCreateEmployee` uses `client.CallResult("agents.create", map)` — mirror this pattern for a comparison baseline.
- Daemon client: `e2e/harness/clients.go` (how to dial the RPC socket and call methods).
- manifest format: `e2e/manifest.json` — copy the `cli-surface` suite block (around line 980) and the scenario objects (around line 225 and 2857); add a `path_map` row `e2e/suites/employee-markdown -> ["employee-markdown"]` and route `internal/employeemd/` to the suite too.

## Test cases (all hermetic, fake LLM, no network)

`go:build e2e` tag on every test file.

1. `TestValidateMarkdownDefinition` — write the fixture `companion.md` (frontmatter with a valid constitution + body prompt) into a temp file; run `meept agents validate <fixture>` via `s.RunCLI`; assert exit success and stdout contains `valid:` and the employee id.
2. `TestCreateEmployeeFromMarkdown` — `s := harness.Start(t)`; `s.RunCLI(..., "agents", "create", fixture)`; assert success. Then `s.RunCLI(..., "agents", "list")` contains the employee id. Then deep-check the persisted definition this way: dial the daemon client and call `agents.get` with the id, assert the returned constitution `purpose` equals the fixture's.
3. `TestCreateEmployeeFromMarkdownConstitution` — assert the serialized constitution round-trips (call `agents.get`, check `constitution.purpose`, `autonomy_tier`, a `constraints.risk_ceiling`).
4. `TestUpdateEmployeeFromMarkdown` — create from md, then `meept agents update <id> <second md fixture with same id>` and assert the prompt changed.

## Fixture `companion.md` contract (frozen — must match the 01 parser's handshake)

```markdown
---
id: companion
name: Companion
description: Conversational companion, read-only
memory_scope: private
tools: [memory_search, platform_status]
constraints:
  max_iterations: 4
  timeout_seconds: 300
  daily_budget_cents: 150
triggers:
  - type: webhook
    enabled: true
enabled: true
constitution:
  purpose: Keep the operator company and report on recent work
  role: Companion
  autonomy_tier: tier_2_propose
  escalates_to: [user]
  constraints:
    risk_ceiling: low
    max_tokens_per_turn: 800
---
You are Companion. Speak for the ear, short sentences, plain words.
```

Note for the gate test: the fixture lives in the suite dir and is read by the test (use `os.ReadFile` on its own path, e.g. relative to the test's package dir), then written to the daemon's temp home or a temp dir — copies the fixture into a temp file path that `meept agents create <path>` can read.

## Manifest wiring (exact)

In `e2e/manifest.json`:
- Add suites row: `{"id":"employee-markdown","dir":"e2e/suites/employee-markdown","tests":[...]}` — copy the cli-surface shape.
- Add path_map entries: `e2e/suites/employee-markdown -> ["employee-markdown"]` and `cmd/meept/ -> ["employee-markdown", ...existing cli-surface...]`.
- Add scenarios `employee-markdown-01` (validate), `employee-markdown-02` (create+get), `employee-markdown-03` (update).

## Self-Verification

- [] `gofmt -l e2e/suites/employee-markdown/` clean.
- [] `make e2e-fast-area AREA=employee-markdown` compiles and the three tests FAIL (RED) with missing-symbol/`no such table` errors that clearly trace to the absent parser / CLI dispatch — NOT to test bugs.
- [] manifest validates: `python3 -c "import json;json.load(open('e2e/manifest.json'))"`.

## Review Checklist

- Tests are hermetic (no real LLM, no internet, no port assumptions beyond the harness).
- RED failure is attributable to missing implementation, not a fixture/assertion bug.
- manifest.json is valid JSON and the path_map routes `internal/employeemd/`.
