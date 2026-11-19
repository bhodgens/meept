# Kyutai STT registration + docs - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Gate
> tests exist and are committed. Do NOT delete/skip/weaken gate tests. Do NOT
> commit — the orchestrator handles git. Do NOT use read_file on source;
> explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Register Kyutai in `NewSTT`, keep whisper default, update docs
- **Dependencies:** 00T-tests.md at TESTS_VALIDATED
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **Audit references:** F2, F3

## Goal

Add `case "kyutai"` to the STT vendor switch so `stt_provider: "kyutai"`
constructs the adapter from leaf 01. Preserve the default path byte-identically
(whisper stays default). Document the new selector and the CC BY 4.0 / en-fr
limitations.

## Context

- `internal/voicechild/providers/stt_deepgram.go:154-172` — NewSTT switch
- `internal/voicechild/providers/providers.go:71-75` — compile-time pins; add Kyutai pin
- `cmd/meept-voice/main.go:90-104` — factory injection; comment lists deepgram/whisper
- `config/meept.json5:1373,1416-1422` — selector comments
- `docs/workflows/voice-sidecar.md`, `docs/workflows/voicechild.md` — user docs
- root `AGENTS.md`, `internal/voice/AGENTS.md` — same-commit invariant rule

## Verified Anchors

- `internal/voicechild/providers/stt_deepgram.go:154-172` — vendor switch
- `internal/voicechild/providers/providers.go:71-75` — compile pins
- `cmd/meept-voice/main.go:92-96` — STT factory closure comment
- `config/meept.json5:1416-1422` — stt_base_url comment

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
NewSTT case "kyutai": calls newKyutaiSTT(cfg) from leaf 01.
Compile-time pin: var _ voicechild.STTProvider = (*KyutaiSTT)(nil)
```

### What This Leaf Consumes

- C3 NewSTT — `NewSTT(cfg ProviderConfig, key string) (voicechild.STTProvider, error)`
- C5 config selector — `voice.stt_provider`, `voice.stt_base_url` (existing fields)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add kyutai case to NewSTT | Modify: `internal/voicechild/providers/stt_deepgram.go` | `go test ./internal/voicechild/providers/ -run TestNewSTT_Kyutai` | unknown vendor | PASS | Given cfg.Vendor="kyutai"; When NewSTT; Then Kyutai provider constructed |
| T2 | Keep default unchanged | Modify: `internal/voicechild/providers/providers.go` (pin only) | `go test ./internal/voicechild/providers/ -run TestNewSTT_DefaultUnchanged` | n/a | PASS | Given vendor=""/"whisper"; When NewSTT; Then same provider as before |
| T3 | Update comments/docs/invariants | Modify: `cmd/meept-voice/main.go`, `config/meept.json5`, `docs/workflows/voice-sidecar.md`, `docs/workflows/voicechild.md`, root `AGENTS.md`, `internal/voice/AGENTS.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then kyutai selector + CC BY 4.0 / en-fr limits documented |

T1 detail: add `case "kyutai": return newKyutaiSTT(cfg)` before the default
branch. `newKyutaiSTT` comes from leaf 01 (do not implement it here). If leaf
01 has not landed when you run tests, that is expected RED; report it.

T3 detail: generalize the `stt_base_url` comment from "Deepgram-compatible"
to "vendor base URL (Deepgram / whisper / Kyutai)". State that Kyutai weights
are CC BY 4.0, code MIT+Apache-2.0, English/French only, and that whisper
remains the default when `stt_provider` is unset.

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests GREEN
- [ ] Gate test files unmodified (git diff clean)
- [ ] Default provider behavior unchanged
- [ ] Docs updated in same commit scope; AGENTS.md rule satisfied
- [ ] gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] kyutai case present and calls leaf-01 constructor
- [ ] No default-path change; whisper guardrail GREEN
- [ ] Docs state license + language limits; AGENTS.md updated
- [ ] No scope creep into leaf 01/03 files; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

No config schema field is needed: `stt_provider` is a free-form string.
Do not add an enum validator. Do not touch the keys frame plumbing (generic).
