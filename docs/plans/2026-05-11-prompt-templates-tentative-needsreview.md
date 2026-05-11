# Prompt Templates

**Status**: tentative -- needs review
**Date**: 2026-05-11
**Priority**: medium
**Estimate**: 5-7 days across 4 phases

## 1. Problem Statement

Meept's skills system (`internal/skills/`) provides **heavyweight, full-agent behaviors**: a skill declares capability requirements, tool filters, risk levels, iteration limits, temperature overrides, and a complete instruction body. Skills are resolved to models via the `llm.Resolver`, executed through the `skills.Executor`, and can drive multi-step agent loops. This is powerful for complex tasks like code review or debugging workflows.

However, many user and agent interactions are much lighter: a reusable prompt fragment with a few argument slots. Examples:

- `/summarize <text>` -- "Summarize the following concisely: ..."
- `/explain <code>` -- "Explain this code step by step: ..."
- `/translate <lang> <text>` -- "Translate the following to ${1}: ${@}"
- `/format-json` -- "Pretty-print and validate the following JSON: ..."

Creating a full skill for each of these is overkill. The overhead of `requires`, `risk-level`, `allowed-tools`, `max-iterations` frontmatter, plus model resolution, is unnecessary for a simple prompt substitution.

Pi Agent (an external reference) solves this with **prompt templates**: `.md` files with minimal YAML frontmatter (`name`, `description`) that define reusable, parameterized prompts. They use positional argument substitution (`$1`, `$2`, `$@`, `${@:N}`, `${@:N:L}`) and are invoked as slash commands.

**This plan introduces prompt templates to Meept**, filling the gap between "type a raw prompt" and "invoke a full skill". Templates are lighter than skills, share the same discovery pattern, and critically are available to **agents themselves** for dynamic prompt composition.

## 2. Current Architecture

### 2.1 Skills System (`internal/skills/`)

The skills system is a well-structured pipeline:

| Component | File | Role |
|-----------|------|------|
| `Skill` model | `models.go` | Struct with Name, Description, Requires, Tags, Body, AllowedTools, RiskLevel, MaxIterations, Temperature, MaxTokens |
| `SkillMetadata` | `models.go` | YAML frontmatter struct |
| `SkillIndexEntry` | `index.go` | Metadata-only entry for fast lookup (no body) |
| `SkillIndex` | `index.go` | In-memory index with tag/capability secondary indices |
| `Parser` | `parser.go` | `ParseSkillFile()` and `ParseSkillMetadataOnly()` -- reads `.md`, splits `---` frontmatter, unmarshals YAML |
| `Discovery` | `discovery.go` | Scans 3-tier directory hierarchy (project/user/system), supports both `SKILL.md` in subdirectories and flat `.md` files, priority shadowing |
| `LazySkillLoader` | `lazy_loader.go` | LRU cache that loads skill bodies on demand from the index |
| `Registry` | `registry.go` | Name-based lookup with tag/capability/match queries |
| `Executor` | `executor.go` | Runs skills against the LLM: resolves model, builds system+user messages, calls `Chatter.Chat()` |

**Discovery tiers** (from `discovery.go`):
```
Priority 0 (highest): .meept/skills/
Priority 1:           ~/.meept/skills/
Priority 2 (lowest):  ~/.config/meept/skills/
```

**Frontmatter parsing** (`parser.go`): Uses `---` delimiters, `gopkg.in/yaml.v3`. Supports both hyphenated (`allowed-tools`) and underscored (`allowed_tools`) field names.

### 2.2 Dispatcher Integration

The dispatcher (`internal/agent/dispatcher.go`) checks for skill invocation at line 170:

```go
if strings.HasPrefix(input, "/") {
    skillName, skillInput := d.parseSkillInvocation(input)
    if skill := d.getSkill(skillName); skill != nil {
        return d.executeSkill(ctx, skill, skillInput, sessionID)
    }
}
```

