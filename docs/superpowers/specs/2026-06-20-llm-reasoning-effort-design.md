# LLM Reasoning Effort Support

**Date:** 2026-06-20
**Status:** Design — pending implementation
**Topic:** Add tunable reasoning/thinking effort across all LLM providers (Anthropic, OpenAI, Qwen, DeepSeek, GLM, Gemini, Kimi, Grok, Llama), surfaced through agent config, per-request overrides, natural-language prompts, and full UI/API wiring.

## Goals

1. **Single typed abstraction** that captures every vendor's reasoning configuration dimensions (named tier, token budget, on/off, force flag).
2. **Per-agent config** so admins can set a default reasoning tier per agent role (e.g. planner defaults to `xhigh`, committer to `low`).
3. **Per-model default** so `config/models.json5` can declare `"default_reasoning": {"effort": "medium"}` for thinking-capable models.
4. **Per-request override** through CLI flag, HTTP API, RPC, and natural-language prompt.
5. **Agent self-modulation** within admin-defined bounds so the loop can downshift between turns.
6. **Full UI wiring** per CLAUDE.md `Wiring/Integration Requirement`: CLI flag, TUI keybinding/indicator, Flutter widget, HTTP endpoints, RPC methods, ConfigUI sections.
7. **Response surfacing** of reasoning content (Anthropic thinking blocks, DeepSeek `reasoning_content`, Qwen thinking deltas) on `Response.Reasoning` for self-reflection and UI display.
8. **Fix Bedrock/OpenRouter Claude routing** so thinking is enabled for Claude accessed indirectly.

## Non-Goals (Out of Scope)

- Native Gemini `generateContent` driver (using OpenAI-compat only; Q8a).
- Per-message reasoning tier within a single LLM call (request-level only).
- Cross-call tier learning / persisted per-session optimization.
- TLS/cost budget integration with reasoning tiers (cost is tracked via existing metrics on response).

## Design Decisions (from brainstorm)

| # | Decision | Choice | Rationale |
|---|---|---|---|
| Q1 | Effort taxonomy | OpenAI-aligned `none\|low\|medium\|high\|xhigh\|max` | Maps cleanly to OpenAI/Grok; losslessly translates to all others |
| Q2 | Config surface | Both `AgentConstraints.Reasoning` and `ModelConfig.DefaultReasoning` | Admins get per-agent control; users get per-model sensible defaults |
| Q3 | Override sources | Dispatcher NL parse + agent self-modulation (admin-bounded) | User-driven directives + autonomous optimization |
| Q4 | Precedence | Prompt > Agent > Model > nothing | User is in control |
| Q5 | NL triggers | Tier words + aliases + token hints + dispatcher intent classifier | Maximum natural interaction |
| Q6 | Capability gating | Strict-by-default with `Force` escape hatch | Safe defaults, power-user override |
| Q7 | Anthropic budget mapping | Tier→token table, configurable, user-overridable | Sensible defaults, escape hatches at every layer |
| Q8 | Gemini transport | OpenAI-compat only | Defers native driver work |
| Q9 | Reasoning content | Parsed and surfaced on `Response.Reasoning` | UI display + agent self-reflection |
| Q10 | Bedrock/OpenRouter Claude fix | Yes, in-scope | Current gap silently loses thinking |
| Q11 | ConfigUI | Per-agent + per-model sections | Symmetric with how defaults are set |
| Q12 | Phasing | 3 phases: core+OpenAI+Anthropic → other vendors → NL parser + UI | Each phase is a complete, verifiable unit |

## Section 1: Core Abstraction

New file: `internal/llm/reasoning.go`.

