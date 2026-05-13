# Prompt Templates

**Status**: tentative -- needs review
**Date**: 2026-05-11
**Priority**: medium
**Estimate**: 6-8 days across 4 phases

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
scope: turn
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

**Scope field** (`scope`, optional, defaults to `turn`):

The `scope` field controls how long an injected template persists in an agent's context:

| Scope | Duration | Use case | Example |
|-------|----------|----------|---------|
| `turn` | Current turn only (default) | One-shot prompts, format requests | `/summarize`, `/format-json` |
| `session` | Entire conversation until explicitly cleared | Behavioral modifiers, persona adoption, project conventions | `/role senior-go-dev`, `/convention go-project` |

When `scope: turn`, the injected template context is included in the agent's prompt for the current turn and then discarded. This is the safe default and matches the behavior described in section 3.6.

When `scope: session`, the injected template context persists across turns. The agent's prompt builder includes all active session-scoped templates in every subsequent turn. Session-scoped templates are tracked per-conversation and can be listed or cleared:

- `template_list` tool with `active: true` filter shows currently active session-scoped templates
- `template_clear` tool removes a specific session-scoped template or all session-scoped templates
- The TUI shows active session-scoped templates as a status indicator (e.g., "active: senior-go-dev, go-project")

Session-scoped templates have stricter limits than turn-scoped ones to prevent context bloat:

```go
const (
    MaxSessionScopedTemplates = 5     // Maximum concurrently active session-scoped templates
    MaxSessionScopedCharsTotal = 8000 // Maximum total chars from session-scoped templates
)
```

If a session-scoped template injection would exceed these limits, the tool returns an error listing the currently active templates so the agent can decide which to clear.

The `scope` field is purely advisory for user-facing invocations (slash commands always produce a single response). It only affects behavior when agents use `template_invoke` with `inject=true`.

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

type TemplateScope string

const (
    ScopeTurn    TemplateScope = "turn"    // Current turn only (default)
    ScopeSession TemplateScope = "session" // Entire conversation
)

type Template struct {
    Name        string        `json:"name"`
    Description string        `json:"description"`
    Scope       TemplateScope `json:"scope"`
    Body        string        `json:"body"`
    Path        string        `json:"path"`
    Priority    int           `json:"priority"`
}

type TemplateMetadata struct {
    Name        string        `yaml:"name"`
    Description string        `yaml:"description"`
    Scope       TemplateScope `yaml:"scope"`
}
```

If `scope` is omitted from frontmatter, it defaults to `ScopeTurn` during parsing.

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

A lightweight registry with the same API shape as `skills.Registry`, plus session-scoped template tracking:

```go
// internal/templates/registry.go

type Registry struct {
    mu        sync.RWMutex
    templates map[string]*Template
    sessions  *SessionStore  // Manages per-conversation active templates
    logger    *slog.Logger
}

func (r *Registry) Get(name string) *Template
func (r *Registry) List() []*Template
func (r *Registry) Names() []string
func (r *Registry) Count() int
func (r *Registry) Substitute(name string, args []string) (string, error)

// Session-scoped template management
func (r *Registry) ActivateSessionTemplate(conversationID, templateName string, args []string) error
func (r *Registry) DeactivateSessionTemplate(conversationID, templateName string) error
func (r *Registry) ClearSessionTemplates(conversationID string) error
func (r *Registry) GetActiveTemplates(conversationID string) []ActiveTemplate
func (r *Registry) SessionTemplateContext(conversationID string) string
```

`Substitute` is the primary convenience method: looks up the template by name and applies argument substitution. Returns an error if the template is not found.

`SessionStore` tracks which templates are active for each conversation:

```go
// internal/templates/session_store.go

type ActiveTemplate struct {
    Name      string        `json:"name"`
    SubstitutedBody string  `json:"substituted_body"` // Body with args already applied
    ActivatedAt time.Time   `json:"activated_at"`
    CharCount  int           `json:"char_count"`
}

type SessionStore struct {
    mu       sync.RWMutex
    sessions map[string][]ActiveTemplate  // conversationID -> active templates
}
```

`SessionTemplateContext` assembles all active session-scoped templates for a conversation into a single fenced block for injection into the agent's system prompt. This is called by the prompt builder on every turn.

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
- `inject=true`: the substituted template text is injected into the agent's context/prompt. The injection duration depends on the template's `scope`:
  - `scope: turn` (default): injected for the current turn only, then discarded.
  - `scope: session`: added to the conversation's active template list. The prompt builder includes it on every subsequent turn until explicitly cleared.

**New builtin tool**: `template_list`

```go
// internal/tools/builtin/tool_template_list.go

