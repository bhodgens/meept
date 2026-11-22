# Agent Invariants: Global Red Lines (Full Text)

Moved from the root `AGENTS.md` to keep the root under the size cap. The root
keeps a one-line red line and a pointer for each item. Update both in the same
commit. Package-specific contracts still live next to the code.

## Critical Invariants

### Chat replies must be honest and user-shaped

The chat path is the user's only window into the daemon. Full contracts (async turns,
errored steps, per-session working dirs, no global active-project fallback,
machine-shaped-output guard + one bounded loop-seam rewrite retry, completion
events deduplicated by queue-job state) live in `internal/agent/AGENTS.md`. Short form: turns are
async, errored steps never pass review, and projects bind PER SESSION with no global
fallback. `session_id` and `conversation_id` are distinct; new code handles both.

### Projects, working dirs, and trust boundaries

Full rules live in `docs/agents/projects-and-workdirs.md`. Short form: never use
`os.Getwd()` as a fallback in `internal/`/`pkg/` daemon code; projects bind PER
SESSION with no global active-project fallback; `multiuser.enabled=false` preserves
the legacy flat-key HTTP path byte-identically; Unix RPC stays owner-trusted (no
token auth on the socket).

### First-run pairing and the dev key

The per-installation dev key ($MEEPT_HOME/dev_key) is generated daemon-side at
first boot; it must NEVER be baked into a distributed GUI binary. `make
build-gui-dist` builds with no embedded key; a key-less GUI pairs on first run
through the daemon's loopback-only handshake (`internal/comm/http/pairing.go`):
a single-use `crypto/rand` code printed to the daemon console, exchanged once
at `POST /api/v1/pair/exchange`. Pairing arms ONLY when require_auth is on with
no explicit api_keys and no auth store. Never log the dev key or pairing code
at info level; the build-time dart-define path (`make build-gui`/`devbuild`)
remains dev-workflow-only.

### WS event classification and bus topics

Full rules live in `internal/comm/AGENTS.md`. Short form: only `chat_message` topics
produce `type: "chat_message"` (everything else is `agent_progress`); `chat.response`
is never WS-relayed; typed `bus.Topic[T]` for stable payloads, raw `Publish` for
multi-shape; the kanban board surface (`kanban.card.moved` / `kanban.board.changed`
/ the five `kanban.event.*.state` topics) classifies as the `WSBoard` wire type
`board_event`, relayed like progress and never `chat_message` (kanban-board C8).
Execution results use two typed `Topic[agent.ExecutionResultEvent]` topics —
`bot.result` (bots) and `employee.result` (employees), issue #140 — with ONE
shared payload that has NO `kind` field: the topic name is the sole tier
discriminator. Both marker-classify as `agent_progress`, never `chat_message`.
Bots are `send_agent_message` targets and drain inbox messages at turn start.
A
bus proxy registration needs a live responder on both sides. The
per-connection session filter is `(channel, session)`-scoped — a chat
unsubscribe must not silence progress/turn-terminal for the same session, an
unsubscribe records a suppression rather than deleting the grant, and a
session-less event broadcasts to every connection.

### Quota errors are not failures (quota-reset-resilience)

Full invariants live in `internal/llm/AGENTS.md` (quota blocks, endpoint cooldowns,
refusals, alias resolution, universal parking, slot gate). Short form: a
`*llm.QuotaResetError` is never an alias failure and never short-retried; check
`ErrAllModelsQuotaBlocked` / `ErrAllEndpointsBlocked` with `errors.Is`. An
empty/whitespace completion from ANY provider IS an alias failure — every
client classifies it (openai, anthropic, codex): the client retries it
immediately within the short budget, then the bare `ErrEmptyResponse`
sentinel rotates to the fallback (never surfaced as a completed turn).

### Opt-in defaults and evolver wiring

Full rules live in `docs/agents/opt-in-defaults.md`. Short form: `skills.state`,
`plans.parallel_phases`, and `plans.plan_compiler_enabled` are opt-in with default
false; flipping a default is a product decision. Kanban is the counter-case:
`[kanban].enabled` and `[kanban].sidecar_enabled` are now DEFAULT-ON opt-out
knobs (board projection + loopback sidecar; dev key seeded into the sidecar token
store with three grants) — set either to `false` for the byte-identical pre-kanban
boot. Wiki/trace stores are evolver-only,
never reachable from inference-path prompt builders; wire trace writer + usage
tracker + learning pipeline on every new loop; construct the evolver after its
dependencies in daemon startup.

