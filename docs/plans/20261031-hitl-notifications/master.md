# HITL Notifications: Slack Approval Gates, Checkpointed Pause/Resume, Deep Links - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 6 leaf documents under this node (flat tree)
- **Scope:** Close the loop on human-in-the-loop gates: approvals answerable
  from Slack, gate pauses that survive restart and resume from checkpoint,
  clickable dock/menubar deep links into the session/activity surface, and a
  harness that measures which autonomy regime (per-action, plan-gate-lead,
  autonomous + sampled) actually earns its interruptions.

## Goal

Meept already has a strong human permission relay (internal/acp/relay.go:
pending registry, fail-closed, unanswered requests cancel after
[acp] relay_timeout, default 120 s; approver attribution via
internal/acp/approver.go) but the human must be at the keyboard: the only
answer surfaces are the TUI (internal/tui/acp_permission.go) and the Flutter
GUI (sdk_client.dart:374-395). An operator away from the machine cannot
approve, and any interruption still costs the agent its turn progress because
plan and goal gates (internal/services/plan_service.go:182,
internal/employee/goal.go:208 + goal_loop.go:1050-1189) have no checkpointed
resume. Nothing in the menubar notification tap actually opens a view
(NotificationManager.swift:288-310 only marks read). And nobody has measured
whether the interruption regime itself is worth its cost.

This tree delivers four approved workstreams:

(a) a Slack approval channel - a new PushChannel implementation with
    interactive buttons that round-trips into acp.permissions.resolve,
    attributed as a new `slack` Approver kind, honoring the 120 s
    fail-closed timeout and verifying Slack request signatures;
(b) checkpointed pause/resume - persist pending gate state plus turn
    re-entry data so approval can arrive OUT-OF-BAND (Slack, CLI, after a
    daemon restart) and the turn resumes from checkpoint, reusing the
    TurnParker pattern (internal/agent/parked_turn.go) - never a second
    park path;
(c) dock/menubar deep links - `meept://session/<id>` URL scheme into the
    Flutter macOS app plus a menubar tap-through handler, with a web
    fallback URL served by the existing internal/comm/web substrate;
(d) a HITL regime evaluation harness - run identical hermetic e2e scenarios
    under three regimes (per-action relay, plan-approve-lead with
    gate_command, autonomous + sampled post-hoc spot-check) and report
    completion rate, wall-clock, interrupt count, intervention precision,
    and per-risk-class escape rate, grounded in the auditlog hash chain,
    the permission table RuleSource, and acp.permission.decision events.

Integration with the in-flight activity-views tree
(docs/plans/20261030-agent-activity-audit-views/, approved): the deep-link
TARGET surface is that tree's Flutter activity tab and its HTTP endpoints
(`GET /api/v1/activity`, `/api/v1/activity/recent`, `/api/v1/activity/overview`,
`/api/v1/activity/findings`) exposed under the go_router route `/activity`.
DO NOT reference that tree's files as spec - the dependency is described
inline here and the deep link MUST feature-gate: when the `/activity` route
is absent (activity-views not yet merged), deep links fall back to the web
session URL. Prior art: docs/plans/20260609-menubar-desktop-notifications-
implementation.md is Pending-Approval and was never executed - cite as
prior art only, never as an implemented baseline.

## Architecture

