# Master: Employee Markdown Authoring Surface

> Plan tree. Implement with the hierarchical-planning + subagent-driven-development skills. Orchestrator (main model) reviews every leaf in-session; leaf agents never commit.

## Goal

Give employees/bots a second authoring surface: a markdown definition file (`EMPLOYEE.md`, YAML frontmatter + markdown body) in the same style as the existing `AGENT.md` agent definitions. The SQLite DB remains the store for both the parsed definition blob and all runtime state (constitution, goals, audit, messages). The markdown file is the human-authored configuration input; `agents.create` / `agents.validate` / `agents.update` already persist to the DB unchanged.

## Why (architecture rationale)

- Agents (`internal/agents/discovery.go`) are static role definitions discovered from `AGENT.md` on disk — markdown is the right fit.
- Employees are persistent, mutable runtime records backed by `internal/bot`; their state (constitution, goals, audit findings, messages) must live in SQLite for atomicity, FK integrity, indexed queries, and the tamper-evident audit chain.
- These are two different persistence models, deliberately. This plan adds a markdown *input* format for employees so operators can hand-author employee config like they hand-author agents — without moving employee *runtime state* out of the DB.
- **No DB schema change.** `HireRequest` (internal/employee/manager.go:66) and `createRequest` (handler.go:209) already accept the bot-definition shape + a `constitution` map. The new parser emits exactly that payload; the existing `agents.create` RPC, `Manager.Hire`, bot store, and constitution/goal stores are untouched.
- **No daemon/API change.** `meept agents create <file>` currently reads JSON5 via `readDefinitionFile` (cmd/meept/agents_cmd.go:219) into a `map[string]any` and sends it over RPC. The CLI dispatch gains a markdown branch that returns the same map.

## Interface Contract (frozen — all leaves build to this)

### File convention
- Extension `.md` (and `.markdown`) selects the markdown path; `.json5`/`.json` keeps the existing path. Dispatch by file extension, no flags.
- File location is caller's choice (e.g. `~/.meept/employees/secretary.md` or repo `config/employees/`). This plan does not add auto-discovery; files are parsed on demand by `create`/`update`/`validate`.

### EMPLOYEE.md frontmatter schema (YAML, key names fixed)
Top-level keys (all optional except `id`):

| Key | Type | Maps to |
|-----|------|---------|
| `id` | string | required; `createRequest.ID` / `HireRequest.ID` |
| `name` | string | `Name` |
| `description` | string | `Description` |
| `model` | string | `Model` |
| `memory_scope` | string `private\|shared\|read_only` | `MemoryScope` |
| `tools` | list[string] | `Tools` |
| `additional_tools` | list[string] (agent-style alias, optional) | merged into `Tools` |
| `constraints` | map | `Constraints` → `BotConstraints` |
| `triggers` | list of trigger maps `{type, schedule?, topic?, prompt_template?, enabled}` | `Triggers` → `[]bot.BotTrigger` |
| `enabled` | bool | `Enabled` |
| `executor_agent` | string (optional) | `ExecutorAgent` |
| `constitution` | map (nested, same shape as JSON5 constitution) | `Constitution map[string]any` (passed through verbatim) |
| `goals` | list of goal maps (optional) | `Goals []GoalSpec` (passed through) |

`constraints` sub-keys (all optional): `max_iterations` int, `timeout_seconds` int, `max_tokens_per_turn` int, `daily_budget_cents` int, `max_invocations_per_day` int → `BotConstraints{MaxIterations, Timeout(time.Duration), MaxTokensPerTurn, DailyBudgetCents, MaxInvocationsPerDay}`. Note `Timeout` is a `time.Duration` in the JSON blob; the parser converts `timeout_seconds` int → `time.Duration(seconds)*time.Second`.

#### Agent-style top-level aliases (optional, folded only when the canonical key is absent)
- `max_iterations` (int) → `constraints.max_iterations`
- `timeout_seconds` (int) → `constraints.timeout_seconds`

### Body → prompt
The entire markdown body after the closing `---` frontmatter delimiter becomes `createRequest.Prompt` / `HireRequest.Prompt`. Trim leading/trailing blank lines; preserve internal markdown verbatim.

