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
