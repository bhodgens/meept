# Skill System

## Overview
Meept's skill system enables extensibility through modular, discoverable skills. Skills are defined using SKILL.md files with YAML frontmatter and loaded dynamically from multiple discovery tiers.

## Problem
Hardcoded functionality limits adaptability and requires code changes for new capabilities. The skill system provides:
- Modular, reusable functionality
- Multi-tier discovery with priority shadowing
- Model resolution based on capability requirements
- User customization without code changes

## Behavior

### Skill Discovery Hierarchy (Priority Order)
1. **Project-local**: `.meept/skills/` (highest priority)
2. **User-global**: `~/.meept/skills/`
3. **System-wide**: `~/.config/meept/skills/`
When multiple skills have the same name, the highest-priority version wins.

### Bundled skills

`make install` ships the repo's bundled skills (`config/skills/`) to
`~/.meept/skills/` (one directory per skill):

- **No-clobber**: an existing skill directory in `~/.meept/skills/` is
  never overwritten. `make install` prints `skipping <name> (exists)` for
  those and `installed skill <name>` for fresh installs.
- **User edits shadow bundled**: discovery resolves `~/.meept/skills/`
  above the bundled source (and project-local `.meept/skills/` above
  both). Editing or replacing an installed bundled skill is safe —
  reinstalls will not revert it.
- **Uninstall safety**: `make uninstall-all` removes a skill directory
  under `~/.meept/skills/` only when it is byte-identical to the bundled
  copy in `config/skills/`. User-modified or custom skills are listed and
  kept.
- **Re-install a bundled skill**: delete its directory, then re-run
  `make install`:

  ```sh
  rm -rf ~/.meept/skills/web-browsing && make install
  ```

### SKILL.md Format
```markdown
---
name: Code Reviewer
requires: [code, reasoning]
tools: [file_read, memory_search]
triggers: [review, code, check]
---

# Code Reviewer Skill

Review code changes for correctness, style, security, and completeness.

## Usage
When reviewing code, check for:
- Correctness: Does it accomplish the intended goal?
- Style: Follows best practices and conventions
- Security: No vulnerabilities or issues
- Completeness: Error cases handled appropriately
```

### Tool requirements

A skill can declare concrete tool dependencies in frontmatter with the
`requires-tools` key:

```yaml
---
name: Web Shot
requires-tools:
  - web_fetch
  - cua-driver.capture
---
```

Matching semantics: each entry is a full registered tool name. A bare
name (`web_fetch`) must exist in the live builtin tool registry; a
server-qualified name (`cua-driver.capture`) must be exposed by a
registered, enabled MCP server.

Failure behavior: when `skills.validate_prerequisites` is enabled, the
executor checks every entry before running the skill. If any tool is
unavailable, execution is refused with an error naming them:
`skill <name> requires unavailable tool(s): <list> — run 'meept doctor'
to diagnose`. Skills without the key are unaffected — no tool check
runs for them.

### Model Resolution
- Skills declare `requires: [code, reasoning]` in YAML
- Models declare `capabilities: [code, tool_use]` in config
- Resolver finds cheapest model satisfying requirements

### Skill Invocation
1. **Trigger Matching**: Keywords matched against skill triggers
2. **Capability Check**: Agent capabilities verified
3. **Tool Availability**: Required tools checked
4. **Execution**: Skill logic executed with context

## Configuration

```toml
[skills]
enabled = true
search_paths = []
auto_reload = false

```

## Observability

### Logging
- Skill discovery and loading
- Model resolution decisions
- Skill invocation events
- Capability mismatch warnings

### Metrics
- Skill discovery time
- Model resolution latency
- Skill execution success rate
- Capability matching accuracy

### Debug Info
- Available skills per agent
- Model capability mappings
- Skill trigger patterns
- Discovery path resolution

## Edge Cases

### Skill Not Found
- Clear error message indicating missing skill
- Suggests similar available skills
- Logs discovery failure for monitoring

### Capability Mismatch
- Agent lacks required capabilities
- Alternative skills suggested
- Model upgrade recommended

### Tool Unavailable
- Required tools not accessible to agent
- Permission or capability issue
- Alternative approaches suggested

### Discovery Path Conflict
- Multiple versions of same skill
- Highest priority path wins
- Shadowing logged for transparency

