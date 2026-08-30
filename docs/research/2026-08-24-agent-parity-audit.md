# Agent Platform Parity Audit — meept vs. FrontierAgent / duckagent / atomic-agent / prime-agent

Date: 2026-08-24
Method: four parallel subagent audits of the live GitHub repos (READMEs, LICENSE files, package manifests, source trees, release pages via web extract) cross-checked against a local read of meept (`AGENTS.md`, `docs/features.md` end-to-end, `docs/workflows/employees.md`, `internal/` tree, `LICENSE`). Repo liveness signals taken from GitHub metadata at audit time. Every claim below traces to one of those sources; nothing is recalled from prior sessions.

meept baseline: Go daemon + CLI/TUI + Flutter GUI (web+desktop) + macOS MenuBar + Telegram. Multi-agent specialists, AI Employees (constitution/goal-loop/enforcement), persistent bot framework, 6-tier memory incl. knowledge graph and epistemic claims, evidence pipeline + validation gates, taint tracking + tirith + fence security, context firewall, shadow-training/skill-evolution self-improvement loops, P2P cluster mesh, MCP client AND server. License: MIT (c) 2026 Benjamin Hodgens.

---

## 1. Identity Matrix

| | meept | FrontierAgent | duckagent | atomic-agent | prime-agent |
|---|---|---|---|---|---|
| What it is | Personal-agent daemon + employees + memory platform | Research/file-work agent runtime + Textual TUI + benchmark suite | Sandbox-first personal agent runtime, Rust single binary | Desktop operator agent (browser+fs+shell) for small local models, TS/Node | Self-improving coding/research agent harness (Claude-Code-class CLI), TS host + Python RLM kernel |
| Category fit vs. meept | — | Partial overlap (no daemon/memory/channels) | Closest shape match | Same niche competitor | Poor 1:1 fit (dev tool); excellent idea donor |
| Language | Go (+Flutter/Dart UI) | Python 3.12 | Rust | TypeScript (Node ≥25.7) | TypeScript + Python |
| License | MIT | Apache-2.0 | Apache-2.0 (vendored bubblewrap subtree = LGPL-2.1+) | MIT | MIT (fork of Zechner `pi`) |
| Age / maturity | personal project, mature internal QA | 2 days old at audit, v0.1.0 unreleased, corporate-backed (Apodex), 44★ | 2.5 months old, v0.1.3, 17 commits, **dormant ~10 weeks**, 7★ | 4 months old, v0.1.72, weekly releases, ~2.5k★ | 3.5 months old, v0.8.0, near-daily betas, pre-1.0 churn, 18k★ |
| Bus factor risk | solo author | low (corporate) | high (single account, quiet) | medium (one dominant maintainer + community) | high concentration (badlogic ≈3,071 contributions vs 327 next) |

Key category corrections found during audit:

- prime-agent is NOT an RL/training harness and NOT an assistant daemon. It is a terminal coding agent built on a persistent IPython kernel ("Recursive Language Model"). PrimeIntellect's training stack is separate.
- duckagent is not just "JSON policy" — it has real OS-enforced sandbox backends (vendored bubblewrap, Windows ACL/WFP/DPAPI).
- FrontierAgent's default interactive mode runs commands unsandboxed by its own admission; its strong isolation claims hold only for bwrap/container/E2B paths.
- atomic-agent ships telemetry (PostHog + Sentry) ON by default despite "private by default" marketing.

---

## 2. Feature Audits (per repo)

### 2.1 FrontierAgent (ApodexAI)

**Loop/autonomy:** domain-neutral ReAct kernel; PipelineSpec DAG workflow engine driven by YAML profiles; formal Observer contract (9 callbacks returning Intervention objects that stop/retry/replace content without burning turn budget; authorization observers fail closed); runaway watchdogs (reasoning-only timeout/tokens, logical-call timeout); loop guards — RepetitionGuard, DuplicateQueryRollbackObserver (pops turn before duplicate web_search, re-samples free), NoProgressGuard, wall-clock/context guards; per-turn checkpoints + `--resume`; async steering queued to next safe turn boundary; `--plan` mode locks edits until approved.

**Memory/learning:** none persistent. In-loop compaction only. Skills loader wired but zero bundled skills. No episodic store, no consolidation, no self-improvement.

**Tools:** explicit allowlist registry; bash (policy-filtered), file ops, glob/grep, run_python_code, view_image; web search/fetch with academic routing, bounded fetch, scrape cache, single-flight dedup, eval-aligned deterministic variants; PDF/DOCX/PPTX/XLSX readers AND writers; deliverable policy; cgroup-bounded exec; net guard. **No MCP anywhere.**

**Channels/UI:** terminal only. Polished Textual TUI (task board, live tool activity, deliverables, session diff tabs; WCAG-tested themes asserted byte-for-byte in tests). No chat platforms, no GUI, no resident daemon/API.

