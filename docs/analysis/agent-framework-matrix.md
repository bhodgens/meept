# Agent Framework Comparative Matrix

**Date:** 2026-06-25
**Purpose:** Feature-by-feature comparison matrix with frameworks as columns and capabilities as rows.

---

## Legend

| Cell Style | Meaning |
|------------|---------|
| <span style="background-color: #ff4444; color: white; padding: 2px 8px;">**MISSING**</span> | Feature not implemented |
| <span style="background-color: #44aa44; color: white; padding: 2px 8px;">**WINNER**</span> | Clear differentiator / best-in-class |
| ✅ | Implemented / Yes |
| ⚠️ | Partial / Limited |
| ❌ | No / None |
| 🔲 | TODO / Unknown |

---

## Master Comparison Matrix

<table>
<thead>
<tr>
<th style="min-width: 200px;">Feature Category</th>
<th style="min-width: 140px; background-color: #2E86AB;">Meept</th>
<th style="min-width: 140px;">OpenCode</th>
<th style="min-width: 140px;">OpenAgent (Rust)</th>
<th style="min-width: 140px;">OpenAgent (Python)</th>
<th style="min-width: 140px;">OpenClaw</th>
<th style="min-width: 140px;">oh-my-pi</th>
<th style="min-width: 140px;">Hermes</th>
</tr>
</thead>
<tbody>

<!-- Agent Architecture -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">📊 Agent Architecture</th></tr>

<tr>
<td><strong>Specialist Agents</strong></td>
<td style="background-color: #44aa44; color: white;">18 + 5 reviewers<br/><small>Intent-based routing</small></td>
<td>Single generic</td>
<td>Single ReAct</td>
<td>Single + Teams</td>
<td>Single generic</td>
<td>Single agent</td>
<td>Single + subagent</td>
</tr>

<tr>
<td><strong>Multi-Agent Delegation</strong></td>
<td style="background-color: #44aa44; color: white;">Yes<br/><small>Delegation + handoff</small></td>
<td>❌ No</td>
<td>🔲 TODO</td>
<td>✅ Dynamic teams</td>
<td>❌ No</td>
<td>❌ No</td>
<td>✅ Kanban swarm</td>
</tr>

<tr>
<td><strong>Intent Classification</strong></td>
<td style="background-color: #44aa44; color: white;">LLM classifier</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Model Routing</strong></td>
<td style="background-color: #44aa44; color: white;">Capability-based resolver</td>
<td>Manual</td>
<td>Single provider</td>
<td>Team-as-router</td>
<td>Manual</td>
<td>N/A</td>
<td>Per-session swap</td>
</tr>

<!-- Memory System -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🧠 Memory System</th></tr>

<tr>
<td><strong>Short-Term Memory</strong></td>
<td>Conversation store</td>
<td>In-memory</td>
<td>Sliding window (40)</td>
<td>Sessions table</td>
<td>In-memory</td>
<td>Conversation history</td>
<td>SQLite + FTS5</td>
</tr>

<tr>
<td><strong>Long-Term Memory</strong></td>
<td style="background-color: #44aa44; color: white;">5 tiers: Episodic+Task+Graph+Vector+memvid</td>
<td>❌ None</td>
<td>LanceDB (stub)</td>
<td>SQLite + skills</td>
<td>❌ None</td>
<td>SQLite FTS</td>
<td>9 pluggable</td>
</tr>

<tr>
<td><strong>Cross-Session Isolation</strong></td>
<td style="background-color: #44aa44; color: white;">Thread partitioning</td>
<td>❌ None</td>
<td>Dump to files</td>
<td>Session search</td>
<td>❌ None</td>
<td>Session-based</td>
<td>Parent chains</td>
</tr>

<tr>
<td><strong>Context Compaction</strong></td>
<td style="background-color: #44aa44; color: white;">Context firewall + summarization</td>
<td>Truncation</td>
<td>🔲 TODO</td>
<td>In-session recap</td>
<td>Basic truncation</td>
<td>Limited</td>
<td>LLM summarization</td>
</tr>

<!-- Tool System -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🔧 Tool System</th></tr>

<tr>
<td><strong>Tool Count</strong></td>
<td>40+ builtin + MCP</td>
<td>~20</td>
<td>~25 service tools</td>
<td>16+ MCP servers</td>
<td>~15</td>
<td>~10</td>
<td style="background-color: #44aa44; color: white;">86 tools</td>
</tr>

<tr>
<td><strong>Tool Discovery</strong></td>
<td style="background-color: #44aa44; color: white;">Intent-based routing</td>
<td>Manual</td>
<td>BM25 search</td>
<td>MCP registry</td>
<td>Manual</td>
<td>Static</td>
<td>Registry + MCP</td>
</tr>

