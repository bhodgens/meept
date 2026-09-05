# Plan Tree: Claim Temporal Validity + Monotonic Revision

Plan: first-class temporal validity (observed_at, valid_from, valid_to) and a
monotonic per-claim revision counter (`rev`) for meept's epistemic claim
system, with expiry enforcement in the trust/detection path and surfacing on
the tool + CLI layers.

Reference model (external article "Graph and Loop Engineering using Grok Bot"):
knowledge-graph claim records carry minimum fields `id`, `kind/type`,
`source_ids`, `status` (proposed/verified/rejected/superseded), `rev`
(monotonic revision), `confidence`, `observed_at`, and optional
`valid_from`/`valid_to`. meept already has id/status/confidence/source;
this tree adds the temporal + revision fields and makes them matter.

## Architecture Overview

**Current state (verified against source):**

- `internal/memory/epistemic.go:88-95` — `Claim{Text, Premises, Source,
  Confidence, Tags, Status}`. No temporal fields, no revision counter.
- `internal/memory/epistemic.go:167-190` — `StoreClaim` maps the Claim into a
  `Memory` with metadata keys `status`, `confidence`, optional `source`,
  `premises`, `tags`.
- `internal/memory/manager.go:1367-1390` — `StoreVersioned` version storage
  exists (metadata keys `parent_id`, `version`, `is_current` extracted into SQL
  columns by `internal/memory/episodic.go:147-160`), but there is no
  claim-level revision semantics.
- `internal/memory/epistemic.go:458-533` — `MarkSuperseded` writes an
  `EdgeTypeSuperseded` graph edge (`internal/memory/graph.go:38`) and flips
  `is_current=0` on the old row; it does not stamp any temporal validity on the
  old claim nor bump a rev on the new one.
- `internal/memory/epistemic_detection.go:122-126` — the detection candidate
  filter excludes rejected claims; expired claims are not excluded.
- Metadata round-trip is JSON: `internal/memory/types.go:136` (`MetadataJSON`)
  → `internal/memory/episodic.go:24` (`metadata_json TEXT NOT NULL DEFAULT
  '{}'`) → `ParseMetadata` on read. **No schema migration is required** for new
  metadata keys. Caveat: JSON round-trip turns every number into `float64` —
  reading `rev` back MUST use a two-value assertion on `float64` and convert
  (see Interface Contracts).
- Claim-creation surfaces: `internal/tools/builtin/retain_typed.go` (tool
  `retain_claim`, lines 40-122 — NOT `internal/tools/builtin/memory.go`, which
  only handles episodic/task), `internal/rpc/epistemic.go` (RPC handler
  `handleRetainClaim` + registrations ~line 40), and CLI subcommands in
  `cmd/meept/memory.go` (review ~361, supersede ~391, promote ~439, reject
  ~463).
- Generated reference docs: `docs/reference/generated/memory.md` is produced by
  gomarkdoc via `make docs-generate` (→ `mage -d magefiles docsGenerate`,
  package map in `magefiles/docs.go:51`). Never hand-edit; regenerate.
- Workflow docs: `docs/workflows/memory.md` (human-authored; feature mapping
  rule in AGENTS.md: `internal/memory/` → `docs/workflows/memory.md`).

**Target state:**

1. `Claim` gains `ObservedAt time.Time` (zero = store time),
   `ValidFrom *time.Time` / `ValidTo *time.Time` (nil = unbounded),
   `Rev int64` (0 on create). `StoreClaim` persists them as metadata keys.
2. Supersede path stamps, in place (no new version row — see Decision D2):
   `rev = oldRev + 1` on the successor claim and `valid_to = supersede time`
   on the superseded claim, co-located with the existing
   `MarkSuperseded` flow.
3. Enforcement: claims outside their validity window are excluded from the
   detection candidate filter and canonical-claim selection
   (`claimInForce` predicate), and are listable via
   `Manager.ListExpiredClaims(ctx, limit) ([]MemoryResult, error)`.
4. Surface: `retain_claim` tool gains `valid_from` / `valid_to` /
   `observed_at` params; an expired-claims listing reaches the tool + CLI
   layer; `docs/workflows/memory.md` documents the feature; generated
   reference docs are refreshed via `make docs-generate`.
