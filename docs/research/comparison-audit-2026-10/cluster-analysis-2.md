# Meept feature-parity analysis — clusters 6–11 (shipped since 2026-09-05)

**Subject:** `/Users/caimlas/git/meept` @ `ee0de064` (read-only; nothing modified)
**Competitors:** `/var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-compare/{frontier-agent,duckagent,atomic-agent,prime-agent,hermes,opencode,oh-my-pi}`
**Method:** every claim below carries a `file:line` plus a verbatim quote read from source. Absence claims name the grep command and directories. Default-on vs opt-in is stated per cluster from the shipped config template + Go schema defaults, not from docs.

---

## Verdict table

| # | Cluster | Meept default-on? | Best competitor match | Verdict |
|---|---|---|---|---|
| 6 | Agent-writable skills | **Yes** (tools unconditional; gate default-on) | hermes `tools/skill_manager_tool.py:811` `skill_manage` | **PARITY (~)** — hermes matches both halves; oh-my-pi/prime-agent are partial |
| 7 | Per-agent/per-provider token accounting + tokscale export | **Yes** (`llm_calls` ledger always on) | hermes `agent/usage_pricing.py:66-71` `class CanonicalUsage` | **PARITY (~)** — field-level parity; tokscale export NOVEL |
| 8 | Orphan sweep + parent-death supervision | **Yes** (`supervise` absent ⇒ true) | hermes `tools/mcp_death_supervisor.py:23-25` pipe-EOF supervisor | **PARITY (~)** — atomic-agent's boot reaper is comparable |
| 9 | Anomaly detection on live streams | **No** (2 of 3 default-off, log-only) | prime-agent `crates/pa-types/src/incident/anomaly.rs:53-66` `densest_window_run` | **WEAKER (-)** — off by default, log-only, and no competitor beats it |
| 10 | GUI/CLI breadth (config R/W, per-model usage) | **Yes** (routes unconditional) | hermes `PUT /api/config` + `AnalyticsPage.tsx:581` | **PARITY (~)** on both axes |
| 11 | Thinking-safe summarization | **Yes** (no flag; per-call option) | oh-my-pi `title-generator.ts:348` `disableReasoning: true,` | **PARITY (~)** — oh-my-pi has 8 sites, meept has 4 |

**Net: 0 clean NOVEL clusters.** Two narrow NOVEL slices survive (cluster 7's tokscale export; cluster 9's log-only detectors *if* enabled). Details below.

---

## CLUSTER 6 — skills as a first-class agent-writable platform

### (a) Meept side

**Agent-writable, default-on, no enable flag.** `internal/daemon/components.go:5846-5848`:

> `// Registration is unconditional:`
> `// self-modification risk is governed by the security engine (skills_create /`
> `// skills_patch seed HIGH), not by an enable flag.`

Wiring `internal/daemon/components.go:5851,5857`:

> `createTool := builtin.NewSkillCreateTool(skillCtx.writer, logger)`
> `registry.Register(builtin.NewSkillPatchTool(skillCtx.registry, skillCtx.writer, logger))`

Tool names `internal/tools/builtin/skill_create_tool.go:54` `func (t *SkillCreateTool) Name() string { return "skills_create" }` and `internal/tools/builtin/skill_patch_tool.go:58` `func (t *SkillPatchTool) Name() string { return "skills_patch" }`.

Reachable from a user surface: registered in the agent tool registry (so every chat turn), plus a GUI reflection surface (`ui/flutter_ui/lib/features/reflection/reflection_panel.dart` references both tools), plus `internal/tui/command_handler_improvements_test.go` / `internal/comm/http/reflection_handlers_test.go`.

**Security gate, not an enable flag.** `internal/security/seed_rules.go:92-93`:

> `{ToolName: "skills_create", Action: "skills_create", RiskLevel: RiskHigh, Description: "Create a new skill (agent-authored instructions; self-modification)", RequiresConfirmation: true, Immutable: false},`
> `{ToolName: "skills_patch", Action: "skills_patch", RiskLevel: RiskHigh, Description: "Patch an existing skill (agent-authored instructions; self-modification)", RequiresConfirmation: true, Immutable: false},`

**Patch is a real unique-string swap, not whole-file replace.** `internal/tools/builtin/skill_patch_tool.go:28-31`:

> `//   - replace: a unique-string swap on the body (frontmatter is never`
> `//     touched — use rewrite for structural changes). This keeps old_string`
> `//     uniqueness meaningful and avoids YAML surgery.`
> `//   - rewrite: the full new SKILL.md content, validated through the same`

Plus an immutability boundary — `skill_patch_tool.go:34-36`:

> `// Skills discovered in the system tier (skills.PrioritySystem) are refused:`
> `// the system tier is immutable from the agent's perspective, and the agent`
> `// is directed to create a user-tier shadow instead.`

**`requires-tools` availability gate — default-ON and consulted live.** Frontmatter key `internal/skills/models.go:240` `RequiresTools stringList \`yaml:"requires-tools"\``. The gate is not a config flag: `internal/daemon/components.go:7668-7673`:

> `// requires-tools gating (skills-discovery-02): consult the LIVE tool`
> `// registry at execution time, not a snapshot — builtin tools`
> `// register after initializeSkills and MCP tools come and go with`
> `// their servers. A skill listing unavailable tools fails before any`
> `// LLM call (checkRequiredTools runs pre-flight in the executor).`

and `components.go:7672-7674`:

> `skills.WithToolAvailability(func(toolName string) bool {`
> `	return c.ToolRegistry.Get(toolName) != nil`

`requires-tools` specifically bypasses the separate opt-in prerequisite checker — `internal/daemon/components.go:7661-7666`:

> `if cfg.Skills.ValidatePrerequisites {`
> `	executorOpts = append(executorOpts,`
> `		skills.WithValidatePrerequisites(true),`

That flag does default true (`internal/config/schema.go:3267` `ValidatePrerequisites: true,`), but `WithToolAvailability` is appended **outside** the `if` (line 7672), so the availability gate is unconditional. Confirmed by the gate's own test, `internal/skills/executor_requires_tools_test.go:50-52`:

> `if counter.calls != 0 {`
> `	t.Errorf("checker should never be called without requires-tools, got %d calls", counter.calls)`

**Bundled shipping, no-clobber.** `scripts/install-sync.py:7,20`:

> `Merge semantics — never clobbers user modifications:`
> `keep   (default) — user file untouched, new default saved as <f>.new`

**Directory-layout skills as linked asset packages — NOT PRESENT.** `find config/skills -mindepth 2 -not -name "SKILL.md"` returns **zero** results: all 16 shipped skills are a bare `SKILL.md` with no asset subdirectory. This sub-claim does not hold against source.

