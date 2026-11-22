# QUOTA-FREE TEST RIG: CONFIG INVENTORY (meept-usability-head, HEAD 1cee89d7)

Scope: every config key controlling budgets, quota retry/parking, throttle/failure
policy, turn watchdogs, classifier LLM usage, context firewall/compaction, and
sync chat; plus CONFIG-GATED vs ERROR-DRIVEN classification of each enforcement
mechanism, verified against daemon wiring. All paths relative to repo root
/tmp/meept-usability-head. Schema = internal/config/schema.go unless noted.
TOML and JSON paths are identical strings (toml/json tags match on every field
listed). Durations in TOML accept Go duration strings ("30s", "24h"); JSON5
loader converts quoted durations (schema.go:2680-2685).

Load semantics that matter: user config is unmarshaled ONTO DefaultConfig()
(schema.go:1522-1527, 1642-1646), so an absent section inherits defaults;
explicit false/0 in the file overrides the default. BUT NormalizeQuotaRetryDefaults
(internal/config/config.go:75, :251; schema.go:1771-1780) and
NormalizeFailurePolicyDefaults (config.go:104, :280; schema.go:1736-1751) clamp
zero/negative durations BACK to defaults. You cannot zero out max_wait/horizon
via config; only the Enabled bools are real off-switches, and several mechanisms
ignore even those (findings 12-18).

--------------------------------------------------------------------------
## 1. CONFIG KEY INVENTORY
--------------------------------------------------------------------------

### 1.1 llm.budget.* (BudgetConfig, schema.go:1866-1879; defaults schema.go:4298-4310)

| toml/json key | type | default | schema.go line |
|---|---|---|---|
| llm.budget.enabled | bool | false | 1867 (default 4299) |
| llm.budget.mode | string | "" -> "enforce" (ParseBudgetMode, internal/llm/budget.go:93-96; "steer" = advisory-only) | 1868 |
| llm.budget.hourly_token_limit | int | 0 (unlimited) | 1869 (4300) |
| llm.budget.daily_token_limit | int | 0 | 1870 (4301) |
| llm.budget.daily_cost_limit | float | 0.0 | 1871 (4302) |
| llm.budget.hourly_cost_limit | float | 0.0 | 1872 (4303) |
| llm.budget.rate_limit_rpm | int | 0 (unlimited) | 1873 (4304) |
| llm.budget.aggressiveness | float | 0.5 | 1874 (4305) |
| llm.budget.per_task_token_limit | int | 0 | 1875 (4306) |
| llm.budget.per_session_token_limit | int | 0 | 1876 (4307) |
| llm.budget.per_task_cost_limit | float | 0.0 | 1877 (4308) |
| llm.budget.per_session_cost_limit | float | 0.0 | 1878 (4309) |

Semantics (schema.go:1846-1865): enabled=false is the ONE global budget switch;
zero limit = unlimited, never a default cap. Budget VALUES live only in
meept.json5 (SSOT); DefaultConfig is all-zero/disabled.

### 1.2 agent.budget.* (AgentBudgetConfig, schema.go:2709-2733; defaults 4657-4675)

NO enable flag of its own (removed deliberately, schema.go:2704-2706). Wired only
when llm.budget.enabled AND total > 0 (agentBudgetEnabled, daemon.go:89-94;
consumed daemon.go:926).

| key | type | default | line |
|---|---|---|---|
| agent.budget.total | int | 0 (no cap, hierarchy never wired) | 2710 (4662) |
| agent.budget.reserved_ratio | float | 0.1 | 2711 (4663) |
| agent.budget.phases.enabled | bool | false | 2722 (4665) |
| agent.budget.phases.auto_allocate | bool | true | 2723 (4666) |
| agent.budget.phases.carryover | bool | true | 2724 (4667) |
| agent.budget.phases.borrowing | bool | true | 2725 (4668) |
| agent.budget.warning_thresholds.task | float | 0.7 | 2730 (4671) |
| agent.budget.warning_thresholds.phase | float | 0.8 | 2731 (4672) |
| agent.budget.warning_thresholds.turn | float | 0.9 | 2732 (4673) |