```go
package llm

// ReasoningEffort values. The zero value (empty string) means "do not send
// any reasoning field" — the model uses its provider default.
const (
    ReasoningNone   = "none"   // explicitly disable thinking
    ReasoningLow    = "low"
    ReasoningMedium = "medium"
    ReasoningHigh   = "high"
    ReasoningXHigh  = "xhigh"
    ReasoningMax    = "max"
)

// ReasoningConfig captures LLM reasoning/thinking configuration across vendors.
// A nil pointer or zero-value struct means "do not send" — defer to provider
// default. Vendors translate this into their native wire format.
type ReasoningConfig struct {
    // Effort is the named tier. Empty = don't send (use provider default).
    // "none" = send explicit disable when the vendor supports it.
    Effort string `json:"effort,omitempty"`

    // BudgetTokens overrides tier→budget mapping. When non-nil, used as the
    // raw thinking budget for vendors that accept token counts (Anthropic,
    // GLM, Kimi, Qwen). Ignored by vendors that only accept named tiers
    // (OpenAI, xAI, Gemini-compat).
    BudgetTokens *int `json:"budget_tokens,omitempty"`

    // Enabled explicitly toggles thinking on/off for vendors with a boolean
    // toggle (Qwen enable_thinking, GLM thinking.enabled). When nil, derived
    // from Effort (nil or any tier other than "none" → true).
    Enabled *bool `json:"enabled,omitempty"`

    // Force bypasses capability gating. Use when a model supports thinking
    // but lacks the "reasoning"/"extended_thinking" capability tag. Logs a
    // warning when invoked.
    Force bool `json:"force,omitempty"`
}

// IsZero reports whether the config carries no meaningful fields.
func (r *ReasoningConfig) IsZero() bool {
    if r == nil { return true }
    return r.Effort == "" && r.BudgetTokens == nil && r.Enabled == nil && !r.Force
}

// ResolveEnabled returns the effective on/off state.
func (r *ReasoningConfig) ResolveEnabled() bool {
    if r == nil { return false }
    if r.Enabled != nil { return *r.Enabled }
    return r.Effort != "" && r.Effort != ReasoningNone
}

// Validate returns an error if fields conflict (e.g. Enabled=false but
// Effort=high).
func (r *ReasoningConfig) Validate() error { /* ... */ }

// AgentReasoningConfig is the per-agent config form. It adds admin-defined
// bounds for self-modulation.
type AgentReasoningConfig struct {
    // Initial effort tier used at agent startup and as fallback when the
    // loop hasn't self-modulated.
    Effort string `json:"effort,omitempty"`

    // AllowSelfModulation permits the agent loop to change effort between
    // turns. Default false.
    AllowSelfModulation bool `json:"allow_self_modulation,omitempty"`

    // MinEffort / MaxEffort bound self-modulation. Empty = no bound on
    // that side.
    MinEffort string `json:"min_effort,omitempty"`
    MaxEffort string `json:"max_effort,omitempty"`

    // BudgetTokens is passed through to ReasoningConfig.
    BudgetTokens *int `json:"budget_tokens,omitempty"`

    // Force bypasses capability gating (forwarded to ReasoningConfig).
    Force bool `json:"force,omitempty"`
}

// ToReasoningConfig converts the agent config into a request-level
// ReasoningConfig at the given effective tier.
func (a *AgentReasoningConfig) ToReasoningConfig(effort string) *ReasoningConfig { /* ... */ }

// ClampEffort returns effort bounded by [MinEffort, MaxEffort]. Used by the
// agent loop when applying self-modulation.
func (a *AgentReasoningConfig) ClampEffort(effort string) string { /* ... */ }
```

### Effort ordering

```go
var effortOrder = map[string]int{
    "":       0,
    "none":   0,
    "low":    1,
    "medium": 2,
    "high":   3,
    "xhigh":  4,
    "max":    5,
}
```

`ClampEffort` uses this for `min_effort ≤ requested ≤ max_effort` checks.

### Budget mapping (configurable)

Defaults (configurable via `config/meept.json5`):

```json5
reasoning: {
    budgets: {
        low:    2000,
        medium: 8000,
        high:   16000,
        xhigh:  32000,
        max:    64000,
    },
},
```

Precedence for resolving a budget:
1. `ReasoningConfig.BudgetTokens` (per-request)
2. `AgentReasoningConfig.BudgetTokens` (per-agent)
3. `ModelConfig.DefaultReasoning.BudgetTokens` (per-model)
4. `reasoning.budgets.<tier>` from `config/meept.json5`
5. Hard-coded default table above

## Section 2: Tier → Wire-Format Translation

New file: `internal/llm/reasoning_translate.go`.

### Vendor field mapping