The `parseSkillInvocation` method strips the leading `/`, splits on first whitespace, and passes the remainder as input. If no skill matches, the input falls through to normal intent classification.

### 2.3 TUI Slash Command System

The TUI (`internal/tui/slash.go`) has a `ParseSlash()` function that parses `/command-name args...` into `SlashCommand{Name, Args}`. Built-in commands are checked via `builtinCommands` map. The `command_handler.go` routes built-in commands to handlers and returns an error for non-builtins ("skill invocation not yet implemented").

The `SlashAutocomplete` component provides type-ahead for built-in commands but does not yet include skill names.

### 2.4 Prompt Loader (`internal/agent/prompt/loader.go`)

A separate system loads prompt **components** (not templates) from `config/prompts/` directories. Components are referenced by dot-notation (`base.constitution` -> `base/constitution.md`) and assembled by the `Builder` (`internal/agent/prompt/builder.go`). The builder supports `${VAR}` interpolation via `PromptContext.Variables` but does not support positional arguments.

### 2.5 CLI Commands

Skills are exposed via `cmd/meept/skills.go` with `list`, `show`, and `run` subcommands, all communicating with the daemon over RPC.

### 2.6 Daemon Components

`internal/daemon/components.go` wires everything together, including `SkillDiscovery`, `SkillRegistry`, and `SkillExecutor`.

## 3. Proposed Architecture

### 3.1 Template Format

A template is a `.md` file with minimal YAML frontmatter:

```markdown
---
name: summarize
description: "summarize text concisely"
---

Summarize the following text in 2-3 sentences.
Focus on the key points and actionable takeaways.

$@
```

Another example with positional arguments:

```markdown
---
name: translate
description: "translate text to a specified language"
---

Translate the following text to $1.
Preserve the original formatting and tone.

$2
```

Supported substitution patterns:

| Pattern | Meaning | Example (`args: ["fr", "hello world"]`) |
|---------|---------|----------------------------------------|
| `$1`, `$2`, ... | Positional argument (1-indexed) | `$1` -> `fr` |
| `$@` | All arguments joined by spaces | `$@` -> `fr hello world` |
| `${@:N}` | Arguments from index N onward | `${@:2}` -> `hello world` |
| `${@:N:L}` | L arguments starting at index N | `${@:2:1}` -> `hello` |

### 3.2 Template Model

```go
// internal/templates/models.go

type Template struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Body        string `json:"body"`
    Path        string `json:"path"`
    Priority    int    `json:"priority"`
}

type TemplateMetadata struct {
    Name        string `yaml:"name"`
    Description string `yaml:"description"`
}
```

### 3.3 Discovery

Templates follow the same 3-tier discovery pattern as skills, but scan `templates/` directories instead of `skills/`:

```
Priority 0 (highest): .meept/templates/
Priority 1:           ~/.meept/templates/
Priority 2 (lowest):  ~/.config/meept/templates/
```

The discovery scans for `.md` files (excluding `readme.md`, `changelog.md`, `license.md`, `contributing.md` -- reuse the existing `isSkillFile` exclusion logic). Each file is parsed for frontmatter; files without valid frontmatter (missing `name`) are skipped with a warning.

Priority shadowing works identically to skills: project-local templates shadow user-global, which shadow system-wide.

### 3.4 Argument Substitution Engine

A pure-function substitution engine with no dependencies on the skills package:

```go
// internal/templates/substitute.go

func Substitute(body string, args []string) string
```

Implements the patterns from section 3.1. Key behaviors:
- `$N` where N is a single digit 1-9: replaced by `args[N-1]`, or empty string if out of range
- `$@`: replaced by `strings.Join(args, " ")`
- `${@:N}`: `strings.Join(args[N:], " ")`, clamped to valid range
- `${@:N:L}`: `strings.Join(args[N:N+L], " ")`, clamped to valid range
- Unrecognized patterns (`$FOO`, `${BAR}`) are left as-is to avoid breaking templates that use dollar signs in other contexts

