# AI Employees

Meept's AI Employee framework is the structured-autonomy layer that sits on top of the existing persistent bot runtime. An employee is a bot with a **constitution**, a **goal loop**, and a **constitution enforcement engine** that gates every action the employee takes.

This document is the feature spec. The full design rationale lives in [`docs/superpowers/specs/2026-06-23-ai-employee-design.md`](../superpowers/specs/2026-06-23-ai-employee-design.md). The legacy bot framework doc is preserved in git history at `docs/workflows/bots.md`.

---

## Problem

Meept already had persistent bots (cron/webhook/bus triggered, budget-capped, memory isolated). What it lacked was:

- A **structured constitution** that binds an agent to a purpose, tier of autonomy, hard constraints, and amendment policy.
- A **goal loop** that decides what to do next based on the tier (reactive, propose, autonomous) rather than always running the same prompt.
- A **constitution enforcement engine** that checks actions at three checkpoints (pre-exec, post-turn, periodic) and auto-pauses on violations.
- An **escalation-aware authority model** so tier-2 employees route risky work to a human via the existing Plan signoff flow.

Employees add these primitives without duplicating the bot persistence layer. The `internal/employee/` package wraps `internal/bot/`; storage, triggers, and the runner stay shared.

---

## Concepts

### Constitution

A constitution is a structured document bound to one employee. It has four sections:

| Section | Purpose | Example |
|---------|---------|---------|
| **Identity** | purpose, role, free-form charter | "Keep CI green", "CI Reliability Engineer" |
| **Autonomy** | which tier the employee runs in | `tier_2_propose` |
| **Authority** | who the employee escalates to | `["user"]` |
| **Constraints** | machine-enforceable rules | `never: ["merge to main"]`, `risk_ceiling: medium` |
| **Amendment policy** | how the constitution itself can change | `frozen_fields: ["never", "risk_ceiling"]` |

Structured fields exist only where the engine can enforce them. Everything else goes in `charter`, a free-form markdown blob the LLM reads as part of its system prompt.

A constitution is **required** at load time. An employee without one refuses to start, with an error pointing to this document.

### Goal

A Goal is a long-lived mandate owned by an employee. "Keep CI green for main branch" is a Goal; "fix today's flaky test" is a Plan that serves the Goal.

- A Goal has health: `green`, `yellow` (at risk), `red` (broken), or `unknown`.
- Each Goal tracks its active Plan and plan history.
- Goals are stored in the `employee_goals` SQLite table alongside `bot_definitions`.

### GoalLoop

The GoalLoop is the per-tier runtime that decides what the employee does next. Three operations cycle:

```
ASSESS -> PLAN -> EXECUTE -> REFLECT
   ^                          |
   +--------------------------+
```

The loop is tier-aware. Everything else in the system (storage, triggering, signing) is tier-blind.

**Tier 1 (reactive):** Trigger-driven only. ASSESS on each trigger, EXECUTE the implicit single-step plan, REFLECT via post-turn audit. No self-enqueued work.

**Tier 2 (propose):** Scheduled ASSESS per `Constraints.AssessmentInterval`. The LLM proposes candidate Plans; each goes to `PendingApproval` and routes to the employee's `escalates_to` for signoff via the existing Plan workflow. Once approved, EXECUTE runs `BotRunner.Execute()` with the plan's prompt. REFLECT updates Goal health.

**Tier 3 (autonomous):** Phase 2, not wired. Same as tier 2 except Plans execute immediately after ASSESS — no `PendingApproval` stop. Only the constitution's authority boundaries and escalation triggers gate execution.

### Constitution Engine

Three checkpoints, each with a distinct role. All live in `internal/employee/enforcement.go`.

#### Checkpoint 1: Pre-execution gate

Runs **before every tool call**, inside the existing `SecurityEngine.Check()` flow. The employee package exposes a `PreExecChecker` that the security engine calls as a new stage between base rule lookup and the confirmation gate.

The checker evaluates, in order:

1. **`tools_allowed` / `tools_forbidden`** — allowlist/denylist of tool names.
2. **`risk_ceiling`** — hard upper bound on the risk returned by the engine.
3. **`escalation_triggers`** — matches against action/tool/details. On match, the action is queued as a Plan in `PendingApproval` routed to `escalates_to`; the current invocation returns without executing.
4. **`never[]`** — shell command scan, path scan where possible. Hard deny + employee auto-pause.
5. **Budget** — tokens today, dollars today, invocations today via `BotState`. Hard deny + auto-pause when exhausted.

