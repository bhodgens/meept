# Tree 01 — Layered Async Distillation (orchestrator)

Parent: `../master.md`. Conventions: `../../SHARED-CONVENTIONS.md` (§2.1 L0
schema, §2.2 job types, §4 e2e rule, §5 waves).

**Goal:** Give meept TAM's layered pipeline: raw conversation persisted as L0,
distilled asynchronously into L1 claims and L2 session-scene summaries via the
existing PersistentQueue. L3 persona is DEFERRED per OPEN-QUESTIONS Q1 (rec:
defer).

**Architecture:** New subpackage `internal/memory/l0` (recorder, reader,
retention). Async distill is a new queue job handler in
`internal/memory/distill_async.go` that enqueues from the ambient-extraction
hook point and executes L1/L2 jobs off the turn path. Failure handling reuses
queue retry + dead-letter. Root rationale: meept's ambient extraction today
runs synchronously in the turn and has no raw tier — replay and audit are
impossible; this tree fixes both.

## Interface contracts

With Tree 02: the L1 extract job calls `conflict.DetectBatch` (Tree 02 pkg)
if present; when the package is absent (wave ordering), extraction stores
directly. Contract = SHARED-CONVENTIONS §2.3 Decision type; wiring leaf is
Wave 3, after Tree 02-L1 lands.

With Tree 03: L1 jobs write claims with `status=auto` (existing semantics) —
governance operates unchanged.

## Child Index (inline leaves)

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 l0-recorder | 1 | `internal/memory/l0/` package: record/reader/retention + unit tests | 60K |
| L2 async-worker | 2 | queue job types, handler, enqueue hook | 80K |
| L3 wiring-e2e | 3 | manager/daemon wiring, config, e2e suite, docs | 70K |

## Dispatch Protocol

Standard (root master). Wave gates enforced. Leaf briefs must inline the L0
JSON schema from SHARED-CONVENTIONS §2.1 and the job-type table §2.2.
Do NOT commit instructions in every brief.

## Review Checklist

Root checklist applies. Tree-specific adds:
- [ ] L0 writes never block the turn path (recorder errors are logged, not
      propagated to the turn)
- [ ] `<memory-context>` tags stripped from L0 content (feedback-loop guard)
- [ ] Retention prune runs inside consolidation (no new ticker)
- [ ] Queue job failures land in dead-letter with readable reason
- [ ] `memory.l0_enabled` config defaults **false**

## Coding Conventions

Per `../../SHARED-CONVENTIONS.md` §3: Go stdlib-first, sentinel errors with
`%w` wrap, `pkg/id.Generate()` IDs, JSON5 config opt-in defaults, no mutex
across I/O, lowercase UI text, no `os.Getwd()`, GPL/AGPL dep license stop.

## Integration Test Plan

L3's e2e suite `e2e/suites/memory-layered-distill/`: fake-chatter LLM,
seeded conversation → assert L0 lines exist, an L1 job ran, a claim with
`status=auto` landed, an L2 scene file exists, queue stats show zero
dead-letters. Plus: kill mid-extract → restart → job re-claimed (idempotency).

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| L1 l0-recorder | PENDING | |
| L2 async-worker | PENDING | |
| L3 wiring-e2e | PENDING | |

## Leaf definitions

### Leaf L1 — l0-recorder (Wave 1)

**Scope:** Create `internal/memory/l0/` with:
- `recorder.go` — `Recorder.Append(sessionID, role, content string, meta map[string]any) error`.
  Writes one JSON line per SHARED-CONVENTIONS §2.1 to
  `<memory.data_dir>/l0/YYYY-MM-DD.jsonl`. ID via `pkg/id.Generate()`. Strip
  `<memory-context>...</memory-context>` spans and sanitize (reuse
  `internal/memory` ambient sanitize helpers via a small local function — do
  not import-cycle; if the helper is unexported, export a narrow
  `SanitizeForStore` in `internal/memory` FIRST in this leaf).