<tr>
<td><strong>Security Gating</strong></td>
<td style="background-color: #44aa44; color: white;">SecurityEngine + Tirith</td>
<td>Minimal</td>
<td>Guard whitelist</td>
<td>Approvals</td>
<td>Minimal</td>
<td>Minimal</td>
<td>Heuristic + isolation</td>
</tr>

<tr>
<td><strong>MCP Support</strong></td>
<td>21 preconfigured</td>
<td>Limited</td>
<td>❌ No</td>
<td>✅ Yes</td>
<td>❌ No</td>
<td>❌ No</td>
<td>✅ Yes</td>
</tr>

<!-- Security Model -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🔒 Security Model</th></tr>

<tr>
<td><strong>Permission System</strong></td>
<td style="background-color: #44aa44; color: white;">SQLite policy checker</td>
<td>❌ None</td>
<td>Guard whitelist</td>
<td>Approvals</td>
<td>❌ None</td>
<td>Minimal</td>
<td>Toolset allowlist</td>
</tr>

<tr>
<td><strong>Input Sanitization</strong></td>
<td style="background-color: #44aa44; color: white;">Prompt injection detection</td>
<td>Basic</td>
<td>Credential scrub</td>
<td>❌ No</td>
<td>Basic</td>
<td>❌ No</td>
<td>Message sanitization</td>
</tr>

<tr>
<td><strong>Command Scanning</strong></td>
<td style="background-color: #44aa44; color: white;">Tirith</td>
<td>❌ No</td>
<td>Sandbox only</td>
<td>Pattern blocks</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Tirith</td>
</tr>

<tr>
<td><strong>TLS Support</strong></td>
<td>✅ Yes (auto-cert)</td>
<td>⚠️ Optional</td>
<td>❌ No</td>
<td>P2P auth</td>
<td>⚠️ Optional</td>
<td>❌ No</td>
<td>Transport-level</td>
</tr>

<tr>
<td><strong>Path Fencing</strong></td>
<td style="background-color: #44aa44; color: white;">Project-local</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Path validation</td>
</tr>

<!-- Execution Model -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">⚙️ Execution Model</th></tr>

<tr>
<td><strong>Daemon Mode</strong></td>
<td style="background-color: #44aa44; color: white;">RPC + HTTP</td>
<td>❌ No</td>
<td>✅ Port 8080</td>
<td>✅ Yes</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Gateway</td>
</tr>

<tr>
<td><strong>CLI Interface</strong></td>
<td>Charmbracelet TUI</td>
<td>✅ Yes</td>
<td>❌ No</td>
<td>✅ Yes</td>
<td>✅ Yes</td>
<td>✅ Yes</td>
<td>prompt_toolkit</td>
</tr>

<tr>
<td><strong>GUI Interface</strong></td>
<td style="background-color: #44aa44; color: white;">Flutter + MenuBar</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Electron</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>API Access</strong></td>
<td style="background-color: #44aa44; color: white;">REST + RPC + WebSocket</td>
<td>❌ No</td>
<td>HTTP</td>
<td>HTTP + P2P</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Session Persistence</strong></td>
<td>SQLite</td>
<td>❌ None</td>
<td>SQLite</td>
<td>SQLite</td>
<td>❌ None</td>
<td>SQLite</td>
<td>SQLite</td>
</tr>

<!-- Context Management -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">📑 Context Management</th></tr>

<tr>
<td><strong>Window Management</strong></td>
<td style="background-color: #44aa44; color: white;">Context firewall</td>
<td>Basic truncation</td>
<td>Sliding window</td>
<td>Threshold trigger</td>
<td>Truncation</td>
<td>Limited</td>
<td>LLM summarization</td>
</tr>

<tr>
<td><strong>Compression</strong></td>
<td style="background-color: #44aa44; color: white;">Hierarchical summarization</td>
<td>❌ No</td>
<td>🔲 TODO</td>
<td>In-session recap</td>
<td>❌ No</td>
<td>Limited</td>
<td>Pluggable engine</td>
</tr>

<tr>
<td><strong>Thread Partitioning</strong></td>
<td style="background-color: #44aa44; color: white;">Yes (thread-based)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Session branching</td>
</tr>

<tr>
<td><strong>Token Tracking</strong></td>
<td style="background-color: #44aa44; color: white;">Per-turn + per-session</td>
<td>Limited</td>
<td>❌ No</td>
<td>Basic</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Per-turn</td>
</tr>

<!-- Scheduling -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">📅 Scheduling</th></tr>

<tr>
<td><strong>Cron Jobs</strong></td>
<td>✅ Yes</td>
<td>⚠️ Limited</td>
<td>✅ Full cron</td>
<td>Cron + Workflows</td>
<td>❌ No</td>
<td>❌ No</td>
<td>✅ Full cron</td>
</tr>

