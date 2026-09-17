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
make deps-llama-check   # Enforce the llama.cpp build floor (>= b9660, LFM2.5 tool-call
                        # parser); wired into `make deps` and `make install`. `make
                        # deps-llama` installs it into $MEEPT_HOME/deps/llama.cpp

# Test
#
# macOS: full-sweep runs MUST bound package parallelism (-p 2). Unbounded
# `go test ./...` / `./internal/...` opens enough concurrent localhost
# sockets to exhaust the ephemeral port range (49152-65535) mid-run, and
# unrelated packages then fail with
#   dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address
# `make test` and friends already pass -p 2 (override: make test
# TEST_PACKAGE_PARALLELISM=N).
go test -p 2 ./... -v
go test -p 2 -race ./...
go test -p 2 ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
make test            # canonical full suite (short mode, -p 2)

# Run
./bin/meept-daemon -f           # Daemon foreground
./bin/meept chat "message"      # CLI chat
./bin/meept chat                # Interactive TUI
agent-tui ./bin/meept chat      # TUI testing

# Config
./bin/meept config              # Interactive editor
./bin/meept config get <key>    # Get value
./bin/meept config set <key> <v> # Set value

# Soul (user-authored persona, ~/.meept/SOUL.md)
./bin/meept soul show           # Current content + sha256 + size
./bin/meept soul path           # Resolved path (honors MEEPT_HOME)
make config-bootstrap           # Copy missing config templates into $MEEPT_HOME
make dev-key                    # Provision $MEEPT_HOME/dev_key (0600), shared with the GUI
make gui-connect-setup          # Make an installed home GUI-ready (transport.http + dev key)
make gui-connect-check          # Static self-check of the installed GUI connect path

# Agents (AI Employees) — not the dispatcher roster in config/agents/
./bin/meept agents list                 # List employees
./bin/meept agents show <id>            # Full definition
./bin/meept agents create <def.json5>   # Register new employee
./bin/meept agents pause <id> / resume <id>
./bin/meept agents goals [--employee=<id>]
./bin/meept agents set-gate <goal-id> --command="go test -p 2 ./..."
./bin/meept agents audit <id> [--since=6h]

# Plans
./bin/meept plans list/show/approve/reject/confirm <id>

# Projects
./bin/meept projects list/add/remove/sync/status <name>

# Connectivity graphs
make graphs               # Regenerate bus/RPC/HTTP/WS topology (writes docs/generated/*)
# Both freshness checks below run in CI (code-quality.yml, job
# "generated-artifacts"). Run `make graphs` after changing bus/RPC/HTTP/WS
# surfaces; the generator still embeds absolute line offsets (audit F66), so a
# red graphs-check after an unrelated edit means "regenerate", not "topology
# changed".
make graphs-check         # Verify generated files are fresh (CI gate)
make research-harness      # verify harness catalog evidence + regenerate techniques md
make research-harness-check # CI: fail if evidence missing or techniques md stale

# Reference docs (magefiles/docs.go)
make docs-generate        # Regenerate docs/reference/generated/* (needs mage + gomarkdoc)
make docs-check           # Verify those pages are fresh (CI gate; needs mage + gomarkdoc)

# Classifier-eval guards (tools/classifier-eval)
make classifier-eval-selftest # CI: corpus/replay guard + coverage-floor self-test

# Git hooks (bash >= 3.2; sub-hooks run under whatever `bash` is on PATH, so the
# suite also executes on the Linux CI runner)
make hooks                # core.hooksPath -> .githooks (17 pre-commit checks)
# Package-classification rule for those hooks: a staged package's state comes
# from `go list -e` STDOUT only. {{.Error}} prints there, while stderr carries
# progress noise ("go: downloading ..." on a cold GOMODCACHE) that must never be
# read as a load failure — folding stderr in (2>&1) blocked healthy commits on
# any machine with an empty module cache. The classifier detects LOAD failures
# and nothing else: `broken` is `go list` exiting non-zero with an empty stdout
# (stderr is captured separately for the reason) or a non-empty {{.Error}} that
# matches none of the exclusion reasons. A SYNTAX ERROR or an UNRESOLVED IMPORT
# is NOT `broken`: both give rc=0 with an EMPTY .Error, so the package classifies
# `ok` and surfaces as a go vet / staticcheck finding instead (go build, for
# pre-commit-build). A directory with no (non-test) Go files reports `nofiles` —
# a SKIP, not a failure; that is a deliberate loosening (the pre-68b2338c gate
# failed there).
# pre-commit-build's root-artifact check blocks a new root-level entry only when
# a build can create it: the gitignored binaries (meept, meept-daemon, meept-lite,
# llmdoc — including a stale one a build clobbers), `*.test`, or the basename of
# any `package main` directory in the module (a bare `go build ./cmd/gendoc`
# writes /gendoc), derived from `go list` rather than a fixed list. Any other new
# root-level path is reported, never blocking.

