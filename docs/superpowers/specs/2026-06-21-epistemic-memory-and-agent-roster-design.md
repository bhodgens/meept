# Epistemic Memory Platform and Agent Roster Extension

**Date:** 2026-06-21
**Status:** Design

## Problem

Meept's current agent roster (14 agents: dispatcher, chat, coder, debugger, planner, analyst, researcher, committer, scheduler + 5 reviewers) is heavily coding/ops-oriented. It serves a software engineer well but underserves a **solo tech person wearing many hats: constant researcher, builder, truth-seeker, writer**.

Two gap clusters:

1. **Epistemic / knowledge work.** No agent produces long-form writing, stress-tests hypotheses, gardens long-term knowledge, or teaches adaptively. No memory primitives distinguish a *claim* from a *conversation log* — everything is episodic.
2. **Truth-maintenance substrate.** The memory platform has rich consolidation, dedup, clustering, PageRank, and a typed-edge graph — but no epistemic edge types (`contradicts`, `superseded`, `evidence_for`, `evidence_against`). Nothing detects "this new claim reverses your prior claim." Nothing distinguishes high-trust user-asserted claims from low-trust inferred ones.

The memory platform is closer to supporting this than it appears. This spec extends it surgically and adds four new agents that use the extensions.

## Goal

1. Extend the memory platform with **epistemic types** (Claim, Decision, Prediction, Question), **epistemic edges** (`contradicts`, `superseded`, `evidence_for`, `evidence_against`, `derives_from`, `supports`), **trust-graded claim status**, and **LLM-driven relationship detection**.
2. Add **four creation paths** for epistemic memories: explicit tools, ambient extraction (opt-in), backlog mining, and reflection surfacing.
3. Add **four new agents**: `writer`, `architect`, `skeptic`, `librarian`. Tighten existing personas.
4. Bundle relevant external skills (`litreview`, `dossier`, `pulse`, `grill-me`, `code-tour`, `competitive-teardown`) as `available_skills` references.
5. Wire destructive-tool confirmation protocol across CLI, TUI, and GUI.

## Source Inspiration

- **oh-my-openagent** (`code-yeongyu/oh-my-openagent`): Named-persona orchestrator (Sisyphus), Hephaestus (autonomous deep worker), Prometheus (interview-mode planner), category-based model routing.
- **alirezarezvani/claude-skills**:
  - `engineering-team/` — senior-architect role skill; source for `architect` agent
  - `engineering/` — `grill-me`, `code-tour`, `llm-wiki` (typed-edge contradiction callout)
  - `research/` — `litreview`, `dossier`, `pulse`, `notebooklm`
  - `productivity/` — `capture`, `reflect` patterns
  - `product-team/` — `competitive-teardown` methodology
- **Meept's existing memory platform** (the substrate): `Consolidator`, `KnowledgeGraph`, `ClusterBySimilarity`, `RetainTool`/`RecallTool`/`ReflectTool`. Most "librarian" capabilities already exist as platform code; this spec extends them rather than duplicating.

## Scope Decomposition

Two plans, implemented sequentially. Plan 1 is the substrate; Plan 2 depends on it.

| Plan | Focus | Estimated Size |
|------|-------|----------------|
| Plan 1 — Epistemic Memory Platform | Types, edges, creation paths, detection, reflect deepening, confirmation protocol, TUI/GUI wiring | ~10 new files, ~8 file edits |
| Plan 2 — Agent Roster Extension | 4 new agents, persona tightening, skill bundles | ~5 new AGENT.md files, ~8 AGENT.md edits, ~6 new SKILL.md files |

---

# Plan 1: Epistemic Memory Platform

## Design Decisions

1. **Reuse existing tables.** New MemoryTypes and EdgeTypes are additive constants, not new schemas. The `episodic_memories` and `memory_edges` tables already use TEXT for `type`/`category`/`edge_type` columns.
2. **Trust-graded claims.** Every claim carries a `status` field (`confirmed`, `auto`, `promoted`, `rejected`) in its `Metadata`. Trust weight propagates to search ranking, edge confidence, canonical eligibility, and destructive-action permissions.
3. **Four creation paths, one promotion pipeline.** Explicit tools write `confirmed`; ambient/backlog/reflection all write `auto`. Single TUI/GUI surface promotes `auto`→`promoted` or marks `rejected`.
4. **Ambient extraction is opt-in.** Default off. When enabled, writes `auto` claims with default 0.5 trust weight. Configurable threshold, rate limit, intent exclusions.
5. **Confirmation protocol is a tool-level contract.** Destructive tools return preview responses with `requires_confirmation: true`. UI surfaces (CLI/TUI/GUI) intercept and re-invoke with `confirmed: true`. Headless-safe, auditable.
6. **No auto-supersession.** Destructive actions (`mark_superseded`) always require user confirmation. Auto-detected candidates queue as low-confidence `potential_contradicts` edges.

## Memory Platform Extensions

### New MemoryType constants

`internal/memory/types.go`:

```go
const (
    MemoryTypeClaim      MemoryType = "claim"
    MemoryTypeDecision   MemoryType = "decision"
    MemoryTypePrediction MemoryType = "prediction"
    MemoryTypeQuestion   MemoryType = "question"
)
```

These reuse the existing `episodic_memories` table. The Go-level `Type` discriminates them for tool routing; `Category` provides finer-grained labeling.

### New EdgeType constants

`internal/memory/graph.go`:

```go
const (
    EdgeTypeContradicts     EdgeType = "contradicts"
    EdgeTypeSuperseded      EdgeType = "superseded"
    EdgeTypeEvidenceFor     EdgeType = "evidence_for"
    EdgeTypeEvidenceAgainst EdgeType = "evidence_against"
    EdgeTypeDerivesFrom     EdgeType = "derives_from"
    EdgeTypeSupports        EdgeType = "supports"
)
```

No migration — `edge_type` is TEXT. Existing queries continue to work.

### ClaimStatus enum and trust weight