Four independent subsystems meet at three seams. First, the Slack channel
(internal/comm/slack) is a services.PushChannel implementation registered on
the existing ChannelRegistry (internal/services/push_channels.go:34-56,
wired at internal/daemon/components.go:2114) that additionally consumes the
acp PermissionNotifier seam (relay.go:94-107) so a permission request
publishes as an interactive Slack message; its HTTP callback endpoint
verifies the Slack signature, maps the button action to a relay Resolve with
Approver{Kind:"slack", Detail:"<slack uid>"}, and lets the EXISTING relay
timeout/fail-closed machinery stay the only timeout authority. Second,
checkpointed gates reuse the TurnParker (internal/agent/parked_turn.go) with
a new park-kind scope and its existing SetParkPersistence store - plan and
goal gate pauses park the turn with re-entry data; the existing approve /
resolve paths trigger the resume callback; no new park machinery. Third,
deep links are a thin client concern: a Dart module parses
`meept://session/<id>`, checks whether the `/activity` route is registered
(feature gate), and either navigates or opens the web fallback URL; the
Swift menubar tap handler builds the same URL from the notification
userInfo (sessionID key already present) and opens the app. Fourth, the
regime harness (internal/eval) drives the same scripted hermetic scenario
under three regime configurations and computes metrics from ground-truth
sources that already exist (auditlog chain, permission table RuleSource,
acp.permission.decision bus events). The e2e leaf wires everything into a
hermetic suite + manifest entry.

## Interface Contracts

<!-- Each contract: ID + ONE signature block. Owning + consuming leaves.
     Leaves list consumed contract IDs + exact signatures only - never
     restate full definitions in a leaf; the parent holds them once. -->

### C1: SlackChannel (services.PushChannel implementation)

```
// internal/comm/slack/channel.go - Owner: 01-slack-channel
// Consumers: daemon wiring (internal/daemon/components.go), 05-e2e
func NewSlackChannel(cfg config.SlackConfig, logger *slog.Logger) *SlackChannel
func (c *SlackChannel) CanReceive(ctx context.Context, sessionID string, msg *services.PushMessage) bool
func (c *SlackChannel) Push(ctx context.Context, msg *services.PushMessage) error
// PushType stays the existing enum (notification|alert|summary,
// internal/services/push_service.go:22-24). No new PushType values.
```

### C2: SlackConfig ([comm.slack] config block)

```
// internal/config/schema.go - Owner: 01-slack-channel
// Consumers: 01-slack-channel, 05-e2e, daemon wiring
type SlackConfig struct {
    Enabled        bool     `json:"enabled"`          // default false
    BotToken       string   `json:"bot_token"`        // or ${VAR} / file: path
    SigningSecret  string   `json:"signing_secret"`   // or ${VAR} / file: path
    ChannelID      string   `json:"channel_id"`
    AllowedUserIDs []string `json:"allowed_user_ids"` // empty = allow none
    APIBaseURL     string   `json:"api_base_url"`     // default https://slack.com/api; override for hermetic tests
}
// Validation: enabled requires bot_token + signing_secret + channel_id
// non-empty; validation error at config load otherwise (pattern: the
// [acp] relay_timeout validation, schema.go:2662-2714).
```

### C3: Slack interaction callback

```
// Owner: 01-slack-channel; Consumers: 05-e2e
// POST /api/v1/slack/interactions  (registered on the unified HTTP server)
// Headers: X-Slack-Signature ("v0=<hex hmac-sha256>"),
//          X-Slack-Request-Timestamp (replay window: 5 minutes).
// Signature base string: "v0:<timestamp>:<raw body>" keyed with
// signing_secret (verified against R0 research notes; R0 is the source
// of truth for exact constants).
// Body: application/x-www-form-urlencoded; payload JSON
//   type "block_actions" | "interactive_message".
// Action value grammar (pinned): "perm:<requestID>:<verb>" where verb is
//   allow | deny | always (matches the relay verbs, relay.go:82-86).
// Effect: relay Resolve(requestID, verb, Approver{Kind: "slack",
//   Detail: "<slack user id>"}). Unknown/expired requestID: HTTP 200 with
//   an ephemeral "no pending request" response (never a 500-retry loop).
```

### C4: Approver kind "slack"

```
// internal/acp/approver.go - Owner: 01-slack-channel
// Consumers: 02-checkpointed-gates, 05-e2e
// New Kind value "slack": String() renders "slack:" + Detail
// (e.g. "slack:U123ABC"). Self-approval and identity enforcement rules
// are unchanged: an in-process resolve can never produce Kind "slack".
```

### C5: Gate park checkpoint (TurnParker reuse)

