# e2e suites + manifest - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The parked e2e gate tests from 00T
> T10 activate here. Do NOT commit. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. Write once, do not
> read back.

## Meta

- **Parent:** master.md
- **Scope:** Hermetic e2e suites voice-lifecycle, voice-bargein, voice-concurrency; e2e/manifest.json entries (suites[], scenarios, path_map for internal/voice, internal/voicechild, internal/voiceframe, cmd/meept-voice)
- **Dependencies:** leaf 14 (wired daemon)
- **Estimated Context:** 85K
- **Concurrency Group:** E
- **Files this leaf owns:** e2e/suites/voice-*/*, e2e/manifest.json (MERGE, never overwrite - hottest shared-write file)

## Goal

Prove the whole chain in one boot: scratch daemon + fake Telnyx (httptest
webhook server + websocket media server) + fake child (speaks C1/C2 over
the real sockets; NO jargo, NO ONNX, NO network) -> inbound call answers,
greeting plays, prompt->say round-trip, barge-in clears, hangup tears
down, two concurrent calls to two numbers stay isolated.

## Context

Harness: e2e/harness (Stack, FreePort, WithPreBootHook, findDaemonBinary
prefers ~/go/bin - the fake child binary must be built by the suite into
the scratch bin dir and pointed at via voice config binary_path, NOT
installed). Suites carry the `e2e` build tag. Manifest format: see
docs/workflows/e2e-testing.md + existing entries (read one suite's manifest
block first). path_map entries are MANDATORY for every new package dir
(pre-commit hook [18/18] blocks otherwise).
Fake child: a tiny Go test helper binary (e2e/suites/voice-lifecycle/
testdata/fakechild/main.go) that connects the text socket, echoes setup,
turns any prompt into a canned final transcript, speaks say->audio bytes
to the .in socket, and emits interrupt on a trigger file/env - it replaces
the real jargo child so CI never loads ONNX.
Fake Telnyx: httptest server for webhooks (signed with a test Ed25519 key
whose public half is configured via voice config) + a websocket server the
daemon's media client... note: Telnyx DIALS IN to the media socket, so the
fake is a WS CLIENT connecting to the daemon's media route with a valid
signature over the configured public_stream_url (point public_stream_url
at the scratch daemon's own HTTP addr via ws:// - the signature check is
scheme-agnostic; document).
Barge-in scenario: fake child sends interrupt mid-say; assert the fake
Telnyx WS receives {"event":"clear"} and the second call's say stream is
unaffected.
Concurrency scenario: two employees (roster fixtures via WithPreBootHook
writing bot_definitions + constitutions into the REAL on-disk stores at
the resolved paths - proven seeding pattern), two numbers, simultaneous
calls; assert both progress and neither blocks the other beyond the fake
latency.
Fixed-sleep rule (repo defect class): every wait is a bounded poll on the
real observable (frame received, log line, store row), never a sleep sized
for one machine.

## Verified Anchors

- e2e/manifest.json - current shape (read before editing; MERGE both sides if a sibling changed it)
- e2e/harness/daemon.go - Stack + scratch bin + watchdog reaper
- docs/workflows/e2e-testing.md - tiers + manifest format
- e2e/suites/goal-loop/goal-loop_test.go - employee fixture seeding precedent
- 00T T10 parked suite files (activate here)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Three registered suites + manifest rows; `make e2e-fast-area AREA=voice-lifecycle`
(and -bargein, -concurrency) green in CI-hermetic conditions.

### What This Leaf Consumes

- C1-C10 end to end; leaf 14 wiring; voice config knobs (leaf 01)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Fake child helper binary | Create: e2e/suites/voice-lifecycle/testdata/fakechild/main.go | go build ./e2e/suites/voice-lifecycle/testdata/fakechild/ | - | compiles | Given spawn with socket path; When setup frame arrives; Then handshake answered |
| T2 | Shared fake-Telnyx helper (signed webhooks + WS media client) in e2e/suites/voice-lifecycle/ (exported for sibling suites via a small shared package under e2e/internal or duplicated per suite - check harness conventions first, record choice) | Create: helper files | go vet -tags e2e ./e2e/suites/voice-lifecycle/ | - | clean | Given test keypair; When webhook signed; Then daemon verifies |
| T3 | voice-lifecycle suite: boot armed (log line), inbound answer, greeting say observed at fake Telnyx, prompt->say roundtrip, hangup -> call ended row in calls.db + child idle-stops (poll process gone) | Activate: e2e/suites/voice-lifecycle/voice_lifecycle_test.go.staged; manifest rows | make e2e-fast-area AREA=voice-lifecycle | suite unregistered | PASS | Given scratch stack; When simulated inbound call; Then full frame sequence observed in order |
| T4 | voice-bargein suite: interrupt mid-say -> clear frame at fake Telnyx within one chunk interval; second concurrent call's say unaffected | Activate: e2e/suites/voice-bargein/*.staged | make e2e-fast-area AREA=voice-bargein | - | PASS | Given two calls; When barge-in on A; Then B's frames continue |
| T5 | voice-concurrency suite: two employees, two numbers, both calls progress; max_concurrent_calls=1 variant refuses the second inbound (unanswered) with a log reason | Activate: e2e/suites/voice-concurrency/*.staged | make e2e-fast-area AREA=voice-concurrency | - | PASS | Given cap 1; When second inbound; Then refused, first unaffected |
| T6 | Manifest MERGE: suites[] + scenarios + path_map for internal/voice, internal/voicechild, internal/voiceframe, cmd/meept-voice; re-read manifest immediately before writing (sibling sessions edit it); verify no duplicate suite names and every mapped suite has scenarios | Modify: e2e/manifest.json | go test -p 2 ./e2e/ -run TestManifest (or the repo's manifest completeness test - find it first) | - | PASS | Given merged manifest; When completeness test runs; Then green |
| T7 | Full fast tier regression: no other suite broke | - | make e2e-fast (background + notify; >10 min) | - | green | Given the tree; When fast tier runs; Then voice suites pass, no new failures |

## Self-Verification Checklist

- [ ] No live network, no real Telnyx, no ONNX/jargo in any suite
- [ ] Bounded polls only; zero fixed sleeps for readiness
- [ ] Manifest merged, not overwritten (diff shows only voice rows added)
- [ ] Fake child reaped by the suite (watchdog-compatible: binary under scratch dir)
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines per suite
3. **Deviations:** none / list (shared-helper choice recorded)

## Review Checklist (For Review Agent)

- [ ] Suites genuinely hermetic (grep for api.telnyx.com / real WS endpoints: none)
- [ ] Barge-in timing assertion is interval-bounded, not sleep-bounded
- [ ] Concurrency suite proves isolation (interleaved frame order recorded)
- [ ] Manifest completeness tests green; no scenarioless suite without allowlist justification

Output: APPROVED or specific gaps.

## Notes

If make e2e-fast shows a ROTATING failure set (different suites red each
run, green isolated), that is the known parallel-load class - re-run the
failed packages isolated before attributing to this leaf.
