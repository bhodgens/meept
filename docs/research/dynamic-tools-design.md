# As-Needed Tool Definitions — Design (Branch 03, Leaf 00D)

> **Status:** DESIGN DOC ONLY. Turns the user-ratified shape (branch-03
> orchestrator "Ratified Shape", 2026-10-31) into pinned, implementable
> contracts. No code in this doc. Every `file:line` anchor was grep-verified
> 2026-11-01 (see Appendix A); where an orchestrator-leaf anchor had drifted,
> the corrected location is cited and noted in Appendix B.

**Inputs:**
- Ratified shape: `docs/plans/token-efficiency/03-dynamic-tools/orchestrator.md`
  ("Ratified Shape", "Capability gate on `search`", "Cache-stability mechanics").
- Q1/Q2 answers: `docs/plans/token-efficiency/master.md` Open Questions.
- 00A numbers: `docs/research/token-efficiency-2026-10/meept-token-audit.md`
  (Table 2: indexed 7,800 est-tok vs full 13,421; Table 4: B saves 5,685,
  C(N=10) saves 4,222, D(N=25) saves 1,110; D break-even N=30→31).

---

## 1. Ratified Shape (pinned; do not re-litigate)

Single knob `tool_exposure`, one mechanism, differing only in the up-front
full-schema budget:

| Budget | Mode | Behavior |
|---|---|---|
| all | `full` / `indexed` | today's behavior (loop-economics leaf 02; byte-identical legacy) |
| N (predicted) | `predicted` (DEFAULT, shape D) | intent-map-selected N tools + always-full core get FULL schemas; rest unlisted; `tool_search`/`tool_view` recover misses |
| 0 | `search` (shape B) | core + meta-tools only; no predicted tools; opt-in for frontier-class models |

**Prediction origin (three layers, no new LLM call):**
1. **Ceiling (security):** the agent's role list — AGENT.md
   `additional_tools` → `FilteredToolRegistry`
   (`internal/agent/executor.go:2044-2101`). The predicted subset, the
   `tool_search` result domain, and every promotion NEVER exceed it.
2. **Selection (episode start):** deterministic intent→toolset map (config
   file, auditable — §3.3), keyed by the intent category label the EXISTING
   intent analyzer already produces: `AnalyzeTrueIntent`
   (`internal/agent/intent_analyzer.go:257`), `TrueIntentAnalysis.Category`
   (`internal/agent/intent_analyzer.go:21`), valid labels pinned at
   `internal/agent/intent_analyzer.go:372-377`
   (`research|implementation|investigation|fix|clarification|other|quickplan`,
   plus `_default`). Not the planner/orchestrator LLM. A classifier tool-head
   is the future upgrade behind the same seam (§3.1 `Predict`), used only if
   the map proves coarse.
3. **Promotion (mid-episode, model-driven):** `tool_search` finds a missing
   tool in the FULL registry (the role ceiling, never the active set);
   `tool_view` fetches the schema AND activates the tool — append to the
   active list (§4). `tool_search` + `tool_view` are ALWAYS-full: a stubbed
   discoverer is undiscoverable — already true for `tool_view`
   (`internal/config/schema.go:1956-1973` `DefaultAlwaysFullTools`,
   `internal/tools/registry.go:234` force-inclusion); `tool_search` joins the
   list in this branch.

**Cache-stability mechanics (binds all leaves):** prediction computed ONCE at
episode start; the active list is frozen for the episode except APPEND-ONLY
promotions; shrinking only at the next episode boundary (§4,
`StabilizeToolPrefix` at `internal/agent/conversation.go:1201-1264`).

**Config resolution order** (mirrors the existing `EffectiveSchemaMode` seam,
`internal/llm/schema_mode.go:36`, called from
`internal/agent/loop.go:1435`):
per-model > per-agent (AGENT.md frontmatter `tool_exposure`) > global
`[agent.tools] tool_exposure` > default `predicted`.

**Capability gate on `search` (HARD RULE):** `search` takes effect ONLY when
the ACTIVE model is frontier-class; non-frontier active model silently
downgrades to `predicted` with one warn log naming the model (§5).

**Feature-flag default OFF (two defaults, do not conflate):**
- `[agent.tools] dynamic_tool_exposure = false` (default) — the whole
  mechanism is off; behavior is byte-identical legacy (`full`/`indexed`
  only), per `docs/agents/opt-in-defaults.md`.
