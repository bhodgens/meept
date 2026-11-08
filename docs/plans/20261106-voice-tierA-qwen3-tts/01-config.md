# Voice config fields - Implementation Leaf (01)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Add STT/TTS vendor, model, base_url, and voice fields to
  VoiceConfig and document them in the shipped config template.
- **Dependencies:** none
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

The daemon's `[voice]` block gains the knobs that select and aim the
STT/TTS providers: vendor name, model override, TTS base URL (local
Qwen3-TTS server), and TTS voice. Everything defaults to "" (today's
behavior: hardcoded vendors in cmd/meept-voice).

## Context

- internal/config/schema.go - all config structs (VoiceConfig at line
  ~5233). Fields follow `json:"x" toml:"x"` dual tags.
- config/meept.json5 - shipped template; [voice] block around line 1340.
  This repo treats config templates as drift-checked
  (scripts/config-drift-check.py) - new keys need a `// drift:` comment
  style consistent with siblings (inspect neighboring keys and copy).
- AGENTS.md root rule: config templates in config/ ship via make
  install/sync-config; keep template and schema in the same change.

## Verified Anchors

- internal/config/schema.go:5233-5259 - type VoiceConfig struct
- config/meept.json5:1340-1342 - stt_provider/tts_provider template lines

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go, type VoiceConfig adds (exact names/tags):
STTModel  string `json:"stt_model"   toml:"stt_model"`
TTSModel  string `json:"tts_model"   toml:"tts_model"`
TTSBaseURL string `json:"tts_base_url" toml:"tts_base_url"`
TTSVoice  string `json:"tts_voice"   toml:"tts_voice"`
// Semantics (consumed by leaf 03's wire mapping, C4):
//   "" = default (vendor defaults; no keys frame field value override)
```

### What This Leaf Consumes

- None (leaf is the base of group A).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the four fields to VoiceConfig with dual tags | Modify: internal/config/schema.go (~line 5238, after TTSProvider) | `go test ./internal/config/ -run TestVoiceTTSFields` | gate test fails: fields absent | gate test passes | Given VoiceConfig{STTModel:"m"}; When json.Marshal; Then "stt_model":"m" round-trips |
| T2 | Template documents the four keys in [voice] with comments | Modify: config/meept.json5 [voice] block | `grep -c "tts_base_url" config/meept.json5` returns 1; `make config-bootstrap` dry-run unaffected | grep returns 0 | keys present with brief comments | Given template; When user runs config sync; Then keys appear commented-default "" |
| T3 | Sanity: full config package tests still green | - | `go test ./internal/config/` | n/a | PASS | Given change; When package suite runs; Then green |

## Self-Verification Checklist

- [ ] Gate tests pass; no gate test modified (`git diff --stat` clean of gate files)
- [ ] Fields at exact names/tags per contract
- [ ] Template keys commented in house style
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** internal/config/schema.go, config/meept.json5
2. **Verify command + output:** T1/T3 commands, last lines
3. **Deviations:** none expected

## Review Checklist (For Review Agent)

- [ ] Field names/tags match C2 + leaf contract exactly
- [ ] No other VoiceConfig behavior changed
- [ ] Template comments accurate ("" default = vendor default)
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT add an STTBaseURL unless the gate test names one (it does not);
  Deepgram stays cloud-only this tier.
- Do NOT touch PiperTTSConfig or TTSPlaybackConfig (different subsystem,
  meep TTS surface - out of scope).