### 3.5 Template Registry

A lightweight registry with the same API shape as `skills.Registry`:

```go
// internal/templates/registry.go

type Registry struct {
    mu        sync.RWMutex
    templates map[string]*Template
    logger    *slog.Logger
}

func (r *Registry) Get(name string) *Template
func (r *Registry) List() []*Template
func (r *Registry) Names() []string
func (r *Registry) Count() int
func (r *Registry) Substitute(name string, args []string) (string, error)
```

`Substitute` is the primary convenience method: looks up the template by name and applies argument substitution. Returns an error if the template is not found.

### 3.6 Agent-Facing API

The critical differentiator from Pi's templates: agents can use templates too.

**New builtin tool**: `template_invoke`

```go
// internal/tools/builtin/tool_template_invoke.go

// Tool definition for agents to invoke templates at runtime.
// Parameters:
//   - name (required): template name
//   - args (optional): positional arguments for substitution
//   - inject (optional, bool): if true, inject result as context rather than as user message
```

When an agent calls `template_invoke`, the template body is substituted with the provided args. The `inject` flag controls behavior:
- `inject=false` (default): the substituted template text is sent as a user message, producing an LLM response. This is the "invoke" pattern -- the agent wants the LLM to process the template.
- `inject=true`: the substituted template text is injected into the agent's context/prompt for the current turn. This is the "hot loading" pattern -- the agent wants to augment its own instructions.

**New builtin tool**: `template_list`

```go
// internal/tools/builtin/tool_template_list.go

// Tool definition for agents to discover available templates.
// No required parameters. Returns list of template names and descriptions.
```

This lets agents discover templates at runtime and choose which to invoke based on the task.

### 3.7 CLI Command

```
meept templates list [--json] [--tag <tag>]
meept templates show <name> [--json]
meept templates invoke <name> [args...]
```

`list` and `show` are read-only operations that can work locally (scan template directories) or via RPC. `invoke` sends the substituted template to the daemon for LLM processing, equivalent to `meept chat "<substituted template>"`.

### 3.8 Dispatcher Integration

The dispatcher's slash-command handling (line 170 of `dispatcher.go`) currently checks the skill registry. After template support is added, it should check **both** registries:

```
1. Check skills registry (existing behavior)
2. If no skill found, check templates registry
3. If template found, substitute args and treat as user input (not as skill execution)
4. If neither found, fall through to normal intent classification
```

This means `/summarize some text` would:
1. Not match any skill named "summarize"
2. Match a template named "summarize"
3. Substitute `some text` into the template body
4. Send the result through normal intent classification (likely routed to the chat agent)

### 3.9 TUI Integration

The TUI slash autocomplete (`SlashAutocomplete`) needs to include template names alongside built-in commands and skill names. The `CommandHandler` needs a new branch for template invocation:

```go
// In command_handler.go executeSync():
if IsBuiltin(cmd.Name) {
    return h.executeBuiltin(cmd)
}
// Try skill invocation
// Try template invocation (new)
// Fall through to error
```

### 3.10 Service Layer

A new `TemplatesService` in `internal/services/` exposes template operations to both RPC and HTTP transports:

```go
// internal/services/templates_service.go

type TemplatesService struct {
    registry *templates.Registry
    executor *skills.Executor  // Reuse for LLM execution
}

func (s *TemplatesService) List(ctx context.Context, req TemplatesListRequest) ([]TemplateInfo, error)
func (s *TemplatesService) Get(ctx context.Context, req TemplatesGetRequest) (*TemplateInfo, error)
func (s *TemplatesService) Invoke(ctx context.Context, req InvokeRequest) (*InvokeResult, error)
```

## 4. Pros/Cons Analysis

### 4.1 Pi's Standalone Templates vs Meept's Integrated Approach