**Security:** task-scoped filesystem (/inputs ro, /workspace scratch, /outputs manifest-published); sandbox backends auto/bwrap/container/E2B cloud, fail-closed network+path policies; interactive approval gate with **unified diff preview before every mutation**; hard denials survive `--yes`; **WorkspaceJournal snapshots content before every write/edit/delete → `/revert` undoes whole session**; JSONL trace including refused actions. Caveat: Linux native mode = "not an OS sandbox," announced every run.

**Multi-agent:** Agent Team — coordinator with persistent task board (pending/active/completed/blocked), AgentBus dispatch, SpawnGuard bounding depth/parallelism/wall-time/task budgets, structured sub-agent reports, fan-in observers, optional fast-reporter final review (fail-open).

**Observability/deploy:** per-run artifact dir (session.json checkpoint, trace.jsonl, engine.log, trajectories); uv install w/ extras; GHCR images; Docker re-exec on macOS; local SGLang serving stack with pinned driver/CUDA compat matrix and 35B templates. Unique: benchmark harness with 14 supported benchmarks + own FrontierSearchBench + published scorecards; resumable multi-run experiments; rerun-failures-only.

### 2.2 duckagent (selfonomy)

**Loop/autonomy:** single shared Agent Loop behind TUI + all gateway channels + API server + cron firings. Persistent goals (`/goal` pause/resume/clear, native goal tools, auto-continuation until complete/blocked). Cron as agent capabilities with execution policies: overlap skip/parallel, missed_run run_once/skip, timeouts; storage = append-only JSONL with tombstone deletes + revision-based optimistic concurrency. Sessions append-only JSONL, never rewritten; `/rewind N` restores tracked file edits **only when current checksum matches recorded post-change hash**. Context projection policy (`guarded_mid`) compresses completed tool history into recoverable summaries carrying exact re-fetch handles (path/offset/pid/query); stable system prompt first (prompt-cache friendly); offline benchmark harness claiming ~108M raw tokens saved across 1188 simulated turns (self-reported simulator numbers).

**Memory:** profile-scoped JSONL lists; MainAgent may only request memory review — a privileged MemoryAgent owns get/add/patch/forget. Portable identity per profile: model config, credentials, skills, SOUL.md persona, USER.md user-model, avatar; SillyTavern PNG character-card import (ccv3/chara chunks). No embeddings/graph/consolidation — deliberately simple.

**Tools/MCP:** 18 capability modules (fs, shell/process, web_search defaulting to Exa MCP so fresh installs work, web_extract, vision_analyze, sandbox introspection, load_mcp). Claude-style SKILL.md skills. MCP client: stdio + streamable HTTP + OAuth, per-server explicit env grants, glob-matched tool permissions.

**Channels:** 37 channel source files — Slack Socket Mode, Discord, Mattermost, Teams, Google Chat, IRC, Telegram, Signal, WhatsApp Cloud, Line, Twilio SMS, Weixin/QQBot/Yuanbao, Feishu/Lark, DingTalk, WeCom, BlueBubbles iMessage, email IMAP+SMTP, Home Assistant, generic webhook, websocket, plus OpenAI-compatible api_server. Gateway access control: per-channel allowed_users/allowed_chats; DM modes open|allowlist|pairing|disabled with **one-time pairing codes**; group mention gating. **Cross-channel approvals**: native buttons/cards where available, `/approve` text fallback; pending approvals auto-denied if user sends new message instead.

**Security (center of gravity):** declarative JSON policy, three presets + `extends` inheritance. Filesystem mounts (*→ro, cwd→rw, $TMPDIR→rw) + precedence rules hiding secret globs (.env*, *.pem, id_rsa → none). Network modes proxy|deny|allow with host map + CIDR map (RFC1918/CGNAT→ask; link-local/0.0.0.0→deny); managed egress proxy evaluates hostname AND every resolved IP; sets proxy env vars incl. npm/yarn-specific; clears NO_PROXY-family vars so children can't bypass; chains upstream corporate proxies while enforcing policy first. Env allow/ask/deny globs. **Secret-backed requests**: env var typed `secret` with injection spec → child gets placeholder + rewritten localhost URL; managed reverse proxy injects real credential header upstream; plaintext never enters child env. Shell command-prefix permission table (rm -rf deny; sudo/git push/dd ask). Tool permissions incl. MCP globs. OS backends: vendored bubblewrap (Linux), macOS backend, deep Windows backend (ACLs, SIDs, dedicated sandbox users, DPAPI, WFP firewall filters, guided elevated setup). Agents can never touch ~/.duckagent itself.

**Multi-agent:** none beyond Main + MemoryAgent roles.

**Deploy:** single static binary, checksummed installers (macOS/Linux/Windows/WSL2), CI incl. localhost-proxy sandbox smoke test, Astro/Starlight docs site.

### 2.3 atomic-agent (AtomicBot)

