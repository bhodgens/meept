# Parent baseline — wave 2026-11-06 (range 0a446a21..HEAD @ b3c03f80)

Gate: go build ./... = RC 0. go test -p 2 -count=1 ./... = 135 ok, 2 FAIL packages.
0 port-exhaustion errors (unlike prior wave baseline — tree is genuinely 2-red).

## PB-1 [PV] HIGH — voice_call permission row missing at HEAD; deterministic test FAIL
- internal/tools/builtin/voice_call_test.go:197 (TestVoiceCallPermissionActionRow) fails
  every run: pkg/security.BuiltinRules has no "voice_call" row.
- Root cause, traced: commit 9f8a974c (message claims "Docs-only", committed --no-verify)
  swept the ENTIRE voice-sidecar implementation out of main (cmd/meept-voice/main.go,
  internal/comm/http/voice.go+voice_plain.go, internal/daemon/voice_wiring.go (742 lines) +
  voice_employee.go + components.go voice block, internal/voice/*, config VoiceConfig,
  the voice e2e suites, the BuiltinRules row, the executor ToolActionMap row).
- The restore commit c265636d brought the implementation + tool + tests back, but NOT:
  1. pkg/security/permissions.go — `"voice_call": {Action: "voice_call", RiskLevel: RiskMedium, RequiresConfirmation: false}` row
  2. internal/agent/executor.go — `"voice_call": "voice_call"` ToolActionMap entry
- Blast radius: the voice_call tool registers but every employee call is DENIED
  "Unknown action" (unknown actions DENY by default) — outbound dial dead end-to-end.
  This is exactly the documented H7 class (json_extract, transcript_fetch, github_* precedents).
  The pin test catches it; the committed tree is RED (make test fails at HEAD).
- Verify: go test -count=1 -run TestVoiceCallPermissionActionRow ./internal/tools/builtin/ → FAIL.
- Fix shape: restore both entries verbatim from 0e36ed76 (git show 0e36ed76 -- pkg/security/permissions.go internal/agent/executor.go).

## PB-2 [PV] LOW/MED — rewake eviction test flakes under suite load
- internal/agent/rewake_test.go:348 TestRewake_BufferFullEvictsOldest: FAILS in the full
  -p 2 sweep, PASSES 3/3 isolated (-count=3).
- Mechanism: newest signal sig-10 dropped; buffered [sig-02..sig-09] — exactly ONE behind
  expected [sig-03..sig-10]. Eviction is performed by an async pump goroutine; the settle
  wait (5ms/iteration, added by fffe1fa3 for this same class) doesn't hold under scheduler
  pressure, so the test reads the channel before the pump has moved the last publish.
- Class: test flake (f-previous flake fix incomplete), not a product defect. Fix shape:
  poll until len(loop.rewakeCh) stops growing / bounded settle loop, or make the push path
  synchronous-under-test.

## Notes
- Working tree carries sibling WIP (.githooks/* modified + 2 untracked hooks, Makefile,
  NEXT_STEPS.md, .hermes stash files). Untouched by this wave; auditors told to ignore it.
- Auditors dispatched (8): agent-core, memory, voice, config-tools, llm-daemon, surfaces,
  regression, security → findings in .hermes/audits/2026-11-06-wave/<scope>.md.

---

# Closeout (2026-11-06)

## Auditor fate
Both delegation batches (deleg_c11bb653, deleg_605d4e56; 8 auditors) died mid-sweep when
the owner session exited ("outcome unknown"). Live transcripts survive at
~/.hermes/cache/delegation/live/<id>/task-*.log but NONE wrote a findings file — every
scope was killed in its read phase before the report step. The 8-scope audit is therefore
NOT DONE; only the parent-baseline findings (PB-1, PB-2) are confirmed, both fixed below.
Re-dispatch required for a complete bughunt report.

## PB-1 FIXED (HIGH) — voice_call permission entries restored
- pkg/security/permissions.go: added `"voice_call": {Action: "voice_call",
  RiskLevel: RiskMedium, RequiresConfirmation: false}` to BuiltinRules (MEDIUM per
  send_message precedent; real policy lives in voice.Dialer.Dial). Without the row the
  tool DENIED every employee dial with "Unknown action" — outbound dial dead end-to-end.
- internal/agent/executor.go: restored the `"voice_call": "voice_call"` ToolActionMap
  entry (the restore commit c265636d had missed both rows swept out by 9f8a974c).
- Verified: TestVoiceCallPermissionActionRow ok, TestToolActionMapEntriesAreKnownActions
  ok (both FAILED before the fix), pkg/security + builtin voice suites ok.

## PB-2 FIXED (LOW/MED) — rewake eviction test made deterministic
- internal/agent/loop_rewake.go: UNCHANGED (production code correct as-is; the
  evict-once-then-drop fallback is required — a blocking send deadlocks stopRewake's
  rewakeWG.Wait, demonstrated during this fix).
- internal/agent/rewake_test.go TestRewake_BufferFullEvictsOldest: replaced the
  len==capacity polling settle (races the still-running pump under suite CPU pressure —
  the drain started mid-pipeline, one signal behind) with `loop.stopRewake()` BEFORE
  the length check: unsubscribe closes sub.Channel, the pump drains the buffered
  subscription channel to exhaustion applying the eviction policy, WaitGroup close
  serializes, rewakeCh is then closed with exactly the post-eviction state.
- Verified: go test -run TestRewake -count=3 -race ok; isolated ok; polling version had
  false-failed in the -p 2 full sweep.