// Tool definition for agents to discover available templates.
// Parameters:
//   - active (optional, bool): if true, list only currently active session-scoped templates
// Returns list of template names, descriptions, and scope.
```

This lets agents discover templates at runtime and choose which to invoke based on the task. With `active=true`, agents can inspect what session-scoped templates are currently influencing their behavior.

**New builtin tool**: `template_clear`

```go
// internal/tools/builtin/tool_template_clear.go

// Tool definition for agents to remove active session-scoped templates.
// Parameters:
//   - name (optional): specific template to deactivate. If omitted, clears all active templates.
// Returns the list of deactivated template names.
```

This lets agents (and users via slash commands) clean up session-scoped templates when they're no longer relevant. Without arguments, `/template_clear` removes all active session-scoped templates for the conversation.

### 3.7 CLI Command

```
meept templates list [--json] [--tag <tag>]
meept templates show <name> [--json]
meept templates invoke <name> [args...]
meept templates clear [<name>]
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
| **Template explosion**: Agents over-use templates, injecting many into context, bloating prompts | Medium | Strict size limits: max 3 turn-scoped templates per turn, max 5 session-scoped total, max 2000 chars per template body. Enforced in `template_invoke` tool. |
| **Session-scoped context bloat**: Accumulated session templates consume too much of the context window over time | Medium | Hard char limits (`MaxSessionScopedCharsTotal = 8000`). `template_list` with `active: true` lets agents inspect and self-manage. TUI shows active session templates as a status indicator. |
| **Name collisions**: Template name conflicts with skill name or builtin command | Low | Check order: builtins > skills > templates. Templates are lowest priority in slash-command resolution. |
| **Prompt injection via templates**: Malicious template content injected into agent context | Medium | Templates go through the same `InputSanitizer` as other user content. The `inject=true` path wraps content in `<template-context>` tags with a system note (same pattern as memory context fencing). |
| **Template as skill confusion**: Users unsure whether to create a template or a skill | Low | Documentation. Rule of thumb: if it needs model resolution, tool filtering, or iteration limits, use a skill. Otherwise, use a template. |
| **Discovery performance**: Scanning many template directories on every lookup | Low | Same approach as skills: discover once at startup, cache in registry, reload on signal. |
| **Scope misuse**: Users create `scope: session` templates for one-shot prompts, bloating context unnecessarily | Low | Default scope is `turn`. Session scope is opt-in. Documentation with examples showing appropriate use of each scope. |

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
| `internal/templates/registry.go` | Name-based registry with `Substitute()` convenience and session-scoped template management |
| `internal/templates/session_store.go` | Per-conversation active template tracking for session-scoped templates |
| `internal/templates/registry_test.go` | Registry tests |
| `internal/templates/session_store_test.go` | Session store tests: activate, deactivate, clear, limit enforcement |

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
| `internal/tools/builtin/tool_template_invoke.go` | Agent tool for invoking/injecting templates (turn and session scope) |
| `internal/tools/builtin/tool_template_list.go` | Agent tool for listing available or active templates |
| `internal/tools/builtin/tool_template_clear.go` | Agent tool for clearing session-scoped templates |
| `internal/tools/builtin/tool_template_invoke_test.go` | Tests |
| `internal/tools/builtin/tool_template_list_test.go` | Tests |
| `internal/tools/builtin/tool_template_clear_test.go` | Tests |

**Modified files:**

| File | Change |
|------|--------|
| `internal/agent/dispatcher.go` | In `ClassifyAndRoute`, after skill check, check template registry. If template found, substitute args and treat as user input. Add `templateRegistry` field to `Dispatcher` struct and `DispatcherConfig`. |
| `internal/daemon/components.go` | Wire template registry and session store into dispatcher config |
| `internal/tools/builtin/platform.go` | Register `template_invoke`, `template_list`, and `template_clear` as baseline tools |
| `internal/agent/prompt.go` | Add `WithSessionTemplates()` method, update `DefaultCoworkerAwareness()` |

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