| Dimension | Pi Agent | Meept (proposed) |
|-----------|----------|------------------|
| Scope | User-facing only | User + agent-facing |
| Discovery | Single directory | 3-tier priority shadowing |
| Parsing | Custom markdown parser | Reuse skills frontmatter parser |
| Substitution | $1, $@, ${@:N:L} | Same patterns |
| Invocation | Slash commands only | CLI, TUI, RPC, HTTP, agent tools |
| Model resolution | None (uses current model) | Optional (can use current model or resolve) |
| Hot loading | N/A | Agents can inject templates into context |
| Caching | None | LRU cache (reuse LazySkillLoader pattern) |

### 4.2 Why Agent-Accessible Templates Are Valuable

1. **Dynamic prompt composition**: Agents can adapt their behavior by selecting and injecting relevant templates. A coding agent working on a review task can inject a "code-review-checklist" template; the same agent working on documentation can inject a "docs-standard" template.

2. **User-defined agent behaviors**: Users can create templates that guide agent behavior without needing to understand the full skill system. A template like "always respond in French" is trivial to create.

3. **Reduced prompt engineering in code**: Rather than hardcoding prompt patterns in Go code, patterns can be externalized as templates that both users and agents can discover and use.

4. **Consistency**: The same templates used by users from the CLI are available to agents, ensuring consistent behavior.

### 4.3 Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Template explosion**: Agents over-use templates, injecting many into context, bloating prompts | Medium | Strict size limits: max 3 templates per turn, max 2000 chars per template body. Enforced in `template_invoke` tool. |
| **Name collisions**: Template name conflicts with skill name or builtin command | Low | Check order: builtins > skills > templates. Templates are lowest priority in slash-command resolution. |
| **Prompt injection via templates**: Malicious template content injected into agent context | Medium | Templates go through the same `InputSanitizer` as other user content. The `inject=true` path wraps content in `<template-context>` tags with a system note (same pattern as memory context fencing). |
| **Template as skill confusion**: Users unsure whether to create a template or a skill | Low | Documentation. Rule of thumb: if it needs model resolution, tool filtering, or iteration limits, use a skill. Otherwise, use a template. |
| **Discovery performance**: Scanning many template directories on every lookup | Low | Same approach as skills: discover once at startup, cache in registry, reload on signal. |

## 5. Implementation Phases

### Phase 1: Core Template Engine (2 days)

Create the `internal/templates/` package with the fundamental building blocks.

**New files:**

| File | Purpose |
|------|---------|
| `internal/templates/models.go` | `Template` and `TemplateMetadata` structs |
| `internal/templates/substitute.go` | Argument substitution engine |
| `internal/templates/substitute_test.go` | Comprehensive tests for all substitution patterns |
| `internal/templates/parser.go` | Template file parser (reuse frontmatter splitting from `skills/parser.go`) |
| `internal/templates/parser_test.go` | Parser tests |
| `internal/templates/discovery.go` | 3-tier discovery (reuse pattern from `skills/discovery.go`) |
| `internal/templates/discovery_test.go` | Discovery tests |
| `internal/templates/registry.go` | Name-based registry with `Substitute()` convenience |
| `internal/templates/registry_test.go` | Registry tests |

**Key design decisions:**
- The `substitute.go` package is intentionally standalone with no dependencies on `skills` or `agent`, making it easy to test and reuse.
- The parser reuses the `---` frontmatter splitting approach from `skills/parser.go` but is a separate implementation to avoid pulling in skill-specific fields. Consider extracting `splitFrontmatter()` into a shared `internal/frontmatter` package if it grows further.
- Discovery reuses the same `DiscoveryTier` pattern and priority constants.

### Phase 2: CLI and Daemon Integration (1.5 days)

Wire templates into the daemon and expose them via CLI and RPC.

**New files:**

| File | Purpose |
|------|---------|
| `cmd/meept/templates.go` | `meept templates list/show/invoke` commands |
| `internal/services/templates_service.go` | Service layer for RPC/HTTP |
| `internal/rpc/templates.go` | RPC handler methods |

