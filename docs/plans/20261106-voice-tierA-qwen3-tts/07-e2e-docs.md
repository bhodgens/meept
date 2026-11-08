# E2E coverage + docs closeout - Implementation Leaf (07)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Gate tests exist for unit coverage; THIS leaf owns e2e scenario updates
> and documentation. Do NOT delete, skip, weaken, or rewrite any gate
> test. Do NOT commit - the orchestrator handles all git operations
> after review. Do NOT use read_file on existing source files - explore
> with search_files or terminal cat. After writing a file, do NOT read
> it back - write once and stop. Report what you built, files touched,
> and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Extend the voice e2e suites to cover the wired pipeline
  (keys -> providers -> STT prompts -> barge-in), update the voice docs,
  and record the as-built state including the D1 deviation.
- **Dependencies:** leaves 01-06 all REVIEWED (runs on the integrated
  worktree)
- **Estimated Context:** 35K
- **Concurrency Group:** D (last)

## Goal

Prove the whole chain under the hermetic e2e harness and leave the docs
truthful: docs/workflows/voice-sidecar.md as-built gains the Tier A
additions (config-driven providers, local Qwen3-TTS, STT-partial
barge-in, ONNX no longer required for Tier A).

## Context

- e2e policy (root AGENTS.md): new feature tests go in the hermetic e2e
  tier. Existing suites: e2e/suites/voice-bargein, voice-concurrency,
  voice-lifecycle. Manifest path_map already routes internal/voicechild
  + cmd/meept-voice to all three voice suites - no manifest change
  expected unless you add a scenario (then update e2e/manifest.json
  inventory in the same change).
- Harness: e2e/harness spawns scratch meept-daemon + meept binaries into
  $TMPDIR/meept-e2e-bin* (never hand-roll spawns). Follow existing voice
  suite patterns exactly - read one suite end-to-end before writing.
- Docs: docs/workflows/voice-sidecar.md is the spec + as-built authority
  (referenced by internal/voice/AGENTS.md). Update AS-BUILT sections
  only; spec rev stays. Also touch docs/workflows/voice.md if it
  describes providers.
- AGENTS.md maintenance rule (root): any invariant this tree changed
  (keys frame shape, barge-in trigger source, ONNX no longer required)
  must be reflected in internal/voice/AGENTS.md + internal/voicechild
  contract comments in the same change.

## Verified Anchors

- e2e/suites/voice-bargein/, voice-lifecycle/, voice-concurrency/
- e2e/manifest.json - path_map entries for internal/voice*, cmd/meept-voice
- docs/workflows/voice-sidecar.md - spec rev 2 + as-built sections
- internal/voice/AGENTS.md:26-29 (filler/greeting), :79-89 (supervisor),
  :97-102 (keys frame + secrets lanes) - lines to amend for the new
  wire shape and trigger source
- cmd/meept-voice/main.go:10-12 - ONNX header comment (now describes
  VAD as the barge-in source - D1 changed that)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// e2e additions (scenario names descriptive, placed in existing suites):
voice-lifecycle:  scenario "keys-to-providers" - daemon+child pair,
                  config with fake/loopback provider endpoints, assert
                  keys line flows and providers construct (call proceeds
                  past stub audio).
voice-bargein:    scenario "partial-bargein" - scripted STT partial
                  during say -> playback stops, fresh final starts a new
                  turn whose reply reflects the interrupted prompt.
// docs:
voice-sidecar.md: as-built + config reference rows for stt_model,
                  tts_model, tts_base_url, tts_voice; Tier A note:
                  Qwen3-TTS via OpenAI-compatible streaming endpoint,
                  keyless; barge-in trigger = STT partial (ONNX/VAD
                  optional future).
internal/voice/AGENTS.md: amend keys-frame bullet (extended fields),
                  supervisor/ONNX notes per D1.
```

### What This Leaf Consumes

- The integrated tree (01-06) - all contracts live.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | e2e scenario keys-to-providers in voice-lifecycle | Create/Modify: e2e/suites/voice-lifecycle/ (+ manifest inventory if new scenario) | `make e2e-fast-area AREA=voice-lifecycle` | scenario absent | suite green incl. new scenario | Given scratch daemon with voice enabled + loopback provider fakes; When a call sets up; Then keys line observed, providers construct, stub-audio-only assertions replaced |
| T2 | e2e scenario partial-bargein in voice-bargein | Modify: e2e/suites/voice-bargein/ | `make e2e-fast-area AREA=voice-bargein` | scenario absent | suite green incl. new scenario: playback stops on partial; reply follows the interrupting utterance | Given live child with scripted STT; When partial during say + final after; Then interrupt observed and second turn reflects new prompt |
| T3 | voice-concurrency still green (no regression) | - | `make e2e-fast-area AREA=voice-concurrency` | n/a | green | - |
| T4 | Docs: voice-sidecar.md as-built + config rows + Tier A notes; voice.md provider section if it exists | Modify: docs/workflows/voice-sidecar.md, docs/workflows/voice.md | manual review + `grep tts_base_url docs/workflows/voice-sidecar.md` | docs lack new fields | docs describe the shipped shape (D1 deviation called out) | - |
| T5 | AGENTS.md sync: internal/voice/AGENTS.md keys bullet + ONNX note; cmd/meept-voice header comment | Modify: internal/voice/AGENTS.md, cmd/meept-voice/main.go (comment only) | manual review | stale contract text | AGENTS.md matches shipped behavior | - |

## Self-Verification Checklist

- [ ] All three voice suites green on the integrated worktree
- [ ] New scenarios named in the manifest inventory (if added)
- [ ] Docs + AGENTS.md match shipped behavior; D1 deviation explicit
- [ ] No gate test modified
- [ ] gofmt clean for any Go touched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** e2e suites, manifest, docs, AGENTS.md
2. **Verify command + output:** T1-T3 commands, last lines
3. **Deviations:** any scenario/harness adjustment

## Review Checklist (For Review Agent)

- [ ] Scenarios use the harness (no raw exec.Command spawns)
- [ ] Assertions observe real frame flow (keys line, interrupt frame), not mocks of mocks
- [ ] Docs claims match code (spot-check 2-3 against source)
- [ ] AGENTS.md maintenance rule satisfied
- [ ] Manifest inventory updated if scenarios added

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT attempt a live Qwen3-TTS e2e - vendor fakes only in the
  hermetic tier; live smoke is the user's manual gate (master Integration
  Test Plan step 4).
- Do NOT modify splice/telnyx files (R4 parallel-plan boundary).
