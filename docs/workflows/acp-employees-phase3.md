---
title: ACP Employees — Phase 3 design (autonomous hermes-persona employees)
status: draft — dimensions open for decision (owner picks per dim, then implementation is specced)
decided: n/a
---

# ACP Employees — Phase 3

## Problem

Phases 1–2 landed: `executor: acp` agent definitions are chat-routable persona
slaves (docs/workflows/acp-employees.md, dim 1A/3A). Phase 3 composes the two
layers: an **employee** (bot + constitution, internal/employee/types.go:27–33)
whose GoalLoop EXECUTE runs on an external ACP harness instead of the local
LLM loop — "bob the autonomous employee". The seam already exists:
`GoalLoop.WithExecutor(r bot.BotExecutor)` (internal/employee/goal_loop.go:477),
where `bot.BotExecutor` is exactly
`ExecuteBot(ctx, systemPrompt, userMessage) (output string, tokensUsed int, err error)`
(internal/bot/runner.go:41–43).

## Current state (verified)

- GoalLoop.Execute (goal_loop.go:838–888) calls `runner.ExecuteBot` with
  `SynthesizedPrompt(constitution, "")` (:855) and `plan.Prompt` (:858); nil
  executor is a hard error (:845–847). Decide dispatches by tier at
  goal_loop.go:1363–1387 (Tier1 :1382, Tier2 :1384, Tier3 :1386).
- Today's employee executor is `agentLoopBotExecutorAdapter.ExecuteBot`
  (internal/daemon/employee_service_adapter.go:293–312) → `AgentLoop.RunOnce`;
  it already returns **0 tokens** (same file, :272–276 comment).
- ACPExecutor.Run (internal/agent/acp_executor.go:109–176): GetOrCreatePersona
  → `sess.Send` → streamed emits. The loop seam `runTurnProducer` (:247–266)
  branches `spec.Executor == "acp"` and returns the final text to
  RunOnceWithParts' **shared** post-processing — reply guard, security scan,
  history (:271–273: "no second reply path").
- `acp.Session.Send` (internal/acp/session.go:262–292): sets StateBusy, then
  ONE `tr.Call(MethodSessionPrompt, …)` bounded by `CallTimeout` (default 120s,
  session.go:95–96); the response carries only `stopReason` — **no usage/tokens
  on the wire**.
- Permission lane: per-persona override flows
  `PersonaOpts.PermissionMode → SessionConfig` (internal/acp/manager.go:262–264,
  :280); modes permissive/deny/decision/human validated in
  internal/config/acp_config_test.go:26–102; human relay in internal/acp/relay.go.
- Escalation is plan-level: `EscalationGate` (goal_loop.go:388, :393) wired
  from `ShouldEscalate` (internal/employee/enforcement.go:2238); tier-2/3 route
  candidates to Plan signoff (docs/workflows/employees.md:71–79, :300–306).
- `acp.persona.turns` exists only as a Phase-2 *plan* line (acp-employees.md:177)
  — no emitter found (grep internal/metrics, internal/agent). `employee.invocations`
  is emitted in internal/employee/manager.go (:1472, :1505, :1557, :1572, :1581, :1637).

## Dimensions (decide per dim; no monolith)

### D1 — Composition shape: how does an employee opt into an ACP executor?

- **A. Reference an agent definition id.** Employee gains one field (e.g.
  `executor_agent: bob`) naming a registry spec whose `executor: acp` +
  `acp:` block already exist. GoalLoop resolves the spec via AgentRegistry
  (same resolution the chat lane uses, acp_executor.go:231–234).
- **B. Employee gains its own `acp:` block** duplicating ACPSpec
  (internal/agent/spec.go:143–153) on BotDefinition/Constitution.
- **C. Both** — reference wins, inline block overrides per-field.

Recommendation: **A**. Precedent: spec decision 1A kept bob an
agent-definition (acp-employees.md:21); the `acp:` block IS the harness
binding and belongs to the agent definition. "Bob the chat slave" and "bob the
autonomous employee" then share one binding, one persona session
(acp_executor.go:77–79), one permission lane, one `agents show` section — no
5-step field-pipeline duplication (acp-employees.md:51–65).
**Decision: A (employee gains executor_agent referencing the agent definition)** (owner picks 2026-10-24; parent proceeding with the recommendations)