**Loop/economics:** one inference → GBNF grammar-constrained JSON array of 1..16 tool calls (array-only root defeats sampler first-token bias); resource-class executor (pure_read fans out parallel, browser serializes, mutations serialize; wall time = max(group)); **byte-stable prompt prefix + bounded mutable tail** ordered loaded-skills→loaded-tools→profile→memory-index→session-facts→recalled→world→conversation→notice→respond so llama.cpp KV-cache reuse holds every step (target ≤~2.5k tok/step); lazy tool-schema loading via `tool.view {name}` (LRU, token-budgeted); reasoning-model profiles with dual-channel reasoning capture; durable deferred turns, cron/intervals/webhooks/reminders with Telegram result reporting; no-progress guard warn@3/hard-veto@5/forced graceful reply after 3 vetoes; FIFO TurnController per session. Explicitly no autonomous goal mode.

**Memory (strongest of the four):** SQLite+FTS5 notes + optional embeddings (hybrid recall); versioned profile facts with queryable history; bounded memory-link graph; lessons (distilled principles) + procedures (how-to templates); **voting drives useful/harmful memories up/down; usefulness-based dedup + eviction**; off-slot post-turn reflection. In-repo eval harness E1–E12, LoCoMo, LongMemEval runners.

**Tools/browser:** playwright-core browser family (navigate/click/type/tabs/scroll; compact ARIA snapshots under char budget); pluggable web search (Exa/DDG/Brave/SearXNG); SSRF-guarded fetch (shared host checks, redirect hops re-validated); fs read/write/edit/patch/glob/grep/diff/watch/hash/archive; approved shell; clipboard/notifications/window focus; local doc extraction (PDF/DOCX/XLSX/PPTX/ODT/RTF); read-only git; vision.describe for mmproj models. Skills = Markdown playbooks + approval-gated scripts; 17 seeded; ClawHub marketplace installs. MCP client with per-server trust level (trusted pure_read servers batch with reads; untrusted approval-gated).

**Providers/local inference:** managed TurboQuant llama.cpp fork (KV-cache quant ~6.4×, Lloyd-Max weights, speculative decoding +30–50%); external llama-server/Ollama/LM Studio presets; OpenAI-compatible/OpenRouter/Gemini catalogs with mid-session switching; **subscription-CLI providers driving signed-in `claude --print` or `codex exec --json -s read-only` headlessly, no API key**.