### 1.3 llm.quota_retry.* (QuotaRetryConfig, schema.go:1528-1541; defaults 4341-4346)

| key | type | default | line |
|---|---|---|---|
| llm.quota_retry.enabled | bool | TRUE | 1530 (4342) |
| llm.quota_retry.max_wait | duration | 24h (DefaultQuotaRetryMaxWait, 1545) | 1534 (4343) |
| llm.quota_retry.default_estimate | duration | 1h (1546) | 1537 (4344) |
| llm.quota_retry.defer_check_interval | duration | 10m (1547) | 1540 (4345) |

max_wait <= 0 clamps to 24h at load (NormalizeQuotaRetryDefaults, schema.go:1771-
1774) AND again in every consumer (NewTurnParker parked_turn.go:211-212 ->
llm.DefaultQuotaMaxWait = 24h, internal/llm/client.go:174; NewQuotaResumeWatcher
quota_resume.go:198-199). There is no config value that means "never park".

### 1.4 parking.* (ParkingConfig, schema.go:138-144; default 4827-4832)

| key | type | default | line |
|---|---|---|---|
| parking.queue_backend | bool | false (sqlite parks.db path) | 143 (4831) |

Persistence is NOT configurable off: parks.db is opened unconditionally at
components.go:3342-3348 (DataDir/parks.db); failure to open degrades to
memory-only parking (3344-3346), not to no parking. Owner column partitions:
parked_turns table + owner column, park_store_sqlite.go:126-144; owners
"quota-watcher" | "throttle" | "gate" | "episode" | "budget"
(park_store_sqlite.go:114-122). Queue backend (opt-in) stores parks as
type="parked_turn" jobs, wake=next_retry_at, owner=claimed_by
(park_backend_queue.go:5-27,67); gate at components.go:8119-8127 returns nil
unless Parking.QueueBackend AND queue is PersistentQueue.

### 1.5 llm.failure_policy.* (FailurePolicyConfig, schema.go:1647-1692; defaults 4347-4358)

NO enabled flag on the section itself.

| key | type | default | line |
|---|---|---|---|
| llm.failure_policy.horizon | duration | 24h (1696) | 1651 (4348) |
| llm.failure_policy.base_throttle | duration | 30s (1697) | 1655 (4349) |
| llm.failure_policy.base_quota_402_extra | duration | 5m (1698) | 1659 (4350) |
| llm.failure_policy.poll_floor | duration | 1h (1699) | 1663 (4351) |
| llm.failure_policy.short_retries | int | 3 (1700) | 1667 (4352) |
| llm.failure_policy.pacing.enabled | bool | TRUE (DefaultPacingEnabled, 1702) | 1682 (4354) |
| llm.failure_policy.pacing.target_429_per_hour | int | 1 (1703) | 1686 (4355) |
| llm.failure_policy.pacing.min_interval | duration | 1s (1704) | 1689 (4356) |
| llm.failure_policy.pacing.max_interval | duration | 30s (1705) | 1691 (4357) |

All durations/int clamp back to defaults when <= 0 (NormalizeFailurePolicyDefaults,
schema.go:1736-1751). Pacing enabled=false DOES disable the pacer
(components.go:1415-1420 only calls SetPacer when fp.Pacing.Enabled; nil pacer =
zero overhead, client.go:203-205, 1996).

agent.retry.* (AgentRetryConfig, schema.go:2673-2692; defaults 4640-4650):
agent.retry.enabled=true, default_budget=5, backoff.base_delay=1s, max_delay=30s,
multiplier=2.0, jitter=0.3, per_operation map. enabled=false skips the global
backoff override install (daemon.go:962-991); it does NOT disable the llm-client
short-retry loop (that reads llm.failure_policy.short_retries, client.go:864-869).

### 1.6 orchestrator.turn_watchdog.* (TurnWatchdogConfig, schema.go:3922-3930; defaults 4949-4953)

