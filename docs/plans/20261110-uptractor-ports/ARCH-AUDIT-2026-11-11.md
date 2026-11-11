# ARCH-AUDIT-2026-11-11 — Uptractor Ports Tree (docs/plans/20261110-uptractor-ports)

Post-authoring audit (read-only subagent over meept HEAD @ 436f432 + uptractor-jobfile reference branches). Disposition table: every finding -> amendment applied.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| B1 | BLOCKER | internal/intake already exists (GitHub intake-factory projection, wired from daemon/intake_wiring.go) — leaf 05's package name collides | Amended: leaf 05 -> internal/voiceintake (all task paths, verify commands, self-check); 00T gate test file path moved; master child index + coding conventions note |
| B2 | BLOCKER | Leaf 05 premised on an employee preset mechanism that does not exist (ApplyPreset only in config/presets.go:135 + agent/team_presets.go:109); tools_allowed enforcement is real at employee/enforcement.go:678 | Amended: leaf 05 context + T3 rewritten as config-gate + prompt-extension seam riding existing tools_allowed enforcement; enforcement anchor cited |
| R1 | RISK | Manager maps carry no timestamps and expose no session enumeration — reaper cannot snapshot sessions as planned | Amended: reaper keeps its OWN {ccid: startedAt} map fed by AddAnsweredObserver/AddTerminalObserver lanes; no Manager state/constructor change (master architecture + C3, leaf 02 context/T1, 00T T3) |
| R2 | RISK | HandleCallEvent hangup branch hard-codes reason "call.hangup" (manager.go:417) — frozen reaper reasons can't ride it | Amended: reaper releases via FailOutcome(ccid, outcome, reason) (manager.go:539) — same releaseOnce funnel; master architecture + C3, leaf 02 anchors/T2, 00T T3, Test Gate row |
| R3 | RISK | Transcript persistence is in voice/binding.go:297-316 (AppendTranscript), not voicechild; normalizing at the pipeline would persist normalized text; dedupe (lastFinalText :498) semantics would shift | Amended: leaf 03 T3 pins normalize-emitPrompt-only, raw-capture for binding, dedupe compares raw; review checklist updated |
| R4 | RISK | Adding GetCallStatus to telnyx.CallController would break every existing test fake | Amended: master C3 + conventions pin GetCallStatus on *Client + standalone CallStatusChecker; leaf 02 checklist adds the prohibition |
| R5 | RISK | Canonical test-vector drift: docs say "summer plans", source table says "summer planes" (SPEECH-ROBUSTNESS.md:79) under a verbatim requirement | Amended: master Test Gate row, 00T T6, leaf 03 checklist — now "summer planes" |
| M1 | MINOR | SweepStaleLive cited at :178 (doc comment); func at :198 | Amended: 00T, 04 anchors |
| M2 | MINOR | Leaf 07 internal/voice/eval coexists with existing top-level internal/eval | Amended: master conventions note (explicit import paths) |
| M3 | MINOR | Leaf 03's grep hint ("final" string) finds nothing — code uses Final bool | Amended: leaf 03 anchor to grep "tr.Final" with pipeline.go:35/:458/:498/:523 map |
| M4 | MINOR | internal/voice does not import internal/ledger today | Amended: master conventions; leaf 04 T4 moved the commitment filing adapter to internal/daemon wiring behind a CommitmentFiler seam |
| — | VERIFIED | 16/16 plan anchors point at correct symbols (only the 2 minor drifts above); reaper defaults 30s/60s/30m verbatim from source (reaper.go:38-61); callstatus.go shapes match C1/C2; SPEECH-ROBUSTNESS.md canonical table exists on fix/speech-robustness; no import-cycle risk (util/fuzzy, ledger, callerid all stdlib-only); internal/config + internal/util exist as targeted | No change |

Anchors verified accurate: manager.go:218/:271/:321, callstore.go:41/:92, ledger/store.go:170/:184/:316, contact/store.go:38, telnyx/callcontrol.go:77, telnyx/client.go:53/:70, voice_wiring.go:457, ledger_wiring.go:59.