### (b) Competitors

**MATCH — hermes.** Create *and* patch, same shapes as meept.
`hermes/tools/skill_manager_tool.py:811` `"name": "skill_manage",`; registered at `:873-874` `registry.register(` / `name="skill_manage", toolset="skills", schema=SKILL_MANAGE_SCHEMA,`.
Create `:404` `def _create_skill(name: str, content: str, category: str = None)` → `:427` `skill_md = skill_dir / "SKILL.md"`.
Patch `:459` `def _patch_skill(name: str, old_string: str, new_string: str, file_path: str = None,` — i.e. the same old_string→new_string discipline.
Availability gate `hermes/agent/prompt_builder.py:1313-1314`:

> `or any(ts not in ats for ts in conditions.get("requires_toolsets", []))`
> `or any(t not in at for t in conditions.get("requires_tools", []))`

registry-derived at `agent/system_prompt.py:307` `avail_toolsets = {model_tools.get_toolset_for_tool(tool_name) for tool_name in agent.valid_tool_names} - {None, ""}`.

**PARTIAL — oh-my-pi.** `oh-my-pi/packages/coding-agent/src/tools/manage-skill.ts:41` `readonly name = "manage_skill";` with a create-collision guard at `:78` `if (params.action === "create" && isNameClaimedByAuthoredSkill(sanitizeSkillName(params.name))) {`. **No patch:** the schema is whole-body (`:18` `action: "'create' | 'update' | 'delete'",`, `:22` `"body?": type("string").describe("the SKILL.md body in markdown, no frontmatter (required for create/update)"),`); grep for `old_string|new_string|oldString|newString` in `manage-skill.ts` + `autolearn/managed-skills.ts` → 0 hits. No frontmatter gate.

**PARTIAL — prime-agent.** `prime-agent-runtime/src/rlm/harness.py:867` `def create_skill(` → `:880` `return self.create(`, and `:893` `def update_skill(`. LLM-declared at `crates/pa-core/src/prompts/layers/core.md:99` `` - `rlm.harness.create_skill(title: str, content: str, …` ``. Not SKILL.md (persists a JSON blob, `harness.py:546` `json.dump(data, f, indent=2, ensure_ascii=False)`) and no patch (full `content` replacement).

**PARTIAL (gate only) — atomic-agent.** `atomic-agent/src/skills/skill-manifest.ts:123-126` `const requiresTools = normaliseStringList(` / `obj.requires_tools,`. **Never enforced** — the only consumer is a display route, `src/http/route-skills.ts:46` `requiresTools: r.manifest.requiresTools,`; no registry comparison exists anywhere.

**NO — opencode.** `opencode/packages/opencode/src/tool/skill.ts:8-9` is read-only (a single `name` param). No authoring tool.

**NO — duckagent.** `src/capabilities/registry.rs:627-628` registers only `"load_skill",` / `"read_skill_file",`. Its 5 `write_skill` hits are a test helper (`src/skills.rs:298`).

**Absence method (gate):** `grep -rInE 'requires-tools|required-tools|required_tools|allowed-tools|allowed_tools|allowed-toolsets|requires_tools|requires-toolsets' . --include=*.go --include=*.py --include=*.ts --include=*.tsx --include=*.rs --include=*.js --include=*.mjs` over all seven trees (node_modules/vendor/tests excluded) → **0 non-test hits** in duckagent, prime-agent, opencode. Same command for the authoring-tool pattern → 0 in frontier-agent, atomic-agent, opencode.

**frontier-agent is UNVERIFIABLE here, not absent:** skills are a 9-line shim to an external package (`frontier_agent/components/skills/file_system_loader.py:7` `import agent_core.components.skills.file_system_loader as _implementation`, dep `pyproject.toml:11` `"apodex-agent-core==0.12.2",`); `find . -type d -name agent_core` → no hits in the clone.

### (c) Verdict — **PARITY (~)**

Hermes implements both halves (dedicated create+patch tool with old_string semantics, *and* a live `requires_tools` availability gate), at parity with meept's two-tool split. Meept's own edge is a **tier immutability boundary** (system-tier skills refuse agent edits; the agent must create a user-tier shadow) that hermes has no equivalent of, and a **security-engine gate** (`RiskHigh` + confirmation) rather than a config flag. oh-my-pi and prime-agent cover creation only; atomic-agent parses the gate but never enforces it. **The linked-asset-package sub-claim is false against source** (no shipped skill has asset files) — drop it from the matrix.

---

## CLUSTER 7 — per-provider/per-agent token accounting + tokscale export

### (a) Meept side

**Schema — default-on, no flag.** `internal/metrics/store.go:245-261`:

> `CREATE TABLE IF NOT EXISTS llm_calls (`
> `    id              INTEGER PRIMARY KEY AUTOINCREMENT,`
> `    timestamp       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),`
> `    provider        TEXT NOT NULL DEFAULT '',`
> `    model_id        TEXT NOT NULL DEFAULT '',`
> `    agent_id        TEXT NOT NULL DEFAULT '',`
> `    session_id      TEXT NOT NULL DEFAULT '',`
> `    tokens_sent     INTEGER NOT NULL DEFAULT 0,`
> `    tokens_received INTEGER NOT NULL DEFAULT 0,`
> `    tokens_cached   INTEGER NOT NULL DEFAULT 0,`
> `    reasoning_tokens INTEGER NOT NULL DEFAULT 0,`
> `    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,`

So: `provider` + `model_id` (per-provider/per-model), `agent_id` (**per-agent identity**, not just session), `session_id`, and **reasoning** and **cache-creation** as distinct columns from prompt/completion/cache-read. This answers the key question affirmatively.

**Write path.** `internal/metrics/store.go:727-734`:

> `INSERT INTO llm_calls`
> `			(timestamp, provider, model_id, agent_id, session_id, tokens_sent,`
> `			 tokens_received, tokens_cached, reasoning_tokens, cache_creation_tokens,`

plus an hour-bucketed rollup keyed on agent identity, `store.go:745-748`:

> `// Rollup into model_performance (hour-bucketed, per model+provider+agent)`

`ON CONFLICT(model_id, provider, agent_id, period_start)` at `:755`.

**Reachable:** `GET /api/v1/metrics/live`, `internal/comm/http/server.go:1012` `// GET /api/v1/metrics/live.` Response shape `internal/comm/http/live_metrics_usage.go:29-33`:

> `Models []metrics.ModelUsage \`json:"models"\``
> `Agents []metrics.AgentUsage  \`json:"agents"\``
> `Totals metrics.UsageTotals  \`json:"totals"\``

Per-agent query `internal/metrics/usage.go:96-99`:

> `// AgentUsageSince reports every known agent with its task outcome totals and`
> `// last known state. "Known agents" is the union of agents seen in llm_calls`

**Retention exemption.** `internal/metrics/store.go:590-593`:

> `// llm_calls is exempt from time-based retention: it is tokscale's`
> `// ingest source (docs/plans/20260906-tokscale-ingest/master.md,`
> `// Contract D) and must persist year-scale history for aggregation.`
> `retentionTables := []string{"events", "error_records", "dispatch_log", "response_quality", "lint_runs", "test_runs"}`

**tokscale export — reachable but NOT a live endpoint.** The read surface is the SQLite file itself, with the consumer-side aggregation SQL pinned by a test: `internal/metrics/tokscale_ingest_test.go:63-65` `SUM(tokens_cached) AS cache_read_tokens,` / `SUM(cache_creation_tokens) AS cache_write_tokens,` / `SUM(reasoning_tokens) AS reasoning_tokens`. `grep -rn "tokscale" internal/comm/http/*.go cmd/` → **0 hits**: there is no HTTP/CLI exporter in-product; tokscale reads `metrics.db` out of band.

### (b) Competitors

**Per-agent attribution + all fields — hermes MATCH.**
`hermes/agent/usage_pricing.py:66-71` `class CanonicalUsage:` / `input_tokens` / `output_tokens` / `cache_read_tokens` / `cache_write_tokens` / `reasoning_tokens`.
Per-subagent-id attribution `hermes/tools/delegate_tool_child_run.py:1028-1030`:

> `"input_tokens": _num(getattr(child, "session_prompt_tokens", 0)),`
> `"output_tokens": …`
> `"reasoning_tokens": _num(getattr(child, "session_reasoning_tokens", 0)),`

identity `tui_gateway/contracts/events.py:485-486` `subagent_id: str | None = None` / `parent_id: str | None = None`.
Durable store `hermes/hermes_state_schema.py:96` `PRIMARY KEY (session_id, model, billing_provider, billing_base_url, billing_mode, task)`; additive upsert `hermes_state_usage.go:59` `ON CONFLICT(session_id, model, billing_provider, billing_base_url, billing_mode, task)` / `:66` `reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,`.

**prime-agent** per-child-id attribution `crates/pa-types/src/session.rs:269-271` `pub struct ChildUsageAttributionEntry {` / `pub target_id: String,`; cache fields `crates/pa-types/src/ai/mod.rs:464-470` `pub struct Usage {` / `pub input: u64,` / `pub output: u64,` / `pub cache_read: u64,` / `pub cache_write: u64,` — **no reasoning field** (full struct 464-474; its `reasoning_tokens` hit at `crates/pa-ai/src/providers/openai_codex_responses/mod.rs:986` is the literal `0`).

**opencode** reasoning+cache as distinct fields `packages/opencode/src/session/session.ts:185-192` `reasoning: Schema.Finite,` / `const EmptyTokens = { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } }` — but the record site joins model+session only, `packages/opencode/src/session/processor.ts:452-456` `const usage = Session.getUsage({ model: ctx.model, usage: value.usage ?? new Usage({}), metadata: value.providerMetadata, })`; the session row's `agent` column (`session.ts:237`) is display-only.

**oh-my-pi** fields `packages/catalog/src/types.ts:144-176` `reasoningTokens?: number;` — attribution is role-class only, `packages/stats/src/shared-types.ts:173` `export type AgentType = "main" | "subagent" | "advisor";` derived from path depth `packages/stats/src/db.ts:53` `return rel.split(path.sep).length <= 2 ? "main" : "subagent";`.

**NO — atomic-agent** (buckets keyed by session, `src/analytics/turn-usage-meter.ts:60` `private readonly buckets = new Map<string, TurnBucket>();`, docstring `:50` `Buckets are keyed by session.`); **NO — frontier-agent** (`apodex/usage.py:170-174` `class Usage:` with no agent field); **NO — duckagent**.

**Absence method:** `grep -rInE 'reasoning_tokens|cache_creation_input_tokens|cache_read_input_tokens|cache_write_tokens|cached_tokens' . --include=*.rs` over duckagent (vendor excluded) → **0 hits**; its only `reasoning` member is thinking *text*, `src/llm.rs:92` `reasoning: String,`.

**External export — universal absence.** `grep -ril 'tokscale' .` → **0 hits in all seven clones.** `grep -ril 'genai|gen_ai'` → the only real exporter is oh-my-pi, `packages/agent/src/telemetry.ts:103-104` `UsageCacheCreationInputTokens = "gen_ai.usage.cache_creation.input_tokens",` / `UsageReasoningOutputTokens = "gen_ai.usage.reasoning.output_tokens",` stamped at `:1250-1252`. `grep -rInE 'cost.{0,20}export' .` → **0 hits in all seven.**

### (c) Verdict — **PARITY (~)**, with one NOVEL slice

Field-level accounting is at parity: hermes matches all five fields *and* per-subagent attribution, prime-agent matches per-child-id but lacks reasoning. Meept's genuine edge is narrower than the cluster name suggests:

- **NOVEL:** meept is the only clone that pins `llm_calls` as an **exempt-from-retention, year-scale ingest surface for an external token-accounting tool** (`store.go:590-592`), and it ships the consumer-side aggregation SQL as a test (`tokscale_ingest_test.go:63-65`). No competitor has anything comparable — 0 `tokscale` hits, 0 `cost…export` hits. A competitor would have to (a) exempt its call ledger from pruning, (b) document the schema as a stable external contract, and (c) publish the aggregation SQL.
- Note the export is **out-of-band file**, not a shipped HTTP/CLI exporter (`grep -rn tokscale internal/comm/http cmd/` → 0). Don't claim a user-facing tokscale command.

---

## CLUSTER 8 — daemon lifecycle: orphan sweep + local runtime supervision

### (a) Meept side

**Parent-death supervision — DEFAULT-ON.** `internal/llm/runtime_config.go:37`:

> `Supervise     *bool               \`json:"supervise,omitempty"\` // Spawn under the supervisor; absent means true`

and `runtime_config.go:58-59`:

> `// SuperviseOrDefault reports whether this runtime should be spawned under the`
> `func (c RuntimeLifecycleConfig) SuperviseOrDefault() bool {`
> `	return c.Supervise == nil || *c.Supervise`