**Channels/UI:** CLI, Ink TUI (mouse, 12 themes, approval keys y/n/s/**e**=redirect-write-to-another-path-with-ladder-recheck, free-text denial that folds guidance back into the running turn), Telegram bot (owner pairing, inline approval buttons, throttled progress bubble), OpenAI-compatible HTTP server mapping one request → one macro-turn, Tauri sidecar (NDJSON stdio, correlation IDs).

**Security:** category-based approval ladder; config.json/.env writes refused; browser scheme gating shared across navigate/tabs; honest egress disclosure; key-validation probe before save. Weaknesses: telemetry ON by default; .env inherited by spawned skills/shell ("not complete isolation", their words); traces store prompts/completions unredacted as an explicit non-goal.

**Observability/deploy:** append-only NDJSON traces (salted stable-prefix hash + verbatim tail, both reasoning channels, monotonic seq, 10MiB cap); `trace replay` compares current prefix hash vs recorded for prompt-drift postmortems; unified failure taxonomy surfaced everywhere; public GAIA L1 artifacts (self-run vs Hermes claim: 69.8% vs 58.5%); checksummed installers + self-updater + Node SEA binaries + npm + Tauri embed.

### 2.4 prime-agent (PrimeIntellect)

**Runtime:** one built-in tool — a persistent IPython kernel ("prompt-as-a-variable"); files, %%bash cells, transforms, subagents all happen as code; Python state survives turns AND compaction; TS host owns providers/queues/transcripts/child lifecycles via typed host requests. Compaction hardened (mid-goal resumption, failed-compaction tool-loop recovery). AGENTS.md/CLAUDE.md walk-up from cwd.

**Subagents:** `rlm(...)` spawns real child AgentSessions from inside the kernel; returns admission handles; results via agent_message or files; parent-scoped child registry survives compaction/kernel restarts; depth caps; **idle passivation** (children evicted to disk after ~90 min); daemon-owned append-only spawn ledger. **Direct agent-to-agent messaging**: `prime-agent send`, delivery modes auto/steer/follow_up, delivered/queued receipts, roster broadcast, size/rate/pending limits.

**Self-improvement ("Continual Harness"):** `/refine` reviews trajectory and applies small evidence-backed updates to supplemental prompts/memories/skill descriptions/subagent specs — session-local by default, never touching immutable base system prompt, with history, snapshots/rollback, recorded `[refinement]` diffs, swappable refiner extension hook. No standalone long-term KB.

**Long-running (its strongest area):** daemon supervisor + per-root session worker processes surviving terminal close; detach/reattach; crash recovery rehydrates sessions/schedules/children. Three scheduling surfaces: user /heartbeat, agent-created programmatic heartbeats (multiple), natural-language cron ("in 30m"); **crash-safe tick claiming before delivery; missed ticks coalesced, not accumulated**. `/goal` with token budgets and continuation counts; only goal.complete() marks success. **Bounded autonomous mode**: continuations until quality gates pass or turn/token/wall-clock limits hit; `--autonomous-gate "npm run check"`; skips re-running failed gates when workspace unchanged.

**Skills/MCP:** Agent Skills standard (agentskills.io) markdown skills + Python-backed superset installed editable into kernel venv as typed async callables; progressive disclosure; reads ~/.claude/skills and ~/.codex/skills for interop; skill-creator; MCP client (streamable HTTP + stdio, endpoint-bound OAuth tokens after replay-surface hardening).

**UI/security/ops:** Rich TUI with steer-vs-follow-up queue browse/edit/reorder, /tree session branching, /fork, /clone, /btw side conversations; headless --print/--mode json/rpc/acp. Security: **explicitly "lifecycle containment, not a security sandbox"** — model Python runs with full user permissions; use external sandboxes. Ops: orphan-process journaling (parent-death poll, pid-reuse fixes), doctor --fix, signed builds, changelog-fragment CI, opt-in trace upload, extension/package system.

### 2.5 meept (audited locally — condensed baseline)

Resident daemon (bus, RPC/HTTP/WS/SSE, SQLite backbone); multi-agent specialists (dispatcher/chat/coder/debugger/planner/analyst/committer/scheduler + writer/architect/skeptic/librarian) with DAG planning, amendments, interrupts, async handoff; agentic pairs + CollaborationEngine (pair programming, differential A/B); AI Employees = bots + constitutions + tier-aware GoalLoops + enforcement engine (pre-exec/post-turn/periodic checks, auto-pause) + authority escalation through plan approvals; plan.md lifecycle approve/reject/confirm; persistent bot framework (cron/bus-event/webhook triggers, memory isolation private/shared/read_only, cost caps, auto-pause); 6-tier memory (episodic FTS5/BM25, task domains, knowledge graph PageRank+communities+5 relation types, semantic vectors sqlite-vec HNSW + Matryoshka slicing + hybrid search, distributed memvid hydrate/distill, personality) + epistemic claims (claim/decision/prediction/question, trust statuses, contradiction edges, two-phase destructive confirmations); evidence pipeline (file_hash/process_exit/api_response/db_row) with claim↔evidence matching validator, validation gates + retry loops, git checkpoints; hallucination detection; taint lattice + sinks, input sanitizer, tirith shell scan, adversarial boundary markers; context firewall (3-layer compaction w/ LLM summarization); session persistence + tree branching/forking; steering + follow-up queues with intent classification; token budgets (session/turn/conversation hierarchy), USD cost tracking w/ live OpenRouter pricing sync, L1+L2 token cache; model failover + capability-based resolution + routing-decision logging; self-improvement loops (skill evolution closed loop w/ LLM-judge verifier + versioning, shadow LoRA/DPO training w/ eval gate + hot-swap, reflection collector, Q-Agent meta-analysis); code intelligence (tree-sitter AST tools, LSP client tools, RepoMap PageRank, auto-lint reflection); MCP client (21 preconfigured) AND MCP server mode; scheduler with human-friendly cron; STT (whisper/parakeet/native) + TTS; Google Calendar; projects manager; Flutter GUI + MenuBar + Telegram + TUI parity mandate; P2P cluster mesh (gossip, WireGuard sync, distributed task queue); analytics DB + generated connectivity graphs + 4 custom static analyzers; desktop notifications.

---

## 3. Consolidated Gap Analysis — what they do that meept does NOT

Ranked roughly by how much meept would benefit. (M = meept gap severity.)

1. **OS-enforced sandboxing** [M: MEDIUM-HIGH] — duckagent (bubblewrap/macOS/Windows ACL+WFP+DPAPI, agents barred from their own config dir) and FrontierAgent (bwrap/container/E2B, cgroup exec limits, fail-closed policies). CORRECTED after source review: meept is NOT purely advisory — `internal/runtime/` provides an ExecutionBackend abstraction (local + docker) wired into ShellExecuteTool via `[runtime]` config (image, volume binds, timeouts, auto-cleanup), plus FenceChecker path fencing and a SecretScanner on shell output. Remaining true gaps: the docker backend is opt-in and DEGRADES TO UNSANDBOXED LOCAL EXEC on manager failure (components.go:1664 warns and continues); the default local backend is plain exec with no OS confinement; no lightweight OS-native option between "bare exec" and "full docker container"; no fail-closed posture. The gap is narrower than first reported but real: one misconfiguration or docker outage silently removes containment.
2. **Network egress policy** [HIGH] — duckagent's managed egress proxy: host+CIDR maps, resolved-IP double-check, NO_PROXY scrubbing, npm/yarn coverage, upstream chaining. Meept has zero network-layer control.
3. **Secret-backed credential injection** [HIGH] — duckagent's reverse-proxy placeholder pattern keeps plaintext API keys out of every child process. CORRECTED after source review: meept's LLM provider keys are correctly config/env-referenced (`api_key_env`) and never passed to agent tooling. The confirmed leak is the shell execution path: `internal/runtime/local.go` `buildEnv()` seeds every spawned command with the daemon's FULL `os.Environ()` (local.go:99) and merges command-specific vars on top; docker.go passes only explicit cmd.Env but local is the default backend. So any env-stored secret in the daemon's environment (ANTHROPIC_API_KEY if exported, cloud credentials, tokens) reaches every shell the agent runs, and tirith/SecretScanner only catch secrets in OUTPUT, not exposure via inheritance. Fix shape: env allowlist on buildEnv + optional placeholder-injection proxy (duckagent pattern).
4. **Write-approval diff preview + reversible journal** [HIGH] — FrontierAgent: unified diff before every mutation, WorkspaceJournal snapshot-before-every-change, /revert whole session, hard denials surviving --yes. duckagent: checksum-guarded rewind restoring file edits, refusing when hashes drift. CORRECTED after source review: meept already has the core of this — `internal/tools/builtin/pending_changes.go` + `resolve` tool implement a PendingChangesRegistry where FileEditTool stages edits as PendingChange{Original, Modified, Diff (unified diff preview), ExpiresAt} and an agent/user resolves accept/reject per change or batch, with fence re-validation at write time. What's missing vs FrontierAgent/duckagent: (a) coverage is limited to FileEditTool — WriteFile/shell mutations bypass it; (b) no user-facing diff surface found in TUI/GUI/HTTP (the registry is agent-resolved via the `resolve` tool; grep of internal/comm + internal/rpc shows no pending-changes endpoints); (c) no post-apply undo journal (once accepted, only git checkpoints recover); (d) no checksum-guarded rewind restoring files. Borrow = extend the existing registry, not build a new one.
5. **Desktop/browser computer-use** [HIGH] — CORRECTED: meept has no browser tooling (verified), but there IS a standard implementation to adopt rather than reimplement: **cua-driver (trycua/cua, MIT, ~22k stars)** — the same driver Hermes uses. It speaks MCP over stdio (`cua-driver mcp`) and a CLI (`cua-driver call`), drives native apps in the background on macOS/Windows/Linux without stealing focus, does SOM element overlays for reliable clicking, and has `hermes computer-use doctor`-style health checks. Meept already runs an MCP client with 21 preconfigured servers — wiring cua-driver is configuration + one bundled skill/prompt guide, not a port. Hermes' own integration (~5k lines in tools/computer_use) is MIT and referenceable for schema design (single consolidated tool with action discriminator, capture/click/type/hotkey/scroll/drag/set_value/wait), approval gating (all non-capture actions require approval), vision routing (native-vision vs auxiliary model), and per-app window targeting. Browser automation remains a separate gap (chromedp or playwright via MCP browser servers).
6. **Local inference ownership** [HIGH] — atomic-agent manages the full stack: backend download, GPU/driver detection, KV-cache quantization, warm-daemon lifecycle; subscription-CLI providers (drive logged-in claude/codex subscriptions headlessly, no API key); GBNF grammar-constrained tool calling; stable-prefix prompt layout engineered for llama.cpp slot reuse. Meept assumes external OpenAI-compatible endpoints (it has runtime lifecycle mgmt for llama.cpp/MLX, but no model management or loop economics).
7. **Benchmark/eval discipline with published results** [MED-HIGH] — FrontierAgent (14 benchmarks, judges, scorecards, rerun-failures-only), atomic-agent (public GAIA artifacts, LoCoMo/LongMemEval memory evals, trace-replay prompt-drift postmortems), duckagent (offline context-policy simulator). Meept has internal/eval + internal/benchmark packages but no public harness, judge infra, or outcome-level evidence. → Being addressed: benchmark repo scaffolded 2026-08-24, see section 9.
8. **Broad chat-channel surface** [MED] — duckagent: 30+ channels w/ per-channel allowlists, pairing codes, mention gating, native-button approvals routed to whichever channel you're on. Meept: Telegram + Web API + MenuBar only.
9. **Recursive programmatic subagents** [MED] — prime-agent: agent-spawned children with retained registries, depth caps, idle passivation, spawn ledgers; live agent-to-agent messaging with receipts. Meept routes specialists at ingress; agents don't spawn agents.
10. **Quality-gated autonomous completion** [MED] — prime-agent's --autonomous-gate: employee/goal-loop success requires passing a user-defined check; failed gates skipped when workspace unchanged. Meept's GoalLoop has tiers and enforcement but no completion gate concept.
11. **Crash-safe scheduling semantics** [MED] — prime-agent: claim ticks before delivery, coalesce missed ticks; duckagent: tombstoned append-only job store, revision conflicts, missed-run + overlap policies. Meept's mutable job rows are less rigorous.
12. **Office-document read/write as first-class tools** [MED-LOW] — FrontierAgent + atomic-agent both ship PDF/DOCX/PPTX/XLSX writers. Meept covers this via MCP only.
13. **Vision tooling** [LOW-MED] — atomic-agent's vision.describe (mmproj multimodal).
14. **OpenAI-compatible API facade** [LOW-MED] — duckagent + atomic-agent expose the agent at POST /v1/chat/completions; drop-in ecosystem compatibility. Meept exposes REST/WS/MCP but not this shape.
15. **Skill marketplace** [LOW] — ClawHub installs vs meept local-only registry.
16. **Persona/portability flourishes** [LOW] — SillyTavern card import, SOUL.md/USER.md profiles.
17. **End-user distribution machinery** [LOW for meept's purpose] — checksummed installers, self-updaters, signed builds, npm/SEA packaging.

## 4. What they do BETTER than meept's equivalents

- **Containment honesty**: duckagent/FrontierAgent enforce threat models at the OS; meept suggests them in-process. Their approval gates are per-action with previews; meept's are per-plan.
- **Credential hygiene**: secrets never enter child processes (duckagent proxy injection) vs meept handing keys to tools and redacting output.
- **Loop robustness engineering**: FrontierAgent's observer/Intervention contract + guard suite proven across 14 benchmarks; atomic-agent's no-progress veto ladder; meept documents cycle/convergence detectors but no comparable tuned guard suite.
- **Prompt-cache economics**: atomic-agent (byte-stable prefix + bounded mutability-ordered tail, measured ≤2.5k tok/step) and duckagent (stable-prefix + projected history, published simulator table) vs meept's aggressive-but-unmeasured compression.
- **Memory science**: atomic-agent's voting/usefulness-eviction/lessons/procedures + eval harness exceeds meept's age-based consolidation story.
- **Process-lifecycle hardening**: prime-agent's orphan journals, parent-death polls, pid-reuse fixes, doctor --fix; crash-safe tick claiming. Meept has heartbeat watchdogs but nothing documented at this level for its own daemon lifecycle.
- **Release engineering**: all four ship checksummed multi-platform releases; meept builds from source.
- **Operator documentation density** (FrontierAgent bilingual GPU matrices/artifact specs; prime-agent per-feature docs) vs meept leaning on generated topology + internal plan trees.

## 5. What meept does better / has that they lack

vs all four:
- **It is actually the thing three of them aren't**: an always-on personal-agent daemon with resident scheduler, push notifications, and five frontends (TUI/Flutter web/desktop/MenuBar/Telegram) + voice (STT/TTS). FrontierAgent/prime-agent are per-invocation CLIs; atomic-agent has no goal mode; duckagent is closest but shallow everywhere else.
- **Autonomous governance**: AI Employees (required constitutions, tier-aware GoalLoops ASSESS→PLAN→EXECUTE→REFLECT, enforcement engine with pre-exec/post-turn/periodic audits, authority escalation), plan approve/reject lifecycle. Atomic explicitly lacks any goal layer; prime-agent has no approval layer; FA teams are single-task-scoped.
- **Durable cross-session memory at depth**: 6-tier architecture + epistemic claims with trust statuses and contradiction edges. prime-agent's memory is session-scoped harness state; FA has none; duckagent flat JSONL; atomic is strong but single-user-note oriented.
- **Defense-in-depth application-layer security**: taint lattice with sinks, sanitizer, tirith, adversarial boundary markers — none of the four have anything equivalent (prime-agent disclaims containment outright; atomic leaks .env to subprocesses and ships telemetry on by default).
- **Verification culture**: evidence pipeline with claim↔evidence matching, validation gates + retry loops, hallucination detection, git checkpoints. Nothing comparable in any of them.
- **Learning loops**: closed-loop skill evolution (usage tracking, LLM-judge verifier, versioning/rollback), shadow LoRA/DPO training with eval-gated hot-swap, reflection collection, Q-Agent meta-analysis, routing-decision logging. Only prime-agent approaches this (/refine), and it's session-local.
- **Multi-agent orchestration**: specialist DAG planning, amendments, interrupts, async handoff, agentic pairs, CollaborationEngine differential A/B, coworker-awareness tools. duckagent/atomic: single agent.
- **MCP server mode** — meept is consumable BY other agents; only meept offers this among the five.
- **Cost/token governance**: hierarchical budgets, USD tracking with live pricing sync, L1+L2 caches with file-aware invalidation.
- **Engineering hygiene**: 4 custom analyzers, CI-enforced generated connectivity graphs, mutex scope rules, race-test posture, TUI↔GUI parity mandate.
- **Cluster mesh** (gossip P2P, WireGuard sync, distributed memvid) — unique among the five.
- **Deployment simplicity**: two Go binaries + SQLite; no Node ≥25.7 floor, no Python venv, no TS+kernel bootstrap (prime-agent's footprint), no bleeding-edge Node SEA.

## 6. Borrow List — ranked value ÷ effort, license-checked

All four targets are permissive (Apache-2.0/MIT) except duckagent's vendored bubblewrap subtree (LGPL — invoke as external binary, never copy source into Go). meept is MIT, so inbound borrowing of Apache-2.0 requires preserving NOTICE/attribution; MIT→MIT is clean with copyright retention.

Tier 1 — do first (high value, low-medium effort):

1. Secret-backed env reverse proxy (duckagent, Apache-2.0) — placeholder creds + header injection at localhost proxy. Closes meept's biggest leak surface.
2. Checksum-guarded write journal + /revert + diff-preview approval gate (FrontierAgent Apache-2.0 + duckagent Apache-2.0 patterns) — port concepts to Go around existing file tools.
3. Crash-safe scheduler semantics (prime-agent MIT): claim-tick-before-delivery + coalesced missed ticks; add duckagent-style tombstones/revision conflicts later.
4. Quality-gated autonomy (prime-agent MIT): GoalLoop success requires passing a configurable shell gate; skip re-runs when workspace unchanged.
5. Loop-guard suite (FrontierAgent Apache-2.0 concepts + atomic-agent MIT ladder): repetition guard, duplicate-query rollback w/ free re-sample, reasoning-only watchdogs, no-progress warn@3/veto@5/graceful-exit.
6. Stable-prefix prompt ordering + prefix hashing (atomic-agent MIT): reorder context-firewall assembly by mutability; enables provider caching + drift detection nearly free.

Tier 2 — next wave:

7. Network egress policy proxy (duckagent Apache-2.0 design; implement natively in Go, do NOT vendor bwrap).
8. Steer/follow-up delivery receipts + browse/edit/reorder of queued messages (prime-agent MIT) on top of meept's existing queues.
9. Inter-agent messaging with receipts between employees (prime-agent MIT): send <employee>, delivered/queued, roster broadcast.
10. Parallel tool-call batches from one inference with resource-class execution (atomic-agent MIT) onto meept's executor.
11. Refine-with-rollback discipline for internal/selfimprove (prime-agent MIT): never touch base prompts, snapshot, record diffs, revert.
12. Append-only NDJSON run traces + replay/prompt-drift postmortem (atomic-agent MIT) complementing metrics store.
13. Memory voting + usefulness-based eviction + lessons/procedures (atomic-agent MIT) layered onto ftstore/consolidation.
14. Task-board team pattern + SpawnGuard budgets (FrontierAgent Apache-2.0) as a complement to GoalLoop for ad-hoc decomposition.
15. Channel access-control matrix + pairing codes + cross-channel approvals (duckagent Apache-2.0) — prerequisite before adding any second chat channel.
16. Shell command-prefix permission table as JSON preset (duckagent Apache-2.0) alongside tirith.
17. doctor/status/shutdown lifecycle commands + orphan-child cleanup (prime-agent MIT).
18. SSRF guard pattern for fetch tools (atomic-agent MIT): shared host checks + per-hop redirect revalidation.

Tier 3 — larger projects / situational:

19. Browser tool family (chromedp-based; study atomic's ARIA char-budgeting; MIT ideas).
20. Benchmark harness with judges + published scorecards (FrontierAgent Apache-2.0 architecture; meept already has internal/benchmark to build on).
21. OS sandbox backends (bwrap-exec + macOS sandbox-exec + Windows study) — biggest effort, biggest payoff; duckagent's per-OS module architecture is the blueprint.
22. OpenAI-compatible facade over CommServer.
23. Office-doc tools — cover via MCP instead of porting.
24. Subscription-CLI provider adapter if meept ever wants keyless backends (atomic-agent MIT trick works from Go via stdin pipes).

Not worth borrowing: IPython/RLM kernel (wrong paradigm/language), TurboQuant fork (C++/Metal/CUDA out of scope), Tauri embedding, SillyTavern import (unless GUI personas become a want), marketplace infrastructure (no user base yet).

## 7. Red Flags Summary

- FrontierAgent: 2 days old, unreleased, bus factor 1–2, vendor showcase leading with its own model scores; default TUI mode unsandboxed by its own banner; legacy `swarm` naming debt.
- duckagent: dormant ~10 weeks, 17 commits, single account, 0 open issues (= no users filing bugs), monolithic 100KB+ source files, self-reported benchmark numbers, LGPL subtree inside Apache repo (mixed-license trap for wholesale copying).
- atomic-agent: telemetry ON by default contradicting privacy pitch; secrets inherit into subprocesses by design; traces unredacted; pre-1.0 churn (config migrations mid-stream); GAIA numbers self-run with per-row version variance.
- prime-agent: disclaims security containment entirely (full-permission Python kernel); breaking changes nearly every release; heavy TS+Python footprint; contribution mass in one maintainer; lineage dependence on upstream pi.

## 8. Verdict

No repo matches meept's combination of autonomy governance, memory depth, verification culture, and frontend breadth — three of the four aren't even trying to be personal-assistant daemons, and the fourth (duckagent) is dormant and shallow outside its excellent security stack. meept's genuine deficits are concentrated in three areas: OS/network containment (advisory-only today), local-model economics (no browser, no inference ownership, unmeasured context strategy), and outcome-level evaluation evidence (nothing published). The highest-leverage borrow path is Tier 1 items 1-6: all permissively licensed, all retrofit cleanly onto the existing bus/queue/tools architecture, none require new runtime dependencies.

## 9. Extrapolated Gap: Local Inference Ownership (from gap #6)

The claim in one sentence: atomic-agent can download, quantize, cache-optimize for, and serve small open models entirely on the user's machine with measured per-step token costs, while meept treats every LLM as a remote HTTP endpoint and therefore cannot run offline, cannot control cost-per-step at the inference layer, and pays full context price every step.

What that actually decomposes into:

**a) Model lifecycle.** Atomic pulls GGUF models, detects GPU/driver combos (they've patched driver-version edge cases twice), selects device layers, and keeps a warm llama-server daemon alive across turns (`stopOnExit` semantics). Meept has `internal/llm.RuntimeManager` which starts/stops/health-checks external llama.cpp/MLX processes — but nothing downloads or manages model files, exposes GPU capability detection, or picks quantizations. A user who wants qwen2.5-coder locally must install Ollama themselves and hand meept an endpoint URL.

**b) Loop economics for small models.** Small models fail tool-calling in characteristic ways: JSON drifts, first tokens bias toward prose over arrays, long schemas eat the context window. Atomic's answers: GBNF grammar constraints so the sampler physically cannot emit invalid tool-call JSON (array-only root defeats the first-token bias); lazy schema loading (`tool.view`) keeping rare tools to one line until needed; byte-stable prompt prefix + mutability-ordered tail so llama.cpp KV-cache reuse holds every step (~2.5k tok/step target vs unbounded growth). Meept's loop assumes a frontier model that reliably emits valid calls; against a 9B local model, meept's rich tool surface becomes a liability because every registered tool's schema ships on every call. The Context Firewall compresses history but does not engineer prefix stability — so even hosted-API caching benefits are left on the table.

**c) Keyless backends.** Atomic drives signed-in `claude --print --tools ""` / `codex exec --json -s read-only` subprocesses as providers — subscription capacity instead of API billing. Meept's provider layer is HTTP-native; adding a stdio-subscriber adapter would let meept run on existing Claude Code subscriptions.

Why it matters for meept specifically: meept's differentiators (employees, goal loops, always-on bots) presuppose cheap unattended operation. Cloud-API pricing makes 24/7 autonomous employees expensive precisely for the long-tail steps; a local small-model tier (with cloud escalation for hard steps — which meept's capability-based resolver already supports) is the economic unlock for meept's own core vision. Borrow priority: stable-prefix prompt ordering first (pure refactor, helps cloud bills immediately), then a `model pull/list/test` CLI + RuntimeManager extension, then GBNF-constrained calling for local providers only.

---

## 10. Benchmark Program: meept-bench

Goal: close gap #7 by giving meept outcome-level evidence on the same public benchmarks competitors publish. Repo scaffolded 2026-08-24 at github.com/bhodgens/meept-bench. Design decisions and gap-closure plan live in that repo's docs/PLAN.md; summary:

Phase 1 (harness): runner driving meept headlessly over its RPC/WS interface (not screen-scraping the TUI), per-task isolation in fresh worktrees, result capture, deterministic seeds, rerun-failures-only, scorecard generation. Reuse FrontierAgent's harness architecture (Apache-2.0, port Python→Go).

Phase 2 (benchmarks, in difficulty order): GAIA validation subset (text-only L1/L2 first — no browser dependency), SWE-bench Lite or Terminal-Bench for coding/tool use, τ-bench for tool-policy compliance (maps well onto meept's security gates), LoCoMo/LongMemEval for memory claims (directly exercises episodic+epistemic tiers), WebArena-lite deferred until browser tooling exists.

Phase 3 (gaps these benchmarks expose): browser/computer-use (blocks GAIA L3/WebArena), local-model economics (needed to publish competitive $/task numbers like atomic-agent), eval-aligned deterministic tool variants (FrontierAgent pattern).

Honesty rule: publish failures and partial scores alongside wins; self-run results are labeled as such (see red-flag lessons from all four audited repos).


- Local: /Users/caimlas/git/meept — AGENTS.md, LICENSE, docs/features.md, docs/workflows/employees.md, internal/ tree listings, targeted greps (browser/vision absence verified 2026-08-24)
- https://github.com/ApodexAI/FrontierAgent (+ README, LICENSE, tree) — audit 2026-08-24
- https://github.com/selfonomy/duckagent (+ README, Cargo.toml, LICENSE.txt, docs, commit history) — audit 2026-08-24
- https://github.com/AtomicBot-ai/atomic-agent (+ README, LICENSE, AGENTS.md, package.json, releases) — audit 2026-08-24
- https://github.com/PrimeIntellect-ai/prime-agent (+ README, LICENSE, architecture/rlm/skills/long-running docs, releases) — audit 2026-08-24
- Subagent audit transcripts: ~/.hermes/cache/delegation/subagent-summary-{0..3}-20260824_16*.txt
