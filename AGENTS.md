# AGENTS.md

Guidance for AI coding agents working in this repository.

**This file must be reviewed and validated for completeness and correctness on
every commit.** If a commit adds, removes, or renames a package, changes a build
command, introduces a new convention, or invalidates any statement below, update
this file in the same commit. Stale agent guidance causes more bugs than no
guidance at all.

## Build & Development Commands

```bash
# Build
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
make build              # Everything (daemon + CLI + gendoc + GUI + lite + graphs)
make build-gui          # Flutter GUI only
make menubar-install    # macOS MenuBar app

# Test
go test ./... -v
go test -race ./...
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out

# Run
./bin/meept-daemon -f           # Daemon foreground
./bin/meept chat "message"      # CLI chat
./bin/meept chat                # Interactive TUI
agent-tui ./bin/meept chat      # TUI testing

# Config
./bin/meept config              # Interactive editor
./bin/meept config get <key>    # Get value
./bin/meept config set <key> <v> # Set value

# Agents (AI Employees) — not the dispatcher roster in config/agents/
./bin/meept agents list                 # List employees
./bin/meept agents show <id>            # Full definition
./bin/meept agents create <def.json5>   # Register new employee
./bin/meept agents pause <id> / resume <id>
./bin/meept agents goals [--employee=<id>]
./bin/meept agents set-gate <goal-id> --command="go test ./..."
./bin/meept agents audit <id> [--since=6h]

# Plans
./bin/meept plans list/show/approve/reject/confirm <id>

# Projects
./bin/meept projects list/add/remove/sync/status <name>

# Connectivity graphs
make graphs               # Regenerate bus/RPC/HTTP/WS topology
make graphs-check         # Verify generated files are fresh (CI)
make research-harness      # verify harness catalog evidence + regenerate techniques md
make research-harness-check # CI: fail if evidence missing or techniques md stale

# Git hooks (bash >= 4 required on macOS; sub-hooks use /opt/homebrew/bin/bash)
make hooks                # core.hooksPath -> .githooks (15 pre-commit checks)

# Static analyzers
make analyzers            # mutexio + predid
make lint-ci              # golangci-lint + analyzers + audit scripts
```

See `cmd/meept/`, `cmd/meept-daemon/`, and `Makefile` for full command reference.

## Architecture Overview

Meept is a **Go daemon** with skill-based task orchestration, LLM integration,
memory management, and external integrations.

### Request Flow

```
User Input → CommServer (RPC/HTTP) → MessageBus → AgentLoop → Dispatcher → Planner → Tools → Response
```

### Key Components