5. Backward compat: existing rows (no new metadata keys) behave exactly as
   today — absent `rev` reads 0, absent windows read unbounded.

## Interface Contracts

These are pinned across all leaves. Any implementation that cannot honor one
of these verbatim stops and records the conflict in the leaf's notes / the
orchestrator (which appends to OPEN-QUESTIONS.md).

### C1 — Extended Claim struct (leaf 01)

`internal/memory/epistemic.go`, replacing the struct at lines 88-95:

```go
// Claim is a structured assertion of belief.
type Claim struct {
	Text       string      // the claim itself
	Premises   []string    // supporting claim IDs or text snippets
	Source     string      // URL, citation, or "user"
	Confidence float64     // 0.0-1.0, user-asserted
	Tags       []string    // controlled-vocabulary tags
	Status     ClaimStatus // lifecycle status

	// ObservedAt is when the claim was observed to be true. Zero means
	// "store time" (readers fall back to the memory's CreatedAt).
	ObservedAt time.Time
	// ValidFrom is the earliest instant the claim is in force. Nil = unbounded.
	ValidFrom *time.Time
	// ValidTo is the latest instant the claim is in force. Nil = unbounded.
	ValidTo *time.Time
	// Rev is the monotonic revision counter. 0 on create; incremented by 1
	// on each supersede of the claim lineage (stored on the successor).
	Rev int64
}
```

### C2 — Claim metadata keys (exact strings)

Stored by `StoreClaim`, read by helpers, in the existing `Memory.Metadata`
`map[string]any`:

| Key           | Value type in Go   | Stored as                     |
|---------------|--------------------|-------------------------------|
| `observed_at` | `time.Time`        | RFC3339 string                |
| `valid_from`  | `*time.Time`       | RFC3339 string (omitted if nil) |
| `valid_to`    | `*time.Time`       | RFC3339 string (omitted if nil) |
| `rev`         | `int64`            | number                        |

Precedent: `review_at` / `horizon` are already stored as RFC3339 strings in
this file (`StoreDecision`, `StorePrediction`). Follow that pattern.

**JSON round-trip pitfall (mandatory):** metadata read back from the store has
been through `json.Unmarshal`, so numeric values are `float64`. Reading `rev`:

```go
func claimRev(meta map[string]any) int64 {
	if v, ok := meta["rev"].(float64); ok {
		return int64(v)
	}
	return 0 // absent or non-numeric = backward-compat zero
}
```

All `map[string]any` reads use the two-value assertion form (AGENTS.md).

### C3 — In-force predicate (leaf 01 defines, leaf 02 consumes)

```go
// claimInForce reports whether a claim memory is inside its validity window
// at the given instant. Absent bounds are unbounded. Malformed timestamps are
// treated as unbounded (fail-open) so a corrupt key cannot silently hide a
// claim from enforcement.
func claimInForce(mem Memory, now time.Time) bool {
	if vf, ok := mem.Metadata["valid_from"].(string); ok && vf != "" {
		if t, err := time.Parse(time.RFC3339, vf); err == nil && t.After(now) {
			return false // not yet in force
		}
	}
	if vt, ok := mem.Metadata["valid_to"].(string); ok && vt != "" {
		if t, err := time.Parse(time.RFC3339, vt); err == nil && t.Before(now) {
			return false // expired
		}
	}
	return true
}
```

Note: `valid_from` is stored by leaf 01 but enforced only insofar as this
predicate is adopted by leaf 02's call sites (detection candidates +
`FindCanonicalFor`). Whether the not-yet-in-force branch should also apply
everywhere is OPEN-QUESTIONS Q3 — the predicate ships complete either way.

### C4 — ListExpiredClaims signature (leaf 02)

```go
// ListExpiredClaims returns non-rejected claims whose valid_to is in the past
// at call time, newest first, up to limit (default 20).
func (m *Manager) ListExpiredClaims(ctx context.Context, limit int) ([]MemoryResult, error)
```