### Output of the parser
A `map[string]any` whose JSON encoding is byte-compatible with what `readDefinitionFile` currently produces for an equivalent JSON5 file, so the existing `agents.create` call works unchanged. Unknown top-level frontmatter keys are preserved in the output map (forward-compatible pass-through; `createRequest.RawDefinition` already tolerates extras).

### Extra guard
Constitution decode/validation behavior must be byte-identical to the existing path: the parser does NOT validate the constitution; `Manager.Hire` (via the RPC handler) and `agents validate` (cmd/meept/agents_validate.go) already do.

## Child Index

| Leaf | Scope | Files touched | Depends on |
|------|-------|---------------|------------|
| `00T-employee-markdown-e2e.md` | Hermetic e2e suite proving `agents create`/`validate`/`update` accept `.md` | `e2e/suites/employee-markdown/` (new), `e2e/manifest.json`, fixture `EMPLOYEE.md` | none (writes tests first) |
| `01-employee-md-parser.md` | New `internal/employeemd` package: frontmatter+body → agents.create payload map | `internal/employeemd/md.go` (new) | 00T (gate) |
| `02-cli-wiring.md` | Dispatch `.md` in `agents create`/`validate`/`update` | `cmd/meept/agents_cmd.go`, `cmd/meept/agents_validate.go` | 01 |
| `03-docs-template.md` | docs + sample `EMPLOYEE.md` template | `docs/workflows/employees.md`, `config/employees/example-employee.md` (new) | 01 |

## Completion Tracking Table

| Leaf | Status |
|------|--------|
| 00T-employee-markdown-e2e | PENDING |
| 01-employee-md-parser | PENDING |
| 02-cli-wiring | PENDING |
| 03-docs-template | PENDING |

## Coding Conventions

- `gofmt` on every changed Go file.
- Explicit `json:"..."` tags on any struct crossing JSON.
- Two-value map assertions (`v, ok := m[k]`); never single-value.
- No ignored errors; no `panic(err)`; no `time.Now().UnixNano()` id generation.
- Prefer clean architectural fixes over workarounds; no `//nolint` to silence real findings.
- Plain-language prose; hyphens not em-dashes in docs.

## Review Checklist

- Verify every leaf's output on disk (files exist, symbols resolvable) before trusting a subagent report.
- Run the build and the 00T e2e gate yourself in-session; a green test is the bar, not a subagent claim.
- Confirm no DB schema change (git diff on `internal/employee/manager.go` and `internal/bot/*.go` should show none from this tree).
- Confirm the json5 path is unchanged (only `.md`/`.markdown` routes through the new parser).
- No stray artifacts: grep for TODO/FIXME/debug prints in changed files.

## Dispatch Protocol

Every implementation leaf agent is briefed: "Do NOT run git commit. Do NOT run git add. Write code, run tests, report results only. The orchestrator stages and commits." Go leaves must follow repo conventions: gofmt, explicit json tags where structs cross JSON, no `time.Now().UnixNano()` id generation, two-value map assertions, no ignored errors. Verify commands are pinned in each leaf.

## Test Gate (code branch — mandatory, Step 5.5)

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| md parser maps frontmatter→payload | — (policy: no new unit files; covered via e2e) | `00T` proves parse→create→list round-trip | TESTS_IN_PROGRESS → VALIDATED → green after 01+02 |
| CLI `.md` dispatch | — | `00T` `agents validate`/`create`/`show`/`update` on `.md` | see 00T |
| constitution passed through | — | `00T` asserts serialized constitution | see 00T |

Dispatch order: `00T` (writes e2e tests, RED for missing parser) → validate tests in-session → commit gate tests → `01` (parser turns gate green) → `02` (CLI) → `03` (docs). `01` and `02` are sequential (02 imports 01). `03` can run after 01.

## Integration Test Plan

After all leaves: run `make e2e-fast-area AREA=employee-markdown`, `gofmt -l` on changed Go, and `go build ./...`. Verify no DB schema change (`git diff` on `internal/employee/manager.go` `/internal/bot/*.go` shows none from this tree). Confirm `agents validate secretary.md` and `agents create secretary.md` both work against a local daemon. Merge only via the plan branch per the isolation-worktree rule.
