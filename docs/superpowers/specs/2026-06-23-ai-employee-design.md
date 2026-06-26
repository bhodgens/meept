# AI Employee Design

**Status:** Approved (brainstorm)
**Date:** 2026-06-23
**Author:** Meept (via brainstorming with operator)

## Problem

Meept today has agents (specialist executors), bots (persistent autonomous agents with triggers, budgets, memory isolation), a self-improvement system, a Q meta-agent, and a plan signoff workflow. What it lacks is the notion of an "AI employee": a persistent agent that executes according to a structured constitution, an escalation-aware authority model, an autonomous goal loop with tiered autonomy, and an audit layer that enforces the constitution at runtime.

This spec adds four primitives on top of the existing bot framework:

1. **Constitution** — structured metadata bound to one specific employee: purpose, autonomy tier, authority, hard constraints, amendment policy.
2. **Goal** — a long-lived mandate owned by an employee ("keep CI green") that spawns `Plan`s over time.
3. **GoalLoop** — a per-tier runtime that decides what to do next (assess → plan → execute → reflect), gated by tier.
4. **Constitution Engine** — three-checkpoint enforcement (pre-exec gate, post-turn audit, periodic audit).

## Scope

**In scope (phase 1):**
- Constitution data model and validation
- Goal data model and storage
- GoalLoop for tiers 1 (reactive) and 2 (propose → approve → execute)
- Constitution enforcement at three checkpoints
- Hard cutover migration of `internal/bot/` to `internal/employee/`
- Unified `meept agents` CLI namespace
- TUI / HTTP / RPC / Flutter wiring
- Reference POC: `ci-monitor` employee

**Out of scope (phase 2):**
- Tier 3 (full autonomy within constitution) — data model supports it, runtime not wired
- Cross-employee delegation via Plan signoff chains (delegation uses existing `delegate_task` / `request_handoff`)
- Network-level controls beyond existing path fencing
- A "constitutional language model" — we use the same `llm.Chatter` interface and providers, just different prompts

## Use cases

The primitives are designed to be platform building blocks. Common configurations:

- **Service owner** — tier 2 employee with cron + webhook triggers, owns an ongoing responsibility ("keep CI green"). Strong fit for goal loop + constitution.
- **Personal AI employee** — tier 2 employee with `escalates_to: ["user"]`, acts on a specific user's behalf, knows their preferences. Strong fit for authority model + role evolution.
- **Platform primitive for user-built employees** — users author their own employees via JSON definitions; Meept enforces the constitution.
- **Internal Meept self-management** (follow-on) — existing `selfimprove` and `q_agent` reorganize themselves as employees. Not in this spec.

A single set of primitives covers all of these. The differences are configuration.

## Decisions

| Area | Decision |
|---|---|
| Scope | Platform primitive subsuming service-owner, personal employee, and (phase 2) internal self-management |
| Autonomy | Tiered (1 reactive / 2 propose / 3 autonomous), tightly bound per-agent; phase 1 ships tiers 1 + 2 |
| Constitution | Structured core + free-form charter |
| Authority | Escalation-only model (`escalates_to`); delegation via existing `delegate_task`/`request_handoff` |
| Goal model | Layered — `Goal` owns `Plan`s |
| Migration | In-place evolution of `BotDefinition`; constitution required at load |
| Legacy bots | Refuse to load without constitution; `meept agents migrate` synthesizes conservative defaults |
| Approval | Reuse existing `Plan` signoff |
| Storage | Extend `bot_definitions` table; new `employee_goals` and `employee_audit_findings` tables |
| Enforcement | Pre-exec gate + post-turn audit + periodic audit (small model, configurable) |
| Audit output | Critical → auto-pause + finding; non-critical → finding only |
| CLI | Unified `meept agents` namespace, hard cutover, docs updated |
| Role evolution | Propose-only, human approves via Plan signoff |

## Architecture

Four new concepts layered on top of the existing bot framework:

| Concept | What it is | Where it lives |
|---|---|---|
| **Constitution** | Structured metadata bound to one employee — purpose, tier, authority, escalation, hard constraints | New `internal/employee/` package wrapping `internal/bot/` |
| **Goal** | A long-lived mandate owned by an employee ("keep CI green") that spawns `Plan`s over time | New `internal/employee/goal.go`, new `employee_goals` table |
| **GoalLoop** | A per-tier runtime: tier 1 = triggers only; tier 2 = monitor → propose → approval → execute → reflect; tier 3 = self-enqueue (phase 2) | New `internal/employee/goal_loop.go` |
| **Constitution Engine** | Enforces constitution at three checkpoints: pre-exec tool gate, post-turn output audit, periodic scheduled audit | New `internal/employee/enforcement.go`, extends `internal/security/` |