| key | type | default | line |
|---|---|---|---|
| orchestrator.turn_watchdog.enabled | bool | TRUE | 3924 (4950) |
| orchestrator.turn_watchdog.interval_seconds | int | 30 (0 -> 30) | 3926 (4951) |
| orchestrator.turn_watchdog.stale_after_seconds | int | 600 | 3929 (4952) |

DOC DEFECT (minor): field comment schema.go:3928 says "0 -> 120" but the header
comment 3921 and DefaultConfig 4952 say 600. Effective default is 600.
Wiring: daemon.go:310-326 - enabled=false constructs nothing (zero goroutines).
CONFIG-GATED, fully disableable.

Related: agent.watchdog.* (WatchdogConfig, schema.go:2870-2881; defaults
4612-4618): enabled=true, timeout_minutes=10, heartbeat_interval_sec=30,
max_iterations=50, stuck_iteration_count=5. Start() returns immediately when
!Enabled (internal/agent/watchdog.go:103-107); wired components.go:2329,
started 6367. CONFIG-GATED. NOTE: max_iterations=50 is an iteration cap
independent of tokens; raise it for long rig runs.

### 1.7 Classifier LLM enable/disable

FINDING (MAJOR): there is NO enabled flag that turns off LLM intent
classification. Verified: grep of schema.go classifier keys yields only
orchestrator.classifier_fail_fast (3842), orchestrator.classifier_boot_fail_fast
(3854), orchestrator.classifier_prefilter.* (3862, 3955-3990),
orchestrator.classifier.session_state_upgrade (3871, 3934-3944). None disables
the LLM classifier.

- Dispatcher builds llmClassifier whenever a client exists
  (dispatcher.go:655-682): dedicated ClassifierClient, else falls back to the
  main LLMClient. The dedicated client comes from models.json5 classifier_model
  -> small_model fallback (components.go:1735-1760; ModelsConfig
  internal/config/config.go:531). Even clearing BOTH slots keeps the main-client
  fallback (components.go:1759 "Classifier will use main LLM client").
- orchestrator.classifier_prefilter.enabled (default false, schema.go:3957,
  4899) can ROUTE AROUND the LLM for confident inputs (embedding prefilter,
  dispatcher.go:744) but is not an off-switch; low-confidence inputs still hit
  the LLM chain.
- Short/simple messages skip the classifier chain unconditionally
  (dispatcher.go:1556-1574, isShortSimpleMessage guard) - useful for rig chat
  probes.
- multi_agent.enabled=false (schema.go:2948; default TRUE at 4561; gate
  components.go:4082) removes the dispatcher entirely (single-agent chat path,
  components.go:4932-4961) - the only real config way to avoid classifier LLM
  calls, at the cost of all multi-agent routing.
- orchestrator.classifier_boot_fail_fast default TRUE (4897) makes an unhealthy
  LOCAL spawned classifier runtime a FATAL boot failure; set false for the rig
  (schema.go:3843-3854). Cloud-only chains never trip it.
- orchestrator.classifier_fail_fast default false (4893): true fails the turn
  on primary classifier error instead of alias rotation (testing knob).

### 1.8 Context firewall / compaction switches

llm.context_firewall.* (LLMContextFirewallConfig, schema.go:1481-1516; defaults
4325-4340):

| key | default | line |
|---|---|---|
| llm.context_firewall.enabled | TRUE | 1482 (4326) |
| llm.context_firewall.summarize_history | true | 1483 (4327) |
| llm.context_firewall.small_model_context_threshold | 32768 | 1484 (4328) |
| llm.context_firewall.iteration_budget_ratio | 0.30 | 1485 (4329) |
| llm.context_firewall.conversation_budget_ratio | 0.50 | 1486 (4330) |
| llm.context_firewall.chunk_large_inputs | true | 1487 (4331) |
| llm.context_firewall.chunk_threshold_ratio | 0.25 | 1488 (4332) |
| llm.context_firewall.wrap_up_threshold | 0.50 | 1490 (4333) |
| llm.context_firewall.hard_limit | 0.80 | 1492 (4334) |
| llm.context_firewall.drop_context_on_hard_limit | true | 1494 (4335) |
| llm.context_firewall.proactive_compression | false | 1498 (4336) |
| llm.context_firewall.model_context_limit | 0 (use model) | 1501 |
| llm.context_firewall.hierarchical_summarization | false | 1505 |
| llm.context_firewall.max_summary_level | 3 | 1508 (4337) |
| llm.context_firewall.summary_level_threshold | 500 | 1511 (4338) |
| llm.context_firewall.overflow_strategy | "restart" | 1515 (4339) |