- When the flag is ON, the mode default is `predicted` (D).

---

## 2. Data Flow

### 2.1 Who predicts

The **dispatcher** already runs `AnalyzeTrueIntent` per inbound turn
(`internal/agent/dispatcher.go:1240` and `:2304`) and stores the analysis in
`memCtx.LastIntent` (`Type: analysis.Category`, dispatcher.go:1254-1256).
Prediction reuses THAT label — no new LLM call, no new analyzer instance:

```
episode start (new Conversation instance)
  → intent label = dispatcher's LastIntent.Category for the episode's
    triggering turn (fallthrough: "other" when the triggering turn had no
    analysis — e.g. goal-loop episode parked/resumed; never a fresh LLM call)
  → predictedSet = IntentMap.For(label, budget=N, ceiling=agent.AllTools())
  → activeList   = core ∪ predictedSet ∪ {tool_search, tool_view}
```

`core` = `config.DefaultAlwaysFullTools()` ∩ ceiling
(`internal/config/schema.go:1956`). `IntentMap.For` intersects the map entry
with the ceiling and caps at N (default 25, §7); a map miss (`_default`
missing or empty entry) yields `core + meta-tools` only — never an error,
never a fallback LLM call.

### 2.2 When the list swaps

| Event | List change | Cache effect |
|---|---|---|
| **Episode start** (new Conversation; new goal-loop episode) | recompute: new predicted set; promotions from the previous episode are **dropped** (the only shrink point) | one prefix re-cache — a new episode is a new conversation, nothing is lost |
| **Mid-episode turn** | none (list frozen) | byte-identical tool payload → prefix survives (§4) |
| **Promotion** (model calls `tool_view{name}` for an unlisted tool) | APPEND `name` to activeList only | one bounded re-cache event, splice position in the sorted payload (§4.3); no per-turn churn |
| **Mid-session model switch → lesser model** (configured `search` active) | reverts effective mode to D **on the switch**: append the predicted set for the current episode intent (growth only — the B list carries no predicted tools to drop) | free: the switch already invalidates the prefix; one warn log naming the model |
| **Model switch → frontier again** (configured `search`) | **pending** until the next episode boundary; D's list (with its appended predicted set) stays until then — no mid-episode shrink, no flapping | restored `search` takes effect at episode start |

### 2.3 How misses recover (starvation path)

1. Model wants a tool not in the active list → it is *unlisted*, so the model
   uses `tool_search{query}` (always full schema, always active) → returns
   name + one-line description from the FULL registry within the ceiling.
2. Model calls `tool_view{name}` → full schema returned AND `name` appended
   to the active list → the tool is callable from the next turn of the
   episode.
3. `tool_search` finds nothing (name not in ceiling, or query too narrow) →
   structured "no match" result naming the category digest the model can
   retry against; the actionable error wording tells the model to retry with
   a broader query. If the tool is genuinely outside the agent's ceiling,
   activation is refused with a reason naming the ceiling (§6).
4. Whole-episode starvation (the task needs a class of tool the predicted
   set never got): the meta-tool pair is always present, so recovery is a
   bounded search→view round-trip, never a dead end. The episode-boundary
   recompute (new intent label) is the second, coarser recovery.

---

## 3. Contracts (Go signatures)

All new code lands behind `dynamic_tool_exposure`; when the flag is off, every
seam below is a no-op returning legacy behavior.

### 3.1 Exposure engine (`internal/agent/tool_exposure.go`, NEW)

