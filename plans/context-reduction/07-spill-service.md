# L07 — Tool-Output Spill Service (P7)

**Proposal:** P7. **Depends on:** none. **Files (3):**
- Create: `internal/llm/spill.go` (service)
- Modify: `internal/llm/context_compactor.go` (pruneToolOutputs, :766-853)
- Modify: `internal/config/schema.go` (compaction.spill_enabled, spill.retention_days)
- Test: `internal/llm/spill_test.go`

**Problem:** `pruneToolOutputs` (context_compactor.go:766-777) irreversibly
replaces oversized tool outputs with `"[Output truncated — N tokens saved]"`
(:826, :845). Manus's rule (research doc 04): irreversible compression is risky
— drop content only when a pointer + re-fetch affordance remain. Protected
budgets exist (protectedBudget 40000, minSavings 20000, :780-781) and
file_read/memory_search results are exempt (:814) — those stay.

**Contracts (root #3 + knobs):**
- Pointer line: `[Output spilled to <absolute path> — re-read with the file tool if needed (N tokens)]`
- Spill path: `$MEEPT_HOME/spill/<session_id>/<sha256(content)[:16]>.txt`
- Config: `compaction.spill_enabled` bool default **false** (opt-in);
  `spill.retention_days` int default **30** (consumed by L08; define it here so
  the schema lands once).
- Spill write failure falls back to today's truncation text. NEVER fails the turn.
- Content on disk is raw (unwrapped) tool output. Meept home resolution via
  internal/config home helpers (MeeptPath/MeeptHome).

### Tasks

**Task 1 — failing tests (spill_test.go):**
(i) `TestSpillServiceWritesAndReturnsPointer`: given content + session id,
writes the file, returns the exact pointer line with correct path and token
count. (ii) `TestSpillDisabledReturnsTruncation`: spill_enabled=false →
existing truncation text unchanged. (iii) `TestSpillWriteFailureFallsBack`:
unwritable path (permission-denied dir) → truncation text, nil error.
Run: `go test -p 2 ./internal/llm/ -run TestSpill` — FAIL.

**Task 2 — implement spill.go:** `type SpillService struct` + `Spill(ctx,
sessionID, content string) (pointerLine string, err error)`; sha256 via
crypto/sha256; MkdirAll 0700; file 0600. No goroutines.

**Task 3 — integrate into pruneToolOutputs:** when spill_enabled and the prune
site is NOT in the protected categories (:814), write spill, replace output
with pointer line + keep existing truncation text as fallback. Add the
compaction config plumbing (`compaction.spill_enabled`,
`spill.retention_days`) in schema.go near the existing `[compaction]` keys
(:564-575).

**Task 4 — full package:** `go test -p 2 ./internal/llm/ ./internal/config/` green; `go build ./...`.

**Task 5 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] spill_enabled=false → byte-identical old behavior (test proves it)
- [ ] Files 0600, dirs 0700; no path traversal via session_id (sanitize)
- [ ] Config keys + JSON5 tags + defaults in schema.go

## Review Checklist (for orchestrator)
- [ ] Protected categories + budgets untouched
- [ ] No mutex across I/O; no error swallowed silently (fallback logs at debug)
