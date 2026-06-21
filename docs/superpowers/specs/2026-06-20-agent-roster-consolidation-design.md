# Agent Roster Consolidation

**Date:** 2026-06-20
**Status:** Design

## Problem

There are three parallel, mutually inconsistent definitions of the agent roster:

1. **`config/agents/*/AGENT.md`** — 8 agents (dispatcher, chat, coder, debugger, planner, analyst, committer, scheduler). Loaded at runtime by `internal/agents/discovery.go` and merged into specs by `internal/agent/registry.go`.
2. **`config/agents.json5`** + **`config/agents/{core,specialists}.toml`** — 9 agents including a `researcher` that doesn't exist in the runtime path. Loaded only by `internal/config/agents.go` for the in-use model gate and never reaches the `AgentRegistry.specs` map. This is the only place `prompt_components` metadata lives.
3. **`internal/agent/spec.go` `DefaultSpecs()`** — 13 programmatic specs including 5 reviewer agents (`code-reviewer`, `test-reviewer`, `debug-reviewer`, `analyst-reviewer`, `planner-reviewer`) that have no AGENT.md files and are invisible to `/api/v1/config/agents`.

The `researcher` agent is a phantom — referenced in routing rules, delegation rules, prompt component files, tests, and an unimplemented plan, but never registered. All research intents silently route to `analyst`.