```go
// ToolExposureMode is the single-knob value; "predicted"/"search" are valid
// only while dynamic_tool_exposure is on (config load: off + D/B value ->
// "indexed" legacy default + one warn, byte-identical payload).
type ToolExposureMode string

const (
    ExposureFull      ToolExposureMode = "full"
    ExposureIndexed   ToolExposureMode = "indexed"
    ExposurePredicted ToolExposureMode = "predicted"
    ExposureSearch    ToolExposureMode = "search"
)

// IsFrontierClass is the capability gate (§5): true only when the model's
// models.json5 capabilities carry the "frontier" tag. Nil config /
// unclassified model = false (fail-safe).
func IsFrontierClass(mc *llm.ModelConfig) bool

// EffectiveExposureMode mirrors the EffectiveSchemaMode seam
// (internal/llm/schema_mode.go:36; call site internal/agent/loop.go:1435):
// per-model > per-agent frontmatter > global > default "predicted" (flag on),
// then applies the capability gate (search + non-frontier -> "predicted" +
// warn-once naming the model). Pure function of inputs; thread-safe.
func EffectiveExposureMode(mc *llm.ModelConfig, agentMode string,
    globalMode string, flagOn bool) ToolExposureMode

// ExposurePlan is the episode-frozen active set. ActiveSet is
// PredictedSet ∪ {core, tool_search, tool_view}; Promote is append-only.
type ExposurePlan struct {
    Mode         ToolExposureMode
    ActiveSet    []string
    ceilingNames []string // role ceiling; Promote/For always scoped to it
}

func NewExposurePlan(mode ToolExposureMode, mc *llm.ModelConfig,
    spec *AgentSpec, m *IntentMap, budget int) *ExposurePlan
func (p *ExposurePlan) Promote(name string) error // error: unknown / out-of-ceiling / search-mode-shrinking-pending (never shrinks)
```

**Future upgrade seam (documented, not built):** `Predict` currently delegates
to `IntentMap.For(label, budget, ceiling)`; a classifier tool-head replaces
only that body, same signature.

### 3.2 Registry seams (existing, extended — no new interface)

- Active-list gating reuses `NewFilteredToolRegistry`
  (`internal/agent/executor.go:2052`): dynamic mode wraps the role registry
  with the ExposurePlan's ActiveSet; the parent `tools.Registry` gets
  `SetSchemaMode(SchemaModeIndexed, activeList)`
  (`internal/tools/registry.go:224`) so active tools ship full schemas and
  everything unlisted is simply *absent* from `GetDefinitions`
  (`internal/agent/executor.go:2088-2101`).
- `tool_search` queries the **parent** registry (full, ceiling-scoped), never
  the filtered active view (§2.3).
- `tool_view` activation: one setter on the tool
  (`internal/tools/builtin/tool_view.go:40` `NewToolViewTool`):

```go
// SetActivateHook wires episode promotion: called after a successful
// tool_view result for a tool name. Nil = legacy (no activation). The hook
// is invoked with the tool name only; ceiling/append semantics live in
// ExposurePlan.Promote.
func (t *ToolViewTool) SetActivateHook(func(name string))
```

- Registration: `internal/daemon/components.go:7177` (next to
  `NewToolViewTool` registration) gains the `tool_search` equivalent.

### 3.3 Intent map (`internal/agent/intent_tool_map.go`, NEW)

Config file, auditable — `config/tools_intent_map.json5` (template copied by
`make config-bootstrap`):

```json5
// One entry per valid intent category (internal/agent/intent_analyzer.go:372);
// unknown/missing categories fall through to "_default". Names are
// intersected with the agent's role ceiling; over-budget entries keep the
// first N (N = [agent.tools] predicted_budget, default 25).
{
  "_default": ["memory_search"],
  "research":       ["web_search", "web_fetch", "transcript_fetch", "json_extract", "memory_search", "skills_load"],
  "implementation": ["shell", "file_read", "file_write", "file_edit", "git_overview", "build", "test"],
  "investigation":  ["shell", "file_read", "memory_search", "grep_files"],
  "fix":            ["shell", "file_read", "file_edit", "grep_files", "git_overview"],
  "clarification":  ["memory_search"],
  "other":          ["memory_search"]
  // quickplan: inherits "_default" (planning turns need no extra tools)
}
```

```go
// IntentMap is the deterministic intent->toolset selection table. Loaded
// once at daemon start (invalid names dropped with one warn; a malformed
// file falls back to "_default"-only, never to an LLM call).
type IntentMap struct{ /* entries, ceiling-safe lookup */ }

func LoadIntentMap(path string, log *slog.Logger) (*IntentMap, error)
// For returns entry[label] (or entry["_default"]) ∩ allowed, truncated to
// budget (0 in search mode -> nil). Deterministic: same inputs, same slice.
func (m *IntentMap) For(label string, budget int, allowed []string) []string
```

### 3.4 Config / schema surface

