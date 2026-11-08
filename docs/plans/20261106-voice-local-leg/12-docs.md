# Docs + AGENTS.md + spec amendment - Implementation Leaf

> **For the implementing agent:** This is a DOCUMENTS leaf. Do NOT commit —
> the orchestrator handles git. Explore with search_files or terminal cat,
> never read_file. Verify cross-references resolve (loop cited paths)
> instead of running tests.

## Meta

- **Parent:** master.md
- **Scope:** Write the landed local-leg contract into the spec, the package
  invariant doc, the user docs, and the root AGENTS.md; regenerate graphs.
- **Dependencies:** leaf 11 (cite landed code only)
- **Estimated Context:** 40K
- **Concurrency Group:** E
- **Files this leaf owns:** docs/workflows/voice-sidecar.md (rev 3),
  docs/workflows/voice.md, internal/voice/AGENTS.md, root AGENTS.md,
  docs/generated/* (via make graphs only)

## Goal

A reader of docs/workflows/voice-sidecar.md learns: two legs exist
concurrently, the leg/voice selection model, the CL1 open frame, the local
leg lifecycle and its deliberate policy differences (no effects, no audit,
no carrier), and the CLI control plane. internal/voice/AGENTS.md gains the
invariants a future agent must not break. Root AGENTS.md gets the invariant
summary row per its maintenance rule.

## Tasks

| Task ID | Objective | Files | Verify | PASS state |
|---|---|---|---|---|
| T1 | Spec rev 3: new section "Local leg (C11)" with the open frame, leg matrix (phone/local/inbound/outbound), config keys CL4 verbatim, policy-difference table (effects/audit/carrier ONNX rule: local leg uses RMS VAD, phone still refuses without ONNX) | docs/workflows/voice-sidecar.md | cited code paths exist (grep each) | PASS |
| T2 | User doc: how to run `meept voice local` (file mode + live mode, mic permission note, GOOS support table), status command, both-legs concurrency example | docs/workflows/voice.md | commands match cmd/meept/voice.go actual flags | PASS |
| T3 | internal/voice/AGENTS.md: invariants - local leg never imports telnyx or touches the broker key; registry leg field semantics; open-before-setup ordering; RMS-VAD-only-when-local; LocalSplice has no reorder by contract | internal/voice/AGENTS.md | statements match code (each has a file:line in-paren) | PASS |
| T4 | Root AGENTS.md voice row: add the local-leg one-line invariant + any new package/dir to the table | AGENTS.md | root file review checklist satisfied | PASS |
| T5 | `make graphs` + freshness commit content; update docs/generated reference note if bus topics changed (voice.call.* direction "local") | docs/generated/* | make graphs-check | clean |
| T6 | Cross-ref + ASCII sweep | touched docs | grep for em-dashes/U1000 n/a; loop relative links | clean |

## Interface Contract (What This Leaf Exposes)

The amended spec (rev 3) becomes the authority the way rev 2 was for the
first tree.

## Self-Verification Checklist

- [ ] Every code citation greps non-empty on HEAD
- [ ] No claim about behavior that lacks a test or a file:line
- [ ] Lowercase UI text in command examples where it's UI output

## Review Checklist

- [ ] Root AGENTS.md maintenance checklist (all 6 items) satisfied
- [ ] graphs-check green in the same commit range
- [ ] No TODO/stubs in docs