macOS has no PDEATHSIG, so it uses a pipe + pid poll: `internal/llm/supervisor.go:35` `//	meept-daemon --supervise-parent <daemon pid> --pid-report-fd <fd> -- <spawn argv>`, flag `supervisor.go:62` `supervisorParentFlag = "--supervise-parent"`. The runtime keeps its original argv+pid so the sweep still matches it (`internal/daemon/AGENTS.md`: "The runtime keeps its original argv and pid: the sweep's command-line match and the pid-file ownership token still identify it, never the wrapper").

**Startup orphan sweep — DEFAULT-ON, unconditional.** `internal/daemon/daemon.go:1905` `d.StartupOrphanSweep()`. Body `internal/daemon/orphan.go:48-52`:

> `// StartupOrphanSweep reaps runtime processes left behind by a dead meept`
> `// generation. Best-effort: failures are logged, never fatal, and the sweep is`
> `// a no-op when no runtime manager exists on this daemon.`

The ppid==1 rule: `internal/llm/runtime_sweep.go:114` `// an orphan when its parent is init (ppid==1, so the meept process that spawned` — enforced at `runtime_sweep.go:158,169,612,715,749` (`if p.PPID != 1 || !matchesSpawnCommand(...)`). Ownership veto `runtime_sweep.go:322` `func RuntimeHasLiveOwner(...) bool`. Re-validated immediately before signalling (`internal/daemon/AGENTS.md`: "Every pid is re-validated against the process table immediately before it is signalled").

**Healthy-vs-dead distinction — DEFAULT-ON.** `internal/llm/runtime_manager.go:182-184`:

> `// Bind the checker to the spawned process: an endpoint answered by a`
> `// foreign listener must not read as healthy for a dead child.`
> `hc.SetProcessAliveProbe(proc.IsRunning)`

`internal/llm/health_checker.go:109-114`:

> `// SetProcessAliveProbe binds the checker to the process the endpoint belongs`
> `// to. Without a probe, a foreign listener on the endpoint port answers /health`
> `// with 200 and the runtime is reported healthy forever, even when the child we`
> `// spawned died at bind time. With a probe, a dead child is unhealthy no matter`
> `// what the endpoint says.`

**No-spawn-into-served-endpoint guard** (unique; not found in any competitor): `internal/daemon/AGENTS.md` — "`RuntimeProcess.Start` probes the endpoint address when `spawn_command` declares that port and refuses when something already listens there… `mlx_lm server` does not exit after a failed bind".

### (b) Competitors

**MATCH — hermes, four mechanisms.** Dedicated supervisor child with pipe-EOF death detection, `hermes/tools/mcp_death_supervisor.py:23-25`:

> `* **Death detection is a blocking read on a pipe.**  Hermes holds the only write`
> `  end.  When Hermes dies -- by any means, including SIGKILL -- the write end`
> `  closes and the read returns EOF.  Exact, instant, and free.`

Set-new-session so a parent killpg can't kill it first, `:192-199` `own_pgid = os.getpgid(0)` / `if own_pgid == args.parent_pgid:`. Reap is TERM→grace→KILL by pgid, `:118,130,136` `os.killpg(pgid, signal.SIGKILL)  # windows-footgun: ok — POSIX-only`. Second parent-death pipe for kernel children, `hermes/tools/code_kernel.py:101-102`:

> `outlived its host. Not PR_SET_PDEATHSIG — that is bound to the spawning`
> `THREAD, and kernels are spawned from per-cell threads that exit.`

Wired at spawn `hermes/tools/mcp_tool_transport.py:329` `_core._update_death_supervisor("register", new_pgids.values())`.

**MATCH — atomic-agent, and its reaper is arguably the better design.** Self-terminating poll `atomic-agent/src/cli/serve-orphan-guard.ts:93-94`:

> `const reparented = watchesOwnParent && process.ppid !== bootPpid;`
> `const orphaned = reparented || !isAlive(watched);`

Boot sweep `atomic-agent/src/cli/serve-reaper.ts:195-224` ANDs dead-boot-parent + live pid + `/health` identity + reparent proof + idle — including the exactly-correct refusal to treat `ppid==1` as evidence of death, `atomic-agent/src/local-llm/port-reclaim.ts:31-32`:

> ` * Parentless is not abandoned: a managed llama-server is spawned`
> ` * detached on purpose, so a `ppid` of 1 is what a healthy daemon looks`

**MATCH (narrow) — prime-agent.** `prime-agent-runtime/src/rlm/repl.py:1574-1578`:

> `def _owner_alive_posix(owner: int, initial_ppid: int) -> bool:`
> `    # Reparenting is the race-free parent-death signal when the owner is the`
> `    # parent; the kill-0 probe covers an env-designated non-parent owner.`
> `    if initial_ppid == owner and os.getppid() != initial_ppid:`
> `        return False`

kqueue/pidfd at `:1600` `select.KQ_FILTER_PROC, select.KQ_NOTE_EXIT`. Scope limit: the RLM kernel worker only.

**PARTIAL — oh-my-pi, Linux-gated.** `oh-my-pi/packages/utils/src/ptree.ts:51-52`:

> `if (libc.symbols.prctl(36, 1, 0, 0, 0) !== 0) {`
> `	throw new Error("failed to become a Linux child subreaper");`

Self-declared gap at `ptree.ts:622-628` and gate `ptree.ts:638` `const useSubreaper = subreaper && process.platform === "linux";`. Plus a ppid poll in the eval worker `packages/coding-agent/src/eval/py/runner.py:2074-2075` `if os.getppid() != original_ppid:`.

**PARTIAL — duckagent, sandbox bridge only.** `duckagent/src/sandbox/backends/linux_proxy_routing.rs:620-628`:

> `let res = unsafe { libc::prctl(libc::PR_SET_PDEATHSIG, libc::SIGTERM) };`
> `… else if unsafe { libc::getppid() } == 1 {`
> `    Err(io::Error::other("parent process already exited"))`

**NONE — frontier-agent, opencode.** `rg -n -i -e 'getppid|ppid|parent.death|parent_death|pdeathsig|subreaper' -g '!*.lock' -g '!.git' -g '!**/vendor/**' frontier-agent` → **1 hit**, a comment at `plugins/tools/_sandbox.py:569`. Same command on `opencode` → only `appId`/`sidebarOpen` substrings, CSS `orphans: 3`; `packages/core/src/pty.ts:268` has only `detached: false`. frontier-agent's `_exec_cgroup.py:148-149` killpg-reaches-setsid is killing *its own* children, not reaping a stranded runtime.