- `reader.go` — `ReadSince(path string, sinceID string, limit int) ([]Record, error)`
  and `ReadSession(date, sessionID string)`.
- `retention.go` — `Prune(dir string, maxAgeDays int) (int, error)` —
  deletes whole daily files older than cutoff. Called from Consolidator run
  (wired in L3, not here).
- `l0_test.go` — table tests: append/read roundtrip, tag-stripping, ID
  uniqueness, prune boundary. (Unit tests here are bug-fix tier for new
  package bootstrap; the e2e rule's feature tests land in L3.)

**Verify:** `go build ./internal/memory/... && go test ./internal/memory/l0/ -run . -count=1`.

**Do NOT commit.**

### Leaf L2 — async-worker (Wave 2)

**Scope:** Wire distillation onto `internal/queue.PersistentQueue`:
- Job types `memory.l1_extract` / `memory.l2_scene` per SHARED-CONVENTIONS §2.2
  (payload structs in `internal/memory/distill_async.go`).
- `EnqueueL1(sessionID, l0Path, sinceMsgID)` / `EnqueueL2(sessionID, sinceTS)` —
  thin enqueue helpers; error wrapped, never ignored.
- Handler `handleL1Extract`: read L0 since marker → build extraction prompt
  (reuse existing ambient-extraction prompt path and `memory_model` slot
  resolution) → parse candidates → store as `status=auto` claims via
  `Manager.StoreClaim` → advance the per-session marker (stored as claim
  metadata `l0_progress` on a session-marker memory, matching existing
  metadata patterns). LLM failure → return error (queue retry/backoff/DLQ
  handles it — do NOT add a private retry loop).
- `handleL2Scene`: segment the session's L0 lines into scenes via ONE
  `memory_model` call (LLM scene segmentation per resolved Q2b — output
  schema: `[{scene_name, message_ids}]`, mirroring TAM's
  `SceneSegment` shape) → write one scene file per segment to
  `<data_dir>/l2/<session>-<date>-<scene>.json`
  (`{session_id, date, scene_name, summary, source_message_ids,
  segment_method: "llm"}`). Parse failure → fall back to ONE
  whole-session scene (`segment_method: "fallback"`) so an L2 job never
  dead-letters on a malformed segmentation response.
- Enqueue trigger: end of the ambient extraction hook — one L1 job per
  session-day with new L0 lines (dedupe by marker), one L2 job after L1
  completes (chained inside the handler's success path).
- Tests: fake-chatter LLM (pattern: meept-development skill
  `references/fake-chatter-mapreduce-testing-2026-09-06.md`) covering
  happy path, marker advance, LLM error → error returned (assert no marker
  advance), duplicate enqueue suppressed by marker.

**Verify:** `go build ./... && go test ./internal/memory/ -run 'Async|Distill' -count=1`.

**Do NOT commit.**

### Leaf L3 — wiring-e2e (Wave 3)

**Scope:**
- Config: `memory.l0_enabled` (default false), `memory.l0_retention_days`
  (default 30) in `internal/config` + `config/` template comments.
- Manager wiring: construct Recorder when enabled; call `l0.Prune` in the
  Consolidator run; gate enqueue on config.
- Daemon wiring: register queue handlers where existing memory handler
  registers (`internal/daemon` startup — follow existing memory handler
  construction; single touch point).
- Docs: `docs/workflows/memory.md` — new "Layered Distillation (L0/L1/L2)"
  section incl. config snippet + retention note.
- e2e suite `e2e/suites/memory-layered-distill/` per orchestrator Integration
  Test Plan + `e2e/manifest.json` path_map entry
  (`internal/memory` → suite, plus `internal/queue` mapping if not present).
- CLI (thin, for observability): `meept memory l0 stats` — file count, line
  count, oldest date. Reuses reader; no daemon required.

**Verify:** `make e2e-fast-area AREA=memory-layered-distill && go build ./... && go vet ./internal/memory/...`.

**Do NOT commit.**
