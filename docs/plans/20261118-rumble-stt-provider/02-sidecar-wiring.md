# Rumble STT sidecar wiring - Implementation Leaf

> **STATUS (2026-11-19, bughunt H1 fix wave): T2 SUPERSEDED - NOT WIRED, BY
> DESIGN.** The supervised-vLLM-spawn task below was closed COMPLETE at
> f8fa6c43 but only the config structs + spawn-arg renderer ever landed;
> no registration with ContainerManager/RuntimeManager was ever written
> (bughunt 2026-11-19 H1: tested-but-never-wired stub, all three helpers
> flagged unreachable by deadcode). Before wiring it, the follow-up tree
> `20261119-rumble-native-streaming` (commits e335ea15 / f5e384a2 /
> f6e8d826, closed abd1c2d5) REPLACED the batch adapter with a native
> WebSocket streaming client against an operator-deployed FastAPI +
> transformers server (`socrates:8000`). vLLM CANNOT serve the native
> route `/v1/audio/transcriptions/stream` for this checkpoint
> (`docs/research/rumble2flash-review.md`: `stream=True` is output-token
> streaming after the FULL audio arrives; `/v1/realtime` requires causal
> encoders while Rumble's AuT encoder is bidirectional). A daemon-spawned
> vLLM sidecar would be health-green but unusable by the only shipped
> client, so wiring T2 as written would ship a broken feature.
>
> **As-built disposition (triple gate: unreachable in every main, zero
> textual callers repo-wide, this plan record):**
> - T1 (config): LANDED and kept - `config.VoiceRumbleRuntimeConfig`
>   (`internal/config/schema.go`) is now the ONE source of truth; the
>   duplicate `daemon.RumbleRuntimeConfig` was deleted.
> - T2 (supervised spawn): SUPERSEDED. `DefaultRumbleRuntimeConfig`,
>   `SpawnArgs`, `BaseURL` (daemon copies) deleted from
>   `internal/daemon/rumble_wiring.go`; the file now carries ONLY the
>   wired `resolveSTTBaseURL` derivation.
> - The consumer gap T2 existed to close IS wired: `stt_vendor=rumble`
>   with an empty `voice.stt_base_url` now derives the dial URL from
>   `rumble_runtime.endpoint_key`
>   (`internal/daemon/voice_keys.go` -> `resolveSTTBaseURL`,
>   `config.VoiceRumbleRuntimeConfig.BaseURL()`); an explicit operator
>   `stt_base_url` always wins byte-identically (the live transformers
>   path). Pins: `TestResolveSTTBaseURL`,
>   `TestVoiceKeysEmissionSTTBaseURL[LocalLeg]` (daemon),
>   `TestVoiceRumbleRuntimeConfigDefaults`, `TestVoiceRumbleBaseURL`
>   (config). The F5 GPU-cap gates survive config-side
>   (`TestNormalizeVoiceRumbleDefaults`, `TestRumbleClampWiredAtDaemonBoundary`).
> - Opt-in byte-identity: unset/whisper/deepgram vendors send the exact
>   pre-change wire; NOTHING spawns for ANY provider (pinned).
> - T3 (docs): as-built truth in `docs/workflows/voice-sidecar.md` and
>   the `[voice.rumble_runtime]` template comments - the block is
>   legacy-server knobs (manual vLLM coexistence values + the derived
>   default dial URL), never a daemon spawn source.

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> exist. Do NOT delete/skip/weaken gate tests. Do NOT commit. Explore with
> search_files/terminal. Write once and stop. Follow runtime lifecycle
> invariants in `internal/daemon/AGENTS.md` exactly.

## Meta

- **Parent:** ../master.md
- **Scope:** Supervised vLLM Rumble sidecar + explicit GPU memory cap
- **Dependencies:** 00T + 00Te at TESTS_VALIDATED
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** F5

## Goal

Wire a supervised vLLM sidecar for Rumble on socrates' RTX 3060, with an
explicit low GPU-memory fraction (0.20-0.25) so it coexists with qwen3-tts.
Follow the existing runtime lifecycle: no spawn into a served endpoint,
supervisor process, startup orphan sweep is the only reaper, health-gated.

## Context

- `internal/daemon/AGENTS.md` — runtime lifecycle invariants (read fully)
- `internal/daemon/daemonpath.go` — DaemonPath for spawned processes
- `internal/config/schema.go` — runtime/endpoint config structs
- `config/meept.json5` — runtime config template
- `docs/research/rumble2flash-review.md:16,67` — GPU cap requirement

## Verified Anchors

- `internal/daemon/AGENTS.md` — spawn guards + supervisor + orphan sweep
- `internal/daemon/daemonpath.go` — DaemonPath
- `docs/research/rumble2flash-review.md:67` — gpu-memory-utilization 0.20-0.25

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Rumble vLLM runtime config entry:
  endpoint_key: "rumble:<addr>"
  spawn_command: "vllm serve Reson8/Rumble-2-Flash --max-model-len 4096 --gpu-memory-utilization 0.20"
  health: /health
  auto_stop_on_exit: true
// Consumer: 01-rumble-adapter.md via voice.stt_base_url
```

### What This Leaf Consumes

- C6 sidecar runtime lifecycle (existing pattern)
- C5 config selector — `voice.stt_base_url` points at the sidecar

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Config struct + template for Rumble runtime | Modify: `internal/config/schema.go`, `config/meept.json5` | `go test ./internal/daemon/ -run TestRumbleRuntimeConfigCap` | undefined config | PASS | Given config; When loaded; Then gpu_memory_utilization <= 0.25 explicitly set |
| T2 | Runtime wiring with spawn guards | Create/Modify: `internal/daemon/rumble_wiring.go` | `go build ./cmd/meept-daemon` | undefined symbol | PASS | Given endpoint already listening; When spawn attempted; Then refused; Given clean; Then supervised spawn |
| T3 | Docs for sidecar operation | Modify: `docs/workflows/voice-sidecar.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then GPU cap rationale + coexistence documented |

T1 detail: add optional `[voice.rumble_runtime]` config (or reuse existing
runtime endpoint map if one exists — check before adding). The default spawn
command MUST include `--gpu-memory-utilization 0.20` and
`--max-model-len 4096`. Never allow 0.92 default.

T2 detail: if a generic runtime endpoint config already exists, add an entry,
NOT a new spawn implementation. Reuse `RuntimeProcess`, supervisor,
`SweepOrphanRuntimes`. Do not spawn into a served endpoint. Health endpoint
`/health`. auto_stop_on_exit true.

## Self-Verification Checklist

- [ ] GPU cap explicit and tested; no 0.92 default path
- [ ] Uses existing runtime lifecycle; no new spawn code path
- [ ] Docs updated; gate tests unmodified; gofmt clean
- [ ] No scope creep into 01/03 files

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] GPU cap <= 0.25 pinned; spawn guards honored; supervisor used
- [ ] No new parallel lifecycle implementation
- [ ] Gate tests unmodified; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

The 3060 runs qwen3-tts at ~6.6 GB. Rumble ~2.5-4 GB (vLLM) must fit the
remaining ~5.6 GB. If a live pilot shows OOM, lower the cap further —
document as an operator knob, not a code change.