| File | Scope | Description |
|------|-------|-------------|
| `config/templates/summarize.md` | turn | Concise text summarization |
| `config/templates/explain.md` | turn | Step-by-step explanation |
| `config/templates/translate.md` | turn | Language translation |
| `config/templates/format-json.md` | turn | JSON validation and pretty-printing |
| `config/templates/code-review-checklist.md` | turn | Code review checklist for agent injection |
| `config/templates/role-senior-go-dev.md` | session | Senior Go developer persona |
| `config/templates/role-security-auditor.md` | session | Security auditor persona |
| `config/templates/role-junior-dev-reviewer.md` | turn | Junior-dev-friendly code review |
| `config/templates/convention-go-project.md` | session | Go project coding conventions |
| `config/templates/convention-no-external-deps.md` | session | Prefer stdlib, no new dependencies |
| `config/templates/convention-commit-style.md` | session | Conventional commit message style |
| `config/templates/recovery-test-failure.md` | turn | Test failure diagnosis procedure |
| `config/templates/recovery-build-error.md` | turn | Build error diagnosis procedure |
| `config/templates/recovery-merge-conflict.md` | turn | Merge conflict resolution procedure |

### 5.5 Sample Template Files

**`config/templates/summarize.md`**
```markdown
---
name: summarize
description: "summarize text concisely"
scope: turn
---

Summarize the following text in 2-3 sentences.
Focus on the key points and actionable takeaways.

$@
```

**`config/templates/role-senior-go-dev.md`**
```markdown
---
name: role-senior-go-dev
description: "adopt a senior Go developer persona with deep stdlib knowledge and pragmatic engineering instincts"
scope: session
---

You are a senior Go developer with 10+ years of experience building production systems.

Engineering principles:
- Prefer the standard library over third-party packages. Only reach for external dependencies when the stdlib genuinely cannot do the job (e.g., no stdlib CSV writer with custom delimiters).
- Favor explicit over implicit. Prefer clear, readable code over clever abstractions.
- Use table-driven tests. Organize tests by behavior, not by function.
- Handle errors explicitly. Wrap errors with context using `fmt.Errorf("doing X: %w", err)`. Never swallow errors silently.
- Use `log/slog` for structured logging. No `fmt.Println` in production code.
- Prefer small, composable interfaces. Accept interfaces, return structs.
- Use context propagation throughout. Every function that does I/O takes a `context.Context` as its first parameter.

Code style:
- Group related imports: stdlib, then external, then internal. Separate with blank lines.
- Use meaningful variable names. Avoid single-letter names except for brief loops (`for i, v := range`).
- Keep functions short. If it exceeds 50 lines, extract helper(s).
- Use defer for cleanup (Close, Unlock, etc.).

Review approach:
- Flag potential race conditions, goroutine leaks, and unbounded channels.
- Check error handling paths: are all errors propagated or logged?
- Verify context cancellation is respected in long-running operations.
- Look for resource leaks (unclosed response bodies, etc.).
```

**`config/templates/role-security-auditor.md`**
```markdown
---
name: role-security-auditor
description: "adopt a security auditor persona focused on OWASP top 10 and defensive coding"
scope: session
---

You are a security auditor conducting a thorough review of this codebase.

Your primary focus areas:

**Input validation:**
- Trace all user-supplied data from entry points to usage sites.
- Check for injection vulnerabilities: SQL, command, template, LDAP, header injection.
- Verify all external input is validated, sanitized, and length-checked.
- Look for path traversal, SSRF, and open redirect vectors.

**Authentication and authorization:**
- Verify authentication is enforced on all sensitive endpoints.
- Check for privilege escalation paths.
- Look for insecure direct object references (IDOR).
- Verify session management is secure (token rotation, expiry, secure flags).

**Data handling:**
- Check for sensitive data in logs, error messages, and URLs.
- Verify encryption at rest and in transit for sensitive data.
- Look for hardcoded secrets, API keys, or credentials.
- Verify proper use of constant-time comparison for secrets.

**Dependencies and configuration:**
- Flag known-vulnerable dependency patterns.
- Check for overly permissive CORS, CSP, or security headers.
- Verify TLS configuration (no outdated protocols, proper cert validation).

**Common Go-specific risks:**
- Check `exec.Command` calls for shell injection.
- Verify `html/template` is used (not `text/template`) for HTML output.
- Look for `unsafe` package usage.
- Check file operations for symlink attacks and race conditions (TOCTOU).

Reporting format:
- Rate each finding as Critical / High / Medium / Low / Informational.
- Provide a proof-of-concept or specific line reference for each finding.
- Suggest a concrete fix for each issue.
```