Denial outcomes:

| Trigger | Outcome |
|---------|---------|
| `tools_forbidden` match | hard deny, audit event |
| `risk_ceiling` exceeded | hard deny, audit event, plan signoff required to override |
| `escalation_trigger` matched | escalation (not denial): action queued as PendingApproval Plan |
| `never` pattern match | hard deny, audit event at critical, **employee auto-pause** |
| budget exhausted | hard deny, audit event, **employee auto-pause** |

#### Checkpoint 2: Post-turn audit

Runs **after each LLM turn**. A small-model classifier scans the turn's tool calls and final output against the constitution.

Inputs to the classifier:

- The constitution's `never[]` rules.
- The charter's soft commitments (non-machine-enforceable).
- The turn's tool calls + arguments + results.
- The LLM's final output.

Output:

- `severity`: `info` | `warning` | `critical`
- `violated_rule`: reference into constitution
- `evidence`: the specific tool call or output span

Findings routing:

- `info` / `warning` -> write `AuditFinding` row, attach to owning Goal, no action.
- `critical` -> **auto-pause employee** + write finding + emit bus event `employee.paused` with reason.

#### Checkpoint 3: Periodic audit job

Runs on a schedule (default: every 6h, configurable per employee). Reviews the last N invocations in bulk. Same small-model classifier, different prompt: "here are the last N decisions this employee made. Are there patterns of drift from the constitution?"

Why a separate checkpoint: post-turn audit catches individual violations; periodic audit catches **slow drift** — the LLM gradually pushing boundaries across many turns.

Outputs:

- Findings with severity, same routing as Checkpoint 2.
- A `DriftScore` (0.0-1.0) attached to the employee, surfaced in `meept agents list` as a single number per employee.
- If drift score > threshold (default 0.3, configurable), employee is auto-paused even if no single finding was critical.

---

## Autonomy tiers

| Tier | Name | Behavior | Phase |
|------|------|----------|-------|
| 1 | `tier_1_reactive` | Trigger-only. No self-enqueued work. ASSESS + REFLECT only. | Phase 1 |
| 2 | `tier_2_propose` | Full cycle. Plans route to `escalates_to` for signoff. | Phase 1 |
| 3 | `tier_3_autonomous` | Full cycle, no approval stop. Constitution gates only. | Phase 2 |

Tier is a property of the constitution, not the agent. The same agent binary can be hired as different tiers in different employees.

---

## Authority model

Authority is **escalation-only**. An employee has an `escalates_to` list naming who signs off on its Plans. Entries are agent IDs or the literal string `"user"`.

- There is no delegation field. Delegation uses the existing `delegate_task` and `request_handoff` tools.
- Role evolution is propose-only: an employee may propose amendments to its own constitution via `meept agents amend`, but every amendment goes through Plan signoff. Frozen fields (e.g. `never`, `risk_ceiling`) cannot be amended even with approval.

---

## Enforcement checkpoints

The constitution engine is defense-in-depth. Each checkpoint catches a different failure mode:

| Checkpoint | Catches | Cost | Latency |
|------------|---------|------|---------|
| Pre-exec gate | Hard rule violations before they happen | Zero (no LLM call) | microseconds |
| Post-turn audit | Single-turn violations the rule matcher missed | One small-model call per turn | seconds |
| Periodic audit | Slow drift across many turns | One small-model call per N hours | seconds |

The engine is fail-safe: if the pre-exec checker throws (panic, unexpected type), the action is denied, the finding is logged at `critical`, and the employee is auto-paused. Better paused than rogue.

---

## Migration from legacy bots

`meept agents migrate` reads each existing bot's prompt via the small model and proposes a constitution:

- Default tier: `tier_1_reactive`.
- Default risk ceiling: `low`.
- Default `escalates_to`: `["user"]`.
- Synthesized `never[]` rules from the prompt.

Operators review and apply proposed constitutions:

```bash
meept agents migrate              # scan and propose
meept agents migrate --apply <id> # write proposed constitution to disk
```

For bots whose prompt is too vague to synthesize constraints, the migrate command writes a minimal conservative constitution and flags it for human review. It never refuses to migrate.

Legacy `meept bots` commands are removed. Scripts that call `meept bots` get a clear error pointing to `meept agents --help` and this document.

---

## CLI reference

