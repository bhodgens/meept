# Sprint 4: Verification + Low Severity Fixes

**Source:** `docs/audit-bugs-gaps-2026-04-25.md`

**Status:** IN PROGRESS

**Goals:**
1. Verify 37 unverified issues (may already be fixed)
2. Fix 17 Low severity issues
3. Confirm status of any remaining "STILL_PRESENT" issues

---

## Completion Status

### Low Severity Fixes Completed: 2/17 (12%)

| Issue | File | Fix | Commit |
|-------|------|-----|--------|
| TOOLS-14 (moved from Medium) | `internal/calendar/auth.go:254` | Replace `fmt.Printf` with `slog.Default().Warn()` | d810c17 |
| (unnamed) | `internal/agent/q/agent_designer.go:380` | Replace deprecated `strings.Title` with `unicode.ToUpper` | d810c17 |

### Low Severity Fixes Remaining: 15

**Note:** The original audit (`audit-bugs-gaps-2026-04-25.md`) mentions 17 Low severity issues but never enumerated them with specific IDs and descriptions. The Sprint 4 plan references "CLI-10 through CLI-38" but these issue IDs do not exist in the source audit document.

**Identified Low Severity Patterns (candidates for remaining fixes):**
- Ignored error returns (`_ = `) in non-critical paths
- Use of `context.Background()` instead of propagated contexts
- Minor logging inconsistencies
- Dead code / unused imports
- Magic numbers without named constants

---

## Original Plan

### Unverified Issues (37)

These were in the original audit but never verified by subagents:

### High Severity (21 issues - may already be fixed)
- AGENT-8 through AGENT-16: 9 issues
- SEC-6 through SEC-8: 3 issues (FIXED in Sprint 2)
- LLM-7 through LLM-11: 5 issues (FIXED in Sprint 2)
- MEM-7 through MEM-10: 4 issues (FIXED in Sprint 2)
- TOOLS-8 through TOOLS-12: 5 issues (FIXED in Sprint 2)
- CLI-5 through CLI-7: 3 issues (FIXED in Sprint 2)

**Note:** Most High severity issues were FIXED in Sprint 2. Remaining AGENT issues verified as fixed.

### Medium Severity (16 issues - may already be fixed)
- CORE-5 through CORE-8: 4 issues (FIXED in Sprint 3)
- AGENT-17 through AGENT-23: 7 issues (FIXED in Sprint 3)
- SEC-9 through SEC-11: 3 issues (FIXED in Sprint 3)
- LLM-12, LLM-13: 2 issues (FIXED in Sprint 3)
- MEM-11 through MEM-17: 7 issues (FIXED in Sprint 3)
- TOOLS-13, TOOLS-14: 2 issues (FIXED in Sprint 3)
- CLI-8, CLI-9: 2 issues (FIXED in Sprint 3)

**Note:** All Medium severity issues FIXED in Sprint 3.

### Low Severity (17 issues)
Need to implement these simple fixes:
- CLI-10 through CLI-38 (various low severity issues)

**Issue:** The source audit never enumerated these 17 Low severity issues with specific IDs and file paths. The reference to "CLI-10 through CLI-38" appears to be an error since the audit only documents up to CLI-9.

---

## Implementation Order

1. **Verification Phase** - Verify all High and Medium issues are truly fixed **(COMPLETE)**
2. **Low Severity Phase** - Implement 17 Low severity fixes **(IN PROGRESS - 2/17 = 12%)**
3. **Final Verification** - Run full test suite, verify all fixes

---

## Verification Criteria

Each issue must be:
1. Confirmed fixed with code evidence
2. Build passes: `go build ./...`
3. Tests pass: `go test ./... -race`
4. Committed immediately after verification

---

## Summary

| Severity | Total | Fixed | Already Fixed | Remaining | % Complete |
|----------|-------|-------|---------------|-----------|------------|
| Critical | 24 | 15 | - | 9 verified fixed | 100% |
| High | 42 | 21 | 4 | 17 verified fixed | 100% |
| Medium | 32 | 23 | 2 | 7 verified fixed | 100% |
| Low | 17 | 2 | - | 15 | 12% |
| **TOTAL** | **115** | **61** | **6** | **48** | **58%** |

**Note:** The 17 Low severity issues were never documented in the source audit. The Sprint 4 plan references "CLI-10 through CLI-38" but these issue IDs don't exist. To complete Sprint 4, the remaining 15 low-severity fixes would need to be identified and documented first.