**`config/templates/role-junior-dev-reviewer.md`**
```markdown
---
name: role-junior-dev-reviewer
description: "review code as if explaining to a junior developer, highlighting learning opportunities"
scope: turn
---

Review the following code with a junior developer in mind.

For each piece of feedback:
1. Explain **what** the issue is in plain language.
2. Explain **why** it matters -- what could go wrong, or what principle it violates.
3. Show the **fix** with a code example.
4. If there's a learning opportunity, link the pattern to a broader concept (e.g., "this is an example of the 'accept interfaces, return structs' pattern in Go").

Tone: encouraging and educational. Celebrate good patterns you see. Frame issues as improvements, not mistakes.

Organize your review by:
- **Must fix**: Bugs, security issues, or correctness problems.
- **Should fix**: Performance, readability, or maintainability concerns.
- **Nice to know**: Style suggestions or alternative approaches to learn from.

Code to review:

$@
```

**`config/templates/convention-go-project.md`**
```markdown
---
name: convention-go-project
description: "enforce standard Go project conventions: testing, logging, error handling, and code organization"
scope: session
---

Follow these Go project conventions for all code you write or modify:

**Testing:**
- Write table-driven tests using `t.Run` for subcases.
- Test file naming: `*_test.go` in the same package (whitebox) unless testing requires blackbox access (then use `package_test`).
- Use `testify/assert` only if the project already depends on it; otherwise use stdlib `testing`.
- Cover error paths, not just happy paths.
- Use `t.Parallel()` where safe.

**Logging:**
- Use `log/slog` exclusively. No `log.Println` or `fmt.Println` for logging.
- Use structured logging: `slog.Info("message", "key", value, "key2", value2)`.
- Log at the appropriate level: Debug for internals, Info for state changes, Warn for degraded operation, Error for failures.

**Error handling:**
- Wrap errors with context: `fmt.Errorf("reading config: %w", err)`.
- Use `errors.Is()` and `errors.As()` for error checking, not string matching.
- Define sentinel errors with `var ErrSomething = errors.New("something")` in a `errors.go` file.
- Return errors up the call stack. Handle exactly once (log OR return, never both).

**Code organization:**
- Group by domain, not by type (no `models/`, `handlers/`, `services/` top-level dirs). Instead: `internal/user/`, `internal/order/`, etc.
- Keep `internal/` for packages not importable by external code.
- Use `cmd/` for entry points, one directory per binary.
- Place shared types in `internal/` sub-packages, not in a root `pkg/` directory.

**Naming:**
- Packages: lowercase, single word, no underscores. `user`, not `userManagement`.
- Exported names: use `CamelCase`. Avoid stutter (`user.User` is acceptable; `user.UserModel` is not).
- Interfaces: typically one method, named with `-er` suffix (`Reader`, `Stringer`, `TaskRunner`).
- Acronyms are consistently cased: `HTTPClient`, not `HttpClient`.
```

**`config/templates/convention-no-external-deps.md`**
```markdown
---
name: convention-no-external-deps
description: "prefer stdlib solutions and avoid adding new external dependencies"
scope: session
---

Do not introduce new external dependencies. Before suggesting any package that is not already in go.mod:

1. Check if the Go standard library can accomplish the same goal.
2. Check if an existing dependency already provides the needed functionality.
3. If neither is true, explicitly call out that a new dependency is needed and explain why there is no stdlib or existing-dep alternative.

This applies to:
- HTTP routing, middleware, and server utilities
- JSON/YAML/TOML parsing
- File system operations
- Testing utilities
- CLI argument parsing
- Logging
- Time formatting and parsing
- String manipulation and regex
- Cryptographic operations
- Concurrency primitives

Common stdlib alternatives to prefer:
- `net/http` instead of chi, gin, echo, fiber
- `encoding/json` instead of json-iterator, easyjson
- `log/slog` instead of zap, zerolog, logrus
- `html/template` instead of mustache, pongo2
- `testing` + `net/http/httptest` instead of testify (unless already present)
- `os/exec` instead of go-commandbus or similar
- `sync` primitives instead of concurrent maps from external libs
```

