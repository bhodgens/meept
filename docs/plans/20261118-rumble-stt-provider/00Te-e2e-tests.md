# Rumble STT - E2E Test Gate Leaf

> **For the implementing agent:** Write TESTS ONLY for F1 and F4. Do NOT write
> implementation. Do NOT commit. Tests must fail RED for
> missing-implementation reasons or be honestly skipped with a documented
> reason. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** E2E gate tests for Rumble keys→provider and streaming capability
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A
- **Audit references:** F1, F4

## Goal

Provide e2e gate coverage proving a `stt_vendor: "rumble"` keys line constructs
the provider, and record the streaming-capability decision (F4) so the plan
does not claim incremental input that does not exist.

## Context

- `e2e/suites/voice-lifecycle/` — keys-provider scenarios
- `e2e/manifest.json:4838-4847` — `voice-keys-providers-01` template
- `e2e/manifest.json:523-527` — path_map for `internal/voicechild/`
- `docs/research/rumble2flash-review.md:45` — streaming analysis

## Verified Anchors

- `e2e/manifest.json:4838-4847` — keys-provider scenario template
- `internal/daemon/voice_keys.go:167-168` — STT vendor assignment

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e scenario ids: voice-keys-rumble-01, rumble-streaming-probe
```

### What This Leaf Consumes

- C4 keys frame (`stt_vendor`/`stt_base_url`), C5 config selector

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Keys line constructs rumble | Create: `e2e/suites/voice-lifecycle/rumble_stt_test.go`; Modify: `e2e/manifest.json` | `make e2e-fast-area AREA=voice-lifecycle` | skip/RED | PASS | Given fake vLLM server + keys line stt_vendor=rumble; When call opens; Then provider constructed and Final transcript arrives |
| T2 | Register scenarios in manifest | Modify: `e2e/manifest.json` | `python3 -c 'import json;json.load(open("e2e/manifest.json"))'` | n/a | PASS | Given manifest; When parsed; Then both scenario ids present |
| T3 | Streaming capability probe | Modify: `e2e/suites/voice-lifecycle/rumble_stt_test.go` | `make e2e-fast-area AREA=voice-lifecycle` | n/a | PASS/Gated | Given `stream=true` request; When server responds; Then record whether partial transcripts arrive before full audio; if unsupported use `t.Skip("Rumble batch-only: vllm stream=True emits tokens after full audio")` |

T1 detail: mirror `voice-keys-providers-01` harness. Fake vLLM server on
loopback, set `stt_base_url` to it, assert child logs vendor=rumble (no keys
leaked) and a Final=true transcript reaches the talk loop.

T3 detail: this is a decision record, not a hard requirement. The `t.Skip`
must cite `docs/research/rumble2flash-review.md:45`. Gate passes either way;
the control agent records the result in the Test Gate F4 row.

## Self-Verification Checklist

- [ ] F1 and F4 covered; manifest parses
- [ ] Skip reason documented with source citation if batch-only
- [ ] No implementation files touched; no commit
- [ ] No line-number corruption

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations (include F4 streaming verdict)

## Review Checklist (For Review Agent)

- [ ] Scenarios registered; build tags correct; manifest path_map valid
- [ ] Streaming probe honest (skip reason cites research)
- [ ] No implementation code; no gate weakening

Output: APPROVED or specific gaps with file + line references.

## Notes

Keep hermetic: no real vLLM server required in CI. Live pilot on socrates is
manual and outside this tree.
