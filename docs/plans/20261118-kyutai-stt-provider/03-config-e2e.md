# Kyutai STT config + e2e - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Gate
> tests exist and are committed. Do NOT delete/skip/weaken gate tests (a
> documented `t.Skip` for an unavailable external server is the one
> exception, and must match the gate leaf's T5 note). Do NOT commit.
> Explore with search_files/terminal, not read_file on source. Write once.

## Meta

- **Parent:** ../master.md
- **Scope:** E2E scenario proving a kyutai keys line constructs the provider
- **Dependencies:** 00T-tests.md at TESTS_VALIDATED; 01 for full run
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Audit references:** F5

## Goal

Add an e2e scenario to `e2e/suites/voice-lifecycle/` modelled on
`voice-keys-providers-01` that proves `stt_provider: "kyutai"` in the keys
line constructs the Kyutai provider, and that a transcript with `Final:true`
arrives. Register it in `e2e/manifest.json`.

## Context

- `e2e/suites/voice-lifecycle/` — keys-provider scenarios
- `e2e/manifest.json:4838-4847` — `voice-keys-providers-01` template
- `e2e/manifest.json:523-527` — path_map for `internal/voicechild/`
- `internal/daemon/voice_keys.go:45-47,167-168,206` — keys producer
- `internal/voicechild/providers_wiring.go:89-91,177-179` — keys consumer

## Verified Anchors

- `e2e/manifest.json:4838-4847` — keys-provider scenario template
- `internal/daemon/voice_keys.go:167-168` — STT vendor/model assignment
- `internal/voicechild/providers_wiring.go:177-179` — KeysFrame mapping

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e scenario id: voice-keys-kyutai-01
// Registered in e2e/manifest.json under suite voice-lifecycle
```

### What This Leaf Consumes

- C4 keys frame — `stt_vendor` / `stt_base_url` ride unchanged
- C5 config selector — `voice.stt_provider: "kyutai"`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Register scenario in manifest | Modify: `e2e/manifest.json` | `python3 -c 'import json;json.load(open("e2e/manifest.json"))'` | n/a | PASS | Given manifest; When parsed; Then scenario present with correct suite/paths |
| T2 | Write e2e scenario | Create: `e2e/suites/voice-lifecycle/kyutai_stt_test.go` | `make e2e-fast-area AREA=voice-lifecycle` | skip/RED | PASS | Given keys line with stt_vendor=kyutai + fake WS server; When call opens; Then provider constructed and Final=true transcript observed |
| T3 | Validate manifest scenario wiring | Modify: `e2e/manifest.json` (if needed) | `make classifier-eval-selftest` or manifest check | n/a | PASS | Given new scenario; When manifest validated; Then no orphan paths |

T2 detail: copy the harness shape from `voice-keys-providers-01`. Use a fake
Kyutai WS server on loopback; set `stt_base_url` to its address. Assert the
child logs provider construction with vendor=kyutai (no keys leaked) and
that a final transcript reaches the talk loop. If the real Kyutai protocol
cannot be faked within the harness, use the documented `t.Skip` guard.

## Self-Verification Checklist

- [ ] Scenario registered; manifest parses; no path_map orphan
- [ ] Gate test T5 unmodified (or documented skip matches gate note)
- [ ] `make e2e-fast-area AREA=voice-lifecycle` passes
- [ ] No scope creep into leaves 01/02 files

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Manifest scenario present with correct suite + paths
- [ ] E2E test exercises real keys→provider seam, not a mock of it
- [ ] Gate test file unmodified
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Keep the scenario hermetic: no external Kyutai server required for CI.
The optional live pilot on socrates is a separate manual step, not this leaf.