**`config/templates/convention-commit-style.md`**
```markdown
---
name: convention-commit-style
description: "enforce conventional commit message format with structured body"
scope: session
---

All commit messages must follow the Conventional Commits format:

```
<type>(<scope>): <short summary>

<optional body with context>

<optional footer(s)>
```

Rules:
- **type** must be one of: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `build`, `ci`, `style`
- **scope** is optional but encouraged: the package or component affected (e.g., `agent`, `llm`, `memory`)
- **short summary**: lowercase, imperative mood ("add feature" not "added feature"), no period, max 72 chars
- **body** (optional): explain WHY, not WHAT. The diff shows what changed. Wrap at 72 chars.
- **footer** (optional): breaking changes (`BREAKING CHANGE: description`), issue references (`Closes #123`)

Examples:
```
feat(agent): add template injection to agent loop
fix(memory): prevent duplicate entries in FTS index
refactor(security): extract input sanitizer into standalone package
docs(config): document new template discovery directories
```

When suggesting commit messages, always provide the full message including body if the change needs context.
```

**`config/templates/recovery-test-failure.md`**
```markdown
---
name: recovery-test-failure
description: "systematic procedure for diagnosing and fixing test failures"
scope: turn
---

A test has failed. Follow this procedure to diagnose and fix it.

**Step 1: Read the failure output carefully.**
- Identify the exact test name, file, and line number.
- Read the error message and any diff output completely before acting.
- Determine: is this a assertion failure, a panic, a timeout, or a compilation error?

**Step 2: Reproduce the failure in isolation.**
- Run only the failing test: `go test -run TestName -v ./path/to/package/`
- If the failure is intermittent, run with `-count=N` to check for flakiness.
- Check if the failure is environment-dependent (missing env vars, network, file paths).

**Step 3: Identify the root cause.**
- Read the test code and the code under test. Do not assume -- read both.
- Common root causes, in order of likelihood:
  1. The test assertions don't match recent changes to the code under test.
  2. The code under test has a regression (new bug introduced by recent changes).
  3. Test state pollution (shared mutable state, unclosed resources, goroutine leaks).
  4. Race condition (run with `-race` to check).
  5. Time-dependent logic (use fake clocks or fixed timestamps).
  6. External dependency change (API contract changed, service down).

**Step 4: Fix the minimal necessary change.**
- Fix the root cause, not the symptom. Do not weaken assertions to make the test pass.
- If the test itself was wrong, fix the test AND add a comment explaining the correct expected behavior.
- If the code under test has a bug, fix the bug. Do not add workarounds in the test.

**Step 5: Verify the fix.**
- Run the failing test in isolation: it should pass.
- Run the full package tests: nothing else should break.
- If the fix touched shared code, run the full test suite: `go test ./...`

**Step 6: Document if non-obvious.**
- If the root cause was subtle, add a comment in the test or code explaining the invariant.
- If the failure revealed a gap in test coverage, add a regression test.

Test failure details:

$@
```

**`config/templates/recovery-build-error.md`**
```markdown
---
name: recovery-build-error
description: "systematic procedure for diagnosing and fixing build errors"
scope: turn
---

The build has failed. Follow this procedure to diagnose and fix the error.

**Step 1: Read the full error output.**
- Identify the file, line number, and error message for each compilation error.
- Distinguish between: syntax errors, type errors, undefined references, import errors, and linker errors.
- Fix errors in order from top to bottom -- earlier errors often cause cascading failures below.

**Step 2: Check for common Go build issues.**
- **Undefined reference**: Did you rename or move a function/type? Update all call sites.
- **Import cycle**: Package A imports B which imports A. Restructure by extracting shared types into a third package.
- **Unused import/variable**: Remove it, or use `_ = varName` if intentionally unused (blank import for side effects: `_ "pkg"`).
- **Type mismatch**: Check interface satisfaction. A `*ConcreteType` assigned to an interface is non-nil even if the pointer is nil -- use typed-nil guards.
- **Missing method**: The concrete type doesn't satisfy the interface. Add the missing method or adjust the interface.

**Step 3: Check for cascading errors.**
- Fix the first error, then rebuild. Often 10 errors collapse to 1-2 real issues.
- Do not attempt to fix all errors simultaneously if they may be related.

**Step 4: Verify module state.**
- Run `go mod tidy` to ensure go.mod and go.sum are consistent.
- Check `go.sum` for conflicts if merging branches.
- Verify the Go version matches: `go version` vs `go.mod` directive.