The unified `meept agents` namespace replaces `meept bots`. Hard cutover.

### Lifecycle

```bash
meept agents list                              # all employees, status, tier, drift score
meept agents show <id>                         # full definition: constitution, state, goals, recent findings
meept agents create <definition.json5>         # validates constitution; refuses without one
meept agents update <id> <definition.json5>
meept agents delete <id>                       # stops + deletes; confirms unless --force
meept agents pause <id>                        # operator pause
meept agents resume <id>                       # operator resume (only un-pause path)
meept agents amend <id> --field=<key> <value>  # propose constitution amendment (routes to Plan signoff)
```

### Migration

```bash
meept agents migrate                           # scans ~/.meept/bots/*.json
meept agents migrate --apply <id>              # write proposed constitution to disk
```

### Goals

```bash
meept agents goals [--employee=<id>]           # list goals with health (red/yellow/green)
meept agents goal <goal-id>                    # goal detail + active plan + history
meept agents goal <goal-id> --approve <plan-id>
meept agents goal <goal-id> --reject <plan-id> --reason="..."
```

### Audit

```bash
meept agents audit <id> [--since=<dur>]        # recent findings, severity, resolution
meept agents audit <id> --resolve <finding-id> --as=false_positive
```

UI text is lowercase per CLAUDE.md convention (e.g., "approve", "reject", "pause", "resume").

---

## TUI

Bubbletea panel, accessed via `ctl-x e` (new keybinding, alongside existing `ctl-x o` for MCP).

Panels:

- **agents list** — ID, status, tier, drift score, daily cost, last invocation. Up/down to navigate, enter to drill in.
- **agent detail** — constitution summary, active goals with health bars, recent findings count by severity. Tabs for "constitution / goals / audit / state".
- **approval queue** — tier-2 Plans awaiting signoff for the current user (when `escalates_to` includes `"user"`). Approve/reject inline. Same surface as Plan approvals.
- **audit findings** — severity-colored list. Resolve-as-false-positive inline.

---

## HTTP API