The existing `internal/bot/` package stays as the persistence + execution layer. Bots continue to be the storage shape and runtime executor. The new `internal/employee/` package layers semantics on top: constitution, goal loop, enforcement engine, and the richer CLI.

```
User / Trigger / Timer
    ↓
internal/employee/GoalLoop      ← new
    │
    ├─► Constitution             ← new (decides what's allowed this turn)
    │     ↓
    │   Enforcement              ← new (pre/exec/post gates)
    │     ↓
    ├─► internal/bot/Runner      ← existing (executes one invocation)
    │     │
    │     ↓
    │   AgentLoop.RunOnce()
    │     │
    │     ↓
    │   Tools + LLM
    │
    ├─► Plan signoff             ← existing, reused for approvals
    │
    └─► Goal → Plan              ← new (Goal owns Plans)
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

### Why this shape

- `internal/bot/` stays untouched at the storage layer — no parallel system, no schema duplication.
- The constitution is a struct, not a string. The security engine reads structured fields; the LLM reads a synthesized system prompt that includes both structured fields and the free-form charter.
- The GoalLoop is the only piece that *needs* to be tier-aware; everything else is tier-blind. This keeps tier 3 (phase 2) a localized change.

## Constitution schema

Stored as JSON inside the existing `bot_definitions.data` column alongside the existing `BotDefinition` fields.

```go
// internal/employee/constitution.go
type Constitution struct {
    // Identity
    Purpose       string       `json:"purpose"`        // 1-sentence "why this employee exists"
    Role          string       `json:"role"`           // e.g. "CI Reliability Engineer"
    Charter       string       `json:"charter"`        // free-form markdown for nuance/values/tone

    // Autonomy
    AutonomyTier  AutonomyTier `json:"autonomy_tier"` // tier_1_reactive | tier_2_propose | tier_3_autonomous

    // Authority
    EscalatesTo   []string     `json:"escalates_to"`   // agent IDs or "user"; empty = no escalation path
    // (Delegation uses existing delegate_task/request_handoff — no new field)

    // Hard constraints (machine-enforced)
    Constraints   ConstitutionalConstraints `json:"constraints"`

    // Self-modification
    AmendmentPolicy AmendmentPolicy `json:"amendment_policy"`

    // Provenance
    Version       int          `json:"version"`        // bumped on each approved amendment
    AuthoredBy    string       `json:"authored_by"`    // "user" | agent ID that proposed
    ApprovedAt    time.Time    `json:"approved_at"`
}

type AutonomyTier int
const (
    Tier1Reactive AutonomyTier = iota  // triggers only, no self-enqueued work
    Tier2Propose                       // monitor → propose → approval → execute
    Tier3Autonomous                    // self-enqueue within constitution (phase 2)
)

type ConstitutionalConstraints struct {
    // Tool gating (structured — read by pre-exec gate)
    ToolsAllowed    []string `json:"tools_allowed"`     // allowlist; empty = inherit default toolset
    ToolsForbidden  []string `json:"tools_forbidden"`   // denylist; applied after allowlist
    RiskCeiling     string   `json:"risk_ceiling"`      // safe | low | medium | high | critical

    // Resource envelope (accountability)
    MaxTokensPerTurn      int `json:"max_tokens_per_turn"`
    MaxConversationTokens int `json:"max_conversation_tokens"`
    DailyBudgetCents      int `json:"daily_budget_cents"`
    MaxInvocationsPerDay  int `json:"max_invocations_per_day"`

    // Escalation triggers (when MUST this employee escalate?)
    EscalationTriggers []EscalationTrigger `json:"escalation_triggers"`

    // Hard "never" rules — machine-checked where possible, audited otherwise
    Never []string `json:"never"`

    // Assessment cadence (for tier 2+ GoalLoop)
    AssessmentInterval string `json:"assessment_interval"` // "15m", "1h", "*/30 * * * *"; empty = no scheduled assess
}

type EscalationTrigger struct {
    On      EscalationOn `json:"on"`       // risk_level | tool | action | cost
    Match   string       `json:"match"`    // e.g. "critical", "shell_execute", "file_delete"
    Reason  string       `json:"reason"`   // human/LLM-readable explanation
}