## Skill Evolution (Closed-Loop)

Skills are not static. Meept continuously measures how effective each skill is and evolves them based on real usage data.

### Architecture

```
Agent Loop (inject skills into prompt)
    │
    ▼ after turn
UsageTracker (SQLite: inject_count, outcomes)
    │
    ▼ scheduled (6h default)
Evolver (4 passes: refine, promote, prune, fill_gap)
    │
    ▼ each proposal
Verifier (4-dimension LLM rubric gate)
    │
    ▼ accepted
Writer (atomic write) → Versioner (snapshot) → Registry reload
```

### Usage Tracking

Every time a skill is surfaced in the agent prompt, `inject_count` increments. After the turn completes, the learning pipeline's judgment determines the outcome:

- **Positive:** Task succeeded with no retry
- **Negative:** Task failed or required correction
- **Neutral:** Ambiguous outcome

Effectiveness ratio: `positive_count / inject_count`.

### Evolver Cycle

Runs every 6 hours (configurable). Four passes:

| Pass | What it does | Threshold |
|------|-------------|-----------|
| **A: Refine** | LLM-driven improvement of existing skills based on usage evidence | inject_count >= 5 |
| **B: Promote** | Promotes learned patterns to new skills | UseCount >= 5, Confidence >= 0.7, stable >= 14d |
| **C: Prune** | Archives skills that actively hurt | inject_count >= 10, effectiveness < 0.2 |
| **D: Fill Gap** | Proposes new skills for queries that recurred without matching anything | count >= 5, best_score < 0.5 |

Pattern-to-skill promotion checks TF-IDF similarity via `CapabilityIndex.Match` (threshold 0.7) to avoid duplicates. Name collisions are handled by `dedupePatternSkillName` which appends numeric suffixes.

### Pass D: Gap Analysis

Beyond refining, promoting, and pruning skills based on what *exists*, Meept also surfaces what's *missing*. The capability index records every user or skill-discovery query whose best match score fell below 0.5. Queries that recur at least 5 times become new-skill candidates:

```bash
meept skills gaps          # list current low-match queries
meept skills evolve        # run all four passes (A/B/C/D)
```

Pass D proposals pass through the same verifier as the other passes.

### Verifier Gate

Every proposal passes through a 4-dimension LLM rubric before going live:

1. **grounded_in_evidence** — Is the change backed by usage data?
2. **preserves_existing_value** — Does it remove useful capabilities?
3. **specificity_and_reusability** — Is it specific enough to be useful but general enough to reuse?
4. **safe_to_publish** — Any risk of harmful behavior?

Reject if any dimension < 0.5 or average < 0.75 (configurable). Heuristic fallback (all 0.5) when LLM unavailable.

### Versioning

Before any write, the current SKILL.md is snapshotted:

```
<skillsDir>/<name>/versions/v<N>/SKILL.md
<skillsDir>/<name>/versions/v<N>/bundle.json
```

`bundle.json` contains `content_sha` (SHA-256 of content) and `tree_sha256` (SHA-256 over bundle file list). 20-entry cap; oldest pruned. Restore reverts content atomically.

Content-hash deduplication prevents duplicate skills: if a new skill's SHA matches an existing one, the write is skipped.

### Approval Workflow

When `auto_apply = false` (default), proposals go through the plan system:

```bash
./bin/meept plans list              # See pending skill_evolution proposals
./bin/meept plans approve <id>      # Approve a proposal
./bin/meept plans reject <id>       # Reject with reason
```

Evolver-created plans land in the user-scoped sink
`skills.evolver.plan_dir` (default `~/.meept/plans/evolver`), never in a
repo's `docs/plans/` — that directory is for human-authored plans only.
Each evolver plan's Meta section records `origin: skill-evolver`, the
proposal id, and the proposed action (`archive` | `improve` | `create`),
so approval tooling can identify machine-originated plans. The default
path is resolved against the user's home directory (not the daemon CWD);
relative paths are rejected.

### CLI Reference

```bash
./bin/meept skills stats [name]                # Usage/effectiveness
./bin/meept skills archive <name>              # Archive a skill
./bin/meept skills restore <name>              # Restore archived skill
./bin/meept skills restore <name> --version=N  # Restore specific version
./bin/meept skills history <name>              # Version history
./bin/meept skills evolve                      # Trigger cycle manually
```