```go
type ClaimStatus string

const (
    ClaimStatusConfirmed ClaimStatus = "confirmed"
    ClaimStatusAuto      ClaimStatus = "auto"
    ClaimStatusPromoted  ClaimStatus = "promoted"
    ClaimStatusRejected  ClaimStatus = "rejected"
)

// DefaultAutoClaimTrustWeight is the default trust weight applied to
// ambient-extracted claims. Configurable via MemoryConfig.Epistemic.AutoTrustWeight.
const DefaultAutoClaimTrustWeight = 0.5

// TrustWeight returns the trust weight for a claim status.
//   confirmed, promoted → 1.0
//   auto                → configurable (default 0.5)
//   rejected            → 0.0
func (s ClaimStatus) TrustWeight(autoWeight float64) float64 {
    switch s {
    case ClaimStatusConfirmed, ClaimStatusPromoted:
        return 1.0
    case ClaimStatusAuto:
        if autoWeight <= 0 || autoWeight > 1.0 {
            return DefaultAutoClaimTrustWeight
        }
        return autoWeight
    case ClaimStatusRejected:
        return 0.0
    }
    return 0.0
}
```

Trust weight propagates to:
- Search result ranking (blended with relevance + PageRank)
- Edge confidence (edge weight × min(source trust, target trust))
- PageRank computation (rejected claims excluded; auto claims weighted)
- Canonical source eligibility (only confirmed/promoted eligible)
- `mark_superseded` permissions (auto claim cannot supersede a confirmed claim)
- Rendering: TUI/GUI show "(auto)" badge for `auto` claims

## Configuration

New `MemoryConfig.Epistemic` sub-config:

```go
// EpistemicConfig holds settings for epistemic memory (claims, decisions,
// predictions) and ambient extraction.
type EpistemicConfig struct {
    // AmbientExtraction configures the post-turn LLM classifier that
    // auto-writes low-trust claims. Default off.
    AmbientExtraction AmbientExtractionConfig `json:"ambient_extraction" toml:"ambient_extraction"`

    // AutoTrustWeight is the trust weight applied to auto-extracted claims.
    // Default 0.5. Range (0.0, 1.0]. Confirmed/promoted claims are always 1.0.
    AutoTrustWeight float64 `json:"auto_trust_weight" toml:"auto_trust_weight"`

    // DetectionThreshold is the minimum LLM confidence for epistemic edge
    // creation (contradicts, superseded, etc.). Default 0.7.
    DetectionThreshold float64 `json:"detection_threshold" toml:"detection_threshold"`

    // ReviewPromptFrequency controls how often the librarian surfaces
    // pending auto-claims for promotion. One of: "daily", "weekly",
    // "monthly", "manual". Default "weekly".
    ReviewPromptFrequency string `json:"review_prompt_frequency" toml:"review_prompt_frequency"`

    // MaxPendingReviews caps the number of auto-claims surfaced in a single
    // review prompt. Default 20.
    MaxPendingReviews int `json:"max_pending_reviews" toml:"max_pending_reviews"`
}

type AmbientExtractionConfig struct {
    // Enabled turns on ambient extraction. Default false.
    Enabled bool `json:"enabled" toml:"enabled"`

    // ConfidenceThreshold is the LLM-classifier gate; candidates below this
    // are dropped. Default 0.7.
    ConfidenceThreshold float64 `json:"confidence_threshold" toml:"confidence_threshold"`

    // MaxPerTurn caps the number of auto-claims written per turn.
    // Default 3.
    MaxPerTurn int `json:"max_per_turn" toml:"max_per_turn"`

    // ExcludeIntents lists intent types to skip entirely (e.g., casual chat).
    // Default ["chat", "recall"].
    ExcludeIntents []string `json:"exclude_intents" toml:"exclude_intents"`

    // ExcludeCategories lists classifier-detected categories to skip
    // (e.g., "joke", "sarcasm", "what-if").
    // Default empty.
    ExcludeCategories []string `json:"exclude_categories" toml:"exclude_categories"`

    // ContextWindow is the number of recent conversation messages to scan.
    // Default 5.
    ContextWindow int `json:"context_window" toml:"context_window"`
}
```

`~/.meept/meept.json5` example:

```json5
{
  memory: {
    epistemic: {
      ambient_extraction: {
        enabled: true,                  // opt-in
        confidence_threshold: 0.75,
        max_per_turn: 3,
        exclude_intents: ["chat", "recall"],
        exclude_categories: ["joke", "sarcasm", "what-if"],
        context_window: 5,
      },
      auto_trust_weight: 0.5,           // default
      detection_threshold: 0.7,
      review_prompt_frequency: "weekly",
      max_pending_reviews: 20,
    },
  },
}
```

**Documentation update:** `docs/configuration/` must add a new "Epistemic Memory" page documenting every field with default, range, and example. Also update `docs/reference/generated/` via `make docs-generate` (per CLAUDE.md schema-change requirement).

## Epistemic Memory Helpers

New file: `internal/memory/epistemic.go`.

### Structured types

```go
// Claim is a structured assertion of belief.
type Claim struct {
    Text       string    // the claim itself
    Premises   []string  // supporting claim IDs or text snippets
    Source     string    // URL, citation, or "user"
    Confidence float64   // 0.0-1.0, user-asserted
    Tags       []string  // controlled-vocabulary tags (see Tag Taxonomy)
    Status     ClaimStatus
}

// Decision is a recorded call with expected outcome and review schedule.
type Decision struct {
    Call            string     // the decision made
    Alternatives    []string   // alternatives considered
    ExpectedOutcome string     // what the user expects to happen
    ReviewAt        *time.Time // when to revisit; nil = no auto-review
    Premises        []string   // claim IDs this decision rests on
    Status          string     // "open", "reviewed", "superseded"
}

// Prediction is a forecast with horizon and resolution tracking.
type Prediction struct {
    Forecast         string     // the prediction
    Horizon          time.Time  // when it should resolve
    RelatedDecision  string     // decision ID (optional)
    Outcome          string     // filled in on resolution
    ResolvedAt       *time.Time
}

// Question is an open question the user is tracking.
type Question struct {
    Text         string   // the question
    RelatedClaims []string // claim IDs that bear on this question
    Status       string   // "open", "answered"
    AnswerClaim  string   // claim ID that answers it (if answered)
}
```