New endpoints under `/api/v1/agents/*`. The existing `/api/v1/bot/{id}/trigger` moves to `/api/v1/agents/{id}/trigger` (webhook callers updated in the POC).

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/agents` | GET | list employees |
| `/api/v1/agents` | POST | create employee (validates constitution) |
| `/api/v1/agents/{id}` | GET, PATCH, DELETE | show, update, delete |
| `/api/v1/agents/{id}/trigger` | POST | webhook trigger (existing semantics) |
| `/api/v1/agents/{id}/pause` | POST | operator pause |
| `/api/v1/agents/{id}/resume` | POST | operator resume |
| `/api/v1/agents/{id}/constitution` | GET, PATCH | view / propose amendment |
| `/api/v1/agents/{id}/goals` | GET | list goals with health |
| `/api/v1/agents/{id}/goals/{gid}` | GET | goal detail |
| `/api/v1/agents/{id}/audit` | GET | findings, filterable by `?since=&severity=` |
| `/api/v1/agents/{id}/audit/{fid}/resolve` | POST | resolve finding |
| `/api/v1/agents/migrate` | POST | run migration scan, returns proposed constitutions |

Authenticated via the existing API key mechanism when `require_auth: true`.

---

## RPC API

New methods under the `agents.` namespace. Existing `bot.*` methods are removed (hard cutover).

| Method | Notes |
|--------|-------|
| `agents.list`, `agents.get`, `agents.create`, `agents.update`, `agents.delete` | direct ports of existing `bot.*` lifecycle |
| `agents.pause`, `agents.resume` | direct ports |
| `agents.trigger` | programmatic trigger (used internally by webhook handler) |
| `agents.amend` | propose constitution amendment, routes to Plan signoff |
| `agents.goals.list`, `agents.goals.get` | goal listing |
| `agents.goals.approve`, `agents.goals.reject` | plan signoff |
| `agents.audit.list`, `agents.audit.resolve` | findings |
| `agents.migrate` | migration scan |

Service layer: new `EmployeeService` in `internal/services/` (fits the existing service-registry pattern). Bot-prefixed services are removed.

---

## Flutter (menubar)

New "agents" tab in the menubar app, replacing the "bots" surface.

- Cards for each employee: ID, tier badge, health dot, drift score, today's cost, recent findings count.
- Tap to open detail view: constitution summary, goals, approve/reject inline for pending Plans.
- Pause/resume button per agent.

---

## Configuration

The `employees` block in `~/.meept/meept.json5`:

```json5
{
  employees: {
    enabled: true,
    audit: {
      model: "small",                  // alias from config/models.json5
      periodic_interval: "6h",         // global default
      drift_pause_threshold: 0.3,
      findings_retention_days: 90,
    },
    auto_pause: {
      on_critical_finding: true,
      on_drift: true,
      on_never_violation: true,
      require_operator_resume: true,   // employee can't self-resume
    },
  },
}
```

Defaults are applied by `config.Default()`; users only need to override fields they want to change.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `true` | Turns on the employee layer. When `false`, the legacy bot runtime is used as-is. |
| `audit.model` | `"small"` | Model alias for post-turn and periodic audits. |
| `audit.periodic_interval` | `"6h"` | Global default cadence for the periodic bulk audit. |
| `audit.drift_pause_threshold` | `0.3` | Drift score (0.0-1.0) above which the periodic auditor auto-pauses. |
| `audit.findings_retention_days` | `90` | How long findings are kept before pruning. |
| `auto_pause.on_critical_finding` | `true` | Pause on post-turn or periodic critical findings. |
| `auto_pause.on_drift` | `true` | Pause when drift score exceeds threshold. |
| `auto_pause.on_never_violation` | `true` | Pause on `never` rule violations. |
| `auto_pause.require_operator_resume` | `true` | Prevent self-resume after auto-pause. |

---

## Audit findings

Audit findings are stored in the `employee_audit_findings` SQLite table:

| Column | Description |
|--------|-------------|
| `id` | Finding ID |
| `employee_id` | Owning employee |
| `goal_id` | Related goal (optional) |
| `plan_id` | Related plan (optional) |
| `turn_id` | Turn that produced the finding (optional) |
| `severity` | `info` / `warning` / `critical` |
| `checkpoint` | `pre_exec` / `post_turn` / `periodic` |
| `violated_rule` | Reference into constitution (e.g. `never[2]`) |
| `evidence` | JSON: tool call, output span, etc. |
| `detected_at` | Timestamp |
| `resolved_at` | Resolution timestamp (nullable) |
| `resolution` | `false_positive` / `acknowledged` / `constitution_amended` |

Resolve findings via:

```bash
meept agents audit <id> --resolve <finding-id> --as=false_positive
```

---

## POC walkthrough: `ci-monitor`

The reference employee ships at [`config/employees/ci-monitor.json5`](../../config/employees/ci-monitor.json5). It demonstrates the full tier-2 lifecycle.

### Constitution summary

| Field | Value |
|-------|-------|
| Purpose | Keep CI green for main branch |
| Role | CI Reliability Engineer |
| Tier | `tier_2_propose` |
| Risk ceiling | `medium` |
| `tools_allowed` | `web_fetch`, `shell_execute` |
| `never` | merge to main, delete branches, force push |
| Assessment interval | `15m` |
| Budget | $0.50/day, 100 invocations/day |
| Escalates to | `user` |

### Day in the life

1. **Trigger:** GitHub webhook fires on push to main. The existing bus_event trigger wakes the employee.
2. **ASSESS:** Employee fetches CI status via `web_fetch`, identifies failures.
3. **PLAN:** Employee proposes a Plan ("investigate failing job X, open issue with logs") in `PendingApproval`.
4. **APPROVE:** User approves via TUI (`ctl-x e`), CLI (`meept agents goal <id> --approve <plan-id>`), or HTTP (`POST /api/v1/agents/.../approve`).
5. **EXECUTE:** Employee runs the investigation, stores findings in memory.
6. **REFLECT:** Employee updates Goal health: `green` if CI green, `yellow` if flaky, `red` if broken.

### What the enforcement engine does

- **Pre-exec:** Blocks any tool call not in `tools_allowed`. Blocks any action whose risk exceeds `medium`. Queues critical-risk actions as Plans for user signoff. Scans shell commands for `never` patterns (e.g. `git push --force`, `git merge`).
- **Post-turn:** Small-model audit checks the turn's output against the charter. If the employee tried to merge or delete a branch, the finding is `critical` and the employee auto-pauses.
- **Periodic (every 15m per `assessment_interval`):** Bulk audit reviews the last N invocations. If the employee's tone or approach is drifting from "investigate and propose" toward "fix and commit directly", drift score rises. Above 0.3, auto-pause.

---

## Error handling

### Employee runtime failures

- LLM call fails -> BotRunner's existing retry path. Exhausted retries mark the turn failed; Goal health decays. N consecutive failures (default 3) -> auto-pause with `last_error` populated.
- Tool call fails -> existing tool error propagation. The REFLECT step sees the error and decides whether to retry, escalate, or give up.
- ASSESS produces invalid JSON -> fall back to tier-1 behavior for that turn, log audit finding at `warning`, continue.
- Plan approval times out (default 7d) -> Plan auto-rejected, Goal marked `at_risk`, finding written.

### Constitution failures

- Constitution fails validation at load -> employee refuses to start, error names the failing field. Other employees unaffected.
- Unknown tool in `tools_allowed` -> warning at load, tool removed. Employee starts flagged for review.
- Unknown agent ID in `escalates_to` -> hard fail at load.
- Amendment hits a `frozen_fields` violation -> rejected immediately, audit finding at `info`, no Plan created.

### Enforcement failures

- Pre-exec checker throws -> fail-safe: deny, audit at `critical`, auto-pause.
- Post-turn audit LLM fails -> log warning, skip audit for that turn, no pause.
- Periodic audit fails three times in a row -> write `critical` finding, no auto-pause (auditor being down is not evidence of employee misbehavior). Continue retrying with backoff.
- Audit model produces unparseable output -> retry once with stricter prompt. If still unparseable, skip the turn, log `warning`.

---

## Architecture

The employee layer wraps the existing bot framework without duplicating it:

```
User / Trigger / Timer
    |
    v
