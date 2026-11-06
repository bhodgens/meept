# Secret Restore Scoping - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Session-scope the SecretObfuscator restore lane + fix the
  16-bit placeholder collision (map-order-random wrong-secret restore).
  DEPENDS ON leaf 02 landing (restore only matters once obfuscation runs).
- **Dependencies:** 02 (end-to-end proof needs the transform lane live).
- **Estimated Context:** 55K

## Goal

See parent master.md traceability table (this leaf's Req row).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Collision fix: secrets.go:182-185 placeholder is 16-bit (h&0xFFFF) and :245 overwrites obfuscateMap[ph] on collision -> wrong secret restored by map order. Widen entropy (32/48-bit) or extend replacementFor's existing deobfuscate-map guard to cover placeholder-vs-secret cross-collisions. | Modify: internal/security/secrets.go + test | `go test -p 2 ./internal/security/ -run Obfus` | two secrets collide -> wrong restore | both restore to OWN originals | Given two secrets colliding on the placeholder; When both obfuscated + restored; Then each returns its own value |
| T2 | Session scope: RestoreOutput takes sessionID but Deobfuscate ignores it (process-wide). Session-tag the placeholder map (per-session registry or session-keyed entries). | Modify: internal/security/secrets.go, pii_restore_hook.go call site | same | session B resolves session A placeholders | cross-session resolve refused | Given session A placeholders; When session B restores; Then no resolution |
| T3 | Efficiency: whole-map strings.ReplaceAll per restore (secrets.go:255-259) -> prefix-map lookup ([#XXXX# scan), bounded to the session's entries. | Modify: internal/security/secrets.go | same | O(map) per token | O(session entries) | Given N secrets; When restore scans; Then only session entries consulted |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] scoped tests green (-p 2)
- [ ] gofmt clean, ASCII only
- [ ] wiring-failure proof demonstrated
- [ ] sibling files untouched

## Review Checklist (orchestrator)

- [ ] Wiring point matches the survey table
- [ ] Proof test fails with wiring deleted (witnessed)
- [ ] No silent scope expansion
- [ ] Staged-set audit before commit

## Do NOT commit. The orchestrator stages and commits after review.