### Storage functions

```go
// StoreClaim writes a claim as a typed memory and returns its ID.
func (m *Manager) StoreClaim(ctx context.Context, c Claim) (string, error)

// StoreDecision writes a decision as a typed memory and returns its ID.
func (m *Manager) StoreDecision(ctx context.Context, d Decision) (string, error)

// StorePrediction writes a prediction as a typed memory and returns its ID.
func (m *Manager) StorePrediction(ctx context.Context, p Prediction) (string, error)

// StoreQuestion writes an open question as a typed memory and returns its ID.
func (m *Manager) StoreQuestion(ctx context.Context, q Question) (string, error)

// MarkSuperseded flips is_current=0 on oldID, writes a superseded edge from
// oldID to newID, redirects incoming evidence_for/evidence_against edges.
// Requires user confirmation (see Confirmation Protocol).
func (m *Manager) MarkSuperseded(ctx context.Context, oldID, newID string) (redirectedEdges int, auditID string, err error)

// MarkResolved closes a prediction with the given outcome.
func (m *Manager) MarkResolved(ctx context.Context, predictionID, outcome string) (auditID string, err error)

// RecordReview closes a decision with the actual outcome and scores the
// expected-vs-actual gap.
func (m *Manager) RecordReview(ctx context.Context, decisionID, actualOutcome string) (score float64, auditID string, err error)

// PromoteClaim transitions an auto claim to promoted status.
func (m *Manager) PromoteClaim(ctx context.Context, claimID string) error

// RejectClaim transitions an auto claim to rejected status.
func (m *Manager) RejectClaim(ctx context.Context, claimID string) error

// ListPendingReviews returns decisions whose ReviewAt is before the given
// time, and predictions whose Horizon is before the given time.
func (m *Manager) ListPendingReviews(ctx context.Context, before time.Time) (decisions []MemoryResult, predictions []MemoryResult, err error)

// ListAutoClaims returns claims with status=auto, optionally filtered by
// created_after for incremental review prompts.
func (m *Manager) ListAutoClaims(ctx context.Context, createdAfter time.Time, limit int) ([]MemoryResult, error)

// FindCanonicalFor returns the canonical claim for a topic. Walks
// canonical_for metadata first, falls back to highest-PageRank confirmed
// claim in the topic community. Never returns auto or rejected claims.
func (m *Manager) FindCanonicalFor(ctx context.Context, topic string) (*Memory, error)
```

### Canonical source convention

Claims can declare `Metadata["canonical_for"] = "<topic>"`. The librarian and search use this to identify the authoritative version of an idea.

## Epistemic Detection

New file: `internal/memory/epistemic_detection.go`.

### Detector

```go
// EpistemicDetector identifies relationships between memories using
// embedding similarity and LLM classification.
type EpistemicDetector struct {
    graph    *KnowledgeGraph
    manager  *Manager
    llm      llm.Chatter
    embedder EmbeddingProvider
    threshold float64  // from EpistemicConfig.DetectionThreshold
    autoWeight float64 // from EpistemicConfig.AutoTrustWeight
}

// DetectRelationships examines a new memory against existing memories
// and returns candidate edges. Does not write edges; caller decides.
//
// Pipeline:
//   1. Embed new memory content.
//   2. Find top-K (default 10) similar memories via FTS + embedding search,
//      filtered to confirmed/promoted claims only (auto claims excluded
//      from detection targets to avoid noise).
//   3. LLM prompt: classify each pair into one of:
//      contradicts, superseded, evidence_for, evidence_against,
//      derives_from, supports, unrelated.
//      Return JSON with confidence scores.
//   4. Filter: drop classifications below threshold.
//   5. Return candidate edges with confidence-weighted Weight.
func (d *EpistemicDetector) DetectRelationships(ctx context.Context, newMem Memory) ([]MemoryEdge, error)
```

### Wiring

Detection runs in two places:

1. **`Manager.Store` post-hook** (gated on MemoryType): runs only for Claim, Decision, Prediction, Question. New memories with these types trigger a detection pass against existing memories. Cheap gate on type.
2. **`Consolidator.Run`**: full pass over all epistemic memories added since the last consolidation. Catches relationships missed by the per-store hook (e.g., when the comparison set changes).

### Potential contradictions queue

Low-confidence contradiction candidates (below `DetectionThreshold` but above a lower `PotentialContradictionThreshold` = 0.4) are written as `potential_contradicts` edges with low weight (0.2). These surface in reflection output and librarian prompts as "potential contradictions to review" but do not propagate to search ranking or destructive actions.

## Creation Paths

### Path A: Explicit tools (status=confirmed)

New file: `internal/tools/builtin/retain_typed.go`.

Three tools following the existing `RetainTool` pattern:

```go
// RetainClaimTool writes a claim with status=confirmed.
type RetainClaimTool struct {
    manager *memory.Manager
}

// RetainDecisionTool writes a decision with status=open.
type RetainDecisionTool struct {
    manager *memory.Manager
}

// RetainPredictionTool writes a prediction with status=open.
type RetainPredictionTool struct {
    manager *memory.Manager
}
```

Schema (retain_claim example):

```json
{
  "name": "retain_claim",
  "parameters": {
    "text": {"type": "string", "required": true},
    "premises": {"type": "array", "items": {"type": "string"}},
    "source": {"type": "string"},
    "confidence": {"type": "number"},
    "tags": {"type": "array", "items": {"type": "string"}}
  }
}
```

All three tools are baseline (available to all agents). They write `confirmed` trust weight.

### Path B: Ambient extraction (status=auto)

New file: `internal/memory/epistemic_ambient.go`.

New file: `internal/agent/epistemic_hook.go`.

Post-turn hook pipeline:

1. Trigger: after every agent turn completes.
2. Gate: if `EpistemicConfig.AmbientExtraction.Enabled = false`, exit.
3. Gate: if intent matches `ExcludeIntents`, exit.
4. Read last `ContextWindow` conversation messages.
5. LLM call (cheap classifier model, configurable): extract candidate claims/decisions/predictions.
6. Filter by `ConfidenceThreshold`.
7. Filter by `ExcludeCategories`.
8. Cap to `MaxPerTurn`.
9. Write each as `Memory{Type: Claim/Decision/Prediction, Metadata.status: "auto"}`.
10. Queue for next `ReviewPromptFrequency` cycle.