**Health-vs-liveness AND:** only hermes (`hermes_platform/resolver/app.py:134-139` keeps `running` and `answering` separate; `_pid_alive` at `:266-268` is `os.kill(pid, 0)`) and atomic-agent (`src/local-llm/daemon-lifecycle.ts:1205-1208` `healthy: pid !== null && h === "ok",`). opencode PARTIAL (`packages/desktop/src/main/server.ts:144-163` races an exit event, no liveness query). frontier-agent PARTIAL (`scripts/run-sglang-native.py:802-810` returns `0 if healthy else 1` — a dead process with a foreign listener exits 0). oh-my-pi and duckagent NONE (`packages/metaharness/src/runner.ts:1323-1324` curl-only; `duckagent/src/gateway/mod.rs:1993` hardcodes `{"status": "ok"}`).

### (c) Verdict — **PARITY (~)**

Four of seven do real parent-death supervision and two additionally reap already-stranded orphans. Meept's mechanism (supervisor child + parent-death pipe + 2s pid poll, absent-flag-means-true) is behaviourally equivalent to hermes' pipe-EOF supervisor. **One NOVEL sub-item:** the refuse-to-spawn-into-a-served-endpoint guard is unshared — it exists because `mlx_lm server` survives a failed bind and answers nothing. A competitor would have to pre-probe the declared port at spawn time and abort, rather than trusting a healthy-looking process.

---

## CLUSTER 9 — anomaly detection on the live streams

### (a) Meept side — **this cluster's defaults are the problem**

**Tool-failure burst detector — OFF by default.** `config/meept.json5:721-726`:

> `"burst_detection": {`
> `      "enabled": false,`
> `      "window_size": 0,       // 0 → built-in default (20 turns)`
> `      "threshold": 0,         // 0 → built-in default`
> `      "log_only": true,`

The comment states the reason, `config/meept.json5:716-720`:

> `// Tool-failure burst detector (issue #43): watches the per-session`
> `// dispatch outcome stream (ok/corrected/failed_replan) and signals`
> `// when the failure pattern shifts. Default OFF, log-only. Blocked on`
> `// real outcome data from the outcome loop (#40); compare against a`
> `// simple "N failures in a row" threshold rule before adopting.`

Mechanism is genuine temporal adaptation, `internal/metrics/burst_detector.go:51-58`:

> `// Per session, two exponentially-weighted means run over that signal:`
> `//`
> `//	ewmaFast = ewmaFast*(1-aFast) + s*aFast   (aFast = 0.40)`
> `//	ewmaSlow = ewmaSlow*(1-aSlow) + s*aSlow   (aSlow = 0.02)`
> `//`
> `// The burst score is their difference, clamped to [0,1]:`
> `//`
> `//	score = clamp(ewmaFast - ewmaSlow, 0, 1)`

Wired log-only at the dispatcher, `internal/agent/dispatcher.go:4486-4491` (`if resolvedOutcome != "" && d.burstDetector != nil && d.burstDetector.Enabled() {`), and `dispatcher.go:4259-4260` `// The dispatcher feeds it resolved dispatch outcomes in recordDispatch; the signal is` / `// LOG-ONLY at that call site.`

**Session drift detector — OFF by default, log-only, never acts.** `config/meept.json5:710-715` `"session_drift": { "enabled": false, … "log_only": true, }`. Mechanism (asymmetric baseline/current split on intent embeddings), `internal/agent/session_drift.go:29-43`:

> `//	shift = 1 - cos(mean(B), mean(C))     (how far the stream moved)`
> `//	rate  = 1 - cos(prevMean(B), mean(B)) (how fast the baseline is`
> `//	      itself moving; …`
> `//	score = clamp01(shift + 0.5 * rate)`

The "log-only, not acting" contract is explicit at the call site, `internal/agent/dispatcher.go:4230-4232`:

> `// criterion 3): a drift event logs one Info line and never changes`
> `// routing — the full-chain fall-through decision belongs to a later,`
> `// measured rollout.`

and `dispatcher.go:4245-4246` `// Log-only signal (issue #41): session id and` / `// numbers only, never message text.`

**Embedding-pipeline cosine health gate — ON by default (the one default-on piece).** `config/meept.json5:691-692` `"embed_health_check": { "enabled": true, "reference_path": "", }`. Calibrated threshold, `internal/agent/embed_health.go:127-128` `// calibrate computes the threshold: the pct percentile (0..1) of` / `// cross-fitted nearest-neighbour cosine distances within the reference set.`, verdict `embed_health.go:231` `return best <= c.threshold, best`. Fails safe — `internal/config/schema.go:2664-2666`:

> `// Unhealthy => the prefilter abstains and the`
> `// full LLM chain runs (fail safe: the check can only skip prefilter work,`
> `// never enable a confident route on garbage).`

**Summary: 1 of 3 default-on; the 2 that matter are off, and all 3 are log-only.**

### (b) Competitors

**prime-agent has the only true temporal detector.** `prime-agent/crates/pa-types/src/incident/anomaly.rs:53-66`:

> `fn densest_window_run(items: &[IncidentEvent], window_ms: i64) -> (usize, usize) {`
> `    let mut best_start = 0;`
> `    let mut best_count = 0;`
> `    let mut start = 0;`
> `    for end in 0..items.len() {`
> `        while items[end].time_ms - items[start].time_ms > window_ms {`
> `            start += 1;`
> `        }`

Thresholded use `anomaly.rs:124-129`:

> `if burst.len() >= ERROR_BURST_THRESHOLD {`
> `    // Only events within `ERROR_BURST_WINDOW_MS` of each other`
> `    // form a burst; three isolated warnings days apart in a long`
> `    // window are not one.`
> `    let (start, count) = densest_window_run(&burst, ERROR_BURST_WINDOW_MS);`

Constants `crates/pa-types/src/incident/mod.rs:139,146` `ERROR_BURST_THRESHOLD = 3`, `ERROR_BURST_WINDOW_MS = 10 * 60 * 1000`.

**hermes — consecutive counters, no baseline.** `hermes/agent/tool_guardrails.py:126-127` `same_tool_failure_warn_after: int = 3` / `same_tool_failure_halt_after: int = 8`; cron streak `hermes/cron/scheduler.py:164-166` `streak = int(job.get("failure_streak") or 0) + 1  # +1 = this run`.

**atomic-agent — static cosine gate, not temporal.** `atomic-agent/src/memory/retrieve/embedding-gate.ts:65-74` `let score = cosineSimilarity(unit, ref);` / `if (maxScore >= threshold) {`, threshold default 0.65 (`:21-22`) — per-message against a frozen exemplar corpus, **no baseline over time, no drift**.

