# Rumble STT registration + docs - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. Do NOT commit. Explore with
> search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Register Rumble in NewSTT, keep whisper default, update docs
- **Dependencies:** 00T + 00Te at TESTS_VALIDATED
- **Estimated Context:** 40K
- **Concurrency Group:** B
- **Audit references:** F2, F3

## Goal

Add `case "rumble"` to the STT vendor switch so `stt_provider: "rumble"`
constructs the adapter from leaf 01. Preserve default path byte-identically.
Document the new selector, the batch-only limitation, and the GPU cap.

## Context

- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch
- `internal/voicechild/providers/providers.go:71-75` — compile pins
- `cmd/meept-voice/main.go:92-96` — factory comment
- `config/meept.json5:1416-1422` — stt_base_url comment
- `docs/workflows/voice-sidecar.md`, `docs/workflows/voicechild.md` — docs
- root `AGENTS.md`, `internal/voice/AGENTS.md` — same-commit invariant rule
- `docs/research/rumble2flash-review.md` — accuracy/latency facts

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:154-172` — vendor switch
- `internal/voicechild/providers/providers.go:71-75` — compile pins
- `config/meept.json5:1416-1422` — stt_base_url comment

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
NewSTT case "rumble": calls newRumbleSTT(cfg) from leaf 01.
Compile-time pin: var _ voicechild.STTProvider = (*RumbleSTT)(nil)
```

### What This Leaf Consumes

- C3 NewSTT, C5 config selector

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add rumble case to NewSTT | Modify: `internal/voicechild/providers/stt_deepgram.go` | `go test ./internal/voicechild/providers/ -run TestNewSTT_Rumble` | unknown vendor | PASS | Given cfg.Vendor="rumble"; When NewSTT; Then Rumble provider returned |
| T2 | Keep default unchanged + pin | Modify: `internal/voicechild/providers/providers.go` | `go test ./internal/voicechild/providers/ -run TestNewSTT_DefaultUnchanged` | n/a | PASS | Given vendor=""/"whisper"; When NewSTT; Then unchanged |
| T3 | Update comments/docs/invariants | Modify: `cmd/meept-voice/main.go`, `config/meept.json5`, `docs/workflows/voice-sidecar.md`, `docs/workflows/voicechild.md`, root `AGENTS.md`, `internal/voice/AGENTS.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then rumble selector, batch-only, GPU cap, Apache-2.0 documented |

T3 detail: generalize the `stt_base_url` comment from "Deepgram-compatible"
to "vendor base URL (Deepgram / whisper / Rumble / Kyutai)". State explicitly:
Rumble is batch (no partial transcripts), English WER 6.60, Apache-2.0,
needs a warm vLLM sidecar with `--gpu-memory-utilization 0.20` beside
qwen3-tts, and whisper remains default when `stt_provider` is unset.

## Self-Verification Checklist

- [ ] Tasks implemented; gate tests GREEN; gate files unmodified
- [ ] Default path unchanged; docs + AGENTS.md updated
- [ ] gofmt clean; no scope creep into 01/02

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] rumble case present; no default-path change
- [ ] Docs state batch-only + GPU cap + license; AGENTS.md updated
- [ ] No scope creep; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

No config schema field needed: `stt_provider` is free-form. Do not add an enum
validator. Do not touch keys frame plumbing (generic).