enabled=false is a REAL off-switch: processMessages returns messages unchanged
(internal/llm/context_firewall.go:587-589). Wired via
SetContextFirewallConfig(cfg.LLM.ContextFirewall), components.go:2723.
CAVEAT: the firewall's summarize_history lane makes LLM summarizer calls; for a
quota-free rig set enabled=false (or at least summarize_history=false).

Top-level compaction.* (CompactionConfig, schema.go:921-947; defaults
5145-5158): enabled=FALSE, model="", reserve_tokens=16384,
keep_recent_tokens=20000, max_response_tokens=13107,
summary_format="structured", trigger_ratio=0.60, iterative_updates=true,
track_file_ops=true, timeout_seconds=30, spill_enabled=false,
decision_enabled=false, decision_keep_bar=2. This is the block actually wired
into loops: WithCompactionConfig(cfg.Compaction) components.go:2398 and
registry Compaction: cfg.Compaction components.go:4151; consumed
loop.go:3014. Default off = no compaction LLM calls.

FINDING (MINOR, dead config): agent.compaction.* (AgentCompactionConfig,
schema.go:2757-2765) has NO consumer - grep for cfg.Agent.Compaction across the
repo returns zero non-test hits. Setting it does nothing; the effective knob is
top-level compaction.enabled.

agent.atomic_compaction.* (schema.go:2770-2780; defaults 4687-4696):
enabled=false (opt-in HALO Phase 6), max_turns_before_compact=50,
max_tool_calls_per_turn=10, keep_last_n_turns=10. Leave off.

agent.compression.enabled default false (schema.go:4629).
agent.context_assembler.enabled default false
(internal/config/context_assembler_config.go:24-27).
session.compaction (bool, schema.go:974) default false (SessionConfig default
block 5162-5171 leaves it zero). Legacy; leave off.

### 1.9 orchestrator.sync_chat_enabled

orchestrator.sync_chat_enabled, bool, default FALSE (schema.go:3895, default
4939). false = async everywhere: chat.submit acks immediately, result via
turn.terminal (schema.go:3887-3894). Wired SetSyncMode at BOTH handler
construction sites: components.go:4430 (dispatcher mode) and 4944 (single-agent
mode). Related: sync_wait_stall default 0 = stall detection on, 30m hard cap
(sync_wait_max, schema.go:3906-3914, defaults 4944-4945); negative stall value
restores legacy fixed 110s ceiling. CONFIG-GATED.

--------------------------------------------------------------------------
## 2. CONFIG-GATED vs ERROR-DRIVEN (wiring verified)
--------------------------------------------------------------------------

F1. llm token/cost/RPM budget - CONFIG-GATED. llmBudgetConfigFromConfig
(daemon.go:65-82): enabled=false returns all-zero llm.BudgetConfig ->
NewBudget unlimited. Tracker built unconditionally when llmCfg != nil
(components.go:1289) but an all-zero config enforces nothing. BudgetExceededError
can never fire with enabled=false.

F2. Hierarchical task/phase/turn budget - CONFIG-GATED. agentBudgetEnabled
(daemon.go:89-94) = LLM.Budget.Enabled && Agent.Budget.Total > 0; hierarchy
wired only at daemon.go:926. Default off twice over.

