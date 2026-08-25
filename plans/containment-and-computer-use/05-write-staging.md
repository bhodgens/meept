# Write Staging Generalization - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source — search_files/terminal cat only.

## Meta

- **Parent:** ../master.md
- **Scope:** Extend PendingChangesRegistry: PreImageSHA256 integrity, StageWrite API, and coverage of WriteFile (file_write) so every mutating file tool stages.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** parity-audit gap #4a/#4b; FrontierAgent diff-gate pattern

## Goal

Today FileEditTool stages edits (pending_changes.go) but WriteFile applies directly, and staged changes carry no pre-image hash — a file modified between staging and accept silently overwrites the drift. This leaf adds PreImageSHA256 to PendingChange, verifies it at accept time (refusing drift), and routes WriteFile through staging.

## Context

internal/tools/builtin/pending_changes.go: PendingChange struct lines 9-19 {ID,SessionID,FilePath,Original,Modified,Diff,CreatedAt,ExpiresAt,Metadata}; registry Add/Get/Remove/GetBySession/RemoveBySession + Start() expiry loop. file_edit.go:385-426 shows the staging pattern incl. generateDiffPreview (~1028). resolve.go ResolveTool accept branch re-validates fence at write time — extend, don't bypass. WriteFile tool lives in filesystem.go (locate exact type name via search_files "file_write").

Key files:
- internal/tools/builtin/pending_changes.go
- internal/tools/builtin/file_edit.go (pattern reference)
- internal/tools/builtin/filesystem.go (WriteFile tool)
- internal/tools/builtin/resolve.go (accept path)

## Interface Contracts (From Parent)

### Exposes

```go
// pending_changes.go additions:
type PendingChange struct {
    // existing fields...
    PreImageSHA256 string `json:"pre_image_sha256"` // sha256 hex of Original
}

func (r *PendingChangesRegistry) StageWrite(sessionID, path string, original, modified []byte) (*PendingChange, error)
// Computes Diff via existing generateDiffPreview logic (promote to shared helper),
// sets PreImageSHA256=sha256(original), registers, returns.

// resolve.go accept path addition:
// Before writing Modified: read current file bytes; if sha256(current)==PreImageSHA256 -> proceed;
// else if sha256(current)==sha256(Modified) -> already applied, treat as success no-op;
// else refuse: error text includes "file changed since staging" + both hashes short-form.
```

Backfill: existing in-flight PendingChanges created without hash get empty PreImageSHA256; accept treats empty as legacy (proceed with warning log) to avoid breaking mid-upgrade sessions.

### Consumes

Existing FenceChecker at accept (unchanged), pkg/id for IDs (already used).

## Tasks

### Task 1: StageWrite + hash field

**Files:** Modify pending_changes.go (+ shared diff helper extraction if trivial); Test pending_changes_test.go extension.
Failing tests: StageWrite returns change w/ correct Diff + PreImageSHA256 of known input; GetBySession sees it; empty-original create case (new file) hash = sha256(""). Standard cycle.

### Task 2: Drift-checking accept

**Files:** Modify resolve.go accept branch; Test resolve_test.go extension.
Failing tests table-driven: clean accept proceeds+writes; drifted file refuses w/ message containing "changed since staging"; already-applied no-op succeeds idempotently; legacy empty-hash warns+proceeds. Standard cycle.

### Task 3: WriteFile staging

**Files:** Modify filesystem.go WriteFile Execute path; Test its _test.go.
Behavior mirrors file_edit.go:385-426: when registry present (nil-safe guard), stage instead of write, return Result text naming change ID + evidence NewEvidence("pending_change_created",...) exactly like edit path. Config kill-switch: [security.pending_changes] tools include/exclude? NO — keep simple: env-style config bool `write_file_staging` default TRUE on SecurityConfig? Prefer: reuse existing pattern — check how file_edit decides staging (registry nil or not) and match exactly; add no new knob this leaf.
Failing test: WriteFile w/ registry -> no disk write occurs, change registered, evidence emitted; w/o registry (legacy wiring) -> direct write unchanged. Standard cycle.

## Self-Verification Checklist

- [ ] All three tasks; -race green across internal/tools/builtin
- [ ] Drift refusal message stable/tested; legacy backfill handled
- [ ] No new config knobs added
- [ ] Evidence types match existing strings

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Accept-path fence re-validation preserved (not regressed)
- [ ] sha256 via crypto/sha256, hex lowercase
- [ ] No I/O under registry mutex (mutexio)
- [ ] Existing tests unmodified except where extended

Output: APPROVED or gaps.

## Notes

- TUI/GUI/HTTP surfacing is leaf 07 — do NOT touch comm/tui here.
- If generateDiffPreview extraction gets large, duplicate minimal unified-diff into pending_changes.go instead; note deviation.