**Rejected as matches:** atomic-agent's rolling mean is timeout-sizing only (`src/llm/llama-server-client.ts:457` `const THROUGHPUT_WINDOW = 8;` at `:585-587`, no deviation test); oh-my-pi's is text-segment Jaccard/novelty not embeddings (`packages/ai/src/utils/thinking-loop.ts:236` `if (jaccard(fingerprint, prev) >= SEGMENT_SIMILARITY) cluster++;`); duckagent's counter has no consumer (`src/cron/store.rs:279-281` increments `consecutive_failures`, no thresholding anywhere); frontier-agent's is turn-composition (`workflows/agent_team/observers/no_progress_guard.py:57-58` `soft_streak=6, hard_streak=12`).

**Absence method (embedding drift / cosine gate):** `rg -n -i -e 'cosine|cosine_similarity|cosineDistance|cosine_distance|embedding.drift|semantic.drift' -g '!*.lock' -g '!.git' -g '!**/vendor/**' -g '!**/tests/**' -g '!**/test/**' .` across all seven → only atomic-agent's static gate, oh-my-pi vector-math retrieval (no gate), hermes recall scoring, and prose. **Zero hits anywhere for `CUSUM`, `EWMA`, `z-score`, `standard deviation`, `isolation forest`.**

### (c) Verdict — **WEAKER (-)**

Meept's *algorithms* are the best in the set — a two-rate EWMA adaptation baseline beats prime-agent's fixed sliding window and hermes' bare consecutive counters, and the embedding drift detector (asymmetric baseline/current split + baseline-rate term) and the fail-safe cosine gate have **no analog in any of the seven**. But the cluster as shipped scores **0 for default-on**: `burst_detection.enabled=false`, `session_drift.enabled=false`, and all detectors are log-only by construction (`dispatcher.go:4231` "never changes routing"). Meanwhile prime-agent ships windowed burst detection unconditionally over its own incident log. **A competitor would have to add an enabled-by-default temporal detector whose signal feeds a routing or model-switch decision**, not just log a score. If the matrix needs an upside row here, the honest one is: *meept has the drift/cosine detectors nobody else has at all, but ships them off.*

---

## CLUSTER 10 — GUI/CLI breadth: config R/W + per-model usage

### (a) Meept side

**Loopback-only main config read AND write — default-on, no flag.** Routes `internal/comm/http/server.go:1332-1333`:

> `mux.HandleFunc("GET /api/v1/config/main", s.handleGetMainConfig)`
> `mux.HandleFunc("POST /api/v1/config/main", s.handleSaveMainConfig)`

Write gate `internal/comm/http/main_config_handlers.go:100-102`:

> `if !isLoopbackRequest(r) {`
> `	s.writeError(w, http.StatusForbidden, "config write is restricted to loopback clients")`

Read is loopback-only too, and the reason is credential exposure — `main_config_handlers.go:92-94`:

> `// Writes are accepted only from loopback clients — and so are reads, for the`
> `// same reason (see handleGetMainConfig): the file carries transport API keys,`
> `// so gating only the write left the whole credential set readable.`

Validate-before-write `:112-115` `// Validate before touching the filesystem so a bad body cannot clobber` / `// the config.` / `if err := configCli.ValidateMainConfigJSON5(body.Content); err != nil {`, plus a `.bak` + atomic rename, `main_config_handlers.go:89-91`:

> `// The submitted text is validated as JSON5 before anything is written; the`
> `// previous content is copied to <path>.bak and the new content replaces the`
> `// file atomically (temp file + rename) preserving the existing mode.`

And a last-moment staleness guard in the GUI, `ui/flutter_ui/lib/features/settings/main_config_editor.dart:246-251`:

> `/// Last-moment guard before the POST: the daemon's copy must still hold the`
> `/// CLI) that lands between that read and this POST is still reverted by it.`

**GUI writes the whole file back.** `main_config_editor.dart:24-25`:

> `/// Loads via GET /api/v1/config/main and writes the whole file back with`
> `/// POST /api/v1/config/main. The daemon validates the text as JSON5 and`

with sibling editors `orchestrator_config_editor.dart`, `client_prefs_editor.dart`, `users_panel.dart`.

**GUI metrics panel with per-model AND per-agent usage.** `ui/flutter_ui/lib/features/metrics/metrics_panel.dart:10`:

> `/// (new) per-model / per-agent usage from GET /api/v1/metrics/live.`

rendered at `:334` `final models = state.modelUsage;` and `:369` `final agents = state.agentUsage;` → `:382` `for (final a in agents)`.

**CLI `config get`/`config set` + interactive editor** (per `AGENTS.md` Commands) and **`meept chat --oneshot --await`** default-waits: `cmd/meept/chat.go:53-58`:

> `// --await controls the oneshot/session (non-TUI) wait behavior:`
> `//   "wait" (default): submit via chat.submit and await the real`
> `//     turn.terminal result — long tasks print their genuine reply.`
> `cmd.Flags().StringVar(&chatAwait, "await", "wait", ...)`

**TUI async chat with live liveness** — `internal/tui/app.go:1571` `// Async turn terminal surface (async-turn-migration` and `internal/tui/rpc.go:1432` `// SubmitChat fires the fire-and-forget "chat.submit" RPC (async-turn-`.

### (b) Competitors

**Config read+write with write-back proven:**
- **hermes** — JSON-RPC `config.set` `hermes/tui_gateway/methods_config_set.py:489-491` `@method("config.set")`, HTTP `PUT /api/config` `hermes/hermes_cli/web_routers/config_env.py:117-118`, persist `hermes_cli/config.py:2548-2550` `atomic_config_replace(config_path, normalized, …)`, GUI caller `hermes/apps/desktop/src/app/settings/config-settings.tsx:229` `const result = await saveHermesConfig(patch, writeScope ?? scopeProfile);`
- **opencode** — `PATCH /global/config` `opencode/packages/opencode/src/server/routes/instance/httpapi/groups/global.ts:106` `HttpApiEndpoint.patch("configUpdate", GlobalPaths.config, {`, disk write `packages/opencode/src/config/config.ts:670` `if (changed) yield* fs.writeFileString(file, serialized).pipe(Effect.orDie)`, GUI `packages/app/src/components/settings-general.tsx:341` `serverSync().updateConfig({ shell: option.value })`.
- **atomic-agent** — `PATCH /api/config` `atomic-agent/src/http/route-table.ts:86-90`, write-back `src/http/route-config.ts:83-85` `writeRawUserConfigFileSync(path, tree);` / `resetConfigCache();`, auth-gated `src/http/http-server.ts:217`.
- **oh-my-pi / prime-agent** — TUI panels only (`packages/tui/src/overlays/settings-selector.ts:1430-1436`; `crates/pa-tui/src/session_ui/settings.rs:349`), **no HTTP**.
- **frontier-agent / duckagent** — wizards only (`duckagent/src/profiles.rs:61-68` `pub fn set_active_profile_name(name: &str) -> Result<()> {`).