**Modified files:**

| File | Change |
|------|--------|
| `internal/daemon/components.go` | Add `TemplateDiscovery`, `TemplateRegistry`, `TemplateService` fields |
| `internal/daemon/setup.go` (or equivalent) | Initialize template discovery and registry during daemon startup |
| `internal/rpc/handler.go` | Register template RPC methods |
| `cmd/meept/main.go` | Add `templates` command group |

### Phase 3: Agent Tools and Dispatcher (1.5 days)

Make templates available to agents and integrate with the dispatcher's slash-command handling.

**New files:**

| File | Purpose |
|------|---------|
| `internal/tools/builtin/tool_template_invoke.go` | Agent tool for invoking/injecting templates |
| `internal/tools/builtin/tool_template_list.go` | Agent tool for listing available templates |
| `internal/tools/builtin/tool_template_invoke_test.go` | Tests |
| `internal/tools/builtin/tool_template_list_test.go` | Tests |

**Modified files:**

| File | Change |
|------|--------|
| `internal/agent/dispatcher.go` | In `ClassifyAndRoute`, after skill check, check template registry. If template found, substitute args and treat as user input. Add `templateRegistry` field to `Dispatcher` struct and `DispatcherConfig`. |
| `internal/daemon/components.go` | Wire template registry into dispatcher config |
| `internal/tools/builtin/platform.go` | Register `template_invoke` and `template_list` as baseline tools |

### Phase 4: TUI Integration (1 day)

Complete the user-facing experience with TUI support.

**Modified files:**

| File | Change |
|------|--------|
| `internal/tui/command_handler.go` | Add template invocation branch after builtin check. If the command name matches a template, substitute and send as chat message. |
| `internal/tui/slash.go` | No structural changes needed; templates are resolved dynamically. |
| `internal/tui/slash_autocomplete.go` | Add template names to the autocomplete command list. Needs a way to receive template names from the daemon (RPC call or cached list). |
| `internal/tui/rpc.go` (or equivalent) | Add `ListTemplates()` RPC method for autocomplete population |

**Sample template files (delivered in `config/templates/`):**

| File | Description |
|------|-------------|
| `config/templates/summarize.md` | Concise text summarization |
| `config/templates/explain.md` | Step-by-step explanation |
| `config/templates/translate.md` | Language translation |
| `config/templates/format-json.md` | JSON validation and pretty-printing |
| `config/templates/code-review-checklist.md` | Code review checklist for agent injection |

## 6. Integration Points

### 6.1 Templates vs Skills

| Aspect | Templates | Skills |
|--------|-----------|--------|
| Frontmatter | `name`, `description` | `name`, `description`, `requires`, `tags`, `allowed-tools`, `risk-level`, `max-iterations`, `temperature`, `max-tokens`, `examples` |
| Body | Prompt text with `$N`/`$@` substitution | Full agent instructions |
| Model resolution | Uses current model (no resolution) | Resolves model via `llm.Resolver` based on `requires` |
| Execution | Text substitution, then normal chat | Dedicated `Executor` with system message |
| Tool filtering | None | `allowed-tools` restricts available tools |
| Risk management | None | `risk-level` gates execution |
| Iteration control | Single turn | `max-iterations` controls agent loop |
| Use case | Prompt shortcuts, agent context injection | Complex multi-step agent behaviors |

**Coexistence rule**: If a name exists as both a skill and a template, the skill wins. This is enforced by the dispatcher check order.

### 6.2 Agent Loop Integration

Templates integrate with the agent loop at two points:

1. **Slash-command routing** (dispatcher level): `/template-name args` is intercepted before intent classification, substituted, and routed as normal user input.

2. **Agent tool invocation** (loop level): Agents call `template_invoke` during execution. With `inject=false`, the substituted text becomes a user message in the conversation. With `inject=true`, it is injected as context fencing in the next system prompt.

