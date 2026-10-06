# Meept Feature Parity Matrix

**Date:** 2026-10-05
**Source:** Meept `docs/features.md`, direct repo inspection at commit time, plus a source-level 429/Retry-After audit of all seven cloned harnesses (2026-10-05, refs in the RFC 9110 section below).
**Competitors analyzed:** FrontierAgent, duckagent, atomic-agent, prime-agent, Hermes Agent, OpenCode, oh-my-pi, Claude Code/OpenClaw.
**Method:** `make compare-prep` clones each harness to `$TMPDIR/meept-compare`, then every cell below was verified by reading that harness's own source at a pinned commit. Cells marked with a source citation were re-read by the reviewer before this update; cells carried from 2026-08-29 are not re-verified and are marked stale where the last month of commits is known to have moved them.

---

## Legend

| Symbol | Meaning |
|--------|---------|
| **X** | Implemented (feature present and usable) |
| **~** | Partial (similar capability, limited depth or missing key aspect) |
| **-** | Not implemented |
| **N/A** | Not applicable — fundamentally different product category |

---

## Master Comparison Matrix

### Architecture & Orchestration

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Multi-agent specialists | X | X (Agent Team) | - | - | ~ (rlm children) | ~ (subagent) | - | - | - |
| Intent classification | X | - | - | - | - | - | - | - | ~ |
| DAG workflow/plans | X | X (PipelineSpec) | - | - | - | - | - | - | - |
| Async handoff between agents | X | - | - | - | X (agent_message) | - | - | - | - |
| Dynamic subagent spawning | X (request_handoff) | X (AgentBus) | - | - | X (rlm()) | ~ | - | - | - |
| Recursive programmatic subagents | X (one-level `delegate_task` + `request_handoff` wired into BaselineTools; bounded depth-2/3 recursion machinery exists but unwired) | X | - | - | X (rlm + passivation) | - | - | - | - |
| Agent-to-agent messaging | X | X (AgentBus) | - | - | X (send + receipts) | ~ | - | - | - |
| Constitution-bound employees | X | - | - | - | - | - | - | - | - |
| Autonomy tiers (reactive/propose/autonomous) | X | - | - | - | ~ (bounded auto) | - | - | - | - |
| Goal loop with enforcement | X | - | X (persistent goals) | - | X (/goal + autonomous) | - | - | - | - |
| Quality-gated goal completion | X | - | - | - | - | - | - | - | - |
| Plan approve/reject lifecycle | X | ~ (approval gate) | - | - | - | - | - | - | ~ |
| Collaboration engine (pair/diff) | X | - | - | - | - | - | - | - | - |

### Memory & Context

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Short-term conversation store | X | X | X | X | X | X | X | X | X |
| Long-term memory (SQLite/FTS5) | X (6 tiers) | - | X (JSONL) | X (SQLite+FTS5) | X (JSONL) | X (9 providers) | - | X (SQLite FTS) | ~ |
| Vector/semantic search | X (sqlite-vec HNSW) | - | - | ~ (optional embeddings) | - | ~ | - | - | - |
| Knowledge graph (PageRank) | X | - | - | X (bounded links) | - | - | - | - | - |
| Epistemic memory (claims/trust) | X | - | - | - | - | - | - | - | - |
| Context compaction / summarization | X (3-layer firewall) | X | X (guarded_mid projection) | X (summarize+compact) | X | X | ~ | ~ | ~ |
| Session persistence | X (SQLite, tree) | X (checkpoint) | X (append-only JSONL) | X (SQLite) | X (JSONL) | X | - | X | X |
| Conversation branching | X | ~ (/revert) | ~ (/rewind checksum-guarded) | - | - | - | - | - | - |
| Steering / follow-up queues | X | X | ~ | ~ | X | X | - | - | ~ |

### Tools & Execution

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Tool count | 40+ | ~15 | 18 | ~30 | ~15 | 86 | ~20 | ~10 | ~20 |
| MCP client | X (22 preconfigured stdio entries, each with an `install_hint`; **enabled count corrected 2026-10-05 — the catalog is 22 entries, and `obscura` is the one enabled by default besides the zero-config set**) | - | X | X | X | X | Limited | - | X |
| MCP server mode | X | - | - | - | - | - | - | - | - |
| ACP client (drive external agents) | X (opt-in, default off) | - | - | - | - | ~ (ACP server for editors) | ~ (`opencode acp`) | - | - |
| Parallel tool execution | X (semaphore) | X | X | X (resource-class) | X | X | X | X | X |
| Tool streaming progress | X | - | - | - | - | - | - | - | - |
| Browser automation | X (opt-in `[browser]`; SSRF-guarded headless Chrome tool family) | - | - | X (Playwright) | - | ~ | - | - | ~ |
| Computer use (CUA) | X (opt-in `cua-driver` MCP, security-tiered) | - | - | - | - | X (macOS) | - | - | - |
| GBNF grammar-constrained tools | X (opt-in `gbnf_constrained`) | - | - | X | - | - | - | - | - |
| Vision / image describe | X (multimodal input + auto pre-flight describe) | - | - | X (mmproj) | - | - | - | - | - |
| Office doc read/write | MCP only | X (PDF/DOCX/etc.) | - | X | - | MCP | - | - | X |

