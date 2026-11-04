# ConfigSnapshot Seam Propagation - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Propagate contextAssemblerCfg/tierProvider/contextSeam/
  piiRestore through ConfigSnapshot to session clones (three-site rule).
- **Dependencies:** pairs with 02/03 being load-bearing.
- **Estimated Context:** 45K

## Goal

See parent master.md traceability table (this leaf's Req row).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the 4 seam fields to ConfigSnapshot + inline closures before the EXCLUDED note (loop.go ~:9167), mirroring the 7bffceb9 group-gates pattern. | Modify: internal/agent/loop.go | `go test -p 2 ./internal/agent/ -run Seam` | snapshot lacks fields | clone carries seams | Given armed template; When GetOrCreateWired clones; Then seams non-nil on clone |
| T2 | Audit site 3 (verifier children, verification_hook.go) for the same seams; wire if absent, document if N/A. | Modify: internal/agent/verification_hook.go if needed | same | un-audited | audited + wired or documented | Given verifier child; When built; Then seams propagate or deviation documented |
| T3 | Proof: extend loop_seam_wiring_test.go - arm template seams, GetOrCreateWired clone, assert clone.contextSeamIfEnabled()!=nil + PiiRestoreHook()!=nil; nil = FAIL. Also fix the false loop.go:1803 comment (claims propagation that does not happen - becomes true with T1). | Modify: internal/agent/loop_seam_wiring_test.go, loop.go comment | same | nil seams on clone | assertion passes; comment true | Given armed template; When cloned; Then seams live |

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
