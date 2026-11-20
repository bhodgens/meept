# Fix-wave scratch: 2026-11-19 bughunt (parent notes)

Anchor report: .hermes/audits/2026-11-19-bughunt-20day.md
HEAD at dispatch: 7a5da96e (main). Sibling dirty file excluded everywhere:
e2e/suites/delegate-seam/delegate_seam_test.go

## Wave 1 fixers (7, disjoint ownership, DO NOT COMMIT - parent commits)
- sa-0-887e6ff2 providers A: H2 (--prompt), M4, M5, L8, H3, M2, L18
  owns stt_whisper*.go, pseudostream.go, tts_piper.go + tests
- sa-1-10997470 providers B: H4 (+TestEndpointGuards), M1, L1, L9 (steered)
  owns stt_deepgram*.go, pipeline.go, clips.go + tests
- sa-0-a079a113 daemon: M6, M18-clamp, L3, L4, L5, L12
  owns daemon voice/kanban/roster files, voice/{contextwarm,events,talkloop},
  config/schema.go (Normalize+kanban comments), kanbansvc/AGENTS.md
- sa-1-a2772a75 agent/llm: M7, M8, L10, L11, L19, stale comments
  owns collaboration*, context_injector, context_firewall, groups_service,
  doctor_groups, rpc/groups.go
- sa-0-1512145e TUI: M10, M12, M13, L14, L15, L17; M11 report-only
- sa-1-0ab0a96e GUI: H5, M17, M14, M15, M12-GUI
- sa-0-1bce11a0 mechanical: H6 manifest, M16 set-gate, M9 search; L13 report-only

## Parent direct edits
- L2: internal/voicechild/keys.go comments (daemon-owned selectors) - DONE

## Deferred to wave 2 (after wave-1 commits land)
- H1: Rumble sidecar spawn wiring. Design recon:
  ContainerManager = llm.RuntimeManager (components.go:1590);
  RegisterConfig(providerID, *llm.RuntimeConfig, baseURL) at :2125;
  StartAll async from daemon.Run. Plan: register a "rumble-stt" runtime
  from cfg.Voice.RumbleRuntime when stt_provider=rumble (opt-in),
  SpawnCommand from daemon.RumbleRuntimeConfig.SpawnArgs(), derive
  stt_base_url in voice_keys.go when unset. GPU clamp lands in wave 1
  (M18). Files: components.go, voice_keys.go, rumble_wiring.go,
  config/schema.go - all freed after wave-1 commits.
- M3/L6/L7: STALE at HEAD - the sibling native-WS wave (e335ea15) rewrote
  stt_rumble.go as a WebSocket streaming adapter: no temp WAVs (L6 gone),
  no HTTP JSON decode (L7 gone), no silence-sample window to hang (M3
  gone - server-side endpointing). Verified by parent grep: zero
  CreateTemp/ReadFile/LimitReader/silenceCutoff hits. No fix needed.
  NEW small observation: flush write failures log DEBUG only
  (stt_rumble.go:150,162) - minor observability, wave-2 candidate.
- M11: needs RosterEntry wire key executor_agent (rpc/roster.go frozen
  C1 9-key contract - product decision; TUI fixer reports proposed patch).
- L13: components.go escalation ids - mechanical fixer reports patch;
  parent applies in wave 2 (file owned by daemon fixer in wave 1).
- L16: rename vs auto-namer race (both surfaces) - small, wave 2.
- NEW (GUI fixer observation): internal/agent/loop.go:11200-11225
  periodic title refresher (handleRefreshTitle -> UpdateDescription)
  overwrites a user-renamed DESCRIPTION server-side, so a rename can
  still be lost after enough turns even with the H5 display fix.
  Durable renames need a backend user-set/renamed guard - wave 2.
- M11 proposed patch received (TUI fixer): additive omitempty keys on
  RosterEntry (executor_agent, drift_score, findings_count,
  last_invocation) + roster_rpc.go population from BotDefinition/
  BotState/Manager accessors. Touches the frozen C1 9-key contract -
  needs owner sign-off; wave 2 candidate.
- Known-deferred (unchanged, product decisions): warm conv/session id
  mismatch; clipstore 0-byte Has; BackchannelSoften reload; client.go
  admission gate (sibling lane).

## Commit plan (parent, per concern, explicit pathspecs)
1. voicechild providers fixes (H2/H3/H4/M1/M2/M4/M5/L1/L8/L9/L18 + L2 comments)
2. daemon/voice wiring fixes (M6/M18/L3/L4/L5/L12)
3. agent/llm/groups fixes (M7/M8/L10/L11/L19)
4. TUI fixes (M10/M12/M13/L14/L15/L17)
5. GUI fixes (H5/M17/M14/M15/M12)
6. mechanical (H6/M16/M9)
Then full gate matrix: go build ./... + go test -p 2 -count=1 ./... +
targeted -race + manifest suite + flutter test. Update audit report with
disposition table. Wave 2: H1 + re-audited rumble items + L13/L16/M11.