F3. Budget park (chat turn parked on BudgetExceededError) - CONFIG-GATED
indirectly. Watcher built by SetBudget whenever tracker non-nil
(handler.go:2708-2714; components.go:4440-4442), park fires only on
BudgetExceededError + time-windowed reason (handler.go:1304-1326). With budget
disabled no such error exists, so the park is inert. Persistence/owner wiring
components.go:4465-4476 (owner "budget").

F4. Quota-aware alias rotation / credential blocking (resolver) -
CONFIG-GATED by llm.quota_retry.enabled. ConfigFromSchema copies Enabled
(resolver_direct.go:45-52); SetQuotaConfig at components.go:1469-1470;
quotaEnabled() = quotaCfg != nil && Enabled (resolver.go:258-261) gates every
isQuotaBlocked skip (resolver.go:810, 906, 946, 1139, 1366, 1436, 1536...).
enabled=false disables rotation-blocking. NOTE: the wait+retry wrapper
quotaWaitChatter (resolver_direct.go:30, 54-66 honors !cfg.Enabled) has NO
production call site - grep for newQuotaWaitChatter outside resolver_direct.go
returns nothing; it is dead wiring. The Enabled flag therefore only controls
resolver blocking, NOT the error production or the parks below.

F5. QuotaResetError production - ERROR-DRIVEN, NO SWITCH. HTTP 429 with
quota-window shape and HTTP 402 are classified into QuotaResetError directly in
the client response parser (client.go:2147-2157, 2194-2209; anthropic.go
mirrors). llm.quota_retry.enabled does not gate this; only max_wait rides in as
a cap on derived waits (client.go:2151). ProviderManager quota-credential
persistence likewise only takes MaxWait (components.go:1981,
provider_manager.go:295-301, 381-382) - no enable check.

F6. ThrottleBackoffError production - ERROR-DRIVEN, NO SWITCH. After
short_retries exhausted on a throttle-class failure the client ALWAYS returns
ThrottleBackoffError (client.go:950-975; anthropic.go:512-534, 778-802).
short_retries clamps <=0 to 3 (schema.go:1749-1751), so the retry loop cannot be
zeroed via config either.

F7. Chat quota park (QuotaResumeWatcher) - ERROR-DRIVEN, NOT DISABLEABLE.
SetQuotaResumeConfig(maxWait) is called UNCONDITIONALLY at both handler sites
(components.go:4447 dispatcher mode, 4960 single-agent mode); it never reads
QuotaRetry.Enabled (handler.go:2808-2817). Park fires on AsQuotaResetError with
non-zero UnblockAt whenever h.quotaResumeWatcher != nil (handler.go:1329-1352,
guard at 1336) - always true in the daemon. max_wait<=0 falls back to 24h
(quota_resume.go:198-199) and Park refuses waits > maxWait (quota_resume.go:
292-299) but never "off". Persistence + owner "quota-watcher"
components.go:4480-4494. SEVERITY: MAJOR (missing disable switch for a rig that
must fail fast instead of silently queueing turns for up to 24h).

F8. Throttle park (TurnParker on the loop) - ERROR-DRIVEN, NOT DISABLEABLE.
throttleParker := NewTurnParker(logger, nil, cfg.LLM.QuotaRetry.MaxWait) built
UNCONDITIONALLY in the dispatcher branch (components.go:4505-4536),
SetThrottleParker -> loop.SetThrottleParker (handler.go:2731-2738),
Start(c.ctx) at 4536. parkThrottledTurn (loop_park.go:360) is gated ONLY by
l.turnParker != nil (loop_park.go:361) - no config check anywhere. Call sites
loop.go:5717 and 7390. Per-session loop clones inherit the parker
(loop.go:10029-10032), so specialist sessions park too. Wait > maxWait abandons
with ThrottleGiveUpError (loop_park.go:378-397) instead of parking - still not
"off". Persistence kind=chat owner="throttle" (components.go:4521-4528).
SEVERITY: MAJOR (no disable switch).
NOTE (asymmetry): the single-agent branch (components.go:4932-4961) wires
SetQuotaResumeConfig but NOT SetThrottleParker - with multi_agent.enabled=false
the template loop has no throttle parker and throttle errors pass through
visibly. That is the only config-reachable way to avoid throttle parking, and
it is a side effect, not a designed switch.