# Static analyzers
make analyzers            # mutexio + predid
make lint-ci              # golangci-lint + analyzers + audit scripts + fmt-check-gui
                          # (fmt-check-gui hard-requires the Dart SDK / Flutter)
```

See `cmd/meept/`, `cmd/meept-daemon/`, and `Makefile` for full command reference.

## Architecture Overview

Meept is a **Go platform** with skill-based task orchestration, LLM integration,
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
| **Audit Chain** | `internal/auditlog` (canonical JSON, hash chain, store, anchoring, verification) |
| **Session** | `internal/session` (store, store_sqlite, threads, messages) |
| **Effects** | `internal/effects` (external-effect idempotency ledger) |
| **Services** | `internal/services` (chat, session, terminal, push, reflection) |
| **Project** | `internal/project` (manager, init_deep, detection) |
| **TUI** | `internal/tui` (app, commands, components, handlers, modals, models, tableutil) |
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

### Chat replies must be honest and user-shaped

The chat path (sync dispatch and task-completion events) is the user's
only window into the daemon. These contracts were added after the
2026-09-04 naive-user comparison (docs/plans/chat-dispatch-ux/) and
are guarded by `scripts/e2e-naive-user-chat.sh`:

- **Sync replies carry the real step result.** `waitForTaskCompletion`
  (internal/agent/handler.go) returns the terminal step's `Result` —
  never the `Task <id> completed.` stub except when every step result
  is empty or the store errors.
- **Errored steps never pass review.** `ReviewStep` gates on
  `stepHasError` before every policy path; a task with any failed step
  finalizes `StateFailed` and its `task.completed` payload carries
  `"status": "failed"` plus the error text as `result`.
- **Step jobs run in the session's directory.** `resolveStepWorkingDir`
  resolves WorktreePath > ProjectPath > session `DetectionContext.CWD`
  > "". Never fall back to the daemon's CWD (see also the os.Getwd
  rule above).
- **Chat turns always resolve a working directory.** `session.ResolveWorkingDir`
  (internal/session/working_dir.go) is the single precedence for the
  session-bound sources — `WorktreePath > ProjectPath > DetectionContext.CWD`
  — and the chat path binds it at turn start in `ChatHandler.sessionLoop`,
  then falls to the configured default (`daemon.default_working_dir` via
  `SetDefaultWorkingDir`, unset today) and finally to the actionable
  `tools.ErrNoWorkingDir`.
- **Projects are scoped PER SESSION; there is NO global active-project
  fallback.** A session resolves its OWN binding only. `ProjectManager.GetActive`
  is never consulted at turn start or at dispatch — the
  `SetActiveProjectPathResolver` seam and the `WorkingDirFromActiveProject`
  source were REMOVED, not merely left unset. Every session carries its own
  project: session creation (both the services path and the RPC
  `session.create` path) binds the explicit `project_id`, else the client
  CWD (resolved into a project by `CreateOrResolve`), else leaves the
  session UNBOUND; an unbound session is logged at Warn and its filesystem
  tools return `tools.ErrNoWorkingDir`
  ("no working directory for this session; pass an explicit path") — a
  detectable sentinel (`tools.IsNoWorkingDir`), never the bare
  `no path specified` that made the model retry until the cycle guard
  aborted the turn (fresh-rig daemon11, 2026-09-13). Never synthesize a
  project (`EnsureDefault` is not for session binding) and never use the
  daemon's own CWD. The client detection context is PERSISTED
  (`detection_context` column; `Store.SetDetectionContext`) so a session
  created with `meept session create --cwd DIR` still resolves DIR at turn
  time after a daemon restart, from the store alone.
- **Machine-shaped output never becomes a reply.** `RunOnceWithParts`
  applies `applyReplyGuard` — raw `platform_*` tool dumps, agent
  rosters, and status JSON are replaced with user-language fallbacks.
- **Quota failures surface to the user.** Terminal
  `*llm.QuotaResetError` in a step job publishes the existing
  `agent.quota_wait` event and appends a user-language quota sentence
  to the stored step Result. Quota is still never an alias failure and
  never re-queued through the tactical retry gate.

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
- The `json_extract` tool runs a dedicated extraction model via the
  `extract_model` slot (`provider/id` ref, typically a small local llama.cpp
  endpoint such as `local-extract/lfm2-extract`). Empty slot = tool reports
  not-configured; never fall back to the chat model.
- Session creation binds PER SESSION: the explicit `project_id`, else the
  client CWD resolved into a project by `CreateOrResolve`, else the session
  stays UNBOUND. Never call `ProjectManager.GetActive()` for session binding
  (no global active-project fallback) and never `EnsureDefault()` — it
  creates a synthetic empty git repo.

### WS event type classification

`transformBusEventToWS` in `internal/comm/http/server.go` maps bus topics to
frontend event types. Only the `chat_message` and `chat.message.received`
topics produce `type: "chat_message"`. The `chat.response` topic is
intentionally EXCLUDED from WS relay: it is an RPC reply consumed by
ChatService for the HTTP response body, and relaying it would double-deliver
the reply to HTTP+WS clients (Flutter GUI) — do not add it to the
`chat_message` bucket. All other `chat.*` lifecycle topics (heartbeats,
processing, worker events) produce `type: "agent_progress"`. The Flutter
client creates a visible message bubble for every `chat_message` event —
misclassified lifecycle events appear as blank messages.

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

Turn lifecycle events on `turn.terminal` (async-turn-migration step 1, plan
`docs/plans/20260916-turn-lifecycle-events/`) are likewise classified
`agent_progress`, never `chat_message`:

```go
case strings.HasPrefix(topic, "turn."):
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
- **Alias selection is request-scoped; the ledger records the server.** The
  Resolver is the only component that picks a model, and its decision travels
  WITH the request via `llm.WithResolvedModel` (never by mutating shared
  `ProviderManager`/`Client` state — the manager is shared by every session).
  `ProviderManager` reorders the named provider first for that call only,
  keeping the rest as failover tail; the serving client — OpenAI-compatible
  `Client`, `AnthropicClient`, and `CodexClient` alike — uses the resolved
  model id on the wire and in the `metrics.db` `llm_calls`
  `provider`/`model_id`, so the ledger names the provider/model that actually
  served the call, not the caller's configured default. Turns that resolve no
  alias keep health/cost/priority ordering byte-identical.