### Security & Sandboxing

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Permission system | X (SecurityEngine) | X (allowlist) | X (JSON policy) | X (approval ladder) | - | ~ (allowlist) | - | - | X |
| Input sanitization | X (prompt injection) | - | ~ | - | - | ~ | - | - | ~ |
| Command scanning (Tirith) | X | - | - | - | - | X | - | - | - |
| Output scanning | X | - | - | - | - | - | - | - | ~ |
| Taint tracking | X (lattice) | - | - | - | - | - | - | - | - |
| Path fencing | X | - | - | - | - | X | - | - | - |
| OS-enforced sandbox | ~ (docker opt-in) | X (bwrap/container) | X (bwrap/macOS/Windows) | - | - | - | - | - | - |
| Network egress policy | - | ~ | X (proxy+CIDR) | - | - | - | - | - | - |
| Secret-backed credential injection | X (opt-in: `[secrets.sources]` broker + `MEEPT_SECRET:` placeholder env injection; loopback egress proxy) | - | X (placeholder proxy) | - | - | - | - | - | - |
| Diff preview + write approval | X (PendingChanges, FileEdit only) | X (unified diff all mutations) | X (checksum rewind) | X | ~ | - | - | - | X |
| Fail-closed policy | X | X | X | - | - | - | - | - | ~ |

### Scheduling & Persistence

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Cron jobs | X | - | X | X | X | X | ~ | - | X |
| Job queue with priorities | X | - | - | - | - | - | - | - | - |
| Agent-targeted jobs | X | - | - | - | - | - | - | - | - |
| Daemon / resident mode | X | - | X (gateway service) | - | X (daemon supervisor) | X (gateway) | - | - | - |
| Crash recovery / resume | X (durable queue with atomic claims + startup reclaim of crash-orphaned jobs; persisted parked turns re-arm on boot; sessions + scheduler jobs survive restart) | X (--resume) | ~ | ~ | X (worker restart) | ~ | - | - | ~ |
| P2P cluster mesh | X (gossip+WireGuard) | - | - | - | - | - | - | - | - |
| Distributed task queue | X | - | - | - | - | - | - | - | - |

### Model & Cost

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Provider count | 10+ | ~5 | 30+ | ~10 | ~10 | 15+ | 2-3 | limited | ~3 |
| Capability-based routing | X | - | - | - | - | - | - | - | - |
| Model failover chain | X | - | - | X | - | X | - | - | - |
| Refusal fallback (per-agent) | X (typed signals, one-hop, reply disclosure) | - | - | ~ (refusal/content_filter advance the fallback chain; same-model retries first) | - | ~ (content_filter tries one configured fallback, no per-agent slot) | **- (CORRECTED 2026-10-05: `retry.fallbackChains` does not exist — 0 hits tree-wide for `fallbackChain`/`fallback_chain`/`retry.fallback`, and no 429-triggered model rotation in `src/session`. The prior cell claimed a session-pinned fallback chain; it is absent.)** | - |
| Token budgeting | X | ~ | ~ | X | X | X | - | - | ~ |
| Dollar cost tracking | X (OpenRouter live) | - | - | - | - | ~ | - | - | - |
| Reasoning effort support | X | - | - | - | - | ~ | - | - | X |
| Local inference management | ~ (RuntimeManager) | - | - | X (TurboQuant llama.cpp) | - | ~ | - | - | - |
| Subscription CLI provider | X (opt-in: ACP client drives codex-acp; claude/opencode are catalog entries away) | - | - | X (claude/codex) | X | - | - | - | - |

### HTTP 429 / Retry-After Conformance (RFC 9110 §10.2.3, §5.6.7, §7.1.3 + RFC3339)

Audited 2026-10-05 at the pinned refs listed. "Forms" = which header forms are
parsed: **ds** = delta-seconds (`1*DIGIT`, whole value), **IMF** =
IMF-fixdate (`Fri, 31 Dec 2027 23:59:59 GMT`), **850** = RFC850 (obsolete but
RFC 9110 §5.6.7 requires recipients to accept it), **asc** = asctime (same
requirement), **3339** = RFC3339 ISO date (NOT an RFC `Retry-After` form;
parsing it is compatibility credit for senders that deviate, and it is what
Anthropic-style providers send). **ra-ms** = the vendor `retry-after-ms`
millisecond header.

**Pinned refs** (`make compare-prep`, default branch HEAD on 2026-10-05):
FrontierAgent `179709fe`, duckagent `06550f42`, atomic-agent `58075e4a`,
prime-agent `7a52276c`, Hermes `88c60858`, OpenCode `652c090d`, oh-my-pi
`fc6c0c90`. FrontierAgent's behavior comes from its pinned runtime dep
`apodex-agent-core==0.12.2`; oh-my-pi, prime-agent, atomic-agent and duckagent
implement the behavior first-party (no vendor LLM SDK on the path); Hermes and
OpenCode pin their vendor SDKs to zero retries so their own code is live.

Full per-harness evidence, including the negative-claim enumeration method for
each absence, is in `$TMPDIR/429audit/*.md` (regenerate with `make compare-prep`).