**LLM prompt template:**

```
You are an epistemic extractor. Read the following conversation segment and
extract assertions of belief (claims), forward-looking commitments (decisions),
and forecasts (predictions). For each candidate:

- Only extract statements the speaker is committing to, not hypotheticals,
  questions, sarcasm, jokes, or quotations of others' views.
- Skip pleasantries, agreements without content, and meta-conversation.

Return JSON array. Each element:
{
  "type": "claim" | "decision" | "prediction",
  "text": "<the assertion>",
  "source": "conversation",
  "confidence": 0.0-1.0,
  "premises": [],
  "category": "<one of: architecture, business, technical, prediction, opinion, methodology>"
}

If no candidates, return [].

Conversation:
<conversation>
```

**Cost management:** The hook uses the cheapest configured classifier model (typically a small model like glm-4.5-air or local). Cost is logged per invocation.

### Path C: Backlog mining (auto → promote)

New skill file: `config/skills/librarian-backlog-mining/SKILL.md`.

Librarian walks existing episodic memory (filtered by age, category, or keyword), runs the same LLM classifier as Path B over batches, and writes candidates as `status=auto`. Surfaces them to the user via the promotion UI.

Distinguished from Path B by: (a) batch rather than streaming, (b) walks old memory rather than recent conversation, (c) initiated explicitly by librarian or user command.

### Path D: Reflection surfacing (auto → promote)

Integrated into the deepened `ReflectTool.Execute` (see below).

When `reflect` runs over a period, it identifies "assertions detected in recent conversation" that haven't been recorded as claims. These are surfaced as candidates in the reflection output, with a "record as claim?" prompt. User promotion writes them as `status=auto` initially, immediately eligible for promotion to `promoted`.

### Promotion pipeline (single surface for B/C/D)

All three auto-producing paths (B, C, D) feed into one review surface. The TUI and GUI render a list of pending auto-claims with actions:

| Action | Result |
|---|---|
| Promote | `status=promoted`, trust weight 1.0 |
| Reject | `status=rejected`, excluded from queries |
| Edit then promote | User edits text, then promotes |
| Skip | Stays `auto`, surfaces again next cycle |

Triggered:
- Manually via `meept memory review` (CLI)
- On schedule via `ReviewPromptFrequency` (librarian cron)
- On demand by asking librarian "what should I review?"

## Tag Taxonomy

To avoid uncontrolled vocabulary bloat, define a curated tag taxonomy in `config/epistemic_tags.json5`:

```json5
{
  domains: ["architecture", "business", "technical", "methodology", "opinion", "prediction"],
  confidence_levels: ["speculative", "probable", "certain"],
  scopes: ["project", "personal", "industry", "universal"],
}
```

Librarian applies tags during promotion. Users can extend the taxonomy via `~/.meept/epistemic_tags.json5`. Tags outside the taxonomy are flagged in TUI/GUI with a "non-standard tag" hint but not rejected.

## Deepened ReflectTool

Rewrite `ReflectTool.Execute` in `internal/tools/builtin/memory_curation.go`.

Current behavior: category-counting + one-line summary.

New behavior: pull recent epistemic memories → walk `contradicts`/`superseded`/`potential_contradicts` edges → surface pending reviews → LLM theme synthesis → structured return.

### New return shape

```json
{
  "period": {"start": "2026-06-14", "end": "2026-06-21"},
  "themes": [
    {
      "name": "Go performance reconsidered",
      "memory_ids": ["claim_a1b2", "claim_c3d4", "claim_d5e6"],
      "summary": "You revised your stance on Go's performance after pprof data.",
      "confidence": 0.85
    }
  ],
  "contradictions": [
    {
      "newer_id": "claim_c3d4",
      "newer_preview": "Go is fast enough for most workloads after pprof...",
      "older_id": "claim_a1b2",
      "older_preview": "Go is slow for any production workload",
      "edge_confidence": 0.82,
      "explanation": "Newer claim directly reverses the older one."
    }
  ],
  "potential_contradictions": [
    {
      "newer_id": "...",
      "older_id": "...",
      "edge_confidence": 0.52,
      "explanation": "Possible tension — review needed."
    }
  ],
  "supersessions": [],
  "pending_reviews": {
    "decisions": [
      {"id": "decision_f7g8", "call": "Use SQLite for v1", "review_at": "2026-06-20"}
    ],
    "predictions": [
      {"id": "prediction_h9i0", "forecast": "User count hits 100 by Q3", "horizon": "2026-09-30"}
    ]
  },
  "auto_candidates": [
    {
      "id": "claim_j1k2",
      "text": "TypeScript is better than JavaScript for large projects",
      "detected_at": "2026-06-18",
      "extractor": "ambient",
      "suggested_action": "promote"
    }
  ],
  "open_questions": [
    {"id": "question_l3m4", "text": "Should we adopt CRDTs for sync?"}
  ]
}
```

### LLM theme synthesis

The reflect tool calls the configured LLM to cluster epistemic memories into themes. Prompt:

```
You are reflecting on a user's recent claims, decisions, and predictions.
Group them into 1-5 themes. For each theme:
- Name it concisely
- List the memory IDs that belong
- Summarize the theme in 1-2 sentences
- Note your confidence in the grouping

Return JSON array of themes.

Memories:
<memory list with IDs and content>
```

## Confirmation Protocol

### Shared helper

New file: `internal/tools/builtin/confirmation.go`.

```go
// ConfirmationResponse builds a requires_confirmation tool response.
func ConfirmationResponse(action string, reversible bool, summary string, details map[string]any) map[string]any {
    return map[string]any{
        "requires_confirmation": true,
        "action":                action,
        "reversible":            reversible,
        "summary":               summary,
        "details":               details,
        "confirm_arg":           "confirmed",
    }
}

// IsConfirmationRequest checks whether a tool response is asking for
// user confirmation.
func IsConfirmationRequest(result map[string]any) bool {
    v, _ := result["requires_confirmation"].(bool)
    return v
}

// DeclineResponse builds the response returned when the user declines
// a confirmation prompt.
func DeclineResponse(original map[string]any) map[string]any {
    return map[string]any{
        "declined":  true,
        "action":    original["action"],
        "summary":   original["summary"],
        "user_note": "user declined confirmation",
    }
}
```

