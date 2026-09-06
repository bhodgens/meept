# Open Questions — Claim Temporal Validity

Deviations and genuine forks discovered while authoring the tree. Each entry
names the question, the author's recommendation, and the impact of both
outcomes. The orchestrator resolves these before or during leaf dispatch;
unresolved ones must not block leaf 01 (none of Q1-Q4 change the Claim struct
or metadata keys).

## Q1 — In-place stamping vs StoreVersioned on supersede (deviation from pinned design)

- **Q:** The pinned design said to write the supersede stamp "via
  StoreVersioned with Rev = old Rev + 1". `Manager.StoreVersioned`
  (internal/memory/manager.go:1367-1387) sets `newMem.ID = ""` and mints a
  NEW row ID for the versioned copy. Writing the successor claim through it
  would leave the `EdgeTypeSuperseded` edge pointing at a row that is no
  longer the lineage head; writing the old (tombstoned) claim through it
  would give the tombstone a fresh identity. Should supersede stamp rev and
  valid_to in place (UPDATE metadata_json on the same row) instead?
- **Rec:** Yes — in-place stamping via a new unexported
  `stampMetadataInPlace` helper (single UPDATE through the same
  `m.episodic.store.GetDB()` access pattern as `markVersionNonCurrent`,
  manager.go:1393). Version history is not lost: `version`/`parent_id`/
  `is_current` columns already track lineage, and the supersede event is
  additionally recorded by the existing `EdgeTypeSuperseded` edge plus new
  `superseded_at` metadata. Implemented as pinned in master.md contract C5
  and leaf 01 Task 4.
- **Impact:** In-place (recommended): same IDs everywhere, no edge orphans,
  no consumer of supersede output breaks. StoreVersioned (as originally
  pinned): successor gets a new ID — every existing evidence edge, the
  superseded edge target, and CLI supersede output referencing old IDs
  become stale; MarkSuperseded's evidence-redirect step would need
  re-verification and its return contract (edge count, audit ID) would need
  redefinition. Substantial rework of leaf 01 and regression risk in
  epistemic flows.

## Q2 — Hard-exclude vs down-weight for expired claims

- **Q:** At trust-weight/detection time, should an expired claim be
  (a) hard-excluded from candidate sets and canonical selection, or
  (b) down-weighted to near-zero but still present?
- **Rec:** Hard-exclude, with an explicit `reason: "expired"` debug log and
  the ListExpiredClaims/list_expired_claims surfaces so the exclusion is
  explainable. Matches the article's model (superseded claims leave the
  working set), matches the existing precedent that rejected claims are hard-
  excluded (epistemic_detection.go:122-126), and avoids resurrecting stale
  claims as weak evidence. Implemented as pinned in leaf 02.
- **Impact:** Hard-exclude (recommended): cleaner candidate sets, detector
  saves classifier calls; risk is silent information loss, mitigated by the
  listing surfaces. Down-weight: claims remain greppable in results but
  pollute detection candidates and canonical fallback, and every consumer
  must re-interpret a new weight tier — more code, more ambiguity, no
  user ask for it.

## Q3 — Should expired claims auto-generate Questions for re-verification?

- **Q:** When a claim expires (or at supersede time), should meept
  automatically create a Question memory ("re-verify X?") so the ambient
  review loop re-examines it?
- **Rec:** Later. It couples the expiry moment to a write amplification
  decision (how many Questions per claim? dedup across repeated listings?)
  that deserves its own design pass, and ListExpiredClaims +
  `meept memory expired` already give the review loop an input to act on.
  Defer to a follow-up plan if the review-queue surface wants it.
- **Impact:** Now: expiry becomes an event with side effects; ambient
  extractor needs a new trigger and dedup logic; leaf 02 scope grows past 3
  files. Later (recommended): zero cost now; a small follow-up leaf can add
  `maybeRaiseReverificationQuestion` to the ListExpiredClaims consumers
  without touching the schema or enforcement path.

## Q4 — Surfaces deliberately left for follow-up

- **Q:** The RPC layer (`internal/rpc/epistemic.go` `retainClaimParams`,
  `handleRetainClaim`) does not carry `valid_from`/`valid_to`/`observed_at`
  yet, and `Manager.ListAutoClaims` intentionally does not filter expired
  claims. Wire RPC params now?
- **Rec:** Follow-up leaf (one file: internal/rpc/epistemic.go, mirroring
  `review_at *time.Time` JSON handling already present in
  `retainDecisionParams`). CLI `memory expired` in leaf 03 rides whatever
  invocation pattern the sibling subcommands use, so the CLI works without
  the RPC change; the tool surface (agents' primary path) is fully wired in
  leaf 03. ListAutoClaims stays unfiltered deliberately: it is a triage
  surface and an expired auto-claim still deserves triage visibility.
- **Impact:** Follow-up (recommended): keeps leaf 03 at 3 files; external
  RPC callers can't set validity windows until then (acceptable — no
  documented external consumer does today). Now: leaf 03 exceeds its file
  bound and mixes transport-layer concerns into a surface+docs leaf.

## Authoring-time verifications (resolved, recorded for reviewers)

- **Metadata persistence is schema-free:** `metadata_json TEXT NOT NULL
  DEFAULT '{}'` (internal/memory/episodic.go:24); `Memory.MetadataJSON`
  (types.go:136) serializes the whole map; only `parent_id`/`version`/
  `is_current` are extracted into SQL columns (episodic.go:147-160). New
  keys need no migration. (Closed — master contract C2.)
- **JSON round-trip numeric type:** numbers in metadata come back as
  `float64`; `claimRev` handles this (master C2). (Closed.)
- **Claim-creation tool surface:** it is
  `internal/tools/builtin/retain_typed.go` (`retain_claim`), NOT
  `internal/tools/builtin/memory.go` (episodic/task only, :77-80). Task
  context said memory.go; the tree targets the verified file. (Closed —
  leaf 03.)
- **Generated docs pipeline:** `docs/reference/generated/memory.md` is
  gomarkdoc output via `make docs-generate` → `mage -d magefiles docsGenerate`
  (package map magefiles/docs.go:51). Leaf 03 regenerates, never hand-edits.
  (Closed.)

## Do NOT commit independently

This file is reference documentation inside the claim-temporal-validity plan
tree, not a dispatchable implementation leaf. It is committed together with
the tree's master.md as one unit — never as its own commit.

## Self-Verification Checklist

- [ ] This document is reference documentation, not a dispatchable
      implementation leaf — no code changes, no file edits, no test runs
      belong to it.
- [ ] Every open question carries Q / Rec / Impact, and resolved questions
      point at the contract or leaf that closed them.
- [ ] Nothing here contradicts the pinned contracts in master.md.