| Vendor | ProviderID(s) | Wire field(s) | Translation |
|---|---|---|---|
| OpenAI | `openai` | `reasoning_effort` (top-level) | `effort` direct (omit for `none`/empty) |
| xAI | `xai` | `reasoning_effort` | same as OpenAI; xAI supports `low`/`high` only — clamp `medium`→`low`, `xhigh`/`max`→`high` |
| Gemini (OpenAI-compat) | `google` | `extra_body.reasoning_effort` | direct (mapped to `reasoningConfig.thinkingBudget` server-side) |
| Anthropic (direct) | `anthropic` | `thinking: {type:"enabled", budget_tokens:N}` | budget from §1 mapping or `BudgetTokens`; `type:"enabled"` when effort≠none |
| Anthropic (Bedrock) | `bedrock` (model contains "claude") | same as direct, Bedrock body format | same translation |
| Anthropic (OpenRouter) | `openrouter` (model contains "claude") | same as direct | same translation |
| Qwen3 / Qwq | provider matches `qwen`/`ollama` (qwen model) | `enable_thinking: bool` + `thinking_budget: int` | bool from `ResolveEnabled()`; budget from mapping |
| GLM | `zai` | `thinking: {type:"enabled", budget_tokens:N}` | mirror Anthropic |
| Kimi (Moonshot) | `moonshot` | `thinking: {type:"enabled", budget_tokens:N}` | mirror Anthropic |
| Grok | `grok` | `reasoning_effort` | mirror OpenAI |
| DeepSeek | `deepseek` | (none sent; `reasoning_content` in response) | passthrough; response parsing only |
| Llama / LFM | `local`, `ollama`, `together`, `groq` | (none) | passthrough |
| OpenRouter (non-Claude) | `openrouter` | `reasoning: {effort:X}` top-level + per-upstream field | dual-send (OpenRouter's meta-field + native upstream field when known) |

### Translation entry points

```go
// applyOpenAICompatReasoning mutates a chat-completion request body in place
// to add vendor-specific reasoning fields. Called by the OpenAI Client.
func applyOpenAICompatReasoning(body map[string]any, cfg *ModelConfig, rc *ReasoningConfig) { /* switch on cfg.ProviderID */ }

// applyAnthropicReasoning mutates an anthropicRequest in place.
func applyAnthropicReasoning(req *anthropicRequest, cfg *ModelConfig, rc *ReasoningConfig, budgets map[string]int) { /* ... */ }
```

Both functions:
1. Bail out (`return`) if `rc.IsZero()`.
2. Call `shouldSendReasoning(cfg, rc)` — bail if false.
3. Switch on `cfg.ProviderID` and write vendor fields.

### Capability gating

```go
func shouldSendReasoning(cfg *ModelConfig, rc *ReasoningConfig) bool {
    if rc.IsZero() { return false }
    if rc.Force {
        slog.Debug("reasoning force=true, bypassing capability gate",
            "model", cfg.ModelID, "provider", cfg.ProviderID)
        return true
    }
    return cfg.HasCapability(CapReasoning) || cfg.HasCapability(CapThinking)
}
```

Silently drop when gated. No error returned — the call still succeeds, just without reasoning fields.

## Section 3: Config Schema

### 3.1 `config/models.json5` — per-model default

Extend model entries:

```json5
"glm-5.2": {
    "name": "glm-5.2",
    "capabilities": ["completion", "code", "reasoning", "tool_use", "extended_thinking"],
    "default_reasoning": { "effort": "medium" },
    // ...existing fields
},
```

Schema (`internal/config/schema.go` `ModelConfig` struct):
```go
DefaultReasoning *ReasoningConfig `json:"default_reasoning,omitempty"`
```

### 3.2 `config/agents/<id>/AGENT.md` — per-agent override

Extend frontmatter (existing `constraints:` block):

```yaml
constraints:
  max_iterations: 15
  timeout_seconds: 600
  reasoning:
    effort: high
    allow_self_modulation: true
    min_effort: medium
    max_effort: xhigh
```

Schema (`internal/agent/spec.go` `AgentConstraints`):
```go
type AgentConstraints struct {
    // ...existing fields...
    Reasoning *AgentReasoningConfig `json:"reasoning,omitempty"`
}
```

### 3.3 `config/meept.json5` — global budgets

```json5
reasoning: {
    budgets: {
        low:    2000,
        medium: 8000,
        high:   16000,
        xhigh:  32000,
        max:    64000,
    },
},
```

Schema (`internal/config/schema.go`):
```go
type ReasoningGlobalConfig struct {
    Budgets map[string]int `json:"budgets,omitempty"`
}
// added to top-level Config struct as `Reasoning ReasoningGlobalConfig`
```

## Section 4: Plumbing Through the Call Stack

### 4.1 Precedence resolution

New function in `internal/llm/reasoning.go`:

```go
// ResolveReasoning walks the precedence chain and returns the effective
// ReasoningConfig for a single LLM call.
//
// Order (highest to lowest):
//   1. perRequest  — from CLI flag / HTTP body / RPC param / NL parse
//   2. agentSpec   — AgentReasoningConfig.Effort converted to ReasoningConfig
//   3. modelDefault — ModelConfig.DefaultReasoning
//   4. nil         — defer to provider (current behavior)
func ResolveReasoning(perRequest, agentSpec, modelDefault *ReasoningConfig) *ReasoningConfig {
    if perRequest != nil && !perRequest.IsZero() { return perRequest }
    if agentSpec != nil && !agentSpec.IsZero()   { return agentSpec }
    if modelDefault != nil && !modelDefault.IsZero() { return modelDefault }
    return nil
}
```

### 4.2 `ChatRequest` extension

```go
type ChatRequest struct {
    // ...existing fields...
    Reasoning *ReasoningConfig `json:"reasoning,omitempty"`
}
```

### 4.3 `ChatOption` for per-call override

```go
// WithReasoning sets the reasoning config for a single chat call.
// Overrides all other sources. Nil/zero = defer to lower-precedence sources.
func WithReasoning(rc *ReasoningConfig) ChatOption {
    return func(req *ChatRequest) { req.Reasoning = rc }
}
```

### 4.4 Agent loop wiring

`AgentLoop` holds:
- `agentReasoning *AgentReasoningConfig` (from spec at construction)
- `currentEffort string` (mutable; starts at `agentReasoning.Effort`)
- `reasoningOverride *ReasoningConfig` (set by dispatcher NL parse; per-session)

Each LLM call constructs the per-request config:
```go
effective := ResolveReasoning(
    l.reasoningOverride,                  // from dispatcher
    agentReasoning.ToReasoningConfig(l.currentEffort),  // from agent + current loop state
    modelCfg.DefaultReasoning,
)
chatter.Chat(ctx, messages, WithReasoning(effective))
```

### 4.5 Self-modulation API

```go
// SetReasoningForNextTurn adjusts the agent's current effort tier.
// ClampEffort applies the agent's [min_effort, max_effort] bounds.
// No-op when AllowSelfModulation is false.
func (l *AgentLoop) SetReasoningForNextTurn(effort string) { /* ... */ }
```

Typical patterns (suggested in agent system prompts, not hard-coded):
- Planner: `xhigh` initial → `low` for revision passes
- Debugger: `xhigh` until repro → `medium` after
- Coder: `high` for architecture, `low` for trivial edits
- Reviewer: `xhigh` for first pass, `medium` for re-review

## Section 5: Broker Routing Fix (Q10)

`internal/llm/broker.go:106-130`:

```go
func (b *ModelBroker) newChatterFor(cfg *ModelConfig) Chatter {
    // Detect Anthropic via provider, URL, or model name on Anthropic-shaped routes
    if isAnthropicRoute(cfg) {
        // ...build AnthropicClient...
    }
    // OpenAI-compat fallback
    // ...
}

// isAnthropicRoute returns true for direct Anthropic, Bedrock-Claude,
// and OpenRouter-Claude routes. All three use the Anthropic Messages API
// (Bedrock via its Anthropic-flavored endpoint; OpenRouter via its
// /anthropic passthrough when model starts with "anthropic/").
func isAnthropicRoute(cfg *ModelConfig) bool {
    if cfg.ProviderID == ProviderIDAnthropic { return true }
    if strings.Contains(strings.ToLower(cfg.BaseURL), "anthropic") { return true }
    if cfg.ProviderID == ProviderIDBedrock && strings.Contains(strings.ToLower(cfg.ModelID), "claude") { return true }
    if cfg.ProviderID == ProviderIDOpenRouter && strings.HasPrefix(strings.ToLower(cfg.ModelID), "anthropic/") { return true }
    return false
}
```

**Implementation note:** Bedrock's Anthropic endpoint uses a slightly different URL pattern (`/model/{modelId}/invoke` vs `/v1/messages`). The `AnthropicClient` may need a small URL-construction branch by `ProviderID`. If significant, defer Bedrock to Phase 2 and ship OpenRouter in Phase 1 (OpenRouter speaks raw Anthropic Messages API).

## Section 6: Response Handling (Q9a)

### 6.1 `Response.Reasoning` field

```go
type Response struct {
    Content      string     `json:"content,omitempty"`
    Reasoning    string     `json:"reasoning,omitempty"` // NEW
    ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
    // ...existing fields...
}
```

### 6.2 Anthropic parsing

In `anthropic.go` streaming handler, intercept content blocks with `type:"thinking"`:

```go
case "thinking":
    // accumulate delta.thinking into reasoningBuf
```

Non-stream: scan `response.Content` array for `{type:"thinking", thinking:"..."}` blocks.

### 6.3 OpenAI-compat parsing

In `client.go`, parse both streaming and non-streaming:

```go
// streaming
if delta, ok := chunk["delta"].(map[string]any); ok {
    if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
        reasoningBuf.WriteString(rc)
    }
}

// non-streaming
if rc, ok := msg["reasoning_content"].(string); ok {
    resp.Reasoning = rc
}
```

Applies to DeepSeek, Qwen, GLM, and any OpenAI-compat provider that emits `reasoning_content`.

### 6.4 UI streaming

Existing `ProgressStageThinking` callback now emits actual reasoning text (not just "Model is thinking..."). Streamed to:
- TUI thinking panel (collapsed by default, toggle with `t`)
- Flutter reasoning trace widget
- HTTP API: streamed as `{type:"reasoning", text:"..."}` events on WS/SSE

## Section 7: Natural-Language Parser (Q3a + Q5d)

New file: `internal/agent/reasoning_parser.go`. Sibling to `model_parser.go`.

### 7.1 Recognized forms

| Pattern | Maps to | Examples |
|---|---|---|
| Tier words + "reasoning" | direct tier | "use high reasoning", "set reasoning effort to xhigh", "reasoning: max" |
| "reasoning_effort: X" | direct tier | "reasoning_effort: low" |
| `[/reasoning X]` slash-directive | direct tier | `[/reasoning high]` |
| Aliases | mapped tier | "think hard" → high, "deep think"/"reason maximally"/"deep reasoning" → xhigh, "minimal thinking"/"quick" → low, "extended thinking" → high |
| Token hints | `BudgetTokens` | "use 8000 thinking tokens", "reasoning budget: 4000" |
| "stop thinking"/"no reasoning" | `none` | "stop thinking", "no reasoning", "disable thinking" |

### 7.2 API

```go
type ReasoningDirective struct {
    Config       *ReasoningConfig
    Scope        string  // "session" (default), "next-turn", "task"
    Ambiguous    bool    // "use reasoning" with no tier
    ReasoningReq string  // original phrase
}

func ParseReasoningDirective(text string) (*ReasoningDirective, error) { /* ... */ }
```

### 7.3 Dispatcher integration

In `internal/agent/dispatcher.go`, after model reassignment parse, run reasoning parse:

```go
if rc, err := ParseReasoningDirective(userInput); err == nil && rc != nil && !rc.Ambiguous {
    dispatchResult.ReasoningOverride = rc.Config
}
```

When ambiguous (user said "use reasoning" without tier), dispatcher includes a clarifying question alongside the model-clarification path (reuses existing `requestHandoff` mechanism).

### 7.4 DispatchResult threading

```go
type DispatchResult struct {
    // ...existing fields...
    ReasoningOverride *ReasoningConfig `json:"-"`
}
```

Tagged `json:"-"` to avoid serializing thinking config in audit logs (matches `Parts` precedent — reasoning configs are operational, not user-facing metadata).

`AgentLoop` receives `ReasoningOverride` at construction and uses it as the top of the precedence chain (§4.1).

### 7.5 Intent classifier hook

Dispatcher intent classifier (existing) emits a suggested tier based on task complexity:

| Intent | Suggested tier (only when user hasn't specified) |
|---|---|
| `IntentPlan` | `xhigh` |
| `IntentDebug` | `high` |
| `IntentResearch`/`IntentAnalyze` | `high` |
| `IntentCode` | `medium` |
| `IntentChat` | `low` (or none) |

Suggestion is only applied when: (a) no explicit user directive, AND (b) agent config has `allow_self_modulation: true`. The suggestion becomes the agent's initial `currentEffort`.

## Section 8: UI / API Surfaces

### 8.1 CLI (`cmd/meept/`)

```bash
# Per-message override
meept chat --reasoning high "plan a migration"
meept chat --reasoning-effort xhigh "analyze this architecture"
meept chat --reasoning-budget 16000 "deep think on this"

# Config management (existing commands extended)
meept config agents                     # TUI now shows reasoning fields
meept config get agents.coder.constraints.reasoning.effort
meept config set agents.coder.constraints.reasoning.effort high
meept config get models.glm-5.2.default_reasoning.effort
meept config set models.glm-5.2.default_reasoning.effort medium

# Runtime query
meept status --reasoning                # shows current tiers per agent
```

### 8.2 TUI (`internal/tui/`)

- `ctl-x r` — cycle active tier for current session (`none → low → medium → high → xhigh → max → none`)
- Status bar indicator: `[R:H]` (R:<tier letter>), empty when no reasoning active. Lowercase per CLAUDE.md UI conventions: `[r:h]`.
- `t` — toggle thinking panel (shows streamed `Response.Reasoning`)
- ConfigUI (`internal/configui/`):
  - `sections_agents.go`: new `reasoning` subsection with effort dropdown, self-modulation toggle, min/max dropdowns
  - `sections_models_reasoning.go` (new): per-model default reasoning section

### 8.3 Flutter (`ui/flutter_ui/`)

- Chat toolbar: reasoning tier dropdown (`none/low/medium/high/xhigh/max`)
- Toggle button to show/hide reasoning trace panel (per-message)
- Settings → Agents → edit agent → reasoning fields
- Settings → Models → edit model → default reasoning

### 8.4 HTTP API (`internal/comm/http/`)

| Method | Path | Body / Query | Purpose |
|---|---|---|---|
| `GET` | `/api/v1/reasoning` | — | List tiers, budget mapping, descriptions |
| `GET` | `/api/v1/reasoning/agents` | — | Current reasoning config per agent |
| `GET` | `/api/v1/reasoning/agents/:id` | — | Single agent reasoning config |
| `PUT` | `/api/v1/reasoning/agents/:id` | `{effort, allow_self_modulation, ...}` | Update agent reasoning config (persists to AGENT.md) |
| `GET` | `/api/v1/reasoning/models/:id` | — | Model default reasoning |
| `PUT` | `/api/v1/reasoning/models/:id` | `{effort}` | Update model default (persists to models.json5) |
| `GET` | `/api/v1/reasoning/budgets` | — | Current tier→budget mapping |
| `PUT` | `/api/v1/reasoning/budgets` | `{low, medium, high, xhigh, max}` | Update global budgets |
| `POST` | `/api/v1/sessions/:id/reasoning` | `{effort}` | Per-session override |
| `GET` | `/api/v1/sessions/:id/reasoning` | — | Current session effective tier |

Responses include `effective_effort` (computed) and `configured_effort` (admin-set).

### 8.5 RPC (`internal/rpc/`)

| Method | Params | Returns |
|---|---|---|
| `reasoning.list_tiers` | — | `[{name, description, default_budget}]` |
| `reasoning.get` | `{agent_id?}` | current config |
| `reasoning.set` | `{agent_id, effort, allow_self_modulation, min_effort, max_effort}` | ack |
| `reasoning.get_budgets` | — | `{low, medium, high, xhigh, max}` |
| `reasoning.set_budgets` | `{...}` | ack |
| `reasoning.get_model_default` | `{model_id}` | `{effort, budget_tokens}` |
| `reasoning.set_model_default` | `{model_id, effort}` | ack |

### 8.6 Menubar (`menubar/`)

New "reasoning" section in Settings:
- Per-agent effort picker (SwiftUI `Picker`)
- Global budgets editor
- Live indicator in menu bar dropdown: agent name + tier badge

## Section 9: Capability Tag Updates

Update `internal/llm/models_catalog.go` and `config/models.json5` with accurate capability tags and `default_reasoning`:

| Model | Capabilities (add) | default_reasoning |
|---|---|---|
| `claude-opus-4-7` | (already `CapThinking`) | `{effort: "medium"}` |
| `claude-sonnet-4-6` | add `extended_thinking` | (no default — let agent decide) |
| `gpt-5.4` | (add `reasoning`) | `{effort: "medium"}` |
| `gpt-4.1-mini` | (add `reasoning`) | (no default) |
| `gemini-2.5-pro` | (already has) | `{effort: "medium"}` |
| `deepseek-chat` | add `extended_thinking` | (passthrough — no default) |
| `glm-5.2` (if/when added) | add `extended_thinking` | `{effort: "medium"}` |
| `glm-4.7` | add `extended_thinking` | (no default) |
| Qwen3-thinking models | add `extended_thinking` when catalogued | (no default) |

`config/models.json5` follows the same pattern for locally-declared models.

## Section 10: Migration & Backward Compatibility

### 10.1 Existing `extended_thinking` capability

- Remains valid. `AnthropicClient` treats presence of the capability (with no `ReasoningConfig`) as "send `{type:"enabled"}` with no budget" — same wire format as today.
- New `ReasoningConfig` overrides this when set.
- Explicit `effort: "none"` overrides the legacy default and disables thinking entirely (sends no `thinking` field). This is the only case where a model with `extended_thinking` capability does not get a thinking block.

### 10.2 Schema additions

All new fields are pointers / `omitempty`. No breaking changes to JSON or JSON5 schemas. Old configs load cleanly.

### 10.3 Existing clients

`Client.Chat()` and `AnthropicClient.Chat()` continue to work unchanged when no `Reasoning` option is passed.

### 10.4 Migration helper

`cmd/meept/` subcommand (Phase 3):
```bash
meept config migrate-reasoning
```
Walks `config/agents/*/AGENT.md` and suggests `reasoning:` blocks based on agent role heuristics (planner → high, debugger → high, committer → low, etc.). User confirms each.

## Section 11: Testing

### 11.1 Unit tests

| File | Coverage |
|---|---|
| `internal/llm/reasoning_test.go` | `ReasoningConfig.IsZero`, `ResolveEnabled`, `Validate`, `effortOrder`, clamp logic |
| `internal/llm/reasoning_translate_test.go` | Table-driven: every vendor × every tier → expected wire fields |
| `internal/llm/budget_resolver_test.go` | Budget precedence chain (per-request > agent > model > config > default) |
| `internal/agent/reasoning_parser_test.go` | All NL forms, ambiguous cases, token hints, edge cases |
| `internal/agent/loop_reasoning_test.go` | Self-modulation clamping, precedence in loop wiring |
| `internal/llm/anthropic_reasoning_test.go` | Anthropic-specific: thinking block construction, budget translation, response parsing |
| `internal/llm/client_reasoning_test.go` | OpenAI client: `reasoning_effort` field, DeepSeek `reasoning_content` parsing |
| `internal/llm/broker_routing_test.go` | Bedrock/OpenRouter Claude detection |

### 11.2 Integration tests

| File | Scenario |
|---|---|
| `tests/integration/reasoning_e2e_test.go` | Full loop: dispatcher parses NL → attaches override → agent loop calls mock LLM → assert wire format on request body |
| `tests/integration/reasoning_self_modulation_test.go` | Agent starts at `high`, downshifts to `low`, asserts subsequent requests carry new tier |
| `tests/integration/reasoning_precedence_test.go` | Model default vs agent config vs NL override — verify winner |

### 11.3 Manual verification checklist

- [ ] `meept chat --reasoning high "..."` produces a request with `reasoning_effort: "high"` on OpenAI provider
- [ ] Anthropic request includes `thinking.budget_tokens` matching the tier
- [ ] Bedrock Claude request reaches `AnthropicClient`
- [ ] `[/reasoning xhigh]` in TUI updates subsequent turns
- [ ] `ctl-x r` cycles through tiers visibly
- [ ] Flutter reasoning dropdown persists per-session
- [ ] HTTP `PUT /api/v1/reasoning/agents/coder` with `{effort:"high"}` updates AGENT.md
- [ ] DeepSeek response populates `Response.Reasoning`
- [ ] Agent with `min_effort: medium` rejects `SetReasoningForNextTurn("low")` (clamps)

## Section 12: Phased Implementation

### Phase 1 — Core Abstraction + OpenAI + Anthropic

**Scope: complete wired feature for the two primary drivers.**

- `internal/llm/reasoning.go` — `ReasoningConfig`, `AgentReasoningConfig`, constants, `IsZero`, `ResolveEnabled`, `Validate`, `ResolveReasoning`, `ClampEffort`
- `internal/llm/reasoning_translate.go` — OpenAI (`reasoning_effort`) + Anthropic (`thinking` block) translators
- Wire into `ModelConfig.DefaultReasoning`, `AgentConstraints.Reasoning`, `ChatRequest.Reasoning`, `ChatOption.WithReasoning`
- Anthropic client: populate `thinking.budget_tokens` from mapping; parse thinking response blocks into `Response.Reasoning`
- OpenAI client: send `reasoning_effort`; parse `reasoning_content` response field
- Broker routing fix for OpenRouter Claude (defer Bedrock if non-trivial)
- Config schema: `config/meept.json5` `reasoning.budgets`, `models.json5` `default_reasoning`, agent frontmatter `constraints.reasoning`
- ConfigUI: per-agent section + per-model section
- CLI: `--reasoning` / `--reasoning-effort` / `--reasoning-budget` flags
- RPC: `reasoning.*` methods
- HTTP: `/api/v1/reasoning/*` endpoints
- TUI: `ctl-x r`, status indicator, `t` panel toggle
- Tests: full unit + integration for OpenAI + Anthropic paths
- Docs: `make docs-generate`, update `docs/configuration/llm.md`, `docs/concepts/multi-agent.md`
- Capability updates for `models_catalog.go` + `models.json5` (Anthropic, OpenAI models)

**Exit criteria:** user can run `meept chat --reasoning high "..."` against Claude or GPT-5 and see the wire field sent, reasoning parsed, tier shown in TUI.

### Phase 2 — Other Vendors

**Scope: extend translation to Qwen, GLM, Kimi, Gemini, Grok, DeepSeek; Bedrock routing fix.**

- Extend `applyOpenAICompatReasoning` switch with cases for each vendor in §2 table
- DeepSeek response parsing for `reasoning_content`
- Bedrock Claude routing fix in `isAnthropicRoute` + `AnthropicClient` URL construction branch
- Update capability tags + `default_reasoning` for: GLM, Qwen-thinking, DeepSeek, Gemini, Grok
- Per-vendor translation tests (table-driven)
- Manual vendor test plan (requires API keys)

**Exit criteria:** every supported vendor translates the tier to native wire format; DeepSeek responses populate `Response.Reasoning`.

### Phase 3 — NL Parser + Self-Modulation + Flutter + Polish

**Scope: natural-language control, agent autonomous modulation, full UI parity, docs polish.**

- `internal/agent/reasoning_parser.go` — NL parsing (§7)
- Dispatcher integration: parse directive → attach to `DispatchResult.ReasoningOverride`
- Agent loop `SetReasoningForNextTurn` + `currentEffort` state
- Dispatcher intent classifier hook: suggest tier for known intents
- Flutter: tier dropdown, reasoning trace widget, agent/model settings pages
- Menubar: reasoning section in Settings
- TUI: `ctl-x r` cycle behavior refined, thinking panel content streaming
- `meept config migrate-reasoning` subcommand
- HTTP session-override endpoints
- End-to-end integration tests: NL parse → override → wire format
- Documentation: `docs/workflows/llm-management.md` update, `docs/configuration/llm.md` reasoning section, README feature mention

**Exit criteria:** user can type "use high reasoning" in any client (CLI/TUI/Flutter) and have it take effect; agent loops self-modulate within bounds; every UI surface shows and controls the tier.

## Open Questions for Implementation

These are resolved at implementation time, not blockers for the spec:

1. **Bedrock URL construction** — if Bedrock's Anthropic endpoint diverges enough to need a separate client, defer to Phase 2 and ship OpenRouter fix only in Phase 1.
2. **OpenRouter dual-send format** — may need empirical testing against the live API. If `reasoning.effort` + `thinking` conflict, prefer the upstream-provider-appropriate one.
3. **xAI tier clamping** — xAI supports `low`/`high` only; decide whether to error or silently clamp. Spec says clamp with a debug log.
4. **Reasoning trace persistence** — whether `Response.Reasoning` is stored in conversation history (consumes context) or treated as ephemeral. Default: ephemeral (not in history), surfaced only at UI layer.

## File Impact Summary

| File | Change |
|---|---|
| `internal/llm/reasoning.go` | NEW — core types |
| `internal/llm/reasoning_translate.go` | NEW — vendor translators |
| `internal/llm/reasoning_test.go` | NEW |
| `internal/llm/reasoning_translate_test.go` | NEW |
| `internal/llm/models.go` | Add `DefaultReasoning` to `ModelConfig`, `Reasoning` to `ChatRequest`, `Reasoning` to `Response`, `WithReasoning` option |
| `internal/llm/client.go` | Call `applyOpenAICompatReasoning`; parse `reasoning_content` |
| `internal/llm/anthropic.go` | Call `applyAnthropicReasoning`; parse thinking response blocks |
| `internal/llm/broker.go` | `isAnthropicRoute` extension |
| `internal/agent/spec.go` | `AgentConstraints.Reasoning` field |
| `internal/agent/loop.go` | Self-modulation state, per-call wiring |
| `internal/agent/reasoning_parser.go` | NEW — NL parser |
| `internal/agent/reasoning_parser_test.go` | NEW |
| `internal/agent/dispatcher.go` | NL parse integration, `DispatchResult.ReasoningOverride` |
| `internal/config/schema.go` | `ReasoningGlobalConfig`, `ModelConfig.DefaultReasoning` schema |
| `internal/configui/sections_agents.go` | Reasoning subsection |
| `internal/configui/sections_models_reasoning.go` | NEW — per-model section |
| `internal/rpc/handlers.go` | `reasoning.*` methods |
| `internal/comm/http/api_handlers.go` | `/api/v1/reasoning/*` endpoints |
| `cmd/meept/chat.go` (or equivalent) | `--reasoning*` flags |
| `internal/tui/` | `ctl-x r`, status indicator, thinking panel |
| `ui/flutter_ui/lib/` | Tier dropdown, trace widget, settings |
| `menubar/` | Settings reasoning section |
| `config/meept.json5` | `reasoning.budgets` block |
| `config/models.json5` | `default_reasoning` entries |
| `config/agents/*/AGENT.md` | `constraints.reasoning` blocks |
| `internal/llm/models_catalog.go` | Capability + default updates |
| `docs/configuration/llm.md` | Reasoning documentation |
| `docs/workflows/llm-management.md` | Update with reasoning features |
| `docs/reference/generated/llm.md` | `make docs-generate` |
