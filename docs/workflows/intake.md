# GitHub Ticket Intake (the intake factory)

`internal/intake` is the ticket-intake seam for the github-intake-factory
tree: a label-gated GitHub Issues poller, the ticket→goal identity layer,
and the outcome write-back. The daemon wiring that runs it lives in
`internal/daemon/intake_wiring.go`; the `[intake]` config block is
`config.IntakeConfig` (internal/config/schema.go).

## Problem

A repository's issue tracker is where work arrives, but an AI employee
only acts on goals. Today a human reads the issue, writes a goal, pastes
the mandate, and later pastes the outcome back as a comment. Two hazards
make naive automation wrong:

- **Duplication.** Polling is retry-prone: a crash between "created the
  goal" and "recorded that I created it" re-creates the goal on the next
  poll. GitHub comments are not deduped either — a repeated outcome must
  not storm the ticket with copies.
- **Drift.** An edited issue body is a changed mandate. The linked goal
  must be re-dispatched in place (still exactly one goal), not duplicated.

## Behavior

When `[intake].enabled` is true, the daemon schedules one interval job
(`intake_poller`) via the same scheduler adapter the employee assess jobs
use (`employeeSchedulerAdapter.RunAtInterval` — idempotent per name, no
new goroutine). Each tick:

1. **Poll** — `intake.GitHubPoller` lists the watch repo's issues
   (state=all, sorted by updated, one page of 50), skips PRs, applies the
   label gate (`required_labels`, ALL must be present,
   case-insensitive), and tracks an in-memory watermark so each poll
   returns only new-or-changed tickets. A daemon restart re-scans once;
   the dedup layer makes that a no-op.
2. **Link** — for each ticket, `intake.Linker.EnsureGoal` maps it to
   exactly one live goal identified by
   `TriggerRef = intake:<repo>#<ticket>` on
   `cfg.Intake.default_employee`:
   - no linked goal → claim the effects key (`intake_ticket_goal`, keyed
     on repo + ticket + body SHA256) then create a trigger-source goal
     with the ticket body as mandate (the optional `gate_command`
     attaches as the goal's quality gate);
   - identical body hash → no-op (dedup);
   - changed body → re-dispatch in place: mandate updated, state reset
     active, health reset unknown, persisted via `UpdateGoal`.
3. **Write back** — `intake.WriteBack` (built at wiring time, held on
   `Components.IntakeWriteBack`) posts goal outcomes to the source
   ticket as `[meept factory] goal <id>: <state>` comments,
   effects-wrapped per (ticket, state, goal) so a repeated outcome
   reuses the receipt instead of re-posting. A healthy outcome adds the
   done label (`done_label`, default `factory:done`).
4. **Project (opt-in)** — when `project_number` > 0, `intake.Projector`
   (held on `Components.IntakeProjector`) mirrors the plan hierarchy
   into a GitHub Projects V2 board as sub-issues of the ticket issue:
   ticket issue → one sub-issue per plan phase → one sub-issue per task
   step. A `State` single-select field on the board mirrors phase
   state. Two bus subscribers drive it: `plan.executing` (the plan just
   synthesized → `EnsureProjection`) and `plan.phase_state` (state
   transitions → `MirrorState`). The effects receipts ARE the
   correlation store: `intake_projection:<planID>:<phaseID>` answers
   "which GitHub issue and which board item is this phase?" — no new
   column, no new table. Projection is fail-soft: a GitHub or GraphQL
   error logs Warn and never fails the plan that triggered it.

Poll errors log Warn and wait for the next tick; a panic inside one tick
is recovered so the scheduler goroutine survives (same posture as the
github re-arm ticker). A per-ticket failure logs Warn and continues with
the remaining tickets.

When `[intake]` is absent or `enabled=false` (the shipped default),
nothing is scheduled and nothing runs.

## Config

`[intake]` in `meept.json5` (template: `config/meept.json5`). Disabled
by default:

```json5
"intake": {
  "enabled": true,
  "watch_repo": "octo/forge",           // REQUIRED when enabled
  "default_employee": "factory-bot",    // REQUIRED when enabled
  "poll_interval": "5m",                // Go duration string; default 5m
  "required_labels": ["factory"],       // ALL required; empty = no gate
  "done_label": "factory:done",         // added on healthy outcomes
  "gate_command": "",                   // optional per-goal quality gate
  "project_number": 0                   // 0 = projection disabled
}
```

`ValidateEnabled` names EVERY missing required field, and the wiring is
fail-closed: an invalid block logs Warn naming the fields and schedules
nothing.

For projection (`project_number` > 0): the board must already exist in
the watch repo's GitHub Projects V2, owned by the token's user or
organization. The projector looks up the board by number and resolves
its `State` single-select field lazily on first use; if the field is
missing, state mirroring sends the mutation with unresolved ids and
GitHub rejects it (logged Warn, never fatal). Create the `State` field
with options matching the phase states (`pending`, `in_progress`,
`completed`, `confirmed`, `failed`) for clean mirroring.

## Wiring and the token coupling

`wireIntakePoller` (internal/daemon/intake_wiring.go, called right after
`wireGitHubTools`) builds the poller, linker, and write-back on ONE
githubsvc client pool:

- when the `github_*` tools are also enabled, it **shares**
  `c.GitHubPool` — one pool, one token source;
- when intake is enabled alone, it builds a dedicated pool from the
  `[github].token_secret` sources (same file/broker precedence as the
  github tools) and holds it on the wiring-local poller/write-back;
- with NO token material at all it still schedules, on an erroring
  token source: every poll fails closed at request time with a nameable
  error, so the misconfiguration stays visible in the logs until a
  token is configured.

Intake has **no separate token config** — it reuses `[github]`'s. The
token itself is never logged or printed (`meept intake status` is
report-only and never touches token material; `meept github status`
reports the token source presence).

Known v1 limits, accepted by the plan: one page of 50 issues per poll
(repos with more matches lose rows), and the watermark is in-memory
only (restart re-scans; dedup makes it a no-op). A final scheduler tick
left in flight at daemon shutdown may complete after Stop returns —
acceptable for v1; the tick's effects are all effects-ledger idempotent.

## CLI

`meept intake status` (report-only, doctor convention): enabled?,
watch repo, poll interval, required labels, done label, default
employee resolved-or-missing, gate command, project number, and a
validity preview mirroring the daemon's fail-closed posture. Always
exit 0 — a disabled state is the default, not a failure.