| Property | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|
| Reads `Retry-After` | X | ~ (via delegate) | **-** | X | X | X | ~ | X |
| ds (delta-seconds) | X (strict `Atoi`) | ~ (lax float) | - | ~ (lax `Number()`) | ~ (lax f64) | ~ (lax float) | ~ (lax `parseFloat`) | ~ (lax float) |
| IMF-fixdate | X | - | - | X | X | X | ~ (via `Date.parse`) | X |
| RFC850 | X | - | - | X | **-** | X | **-** | X |
| asctime | X | - | - | ~ (parsed as LOCAL tz) | **-** | X | ~ (parsed as LOCAL tz) | X |
| RFC3339 | X | - | - | X | - | **-** | ~ (misparsed as huge ds) | ~ (correct in 2 of 3 parsers) |
| `retry-after-ms` | ~ (Codex delta hdrs) | - | - | - | X (overrides std) | - | X (no lower bound) | X |
| Provider reset headers | X (Anthropic ×3 + Codex ×2) | - | - | - | - | X (6 headers) | ~ (parsed, never slept on) | ~ (different family) |
| Header drives the real sleep | X | ~ (300s cap) | **- (hardcoded 1s)** | ~ (5s cap) | ~ (jitter undercuts) | X (600s cap) | ~ (opt-in path 10s cap) | ~ (caps 30s–5min) |
| Negative/past value handling | X (clamp, floor 0) | **- (negative sleep)** | - (n/a) | X (clamp) | X (clamp) | X (clamp) | **- (negative sleep)** | X (clamp) |
| 429 detected by HTTP status | X | X | ~ (string/regex on body) | X | X | X | X | X |
| Default attempts | 3 short + 24h quota horizon | 5 | 6 per model | 3 | 3 | 3 | 5 (SDK layer 0) | 3–10 by path |
| Quota vs throttle separated | X (never short-retries quota) | **-** | **-** | ~ (5-min park) | X (durable scheduler park) | ~ | ~ | ~ (capacity state machine) |
| **RFC 9110 verdict** | **full** | partial | **none** | partial | partial | near-full | partial | partial |

**Meept is the only harness that parses all four RFC 9110 date forms *and*
RFC3339, and the only one with a strict whole-value delta-seconds parse
(`internal/llm/retry_after.go:44`, `strconv.Atoi` on the entire header value,
deliberately strict so an RFC3339 date cannot be read as a year-sized delta).
It is also the only one that never sleeps a negative duration: a past date
yields a negative delta which the caller clamps (`internal/llm/client.go:223`,
`if step < 0 { step = 0 }`).

**Meept's own limits, stated honestly.** The server wait is not unbounded.
`shortThrottleSleep` (`internal/llm/client.go:209`) takes the server schedule
only when it is *later* than the computed exponential step, and `BackoffPlan.NextAttempt`
(`internal/llm/failure_policy.go:88`) clamps every step to `PollFloor` (default
1h) and the whole schedule to `GiveUpAt` = now + `Horizon` (default 24h). So a
`Retry-After` beyond the horizon is not slept to completion — the turn gives up
or parks instead. That is the correct trade for an always-on daemon (a 7-day
`Retry-After` must not hold a turn open for a week), but it means "honored"
means "honored up to a 24h horizon, else surfaced as a park/give-up", not
"honored unconditionally".

Where each harness actually fails, with the verified source line:

1. **duckagent is the worst of the seven — it reads no 429 headers at all.**
   One shared error funnel keeps status and body and drops the headers:
   `src/client/sse.rs:15-22` bails with `"provider returned error for {url}:
   HTTP status {} ({})\nbody: {}"` and never touches `response.headers()`. The
   retry is a fixed one second — `src/client.rs:30` `const MODEL_RETRY_DELAY:
   Duration = Duration::from_secs(1)` over `MODEL_RETRY_COUNT: usize = 5`
   (`src/client.rs:29`, loop at `:292`) — so a server asking for 3600s is hit
   6 times per model, then rotated to another model on the same provider.
   A test even pins the substring behavior: `"HTTP status 429 body:
   insufficient_quota"` is asserted **non**-retryable (`src/client.rs:570-579`).
   No test anywhere asserts a header is read.

2. **FrontierAgent delegates and inherits a negative-sleep bug.** Its own tree
   has zero `Retry-After` reads; it runs on `apodex-agent-core==0.12.2`
   (confirmed in `pyproject.toml` deps and `uv.lock`). That engine parses with a
   bare `float(val)` (`agent_core/runtime/loop/_call.py:72`) and sleeps
   `min(retry_after, 300)` (`_call.py:1124-1128` → `:1184`). The cap is a
   *ceiling only*: `Retry-After: -5` passes the truthiness guard and
   `asyncio.sleep(-5.0)` returns immediately, so up to 5 attempts burn the
   window instantly. Second-largest gap: the pinned `openai==2.54.0` SDK ships a
   *correct* parser (`_base_client.py:759-795` — `retry-after-ms`, float
   seconds, then `parsedate_tz` for every date form) but it is disabled by
   `max_retries=0` (`openai_chat.py:196`), so the hand-rolled weaker parser is
   what actually runs.

