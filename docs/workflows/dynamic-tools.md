# Dynamic Tool Exposure (as-needed tool definitions)

`tool_exposure` is a single knob that trades tool-definition tokens for a
discover-on-demand round-trip. Instead of shipping the full schema of every
tool on every turn (GAP-3: ~75 stub definitions even in indexed mode), the
daemon ships FULL schemas only for a predicted, intent-selected subset of
N tools plus the always-full core; the rest are NOT listed. When the model
needs a tool it does not see, it discovers it via `tool_search` and fetches
its schema with `tool_view`, which also activates the tool for the episode.

The design contract lives in `docs/research/dynamic-tools-design.md`. This
page is the user-facing how-to.

## The knob

All modes are one mechanism, one config value. They differ only in how many
tools get full schemas up front:

| Budget  | Mode value    | Behavior |
|---------|---------------|----------|
| all     | `full`        | legacy: every tool ships a full schema |
| all     | `indexed`     | legacy: every tool ships a one-line stub except the always-full core |
| N       | `predicted`   | DEFAULT when the flag is on: N intent-selected tools + always-full core get full schemas; the rest are unlisted; `tool_search`/`tool_view` recover misses |
| 0       | `search`      | opt-in for frontier-class models only: category digest + the meta pair; nothing else is listed up front |

Config lives under `[agent.tools]` in `config/meept.json5`
(`AgentToolsConfig`, `internal/config/schema.go`):

```json5
[agent.tools]
# Master switch for the as-needed tool-exposure mechanism. DEFAULT FALSE =
# byte-identical legacy (full/indexed only). See docs/agents/opt-in-defaults.md.
dynamic_tool_exposure = false
# full | indexed | predicted | search. With the flag off, predicted/search
# silently downgrade to indexed legacy (one warn). With the flag on, the
# default is "predicted". Resolution: per-model > per-agent frontmatter >
# this global value > "predicted".
tool_exposure = "predicted"
# N: how many intent-selected tools get full schemas in predicted mode.
# Default 25; valid 1..30 (values above 30 clamp to 30 with one warn - N=31
# costs MORE than the indexed baseline).
predicted_budget = 25
```

## Resolution order

The effective mode is resolved per turn in this order (first non-empty
wins), mirroring the existing `EffectiveSchemaMode` seam
(`internal/llm/schema_mode.go`):

1. **Per-model**: `tool_exposure` on the model entry in `models.json5`
   (`ModelDef.ToolExposure` -> `ModelConfig.ToolExposure`).
2. **Per-agent**: the `tool_exposure` frontmatter field on `AGENT.md`
   (`AgentSpec.ToolExposure`).
3. **Global**: `[agent.tools] tool_exposure`.
4. **Default**: `predicted` when the flag is on.

## Capability gate on `search` (hard rule)

`search` (the 0-budget mode) takes effect ONLY when the ACTIVE model is
frontier-class. If the configured mode is `search` but the active model is
NOT frontier-class, the daemon downgrades to `predicted` and logs ONE warn
line naming the model. Consequences that hold:

- A mid-session switch to a lesser model reverts the effective mode to
  `predicted` ON THE SWITCH - even when the agent config says `search` and
  even mid-episode (a model switch already invalidates the cache prefix, so
  this churn is free).
- Switching BACK to a frontier model restores `search` at the next EPISODE
  boundary, not mid-episode (avoids flapping).
- The frontier signal is the `capabilities` array carrying the `"frontier"`
  tag (`llm.CapFrontier`, `ModelConfig.HasCapability`).
- Unknown / unclassified models are treated as NON-frontier (fail-safe:
  default to `predicted`).

## Starvation recovery

In `predicted` and `search` modes, a tool the model does not see is NOT a
dead end:

- `tool_search` queries the FULL registry (scoped to the agent's role
  ceiling) by name + one-line description and returns top-k candidates. It
  is never limited to the active set.
- `tool_view` fetches the full schema of a named tool AND activates it - the
  name is appended to the episode's active list (append-only; `StabilizeToolPrefix`
  keeps the existing prefix byte-identical so the prompt cache survives).

Two guards keep this from starving the episode:

- **Known-but-inactive** (`internal/agent/executor.go` §6 row 4): if the
  model calls a tool that is registered but not in the active set, the
  executor returns an actionable error - `tool "X" is not active in this
  episode; call tool_view{"X"} first` - instead of the generic
  `unknown tool: X`.
- **Role-ceiling refinement** (§6 row 5): activation refuses a name outside
  the agent's role ceiling and names the ceiling.

`tool_search` and `tool_view` are always-full in dynamic modes: a stubbed
discoverer would be undiscoverable, so the meta pair is exempt from
`DefaultAlwaysFullTools` trimming.

## §5.3 frontier warning (verbatim, required in config template + this page + AGENT.md frontmatter doc)

> search mode requires a frontier-class model. Do NOT enable it for
> small/local models - they fail to use the search/view round-trip
> reliably (live-smoke evidence: always-full promotions
> 2026-09-07/09-12, `internal/config/schema.go:1959-1971`). The daemon
> downgrades search to predicted automatically for non-frontier models.

## When to use each mode

- **`full` / `indexed`** (flag off): the legacy behavior. Zero risk, no
  savings. Keep this if you do not want the discovery round-trip.
- **`predicted`** (flag on, default): the recommended setting. Saves
  ~1,110 tokens/turn at N=25 (branch-03 token audit) with a bounded
  discovery cost on tasks that need uncommon tools.
- **`search`**: only for frontier-class models that reliably drive the
  search/view round-trip. Do NOT enable it for small or local models.

## Files

- `internal/agent/tool_exposure.go` - the engine (`EffectiveExposureMode`,
  `NewExposurePlan`, `Promote`, `ExposurePlan`).
- `internal/agent/executor.go` - the §6 starvation/ceiling guards.
- `internal/agent/loop.go` - the episode-start arm + `ApplyRequestModel`
  warn-once downgrade.
- `internal/tools/builtin/tool_search.go` - the discoverer meta-tool.
- `internal/tools/builtin/tool_view.go` - the expander meta-tool (+
  `SetActivateHook` activation seam).
- `internal/agent/intent_tool_map.go` - the deterministic intent->toolset
  map (`config/tools_intent_map.json5`).
- `internal/config/schema.go` - `AgentToolsConfig` + validation.