**Per-model usage RENDERED (4):** hermes `hermes_cli/web_routers/analytics.py:104` `GROUP BY model ORDER BY SUM(input_tokens) + SUM(output_tokens) DESC` → `web/src/pages/AnalyticsPage.tsx:581` `<ModelTable models={data.by_model} />`; opencode `packages/opencode/src/cli/cmd/stats.ts:185` `const modelKey = \`${message.info.providerID}/${message.info.modelID}\`` → `:337-347`; oh-my-pi `packages/stats/src/aggregator.ts:508` `byModel: getStatsByModel(window.cutoff),` → `client/routes/CostsRoute.tsx:159-160` `title="By model"`; prime-agent `crates/pa-tui/src/info_commands/context_tree.rs:646` `dim(format!("{}/{}:", bucket.provider, bucket.id)),`.

**Per-agent usage RENDERED — exactly 1 clone:** prime-agent, `crates/pa-tui/src/info_commands/context_tree.rs:507` `let mut header = format!("  {}", pad_end("agent", label_width));`. (PARTIAL — collapses to `"{} more agents"` over a row budget, `:431`.)

**Absence method:** `rg -c -i 'by_agent|byAgent|usageByAgent|usage_by_agent' -g '!node_modules' -g '!target' -g '!dist' -g '!.git' -g '!vendor' -g '!*.lock' -g '!*.md' .` in each clone → hermes **0**, opencode **0**, frontier-agent **0**, duckagent **0**; atomic-agent's 1 hit is an offline eval script (`eval-agents/scripts/scorecard.mjs:92`); oh-my-pi's 6 are `byAgentType` only (`packages/stats/src/db.ts:261-268` `ALTER TABLE messages ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'main'`). duckagent persists **no** usage at all — `src/gateway/mod.rs:2183` `"usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},` is a hardcoded zero.

### (c) Verdict — **PARITY (~)**

Both halves are matched: hermes/opencode/atomic-agent all ship read-write config APIs with GUI callers, and four clones render per-model usage. Meept's edges are **security design, not capability**: it is the only one gating the *read* of the main config on loopback for a stated credential reason (`main_config_handlers.go:92-94`), the only one validating JSON5 + `.bak` + atomic rename before write (`:89-115`), and the only one with a GUI-side staleness guard against a concurrent CLI edit (`main_config_editor.dart:246-251`). Per-agent usage: meept and prime-agent both render it — meept is a tie, not a win. Note meept's TUI has no metrics panel; the GUI does.

---

## CLUSTER 11 — thinking-safe summarization + local-grammar default

### (a) Meept side

**DisableThinking is a ChatOption, not a config flag — so it is always on at the call sites.** `internal/llm/client.go:1304-1315`:

> `// DisableThinking returns a ChatOption that explicitly disables`
> `// reasoning/thinking for the request. It is the shared seam for small,`
> `// fixed-prompt machine-parsed calls (intent classification,`
> `// session/task summarization): a thinking model would otherwise burn`
> `// the output budget on chain-of-thought and leak reasoning into the`
> `// parsed result. … The wire field remains subject to capability gating`
> `// (shouldSendReasoning), so pair with output-side stripping`
> `// (stripThinking) for defense in depth.`
> `func DisableThinking() ChatOption {`
> `	noThinking := false`
> `	return WithReasoning(&ReasoningConfig{Enabled: &noThinking})`

**Four call sites, all utility/short-extractive.** `internal/llm/task_summarizer.go:127,147,166` `resp, err := s.chatter.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: prompt}}, DisableThinking())`, plus `internal/daemon/epistemic_wiring.go:111`:

> `}, llm.WithTemperature(0.2), llm.WithRawGrammar(llm.AmbientCandidateGrammar()), llm.DisableThinking())`

**Ambient thinking off** — the same wiring line pairs `DisableThinking()` with the grammar, and `internal/agent/skill_state.go:503` pairs `stateGBNFGrammar`.

**Raw grammar default-on for local endpoints, no flag.** `internal/llm/client.go:1392-1404`:

> `// attachRawGrammar attaches a caller-supplied grammar to the payload. The`
> `// caller opted in explicitly via WithRawGrammar and owns the grammar body —`
> `// no config surface. The only gate is the endpoint: the "grammar" wire field`
> `// is a llama.cpp extension, so it is attached only when the resolved endpoint`
> `// is local (loopback). Cloud providers must never see the field.`
> `func attachRawGrammar(payload map[string]any, cfg *ModelConfig, chatOpts *chatOptions) {`
> `	if chatOpts.rawGrammar == "" {`
> `		return`
> `	}`
> `	if !isLocalEndpoint(cfg.BaseURL) {`
> `		return`
> `	}`
> `	payload["grammar"] = chatOpts.rawGrammar`

(The separate *tool-call* GBNF switch is default-false — `internal/llm/client.go:1327-1328` `// GBNFConstrained is the global kill-switch for grammar-constrained tool` / `// calling ([agent.tools] gbnf_constrained). Default FALSE` — do not conflate the two.)

**LessonGrammar + distill wiring.** `internal/llm/gbnf.go:454` `func LessonGrammar() string {`; `internal/memory/distill.go:620` `grammar := llm.LessonGrammar()`, `:627` `grammar = llm.ProcedureGrammar()`, `:632`:

> `}, llm.WithMaxTokens(500), llm.WithTemperature(0.2), llm.WithRawGrammar(grammar))`

**Thinking-tag stripping in the memory-eval harness.** `tools/memory-eval/grade.go:410-413`:

> `// stripThink removes LFM2.5's (possibly empty) <think>...</think> block so the`
> `	if i := strings.Index(s, "<think>"); i >= 0 {`

### (b) Competitors

**oh-my-pi — strongest, 8 distinct sites, honored on the wire.** `oh-my-pi/packages/coding-agent/src/utils/title-generator.ts:348` `disableReasoning: true,` with the extraction rationale at `:353-354`:

> `// Greedy decode: titling is extraction, not generation. Backends that`
> `// default temperature high (e.g. Ollama's 0.8) otherwise garble names`