3. **atomic-agent parses the most forms and then throws the answer away.**
   First-party, no SDK to delegate to, five read sites, and the value genuinely
   reaches the sleep — but capped to five seconds on the live cloud path:
   `Math.min(err.retryAfterMs, OPENAI_RETRY_AFTER_CAP_MS)` where the cap is
   `5_000` ms, fed into `Math.max(backoff, retryAfter)` and slept
   (`src/llm/provider/openai/openai-http.ts:931-935`, `:914`). A
   `Retry-After: 3600` is answered with 5s, three times. Its asctime parse goes
   through `Date.parse`, which reads as local time (measured: +5h under
   America/New_York). Its local llama.cpp path reads **no** response headers at
   all (`llama-server-client.ts:385-400`) and `isRetryableLlamaError`
   (`:1668`) retries only 5xx, so a local 429 rotates to a fallback immediately.

4. **opencode prefix-parses dates into huge numbers and never sleeps them.** Both
   parsers use `Number.parseFloat` on the header (`src/session/retry.ts:52,61`),
   so `Retry-After: 2027-12-31T23:59:59Z` becomes `2027000` ms (measured under
   bun) — the date branch below it is unreachable for any digit-leading value.
   `cap()` is `Math.min` only (`retry.ts:43`), with no lower bound, so
   `retry-after-ms: "-5"` yields a negative sleep; the repo's own tests use
   `"retry-after-ms": "0"` as their fast fixture (`retry.test.ts:100,130`).
   Its Anthropic reset headers are parsed into `HttpRateLimitDetails`
   (`llm/route/executor.ts:118-131`) and then no consumer ever computes a sleep
   from them. The one path that honors a real sleep is default-off behind
   `OPENCODE_EXPERIMENTAL_NATIVE_LLM` and caps at 10s (`executor.ts:346`).
   It delegates to Vercel AI SDK `ai@6.0.168`, but pins `maxRetries: 0`
   (`llm.ts:323`), so the SDK's own (stricter) guard is inert.

5. **oh-my-pi has three parsers, and the live one is the broken one.** It
   hand-rolls all transports (no vendor SDK; root `package.json` has 3 deps),
   so its own source is the runtime behavior, and the richest header awareness
   of the seven. But the Anthropic transport does
   `const seconds = Number.parseFloat(retryAfter);`
   (`packages/ai/src/providers/anthropic-client.ts:121`) — prefix parse, so an
   RFC3339 date yields 2027s (~19,000× too early) and the `Date.parse` fallback
   at `:123` is dead code for any digit-leading value. The other two parsers
   (`fetch-retry`, `pi-ai`) get RFC3339 right via `Number()` → NaN →
   `Date.parse`. It also does **not** read the classic
   `anthropic-ratelimit-*-reset` headers (0 hits); it reads a newer
   `anthropic-ratelimit-unified-*` / slow-lane family that feeds a capacity
   state machine rather than the 429 sleep.

6. **prime-agent is the best-behaved rival and still misses two RFC forms.**
   Hand-rolled reqwest, whole-value `f64` parse with a `.max(0)` floor, the
   wait `max()`ed against exponential backoff and handed to a durable scheduler
   park (`agent_engine/turn/quota.rs:34-73`) — so no hot loop, which is the
   failure mode the others share. Two real gaps: only IMF-fixdate parses
   (`crates/pa-ai/src/utils_inner/stream_failure/http_retry.rs:30-48`; RFC850 and
   asctime both return None — verified by executing the parser), and the ±20%
   jitter is applied **after** the server wait
   (`provider_retry.rs:304` vs the correct `max` at `:236-237`), so
   `Retry-After: 60000` sleeps 48000 half the time. The repo's own e2e test
   asserts that undercut as intended (`provider_failure_e2e.rs:672-678`).
   Its pa-ai `retry-after-ms` also *overrides* standard `Retry-After` — inverted
   versus Meept's rule that a standard header always wins.

7. **Hermes is the only rival that reaches near-full.** `wait_time = _retry_after
   if _retry_after is not None else jittered_backoff(...)`
   (`agent/turn_recovery.py:1449`) makes the header the real sleep, and every
   RFC 9110 date form parses via `parsedate_to_datetime`, clamped, capped 600s.
   It deliberately takes the retry itself, pinning `anthropic==0.87.0` and
   `openai==2.24.0` to `max_retries=0`, and it knows six rate-limit reset
   headers. Its single RFC gap is the one meept has covered: **no RFC3339
   fallback** (`retry_utils.py:57`), so an ISO-valued header degrades to 2–60s
   jittered backoff. Secondary: `float('inf')` is not rejected (currently masked
   by the 600s cap). It is the only harness besides Meept with an explicit
   anti-hot-loop rule — a zero/clamped `_retry_after` is treated as absent
   precisely "so we never hot-loop the provider" (`turn_recovery.py:1445-1447`).

### New Since 2026-09-05 (141 `feat` commits in 820) — NOVEL vs PARITY

Audited 2026-10-05 against the same seven pinned refs. **11 clusters, 0 clean
NOVEL, 2 NOVEL, 1 WEAKER, 8 PARITY.** The headline is that the last month did
not extend Meept's lead — it closed gaps. Four clusters that a grep-only read
would have scored NOVEL collapsed to PARITY under source reading, and three of
those matched **Hermes**, which is the matrix's most capable rival once its
own code is read rather than its README.

**Default-on is the unit of account.** A cluster shipping dark is not a
position: `plan_compiler_enabled` is `false` in `config/meept.json5`,
`session_drift` and `burst_detection` are both `enabled: false, log_only:
true`, quickplan is `enabled: false`. Those rows are marked opt-in and must
not be counted as leads.