```go
// internal/config/schema.go AgentToolsConfig (line 1924) gains:
//   DynamicToolExposure bool   `json:"dynamic_tool_exposure" toml:"dynamic_tool_exposure"` // default false
//   ToolExposure        string `json:"tool_exposure" toml:"tool_exposure"`                 // "" | full | indexed | predicted | search
//   PredictedBudget     int    `json:"predicted_budget" toml:"predicted_budget"`            // default 25, valid 1..30
// Validate() extension: off-flag + "predicted"/"search" value -> warn + "indexed";
// on-flag: any of the four; other values -> load error naming agent.tools.tool_exposure
// (same fail-fast style as the existing SchemaMode validation, schema.go:1944-1951).
// PredictedBudget > 30 -> clamp to 30 + one warn (00A: N=31 costs MORE than
// the indexed baseline, so >30 can never be the better choice).

// internal/llm/models.go ModelConfig (line 221) gains, next to SchemaMode (246):
//   ToolExposure string // per-model tier; "" = inherit
// internal/llm/providers.go ModelDef (line 74) gains:
//   ToolExposure string `json:"tool_exposure,omitempty"`
// internal/llm/provider_registry.go gains, next to CapToolUse (line 35):
//   CapFrontier = "frontier"
```

---

## 4. Cache Stability

### 4.1 Why the prefix survives (steady state)

The loop fetches definitions once per iteration — `l.registry.GetDefinitions()`
(`internal/agent/loop.go:4454`) — and stabilizes them through
`conv.StabilizeToolPrefix(tools)` (`internal/agent/loop.go:4459`;
implementation `internal/agent/conversation.go:1201-1264`). That function
serializes deterministically: tools sorted alphabetically by name, per-tool
`name + description + parameters sorted by key`, SHA-256 into
`cachePrefixHash`, change detected by `PrefixChanged`
(`internal/agent/conversation.go:1269`). The cache-boundary sentinel sits
behind the stable system-prompt sections (`internal/agent/prompt.go:322`).

Consequence: with the episode-frozen active list, **every turn serializes to
the same byte sequence** → same SHA-256 → `PrefixChanged()` false → the
provider's prefix cache (tools+system+conversation head) is never invalidated
mid-episode. This is the property that makes D/B cheap: the tool payload
changes at most once per episode, not once per turn (the today hazard: any
per-turn list churn invalidates the KV-cache prefix).

### 4.2 What changes at an episode boundary

New conversation → new `Conversation` → new `cachePrefixHash` seed. The
predicted set may differ; the drop of stale promotions happens HERE and only
here. One re-cache event per episode is the designed steady cost.

### 4.3 Promotion: bounded one-time churn (pinned precisely)

`StabilizeToolPrefix` re-sorts the whole list each call
(conversation.go:1205-1207). A promoted tool therefore splices into its
alphabetical position, not at the payload tail. Pinned consequence:
- Per-tool serialized bytes are unchanged (canonical serialization is
  stable under growth — the ratified "append-only" invariant is about the
  SET, never a removal), so no existing tool's bytes move or change.
- One promotion produces exactly ONE prefix change: bytes from the splice
  point to the end of the tool block re-cache on the next turn;
  `PrefixChanged()` fires once ("prefix cache invalidated" debug line,
  loop.go:4461) and is quiet again until the next distinct promotion.
- This is the same cost class as the already-accepted model-switch
  invalidation (orchestrator "Cache-stability mechanics"), and promotions
  are rare (they are misses). No further churn for the rest of the episode.

Rule that binds all leaves: **the active set is append-only mid-episode;
no list change is legal between episode boundaries except a promotion and
the one switch-to-D append (§5.2).**

---

## 5. Capability Gate

### 5.1 Frontier signal (pinned)

**Exact models.json5 key:** the per-model `capabilities` array
(`internal/llm/providers.go:76` `Capabilities []string
json:"capabilities"`; parsed into `ModelConfig.Capabilities`
`map[string]bool` at `internal/llm/providers.go:660-663`, struct field
`internal/llm/models.go:234`).

The gate reads one tag value: **`"frontier"`** in that array, exposed as
`llm.CapFrontier = "frontier"` (new constant beside `CapToolUse`,
`internal/llm/provider_registry.go:32-46`) and tested via
`ModelConfig.HasCapability` (`internal/llm/models.go:349-352`). No new
top-level key, no new modelDef field for the tier — the capability list is
the tier metadata the orchestrator anticipated ("likely a `frontier: true`
tag or a capability-class field"; pinned to the capability list, the
existing parse path, zero new fields for the signal itself).

