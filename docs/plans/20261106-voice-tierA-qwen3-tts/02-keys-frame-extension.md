# Keys frame extension + provider config plumbing - Implementation Leaf (02)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Extend the C1 keys wire shape with vendor/model/base_url/
  voice; store a ProviderConfig on the pipeline; change factory
  signatures to take the full config; dispatch on vendor in
  cmd/meept-voice.
- **Dependencies:** none (03 and 05 consume this leaf's shapes)
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Close the keys-frame half of the wiring gap: today the child can decode
stt_key/tts_key but carries no vendor/model/endpoint info, and the
factories hardcode vendor in cmd/meept-voice. After this leaf, one keys
line fully determines which adapters construct and where they point.

## Context

- internal/voicechild/providers_wiring.go - keysWire (raw JSON decode),
  Child.handleKeysFrame/handleKeys, STTFactory/TTSFactory types,
  Child.constructProviders.
- internal/voicechild/keys.go - KeysFrame struct + redacting
  String/LogValue (leaf-07 contract). The REDACTED values must extend to
  the new fields ONLY IF they could carry secrets - vendor/model/base_url
  are not secrets (base_url is loopback in the local case); document why
  in a comment rather than redacting.
- internal/voicechild/providers/providers.go - ProviderConfig (add
  BaseURL, Voice), ErrUnknownVendor/ErrMissing*Key.
- internal/voicechild/providers/stt_deepgram.go - NewSTT dispatch on
  Vendor; gains BaseURL passthrough (default deepgram cloud URL).
- internal/voicechild/providers/tts_openai.go - NewTTS dispatch; gains
  BaseURL (default api.openai.com) + Voice (default "alloy").
- cmd/meept-voice/main.go - SetProviderFactories closures; dispatch
  Vendor from the config the child now passes (keyless qwen3 + openai).
- Secrets rules (root AGENTS.md): keys live in child memory only; the
  audit script allowlist applies to daemon-side Broker.Value callers
  (leaf 03's business, not yours).

## Verified Anchors

- internal/voicechild/providers_wiring.go:15-18 - factory types today
- internal/voicechild/providers_wiring.go:54-65 - keysWire struct
- internal/voicechild/providers_wiring.go:24-52 - constructProviders
- internal/voicechild/keys.go:54-59 - applyKeys (stores keys only)
- internal/voicechild/providers/providers.go:41-46 - ProviderConfig
- internal/voicechild/providers/providers.go:70-73 - vendor default models
- cmd/meept-voice/main.go:65-78 - hardcoded factory closures

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C1 wire (raw line, new OPTIONAL fields, absent = ""):
{"call_id":"...","event":"keys","stt_key":"...","tts_key":"...",
 "stt_vendor":"...","stt_model":"...","stt_base_url":"...",
 "tts_vendor":"...","tts_model":"...","tts_base_url":"...","tts_voice":"..."}

// internal/voicechild (C3):
type STTFactory func(cfg providers.ProviderConfig, key string) (STTProvider, error)
type TTSFactory func(cfg providers.ProviderConfig, key string) (TTSProvider, error)

// pipeline gains (unexported, playMu-guarded like sttKey/ttsKey):
sttCfg, ttsCfg providers.ProviderConfig

// C2 ProviderConfig adds:
BaseURL string // empty = vendor default
Voice   string // TTS voice name; empty = vendor default
```

### What This Leaf Consumes

- C2 ProviderConfig (this leaf extends it - owner per master: 01 defines
  config fields; you own ProviderConfig).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add BaseURL + Voice to ProviderConfig | Modify: internal/voicechild/providers/providers.go | `go build ./internal/voicechild/...` | fields absent (gate T3 compile RED) | build green | - |
| T2 | Extend keysWire with 6 new optional string fields; decode absent as "" | Modify: internal/voicechild/providers_wiring.go | `go test ./internal/voicechild/ -run TestKeysFrameExtendedFields` | gate T1 RED | gate T1 passes | Given keys line with all fields; Then handleKeysFrame stores them |
| T3 | Store sttCfg/ttsCfg on pipeline in applyKeys; keep redaction; extend KeysFrame struct fields | Modify: internal/voicechild/keys.go | `go test ./internal/voicechild/ -run TestKeys` | store missing | keys + cfg stored; String/LogValue still redact stt_key/tts_key (existing keys_test.go green) | Given KeysFrame; When applyKeys; Then p.sttCfg.TTSVendor == frame field |
| T4 | Factory signatures -> (cfg ProviderConfig, key string); constructProviders passes p cfg; fix all callers/tests | Modify: internal/voicechild/providers_wiring.go, providers_wiring_test.go (factories in tests), internal/voicechild/child.go:153 signature comment only if needed | `go test ./internal/voicechild/` | signature mismatch | package green | Given recording factories; When keys frame; Then factory receives full cfg + key |
| T5 | NewSTT: honor cfg.BaseURL (default existing deepgram URL), cfg.Model (default nova-3) | Modify: internal/voicechild/providers/stt_deepgram.go + stt_test.go additions if needed | `go test ./internal/voicechild/providers/ -run TestSTT` | BaseURL ignored today | custom base URL used in dial; default unchanged when "" | Given cfg.BaseURL="http://127.0.0.1:1"; When Stream dials; Then dial hits that host (assert via injected dialer/transport seam the file already has) |
| T6 | NewTTS: vendor dispatch openai|qwen3; BaseURL default api.openai.com; Voice default "alloy"; qwen3 constructs with key "" | Modify: internal/voicechild/providers/tts_openai.go (rename dispatch core to openAICompatibleTTS), providers.go | `go test ./internal/voicechild/providers/ -run TestTTSDispatch` | qwen3 = ErrUnknownVendor | qwen3+empty key constructs; openai+empty key still ErrMissingTTSKey; vendor "" falls back per today's behavior | Given each vendor case; When NewTTS; Then per Then column |
| T7 | cmd/meept-voice factories dispatch on cfg.Vendor (deepgram STT; openai/qwen3 TTS), pass Model/BaseURL/Voice through | Modify: cmd/meept-voice/main.go | `go build ./cmd/meept-voice/ && go vet ./cmd/meept-voice/` | hardcoded vendor ignores cfg | build green; vet clean | Given cfg from keys frame; When factory invoked; Then vendor-specific constructor called |

## Self-Verification Checklist

- [ ] Gate tests T1 (F1) pass; no gate test modified
- [ ] All six new wire fields optional; old 2-field keys lines still work (existing tests green)
- [ ] Redaction intact; no key material in logs
- [ ] gofmt clean; `go vet ./internal/voicechild/... ./cmd/meept-voice/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** list every path
2. **Verify command + output:** the T2/T4/T6 commands, last lines
3. **Deviations:** any (e.g. where the vendor dispatch lives)

## Review Checklist (For Review Agent)

- [ ] Wire shape matches C1 exactly (field names, optionality, "" defaults)
- [ ] Factory signatures match C3 exactly
- [ ] qwen3 keyless rule (D2) enforced: TTS vendor qwen3 + key "" constructs
- [ ] Secrets: no new log lines carrying key material; LogValue covers new struct
- [ ] No unrelated refactors (OpenAITTS type name may stay; dispatch added cleanly)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep the raw-string "keys" event constant where it lives today
  (child.go:264 comment explains why it is not in voiceframe.Frame).
- tts_openai.go:21 hardcodes the URL in a const; C1/D3 need it
  per-instance now - move to a field, keep the const as default.
- Do NOT implement the daemon-side emission (leaf 03) or the pump (leaf
  05) - signatures only.