```
// internal/agent - Owner: 02-checkpointed-gates
// Consumers: 05-e2e
// New ParkKind constant ParkKindGate (same scoping semantics as the
// existing chat/episode kinds: kind-scoped persistence rows via
// SetParkPersistence, internal/agent/parked_turn.go:587-596).
type GateParkData struct {
    GateKind    string         `json:"gate_kind"`    // "plan" | "goal"
    GateID      string         `json:"gate_id"`      // plan id / goal id
    SessionID   string         `json:"session_id"`
    TurnReentry map[string]any `json:"turn_reentry"` // turn re-entry payload
    RequestedAt time.Time      `json:"requested_at"`
}
// Rules: the park goes through the EXISTING TurnParker.Park path (never a
// second park path); the resume callback re-enqueues the turn; an
// at-most-once resume (existing delete-before-callback) is preserved;
// daemon restart re-arms surviving rows (existing Start behavior).
```

### C6: Deep link grammar and feature gate

```
// Owner: 03-deep-links; Consumers: 05-e2e
// URL: meept://session/<session-id>
//   - scheme literal "meept"; host literal "session";
//   - <session-id> matches ^[A-Za-z0-9-]+$ (Go/Dart/Swift regex mirror).
// Dart module: ui/flutter_ui/lib/core/deep_link.dart
//   - resolveDeepLink(Uri) -> DeepLinkTarget{sessionID, useWebFallback}
//   - feature gate: if the go_router route "/activity" is not registered,
//     useWebFallback is true and the target URL is
//     <web base>/#/activity?session=<id> served by internal/comm/web.
// Swift: menubar tap handler builds the same URL from userInfo["sessionID"]
//   and opens the Flutter app with it (NotificationManager.swift:288-310).
// Web fallback route param handling lives in the Dart module; the menubar
// never scrapes daemon HTTP endpoints directly.
```

### C7: HITL regime metrics