### Destructive tools

The following tools use the confirmation protocol:

| Tool | Reversible | What it changes |
|---|---|---|
| `mark_superseded` | Yes | Flips `is_current=0` on old memory; redirects edges |
| `mark_resolved` | No | Closes prediction; writes outcome |
| `record_review` | No | Closes decision; writes outcome |
| `reject_claim` | Yes | Sets status=rejected |
| `purge_auto_claims` | No | Bulk delete auto claims matching filter |

Each tool takes a `confirmed` parameter (default `false`). Phase 1 returns preview via `ConfirmationResponse`. Phase 2 performs the action when `confirmed=true`.

New file: `internal/tools/builtin/epistemic_actions.go` containing all five tools.

### Example: `mark_superseded`

```go
func (t *MarkSupersededTool) Execute(ctx context.Context, args map[string]any) (any, error) {
    oldID, _ := args["old_id"].(string)
    newID, _ := args["new_id"].(string)
    confirmed, _ := args["confirmed"].(bool)

    if oldID == "" || newID == "" {
        return nil, fmt.Errorf("old_id and new_id are required")
    }

    // Phase 1: preview
    if !confirmed {
        old, err := t.manager.GetByID(ctx, oldID)
        if err != nil { return nil, err }
        new, err := t.manager.GetByID(ctx, newID)
        if err != nil { return nil, err }

        affectedEdges, _ := t.graph.EdgeCount(ctx, oldID)

        return ConfirmationResponse(
            "mark_superseded",
            true,
            fmt.Sprintf("Mark %q as superseded by %q?", truncate(old.Content, 80), truncate(new.Content, 80)),
            map[string]any{
                "old_id":         oldID,
                "old_preview":    truncate(old.Content, 200),
                "old_created_at": old.CreatedAt,
                "old_status":     old.Metadata["status"],
                "new_id":         newID,
                "new_preview":    truncate(new.Content, 200),
                "new_created_at": new.CreatedAt,
                "new_status":     new.Metadata["status"],
                "affected_edges": affectedEdges,
            },
        ), nil
    }

    // Phase 2: execute
    redirected, auditID, err := t.manager.MarkSuperseded(ctx, oldID, newID)
    if err != nil { return nil, err }
    return map[string]any{
        "executed":          true,
        "action":            "mark_superseded",
        "old_id":            oldID,
        "new_id":            newID,
        "edges_redirected":  redirected,
        "audit_id":          auditID,
    }, nil
}
```

## UI Wiring

### TUI (bubbletea)

**New file:** `internal/tui/confirmation.go` — a centered modal component.

```
┌─────────────────────────────────────────────────────────┐
│  mark_superseded — confirm action                       │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  Mark the following claim as superseded?                │
│                                                         │
│  OLD (claim_a1b2, 2026-03-14, confirmed):               │
│    "Go is slow for any production workload"             │
│                                                         │
│  NEW (claim_c3d4, 2026-06-21, confirmed):               │
│    "Go is fast enough for most workloads after          │
│     pprof-driven optimization"                          │
│                                                         ││  3 edges will be redirected to the new claim.           │
│  Reversible: yes                                        │
│                                                         │
│  [y] confirm    [n] cancel    [v] view full details     │
└─────────────────────────────────────────────────────────┘
```

Keybindings: `y` confirm, `n` cancel, `v` full details, `esc` cancel.

**Edit:** `internal/tui/tools.go` (or the tool dispatch site). After every tool call, before returning to the agent loop, intercept:

```go
result, err := tool.Execute(ctx, args)
if err != nil { return ... }

if resultMap, ok := result.(map[string]any); ok && builtin.IsConfirmationRequest(resultMap) {
    confirmed, err := t.confirmationModal(resultMap)
    if err != nil || !confirmed {
        return builtin.DeclineResponse(resultMap), nil
    }
    args["confirmed"] = true
    return tool.Execute(ctx, args)
}
return result, nil
```

Reuses the existing `ask.go` blocking-UI-from-tool pattern. ~30 lines of glue + ~150 lines for the modal component.

### GUI (Flutter)

**New file:** `ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart` — a `StatefulWidget` dialog rendering old/new memory previews, affected edges, reversibility, and confirm/cancel buttons.

**Edit:** `ui/flutter_ui/lib/services/tool_runner.dart`. Same interceptor:

```dart
final result = await toolRunner.execute(toolName, args);
if (result['requires_confirmation'] == true) {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (_) => DestructiveConfirmationDialog(response: result),
  );
  if (confirmed != true) {
    return ToolResult.declined(result);
  }
  return toolRunner.execute(toolName, {...args, 'confirmed': true});
}
return result;
```

### CLI

The CLI renders the preview as text and prompts:

```bash
$ meept memory supersede claim_a1b2 claim_c3d4

Action: mark_superseded
  OLD (claim_a1b2, 2026-03-14, confirmed):
    "Go is slow for any production workload"
  NEW (claim_c3d4, 2026-06-21, confirmed):
    "Go is fast enough for most workloads after pprof-driven optimization"
  3 edges will be redirected. Reversible: yes.

Confirm? [y/N] y
✓ Supersession recorded (audit_id: audit_7g8h)
```

Non-interactive: `meept memory supersede claim_a1b2 claim_c3d4 --confirm`.

## HTTP/RPC API Wiring

Per CLAUDE.md wiring requirement, the new tools and operations surface via RPC and HTTP:

### RPC methods (new)

