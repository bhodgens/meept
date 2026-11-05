# Residual Follow-ups - Final Small Campaign

## Goal

Close the 4 S-sized follow-ups carried out of the 20261031 forest + 20261103 remediation campaigns.

## Traceability (Req -> leaf)

| Req | Source (verbatim) | Leaf | Verified by |
|---|---|---|---|
| R1 | "llm.call span's conversation_id attribute is now always '' - needs TraceID->conversation state" | 01 | conversation_id attribute populated on traced turns |
| R2 | "buildConversationRestoreFn rebuilds ChatMessages role+content only, dropping ToolCalls" | 02 | restore preserves tool calls |
| R3 | "two-stage verifier recommendation is NOT implemented" | 03 | second-stage verification gates near-threshold hits |
| R4 | "adding the remaining call sites is a 2-line follow-up each" (streaming + third variant semantic cache sites) | 04 | streaming turn serves from semantic cache |

## Interface Contracts

- C1 (leaf 01): conversation state lives in the daemon wiring layer (bounded map on TraceID, same cap discipline as the turnSpanID map); no loop.go changes; attribute populated only when the conversation is known.
- C2 (leaf 02): restore path = GetToolCallsForMessages joined by message_id; ToolCalls re-attached to the assistant ChatMessage before conversation rehydration; schema unchanged (columns exist).
- C3 (leaf 03): two-stage verify = embedder-verify pass on candidates in (0.95-band minus exact) hits only; verifier failure degrades to miss (never serves wrong content, never parks); reuses the existing Embedder seam; no new config keys (behavior piggybacks llm.cache.semantic.enabled).
- C4 (leaf 04): the 2 remaining exact-miss sites (ChatWithDeltaCallback streaming + third variant) call the SAME shared semanticLookupBehindMiss/semanticStoreAfterSuccess seam helpers - zero new logic.

## Dispatch Protocol

Single agent, one wave, 4 leaves sequential (small; 01+04 share client.go
wiring neighborhood - serialize, not parallel). Order: 04 (streaming
sites) -> 03 (two-stage verifier, builds on cache tests) -> 01
(conversation attr, daemon side) -> 02 (restore, agent side). Final
integration gates per master.

## Review Checklist

Per-leaf in-session review by the orchestrator: wiring-failure/RED proof
witnessed, scoped gates green, staged-set audit, sibling-sweep attribution.

## Coding Conventions

Inherited from repo AGENTS.md + prior campaigns: ASCII only, no stubs,
wiring-failure proofs witnessed, -p 2 sweeps, best-effort semantics on
persistence/restore paths, bounded nil-safe maps in wiring layers.

## Invariants

- Streaming site: tool-call responses never served from cache (both lookup and store) - same hard rule as the non-streaming sites.
- Cache-hit turns keep the TTFT sentinel semantics (unarmed clock = -1).
- Trace->conversation map: bounded, panic-recovered, nil-safe - same posture as the turnSpanID map.
- Restore path: read-only addition to the loop restore fn; no schema changes (columns + reader already exist).

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 01-conversation-attr | PENDING | | daemon wiring layer only |
| 02-restore-toolcalls | PENDING | | loop restore fn + tests |
| 03-two-stage-verifier | PENDING | | semantic_cache.go + tests |
| 04-streaming-cache-sites | PENDING | | client.go 2 hunks + tests |

## Integration Test Plan

1. go build ./... green.
2. go test -p 2 on touched packages green (-race on semantic cache).
3. llm-hop-tracing e2e suite still green (leaf 01 touches its wiring).
4. boundary-hardening suite still green (leaf 02 touches restore path).
5. lint-ci gosec gate green.