The `config/prompts/` component system (30 files) is not wired to the AGENT.md runtime path. `mergeSpec()` in `registry.go` never reads `prompt_components` from `AgentMetadata` (the field doesn't exist on that struct). The body of AGENT.md is the whole system prompt; component files are orphans.

## Goal

Consolidate to a single canonical agent definition path (AGENT.md), surface all agents including reviewers, implement the researcher agent, wire the prompt component system, and delete all legacy cruft.

## Design Decisions

1. **AGENT.md is the only source of truth** for agent definitions. No Go-defined specs, no TOML/JSON5 agent files.
2. **Minimal schema**: only `id` is required. Missing fields get sensible defaults. This enables cross-compatibility with Claude Code skill files and Hermes-style agent files (an AGENT.md with just `id` and a body works).
3. **Reviewer agents become first-class** — they get AGENT.md files, show up in `platform_agents` output, and can be routed to by the dispatcher.
4. **Researcher agent is implemented** — `IntentResearch` routes to `researcher` instead of `analyst`.
5. **Prompt components wrap the body** — shared components (constitution, restrictions, conditional, capabilities) are assembled first, then the AGENT.md body is injected as the Purpose section.
6. **Greenfield cleanup** — delete all legacy config formats, dead component files, and orphaned Go code.

## Canonical Definition Source & Schema

### Single source

`config/agents/*/AGENT.md` (and the 3-tier discovery hierarchy):

```
.meept/agents/*/AGENT.md              # Project-local (highest priority)
~/.meept/agents/*/AGENT.md            # User-global
~/.config/meept/agents/*/AGENT.md     # System-wide
config/agents/*/AGENT.md              # Bundled defaults (lowest priority)
```

Higher-priority tiers shadow lower ones by agent ID. Discovery is handled by the existing `internal/agents/discovery.go`.

### Minimal AGENT.md frontmatter schema

Only `id` is required. All other fields are optional with defaults:

```yaml
---
# Required
id: coder

# Optional (defaults shown)
name: <defaults to id>
role: executor          # dispatcher|executor|reviewer|conversational|bot
description: ""         # one-liner for UI/API display
enabled: true           # toggle agent on/off (nil/absent = true)
can_delegate: false     # can call delegate_task
model: ""               # alias or direct model ref

# Tools & capabilities
additional_tools: []
capabilities: []
available_skills: []
skill_triggers: {}

# Constraints (default to DefaultConstraints() values)
max_iterations: 25
timeout_seconds: 300
max_tokens_per_turn: 4096
max_memory_refs: 20
temperature: null
top_p: null

# Prompt assembly (optional)
prompt_components: []

# Reviewer routing (reviewer agents only)
reviews_domain: ""      # code|debug|plan|analysis|test
---

Agent body text (the system prompt purpose section)
```

An AGENT.md with just `id: foo` and a body parses and runs with executor role, default constraints, and baseline tools only.

### New AgentMetadata fields

Migrated from `config.AgentDefinition` to `agents.AgentMetadata` in `internal/agents/models.go`:

| Field | Type | Default | Source |
|-------|------|---------|--------|
| `Description` | `string` | `""` | `config.AgentDefinition.Description` |
| `Enabled` | `*bool` | `nil` (= true) | `config.AgentDefinition.Enabled` |
| `CanDelegate` | `bool` | `false` | `config.AgentDefinition.CanDelegate` |
| `PromptComponents` | `[]string` | `nil` | `config.AgentDefinition.PromptComponents` |
| `ReviewsDomain` | `string` | `""` | New — for reviewer routing |

### New AgentSpec fields

Carried through from metadata to consumers:

| Field | Type | Purpose |
|-------|------|---------|
| `Description` | `string` | API/UI display |
| `Enabled` | `bool` | Filtering at load time |
| `CanDelegate` | `bool` | Tool filtering (adds/removes `delegate_task`) |
| `ReviewsDomain` | `string` | Reviewer routing by `ReviewPolicy.SelectReviewer` |

## Researcher Agent

New file: `config/agents/researcher/AGENT.md`

**Frontmatter:**
```yaml
---
id: researcher
name: Research Specialist
role: executor
description: Gathers information from web, documentation, and codebase
additional_tools:
  - web_fetch
  - web_search
  - file_read
  - list_directory
capabilities:
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
max_memory_refs: 15
prompt_components:
  - base.constitution
  - base.restrictions
  - base.task_principles
  - conditional.source_evaluation
  - capabilities.memory
  - capabilities.tasks
---
```

**Body:** Content from `config/prompts/specialist/researcher.md` (research methodology, source priorities, web/codebase research guidelines, output format).

**Routing change:** `IntentResearch` in `internal/agent/intent.go` routes to `researcher` instead of `analyst`. Analyst retains synthesis/insights/summarization; researcher owns gathering/citation/source evaluation.

**Constant:** Add `AgentIDResearcher = "researcher"` to `internal/config/agents.go` constants (the constants block stays; only the loader functions are deleted).

## Reviewer Agents

5 new AGENT.md files, each migrated from the corresponding Go spec in `spec.go`:

| File | ID | Reviews Domain |
|------|----|---------------|
| `config/agents/code-reviewer/AGENT.md` | `code-reviewer` | `code` |
| `config/agents/test-reviewer/AGENT.md` | `test-reviewer` | `test` |
| `config/agents/debug-reviewer/AGENT.md` | `debug-reviewer` | `debug` |
| `config/agents/analyst-reviewer/AGENT.md` | `analyst-reviewer` | `analysis` |
| `config/agents/planner-reviewer/AGENT.md` | `planner-reviewer` | `plan` |

Each frontmatter block carries the constraints, tools, and capabilities from the Go spec. Each body is the review prompt from the Go spec's `Purpose` field. Each declares `role: reviewer` and `reviews_domain: <domain>`.

### ReviewPolicy refactor

`internal/agent/review.go` `DefaultReviewPolicy()`:

- `ReviewerMapping` becomes empty (no hardcoded IDs).
- `SelectReviewer` queries the registry for agents with `role: reviewer` and matches the originating agent's domain to the reviewer's `reviews_domain`. Falls back to a generic `test-reviewer` if no domain match.
- The `SourceCodeReviewer` constant in `internal/agent/cache.go` is removed. Reviewer IDs are dynamic agent IDs from the registry.

## Prompt Component System

### ComponentRegistry

New type in `internal/agents/components.go`:

```go
type ComponentRegistry struct {
    components map[string]string  // id -> content
    logger     *slog.Logger
}

type ComponentSection struct {
    Title   string
    Content string
}
```

**Discovery:** Scans the same 3-tier hierarchy as agents:
- `.meept/prompts/` (project)
- `~/.meept/prompts/` (user)
- `~/.config/meept/prompts/` (system)
- `config/prompts/` (bundled)

**ID scheme:** Component ID = file path relative to prompts root, with `/` → `.` and `.md` stripped. `base/constitution.md` → `base.constitution`. Matches the IDs already used in TOML/JSON5.

**Resolve:** Given a list of component IDs, returns ordered content sections. Missing IDs log a warning and are skipped.

### Assembly model (components wrap body)

Assembly happens at spec-loading time in `definitionToSpec()` and `mergeSpec()`. The `ComponentRegistry` is passed via `RegistryConfig` and used to produce the final `Purpose` string.

```
[1] Constitution (from base.constitution component, or DefaultConstitution fallback)
[2] Restrictions (from base.restrictions component, or DefaultRestrictions fallback)
[3] All other declared components, in listed order (each becomes a titled section)
[4] AGENT.md body (injected as the "Purpose & Task Principles" section)
```

If `prompt_components` is empty or absent, the body alone becomes the Purpose with default constitution/restrictions — backward compatible with current behavior.

### Component files retained (16 files)

```
config/prompts/
├── base/
│   ├── constitution.md          → special: WithConstitution slot
│   ├── restrictions.md          → special: WithRestrictions slot
│   └── task_principles.md       → AddSection("Task Principles")
├── conditional/
│   ├── code_style.md
│   ├── error_context.md
│   ├── source_evaluation.md
│   ├── analysis_depth.md
│   ├── task_decomposition.md
│   └── git_safety.md
├── capabilities/
│   ├── memory.md
│   ├── tasks.md
│   └── platform.md
└── tools/
    ├── bash.md
    ├── file_ops.md
    ├── web.md
    └── git.md
```

## Dispatcher AGENT.md Migration

The dispatcher is the most critical file to get right because it owns routing.

### Frontmatter additions

```yaml
description: "Intake agent that classifies user intent and routes to specialists"
enabled: true
can_delegate: false
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.platform
```

### Body content migration

Content from two deleted component files gets folded into the dispatcher AGENT.md body:

**From `dispatcher/purpose.md`** (task creation details):
- Task creation pattern: include `memory_refs`, `context_query`, `inherited_from` when delegating
- Agent discovery via `platform_agents`: each agent has ID, Name, Role, Purpose

**From `dispatcher/routing_rules.md`** (routing decision process):
- Routing decision process: identify keywords → consider context → match specialist → default to chat
- Multi-step task flow: route to planner first for decomposition, planner creates subtasks, subtasks inherit parent context
- Confidence levels: high (>0.8), medium (0.5-0.8), low (<0.5 → route to chat)

### Routing table correction

Current AGENT.md body routes Research → analyst (wrong). Post-migration table:

| Intent | Route To | Example |
|--------|----------|---------|
| Write/modify code | `coder` | "Add a login form" |
| Fix bug/error | `debugger` | "Why is this crashing?" |
| Find information | `researcher` | "How does X work?" |
| Summarize/analyze | `analyst` | "Explain this codebase" |
| Plan complex task | `planner` | "Help me build a feature" |
| Git operations | `committer` | "Commit these changes" |
| Schedule/remind | `scheduler` | "Remind me tomorrow" |
| Review code/work | `code-reviewer` | "Review my changes" |
| General chat | `chat` | "Hello", "Thanks" |

### Dynamic discovery emphasis

Add explicit instruction that the routing table is a baseline. The dispatcher must call `platform_agents` to discover custom/extra agents not listed, since AGENT.md-defined agents can be added dynamically at any tier. The 5 reviewer agents are first-class and can be routed to directly (not just through programmatic `ReviewPolicy`).

## Component vs Skill Distinction

| Aspect | Prompt Components | Skills |
|--------|------------------|--------|
| **What they are** | Static text fragments assembled into the system prompt at startup | Runnable tool-like capabilities discovered at runtime |
| **When they apply** | Always-on for the declaring agent | Triggered by keyword, capability match, or explicit invocation |
| **Granularity** | Cross-cutting concerns (constitution, restrictions, memory instructions) | Task-specific procedures (debugging methodology, code review checklist) |
| **Source** | `config/prompts/{base,conditional,capabilities,tools}/*.md` | `.meept/skills/`, `~/.meept/skills/`, `~/.config/meept/skills/` |
| **Discovery** | Referenced by ID in AGENT.md `prompt_components` frontmatter | Auto-discovered by 3-tier scan, matched via `requires:` frontmatter |
| **Boundary rule** | "How to think" — attitudes, constraints, persistent rules | "How to do" — step-by-step procedures for specific task types |

If content is "always relevant regardless of task," it's a component. If content is "relevant only when doing X," it's a skill.

## Full Deletion List

### Legacy config formats

- `config/agents.json5`
- `config/agents/core.toml`
- `config/agents/specialists.toml`

### Specialist component files (content folded into AGENT.md bodies)

- `config/prompts/specialist/coder.md`
- `config/prompts/specialist/debugger.md`
- `config/prompts/specialist/researcher.md`
- `config/prompts/specialist/analyst.md`
- `config/prompts/specialist/planner.md`
- `config/prompts/specialist/committer.md`
- `config/prompts/specialist/scheduler.md`

### Dispatcher component files (content folded into dispatcher AGENT.md body)

- `config/prompts/dispatcher/purpose.md`
- `config/prompts/dispatcher/routing_rules.md`

### Chat component files (content folded into chat AGENT.md body)

- `config/prompts/chat/personality.md`
- `config/prompts/chat/delegation_rules.md`

### Dead reminder files (never wired, redundant)

- `config/prompts/reminders/memory_context.md`
- `config/prompts/reminders/task_status.md`
- `config/prompts/reminders/plan_mode.md`

### Empty directories removed

- `config/prompts/specialist/`
- `config/prompts/dispatcher/`
- `config/prompts/chat/`
- `config/prompts/reminders/`

### Go code deleted

- `internal/config/agents.go` — entire file deleted. `AgentID*` and `AgentRole*` constants move to `internal/config/schema.go` (which already holds config types and is imported everywhere `config.AgentID*` is referenced). Add `AgentIDResearcher` alongside the existing constants.
- `internal/config/agents_test.go` — entire file deleted.
- `internal/agent/spec.go` — all 14 `*Spec()` constructors + `DefaultSpecs()` + `ptr()` helper deleted. Keep: `AgentSpec` (with new fields), `AgentRole`, `AgentConstraints`, `DefaultConstraints()`, `BaselineTools`, `ExecutorAgentIDs()`, `HasTool`, `AllTools`, `HasSkill`, `GetSkillForTrigger`.
- `internal/agent/cache.go:52` — `SourceCodeReviewer` constant removed.

## Go Code Changes

| File | Change |
|------|--------|
| `internal/agent/spec.go` | Delete 14 `*Spec()` constructors, `DefaultSpecs()`, `ptr()`. Add `Description`, `Enabled`, `CanDelegate`, `ReviewsDomain` fields to `AgentSpec`. |
| `internal/agent/registry.go` | Remove `DefaultSpecs()` loop in `NewAgentRegistry`. Add `ComponentRegistry` to `RegistryConfig`. Wire component assembly in `definitionToSpec()` and `mergeSpec()`. Filter disabled agents (`enabled: false`). Handle `CanDelegate` in tool filtering. |
| `internal/agents/models.go` | Add `Description`, `Enabled *bool`, `CanDelegate`, `PromptComponents`, `ReviewsDomain` to `AgentMetadata`. Update `DefaultMetadata()`. |
| `internal/agents/parser.go` | Parser already handles unknown YAML fields via struct tags — add tags for new fields. |
| `internal/agents/components.go` | **New file.** `ComponentRegistry` with `Discover()`, `Resolve()`, 3-tier scan. |
| `internal/agents/components_test.go` | **New file.** Tests for discovery and resolution. |
| `internal/agent/review.go` | `DefaultReviewPolicy()` `ReviewerMapping` becomes empty. `SelectReviewer` queries registry for `role: reviewer` agents by `reviews_domain`. |
| `internal/agent/cache.go` | Remove `SourceCodeReviewer` constant. |
| `internal/agent/intent.go` | `IntentResearch` default agent: `analyst` → `researcher`. |
| `internal/config/agents.go` | Delete entire file. Move `AgentID*` / `AgentRole*` constants to `internal/config/schema.go`. Add `AgentIDResearcher`. |
| `internal/config/agents_test.go` | Delete entire file. |
| `internal/daemon/components.go` | `loadAgentModelRefs` switches from `config.LoadAgentDefinitionsDefault` to `agents.Discovery` for the in-use model gate. |

## New Files Created

| File | Purpose |
|------|---------|
| `config/agents/researcher/AGENT.md` | Researcher agent definition |
| `config/agents/code-reviewer/AGENT.md` | Code reviewer agent definition |
| `config/agents/test-reviewer/AGENT.md` | Test reviewer agent definition |
| `config/agents/debug-reviewer/AGENT.md` | Debug reviewer agent definition |
| `config/agents/analyst-reviewer/AGENT.md` | Analyst reviewer agent definition |
| `config/agents/planner-reviewer/AGENT.md` | Planner reviewer agent definition |
| `internal/agents/components.go` | `ComponentRegistry` type |
| `internal/agents/components_test.go` | Component registry tests |

## Files Modified

| File | Changes |
|------|---------|
| `config/agents/dispatcher/AGENT.md` | Add frontmatter fields. Fold in purpose + routing rules content. Fix routing table (researcher, code-reviewer). Add dynamic discovery emphasis. |
| `config/agents/chat/AGENT.md` | Add frontmatter fields. Fold in personality + delegation rules content. |
| `config/agents/coder/AGENT.md` | Add frontmatter fields. Fold in specialist content if missing. |
| `config/agents/debugger/AGENT.md` | Add frontmatter fields. Fold in specialist content. |
| `config/agents/planner/AGENT.md` | Add frontmatter fields. Fold in specialist content. |
| `config/agents/analyst/AGENT.md` | Add frontmatter fields. Fold in specialist content. |
| `config/agents/committer/AGENT.md` | Add frontmatter fields. Fold in specialist content. |
| `config/agents/scheduler/AGENT.md` | Add frontmatter fields. Fold in specialist content. |
| `config/prompts/tools/web.md` | Remove stale `agent_types: [researcher]` HTML-comment frontmatter. |
| `internal/agent/spec.go` | Add new fields to `AgentSpec`, delete constructors. |
| `internal/agent/registry.go` | Remove `DefaultSpecs()`, add `ComponentRegistry`, wire assembly, filter disabled. |
| `internal/agents/models.go` | Add new metadata fields. |
| `internal/agents/parser.go` | Add struct tags for new fields. |
| `internal/agent/review.go` | Dynamic reviewer lookup. |
| `internal/agent/cache.go` | Remove constant. |
| `internal/agent/intent.go` | Research routing change. |
| `internal/daemon/components.go` | Model ref loading via `agents.Discovery`. |

## Testing Strategy

1. **ComponentRegistry unit tests** — discovery (finds all 16 components), resolution (ordered sections), missing component (warn + skip).
2. **Assembled prompt content tests** — verify component ordering, body injection position, constitution/restrictions special handling.
3. **Agent loading tests** — all 14 agents (8 standard + researcher + 5 reviewers) load from AGENT.md with correct metadata.
4. **Disabled agent test** — `enabled: false` agent is filtered out.
5. **Minimal AGENT.md test** — just `id` + body works with defaults.
6. **Reviewer routing test** — `ReviewPolicy.SelectReviewer` finds reviewers dynamically by `reviews_domain`.
7. **Research routing test** — `IntentResearch` routes to `researcher`.
8. **Dispatcher routing table test** — verify the routing table in dispatcher AGENT.md matches the actual agent roster (no phantoms, no missing agents).

## Backward Compatibility

- Existing `~/.meept/agents.json5` files: the daemon no longer reads them. On first startup after upgrade, log a one-time notice.
- User-defined AGENT.md files in `.meept/agents/` or `~/.meept/agents/`: continue to work and shadow bundled defaults.
- An AGENT.md with no `prompt_components` works exactly as before (body-only system prompt with default constitution/restrictions).