The context injection path uses the same fencing pattern as memory context:

```xml
<template-context>
[System note: The following is an injected template, NOT new user input.
It provides additional instructions for this turn only.]

...substituted template body...
</template-context>
```

### 6.3 Dispatcher Integration Detail

Current flow in `ClassifyAndRoute`:
```
input -> /prefix check -> skill registry -> intent classification -> routing
```

Proposed flow:
```
input -> /prefix check -> skill registry -> template registry -> intent classification -> routing
```

The template branch produces a pre-processed input string (after substitution) that flows into the normal intent classification pipeline. This means a `/summarize` template invocation still goes through intent classification, memory context building, and agent routing -- the template just transforms the user's raw input into a more structured prompt.

### 6.4 TUI Integration Detail

The `SlashAutocomplete` currently only knows about built-in commands. It needs to be extended to include skill names and template names. The proposed approach:

1. On TUI connect, fetch skill names and template names via RPC
2. Merge into a single command list: builtins + skills + templates
3. The `UpdateCommands()` method already supports dynamic updates

The `CommandHandler` needs a template registry reference (or RPC fallback) to resolve template names during execution.

### 6.5 Prompt Builder Integration

The existing `prompt.Builder` (`internal/agent/prompt/builder.go`) supports `${VAR}` interpolation via `PromptContext.Variables`. Template substitution is a different mechanism (positional args, not named vars) and operates at a different layer (before the prompt reaches the builder). No changes to the prompt builder are needed.

However, the `PromptBuilder.WithCoworkerAwareness()` method should be updated to mention template availability:

```go
// Add to DefaultCoworkerAwareness():
// - **template_invoke**: Invoke a prompt template with arguments, optionally injecting it as context.
// - **template_list**: List available prompt templates with descriptions.
```

## 7. Testing Strategy

| Area | Approach |
|------|----------|
| Substitution engine | Table-driven tests covering all patterns, edge cases (missing args, out-of-range indices, empty args) |
| Parser | Test valid templates, missing frontmatter, missing name, malformed YAML |
| Discovery | Test with temp directories, verify priority shadowing, non-existent tiers |
| Registry | Concurrent access tests, name normalization |
| CLI | Integration tests against daemon RPC |
| Agent tools | Unit tests with mock registries |
| Dispatcher | Test slash-command routing with both skills and templates present |
| TUI | Manual testing (TUI tests are integration-level) |

## 8. Configuration

No new configuration file is needed. Template directories follow the existing convention (`.meept/templates/`, `~/.meept/templates/`). If users want to customize template locations, this can be added to `meept.json5` in a future iteration.

Template size limits (for agent injection) can be configured via constants in `internal/templates/registry.go`:

```go
const (
    MaxTemplateBodySize   = 4096   // Maximum template body size in characters
    MaxTemplatesPerTurn   = 3      // Maximum templates an agent can inject per turn
    MaxInjectedCharsTotal = 6000   // Maximum total injected template characters per turn
)
```

## 9. Open Questions

1. **Shared frontmatter parsing**: Should we extract `splitFrontmatter()` from `skills/parser.go` into a shared `internal/frontmatter/` package? This would avoid code duplication but adds a new package. Decision: defer until a third consumer appears.

2. **Template hot-reload**: Should the daemon watch template directories for changes and reload automatically? Skills currently do not hot-reload. Decision: match skill behavior -- no hot-reload. Templates are loaded at startup and can be reloaded via daemon restart or a future `/reload` command.

3. **Template composition**: Should templates be able to reference other templates (e.g., `$template:other-name`)? Decision: no in v1. This adds complexity and is better addressed by skill-level composition if needed.

4. **HTTP API endpoints**: Should templates be exposed via the REST API for the menubar app? Decision: yes, but as a follow-up. Phase 2 adds RPC support; HTTP can be added in a subsequent PR using the existing service layer pattern.
