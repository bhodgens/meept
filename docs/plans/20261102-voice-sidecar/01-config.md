# Voice config schema - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate test ALREADY EXISTS
> (parked .staged from leaf 00T) - make it pass; do NOT delete, skip,
> weaken, or rewrite any gate test. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. Write
> once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** `voice` config section: schema struct, defaults, template, configui section
- **Dependencies:** none (gate test 00T T1)
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/config/schema.go (voice block only), internal/config/defaults (voice entries only), config/meept.json5 (voice section only), internal/configui/sections_voice.go, internal/configui/sections.go (one case line)

## Goal

Add the C3 config surface so every later leaf reads typed config. All keys
default safe: enabled=false, supervisor on, loopback-only (no public URL
default).

## Context

Config is JSON5; structs in internal/config/schema.go with json+toml tags;
defaults in DefaultConfig(); template in config/meept.json5 with comments;
interactive editor sections in internal/configui/sections_<name>.go plus a
switch case in sections.go (pattern: sections_plugins.go). Schema changes
require `make docs-generate` (orchestrator runs it at wave close, not the
leaf).

## Verified Anchors

- internal/config/schema.go - STT STTConfig / TTS TTSConfig fields (client-side speech precedent; voice is a NEW sibling section, do not extend stt/tts)
- config/meept.json5:1114/1144 - stt/tts template blocks (placement precedent)
- docs/reference/generated/config.md:1734 - generated Config struct doc (regenerated later)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go
type VoiceConfig struct { /* every C3 key, json+toml tags, names EXACT */ }
// on root Config: Voice VoiceConfig `json:"voice" toml:"voice"`
// DefaultConfig(): every C3 default (master.md C3 list, verbatim values)
```

### What This Leaf Consumes

- C3 (master.md) - key names and defaults, verbatim

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate test; confirm RED | mv internal/config/voice_config_test.go.staged -> .go | go test -p 2 ./internal/config/ -run TestVoiceConfigDefaults | undefined: VoiceConfig | RED confirmed | Given parked test; When activated; Then compile error names VoiceConfig |
| T2 | VoiceConfig struct + root field + defaults | Modify: internal/config/schema.go | same | - | PASS | Given DefaultConfig(); When Voice read; Then enabled=false, idle_stop_seconds=30, supervisor_enabled=true, supervisor_timeout_ms=400, timeout_secs=30, max_call_minutes=15, silence_prompt_seconds=45, reorder_window_frames=10, reorder_gap_ms=60, filler=["Hold on, let me check."], public_stream_url="" |
| T3 | Template section with comments (ASCII) | Modify: config/meept.json5 | go test -p 2 ./internal/config/ (template parse tests) | - | PASS | Given shipped template; When parsed; Then voice section present, enabled false, every key commented |
| T4 | ConfigUI section | Create: internal/configui/sections_voice.go; Modify: internal/configui/sections.go | go build ./internal/configui/ | - | PASS | Given `meept config voice`; When editor opens; Then section renders enabled + provider + limits fields |
| T5 | Format + vet | - | gofmt -l internal/config internal/configui; go vet ./internal/config/ ./internal/configui/ | - | empty | Given touched files; When checked; Then clean |

## Self-Verification Checklist

- [ ] Gate test passing, untouched
- [ ] C3 key names byte-exact (json tags)
- [ ] Defaults match master.md verbatim
- [ ] Template ASCII-only; no behavior baked into logic that belongs in config
- [ ] No scope creep (no voice logic in this leaf)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Every C3 key present with exact tag name and default
- [ ] configui switch case registered (both files, same commit)
- [ ] Template comments accurate; enabled=false default
- [ ] No docs-generate run needed inside the leaf (orchestrator handles)

Output: APPROVED or specific gaps.

## Notes

allowed_numbers is []string of E.164. Validate format lazily (at dial /
answer time in later leaves), not at config load, so an operator can save
a partially edited list.