### API Endpoints

| Method | Endpoint | Purpose |
|--------|----------|---------|
| GET | `/api/v1/skills/stats` | Usage statistics |
| GET | `/api/v1/skills/{slug}/history` | Version history |
| POST | `/api/v1/skills/{slug}/archive` | Archive a skill |
| POST | `/api/v1/skills/{slug}/restore` | Restore (archive or version) |
| POST | `/api/v1/skills/evolve` | Trigger evolver cycle |

### Configuration

```json5
{
  skills: {
    enabled: true,
    evolver: {
      enabled: false,
      interval: "6h",
      min_injections: 5,
      min_effectiveness: 0.2,
      pattern_promotion_confidence: 0.7,
      pattern_promotion_use_count: 5,
      auto_apply: false,
      run_on_start: false,
      plan_dir: "~/.meept/plans/evolver",  // sink for evolver-created plans
    },
    wiki: {
      enabled: true,
      dir: "~/.meept/wiki",
    },
    state: {
      enabled: false,
      max_state_chars: 2000,
    },
  },
}
```

## Agent-facing skill authoring

Agents can author and edit skills directly through two builtin tools,
`skills_create` and `skills_patch` (registered in
`internal/daemon/components.go` `registerSkillAuthoringTools`, wired in the
main tools block where the skills writer/registry are already live).

### the two tools

| tool | parameters | behavior |
|------|-----------|----------|
| `skills_create` | `name` (kebab-case, required), `description` (one-line, required), `body` (markdown after the frontmatter, required), `tags` (optional) | assembles the SKILL.md frontmatter + body, validates through the skills parser, then writes via `lifecycle.Writer.WriteSkill`. Refuses invalid names (`^[a-z0-9][a-z0-9-]*$`), empty bodies, and content that duplicates an existing skill's SHA. |
| `skills_patch` | `name` (required), plus either `old_string` + `new_string` (replace mode) or `content` (rewrite mode) — never both | replace mode loads the current body, requires `old_string` to occur exactly once, swaps, and writes. rewrite mode validates and rewrites the whole SKILL.md. |

Both tools return the concrete path they wrote.

### risk and confirmation