| Layer | Packages |
|-------|----------|
| **Server** | `cmd/meept-daemon`, `internal/daemon`, `internal/rpc`, `internal/bus`, `internal/comm` |
| **Agent** | `internal/agent` (loop, orchestrator, planner, collaborative, workspace, executor, dispatcher) |
| **LLM** | `internal/llm` (client, resolver, budget, providers, token cache, context firewall) |
| **Memory** | `internal/memory` (manager, episodic, task, ftstore, facts) |
| **Tools** | `internal/tools` (registry, builtin/*, mcp), `internal/acp` (ACP client wire) |
| **Security** | `internal/security` (engine, sanitizer, tirith, tls, fence), `internal/auth` (multi-user store: users/keys/expiry, quota+permission stubs) |
| **Employee** | `internal/employee` (constitution, goal, goal_loop, enforcement, authority, manager) |
| **Session** | `internal/session` (store, store_sqlite, threads, messages) |
| **Services** | `internal/services` (chat, session, terminal, push, reflection) |
| **Project** | `internal/project` (manager, init_deep, detection) |
| **TUI** | `internal/tui` (app, commands, components, handlers, modals, models) |
| **GUI** | `ui/flutter_ui` (Flutter web + desktop) |
| **Scheduling** | `internal/scheduler`, `internal/queue`, `internal/worker` |
| **Skills** | `internal/skills`, `internal/selfimprove` |
| **Infra** | `internal/config`, `internal/metrics`, `internal/transport`, `internal/pty`, `internal/eval`, `internal/gate` |

See `docs/concepts/architecture.md` for full documentation.

### Connectivity Graph (Bus / RPC / HTTP / WS)

The daemon's components communicate via string-typed bus topics, RPC handlers,
and HTTP routes — all invisible to the compiler. A generated connectivity graph
maps every publish/subscribe edge so you can trace cross-boundary data flow
without reading the code:

- **`docs/generated/bus-topology.md`** — human-readable: every bus topic with
  publishers, subscribers, payload keys; orphan analysis; WS event
  classification; RPC handler map; HTTP route map.
- **`docs/generated/bus-topology.json`** — machine-readable version.
- **`docs/generated/rpc-handlers.json`**, **`http-routes.json`**,
  **`ws-event-map.json`** — individual layer exports.

Regenerate with `make graphs` (runs automatically on every `make build`).

**When debugging cross-boundary issues** (events not reaching clients, wrong
payload fields, orphaned listeners), start with `docs/generated/bus-topology.md`
before reading source.

## Critical Invariants

### session_id vs conversation_id

`session_id` (primary key, e.g. `session-abc123`) and `conversation_id`
(internal, e.g. `conv-xyz789`) are distinct identifiers. The Flutter client
sends `session_id` as `conversation_id` in chat requests. WS subscriptions use
`session_id`. Bus events carry `conversation_id`. New event/filter/routing code
MUST handle both. The WS filter in `internal/comm/http/server.go` falls back
from `session_id` to `conversation_id` — preserve this.

### Multi-user is opt-in; RPC stays owner-trusted

`multiuser.enabled` (default **false**) gates per-user key auth. When off:
the daemon never constructs `internal/auth.Store`, HTTP auth uses the legacy
flat-key path byte-identically, and sessions have no owner — every visible
session belongs to everyone. Code touching auth, session ownership, or
identity MUST preserve the disabled-path behavior exactly.

Two trust boundaries, never mix them:

- **HTTP/WS** (`transport.http`): bearer-key identity. In multi-user mode,
  keys map to `*auth.Identity` via `IdentityFromContext`; expired/unknown
  keys get 418. Sessions created through it are owner-scoped.
- **Unix RPC** (`transport.rpc`): kernel-enforced same-OS-user only (0600
  socket, optional SO_PEERCRED/LOCAL_PEERCRED UID allowlist). It has NO
  user identity by design — RPC callers act as the daemon owner. Do not add
  token auth to the socket path and do not expose the socket over a network.

Cluster user pooling syncs users over gossip events
(`USERS_SYNC` in `internal/backup/usersync.go`); foreign users' lifecycle
belongs to peer sync — local code must not delete them.

### Daemon CWD is NOT the user's project

The daemon process's working directory is wherever it was launched from (often
the meept repo itself). It is NEVER the user's project directory.

- **Never use `os.Getwd()` as a project/working-directory fallback** in daemon
  code (`internal/`, `pkg/`). The CLI client (`cmd/meept/`) and TUI
  (`internal/tui/`) may use `os.Getwd()` because they run in the user's shell.
- Tools receive their working directory per-session via
  `tools.ContextWithWorkingDir` or `SetWorkingDir`, not from the daemon's CWD.
- `generate_image` / `generate_video` write under `media.output_dir`
  (`~/.meept/media`) or a relative `output_path` resolved against the session
  working dir. Never `os.Getwd()`.
- Image and video models are `provider/id` entries in `models.json5` with
  capability `image` or `video`. Slots: `image_model`, `video_model`. Do not
  add a second provider catalog.
- Session creation binds to the user's active project via
  `ProjectManager.GetActive()`. Do NOT call `EnsureDefault()` for session
  binding — it creates a synthetic empty git repo.

### WS event type classification

`transformBusEventToWS` in `internal/comm/http/server.go` maps bus topics to
frontend event types. Only `chat_message` and `chat.response` topics produce
`type: "chat_message"`. All other `chat.*` lifecycle topics (heartbeats,
processing, worker events) produce `type: "agent_progress"`. The Flutter client
creates a visible message bubble for every `chat_message` event — misclassified
lifecycle events appear as blank messages.

Quota events on `agent.quota_wait` MUST be classified as `agent_progress`, never
`chat_message`. This is enforced by the topic prefix match:

```go
case strings.HasPrefix(topic, "agent.quota"):
    eventType = "agent_progress"
```

Model-escalation events on `agent.model_escalated` (verification fix-loop
exhaustion, tree 01 leaf 04) are likewise classified `agent_progress`, never
`chat_message`:

```go
case strings.HasPrefix(topic, "agent.model_escalated"):
    eventType = "agent_progress"
```

Future agents adding new bus topics must verify they land in the correct bucket.

### Quota errors are not failures (quota-reset-resilience)

The quota subsystem (`docs/workflows/quota-resilience.md`, plan in
`docs/plans/quota-reset-resilience/`) has invariants that cross package
boundaries:

- **Quota is not a health failure.** A `*llm.QuotaResetError` must NEVER
  reach `Resolver.RecordAliasFailure` — the agent loop's quota branch
  (`internal/agent/loop.go`) tracks the episode, marks `BlockQuotaEntry` +
  `BlockQuotaCredential`, and returns BEFORE the failure path. Quota blocks
  live in separate Resolver state (`entryBlocks`/`credentialBlock` on
  `AliasHealth`) and lazily clear only after expiry + a successful call.
- **Never short-retry a quota error.** `QuotaResetError` implements
  `NonRetryable`; every client retry loop (openai non-streaming/streaming,
  openai streaming-delta, anthropic non-streaming/streaming) has an
  explicit `errors.As` quota early-exit BEFORE the
  `RateLimitError`/retryable-status checks. A new retry loop must
  preserve this — a 429 quota window is hours, and the default
  3-attempt loop would burn it.
- **All-blocked is a distinct error.** When every alias candidate is
  quota-blocked, the Resolver returns `ErrAllModelsQuotaBlocked` — never a
  blocked model.
- **Endpoint-level cooldown identity (tree 02 leaf 04, D10).** Timeout
  cooldowns key on the base endpoint — `EndpointKey` = host + credential
  fingerprint — and their state lives ON THE RESOLVER
  (`Resolver.endpointBlocks`), never on `AliasHealth`: a timeout on
  `openai/model-1` (medium alias) must also skip `openai/model-2`
  (thinkhard alias), and per-alias state cannot deliver that cross-alias
  shared fate. A throttled/timed-out model's endpoint is blocked for the
  alias `timeout` base (30s default), cleared lazily after expiry + a
  success (same single lazy-clear pattern as quota blocks). Alias-level
  timeout blocks arm ONLY when the alias config declares `timeout:`
  explicitly, and only on consistent same-member consecutive failure
  (doubling capped at 4× base). When every candidate is endpoint- or
  alias-blocked, the Resolver returns the DISTINCT
  `ErrAllEndpointsBlocked` — check with `errors.Is`, never string
  matching. Precedence: quota blocks > endpoint blocks > alias blocks.
- **Deferral parks at the handler, mirroring budget.** `ChatHandler`
  parks quota-interrupted turns in `QuotaResumeWatcher`
  (`internal/agent/quota_resume.go`, the quota twin of
  `BudgetResumeWatcher`/`ParkedTurn`) and auto-resumes them at
  `min(unblockAt, now+MaxWait)`. Turn-level deferral is the wired
  mechanism; task-checkpoint-level deferral is a documented deviation.
- **State machine must stay reachable.** `agent.StateQuotaWait`
  ("quota_wait") is a legal transition target from all active states and
  Idle; the tracker drives it via `SetStateSetter` → `SafeTransition`.
  Adding an AgentState without a transition-table entry makes it
  unreachable (safe-by-default table rejects unknown states).
- **Surfaces consume the event, not the RPC.** `agents.list`/`agents.get`
  do not carry quota fields; TUI/GUI quota state arrives solely via
  `agent.quota_wait` bus events (WS type `agent_progress`). Restarting a
  client mid-episode shows base status until the next event.
- **Slot priority is a ChatOption, two tiers only (tree 04 leaf 03,
  D11).** Model-concurrency slots are gated by `slotGate`
  (`internal/llm/slot_gate.go`), not a raw channel: interactive chat
  turns pass `llm.WithPriority(true)` and jump background waiters
  (starvation-guarded: 3 interactive grants → 1 background). Priority
  is request-scoped ordering ONLY — never serialized into payloads, and
  never inferred from the queue job's `Interactive` flag (that flag is
  queue-layer, stamped at enqueue; the slot gate reads the calling
  turn). New client transports that honor `max_concurrency` must go
  through `acquireConcurrencyLimit` so they inherit the two-lane
  behavior.

### Bus proxy registrations must have a live responder

`internal/rpc/proxy.go makeProxy` publishes a request topic and waits on a
response topic. Before adding a proxy registration, verify BOTH sides exist
in source: a subscriber for the request topic AND a publisher that echoes
`ReplyTo` on the response topic (see `internal/memory/handler.go` for the
correct pattern). A proxy with no responder blocks the caller for the full
timeout (10-30s) and then fails — worse than method-not-found. Prefer a
direct `RegisterHandler` closure (the epistemic/memory_rpc/scheduler
pattern). The generator's `ANNOTATED_ORPHANS` table
(`scripts/gen-connectivity-graph.py`) suppresses topics with documented
external-only paths (e.g. `dispatcher.stats` via the `bus.publish` RPC);
add entries there instead of deleting intentional external surfaces.

### Wiki and traces are evolver-only

The skill knowledge stores (`internal/selfimprove` WikiStore + TraceStore,
rooted at `skills.wiki.dir`) are inputs to the skill EVOLVER only. They must
never be reachable from `ContextInjector`, `BuildSystemPrompt`, or any
inference-path prompt builder (WikiSkill §5.1: giving the worker wiki access
during evolution degrades final skill quality). Sampling constants
(5 fail / 3 pass traces, 15k chars) live in code, not config.

Every loop that serves user turns must be wired for trace persistence: the
primary loop (components.go, `agent.WithTraceWriter`) AND every
registry-created specialist loop (`AgentRegistry.SetTraceWriter`) — chat,
coder, etc. turns all reach the store via `agent.NewTraceStoreWriter` +
`traceStorePersist`. When adding a new loop construction path, wire these
three (trace writer, usage tracker, learning pipeline) or the evolver
blind spot grows.

### State mode is per-skill opt-in

`SKILL.state` execution (`internal/agent/skill_state.go`) activates only when
BOTH the skill frontmatter declares `state: true` AND `skills.state.enabled`
is true (default false). A skill declaring `state: true` with no runtime wired
falls back to the conversation path. Never force state mode on audit, debug,
or provenance tasks — for those, the history IS the deliverable (SKILL.state
§7). The state Σ uses null-deletion semantics: explicit `null` deletes a key,
a missing key leaves it unchanged.

### Skill evolver ordering: constructed after its dependencies

The evolver requires `SkillUsageTracker`, `SkillWriter`, and `PlanManager`.
It is constructed by `initializeSkillEvolver` (components_wiki.go), invoked
from daemon.go AFTER the plan system initializes — NOT inside
`initializeSkills`, which runs before those dependencies exist (the old
inline gate was always false; found by the wiki smoke test, 2026-08-29).
Keep this ordering if you refactor daemon startup.

### Wiki/state defaults

`skills.wiki` is enabled by default but inert until wired into the daemon
(writes happen only via the learning pipeline + evolver paths);
`skills.state.enabled` and `skills.evolver.enabled` default false. Flipping
these defaults is a product decision, not a code cleanup.

## Coding Practices

### Predictable ID Prevention

**Never use `time.Now().UnixNano()` or `math/rand` for ID generation, even in
fallback paths.**

When `crypto/rand` fails, use `pkg/id.Generate()` which has a documented
zero-suffix fallback. The zero suffix indicates catastrophic system failure
(entropy exhaustion) rather than providing pseudo-random IDs that appear secure
but are predictable.

**Custom analyzers:** Run `go run ./tools/analyzers/predid/... ./...` to detect
predictable ID patterns.

### Optimization Posture

**Prefer early optimization over defensible-but-suboptimal defaults.** Pick the
optimized approach when cost/benefit is reasonable.

- Share genuinely-sharable state (configs, registries, builders) across instances
- Prefer structural isolation over convention-based isolation
- Don't over-analyze micro-optimations (<100KB memory, <10ms latency)

### Wiring/Integration Requirement

**Implementations MUST include wiring — data structures without user-facing
interfaces are INCOMPLETE.**

Every feature must answer: **"How does a user actually use this?"**

**Complete feature checklist:**
- [ ] **Core logic** — data structures, interfaces, business logic
- [ ] **At least ONE interface** — CLI (`cmd/meept/`), TUI (`internal/tui/`),
      GUI (`ui/flutter_ui/`), or HTTP API (`internal/comm/http/`)
- [ ] **Agent wiring** — dispatcher routing, intent classification, tool
      exposure (if agents should use it)
- [ ] **Tests**

**Exception for prototypes:** Experimental features can ship with partial wiring
if the PR explicitly notes which interfaces are deferred and why.

**Red flags:**
- Files only in `internal/` with no changes to `cmd/`, `internal/tui/`, `ui/`,
  or `internal/comm/http/`

### Typed-nil interface guard

Nil `*ConcreteType` assigned to an interface produces a non-nil interface that
panics on method calls. Guard at call sites and in `With*` functions:

```go
if tokenCache != nil {
    opts = append(opts, WithTokenCache(tokenCache))
}
```

### Setter methods

Every `Set*` method MUST include a nil guard. Verified by
`internal/tools/builtin/setters_test.go`:

```go
func (t *SomeTool) SetFenceChecker(fc FenceChecker) {
    if fc != nil {
        t.fenceChecker = fc
    }
}
```

### Mutex scope

Never hold a mutex across I/O operations. Use "collect under lock, release,
then operate":

```go
mu.Lock()
cfg := m.config  // snapshot
mu.Unlock()
result, err := doNetworkCall(ctx, cfg)  // I/O outside lock
```

When the collect-then-operate pattern spans an IIFE or closure boundary, the
`mutexio` static analyzer cannot see the scope separation and will flag it as
a false positive. Suppress with a `//nolint:mutexio` directive that explains
why the call is outside the lock scope:

```go
var stale *Resource
func() {
    mu.Lock()
    defer mu.Unlock()
    stale = m.resource  // collect under lock
    delete(m.resources, id)
}()

// Lock released by IIFE above; safe to do I/O here.
if stale != nil {
    stale.Close() //nolint:mutexio // collected outside IIFE lock scope
}
```

### Error handling

Pre-commit hooks block commits that introduce new `_ = someFunc()` ignored-error
sites or bare `panic(err)`. Always handle errors:

```go
if err != nil {
    return fmt.Errorf("context: %w", err)
}
```

Type assertions on `map[string]any` values (common in bus payloads) must use
the two-value form:

```go
if convID, ok := payload["conversation_id"].(string); ok {
    // use convID
}
```

### Surface Unacted Observations

**At the end of every turn, surface to the user any observations you made but
did not act on.** Silent discoveries are lost discoveries.

While working on one task, the agent often notices adjacent problems — a
potential bug in a sibling code path, a misconfiguration, a stale comment, an
inconsistent pattern, a suspicious value. If the agent decides not to address
these within the current task scope (out of focus, lacking certainty, or out of
respect for the task boundary), it MUST still tell the user about them before
completing the turn.

**Format:** Append a brief **Observations** section at the end of the response.
List each observation with:

- **Location** — `file:line`
- **What** — a one-line description
- **Why not acted on** — the reason it was left out of this turn

Example:

```
Observations:
- internal/agent/loop.go:412 — error from planner is swallowed (no log). Not
  acted on: out of scope for this PR; needs its own investigation.
- config/models.json5:55 — capability "code" has no provider mapped. Not acted
  on: unclear if intentional; flagging for the user to confirm.
```

Do not bury observations inside a wall of prose. If there are no observations,
omit the section entirely (do not write "No observations.").

### Prefer Clean Architectural Fixes

**Always prefer clean architectural fixes over hacky workarounds, even when the
clean fix requires more work.** Hacky workarounds accumulate technical debt and
create fragile systems that are hard to reason about.

Examples of hacky patterns to avoid:
- **Content comparison for deduplication** — comparing serialized content
  instead of using proper identity keys (`id`, `session_id`, hash of source).
- **Suppressing symptoms** — catching/swallowing an error to make a test pass
  without understanding why the error occurs.
- **Patching around a root cause** — adding a special case downstream instead
  of fixing the upstream producer of bad data.

**When fixing a bug:**

1. **Trace the root cause** through the full data flow — from the observed
   symptom back to its origin. Do not stop at the first place you *could* patch.
2. **Fix it at the source** — change the code that produces the incorrect
   behavior, not the code that merely reacts to it.
3. **If a workaround is temporarily necessary**, add a `TODO` comment with the
   ticket/reference and a concrete plan for the proper fix:

   ```go
   // TODO(subagent-1234): Temporary dedup by content string. Replace with
   // proper id-based dedup once Dispatcher emits stable task IDs.
   if seen[task.Payload] { continue }
   ```

**Never ship a workaround as the final solution without explicit user
approval.** If the clean fix is too large for the current change, say so, get
approval for the temporary measure, and record the follow-up work as an issue
or TODO.

## UI Conventions

- **All UI text must be lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- For TUI, use bubblezone for positioning
- Default to clickable elements for context switching
- **TUI and Flutter GUI features must be kept at parity.** When a feature is
  added or changed in one surface, the other surface gets the same capability.
  This includes: status bar elements, command palette items, keyboard shortcuts
  (prefer identical keys across surfaces — e.g., `Ctrl+V` for verbosity on all
  platforms, not `Cmd+V` on mac), session/agent/tab semantics (e.g., archive vs
  delete), and tab affordances. Document surface-specific deviations explicitly
  with a justification.

## Flutter Multi-Platform (Web + Desktop)

When modifying Flutter UI code, ensure web compatibility alongside desktop
(macOS/Linux/Windows):

- **Avoid top-level `dart:io` imports in shared code** — use `kIsWeb` guards
  or conditional imports
- **Platform detection:** use `bool.fromEnvironment('dart.library.io')` for
  compile-time checks, `Platform.isMacOS` only in `!kIsWeb` guarded code
- **File I/O:** wrap in `if (kIsWeb) return;` guards; web uses file pickers,
  not direct paths
- **Platform abstraction:** for shared platform abstractions, use a singleton
  service pattern (e.g., `PlatformService`) that provides safe null/default
  returns on web

**See also:** `ui/flutter_ui/lib/core/platform/platform_service.dart` and
`platform_native_helpers.dart` for the platform abstraction layer pattern.

## Configuration

All config uses **JSON5** format. Templates in `config/`, copied on
`make install`.

- **Main**: `~/.meept/meept.json5`
- **Models**: `config/models.json5` (capability-based resolution)
- **MCP servers**: `~/.meept/mcp_servers.json5` (20 preconfigured, 6 enabled
  by default)
- **ACP agents**: `~/.meept/acp_agents.json5` (catalog of external ACP
  agents; `[acp] enabled` defaults false — no subprocesses until opted in)
- **UI theme** (TUI + GUI): shared tokens in `theme/tokens.json5`; select per
  client via `rendering.ui_theme` — see `docs/configuration/theming.md`.
- **Client**: `~/.meept/client.json5` (TUI keybindings)
- **Log level**: `log_level` field in `~/.meept/meept.json5` (NOT env vars)

See `docs/configuration/` for full reference.

## Static Analyzers

Custom analyzers in `tools/analyzers/`:

| Analyzer | Purpose | Run |
|----------|---------|-----|
| `mutexio` | Detects I/O under mutex | `make mutexio` |
| `predid` | Detects predictable ID generation | `make predid` |
| `fieldguard` | Guards immutable struct fields | `go run ./tools/analyzers/fieldguard/...` |
| `selflock` | Detects self-deadlocking locks | `go run ./tools/analyzers/selflock/...` |

Audit scripts in `scripts/`:

| Script | Purpose |
|--------|---------|
| `audit-dart-enum-name-shadow.py` | Flags Dart extensions shadowing Enum.name/index |
| `audit-utf8-byte-arithmetic.py` | Flags hand-rolled ASCII case-conversion corrupting UTF-8 |
| `gen-connectivity-graph.py` | Generates bus/RPC/HTTP/WS topology (`make graphs`) |
| `research-harness-lit.py` | Verifies harness-technique catalog evidence and markdown freshness (`make research-harness-check`) |

All analyzers and audit scripts run via `make lint-ci`.

## Feature Documentation Requirements

All code changes to feature implementations must have corresponding
documentation updates.

**Documentation locations:**
- `docs/workflows/` — Feature specifications
- `docs/concepts/` — Architecture
- `docs/reference/` — CLI, API, tools
- `docs/research/` — living research maps (harness techniques)
- `docs/generated/` — Auto-generated connectivity graphs (do not edit)

**Feature mapping:** `internal/<pkg>/` → `docs/workflows/<pkg>.md`

## AGENTS.md Maintenance Rule

**Every commit MUST review this file for completeness and correctness.**

Before committing, verify:
1. **Package table** — any added/removed/renamed packages in `internal/`,
   `cmd/`, or `pkg/` are reflected in the Key Components table.
2. **Build commands** — new Makefile targets or changed build steps are listed.
3. **Invariants** — new cross-boundary contracts (bus topics, session ID
   semantics, WS event types) are documented under Critical Invariants.
4. **Analyzers/scripts** — new static analyzers or audit scripts are listed.
5. **Conventions** — new coding conventions discovered during the change are
   captured.

If any item is stale, fix it in the same commit. Do not defer AGENTS.md
updates to a follow-up.
