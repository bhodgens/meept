# Two-Week Plan Audit — Findings Ledger (2026-11-08)

Scope: all plan trees under `docs/plans/` created 2026-10-25 .. 2026-11-08,
validated against the actual working tree at HEAD+dirty state.

Method: parallel audit subagents (wave 1: 10, wave 2: 2) verify each tree's
planned deliverables (files, symbols, tests, config keys, wiring) against code.
Parent re-verifies every CRITICAL/HIGH finding in-session before the fix wave
(subagent self-reports carry a measured ~30% false-positive rate).

Status: AUDIT IN PROGRESS. Findings appended below per tree, each tagged
[SEVERITY][category] with file:line evidence. Categories: omission,
unwired-stub, stub, incomplete-impl, bug, security.

Trees in scope (24):
20261030-agent-activity-audit-views, 20261030-agent-groups,
20261030-memory-second-brain, 20261031-agent-boundary-hardening,
20261031-context-memory-engine, 20261031-hitl-notifications,
20261031-jev-command-gate, 20261031-llm-hop-tracing,
20261031-llm-serving-tiers, 20261031-tool-provider-seam,
20261031-trajectory-golden-ci, 20261031-turn-journal,
20261101-fresh-context-critic, 20261101-security-gate-followups,
20261102-dsh-convergence, 20261102-voice-sidecar,
20261103-wiring-followups-final, 20261103-wiring-gap-remediation,
20261106-voice-local-leg, 20261106-voice-tierA-qwen3-tts,
budget-status-consistency, e2e-boot-hardening, token-efficiency,
github-intake-factory

---

## WAVE 1 FINDINGS (2026-11-08, audited vs working tree; parent re-verification pending for fix items)

