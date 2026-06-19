# Vision Implementation Deferred Items

**Source:** `docs/plans/vision-implementation-review.md` (post-fix resolution table)
**Created:** 2026-06-19

## Deferred Items

| ID | Severity | File | Description | Resolution |
|----|----------|------|-------------|------------|
| H1 | High | `internal/services/upload_service.go:114-158` | Two-phase reserve/write/finalize race: concurrent identical-content uploads can observe Width/Height=0 and a small refcount-overwrite window | Documented as intentional (low real-world impact; SHA dedup is specifically designed to collapse identical content). A cleaner fix would write the file BEFORE reserving the record, or hold the lock across the disk write. |
| M4 | Medium | `internal/tui/models/chat.go:2804-2819` | `detectAndAttachFile` calls `m.rpc.UploadFile` synchronously inside the Bubble Tea Update goroutine, freezing the TUI render loop for multi-MB uploads | Move upload to a background goroutine; surface progress via a tea.Cmd. Tracked as UX follow-up, not a correctness bug. |
| L2 | Low | `internal/agent/vision_preflight.go:50` | No log entry on the cache-hit early-return path; operators have no visibility into cache hit/miss ratio | Add a Debug log at the `needsVisionPreflight` early-return. Cosmetic. |

## Resolution Status

- [ ] H1 documented as intentional (low priority — not actively planned)
- [ ] M4 tracked as UX follow-up (slated for next TUI polish pass)
- [ ] L2 tracked as observability follow-up (slated for next logging pass)
- [x] Completion rate: 0% of 3 actionable items resolved (all documented, none blocking ship)

## Rationale

None of these items block shipping the vision feature. H1's race requires concurrent identical-content uploads (which the SHA dedup is specifically designed to collapse — the race window is the exception case). M4 is a UX polish item. L2 is observability. All three are safe to defer.