**Step 5: Verify the fix.**
- Run `go build ./...` -- the full project should compile.
- Run `go vet ./...` -- catch issues the compiler doesn't flag.
- Run `go test ./...` -- ensure no tests were broken by the fix.

Build error output:

$@
```

**`config/templates/recovery-merge-conflict.md`**
```markdown
---
name: recovery-merge-conflict
description: "systematic procedure for resolving git merge conflicts"
scope: turn
---

There are merge conflicts to resolve. Follow this procedure.

**Step 1: Understand the conflict scope.**
- Run `git diff --name-only --diff-filter=U` to list conflicted files.
- For each file, run `git diff <file>` to see all conflict markers.
- Read the commit messages of both branches to understand intent:
  - `git log --oneline HEAD...MERGE_HEAD` for the incoming changes.
  - `git log --oneline MERGE_HEAD...HEAD` for our changes.

**Step 2: Resolve each conflict.**
- Open the conflicted file. Each conflict looks like:
  ```
  <<<<<<< HEAD
  our version
  =======
  their version
  >>>>>>> branch-name
  ```
- For each conflict block:
  1. Read both versions carefully. Understand what each side was trying to achieve.
  2. Decide: keep ours, keep theirs, or merge both changes.
  3. The correct resolution is usually **both** -- integrate the incoming change into our modified code (or vice versa). Do not blindly pick one side.
  4. Remove the conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`).
  5. Verify the resolved code compiles and makes logical sense.

**Step 3: Verify the resolution.**
- For Go code: run `go build ./path/to/package/` to check compilation.
- Run `go vet ./path/to/package/` to catch issues.
- If tests exist for the affected code, run them.
- Read the resolved file end-to-end to catch orphaned conflict markers or garbled code.

**Step 4: Stage and verify.**
- `git add <resolved-files>`
- Run `git status` to confirm no remaining unmerged paths.
- Run `go build ./...` to verify the full project compiles.
- Commit with a descriptive message noting the merge and any non-trivial resolutions.

**Common pitfalls:**
- Don't leave conflict markers in the file.
- Don't accidentally delete code from one side that the other side depends on.
- Watch for duplicate imports after merging -- both sides may have added the same import.
- If a file was renamed on one side and modified on the other, `git` may not handle this well. Check for orphaned files.

Conflicted files:

$@
```

## 6. Integration Points

### 6.1 Templates vs Skills

| Aspect | Templates | Skills |
|--------|-----------|--------|
| Frontmatter | `name`, `description`, `scope` | `name`, `description`, `requires`, `tags`, `allowed-tools`, `risk-level`, `max-iterations`, `temperature`, `max-tokens`, `examples` |
| Body | Prompt text with `$N`/`$@` substitution | Full agent instructions |
| Persistence | `scope: turn` (default) or `scope: session` | Per-invocation |
| Model resolution | Uses current model (no resolution) | Resolves model via `llm.Resolver` based on `requires` |
| Execution | Text substitution, then normal chat | Dedicated `Executor` with system message |
| Tool filtering | None | `allowed-tools` restricts available tools |
| Risk management | None | `risk-level` gates execution |
| Iteration control | Single turn | `max-iterations` controls agent loop |
| Use case | Prompt shortcuts, behavioral modifiers, recovery procedures | Complex multi-step agent behaviors |

**Coexistence rule**: If a name exists as both a skill and a template, the skill wins. This is enforced by the dispatcher check order.

### 6.2 Agent Loop Integration

Templates integrate with the agent loop at two points:

1. **Slash-command routing** (dispatcher level): `/template-name args` is intercepted before intent classification, substituted, and routed as normal user input.

2. **Agent tool invocation** (loop level): Agents call `template_invoke` during execution. With `inject=false`, the substituted text becomes a user message in the conversation. With `inject=true`, it is injected as context fencing. The persistence depends on scope:
   - `scope: turn`: injected into the current system prompt only, then discarded.
   - `scope: session`: added to the conversation's `SessionStore`, automatically included in every subsequent system prompt until cleared.

3. **Prompt building** (every turn): The prompt builder calls `registry.SessionTemplateContext(conversationID)` to assemble all active session-scoped templates. These are included in the system prompt alongside memory context and coworker awareness.

The context injection path uses the same fencing pattern as memory context:

