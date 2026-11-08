# Frame + config schema - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back — write once and stop. Report files touched and
> deviations when done.

## Meta

- **Parent:** master.md
- **Scope:** Add the C1 `open` frame + Local/BargeInRms fields to
  internal/voiceframe, and the leg/voice/employee_leg config keys to
  internal/config.
- **Dependencies:** none (wave A)
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voiceframe/frame.go,
  internal/voiceframe/frame_test additions (or frame_local_test.go),
  internal/config/schema.go (VoiceConfig block only), config/meept.json5
  (voice block), internal/configui/sections_voice.go

## Goal

Two additive surfaces, zero behavioral change when unset:

1. voiceframe.Frame gains `EventOpen` and seven optional fields so the
   daemon can open a pipeline on the child with a per-call provider
   profile (master CL1, verbatim below).
2. VoiceConfig gains leg/voice selection keys including a `local` sub
   block (master CL4, verbatim below). Defaults must keep a config-less
   boot byte-identical to today.

## Context

- internal/voiceframe/frame.go is the stdlib-only shared codec. JSON keys
  are byte-exact contract C1; the child decodes them. `Last *bool` shows
  the omitempty-pointer pattern to mirror.
- internal/config/schema.go VoiceConfig is at ~line 5231 (grep
  `type VoiceConfig`). json+toml double tags are mandatory on every field
  (mirror the existing rows).
- config/meept.json5 carries the shipped voice block; add keys with
  default values, JSON5 comments allowed.
- internal/configui/sections_voice.go lists the interactive fields; add
  the new ones the same way.
- internal/config/voice_config_test.go pins defaults; extend it.

## Interface Contracts (From Parent)

### CL1 frame additions (verbatim)

New constant: `EventOpen = "open"` (daemon -> child). New Frame fields,
all omitempty, appended after Reason (keep existing tags untouched):

```go
Local      bool   `json:"local,omitempty"`
SttVendor  string `json:"stt_vendor,omitempty"`
SttModel   string `json:"stt_model,omitempty"`
TtsVendor  string `json:"tts_vendor,omitempty"`
TtsModel   string `json:"tts_model,omitempty"`
TtsVoice   string `json:"tts_voice,omitempty"`
BargeInRms int    `json:"barge_in_rms,omitempty"`
```

Open frame byte-exact shape (key order as emitted by encoding/json:
struct order):

```
{"call_id":"cc1","event":"open","from":"alice","to":"local:host","direction":"local","codec":"L16","sample_rate":16000,"local":true,"stt_vendor":"whisper","stt_model":"base","tts_vendor":"piper","tts_model":"en_US-lessac-medium","barge_in_rms":3000}
```

### CL4 config keys (verbatim)

```
default_leg        "phone"          // phone | local
tts_model          ""
tts_voice          ""
stt_model          ""
local.enabled      false
local.stt_provider ""               // empty = voice.stt_provider
local.tts_provider ""               // empty = voice.tts_provider
local.tts_model    ""
local.tts_voice    ""
local.barge_in_rms 3000
local.name         ""               // empty = hostname (resolved by caller)
```

Go shape: `Local LocalConfig` with json/toml tag `local`,
`type LocalConfig struct { Enabled bool; STTProvider string; TTSProvider string; TTSModel string; TTSVoice string; BargeInRms int; Name string }`
with matching tags (`enabled`, `stt_provider`, `tts_provider`,
`tts_model`, `tts_voice`, `barge_in_rms`, `name`). Default
BargeInRms 3000 set in DefaultConfig.

Per-agent defaults (OQ4 pending; code to this shape regardless, resolve
the home later): `EmployeeLeg []EmployeeLegConfig` tag
`employee_leg`, `type EmployeeLegConfig struct { Employee string
`json:"employee" toml:"employee"`; Leg string `json:"leg" toml:"leg"`;
TTSVoice string `json:"tts_voice" toml:"tts_voice"`; TTSModel string
`json:"tts_model" toml:"tts_model"` }`, default empty slice.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Failing test: decode of the CL1 open line yields all fields; encode round-trips byte-exact | Create: internal/voiceframe/frame_local_test.go | go test -p 2 ./internal/voiceframe/ -run TestOpenFrame | EventOpen + fields missing (compile error) | PASS |
| T2 | Implement CL1 additions in frame.go; existing zero-value omission test proves old frames unchanged | internal/voiceframe/frame.go | go test -p 2 ./internal/voiceframe/ | - | PASS |
| T3 | Failing test: DefaultConfig().Voice has DefaultLeg "phone", empty vendors, Local.Enabled false, Local.BargeInRms 3000 | internal/config/voice_config_test.go (extend) | go test -p 2 ./internal/config/ -run TestVoiceConfig | fields missing (compile error) | PASS |
| T4 | Implement VoiceConfig additions + LocalConfig struct + DefaultConfig rows | internal/config/schema.go | go test -p 2 ./internal/config/ | - | PASS |
| T5 | Shipped template + configui rows for the new keys | config/meept.json5, internal/configui/sections_voice.go | go build ./internal/configui/ && python3 -c "import json5 as j; j.load(open('config/meept.json5'))" or go test -p 2 ./internal/config/ -run TestTemplateParses if such a test exists; else go test -p 2 ./internal/config/ | - | clean |
| T6 | gofmt / vet | - | gofmt -l internal/voiceframe internal/config internal/configui; go vet ./internal/voiceframe/... ./internal/config/... | - | clean |

## Interface Contract (What This Leaf Exposes)

- `voiceframe.EventOpen`, `Frame.Local/SttVendor/SttModel/TtsVendor/TtsModel/TtsVoice/BargeInRms` (CL1 exactly).
- `config.VoiceConfig.DefaultLeg/TTSModel/TTSVoice/STTModel` + `.Local LocalConfig` (CL4 exactly).
- Nothing else. Do not touch socket/splice/dial code.

## Self-Verification Checklist

- [ ] Existing frame tests and wire-byte tests in internal/voicechild pass
      unchanged (run `go test -p 2 ./internal/voicechild/ -run TestPromptFrames`)
- [ ] Zero-value Frame encodes with no new keys (omitempty proof test)
- [ ] A config file without the new keys parses and defaults apply
- [ ] ASCII only; json5 template valid; no ignored errors

## Review Checklist

- [ ] CL1 struct tags match character-for-character
- [ ] CL4 defaults: phone / false / 3000 as specified
- [ ] VoiceConfig zero value still means today's behavior (phone only, no local)
- [ ] No unrelated schema rows touched; no debug artifacts
