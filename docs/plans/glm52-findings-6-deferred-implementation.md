# GLM-5.2 Round 6 Findings — Deferred Implementation Plan (All Resolved)

**Source:** `docs/plans/glm52-findings-6.md`

## Overview

All four items initially marked as deferred during the round-6 review pass
were resolved by the orchestrator's `oneshot-yeet` follow-up. Two were
fixed in code; two were resolved by strengthening docstrings to make the
intentional trade-offs explicit.

**Total deferred findings: 4 (all resolved)**
**Resolved: 4/4 (100%)**
**Remaining: 0**

## Resolution Summary

| ID | Severity | File | Description | Resolution |
|----|----------|------|-------------|------------|
| S4-5 | Low | `internal/shadow/exporter.go`, `internal/shadow/teacher.go` | Hand-rolled `toLower` (UTF-8 hazard, same class as S4-2) | Fixed: replaced with `strings.ToLower` / `strings.Contains`; added `strings` import to `teacher.go` |
| S4-6 | Info→Low | `internal/tools/builtin/platform.go` | `// Sort categories` comment present but `sort.Strings` call missing | Fixed: added missing `sort.Strings(categories)`; imported `sort` |
| S3-15 | Medium | `internal/auth/encryption.go` | Encryption key derives from hostname (can change across reboots/DHCP/container rebuilds) | Documented: added "Stability note" docstring pointing operators at `NewEncryptionKey(userKey)` override. Behavior unchanged to avoid invalidating existing tokens. |
| S3-16 | Low | `pkg/id/id.go` | ID-generator fallback predictable on `crypto/rand` failure | Documented: strengthened `Generate` docstring to make the intentional trade-off explicit. Behavior unchanged. |

## Resolution Status

- [x] All deferred items addressed (4 of 4, all resolved)
- [x] Completion rate: 100% (4 of 4)
  - S4-5: fixed in code
  - S4-6: fixed in code (also caught a latent bug — the sort docstring lied)
  - S3-15: documented with workaround
  - S3-16: documented as intentional

## Verification

All fixes verified with:
- `go build ./...` — clean
- `go vet ./internal/shadow/... ./internal/tools/builtin/... ./internal/auth/... ./pkg/id/...` — clean
- `go test -count=1 -timeout 300s ./...` — 73 packages pass, 0 failures
- Verification commands run by orchestrator:
  - `grep "toLowerString\|toLowerASCII" internal/shadow/` → no matches (helpers removed)
  - `grep "strings.ToLower" internal/shadow/exporter.go internal/shadow/teacher.go` → matches at expected lines
  - `grep "sort.Strings(categories)" internal/tools/builtin/platform.go` → match at line 192
  - `grep "Stability note" internal/auth/encryption.go` → match at line 105
  - `grep "Fallback behavior" pkg/id/id.go` → match at line 11

## Bonus Findings (SX-1 through SX-10)

During the oneshot-yeet pass, the orchestrator's verification step surfaced
10 additional cross-cutting fixes applied by auxiliary fixers (sub-subagents
spawned by review subagents). These are documented in the main findings
document under "Auxiliary Findings" and are not considered deferred — they
were all verified clean by the final `go build` + `go test` pass.

Highlights:
- SX-1: bounded gossip send goroutines (cluster)
- SX-2: scheduler job context now derives from shutdown-aware `runNowCtx`
- SX-6: PTY session no longer truncates large output chunks
- SX-7: `TestHarness.Validate` no longer nil-derefs on backend error
- SX-8: `SkillIndex.Match` tie-breaking now deterministic (kills a flaky test)