- **Model-selection precedence: user directive > alias resolution > default
  ordering.** A user's reassignment directive (dispatcher / PrepareNextTurn
  hook) travels its OWN request-scoped channel, `llm.WithModelOverride`, and
  `llm.requestModelOverride` ranks the channels explicitly — the user
  directive beats the alias resolution regardless of option-append order
  (chatWithFailoverRaw appends the alias option AFTER caller opts; append
  order must never decide precedence). With a `ProviderManager` chatter the
  directive works per-request too: reasoningCycle stages the resolved config
  (`pendingModelOverrideConfig`) and chatWithFailoverRaw stamps it onto the
  call, and the one-shot override is cleared after that turn. Every client
  type that observes a selection names the actually-serving provider/model in
  the ledger.
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
- **No turn hangs on a provider wait — universal parking (tree 03,
  DECISIONS.md D9).** Every turn type (chat, goal-loop episode,
  specialist agent, queue job) PARKS on a classified provider wait
  instead of blocking or failing: the turn's re-entry data goes to the
  ONE shared `agent.TurnParker`, the agent/model slot is released, and
  the parker resumes the turn when the schedule allows. Throttle waits
  REUSE the `quota_wait` state (`agent.StateQuotaWait`) — no
  `StateThrottleWait` exists — with the reason payload
  ("throttle_wait" / "throttle_resumed" / "throttle_give_up") and the
  wait label ("quota_wait · throttle retry HH:MM") carrying the class.
  A wait beyond MaxWait never parks: throttle surfaces
  `ThrottleGiveUpError` (D8) and quota escalates to `blocked` at 24h.
  Park/resume/give-up events ride the existing `agent.quota_wait` topic
  (`agent.ParkTurnEvent` payloads with a `class` key) — never a new
  topic prefix — so the WS `agent.quota` classification above keeps
  every park event on `agent_progress`.
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