<tr>
<td><strong>Job Queue</strong></td>
<td style="background-color: #44aa44; color: white;">Yes (claim-based)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Request queue</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Priority Queue</strong></td>
<td style="background-color: #44aa44; color: white;">Yes</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Agent Targeting</strong></td>
<td style="background-color: #44aa44; color: white;">Yes (agent_id)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>DAG/Workflow</strong></td>
<td>Plans system</td>
<td>❌ No</td>
<td>❌ No</td>
<td style="background-color: #44aa44; color: white;">Yes (DAG engine)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<!-- Observability -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">📈 Observability / Metrics</th></tr>

<tr>
<td><strong>Metrics Store</strong></td>
<td style="background-color: #44aa44; color: white;">SQLite TSDB</td>
<td>❌ None</td>
<td>OTEL JSONL</td>
<td>SQLite usage</td>
<td>❌ None</td>
<td>Limited</td>
<td>Cost estimation</td>
</tr>

<tr>
<td><strong>Prometheus Export</strong></td>
<td>✅ Compatible</td>
<td>❌ No</td>
<td>🔲 TODO</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Structured Logging</strong></td>
<td style="background-color: #44aa44; color: white;">slog</td>
<td>Basic</td>
<td>✅ Structured</td>
<td>elog()</td>
<td>Basic</td>
<td>Basic</td>
<td>RotatingFileHandler</td>
</tr>

<tr>
<td><strong>Health Endpoints</strong></td>
<td>✅ Yes</td>
<td>❌ No</td>
<td>/api/diagnose</td>
<td>/api/usage</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<!-- Model Routing -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🎯 Model Routing</th></tr>

<tr>
<td><strong>Provider Support</strong></td>
<td>10+ providers</td>
<td>2-3</td>
<td>1</td>
<td>15+</td>
<td>1-2</td>
<td>Limited</td>
<td style="background-color: #44aa44; color: white;">15+ providers</td>
</tr>

<tr>
<td><strong>Capability Matching</strong></td>
<td style="background-color: #44aa44; color: white;">Skill-based resolution</td>
<td>Manual</td>
<td>🔲 TODO</td>
<td>Team-as-router</td>
<td>Manual</td>
<td>❌ No</td>
<td>Per-session swap</td>
</tr>

<tr>
<td><strong>Fallback Chain</strong></td>
<td>✅ Yes</td>
<td>❌ No</td>
<td>🔲 TODO</td>
<td>✅ Yes</td>
<td>⚠️ Limited</td>
<td>❌ No</td>
<td>✅ Yes</td>
</tr>

<tr>
<td><strong>Model Presets</strong></td>
<td style="background-color: #44aa44; color: white;">7 presets</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Reasoning Effort</strong></td>
<td style="background-color: #44aa44; color: white;">Vendor translation</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Auxiliary model</td>
</tr>

<!-- Self-Improvement -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🔄 Self-Improvement</th></tr>

<tr>
<td><strong>Detection</strong></td>
<td style="background-color: #44aa44; color: white;">Multiple sources</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Auto-skills</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Background review</td>
</tr>

<tr>
<td><strong>Analysis</strong></td>
<td style="background-color: #44aa44; color: white;">Root cause</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Pattern matching</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Skill creation</td>
</tr>

<tr>
<td><strong>Fix Generation</strong></td>
<td style="background-color: #44aa44; color: white;">AI-powered</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Skill updates</td>
</tr>

<tr>
<td><strong>Validation</strong></td>
<td style="background-color: #44aa44; color: white;">Sandboxed testing</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Implicit</td>
</tr>

<tr>
<td><strong>Application</strong></td>
<td style="background-color: #44aa44; color: white;">With approval</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>Automatic</td>
</tr>

<!-- AI Employees -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">🤖 AI Employees / Autonomous Agents</th></tr>

<tr>
<td><strong>Constitution System</strong></td>
<td style="background-color: #44aa44; color: white;">Yes (4 sections)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Autonomy Tiers</strong></td>
<td style="background-color: #44aa44; color: white;">3 tiers (reactive/propose/autonomous)</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Enforcement Engine</strong></td>
<td style="background-color: #44aa44; color: white;">Pre-exec + Post-turn + Periodic</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Goal Loop</strong></td>
<td style="background-color: #44aa44; color: white;">ASSESS→PLAN→EXECUTE→REFLECT</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>⚠️ Kanban tasks</td>
</tr>

<tr>
<td><strong>Audit Findings</strong></td>
<td style="background-color: #44aa44; color: white;">SQLite findings</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<!-- Evidence Execution -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">✅ Evidence-Based Execution</th></tr>

<tr>
<td><strong>Evidence Types</strong></td>
<td style="background-color: #44aa44; color: white;">File hashes, exit codes, API responses</td>
<td>❌ None</td>
<td>❌ None</td>
<td>❌ None</td>
<td>❌ None</td>
<td>❌ None</td>
<td>❌ None</td>
</tr>

