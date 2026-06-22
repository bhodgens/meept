# Epistemic Memory Configuration

The epistemic memory platform lets Meept track claims, decisions, and
predictions as first-class memory entities. It detects relationships
between them (contradicts, superseded, evidence_for/against) via LLM
classification and can extract claims ambiently from conversation turns.

## Configuration Location

Epistemic settings live under `memory.epistemic` in the main config file
(`~/.meept/meept.json5`):

```json5
{
  memory: {
    epistemic: {
      ambient_extraction: {
        enabled: false,
        confidence_threshold: 0.75,
        max_per_turn: 3,
        exclude_intents: ["chat"],
        exclude_categories: ["joke"],
        context_window: 5,
      },
      auto_trust_weight: 0.5,
      detection_threshold: 0.7,
      review_prompt_frequency: "weekly",
      max_pending_reviews: 20,
    },
  },
}
```

## Field Reference

### EpistemicConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ambient_extraction` | object | see below | Settings for ambient claim extraction |
| `auto_trust_weight` | float | `0.5` | Trust weight (0.0-1.0) applied to ambient-extracted claims. Zero or invalid values fall back to `DefaultAutoClaimTrustWeight` (0.5). |
| `detection_threshold` | float | `0.7` | Minimum LLM confidence for an epistemic edge to be persisted. Below this, only `potential_contradicts` edges in range [0.4, 0.7) are written with low weight (0.2). |
| `review_prompt_frequency` | string | `""` | How often to surface pending reviews (e.g., `"weekly"`, `"daily"`). Empty = no prompt. |
| `max_pending_reviews` | int | `0` | Maximum items to surface in a single review prompt. Zero = no cap. |

### AmbientExtractionConfig

Controls automatic claim extraction after each agent turn.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. When `false`, no ambient extraction occurs. |
| `confidence_threshold` | float | `0.0` | Minimum LLM confidence for an extracted claim to be stored. |
| `max_per_turn` | int | `0` | Maximum claims extracted per conversation turn. Zero = no limit (not recommended). |
| `exclude_intents` | []string | `[]` | Intent types to skip during extraction (e.g., `["chat"]`). |
| `exclude_categories` | []string | `[]` | Memory categories to skip (e.g., `["joke"]`). |
| `context_window` | int | `0` | Number of preceding turns to include as context for the extractor. |

## Claim Lifecycle

Claims progress through the following statuses:

```
confirmed (user-asserted, weight 1.0)
    |
    v
auto (ambient-extracted, weight = auto_trust_weight)
   / \
  v   v
promoted  rejected
(1.0)     (0.0, excluded from queries)
```

- **confirmed**: User explicitly stored the claim via `retain_claim` tool or RPC.
- **auto**: The ambient extractor created the claim after a conversation turn.
- **promoted**: User upgraded an auto-claim to full trust via `meept memory promote`.
- **rejected**: User discarded an auto-claim via `meept memory reject`. Rejected
  claims are excluded from search results and cannot be relationship targets.

## Epistemic Edges

The detector classifies relationships between memories and writes edges:

| Edge Type | Description |
|-----------|-------------|
| `contradicts` | New memory asserts the opposite of the target |
| `superseded` | New memory replaces the target |
| `evidence_for` | New memory supports the target |
| `evidence_against` | New memory undermines the target |
| `derives_from` | New memory is derived from the target |
| `supports` | New memory reinforces the target (weaker than evidence_for) |
| `potential_contradicts` | Low-confidence contradicts (in [0.4, 0.7) range), weight 0.2 |

## CLI Commands

```bash
# View pending review queue (auto-claims, decisions, predictions)
meept memory review

# Mark a claim as superseded by a newer one
meept memory supersede OLD_ID NEW_ID
meept memory supersede OLD_ID NEW_ID --confirm  # skip confirmation prompt

# Promote an auto-claim to confirmed status
meept memory promote ID

# Reject an auto-claim
meept memory reject ID
```

## HTTP API

All endpoints are under `/api/v1/memory/` and require the HTTP transport
to be enabled with `rpcCall` wired:

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/memory/claims` | Store a claim |
| POST | `/api/v1/memory/claims/{id}/promote` | Promote a claim |
| POST | `/api/v1/memory/claims/{id}/reject` | Reject a claim |
| POST | `/api/v1/memory/decisions` | Store a decision |
| POST | `/api/v1/memory/decisions/{id}/review` | Record a decision review |
| POST | `/api/v1/memory/predictions` | Store a prediction |
| POST | `/api/v1/memory/predictions/{id}/resolve` | Resolve a prediction |
| POST | `/api/v1/memory/supersede` | Mark superseded |
| GET | `/api/v1/memory/canonical?topic=` | Find canonical memory for topic |
| GET | `/api/v1/memory/review-queue` | Get combined review queue |
| GET | `/api/v1/memory/auto-claims` | List auto-extracted claims |

## RPC Methods

The following RPC methods are registered by `EpistemicHandler`:

| Method | Parameters | Returns |
|--------|------------|---------|
| `memory.retainClaim` | `text`, `premises?`, `source?`, `confidence?`, `tags?` | `{id}` |
| `memory.retainDecision` | `call`, `alternatives?`, `expected_outcome?`, `review_at?`, `premises?` | `{id}` |
| `memory.retainPrediction` | `forecast`, `horizon`, `related_decision?` | `{id}` |
| `memory.markSuperseded` | `old_id`, `new_id` | `{redirected_edges, audit_id}` |
| `memory.markResolved` | `prediction_id`, `outcome` | `{id}` |
| `memory.recordReview` | `decision_id`, `actual_outcome` | `{overlap_score, audit_id}` |
| `memory.promoteClaim` | `id` | `{status: "promoted"}` |
| `memory.rejectClaim` | `id` | `{status: "rejected"}` |
| `memory.listAutoClaims` | `since_hours?`, `limit?` | `{claims}` |
| `memory.listPendingReviews` | none | `{decisions, predictions}` |
| `memory.findCanonical` | `topic` | `{found, memory?}` |
| `memory.reviewQueue` | `since_hours?`, `limit?` | `{auto_claims, pending_decisions, pending_predictions}` |

## See Also

- [Memory Concepts](../concepts/memory.md)
- [LLM Configuration](llm.md)
- [HTTP API Reference](../reference/http-api.md)
