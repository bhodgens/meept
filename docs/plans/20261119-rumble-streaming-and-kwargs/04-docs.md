# Rumble streaming docs - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Do NOT write code.
> Do NOT commit. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Document request-shape fix, generic pseudo-streaming, kwargs selectors
- **Dependencies:** gates validated
- **Estimated Context:** 35K
- **Concurrency Group:** C
- **Audit references:** none

## Goal

Update user-facing docs and invariant files so operators can use the new
selectors and agents know the contracts.

## Context

- `docs/workflows/voicechild.md` - provider config docs
- `docs/workflows/voice-sidecar.md` - sidecar as-built
- `docs/research/rumble2flash-evaluation.md` - evaluation record
- root `AGENTS.md`, `internal/voice/AGENTS.md` - invariant files

## Verified Anchors

- `docs/workflows/voicechild.md` - Rumble STT section (prior tree)
- `docs/workflows/voice-sidecar.md` - Rumble sidecar section (prior tree)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Documentation only; no code symbols.

### What This Leaf Consumes

- C2 ProviderConfig fields, C5 decorator, C6 operator config - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Voicechild docs | Modify: `docs/workflows/voicechild.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then request shape, partials, kwargs documented |
| T2 | Sidecar docs | Modify: `docs/workflows/voice-sidecar.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then operator example with stt_partial_ms and kwargs |
| T3 | Invariants | Modify: root `AGENTS.md`, `internal/voice/AGENTS.md` | `git diff --check` | n/a | PASS | Given invariants; When read; Then generic decorator opt-in + request shape pinned |

T1 detail: update the Rumble STT section: transport is chat.completions with
`audio_url` data URI (NOT the transcriptions JSON body); `chat_template_kwargs`
(`language` is a NAME, `digits`, `filler`, `verbatim`) ride ProviderConfig and the
keys frame; `stt_partial_ms` wraps ANY batch vendor in the provider-agnostic
pseudo-stream decorator (partials are client-side snapshots, NOT server
incremental input); do not enable on native-partial vendors (deepgram); default
0 keeps whisper/byte-identical behavior.

T2 detail: add a short operator example showing `stt_provider: "rumble"`,
`stt_base_url`, `stt_partial_ms: 300`, and the four kwargs keys. Note the live
pilot is still deferred and partials do not reduce time-to-first-word on a batch
model - they only feed partial-driven barge-in/context-warm earlier.

T3 detail: add ONE compact invariant per file: Rumble uses chat.completions
audio_url; client-side pseudo-streaming is generic and opt-in; all kwargs and
selectors absent = legacy. Match existing style.

## Self-Verification Checklist

- All tasks done; cross-references resolve; diff check clean
- No code files touched; no scope creep; no line-number corruption

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Docs accurate to contracts; invariants updated in same change
- No code changes; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit `docs/generated/*`. If the evaluation record needs a correction,
note it in the report; the orchestrator applies it separately.
