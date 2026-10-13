# Tree 03 — Human Governance Surfaces (orchestrator)

Parent: `../master.md`. Conventions: `../../SHARED-CONVENTIONS.md` (§2.4
verdict surface, §5 waves).

**Goal:** Port TAM's human-governance idea in meept's idiom: a review queue
over `status=auto` claims with promote/reject verdicts reachable by CLI and
RPC, every verdict audited. Meept already has the claim lifecycle
(`epistemic.go`: auto/promote/reject + calibration logging); what is missing
is the OPERATOR SURFACE — today review happens only through the librarian
agent intent, with no direct human tooling.

**Architecture:** New subpackage `internal/memory/review` (queue assembly:
filter/sort/paginate auto claims needing attention). New RPC methods
`memory.review.list` / `memory.review.verdict` (existing RPC handler
conventions). New CLI verbs under `meept memory` (`review`, `verdict`).
Audit: one `internal/auditlog` entry per verdict. Offline verdict mode per
OPEN-QUESTIONS Q5 rec (direct store write when daemon is down; document the
list-requires-daemon asymmetry).

Out of scope (Q6 rec): TUI/GUI parity view — deliberately deferred; the
deviation is documented in `docs/workflows/memory.md`, not silently dropped.

## Interface contracts

- RPC `memory.review.list` request:
  `{status?: "auto", min_confidence?: float, max_confidence?: float, older_than?: duration, limit?: int (default 50, cap 200), offset?: int}`
  → response `{items: [{claim_id, content, type, confidence, created_at, category}], total: int}`.
- RPC `memory.review.verdict` request:
  `{claim_id: string, verdict: "promote"|"reject", reason?: string (≤512B)}`
  → response `{ok: true}`. Errors: unknown claim (404-shape), already
  reviewed (409-shape).
- Verdict semantics map to existing `Manager.PromoteClaim` and reject path;
  both already write `claim_verdicts.jsonl` — this tree adds the auditlog
  entry alongside, not instead.
- Review scope = claims with `status=auto` only (confirmed/promoted/rejected
  are final states).

## Child Index (inline leaves)

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 review-cli-rpc | 1 | review pkg + RPC methods + CLI verbs | 70K |
| L2 audit-wiring | 3 | auditlog entries, offline verdict mode, docs, e2e | 60K |

## Dispatch Protocol

Standard (root master). L1 brief inlines both RPC request/response shapes
verbatim (they are the frozen contract — the GUI will consume them later).
L1 and Tree 04-L1 are the only two leaves allowed to touch `internal/rpc`
registration in wave 1 — they touch different handler files; if the same
registration file is shared, the orchestrator serializes L1 after Tree 04-L1.

## Review Checklist

Root checklist plus:
- [ ] Reject path excludes rejected claims from all queries (existing
      invariant, `epistemic.go`) — verified by test
- [ ] Reason length enforced ≤512B (match `memory_vote` convention)
- [ ] Audit entry written BEFORE the HTTP/RPC response returns (verdict is
      durable-then-acknowledged)
- [ ] Offline verdict path writes through the same store code, not a
      parallel SQL string
- [ ] Every verdict still lands in `claim_verdicts.jsonl` (calibration
      intact)

## Coding Conventions

Per `../../SHARED-CONVENTIONS.md` §3: Go stdlib-first, sentinel errors with
`%w` wrap, `pkg/id.Generate()` IDs, JSON5 config opt-in defaults, no mutex
across I/O, lowercase UI text, no `os.Getwd()`, GPL/AGPL dep license stop.

## Integration Test Plan

L2 e2e suite `memory-review-governance`: seed 5 auto claims (mixed
confidence) → `memory.review.list` pagination + filters → verdict promote on
one, reject on another → assert status transitions, calibration log rows,
auditlog chain verifies (`meept` auditlog verify path), rejected claim absent
from canonical-claim selection.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| L1 review-cli-rpc | PENDING | |
| L2 audit-wiring | PENDING | |

## Leaf definitions

### Leaf L1 — review-cli-rpc (Wave 1)

**Scope:**
- `internal/memory/review/queue.go` — `Queue.List(filter) ([]Item, total int, err error)`
  over `Manager.ListAutoClaims` (existing, `epistemic.go:359`) with
  confidence-band + age filters and stable ordering (created_at ASC — oldest
  first, review-frontier semantics).
- RPC: two methods per Interface Contracts above, in the existing memory
  RPC handler area (follow `rpc-package-patterns` conventions — handler map
  registration, typed request/response, error mapping).
- CLI `cmd/meept/memory.go`: `meept memory review [--status=auto]
  [--min-confidence=F] [--limit=N]` (table output: id, conf, category, age,
  content preview) and `meept memory verdict <claim-id> promote|reject
  [--reason=S]`. RPC-first; offline fallback lands in L2 — L1 returns a
  clear "daemon unreachable" error.
- Tests: queue filter/pagination unit tests (new package bootstrap tier);
  CLI arg parsing tests.

**Verify:** `go build ./... && go test ./internal/memory/review/ ./cmd/meept/ -run 'Review|Verdict' -count=1`.

**Do NOT commit.**

### Leaf L2 — audit-wiring (Wave 3)

**Scope:**
- Auditlog: append one canonical-JSON entry per verdict
  (`{action: "memory.claim.verdict", claim_id, verdict, reason_hash}` —
  hash the reason, never store raw text in the chain; raw reason stays in
  calibration log).
- Offline verdict: `meept memory verdict` falls back to direct store access
  (open the claim store path from config; same `Manager` code path, not raw
  SQL) when RPC dial fails; print which mode was used.
- Docs: `docs/workflows/memory.md` — "Human Review Queue" section: filters,
  verdict semantics, offline asymmetry, audit behavior, TUI/GUI deferral note.
- e2e suite per Integration Test Plan incl. auditlog chain verification.

**Verify:** `make e2e-fast-area AREA=memory-review-governance && go vet ./internal/memory/... ./cmd/meept/...`.

**Do NOT commit.**
