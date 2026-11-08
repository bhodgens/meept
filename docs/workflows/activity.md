# activity (agent activity ledger + audit views)

One read shape over every activity silo the daemon keeps: task steps, session
messages, audit-chain records, employee audit findings, plus a live ring of
bus events. The aggregator maps them into `ActivityRecord` (master contract
C1 in `docs/plans/20261030-agent-activity-audit-views/master.md`), and four
RPC queries plus `agents.review` expose the merged view to the TUI, the
Flutter GUI, and any HTTP client.

Feature mapping: `internal/activity/` (aggregator, ring, bus bridge),
`internal/daemon/activity_wiring.go` + `internal/daemon/activity_rpc.go`
(wiring + RPC surface), `internal/comm/http/activity_handlers.go` (HTTP),
`internal/tui/models/activity.go` (TUI), `ui/flutter_ui/lib/features/activity/`
(GUI), `internal/config/otlp_traces.go` + `internal/metrics/otlp_trace_*.go`
(trace sink).

## what the ledger records

`ActivityRecord` (`internal/activity/types.go`) is the byte-identical read
contract — do not edit field order or json tags without updating the master
contract. Every record carries `kind`, `at` (RFC3339, primary sort key),
`summary` (one-line, envelope-stripped, <=200 runes), optional
`agent_id`/`session_id`/`task_id`/`step_id`/`plan_id`/`state`/`severity`,
and a string-map `detail`.

Four kinds come from the aggregator (`internal/activity/aggregator.go`),
which merges rows from four silos through narrow read seams
(`internal/activity/adapters.go`, `sources.go`); a missing store handle
degrades to fewer record kinds, never an error:

| kind | silo | source handle |
|------|------|---------------|
| `task_step` | task registry + step store | `*task.Registry`, `*task.StepStore` |
| `session_message` | session store | `session.Store` |
| `audit_record` | audit hash chain | `*auditlog.Store` (shared DB pool via `employee.Manager.AuditChain`) |
| findings | employee audit findings | `*employee.AuditStore` (severity `info\|warning\|critical`) |

Step results and message content pass through envelope stripping
(`internal/activity/envelope.go`, a local re-implementation of
`agent.StripClaimsEvidenceOrProse` kept import-cycle-free), so machine-facing
claims/evidence JSON never reaches the user-facing summary.

The fifth kind, `bus_event`, is owned by the ring leaf: `WireBusBridge`
(`internal/activity/busbridge.go`) subscribes to `queue.job.claimed/completed/
failed/retry/requeue/recovered`, `task.completed`/`task.failed`,
`agent.progress`, and the typed `turn.terminal` topic, mapping each payload
into a record. Handler panics are recovered and logged; appending is
best-effort and never blocks publishers. Identifier spaces are kept honest:
queue `job_id`/`worker_id` go in `detail`, never into `task_id`/`agent_id`.

Plan linkage is inverted (`plans.task_id` points at the parent task); the
daemon wiring supplies a `PlanResolver` built on `PlanManager.GetPlan` so the
package never imports `internal/plan`. The resolver powers plan stamping, the
`plan_id` filter, and the `plan` sort mode.

## the ring buffer (live tail)

`activity.Ring` (`internal/activity/ring.go`) is a fixed-capacity,
append-only, mutex-protected ring: oldest records are evicted when full, and
the total eviction count is exposed as `dropped`. The daemon wires it at
capacity 1000 (`activityRingCapacity` in
`internal/daemon/activity_wiring.go`, ~1 MB). `Tail(n)` returns the newest n
records in chronological order plus the dropped counter. On shutdown
`stopActivityRing` unsubscribes the bridge so reader goroutines do not leak
across in-process restarts.

## RPC surface

Registered by `registerActivityRPC` (`internal/daemon/activity_rpc.go`) as
direct closures (no bus proxies), nil-guarded per method, refusing to
clobber sibling surfaces (e.g. the employee `agents.audit.*` handlers):

| method | params | result |
|--------|--------|--------|
| `activity.query` | `since`/`until` RFC3339 (second precision), `sort` `chrono\|task\|plan` (empty = chrono), `task_id`, `plan_id`, `agent_id`, `kinds` list, `limit`/`offset` | `{"records":[ActivityRecord],"total":N}` — total counted BEFORE pagination |
| `activity.recent` | `limit` (default 50) | `{"records":[ActivityRecord],"dropped":N}` from the ring |
| `activity.overview` | none | `{"by_agent":[{agent_id, active_tasks, last_activity, drift_score, open_findings}], "generated_at"}` — agents with no activity are omitted |
| `activity.findings` | `employee_id`, `since`, `severity`, `resolved` (optional bool), `limit` (default 100)/`offset` | `{"findings":[AuditFinding],"total":N}` |
| `agents.review` | `employee_id` (required) | the `Manager.Review` struct (drift score + recent findings + employee + goals) |

