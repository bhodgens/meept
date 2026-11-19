# Rumble STT sidecar wiring - Implementation Leaf

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
