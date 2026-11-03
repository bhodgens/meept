# Audit: github.com/sausheong/harness vs meept

Date: 2026-11-01. Clone: /tmp/harness-sausheong @ 9d338dd (v0.6.3, last commit 2026-10-27).
License: MIT. Scale: ~25,237 LOC non-test Go, 321 files, 161 test files. Go 1.26.

Detail notes (source-level, file:line cited): /tmp/harness-notes-A.md (runtime+llm),
/tmp/harness-notes-B.md (session/compaction/budget), /tmp/harness-notes-C.md
(tools/execution/web/browser), /tmp/meept-baseline.md (meept inventory).

## What harness is

A library-only Go agent substrate: streaming think-act loop, tool registry, JSONL
session DAG, summarize-and-splice compaction, durable token/cost/time budget
ledgers, provider adapters (anthropic/openai/gemini + openrouter/local/litellm as
thin OpenAI-compatible wrappers), Docker/Host execution backends, browser + NL
browser (Stagehand) + web tools, MCP, skills/memory/cron tool interfaces.
No binary, no server, no multi-user, no scheduler subsystem. Explicitly not a
graph orchestrator, multi-role crew, or RAG platform (README "When Harness is
the wrong fit"). Single tenant, one LLMProvider per Runtime.

## Axis-by-axis verdicts

### LLM resilience: meept superior, decisively
- harness: one provider per Runtime + same-provider FallbackModel. Retries are
  exactly single-shot recoveries: 1 overflow-compaction retry, 1 fallback-model
  swap, 1 refusal retry, 1 empty-response retry, 1 pre-token stream recovery,
  1 Anthropic-only non-streaming fallback. ZERO backoff/sleep/retry-after
  parsing (grep-verified, llm/retry.go is a classification predicate only).
  No quota-block state, no endpoint cooldowns, no model aliasing, no session
  affinity headers (ChatRequest.SessionID "routing metadata only, not sent").
- meept: ~16 canonical providers, capability-based alias resolution, short-retry
  + 429 pacing, QuotaResetError durable parking + resume watcher, endpoint
  cooldowns (host+cred key), sticky rotation + session_affinity headers
  default-ON, KV teardown, token cache L1+L2, GBNF constraint, local runtime
  manager + hang watchdog.
- harness leads on request hygiene: prompt-cache-stable request building
  (sorted tool defs, cached static prefix + uncached dynamic suffix), per-model
  context-window family table + persisted per-session token-ratio calibrator,
  rich Anthropic reasoning mapping (adaptive/between_tools/disabled per model
  family), refusal stop-reason + category surfaced, usage ledger discipline
  (nil usage = "unknown, possibly billable", never zero; per-attempt
  RequestUsage records; cache tokens folded into InputTokens).

### Compaction: comparable core; harness leads on robustness detail
- Both: summarize-and-splice, cut on user-turn boundaries, default-ON style
  wiring (harness only when a Manager is passed; meept firewall default ON).
- harness-only: context pins (<=16 objective/constraint) + context state
  (<=32 items, optimistic revision CAS) as durable session annotations that
  survive compaction; post-compact re-inject of last <=5 touched files x 5KB;
  truncated summary is REFUSED and never spliced; 3-strike circuit breaker;
  async pre-kickoff at 0.8x threshold + 8s join at turn start; anti-drift
  9-section prompt (all user messages verbatim, untrusted tool-result
  delimiters with per-call random suffix); oversized tool results spilled to
  <ws>/.harness/spill/<key>/<id>.txt with a read_file pointer message.
- meept-only: thread partitioning for long sessions, 3-layer firewall
  (60%/70%/80% stages), per-session working-dir binding.
- No context-pins/state equivalent on the meept side: state survives only via
  compaction summary + threads (verified by grep, internal/llm/context_firewall.go).

### Sessions / persistence: class split, both keep their store
- meept SQLite: wins on query (FTS5, relational, idle-reaper wiring, owner
  scoping, multi-user path).
- harness JSONL DAG: wins on crash-recovery discipline (LoadRecovering:
  exclusive writer lease + full synced original backup + byte-exact retained
  prefix; process-death tested), durable branch selection records, durable CAS
  annotations (AnnotateIfCount), content-addressed attachment store (0700 dir,
  sha256-verified reads, 32MiB blob / 1GiB quota), versioned format +
  migration tooling. Portable/inspectable/exportable; zero index.
- Note: harness session ID generation falls back to time.Now().UnixNano()
  (session/session.go:532-538) - violates the predictable-ID policy meept
  enforces via pkg/id.

### Budget / cost admission: harness superior
- harness: durable token/cost/deadline ledgers journaled as session
  annotations; pre-dispatch Admission reserves inputEstimate+MaxTokens,
  settles post-hoc (reported/unknown/not_dispatched); run-scope ledgers fail
  closed; PriceSnapshot with AllChargesBounded + big.Int math; "token
  admission, not financial billing".
- meept: sliding-window counter (hourly/daily tokens/USD/RPM, per-task/session
  scope) checked pre-call, parks on exceed - default OFF, no per-run
  admission ledger, no durable journal, no "unknown usage" discipline.
  (internal/llm/budget.go; issue #16 tracks full enablement.)

### Tools / skills / memory: meept far richer
- meept: ~91 builtins, 22 preconfigured MCP servers (8 enabled), ACP client
  (9 agents), scheduler+queue+worker with in-agent cron tools, 15 bundled
  skills + evolver, episodic FTS5 + claims + KG + L0/L1/L2 distillation.
- harness: focused concrete tool set (file/bash/web/browser/stagehand/todo/
  mcp + skills/memory/cron interface tools). No episodic/FTS/semantic memory
  in the state layer (BYO JSONL facts store + optional KG plug point).
- harness patterns worth stealing: (a) RefreshSkills - re-render the
  system-prompt skill index while idle so mid-session skill authoring is
  visible next turn; (b) single action-enum tool per domain (skill_manage /
  memory with save|update|remove|list|get) to cut schema bloat and protect
  prompt-cache hits - meept ships ~15 memory tools; (c) skill writes
  tagged origin ("agent"|"review"|"user").

### Execution / sandboxing: meept broader; harness leads on hardening detail
- meept: goja codemode C3 VM (core shipped, agent tool not yet wired),
  Docker + bwrap backends (default local, require_sandbox fail-closed),
  security engine + taint + PII + prompt guard, SSRF guard default ON
  (web_fetch/search + browser manager, dial-time re-check), secrets egress
  proxy (opt-in), pty, effects ledger, audit chain + off-host anchors.
- harness: Host/Container backends with container hardening: immutable
  sha256 image pin + --pull=never, --cap-drop=ALL --no-new-privileges
  --pids-limit=128 --memory=512m, image VOLUME declarations rejected,
  worker binary snapshot-copied + sha256-verified (not mounted), fail-closed
  with no host fallback. Browser SSRF containment: in-process loopback
  EgressProxy that Chrome is forced through (sub-resources, in-page
  fetch/XHR, WebSockets; WebRTC UDP disabled; one exception for Chrome's
  debug port), validated-IP pinning at dial (DNS-rebinding closed), redirect
  re-validation; Stagehand NL browser routing its inference through the
  agent's own provider; Exa search tool; bounded output artifacts with
  sha256 re-verification on read (8MiB/file, 64MiB store, 24h retention).

### Subagents / steering / review: meept deeper; harness sharper contracts
- subagents: meept delegate_task + ReportRouter cap 5 + RLM analyzer
  (max-parallel 4, depth 2) vs harness task tool, depth cap 3, in-memory
  child sessions, text-only result return.
- steering: meept employee steer (parked/running episodes) + chat.steer bus +
  /amend vs harness WithSteering at joined tool boundaries (forces serial
  execution, pending calls get explicit "skipped" results, stable-ID user
  entry, image SHA-256 digest verification against forged-JSONL replay).
- review: meept 6-hook registry + verification escalation + plan critics vs
  harness post-hoc Review curator (16 turns/60s, shared memory+skill stores
  with origin tagging, __review__ recursion guard) - an analog of meept's
  opt-in evolver.
- human approval lane: meept has the ACP human permission relay; harness has
  NONE (synchronous in-process PermissionChecker, allow/deny only).

### Surfaces: meept exclusive
TUI + Flutter GUI + menubar + Unix RPC/HTTP/WS/SSE + ~509 bus topics + hermes
gateway + employees/goals + bots + audit chain: harness has none of these
by design (library).

## Replacement verdict (can harness-as-library replace meept capabilities?)

NO, on every shipped axis.
- LLM: replacing internal/llm with harness providers loses alias resolution,
  quota-block parking, endpoint cooldowns, session affinity + KV teardown,
  429 pacing, token cache, GBNF, local runtime supervision. None exist in
  harness.
- Sessions: replacing SQLite with JSONL loses FTS5/relational query,
  idle-reaper wiring, owner/multi-user scoping.
- Tools/UI/employees/scheduler/security: harness covers ~0% of that surface.
- The product classes differ: harness = embeddable single-tenant substrate;
  meept = persistent multi-surface daemon. harness is not a competitor; it
  is a well-built ~40% slice of meept's LLM/session/compaction stack with
  sharp patterns inside.

## Ranked adoption candidates (value per effort)

1. **Per-run budget admission** - port the harness budget/ discipline
   (pre-dispatch reserve + post-hoc settle, unknown-never-zero, durable
   journal) onto internal/llm/budget.go + agent parking; closes issue #16's
   "admission" gap. Effort: medium-high (new ledger + wiring into
   CheckBudgetWithScope callers: anthropic.go:419/686, handler.go:1029,
   budget_resume.go).
2. **Browser SSRF egress proxy** - port tools/web/egress_proxy.go (~600 LOC +
   tests): loopback proxy Chrome is forced through, covering in-page
   fetch/XHR/WS/WebRTC; fold into internal/browser/manager.go + obscura MCP
   config. Meept's SSRF guard (dial-time re-check) does not cover
   sub-resource and WebRTC paths. Effort: medium.
3. **Compaction robustness details** - context pins + context state as
   durable annotations that survive compaction; refuse-to-splice truncated
   summaries; 3-strike circuit breaker; post-compact restore of last N
   touched files (verify whether HALO/decision_compactor already does
   file-op restore before building). Host: internal/llm/context_compactor.go
   + internal/session store (annotation = new table, SQLite equivalent of
   harness AnnotateIfCount). Effort: medium.
4. **Container hardening list** - immutable sha256 image pin + --pull=never +
   cap-drop=ALL + pids-limit + image-VOLUME rejection + snapshot-verified
   worker copy into internal/runtime/docker.go. Effort: low-medium.
5. **Skills refresh + origin tags** - re-render skill index while idle
   (runtime/skills_refresh.go pattern); origin tagging on skill/memory
   writes (agent|review|user). Host: internal/skills + evolver. Effort: low.
6. **Usage accounting discipline** - RequestUsage per-attempt records with
   reported/unavailable source, cache tokens folded into InputTokens
   (harness USAGE.md contract); audit whether internal/llm token accounting
   matches. Effort: low (analysis first).

NOT worth adopting: JSONL session layer, subagent primitives, provider
adapters, harness's cron interface (meept ships real cron tools).
