# LLM Provider Management

## Overview
Meept supports multiple LLM providers with intelligent model resolution, failover mechanisms, budget tracking, and native Anthropic driver support. The system optimizes for cost, capability matching, and reliability.

## Problem
Different tasks require different LLM capabilities and cost profiles. LLM management addresses:
- Multi-provider support with consistent interfaces
- Capability-based model selection
- Cost optimization and budget control
- Reliability through failover mechanisms

## Behavior

### Multi-Provider Support
- **OpenAI**: GPT models via API
- **Anthropic**: Claude models with native driver
- **Google**: Gemini models
- **Ollama**: Local models
- **Custom**: OpenAI-compatible endpoints

### Model Resolution
- **Skill Requirements**: Skills declare `requires: [code, reasoning]`
- **Model Capabilities**: Models declare `capabilities: [code, tool_use]`
- **Cost Optimization**: Cheapest capable model selected
- **Automatic Fallback**: Retryable errors trigger failover

### Model-Slot Fairness (Interactive Priority)

When a model's `max_concurrency` is set, requests wait for a free slot
before hitting the provider. Slots are handed out through a two-lane
gate (`internal/llm/slot_gate.go`, tree 04 leaf 03):

- **Interactive lane** — chat turns (the user is actively conversing)
  call `llm.WithPriority(true)`; when a slot frees, an interactive
  waiter is granted ahead of background waiters.
- **Background lane** — queue jobs, specialist agents, goal loops, and
  any caller that does not pass priority (the default) wait FIFO.
- **Starvation guard** — after 3 consecutive interactive grants, one
  background waiter is granted before further interactive ones, so
  constant chatting cannot starve background work indefinitely.
- **No wire change** — priority affects acquisition order only; nothing
  is added to the request payload. Callers that never pass priority
  behave exactly as before the gate existed.

Two priority layers exist in meept, and they read DIFFERENT signals
(cross-layer divergence, intentional scope — audit 2026-09-01):

1. **Queue ordering** (see [Job Scheduling](job-scheduling.md),
   "Claim Ordering"): jobs carry an `Interactive` flag stamped at
   ENQUEUE time from the ORIGINATING SESSION (recent user message or
   foreground flag, DECISIONS.md D11). It decides which job a worker
   claims next.
2. **Slot fairness** (this section): the gate reads the CALLING TURN —
   chat turns are interactive, queue work is background. It decides who
   gets a model-concurrency slot.

Consequence: an `Interactive=true` planner job that wins its claim
STILL acquires slots with priority=false (it is a queue turn, not a
chat turn). Interactive queue work can therefore wait behind background
chat turns at `max_concurrency`. This is intentional: queue jobs are
not slot-prioritized in this tree (D11's two-tier rule; no third
"priority" tier). Chat turns themselves never touch the queue (direct
dispatch) — they are prioritized only at the slot layer.

Note: `AnthropicClient` and `CodexClient` transports are not gated by
`max_concurrency` today (pre-existing scope, unchanged by this leaf);
slot fairness applies to the OpenAI-compatible client paths.

### Token Budgeting
- **Hourly/Daily Limits**: Configurable token ceilings
- **Rate Limiting**: Requests per minute control
- **Aggressiveness Setting**: Cost control granularity (0.0-1.0)
- **Usage Tracking**: Real-time budget monitoring
- **Cost Limits**: Dollar-based budget caps (requires model pricing)
- **Per-Task Caps**: Prevent single task from exhausting budget
- **Per-Session Caps**: Limit individual conversation consumption

See [Token Budgets Configuration](../configuration/token-budgets.md) for detailed setup.

### Native Anthropic Driver
- **Messages API**: Native implementation
- **Extended Thinking**: Mode support
- **Streaming**: Progress callbacks
- **SSE Parsing**: Real-time updates

## Configuration

```toml
[llm]
# Directory for locally pulled GGUF models (default shown)
models_dir = "~/.meept/models"

[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5

[llm.providers.anthropic]
base_url = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[llm.providers.openai]
base_url = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"

[llm.providers.ollama]
base_url = "http://localhost:11434"
api_key_env = ""
```

### Models Configuration (`config/models.json5`)
```json5
{
  providers: {
    anthropic: {
      base_url: "https://api.anthropic.com",
      api_key_env: "ANTHROPIC_API_KEY",
      models: {
        "claude-opus-4-5-20251101": {
          capabilities: ["code", "tool_use", "extended_thinking"],
          max_tokens: 8192,
        }
      }
    }
  }
}
```

## Context-Length Discovery

