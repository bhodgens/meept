# internal/llm/AGENTS.md

Guidance for AI agents working in `internal/llm/`. Referenced from the root
AGENTS.md; full-quota-policy prose moved here from the root file. Update both
in the same commit per the root maintenance rule.

## Quota errors are not failures (quota-reset-resilience)

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
  3-attempt loop would burn it. (`CodexClient` classifies every
  transport failure through `codexErrorFromResponse` in one place and
  has no per-status retry loop, so it needs no such early-exit; its one
  retry wrapper re-dispatches ONLY the empty-completion lane.)
- **Empty/whitespace completions are provider flakes, not fatal errors.**
  EVERY client classifies them, not just the openai ones: a 200-OK body
  that produces no answer — blank or whitespace-only text, no tool call,
  no reasoning — is `ErrEmptyResponse`, not a success with empty
  content. Per-client sites:
  - openai non-streaming: `parseResponseWithTools` (trimmed; leading
    whitespace on real content passes byte-identical, so agnes-2.5-flash's
    `"\n\nok"` is never misread as empty);
  - openai streaming-delta: the stream terminator, same trim rule over
    content + reasoning;
  - anthropic: `classifyAnthropicEmpty`, shared by the non-streaming
    `parseResponse` and the streaming `buildResponseFromBlocks`. It
    judges the RAW answer text (pre `[Thinking]/[Response]` decoration),
    so a thinking-only reply classifies, and it exempts any reply
    carrying a `tool_use` block — the tool call IS the answer;
  - codex: `parseResponse` and `parseResponsesSSE`, judging
    output_text + reasoning together and exempting `function_call`.

  Every retry loop re-dispatches the sentinel IMMEDIATELY within the
  short budget — no plan sleep, a served-but-blank body is not a
  rate-limit window — and surfaces the BARE sentinel on exhaustion
  (never a wrapped `ClientError`), so it is an alias failure: the agent
  loop's generic branch records it and rotation lands on the local
  fallback instead of completing the turn with garbage. The loops:
  openai `Chat` / `ChatWithProgress` / `ChatWithDeltaCallback`,
  anthropic `Chat` / `ChatWithProgress`, and codex's
  `doRequestWithEmptyRetry` (its single wrapper covers both the
  non-streaming and the streaming-delta caller, and retries ONLY the
  empty lane — every other codex failure keeps the single-shot shape so
  a 429/quota window is not double-retried).

  Ordering is load-bearing: the refusal/quota/overflow early-exits keep
  precedence over the empty branch, at both the loop level and the
  parser level (anthropic's `stop_reason:"refusal"` is detected before
  `parseResponse` runs, so a refusal with no text is a `RefusalError`,
  never an empty retry). A new client or a new retry loop must preserve
  that ordering, and exhaustion must return the bare sentinel by
  POINTER identity — `errors.Is` also matches through a wrap, so it is
  not a sufficient pin.
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
- **Refusal is not a failure.** A `*llm.RefusalError` must never reach
  `Resolver.RecordAliasFailure`; the loop's refusal branch
  (`internal/agent/loop_refusal.go`) re-dispatches once to the agent's
  `refusal_model` (per-agent AGENT.md spec field, else the global
  `models.json5` slot; default off) and surfaces the refusal if the
  fallback also refuses. One hop only, never rotation.
  `ProviderManager` treats refusals the same way: no `recordFailure`, no
  rotation — it returns the refusal so the loop's one-hop policy owns it.
  A refusal is not free: the provider-reported usage rides on
  `RefusalError.Usage` and is ledgered before the refusal surfaces. The
  fallback pin (persistent override + staged config) clears at the start
  of the next fresh turn (`clearRefusalFreshTurnState`); the global
  refusal_model slot propagates to session clones via
  `ConfigSnapshot`/`WithGlobalRefusalModel` and to registry-built
  specialists via `RegistryConfig.GlobalRefusalModel`.
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

## Alias reachability audit and pool-exhaustion errors (2026-10-23 outage)

- **The audit runs at boot AND at doctor time — keep the twins in sync.**
  `AuditAliasReachability` (`alias_audit.go`) checks every `model_aliases`
  member against the `buildProviderConfigs` registration rules (provider
  exists, not in `disabled_providers`, `apiKey != "" || noAuth`). The
  daemon (`components.go`, after `registerOAuthProviders`) passes its
  ACTUAL pool membership as the `registered` map, which OVERRIDES the
  static predicate per provider — that is what makes dynamically
  OAuth-registered providers count as reachable and boot-skipped ones
  count as unreachable. `meept doctor`'s `alias-reachability` check
  (`cmd/meept/doctor.go`) passes `registered=nil` (offline: pure static
  predicate) and reports findings as `[warn]`, NEVER `[fail]`.