```xml
<template-context>
[System note: The following is an injected template, NOT new user input.
It provides additional instructions for this turn only.]

...substituted template body...
</template-context>
```

For session-scoped templates, the fencing indicates persistence:

```xml
<template-context scope="session">
[System note: The following templates are active for this session.
They persist until explicitly cleared via template_clear.
Currently active: role-senior-go-dev, convention-go-project]

--- Template: role-senior-go-dev ---
...substituted template body...

--- Template: convention-go-project ---
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

The existing `prompt.Builder` (`internal/agent/prompt/builder.go`) supports `${VAR}` interpolation via `PromptContext.Variables`. Template substitution is a different mechanism (positional args, not named vars) and operates at a different layer (before the prompt reaches the builder).

Two changes to the prompt builder:

1. **New method**: `WithSessionTemplates(context string)` -- injects session-scoped template context into the system prompt. Called by the agent loop before each turn if the template registry has active session-scoped templates for the conversation.

2. **Update `DefaultCoworkerAwareness()`** to mention template availability:

```go
// Add to DefaultCoworkerAwareness():
// - **template_invoke**: Invoke a prompt template with arguments. Templates with scope:session persist until cleared.
// - **template_list**: List available prompt templates, or show currently active session templates with `active: true`.
// - **template_clear**: Remove active session-scoped templates for this conversation.
```

## 7. Testing Strategy

| Area | Approach |
|------|----------|
| Substitution engine | Table-driven tests covering all patterns, edge cases (missing args, out-of-range indices, empty args) |
| Parser | Test valid templates, missing frontmatter, missing name, malformed YAML, scope field parsing |
| Discovery | Test with temp directories, verify priority shadowing, non-existent tiers |
| Registry | Concurrent access tests, name normalization |
| Session store | Activate/deactivate lifecycle, limit enforcement (max templates, max chars), clear operations, concurrent access per conversation |
| CLI | Integration tests against daemon RPC |
| Agent tools | Unit tests with mock registries, test scope behavior (turn vs session), test clear operations |
| Dispatcher | Test slash-command routing with both skills and templates present |
| Prompt builder | Verify session-scoped templates are included in system prompt, verify fencing format |
| TUI | Manual testing (TUI tests are integration-level) |

## 8. Configuration

No new configuration file is needed. Template directories follow the existing convention (`.meept/templates/`, `~/.meept/templates/`). If users want to customize template locations, this can be added to `meept.json5` in a future iteration.

Template size limits (for agent injection) can be configured via constants in `internal/templates/registry.go`:

```go
const (
    MaxTemplateBodySize         = 4096   // Maximum template body size in characters
    MaxTemplatesPerTurn         = 3      // Maximum turn-scoped templates an agent can inject per turn
    MaxInjectedCharsTotal       = 6000   // Maximum total injected template characters per turn
    MaxSessionScopedTemplates   = 5      // Maximum concurrently active session-scoped templates
    MaxSessionScopedCharsTotal  = 8000   // Maximum total chars from session-scoped templates
)
```

## 9. Open Questions

1. **Shared frontmatter parsing**: Should we extract `splitFrontmatter()` from `skills/parser.go` into a shared `internal/frontmatter/` package? This would avoid code duplication but adds a new package. Decision: defer until a third consumer appears.

2. **Template hot-reload**: Should the daemon watch template directories for changes and reload automatically? Skills currently do not hot-reload. Decision: match skill behavior -- no hot-reload. Templates are loaded at startup and can be reloaded via daemon restart or a future `/reload` command.

3. **Template composition**: Should templates be able to reference other templates (e.g., `$template:other-name`)? Decision: no in v1. This adds complexity and is better addressed by skill-level composition if needed.

4. **HTTP API endpoints**: Should templates be exposed via the REST API for the menubar app? Decision: yes, but as a follow-up. Phase 2 adds RPC support; HTTP can be added in a subsequent PR using the existing service layer pattern.

5. **Scope escalation**: Should agents be allowed to promote a turn-scoped template to session-scoped at runtime (e.g., an agent decides a behavioral template should persist)? Decision: no in v1. The scope is a property of the template file, not a runtime decision. If a user wants a session-scoped variant, they create a separate template with `scope: session`.

6. **Session-scoped template persistence across daemon restarts**: Should active session-scoped templates survive daemon restarts? Decision: no in v1. Session-scoped templates are in-memory only. On restart, the user or agent re-activates them. This matches how conversation state works today.