### D2 — Executor seam mechanics: what does the injected BotExecutor call?

- **A. Direct ACPExecutor.Run adapter** — a new `acpBotExecutor` implements
  `bot.BotExecutor`, resolves the referenced spec, calls `ACPExecutor.Run`
  (acp_executor.go:109) with a no-op/`emit` shim.
- **B. Route through the referenced agent's AgentLoop** — the adapter calls
  the registry-built loop's `RunOnceWithParts` path (i.e. reuse
  `runTurnProducer` :247–266), exactly as the LLM adapter reuses `RunOnce`
  today (employee_service_adapter.go:293–312).
- **C. Duplicate a second post-processing pipeline for employees.**

Recommendation: **B**. The acp final text already earns reply guard, output
filters, security scan and progress emission at the loop seam
(acp_executor.go:271–273, :294–315) — spec step 4 requires machine-shaped
harness output must not become a raw reply (acp-employees.md:107–109).
C is the anti-pattern; A silently drops that whole lane. `ExecuteBot`'s
`systemPrompt` stays unused-by-design (adapter precedent,
employee_service_adapter.go:271–272); constitution prompt rides a steer/prefix
lane or is documented advisory.
**Decision: B (route through the referenced agent's loop; shared reply-guard/filter lane)** (owner picks 2026-10-24; parent proceeding with the recommendations)

### D3 — Tier semantics + escalation gate with a remote executor

- **A. Tier semantics unchanged at plan level.** ASSESS/PLAN stay meept's LLM
  work; the escalation gate (ShouldEscalate, enforcement.go:2238) still gates
  every candidate before Execute. Tier1 = trigger→execute; Tier2 = plan
  signoff; Tier3 = no stop unless gate fires. Permission lane runs
  independently inside the harness turn.
- **B. A + a validation guard:** a Tier3 acp employee may NOT ship
  `permission_mode: permissive` — constitution load rejects it; Tier3 acp
  requires `decision` or `human` (relay: internal/acp/relay.go).
- **C. Map tiers→modes** (tier1→read-ish, tier3→decision) as a hard
  derivation.

Recommendation: **B**. Constitution risk bands gate the *plan* (what meept
dispatches); permission modes gate the *tool calls the harness makes*
(employees.md:79 already splits candidate-risk vs per-tool-call risk). They
are orthogonal controls and both must stay. C fuses two axes that legitimately
diverge (a Tier2 propose employee can still need `human` mode). What Tier3
means remotely: meept stops binding once the prompt is sent — only the
permission lane binds (see Security).
**Decision: B (plan gate + permission lane orthogonal; tier-3 acp employee must not ship permissive)** (owner picks 2026-10-24; parent proceeding with the recommendations)

### D4 — Cost/budget accounting gap

Verified: ACP `session/prompt` returns `stopReason` only (session.go:279–286);
no usage read anywhere in internal/acp. The LLM adapter already reports
`tokensUsed=0` (employee_service_adapter.go:272–276). For an acp employee:
**enforceable** — `MaxInvocationsPerDay`, `MaxIterations`, `Timeout`, run counts
(bot/runner.go:82–106); **not enforceable** — `DailyBudgetCents` /
`TotalCostCents` (internal/bot/types.go:59, :112) and cost escalation triggers
(no cost signal to match).

- **A. Accept the gap:** document that budget caps are no-ops for acp
  employees; rely on invocation/day caps + permission lane.
- **B. Proxy cost:** estimate cents from turn duration × a per-agent rate
  table in the employee def.
- **C. Wait for the harness:** conditionally surface usage if a future ACP
  revision/harness emits it (deny speculative parsing today).

Recommendation: **A now, C in wire-forward code** (read `usage` from the
prompt response iff present). B's fake precision would silently mis-fire cost
escalation. Refuse to load (or loudly warn) an acp employee whose constitution
has `On: "cost"` escalation triggers or `daily_budget_cents > 0`.
**Decision: A now + C wire-forward (usage surfaced iff a future harness emits it); warn/refuse cost-bound acp employees** (owner picks 2026-10-24; parent proceeding with the recommendations)

### D5 — Steering a running acp employee

`agents steer <id>` feeds a `Source: "steer"` TriggerEvent consumed at the
next Decide re-entry via `pendingSteer` (goal_loop.go:368–376, :1857–1867) —
already a BETWEEN-turns design. Mid-turn injection would need `Session.Send`
during StateBusy: the wire is one Call per prompt, blocked until the harness's
stopReason (session.go:262–292); a concurrent `session/prompt` on one sessionId
is untested against hermes-acp. **Mid-turn steering: DEFERRED — the wire does
not support it today.**

- **A. Defer mid-turn steering.** Steers queue (existing lane) and land on the
  next ASSESS; document that an acp episode cannot be interrupted mid-turn.
- **B. Cancel-and-resend:** kill/restart the session on steer, re-prompt with
  steering merged. Loses harness context; process-boundary churn.
- **C. Optimistic concurrent Send** — rejected: no evidence the harness
  accepts it; corrupts the single in-flight prompt's chunk accumulator
  (`s.chunks = nil` at session.go:269–271 races a second Send).

Recommendation: **A** (C explicitly unsafe). Revisit only if ACP gains a
mid-turn input method.
**Decision: A (defer mid-turn steering; steers land between turns)** (owner picks 2026-10-24; parent proceeding with the recommendations)

### D6 — Observability

`acp.persona.turns {agent, persona, outcome}` is still unimplemented (see
Current state). Employee-driven persona turns need attribution distinguishing
them from chat-driven turns.

- **A. Minimal:** emit `acp.persona.turns` from the loop seam with an added
  `source=chat|employee` tag; stamp AuditFinding / TurnRecord
  (enforcement.go:376, :418) with employee_id + acp harness/persona so
  `agents audit` attributes constitution findings on remote turns.
- **B. Full per-persona metrics suite** (latency histograms, permission-lane
  counters) in this phase.
- **C. No new metrics** — reuse `employee.invocations` tier/outcome tags only.

Recommendation: **A**. `employee.invocations` (manager.go:1472ff) already
counts the episode; the persona counter + audit attribution are the missing
two bits; permission_decision events already exist in the acp lane.
**Decision: A (persona counter shipped fe4cdc44; add employee attribution on audit findings)** (owner picks 2026-10-24; parent proceeding with the recommendations)

## Security

**Fence gap (carries over, now amplified):** the external harness executes
outside meept's path fence — an ACP persona is a full agent subprocess with
its own tool execution (acp-employees.md:153–158). With Phase 3 the loop that
decides *what to do* also runs unattended (tier 3), so meept's ONLY runtime
controls are: (1) the plan-level escalation gate before Execute, and (2) the
permission lane (permissive/deny/decision/human) on the harness's own
tool calls. **The constitution cannot bind what the harness does internally** —
PostTurnAuditor sees only the returned final text, not intermediate actions.
Therefore: D3-B's mode floor for tier-3 acp employees is a security invariant,
not taste; spawn cwd stays the only filesystem scoping; the kill switch
(`[acp] enabled=false`) disables every autonomous persona employee too;
operator-authored agent defs remain non-model-writable.

## Implementation phases (post-decision, indicative)

1. Resolution + adapter: `executor_agent` field, load-time registry validation
   (target spec exists, `executor: acp`), `acpEmployeeExecutor` over the loop
   seam (D1/D2).
2. Constitution guards: tier3×permissive rejection, cost-field warnings,
   escalation-trigger sanity (D3/D4).
3. Observability: persona.turns metric, audit attribution tags (D6).
4. e2e: extend hermetic fakeagent (internal/acp/testdata/fakeagent) with an
   employee-driven turn; suite per e2e-testing.md tiers. No surface changes
   beyond `agents show` executor line (TUI/GUI parity per AGENTS.md).

## Open questions

- Which conversation does an employee's acp turn belong to — the current
  bot-exec id scheme (employee_service_adapter.go:300–302) or a per-employee
  persona session visible in the GUI?
- Idle-reap interaction: an employee episode against an open-question persona
  reap (acp-employees.md:221–225) — reap mid-episode?
- Does SynthesizedPrompt(constitution) need any delivery to the harness at all
  (persona profile `-p borg` already carries persona voice)?

## Verification

Docs-only change: no `.go` file touched (git status in the delivery report
shows only this file + one line in docs/workflows/acp-employees.md).