## HTTP routes

`internal/comm/http/activity_handlers.go` bridges GET routes onto the RPC
methods (the GUI's transport):

- `GET /api/v1/activity` -> `activity.query` (same params as query-string;
  `kinds` comma-separated; `limit` default 50, max 1000)
- `GET /api/v1/activity/recent` -> `activity.recent` (default 50, max 1000)
- `GET /api/v1/activity/overview` -> `activity.overview`
- `GET /api/v1/activity/findings` -> `activity.findings` (default 100, max 1000)
- `GET /api/v1/agents/{id}/review` -> `agents.review`

Route registration lives in `internal/comm/http/server.go`.

## TUI + GUI surfaces

TUI (`internal/tui/models/activity.go`): a sortable activity table (chrono /
by task / by plan, 6 columns), a live tail panel polling `activity.recent`
every 2s (`activityPollInterval` — client pull, never server push; the
ticker arms only while the view is visible), and a findings + drift panel
over `activity.findings` + `agents.review`. Entry: the `view_activity`
keybinding (default `a`, `~/.meept/client.json5` keybindings) and the
command palette ("activity view — agent activity, findings + drift",
`internal/tui/config.go`). Switching away tears the tail poller down
(`HideActivityView`).

GUI (`ui/flutter_ui/lib/features/activity/`, 6 dart files): `activity_tab.dart`
composes the same four sections at TUI parity — `activity_list.dart` (sortable
query view), `activity_recent_panel.dart` (live tail), `activity_findings_panel.dart`
and `activity_drift_panel.dart` — over the shared `activity_models.dart`, polling
the HTTP routes above.

## OTLP/Langfuse trace sink (default-off)

Separate from the ledger but shipped in the same plan tree (leaf
07-langfuse-sink): `metrics.otlp_traces` (`internal/config/otlp_traces.go`)
exports chat turns and task steps as OTLP/HTTP protobuf spans, aimed at (but
not coupled to) self-hosted Langfuse v3+. **Default off**: with
`enabled = false` (or an absent block) the wiring constructs no exporter, no
producer, and no bus subscriptions — zero network activity, one boot-time
Warn line. This is the documented opt-in posture
(`docs/agents/opt-in-defaults.md`); the full span/attribute contract lives in
`docs/workflows/llm-tracing.md`.

Config keys (`~/.meept/meept.json5`, `[metrics.otlp_traces]`):

| key | default | meaning |
|-----|---------|---------|
| `enabled` | `false` | opt-in switch |
| `endpoint` | `http://localhost:4318` | receiver base URL; spans POST to `<endpoint>/v1/traces` (Langfuse: `http://langfuse:3000/api/public/otel`) |
| `headers` | — | extra HTTP headers (auth, `x-langfuse-ingestion-version: 4`); secret material travels ONLY here and is never logged |
| `sample_rate` | `1.0` | fraction exported, clamped to (0, 1]; deterministic per trace id |
| `export_interval` | `5s` | batch flush cadence |

Implementation: `internal/metrics/otlp_trace_sink.go` (hand-rolled protobuf
writer over stdlib `net/http`, circuit breaker), `otlp_trace_exporter.go`,
`otlp_trace_producer.go` (bus-side span production — the agent loop never
blocks on export), `otlp_trace_sampling.go`; wiring in
`internal/daemon/otlp_traces_wiring.go`. Export is best-effort and never
fails a turn.

## e2e coverage

`e2e/suites/activity-views/` (build tag `e2e`, manifest entry
`activity-views` -> `internal/activity/`): boot-log arming + `activity.query`
paging, sort modes and filters, the live ring via `activity.recent`, and
overview/findings/review. Run with `make e2e-fast-area AREA=activity-views`.

## related

- trace sink detail: `docs/workflows/llm-tracing.md`
- employee findings + review semantics: `docs/workflows/employees.md`
- opt-in defaults policy: `docs/agents/opt-in-defaults.md`
- plan tree of record: `docs/plans/20261030-agent-activity-audit-views/master.md`