F9. Endpoint-blocked park - ERROR-DRIVEN, NOT DISABLEABLE.
parkEndpointBlockedTurn (loop_park.go:499-535) fires when all alias candidates
are endpoint-blocked after timeout-class failures (loop.go:7095-7112,
7650-7660, 7687-7692). Gate: l.turnParker != nil && l.resolver != nil
(loop_park.go:523) - same always-wired parker as F8. Constructs a synthetic
ThrottleBackoffError with RetryAt = endpoint block deadline and reuses the
throttle schedule. No config switch. SEVERITY: MAJOR for a rig pointed at a
flaky/dead endpoint: turns park silently until cooldown expiry.

F10. Quota episode tracker - ERROR-DRIVEN, NOT DISABLEABLE. quotaTracker :=
NewQuotaEpisodeTracker built unconditionally (components.go:2377-2378), shared
into registry (4158). Enter() called on every QuotaResetError in the loop
(loop.go:7507-7512). Background reaper fires 12h/20h/24h escalation tiers
(quota_episode.go:106-115, tiers at 114) and drives agent state
running -> quota_wait -> blocked -> idle. No config gate. Observability/state
only (does not itself park), but it mutates agent state on quota errors
regardless of any switch. SEVERITY: MINOR for rig (state noise), no off switch.

F11. Failure-policy install - CONFIG-VALUED, ALWAYS INSTALLED.
agent.SetFailurePolicyDefaults (components.go:4537-4547) installs the schedule
process-wide from llm.failure_policy; workers get the same via SetProviderPolicy
(components.go:6275-6284, 6308, 6327). No enabled flag; only pacing.enabled
inside it is a true off-switch (F13).

F12. Worker provider-wait requeue - ERROR-DRIVEN, NOT DISABLEABLE.
Worker.requeueOnProviderWait (internal/worker/worker.go:596-671, call site 389)
requeues quota/throttle-class job failures WITHOUT consuming a retry whenever
the queue implements queue.Requeueable and resumeAt <= now+horizon. Reads only
failure_policy (horizon); no enable switch. horizon<=0 clamps to 24h, so it
cannot be zeroed. SEVERITY: MAJOR for queue-job rig runs (jobs silently defer
instead of failing).

F13. Adaptive pacing - CONFIG-GATED. fp.Pacing.Enabled gate at
components.go:1415; SetPacer only when true; nil pacer never waits
(client.go:1996-1997, 203-205). Default TRUE (schema.go:1702). Disable with
llm.failure_policy.pacing.enabled=false.

F14. Tactical scheduler quota deferral (the 10-deferrals/6h logic) -
ERROR-DRIVEN, NOT DISABLEABLE BY CONFIG. Lives in internal/agent/tactical.go
(NOT agent_quota_aware.go - that file is a 16-line AgentHealthSource interface,
agent_quota_aware.go:1-16). DefaultQuotaDeferralPolicy: MaxDeferrals=10,
MaxTotalDeferral=6h, UnknownResetDelay=5m (tactical.go:37-43).
quotaDeferralScheduled (tactical.go:1048-1075) requeues quota-failed step jobs;
a policy with MaxDeferrals<=0 disables it (tactical.go:46-47, 1050-1052) and
TacticalSchedulerConfig.QuotaDeferral is the override seam (tactical.go:
409-412, 449-451) - BUT the daemon construction (components.go:4692-4705) never
sets QuotaDeferral, and NO config key maps to it (grep quota_deferral in
schema.go: zero hits). SEVERITY: MAJOR (code seam exists, config wiring
missing).

F15. Turn watchdog (async reaper) - CONFIG-GATED (see 1.6). enabled=false ->
nothing constructed (daemon.go:310-326). Default TRUE, stale_after 600s. For a
rig with long silent steps either disable or raise stale_after_seconds.