### Phase dispatch mode is per-config opt-in

`plans.parallel_phases` (default false) preserves strict serial plan phases;
when true, phase starts are frontier-driven (artifact + dependency gating;
list order is a tiebreak only), conversationIDs stay phase-scoped
(`phase-<phaseID>-<stepID>`), per-phase worktrees are provisioned via the
orchestrator hook (leaf 03) and win in step working-dir resolution
(`internal/daemon` `resolveStepWorkingDirFor`: phase worktree > session
WorktreePath > ProjectPath > session CWD), and BudgetHierarchy phase
selection is per-phase (multi-select). Subscribers to the phase-transition
hook must tolerate `fromPhase == ""` (frontier activations have no completed
predecessor). Flipping the default is a product decision, not a code cleanup.
See docs/workflows/agent-orchestration.md (phase frontier section).

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

### Plan compiler pipeline is opt-in

`plans.plan_compiler_enabled` (default false) gates the brainstorm
draft→seal→compile planning pipeline. When false, the legacy JSON
`spec_plan` path is byte-identical — no code may assume the draft store
(`task.Metadata["plan_draft"]`) exists. When true, the draft IS the
interview: the one-shot `task.interview` path stays only for the legacy
pipeline. `plan.seal` and `plan.draft` are RPC-only, never a bus topic.
See docs/workflows/agent-orchestration.md ("Plan compiler pipeline").

### Local runtime lifecycle: spawn guards and orphan reaping

Local LLM runtimes (`internal/llm`) are long-lived children of the daemon. Each
rule below was added after a real leak; changing any of them requires a
replacement mechanism, not just a deletion:

- **No spawn into a served endpoint.** `RuntimeProcess.Start` probes the
  endpoint address when `spawn_command` declares that port and refuses when
  something already listens there. Keep the refusal: `mlx_lm server` does not
  exit after a failed bind — it stays alive with the model loaded and no socket
  while the foreign listener answers its `/health`, so the spawn "succeeds" as a
  healthy-looking runtime that serves nothing. The check applies only to a spawn
  command that declares the port, so wrappers and test harnesses are unaffected.
- **Health requires a live process, and the run outlives its caller.**
  `HealthChecker.SetProcessAliveProbe` binds each endpoint's checker to its own
  `RuntimeProcess`; a dead child is unhealthy whatever the endpoint returns. The
  check run detaches from the caller's context and re-arms whenever no run is
  active: every caller passes a short-lived context (the daemon cancels its boot
  context when `StartAll` returns), so binding the run to it silently ended
  health monitoring seconds after boot.
- **Ownership forbids killing; the sweep permits it.**
  `RuntimeProcess.Stop` never stops a runtime whose PID file carries another
  instance's token (adopted observed-not-owned — that guard is what keeps a CLI
  or eval process from killing the daemon's server) and returns
  `ErrRuntimeNotOwned`, so no surface reports a stop that did not happen.
  `StopAsOperator` is the explicit operator override used by
  `meept runtime stop`, and it verifies the pid's identity before signalling.
  The startup sweep (`RuntimeManager.SweepOrphanRuntimes`, driven by
  `Daemon.StartupOrphanSweep`) is the only path that reaps a leftover, and only
  when all hold: `ppid == 1` (spawner gone), the command line matches the
  endpoint's `spawn_command` or its durable spawn record, the endpoint is
  sweepable under `auto_stop_on_exit` (absent means true), and no live recorded
  owner vetoes it. Every pid is re-validated against the process table
  immediately before it is signalled. Do not widen the sweep to untagged or
  unrelated processes.