| # | Cluster (shipped 2026-09-05 → 2026-10-05) | Meept default? | Nearest competitor match | Verdict |
|---|---|:---------:|---|:-----:|
| 1 | Async turn lifecycle + stall watchdog | **yes** (`sync_chat_enabled: false`, async is default) | hermes `gateway/run_turn.py:3468` watchdog + `run.py:2740` synthetic abort | ~ |
| 2 | Output filters as a pipeline (json_format / language / lint_go / lint_js) | **yes** (`schema.go:2864` `Enabled: true`) | hermes `gateway/model_tools.py:854` "plugins may replace the final result string" | ~ |
| 3 | Quickplan + allotment + embedding prefilter | opt-in (all three knobs false) | atomic-agent `src/memory/retrieve/query-rewriter-runner.ts:141` returns before the prompt is built | ~ |
| 4 | Memory confidence bands + calibration feedback | **yes** (live ambient path) | none — oh-my-pi *derives* confidence from band (`veracity-consolidation.ts:330`) | **X** |
| 5 | Plan compiler + tier routing + critique self-seal | opt-in (`plan_compiler_enabled` false) | none — all 7 ship model-authored free-text plans | **X** |
| 6 | Agent-writable skills (`skills_create`/`skills_patch`) | **yes** (`components.go:6140` "Unconditionally") | hermes `tools/skill_manager_tool.py:811` `skill_manage` + live `requires_tools` gate | ~ |
| 7 | Per-agent/per-provider token ledger + tokscale ingest | **yes** (`store.go:245-261`) | hermes `agent/usage_pricing.py:66` `class CanonicalUsage` | ~ (+1 X slice) |
| 8 | Orphan sweep + parent-death runtime supervision | **yes** (`runtime_config.go:58`) | hermes `tools/mcp_death_supervisor.py:23-25` pipe-EOF supervisor | ~ |
| 9 | Anomaly detection on live streams | opt-in (both detectors dark, log-only) | prime-agent `incident/anomaly.rs:53-66` `densest_window_run`, **on unconditionally** | **-** |
| 10 | GUI/CLI config read-write + usage surfaces | **yes** (`server.go:1332`) | hermes `PUT /api/config` + `AnalyticsPage.tsx:581`; opencode `PATCH /global/config` | ~ |
| 11 | Thinking-safe summarization (`DisableThinking`) | **yes** (ChatOption, no flag) | oh-my-pi `title-generator.ts:348` `disableReasoning: true` (8 sites vs our 4) | ~ |

The two NOVEL clusters:

**4 — measured-confidence rejection with a closed feedback loop.** Meept
disposes ambient-extracted memories by three confidence bands, and the band
thresholds come from *measured* accuracy, not intuition: the calibration data
"showed the middle band is 33% correct, so hard-dropping it loses real memory"
(`internal/agent/rejected_candidates.go:113-116`), with a rejection reason
recorded per dropped candidate (`splitFiltered`) and size-capped JSONL rotation
(`internal/memory/rotate.go:20`). oh-my-pi is the only rival with a band
concept at all, but it inverts the dependency — confidence is computed *from*
the band (`weight * 0.5`), so no measurement can ever correct it. To match, a
competitor needs confidence as the input and the band as the output, plus
persisted predicted-vs-actual pairs. This is default-on.

**5 — deterministic plan compiler with self-seal** (opt-in, so it is a lead in
architecture and dark in practice). Five structural checks plus `detectCycle`
return every problem in one pass (`internal/plan/compiler.go:29-33`), and the
critique loop self-seals with provenance `planner-self`
(`internal/agent/plan_critique_loop.go:480-483`). No competitor compiles a
plan at all: FrontierAgent, the one rival with an approval gate, makes human
confirmation mandatory (`frontier_agent/apodex/observers.py:545` `await
self.approver.confirm(...)`), which is the opposite of a self-seal. Meept runs
the legacy `spec_plan` path at default settings.

Three narrow NOVEL slices inside otherwise-parity clusters, each verified:

- **Retention-exempt year-scale call ledger as a stable external contract**
  (`internal/metrics/store.go:590-592`: "llm_calls is exempt from time-based
  retention: it is tokscale's ingest source … must persist year-scale history
  for aggregation", consumer SQL pinned at `tokscale_ingest_test.go:63`). Zero
  `tokscale` or cost-export hits across all seven. Note the shape honestly: the
  export is out-of-band SQLite, **not** an HTTP or CLI exporter.
- **Spawn-time port-conflict refusal** (`internal/llm/runtime_manager.go:182`
  binds health to `proc.IsRunning`; the port pre-probe refuses to spawn into a
  served endpoint). No competitor has it, and the reason is specific: a
  runtime can survive a failed bind and look healthy while serving nothing.
- **Grammar-constrained *and* thinking-disabled extraction** — the two are
  paired at the wire, not just prompted: `WithRawGrammar(AmbientCandidateGrammar())`
  alongside `DisableThinking()` (`internal/daemon/epistemic_wiring.go:111`), and
  `LessonGrammar()`/`ProcedureGrammar()` the same way
  (`internal/memory/distill.go:632`). oh-my-pi suppresses reasoning at 8 call
  sites but binds no grammar at any of them.

**The one regression: cluster 9.** Meept built two temporal anomaly detectors
and ships both dark — `session_drift` and `burst_detection` are
`enabled: false, log_only: true` in `config/meept.json5:710-728`, and the file's
own comments say why ("Blocked on real outcome data from the outcome loop";
"compare against a simple 'N failures in a row' threshold rule before
adopting"). prime-agent runs anomaly detection on its own telemetry
*unconditionally* (`densest_window_run`, `incident/anomaly.rs:53-66`). So on
this row Meept is behind a rival that turned it on. The two NOVEL-looking
detector internals (asymmetric embedding-drift split,
`internal/agent/session_drift.go:29-43`; calibrated cross-fitted cosine gate,
`internal/agent/embed_health.go:231`) are real but log-only, and the embedding
*health* gate is default-on while the *drift* detector is not.

### Self-Improvement & Learning

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Skill evolution (closed loop) | X | - | - | - | ~ (/refine) | ~ | - | - | - |
| Shadow training (LoRA/DPO) | X | - | - | - | - | - | - | - | - |
| Reflection collector | X | - | - | X | - | X | - | - | - |
| Routing decision log | X | - | - | - | - | - | - | - | - |
| Q Agent meta-optimization | X | - | - | - | - | - | - | - | - |
| Automated code fixing | X | - | - | - | - | - | - | - | - |

### Observability & Ops

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Metrics store | X (SQLite TSDB) | X (artifacts) | X | X (NDJSON traces) | ~ | X (cost) | - | - | ~ |
| Structured logging | X (slog) | X | X | X | X | X | - | - | ~ |
| Health endpoints | X | X | X | X | X | X | - | - | - |
| Benchmark harness | X (external meept-bench over JSON-RPC + internal eval suite) | X (14 benchmarks) | X (context policy) | X (GAIA published) | - | - | - | - | - |
| Trace replay / prompt drift | - | X | X | X (NDJSON+hash) | - | - | - | - | - |

### UI & Channels

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| TUI | X (Bubbletea) | X (Textual) | X | X (Ink) | X | X (prompt_toolkit) | X | X | X |
| CLI | X | X | X | X | X | X | X | X | X |
| GUI (Flutter web+desktop) | X | - | - | - | - | - | - | - | - |
| macOS MenuBar app | X | - | - | - | - | - | - | - | - |
| Web API + REST | X | - | ~ | - | ~ | - | - | - | - |
| WebSocket events | X | - | - | - | - | - | - | - | - |
| Telegram bot | X | - | - | X | X | X | - | - | X |
| Chat channels (30+) | ~ | - | X (30+ channels) | - | - | ~ | - | - | - |
| STT / TTS | X | - | - | - | - | - | - | - | X |
| Desktop notifications | X | - | - | - | - | - | - | - | - |
| Multi-user auth | X | - | X (per-channel allowlists) | - | - | X | - | - | X |

### Code Intelligence

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| AST tools (tree-sitter) | X | - | - | - | - | - | - | - | ~ |
| LSP client | X | - | - | - | - | - | - | - | X |
| RepoMap / PageRank | X | - | - | - | - | - | - | - | - |
| Auto-lint + reflection | X | - | - | - | - | - | - | - | X |

### Other

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Evidence pipeline (claim-evidence) | X | - | - | - | - | - | - | - | - |
| Hallucination detection | X | - | - | - | - | - | - | - | - |
| Context propagation to subtasks | X | - | - | - | - | - | - | - | - |
| Token cache (L1+L2) | X | - | - | - | - | - | - | - | - |
| Session branching / forking | X | ~ | ~ | - | - | - | - | - | - |
| Change journal with revert | ~ (git checkpoints) | X (WorkspaceJournal) | X (checksum rewind) | - | - | - | - | - | - |
| Multi-platform installers | - | ~ | X (static binary) | X | X | X | - | - | X |
| Google Calendar integration | X | - | - | - | - | - | - | - | - |
| Video generation | X | - | - | - | - | - | - | - | - |
| Image generation | X | - | - | - | - | - | - | - | - |

---

## Summary by Category

### Where Meept Leads

| Category | Meept Advantage |
|----------|----------------|
| **Multi-agent orchestration** | Only framework with 22 specialists + 6 reviewers, intent classification, DAG planning, async handoff, and a collaboration engine covering pair programming, differential A/B, and parallel-team mixture-of-agents (lead + N specialists, presets) |
| **Memory depth** | 6-tier system: episodic (FTS5), task, knowledge graph (PageRank), semantic (vector HNSW), distributed (memvid), epistemic (claims/trust) |
| **AI Employees** | Only framework with constitution-bound autonomous agents, three autonomy tiers, enforcement engine, and optional quality-gated goal completion |
| **Evidence pipeline** | Only framework with claim-evidence matching, validation gates, and needs_info routing for human review |
| **Defense-in-depth security** | Only framework combining taint lattice, input sanitizer, Tirith shell scan, adversarial boundary markers, and fail-closed policy |
| **429 / rate-limit RFC conformance** | Only harness parsing all four RFC 9110 date forms *and* RFC3339, with a strict whole-value `1*DIGIT` delta parse (`retry_after.go:44`) and a guaranteed non-negative sleep. duckagent reads no 429 headers at all; FrontierAgent and opencode can sleep a negative duration; opencode and oh-my-pi prefix-parse an RFC3339 date into a ~19,000× too-early delta |
| **Self-improvement loops** | Only framework with closed-loop skill evolution (usage tracking + LLM-judge verifier + versioning), shadow LoRA/DPO training with eval gate, reflection collector, and Q-Agent meta-optimization |
| **Always-on daemon** | Only framework with resident daemon + five frontends (TUI/Flutter/MenuBar/Telegram/HTTP+WS) + voice (STT/TTS) |
| **Cluster mesh** | Only framework with P2P gossip networking, WireGuard sync, and distributed task queue |
| **MCP server mode** | Only framework that can be consumed BY other agents as an MCP server |
| **ACP client** | Drive Codex/OpenCode/other Agent Client Protocol agents as full peers (`acp_agent`); `[acp] enabled` defaults false |

### Where Competitors Lead

| Category | Leader | Gap for Meept |
|----------|--------|---------------|
| **OS-enforced sandboxing** | duckagent (bwrap/macOS/Windows), FrontierAgent (bwrap/container) | Meept's docker backend is opt-in and degrades to unsandboxed on failure |
| **Network egress policy** | duckagent (host+CIDR maps, NO_PROXY scrubbing) | Zero network-layer control in meept |
| **Secret-backed credential injection** | duckagent (reverse-proxy placeholder) | Meept's broker uses placeholder env injection + loopback egress proxy instead of a reverse proxy; raw env inheritance is gone for declared secrets |
| **Diff preview + reversible journal** | FrontierAgent (WorkspaceJournal, /revert), duckagent (checksum rewind) | Meept has PendingChangesRegistry but only for FileEditTool, no user-facing surface |
| **Computer use / browser** | atomic-agent (Playwright suite), Hermes (macOS CUA) | Meept ships both opt-in (headless-Chrome tool family + cua-driver MCP), but neither is enabled by default |
| **Local inference ownership** | atomic-agent (TurboQuant llama.cpp, GBNF, subscription CLI) | Meept assumes external endpoints; no model management or KV-cache economics |
| **429 / rate-limit RFC correctness** | **Hermes** (`turn_recovery.py:1449`, all four RFC date forms, clamped, header is the real sleep) | Meept is compliant too and adds RFC3339 + a strict delta-seconds parse, but **every** harness truncates the server wait somewhere — atomic-agent to 5s (`OPENAI_RETRY_AFTER_CAP_MS`), oh-my-pi to 30s–5min, FrontierAgent to 300s. Meept bounds by horizon (1h floor / 24h give-up) instead of discarding the value |
| **Telemetry anomaly detection, actually enabled** | prime-agent (`densest_window_run`, `incident/anomaly.rs:53-66`, on unconditionally) | Meept built two detectors and ships both dark: `session_drift` and `burst_detection` are `enabled: false, log_only: true` in `config/meept.json5` |
| **30+ chat channels** | duckagent (Slack, Discord, Signal, Teams, Home Assistant, etc.) | Meept: Telegram + Web API + MenuBar only |
| **Benchmark + published results** | FrontierAgent (14 benchmarks), atomic-agent (GAIA L1) | Meept-bench is external and working but has no published scorecards yet |
| **Crash-safe scheduling** | prime-agent (tick claiming, coalesced missed ticks), duckagent (tombstones) | Meept's queue claims are atomic and startup-reclaimed, but not tombstoned/coalesced |
| **OpenAI-compatible API** | duckagent + atomic-agent (`POST /v1/chat/completions`) | Meept exposes REST/WS/MCP but not this shape |

---

## Competitive Positioning

```
Feature Breadth (unique capabilities count)
┌─────────────────────────────────────────────────────────────────┐
│ Meept          ████████████████████████████████████  42 unique  │
│ duckagent      ████████████████                    22 unique   │
│ prime-agent    █████████████                       18 unique   │
│ atomic-agent   █████████████                       18 unique   │
│ FrontierAgent  █████████████                       17 unique   │
│ Hermes         ████████████                        15 unique   │
│ Claude Code    ████████                            10 unique   │
│ OpenCode       ████                                 5 unique   │
│ oh-my-pi       ███                                  3 unique   │
└─────────────────────────────────────────────────────────────────┘

Category Dominance (X count across 28 features)
┌─────────────────────────────────────────────────────────────────┐
│ Meept          ████████████████████████████████████████  22/28  │
│ atomic-agent   ████████████████                          13/28  │
│ prime-agent    ███████████████                           12/28  │
│ duckagent      ██████████████                            11/28  │
│ FrontierAgent  ███████████                               10/28  │
│ Hermes         ████████████████                          12/28  │
│ Claude Code    ██████████                                8/28   │
│ OpenCode       ████                                       4/28  │
│ oh-my-pi       ███                                        3/28  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Key Takeaways

1. **Meept is the only framework that combines all three**: always-on daemon, constitution-bound AI employees, and evidence-based execution. No competitor tries to be all three.

2. **Meept's deepest gaps are in containment and local economics**: OS sandboxing (duckagent/FrontierAgent win), network egress policy (duckagent), and local model stack ownership (atomic-agent). These are architectural choices, not missing features per se.

3. **Meept's breadth advantage comes from the daemon model**: being an always-on personal agent enables scheduling, memory depth, multi-frontend, and autonomous employees. Per-invocation CLIs (FrontierAgent, atomic-agent) can't match this without significant architectural change.

4. **Computer use/browser automation is now shipped but opt-in**: Meept has a headless-Chrome tool family and a cua-driver MCP server with a dedicated security tier; atomic-agent's Playwright suite and Hermes's macOS CUA still lead on default-on depth. This keeps GAIA L3 and WebArena in reach.

5. **Output evaluation evidence is the next milestone**: FrontierAgent and atomic-agent publish benchmark scores. Meept-bench (external harness, live regression gate) plus the in-daemon eval suite close the mechanism gap; published scorecards remain the outstanding step.

6. **The 429 audit found a lead that is real but narrow (2026-10-05).** Meept is the only harness with full RFC 9110 + RFC3339 conformance and a guaranteed non-negative sleep. But the honest reading is that everyone *partially* gets this: all seven rivals read the header somewhere, and every one of them truncates the value somewhere (5s, 10s, 30s, 300s, 600s). The durable difference is that Meept truncates by **horizon** (a 24h give-up, a 1h polling floor) rather than **discarding** the header, so an hour-long `Retry-After` is honored instead of being capped to 5 seconds and re-fired three times. The competitor-side defects are real bugs, not design choices — duckagent's hardcoded 1s and FrontierAgent's negative sleep both burn the window the server asked to protect.

7. **The last month closed gaps rather than opening them (2026-10-05).** Of 11 new-feature clusters, 8 are PARITY and only 2 are NOVEL. The most consequential finding is that **Hermes matches Meept on four clusters** — async turn lifecycle, output filters as a pipeline, agent-writable skills, and config read/write surfaces — where the 2026-08-29 matrix scored Hermes `-`/`~`. Those old cells were produced by reading its README; the source shows otherwise. Any future comparison should be source-only.

8. **Two rows where Meept is behind.** Anomaly detection is the clearest: Meept built two temporal detectors and ships both dark (`enabled: false, log_only: true`) while prime-agent runs one unconditionally. The second is rate-limit truncation depth, where atomic-agent's 5s cap and oh-my-pi's 30s cap are defects but are at least *short* caps — a genuinely long server window is handled worse there than here.

---

*Matrix generated 2026-08-29 from `docs/features.md`, `docs/research/2026-08-24-agent-parity-audit.md`, and direct repo inspection. MCP catalog count from `config/mcp_servers.json5`. Updated 2026-08-29 for ACP client (`acp_agent`, `[acp]` disabled by default). Updated 2026-09-04 after code re-validation: browser automation, computer use, GBNF tool constraints, vision input, secrets broker/credential injection, subscription-CLI-over-ACP, and the meept-bench harness marked X (all opt-in or external by design); crash-recovery and recursion rationales sharpened (queue claims are atomic; parked turns are memory-only by design; production delegation is one-level). Updated 2026-09-05: crash recovery upgraded to X (startup reclaim of crash-orphaned queue jobs + durable parked-turn persistence landed); recursive subagents upgraded to X (request_handoff now wired into BaselineTools; depth-capped recursion machinery still unwired, noted).*

## Update Log

**2026-10-05 — 429/RFC audit + last-month feature audit.** 141 `feat` commits in
820 since the previous content update. Two new sections added, both verified at
pinned refs (see the pinned-ref block in the 429 section):

- **429/Retry-After conformance** — all 8 harnesses audited against RFC 9110
  §10.2.3/§5.6.7/§7.1.3 plus RFC3339 compatibility. Result: Meept is the only
  harness with **full** conformance; Hermes is the only rival at near-full;
  duckagent reads **no** 429 headers at all and hardcodes a 1-second retry;
  FrontierAgent and opencode can both sleep a **negative** duration; opencode and
  oh-my-pi prefix-parse a date into a year-sized delta. Every competitor caps
  the server wait somewhere (5s–600s); Meept caps by horizon (1h floor, 24h
  give-up) instead of truncating the value.
- **New-feature NOVEL vs PARITY** — 11 clusters, **0 clean NOVEL, 2 NOVEL, 1
  WEAKER, 8 PARITY**. This is the material change in the matrix's story: the
  last month closed gaps rather than extending the lead. **Hermes matched
  Meept on 4 clusters** (async turns, output filters, agent-writable skills,
  config surfaces), which the 2026-08-29 matrix scored `-`/`~` because it read
  Hermes' README instead of its source.
- **Corrections to existing cells.** opencode's `retry.fallbackChains` cell
  removed — 0 hits tree-wide, the feature does not exist. MCP client count
  corrected 21 → 22 preconfigured stdio entries.
- **Claims withdrawn by verification.** "Skills as linked asset packages" is
  false: `find config/skills -mindepth 2 -not -name SKILL.md` returns 0 across
  all 15 skill directories. Not added to this matrix.

**How to re-run:** `make compare-prep` clones the seven harnesses; the audit
method is in the `external-repo-source-audit` skill (evidence = file:line plus a
verbatim quote, absence claims must name their enumeration method, defaults must
be reported as defaults). The full evidence reports are archived in-repo at
`docs/research/comparison-audit-2026-10/` — one per harness (`<name>.md`),
the audit protocol (`PROTOCOL.md`), and the two feature-cluster reports
(`cluster-analysis*.md`).