| Method | Purpose |
|---|---|
| `memory.retainClaim` | Path A: write a confirmed claim |
| `memory.retainDecision` | Path A: write a decision |
| `memory.retainPrediction` | Path A: write a prediction |
| `memory.markSuperseded` | Destructive: supersede (two-phase) |
| `memory.markResolved` | Destructive: resolve prediction |
| `memory.recordReview` | Destructive: review decision |
| `memory.promoteClaim` | Promote auto claim to promoted |
| `memory.rejectClaim` | Reject auto claim |
| `memory.listAutoClaims` | List pending auto claims |
| `memory.listPendingReviews` | List decisions/predictions due for review |
| `memory.findCanonical` | Find canonical claim for a topic |
| `memory.reviewQueue` | Get the full review queue (auto + pending reviews) |

### HTTP endpoints (new)

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/v1/memory/claims` | POST | Retain a claim |
| `/api/v1/memory/claims/{id}/promote` | POST | Promote auto claim |
| `/api/v1/memory/claims/{id}/reject` | POST | Reject auto claim |
| `/api/v1/memory/decisions` | POST | Retain a decision |
| `/api/v1/memory/decisions/{id}/review` | POST | Record decision review |
| `/api/v1/memory/predictions` | POST | Retain a prediction |
| `/api/v1/memory/predictions/{id}/resolve` | POST | Resolve a prediction |
| `/api/v1/memory/supersede` | POST | Mark superseded (two-phase) |
| `/api/v1/memory/canonical` | GET | Find canonical for topic |
| `/api/v1/memory/review-queue` | GET | Get review queue |
| `/api/v1/memory/auto-claims` | GET | List auto claims |

All endpoints flow through the existing service layer pattern. RPC handlers + HTTP handlers dispatch to the same `Manager` methods.

### Menubar app

The menubar app exposes a "memory review" tab showing pending auto-claims, pending decision/prediction reviews, and detected contradictions. Tap to promote/reject/confirm-destructive-action via HTTP.

## Files Changed (Plan 1)

### New files

| File | Purpose |
|---|---|
| `internal/memory/epistemic.go` | Claim/Decision/Prediction/Question types + Manager helper methods |
| `internal/memory/epistemic_detection.go` | LLM-driven relationship detection |
| `internal/memory/epistemic_ambient.go` | Post-turn ambient extraction pipeline |
| `internal/agent/epistemic_hook.go` | Post-turn hook wiring for ambient extraction |
| `internal/tools/builtin/retain_typed.go` | Path A tools (retain_claim, retain_decision, retain_prediction) |
| `internal/tools/builtin/epistemic_actions.go` | Destructive tools with confirmation protocol |
| `internal/tools/builtin/confirmation.go` | Shared confirmation helpers |
| `internal/tui/confirmation.go` | TUI modal component |
| `ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart` | Flutter dialog |
| `config/epistemic_tags.json5` | Curated tag taxonomy |
| `internal/memory/epistemic_test.go` | Tests for helpers |
| `internal/memory/epistemic_detection_test.go` | Tests for detection |
| `internal/memory/epistemic_ambient_test.go` | Tests for ambient pipeline |
| `internal/tools/builtin/retain_typed_test.go` | Tests for Path A tools |
| `internal/tools/builtin/epistemic_actions_test.go` | Tests for destructive tools |
| `internal/tools/builtin/confirmation_test.go` | Tests for confirmation helpers |
| `internal/tui/confirmation_test.go` | Tests for TUI modal |

### Modified files

| File | Change |
|---|---|
| `internal/memory/types.go` | Add `MemoryTypeClaim/Decision/Prediction/Question` constants |
| `internal/memory/graph.go` | Add `EdgeType*` constants for epistemic edges |
| `internal/memory/manager.go` | Add `Epistemic` config field; wire post-Store hook for detection |
| `internal/memory/consolidation.go` | Add epistemic detection pass |
| `internal/tools/builtin/memory_curation.go` | Deepen `ReflectTool.Execute` |
| `internal/config/schema.go` | Add `EpistemicConfig` and `AmbientExtractionConfig`; add to `MemoryConfig` |
| `internal/daemon/components.go` | Wire `EpistemicDetector`, ambient hook |
| `internal/tui/tools.go` | Intercept `requires_confirmation` |
| `ui/flutter_ui/lib/services/tool_runner.dart` | Intercept `requires_confirmation` |
| `internal/rpc/server.go` | New RPC methods |
| `internal/comm/http/api_handlers.go` | New HTTP endpoints |
| `internal/comm/http/config_service.go` | Expose epistemic config |
| `cmd/meept/memory.go` (or equivalent) | CLI: `meept memory review`, `meept memory supersede`, etc. |
| `docs/configuration/epistemic-memory.md` | New config docs |
| `docs/reference/generated/` | Regenerate via `make docs-generate` |

## Testing Strategy

1. **Unit tests** for all new helpers (`epistemic.go`, `epistemic_detection.go`, `epistemic_ambient.go`).
2. **Tool tests** for Path A and destructive tools, including two-phase confirmation flow.
3. **Integration test** for ambient extraction hook: configure enabled, simulate conversation, verify auto-claim written with correct status.
4. **Integration test** for promotion pipeline: auto claim → promote → trust weight changes.
5. **Integration test** for detection: store two contradicting claims, verify `contradicts` edge written.
6. **Integration test** for supersession: two-phase confirmation, edge redirection, `is_current` flip.
7. **TUI test** for confirmation modal rendering and keybinding.
8. **GUI test** for dialog rendering and callback.

---

# Plan 2: Agent Roster Extension

## Design Decisions

1. **AGENT.md is the only source of truth** (per the 2026-06-20 consolidation spec). All new agents get AGENT.md files in `config/agents/<id>/`.
2. **Librarian explicitly orchestrates Plan 1 primitives.** It does not reimplement consolidation, dedup, or clustering — those exist. Its value-add is: tag hygiene, deepened reflection, epistemic-integrity surfacing, promotion UI driving, backlog mining.
3. **Writer and architect are high-value, low-risk additions.** No dependencies on Plan 1; they work on existing tools.
4. **Skeptic is a dedicated adversarial persona.** Distinct from reviewer agents (which review work products) — the skeptic hunts for what's wrong with the user's *own claims* using Plan 1's epistemic edges.
5. **Skills are bundled as `available_skills` references in AGENT.md frontmatter.** The skill files ship in `config/skills/` as bundled defaults.

## New Agents

### `writer` — Writing Specialist

```yaml
---
id: writer
name: Writing Specialist
role: executor
description: Produces long-form writing — essays, docs, briefs, explanations
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - file_write
  - web_fetch
  - web_search