- **No silent drops.** A no-api-key skip in `buildProviderConfigs` is a
  WARN when the provider is a loopback-lifecycle local runtime (it will
  never serve failover while skipped); the disabled-provider skip stays
  Debug (explicit operator choice). A request-scoped selection naming a
  provider absent from the failover pool logs ONE Warn in
  `preferRequestedProvider` and the selection is ignored — never a
  silent dead reorder.
- **Pool-exhaustion error-string contract.** `poolExhaustedError`
  (provider_manager.go) returns the bare `"no providers available"` ONLY
  for the genuinely-empty-registration case; when providers ARE
  registered but EVERY credential is quota-blocked, the error says
  `"all N provider credentials quota-blocked (see boot alias audit)"`.
  Do not reintroduce the bare string on the all-blocked path — callers
  and logs distinguish the two by message.

## Session affinity headers, sticky rotation, and upstream teardown
(session-affinity tree; user-facing doc `docs/workflows/session-affinity.md`)

- **Headers are default-on and never sent empty.** Every OpenAI-compatible
  request path (`applyAffinityHeader` — BOTH the streaming and
  non-streaming builders, immediately after `applyExtraHeaders`) and every
  Anthropic request path (`applyAnthropicAffinityHeader`) sets
  `llm.session_affinity.header` / `.anthropic_header` when the request
  carries a non-empty session scope and no explicit `extra_headers` entry
  with the same key (extras win per key — Contract 3 precedence). An
  empty scope or an empty configured header omits the header entirely.
  The `${session_id}` sentinel substitution in `applyExtraHeaders` is
  independent and unchanged. Codex keeps its wire-mandated
  `session_id` header. Config reaches clients ONLY through
  `SetSessionAffinityConfig` (daemon wiring / ProviderManager
  propagation, mirroring `SetFailurePolicyConfig`) — clients never read
  config directly.
- **The header value is the turn's ACCOUNTING session id**
  (`chatOptions.sessionID`, stamped by `chatWithFailoverRaw` from
  `turnAccountingSessionID`; leaf 01 threads the same scope through the
  secondary call sites). Never source it from `currentSessionID`.
- **Sticky rotation precedence is frozen** (Contract 4): quota blocks >
  endpoint cooldowns > alias blocks > stickiness. The per-session
  endpoint map has ONE writer (`ProviderManager.reportSessionEndpoint`,
  on success, overwriting) and one reader seam
  (`Resolver.ResolveForAlias`'s resolve-time hook); the remembered
  endpoint moves to the front only when it is healthy under the SAME
  filters that gated the rotation winner. In-memory only (LRU 1024),
  never persisted.
- **Teardown is best-effort and never fails a turn, an archive, or a
  shutdown.** `TeardownCoordinator` fans archive/delete/idle events to
  adapters asynchronously; `ShutdownSweep` (daemon stop, before
  `ContainerManager.StopAll`) is synchronous under a bounded budget —
  the in-flight wait and the sweep each get the full
  `TeardownTimeout*min(8,n)` window, so the worst-case total is 2x that
  bound (deliberate: splitting would halve every healthy shutdown's
  erase window). The
  erase route is the VERIFIED `POST <base>/slots/0?action=erase`
  (pinned llama.cpp tools/server/server.cpp:286 +
  server-context.cpp:4796; the adapter strips the provider baseURL's
  `/v1` version prefix first — the slot route lives at the server root).
  `Teardown` (live events) reference-counts over the snapshot and never
  erases while other live sessions map to the endpoint;
  `SweepTeardown` (daemon stop) does NOT reference-count — every session
  ends together at shutdown, so per-session refcounting would skip every
  erase. `CloudTeardown` is the explicit no-op for non-llama endpoints.
- **Teardown identity and wiring (leaf-07 gap fixes, verified by the
  unskipped e2e proofs):**
  - `LlamaServerTeardown.Handles` accepts BOTH key shapes: the runtime
    `llama-cpp:<host>:<port>` prefix AND the resolver-snapshot
    `host|credential` shape (resolved through the manager — an
    unresolvable key claims nothing). `RuntimeManager.TeardownBaseURL`'s
    host fallback compares `endpointHost` (host:PORT, the EndpointKey
    shape) on both sides; the bare-hostname compare is the legacy
    fallback only.
  - The `RuntimeManager` MUST be constructed BEFORE the teardown
    adapters: the llama adapter captures the manager pointer at
    construction, and the later position left it nil for the daemon's
    lifetime (unit tests pass with an explicitly-constructed manager;
    only the wiring order exposes the nil).
  - `TeardownCoordinator.SetIdentityResolver` wires the
    session_id/conversation_id duality seam (root AGENTS.md): lifecycle
    events carry the PRIMARY id, the snapshot keys on the ACCOUNTING id;
    on a miss the coordinator retries under the alternate identity and
    dispatches with the SNAPSHOT id so reference counting sees the
    session's own row.
- **A plain 5xx alias failure arms an endpoint block** (GAP A fix):
  `VerdictForFailure` unwraps ANY `*APIError` in the wrap chain (the
  streaming client's short-retry exhaustion wraps the 5xx in a
  `ClientError`), and `RecordAliasFailure` arms the endpoint cooldown for
  BOTH the throttle and server-error classes. This is what disqualifies a
  dead remembered endpoint at the sticky hook — without it the resolve
  re-pulled the failed member to the front and the turn ping-ponged on it
  while the healthy peer served zero requests. Blocks expire on the alias
  timeout and clear lazily via expiry + success, like every other block
  map.

## Hang detection (canary + self-watchdog)

The llama-hang-watchdog layer (`health_checker.go` canary lane,
`runtime_manager.go` self-watchdog; user-facing doc:
`docs/workflows/runtime-hang-watchdog.md`) detects a runtime whose
`/health` answers 200 while inference is dead. Invariants — each was
pinned by the tree's gate tests (`health_checker_verdict_internal_test.go`,
`runtime_manager_watchdog_internal_test.go`):

