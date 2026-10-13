# B02 — Bridge Generation Pass in the Consolidator (P10)

**Proposal:** P10 (branch 09). **Depends on:** B01. **Files (3):**
- Create: `internal/memory/bridges.go` (generation logic)
- Modify: `internal/memory/consolidation.go` (periodic pass hook — locate
  `StartPeriodicConsolidation` and the Consolidator writer discipline)
- Modify: `internal/config/schema.go` (`memory.bridges.*` keys if B01 did not add them)
- Test: `internal/memory/bridges_test.go`

**Problem:** Stage 2 of IndexRAG: group memories by shared entities with
document frequency `2 <= df(e) <= cfg.BridgeDFMax` (default 10), then generate
bridging facts per bridge entity with the local model, grammar-constrained.
Offline-only — this runs in the Consolidator background pass, never on the chat
path.

**Contracts (branch #2):**
- Signature: `GenerateBridges(ctx, store MemoryStore, gen LLMClient, cfg BridgesConfig) (generated int, rejected int, err error)` in bridges.go.
- Per bridge entity: max 5 source memories, max 8 facts each (paper §3.2.2).
- Generation prompt outputs JSON array of `{"text": string, "evidence_ids": [..]}`;
  grammar-constrained via the existing distill grammar wiring pattern
  (see distill_grammar_wiring_test.go + SetDistillSummarizer pluggability).
- Config gate: entire pass no-ops unless `memory.bridges.enabled` (default false).
- Bridges persisted ONLY via B01's `PutBridge` (evidence validation inherited).

### Tasks

**Task 1 — failing tests (bridges_test.go):** with a fake LLMClient returning
canned JSON: (i) `TestGenerateBridgesRespectsDFWindow`: entity with df=1 → no
bridge; df=3 → bridge attempted; df=15 (> default max) → skipped.
(ii) `TestGenerateBridgesDisabledNoOps`: enabled=false → zero calls to gen.
(iii) `TestGenerateBridgesCapsSourcesAndFacts`: >5 candidate memories → only 5
used; >8 facts per memory → only 8 in prompt. (iv) `TestGenerateBridgesMalformedJSONRejected`:
gen returns garbage → rejected count increments, nothing persisted.
Run: `go test -p 2 ./internal/memory/ -run TestGenerateBridges` — FAIL.

**Task 2 — implement bridges.go:** entity extraction from existing memory
metadata/text (reuse distill Stage-1 output if present; otherwise a regex/light
extractor is acceptable for v1 — record which in the report). df computation
over the memory store. Prompt assembly with caps. JSON parse + persistence via
PutBridge. Structured logging: generated/rejected counts.

**Task 3 — consolidate hook:** call GenerateBridges from the Consolidator's
periodic pass, gated on config. Respect the writer discipline (no mutex across
I/O — batch writes outside locks). Locate the consolidation completion seam
with search_files; do not alter existing merge/summarize behavior.

**Task 4 — schema keys:** `memory.bridges.enabled` (false), `.kb` (2),
`.bridge_df_max` (10), `.verify` (true) in schema.go with JSON5 tags (if not
already added by B01/L07-era schema edits — check to avoid duplicate keys).

**Task 5 — full package + report.** `go test -p 2 ./internal/memory/
./internal/config/`. Do NOT commit.

## Self-Verification Checklist
- [ ] enabled=false → zero LLM calls (test proves it)
- [ ] df window + caps enforced and tested
- [ ] All persisted bridges have evidence_ids (PutBridge validates)

## Review Checklist (for orchestrator)
- [ ] Call-graph check: no path from chat/inference loop to GenerateBridges
- [ ] Consolidator existing behavior untouched (merge/summarize tests green)