F16. Agent watchdog - CONFIG-GATED (watchdog.go:103-107). Default enabled,
10min timeout / 50 iterations.

F17. Context firewall - CONFIG-GATED (context_firewall.go:587). Default ON;
disable to remove summarizer LLM calls and context-drop behavior.

F18. Compaction - CONFIG-GATED, default OFF (compaction.enabled=false,
schema.go:5146; loop.go:3014 gate). agent.compaction.* is dead config (1.8).

F19. Sync chat - CONFIG-GATED, default async (1.9).

F20. Gate checkpoint parker (HITL plan approval parks, owner "gate",
ParkKindGate) - built whenever ParkStore opens (components.go:3349-3353,
wireGateParker). Not quota-related but shares parks.db; parks plan turns waiting
for human approval. Not disableable by a parking switch; avoid by not using
HITL-gated plans in the rig.

--------------------------------------------------------------------------
## 3. PARKING/DEFERRAL THAT CANNOT BE DISABLED BY CONFIG (summary)
--------------------------------------------------------------------------

1. Chat quota park (QuotaResumeWatcher): always wired (components.go:4447,
   4960); fires on QuotaResetError (handler.go:1336). Max 24h wait, clamped.
2. Throttle park (TurnParker): always wired in dispatcher mode
   (components.go:4505-4536); gate is only turnParker != nil
   (loop_park.go:361). Clones inherit (loop.go:10030).
3. Endpoint-blocked park: same parker, fires on all-candidates-blocked timeouts
   (loop_park.go:522-535; loop.go:7112, 7660).
4. Quota episode tracker + 12h/20h/24h escalations + agent state transitions:
   always constructed (components.go:2377), Enter on every quota error
   (loop.go:7512).
5. Worker provider-wait requeue: always active on requeueable queues
   (worker.go:389, 596); horizon clamped to 24h minimum via normalization.
6. Tactical 10-deferrals/6h quota deferral: defaults always installed
   (tactical.go:449); disable seam exists in code but no config key and daemon
   never sets it (components.go:4692).
7. parks.db persistence itself: unconditional open (components.go:3342);
   parking.queue_backend only swaps the storage backend, never disables parking.
8. QuotaResetError/ThrottleBackoffError production in the HTTP clients: no
   switch at all (client.go:2147-2209, 950-975).

Partial escape hatches that DO exist:
- llm.quota_retry.enabled=false disables resolver quota-blocking/rotation
  (resolver.go:260-261) but NOT any park above.
- multi_agent.enabled=false removes the dispatcher AND (side effect) the
  throttle parker wiring, leaving only the quota watcher park on the
  single-agent handler. Classifier LLM calls disappear with the dispatcher.
- Short/simple chat inputs bypass the classifier chain (dispatcher.go:1559).
- Failure horizons: waits beyond max_wait/horizon FAIL the turn instead of
  parking (quota_resume.go:292, loop_park.go:383-397, worker.go:626-639) -
  fail-fast is reachable only by making the provider's reset horizon exceed
  24h, not by config.

--------------------------------------------------------------------------
## 4. RECOMMENDED MINIMAL RIG CONFIG (toml)
--------------------------------------------------------------------------

Disables everything disableable; the non-disableable residue is listed after.

