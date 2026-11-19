# Operator config for kwargs and partial streaming - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. Do NOT commit. Explore with
> search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Add C6 operator config fields and template block; wire daemon emit
- **Dependencies:** 01 (C4 wire fields)
- **Estimated Context:** 40K
- **Concurrency Group:** C
- **Audit references:** F3, F5

## Goal

Expose the new selectors in operator config so the daemon keys producer sends
them. All fields additive: absent = zero/nil = legacy byte-identical boot.

## Context

- `internal/config/schema.go:5630-5650` - VoiceConfig area
- `internal/daemon/voice_keys.go:160-215` - daemon emit (leaf 01 added wire fields)
- `config/meept.json5:1415-1425` - voice config template
- C6 in ../master.md is authoritative.

## Verified Anchors

- `internal/config/schema.go` - VoiceConfig struct
- `internal/daemon/voice_keys.go` - keysWire producer
- `config/meept.json5` - stt_base_url comment area

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// VoiceConfig gains (C6):
ChatTemplateLanguage string `json:"chat_template_language"`
ChatTemplateDigits   *bool  `json:"chat_template_digits,omitempty"`
ChatTemplateFiller   *bool  `json:"chat_template_filler,omitempty"`
ChatTemplateVerbatim *bool  `json:"chat_template_verbatim,omitempty"`
STTPartialMs         int    `json:"stt_partial_ms"`
```

### What This Leaf Consumes

- C4 keys frame, C6 - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add C6 fields to VoiceConfig | Modify: `internal/config/schema.go` | `go build ./internal/config/ && go test ./internal/config/ -count=1` | undefined fields | PASS | Given config load; When selectors absent; Then zero/nil and all existing tests pass |
| T2 | Wire daemon emit | Modify: `internal/daemon/voice_keys.go` (complete leaf 01's emit) | `go test ./internal/daemon/ -run 'TestKeys|TestRumble' -count=1` | undefined fields | PASS | Given config selectors; When keys line emitted; Then C4 fields carry values |
| T3 | Template block | Modify: `config/meept.json5` | `python3 -c 'import json5;json5.load(open("config/meept.json5"))' 2>/dev/null || git diff --check` | n/a | PASS | Given template; When read; Then commented block documents selectors with safe defaults |

T3 detail: add a commented block near `stt_base_url` with `chat_template_language`,
`chat_template_digits`, `chat_template_filler`, `chat_template_verbatim`,
`stt_partial_ms`; state: Language is a NAME ("English"), partial_ms=0 keeps
legacy batch behavior, generic decorator works on any batch provider but do not
enable on native-partial vendors (deepgram), whisper remains default.

## Self-Verification Checklist

- All tasks implemented; config + daemon tests pass; gate tests untouched
- All fields additive with zero/nil defaults; template parses; gofmt clean
- No scope creep into 01/02/04 files

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- C6 fields exact; absent-on-wire stays legacy; daemon emit complete
- Template documents opt-in posture; no gate weakening

Output: APPROVED or specific gaps with file + line references.

## Notes

If leaf 01 has not landed the C4 wire fields when you start, wait for the
orchestrator; do not add duplicate wire fields.