Ship examples in `config/models.json5`: add `"frontier"` to the
`agnes-3.0-flash` capabilities array (line 405) and `kimi-k2.6` (line 605) —
the frontier-class cloud entries. Local `lfm-*` models stay untagged →
non-frontier.

**Fail-safe:** unknown / unclassified / nil-Config models are
**non-frontier** (`IsFrontierClass(nil) == false`): absence of the tag
never upgrades a mode. This is the only direction allowed: the gate can
only down-escalate.

### 5.2 Semantics (all pinned consequences)

1. **Configured `search` + non-frontier active model:** effective mode :=
   `predicted`; exactly **one** warn log line names the model and the
   downgrade (`tool_exposure: model %s is not frontier-class; search
   downgraded to predicted`); repeat downgrades for the same model+mode
   are not re-logged (warn-once per active model).
2. **Mid-session switch to a lesser model** (configured `search` in effect):
   effective mode reverts to D **on the switch** — even mid-episode. The
   list effect is the switch-to-D append (§2.2): predicted set for the
   current episode intent appended to the active list. Allowed non-append
   churn class: the model switch already invalidates the prefix.
3. **Switch back to a frontier model:** `search` is RESTORED AT THE NEXT
   EPISODE BOUNDARY, not mid-episode — the D list (with its appended
   predicted tools) stays until the boundary; no flapping on repeated
   mid-episode model switches.
4. **Unknown model:** non-frontier per §5.1.

### 5.3 Docs requirement (binds later docs leaves)

Config template comment, the `docs/workflows` page, and the AGENT.md
frontmatter docs ALL state plainly:

> search mode requires a frontier-class model. Do NOT enable it for
> small/local models — they fail to use the search/view round-trip
> reliably (live-smoke evidence: always-full promotions
> 2026-09-07/09-12, `internal/config/schema.go:1959-1971`). The daemon
> downgrades search to predicted automatically for non-frontier models.

---

## 6. Starvation Recovery

Pinned behaviors (all covered by the 00T e2e cases, §8 leaf 00T):

| Situation | Model-visible behavior |
|---|---|
| Needed tool unlisted, in ceiling | `tool_search{query}` → name(s) + one-line desc → `tool_view{name}` → full schema + activation → callable next turn |
| `tool_search` query misses | structured "no matches" with the category digest (~10-line, ~150 tok 00A bound) for a broader retry; meta-tools never go away |
| Tool outside the role ceiling (never listed, never activatable) | activation refused with a reason naming the ceiling; `tool_search` never returns it (search domain = ceiling, §1 layer 1) — the model proceeds with in-ceiling tools or states the limitation |
| Executor sees a call to an unlisted-but-known tool name (hallucinated from digest/search) | actionable error: `tool "X" is not active in this episode; call tool_view{X} first` — not a generic unknown-tool error; this is the recovery vehicle (no new loop machinery; the existing nudge ladder is untouched) |
| Whole-episode class-miss | meta round-trip per §2.3; coarser recovery at the next episode boundary (new intent label re-predicts) |

No starvation may fail an episode: `tool_search`/`tool_view` are always in
the active set in dynamic modes, and they are always-full
(DefaultAlwaysFullTools, `internal/config/schema.go:1956`; registry
force-inclusion `internal/tools/registry.go:234`).

---

## 7. Config

All under `[agent.tools]` (`internal/config/schema.go:1924`
`AgentToolsConfig`, template `config/meept.json5`):

```json5
[agent.tools]
# dynamic_tool_exposure: master switch for the as-needed tool-exposure
# mechanism (branch 03). DEFAULT FALSE = byte-identical legacy
# (full/indexed only), per docs/agents/opt-in-defaults.md.
dynamic_tool_exposure = false
# tool_exposure: full | indexed | predicted | search. With the flag off,
# predicted/search silently downgrade to the indexed legacy default (one
# warn). With the flag on, the default is "predicted" (hybrid D).
# Resolution: per-model (models.json5 tool_exposure) > per-agent
# (AGENT.md frontmatter) > this value > "predicted".
#
# WARNING: "search" requires a frontier-class model (capabilities carry
# "frontier"). Do NOT enable it for small/local models — they fail to
# use the search/view round-trip reliably; the daemon downgrades search
# to predicted automatically for non-frontier models.
tool_exposure = "predicted"
# predicted_budget: N, the predicted-subset cap. Default 25; valid 1..30
# (00A: break-even at N=30->31 — N=31 costs more than indexed).
predicted_budget = 25
```

