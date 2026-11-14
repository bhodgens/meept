# Leaf 01 — LLM Warm Seam

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD in the
> isolation worktree (path in your dispatch context). Do NOT commit. Do NOT commit. Do NOT run `git commit`
> or `git add`. Make the 00T gate tests green without editing them (report
> any contract contradiction to the orchestrator instead).

## Meta

- **Parent:** orchestrator.md (phase2-context-warm)
- **Depends on:** 00T TESTS_VALIDATED
- **Estimated context:** ~55K
- **Parallel group:** Wave A (with 02; file-disjoint, seam-pinned)
- **Requirements:** R8, C4

## Goal

`internal/llm`: the Warm request path per root Contract C4 — signature.

## Scope

`internal/llm`: the Warm request path per root Contract C4 — signature
verbatim:
```go
func (c *Client) Warm(ctx context.Context, alias string, messages []Message, sessionScope string)
```
(adjust receiver/arg names only if the frozen signature already differs —
then REPORT, don't drift).

## Tasks

1. `internal/llm/warm.go` (new file): build the turn payload via the SAME
   rendering path the real turn uses (prefix identity is the whole point —
   reuse the message serializer, do not rebuild it), force
   `max_tokens=1`, non-streaming, attach the session-affinity header via
   the existing scope plumbing (client.go:725 `prompt_cache_key` lane),
   issue on a dedicated in-flight semaphore (default cap 1; a warm never
   queues behind another warm — replace-discard instead, cancel previous).
2. Error posture: any failure (transport, quota `*QuotaResetError`, 4xx,
   timeout) → DEBUG log with alias + reason, return quietly. NEVER: retry,
   park, mark an alias failed, surface a `llm_tier*` degradation, or write
   a `llm_calls` completion row that could look like a turn (check
   metrics/tier rules in internal/llm/AGENTS.md before choosing where to
   count it — a `warm=true` metric dimension is acceptable; a turn-visible
   row is not).
3. Timeout: hard 10 s context deadline independent of the caller ctx? No —
   caller owns cancel; add an internal ceiling anyway (a hung warm must
   not pin the semaphore forever).
4. Anthropic + Codex client classes: out of scope this leaf (voice turns
   ride the OpenAI-compatible local endpoint). Guard: Warm on a
   non-supporting client class = no-op DEBUG (pin with test) so leaf 02
   never needs to know.

## Interface Contract (exposes to siblings)

`Client.Warm` exactly per C4. Leaf 02 calls only this.

## Self-Verification Checklist

- [ ] `go build ./internal/llm/...`
- [ ] `go test -p 2 ./internal/llm/... -count=1` green
- [ ] Payload snapshot test: byte-compare warm request vs the same turn's
      request minus max_tokens/stream (prefix identity proof)
- [ ] Semaphore release on every path incl. panic-safety (no pinned cap)
- [ ] `gofmt -l` empty

## Review Checklist (for the orchestrator)

- [ ] No budget-gate consumption that can starve real turns (R8) — check
      the diff against internal/llm/AGENTS.md quota/sticky rules.
- [ ] Warm cannot emit WS relay events or session messages.
- [ ] Typed-nil/mutex conventions clean; setters_test unaffected.