- **The canary never touches the Resolver, the slot gate, or the ledger.**
  It runs on its own plain `http.Client` inside the health checker's run
  goroutine and writes NO `llm_calls` row — a canary probe is
  infrastructure, not a turn. Do not route it through the LLM client.
- **HUNG drives restart ONLY through the existing bool callback.** The
  HUNG edge fires exactly one `false` on `HealthChangeCallback`, so
  `attemptAutoRestart` and the `restart_policy` machinery run unchanged;
  the recovery edge fires NO bool `true` (h.healthy never flipped). The
  verdict callback (`OnHealthVerdict`) is additive: bus event + hang
  counter only, never a second restart path.
- **HUNG is its own verdict state.** An answering `/health` must NOT
  clear `VerdictHealthHung` (only a canary success can), or the verdict would
  oscillate on every health tick while inference stays dead. Canary
  failures count in `canaryFails`, separate from `unhealthyCount`.
- **The self-watchdog re-arms via `Start()` and never spawns a second
  run.** `startWatchdogLocked` is idempotent (armed by `StartAll` AND
  lazily by `RegisterConfig`); exactly one watchdog goroutine exists and
  `StopAll` stops it. A stalled run (no `RunTicks()` advance across two
  samples, process alive, checker not stopped) is re-armed through the
  checker's documented `Start()` re-arm contract — with the generation
  guard making the old run inert — never by launching a parallel ticker.
- **`SetHealthEventPublisher` is the ONLY bus seam and stays nil-safe.**
  The manager has no bus dependency; the daemon wires a closure over its
  raw `Publish` (`internal/daemon/components.go`, at ContainerManager
  construction). Every publish site no-ops while the publisher is nil,
  and the setter ignores nil. The payload keys are exactly the Contract-2
  set: `endpoint_key`/`verdict`/`model`/`detail`/`ts` — no lock is held
  across the publish call.

## Serving tiers, semantic cache, and TTFT (llm-serving-tiers tree)

- **The tier floor BOUNDS rotation; it never reorders precedence.**
  `llm.tiers` (`config.TierPolicyConfig`, mirrored 1:1 as `llm.TierPolicy`
  to dodge the internal/llm -> internal/config import cycle, same pattern
  as `FailurePolicyConfig`) arms a per-request depth limit
  (`EffectiveDepth`, default 2 when enabled) plus per-capability
  `TurnCeilings`. The rotation loops in `provider_manager.go` consult
  `tierGate.nextHopBlocked` BEFORE each dispatch (three call sites:
  provider_manager.go:569, :788, :1044); past the ceiling the typed
  `*TierFloorError` fires (wraps `ErrTierFloor` via `Unwrap()[]error`,
  `NonRetryable` — a classified terminal error like
  `ErrAllModelsQuotaBlocked`: never retried, never a completed turn,
  never parked). The one rule: **the floor consumes DEPTH ONLY.** A
  candidate skipped pre-dispatch (disabled, unhealthy, quota/endpoint/
  alias-blocked) never reaches the floor check and consumes no depth;
  the frozen precedence (quota blocks > endpoint cooldowns > alias
  blocks > stickiness, resolver.go:660-661) is untouched. Signaling
  rides per-step response metadata ONLY — `MetaKeyLLMTier` /
  `MetaKeyLLMTierDegraded` / `MetaKeyLLMTierDepth`
  (tier_policy.go:42-47) written into `Response.Metadata` on EVERY
  completed call when `Enabled && SignalDegraded`; never a new bus
  topic, never a new WS type. Disabled (nil policy or `Enabled=false`)
  = unbounded rotation, zero metadata bytes. Daemon arm:
  `applyTierPolicyToProviderManager(pm, cfg)`
  (internal/daemon/components.go:1841, wiring in
  internal/daemon/tierpolicy_wiring.go) — disabled installs nil.
