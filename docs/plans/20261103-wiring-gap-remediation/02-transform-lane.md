# Transform Context Lane - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Call RunTransformContext in the loop input path (currently
  zero production callers; secret-obfuscation/security-transform/pii
  registrations are dead).
- **Dependencies:** leaf 07 depends on this landing.
- **Estimated Context:** 50K

## Goal

See parent master.md traceability table (this leaf's Req row).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Call hookRegistry.RunTransformContext per reasoningCycle iteration beside RunPrepareNextTurn (loop.go ~:4663-4698; messages finalized ~:4633). Fixpoint until Modified==false, cap 5 iterations (pii_hook_order_test.go:103 pattern). CLONE the messages slice first: conv.GetWindowedMessages results must never be mutated into history. | Modify: internal/agent/loop.go | `go test -p 2 ./internal/agent/ -run Transform` | no production call | transforms run per iteration | Given registered hooks; When cycle iterates; Then transforms apply to a cloned message set |
| T2 | Proof: register SecretObfuscationHook + fake provider -> assert provider received #XXXX#-obfuscated user content; remove the call -> raw secret reaches provider = FAIL. Pin history non-mutation (post-turn history carries originals, not placeholders). | Modify: internal/agent/loop_test.go or new test | same | raw secret leaks to provider | obfuscated content at provider; history clean | Given a secret in user input; When turn runs; Then provider sees placeholders and history keeps originals |

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