```
// internal/eval - Owner: 04-regime-evaluation; Consumers: 05-e2e
type RegimeMetrics struct {
    Regime                string             `json:"regime"` // "per_action" | "plan_gate_lead" | "autonomous_sampled"
    ScenarioID            string             `json:"scenario_id"`
    CompletionRate        float64            `json:"completion_rate"`         // turns completed / attempted
    WallClockSeconds      float64            `json:"wall_clock_seconds"`
    InterruptCount        int                `json:"interrupt_count"`
    InterventionPrecision float64            `json:"intervention_precision"` // interventions that changed outcome / total interventions
    EscapeRate            map[string]float64 `json:"escape_rate"`            // by risk class: autonomous actions a spot-check would block
}
// Report: HitlRegimeReport{Scenarios []RegimeMetrics, GeneratedAt time.Time}
// serialized as JSON + a markdown summary. Ground truth (ONLY these):
// auditlog hash-chain records, the permission table RuleSource column,
// and acp.permission.decision bus events.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf | none | 30K | A |
| 01 | 01-slack-channel.md | leaf | R0 | 90K | B |
| 02 | 02-checkpointed-gates.md | leaf | none | 85K | B |
| 03 | 03-deep-links.md | leaf | none | 90K | B |
| 04 | 04-regime-evaluation.md | leaf | none | 85K | B |
| 05 | 05-e2e.md | leaf | 01, 02, 03, 04 | 80K | C |

Same-letter children have no inter-dependencies and dispatch together
(max 3 per batch; group B has four - dispatch in two batches).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Research gate

Dispatch R0-research.md first, alone. It produces
`docs/plans/20261031-hitl-notifications/research-notes.md` with (A) verified
Slack interactive-messaging security constants (request signing scheme and
exact base string, timestamp replay tolerance, response_url semantics and
limits, bot token scoping) and (B) HITL regime evaluation methodology
(intervention precision, escape-rate metrics for autonomy regimes). On
return, review the notes in-session: every constant that leaf 01 consumes
must carry a source URL and a verbatim quote. Then inline the relevant
sections into 01-slack-channel.md and 04-regime-evaluation.md dispatch
contexts.

### Phase 1: Dispatch Concurrency Group B

Dispatch 01, 02, 03 (batch 1) then 04 (batch 2) via `delegate_task`: goal =
"Implement all tasks from <leaf>"; context = full leaf text + the parent
contracts C1-C7 + coding conventions + relevant source INLINED; include the
no-commit, no-git-add, no-read_file, write-once-and-stop rules. Note: leaf
03 spans Dart + Swift; it is authored as two self-contained halves sharing
the pinned C6 grammar - dispatch it as ONE agent but the brief must state
both halves complete before reporting.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts, Review Checklist; run the leaf verify commands; pass:
commit + status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles,
then escalate to the user.

### Phase 3: Integration (Group C)

After all group B children are REVIEWED: dispatch 05-e2e.md, review, commit.
Then run the full Integration Test Plan below, `make graphs` (new HTTP route
+ bus topics change topology), regenerate docs per the root AGENTS.md
documentation rule, update the root AGENTS.md Key Components table for the
new internal/comm/slack package, and set all children COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (C1-C7 signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed); e2e leaf suite green via
      `make e2e-fast-area AREA=hitl-notifications`
- [ ] Code follows project conventions (root + package AGENTS.md chains)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] Security spot-checks for this tree specifically: signing verification
      constant-time compare, replay window enforced, signing secret never
      logged, AllowedUserIDs enforced on every action, self-approval still
      impossible, fail-closed timeout never bypassed
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Flutter changes: `flutter test` + `dart format` run before staging

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go (module github.com/caimlas/meept), Dart/Flutter
  (ui/flutter_ui), Swift (menubar/MeeptMenuBar). ASCII only in all new
  files, including docs.
- **Naming:** exported Go identifiers for cross-package seams only; unexported
  otherwise. Lowercase ALL user-visible UI text (TUI/GUI/menubar), e.g.
  "approve", not "Approve".
- **Imports:** stdlib group, then module-internal group, then external; no
  unused imports; no import cycles (internal/comm/slack may import
  internal/services and internal/acp; internal/acp must NEVER import
  internal/comm/slack - the Approver kind is a string, no import needed).
- **Error handling:** wrap with context and return early; no ignored errors;
  no bare panic(err); never hold a mutex across I/O (mutexio analyzer).
- **IDs:** never time.Now().UnixNano() or math/rand - pkg/id.Generate() only
  (predid analyzer).
- **Testing:** all new FEATURE tests in the hermetic e2e tier
  (e2e/suites/hitl-notifications + e2e/manifest.json entry); unit tests are
  allowed inside the new packages for pure logic (signature verification,
  URL parsing, metrics math) - no new unit test files for features in
  existing packages. Go tests run with -p 2. Flutter: `flutter test` +
  `dart format` before staging.
- **Config:** JSON5 with template in config/ and drift annotations where
  relevant; opt-in defaults stay default-off (slack enabled=false is a
  product default - do not flip).
- **Docs:** every new feature gets docs/workflows/ coverage and an AGENTS.md
  summary update in the same commit (root AGENTS.md maintenance rule).

## Requirement Traceability

| Req | Source (approved scope, verbatim intent) | Plan element | Verified by |
|-----|------------------------------------------|--------------|-------------|
| R1 | Slack approval channel: new PushChannel with interactive buttons -> resolve callback -> acp.permissions.resolve round trip | 01-slack-channel.md T1-T4; C1, C3 | e2e suite scenario 1 |
| R2 | signing-secret verification per research | R0-research.md section A; 01 T3 | 01 unit tests (bad signature, stale timestamp) |
| R3 | slack:<uid> Approver kind | 01 T4; C4 | 01 unit test (attribution renders slack:<uid>) |
| R4 | 120 s fail-closed timeout honored | 01 T2/T3 (relay stays timeout authority); C3 note | e2e scenario 1 (late answer -> "no pending request") |
| R5 | checkpointed pause-resume: persist pending gate + turn re-entry; out-of-band approval; TurnParker precedent; never a second park path | 02-checkpointed-gates.md T1-T3; C5 | e2e scenario 2 |
| R6 | meept://session/<id> deep links + menubar tap-through + web fallback; feature-gated on the /activity route | 03-deep-links.md T1-T4; C6 | 03 dart tests + e2e scenario 3 |
| R7 | regime evaluation harness: three regimes, completion/wall-clock/interrupt/precision/escape metrics, ground truth from auditlog + RuleSource + acp.permission.decision | 04-regime-evaluation.md T1-T4; C7 | 04 unit tests + e2e scenario 4 |
| R8 | hermetic e2e suite hitl-notifications + manifest entry; scripted-human pattern (step-pipeline precedent) | 05-e2e.md T1-T3 | make e2e-fast-area AREA=hitl-notifications |

## Completion Tracking Table

<!-- Single source of truth for subtree status; parent reads this. -->

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research | COMPLETE | 1 | c5ef67e9: research-notes.md - verified Slack signing constants (sourced/quoted) + regime methodology |
| 01-slack-channel | COMPLETE | 1 | 71312c96: Slack PushChannel, Block Kit, signed interactions endpoint, slack:<uid> approver, relay stays timeout authority; schema close 3723542d |
| 02-checkpointed-gates | COMPLETE | 1 | 52148db1: ParkKindGate TurnParker over shared parks.db, GateParkData (C5), resumeByKey at-most-once, restart re-arm, plan.approved pump; orphaned hitl C4 gate landed 8ef5c90c |
| 03-deep-links | COMPLETE | 1 | 8aa404da: meept://session/<id> deep links, Flutter DeepLinkTarget with route-presence feature gate, menubar tap-through via NSWorkspace |
| 04-regime-evaluation | COMPLETE | 1 | 8f053abd: three-regime harness + ground-truth metrics in internal/eval/hitl (Go tests; no dart surface in this leaf) |
| 05-e2e | COMPLETE | 1 | e7a966d4: hitl-notifications e2e suite (signed round trip, late-click fail-closed, park->restart->resume, deep-link web substrate, regime report) + manifest entry; issue 86 closed |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

Run after 05-e2e is REVIEWED. All commands from the repo root.

```
go build ./...                                        # clean build
go test -p 2 ./internal/comm/slack/ ./internal/agent/ ./internal/eval/ ./internal/acp/
make e2e-fast-area AREA=hitl-notifications            # new suite green
make e2e-affected                                     # no sibling suite regressed
cd ui/flutter_ui && flutter test && dart format --set-exit-if-changed --output=none lib test
make graphs && make graphs-check                      # new HTTP route registered in topology
make lint-ci                                          # analyzers + audits
```

Cross-boundary checks (contracts exercised end to end):
- C1/C3: a signed interaction callback resolves a pending permission; the
  audit trail shows Approver kind "slack" (C4).
- C4/timeout: an interaction arriving after relay_timeout gets
  "no pending request" and the permission record shows reason "timeout"
  (fail-closed preserved).
- C5: a plan gate park survives a daemon restart and resumes when the
  approval arrives out-of-band (e2e scenario 2).
- C6: with no /activity route, the deep link resolves to the web fallback
  URL and internal/comm/web serves the session page (e2e scenario 3).
- C7: one scripted scenario run under all three regimes yields a report
  with every C7 field populated (e2e scenario 4).

## Test Gate

Deliberate deviation from the default per-branch 00T test-gate shape: this
repo's e2e policy (root AGENTS.md) places all new FEATURE tests in the
hermetic e2e tier, and the approved scope assigns the suite to leaf 05.
Each implementation leaf still carries its own TDD unit tests (RED before
implementation per task tables), and leaf 05 owns the cross-cutting e2e
gate. The matrix below is the coverage contract; the orchestrator validates
it against returned tests in-session and applies the delta protocol
(update the LESSER artifact; converge upward).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | Slack signature verification (good/bad/stale/replay) | 01 T3, C3 | TestInteractionsRejectsBadSignature / RejectsStaleTimestamp / AcceptsFreshTimestamp / VerifiesRawBody - internal/comm/slack/interactions_test.go | hitl-notifications:1 | VALIDATED |
| F2 | Button action -> relay resolve round trip with slack:<uid> attribution | 01 T3-T4, C3, C4 | TestInteractionsResolvesWithSlackApprover, RejectsDisallowedUser - internal/comm/slack/interactions_test.go | hitl-notifications:1 | VALIDATED |
| F3 | Fail-closed: late/expired action gets "no pending request", permission times out denied | 01 T3, R4 | TestInteractionsUnknownRequestStill200 - internal/comm/slack/interactions_test.go | hitl-notifications:1 | VALIDATED |
| F4 | Gate park persists + resumes out-of-band (plan and goal gates) | 02 T1-T3, C5 | TestGateParkPersistsKindScoped / PersistsThroughSQLiteStore / PlanResumeAtMostOnce / GoalRestartReArmLateApproval / KindIsolation - internal/agent/gate_park_test.go; wiring: internal/daemon/gate_park_wiring_test.go | hitl-notifications:2 | VALIDATED |
| F5 | Deep link parse + feature gate + web fallback URL | 03 T1-T3, C6 | 20 flutter deep-link tests - ui/flutter_ui/test/core/deep_link_test.dart + deep_link_nav_test.dart | hitl-notifications:3 | VALIDATED |
| F6 | Menubar tap opens deep link (userInfo sessionID -> URL) | 03 T2 | Swift handler landed (menubar/MeeptMenuBar/Services/NotificationManager.swift tap-through + GuiLauncher.swift NSWorkspace); no Swift test harness - grammar verified via the dart-mirrored deep_link_test.dart, as the plan anticipated | n/a - no Swift test harness; handler delegates parsing to pinned C6 grammar | VALIDATED |
| F7 | Regime metrics computed from ground-truth sources | 04 T1-T3, C7 | TestMetrics* - internal/eval/hitl/metrics_test.go; harness TestHarness* + TestRegimeOverlayMapping - harness_test.go; TestReport* - report_test.go | hitl-notifications:4 | VALIDATED |
| F8 | Manifest + path_map entry for new packages | 05 T3 | n/a - manifest JSON | e2e/manifest.json suite hitl-notifications (path_map + suite entry); suite at e2e/suites/hitl-notifications/ | VALIDATED |

Delta log: (empty - entries appended as date, feature ID, direction, reason)

## Open Questions

1. Slack app distribution: this tree assumes a per-installation Slack app
   (user creates their own app, pastes bot token + signing secret). A
   distributed/OAuth-install flow is out of scope unless the user says
   otherwise.
2. Goal-gate resume edge: goal_loop currently re-evaluates gates on its own
   cadence; if checkpointed goal parks duplicate that re-entry path in
   practice, the orchestrator should escalate rather than let leaf 02
   invent a second scheduler.
3. Escape-rate risk classes: leaf 04 starts with the classes derivable
   from the permission table (RuleSource + decision). A richer taxonomy
   (per-tool risk weights) is future work pending R0 methodology findings.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- The activity-views tree (docs/plans/20261030-agent-activity-audit-views/)
  is IN FLIGHT. Leaf 03 must not import or require anything from it: the
  dependency is the `/activity` go_router route + the four
  `/api/v1/activity*` endpoints described inline in contract C6, and the
  feature gate must degrade cleanly when they are absent.
- Prior art doc docs/plans/20260609-menubar-desktop-notifications-
  implementation.md is Pending-Approval and was never executed. Cite it in
  research/notes only; do not treat any of its claims as implemented.
- Checkpoint struct (internal/agent/retry_recovery.go:26) is crash-recovery
  ONLY - it is NOT the approval gate mechanism. Leaf 02 uses the TurnParker.
- notification_prefs.go already anticipates a per-type "requires_approval"
  key - leaf 01 may read it for opt-in but must not redefine push types.
- macOS full-sweep tests MUST run with -p 2 (ephemeral port exhaustion).