type AmendmentPolicy struct {
    SelfProposeAllowed bool     `json:"self_propose_allowed"`
    RequiresApproval   bool     `json:"requires_approval"`     // always true per design, but explicit
    FrozenFields       []string `json:"frozen_fields"`         // can't be amended even with approval,
                                                              // e.g. ["never", "risk_ceiling"]
}
```

### How the LLM consumes it

`enforcement.go` produces a `SynthesizedPrompt()` that joins:

1. The structured constraints (rendered as markdown rules — "you may never: ...")
2. The free-form charter
3. A short header with purpose, role, autonomy tier, escalation policy
4. Existing prompt-builder output (memory, skills, project context)

This synthesized prompt is what `internal/bot/runner.go` injects as the system prompt.

### How the security engine consumes it

`Constraints` are the machine-enforceable subset:

- `ToolsAllowed`/`ToolsForbidden` → applied by the existing `SecurityEngine.Check()` tool-gating stage.
- `RiskCeiling` → enforced as a hard upper bound on the risk returned by the engine.
- `EscalationTriggers` → pre-exec gate compares each tool call against triggers; on match, calls Plan signoff flow with the matching `EscalatesTo` approver.
- `Never[]` → pre-exec attempts string/pattern match where possible (e.g. shell command scan); post-turn audit always scans for Never violations in LLM output.

### Design rationale

- Structured fields exist *only* where the engine can actually enforce them. Everything else goes in `Charter`. No fake-machine-readable fields that are really just LLM hints.
- `Never[]` is deliberately a list of strings, not structured predicates — matches real constitutions ("we don't do X") and matches what the audit LLM is good at scanning for. Where it can be structurally enforced (e.g. `risk_ceiling`), we also do that.
- `AmendmentPolicy.FrozenFields` is the "values lock" — the human can pin specific fields as unamendable even if the rest of the constitution is revisable.

### Constitution required

A constitution is required at load time. An employee without one fails to start with an error pointing to `docs/workflows/employees.md`.

`meept agents migrate` reads each existing bot's prompt via the small model and proposes a constitution (`tier_1_reactive`, `risk_ceiling: low`, `escalates_to: ["user"]`, `never: [...]`). Operator reviews and applies. Nothing runs unconstrained.

For bots whose prompt is too vague to synthesize constraints, the migrate command writes a minimal conservative constitution and flags for human review. It never refuses to migrate.

## Goal model and GoalLoop

The Goal is the long-lived mandate; Plans are the concrete iterations. The GoalLoop is the per-tier runtime that decides what to do.

### Goal data model

```go
// internal/employee/goal.go
type Goal struct {
    ID           string         `json:"id"`              // pkg/id.Generate()
    EmployeeID   string         `json:"employee_id"`     // owning agent
    Title        string         `json:"title"`           // "keep CI green for main branch"
    Mandate      string         `json:"mandate"`         // the stable objective, in plain prose
    State        GoalState      `json:"state"`
    CreatedAt    time.Time      `json:"created_at"`

    // For tier-2+ goals: what triggered this goal's existence
    Source       GoalSource     `json:"source"`          // user | trigger | self_proposed | audit_finding
    TriggerRef   string         `json:"trigger_ref,omitempty"`

    // Health — how well is the mandate being met?
    Health       GoalHealth     `json:"health"`
    LastAssessed time.Time      `json:"last_assessed"`

    // Plans spawned to pursue this goal
    ActivePlanID string         `json:"active_plan_id,omitempty"`
    PlanHistory  []string       `json:"plan_history"`
}

type GoalState int
const (
    GoalActive   GoalState = iota  // pursuing mandate
    GoalPaused                      // operator or amendment paused
    GoalRetired                     // no longer relevant
)

type GoalHealth int
const (
    GoalHealthy   GoalHealth = iota  // last assessment: mandate satisfied
    GoalAtRisk                        // warning signs
    GoalBroken                        // mandate violated
    GoalUnknown                       // not yet assessed
)
```

**Storage:** new `employee_goals` SQLite table alongside `bot_definitions`. The Goal store follows the existing `internal/bot/store.go` pattern (one store per table, atomic writes, soft-delete via `retired_at`).

### GoalLoop state machine

One loop per employee, driven by the existing scheduler. Three operations:

```
ASSESS  →  PLAN  →  EXECUTE  →  REFLECT
   ↑                                  │
   └──────────────────────────────────┘