- **Sweep before spawn.** `StartupOrphanSweep` runs after components exist and
  before `ContainerManager.StartAll`. Moving it later makes the duplicate-spawn
  guard refuse the spawn of the endpoint the sweep has not freed yet.

**Termination is two-sided; only one side is automatic.** Graceful shutdown
stops owned runtimes: `Daemon.Stop` calls `ContainerManager.StopAll(ctx)`
(`internal/daemon/daemon.go:1904`), which stops every endpoint whose
`auto_stop_on_exit` is true. SIGINT/SIGTERM therefore leaves no children
behind - verified 2026-09-12, killing a scratch daemon took its `mlx_lm` and
`llama-server` children down with it.

A hard kill no longer leaks. Each runtime is spawned under a supervisor (the
daemon binary in a hidden mode, `meept-daemon --supervise-parent <pid> -- <argv>`)
that kills the runtime's process group - SIGTERM, then SIGKILL after 10s - once
its parent disappears, and exits with the runtime. macOS has no parent-death
signal, so the supervisor uses a parent-death pipe plus a 2s pid poll. The
runtime keeps its original argv and pid: the sweep's command-line match and the
pid-file ownership token still identify it, never the wrapper. Per-endpoint
escape hatch: `supervise: false`. Cleanup after a hard kill, in order:

1. `kill <daemon-pid>` first (graceful) and re-check; do not kill children of
   a live daemon, its restart policy respawns them.
2. `pgrep -fl "llama-server|mlx_lm"` and
   `lsof -nP -iTCP:<port> -sTCP:LISTEN` to find what survived.
3. Kill each leftover child explicitly and confirm the port is free.

The boot sweep (`StartupOrphanSweep`) remains the backstop: it reaps what
self-termination cannot - a runtime whose supervisor was itself SIGKILLed, and
anything left by an older build. Windows has no `ps`, so it gets no sweep
(documented gap).

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
- **TUI tables are sized on both axes.** `bubbles/table` owns a viewport that
  starts at width 0, and a width-0 viewport renders no lines: a table given only
  `SetHeight` draws its header over an empty body while cursor navigation still
  works. Size every table through `internal/tui/tableutil.Size` and write rows
  through `tableutil.SetRows`, which normalizes each row to the current column
  count (a row longer than the column list panics inside `bubbles/table`). A
  column change clears the rows first (`SetColumns` re-renders them) and
  repopulates from cache afterwards. See `docs/workflows/tui.md`.

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
- **MCP servers**: `~/.meept/mcp_servers.json5` (22 preconfigured, 7 enabled
  by default — incl. `obscura` browser MCP, enabled; `excel` xlsx fallback,
  disabled). Every stdio entry carries `install_hint`; `meept doctor`
  surfaces it and `meept doctor --fix --install-missing` runs hints
  opt-in (consent per command). Daemon/launchd/menubar launches augment
  PATH via `internal/daemon/daemonpath.go` `DaemonPath()` (menubar keeps
  a synced Swift copy) — keep new guaranteed dirs in both.
- **Skills**: `config/skills/` ships via `make install` / `make sync-config`
  into `~/.meept/skills/` (merge, no-clobber: user-modified files are kept,
  new defaults land as `<file>.new` — see `scripts/install-sync.py`).
  Frontmatter `requires-tools:` gates
  execution on tool availability (see docs/workflows/skills.md).
- **Home dir**: everything meept reads/writes lives under one home —
  `$MEEPT_HOME` when the env var is set, else `~/.meept`
  (`config.MeeptHome()` / `MeeptPath()` in internal/config/home.go are THE
  resolution points; config paths carrying the `~/.meept` prefix are
  redirected through it too). Set `MEEPT_HOME` consistently for daemon,
  CLI, and `make sync-config` — they all honor it.
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
| `ensure-dev-key.sh` | Provision the per-installation dev API key (`$MEEPT_HOME/dev_key`, 0600) |
| `gui-daemon-connect.py` | Reconcile the installed daemon config with the GUI connect path (`ensure-config` / `endpoint` / `check` / `probe`) |
| `verify-gui-connect.sh` | Prove a fresh install's GUI reaches the daemon (config, key, TLS pin, WS upgrade) |
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
