# LLM Refusal Fallback

When a provider's safety layer declines a turn, the agent loop can
re-dispatch that same turn to a different model instead of failing the
step. This page describes what triggers the fallback, how to configure
it, and what it deliberately does not do.

Implementation: `internal/agent/loop_refusal.go` (loop branch),
`internal/llm/errors_refusal.go` (typed error and detection).

## What triggers a fallback

Only typed provider signals trigger a fallback. There is no output
inspection of model text:

- OpenAI-compatible: `finish_reason: "content_filter"` (non-streaming
  and streaming), or a typed safeguard body in the provider error.
- Anthropic: `stop_reason: "refusal"` (non-streaming and streaming), or
  a typed safeguard body in the provider error.

All six client sites (both protocols, streaming and non-streaming,
success and error bodies) classify these into `*llm.RefusalError`,
which implements `NonRetryable`: a refusal is never retried against the
same model and never reaches alias failover (`Resolver.RecordAliasFailure`)
or rotation.

Error-body detection is deliberately conservative: only a small set of
explicit safeguard/refusal markers matches. Broad keywords like "policy"
alone never classify a refusal, because a false positive would silently
re-route a legitimate answer to a different model.

## Configuration

Two levels, with fixed precedence:

1. **Per-agent** - `refusal_model` in the agent's `AGENT.md` frontmatter
   (alias name or `"provider/model"` ref). See
   [Agent Configuration](../configuration/AGENTS.md).
2. **Global** - the `refusal_model` slot in `models.json5`. Used by any
   agent whose frontmatter does not set its own. See
   [LLM Configuration](../configuration/llm.md).

An agent with neither set has refusal fallback disabled: the refusal
surfaces to the caller as a normal error.

Precedence: per-agent spec > global slot > off.

## One-hop rule

The loop re-dispatches the refused turn to the fallback model exactly
once. If the fallback model also refuses, the original refusal error
surfaces - there is no second hop and no cascade through the alias
chain. Refusals never consume alias failover attempts, never mark a
model unhealthy, and never touch `RecordAliasFailure`.

## Observability

- **Bus event:** `agent.model_escalated` with `"reason":
  "refusal_fallback"`. Classified as `agent_progress` on the WebSocket
  bridge (same bucket as quota events).
- **Reply disclosure:** a turn answered via the fallback ends with
  `\n\n[answered by <fallback model> after refusal]`, appended by code
  to the chat reply and task/step results. Turns without a refusal are
  byte-identical to before.
- **Ledger:** the `llm_calls` entry names the fallback model that
  actually served the request.

## Non-goal: no content sniffing

Meept does not read the model's output text to decide whether it "looks
like" a refusal. Sniffing produces false positives - a model that
legitimately discusses a policy topic, quotes a refusal, or explains a
safety constraint would be misrouted to a different model or dropped.
Only the provider's own typed signals (finish/stop reason, typed
safeguard error bodies) count. If a provider signals nothing, the
output is taken at face value.

## See also

- [Quota Reset Resilience](quota-resilience.md) - the sibling policy for
  quota errors (also never an alias failure).
- [Verification escalation](agent-orchestration.md) - the separate
  `escalation_model` used when adversarial verification exhausts its fix
  loops (`internal/agent/verification_escalation.go`). Unrelated to
  refusals.
- [Routing Decisions](routing-decisions.md) - the model-resolution
  decision log.
