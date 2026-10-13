# B03 — Entailment Verification + Provenance Cascade-Invalidation (P10)

**Proposal:** P10 (branch 09). **Depends on:** B02. **Files (3):**
- Create: `internal/memory/bridges_verify.go` (entailment gate)
- Modify: `internal/memory/staleness.go` (invalidation hook — locate the
  source-memory update/delete/expiry seams with search_files)
- Test: `internal/memory/bridges_verify_test.go`

**Problem:** The paper's limitation #1: hallucinated bridging facts become
permanently retrievable units. Meept generates with a local 8B model — a step
down from the paper's GPT-4o-mini — so verification is mandatory. Second risk:
precomputed bridges go stale exactly when their source facts change; meept's
distill pipeline already collects evidence IDs, so cascade-invalidation is
half-built.

**Contracts (branch #3, #4):**
- Entailment verdict: grammar-constrained JSON `{"supported": bool, "reason":
  string}` from the same local model class. Unsupported → DELETE the bridge
  before it is ever retrievable. `verified=true` set on survivors.
- `memory.bridges.verify` default **true** (ship-on quality gate).
- Invalidation: on update/delete/expiry of a source memory, delete every bridge
  whose `evidence_ids` intersect the mutated memory ID. Must fire on ALL THREE
  paths. Uses B01's `ListBridgesByEvidence`.

### Tasks

**Task 1 — failing tests (bridges_verify_test.go):** fake LLMClient:
(i) `TestVerifyDropsUnsupported`: gen says supported=false → bridge deleted,
absent from search. (ii) `TestVerifySurvivorsMarkedVerified`: supported=true →
bridge retrievable, metadata verified="true". (iii) `TestVerifyDisabledKeepsUnverified`:
verify=false → bridges persist with verified="false" (still retrievable — the
flag is a quality gate, not a recall gate).
Run: `go test -p 2 ./internal/memory/ -run TestVerify` — FAIL.

**Task 2 — implement bridges_verify.go:** `VerifyBridges(ctx, store, gen, cfg)
(kept, dropped int, err error)`; batch (one call per bridge v1; batching is a
follow-up). Wire into the B02 generation flow immediately after generation (same
pass, before persistence completes — or delete-after-persist if the flow makes
that cleaner; either is fine, tests decide).

**Task 3 — failing tests for invalidation:** `TestSourceMemoryDeleteInvalidatesBridge`,
`TestSourceMemoryUpdateInvalidatesBridge`, `TestSourceMemoryExpiryInvalidatesBridge`
— locate the three seams in staleness.go / manager.go; each must cascade.
Run: FAIL first.

**Task 4 — implement invalidation hook:** single helper
`invalidateBridgesForMemory(ctx, store, memoryID)` called from all three seams.
Idempotent; logs removals at debug.

**Task 5 — full package + report.** `go test -p 2 ./internal/memory/` green.
Do NOT commit.

## Self-Verification Checklist
- [ ] Unsupported bridges never retrievable at any flag setting (verify=true deletes; recall gating is B04's k_b, orthogonal)
- [ ] All three mutation paths tested, one helper, no copy-paste
- [ ] Grammar-constrained verdict (no free-text parsing)

## Review Checklist (for orchestrator)
- [ ] verify=false is possible WITHOUT removing the gate code path
- [ ] Invalidation cannot deadlock with Consolidator writes (lock ordering noted)