capabilities:
  - reasoning
max_iterations: 20
timeout_seconds: 900
max_tokens_per_turn: 8192
temperature: 0.7
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
---
```

**Body focus:** long-form writing (essays, documentation, briefs, explanations). Adapts voice to audience. Uses memory to ground claims and maintain consistency with prior writing. Suggests retention of strong formulations as claims.

**Intent routing:** `IntentWrite` (new) for "write an essay about X", "draft a doc for Y", "explain Z for a general audience". Falls back to `planner` for decomposition if the writing task is multi-step.

### `architect` — Architecture Specialist

```yaml
---
id: architect
name: Architecture Specialist
role: executor
description: Designs systems, chooses technologies, documents trade-offs and decisions
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - file_write
  - list_directory
  - shell_execute
  - web_fetch
  - web_search
capabilities:
  - code
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
temperature: 0.4
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
  - conditional.code_style
---
```

**Body focus:** system design, tech stack evaluation, architecture diagrams, trade-off matrices. Records architectural decisions as `Decision` memories (Plan 1). Distinct from `planner` (which decomposes tasks) and `coder` (which implements).

**Intent routing:** `IntentArchitect` (new) for "design a system for X", "compare these technologies", "should we use Y or Z".

### `skeptic` — Adversarial Reasoner

```yaml
---
id: skeptic
name: Skeptic
role: executor
description: Stress-tests claims, hunts for flaws in reasoning, surfaces contradictions
enabled: true
can_delegate: false
additional_tools:
  - memory_search
  - web_search
  - web_fetch
  - file_read
capabilities:
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
temperature: 0.3
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - conditional.source_evaluation
---
```

**Body focus:** adversarial reasoning, steelman-then-attack methodology (from `grill-me`), contradiction surfacing via Plan 1 epistemic edges, evidence weighing. Distinct from reviewer agents (which review work products) — the skeptic interrogates claims and beliefs.

**Intent routing:** `IntentSkeptic` (new) for "stress-test this claim", "what's wrong with my reasoning", "steelman the opposing view".

**Plan 1 dependency:** uses `contradicts`/`evidence_against` edges to find tensions. Without Plan 1, still functional as a reasoning agent but loses the substrate-backed contradiction surfacing.

### `librarian` — Memory Steward

```yaml
---
id: librarian
name: Memory Steward
role: executor
description: Tends the memory platform — dedup, tag hygiene, reflection, epistemic integrity
enabled: true
can_delegate: false
additional_tools:
  - memory_search
  - memory_store
  - retain
  - recall
  - reflect
  - retain_claim
  - retain_decision
  - retain_prediction
  - mark_superseded
  - mark_resolved
  - record_review
  - promote_claim
  - reject_claim
capabilities:
  - reasoning
max_iterations: 25
timeout_seconds: 900
max_tokens_per_turn: 4096
temperature: 0.3
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
available_skills:
  - librarian-backlog-mining
  - librarian-reflection-surfacing
  - librarian-tag-hygiene
skill_triggers:
  "review memory": librarian-reflection-surfacing
  "clean up tags": librarian-tag-hygiene
  "mine backlog": librarian-backlog-mining