Mirrors the `ListAutoClaims` pattern (`epistemic.go:303`): RLock-initialized
check, broad `Search` with `Type: MemoryTypeClaim` and a `Limit` multiplier,
post-query metadata filtering. Filter: type == claim, status != rejected,
has a parsable `valid_to` that is before now.

### C5 — Supersede semantics (leaf 01 implements, leaf 02/03 rely on)

Inside `MarkSuperseded` (`epistemic.go:458`), after the status guards and
BEFORE the existing `markVersionNonCurrent` + edge write:

1. Compute `supersedeAt := time.Now().UTC()` and `oldRev := claimRev(oldMem.Metadata)`.
2. Stamp the successor **in place** (same row, same ID): `rev = oldRev + 1`.
3. Stamp the superseded claim **in place**: `valid_to = supersedeAt`
   (RFC3339), plus `superseded_at = supersedeAt` for observability.
4. Existing behavior (mark non-current, `EdgeTypeSuperseded` edge, evidence
   redirect) is unchanged and still uses the same IDs.

**Decision D2 — why in place, not StoreVersioned:** the pinned design said
"write the new version via StoreVersioned with Rev = old Rev + 1", but
`StoreVersioned` (manager.go:1367-1387) sets `newMem.ID = ""` and mints a NEW
row ID for the versioned copy. Re-storing `newID` through it would orphan the
`EdgeTypeSuperseded` edge target (the edge points at a now-non-current row
that is no longer the lineage head), and re-storing `oldID` through it would
give the tombstone a fresh identity. Therefore rev + valid_to stamps are
in-place metadata updates via a new unexported helper
(`stampMetadataInPlace`, single SQL `UPDATE episodic_memories SET
metadata_json = ? WHERE id = ?` through `m.episodic.store.GetDB()`, same
access pattern as `markVersionNonCurrent` at manager.go:1393). Recorded as
OPEN-QUESTIONS Q1 with recommendation.

### C6 — Tool surface (leaf 03)

`internal/tools/builtin/retain_typed.go` `RetainClaimTool.Parameters` gains
optional string params `valid_from`, `valid_to`, `observed_at` (RFC3339;
parsed with `time.Parse(time.RFC3339, ...)`, invalid values are a returned
error, not silently dropped) and `Execute` maps them into `memory.Claim`.
An expired-claims listing reaches agents via a new small tool in the same
file (see leaf 03) and users via a `meept memory expired` subcommand.

## Dispatch Protocol

- **Order: strictly sequential 01 → 02 → 03.** Leaf 02 compiles against
  leaf 01's helpers and signature; leaf 03's docs describe both.
- Dispatch one leaf per implementation agent session (each leaf = one
  subagent session, Go-only, ≤3 changed files). The orchestrator reviews
  in-session after each leaf (build + `go test -p 2 ./internal/memory/...`
  + diff read) before dispatching the next.
- If a leaf's Self-Verification Checklist fails, fix in-session; only
  re-dispatch after two failed fix attempts.
- Deviations from this master (C1-C6, D2) are NOT permitted at leaf level;
  they go back to the orchestrator and land in OPEN-QUESTIONS.md with
  Q/Rec/Impact before any leaf proceeds.

## Child Index

| Leaf | File | Scope | Depends on |
|------|------|-------|------------|
| 01 | `01-claim-schema.md` | Claim struct extension, metadata mapping, claimRev/claimInForce helpers, in-place stamp helper, supersede rev/valid_to stamping, storage verification, table-driven tests incl. zero-value backward compat | — |
| 02 | `02-validity-integration.md` | Expiry exclusion in detection candidates + FindCanonicalFor, ListExpiredClaims, ambient/skeptic-flow compatibility survey | 01 |
| 03 | `03-surface-docs.md` | retain_claim tool params, expired-claims listing tool, CLI `memory expired`, docs/workflows/memory.md section, `make docs-generate` refresh | 01, 02 |

Open forks live in `OPEN-QUESTIONS.md` (hard-exclude vs down-weight, expired
claims auto-generating Questions, valid_from enforcement scope, D2 deviation
record).

## Review Checklist