- **The semantic cache sits strictly BEHIND the exact-key miss.**
  `llm.cache.semantic` (`config.SemanticCacheConfig`, schema.go:1284;
  opt-in, `Enabled` default FALSE) is a second-chance lookup: exact-key
  `Get` first, semantic `LookupBehindMiss` second, budget gate third —
  wired on the three chat-variant cache paths (client.go:792-804
  non-streaming, :1063-1075 with-progress, :2532-2540 streaming-delta;
  the other exact-key cache paths client.go:996, :1266, :2650 stay
  exact-only). Hard rule: **a response carrying tool calls is NEVER
  STORED and NEVER SERVED as a semantic hit**
  (semantic_cache.go:211-215 serve side, :283 store side — the tool
  call IS the answer). Any embedder failure degrades silently to
  exact-only — no error, no parked turn — and ErrEmptyResponse
  accounting is untouched. Default threshold 0.95
  (`DefaultSemanticCacheThreshold`, schema.go:1317-1318). The optional
  `VerifyFloor` two-stage band (wiring-followups-final leaf 03)
  re-embeds candidates scoring in [VerifyFloor, Threshold) and serves
  only on direct-cosine >= Threshold; 0 (the default) disables the
  band, and values at/above Threshold clamp to 0 at the load boundary
  (`NormalizeSemanticCacheDefaults`, schema.go:1341-1344). Disabled =
  structurally unreachable path, byte-identical responses.
- **TTFT is the first content delta; -1 means unmeasured.** Migration
  v9 adds `llm_calls.ttft_ms INTEGER NOT NULL DEFAULT -1` via
  columnMissingGuard (internal/metrics/store.go:523-533, `Version: 9`
  at :530); `captureFirstDelta` (client.go:2211) timestamps the first
  non-empty content delta on the OpenAI-compatible and Anthropic
  streaming paths and `ttftMS` (client.go:2224) writes the record;
  `AvgTTFTMs` (store.go:911) averages ONLY measured rows. Non-streaming
  calls, exact/semantic cache hits (they short-circuit before any clock
  arm), and errors stay -1 — never write 0 for "not measured".

## Context-warm seam (`Client.Warm`, plan 20261113-filler-clips-and-context-warm phase 2)

`Warm` (internal/llm/warm.go) pre-issues a throwaway max_tokens=1,
non-streaming chat request whose ONLY purpose is planting the imminent
turn's prefix in the provider prompt cache / KV slot. Invariants:

- **Prefix identity over the real serialization path.** The request is
  built through the SAME buildChatRequest shape a real turn uses
  (system-prompt cache boundary, ToOpenAIDictWithStore rendering, stable
  tool order, prompt_cache_key, affinity header via
  applyExtraHeaders + applyAffinityHeader), so the cached prefix matches
  by construction. A divergence on ANY field silently kills the cache
  hit — that is the whole feature.
- **Zero side effects are the contract (R8).** Warm participates in NONE
  of: the short-retry budget, recordAliasFailure / endpoint / alias /
  quota blocks, the slot gate / pacer, the budget gate, semantic-cache
  put, the `llm_calls` ledger, metrics.Store records, tier signaling, WS
  relay, or session messages. It never returns a completion or an error;
  ANY failure (transport, `*QuotaResetError`, 4xx, timeout, panic) is
  DEBUG-logged and swallowed. Do not route it through doRequest — that
  function owns the slot gate, pacer, Error-level logging, and metrics
  rows.
- **One lane per client, replace-discard.** A new Warm cancels the
  in-flight one rather than queueing; a hung warm is bounded by an
  internal ceiling deadline over the caller ctx so the lane always
  releases. The completion text is discarded and NEVER logged.
- **Provider coverage.** Only the OpenAI-compatible `Client` performs a
  real warm (the llama.cpp-family serving tier). `AnthropicClient.Warm`
  and `CodexClient.Warm` are explicit no-op twins by design — keep the
  `warmDispatcher` shape every client class implements so callers never
  type-switch.
- **`alias` is a DEBUG-attribution label only.** A bare *Client serves
  whatever (possibly Reconfigured) config it holds; request-scoped alias
  resolution is the Resolver/ProviderManager lane and is not reachable
  from Warm. Callers pass the alias of the real turn for log
  correlation.
