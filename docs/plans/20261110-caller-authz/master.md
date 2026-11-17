# Inbound Caller Authorization (issue #125) — Master Plan

Date: 2026-11-10. Repo: meept. Tree: docs/plans/20261110-caller-authz/
Worktree: .hermes/worktrees/caller-authz (branch plan/caller-authz)

## Goal

Give inbound voice calls a per-caller action gate: resolve WHO is calling
at answer time, stamp that identity onto every turn, refuse non-approved
callers anything beyond message-taking, and make the existing per-employee
constitution tool gate actually effective on voice turns (agentID fix).

## Architecture Overview

Grounding (survey 2026-11-10, file:line verified):
- Constitution ToolsAllowed/ToolsForbidden exist and enforce per-execution
  via PreExecChecker routed by agent_id: internal/employee/constitution.go:250-254,
  enforcement.go:585-616 (steps 1a/1b), manager.go:980-996.
- BUT voice loops execute with agentID "default" (components.go:2369,
  loop.go:9677 clone inheritance; voice_employee.go:240-276 overrides only
  prompt/config/model), so per-employee tool gating does NOT apply to
  voice turns today. This tree fixes that (decision O5).
- Inbound callers are bound to DefaultEmployee with the full global
  registry (voice_wiring.go:291-310); contact resolution exists
  (internal/contact/resolve.go:12-24) but is outbound-only today.

Two branches: 01-identity (plumbing), 02-enforcement (policy + docs).

## Owner-Ratified Decisions (do NOT re-litigate in leaves)

- O1 Answer every call; gate ACTIONS, not answering.
- O2 Approved = contacts.db new `voice_approved` column (default FALSE;
  unknown number = unapproved). Not config-list based; contacts is the
  system of record.
- O3 Unapproved callers keep an allowed-tool list (message tool +
  ledger_append), config key [voice] unapproved_caller_allowed_tools.
- O4 Ledger state-changing appends by unapproved callers are an ACCEPTED
  residual risk this tree (ledger_append can't distinguish easily at the
  gate); leaf 02-02 pins the shipped behavior in a test and flags this for
  the owner in its report. Do not silently narrow.
- O5 The voice-loop agentID = employee id stamp ships in THIS tree.
- O6 Display-surface redaction is issue #126 / tree 20261110-contact-display,
  NOT this tree.

## Interface Contracts (FROZEN — all leaves)

New package `internal/callerid` (stdlib only; neutral seam between voice
stamping and enforcement reading):

```go
package callerid

type Info struct {
    Number   string // E.164; storage/routing only, never rendered to prompts
    Name     string // resolved contact display name ('' if unknown)
    Approved bool
    Known    bool
}

type ctxKey struct{}
func With(ctx context.Context, info Info) context.Context
func From(ctx context.Context) (Info, bool)
```

contacts (internal/contact): Record gains `VoiceApproved bool`; store
migrate adds guarded ALTER
`ALTER TABLE contacts ADD COLUMN voice_approved INTEGER NOT NULL DEFAULT 0`
(duplicate-column tolerated per callstore.go:118-130 idiom) +
`CREATE INDEX IF NOT EXISTS idx_contacts_phone ON contacts(phone)`;
new method `LookupByPhone(ctx, e164 string) (*Record, bool, error)`
exact match, parameterized.

config [voice] (internal/config/schema.go):
`unapproved_caller_allowed_tools []string`, DefaultConfig =
["ledger_append", <REAL message-tool name>]; leaf 01-identity locates the
actual message-taking tool via grep of internal/tools/builtin (candidates:
note_message / leave_message / message — USE THE REAL NAME) and pins it in
its report; normalize at load (empty -> default).

voice (internal/voice): `Instance` gains `Caller callerid.Info`; seam
`type CallerResolver interface { ResolveCaller(ctx, e164) (callerid.Info, error) }`
in the file's existing seam style; nil/failing resolver -> fail-soft
Info{Number: e164} with WARN — the call ALWAYS proceeds.

daemon (internal/daemon/voice_wiring.go): resolve once at the inbound
answer site (~:291-310, 2s ctx), populate Instance.Caller; stamp on every
turn: bindingTurnRunner.Run (~:1179) adds
`turnCtx = callerid.With(turnCtx, inst.Caller)` ALONGSIDE the existing
employeeIDContextKey stamp. ALSO (O5): voice-cloned loops execute with
executor agent id = the employee id (WithExecutorAgentID precedent,
internal/agent/executor.go:1034; reach via loop.agentID for VOICE clones
only — do not change global defaults).