### Local runtime lifecycle: spawn guards and orphan reaping

Full rules live in `internal/daemon/AGENTS.md`. Short form: runtimes are long-lived
daemon children — never spawn into a served endpoint, never kill another instance's
runtime, only `StartupOrphanSweep` reaps orphans, sweep before spawn, a local
classifier that never becomes healthy fails startup (F-D8), and client transports
(RPC socket, HTTP/WS listener) come up BEFORE the F-D8 gate so clients can connect
and enqueue during slow model loads (boot-window submits park on
`ClassifierBootReadiness`). The SIGINT/SIGTERM/SIGHUP handler is installed before
those listeners: a reload during boot is queued, not fatal. Hang detection
(inference canary + checker self-watchdog, `VerdictHealthHung`, `runtime.health` bus
events) adds one rule: HUNG drives restart ONLY through the existing bool
callback/auto-restart path and the bus seam is `SetHealthEventPublisher` — full
invariants in `internal/llm/AGENTS.md` (`docs/workflows/runtime-hang-watchdog.md`).

### Session affinity headers and upstream teardown

Full rules live in `internal/llm/AGENTS.md` (header plumbing, sticky rotation,
teardown adapters; `docs/workflows/session-affinity.md` is the user-facing doc).
Short form: affinity headers are DEFAULT-ON (`llm.session_affinity`, header
`session_id`) on the OpenAI-compatible, Anthropic, and Codex clients — an empty
scope sends NO header, never an empty value; sticky endpoint preference is a
request-scoped READ whose precedence is quota blocks > endpoint cooldowns >
alias blocks > stickiness; teardown (KV-cache erase on session archive/delete/
idle/shutdown) is best-effort with the verified route `POST /slots/0?action=erase`
and NEVER fails a turn, an archive, or a shutdown; cloud providers are an
explicit no-op.

### LLM routing: red lines

Full contracts: `internal/llm/AGENTS.md`, `docs/workflows/llm-routing-quality.md`.
The pre-dispatch chain is THREE filters in fixed order: posture -> quality ->
admission. First non-Allow verdict binds. Frozen precedence stays:
quota > endpoint > alias > sticky. Posture is a hard compliance boundary: a
sensitive prompt never reaches a non-cleared model; all-non-cleared refuses the
turn, never a leak. Admission (`*ContextTooSmallError`, NonRetryable) fires
pre-dispatch on ProviderManager rotation loops and bare-client paths; a size
verdict is never a `RecordAliasFailure` and never parked. Tier floors consume
DEPTH only and never reorder the chain. The semantic cache sits behind the
exact-key miss and before the budget gate; it never stores tool-call responses.
Every routing dimension is opt-in: absent config is byte-identical legacy
behavior, pinned by the flag-off golden in `e2e/suites/sovereign-parity/`.
Shadow routing (`ResolveShadow`) is read-only: no ledger, budget, health, or pin
mutation. `ErrToolCallInvalid` keeps the empty/refusal ordering and the
bare-sentinel pointer rule. Advisory budget never returns `BudgetExceededError`.

### Voice sidecar (pointer)

Full contract: `docs/agents/voice-invariants.md` (load only for voice work) and
`internal/voice/AGENTS.md`. Red lines: C1 frames are byte-exact JSON lines with
`call_id` on every frame; only final prompts start a turn; barge-in `clear`
affects one call; hangups fan out release exactly once; one live call per
employee and target number; local callers are always approved.

### GitHub tool registration: import direction

`internal/tools/builtin` imports `internal/githubsvc`; `githubsvc` NEVER imports
`builtin` back (import cycle). github_* tools go in BOTH the provider Discover
list and `githubsvc.ToolFactories`. Token material is NEVER logged; a token file
with group/other access bits is refused. Full contract: `docs/workflows/github.md`.


## LLM routing detail preserved from the root file

`ClassifyFailure` takes 5 arguments (poolID third). `(scope,target)` block keys
coexist with legacy `host|credential` keys; model-scope arming is deduped into
the alias cooldown; a request fault (400/413/422) arms nothing. `ResolveShadow`
is RLock-only with zero side effects. `ErrToolCallInvalid` is a bare
pointer-identity sentinel: the transport enforces only the ARGUMENTS-SHAPE
half, while name membership stays with the executor's audited gates
(group_deny / unknown-tool / exposure recovery). Advisory-budget `steer`
never returns `BudgetExceededError`; one `budget_advisory` event rides
`agent.quota_wait` and only the failover TAIL reorders by cost.
