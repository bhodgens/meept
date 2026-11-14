# Leaf 00T — Phase 1 Gate Tests (tests only)

> **DISPATCH INSTRUCTION:** Write TESTS ONLY — no implementation files, no
> production code changes. Tests must fail RED for missing-implementation
> reasons (compile errors on named new symbols are acceptable and expected;
> list them in your report). Do NOT commit. Do NOT run `git commit` or `git add`. Report
> the RED list so the orchestrator can validate coverage against the matrix.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** none (dispatch FIRST)
- **Estimated context:** ~60K
- **Requirements:** R1-R6, R9

## Goal

The full Phase 1 test suite, authored against the FROZEN contracts in.

## Scope

The full Phase 1 test suite, authored against the FROZEN contracts in
../master.md (C1 key formula, C2 store layout, C3 frame shapes, C5 lanes).
Copy the contract text verbatim into the test-file headers — these tests
ARE the cross-leaf agreement check (byte-for-byte ClipKey agreement between
daemon planner and child renderer is pinned HERE, not in either impl leaf).

## Tasks

1. `internal/voice/clipstore/clipkey_contract_test.go` — table: exact
   expected hex keys for pinned inputs (compute via a standalone sha256 in
   the test, mirroring C1 independently of the impl helper).
2. `internal/voiceframe/clip_frames_test.go` — encode/decode of the three
   C3 frame shapes incl. omitempty back-compat (a frame without
   clip_id/clip fields round-trips to today's exact JSON).
3. `e2e/suites/voice-clips/` (new suite dir - HYPHEN, matching the existing
   `voice-lifecycle` / `voice-bargein` / `voice-local` names, `e2e` build
   tag) + `e2e/manifest.json` entry + `path_map` additions, per
   `docs/workflows/e2e-testing.md` and the pre-commit [18/18] rule. Author
   the manifest entry HERE, in wave 0: leaf 01 adds the new
   `internal/voice/clipstore` package and the check requires the mapping to
   already exist. Hermetic scenarios via the standard harness (throwaway
   MEEPT_HOME at a SHORT path, local leg or fake child per harness
   capabilities):
   - sidecar disabled ⇒ `<MEEPT_HOME>/voice_clips` never created (R4).
   - configured pools ⇒ after a call, clip files exist for every pool line
     and no others.
   - render ordering ⇒ `clip_render` for a ccid is observed AFTER that ccid's
     keys line.
   - stored PCM is L16@16000 and byte-equal to the live-lane bytes for the
     same text.
   - voice identity change (edit tts_voice in config, reload) ⇒ new keys
     appear, turn still works.
   - filler fires cached: fake/slow TTS server that WOULD take 2 s — assert
     filler lands under 300 ms after grace expiry (proves PCM came from
     store, not synth).
   - farewell: caller says "bye" ⇒ goodbye audio (played edge) precedes the
     end frame; effects/slot release still exactly-once; a carrier-hangup
     path speaks no farewell.
4. Unit-test stubs in `internal/voice/lanes_test.go` for the C5 precedence
   table (row list > row scalar > global list > global scalar > default)
   and single-line-pool legacy equivalence.
5. `internal/voicechild/` test file pinning: say-with-hit ⇒ zero provider
   calls; say-with-miss ⇒ provider called with frame.Text; clip_ready
   emission on render success/failure.

## Interface Contract (exposes to siblings)

The RED list IS the deliverable; impl leaves 01-05 make it GREEN without
editing this leaf (gate validation rule).

## Self-Verification Checklist

- [ ] `go vet ./internal/voice/... ./internal/voiceframe/... ./internal/voicechild/...` (RED tests may fail only on the named missing symbols)
- [ ] `go test -p 2 ./e2e/... -run VoiceClips` compiles under the e2e tag and fails RED for implementation reasons.
- [ ] No production `.go` file outside `_test.go` touched (git status proof in report).

## Review Checklist (for the orchestrator)

- [ ] Every Test Gate matrix row in orchestrator.md has a named test.
- [ ] No test encodes a contract value that contradicts master.md C1-C5
      (control agent compares the pinned strings, in-session).
