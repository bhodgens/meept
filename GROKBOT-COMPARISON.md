# Grok Bot vs meept — capability comparison

Date: 2026-09-05. Sources: official Grok Bot documentation
(docs.x.ai/grok-bot/* — overview, get-started, bots, computer-and-apps,
chat-and-collaboration, skills-routines-and-automations,
approvals-security-and-privacy, settings-and-notifications,
teams-and-enterprises, mobile, files-and-results, faq, troubleshooting)
plus launch coverage. Meept side: docs/workflows/* and code in
internal/employee, internal/agent, internal/security (docs verified
against implementation signatures).

These are different products. Grok Bot is a hosted consumer SaaS
("AI teammates", subscription, cloud VM per user). Meept is a
self-hosted platform (Go daemon, local-first, Bring-your-own-everything).
The comparison below grades each capability on its own terms.

---

## 1. High-level contrast chart

| Capability | Grok Bot | meept | Edge |
|---|---|---|---|
| Persistent named agents | Bots (max 50/account) | Employees + specialist agents (unbounded) | comparable |
| Own execution computer | Cloud Linux VM per USER, shared by all bots | Your machines (local daemon); cua-driver desktop control | grokbot |
| Persistent browser sessions on that computer | Yes (shared, durable) | No managed browser | grokbot |
| Watch + take over the agent's screen | Live view, takeover/hand-back, desktop + iOS | No equivalent surface | grokbot |
| Learn by demonstration ("teach a task") | 10-min screen recording -> draft skill | None (hand-authored/generated SKILL.md) | grokbot |
| Native mobile app | iOS, near-full parity incl. takeover + approvals | None (TUI/GUI/CLI/Telegram instead) | grokbot |
| Consumer connectors (Gmail, Slack, Notion, GitHub...) | Plugin marketplace, one-click | Via MCP servers you wire yourself | grokbot |
| Event triggers for automations | Slack/GitHub/Teams messages | Webhooks + bus topics + cron (any bus event) | comparable |
| Scheduled automations | Routines (50/bot, 20 run records kept, may auto-pause after absence) | Cron jobs: crash-safe claims, missed-tick coalescing, dead-letter + recovery | meept |
| Model choice | None. Fixed model set, no picker, ever | Any provider, local models, subscription OAuth, capability resolver + failover | meept |
| Governance of agent behavior | Approval cards + model-based Auto Review rules | Constitution: deterministic pre-exec gate, post-turn audit, drift audit, auto-pause | meept |
| Per-agent budgets | Usage allowance per account only | Tokens/cost/invocations per employee per day, hard deny + auto-pause | meept |
| Memory | Opaque "bot remembers preferences/facts/summaries" | 6 subsystems: episodic, task, knowledge graph, vector, personality, distributed + voting | meept |
| Agent-to-agent coordination | Group chat (2-6 bots), @mentions, async messages | Dispatcher + DAG, delegate_task, request_handoff (depth-capped), ReportRouter with context accumulation | comparable |
| Work verification | Model says done; approval on risky actions | Quality gates (shell check must exit 0), evidence-based claim validation, reviewer agents | meept |
| Security engineering | Shared-computer warnings, secret masking, local-exec policy | Risk engine, tirith shell scan, taint tracking, SSRF guard, adversarial-input defense, sandboxing | meept |
| Audit trail | Admin audit view (teams) | Per-employee AuditStore, findings, resolve workflow, `meept agents audit` | meept |
| Skills | Shared skill library + marketplace | SKILL.md, 3-tier discovery, state mode, wiki + evolver (self-improvement) | comparable |
| Extensibility | Plugins/MCP (hosted, admin-governed) | MCP client+server, ACP, arbitrary Go tools, skills in git | meept |
| Multi-node / scale-out | None (one cloud VM per user) | Cluster: P2P mesh, shared queue, gossip, WireGuard | meept |
| Multi-user | Account = one human + team plans (SSO, admin dashboard) | Opt-in multi-user: per-user keys, owner-scoped sessions; no admin dashboard | grokbot (admin UX) |
| Data ownership | Cloud only; Legacy Privacy Mode unsupported | Local disk, you own everything | meept |
| Voice | Dictation into text field; no live voice | STT dictation (whisper/parakeet/native) + TTS readback (Piper); no live voice | comparable |
| Setup friction | Install, sign in, name a bot | Daemon + config + models | grokbot |
| Price | ~$120-300/mo subscription, no free tier | Free (self-hosted, your API keys) | meept |

---

## 2. What meept does better

1. **Model freedom.** Grok Bot states plainly: "Grok Bot has no model
   picker, for members or admins. We do not plan to allow admin or user
   choice." Meept resolves models by capability across any provider,
   supports local models, subscription OAuth (ChatGPT/Claude/SuperGrok),
   alias failover, quota-aware parking, and slot priority.
2. **Enforcement is deterministic, not advisory.** Grok Bot's Auto
   Review is model-based ("should complement, not replace" least
   privilege). Meept employees carry constitutions enforced at three
   checkpoints: a zero-LLM pre-exec gate (tool allow/deny, risk ceiling,
   never-rules, budgets — microseconds), a per-turn small-model audit,
   and a periodic drift audit with DriftScore and auto-pause. A Grok Bot
   bot has no equivalent hard gate.
3. **Budgets per agent.** Meept caps tokens/cost/invocations per
   employee per day and pauses on exhaustion. Grok Bot manages usage at
   the account level only; a single bot can burn the whole allowance.
4. **Memory is a real architecture.** Grok Bot memory is opaque and
   per-bot. Meept has episodic (FTS5/BM25), task, knowledge-graph
   (PageRank, community detection), vector-hybrid, personality, and
   distributed (memvid) memory with usefulness voting and an
   epistemic-integrity pipeline (librarian/skeptic agents).
5. **Scheduling reliability.** Grok Bot routines cap at 50/bot, keep 20
   run records, and the platform "may ask whether to keep routines
   running after a long period away and pause them." Meept's scheduler
   claims ticks atomically in SQLite before dispatch, coalesces
   missed ticks, dead-letters failed jobs with a recovery API, and
   orders claims interactive-first.
6. **Verification before "done".** Meept gates completion on real shell
   checks (goal gate + roster gate, workspace-hash aware), validates
   claims against evidence (deterministic execution), and routes work
   through reviewer agents. Grok Bot has approval cards for risky
   actions but no completion proof.
7. **Self-hosting, data ownership, cost.** Meept runs on your hardware;
   data never leaves your disk. Grok Bot requires cloud storage,
   blocks Legacy Privacy Mode, and costs $120-300/month.
8. **Scale-out and depth.** Cluster mesh with shared queue (no Grok Bot
   equivalent). Orchestration goes deeper than group chat: dispatcher
   classification, task DAG, mid-task handoff injection (depth-capped
   at 5), report routing with accumulated context.

## 3. What Grok Bot does better

1. **The cloud computer is the product.** Every user gets a persistent
   Linux VM with a browser whose sessions and logins survive restarts.
   Bots sign into websites once and keep working after the laptop
   closes. Meept's computer use (cua-driver) drives YOUR desktop and
   stops when your machine does; it has no persistent managed browser.
2. **Watch-and-take-over UX.** Live screen view with status/preview/
   takeover levels, on desktop and iPhone, including hand-back for
   passwords, 2FA, and CAPTCHAs, with masked secret entry excluded
   from the transcript and never shown to the model. Meept has no
   interactive takeover surface and no transcript-excluded secret path.
3. **Learn by demonstration.** "Teach a task" records up to 10 minutes
   of browser work and produces a reviewable draft skill. Meept skills
   are authored by hand or written by agents; there is no
   demonstration-learning path. (Caveat: the feature is in gradual
   rollout and browser-only.)
4. **Native mobile.** Full iOS app with near-desktop parity — create
   bots, approve actions, take over the computer. Meept's surfaces are
   TUI, Flutter desktop/web, CLI, and Telegram; there is no native
   mobile app, and Telegram is the only pocket reach.
5. **Zero-config and consumer connectors.** Install, sign in, name a
   bot. Plugin marketplace covers Gmail, Slack, Notion, Google
   Workspace, GitHub and more with one-click OAuth, fleet-shared. In
   meept you wire MCP servers yourself; power, but friction.
6. **Team administration.** SSO (Okta/Entra), MCP allow/deny lists,
   team rules scoped into bot context, static egress IPs for
   allowlisting, admin computer kill, Cloud Agents toggle. Meept's
   multi-user auth is opt-in and minimal (keys, quotas as stubs,
   owner-scoped sessions) — no admin dashboard. (AgentAudit targets
   this gap but is a separate project.)
7. **Bot sharing and ecosystem.** Public share links for bot configs,
   a marketplace, third-party tooling already forming. Meept employees
   are JSON5 files you can copy, but there is no ecosystem surface.

## 4. Comparable capabilities

- **Persistent named agents with roles.** Bot identity (name, title,
  description, avatar) maps to employee identity + charter. Meept adds
  the constitution; Grok Bot adds the avatar/presence UX.
- **Automation = schedule or event.** Routines map to meept cron jobs.
  Both support event triggers; Grok Bot's are packaged integrations
  (Slack/GitHub/Teams), meept's are webhooks and any bus topic.
- **Skills.** Both have reusable instruction libraries. Grok Bot
  contributes teach-by-demo and a marketplace; meept contributes
  capability-based model resolution, state mode, and the wiki/evolver
  self-improvement loop.
- **Delegation.** Grok Bot: bots message each other, receiver wakes and
  replies later; group chats of 2-6 with @routing. Meept:
  delegate_task, request_handoff with DAG injection and a 5-hop cap,
  multi-participant sessions with attributed sources. Same shape,
  different control philosophy (chat-first vs orchestrator-first).
- **Approval for consequential actions.** Grok Bot: allow once / always
  / deny cards, require-approval rules. Meept: security-engine
  confirmations by risk class, plan signoff for tier-2 employees,
  escalation triggers. Equivalent intent; meept's is rule-deterministic.
- **Dictation-grade voice.** Both are dictation-only today; neither
  ships a live voice mode. Meept additionally reads replies aloud
  (client-side Piper TTS).
- **Multi-surface clients.** Desktop + iOS vs TUI + Flutter + CLI +
  Telegram + HTTP. Both sync one logical workspace across surfaces.

## 5. Bottom line

- Grok Bot wins on **execution environment** (persistent cloud VM,
  durable browser sessions, watch/take-over, mobile) and on **adoption
  friction** (zero config, packaged connectors, admin UX).
- Meept wins on **control** (model choice, deterministic enforcement,
  budgets, verification gates, audit), **memory depth**,
  **scheduling reliability**, **extensibility**, **scale-out**, and
  **data ownership/cost**.
- The strategic read: Grok Bot's genuinely novel primitives are
  learn-by-demonstration and the takeover/hand-back credential flow.
  Both are portable ideas: teach-a-task could become a meept skill
  recorder (computer-use capture -> SKILL.md draft), and masked
  secret handoff could gate meept tool calls that need credentials.
  The cloud-VM runtime is the one gap that is architectural, not
  incremental — meept's design goal is local-first, so the honest
  framing is "meept deliberately does not compete here; cua-driver +
  a remote node in the cluster is the meept-shaped answer."

## Appendix: source notes

- "50 Bots and group chats combined" per account — docs.x.ai bots/faq.
- One shared computer per user, "not a security boundary" — overview,
  computer-and-apps, faq, teams-and-enterprises.
- "No model picker... We do not plan to allow" — teams-and-enterprises.
- Teach a task: 10-min cap, browser-only, no mic audio, gradual
  rollout — skills-routines, faq.
- Routine limits (50/bot, 20 run records, absence-pause prompt) —
  skills-routines-and-automations.
- Auto Review "model-based... complement, not replace" —
  approvals-security-and-privacy.
- Pricing tiers and absence of free tier — faq + launch coverage
  (dev.to review: $200/mo entry incl. Cursor Ultra).
- Meept claims verified against docs/workflows/employees.md,
  job-scheduling.md, memory.md, agent-orchestration.md, security.md,
  computer-use-security.md, cluster.md, auth.md, tts.md,
  speech-to-text.md, multi-participant-comms.md and
  internal/employee/*.go (Manager, GoalLoop, PreExecChecker,
  PostTurnAuditor, PeriodicAuditor, AuditStore, EpisodeParker).
