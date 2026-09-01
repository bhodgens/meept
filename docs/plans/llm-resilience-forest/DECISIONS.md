# DECISIONS — LLM Resilience Forest

Ratified decisions from the 2026-08-31 planning session. Every leaf cites
these by ID. New decisions append here, dated.

| ID | Decision | Source |
|----|----------|--------|
| D1 | Verification-failure model escalation is PER-AGENT (AgentSpec), not per-employee. Employees are agents; the coder agent is not an employee. One field covers both. | user, 2026-08-31 |
| D2 | Escalation trigger = `verification.max_fix_loops` exhausted (same variable, default 3). No separate escalation counter. | user, 2026-08-31 |
| D3 | Escalation target accepts an alias name OR a `provider/model` ref, configured per agent as `escalation_model`. Empty = disabled. | user, 2026-08-31 |
| D4 | 429 "service limit" (provider throttling) and 429/402 "quota exhaustion" are DIFFERENT classes with different horizons, unified under ONE failure-policy handler with ONE behavior. All five client retry loops delegate to it. | user, 2026-08-31 |
| D5 | 402 is treated like 429 (quota class) but starts with a retry timer longer by minutes than the equivalent 429 path. | user, 2026-08-31 |
| D6 | Retry-After parsing must fully implement RFC3339 (as delta-seconds RFC uses) and RFC7231 (HTTP-date: IMF-fixdate, RFC850, asctime). | user, 2026-08-31 |
| D7 | 429/402 responses whose headers/content match quota-indicating words ("quota", "usage", etc.) bucket as FailureQuota; WITHOUT such signals a 429 is FailureThrottle so spurious provider-load 429s never inherit quota-length delays. | user, 2026-08-31 |
| D8 | Long-horizon backoff: exponential growth → 1-hour polling floor → 24h cap (configurable). On cap: turn fails with a clear user-facing error; the queue/goal-loop then applies its own retry policy. | user, 2026-08-31 |
| D9 | Park-and-resume applies to ALL turn types (chat, goal-loop, specialist agents, queue jobs). Meept is an electric machine: on (working) or off (broken). No turn may hang on a dead connection. Parked turns release their agent/model slot. | user, 2026-08-31 |
| D10 | Timeout/cooldown granularity is the BASE MODEL ENDPOINT: openai/model-1 (medium) timing out also skips openai/model-2 (thinkhard) — same endpoint, shared fate. Alias-level timeouts apply only when the alias config declares `timeout:` explicitly, and only on CONSISTENT member-model failure. Documented in plan docs. | user, 2026-08-31 |
| D11 | "Interactive" = recent user message on the originating session OR the session's foreground-session flag. (WS presence was not ratified.) | user, 2026-08-31 |
| D12 | LM Studio ships as a FIRST-CLASS provider (same treatment as Ollama: registry entry, discovery, catalog). | user, 2026-08-31 |
| D13 | Context-length discovery: sync from provider endpoints where exposed (Ollama /api/show, llama.cpp /props, LM Studio /v1/models, OpenRouter models endpoint); catalog values remain the fallback. OpenAI/Anthropic do not expose it — keep catalog defaults. | user + recon, 2026-08-31 |
| D14 | No roster agent AGENT.md sets `model:` today. Escalation is config-surface only; a defaults block MAY provide a fallback escalation target, but per-agent `escalation_model` overrides it. Proper config surface + docs required either way. | user + recon, 2026-08-31 |
| D15 | Adaptive pacing (original item 3): when a provider emits spurious 429s without Retry-After, meept learns its effective ceiling from the rate-limit metrics store and paces outbound requests below it. Gated on a config flag (default off). | user, 2026-08-31 |
| D16 | AUDIT RESULT (2026-09-01): AGENT.md frontmatter is parsed by `internal/agents` (AgentMetadata, yaml) and converted to `AgentSpec` by `internal/agent/registry.go` (`definitionToSpec` + `mergeSpec`). New roster keys are added in BOTH packages and wired at BOTH conversion sites — the sanctioned two-half pattern. Tree 01 leaf 01 implements accordingly; the mergeSpec Verification/Gap omission is a known pre-existing gap the leaf must consciously mirror or document. | subagent audit vs HEAD 90fd2afb |

## Open questions (do not block leaf 01 of any tree)

| ID | Question | Default if unanswered |
|----|----------|----------------------|
| Q1 | Interactive window length (how recent is "recent user message")? | 5 minutes |
| Q2 | Does escalation reset `fixCount` after switching models, so the escalated model also gets max_fix_loops attempts? | yes, reset |
| Q3 | Should goal-loop assessment/reflect calls (internal/employee/goal_loop.go:633,1049) escalate too, or only verification-hook failures? | verification-hook only in tree 01; goal loops follow in 03 |
| Q4 | Keyword list for D7 quota detection: exact tokens and where to scan (header names only vs header names + body)? | headers with quota/usage/limit tokens in the NAME + structured body codes; list frozen in leaf 01 of tree 02 |

---

Do NOT commit this file independently — the forest orchestrator commits
it with the tree dispatches. Deviations from the ratified defaults are
recorded per-tree in the executing orchestrator's tracking notes; a
deviation that changes a frozen contract (SHARED-CONVENTIONS §4)
requires a new dated decision row here BEFORE the consuming leaf runs.

## Self-Verification Checklist (forest-level)

- [ ] Every tree's master references the decision IDs it implements
- [ ] No leaf contradicts a ratified decision (spot-check during review)
- [ ] Q1-Q4 outcomes recorded here after each tree completes
