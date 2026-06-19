# Vision Implementation Deferred Items

**Source:** `docs/plans/vision-implementation-review.md` (post-fix resolution table)
**Created:** 2026-06-19

## Deferred Items

| ID | Severity | File | Description | Resolution |
|----|----------|------|-------------|------------|
| H1 | High | `internal/services/upload_service.go:114-158` | Two-phase reserve/write/finalize race: concurrent identical-content uploads can observe Width/Height=0 and a small refcount-overwrite window | **Fixed.** Restructured to disk-write-before-reserve: write file + decode dimensions outside the lock, then atomically insert-or-merge under the lock (increment existing record's refcount if a concurrent caller won, otherwise insert fresh with all fields populated). |
| M4 | Medium | `internal/tui/models/chat.go:2804-2819` | `detectAndAttachFile` calls `m.rpc.UploadFile` synchronously inside the Bubble Tea Update goroutine, freezing the TUI render loop for multi-MB uploads | **Fixed.** `detectAndAttachFile` now returns `tea.Cmd`. Attachment is appended immediately with empty UploadID; the returned cmd performs the upload async and yields an `uploadResultMsg` that `Update` dispatches to fill in the UploadID (or demote IsImage on failure). Matches existing `STTResultMsg` pattern. |
| L2 | Low | `internal/agent/vision_preflight.go:50` | No log entry on the cache-hit early-return path; operators have no visibility into cache hit/miss ratio | **Fixed** (in prior commit `0248ed97 fix(vision): close multimodal gaps from review pass` — L2 re-analysis confirmed the Debug log is present at line 55). |

## Resolution Status

- [x] H1 fixed (disk-write-before-reserve pattern)
- [x] M4 fixed (async tea.Cmd upload)
- [x] L2 fixed (Debug log on cache-hit path)
- [x] Completion rate: 100% of 3 actionable items resolved
- [x] Verification: `go build ./...` PASS, `go test ./internal/services/... ./internal/tui/... ./internal/agent/...` PASS

## Rationale

H1 was escalated from "documented as intentional" to a real fix after re-analysis confirmed the race scenario: two callers A and B both passing the dedup check before either reserves produces a final RefCount=1 instead of 2. The disk-write-before-reserve pattern eliminates the window. M4 and L2 are straightforward polish items now completed.
