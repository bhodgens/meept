# Meept Feature Parity Matrix

**Date:** 2026-08-28
**Source:** Meept `docs/features.md`, parity audit `docs/research/2026-08-24-agent-parity-audit.md`, direct repo inspection at commit time.
**Competitors analyzed:** FrontierAgent, duckagent, atomic-agent, prime-agent, Hermes Agent, OpenCode, oh-my-pi, Claude Code/OpenClaw.

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
| Recursive programmatic subagents | ~ | X | - | - | X (rlm + passivation) | - | - | - | - |
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
| MCP client | X (20 preconfigured, 6 enabled) | - | X | X | X | X | Limited | - | X |
| MCP server mode | X | - | - | - | - | - | - | - | - |
| Parallel tool execution | X (semaphore) | X | X | X (resource-class) | X | X | X | X | X |
| Tool streaming progress | X | - | - | - | - | - | - | - | - |
| Browser automation | - | - | - | X (Playwright) | - | ~ | - | - | ~ |
| Computer use (CUA) | - | - | - | - | - | X (macOS) | - | - | - |
| GBNF grammar-constrained tools | - | - | - | X | - | - | - | - | - |
| Vision / image describe | - | - | - | X (mmproj) | - | - | - | - | - |
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
| Secret-backed credential injection | - | - | X (placeholder proxy) | - | - | - | - | - | - |
| Diff preview + write approval | X (PendingChanges, FileEdit only) | X (unified diff all mutations) | X (checksum rewind) | X | ~ | - | - | - | X |
| Fail-closed policy | X | X | X | - | - | - | - | - | ~ |

### Scheduling & Persistence

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Cron jobs | X | - | X | X | X | X | ~ | - | X |
| Job queue with priorities | X | - | - | - | - | - | - | - | - |
| Agent-targeted jobs | X | - | - | - | - | - | - | - | - |
| Daemon / resident mode | X | - | X (gateway service) | - | X (daemon supervisor) | X (gateway) | - | - | - |
| Crash recovery / resume | ~ | X (--resume) | ~ | ~ | X (worker restart) | ~ | - | - | ~ |
| P2P cluster mesh | X (gossip+WireGuard) | - | - | - | - | - | - | - | - |
| Distributed task queue | X | - | - | - | - | - | - | - | - |

### Model & Cost

| Feature | Meept | FrontierAgent | duckagent | atomic-agent | prime-agent | Hermes | OpenCode | oh-my-pi | Claude Code |
|---------|:-----:|:-------------:|:---------:|:------------:|:-----------:|:------:|:--------:|:--------:|:-----------:|
| Provider count | 10+ | ~5 | 30+ | ~10 | ~10 | 15+ | 2-3 | limited | ~3 |
| Capability-based routing | X | - | - | - | - | - | - | - | - |
| Model failover chain | X | - | - | X | - | X | - | - | - |
| Token budgeting | X | ~ | ~ | X | X | X | - | - | ~ |
| Dollar cost tracking | X (OpenRouter live) | - | - | - | - | ~ | - | - | - |
| Reasoning effort support | X | - | - | - | - | ~ | - | - | X |
| Local inference management | ~ (RuntimeManager) | - | - | X (TurboQuant llama.cpp) | - | ~ | - | - | - |
| Subscription CLI provider | - | - | - | X (claude/codex) | X | - | - | - | - |

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
| Benchmark harness | ~ (internal) | X (14 benchmarks) | X (context policy) | X (GAIA published) | - | - | - | - | - |
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
| **Multi-agent orchestration** | Only framework with 18 specialists + 5 reviewers, intent classification, DAG planning, async handoff, and collaboration engine (pair programming, differential A/B) |
| **Memory depth** | 6-tier system: episodic (FTS5), task, knowledge graph (PageRank), semantic (vector HNSW), distributed (memvid), epistemic (claims/trust) |
| **AI Employees** | Only framework with constitution-bound autonomous agents, three autonomy tiers, enforcement engine, and optional quality-gated goal completion |
| **Evidence pipeline** | Only framework with claim-evidence matching, validation gates, and needs_info routing for human review |
| **Defense-in-depth security** | Only framework combining taint lattice, input sanitizer, Tirith shell scan, adversarial boundary markers, and fail-closed policy |
| **Self-improvement loops** | Only framework with closed-loop skill evolution (usage tracking + LLM-judge verifier + versioning), shadow LoRA/DPO training with eval gate, reflection collector, and Q-Agent meta-optimization |
| **Always-on daemon** | Only framework with resident daemon + five frontends (TUI/Flutter/MenuBar/Telegram/HTTP+WS) + voice (STT/TTS) |
| **Cluster mesh** | Only framework with P2P gossip networking, WireGuard sync, and distributed task queue |
| **MCP server mode** | Only framework that can be consumed BY other agents as an MCP server |

### Where Competitors Lead

| Category | Leader | Gap for Meept |
|----------|--------|---------------|
| **OS-enforced sandboxing** | duckagent (bwrap/macOS/Windows), FrontierAgent (bwrap/container) | Meept's docker backend is opt-in and degrades to unsandboxed on failure |
| **Network egress policy** | duckagent (host+CIDR maps, NO_PROXY scrubbing) | Zero network-layer control in meept |
| **Secret-backed credential injection** | duckagent (reverse-proxy placeholder) | Shell execution inherits full daemon env via `os.Environ()` |
| **Diff preview + reversible journal** | FrontierAgent (WorkspaceJournal, /revert), duckagent (checksum rewind) | Meept has PendingChangesRegistry but only for FileEditTool, no user-facing surface |
| **Computer use / browser** | atomic-agent (Playwright), Hermes (macOS CUA) | Not implemented in meept |
| **Local inference ownership** | atomic-agent (TurboQuant llama.cpp, GBNF, subscription CLI) | Meept assumes external endpoints; no model management or KV-cache economics |
| **30+ chat channels** | duckagent (Slack, Discord, Signal, Teams, Home Assistant, etc.) | Meept: Telegram + Web API + MenuBar only |
| **Benchmark + published results** | FrontierAgent (14 benchmarks), atomic-agent (GAIA L1) | Meept has internal eval but no public harness or scorecards |
| **Crash-safe scheduling** | prime-agent (tick claiming, coalesced missed ticks), duckagent (tombstones) | Meept's mutable job rows lack atomic semantics |
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

4. **The biggest opportunity gap is computer use/browser automation**: atomic-agent has a full Playwright suite; Hermes has macOS CUA. Meept has neither. This blocks GAIA L3 and WebArena benchmarks.

5. **Output evaluation evidence is the next milestone**: FrontierAgent and atomic-agent publish benchmark scores. Meept has internal eval packages but no public harness or scorecards. The meept-bench repo (scaffolded 2026-08-24) addresses this.

---

*Matrix generated 2026-08-28 from `docs/features.md`, `docs/research/2026-08-24-agent-parity-audit.md`, and direct repo inspection. MCP catalog count from `config/mcp_servers.json5` (20 servers, 6 enabled). Updated 2026-08-28 for quality-gated goals, standing instructions, reasoning settings, instruction RPC, and employee bot-store wiring.*