Both authoring tools classify **HIGH** (self-modification: the agent edits
its own future instructions). With the default
`require_confirmation_high = true`, every `skills_create`/`skills_patch`
call pauses for user confirmation before it runs. The rules seed as mutable
base rules in the security DB, so operators can re-rate them per deployment;
`transcript_fetch` (the ingest counterpart, see
[external integrations](external-integrations.md#transcript-fetch)) seeds
LOW. See [security](security.md) for the confirmation flow.

### tier targeting and versioning

- **new skills land in the user tier** (`~/.meept/skills/<name>/SKILL.md`)
  via the writer's tier resolver — the highest-priority writable tier.
- **the system tier (`~/.config/meept/skills/`) is read-only** to the
  authoring tools. `skills_patch` refuses to touch a system-tier skill and
  says so; write a user-tier skill with the same name to override it.
- every write to an existing skill passes through `lifecycle.Writer`, which
  captures a versioned snapshot of the previous content before overwriting
  (when the versioner is wired, the daemon default). Archive/restore reuse
  the same writer.

### directory-layout skills and linked assets

`skills.list` / `skills.get` expose `dir` (the directory containing
SKILL.md, empty for flat skills) and `linked_assets` (helper files in the
skill directory's `scripts/`, `references/`, `templates/`, `assets/`
subdirectories, one level deep). When a directory-layout skill executes, the
execution context includes a `skill_dir: <Dir>` line and any `SKILL_DIR`
literal in the skill body is substituted with the same value — so skill
bodies can invoke helper scripts relative to their own directory through
the normal `shell_execute` risk classification. Helper scripts run as shell
commands; nothing about linked assets bypasses security.

### the composition flow

The shipped `learn-from-video` skill (`config/skills/learn-from-video/`)
composes the chain end to end: `transcript_fetch` fetches a YouTube
transcript with `output_path` set so the full text lands on disk (tool
results are bounded; the conversation never carries the bulk), the model
pages the file with `file_read` (~2k-char slices via offset/limit —
small enough that a whole slice survives even at the tool-result
budget floor),
taking structured notes per slice — steps, decision rules, tool/API
names, numbers, and the WHY — then distills the generalizable procedure
from those notes, and `skills_create`/`skills_patch` persist it — with a
mandatory show-the-draft confirmation step before any write. When the
user wants a gist ("describe the shape") rather than a durable skill,
the skill routes to `transcript_fetch` with `summarize=true` and works
from the ~4k digest; the file is paged only for verbatim detail (see
[external-integrations](external-integrations.md#transcript-fetch-integration)
for the file-backed output and local summarization contracts). Note that
`skills_patch` resolves the target skill through the registry snapshot
loaded at daemon startup, so a skill written by `skills_create` in the
same session is picked up on the next daemon restart. An RPC write
surface (`skills.create`) is future work; the agent tools are the
supported path.

## Wiki Layer

The wiki is the persistent knowledge store behind skill evolution
(arXiv:2608.27454 "WikiSkill"). Learned patterns survive platform restarts, and
every evolver verdict — accepted AND rejected — is recorded so later cycles do
not repeat rejected edits.

### Layout

```
~/.meept/wiki/
  index.md              # one line per pattern: description one-liner
  logs.md               # append-only evolution log ("<RFC3339> <entry>")
  skill-impact.md       # append-only JSONL ledger of every proposal verdict
  patterns/<domain>-<hash12>.md   # one page per learned pattern
  traces/<yyyy-mm-dd>/<trace-id>.json   # immutable raw trajectories
```

### Behavior

- **Write-through**: `LearningPipeline.StorePattern` writes the pattern page
  and rebuilds the index. Wiki I/O failures log a warning and never fail the
  store call.
- **Restart survival**: `LearningPipeline.Initialize` reloads pattern pages
  from disk and inserts only patterns whose IDs are absent from the in-memory
  map.
- **Skill-impact ledger**: one JSONL row per verifier verdict — action, skill
  name, candidate diff (capped at 4k chars), verifier score, accepted flag,
  reasons. Pass A reads the ledger (newest rows first, capped at 20k chars)
  with the instruction "do not repeat rejected proposals".
- **Trace sampling**: Pass A prepends up to 5 failure + 3 success traces
  (15k chars per record) alongside the ledger and index.
- **Prompt isolation**: the wiki and trace stores are read ONLY by the
  evolver. Nothing in them is reachable from `ContextInjector` or any
  inference-path prompt builder (WikiSkill §5.1: wiki access for the worker
  degrades final skill quality).

## Trace Store

Every learning-eligible agent turn — success AND failure — writes an immutable
trace to `~/.meept/wiki/traces/<yyyy-mm-dd>/<id>.json` via `agent.WithTraceWriter`.
Failure records carry the error text; step flags reflect the turn outcome so
sampled evidence is not misleadingly green. `TraceStore.Sample` returns
stratified fail/pass records newest-first with per-record char caps marked
`...[truncated]`.

## State-Mode Execution

Skills with frontmatter `state: true` (and `skills.state.enabled = true` in
config) execute through `SkillStateRuntime` instead of the conversation loop
(arXiv:2608.26263 "SKILL.state"):

- Per step the model sees ONLY: the skill body, the current state Σ as JSON
  (default schema: `files_touched`, `tests_run`, `errors`, `next_step`), and
  the latest observation. Prompt size stays bounded regardless of run length.
- State patches: replace values; explicit `null` deletes a key; a key missing
  from the patch is left unchanged (small models drop keys — missing must not
  delete); unknown keys are dropped and reported.
- Intermediate reasoning is discarded after each step; it never persists into
  the next prompt.
- Steps cap at `MaxIterations` (default 25). Malformed responses get one
  corrective retry, then fail without corrupting Σ.
- When the global `[agent.tools] gbnf_constrained` switch is on, the response
  shape is grammar-constrained via `llm.WithRawGrammar`.

**When NOT to use state mode**: tasks where the history IS the deliverable —
auditing, debugging provenance, explaining past actions. State mode is
per-skill opt-in and must never be forced on such tasks (SKILL.state §7).

State-run trajectories persist to the same trace store as turn traces
(via `agent.NewTraceStoreWriter`), so the evolver's Pass A samples evidence
from both ordinary turns and state-mode runs.