```

**Tier 1 (reactive)** — only ASSESS + REFLECT:
- ASSESS: triggered by cron/webhook/bus. Reads current state, invokes the LLM with "given this trigger, what should you do?". The LLM's response becomes an implicit Plan (single-step).
- EXECUTE: existing `BotRunner.Execute()` path, unchanged.
- REFLECT: post-turn audit runs (see Constitution Engine). Writes Goal health if relevant.

**Tier 2 (propose)** — full cycle:
- ASSESS: scheduled (per `Constraints.AssessmentInterval`). Reads domain state, asks LLM "are there issues worth addressing?". If yes, produces one or more candidate Plans.
- PLAN: each candidate becomes a `Plan` in `PendingApproval`. Routes to the employee's `escalates_to` for signoff via the existing Plan signoff workflow.
- EXECUTE: when a Plan is approved, GoalLoop triggers `BotRunner.Execute()` with the plan's prompt. Active plan ID recorded on the Goal.
- REFLECT: post-execution, asks LLM "did this help?". Updates `Goal.Health`. Failed executions mark the goal `at_risk` or `broken` after N consecutive failures (configurable, default 3).

**Tier 3 (autonomous)** — phase 2. Same as tier 2 except:
- No `PendingApproval` stop — Plans execute immediately after ASSESS.
- Authority boundaries and `escalation_triggers` in the constitution are the only gates.

### What's NOT new

- The LLM call inside ASSESS uses the existing `AgentLoop.RunOnce()` — no new inference path.
- Plan creation uses the existing `internal/plan/` package — same state machine, same signoff.
- Approval routing uses the existing Plan signoff flow — `escalates_to` just specifies who signs off.
- Trigger wiring uses the existing `internal/bot/router.go` cron/webhook/bus subscriptions.

### Genuinely new machinery

1. The Goal store + lifecycle.
2. The GoalLoop driver (a scheduler job that calls Assess/Plan/Execute/Reflect per employee on a per-employee schedule).
3. Tier-aware decision logic inside GoalLoop (a switch on `Constitution.AutonomyTier`).

### Schedule

Each employee declares its own assessment cadence via `Constraints.AssessmentInterval`. The scheduler spawns one `JobTypeAgent` job per employee per interval. Tier 1 employees use this for trigger-driven invocations; tier 2+ employees use it for ASSESS.

### Design rationale

- **Goal ≠ Plan because responsibilities persist, tasks don't.** "Keep CI green" is a Goal that lives for months. "Fix the flaky test from today's run" is a Plan that lives for an afternoon. Layering keeps the Plan machinery unchanged.
- **Health is the executive summary.** Operators see `meept agents goals --employee=ci-monitor` and get a list of active mandates with red/yellow/green. This is the accountability surface.
- **Tier logic is isolated.** Every tier-blind piece of code (storage, signing, scheduling) is shared. Only `GoalLoop.Decide()` branches on tier.

## Constitution enforcement engine

Three checkpoints, each with a distinct role. All live in `internal/employee/enforcement.go`.

### Checkpoint 1: Pre-execution gate

Runs **before every tool call**, inside the existing `SecurityEngine.Check()` flow. The employee package exposes a `PreExecChecker` that the security engine calls in a new stage between base rule lookup and confirmation gate.

```go
// internal/employee/enforcement.go
type PreExecChecker struct {
    constitution *Constitution
    // snapshot under lock, compare under lock — no I/O under lock
}

// Check is called by SecurityEngine between base-rule and confirmation stages.
// Returns Denied with reason, or Allowed with any required-confirmation flags.
func (p *PreExecChecker) Check(action, toolName string, details map[string]string) Decision {
    // 1. tools_allowed / tools_forbidden
    // 2. risk_ceiling (compare to SecurityEngine's computed risk)
    // 3. escalation_triggers (match against action/tool/details)
    // 4. never[] (shell command scan, path scan where possible)
    // 5. budget check (tokens today, $ today, invocations today) via BotState
}
```

**Denial outcomes:**
- `ToolsForbidden` match → hard deny, audit event.
- `RiskCeiling` exceeded → hard deny, audit event, plan signoff required to override.
- `EscalationTrigger` matched → **escalation** (not denial): the action is queued as a `Plan` in `PendingApproval` routed to `EscalatesTo`. The current invocation returns without executing the action.
- `Never` pattern match → hard deny, audit event at `RiskCritical`, **employee auto-pause**.
- Budget exhausted → hard deny, audit event, employee auto-pause.

### Checkpoint 2: Post-turn audit

Runs **after each LLM turn** (after AgentLoop.RunOnce completes). Small-model classifier scans the turn's tool calls and final output against the constitution.

```go
type PostTurnAuditor struct {
    model   llm.Chatter  // configurable; default = small model from config/models.json5
    prompt  string       // built from constitution.constraints.never + charter
}

