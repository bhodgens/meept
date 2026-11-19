# Rumble streaming tree - Unit Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1-F5. Do NOT write
> implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons (undefined symbols / wrong request shape),
> not syntax errors. Explore with search_files/terminal, not read_file on
> source. Write files once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Unit gate tests for request-shape fix, pseudo-stream, kwargs, keys frame
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** F1, F2, F3, F4, F5

## Goal

Provide executable RED unit tests defining: the corrected chat.completions
request shape, kwargs in the body, generic pseudo-stream partial/final behavior,
default-path preservation, and additive keys-frame selectors.

## Context

Contracts C1-C6 live in ../master.md. Key anchors:
- `internal/voicechild/providers/stt_rumble.go` - current (buggy) JSON POST
- `internal/voicechild/providers/stt_rumble_test.go` - existing gate test (DO NOT EDIT)
- `internal/voicechild/providers_wiring.go:17-30` - ProviderConfig
- `internal/voicechild/providers_wiring.go:100-135` - keysWire decode
- `internal/daemon/voice_keys.go:35-55` - daemon keysWire producer

## Verified Anchors

- `internal/voicechild/providers/stt_rumble.go:257-280` - transcribe POST body (buggy)
- `internal/voicechild/providers/stt_rumble.go:27-29` - rumbleTranscriptionsPath
- `internal/voicechild/providers_wiring.go:17-24` - ProviderConfig
- `internal/voicechild/providers/providers.go:43-45` - ProviderConfig alias

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test files only; no production symbols.

### What This Leaf Consumes

- C1 STTProvider, C2 ProviderConfig, C3 request shape, C4 keys frame, C5 decorator, C6 config

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Request shape: chat.completions audio_url, parse choices | Modify: `internal/voicechild/providers/stt_rumble_test.go` (NEW test funcs ONLY, do not edit existing) | `go test ./internal/voicechild/providers/ -run TestRumbleRequestShape -count=1` | wrong path/body | PASS | Given fake chat.completions server; When Stream; Then POST to /v1/chat/completions with messages[0].content[0].audio_url.data URI and transcript from choices[0].message.content |
| T2 | kwargs in request body | Modify: `internal/voicechild/providers/stt_rumble_test.go` (NEW funcs) | `go test ./internal/voicechild/providers/ -run TestRumbleTemplateKwargs -count=1` | undefined C2 fields | PASS | Given C2 kwargs set; When POST; Then body has chat_template_kwargs with exactly the set fields; nil fields omitted |
| T3 | Pseudo-stream partials then final | Create: `internal/voicechild/providers/pseudostream_test.go` | `go test ./internal/voicechild/providers/ -run TestPseudoStreamEmitsPartialsThenFinal -count=1` | undefined newPseudoStreamSTT | PASS | Given fake base provider returning text per call + PartialIntervalMs; When PCM fed over time; Then >=1 Final=false then exactly one Final=true on close |
| T4 | Default path unchanged | Create: `internal/voicechild/providers/pseudostream_test.go` (add func) | `go test ./internal/voicechild/providers/ -run TestNewSTTDefaultNoDecorator -count=1` | n/a guardrail | PASS | Given PartialIntervalMs=0; When NewSTT; Then no decorator (base provider type returned exactly) |
| T5 | Keys frame selectors | Create: `internal/voicechild/keys_rumble_selectors_test.go` | `go test ./internal/voicechild/ -run TestKeysFrameRumbleSelectors -count=1` | undefined C4 fields | PASS | Given raw keys line with selectors; When handleKeysFrame; Then KeysFrame carries them; absent line = zero/nil |

T1 detail: fake server asserts URL path suffix `/v1/chat/completions`, decodes JSON,
finds `messages[0].content[0].audio_url.url` prefix `data:audio/wav;base64,`,
no Authorization header, and returns `{"choices":[{"message":{"content":"hello there"}}]}`.
Assert the emitted Transcript text is "hello there" and Final=true.

T2 detail: set C2 fields Language="English", Digits=&false, Filler=&true,
Verbatim=&false; assert body `chat_template_kwargs` has those exact values.
Then a second row with all nil/empty asserts the key is absent.

T3 detail: use STTBaseFactory that returns a fake STTProvider whose Stream emits
one Final with a counter-derived text. Feed 3 chunks separated by >PartialIntervalMs
then close. Assert partial transcripts arrive BEFORE close and exactly one final.
Use timeouts <=3s.

## Self-Verification Checklist

- F1-F5 covered; RED for missing implementation only
- No production files touched; no commit; no git add; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + last 20 lines (showing RED)
3. Deviations

## Review Checklist (For Review Agent)

- Every F row covered; RED reasons correct; behavior asserted not impl detail
- No implementation code; no line-number corruption; existing gate tests untouched

Output: APPROVED or specific gaps with file + line references.

## Notes

The prior tree's `TestRumbleSTTStream` will go RED when the transport changes;
this gate does NOT edit it. Leaf 01 updates that existing test as part of the
bug fix (documented deviation in its brief).