When `llm.context_discovery.enabled` is true, meept runs a background
syncer that asks each provider for its true model context lengths and
merges them into the in-memory model catalog (see
[LLM Configuration](../configuration/llm.md#context-discovery-configuration)
for the config block and precedence rule).

- **What runs:** one fetcher per provider that exposes context length —
  Ollama (`/api/show`), LM Studio (`/v1/models` for ids +
  `/api/v0/models` for `max_context_length`), llama.cpp `local-models`
  (`/props`), and OpenRouter (the same `/api/v1/models` fetch the pricing
  sync uses). OpenAI and Anthropic expose no context length and are never
  queried.
- **When:** immediately at daemon startup, then every re-sync tick
  (`interval`, default 6h).
- **What it changes:** in-memory context windows only — resolver model
  entries and the TUI model picker's display catalog. Deltas are logged
  as `context window updated` lines.
- **What it never changes:** your models.json5. Explicit
  `context_limit` values are always authoritative; nothing discovered is
  written back to any config file.
- **Where LM Studio models come from:** LM Studio ships no static
  catalog — discovered models are registered from its OpenAI-compatible
  `/v1/models` list as `lmstudio/<id>` entries, with context lengths from
  `/api/v0/models`. The model list is whatever that machine has loaded.

## Adaptive 429 Pacing

When `llm.failure_policy.pacing.enabled` is true, meept paces outbound
requests per provider below that provider's effective rate-limit ceiling.
The loop is observe → interval → decay: every provider response is
classified (see the failure-policy docs), and a bare throttle 429 — no
`Retry-After`, no quota signal — doubles the minimum gap between requests
to that provider, clamped at `max_interval`. Clean traffic decays the gap
by half per quiet window (one full gap's worth of successful traffic), so
pacing fades back out as the provider recovers. Independently, the metrics
store's hourly rate-limit count acts as a floor: while a provider exceeds
`target_429_per_hour` events in the last hour, the enforced gap never
drops below `min_interval`, even without fresh 429s. Pacing composes with
the retry policy — it stretches the gap between requests, it never blocks
or replaces a retry. See
[LLM Configuration](../configuration/llm.md#failure-policy-configuration)
for the knobs; the feature is off by default.

## Grammar-Constrained Tool Calling (GBNF)

Small local models frequently emit malformed tool-call JSON. When the
endpoint supports grammar constraints, meept can attach a grammar that makes
invalid tool-call output impossible.

### Endpoint support matrix

| Endpoint type | Constraint key | `tool_constraint` value | Notes |
|---|---|---|---|
| llama.cpp server (managed or remote) | `grammar` (GBNF) | `"llamacpp"` | Full enum-tight grammar; managed runtimes auto-declare this |
| vLLM | `guided_grammar` (GBNF) | `"vllm"` | Same GBNF grammar, different wire key |
| OpenAI-compat structured outputs | `response_format: {type:"json_schema"}` | `"json_schema"` | JSON Schema instead of GBNF; enum tightness may be lower depending on server |
| MLX-server, Ollama, most remote APIs | none | `""` (default) | No grammar attached — not an error |

### Configuration

```toml
# config.toml — global kill-switch (default false)
[agent.tools]
gbnf_constrained = true
```

```json5
// models.json5 — per-provider and per-model constraint declaration
{
  "providers": {
    "local": {
      "options": { "baseURL": "http://127.0.0.1:8080", "tool_constraint": "llamacpp" },
      "models": {
        "my-model": { "name": "...", "tool_constraint": "json_schema" } // optional override
      }
    }
  }
}
```

Behavior:
- Attach requires ALL of: `gbnf_constrained = true`, a declared matching
  `tool_constraint`, tools present on the request.
- Unsupported schema constructs exclude that tool from the grammar (never
  brick generation); the exclusion is logged once per session.
- Managed llama.cpp runtimes (`llama-cpp`) auto-declare `llamacpp`; MLX
  runtimes declare nothing.

## Universal Parking on Provider Waits (D9)

Any meept turn can hit a provider wall — a quota window (hours until reset)
or a throttle signal (seconds-to-minutes of load shedding). Per DECISIONS.md
D9, meept is an electric machine: on (working) or off (broken). **No turn
hangs on a dead or waiting connection** — every turn type parks instead,
releasing its agent/model slot, and resumes automatically when the provider
recovers.

### What parks

| Turn type | Park site | Mechanism |
|-----------|-----------|-----------|
| Chat turns (quota) | `ChatHandler` | `QuotaResumeWatcher` over the shared parker (`internal/agent/quota_resume.go`) |
| Chat turns (throttle) | `AgentLoop` | `parkThrottledTurn` (`internal/agent/loop_park.go`) |
| Goal-loop episodes | `EpisodeParker` | `maybeParkProviderWait` (`internal/employee/park.go`) |
| Queue/specialist jobs | loop park branch | same `AgentLoop` throttle path |

All of these feed ONE shared `agent.TurnParker`
(`internal/agent/parked_turn.go`) per daemon: it holds
`ParkedTurnRecord`s of any failure class, resumes them oldest-first at
their scheduled time, and answers the surfaces' single query
`TurnParker.WaitInfo() []ParkWaitInfo` — one `{Class, Next, Pending}` row
per class with parked work. The parker is memory-only: a daemon restart
drops parked records (logged), and quota blocks re-probe providers anyway.

### Failure classes and schedules

Two provider-wait classes exist (SHARED-CONVENTIONS §4.1, D4):

- **`quota`** — a 429/402 with a quota-shaped signal. Parks until the
  server-provided reset time (honored when later than the plan's first
  step). The 24h `llm.quota_retry.max_wait` caps any single wait: waits
  beyond it are refused and escalate (see quota escalation ladder in
  [Quota Resilience](quota-resilience.md)).
- **`throttle`** — 429/503 load shedding. Parks on the composed backoff
  plan (`llm.DefaultBackoffPlan`): exponential steps from a 30s base, the
  server `Retry-After` winning when later. Each re-park generation grows
  the attempt count, so the schedule keeps stepping outward. A wait that
  would exceed MaxWait **gives up**: the D8 `ThrottleGiveUpError` surfaces
  to the user instead of an invisible multi-hour park.

### State semantics: StateQuotaWait is reused, reason distinguishes

Both classes drive the agent into the SAME `quota_wait` state
(`agent.StateQuotaWait`). The leaf-04 decision: **keep one state + a
`reason` payload, add no `StateThrottleWait`**. Rationale: the machine
truth ("parked on a provider wait") is identical for both classes; every
existing surface, transition-table entry, and WS classification keeps
working unchanged; the reason string on the event and the wait label on
the UI carry the distinction. The transition table needed zero new
entries.

### Bus events on the existing quota topic

Park/resume/give-up events ride the EXISTING `agent.quota_wait` topic
(published by the shared parker's `SetParkEventBus` wiring; payload type
`agent.ParkTurnEvent`):

| Lifecycle | Payload keys |
|-----------|--------------|
| park | `agent_id`, `to: "quota_wait"`, `reason` (follows the class: `"quota_wait"` \| `"throttle_wait"`), `class` (`"quota"\|"throttle"`), `resume_at` (RFC3339), `provider_id`, `session_id`, `model_id` (throttle parks; chat quota parks are provider-scoped and omit it) |
| resume | `agent_id`, `to: "running"`, `reason: "throttle_resumed"`, `class: "throttle"`, `waited` (Go duration string), `session_id` (quota resume visibility is the episode tracker's existing `quota_cleared` event) |
| give-up | `agent_id`, `reason: "throttle_give_up"`, `class: "throttle"`, `waited`, `model_id`, `provider_id` |

No new topic was introduced, so the WS handler's `agent.quota` prefix
match classifies every park event as `agent_progress` — never
`chat_message` (pinned by
`TestWSBridgeClassifiesParkTurnEventsAsProgress`). Legacy
`QuotaEvent`-shape messages on the same topic remain byte-identical.

### Wait labels (TUI + GUI parity)

The agents tab renders the parked state with an absolute-time label —
identical strings on both surfaces (lowercase per UI rule):

- quota wait: `quota_wait · reset HH:MM`
- throttle wait: `quota_wait · throttle retry HH:MM`

`HH:MM` is the daemon-provided resume instant rendered as absolute local
time (`QuotaWaitLabel` in `internal/tui/quota_status.go`, mirrored by
`quotaWaitLabel` in `ui/flutter_ui/lib/features/agents/quota_status.dart`).
Both surfaces deliberately avoid relative countdowns here: the GUI runs on
web where client wall clocks cannot be trusted.

### Give-up semantics

Give-up is not a crash — it is the machine reporting a wait too long to
hide. A throttle wait beyond MaxWait surfaces `ThrottleGiveUpError` (D8)
as the turn's failure; the quota equivalent is the 24h escalation to
`blocked`. Both are visible on the same surfaces, and both leave the
agent slot free.

## Observability

### Logging
- Model selection decisions
- Provider API calls
- Budget usage events
- Failover triggers

### Metrics
- Model utilization rates
- API response times
- Token consumption rates
- Budget utilization percentages

### Debug Info
- Available models and capabilities
- Current budget status
- Provider health status
- Model alias mappings


## Budget Management Workflow

### Check Current Budget Status

```bash
# View budget status via CLI
meept status

# JSON output for programmatic access
meept status --json
```

### Adjust Budget Limits Dynamically

Budget limits are **dynamic** - changes take effect immediately without daemon restart:

1. Edit `~/.meept/meept.json5`
2. Modify `llm.budget` section
3. Changes apply to next LLM call (no restart needed)

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 50000,   // Reduce for testing
      "daily_cost_limit": 5.0,        // Strict daily cap
      "aggressiveness": 0.3,          // More conservative
    }
  }
}
```

### Per-Task Budget Isolation

When running many concurrent tasks, set `per_task_token_limit` to prevent one task from consuming the entire budget:

```json5
{
  "per_task_token_limit": 25000,  // Each task limited to 25k tokens
}
```

Benefits:
- Prevents runaway tasks from starving others
- Forces task decomposition for large jobs
- Predictable per-task cost bounds

### Per-Session Budget Caps

For multi-user deployments, limit individual session consumption:

```json5
{
  "per_session_token_limit": 50000,  // Each conversation session capped
}
```

### Cost Tracking Setup

1. Ensure models have pricing in `~/.meept/models.json5`:
```json5
{
  "models": [{
    "id": "claude-sonnet-4-5-20241022",
    "provider": "anthropic",
    "input_cost": 0.000003,
    "output_cost": 0.000015
  }]
}
```

2. Enable cost limits:
```json5
{
  "daily_cost_limit": 10.0,   // $10/day max
  "hourly_cost_limit": 2.0,   // $2/hour max
}
```

### Rate Management

Set `rate_limit_rpm` to pace API calls and avoid provider rate limits:

```json5
{
  "rate_limit_rpm": 30,  // Max 30 requests/minute
}
```

When exceeded, requests **block and wait** (not rejected) until capacity is available.

### Tuning Aggressiveness

The `aggressiveness` factor applies a safety multiplier to all limits:

```
effective_limit = base_limit * (0.5 + 0.5 * aggressiveness)
```

| Use Case | Aggressiveness | Effective Limit |
|----------|----------------|-----------------|
| Production safety | 0.0-0.3 | 50-65% of base |
| Default (balanced) | 0.5 | 75% of base |
| Development | 0.7-1.0 | 85-100% of base |

### Monitoring and Alerts

Watch for budget warnings in daemon logs:
```
WARN budget hourly limit approaching (85% used)
ERROR budget daily cost exceeded: $10.00 / $10.00
```

Use `meept status` periodically or integrate with monitoring via the JSON output.

## Local Model Lifecycle

`meept model` manages local GGUF model *files* (runtimes themselves are managed by `meept runtime`):

```sh
# Pull a GGUF from Hugging Face (resumable; picks the single matching quant)
meept model pull bartowski/Llama-3.2-1B-Instruct-GGUF --quant Q4_K_M
meept model pull <repo> --force        # re-download

# List pulled models
meept model list
meept model list --json

# One-token completion probe through the local runtime (starts it if needed)
meept model test <name>
```

Behavior:

- Downloads use plain HTTPS against `https://huggingface.co/api/models/<id>` and
  `.../resolve/main/<file>`. Interrupted downloads resume via a `.part` file and
  Range requests; servers without Range support restart cleanly.
- `sha256` is computed streaming during the pull; a stored file whose size or
  hash disagrees with the manifest is re-downloaded.
- If multiple `.gguf` files match, the command fails listing candidates — pass
  `--quant` to disambiguate.
- `HF_TOKEN`, when set in the environment, is sent as a bearer token (never logged).
- Pulled models register under the `local-models` provider alias on a shared
  llama.cpp-style endpoint (`127.0.0.1:8080`), making them selectable through
  normal provider/alias resolution.
- Storage location defaults to `models_dir = "~/.meept/models"` under `[llm]`.

## Edge Cases

### Provider Outage
- Automatic failover to alternative providers
- Graceful degradation of capabilities
- Health monitoring for recovery

### Budget Exceeded
- Requests blocked with `BudgetExceededError` (non-retryable)
- User notified with specific limit exceeded (hourly/daily/task/session)
- CLI and API return descriptive error messages
- Alternative: lower aggressiveness, split tasks, or increase limits

### Capability Mismatch
- No model satisfies requirements
- Fallback to closest available capability
- User notified of limitation

### Model Alias Resolution
- Alias cooldown periods enforced
- Failover rotation maintains availability
- Usage patterns optimized over time