func (a *PostTurnAuditor) Audit(ctx context.Context, turn TurnRecord) AuditFinding {
    // Inputs to classifier:
    //   - the constitution's never[] rules
    //   - the charter's soft commitments (non-machine-enforceable)
    //   - the turn's tool calls + arguments + results
    //   - the LLM's final output
    // Output:
    //   - severity: info | warning | critical
    //   - violated_rule: reference into constitution
    //   - evidence: the specific tool call / output span
}
```

**Findings routing:**
- `info` / `warning` → write `AuditFinding` row, attach to owning Goal, no action.
- `critical` → **auto-pause employee** + write finding + emit bus event `employee.paused` with reason.

Auto-pause sets `BotState.status = paused`, increments a `pause_count` counter, and stops the scheduler from invoking the employee until an operator resumes.

### Checkpoint 3: Periodic audit job

Runs on a schedule (default: every 6h, configurable per employee). Reviews the last N invocations in bulk. Same small-model classifier, different prompt: "here are the last N decisions this employee made. Are there patterns of drift from the constitution?"

**Why a separate checkpoint:** post-turn audit catches individual violations; periodic audit catches slow drift — the LLM gradually pushing boundaries across many turns. This is how real compliance works.

**Outputs:**
- Findings with severity, same routing as Checkpoint 2.
- A `DriftScore` (0.0–1.0) attached to the employee — surfaced in `meept agents status` as a single number per employee.
- If drift score > threshold (default 0.3, configurable), employee is auto-paused even if no single finding was critical.

### Audit findings data model

New table `employee_audit_findings`:

```sql
CREATE TABLE employee_audit_findings (
    id              TEXT PRIMARY KEY,
    employee_id     TEXT NOT NULL,
    goal_id         TEXT,
    plan_id         TEXT,
    turn_id         TEXT,
    severity        TEXT NOT NULL,        -- info | warning | critical
    checkpoint      TEXT NOT NULL,        -- pre_exec | post_turn | periodic
    violated_rule   TEXT,                 -- reference into constitution (e.g. "never[2]")
    evidence        TEXT,                 -- JSON: tool call, output span, etc.
    detected_at     TEXT NOT NULL,
    resolved_at     TEXT,
    resolution      TEXT,                 -- "false_positive" | "acknowledged" | "constitution_amended"
    FOREIGN KEY (employee_id) REFERENCES bot_definitions(id) ON DELETE CASCADE
);
CREATE INDEX idx_audit_employee ON employee_audit_findings(employee_id, detected_at);
```

### Configurability

```json5
// ~/.meept/meept.json5 — new employees section
{
  employees: {
    audit: {
      model: "small",                  // alias from config/models.json5; configurable
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

### Wiring into SecurityEngine

Existing `SecurityEngine.Check()` already accepts `conversationID`. We add an optional `agentID` parameter (or a new `CheckForAgent(action, tool, details, convID, agentID)` method — whichever the existing code tolerates). The engine then looks up the employee's constitution via the `*PreExecChecker` registered for that agent ID, and runs it as an additional stage.

Non-employee agents (regular chat/coder/etc.) skip the employee pre-exec stage entirely — no behavior change for existing agents.

### Out of scope for enforcement

- **No output sanitization changes.** The existing `OutputMonitor` and `PromptGuard` continue to do their jobs. The post-turn auditor reads the same output but for a different purpose (constitution compliance, not injection defense).
- **No new "constitutional language model".** Same `llm.Chatter` interface, same providers, just a different prompt template.
- **No network-level controls.** Employees are bounded by tools + path fencing + risk ceiling, not by network policy. That's a separate concern.
- **No "kill switch" beyond auto-pause.** Auto-pause is already the strongest action available. Operator resume is the only un-pause path.

## CLI, TUI, HTTP, RPC surface

Hard cutover from `meept bots` to `meept agents`. All four interfaces updated together. Per CLAUDE.md's wiring requirement, every primitive ships with CLI + TUI + GUI + HTTP + RPC wiring.

### CLI

New `meept agents` namespace. Replaces `meept bots`.

```
# lifecycle
meept agents list                              # all employees, status, tier, drift score
meept agents show <id>                         # full definition: constitution, state, goals, recent findings
meept agents create <definition.json>          # validates constitution; refuses without one
meept agents update <id> <definition.json>
meept agents delete <id>                       # stops + deletes; confirms unless --force
meept agents pause <id>                        # operator pause
meept agents resume <id>                       # operator resume (only un-pause path)
meept agents amend <id> --field=<key> <value>  # propose constitution amendment (goes to Plan signoff)

# migration
meept agents migrate                           # scans ~/.meept/bots/*.json
meept agents migrate --apply <id>              # write proposed constitution to disk

# goals
meept agents goals [--employee=<id>]           # list goals with health (red/yellow/green)
meept agents goal <goal-id>                    # goal detail + active plan + history
meept agents goal <goal-id> --approve <plan-id>
meept agents goal <goal-id> --reject <plan-id> --reason="..."

# audit
meept agents audit <id> [--since=<dur>]        # recent findings, severity, resolution
meept agents audit <id> --resolve <finding-id> --as=false_positive
```

`meept bots` removed. Existing scripts that call `meept bots` get a clear error: "meept bots was removed; see `meept agents --help` and `docs/workflows/employees.md`."

### TUI

Bubbletea panel, accessed via `ctl-x e` (new keybinding, alongside existing `ctl-x o` for MCP).

Panels:
- **agents list**: ID, status, tier, drift score, daily cost, last invocation. Up/down to navigate, enter to drill in.
- **agent detail**: constitution summary, active goals with health bars, recent findings count by severity. Tabs for "constitution / goals / audit / state".
- **approval queue**: tier-2 plans awaiting signoff for the current user (when `escalates_to` includes "user"). Approve/reject inline. Same surface as Plan approvals.
- **audit findings**: severity-colored list. Resolve-as-false-positive inline.

Lower-element text lowercase per CLAUDE.md UI convention (e.g., "approve", "reject", "pause", "resume", not "Approve"/"Reject").

### HTTP

New endpoints under `/api/v1/agents/*`. The existing `/api/v1/bot/{id}/trigger` moves to `/api/v1/agents/{id}/trigger` (webhook callers updated in the POC).

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/v1/agents` | GET | list employees |
| `/api/v1/agents` | POST | create employee (validates constitution) |
| `/api/v1/agents/{id}` | GET, PATCH, DELETE | show, update, delete |
| `/api/v1/agents/{id}/trigger` | POST | webhook trigger (existing semantics) |
| `/api/v1/agents/{id}/pause` | POST | operator pause |
| `/api/v1/agents/{id}/resume` | POST | operator resume |
| `/api/v1/agents/{id}/constitution` | GET, PATCH | view / propose amendment |
| `/api/v1/agents/{id}/goals` | GET | list goals with health |
| `/api/v1/agents/{id}/goals/{gid}` | GET | goal detail |
> **Note (S6, 2026-06-26):** The agent-specific plan approval endpoints below are deprecated and removed. Use `POST /api/v1/plans/{pid}/approve` and `POST /api/v1/plans/{pid}/reject` with body `{approver_id, employee_id}` instead.

| `/api/v1/agents/{id}/goals/{gid}/plans/{pid}/approve` | POST | approve plan (deprecated — use `POST /api/v1/plans/{pid}/approve`) |
| `/api/v1/agents/{id}/goals/{gid}/plans/{pid}/reject` | POST | reject plan (deprecated — use `POST /api/v1/plans/{pid}/reject`) |
| `/api/v1/agents/{id}/audit` | GET | findings, filterable by `?since=&severity=` |
| `/api/v1/agents/{id}/audit/{fid}/resolve` | POST | resolve finding |
| `/api/v1/agents/migrate` | POST | run migration scan, returns proposed constitutions |

Authenticated via the existing API key mechanism when `require_auth: true`.

### RPC

New methods under the `agents.` namespace. Existing `bot.*` methods removed (hard cutover).

| Method | Notes |
|---|---|
| `agents.list`, `agents.get`, `agents.create`, `agents.update`, `agents.delete` | direct ports of existing `bot.*` lifecycle |
| `agents.pause`, `agents.resume` | direct ports |
| `agents.trigger` | programmatic trigger (used internally by webhook handler) |
| `agents.amend` | propose constitution amendment → routes to Plan signoff |
| `agents.goals.list`, `agents.goals.get` | goal listing |
| `agents.goals.approve`, `agents.goals.reject` | plan signoff |
| `agents.audit.list`, `agents.audit.resolve` | findings |
| `agents.migrate` | migration scan |

Service layer: new `EmployeeService` in `internal/services/` (fits the existing service-registry pattern). Bot-prefixed services removed.

### Flutter (menubar)

New "agents" tab in the menubar app, replacing the "bots" surface.

- Cards for each employee: ID, tier badge, health dot, drift score, today's cost, recent findings count.
- Tap → detail view: constitution summary, goals, approve/reject inline for pending plans.
- Pause/resume button per agent.

### POC: `ci-monitor` employee

Ships in `config/employees/ci-monitor.json5` as a worked example. Covers:

- Constitution with `tier_2_propose`, `risk_ceiling: medium`, `escalates_to: ["user"]`.
- Constraint example: `tools_allowed: ["web_fetch", "shell_execute"]`, `never: ["merge to main", "delete branches", "execute force pushes"]`.
- Assessment interval: 15m.
- Trigger: GitHub webhook on PR/push events.
- Budget: `daily_budget_cents: 50`, `max_invocations_per_day: 100`.
- Goal: "keep CI green for main branch".

The POC's day-in-the-life:

1. GitHub webhook fires on push → existing bus_event trigger wakes the employee.
2. ASSESS: employee fetches CI status via `web_fetch`, identifies failures.
3. PLAN: proposes a Plan ("investigate failing job X, open issue with logs") in `PendingApproval`.
4. User approves via TUI/CLI/HTTP.
5. EXECUTE: employee runs investigation, stores findings in memory.
6. REFLECT: updates Goal health (green if CI green, yellow if flaky, red if broken).

### Documentation

- **New:** `docs/workflows/employees.md` — full feature spec replacing `docs/workflows/bots.md`. Includes constitution authoring guide, tier reference, audit explanation, POC walkthrough.
- **Updated:** `docs/concepts/multi-agent.md` — add employees section, explain relationship between RoleBot/RoleExecutor and employees.
- **Updated:** `docs/concepts/architecture.md` — add the GoalLoop + Constitution Engine to the architecture diagram.
- **Updated:** `CLAUDE.md` — add `meept agents` commands, reference the new `internal/employee/` package.
- **Updated:** `docs/reference/cli.md` — replace bots section with agents section.
- **Updated:** `docs/reference/http-api.md` and openapi.yaml — replace bot endpoints with agent endpoints.
- **Updated:** `docs/reference/generated/config.md` — regenerated from the new schema structs (`make docs-generate`).
- **Removed:** `docs/workflows/bots.md` — kept in git history; a one-liner redirect at commit time suffices.
- **README.md** — replace bots mention with employees.

## Error handling

### Employee runtime failures

- LLM call fails → BotRunner's existing retry path. If retries exhaust, turn marked failed, Goal health decays. N consecutive failures (configurable, default 3) → employee auto-pause with `last_error` populated.
- Tool call fails → existing tool error propagation. Employee's REFLECT step sees the error and decides whether to retry, escalate, or give up.
- ASSESS produces invalid JSON (LLM hallucinates schema) → fall back to tier-1 behavior for that turn, log audit finding at `warning`, continue. Never crash the loop.
- Plan approval times out (no human signs off within configurable `approval_timeout`, default 7d) → Plan auto-rejected, Goal marked `at_risk`, finding written.

### Constitution failures

- Constitution fails validation at load → employee refuses to start, error names the failing field and points to docs. Other employees unaffected.
- Constitution references unknown tool name in `tools_allowed` → warning at load, tool removed from list. Employee starts but flagged for review.
- Constitution references unknown agent ID in `escalates_to` → hard fail at load. Operator must fix the ID.
- Amendment attempt hits a `FrozenFields` violation → rejected immediately, audit finding at `info` level, no Plan created.

### Enforcement failures

- Pre-exec checker throws (panic, unexpected type) → fail-safe: deny the action, audit at `critical`, employee auto-pause. Better paused than rogue.
- Post-turn audit LLM call fails → log warning, skip audit for that turn, no pause. Don't cascade one LLM failure into a system-wide outage.
- Periodic audit fails three times in a row → write `critical` finding with `checkpoint=periodic`, no auto-pause (the auditor being down isn't evidence the employee is misbehaving). Continue retrying with backoff.
- Audit model produces unparseable output → retry once with stricter prompt. If still unparseable, skip the turn, log `warning`.

### Storage failures

- Goal store write fails → log error, continue with in-memory state. Attempt backfill on next successful write. Never block the GoalLoop on persistence.
- `bot_definitions` row corrupted → employee refuses to load, log error with row ID. Other employees unaffected.

### Concurrency

- Two triggers fire simultaneously for the same employee → `BotRunner` already serializes invocations per bot via a per-employee mutex. Goal loop uses the same pattern.
- Operator pauses while invocation in flight → in-flight invocation completes (we don't kill LLM calls mid-turn), but the post-turn REFLECT step is skipped and no new invocations start until resumed.
- Constitution amendment approved while invocation in flight → the next invocation uses the new constitution; the in-flight one continues under the old one. Version number on the constitution snapshot prevents inconsistency.

## Edge cases

- **Empty `escalates_to`** → tier 2 employees can't get plan approval. ASSESS may produce plans, but they sit in `PendingApproval` forever. Logged as `warning` at ASSESS time. Operator must set `escalates_to: ["user"]` or another approver.
- **Employee escalates to itself** (directly or transitively via Plan signoff chain) → rejected at constitution load time. Cycle detection over `escalates_to` graph.
- **Webhook triggers tier-1 employee that has `risk_ceiling: safe`** → if the trigger context requires a higher-risk action, pre-exec gate denies. Employee writes audit finding and stops for that invocation.
- **Goal has no active plan and tier 2 ASSESS finds nothing to do** → no-op. Logged at debug. Goal health stays unchanged or decays slowly (configurable `idle_decay`, default off).
- **Budget exhausted mid-turn** (turn starts within budget, tool call tips it over) → pre-exec gate on the next tool call denies with budget reason. Turn ends incomplete. Goal marked `at_risk`.
- **Audit model reports a `critical` finding for action X, but the constitution's `never[]` explicitly permits X** → finding downgraded to `info`, operator-noted contradiction in the constitution is flagged for review. We trust the structured rules over the LLM's read of the charter.
- **Operator deletes an employee with active goals** → goals and findings cascade-delete (FK ON DELETE CASCADE). Plans survive (they're in the separate `plans` table). Historical metrics survive.
- **Migration run on a bot whose prompt is too vague to synthesize constraints** → migrate command writes a minimal conservative constitution (`tier_1_reactive`, `risk_ceiling: low`, `escalates_to: ["user"]`, `never: ["execute financial transactions"]`) and flags for human review. Never refuses to migrate.

## Testing strategy

### Unit tests (per package, table-driven)

- `internal/employee/constitution_test.go`: validation (frozen fields, cycle detection, tier constraints, tool-name resolution).
- `internal/employee/enforcement_test.go`: pre-exec gate truth table (every constraint type × every outcome), post-turn auditor with stub LLM, periodic audit finding aggregation.
- `internal/employee/goal_test.go`: state transitions, health computation, plan history.
- `internal/employee/goal_loop_test.go`: tier 1 and tier 2 Decide() logic with mocked LLM + executor.
- `internal/employee/authority_test.go`: escalation routing, cycle detection, delegate routing via existing tools.

### Integration tests (in `tests/integration/`)

- `employee_lifecycle_test.go`: create → start → trigger → pause → resume → delete, end-to-end through the daemon.
- `employee_audit_test.go`: inject a fake LLM response that violates `never[]`, assert finding + auto-pause.
- `employee_migration_test.go`: drop a legacy bot JSON on disk, run migrate, assert valid constitution produced and employee starts.
- `employee_tier2_approval_test.go`: ASSESS produces plan → signoff via RPC → EXECUTE → REFLECT updates goal health.

### POC smoke test

- `tests/integration/ci_monitor_poc_test.go`: simulates a GitHub webhook payload, runs the full tier-2 cycle on a fixture repo, asserts goal health transitions green→yellow→green.

### Race tests

`go test -race ./internal/employee/...` — especially the concurrent-trigger path and the in-flight-invocation-vs-pause path.

### Pre-commit hooks

Existing hooks cover: mutexio (no I/O under lock in the new code), setters nil guards, staticcheck, feature-docs (forces `docs/workflows/employees.md` to exist if `internal/employee/` is touched).

### Out of scope for phase 1 testing

- Tier 3 (not shipped).
- Cross-employee delegation via Plan signoff chains (delegation uses existing platform tools, which have their own tests).
- Multi-employee constitutions interacting on shared memory (covered by existing memory namespace isolation).

## Telemetry

Metrics emitted to the existing `internal/metrics/`:

- `employee.invocations` (counter, tagged by employee_id, tier, outcome).
- `employee.audit.findings` (counter, tagged by employee_id, severity, checkpoint).
- `employee.plan.approvals` (counter, tagged by employee_id, outcome).
- `employee.budget.burn` (gauge, tagged by employee_id, unit=cents).
- `employee.drift.score` (gauge, tagged by employee_id).
- `employee.goal.health` (gauge, tagged by goal_id, value=0/1/2/3 for healthy/at_risk/broken/unknown).

Surfaced in menubar and TUI.

## Phase 2 (out of scope for implementation plan)

- Tier 3 (full autonomy within constitution).
- Cross-employee Plan signoff chains.
- Network-level controls beyond existing path fencing.
- Internal Meept self-management: reorganize `selfimprove` and `q_agent` as employees.

Note: constitution amendment via employee self-proposal IS in phase 1 — `meept agents amend` is wired, `AmendmentPolicy.SelfProposeAllowed` controls whether a given employee may propose, and proposals route through Plan signoff per Q7(b).