Per leaf, the orchestrator verifies:

- [ ] Only the files listed in the leaf's Scope were touched.
- [ ] `go build ./...` and `go test -p 2 ./internal/memory/...` pass (the
      `-p 2` bound is mandatory on macOS — ephemeral-port exhaustion rule in
      AGENTS.md).
- [ ] No debug artifacts, no TODOs, no placeholder values.
- [ ] All new `map[string]any` reads use two-value type assertions.
- [ ] No `time.Now().UnixNano()`/`math/rand` IDs; audit IDs keep using the
      existing `generateAuditID`.
- [ ] Errors wrapped with `fmt.Errorf("context: %w", err)`; no ignored errors
      (pre-commit blocks new `_ =` sites).
- [ ] No I/O under mutex (mutexio-clean; `go run ./tools/analyzers/mutexio/...`
      if touched code is flagged).
- [ ] Table-driven tests where the leaf specifies them.
- [ ] The literal line-number-corruption rule was honored: files located via
      search_files / terminal, never by piping read_file output into
      write_file.
- [ ] "Do NOT commit. Do NOT run git add." was honored — orchestrator does the
      commit after review.

## Coding Conventions

Match `internal/memory` house style (verified from epistemic.go /
epistemic_detection.go):

- **Logging:** `log/slog` via injected `*slog.Logger` fields (see
  `EpistemicDetector.logger`); `logger.Warn("...", "error", err, "key", val)`.
  Manager methods mostly do not log; they return errors.
- **Errors:** wrap with context: `fmt.Errorf("load claim %s: %w", id, err)`.
  Sentinel: `errors.New("memory manager not initialized")` for uninitialized
  guards. No bare `panic(err)`.
- **Tests:** table-driven (`tests := []struct{ name string; ... }{...}` with
  `t.Run(tc.name, ...)`), see `TestClaimStatusTrustWeight`
  (`internal/memory/epistemic_test.go:9`). Uninitialized-manager guards get
  dedicated tests (see `TestMarkSupersededUninitialized`, :112).
- **Concurrency:** RLock to read `m.initialized`, unlock immediately, then
  operate — never hold a mutex across I/O.
- **IDs:** `pkg/id.Generate` (or the existing `generateAuditID`) — never
  timestamp/rand IDs.
- **Metadata:** values in `map[string]any`; reads always two-value assertions;
  times as RFC3339 strings.
- **Test command everywhere:** `go test -p 2 ./internal/memory/...`

## Completion Tracking Table

| Leaf | Status | Implementer session | Review pass | Notes / deviations |
|------|--------|--------------------|-------------|--------------------|
| 01-claim-schema | pending | — | — | |
| 02-validity-integration | pending | — | — | |
| 03-surface-docs | pending | — | — | |
| OPEN-QUESTIONS resolutions | pending | — | — | Q1-Q4 open at authoring time |

## Integration Test Plan

After all three leaves:

1. **Unit/regression:** `go test -p 2 ./internal/memory/...` — includes leaf
   01/02 table tests (zero-value backward compat, expiry predicate, supersede
   stamping, ListExpiredClaims).
2. **Tool-layer test:** `go test -p 2 ./internal/tools/builtin/...` — leaf 03
   tests for `retain_claim` param parsing (valid RFC3339, invalid → error,
   absent → unchanged behavior) and the expired-claims listing tool.
3. **Cross-layer wiring check (manual, orchestrator):**
   `go build -o bin/meept ./cmd/meept && go build -o bin/meept-daemon
   ./cmd/meept-daemon`; run daemon; store a claim with `valid_to` in the past
   via `retain_claim`; confirm `meept memory expired` lists it with an expired
   reason; confirm it does not appear as a canonical claim.
4. **Backward-compat spot check:** an existing DB row written before this
   change (no new metadata keys) still: is returned by claim searches, is
   eligible as canonical, reads rev=0.
5. **Docs freshness:** `make docs-generate` runs clean and
   `docs/reference/generated/memory.md` contains `ListExpiredClaims` and the
   extended `Claim` fields; `git diff --stat` shows no hand-edits to
   `docs/reference/generated/`.