- **models.json5:** new optional per-model/per-provider `tool_exposure`
  (mirrors the existing per-model `schema_mode`, `internal/llm/providers.go:104`
  and `internal/llm/models.go:246`); capability tag `"frontier"` in the
  existing `capabilities` array (§5.1).
- **AGENT.md frontmatter:** `tool_exposure: predicted|search|full|indexed`
  (new key, parsed beside the existing `model:`/`model_alias:` spec fields,
  `internal/agent/spec.go:104-110`).
- **Intent map:** `config/tools_intent_map.json5` (§3.3), copied by
  `make config-bootstrap`; path overridable via the standard config-load
  mechanism.
- **Default N pin: 25.** 00A Table 4: D(N=25) saves 1,110 est-tok/turn;
  D(N=30) saves only 165; N=31 is worse than the indexed baseline. 25 stays
  on the favorable side of break-even with headroom and is the budget the
  00A shape-D line was measured at.

---

## 8. Leaf Decomposition (proposed)

Sizing per the skill rules: ≤128K context, single language per leaf, ≤3
files where possible. Repo e2e policy: new-feature tests live in the hermetic
e2e tier only (root AGENTS.md); no new unit-test files from these leaves.

| Leaf | Objective | Files | Est. ctx |
|---|---|---|---|
| **00T** | RED gate: hermetic e2e suite + manifest path_map entries for all below; capability-gate cases 1-4 from the orchestrator Integration Test Plan + starvation canary + 3-turn cache-continuity assertion | `e2e/suites/dynamic-tools/*.go` (new), `e2e/manifest.json` | 60K |
| **01** | Global config surface: flag + mode + budget on `AgentToolsConfig` with validation (§3.4, §7); `CapFrontier` constant; `tool_search` added to `DefaultAlwaysFullTools`; template comment (incl. the §5.3 warning) | `internal/config/schema.go`, `internal/llm/provider_registry.go`, `config/meept.json5` | 55K |
| **02** | Per-model tier + agent frontmatter: `ModelDef.ToolExposure` + `modelConfigFrom` parse; `ModelConfig.ToolExposure`; `AgentSpec.ToolExposure` frontmatter field; `config/models.json5` tags `frontier` on agnes-3.0-flash + kimi-k2.6 with per-model example | `internal/llm/providers.go`, `internal/llm/models.go`, `internal/agent/spec.go` | 60K |
| **03** | Intent map engine: `IntentMap`/`LoadIntentMap`/`For` (§3.3), ceiling-intersection + budget-cap; template `config/tools_intent_map.json5` (hand-curated v1, `_default`-safe) | `internal/agent/intent_tool_map.go`, `config/tools_intent_map.json5` | 40K |
| **04** | `tool_search` builtin: full-registry query scoped to the ceiling (name + one-line desc, top-k); params pinned to the 00A cost sketch (query string + limit int, ~168 tok); registered beside `tool_view` | `internal/tools/builtin/tool_search.go`, `internal/daemon/components.go` | 50K |
| **05** | Exposure engine + loop wiring: `tool_exposure.go` (§3.1: `EffectiveExposureMode`, `NewExposurePlan`, `Promote`, gate warn-once), loop episode-start hook (predict + filtered-registry + SetSchemaMode swap at the loop.go:4454/4459 site), switch-to-D append + pending-search-restore, `tool_view` activation hook (§3.2) | `internal/agent/tool_exposure.go`, `internal/agent/loop.go`, `internal/tools/builtin/tool_view.go` | 90K |
| **06** | Starvation/ceiling guards: executor actionable errors (§6 table rows 4-5: unlisted-but-known → tool_view directive; out-of-ceiling activation refusal naming the ceiling) | `internal/agent/executor.go` (+`internal/agent/loop.go` if the nudge hook needs a line) | 45K |
| **07** | Docs: `docs/workflows/dynamic-tools.md` (user-facing: knob, resolution order, gate, recovery); AGENT.md frontmatter doc; opt-in-defaults note; §5.3 warning verbatim in all three places | `docs/workflows/dynamic-tools.md`, `docs/agents/opt-in-defaults.md`, `AGENT.md` | 35K |

Dependency order: 00T (RED) → {01, 03, 04 parallel} → 02 → 05 → 06 → 07.
05 is the only leaf that may exceed 3 files of churn; it stays single-language.