Others: `extensibility/skill-descriptions.ts:69`, `custom-commands/bundled/annotate/text-summary.ts:28`, `packages/ai/src/judgment/chat.ts:64`, `edit/auto-repair.ts:331`, `cli/dry-balance-cli.ts:433`; local backends `coding-agent/src/tiny/worker.ts:238` `enableThinking: false,` and `tiny/mlx-server.py:195` `enable_thinking=False,`. Wire encoding `packages/ai/src/providers/factory-droid.ts:234` `if (options?.disableReasoning || options?.forceReasoningOff) return { effort: undefined, disabled: true };`

**hermes — one hard-coded disable, with the bug it fixes named.** `hermes/agent/title_generator.py:530` `reasoning_config={"enabled": False},` and `:522-529`:

> `# The module contract above promises thinking-disabled operation,`
> `# but nothing enforced it: with the aux default reasoning_effort`
> `# "" (provider default), Gemini enables internal thinking and`
> `# bills thought tokens against max_tokens=64 — the JSON payload`

Provider encodings `hermes/agent/anthropic_adapter.py:608` `return {"thinking": {"type": "disabled"}} if _accepts_thinking_disable(model) else {}`.

**frontier-agent — 3 sites.** `frontier_agent/infra/summary_llm.py:251` `"reasoning_effort": "minimal",` and `:261` `payload["chat_template_kwargs"] = {"enable_thinking": False}`, rationale at `:236-239`:

> `Self-hosted reasoning models behind SGLang get`
> ```chat_template_kwargs.enable_thinking=false`` — extraction is an`
> `auxiliary call; with thinking ON (their default) the model burns the`
> `whole completion budget on hidden reasoning and returns`

PARTIAL: the Anthropic aux path accepts `thinking: {type: disabled}` (`infra/llm/aux_builder.py:155-158`) but **no shipped profile sets it**.

**opencode — PARTIAL.** `packages/opencode/src/session/prompt.ts:230` `small: true,` → `packages/opencode/src/provider/transform.ts:1401-1404`:

> `if (model.providerID === "openrouter" || model.providerID === "llmgateway") {`
> `    if (Object.keys(small).length === 0 && model.api.id.includes("google")) {`
> `      return { reasoning: { enabled: false } }`

but `:1412` `return small` for the general case, and compaction gets no flag (`session/compaction.ts:358`).

**atomic-agent — PARTIAL, narrow.** `src/llm/provider/llama-server/llama-server-vision.ts:134` `chat_template_kwargs: { enable_thinking: false },` — one site, one provider. Its own title path pays for reasoning and never stops it (`src/session/session-title.ts:141-145`).

**NO — duckagent (structurally).** `duckagent/src/client/openai_compat.rs:76-79` only *enables* thinking:

> `if policy.deepseek_thinking {`
> `    body["reasoning_effort"] = Value::String("high".to_string());`
> `    body["thinking"] = json!({ "type": "enabled" });`

Its title call site has no channel to pass one through (`src/client.rs:51-66`). Absence grep: `rg -n -i 'disable_thinking|disableThinking|enable_thinking|enableThinking|thinking_budget|no_think|type.*disabled' src/` → exit 1.

**NO — prime-agent.** Summarizer sets max_tokens but no reasoning level (`crates/pa-core/src/session_engine/branch_summarization.rs:443-450`); absence grep `rg -c -e 'reasoning:\s*Some\(ModelThinkingLevel::(Off|Minimal)' --type rust crates prime-agent-runtime skills` → exit 1.

**Think-tag stripping before persistence:** YES hermes (`hermes/agent/chat_completion_helpers.py:1608-1615` `content = agent._strip_think_blocks(content).strip()`) and frontier-agent (`workflows/stateful_react_agent/_runtime.py:466` `content = _strip_leaked_tool_calls(_strip_thinking(text_of(resp.content)))`). opencode PARTIAL — one derived field (`prompt.ts:243-244` `.replace(/[\s\S]*?<\/think>\s*/g, "")` before `setTitle`) but reasoning is otherwise persisted (`session/processor.ts:282-291`).

### (c) Verdict — **PARITY (~)**

The key question is answered **yes** for meept — `DisableThinking()` exists precisely so a reasoning model does not pay reasoning tokens on a short extractive call, and it's applied at three summarizer sites plus one ambient-candidate call, unconditionally (no flag, `client.go:1315-1317`). But the cluster is not novel: **oh-my-pi has 8 such sites to meept's 4**, hermes has the same title-gen disable with a documented rationale, and frontier-agent has 3. Meept's genuine sub-edges: (a) thinking-off is **coupled with a GBNF grammar** at the same call sites (`epistemic_wiring.go:111`, `distill.go:632`) — machine-parsed output is constrained *and* un-reasoned, whereas oh-my-pi's disable sites are prompt-only; (b) defense-in-depth stripping (`grade.go:413`). A competitor would have to bind a grammar to the same call to match that.

---

## Matrix recommendations

| Cluster | Matrix row | Rationale |
|---|---|---|
| 6 | **PARITY** — drop the asset-package claim | hermes matches both halves; no shipped meept skill has asset files |
| 7 | **PARITY** + one NOVEL row (retention-exempt external ingest contract) | field parity w/ hermes; 0 competitor hits for tokscale |
| 8 | **PARITY** + one NOVEL sub-row (refuse-to-spawn-into-served-endpoint) | 4/7 competitors supervise; the port pre-probe is unshared |
| 9 | **WEAKER** (0 default-on) with a NOVEL-if-enabled sub-row (embedding drift + cosine gate) | algorithms best-in-set, but off + log-only vs prime-agent's on-by-default windowed burst |
| 10 | **PARITY** on both halves | 3 competitors ship config R/W APIs; meept wins on write safety, ties per-agent usage |
| 11 | **PARITY** (oh-my-pi 8 sites vs meept 4) + NOVEL sub-row (grammar+thinking-off coupled) | reasoning suppression is industry standard |

**Aggregate: 5 PARITY, 1 WEAKER, 0 clean NOVEL.** The defensible novelty claims are four narrow ones, all verified above: (1) retention-exempt year-scale token ledger designed as an external ingest contract, (2) spawn-time port-conflict refusal for local runtimes, (3) embedding-stream drift detection + fail-safe cosine pipeline gate, (4) grammar-constrained *and* thinking-disabled extraction calls.

### Unverified / caveated
- **frontier-agent** skills, metering, and Anthropic thinking-disable delegate to `apodex-agent-core==0.12.2`, absent from the clone. Absence cannot be claimed there.
- **hermes** documents an accepted pgid-reuse race (`tools/mcp_death_supervisor.py:45-67`, "would signal a stranger") — relevant to the cluster-8 comparison, not to meept.