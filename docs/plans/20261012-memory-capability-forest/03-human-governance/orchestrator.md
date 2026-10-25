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

**RE-SCOPED at pre-dispatch drift audit (2026-10-12):** original L1's RPC
methods and CLI verbs ALREADY EXIST (`internal/rpc/epistemic.go:35-48` —
`memory.reviewQueue`, `memory.promoteClaim`, `memory.rejectClaim`,
`memory.listAutoClaims`; `cmd/meept/memory.go` — `review`, `promote ID`,
`reject ID`). Old L1 is SUPERSEDED. Remaining work consolidated:

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 review-filters-audit | 1 | queue filters/pagination + verdict --reason + auditlog entries | 70K |
| L2 tui-review-view | 3 | TUI review table + docs + e2e | 70K |

Old L2 (audit-wiring) folded into L1; old L3 (TUI) is the new L2 with docs+e2e.
Daemon-only verdicts (Q5b) is ALREADY TRUE — CLI is RPC-only today.

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

### Leaf L1 — review-filters-audit (Wave 1; re-scoped, supersedes old L1+L2)

**Scope (ONLY the missing parts — the RPC methods and CLI verbs exist):**
- Extend `memory.reviewQueue` handler (`internal/rpc/epistemic.go:48`):
  params `{min_confidence?, max_confidence?, older_than?, limit? (default
  50, cap 200), offset?}` → response gains `total`. Backing query:
  extend `Manager.ListAutoClaims` with an options struct (keep the
  existing signature working; add `ListAutoClaimsFiltered`).
- Verdict reason: `memory.promoteClaim`/`memory.rejectClaim` accept
  optional `reason` (≤512B, same cap as `memory_vote`); stored as claim
  metadata `verdict_reason`.
- Auditlog (the genuinely missing piece — nothing in internal/memory
  imports internal/auditlog today): one canonical-JSON entry per verdict,
  `{action: "memory.claim.verdict", claim_id, verdict, reason_hash}` —
  hash the reason (sha256 first 16 hex), never store raw text in the
  chain. Raw reason stays in the calibration log (existing
  `logCalibration` path, extended to carry the reason). Wire the auditlog
  writer via a narrow interface injected at daemon startup (memory pkg
  must not import employee-style heavy wiring; follow
  `internal/employee/wiring.go` injection pattern).
- CLI: `--reason=S` flag on `meept memory promote/reject`.
- Tests: filter/pagination table tests; reason-cap rejection; audit-entry
  written-before-ack ordering (fake audit sink).

**Verify:** `go build ./internal/memory/... ./internal/rpc/... ./cmd/meept/ && go test ./internal/memory/ ./internal/rpc/ -run 'Review|Claim|Verdict|AutoClaims' -count=1`.

**Do NOT commit.**

### Leaf L2 — tui-review-view (Wave 3, after L1)

**Scope (resolved Q6b — parity satisfied in-tree):**
- `internal/tui` review view: a table of pending auto claims
  (columns: id, confidence, category, age, content preview) sized via
  `internal/tui/tableutil.Size` and populated via `tableutil.SetRows`
  (both required — bubbles/table renders nothing at width 0).
- Keys: `p` promote selected, `r` reject selected, `enter` detail,
  `esc` close. All UI text lowercase. Commands go through the same
  `memory.review.*` RPC methods — no second write path.
- Command palette entry `memory review`; status-bar count of pending
  claims when the review RPC is cheap (skip if it adds a per-turn call).
- GUI parity: a Flutter panel is OUT OF SCOPE here; the deviation is
  documented in the L2 docs section (GUI follows in a later tree).
- Docs + e2e (folded from old L2): `docs/workflows/memory.md` "Human
  Review Queue" section (filters, reason, audit behavior, TUI keys, GUI
  deferral); e2e assertions added to the suite covering reason+audit via
  RPC round-trip.
- Tests: TUI component tests for table sizing + key handling with a fake
  review client.

**Verify:** `go build ./... && go test ./internal/tui/ -run Review -count=1 && go vet ./internal/memory/... ./cmd/meept/...`.

**Do NOT commit.**

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

N/A — no leaf documents found alongside this orchestrator. If this tree
gains leaves, build the coverage matrix before dispatching them.