---

## Appendix A — Verified anchors (grep-confirmed 2026-11-01)

| Anchor | Location |
|---|---|
| `GetDefinitions` call site | `internal/agent/loop.go:4454` |
| `StabilizeToolPrefix` call site | `internal/agent/loop.go:4459-4463` |
| `StabilizeToolPrefix` impl | `internal/agent/conversation.go:1201-1264` |
| `PrefixChanged` | `internal/agent/conversation.go:1269` |
| cache-boundary sentinel | `internal/agent/prompt.go:322` |
| `EffectiveSchemaMode` seam | `internal/llm/schema_mode.go:36`; call site `internal/agent/loop.go:1435` |
| `applySchemaModeLocked` | `internal/agent/loop.go:1410-1445` |
| `FilteredToolRegistry` / `NewFilteredToolRegistry` | `internal/agent/executor.go:2044` / `:2052` |
| `ToolRegistry` interface | `internal/agent/executor.go:463-470` |
| `AnalyzeTrueIntent` | `internal/agent/intent_analyzer.go:257` |
| `TrueIntentAnalysis` / `Category` | `internal/agent/intent_analyzer.go:17-25` / `:21` |
| valid intent categories | `internal/agent/intent_analyzer.go:372-377` |
| dispatcher call sites | `internal/agent/dispatcher.go:1240`, `:2304`; `LastIntent` store `:1254-1256` |
| `AgentToolsConfig` / `Validate` | `internal/config/schema.go:1924-1937` / `:1944-1951` |
| `DefaultAlwaysFullTools` | `internal/config/schema.go:1956-1973` |
| `Registry.SetSchemaMode` / force-inclusion | `internal/tools/registry.go:224` / `:234` |
| `SchemaModeIndexed`/`Full` consts | `internal/tools/registry.go:34-37` |
| `NewToolViewTool` | `internal/tools/builtin/tool_view.go:40` |
| `tool_view` registration | `internal/daemon/components.go:7177` |
| `ModelConfig` / `Capabilities` / `SchemaMode` | `internal/llm/models.go:221-302` / `:234` / `:246` |
| `HasCapability` / `HasCapabilities` | `internal/llm/models.go:349-352` / `:380-383` |
| `ModelDef.Capabilities` (json `capabilities`) | `internal/llm/providers.go:76` |
| `ModelDef.SchemaMode` (json `schema_mode`) | `internal/llm/providers.go:104` (model level; provider level `:56`) |
| caps-map parse (`modelConfigFrom`) | `internal/llm/providers.go:660-663` |
| capability constants | `internal/llm/provider_registry.go:32-46` |
| `AgentSpec` model frontmatter fields | `internal/agent/spec.go:104-110` |

## Appendix B — Deviations & notes

1. **Anchors drifted vs the orchestrator leaf:** the plan cited
   `loop.go:4437-4448` (actual call sites now 4454/4459), `executor.go:1927`
   (that line is `extractEditedFiles`; `FilteredToolRegistry` is at 2044-2101,
   constructor 2052), `schema.go:1954-1971` (`DefaultAlwaysFullTools` at
   1956), `registry.go:218` (`SetSchemaMode` at 224). This doc cites the
   verified current lines throughout.
2. **Cache-stability claim refined (§4.3):** the ratified "append-only
   promotions keep the prefix byte-identical" holds exactly for per-tool
   bytes and across turns; because `StabilizeToolPrefix` re-sorts, a
   promotion splices into alphabetical position and produces ONE bounded
   prefix re-cache event (same cost class as the accepted model-switch
   invalidation). Pinned as the mechanism leaves must preserve; no leaf may
   introduce per-turn list churn.
3. **Co-occurrence evidence skipped** (00A deviation 137: no per-tool
   telemetry in-repo) — the v1 intent map is hand-curated and `_default`-safe;
   N=25 is pinned from the 00A break-even math, not usage data. Telemetry-
   derived maps are a follow-up, not a gate.
4. **Default N = 25** pinned (§7): the ratified "N (predicted)" was left
   unsized; 00A Table 4 makes 25 the measured shape-D budget (saves 1,110
   est-tok; N≥31 loses to indexed).
5. **Flag-off + D/B config value** silently downgrades to indexed (one warn),
   not a load error — preserves "byte-identical legacy behavior when off";
   unknown *values* with the flag ON still fail fast at load (§3.4).