<tr>
<td><strong>Validator</strong></td>
<td style="background-color: #44aa44; color: white;">Yes</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<tr>
<td><strong>Claim Checking</strong></td>
<td style="background-color: #44aa44; color: white;">Claims vs evidence</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>⚠️ Heuristic abort</td>
</tr>

<tr>
<td><strong>Needs Info Routing</strong></td>
<td style="background-color: #44aa44; color: white;">Human review</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
<td>❌ No</td>
</tr>

<!-- Implementation -->
<tr><th colspan="8" style="background-color: #f0f0f0; text-align: left; padding: 8px;">💻 Implementation</th></tr>

<tr>
<td><strong>Language</strong></td>
<td style="background-color: #44aa44; color: white;">Go 1.24</td>
<td>TypeScript</td>
<td>Rust + Go</td>
<td>Python</td>
<td>TypeScript</td>
<td>Python</td>
<td>Python</td>
</tr>

<tr>
<td><strong>Native Threading</strong></td>
<td style="background-color: #44aa44; color: white;">Yes (goroutines)</td>
<td>Event loop</td>
<td>Yes (async)</td>
<td>❌ GIL</td>
<td>Event loop</td>
<td>❌ GIL</td>
<td>❌ GIL</td>
</tr>

<tr>
<td><strong>Deployment</strong></td>
<td style="background-color: #44aa44; color: white;">Single binary</td>
<td>npm + Node</td>
<td>Cargo build</td>
<td>pip + venv</td>
<td>npm + Node</td>
<td>pip + venv</td>
<td>pip + venv</td>
</tr>

</tbody>
</table>

---

## Summary: Clear Winners by Category

| Category | Winner | Rationale |
|----------|--------|-----------|
| **Agent Architecture** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with 18 specialists + 5 reviewers + intent classification + capability routing |
| **Memory System** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | 5-tier memory with FTS5, thread partitioning, ContextFirewall |
| **Tool Count** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Hermes</span> | 86 tools vs Meept's 40+ |
| **Tool Security** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | SecurityEngine + Tirith + path fencing |
| **Security Model** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with all 5: permissions, sanitization, Tirith, TLS, fencing |
| **Execution Model** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with Daemon + CLI + GUI + REST/RPC/WS |
| **Context Management** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | ContextFirewall + thread partitioning + hierarchical summarization |
| **Scheduling** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with job queue + agent targeting; OpenAgent (Python) leads on DAG |
| **Observability** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | SQLite TSDB + slog + Prometheus + health endpoints |
| **Model Routing** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Capability-based resolution + reasoning effort translation |
| **Self-Improvement** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Full cycle: detect → analyze → generate → validate → apply |
| **AI Employees** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with constitution, autonomy tiers, enforcement, goal loops |
| **Evidence Execution** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Meept</span> | Only framework with evidence pipeline, validators, claim checking |
| **Browser Automation** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">Hermes</span> | 12 browser tools, macOS CUA |
| **DAG Workflows** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">OpenAgent (Python)</span> | Full DAG engine vs Meept's simpler Plans system |
| **P2P Networking** | <span style="background-color: #44aa44; color: white; padding: 2px 6px;">OpenAgent (Python)</span> | Iroh-based P2P vs Meept's centralized daemon |

---

## Features Missing in Meept (Dark Red Categories)

| Feature | Leader | Meept Status |
|---------|--------|--------------|
| Browser automation | Hermes (12 tools) | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Basic web fetch only |
| Computer use (CUA) | Hermes (macOS) | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Not implemented |
| Native desktop app | OpenAgent (Python) Electron | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Flutter is web-based |
| P2P networking | OpenAgent (Python) Iroh | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Centralized daemon |
| Home Assistant | Hermes | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Not implemented |
| DAG workflow engine | OpenAgent (Python) | <span style="background-color: #ff4444; color: white; padding: 2px 8px;">MISSING</span> Plans system is simpler |

---

## Key Takeaways

### Meept's Unmatched Advantages (Green Across the Board)

1. **AI Employees** - No other framework has constitution-bound autonomous agents
2. **Evidence-Based Execution** - Only Meept validates agent claims against ground truth
3. **Seven-Layer Safety** - Watchdog, cycle/convergence detection, budget tracking, model failover, hallucination recovery, context firewall
4. **Multi-Transport Daemon** - RPC + HTTP + WebSocket in a single daemon
5. **Intent-Based Tool Routing** - Tools selected per-agent based on capabilities

### Areas for Improvement (Red Cells for Meept)

1. **Browser automation** - Critical for modern web agent tasks
2. **Computer use** - Growing expectation for desktop automation
3. **DAG workflows** - Complex multi-step tasks would benefit from workflow engine

---

*Matrix generated 2026-06-25. Based on analysis of framework repositories and documentation.*