enforcement (internal/employee/enforcement.go): PreExecChecker gains step
1c after 1b, before Never (update the order comment at :540-545): if caller
Info present AND !Approved AND tool not in the allowed list -> deny, reason
`caller_not_approved`, model-facing text: "This caller is not approved for
that action. Take a message instead." NO auto-pause (contrast Never which
pauses — assert in test). One audit row {type:"voice.unapproved_action",
employee_id, tool, ccid} best-effort via the checker's existing audit seam.
Allowed list threaded at checker construction (manager.RegisterPreExecCheckers
:980 keeps constructor shape stable; options pattern if present).

executor (internal/agent/executor.go ~:1981 details injection): when
callerid.From(ctx) present, details gains caller_approved/caller_known keys
(omit when absent).

Doc fix: docs/workflows/voice-sidecar.md inbound-gating claim rewritten to
the shipped chain (answer-all + per-turn action gate + webhook signature;
voice.allowed_numbers stays outbound-only).

## Child Index

| Child | Type | Deps |
|---|---|---|
| 01-identity/00T | test-gate | — |
| 01-identity/01 | leaf | 00T |
| 01-identity/02 | leaf | 01 |
| 02-enforcement/00T | test-gate | branch 01 |
| 02-enforcement/01 | leaf | 00T(B) |
| 02-enforcement/02 | leaf | 02-01 |

## Dispatch Protocol

1. Dispatch each branch's 00T first (tests ONLY, RED on missing impl,
   Do NOT commit); orchestrator validates IN-SESSION vs the Test Gate
   matrix; commit gate tests; TESTS_VALIDATED.
2. Implementation leaves per order; review in-session; max 3 loops.
3. Orchestrator-only commits with per-file pathspec
   (`git commit -- <paths>` — repo shared with sibling sessions).
4. Tracking table updated at every transition; trickle to master.

## Coding Conventions

Go, gofmt, table-driven stdlib tests (house norm; check package test files
first and match posture). Error prefixes per package norm
('contact:', 'employee:', 'voice:', 'callerid:'). Parameterized SQL only.
Leaves touch ONLY files they own; never edit *_test.go owned by the gate.
Do NOT commit (leaves).

## Test Gate

| Branch | Gate | Status |
|---|---|---|
| 01-identity | 00T | PENDING |
| 02-enforcement | 00T | PENDING (needs branch 01 committed) |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 01-identity/00T | PENDING | | |
| 01-identity/01 | PENDING | | |
| 01-identity/02 | PENDING | | |
| 02-enforcement/00T | PENDING | | |
| 02-enforcement/01 | PENDING | | |
| 02-enforcement/02 | PENDING | | |

## Integration Test Plan

1. go test ./internal/callerid/ ./internal/contact/ ./internal/config/ -count=1
2. go test ./internal/voice/ ./internal/employee/ ./internal/agent/ -count=1
3. go test ./internal/daemon/ -run 'Voice|Caller|Ledger' -count=1 -race
4. e2e voice-lifecycle suite (fake Telnyx): unknown-number inbound call
   completes; step-1c deny observed in daemon logs.
5. Behavior matrix by test: approved -> non-allowed-by-constitution still
   gated by employee constitution (O5 proof); unknown caller -> message tool
   OK, shell_execute denied with pinned text, NO pause, one audit row.
6. go build ./... + go vet clean -> merge plan/caller-authz to main.

## Review Checklist

- [ ] Frozen contract strings present verbatim (caller_not_approved, audit
      type, deny text, config key).
- [ ] Message-tool name choice recorded by leaf 01 and used consistently.
- [ ] Step order comment (enforcement.go:540s) updated to include 1c.
- [ ] No answering-path gating introduced anywhere (O1).
- [ ] Non-voice turns byte-identical (no caller in ctx -> 1c inert).
- [ ] O4 residual risk flagged in 02-02 report to owner.

## Open Questions

- Q1: Should ledger_append for unapproved callers be narrowed to committal-
      only via a payload-kind check in step 1c? Deferred (O4).
      RESOLVED 2026-11-16 (issue #149, commit 44f0e14c): unapproved callers are
      now refused on the work-opening kinds (commitment, resolution) and on
      resolves_id, with WARN provenance and zero rows written; the stranger lane
      keeps the message/observation kinds. set_status refusal unchanged.
- Q2: Signal-leg callers — same stamp path? (Instance.Caller is leg-neutral;
      wire signal leg resolution as a fast-follow in the same file if the
      stamp site is shared.)
      OUT OF SCOPE 2026-11-16 (issue #149): the signal path carries no
      caller-resolution seam — internal/daemon/signal_wiring.go has zero
      callerid references, and RegisterMessages/RouteInbound are never wired
      to any stamping site. Mirroring 00a0071b would require building contact
      resolution into the signal leg (new plumbing, not a fast-follow).
      Signal-leg inbound callers therefore run unauthenticated-by-design
      (no caller in ctx -> step 1c inert, byte-identical non-voice posture).
      Follow-up if the signal leg ever fronts human-facing state changes:
      resolve the sender against contacts.db and stamp callerid.Info, then
      file a new issue referencing this note.