- ACT-1 [MEDIUM][bug] internal/daemon/activity_wiring_test.go:126 — TestActivityWiring_AllNilHandlesNeverPanics is committed RED. Lint commit 7ea05edd changed the test to assert err==nil for activity.overview/findings/agents.review, but those handlers correctly return clean errors when EmployeeManager is nil (typed-nil contract the test comment itself states). Fix is test-side. Blocks a clean ./internal/daemon run (confirmed independently by 4 sibling audits).
- ACT-2 [LOW][drift] activity tree: leaf-04 Dart method naming + missing `total` field in recent result; C5 grew additive ExportInterval field; ring capacity hardcoded const (leaf said configurable, default 1000); dead placeholder const defaultOTLPTracesHeaderTrimLen=0 (otlp_traces.go:57); activity.overview runs unbounded full-store query per call.
- GRP-1 [MEDIUM][unwired-stub] Boot-time C6 group validation inert: internal/daemon/components.go:1022 constructs groups.NewResolver WITHOUT WithKnownTools; ValidatePolicies (internal/groups/resolution.go:186-188) returns nil when known==0, so a policy naming an unregistered tool boots fine. Comment claims boot fails naming the offender. RPC leg is wired; boot leg is not; no e2e covers the boot leg.
- GRP-2 [MEDIUM][unwired-stub] Daemon template loop (components.go:2494 agent.NewAgentLoop("daemon",...)) never receives a group resolution record; under default_managed=true a plain chat turn on the template lane carries nil record and skips the call-time gate (executor.go:2030). Bypass class leaf 04 claims to close; no e2e covers default_managed=true on template lane.
- GRP-3 [LOW][drift] rule_source "override" is in the frozen C2 vocabulary but never stamped (override-rescued allows emit group_allow). Plus stale PENDING status fields in master.md Test Gate + two branch orchestrators despite green suites.
- MEM-1 [LOW][drift] memory-second-brain: bookkeeping drift only (Test Gate matrix PENDING vs green suites). Tree otherwise COMPLETE and verified.
- BND-1 [HIGH][unwired-stub] agent-boundary-hardening: Swapper.Mask (internal/security/pii/swapper.go) has ZERO production callers. The pre-LLM masking half of the PII proxy never got wired; with [security.pii].enabled=true nothing mints [PII:] tokens; restore lane has nothing to restore in live chat path (secret #XXXX# lane does work). No leaf task covers input-side masking - plan-level omission; e2e asserts the OPPOSITE of contract C1-C scenario 03(a) (restored originals in provider-bound history, main_test.go:370-378).
- BND-2 [MINOR] handoff default is warn/monitor not block (documented decision, user-facing wording unclear); pii_swap sqlite stores plaintext swap values (0700/0600 mitigated); agent_handoff_mode missing config template comment; line-cite drift throughout.
- CME-1 [LOW][drift] context-memory-engine: clean tree; seam signature drift (windowTokens param), stale master anchors, TestSuiteScaffolds name never landed (equivalent coverage exists). No action needed beyond doc touch-up.
- HITL-1 [MEDIUM][incomplete-impl] goal-gate checkpoint parks are test-only: no production code parks/resumes GateKind:"goal" (only "plan"); Test Gate F4 claims "plan and goal gates" VALIDATED. Disclosed deviation in commit 52148db1 but F4 row overstates. 
- HITL-2 [LOW][drift] SlackConfig.ValidateEnabled never called (validation at wiring-time warn+disable, not config-load error); harness cites nonexistent topic name acp.permission.decision (actual .requested/.resolved).
- JEV-1 [MAJOR][bug] e2e/suites/jev-command-gate/capture_test.go:121 uses rec.Ts but commit 7ea05edd renamed CommandGateRecord.Ts -> TS (internal/gatecapture/store.go:65) without updating the suite - the e2e suite NO LONGER COMPILES. One-line fix. Pre-commit e2e gate missed/bypassed on that commit.
- JEV-2 [MINOR] No automated assertion that serializer output matches contract serialization_template; config/meept.json5 lacks jev_command_gate template block (T5 deliverable dropped); two stale freeze TODOs (serialize.go:14, command_score.go:59); citation drift in leaf 01 + master.md:375.
- HOP-1 [LOW][drift] llm-hop-tracing clean; make graphs-check stale repo-wide (line offsets, known F66); agent.event.tool_execution_end "never published" is a generator limitation (string-concat topic).
- TIER-1 [MAJOR][unwired-stub] llm-serving-tiers: degraded-tier metadata stamped into llm.Response.Metadata (models.go:207, json:"-") with ZERO consumers outside internal/llm - no bridge to agent step metadata, WS, TUI, or GUI. The "user-visible served by degraded tier" requirement is unmet; tracking claims COMPLETE.
- TIER-2 [MEDIUM][incomplete-impl] ttft e2e value assertions (config_ttft_test.go per leaf 05) never landed though leaf 04 (migration v9) has landed - deferral rationale stale; Integration Test Plan item 4 unmet.
- TIER-3 [MEDIUM][omission] Phase 5 gate skipped: root AGENTS.md + internal/llm/AGENTS.md never updated for tier policy / semantic cache / TTFT.
- TIER-4 [MINOR][drift] C4 default 0.92 vs shipped 0.95 (superseded by R0, master never corrected); VerifyFloor absent from frozen C4; stale manifest note (e2e/manifest.json:1135); citation drift in VERIFIED ANCHORS.
- SEAM-1 [LOW][drift] tool-provider-seam clean and wired; leaf-05 "third-party-style e2etest provider" deviation unrecorded; minor test-name drift in gate matrix.

## RECURRING CROSS-TREE FACT
TestActivityWiring_AllNilHandlesNeverPanics (ACT-1) fails at committed HEAD and blocks ./internal/daemon for every tree. Highest-priority fix; test-side.

(Waves 2 findings append below.)

## WAVE 2 FINDINGS (2026-11-08; parent re-verified the HIGH items in-session — all CONFIRMED)

- TRJ-1 [LOW][drift] trajectory-golden-ci + turn-journal: both fully landed/wired/green. Master "70 suites" stale (actual 95); turn-journal has 2 disclosed known-gaps (RecoverStaleTasks force-fail row state; multi-round OpenRuns) missing from master delta log.
- FC-1 [HIGH][drift] fresh-context-critic: commit 977d4044 (2026-11-03) inverted C1's frozen "default false, ships dark" contract to disable-form keys defaulting ON, one day after COMPLETE; master/leaves never got the dated addendum. Consequence: TierComplex plans cannot self-seal by default (hostile slot empty -> hostile_skipped -> human seal). Code is deliberate + tested; the TREE is the stale artifact.
- FC-2 [MED][contract] C5 flag_off skip-reason never fires (flag-off is structural); C1 test renamed TestCriticFlagsDefaultOn. Doc deltas.
- SGF-1: security-gate-followups 4/4 DONE, verified. Only cross-tree ACT-1 affects its daemon package run.
- DSH-1 [HIGH][unwired-stub] CONFIRMED: fork mode never seeds from a real parent transcript - childPrompt (delegate.go:430) calls m.forkSeed(nil); completedTurnPrefix(nil) returns nil -> fresh brief. SetForkSeed has zero production callers; Spawn call at internal/tools/builtin/platform.go:487 passes "". Slicer logic itself is real and tested.
- DSH-2 [HIGH][unwired-stub] CONFIRMED: session projections + storage events inert - SetProjectionRegistry/SetStorageEventEmitter defined (store_sqlite.go:1501-1518) with zero production callers; components.go:3005 builds store without either; storage.record.changed has no subscriber.
- DSH-3 [HIGH][incomplete-impl] CONFIRMED (in-source): goal round driver autonomy is a declared future seam (goal_round_driver.go:12-13) - advances only via human RPC/CLI; commit fb8f6d73 + tracking claim "autonomous". Cap/auto-block/disarm machinery real.
- DSH-4 [HIGH][unwired-stub] durable skill catalog test-only: NewSkillCatalog only called within context_injector.go itself; prompt path still uses CachedNames().
- DSH-5 [MED][unwired-stub] CONFIRMED: modelselect.PreflightRoute zero production callers; no delegation.allowed_routes config key; delegate_task tool lacks mode/model/provider/reasoning_effort/depth params.
- DSH-6 [MED][bug] CONFIRMED: Spawn(ctx, req, "") at platform.go:487 - children registered under blank parent session; control tools then require explicit parent_session_id.
- DSH-7 [MED][bug] CONFIRMED: CanDelegate strips only delegate_task (registry.go:971-973); subagent_send_message/interrupt/list_children leak to can_delegate:false agents when additional_tools granted.
- DSH-8 [MED][bug] workflow RunBackground (delegate_workflow.go:375) = bare goroutine + fabricated job id, no completion delivery (violates leaf's no-second-registry rule).
- DSH-9 [MED][drift] timing kinds serialize TimingRRule etc. instead of "rrule"/"at" (timing.go:21-25); branch-05 e2e scenarios + branch-04 F10 gates (.staged files) never landed; plan-mode persistence process-local; branch-01 docs/CLI gap (no delegation.md, no delegate CLI).
- VS-1 [HIGH][test-defect] internal/comm/http TestVoiceStartFrameSkipAndParse + TestVoiceMediaHandoffIdempotent panic at HEAD: lint commit 7ea05edd added `defer resp.Body.Close()` but coder/websocket 101 responses carry nil Body (voice_test.go:330-331).
- VS-2 [HIGH][drift] voice-sidecar tracking table all-PENDING though 15/16 leaves landed+wired; leaf 16 partial: Makefile/magefiles lack meept-voice build/install, features.md row + git-hooks mapping missing.
- VS-3 [MED][flake] internal/voicechild RMS barge-in tests fail rotatingly under -p 2 load (timing); pass isolated.
- VS-4 [LOW] env name mismatch JARGO_ONNXRUNTIME_LIB (main.go:103) vs VOICE_ONNX_LIB (childproc.go:366); registry in dial.go not registry.go; TestVoiceDTMF renamed.
- WGR-1: wiring-gap-remediation 8/8 FIXED; wiring-followups-final 5/5 FIXED - both honest. Only JEV-1 (rec.Ts build break) blocks the jev e2e gate today.
- VLC-1 [MED][flake] TestRMSDisabledOnPhoneLeg fails ~75% scoped on this host (5s poll timeout); timing fix or named-flake annotation needed.
- VLC-2 [LOW][doc] internal/voice/AGENTS.md stale file:line cites; master tracking row 07 pipe-corrupt (4 cells vs 5).
- TTS-1: voice-tierA-qwen3-tts COMPLETE and accurate; Test Gate matrix rows still PENDING; C2 alias deviation unannotated.
- INT-1 [MED][functional-risk] github-intake-factory projection: sub-issue create uses {title,body} on the sub_issues endpoint; hosted GitHub parents via {issue_number} of an existing issue -> live 422 risk. Recorded deviation, no follow-up tracked. FIX: two-step form.
- BUD-1 [LOW] comm/http legacy-tuple bridge passes 0,0 token limits vs RPC twin passing hl,dl.
- TE-1: token-efficiency + budget-status + e2e-boot-hardening all COMPLETE/accurate (minor notes only).
- CROSS: TestActivityWiring_AllNilHandlesNeverPanics (ACT-1) is the single committed RED blocking ./internal/daemon; make graphs-check RED repo-wide (line-offset drift, needs `make graphs`).

## VERDICT SUMMARY (24 trees)
- Fully landed+accurate: memory-second-brain, context-memory-engine, hitl-notifications, llm-hop-tracing, tool-provider-seam, trajectory-golden-ci, turn-journal, security-gate-followups, wiring-gap-remediation, wiring-followups-final, voice-tierA-qwen3-tts, budget-status-consistency, e2e-boot-hardening, token-efficiency (14)
- Landed w/ real code gaps needing fixes: agent-boundary-hardening (PII mask unwired), llm-serving-tiers (tier signal undelivered), agent-groups (boot C6 + template loop), agent-activity-audit-views (RED test), voice-sidecar (leaf16 build/docs + panic tests), dsh-convergence (fork-seed, projections, catalog, modelselect, CanDelegate, round-driver), github-intake-factory (sub-issue 422), fresh-context-critic (doc addendum) (8)
- Jeopardy items: jev-command-gate (e2e suite build break), voice-local-leg (flaky gate test) (2)