---
```

**Body focus:**

> You are the memory steward. The platform already has consolidation, dedup, PageRank, and typed-edge graphs — your job is to drive them.
>
> Your core concerns:
> 1. **Tag hygiene**: normalize tags to the controlled vocabulary; propose canonical versions of near-duplicate claims.
> 2. **Reflection**: run `reflect` on schedule; surface themes, contradictions, pending reviews, and auto-claim candidates to the user.
> 3. **Epistemic integrity**: when Plan 1 detection surfaces `contradicts` or `superseded` edges, present them to the user for resolution. Never auto-supersede.
> 4. **Backlog mining**: periodically walk old episodic memory to recover claims/decisions worth promoting.
> 5. **Promotion pipeline**: drive the single review surface for auto-claims from Paths B/C/D.
>
> You do NOT reimplement consolidation, dedup, or clustering. Those exist in the platform. You orchestrate them.

**Intent routing:** `IntentLibrarian` (new) for "review my memory", "what have I been thinking about", "clean up tags", "what contradictions exist".

**Plan 1 dependency:** core functionality. Without Plan 1, librarian degrades to driving existing `reflect`/`retain`/`recall` — still useful but much weaker.

## Existing Persona Tightening

### `analyst` — sharper synthesis focus

Edit `config/agents/analyst/AGENT.md` body to sharpen the synthesis/gather boundary with `researcher`. Add explicit instruction: "When asked to evaluate competing claims, use the skeptic agent or `memory_search` to find contradicting evidence rather than gathering fresh sources yourself."

### `researcher` — owns gathering with citation

Already tightened in the 2026-06-20 consolidation. Add explicit reference to the `litreview` and `dossier` skills for long-form research.

### `chat` — clearer delegation routing

Edit `config/agents/chat/AGENT.md` to add the new agents (writer, architect, skeptic, librarian) to the delegation routing table.

### `dispatcher` — updated routing table

Edit `config/agents/dispatcher/AGENT.md` routing table to include the four new agents and their intent types.

## New Intent Types

`internal/agent/intent.go` additions:

```go
IntentWrite      // essays, docs, briefs, long-form explanations
IntentArchitect  // system design, tech evaluation, trade-offs
IntentSkeptic    // stress-testing claims, steelman-attack
IntentLibrarian  // memory review, reflection, tag cleanup
```

Each maps to its respective agent with keyword triggers.

## Bundled Skills

Six new skill files in `config/skills/`:

| Skill | Adapted From | Lives On |
|---|---|---|
| `litreview` | alirezarezvani/research/litreview | `researcher.available_skills` |
| `dossier` | alirezarezvani/research/dossier | `researcher.available_skills` |
| `pulse` | alirezarezvani/research/pulse | `scheduler` (recurring job template) |
| `grill-me` | alirezarezvani/engineering/grill-me | `skeptic.available_skills` (core methodology) |
| `code-tour` | alirezarezvani/engineering/code-tour | `researcher.available_skills` |
| `competitive-teardown` | alirezarezvani/product-team/competitive-teardown | `analyst.available_skills` |

Plus three new librarian-specific skills:

| Skill | Purpose |
|---|---|
| `librarian-backlog-mining` | Walks old episodic memory, proposes claim/decision candidates |
| `librarian-reflection-surfacing` | Surfaces reflection themes + auto-claim candidates to user |
| `librarian-tag-hygiene` | Normalizes tags to controlled vocabulary |

Each skill is a SKILL.md file following the existing format (YAML frontmatter + procedural body).

## Files Changed (Plan 2)

### New files

| File | Purpose |
|---|---|
| `config/agents/writer/AGENT.md` | Writer agent definition |
| `config/agents/architect/AGENT.md` | Architect agent definition |
| `config/agents/skeptic/AGENT.md` | Skeptic agent definition |
| `config/agents/librarian/AGENT.md` | Librarian agent definition |
| `config/skills/litreview/SKILL.md` | Literature review methodology |
| `config/skills/dossier/SKILL.md` | Long-running profile accumulation |
| `config/skills/pulse/SKILL.md` | Topic monitoring scheduler template |
| `config/skills/grill-me/SKILL.md` | Adversarial questioning methodology |
| `config/skills/code-tour/SKILL.md` | Codebase walkthrough procedure |
| `config/skills/competitive-teardown/SKILL.md` | Multi-dimension competitive analysis |
| `config/skills/librarian-backlog-mining/SKILL.md` | Backlog mining procedure |
| `config/skills/librarian-reflection-surfacing/SKILL.md` | Reflection surfacing procedure |
| `config/skills/librarian-tag-hygiene/SKILL.md` | Tag normalization procedure |

### Modified files

| File | Change |
|---|---|
| `internal/agent/intent.go` | Add `IntentWrite`, `IntentArchitect`, `IntentSkeptic`, `IntentLibrarian` |
| `internal/agent/dispatcher.go` | Route new intents to new agents |
| `config/agents/dispatcher/AGENT.md` | Update routing table |
| `config/agents/chat/AGENT.md` | Update delegation routing table |
| `config/agents/analyst/AGENT.md` | Sharpen synthesis/gather boundary, add skill references |
| `config/agents/researcher/AGENT.md` | Add `litreview`/`dossier`/`code-tour` skills |
| `docs/concepts/multi-agent.md` | Document new agents, intents, skills |
| `docs/workflows/agent-orchestration.md` | Update routing examples |
| `CLAUDE.md` | Update agent roster table |
| `cmd/meept/` (wherever agent list is rendered) | Include new agents in `meept status`, TUI agent picker |

## Testing Strategy

1. **AGENT.md loading tests**: verify all 4 new agents load with correct metadata.
2. **Intent routing tests**: each new intent routes to the correct agent.
3. **Skill discovery tests**: verify new skills are discoverable and assignable to agents.
4. **Dispatcher routing test**: routing table in dispatcher AGENT.md matches actual agent roster (no phantoms).
5. **Persona regression tests**: existing agent behaviors unchanged after tightening edits.

---

# Implementation Order

1. **Plan 1 first** (substrate). Estimated ~10 new files + ~10 edits.
2. **Plan 2 second** (agents that use the substrate). Estimated ~13 new files + ~10 edits.

Within Plan 1, the order matters:

1. Constants (types, edges) — zero-risk additive change
2. Epistemic helpers (`epistemic.go`) — pure additions
3. Configuration schema + defaults
4. Path A tools (`retain_typed.go`)
5. Confirmation protocol (`confirmation.go`)
6. Destructive tools (`epistemic_actions.go`) — depends on (4) and (5)
7. Epistemic detection (`epistemic_detection.go`)
8. Ambient extraction (`epistemic_ambient.go` + `epistemic_hook.go`) — depends on (7)
9. Deepened `ReflectTool.Execute`
10. UI wiring (TUI modal, Flutter dialog)
11. RPC + HTTP wiring
12. Tests throughout

Within Plan 2, agents can be added in any order. Recommended: librarian first (depends on Plan 1), then skeptic (also benefits from Plan 1), then writer and architect (independent).

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| EpistemicDetector false positives pollute the KG | Confidence threshold (0.7 default); low-confidence candidates go to `potential_contradicts` queue (0.4 threshold); user must confirm all destructive actions |
| Ambient extraction cost | Opt-in; cheap classifier model; rate-limited (`MaxPerTurn`); cost logged |
| Roster bloat confuses dispatcher | 18 agents total after Plan 2 (was 14). Dispatcher routing table explicitly updated; intent classification remains the bottleneck, not roster size |
| Tag taxonomy rigidity | User-extensible via `~/.meept/epistemic_tags.json5`; non-standard tags flagged but not rejected |
| Flutter dialog drift from TUI modal | Shared response contract; cross-platform test in CI |
| Migration risk | Zero migrations required — all new constants, fields are metadata-only |

## Backward Compatibility

- Existing `MemoryTypeEpisodic`/`Task`/`Personality` memories continue to work unchanged.
- Existing `EdgeType` enum values are unchanged; new values are additive.
- Existing tools (`retain`, `recall`, `reflect`) continue to work; `reflect` returns richer output but old consumers ignore extra fields.
- Existing agents unchanged except for tightened personas (additive edits to AGENT.md bodies).
- Configuration: new `Epistemic` sub-config defaults to ambient-extraction-off, so users upgrading see no behavior change unless they opt in.

## Documentation Updates

- `docs/configuration/epistemic-memory.md` — new page documenting all config fields
- `docs/concepts/multi-agent.md` — new agents table
- `docs/concepts/memory.md` (or equivalent) — epistemic memory types and edges
- `docs/workflows/agent-orchestration.md` — updated routing examples
- `docs/reference/generated/` — regenerate via `make docs-generate`
- `CLAUDE.md` — update agent roster table, memory architecture section
- `docs/features.md` — add epistemic memory and new agents as features