internal/employee/GoalLoop
    |
    +--> Constitution (decides what is allowed this turn)
    |       |
    |       v
    |     Enforcement (pre/exec/post gates)
    |       |
    |       v
    +--> internal/bot/Runner (executes one invocation)
    |       |
    |       v
    |     AgentLoop.RunOnce()
    |       |
    |       v
    |     Tools + LLM
    |
    +--> Plan signoff (existing, reused for approvals)
    |
    +--> Goal -> Plan (Goal owns Plans)
```

### Package layout

```
internal/employee/
  constitution.go    Constitution struct + validation + loader
  goal.go            Goal struct, GoalStore (SQLite)
  goal_loop.go       Per-tier runtime: Decide(), Execute(), Reflect()
  enforcement.go     Pre-exec gate, post-turn audit, periodic auditor
  authority.go       Escalation resolution, delegate routing
  manager.go         Lifecycle: Hire, Retire, Review, AmendConstitution
  wiring.go          Daemon wiring (registers with daemon components)
  handler.go         RPC handlers
  api_handlers.go    HTTP handlers (/api/v1/agents/*)
  types.go           Employee (= BotDefinition + Constitution wrapper)
```

### Data model

Three SQLite tables:

- `bot_definitions` (existing, extended with constitution JSON in `data` column)
- `employee_goals` (new)
- `employee_audit_findings` (new)

The `bot_states` table is reused as-is for runtime counters (runs, costs, failures, status).

---

## Relationship to existing agents

The existing multi-agent system (dispatcher, coder, planner, etc.) handles interactive, user-driven tasks. Employees handle **persistent, autonomous, constitution-bound** work. The two systems are complementary:

- An employee uses the same `AgentLoop.RunOnce()` and the same tools as any other agent.
- An employee can delegate to specialist agents via `delegate_task` and `request_handoff`.
- The dispatcher does not route user requests to employees; employees are triggered by cron/webhook/bus.
- Employees show up in `platform_agents` so other agents can discover and delegate to them.

See [Multi-Agent System](../concepts/multi-agent.md#employees) for details.

---

## See also

- [AI Employee Design spec](../superpowers/specs/2026-06-23-ai-employee-design.md) - Full design rationale and decisions table.
- [Multi-Agent System](../concepts/multi-agent.md) - How employees relate to dispatcher, executors, and reviewers.
- [Architecture](../concepts/architecture.md) - Where GoalLoop + Constitution Engine fit in the system diagram.
- [Plans](plans.md) - Plan signoff workflow reused for tier-2 approvals.
- [Job Scheduling](job-scheduling.md) - Scheduler that drives the GoalLoop.
- [Security Engine](security.md) - Where the pre-exec gate hooks in.
- [HTTP API](../reference/http-api.md) - REST endpoint reference.
- [CLI Reference](../reference/cli.md) - `meept agents` command reference.