```toml
# --- budgets: fully off (defaults already off; explicit for clarity) ---
[llm.budget]
enabled = false
# mode/hourly/daily/per-task/per-session limits: leave absent (0 = unlimited)

[agent.budget]
total = 0            # hierarchy never wired (also requires llm.budget.enabled)

# --- quota retry: disable what the flag actually controls ---
[llm.quota_retry]
enabled = false      # disables resolver quota-blocking/rotation ONLY.
                     # Chat quota park + episode tracker STILL fire (F7/F10).
max_wait = "24h"     # cannot be lowered to 0 (clamps back); keep default.

# --- failure policy: no enable flag; minimize waits, kill pacing ---
[llm.failure_policy]
horizon = "24h"          # clamped; cannot zero
base_throttle = "30s"    # clamped; cannot zero
poll_floor = "1h"        # clamped; cannot zero
short_retries = 3        # clamped; cannot zero
[llm.failure_policy.pacing]
enabled = false          # REAL off-switch (F13)

# --- parking backend: keep sqlite (queue backend is opt-in, irrelevant) ---
[parking]
queue_backend = false

# --- watchdogs: off so long silent rig steps are never reaped ---
[orchestrator.turn_watchdog]
enabled = false
[agent.watchdog]
enabled = false
# if you keep agent.watchdog on, raise max_iterations above 50

# --- context management: no summarizer LLM calls, no compaction ---
[llm.context_firewall]
enabled = false          # REAL off-switch (F17)
[compaction]
enabled = false          # default; explicit
[agent.atomic_compaction]
enabled = false

# --- classifier: no enabled flag exists (MAJOR gap). Options: ---
[orchestrator]
sync_chat_enabled = false          # async everywhere (default)
classifier_boot_fail_fast = false  # don't die at boot on local classifier health
classifier_fail_fast = true        # honest classifier failures, no alias rotation
# To eliminate classifier LLM calls entirely, ALSO set:
[multi_agent]
enabled = false        # removes dispatcher + classifier; SIDE EFFECT: also
                       # removes throttle-parker wiring (F8 note). Single-agent
                       # chat only. Quota watcher park remains (F7).

# --- misc defaults that matter for a rig ---
[agent.retry]
enabled = false        # skips global backoff override install (daemon.go:962)
[employees]
enabled = false        # default TRUE (schema.go:5182); off avoids goal-loop
                       # episode parker (components.go:6849) and bot runtimes
[bots]
enabled = false        # default false already
```

Caveats (what this config CANNOT stop):
1. A provider 429-quota/402 still parks the chat turn for up to 24h via the
   always-wired QuotaResumeWatcher (F7), in BOTH multi_agent modes. The turn
   acks "queued and will run automatically when the quota resets"
   (handler.go:1348-1350). Rig must treat that reply as a quota event, or use a
   provider/endpoint that never returns quota-shaped errors.
2. In multi_agent mode, throttle 429s park turns (F8) and dead endpoints park
   turns (F9) regardless of every switch above.
3. Queue jobs still quota-defer: worker requeue (F12) and tactical
   10-deferrals/6h (F14) have no config off-switch. Avoid queue-dispatched task
   runs in the rig, or accept deferral and detect "quota-deferred" strings.
4. Quota episode tracker still flips agent state to quota_wait/blocked and
   emits escalations (F10).
5. Duration knobs cannot be zeroed: Normalize* clamps <=0 back to defaults at
   load (config.go:75,104,251,280) and consumers re-clamp (parked_turn.go:211,
   quota_resume.go:198). "max_wait=0" is NOT a disable.
6. models.json5 is a separate file: classifier_model/small_model live there
   (config.go:529-531), not in this toml; clearing them does NOT disable the
   classifier (main-client fallback, components.go:1759).
7. llm.budget.mode="steer" is irrelevant with enabled=false; do not rely on it.

Defect register (severity):
- D1 MAJOR: no config switch for chat quota park (F7).
- D2 MAJOR: no config switch for throttle park (F8) / endpoint-blocked park (F9).
- D3 MAJOR: no config key for tactical QuotaDeferral despite code seam (F14).
- D4 MAJOR: no config switch for worker provider-wait requeue (F12).
- D5 MAJOR: no enabled flag for LLM intent classifier (1.7); only structural
  workaround multi_agent.enabled=false.
- D6 MINOR: agent.compaction.* dead config, no consumer (1.8).
- D7 MINOR: turn_watchdog.stale_after_seconds comment says 0->120, actual
  default/normalization 600 (schema.go:3928 vs 3921/4952).
- D8 MINOR: quotaWaitChatter (resolver_direct.go:30) never constructed in
  production; llm.quota_retry.enabled's wait+retry semantics are dead code,
  flag only gates resolver blocking (F4).
- D9 INFO: quota_episode tracker always on (F10); state-machine side effects
  unavoidable.